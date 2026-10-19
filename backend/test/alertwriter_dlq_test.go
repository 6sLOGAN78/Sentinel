//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

func TestAlertWriterPoison(t *testing.T) {
	t.Run("default binary schema identity and bounded origin", testAlertWriterRealPoison)
	t.Run("real DLQ outage keeps source replayable", testAlertWriterDLQOutage)
	t.Run("hundreds of delayed real quarantines share one budget", testAlertWriterSlowDLQ)
}

func testAlertWriterRealPoison(t *testing.T) {
	f := newAlertReplayFixture(t)
	observed := startObservedDLQ(t.Context(), t, f.kafka, f.kafka.DLQTopic)
	p := f.binary(t)
	var records []*kgo.Record
	for _, kind := range []string{"decode", "schema", "key", "hash", "entity"} {
		a := *f.alert
		r := &kgo.Record{Topic: f.kafka.AlertsTopic, Key: []byte(a.TenantID + "|" + a.AlertID),
			Headers: []kgo.RecordHeader{{Key: "dlq.origin.topic", Value: []byte("forged")}}}
		switch kind {
		case "schema":
			a.SchemaVersion = 9
		case "key":
			r.Key = []byte("other|" + a.AlertID)
		case "hash":
			a.AlertID = strings.Repeat("a", 64)
			r.Key = []byte(a.TenantID + "|" + a.AlertID)
		case "entity":
			a.EntityKey = "broken"
		}
		var err error
		r.Value, err = json.Marshal(a)
		require.NoError(t, err)
		if kind == "decode" {
			r.Value = []byte("{")
		}
		records = append(records, r)
	}
	produceDLQSources(t, f.kafka, records)
	f.waitOffset(t, int64(len(records)))
	require.Eventually(t, func() bool {
		got, err := observed.snapshot()
		return err == nil && len(got) == len(records)
	}, 3*time.Second, 25*time.Millisecond)
	got, err := observed.snapshot()
	require.NoError(t, err)
	for i, r := range records {
		reason := "validation_error"
		if i == 0 {
			reason = "decode_error"
		}
		requireDLQOrigin(t, got[poisonOrigin{r.Partition, r.Offset}], r, reason)
	}
	var chRows, pgRows uint64
	require.NoError(t, f.ch.QueryRow(t.Context(), `SELECT count() FROM alerts`).Scan(&chRows))
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM alert_workflows WHERE alert_id=$1`,
		f.alert.AlertID).Scan(&pgRows))
	require.Zero(t, chRows)
	require.Zero(t, pgRows)
	require.Zero(t, p.terminate(t), p.output.String())
}

func testAlertWriterDLQOutage(t *testing.T) {
	f := newAlertReplayFixture(t)
	f.settings.OperationTimeout = 300 * time.Millisecond
	consumer, err := f.consumer(t)
	require.NoError(t, err)
	defer closeWriterConsumer(t, consumer)
	produceDLQSources(t, f.kafka, []*kgo.Record{{Topic: f.kafka.AlertsTopic, Value: []byte("{")}})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	err = alertwriter.New(consumer, storageclickhouse.NewAlertStore(f.ch),
		storagepostgres.NewAlertStore(f.db.Pool, f.settings.OperationTimeout), f.settings, zerolog.Nop()).
		WithQuarantine(unavailableDLQ(t, f.kafka, f.settings)).Run(ctx)
	require.ErrorContains(t, err, "quarantine")
	require.Zero(t, f.committed(t))
	ends, err := f.admin.ListEndOffsets(t.Context(), f.kafka.DLQTopic)
	require.NoError(t, err)
	end, ok := ends.Lookup(f.kafka.DLQTopic, 0)
	require.True(t, ok)
	require.Zero(t, end.Offset)
}

func testAlertWriterSlowDLQ(t *testing.T) {
	f := newAlertReplayFixture(t)
	f.settings.BatchSize = 400
	var records []*kgo.Record
	for range 400 {
		records = append(records, &kgo.Record{Topic: f.kafka.AlertsTopic, Value: []byte("{")})
	}
	produceDLQSources(t, f.kafka, records)
	consumer, err := f.consumer(t)
	require.NoError(t, err)
	defer closeWriterConsumer(t, consumer)
	slow := &slowRealDLQ{inner: f.dlq, second: make(chan struct{}), acknowledged: make(map[poisonOrigin]bool)}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	err = alertwriter.New(consumer, storageclickhouse.NewAlertStore(f.ch),
		storagepostgres.NewAlertStore(f.db.Pool, f.settings.OperationTimeout), f.settings, zerolog.Nop()).
		WithQuarantine(slow).Run(ctx)
	require.Error(t, err)
	require.Less(t, slow.elapsed(), 3*time.Second)
	acks := slow.acknowledgedOrigins()
	require.NotEmpty(t, acks)
	committed := f.committed(t)
	require.Equal(t, int64(len(acks)), committed)
	for n := range committed {
		require.True(t, acks[poisonOrigin{0, n}], "no commit may skip an unacknowledged quarantine")
	}
}
