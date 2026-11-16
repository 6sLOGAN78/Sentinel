package kafkax //nolint:testpackage // drive the real detector rebalance callbacks into the lag tracker.

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

type staticOffsets struct {
	mu        sync.Mutex
	offsets   map[int32]int64
	err       error
	calls     int
	deadlines []time.Duration
}

func (s *staticOffsets) list(ctx context.Context, topics ...string) (kadm.ListedOffsets, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if deadline, ok := ctx.Deadline(); ok {
		s.deadlines = append(s.deadlines, time.Until(deadline))
	} else {
		s.deadlines = append(s.deadlines, -1)
	}
	if s.err != nil {
		return nil, s.err
	}
	out := kadm.ListedOffsets{}
	for _, topic := range topics {
		out[topic] = map[int32]kadm.ListedOffset{}
		for partition, offset := range s.offsets {
			out[topic][partition] = kadm.ListedOffset{Topic: topic, Partition: partition, Offset: offset}
		}
	}
	return out, nil
}

func (s *staticOffsets) ListEndOffsets(ctx context.Context, topics ...string) (kadm.ListedOffsets, error) {
	return s.list(ctx, topics...)
}

func (s *staticOffsets) ListStartOffsets(ctx context.Context, topics ...string) (kadm.ListedOffsets, error) {
	return s.list(ctx, topics...)
}

func gaugeFor(t *testing.T, reg prometheus.Gatherer, name string, partition int32) (float64, bool) {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, pair := range metric.GetLabel() {
				if pair.GetName() == "partition" && pair.GetValue() == strconv.Itoa(int(partition)) {
					return metric.GetGauge().GetValue(), true
				}
			}
		}
	}
	return 0, false
}

func TestDetectorLagHooksSeedFromRestoredStartAndRevoke(t *testing.T) {
	client := detectorHistoryClient(t) // start offset 12, end offset 23
	rules := &detection.Rules{SchemaVersion: 1}
	ends := &staticOffsets{offsets: map[int32]int64{1: 23, 3: 40}}
	reg := telemetry.NewRegistry()
	tracker := NewLagTracker(ends, telemetry.NewConsumerMetrics(reg), "detector-test", "events", 0)
	driver := &readinessDriver{generation: 9}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := &DetectorConsumer{client: driver, rules: rules, group: "detector-test", topic: "events", cancel: cancel,
		settings: config.WriterConfig{OperationTimeout: time.Second}, lag: tracker,
		assignments: map[detection.TopicPartition]DetectorAssignment{}}

	c.assigned(ctx, nil, map[string][]int32{"events": {1}})
	require.NoError(t, tracker.Tick(ctx))
	_, seeded := gaugeFor(t, reg, "sentinel_kafka_consumer_lag", 1)
	require.False(t, seeded, "a pending restore has no known position and exports no lag")

	require.NoError(t, c.fetched(ctx, client, &kmsg.OffsetFetchResponse{Topics: []kmsg.OffsetFetchResponseTopic{
		{Topic: "events", Partitions: []kmsg.OffsetFetchResponseTopicPartition{{Partition: 1, Offset: -1}}},
	}}))
	require.NoError(t, tracker.Tick(ctx))
	lag, ok := gaugeFor(t, reg, "sentinel_kafka_consumer_lag", 1)
	require.True(t, ok)
	require.InDelta(t, 11, lag, 0, "a new group replays from the retained history start: end 23 - start 12")

	tracker.Processed(1, 20)
	require.NoError(t, tracker.Tick(ctx))
	lag, _ = gaugeFor(t, reg, "sentinel_kafka_consumer_lag", 1)
	require.InDelta(t, 3, lag, 0)

	c.revoked(ctx, nil, map[string][]int32{"events": {1}})
	_, ok = gaugeFor(t, reg, "sentinel_kafka_consumer_lag", 1)
	require.False(t, ok, "revoke deletes the lag series")
	tracker.Processed(1, 22)
	require.NoError(t, tracker.Tick(ctx))
	_, ok = gaugeFor(t, reg, "sentinel_kafka_consumer_lag", 1)
	require.False(t, ok, "a late processed report cannot recreate a revoked series")
}

func TestDetectorLagSeedsFromCommittedPinAndDropsOnLoss(t *testing.T) {
	ends := &staticOffsets{offsets: map[int32]int64{3: 40}}
	reg := telemetry.NewRegistry()
	tracker := NewLagTracker(ends, telemetry.NewConsumerMetrics(reg), "detector-test", "events", 0)
	tp := detection.TopicPartition{Topic: "events", Partition: 3}
	c := &DetectorConsumer{client: &readinessDriver{generation: 9}, group: "detector-test", topic: "events",
		lag: tracker, assignments: map[detection.TopicPartition]DetectorAssignment{
			tp: {Token: 1, HistoryStart: 5, Checkpoint: &detection.Checkpoint{Offset: 30, Metadata: "m"}},
		}}
	c.trackRestored([]int32{3})
	require.NoError(t, tracker.Tick(t.Context()))
	lag, ok := gaugeFor(t, reg, "sentinel_kafka_consumer_lag", 3)
	require.True(t, ok)
	require.InDelta(t, 10, lag, 0, "restored owners replay from the committed pin: end 40 - pin 30")

	c.lost(t.Context(), nil, map[string][]int32{"events": {3}})
	_, ok = gaugeFor(t, reg, "sentinel_kafka_consumer_lag", 3)
	require.False(t, ok, "loss deletes the lag series")
}

func pinConsumer(starts *staticOffsets) *DetectorConsumer {
	return &DetectorConsumer{topic: "events", starts: starts, assignments: map[detection.TopicPartition]DetectorAssignment{
		{Topic: "events", Partition: 0}: {Token: 1, Checkpoint: &detection.Checkpoint{Offset: 20, Metadata: "m"}},
		{Topic: "events", Partition: 1}: {Token: 2, HistoryStart: 12},
		{Topic: "events", Partition: 2}: {Token: 3, RestorePending: true,
			Checkpoint: &detection.Checkpoint{Offset: 30, Metadata: "m"}},
	}}
}

func TestDetectorPinMargins(t *testing.T) {
	starts := &staticOffsets{offsets: map[int32]int64{0: 12, 1: 12, 2: 12}}
	margins, err := pinConsumer(starts).PinMargins(t.Context())
	require.NoError(t, err)
	require.Equal(t, map[int32]int64{0: 8}, margins,
		"only restored owners with a committed pin report pin - log start")
	require.Len(t, starts.deadlines, 1)
	require.Positive(t, starts.deadlines[0])
	require.LessOrEqual(t, starts.deadlines[0], lagQueryTimeout, "each admin query is bounded")

	starts.err = errors.New("broker unavailable")
	_, err = pinConsumer(starts).PinMargins(t.Context())
	require.Error(t, err)

	empty := &DetectorConsumer{topic: "events", starts: starts,
		assignments: map[detection.TopicPartition]DetectorAssignment{}}
	calls := starts.calls
	margins, err = empty.PinMargins(t.Context())
	require.NoError(t, err)
	require.Empty(t, margins)
	require.Equal(t, calls, starts.calls, "no owned pins means no admin query")
}

func TestDetectorPinMarginTickPublishesAndDeletesRevoked(t *testing.T) {
	starts := &staticOffsets{offsets: map[int32]int64{0: 12, 1: 12, 2: 12}}
	c := pinConsumer(starts)
	reg := telemetry.NewRegistry()
	sink := telemetry.NewDetectorMetrics(reg)
	published := map[int32]bool{}
	c.pinMarginTick(t.Context(), sink, published)
	margin, ok := gaugeFor(t, reg, "sentinel_detector_pin_retention_margin_offsets", 0)
	require.True(t, ok)
	require.InDelta(t, 8, margin, 0)

	starts.err = errors.New("transient")
	c.pinMarginTick(t.Context(), sink, published)
	_, ok = gaugeFor(t, reg, "sentinel_detector_pin_retention_margin_offsets", 0)
	require.True(t, ok, "a transient admin error keeps the last value")

	starts.err = nil
	c.releaseAssignments(t.Context(), "revoked", map[string][]int32{"events": {0}})
	c.pinMarginTick(t.Context(), sink, published)
	_, ok = gaugeFor(t, reg, "sentinel_detector_pin_retention_margin_offsets", 0)
	require.False(t, ok, "a partition no longer owned loses its margin series")
}

func TestDetectorRunPinMarginsExitsOnCancel(t *testing.T) {
	starts := &staticOffsets{offsets: map[int32]int64{0: 12}}
	c := pinConsumer(starts)
	sink := telemetry.NewDetectorMetrics(telemetry.NewRegistry())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.RunPinMargins(ctx, sink, time.Hour) }()
	require.Eventually(t, func() bool { return starts.callCount() >= 1 }, time.Second, 5*time.Millisecond,
		"the first margin poll runs immediately")
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("RunPinMargins did not stop on cancel")
	}
	require.Error(t, (&DetectorConsumer{topic: "events"}).RunPinMargins(t.Context(), sink, time.Second),
		"an unwired consumer has no start offset source")
}

func TestNewDetectorConsumerRejectsLagTrackerForAnotherGroupOrTopic(t *testing.T) {
	settings := config.WriterConfig{BatchSize: 1, MaxAttempts: 1, OperationTimeout: time.Second,
		RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
		FlushInterval: time.Second, ShutdownTimeout: time.Second,
		FetchMaxBytes: 1 << 20, FetchMaxPartitionBytes: 512 << 10, MaxConcurrentFetches: 2}
	kafka := config.KafkaConfig{Brokers: "127.0.0.1:1", EventsTopic: "events", DetectorGroup: "detector-test"}
	for _, tracker := range []*LagTracker{
		NewLagTracker(nil, nil, "event-writer", "events", 0),
		NewLagTracker(nil, nil, "detector-test", "alerts", 0),
	} {
		_, err := NewDetectorConsumer(t.Context(), kafka, settings, nil, nil, nil, WithLagTracker(tracker))
		require.ErrorContains(t, err, "lag tracker")
	}
	tracker := NewLagTracker(nil, nil, "detector-test", "events", 0)
	c, err := NewDetectorConsumer(t.Context(), kafka, settings, nil, nil, nil, WithLagTracker(tracker))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, c.Close(ctx))
	})
	require.NotNil(t, tracker.source(), "the detector binds its own kadm client as the end-offset lister")
	require.NotNil(t, c.starts)
}

func (s *staticOffsets) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}
