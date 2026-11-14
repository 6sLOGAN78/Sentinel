package kafkax_test

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

	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

const (
	lagGroup = "event-writer"
	lagTopic = "security.events.v1"
	lagName  = "sentinel_kafka_consumer_lag"
)

// fakeEnds is a test-local EndOffsetLister with mutable broker end offsets.
type fakeEnds struct {
	mu        sync.Mutex
	ends      map[int32]int64
	errs      map[int32]error
	err       error
	calls     int
	deadlines []time.Duration
}

func newFakeEnds(ends map[int32]int64) *fakeEnds {
	return &fakeEnds{ends: ends, errs: map[int32]error{}}
}

func (f *fakeEnds) ListEndOffsets(ctx context.Context, topics ...string) (kadm.ListedOffsets, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if deadline, ok := ctx.Deadline(); ok {
		f.deadlines = append(f.deadlines, time.Until(deadline))
	} else {
		f.deadlines = append(f.deadlines, -1)
	}
	if f.err != nil {
		return nil, f.err
	}
	out := kadm.ListedOffsets{}
	for _, topic := range topics {
		out[topic] = map[int32]kadm.ListedOffset{}
		for partition, end := range f.ends {
			out[topic][partition] = kadm.ListedOffset{
				Topic: topic, Partition: partition, Offset: end, Err: f.errs[partition],
			}
		}
	}
	return out, nil
}

func (f *fakeEnds) set(partition int32, end int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ends[partition] = end
}

func (f *fakeEnds) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeEnds) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeCommitted is a test-local CommittedOffsetSource.
type fakeCommitted struct {
	committed map[int32]int64
	starts    map[int32]int64
	fetchErr  error
	startErr  error
	group     string
}

func (f *fakeCommitted) FetchOffsets(_ context.Context, group string) (kadm.OffsetResponses, error) {
	f.group = group
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	out := kadm.OffsetResponses{lagTopic: map[int32]kadm.OffsetResponse{}}
	for partition, at := range f.committed {
		out[lagTopic][partition] = kadm.OffsetResponse{
			Offset: kadm.Offset{Topic: lagTopic, Partition: partition, At: at},
		}
	}
	return out, nil
}

func (f *fakeCommitted) ListStartOffsets(_ context.Context, topics ...string) (kadm.ListedOffsets, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	out := kadm.ListedOffsets{}
	for _, topic := range topics {
		out[topic] = map[int32]kadm.ListedOffset{}
		for partition, start := range f.starts {
			out[topic][partition] = kadm.ListedOffset{Topic: topic, Partition: partition, Offset: start}
		}
	}
	return out, nil
}

func lagTracker(t *testing.T, lister kafkax.EndOffsetLister) (*kafkax.LagTracker, *prometheus.Registry) {
	t.Helper()
	reg := telemetry.NewRegistry()
	metrics := telemetry.NewConsumerMetrics(reg)
	return kafkax.NewLagTracker(lister, metrics, lagGroup, lagTopic, 0), reg
}

// lagValue returns the gauge for partition and whether the series exists.
func lagValue(t *testing.T, reg prometheus.Gatherer, partition int32) (float64, bool) {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != lagName {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, pair := range metric.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}
			if labels["group"] == lagGroup && labels["topic"] == lagTopic &&
				labels["partition"] == strconv.Itoa(int(partition)) {
				return metric.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

func requireLag(t *testing.T, reg prometheus.Gatherer, partition int32, want float64) {
	t.Helper()
	got, ok := lagValue(t, reg, partition)
	require.True(t, ok, "lag series for partition %d must exist", partition)
	require.InDelta(t, want, got, 0, "lag for partition %d", partition)
}

func requireAbsent(t *testing.T, reg prometheus.Gatherer, partition int32) {
	t.Helper()
	_, ok := lagValue(t, reg, partition)
	require.False(t, ok, "lag series for partition %d must be absent", partition)
}

func TestLagEqualsEndMinusProcessedNext(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 100, 1: 50})
	tracker, reg := lagTracker(t, ends)
	tracker.Assigned(map[int32]int64{0: 0, 1: 0})
	tracker.Processed(0, 40)
	tracker.Processed(1, 50)

	require.NoError(t, tracker.Tick(t.Context()))
	requireLag(t, reg, 0, 60)
	requireLag(t, reg, 1, 0)
}

func TestLagNeverNegative(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 30})
	tracker, reg := lagTracker(t, ends)
	tracker.Assigned(map[int32]int64{0: 40})

	require.NoError(t, tracker.Tick(t.Context()))
	requireLag(t, reg, 0, 0)
}

func TestLagProcessedIsMonotonic(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 100})
	tracker, reg := lagTracker(t, ends)
	tracker.Assigned(map[int32]int64{0: 0})
	tracker.Processed(0, 40)
	tracker.Processed(0, 10) // replay after a rebalance must not move backwards

	require.NoError(t, tracker.Tick(t.Context()))
	requireLag(t, reg, 0, 60)
}

func TestLagRisesWhileWorkerIsStuck(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 100})
	tracker, reg := lagTracker(t, ends)
	tracker.Assigned(map[int32]int64{0: 40})

	previous := -1.0
	for _, end := range []int64{100, 150, 220} {
		ends.set(0, end)
		require.NoError(t, tracker.Tick(t.Context()))
		got, ok := lagValue(t, reg, 0)
		require.True(t, ok)
		require.Greater(t, got, previous, "lag must keep rising while nothing is processed")
		previous = got
	}
	requireLag(t, reg, 0, 180)
}

func TestLagRevokedDeletesSeriesAndStaysAbsent(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 100, 1: 50})
	tracker, reg := lagTracker(t, ends)
	tracker.Assigned(map[int32]int64{0: 10, 1: 20})
	require.NoError(t, tracker.Tick(t.Context()))
	requireLag(t, reg, 0, 90)

	// Revoked must call DeleteLabelValues so the old owner exports no stale series.
	tracker.Revoked([]int32{0})
	requireAbsent(t, reg, 0)
	requireLag(t, reg, 1, 30)

	// The broker still reports partition 0; it must not be recreated.
	require.NoError(t, tracker.Tick(t.Context()))
	requireAbsent(t, reg, 0)
	// A late commit for a partition this process no longer owns is ignored.
	tracker.Processed(0, 99)
	require.NoError(t, tracker.Tick(t.Context()))
	requireAbsent(t, reg, 0)
}

func TestLagSumAcrossReplicasEqualsBacklogAfterRebalance(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 100, 1: 50})
	oldOwner, oldReg := lagTracker(t, ends)
	newOwner, newReg := lagTracker(t, ends)
	oldOwner.Assigned(map[int32]int64{0: 10, 1: 20})
	require.NoError(t, oldOwner.Tick(t.Context()))

	oldOwner.Revoked([]int32{1})
	newOwner.Assigned(map[int32]int64{1: 20})
	require.NoError(t, oldOwner.Tick(t.Context()))
	require.NoError(t, newOwner.Tick(t.Context()))

	sum := 0.0
	for _, reg := range []prometheus.Gatherer{oldReg, newReg} {
		for _, partition := range []int32{0, 1} {
			if value, ok := lagValue(t, reg, partition); ok {
				sum += value
			}
		}
	}
	require.InDelta(t, 90+30, sum, 0, "sum(lag) across replicas must equal the true backlog")
}

func TestLagListErrorKeepsPreviousValues(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 100, 1: 50})
	tracker, reg := lagTracker(t, ends)
	tracker.Assigned(map[int32]int64{0: 40, 1: 45})
	require.NoError(t, tracker.Tick(t.Context()))

	ends.set(0, 500)
	ends.fail(context.DeadlineExceeded)
	require.Error(t, tracker.Tick(t.Context()))
	requireLag(t, reg, 0, 60)
	requireLag(t, reg, 1, 5)

	// A per-partition load error leaves only that partition's last value.
	ends.fail(nil)
	ends.errs[1] = errors.New("leader not available")
	ends.set(1, 900)
	require.NoError(t, tracker.Tick(t.Context()))
	requireLag(t, reg, 0, 460)
	requireLag(t, reg, 1, 5)
}

func TestLagTickBoundsEveryQueryWithTimeout(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 10})
	tracker, _ := lagTracker(t, ends)
	tracker.Assigned(map[int32]int64{0: 0})
	require.NoError(t, tracker.Tick(context.Background()))

	ends.mu.Lock()
	defer ends.mu.Unlock()
	require.Len(t, ends.deadlines, 1)
	require.Positive(t, ends.deadlines[0], "ListEndOffsets must receive a deadline")
	require.LessOrEqual(t, ends.deadlines[0], 2*time.Second, "query timeout is 2s")
}

func TestLagTickWithoutOwnedPartitionsSkipsQuery(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 10})
	tracker, reg := lagTracker(t, ends)
	require.NoError(t, tracker.Tick(t.Context()))
	require.Zero(t, ends.callCount(), "no admin call when nothing is owned")
	requireAbsent(t, reg, 0)
}

func TestLagRunSurvivesTransientErrorsAndExitsOnCancel(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 10})
	ends.fail(errors.New("broker unavailable"))
	reg := telemetry.NewRegistry()
	tracker := kafkax.NewLagTracker(ends, telemetry.NewConsumerMetrics(reg), lagGroup, lagTopic, time.Millisecond)
	tracker.Assigned(map[int32]int64{0: 0})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- tracker.Run(ctx) }()

	require.Eventually(t, func() bool { return ends.callCount() >= 3 }, 2*time.Second, time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("Run exited on a transient error: %v", err)
	default:
	}
	ends.fail(nil)
	require.Eventually(t, func() bool {
		_, ok := lagValue(t, reg, 0)
		return ok
	}, 2*time.Second, time.Millisecond, "Run recovers after the error clears")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after cancel")
	}
}

func TestLagRunRejectsMissingOffsetSource(t *testing.T) {
	tracker := kafkax.NewLagTracker(nil, telemetry.NewConsumerMetrics(telemetry.NewRegistry()),
		lagGroup, lagTopic, 0)
	require.Error(t, tracker.Run(t.Context()))
}

func TestLagSeedAssignedFromCommittedThenStartOffsets(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 100, 1: 50})
	tracker, reg := lagTracker(t, ends)
	source := &fakeCommitted{committed: map[int32]int64{0: 40, 1: -1}, starts: map[int32]int64{1: 7}}

	tracker.SeedAssigned(t.Context(), source, []int32{0, 1})
	require.Equal(t, lagGroup, source.group)
	require.NoError(t, tracker.Tick(t.Context()))
	requireLag(t, reg, 0, 60)
	requireLag(t, reg, 1, 43)
}

func TestLagSeedFailureLeavesPartitionUnseededUntilProcessed(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 100})
	tracker, reg := lagTracker(t, ends)
	tracker.SeedAssigned(t.Context(), &fakeCommitted{fetchErr: errors.New("coordinator unavailable")}, []int32{0})

	require.NoError(t, tracker.Tick(t.Context()))
	requireAbsent(t, reg, 0) // unknown position: no invented value

	tracker.Processed(0, 90)
	require.NoError(t, tracker.Tick(t.Context()))
	requireLag(t, reg, 0, 10)
}

func TestLagLabelsStayInsideAllowlist(t *testing.T) {
	ends := newFakeEnds(map[int32]int64{0: 100})
	tracker, reg := lagTracker(t, ends)
	tracker.Assigned(map[int32]int64{0: 1})
	require.NoError(t, tracker.Tick(t.Context()))

	outside, err := telemetry.LabelNamesOutsideAllowlist(reg)
	require.NoError(t, err)
	require.Empty(t, outside)
}
