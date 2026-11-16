package opshttp

import "net/http"

// Option extends the internal ops handler without a global metric registry.
type Option func(*handler)

// WithMetricsHandler mounts h at GET /metrics on the internal ops listener.
func WithMetricsHandler(metrics http.Handler) Option {
	return func(h *handler) { h.metricsHandler = metrics }
}
