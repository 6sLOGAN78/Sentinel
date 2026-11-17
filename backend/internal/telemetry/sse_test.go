package telemetry_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func TestSSEMetricsClientsGaugeIncDec(t *testing.T) {
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewSSEMetrics(reg)

	family := findFamily(t, reg, "sentinel_sse_clients")
	require.NotNil(t, family, "gauge must be exported at 0 before the first client")
	require.Len(t, family.GetMetric(), 1)
	require.InDelta(t, 0, family.GetMetric()[0].GetGauge().GetValue(), 0)
	require.Empty(t, family.GetMetric()[0].GetLabel(), "process total only: no tenant or client labels")

	metrics.Inc()
	metrics.Inc()
	metrics.Inc()
	metrics.Dec()
	family = findFamily(t, reg, "sentinel_sse_clients")
	require.InDelta(t, 2, family.GetMetric()[0].GetGauge().GetValue(), 0)

	metrics.Dec()
	metrics.Dec()
	family = findFamily(t, reg, "sentinel_sse_clients")
	require.InDelta(t, 0, family.GetMetric()[0].GetGauge().GetValue(), 0)

	outside, err := telemetry.LabelNamesOutsideAllowlist(reg)
	require.NoError(t, err)
	require.Empty(t, outside)
}

func TestSSEMetricsNilIsNoop(t *testing.T) {
	var metrics *telemetry.SSEMetrics
	require.NotPanics(t, func() {
		metrics.Inc()
		metrics.Dec()
	})
}
