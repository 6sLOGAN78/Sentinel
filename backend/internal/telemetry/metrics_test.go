package telemetry_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func findFamily(t *testing.T, reg prometheus.Gatherer, name string) *dto.MetricFamily {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}

	return nil
}

func counter(t *testing.T, reg prometheus.Gatherer, name, label, value string) float64 {
	t.Helper()
	family := findFamily(t, reg, name)
	if family == nil {
		return 0
	}
	for _, metric := range family.GetMetric() {
		for _, pair := range metric.GetLabel() {
			if pair.GetName() == label && pair.GetValue() == value {
				return metric.GetCounter().GetValue()
			}
		}
	}

	return 0
}

func TestRegistryExposesGoAndProcessCollectors(t *testing.T) {
	reg := telemetry.NewRegistry()
	require.NotNil(t, findFamily(t, reg, "go_goroutines"))
	require.NotNil(t, findFamily(t, reg, "process_resident_memory_bytes"))

	rec := httptest.NewRecorder()
	telemetry.Handler(reg).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "go_goroutines")
}

func TestRegistriesAreIndependent(t *testing.T) {
	regA, regB := telemetry.NewRegistry(), telemetry.NewRegistry()
	a, b := telemetry.NewIngestMetrics(regA), telemetry.NewIngestMetrics(regB)
	a.Events(telemetry.ResultAccepted, 3)
	b.Events(telemetry.ResultShed, 0)

	require.InDelta(t, 3, counter(t, regA, "sentinel_ingest_events_total", "result", "accepted"), 0)
	require.InDelta(t, 0, counter(t, regB, "sentinel_ingest_events_total", "result", "accepted"), 0)
}

func TestLabelNamesStayInsideAllowlist(t *testing.T) {
	require.ElementsMatch(t,
		[]string{"result", "status", "rule_id", "severity", "reason", "group", "topic", "partition", "service"},
		telemetry.AllowedLabelNames())

	reg := telemetry.NewRegistry()
	metrics := telemetry.NewIngestMetrics(reg)
	for _, result := range []string{
		telemetry.ResultAccepted, telemetry.ResultRejected, telemetry.ResultRateLimited,
		telemetry.ResultShed, telemetry.ResultUnavailable,
	} {
		metrics.Events(result, 1)
	}
	metrics.Request(http.StatusAccepted, time.Millisecond)
	var buffered atomic.Int64
	telemetry.RegisterProducerBuffer(reg, buffered.Load, 100)

	outside, err := telemetry.LabelNamesOutsideAllowlist(reg)
	require.NoError(t, err)
	require.Empty(t, outside)

	bad := prometheus.NewRegistry()
	unobserved := prometheus.CounterOpts{Name: "bad_total", Help: "unobserved"}
	bad.MustRegister(prometheus.NewCounterVec(unobserved, []string{"tenant_id"}))
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "bad2_total", Help: "observed"}, []string{"request_id"})
	vec.WithLabelValues("x").Inc()
	bad.MustRegister(vec)
	outside, err = telemetry.LabelNamesOutsideAllowlist(bad)
	require.NoError(t, err)
	require.Equal(t, []string{"request_id"}, outside, "the guard must flag a forbidden label")
}

func TestRequestStatusIsBoundedAndNilSafe(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewIngestMetrics(reg)
	metrics.Request(http.StatusAccepted, 10*time.Millisecond)
	metrics.Request(http.StatusTeapot, time.Millisecond)
	metrics.Request(599, time.Millisecond)

	require.InDelta(t, 1, counter(t, reg, "sentinel_ingest_requests_total", "status", "202"), 0)
	require.InDelta(t, 2, counter(t, reg, "sentinel_ingest_requests_total", "status", "other"), 0)
	require.InDelta(t, 0, counter(t, reg, "sentinel_ingest_requests_total", "status", "418"), 0)
	hist := findFamily(t, reg, "sentinel_ingest_request_duration_seconds")
	require.NotNil(t, hist)
	require.EqualValues(t, 3, hist.GetMetric()[0].GetHistogram().GetSampleCount())

	var none *telemetry.IngestMetrics
	require.NotPanics(t, func() {
		none.Events(telemetry.ResultAccepted, 1)
		none.Request(http.StatusOK, time.Second)
	})
}

func TestProducerBufferGauges(t *testing.T) {
	reg := telemetry.NewRegistry()
	var buffered atomic.Int64
	telemetry.RegisterProducerBuffer(reg, buffered.Load, 4096)
	buffered.Store(17)

	depth := findFamily(t, reg, "sentinel_kafka_producer_buffered_records")
	require.NotNil(t, depth)
	require.InDelta(t, 17, depth.GetMetric()[0].GetGauge().GetValue(), 0)
	limit := findFamily(t, reg, "sentinel_kafka_producer_buffered_records_limit")
	require.NotNil(t, limit)
	require.InDelta(t, 4096, limit.GetMetric()[0].GetGauge().GetValue(), 0)
}
