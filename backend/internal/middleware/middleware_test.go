package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
