package opshttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/opshttp"
	"github.com/stretchr/testify/require"
)

func TestLateMetric(t *testing.T) {
	h := opshttp.NewHandler(time.Second, &opshttp.Draining{}, nil)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, r.Code)
	require.Equal(t, "text/plain; version=0.0.4; charset=utf-8", r.Header().Get("Content-Type"))
	require.Equal(t, "# TYPE detection_late_events_total counter\ndetection_late_events_total 0\n", r.Body.String())
}
