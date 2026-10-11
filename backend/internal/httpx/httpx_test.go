package httpx_test

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"strings"
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
}

func TestShutdownStopsServe(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := httpx.New(config.ServerConfig{}, okHandler())
	done := make(chan error, 1)

	go func() { done <- s.Serve(l) }()

	resp, err := http.Get("http://" + l.Addr().String()) //nolint:noctx // test against a local listener
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, s.Shutdown(ctx))
	require.NoError(t, <-done)

	conn, err := net.DialTimeout("tcp", l.Addr().String(), time.Second)
	if err == nil {
		_ = conn.Close()
	}

	require.Error(t, err)
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
		assert.False(t, strings.Contains(string(out), banned), "httpx must not depend on %s", banned)
	}
}
