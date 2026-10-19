// Package opshttp serves Sentinel liveness and dependency readiness probes on
// a listener that is isolated from public application traffic.
package opshttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const (
	maxConcurrentReadiness = 16
	readHeaderTimeout      = 5 * time.Second
	readTimeout            = 5 * time.Second
	writeTimeout           = 15 * time.Second
	idleTimeout            = 30 * time.Second
)

const (
	stateOK          = "ok"
	stateUnavailable = "unavailable"
	statusLive       = "live"
	statusReady      = "ready"
	statusNotReady   = "not_ready"
	statusDraining   = "draining"
)

// Check is one named, context-bounded dependency operation. Names are trusted
// service wiring constants; errors are never returned to probe clients.
type Check struct {
	Name string
	Fn   func(context.Context) error
}

// Draining is the process-wide readiness gate used during graceful shutdown.
type Draining struct{ active atomic.Bool }

// Begin permanently marks the process as draining. It is safe and idempotent.
func (d *Draining) Begin() {
	if d != nil {
		d.active.Store(true)
	}
}

// Active reports whether graceful shutdown has begun.
func (d *Draining) Active() bool { return d != nil && d.active.Load() }

type response struct {
	Status       string            `json:"status"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}

type handler struct {
	timeout  time.Duration
	draining *Draining
	checks   []Check
	sem      chan struct{}
	metrics  *DetectionMetrics
}

type checkResult struct {
	name  string
	state string
}

// NewHandler returns /healthz, /readyz and an optional fixed detector metric. Readiness admits
// at most sixteen concurrent requests; saturation launches no checks.
func NewHandler(timeout time.Duration, draining *Draining, checks []Check, options ...Option) http.Handler {
	if timeout <= 0 {
		timeout = time.Second
	}

	h := &handler{
		timeout: timeout, draining: draining,
		checks: append([]Check(nil), checks...),
		sem:    make(chan struct{}, maxConcurrentReadiness),
	}
	for _, option := range options {
		option(h)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	if h.metrics != nil {
		mux.HandleFunc("GET /metrics", h.serveMetrics)
	}

	return mux
}

func (h *handler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, response{Status: statusLive})
}

func (h *handler) readyz(w http.ResponseWriter, r *http.Request) {
	if h.draining.Active() {
		writeJSON(w, http.StatusServiceUnavailable, response{Status: statusDraining})

		return
	}

	select {
	case h.sem <- struct{}{}:
	default:
		writeJSON(w, http.StatusServiceUnavailable, response{Status: statusNotReady})

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	states := make(map[string]string, len(h.checks))
	for _, check := range h.checks {
		states[check.Name] = stateUnavailable
	}
	results := make(chan checkResult, len(h.checks))
	go h.runChecks(ctx, results)

	for range h.checks {
		select {
		case result, ok := <-results:
			if ok {
				states[result.name] = result.state
			}
		case <-ctx.Done():
			writeJSON(w, http.StatusServiceUnavailable, response{Status: statusNotReady, Dependencies: states})
			return
		}
	}
	ready := ctx.Err() == nil
	for _, state := range states {
		ready = ready && state == stateOK
	}

	if !ready {
		writeJSON(w, http.StatusServiceUnavailable, response{
			Status: statusNotReady, Dependencies: states,
		})

		return
	}

	writeJSON(w, http.StatusOK, response{Status: statusReady, Dependencies: states})
}

// Retain admission until the actual checks finish, even after the response
// deadline. A client ignoring cancellation can consume at most sixteen workers.
func (h *handler) runChecks(ctx context.Context, results chan<- checkResult) {
	defer func() {
		// Keep dependency panics isolated as they were in the HTTP handler.
		_ = recover()
		<-h.sem
		close(results)
	}()
	for _, check := range h.checks {
		state := stateUnavailable
		if check.Fn != nil && ctx.Err() == nil {
			if err := check.Fn(ctx); err == nil && ctx.Err() == nil {
				state = stateOK
			}
		}
		// Buffered for every declared check: completion cannot block on a
		// response that has already timed out or disconnected.
		results <- checkResult{name: check.Name, state: state}
	}
}

func writeJSON(w http.ResponseWriter, status int, body response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Server owns a synchronously bound ops listener and its serving goroutine.
type Server struct {
	srv      *http.Server
	listener net.Listener
	done     chan struct{}
	errMu    sync.RWMutex
	serveErr error
}

// Start binds cfg synchronously, so address conflicts fail service startup,
// then begins serving in the background.
func Start(cfg config.OpsConfig, draining *Draining, checks []Check, options ...Option) (*Server, error) {
	addr := net.JoinHostPort(cfg.BindAddress, cfg.Port)
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	s := &Server{
		srv: &http.Server{
			Addr:              addr,
			Handler:           NewHandler(cfg.CheckTimeout, draining, checks, options...),
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
		},
		listener: listener,
		done:     make(chan struct{}),
	}

	go s.serve()

	return s, nil
}

func (s *Server) serve() {
	defer close(s.done)

	err := s.srv.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}

	s.errMu.Lock()
	if err != nil {
		s.serveErr = fmt.Errorf("serve: %w", err)
	}
	s.errMu.Unlock()
}

// Done closes after the serving goroutine exits.
func (s *Server) Done() <-chan struct{} { return s.done }

// Err returns an unexpected listener/serve error after Done closes.
func (s *Server) Err() error {
	s.errMu.RLock()
	defer s.errMu.RUnlock()

	return s.serveErr
}

// Addr returns the actual bound address, including a kernel-selected port.
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Shutdown drains handlers, closes the listener, and waits for Serve to exit.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}

	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	select {
	case <-s.done:
		return s.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DrainCloser marks readiness unavailable and optionally holds the ops
// listener open for a bounded propagation interval before shutdown continues.
func DrainCloser(draining *Draining, hold time.Duration) func(context.Context) error {
	return func(ctx context.Context) error {
		draining.Begin()
		if hold <= 0 {
			return nil
		}

		timer := time.NewTimer(hold)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
