package eventwriter_test

import (
	"context"
	"encoding/json"
	"errors"
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
)

type scriptedConsumer struct {
	mu             sync.Mutex
	records        []*kgo.Record
	pollLimits     []int
	commits        [][]*kgo.Record
	allowRebalance int
	onCommit       func()
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

func (c *scriptedConsumer) CommitRecords(_ context.Context, records ...*kgo.Record) error {
	c.mu.Lock()
	c.commits = append(c.commits, append([]*kgo.Record(nil), records...))
	onCommit := c.onCommit
	c.mu.Unlock()
	if onCommit != nil {
		onCommit()
	}

	return nil
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
	w := eventwriter.New(consumer, store, writerSettings(2, time.Hour), limits, zerolog.Nop())

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
	w := eventwriter.New(
		consumer, store, writerSettings(5, 10*time.Millisecond), contracts.DefaultLimits(), zerolog.Nop(),
	)

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
	w := eventwriter.New(
		consumer, store, writerSettings(1, time.Second), contracts.DefaultLimits(), zerolog.Nop(),
	)

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
	w := eventwriter.New(consumer, store, settings, contracts.DefaultLimits(), zerolog.Nop())
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
