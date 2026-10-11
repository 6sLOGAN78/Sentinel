package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
)

type envelope struct {
	Error map[string]string `json:"error"`
}

func newEcho(t *testing.T) *echo.Echo {
	t.Helper()

	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, []string{"https://example.test"})

	e.GET("/panic", func(echo.Context) error { panic("boom secret-internal") })
	e.GET("/slow", func(echo.Context) error { return errs.NewTooManyRequestsError("slow", 7) })
	e.GET("/down", func(echo.Context) error { return errs.NewServiceUnavailableError("down", 3) })

	return e
}

func do(t *testing.T, e *echo.Echo, path string, hdr map[string]string) (*httptest.ResponseRecorder, envelope) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var env envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)

	return rec, env
}

func TestRecoverPanicYieldsEnvelope(t *testing.T) {
	t.Parallel()

	rec, env := do(t, newEcho(t), "/panic", nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "INTERNAL_SERVER_ERROR", env.Error["code"])
	assert.NotEmpty(t, env.Error["request_id"])
	assert.Equal(t, env.Error["request_id"], rec.Header().Get("X-Request-ID"))
	assert.NotContains(t, rec.Body.String(), "secret-internal")
	assert.Len(t, env.Error, 3)
}

func TestRecoverRegisteredFirst(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	chain := middleware.Chain(&logger, nil)
	require.NotEmpty(t, chain)
	assert.Equal(t, "recover", chain[0].Name)
}

func TestRetryAfterEnvelope(t *testing.T) {
	t.Parallel()

	e := newEcho(t)

	rec, env := do(t, e, "/slow", nil)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "7", rec.Header().Get("Retry-After"))
	assert.Equal(t, "slow", env.Error["message"])

	rec, env = do(t, e, "/down", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "3", rec.Header().Get("Retry-After"))
	assert.Equal(t, "SERVICE_UNAVAILABLE", env.Error["code"])
}

func TestEchoHTTPErrorRenderedAsEnvelope(t *testing.T) {
	t.Parallel()

	rec, env := do(t, newEcho(t), "/nope", nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "NOT_FOUND", env.Error["code"])
	assert.NotEmpty(t, env.Error["request_id"])
	assert.Len(t, env.Error, 3)
}

func TestRequestIDSanitized(t *testing.T) {
	t.Parallel()

	e := newEcho(t)

	rec, _ := do(t, e, "/slow", map[string]string{"X-Request-ID": "good.id_1-2"})
	assert.Equal(t, "good.id_1-2", rec.Header().Get("X-Request-ID"))

	for _, bad := range []string{strings.Repeat("a", 65), "bad id", "a\x01b", "x;y"} {
		rec, _ = do(t, e, "/slow", map[string]string{"X-Request-ID": bad})
		got := rec.Header().Get("X-Request-ID")
		assert.NotEqual(t, bad, got)
		assert.Len(t, got, 36)
	}
}

// syncBuffer is a goroutine-safe log sink for request-logger assertions.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// apiLogLine returns the request-logger ("API") line written to sink.
func apiLogLine(t *testing.T, sink *syncBuffer) map[string]any {
	t.Helper()

	for line := range strings.SplitSeq(strings.TrimSpace(sink.String()), "\n") {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))

		if entry["message"] == "API" {
			return entry
		}
	}

	require.FailNow(t, "no API log line", sink.String())

	return nil
}

func newLoggedEcho(t *testing.T, sink *syncBuffer) *echo.Echo {
	t.Helper()

	e := echo.New()
	logger := zerolog.New(sink)
	middleware.Apply(e, &logger, []string{"https://example.test"})

	return e
}

func TestRequestLoggerStatusMatchesResponseForPlainError(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	e := newLoggedEcho(t, sink)
	e.GET("/plain", func(echo.Context) error { return errors.New("boom") })

	rec, env := do(t, e, "/plain", nil)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "INTERNAL_SERVER_ERROR", env.Error["code"])

	entry := apiLogLine(t, sink)
	assert.InDelta(t, float64(http.StatusInternalServerError), entry["status"], 0)
	assert.Equal(t, "error", entry["level"])
}

func TestRequestLoggerStatusForHTTPError(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	e := newLoggedEcho(t, sink)
	e.GET("/slow", func(echo.Context) error { return errs.NewTooManyRequestsError("slow", 1) })

	rec, _ := do(t, e, "/slow", nil)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)

	entry := apiLogLine(t, sink)
	assert.InDelta(t, float64(http.StatusTooManyRequests), entry["status"], 0)
	assert.Equal(t, "warn", entry["level"])
}

func TestRealIPIgnoresClientForwardingHeaders(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	e := newLoggedEcho(t, sink)
	e.GET("/ok", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "192.0.2.10:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("X-Real-IP", "198.51.100.8")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	entry := apiLogLine(t, sink)
	assert.Equal(t, "192.0.2.10", entry["ip"])
	assert.NotContains(t, sink.String(), "198.51.100.")
}

func TestApplyKeepsConfiguredIPExtractor(t *testing.T) {
	t.Parallel()

	_, proxies, err := net.ParseCIDR("192.0.2.0/24")
	require.NoError(t, err)

	e := echo.New()
	e.IPExtractor = echo.ExtractIPFromXFFHeader(echo.TrustIPRange(proxies))
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)

	var got string
	e.GET("/ip", func(c echo.Context) error {
		got = c.RealIP()

		return c.NoContent(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "192.0.2.10:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	e.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "198.51.100.7", got)
}
