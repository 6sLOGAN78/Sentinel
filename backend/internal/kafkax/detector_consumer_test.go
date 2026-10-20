package kafkax //nolint:testpackage // exercise real callback validation with a transport driver.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"

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

func TestDetectorPendingOffsetFetch(t *testing.T) {
	for _, scenario := range []string{"readiness before fetched callback", "commit before fetched callback"} {
		t.Run(scenario, func(t *testing.T) {
			called := false
			c := &DetectorConsumer{client: commitDriver{t: t, called: &called,
				response: commitResponse(kmsg.OffsetCommitResponseTopicPartition{Partition: 0},
					kmsg.OffsetCommitResponseTopicPartition{Partition: 1})},
				settings:    config.WriterConfig{OperationTimeout: time.Second},
				assignments: make(map[detection.TopicPartition]DetectorAssignment)}
			// Deliberately stop at this real callback boundary. OnOffsetsFetched
			// has not returned metadata or validated log bounds for either token.
			c.assigned(t.Context(), nil, map[string][]int32{"events": {0, 1}})
			t.Logf("assigned tokens exposed before offset fetch: %+v", c.Assignments())
			if scenario == "readiness before fetched callback" {
				require.False(t, c.ReadinessSnapshot().Observed,
					"assigned membership cannot certify pending authoritative offset fetch")
				return
			}
			err := c.CommitCheckpoints(t.Context(), map[detection.TopicPartition]detection.Checkpoint{
				{Topic: "events", Partition: 0}: {Offset: 12, Metadata: "canonical-0"},
				{Topic: "events", Partition: 1}: {Offset: 23, Metadata: "canonical-1"},
			})
			require.Error(t, err, "unfetched assignment must not overwrite the broker checkpoint")
			require.False(t, called, "pending metadata cannot reach the broker commit callback")
		})
	}
	for _, change := range []string{"completed new group", "revoke and reacquire", "generation", "member"} {
		t.Run(change, func(t *testing.T) {
			driver := &readinessDriver{generation: 9}
			c := &DetectorConsumer{client: driver, assignments: make(map[detection.TopicPartition]DetectorAssignment)}
			tp := detection.TopicPartition{Topic: "events", Partition: 0}
			c.assigned(t.Context(), nil, map[string][]int32{"events": {0}})
			expected := c.Assignments()
			// A genuinely new group becomes valid only after the absent offset
			// and actual retained-history bounds have both been validated.
			require.NoError(t, c.restorePartition(tp, kmsg.OffsetFetchResponseTopicPartition{Offset: -1},
				kadm.ListedOffsets{"events": {0: {Offset: 12}}}, kadm.ListedOffsets{"events": {0: {Offset: 23}}}))
			require.True(t, c.Assignments()[tp].RestorePending, "validation alone does not complete the full fetch callback")
			require.False(t, c.ReadinessSnapshot().Observed)
			switch change {
			case "revoke and reacquire":
				c.revoked(t.Context(), nil, map[string][]int32{"events": {0}})
				c.assigned(t.Context(), nil, map[string][]int32{"events": {0}})
			case "generation":
				driver.generation++
				c.assigned(t.Context(), nil, map[string][]int32{})
			case "member":
				c.mu.Lock()
				a := c.assignments[tp]
				a.Member = "different-member"
				c.assignments[tp] = a
				c.mu.Unlock()
			}
			err := c.completeOffsetFetch(expected)
			if change == "completed new group" {
				require.NoError(t, err)
				require.False(t, c.Assignments()[tp].RestorePending)
				require.Nil(t, c.Assignments()[tp].Checkpoint, "validated new group does not invent checkpoint metadata")
				require.Equal(t, int64(12), c.Assignments()[tp].HistoryStart)
				require.True(t, c.ReadinessSnapshot().Observed)
			} else {
				require.Error(t, err)
				require.True(t, c.Assignments()[tp].RestorePending, "stale fetch cannot clear a newer pending owner")
				require.False(t, c.ReadinessSnapshot().Observed)
			}
		})
	}
}

func TestDetectorCooperativeOffsetFetch(t *testing.T) {
	for _, missingNew := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing new pending=%t", missingNew), func(t *testing.T) {
			client := detectorHistoryClient(t)
			rules := &detection.Rules{SchemaVersion: 1, Rules: []detection.Rule{
				{ID: "AUTH_BRUTE_FORCE", Version: 1, Window: 5 * time.Minute},
				{ID: "PORT_SCAN", Version: 1, Window: time.Minute},
				{ID: "REQUEST_FLOOD", Version: 1, Window: time.Minute},
				{ID: "DENY_SPIKE", Version: 1, Window: 2 * time.Minute},
			}}
			checkpoint, err := detection.NewPartitionState(rules).Checkpoint()
			require.NoError(t, err)
			_, err = detection.CheckpointMetadata(checkpoint)
			require.NoError(t, err)
			retained := detection.TopicPartition{Topic: "events", Partition: 0}
			added := detection.TopicPartition{Topic: "events", Partition: 1}
			driver := &readinessDriver{generation: 9}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var fatal error
			c := &DetectorConsumer{client: driver, rules: rules, group: "detector-test", cancel: cancel,
				settings: config.WriterConfig{OperationTimeout: time.Second}, sequence: 7,
				onFatal: func(err error) { fatal = err },
				assignments: map[detection.TopicPartition]DetectorAssignment{
					retained: {Token: 7, Group: "detector-test", Member: "member-7", Generation: 9,
						HistoryStart: 7, Checkpoint: &checkpoint},
				}}
			// Pinned v1.22.1 calls assigned with additions and fetchOffsets(ctx,
			// added), not the entire cooperative assignment. Empty additions do
			// not invoke OnOffsetsFetched at all.
			driver.generation = 10
			additions := []int32{1}
			if missingNew {
				additions = append(additions, 2)
			}
			c.assigned(ctx, nil, map[string][]int32{"events": additions})
			before := c.Assignments()
			require.False(t, before[retained].RestorePending)
			require.True(t, before[added].RestorePending)
			require.Equal(t, uint64(7), before[retained].Token)
			require.Equal(t, int32(10), before[retained].Generation)
			require.Equal(t, "member-7", before[retained].Member)
			require.NotEqual(t, before[retained].Token, before[added].Token)
			partitions := []kmsg.OffsetFetchResponseTopicPartition{{Partition: 1, Offset: -1}}
			// Exercise the actual registered adapter callback, including native
			// kadm metadata/ListOffsets validation through the in-memory wire.
			err = c.fetched(ctx, client, &kmsg.OffsetFetchResponse{Topics: []kmsg.OffsetFetchResponseTopic{
				{Topic: "events", Partitions: partitions},
			}})
			after := c.Assignments()
			require.Equal(t, before[retained], after[retained], "retained state/token/current owner must not be restored again")
			if missingNew {
				require.ErrorContains(t, err, "restore response incomplete")
				require.ErrorIs(t, fatal, err)
				require.True(t, after[added].RestorePending)
				require.True(t, after[detection.TopicPartition{Topic: "events", Partition: 2}].RestorePending)
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				return
			}
			require.Equal(t, int64(12), after[added].HistoryStart, "actual broker history validation completed")
			require.NoError(t, err, "cooperative fetch response legitimately omits the retained completed partition")
			require.NoError(t, fatal)
			require.False(t, after[added].RestorePending)
			require.Equal(t, int64(12), after[added].HistoryStart)
			require.Nil(t, after[added].Checkpoint, "validated absent broker offset remains a legitimate new group")
			require.Equal(t, before[added].Token, after[added].Token)
			require.True(t, c.ReadinessSnapshot().Observed)
		})
	}
}

func TestDetectorCooperativeFetchValidation(t *testing.T) {
	for _, scenario := range []string{"duplicate", "foreign partition", "foreign topic", "retained", "partition error"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c := &DetectorConsumer{client: commitDriver{}, cancel: cancel,
				settings: config.WriterConfig{OperationTimeout: time.Second},
				assignments: map[detection.TopicPartition]DetectorAssignment{
					{Topic: "events", Partition: 0}: {Token: 7, HistoryStart: 7},
					{Topic: "events", Partition: 1}: {Token: 8, RestorePending: true},
				}}
			before := c.Assignments()
			partitions := []kmsg.OffsetFetchResponseTopicPartition{{Partition: 1, Offset: -1}}
			extra := kmsg.OffsetFetchResponseTopicPartition{Partition: 1, Offset: -1}
			topic := "events"
			switch scenario {
			case "foreign partition":
				extra.Partition = 2
			case "foreign topic":
				topic = "foreign"
			case "retained":
				extra.Partition = 0
			case "partition error":
				extra.ErrorCode = 29
			}
			response := &kmsg.OffsetFetchResponse{Topics: []kmsg.OffsetFetchResponseTopic{
				{Topic: "events", Partitions: partitions},
				{Topic: topic, Partitions: []kmsg.OffsetFetchResponseTopicPartition{extra}},
			}}
			err := c.fetched(ctx, detectorHistoryClient(t), response)
			require.Error(t, err)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, before, c.Assignments(),
				"invalid response must not partially restore any pending or retained state")
		})
	}
}

// Supply only the broker history reads made by the real fetched callback. No
// adapter restore/completion function is replaced and no external broker is used.
func detectorHistoryClient(t *testing.T) *kgo.Client {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers("history:9092"), kgo.MaxVersions(kversion.V0_10_1()),
		kgo.Dialer(func(context.Context, string, string) (net.Conn, error) {
			local, remote := net.Pipe()
			go func() {
				defer remote.Close()
				_ = serveDetectorHistory(remote)
			}()
			return local, nil
		}))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func serveDetectorHistory(conn net.Conn) error {
	for {
		var size [4]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return err
		}
		length := binary.BigEndian.Uint32(size[:])
		if length < 10 || length > 1<<20 {
			return errors.New("invalid fixture request size")
		}
		raw := make([]byte, length)
		if _, err := io.ReadFull(conn, raw); err != nil {
			return err
		}
		request := kmsg.RequestForKey(int16(binary.BigEndian.Uint16(raw)))
		request.SetVersion(int16(binary.BigEndian.Uint16(raw[2:])))
		header := 10 + int(int16(binary.BigEndian.Uint16(raw[8:])))
		if header < 10 || header > len(raw) {
			return errors.New("invalid fixture request header")
		}
		if err := request.ReadFrom(raw[header:]); err != nil {
			return err
		}
		response, err := detectorHistoryResponse(request)
		if err != nil {
			return err
		}
		response.SetVersion(request.GetVersion())
		wire := response.AppendTo(append([]byte(nil), raw[4:8]...))
		binary.BigEndian.PutUint32(size[:], uint32(len(wire)))
		if _, err = conn.Write(append(size[:], wire...)); err != nil {
			return err
		}
	}
}

func detectorHistoryResponse(request kmsg.Request) (kmsg.Response, error) {
	switch req := request.(type) {
	case *kmsg.ApiVersionsRequest:
		return &kmsg.ApiVersionsResponse{ApiKeys: []kmsg.ApiVersionsResponseApiKey{
			{ApiKey: 2, MaxVersion: 1}, {ApiKey: 3}, {ApiKey: 18},
		}}, nil
	case *kmsg.MetadataRequest:
		topic := "events"
		return &kmsg.MetadataResponse{Brokers: []kmsg.MetadataResponseBroker{{NodeID: 1, Host: "history", Port: 9092}},
			Topics: []kmsg.MetadataResponseTopic{{Topic: &topic, Partitions: []kmsg.MetadataResponseTopicPartition{
				{Partition: 0, Leader: 1, Replicas: []int32{1}, ISR: []int32{1}},
				{Partition: 1, Leader: 1, Replicas: []int32{1}, ISR: []int32{1}},
				{Partition: 2, Leader: 1, Replicas: []int32{1}, ISR: []int32{1}},
			}}}}, nil
	case *kmsg.ListOffsetsRequest:
		listed := &kmsg.ListOffsetsResponse{}
		for _, topic := range req.Topics {
			out := kmsg.ListOffsetsResponseTopic{Topic: topic.Topic}
			for _, partition := range topic.Partitions {
				offset := int64(12)
				if partition.Timestamp == -1 {
					offset = 23
				}
				out.Partitions = append(out.Partitions, kmsg.ListOffsetsResponseTopicPartition{
					Partition: partition.Partition, Offset: offset, OldStyleOffsets: []int64{offset}})
			}
			listed.Topics = append(listed.Topics, out)
		}
		return listed, nil
	default:
		return nil, fmt.Errorf("unexpected fixture request %T", request)
	}
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

type cancellationPollDriver struct {
	commitDriver
	pollErr error
}

func (d cancellationPollDriver) PollRecords(ctx context.Context, _ int) kgo.Fetches {
	if d.pollErr != nil {
		return kgo.NewErrFetch(d.pollErr)
	}
	<-ctx.Done()
	return kgo.NewErrFetch(ctx.Err())
}

func TestDetectorPollCancellation(t *testing.T) {
	for _, scenario := range []string{
		"sibling lifetime", "caller cancellation", "terminal restore", "independent transport",
	} {
		t.Run(scenario, func(t *testing.T) {
			lifetime, cancelLifetime := context.WithCancel(t.Context())
			defer cancelLifetime()
			caller, cancelCaller := context.WithCancel(t.Context())
			defer cancelCaller()
			driver := cancellationPollDriver{}
			c := &DetectorConsumer{lifetime: lifetime, settings: config.WriterConfig{OperationTimeout: 10 * time.Millisecond}}
			want := error(nil)
			switch scenario {
			case "sibling lifetime":
				// Service cancellation can reach the lifetime sibling before caller.
				cancelLifetime()
			case "caller cancellation":
				cancelCaller()
				want = context.Canceled
			case "terminal restore":
				c.fatal = errors.New("authoritative restore failure")
				cancelLifetime()
				want = c.fatal
			case "independent transport":
				driver.pollErr = context.Canceled
				want = context.Canceled
			}
			c.client = driver
			started := time.Now()
			records, err := c.PollRecords(caller, 1)
			require.Less(t, time.Since(started), time.Second, "stop/fatal polling keeps its existing finite ceiling")
			require.Empty(t, records)
			if want == nil {
				require.NoError(t, err, "sibling lifetime must not fabricate caller cancellation")
			} else {
				require.ErrorIs(t, err, want)
			}
		})
	}
}
