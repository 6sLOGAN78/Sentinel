package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Result label values for sentinel_ingest_events_total.
const (
	ResultAccepted    = "accepted"
	ResultRejected    = "rejected"
	ResultRateLimited = "rate_limited"
	ResultShed        = "shed"
	ResultUnavailable = "unavailable"
)

const (
	durationBucketStart  = 0.005
	durationBucketFactor = 2
	durationBucketCount  = 12
	statusOther          = "other"
)

// IngestMetrics holds the spec 11.2 ingest metrics. A nil *IngestMetrics is a
// valid no-op so callers need no guards.
type IngestMetrics struct {
	events   *prometheus.CounterVec
	requests *prometheus.CounterVec
	duration prometheus.Histogram
}

// NewIngestMetrics registers the ingest metrics on reg.
func NewIngestMetrics(reg prometheus.Registerer) *IngestMetrics {
	m := &IngestMetrics{
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sentinel_ingest_events_total",
			Help: "Events handled by ingest, by outcome.",
		}, []string{labelResult}),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sentinel_ingest_requests_total",
			Help: "Ingest batch requests by HTTP status.",
		}, []string{"status"}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "sentinel_ingest_request_duration_seconds",
			Help:    "Ingest batch request duration.",
			Buckets: prometheus.ExponentialBuckets(durationBucketStart, durationBucketFactor, durationBucketCount),
		}),
	}
	reg.MustRegister(m.events, m.requests, m.duration)

	return m
}

// Events adds n events to the series for result. n may be zero, which creates
// the series without inventing a count.
func (m *IngestMetrics) Events(result string, n int) {
	if m == nil {
		return
	}

	counter := m.events.WithLabelValues(result)
	if n > 0 {
		counter.Add(float64(n))
	}
}

// Request records one finished request. Statuses outside the fixed set are
// recorded as "other" so label cardinality stays bounded.
func (m *IngestMetrics) Request(status int, elapsed time.Duration) {
	if m == nil {
		return
	}

	m.requests.WithLabelValues(statusLabel(status)).Inc()
	m.duration.Observe(elapsed.Seconds())
}

func statusLabel(status int) string {
	switch status {
	case http.StatusAccepted, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusServiceUnavailable:
		return strconv.Itoa(status)
	default:
		return statusOther
	}
}

// RegisterProducerBuffer exposes the producer queue depth next to its
// configured limit.
func RegisterProducerBuffer(reg prometheus.Registerer, buffered func() int64, limit int) {
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "sentinel_kafka_producer_buffered_records",
		Help: "Records currently buffered in the Kafka producer.",
	}, func() float64 { return float64(buffered()) }))

	limitGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "sentinel_kafka_producer_buffered_records_limit",
		Help: "Configured maximum of buffered producer records.",
	})
	limitGauge.Set(float64(limit))
	reg.MustRegister(limitGauge)
}
