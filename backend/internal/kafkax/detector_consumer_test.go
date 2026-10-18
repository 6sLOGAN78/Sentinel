package kafkax //nolint:testpackage // exercise real callback validation with a transport driver.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

type commitDriver struct {
	*kgo.Client
	response *kmsg.OffsetCommitResponse
	err      error
	t        *testing.T
	called   *bool
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
	done(nil, nil, d.response, d.err)
}

func commitResponse(partitions ...kmsg.OffsetCommitResponseTopicPartition) *kmsg.OffsetCommitResponse {
	return &kmsg.OffsetCommitResponse{Topics: []kmsg.OffsetCommitResponseTopic{{Topic: "events", Partitions: partitions}}}
}

// Ignoring callback errors or omitting any requested partition must prevent
// success; corrupt fetched metadata must cancel ownership and report terminal failure.
func TestDetectorCommitMetadata(t *testing.T) {
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
			require.Equal(t, int64(7), c.assignments[tp0].Checkpoint.Offset)
			require.Equal(t, "previous successful", c.assignments[tp0].Checkpoint.Metadata)
		})
	}
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
	})
}
