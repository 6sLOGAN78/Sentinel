package eventwriter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/eventwriter"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

type scriptedConsumer struct {
	mu               sync.Mutex
	records          []*kgo.Record
	pollLimits       []int
	commits          [][]*kgo.Record
	allowRebalance   int
	onCommit         func()
	commitContextErr error
}

func (c *scriptedConsumer) PollRecords(ctx context.Context, maxRecords int) ([]*kgo.Record, error) {
	c.mu.Lock()
	c.pollLimits = append(c.pollLimits, maxRecords)
	if len(c.records) > 0 {
		count := min(maxRecords, len(c.records))
		out := append([]*kgo.Record(nil), c.records[:count]...)
		c.records = c.records[count:]
		c.mu.Unlock()

		return out, nil
	}
	c.mu.Unlock()

	<-ctx.Done()

	return nil, ctx.Err()
}

func (c *scriptedConsumer) CommitRecords(ctx context.Context, records ...*kgo.Record) error {
	c.mu.Lock()
	c.commitContextErr = ctx.Err()
	c.commits = append(c.commits, append([]*kgo.Record(nil), records...))
	onCommit := c.onCommit
	c.mu.Unlock()
	if onCommit != nil {
		onCommit()
	}

	return nil
}

type delayedDLQ struct {
	store       *recordingStore
	delay       time.Duration
	completed   int
	started     int
	insertFirst bool
}

func (p *delayedDLQ) Publish(ctx context.Context, _ *kgo.Record, _ kafkax.DLQReason) error {
	p.started++
	p.insertFirst = p.insertFirst && p.store.attempts > 0
	timer := time.NewTimer(p.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		p.completed++
		return nil
	}
}

func TestWriterAggregatePoisonBudget(t *testing.T) {
	now := time.Now().UTC()
	records := []*kgo.Record{validRecord(t, 0, now, now)}
	for offset := range int64(8) {
		records = append(records, &kgo.Record{Topic: "events", Partition: 0, Offset: offset + 1, Value: []byte(`{"broken":`)})
	}
	independent := validRecord(t, 0, now, now)
	independent.Partition = 1
	records = append(records, independent)
	consumer := &scriptedConsumer{records: records}
	store := &recordingStore{}
	dlq := &delayedDLQ{store: store, delay: 40 * time.Millisecond, insertFirst: true}
	settings := writerSettings(len(records), 10*time.Millisecond)
	settings.MaxAttempts = 1
	settings.OperationTimeout = 100 * time.Millisecond
	settings.ShutdownTimeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	err := newWriter(consumer, store, dlq, settings, contracts.DefaultLimits()).Run(ctx)
	elapsed := time.Since(started)
	t.Logf("aggregate elapsed=%s completed=%d started=%d", elapsed, dlq.completed, dlq.started)
	require.ErrorIs(t, err, context.DeadlineExceeded, "poison batch must exhaust one shared retry budget")
	require.Less(t, elapsed, 250*time.Millisecond)
	require.True(t, dlq.insertFirst)
	require.Equal(t, 2, dlq.completed)
	require.Equal(t, 3, dlq.started, "no new poison work after exhaustion")
	require.Len(t, consumer.commits, 1)
	require.NoError(t, consumer.commitContextErr, "expired effects context must leave the commit reservation live")
	require.Equal(t, map[int32]int64{0: 2, 1: 0}, offsetsByPartition(consumer.commits[0]))
	require.Equal(t, 1, consumer.allowRebalance)
	require.Equal(t, 1, store.attempts, "internal expiry must not trigger a shutdown retry")
}

func (c *scriptedConsumer) AllowRebalance() {
	c.mu.Lock()
	c.allowRebalance++
	c.mu.Unlock()
}

type recordingStore struct {
	mu       sync.Mutex
	attempts int
	events   [][]*contracts.Event
	fail     error
	order    *[]string
}

type recordingDLQ struct {
	mu      sync.Mutex
	records []*kgo.Record
	reasons []kafkax.DLQReason
	fail    error
	failFor func(*kgo.Record) error
}

func (p *recordingDLQ) Publish(_ context.Context, record *kgo.Record, reason kafkax.DLQReason) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, record)
	p.reasons = append(p.reasons, reason)
	if p.failFor != nil {
		return p.failFor(record)
	}

	return p.fail
}

type shutdownStore struct {
	mu       sync.Mutex
	attempts int
	entered  chan struct{}
}

func (s *shutdownStore) InsertBatch(ctx context.Context, _ []*contracts.Event) error {
	s.mu.Lock()
	s.attempts++
	attempt := s.attempts
	if attempt == 1 {
		close(s.entered)
	}
	s.mu.Unlock()
	if attempt == 1 {
		<-ctx.Done()

		return ctx.Err()
	}

	return nil
}

func (s *recordingStore) InsertBatch(_ context.Context, events []*contracts.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	s.events = append(s.events, append([]*contracts.Event(nil), events...))
	if s.order != nil {
		*s.order = append(*s.order, "insert")
	}

	return s.fail
}

func writerSettings(batchSize int, interval time.Duration) config.WriterConfig {
	return config.WriterConfig{
		BatchSize:        batchSize,
		FlushInterval:    interval,
		MaxAttempts:      3,
		RetryMinBackoff:  time.Millisecond,
		RetryMaxBackoff:  2 * time.Millisecond,
		OperationTimeout: time.Second,
		ShutdownTimeout:  time.Second,
	}
}

func newWriter(
	consumer eventwriter.Consumer,
	store eventwriter.EventStore,
	dlq eventwriter.QuarantinePublisher,
	settings config.WriterConfig,
	limits contracts.Limits,
) *eventwriter.Writer {
	return eventwriter.New(consumer, store, dlq, settings, limits, zerolog.Nop())
}

func validRecord(t *testing.T, offset int64, occurredAt, receivedAt time.Time) *kgo.Record {
	t.Helper()
	received := contracts.NewTimestamp(receivedAt)
	event := contracts.Event{
		SchemaVersion: contracts.EventSchemaVersion,
		EventID:       uuid.NewString(),
		TenantID:      "demo",
		OccurredAt:    contracts.NewTimestamp(occurredAt),
		ReceivedAt:    &received,
		Source:        "writer-unit",
		EventType:     "net.connection",
		Attributes:    map[string]any{},
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)

	return &kgo.Record{Topic: "events", Partition: 0, Offset: offset, Value: raw}
}

func TestWriterFlushesAtSizeBeforeCommitAndBoundsPolls(t *testing.T) {
	now := time.Now().UTC()
	consumer := &scriptedConsumer{records: []*kgo.Record{
		validRecord(t, 0, now.Add(-48*time.Hour), now),
		validRecord(t, 1, now.Add(-time.Minute), now),
		validRecord(t, 2, now.Add(-time.Minute), now),
	}}
	var order []string
	store := &recordingStore{order: &order}
	ctx, cancel := context.WithCancel(context.Background())
	consumer.onCommit = func() {
		order = append(order, "commit")
		cancel()
	}
	limits := contracts.DefaultLimits()
	limits.PastSkew = time.Hour
	w := newWriter(consumer, store, &recordingDLQ{}, writerSettings(2, time.Second), limits)

	require.NoError(t, w.Run(ctx))
	require.Equal(t, []string{"insert", "commit"}, order)
	require.Len(t, store.events, 1)
	require.Len(t, store.events[0], 2)
	require.Equal(t, now.Add(-48*time.Hour).Truncate(time.Millisecond), store.events[0][0].OccurredAt.Time)
	require.Equal(t, []int{2}, consumer.pollLimits)
	require.Len(t, consumer.commits, 1)
	require.Len(t, consumer.commits[0], 1, "only the last contiguous record per partition is committed")
	require.Equal(t, int64(1), consumer.commits[0][0].Offset)
}

func TestWriterFlushesOnWallClockInterval(t *testing.T) {
	now := time.Now().UTC()
	consumer := &scriptedConsumer{records: []*kgo.Record{validRecord(t, 0, now, now)}}
	store := &recordingStore{}
	ctx, cancel := context.WithCancel(context.Background())
	consumer.onCommit = cancel
	w := newWriter(consumer, store, &recordingDLQ{}, writerSettings(5, 10*time.Millisecond), contracts.DefaultLimits())

	require.NoError(t, w.Run(ctx))
	require.Len(t, store.events, 1)
	require.Len(t, store.events[0], 1)
	require.Equal(t, []int{5, 4}, consumer.pollLimits)
}

func TestWriterRetryExhaustionDoesNotCommit(t *testing.T) {
	now := time.Now().UTC()
	consumer := &scriptedConsumer{records: []*kgo.Record{validRecord(t, 0, now, now)}}
	storeErr := errors.New("clickhouse unavailable")
	store := &recordingStore{fail: storeErr}
	w := newWriter(consumer, store, &recordingDLQ{}, writerSettings(1, time.Second), contracts.DefaultLimits())

	err := w.Run(context.Background())
	require.ErrorIs(t, err, storeErr)
	require.Equal(t, 3, store.attempts)
	require.Empty(t, consumer.commits)
	require.Equal(t, 1, consumer.allowRebalance)
}

func TestWriterCancellationMidInsertFlushesWithFreshContext(t *testing.T) {
	now := time.Now().UTC()
	consumer := &scriptedConsumer{records: []*kgo.Record{validRecord(t, 0, now, now)}}
	store := &shutdownStore{entered: make(chan struct{})}
	settings := writerSettings(1, time.Second)
	settings.MaxAttempts = 1
	w := newWriter(consumer, store, &recordingDLQ{}, settings, contracts.DefaultLimits())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("insert did not start")
	}
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("writer leaked after cancellation")
	}
	require.Equal(t, 2, store.attempts)
	require.Len(t, consumer.commits, 1)
	require.Equal(t, 1, consumer.allowRebalance)
}

func TestWriterValidationReasonDoesNotExposeUntrustedValue(t *testing.T) {
	now := time.Now().UTC()
	record := validRecord(t, 0, now, now)
	secret := "sentinel-secret-event-type"
	record.Value = bytes.Replace(record.Value, []byte("net.connection"), []byte(secret), 1)
	consumer := &scriptedConsumer{records: []*kgo.Record{record}}
	ctx, cancel := context.WithCancel(context.Background())
	consumer.onCommit = cancel
	dlq := &recordingDLQ{}
	w := newWriter(
		consumer, &recordingStore{}, dlq, writerSettings(1, time.Second), contracts.DefaultLimits(),
	)

	require.NoError(t, w.Run(ctx))
	require.Equal(t, []kafkax.DLQReason{kafkax.DLQReasonValidation}, dlq.reasons)
	require.NotContains(t, string(dlq.reasons[0]), secret)
	require.Len(t, consumer.commits, 1)
}

func TestWriterClassifiesOnlyPoisonWithStableReasons(t *testing.T) {
	now := time.Now().UTC()
	invalidJSON := &kgo.Record{Topic: "events", Partition: 0, Offset: 0, Value: []byte(`{"broken":`)}
	invalidSchema := validRecord(t, 1, now, now)
	invalidSchema.Value = bytes.Replace(invalidSchema.Value, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1)
	missingReceivedAt := validRecord(t, 2, now, now)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(missingReceivedAt.Value, &decoded))
	delete(decoded, "received_at")
	missingReceivedAt.Value, _ = json.Marshal(decoded)

	consumer := &scriptedConsumer{records: []*kgo.Record{invalidJSON, invalidSchema, missingReceivedAt}}
	dlq := &recordingDLQ{}
	ctx, cancel := context.WithCancel(context.Background())
	consumer.onCommit = cancel
	w := newWriter(consumer, &recordingStore{}, dlq, writerSettings(3, time.Second), contracts.DefaultLimits())

	require.NoError(t, w.Run(ctx))
	require.Equal(t, []kafkax.DLQReason{
		kafkax.DLQReasonDecode,
		kafkax.DLQReasonValidation,
		kafkax.DLQReasonMissingReceivedAt,
	}, dlq.reasons)
	require.Len(t, consumer.commits, 1)
	require.Equal(t, int64(2), consumer.commits[0][0].Offset)
}

func TestWriterDLQFailureDoesNotCommitPastPoison(t *testing.T) {
	now := time.Now().UTC()
	consumer := &scriptedConsumer{records: []*kgo.Record{
		validRecord(t, 0, now, now),
		{Topic: "events", Partition: 0, Offset: 1, Value: []byte(`{"broken":`)},
		validRecord(t, 2, now, now),
	}}
	dlqErr := errors.New("DLQ unavailable")
	dlq := &recordingDLQ{fail: dlqErr}
	w := newWriter(consumer, &recordingStore{}, dlq, writerSettings(3, time.Second), contracts.DefaultLimits())

	err := w.Run(context.Background())
	require.ErrorIs(t, err, dlqErr)
	for _, commit := range consumer.commits {
		for _, record := range commit {
			require.Less(t, record.Offset, int64(1), "commit skipped failed poison offset")
		}
	}
}

func TestWriterMixedPartitionsCommitIndependentDurablePrefixes(t *testing.T) {
	for _, failedPartition := range []int32{0, 1} {
		t.Run(fmt.Sprintf("partition_%d_fails", failedPartition), func(t *testing.T) {
			records := mixedPartitionRecords(t)
			consumer := &scriptedConsumer{records: records}
			dlqErr := errors.New("partition DLQ unavailable")
			dlq := &recordingDLQ{failFor: func(record *kgo.Record) error {
				if record.Partition == failedPartition {
					return dlqErr
				}

				return nil
			}}
			w := newWriter(
				consumer, &recordingStore{}, dlq, writerSettings(len(records), time.Second), contracts.DefaultLimits(),
			)

			err := w.Run(context.Background())
			require.ErrorIs(t, err, dlqErr)
			require.Len(t, consumer.commits, 1)
			committed := offsetsByPartition(consumer.commits[0])
			require.Equal(t, int64(0), committed[failedPartition])
			require.Equal(t, int64(2), committed[1-failedPartition])
		})
	}
}

func mixedPartitionRecords(t *testing.T) []*kgo.Record {
	t.Helper()
	now := time.Now().UTC()
	records := make([]*kgo.Record, 0, 6)
	for offset := range int64(3) {
		for partition := range int32(2) {
			var record *kgo.Record
			if offset == 1 {
				record = &kgo.Record{Topic: "events", Value: []byte(`{"broken":`)}
			} else {
				record = validRecord(t, offset, now, now)
			}
			record.Partition = partition
			record.Offset = offset
			records = append(records, record)
		}
	}

	return records
}

func offsetsByPartition(records []*kgo.Record) map[int32]int64 {
	offsets := make(map[int32]int64, len(records))
	for _, record := range records {
		offsets[record.Partition] = record.Offset
	}

	return offsets
}

func TestWriterBudgetRejectsDirectInvalidSettings(t *testing.T) {
	settings := writerSettings(1, time.Second)
	settings.MaxAttempts = 0
	err := newWriter(nil, nil, nil, settings, contracts.DefaultLimits()).Run(t.Context())
	require.ErrorContains(t, err, "writer config")
	settings = writerSettings(1, 5*time.Minute)
	err = newWriter(nil, nil, nil, settings, contracts.DefaultLimits()).Run(t.Context())
	require.ErrorContains(t, err, "writer config")
}
