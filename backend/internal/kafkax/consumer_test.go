package kafkax //nolint:testpackage // bounded close and rebalance budgeting require white-box state injection

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

func TestNewConsumerRejectsMalformedBrokerList(t *testing.T) {
	_, err := NewConsumer(context.Background(), config.KafkaConfig{Brokers: "not-host-port"}, config.WriterConfig{})
	require.ErrorContains(t, err, "kafka")
}

func TestConsumerCloseHonorsContext(t *testing.T) {
	release := make(chan struct{})
	consumer := &Consumer{
		closeDone: make(chan struct{}),
		closeFn:   func() { <-release },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, consumer.Close(ctx), context.DeadlineExceeded)
	close(release)
	require.Eventually(t, func() bool {
		return consumer.Close(context.Background()) == nil
	}, time.Second, time.Millisecond)
}

type hookSink struct {
	mu         sync.Mutex
	lag        map[int32]int64
	deleted    []int32
	partitions []int
}

func (s *hookSink) SetTopicPartitions(_, _ string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partitions = append(s.partitions, n)
}

func (s *hookSink) partitionCalls() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.partitions...)
}

func (s *hookSink) SetLag(_, _ string, partition int32, lag int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lag[partition] = lag
}

func (s *hookSink) DeleteLag(_, _ string, partition int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lag, partition)
	s.deleted = append(s.deleted, partition)
}

type hookOffsets struct{ ends, committed map[int32]int64 }

func (h hookOffsets) ListEndOffsets(_ context.Context, topics ...string) (kadm.ListedOffsets, error) {
	return listed(topics, h.ends), nil
}

func (h hookOffsets) ListStartOffsets(_ context.Context, topics ...string) (kadm.ListedOffsets, error) {
	return listed(topics, map[int32]int64{}), nil
}

func (h hookOffsets) FetchOffsets(_ context.Context, _ string) (kadm.OffsetResponses, error) {
	out := kadm.OffsetResponses{"events": map[int32]kadm.OffsetResponse{}}
	for partition, at := range h.committed {
		out["events"][partition] = kadm.OffsetResponse{Offset: kadm.Offset{Topic: "events", Partition: partition, At: at}}
	}
	return out, nil
}

func listed(topics []string, offsets map[int32]int64) kadm.ListedOffsets {
	out := kadm.ListedOffsets{}
	for _, topic := range topics {
		out[topic] = map[int32]kadm.ListedOffset{}
		for partition, offset := range offsets {
			out[topic][partition] = kadm.ListedOffset{Topic: topic, Partition: partition, Offset: offset}
		}
	}
	return out
}

func TestConsumerLagHooksTrackAssignmentCommitAndRevoke(t *testing.T) {
	offsets := hookOffsets{ends: map[int32]int64{0: 100, 1: 80}, committed: map[int32]int64{0: 10, 1: 20}}
	sink := &hookSink{lag: map[int32]int64{}}
	tracker := NewLagTracker(offsets, sink, "writers", "events", 0)
	consumer := &Consumer{lag: tracker, admin: offsets}

	consumer.onAssigned(t.Context(), nil, map[string][]int32{"events": {0, 1}, "other": {7}})
	require.NoError(t, tracker.Tick(t.Context()))
	require.Equal(t, map[int32]int64{0: 90, 1: 60}, sink.lag, "seeded from committed offsets")
	require.Equal(t, []int{2}, sink.partitionCalls(), "the topic partition count is published each tick")

	consumer.recordProcessed([]*kgo.Record{
		{Topic: "events", Partition: 0, Offset: 41}, {Topic: "events", Partition: 0, Offset: 39},
		{Topic: "events", Partition: 1, Offset: 59}, {Topic: "other", Partition: 0, Offset: 99},
	})
	require.NoError(t, tracker.Tick(t.Context()))
	require.Equal(t, map[int32]int64{0: 58, 1: 20}, sink.lag, "processed next is max committed offset + 1")

	consumer.onRevoked(t.Context(), nil, map[string][]int32{"events": {1}})
	require.Equal(t, []int32{1}, sink.deleted)
	consumer.onRevoked(t.Context(), nil, map[string][]int32{"events": {0}}) // lost uses the same hook
	require.ElementsMatch(t, []int32{1, 0}, sink.deleted)
	require.NoError(t, tracker.Tick(t.Context()))
	require.Empty(t, sink.lag)
	require.Equal(t, []int{2, 2, 2}, sink.partitionCalls(),
		"a writer owning nothing after revoke still reports the coverage denominator")
}

func TestConsumerWithoutLagTrackerIgnoresHooks(t *testing.T) {
	consumer := &Consumer{}
	require.NotPanics(t, func() {
		consumer.onAssigned(t.Context(), nil, map[string][]int32{"events": {0}})
		consumer.recordProcessed([]*kgo.Record{{Topic: "events", Offset: 1}})
		consumer.onRevoked(t.Context(), nil, map[string][]int32{"events": {0}})
	})
}

func TestNewConsumerRejectsLagTrackerForAnotherGroupOrTopic(t *testing.T) {
	kafka := config.KafkaConfig{Brokers: "localhost:9092", EventsTopic: "events", EventWriterGroup: "writers"}
	settings := config.WriterConfig{
		BatchSize: 1, MaxAttempts: 1, OperationTimeout: time.Second, RetryMaxBackoff: time.Second,
		FlushInterval: time.Second, ShutdownTimeout: time.Second, RetryMinBackoff: time.Millisecond,
		FetchMaxBytes: 1 << 20, FetchMaxPartitionBytes: 1 << 20, MaxConcurrentFetches: 1,
	}
	sink := &hookSink{lag: map[int32]int64{}}
	for _, tracker := range []*LagTracker{
		NewLagTracker(nil, sink, "other-group", "events", 0),
		NewLagTracker(nil, sink, "writers", "other-topic", 0),
	} {
		_, err := NewConsumer(t.Context(), kafka, settings, WithLagTracker(tracker))
		require.ErrorContains(t, err, "lag tracker")
	}

	tracker := NewLagTracker(nil, sink, "writers", "events", 0)
	consumer, err := NewConsumer(t.Context(), kafka, settings, WithLagTracker(tracker))
	require.NoError(t, err)
	defer closeWithin(t, consumer)
	require.NotNil(t, tracker.lister, "NewConsumer binds the consumer's kadm client as the end-offset source")

	alertKafka := kafka
	alertKafka.AlertsTopic, alertKafka.AlertWriterGroup = "alerts", "alert-writers"
	alertTracker := NewLagTracker(nil, sink, "alert-writers", "alerts", 0)
	alerts, err := NewAlertConsumer(t.Context(), alertKafka, settings, WithLagTracker(alertTracker))
	require.NoError(t, err, "NewAlertConsumer forwards options against the rewritten alert topic/group")
	defer closeWithin(t, alerts)
	require.NotNil(t, alertTracker.lister)
}

func closeWithin(t *testing.T, consumer *Consumer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, consumer.Close(ctx))
}

func TestRebalanceTimeoutCoversWorstProcessingBudget(t *testing.T) {
	settings := config.WriterConfig{
		BatchSize:        1,
		MaxAttempts:      3,
		OperationTimeout: 2 * time.Second,
		RetryMaxBackoff:  time.Second,
		FlushInterval:    time.Second,
		ShutdownTimeout:  3 * time.Second,
		RetryMinBackoff:  time.Millisecond,
	}

	budgets, err := config.ComputeWriterBudgets(settings)
	require.NoError(t, err)
	require.Equal(t, 27*time.Second, budgets.Rebalance,
		"collection + insert + aggregate poison + commit + shutdown + margin")
}
