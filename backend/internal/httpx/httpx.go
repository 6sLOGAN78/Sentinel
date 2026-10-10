// Package httpx wraps net/http.Server with Sentinel timeout defaults and a
// shutdown that is safe to call at any time. It owns no databases or caches.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const (
	readHeaderTimeout = 5 * time.Second
	defaultPort       = "8080"
	defaultTimeoutSec = 30
)

// Server wraps *http.Server.
type Server struct {
	srv *http.Server
}

// New builds a Server from the HTTP settings in cfg. Zero timeouts fall back
// to defaults so a Server never runs without deadlines.
func New(cfg config.ServerConfig, handler http.Handler) *Server {
	port := cfg.Port
	if port == "" {
		port = defaultPort
	}

	return &Server{srv: &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       secondsOrDefault(cfg.ReadTimeout),
		WriteTimeout:      secondsOrDefault(cfg.WriteTimeout),
		IdleTimeout:       secondsOrDefault(cfg.IdleTimeout),
	}}
}

func secondsOrDefault(sec int) time.Duration {
	if sec <= 0 {
		sec = defaultTimeoutSec
	}

	return time.Duration(sec) * time.Second
}

// HTTPServer exposes the underlying server for inspection.
func (s *Server) HTTPServer() *http.Server {
	return s.srv
}

// Serve serves on l until Shutdown; a graceful close returns nil.
func (s *Server) Serve(l net.Listener) error {
	if s == nil || s.srv == nil {
		return errors.New("httpx: nil server")
	}

	if err := s.srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// Start listens on the configured address and serves.
func (s *Server) Start() error {
	if s == nil || s.srv == nil {
		return errors.New("httpx: nil server")
	}

	l, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	return s.Serve(l)
}

// Shutdown gracefully stops the server. It returns nil on a nil or
// never-started server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.srv == nil {
		return nil
	}

	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
