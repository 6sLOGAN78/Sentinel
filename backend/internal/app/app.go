// Package app provides the shared process lifecycle for Sentinel binaries:
// configuration, logging, dependency checks, signal handling and ordered
// shutdown.
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/logger"
)

const (
	// shutdownTimeout bounds the time spent closing resources after a signal.
	shutdownTimeout = 30 * time.Second
	// DialTimeout is the default reachability timeout for dependencies.
	DialTimeout = 5 * time.Second

	exitOK    = 0
	exitError = 1
)

type closer struct {
	name string
	fn   func(context.Context) error
}

// Lifecycle collects resource closers and runs them in reverse registration
// order.
type Lifecycle struct {
	log     zerolog.Logger
	closers []closer
}

// NewLifecycle returns an empty Lifecycle that logs closes to log.
func NewLifecycle(log zerolog.Logger) *Lifecycle {
	return &Lifecycle{log: log}
}

// Add registers a closer. Register immediately after the resource is opened.
func (l *Lifecycle) Add(name string, fn func(context.Context) error) {
	l.closers = append(l.closers, closer{name: name, fn: fn})
}

// Names returns the registered resource names in registration order.
func (l *Lifecycle) Names() []string {
	names := make([]string, 0, len(l.closers))
	for _, c := range l.closers {
		names = append(names, c.name)
	}

	return names
}

// CloseAll runs every closer in LIFO order. A failing closer does not stop
// the others; all errors are joined.
func (l *Lifecycle) CloseAll(ctx context.Context) error {
	var errs []error

	for i := len(l.closers) - 1; i >= 0; i-- {
		c := l.closers[i]

		if err := c.fn(ctx); err != nil {
			l.log.Error().Err(err).Str("resource", c.name).Msg("close failed")
			errs = append(errs, fmt.Errorf("close %s: %w", c.name, err))

			continue
		}

		l.log.Info().Str("resource", c.name).Msg("closed")
	}

	l.closers = nil

	return errors.Join(errs...)
}

// CloseRedis adapts a Redis client to a Lifecycle closer.
func CloseRedis(c *redis.Client) func(context.Context) error {
	return func(context.Context) error { return c.Close() }
}

// SetupFunc opens a binary's dependencies and registers a closer for each.
type SetupFunc func(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *Lifecycle) error

// Run loads configuration, runs setup, blocks until SIGINT or SIGTERM, then
// closes everything. It returns the process exit code.
func Run(service string, setup SetupFunc) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return RunContext(ctx, service, setup)
}

// RunContext is Run with an injected context: cancelling ctx stands in for a
// signal.
func RunContext(ctx context.Context, service string, setup SetupFunc) int {
	cfg, err := config.Load(service)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: configuration error: %v\n", service, err)

		return exitError
	}

	log := logger.New(cfg.Observability, service)
	lc := NewLifecycle(log)

	if err := runSetup(ctx, setup, cfg, log, lc); err != nil {
		log.Error().Err(err).Msg("startup failed")

		if cerr := closeWithTimeout(lc); cerr != nil {
			log.Error().Err(cerr).Msg("cleanup after failed startup")
		}

		return exitError
	}

	log.Info().Str("dependencies", strings.Join(lc.Names(), ",")).Msg("service started")

	<-ctx.Done()

	log.Info().Msg("shutdown signal received")

	if err := closeWithTimeout(lc); err != nil {
		log.Error().Err(err).Msg("shutdown incomplete")

		return exitError
	}

	log.Info().Msg("shutdown complete")

	return exitOK
}

func runSetup(ctx context.Context, setup SetupFunc, cfg *config.Config, log zerolog.Logger, lc *Lifecycle) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during setup: %v", r)
		}
	}()

	return setup(ctx, cfg, log, lc)
}

func closeWithTimeout(lc *Lifecycle) error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return lc.CloseAll(ctx)
}

// DialTCP checks that addr accepts a TCP connection within timeout.
func DialTCP(ctx context.Context, addr string, timeout time.Duration) error {
	d := net.Dialer{Timeout: timeout}

	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}

	return conn.Close()
}

// DialBrokers dials every address in the comma-separated brokers list.
func DialBrokers(ctx context.Context, brokers string, timeout time.Duration) error {
	for _, addr := range strings.Split(brokers, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}

		if err := DialTCP(ctx, addr, timeout); err != nil {
			return fmt.Errorf("kafka broker unreachable: %w", err)
		}
	}

	return nil
}
