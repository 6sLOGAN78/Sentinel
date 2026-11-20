package kafkax //nolint:testpackage // exercise unexported commit-response and refresh validation.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

// A v10+ OffsetCommit response names topics only by ID when franz-go skipped
// name resolution (stale generation or ended session during a rebalance). The
// real per-partition code must surface instead of a synthetic mismatch.
func TestDetectorCommitResponseTopicIDs(t *testing.T) {
	id := [16]byte{1, 2, 3}
	request := &kmsg.OffsetCommitRequest{Topics: []kmsg.OffsetCommitRequestTopic{{Topic: "events", TopicID: id,
		Partitions: []kmsg.OffsetCommitRequestTopicPartition{{Partition: 0, Offset: 12}}}}}
	checkpoints := map[detection.TopicPartition]detection.Checkpoint{
		{Topic: "events", Partition: 0}: {Offset: 12, Metadata: "m"}}
	byID := func(topic [16]byte, code int16) *kmsg.OffsetCommitResponse {
		return &kmsg.OffsetCommitResponse{Version: 10, Topics: []kmsg.OffsetCommitResponseTopic{{TopicID: topic,
			Partitions: []kmsg.OffsetCommitResponseTopicPartition{{Partition: 0, ErrorCode: code}}}}}
	}
	t.Run("accepted by id", func(t *testing.T) {
		require.NoError(t, checkCommitResponse(request, byID(id, 0), checkpoints))
	})
	for _, code := range []*kerr.Error{kerr.RebalanceInProgress, kerr.IllegalGeneration, kerr.UnknownMemberID} {
		t.Run(code.Message, func(t *testing.T) {
			err := checkCommitResponse(request, byID(id, code.Code), checkpoints)
			require.ErrorIs(t, err, code)
			require.NotContains(t, err.Error(), "mismatch")
		})
	}
	t.Run("unknown id is still a mismatch", func(t *testing.T) {
		require.ErrorContains(t, checkCommitResponse(request, byID([16]byte{9}, 0), checkpoints), "mismatch")
	})
	t.Run("no request cannot resolve ids", func(t *testing.T) {
		require.ErrorContains(t, checkCommitResponse(nil, byID(id, 0), checkpoints), "mismatch")
	})
	t.Run("named v9 response is unchanged", func(t *testing.T) {
		named := &kmsg.OffsetCommitResponse{Topics: []kmsg.OffsetCommitResponseTopic{{Topic: "events",
			Partitions: []kmsg.OffsetCommitResponseTopicPartition{{Partition: 0}}}}}
		require.NoError(t, checkCommitResponse(request, named, checkpoints))
	})
}

type refreshAdmin struct {
	committed   kadm.OffsetResponses
	start, end  int64
	fetchErr    error
	fetchedFrom string
}

func (a *refreshAdmin) FetchOffsets(_ context.Context, group string) (kadm.OffsetResponses, error) {
	a.fetchedFrom = group
	return a.committed, a.fetchErr
}

func (a *refreshAdmin) ListStartOffsets(_ context.Context, topics ...string) (kadm.ListedOffsets, error) {
	return refreshListed(topics[0], a.start), nil
}

func (a *refreshAdmin) ListEndOffsets(_ context.Context, topics ...string) (kadm.ListedOffsets, error) {
	return refreshListed(topics[0], a.end), nil
}

func refreshListed(topic string, offset int64) kadm.ListedOffsets {
	return kadm.ListedOffsets{topic: {0: {Topic: topic, Partition: 0, Offset: offset}}}
}

func refreshRules(t *testing.T) (*detection.Rules, detection.Checkpoint) {
	t.Helper()
	file, err := os.Open("../../../config/rules.yaml")
	require.NoError(t, err)
	defer file.Close()
	rules, err := detection.LoadRules(file)
	require.NoError(t, err)
	state := detection.NewPartitionState(rules)
	require.NoError(t, state.BeginAt(4))
	cp, err := state.Checkpoint()
	require.NoError(t, err)
	return rules, cp
}

// After a restore failed because its assignment moved, the retained partition
// re-reads the broker's committed checkpoint instead of its stale snapshot.
func TestDetectorRefreshCheckpoint(t *testing.T) {
	rules, cp := refreshRules(t)
	tp := detection.TopicPartition{Topic: "events", Partition: 0}
	committed := kadm.OffsetResponses{"events": {0: {Offset: kadm.Offset{Topic: "events", At: cp.Offset,
		Metadata: cp.Metadata}}}}
	consumer := func(admin *refreshAdmin, local *detection.Checkpoint) *DetectorConsumer {
		return &DetectorConsumer{rules: rules, group: "g", admin: admin,
			settings:    config.WriterConfig{OperationTimeout: time.Second},
			assignments: map[detection.TopicPartition]DetectorAssignment{tp: {Token: 3, Checkpoint: local}}}
	}
	t.Run("adopts the broker checkpoint", func(t *testing.T) {
		admin := &refreshAdmin{committed: committed, start: 0, end: 10}
		c := consumer(admin, nil)
		require.NoError(t, c.RefreshCheckpoint(t.Context(), tp, 3))
		require.Equal(t, "g", admin.fetchedFrom)
		require.Equal(t, cp, *c.Assignments()[tp].Checkpoint)
		require.Equal(t, int64(0), c.Assignments()[tp].HistoryStart)
	})
	t.Run("no committed checkpoint clears the snapshot", func(t *testing.T) {
		c := consumer(&refreshAdmin{committed: kadm.OffsetResponses{}, end: 10}, &cp)
		require.NoError(t, c.RefreshCheckpoint(t.Context(), tp, 3))
		require.Nil(t, c.Assignments()[tp].Checkpoint)
	})
	t.Run("moved token refuses", func(t *testing.T) {
		require.Error(t, consumer(&refreshAdmin{committed: committed, end: 10}, nil).RefreshCheckpoint(t.Context(), tp, 4))
	})
	t.Run("fetch failure refuses", func(t *testing.T) {
		admin := &refreshAdmin{fetchErr: errors.New("coordinator down"), end: 10}
		require.Error(t, consumer(admin, nil).RefreshCheckpoint(t.Context(), tp, 3))
	})
	t.Run("expired history fails closed", func(t *testing.T) {
		c := consumer(&refreshAdmin{committed: committed, start: cp.Offset + 1, end: 10}, nil)
		require.ErrorContains(t, c.RefreshCheckpoint(t.Context(), tp, 3), "checkpoint history lost")
		require.Nil(t, c.Assignments()[tp].Checkpoint)
	})
}
