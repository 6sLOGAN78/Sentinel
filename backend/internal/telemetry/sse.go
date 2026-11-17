package telemetry

import "github.com/prometheus/client_golang/prometheus"

// SSEMetrics holds the spec 11.2 query-api stream metric. The gauge has no
// labels: it is the per-replica total, so no tenant or client count leaks
// (T-05-14-01). A nil *SSEMetrics is a valid no-op so callers need no guards.
type SSEMetrics struct {
	clients prometheus.Gauge
}

// NewSSEMetrics registers sentinel_sse_clients on reg.
func NewSSEMetrics(reg prometheus.Registerer) *SSEMetrics {
	m := &SSEMetrics{clients: prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "sentinel_sse_clients",
		Help: "SSE alert streams currently admitted on this query-api replica.",
	})}
	reg.MustRegister(m.clients)

	return m
}

// Inc counts one admitted stream.
func (m *SSEMetrics) Inc() {
	if m == nil {
		return
	}
	m.clients.Inc()
}

// Dec releases one admitted stream.
func (m *SSEMetrics) Dec() {
	if m == nil {
		return
	}
	m.clients.Dec()
}
