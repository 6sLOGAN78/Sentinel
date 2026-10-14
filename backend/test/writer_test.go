//go:build integration

package test_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/eventwriter"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
	"github.com/6sLOGAN78/sentinel/migrations"
)

type gatedEventStore struct {
	inner    *storageclickhouse.EventStore
	inserted chan struct{}
	release  chan struct{}
}

func (s *gatedEventStore) InsertBatch(ctx context.Context, events []*contracts.Event) error {
	if err := s.inner.InsertBatch(ctx, events); err != nil {
		return err
	}
	close(s.inserted)
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func integrationWriterSettings() config.WriterConfig {
	return config.WriterConfig{
		BatchSize:              5000,
		FlushInterval:          time.Second,
		MaxAttempts:            5,
		RetryMinBackoff:        100 * time.Millisecond,
		RetryMaxBackoff:        5 * time.Second,
		OperationTimeout:       5 * time.Second,
		ShutdownTimeout:        20 * time.Second,
		FetchMaxBytes:          16 << 20,
		FetchMaxPartitionBytes: 4 << 20,
		MaxConcurrentFetches:   2,
		DLQMaxBytes:            64 << 10,
	}
}

func canonicalWriterEvents(count int, occurredAt time.Time) []*contracts.Event {
	events := make([]*contracts.Event, 0, count)
	for i := range count {
		received := contracts.NewTimestamp(time.Now().UTC())
		events = append(events, &contracts.Event{
			SchemaVersion: contracts.EventSchemaVersion,
			EventID:       uuid.NewString(),
			TenantID:      "demo",
			OccurredAt:    contracts.NewTimestamp(occurredAt.Add(time.Duration(i) * time.Millisecond)),
			ReceivedAt:    &received,
			Source:        "writer-integration",
			EventType:     "net.connection",
			Attributes:    map[string]any{},
		})
	}

	return events
}

func newWriterProducer(t *testing.T, kafka config.KafkaConfig) *kafkax.Producer {
	t.Helper()
	kafka.ProduceTimeout = 10 * time.Second
	kafka.RecordRetries = 5
	kafka.MaxBufferedRecords = 5000
	kafka.MaxBufferedBytes = 16 << 20
	producer, err := kafkax.NewProducer(t.Context(), kafka)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, producer.Close(context.Background())) })

	return producer
}

func committedNextOffset(t *testing.T, kafka config.KafkaConfig, group, topic string) (int64, bool) {
	t.Helper()
	kafkaClient, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...))
	require.NoError(t, err)
	t.Cleanup(kafkaClient.Close)
	client := kadm.NewClient(kafkaClient)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	offsets, err := client.FetchOffsets(ctx, group)
	require.NoError(t, err)
	offset, ok := offsets.Lookup(topic, 0)
	if !ok || offset.Err != nil || offset.At < 0 {
		return 0, false
	}

	return offset.At, true
}

func waitCommittedOffset(t *testing.T, kafka config.KafkaConfig, group, topic string, want int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		got, ok := committedNextOffset(t, kafka, group, topic)

		return ok && got == want
	}, 20*time.Second, 100*time.Millisecond)
}

func writerKafkaConfig(topic, group string) config.KafkaConfig {
	return config.KafkaConfig{Brokers: kafkaCfg.Brokers, EventsTopic: topic, EventWriterGroup: group}
}

func TestWriterCommitAfterInsert(t *testing.T) {
	store, conn := newDedupeStore(t)
	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "writer-order")
	group := "writer-order-" + uuid.NewString()
	kafka := writerKafkaConfig(topic, group)
	producer := newWriterProducer(t, kafka)
	old := time.Now().UTC().Add(-48 * time.Hour)
	events := canonicalWriterEvents(2, old)
	require.NoError(t, producer.PublishBatch(t.Context(), events, uuid.NewString()))

	consumer, err := kafkax.NewConsumer(t.Context(), kafka, integrationWriterSettings())
	require.NoError(t, err)
	t.Cleanup(func() { consumer.Close() })
	gated := &gatedEventStore{inner: store, inserted: make(chan struct{}), release: make(chan struct{})}
	settings := integrationWriterSettings()
	settings.BatchSize = len(events)
	limits := contracts.DefaultLimits()
	limits.PastSkew = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- eventwriter.New(consumer, gated, settings, limits, zerolog.Nop()).Run(ctx) }()

	select {
	case <-gated.inserted:
	case <-time.After(20 * time.Second):
		t.Fatal("writer did not insert the batch")
	}
	var rawCount uint64
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count() FROM events").Scan(&rawCount))
	require.Equal(t, uint64(len(events)), rawCount)
	_, committed := committedNextOffset(t, kafka, group, topic)
	require.False(t, committed, "source offset advanced before InsertBatch returned")

	close(gated.release)
	waitCommittedOffset(t, kafka, group, topic, int64(len(events)))
	cancel()
	require.NoError(t, <-done)
}

func TestWriterInsertFailureNoCommit(t *testing.T) {
	clickhouseCfg, handle, err := sentineltesting.StartClickHouseFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(handle.Close)
	_, err = chmigrate.New(
		clickhouseCfg.HTTPAddress, clickhouseCfg.User, clickhouseCfg.Password, clickhouseCfg.Database, nil,
	).Run(t.Context(), migrations.ClickHouse())
	require.NoError(t, err)
	conn, err := storageclickhouse.Open(t.Context(), clickhouseCfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	store := storageclickhouse.NewEventStore(conn)

	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "writer-failure")
	group := "writer-failure-" + uuid.NewString()
	kafka := writerKafkaConfig(topic, group)
	events := canonicalWriterEvents(2, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(), events, uuid.NewString()))
	consumer, err := kafkax.NewConsumer(t.Context(), kafka, integrationWriterSettings())
	require.NoError(t, err)

	require.NoError(t, handle.Pause(t.Context()))
	settings := integrationWriterSettings()
	settings.BatchSize = len(events)
	settings.MaxAttempts = 2
	settings.OperationTimeout = 500 * time.Millisecond
	settings.RetryMinBackoff = 10 * time.Millisecond
	settings.RetryMaxBackoff = 20 * time.Millisecond
	started := time.Now()
	runErr := eventwriter.New(
		consumer, store, settings, contracts.DefaultLimits(), zerolog.Nop(),
	).Run(t.Context())
	require.Error(t, runErr)
	require.Less(t, time.Since(started), 10*time.Second)
	consumer.Close()
	_, committed := committedNextOffset(t, kafka, group, topic)
	require.False(t, committed, "failed insert advanced the source offset")

	require.NoError(t, handle.Unpause(t.Context()))
	require.Eventually(t, func() bool { return conn.Ping(t.Context()) == nil }, 20*time.Second, 100*time.Millisecond)
	restart, err := kafkax.NewConsumer(t.Context(), kafka, integrationWriterSettings())
	require.NoError(t, err)
	t.Cleanup(func() { restart.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- eventwriter.New(
			restart, store, settings, contracts.DefaultLimits(), zerolog.Nop(),
		).Run(ctx)
	}()
	waitCommittedOffset(t, kafka, group, topic, int64(len(events)))
	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(len(events)), unique)
	cancel()
	require.NoError(t, <-done)

	for _, event := range events {
		got, getErr := store.Get(t.Context(), "demo", event.EventID)
		require.NoError(t, getErr, fmt.Sprintf("event %s", event.EventID))
		require.Equal(t, event.EventID, got.EventID)
	}
}

func TestWriterFailureExitsNonzero(t *testing.T) {
	clickhouseCfg, handle, err := sentineltesting.StartClickHouseFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(handle.Close)
	_, err = chmigrate.New(
		clickhouseCfg.HTTPAddress, clickhouseCfg.User, clickhouseCfg.Password, clickhouseCfg.Database, nil,
	).Run(t.Context(), migrations.ClickHouse())
	require.NoError(t, err)

	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "writer-process-failure")
	group := "writer-process-failure-" + uuid.NewString()
	kafka := writerKafkaConfig(topic, group)
	producer := newWriterProducer(t, kafka)
	env := serviceEnv(t, "event-writer")
	for _, prefix := range []string{
		"SENTINEL_KAFKA.EVENTS_TOPIC=", "SENTINEL_KAFKA.EVENT_WRITER_GROUP=",
		"SENTINEL_CLICKHOUSE.HTTP_ADDRESS=", "SENTINEL_CLICKHOUSE.NATIVE_ADDRESS=",
		"SENTINEL_CLICKHOUSE.USER=", "SENTINEL_CLICKHOUSE.PASSWORD=", "SENTINEL_CLICKHOUSE.DATABASE=",
	} {
		env = without(env, prefix)
	}
	env = append(env,
		"SENTINEL_KAFKA.EVENTS_TOPIC="+topic,
		"SENTINEL_KAFKA.EVENT_WRITER_GROUP="+group,
		"SENTINEL_CLICKHOUSE.HTTP_ADDRESS="+clickhouseCfg.HTTPAddress,
		"SENTINEL_CLICKHOUSE.NATIVE_ADDRESS="+clickhouseCfg.NativeAddress,
		"SENTINEL_CLICKHOUSE.USER="+clickhouseCfg.User,
		"SENTINEL_CLICKHOUSE.PASSWORD="+clickhouseCfg.Password,
		"SENTINEL_CLICKHOUSE.DATABASE="+clickhouseCfg.Database,
		"SENTINEL_WRITER.BATCH_SIZE=1",
		"SENTINEL_WRITER.MAX_ATTEMPTS=2",
		"SENTINEL_WRITER.OPERATION_TIMEOUT=500ms",
		"SENTINEL_WRITER.RETRY_MIN_BACKOFF=10ms",
		"SENTINEL_WRITER.RETRY_MAX_BACKOFF=20ms",
	)
	p := startProc(t, "event-writer", "", env)
	require.Eventually(t, func() bool {
		return strings.Contains(p.output.String(), "service started")
	}, failFastLimit, 50*time.Millisecond, p.output.String())

	require.NoError(t, handle.Pause(t.Context()))
	t.Cleanup(func() { _ = handle.Unpause(context.Background()) })
	events := canonicalWriterEvents(1, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, producer.PublishBatch(t.Context(), events, uuid.NewString()))

	require.NotZero(t, p.requireExit(t, failFastLimit), p.output.String())
	_, committed := committedNextOffset(t, kafka, group, topic)
	require.False(t, committed, "fatal writer failure advanced the source offset")
	require.NotContains(t, p.output.String(), events[0].EventID)
	require.NotContains(t, p.output.String(), clickhouseCfg.Password)
	require.Contains(t, p.output.String(), "background worker failed")
}
