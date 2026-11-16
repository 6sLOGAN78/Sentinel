package opshttp_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/opshttp"
	"github.com/stretchr/testify/require"
)

func TestMetricsNotMountedWithoutHandler(t *testing.T) {
	r := httptest.NewRecorder()
	opshttp.NewHandler(time.Second, &opshttp.Draining{}, nil).ServeHTTP(r,
		httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusNotFound, r.Code, "services without a metrics handler serve no /metrics")
}

func TestWithMetricsHandlerServesMetrics(t *testing.T) {
	body := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "custom_metric 1\n")
	})
	h := opshttp.NewHandler(time.Second, &opshttp.Draining{}, nil, opshttp.WithMetricsHandler(body))
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, r.Code)
	require.Equal(t, "custom_metric 1\n", r.Body.String())

	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	require.Equal(t, http.StatusMethodNotAllowed, r.Code, "only GET is mounted")

	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, r.Code, "probes stay mounted next to /metrics")
}
