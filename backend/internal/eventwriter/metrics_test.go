package eventwriter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/eventwriter"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func writeCount(t *testing.T, reg prometheus.Gatherer, result string) uint64 {
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

func TestWriterObservesEveryInsertAttemptIncludingRetries(t *testing.T) {
	now := time.Now().UTC()
	consumer := &scriptedConsumer{records: []*kgo.Record{validRecord(t, 0, now, now)}}
	store := &flakyStore{failures: 2, fail: errors.New("clickhouse recovering")}
	reg := telemetry.NewRegistry()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	consumer.onCommit = cancel
	w := eventwriter.New(consumer, store, &recordingDLQ{}, jitterSettings(), contracts.DefaultLimits(), zerolog.Nop()).
		WithRetryWaiter((&recordingWaiter{}).wait).
		WithMetrics(telemetry.NewConsumerMetrics(reg))

	require.NoError(t, w.Run(ctx))
	require.Equal(t, 3, store.attempts)
	require.EqualValues(t, 2, writeCount(t, reg, "error"), "each failed attempt is observed")
	require.EqualValues(t, 1, writeCount(t, reg, "ok"))
}

func TestWriterWithoutMetricsStillWrites(t *testing.T) {
	now := time.Now().UTC()
	consumer := &scriptedConsumer{records: []*kgo.Record{validRecord(t, 0, now, now)}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	consumer.onCommit = cancel
	w := eventwriter.New(consumer, &flakyStore{}, &recordingDLQ{}, jitterSettings(), contracts.DefaultLimits(),
		zerolog.Nop()).WithMetrics(nil)

	require.NoError(t, w.Run(ctx))
	require.Len(t, consumer.commits, 1)
}
