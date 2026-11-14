package opshttp

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// DetectionMetrics holds one process-local operational counter. Source replay
// may count a late input again; this is not a lifetime-unique event count.
type DetectionMetrics struct{ late atomic.Uint64 }

// IncLate records one valid input classified strictly below the watermark.
func (m *DetectionMetrics) IncLate() { m.late.Add(1) }

// Option extends the internal ops handler without a global metric registry.
type Option func(*handler)

// WithDetectionMetrics binds the detector's fixed numeric counter.
// It is superseded by WithMetricsHandler and removed when the detector migrates.
func WithDetectionMetrics(metrics *DetectionMetrics) Option {
	return func(h *handler) {
		h.metricsHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			_, _ = fmt.Fprintf(w,
				"# TYPE detection_late_events_total counter\ndetection_late_events_total %d\n",
				metrics.late.Load())
		})
	}
}

// WithMetricsHandler mounts h at GET /metrics on the internal ops listener.
func WithMetricsHandler(metrics http.Handler) Option {
	return func(h *handler) { h.metricsHandler = metrics }
}
