package telemetry_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func gauge(t *testing.T, reg prometheus.Gatherer, name string) float64 {
	t.Helper()
	family := findFamily(t, reg, name)
	require.NotNil(t, family, name)
	require.Len(t, family.GetMetric(), 1, name)

	return family.GetMetric()[0].GetGauge().GetValue()
}

func TestDetectorMetricsPublishedCountsAlertsAndDelay(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewDetectorMetrics(reg)
	metrics.Published("AUTH_BRUTE_FORCE", "high", 2500*time.Millisecond)
	metrics.Published("AUTH_BRUTE_FORCE", "high", 500*time.Millisecond)
	metrics.Published("PORT_SCAN", "medium", time.Second)

	auth := labeledMetric(t, reg, "sentinel_detection_alerts_total",
		map[string]string{"rule_id": "AUTH_BRUTE_FORCE", "severity": "high"})
	require.NotNil(t, auth)
	require.InDelta(t, 2, auth.GetCounter().GetValue(), 0)
	scan := labeledMetric(t, reg, "sentinel_detection_alerts_total",
		map[string]string{"rule_id": "PORT_SCAN", "severity": "medium"})
	require.NotNil(t, scan)
	require.InDelta(t, 1, scan.GetCounter().GetValue(), 0)

	delay := findFamily(t, reg, "sentinel_detection_delay_seconds")
	require.NotNil(t, delay)
	require.Len(t, delay.GetMetric(), 1)
	require.Empty(t, delay.GetMetric()[0].GetLabel(), "delay histogram carries no labels")
	histogram := delay.GetMetric()[0].GetHistogram()
	require.EqualValues(t, 3, histogram.GetSampleCount())
	require.InDelta(t, 4.0, histogram.GetSampleSum(), 1e-9)
	require.Contains(t, findFamily(t, reg, "sentinel_detection_alerts_total").GetHelp(), "replays")
}

func TestDetectorMetricsLateCounter(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewDetectorMetrics(reg)
	metrics.IncLate()
	metrics.IncLate()
	metrics.IncLate()

	family := findFamily(t, reg, "sentinel_detection_late_events_total")
	require.NotNil(t, family)
	require.Len(t, family.GetMetric(), 1)
	require.Empty(t, family.GetMetric()[0].GetLabel())
	require.InDelta(t, 3, family.GetMetric()[0].GetCounter().GetValue(), 0)
}

func TestDetectorMetricsStateAndLimitGauges(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewDetectorMetrics(reg)
	metrics.SetLimits(100000, 200000, 268435456)
	metrics.SetState(12, 34, 5678)

	require.InDelta(t, 12, gauge(t, reg, "sentinel_detector_state_windows"), 0)
	require.InDelta(t, 34, gauge(t, reg, "sentinel_detector_state_ledger_entries"), 0)
	require.InDelta(t, 5678, gauge(t, reg, "sentinel_detector_state_bytes"), 0)
	require.InDelta(t, 100000, gauge(t, reg, "sentinel_detector_state_windows_limit"), 0)
	require.InDelta(t, 200000, gauge(t, reg, "sentinel_detector_state_ledger_entries_limit"), 0)
	require.InDelta(t, 268435456, gauge(t, reg, "sentinel_detector_state_bytes_limit"), 0)

	metrics.SetState(0, 0, 0)
	require.InDelta(t, 0, gauge(t, reg, "sentinel_detector_state_windows"), 0, "state gauges are absolute, not cumulative")
}

func TestDetectorMetricsPinMarginSetAndDelete(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewDetectorMetrics(reg)
	metrics.SetPinMargin("security.events.v1", 0, 1500)
	metrics.SetPinMargin("security.events.v1", 1, 0)

	p0 := labeledMetric(t, reg, "sentinel_detector_pin_retention_margin_offsets",
		map[string]string{"topic": "security.events.v1", "partition": "0"})
	require.NotNil(t, p0)
	require.InDelta(t, 1500, p0.GetGauge().GetValue(), 0)

	metrics.DeletePinMargin("security.events.v1", 0)
	require.Nil(t, labeledMetric(t, reg, "sentinel_detector_pin_retention_margin_offsets",
		map[string]string{"partition": "0"}), "revoked partition series must be absent")
	require.NotNil(t, labeledMetric(t, reg, "sentinel_detector_pin_retention_margin_offsets",
		map[string]string{"partition": "1"}))
}

func TestDetectorMetricsLabelsInsideAllowlistAndNilSafe(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewDetectorMetrics(reg)
	metrics.Published("DENY_SPIKE", "medium", time.Second)
	metrics.IncLate()
	metrics.SetState(1, 2, 3)
	metrics.SetLimits(4, 5, 6)
	metrics.SetPinMargin("t", 2, 7)

	outside, err := telemetry.LabelNamesOutsideAllowlist(reg)
	require.NoError(t, err)
	require.Empty(t, outside)

	var none *telemetry.DetectorMetrics
	require.NotPanics(t, func() {
		none.Published("AUTH_BRUTE_FORCE", "high", time.Second)
		none.IncLate()
		none.SetState(1, 2, 3)
		none.SetLimits(1, 2, 3)
		none.SetPinMargin("t", 0, 1)
		none.DeletePinMargin("t", 0)
	})
}
