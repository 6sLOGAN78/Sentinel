package alertwriter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func alertWriteCount(t *testing.T, reg prometheus.Gatherer, result string) uint64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != "sentinel_clickhouse_write_duration_seconds" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, pair := range metric.GetLabel() {
				if pair.GetName() == "result" && pair.GetValue() == result {
					return metric.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	return 0
}

func TestAlertWriterObservesClickHouseInsertByResult(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		insertErr error
		ok, error uint64
	}{
		{"ok", nil, 1, 0},
		{"error", errors.New("clickhouse unavailable"), 0, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			order := []string{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reg := telemetry.NewRegistry()
			consumer := &orderConsumer{order: &order, record: orderingRecord(t), cancel: cancel}
			settings := config.WriterConfig{BatchSize: 1, FlushInterval: time.Millisecond, MaxAttempts: 1,
				RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
				OperationTimeout: 25 * time.Millisecond, ShutdownTimeout: time.Second}
			writer := alertwriter.New(consumer, &orderDetections{order: &order, insertErr: scenario.insertErr},
				&orderWorkflows{tx: &orderTx{t: t, order: &order}}, settings, zerolog.Nop()).
				WithMetrics(telemetry.NewConsumerMetrics(reg))

			_ = writer.Run(ctx)
			require.Equal(t, 1, countOrder(order, "ch-ack"), "exactly one CH insert attempt")
			require.Equal(t, 1, countOrder(order, "ch-preflight"), "reads are not observed as writes")
			require.Equal(t, scenario.ok, alertWriteCount(t, reg, "ok"))
			require.Equal(t, scenario.error, alertWriteCount(t, reg, "error"))
		})
	}
}
