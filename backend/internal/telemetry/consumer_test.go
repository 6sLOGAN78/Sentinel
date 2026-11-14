package telemetry_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func labeledMetric(t *testing.T, reg prometheus.Gatherer, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	family := findFamily(t, reg, name)
	if family == nil {
		return nil
	}
	for _, metric := range family.GetMetric() {
		matched := 0
		for _, pair := range metric.GetLabel() {
			if want, ok := labels[pair.GetName()]; ok && want == pair.GetValue() {
				matched++
			}
		}
		if matched == len(labels) {
			return metric
		}
	}

	return nil
}

func TestConsumerMetricsLagSetAndDelete(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewConsumerMetrics(reg)
	metrics.SetLag("event-writer", "security.events.v1", 0, 60)
	metrics.SetLag("event-writer", "security.events.v1", 1, 0)

	p0 := labeledMetric(t, reg, "sentinel_kafka_consumer_lag",
		map[string]string{"group": "event-writer", "topic": "security.events.v1", "partition": "0"})
	require.NotNil(t, p0)
	require.InDelta(t, 60, p0.GetGauge().GetValue(), 0)

	metrics.DeleteLag("event-writer", "security.events.v1", 0)
	require.Nil(t, labeledMetric(t, reg, "sentinel_kafka_consumer_lag",
		map[string]string{"partition": "0"}), "deleted series must be absent from Gather")
	require.NotNil(t, labeledMetric(t, reg, "sentinel_kafka_consumer_lag",
		map[string]string{"partition": "1"}))
}

func TestConsumerMetricsWriteDurationByResult(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewConsumerMetrics(reg)
	metrics.ObserveWrite(true, 20*time.Millisecond)
	metrics.ObserveWrite(true, 30*time.Millisecond)
	metrics.ObserveWrite(false, 2*time.Second)

	ok := labeledMetric(t, reg, "sentinel_clickhouse_write_duration_seconds", map[string]string{"result": "ok"})
	require.NotNil(t, ok)
	require.EqualValues(t, 2, ok.GetHistogram().GetSampleCount())
	require.InDelta(t, 0.05, ok.GetHistogram().GetSampleSum(), 1e-9)
	failed := labeledMetric(t, reg, "sentinel_clickhouse_write_duration_seconds", map[string]string{"result": "error"})
	require.NotNil(t, failed)
	require.EqualValues(t, 1, failed.GetHistogram().GetSampleCount())
}

func TestConsumerMetricsDLQByReason(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewConsumerMetrics(reg)
	metrics.IncDLQ("decode_error")
	metrics.IncDLQ("decode_error")
	metrics.IncDLQ("validation_error")

	require.InDelta(t, 2, counter(t, reg, "sentinel_dlq_events_total", "reason", "decode_error"), 0)
	require.InDelta(t, 1, counter(t, reg, "sentinel_dlq_events_total", "reason", "validation_error"), 0)
}

func TestConsumerMetricsLabelsInsideAllowlistAndNilSafe(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewConsumerMetrics(reg)
	metrics.SetLag("g", "t", 3, 1)
	metrics.ObserveWrite(true, time.Millisecond)
	metrics.ObserveWrite(false, time.Millisecond)
	metrics.IncDLQ("missing_received_at")

	outside, err := telemetry.LabelNamesOutsideAllowlist(reg)
	require.NoError(t, err)
	require.Empty(t, outside)

	var none *telemetry.ConsumerMetrics
	require.NotPanics(t, func() {
		none.SetLag("g", "t", 0, 1)
		none.DeleteLag("g", "t", 0)
		none.ObserveWrite(true, time.Second)
		none.IncDLQ("decode_error")
	})
}
