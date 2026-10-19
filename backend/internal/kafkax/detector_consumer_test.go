package kafkax //nolint:testpackage // exercise real callback validation with a transport driver.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

type commitDriver struct {
	*kgo.Client
	response   *kmsg.OffsetCommitResponse
	err        error
	t          *testing.T
	called     *bool
	beforeDone func()
}

func (d commitDriver) CommitOffsetsSync(ctx context.Context, offsets map[string]map[int32]kgo.EpochOffset,
	done func(*kgo.Client, *kmsg.OffsetCommitRequest, *kmsg.OffsetCommitResponse, error),
) {
	*d.called = true
	require.Equal(d.t, map[string]map[int32]kgo.EpochOffset{
		"events": {0: {Epoch: -1, Offset: 12}, 1: {Epoch: -1, Offset: 23}},
	}, offsets)
	deadline, ok := ctx.Deadline()
	require.True(d.t, ok)
	require.LessOrEqual(d.t, time.Until(deadline), time.Second)
	if d.beforeDone != nil {
		d.beforeDone()
	}
	done(nil, nil, d.response, d.err)
}

func commitResponse(partitions ...kmsg.OffsetCommitResponseTopicPartition) *kmsg.OffsetCommitResponse {
	return &kmsg.OffsetCommitResponse{Topics: []kmsg.OffsetCommitResponseTopic{{Topic: "events", Partitions: partitions}}}
}

func (d commitDriver) GroupMetadata() (string, int32) { return "member-7", 9 }

type readinessDriver struct {
	commitDriver
	generation int32
}

func (d *readinessDriver) GroupMetadata() (string, int32) { return "member-7", d.generation }

func TestDetectorAssignmentReadiness(t *testing.T) {
	c := &DetectorConsumer{client: commitDriver{},
		assignments: make(map[detection.TopicPartition]DetectorAssignment)}
	require.False(t, c.ReadinessSnapshot().Observed, "membership is not assignment completion")
	c.assigned(t.Context(), nil, map[string][]int32{})
	snapshot := c.ReadinessSnapshot()
	require.True(t, snapshot.Observed, "explicit empty callback is a completed assignment")
	require.Equal(t, "member-7", snapshot.Member)
	require.Equal(t, int32(9), snapshot.Generation)
	require.Empty(t, snapshot.Tokens)
	tp := detection.TopicPartition{Topic: "events", Partition: 0}
	c.assigned(t.Context(), nil, map[string][]int32{"events": {0}})
	snapshot = c.ReadinessSnapshot()
	require.Equal(t, c.Assignments()[tp].Token, snapshot.Tokens[tp])
	c.revoked(t.Context(), nil, map[string][]int32{"events": {0}})
	require.False(t, c.ReadinessSnapshot().Observed)
	c.assigned(t.Context(), nil, map[string][]int32{})
	c.cancel = func() {}
	_ = c.fail(errors.New("terminal fixture failure"))
	require.False(t, c.ReadinessSnapshot().Observed, "terminal failures invalidate the snapshot")
}

func TestDetectorRetainedAssignment(t *testing.T) {
	driver := &readinessDriver{generation: 9}
	c := &DetectorConsumer{client: driver, assignments: make(map[detection.TopicPartition]DetectorAssignment)}
	tp := detection.TopicPartition{Topic: "events", Partition: 0}
	c.assigned(t.Context(), nil, map[string][]int32{"events": {0}})
	before := c.Assignments()[tp]
	driver.generation = 10
	c.assigned(t.Context(), nil, map[string][]int32{})
	after := c.Assignments()[tp]
	require.Equal(t, int32(10), after.Generation,
		"empty added set retains owned partitions under the new actual generation")
	require.Equal(t, before.Token, after.Token, "retained assignment preserves its ownership lifetime token")
}

// Ignoring callback errors or omitting any requested partition must prevent
// success; corrupt fetched metadata must cancel ownership and report terminal failure.
func TestDetectorCommitMetadata(t *testing.T) {
	t.Run("ownership observations are outside locks and preserve snapshots", func(t *testing.T) {
		tp := detection.TopicPartition{Topic: "events", Partition: 0}
		c := &DetectorConsumer{client: commitDriver{},
			assignments: make(map[detection.TopicPartition]DetectorAssignment)}
		var transitions []string
		c.observer = func(_ context.Context, event string, source detection.TopicPartition, a DetectorAssignment) error {
			_ = c.Assignments() // Observers must never run while the adapter holds its mutex.
			require.Equal(t, tp, source)
			require.NotZero(t, a.Token)
			transitions = append(transitions, event)
			return nil
		}
		c.assigned(t.Context(), nil, map[string][]int32{"events": {0}})
		c.revoked(t.Context(), nil, map[string][]int32{"events": {0}})
		c.assigned(t.Context(), nil, map[string][]int32{"events": {0}})
		c.lost(t.Context(), nil, map[string][]int32{"events": {0}})
		require.Empty(t, c.Assignments())
		require.False(t, c.ReadinessSnapshot().Observed)
		require.Equal(t, []string{"assigned", "revoked", "assigned", "lost"}, transitions)
	})
	tp0 := detection.TopicPartition{Topic: "events", Partition: 0}
	tp1 := detection.TopicPartition{Topic: "events", Partition: 1}
	checkpoints := map[detection.TopicPartition]detection.Checkpoint{
		tp0: {Offset: 12, Metadata: "canonical-0"}, tp1: {Offset: 23, Metadata: "canonical-1"},
	}
	for _, tc := range []struct {
		name     string
		response *kmsg.OffsetCommitResponse
		err      error
		wantErr  bool
	}{
		{"success", commitResponse(kmsg.OffsetCommitResponseTopicPartition{Partition: 0},
			kmsg.OffsetCommitResponseTopicPartition{Partition: 1}), nil, false},
		{"transport", nil, errors.New("transport failed"), true},
		{"partition", commitResponse(kmsg.OffsetCommitResponseTopicPartition{Partition: 0},
			kmsg.OffsetCommitResponseTopicPartition{Partition: 1, ErrorCode: 29}), nil, true},
		{"nil", nil, nil, true},
		{"missing", commitResponse(kmsg.OffsetCommitResponseTopicPartition{Partition: 0}), nil, true},
		{"duplicate", commitResponse(kmsg.OffsetCommitResponseTopicPartition{Partition: 0},
			kmsg.OffsetCommitResponseTopicPartition{Partition: 0}), nil, true},
		{"unexpected", commitResponse(kmsg.OffsetCommitResponseTopicPartition{Partition: 0},
			kmsg.OffsetCommitResponseTopicPartition{Partition: 2}), nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			previous := &detection.Checkpoint{Offset: 7, Metadata: "previous successful"}
			c := &DetectorConsumer{client: commitDriver{t: t, response: tc.response, err: tc.err, called: &called},
				settings: config.WriterConfig{OperationTimeout: time.Second},
				assignments: map[detection.TopicPartition]DetectorAssignment{
					tp0: {Token: 1, Checkpoint: previous}, tp1: {Token: 2},
				}}
			observed := 0
			c.observer = func(_ context.Context, boundary string, tp detection.TopicPartition, a DetectorAssignment) error {
				require.Equal(t, "committed", boundary)
				require.Equal(t, checkpoints[tp], *a.Checkpoint)
				_ = c.Assignments()
				observed++
				return nil
			}
			err := c.CommitCheckpoints(context.Background(), checkpoints)
			require.True(t, called, "checkpoint must reach the authoritative completion callback")
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
			if tc.wantErr {
				require.Equal(t, previous, c.assignments[tp0].Checkpoint)
				require.Zero(t, observed, "failed or incomplete callbacks cannot signal success")
			} else {
				require.Equal(t, 2, observed, "only authoritative successful callbacks may emit committed observations")
				require.Equal(t, checkpoints[tp0], *c.assignments[tp0].Checkpoint,
					"only successful complete callback records the acknowledged checkpoint")
				require.Equal(t, checkpoints[tp1], *c.assignments[tp1].Checkpoint)
			}
		})
	}
	t.Run("stale completion cannot replace reassigned checkpoint", func(t *testing.T) {
		called := false
		previous := &detection.Checkpoint{Offset: 7, Metadata: "previous successful"}
		c := &DetectorConsumer{settings: config.WriterConfig{OperationTimeout: time.Second},
			assignments: map[detection.TopicPartition]DetectorAssignment{tp0: {Token: 1, Checkpoint: previous}, tp1: {Token: 2}}}
		c.client = commitDriver{t: t, called: &called,
			response: commitResponse(kmsg.OffsetCommitResponseTopicPartition{Partition: 0},
				kmsg.OffsetCommitResponseTopicPartition{Partition: 1}),
			beforeDone: func() { c.mu.Lock(); defer c.mu.Unlock(); a := c.assignments[tp0]; a.Token++; c.assignments[tp0] = a }}
		require.Error(t, c.CommitCheckpoints(context.Background(), checkpoints))
		require.True(t, called)
		require.Equal(t, previous, c.Assignments()[tp0].Checkpoint)
		require.Nil(t, c.Assignments()[tp1].Checkpoint, "one changed owner prevents adoption of the checkpoint set")
	})
	t.Run("metadata attached atomically to every requested partition", func(t *testing.T) {
		req := &kmsg.OffsetCommitRequest{Topics: []kmsg.OffsetCommitRequestTopic{{Topic: "events",
			Partitions: []kmsg.OffsetCommitRequestTopicPartition{{Partition: 0, Offset: 12}, {Partition: 1, Offset: 23}},
		}}}
		require.NoError(t, attachMetadata(req, checkpoints))
		require.Equal(t, "canonical-0", *req.Topics[0].Partitions[0].Metadata)
		require.Equal(t, "canonical-1", *req.Topics[0].Partitions[1].Metadata)
		req.Topics[0].Partitions = req.Topics[0].Partitions[:1]
		require.Error(t, attachMetadata(req, checkpoints))
		req.Topics[0].Partitions[0].Offset++
		require.Error(t, attachMetadata(req, checkpoints))
	})
	t.Run("terminal fetched metadata failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var fatal error
		calls := 0
		c := &DetectorConsumer{lifetime: ctx, cancel: cancel,
			onFatal: func(err error) { fatal = err; calls++ }}
		err := c.fetched(ctx, nil, nil)
		require.Error(t, err)
		require.ErrorIs(t, fatal, err)
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.Error(t, c.fetched(ctx, nil, nil))
		require.Equal(t, 1, calls)
		require.ErrorIs(t, c.Ping(context.Background()), fatal)
	})
	t.Run("new groups differ from trimmed existing pins", func(t *testing.T) {
		c := &DetectorConsumer{assignments: map[detection.TopicPartition]DetectorAssignment{tp0: {Token: 1}}}
		starts := kadm.ListedOffsets{"events": {0: {Offset: 12}}}
		ends := kadm.ListedOffsets{"events": {0: {Offset: 23}}}
		require.NoError(t, c.restorePartition(tp0,
			kmsg.OffsetFetchResponseTopicPartition{Partition: 0, Offset: -1}, starts, ends))
		require.Nil(t, c.Assignments()[tp0].Checkpoint)
		require.Equal(t, int64(12), c.Assignments()[tp0].HistoryStart)
		require.Error(t, c.restorePartition(tp0,
			kmsg.OffsetFetchResponseTopicPartition{Partition: 0, Offset: -1}, nil, ends),
			"fresh group must still establish a verified retained history boundary")
		require.Error(t, c.restorePartition(tp0,
			kmsg.OffsetFetchResponseTopicPartition{Partition: 0, Offset: -2}, starts, ends),
			"only the exact Kafka absent-offset sentinel means fresh group")
		require.Error(t, c.restorePartition(tp0,
			kmsg.OffsetFetchResponseTopicPartition{Partition: 0, Offset: 11}, starts, ends))
		require.Error(t, c.restorePartition(tp0,
			kmsg.OffsetFetchResponseTopicPartition{Partition: 0, Offset: 24}, starts, ends))
		require.Nil(t, c.Assignments()[tp0].Checkpoint)
	})
	t.Run("ownership changes clear affected partitions only", func(t *testing.T) {
		c := &DetectorConsumer{client: commitDriver{},
			assignments: make(map[detection.TopicPartition]DetectorAssignment)}
		c.assigned(context.Background(), nil, map[string][]int32{"events": {0, 1}})
		before := c.Assignments()
		c.revoked(context.Background(), nil, map[string][]int32{"events": {0}})
		require.Equal(t, before[tp1], c.Assignments()[tp1])
		require.NotContains(t, c.Assignments(), tp0)
		c.assigned(context.Background(), nil, map[string][]int32{"events": {0}})
		require.Greater(t, c.Assignments()[tp0].Token, before[tp0].Token)
		member, generation := c.GroupMetadata()
		require.Equal(t, "member-7", member)
		require.Equal(t, int32(9), generation)
	})
	t.Run("bounded owned polling with distinct start and reset policies", func(t *testing.T) {
		settings := config.WriterConfig{BatchSize: 1, MaxAttempts: 1, OperationTimeout: time.Second,
			RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
			FlushInterval: time.Second, ShutdownTimeout: time.Second,
			FetchMaxBytes: 1 << 20, FetchMaxPartitionBytes: 512 << 10, MaxConcurrentFetches: 2}
		c, err := NewDetectorConsumer(context.Background(), config.KafkaConfig{
			Brokers: "127.0.0.1:1", EventsTopic: "events", DetectorGroup: "detector-test",
		}, settings, nil, nil)
		require.NoError(t, err)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, c.Close(ctx))
		})
		client := c.client.(*kgo.Client)
		require.Equal(t, true, client.OptValue(kgo.DisableAutoCommit))
		require.Equal(t, true, client.OptValue(kgo.BlockRebalanceOnPoll))
		require.Equal(t, kgo.NewOffset().AtStart(), client.OptValue(kgo.ConsumeStartOffset))
		require.Equal(t, kgo.NoResetOffset(), client.OptValue(kgo.ConsumeResetOffset))
		require.EqualValues(t, 1<<20, client.OptValue(kgo.FetchMaxBytes))
		require.EqualValues(t, 512<<10, client.OptValue(kgo.FetchMaxPartitionBytes))
		require.EqualValues(t, 2, client.OptValue(kgo.MaxConcurrentFetches))
		require.Equal(t, 500*time.Millisecond, client.OptValue(kgo.FetchMaxWait))
		budgets, err := config.ComputeWriterBudgets(settings)
		require.NoError(t, err)
		require.Equal(t, budgets.Rebalance, client.OptValue(kgo.RebalanceTimeout))
	})
}
