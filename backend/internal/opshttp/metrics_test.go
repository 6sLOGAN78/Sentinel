package opshttp_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/opshttp"
	"github.com/stretchr/testify/require"
)

func TestLateMetric(t *testing.T) {
	metrics := &opshttp.DetectionMetrics{}
	h := opshttp.NewHandler(time.Second, &opshttp.Draining{}, nil, opshttp.WithDetectionMetrics(metrics))
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, r.Code)
	require.Equal(t, "text/plain; version=0.0.4; charset=utf-8", r.Header().Get("Content-Type"))
	require.Equal(t, "# TYPE detection_late_events_total counter\ndetection_late_events_total 0\n", r.Body.String())
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 100 {
				metrics.IncLate()
			}
		})
	}
	wg.Wait()
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, "# TYPE detection_late_events_total counter\ndetection_late_events_total 1600\n", r.Body.String())
	other := opshttp.NewHandler(time.Second, &opshttp.Draining{}, nil,
		opshttp.WithDetectionMetrics(&opshttp.DetectionMetrics{}))
	r = httptest.NewRecorder()
	other.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Contains(t, r.Body.String(), "detection_late_events_total 0\n",
		"counter must be per process wiring, not global")
	r = httptest.NewRecorder()
	opshttp.NewHandler(time.Second, &opshttp.Draining{}, nil).ServeHTTP(r,
		httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusNotFound, r.Code, "old callers have no unrelated detection metric")
}
