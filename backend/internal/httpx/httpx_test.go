package httpx_test

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/httpx"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestShutdownBeforeStart(t *testing.T) {
	t.Parallel()

	s := httpx.New(config.ServerConfig{Port: "0"}, okHandler())
	require.NotPanics(t, func() {
		require.NoError(t, s.Shutdown(context.Background()))
	})

	var nilServer *httpx.Server
	require.NoError(t, nilServer.Shutdown(context.Background()))
	var zeroServer httpx.Server
	require.NoError(t, zeroServer.Shutdown(context.Background()))
}

func TestHTTPXBindAddress(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1", ""} {
		t.Run(host, func(t *testing.T) {
			t.Setenv("SENTINEL_PRIMARY.ENV", "test")
			t.Setenv("SENTINEL_KAFKA.BROKERS", "127.0.0.1:9092")
			t.Setenv("SENTINEL_SERVER.BIND_ADDRESS", host)
			t.Setenv("SENTINEL_SERVER.PORT", "0")
			cfg, loadErr := config.Load(config.ServiceDetector)
			require.NoError(t, loadErr)
			s := httpx.New(cfg.Server, okHandler())
			require.Equal(t, net.JoinHostPort(host, "0"), s.HTTPServer().Addr)

			listening := make(chan net.Addr, 1)
			s.HTTPServer().BaseContext = func(l net.Listener) context.Context {
				listening <- l.Addr()
				return context.Background()
			}
			done := make(chan error, 1)
			go func() { done <- s.Start() }()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				require.NoError(t, s.Shutdown(ctx))
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Error("server did not stop")
				}
			})
			select {
			case addr := <-listening:
				actualHost, port, err := net.SplitHostPort(addr.String())
				require.NoError(t, err)
				requestHost := host
				if host == "" {
					assert.True(t, net.ParseIP(actualHost).IsUnspecified())
					requestHost = "127.0.0.1"
				} else {
					assert.Equal(t, host, actualHost)
				}
				client := &http.Client{Timeout: 5 * time.Second}
				resp, err := client.Get("http://" + net.JoinHostPort(requestHost, port))
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				assert.Equal(t, http.StatusNoContent, resp.StatusCode)
			case err := <-done:
				t.Fatalf("listener failed: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("listener did not start")
			}
		})
	}
}

func TestHTTPXStartReportsBindError(t *testing.T) {
	t.Parallel()
	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)
	s := httpx.New(config.ServerConfig{Port: port}, okHandler())
	require.ErrorContains(t, s.Start(), "listen:")
}

func TestShutdownStopsServe(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := httpx.New(config.ServerConfig{}, okHandler())
	done := make(chan error, 1)

	go func() { done <- s.Serve(l) }()

	resp, err := http.Get("http://" + l.Addr().String())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, s.Shutdown(ctx))
	require.NoError(t, <-done)

	// Check the original listener, not a released ephemeral port that another
	// parallel test or connection may have reused after Shutdown returned.
	require.ErrorIs(t, l.Close(), net.ErrClosed, "Shutdown must already have closed the listener")
}

func TestHTTPXTimeouts(t *testing.T) {
	t.Parallel()

	s := httpx.New(config.ServerConfig{Port: "9", ReadTimeout: 11, WriteTimeout: 12, IdleTimeout: 13}, okHandler())
	hs := s.HTTPServer()

	assert.Equal(t, ":9", hs.Addr)
	assert.Positive(t, hs.ReadHeaderTimeout)
	assert.Equal(t, 11*time.Second, hs.ReadTimeout)
	assert.Equal(t, 12*time.Second, hs.WriteTimeout)
	assert.Equal(t, 13*time.Second, hs.IdleTimeout)
}

func TestHTTPXImportsNothingHeavy(t *testing.T) {
	t.Parallel()

	out, err := exec.Command("go", "list", "-deps", "github.com/6sLOGAN78/sentinel/internal/httpx").Output()
	require.NoError(t, err)

	for _, banned := range []string{"internal/database", "internal/middleware", "go-redis", "kafka", "clickhouse"} {
		assert.NotContains(t, string(out), banned, "httpx must not depend on %s", banned)
	}
}
