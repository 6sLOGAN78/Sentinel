package kafkax

import (
	"context"
	"errors"
	"sync"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

// DetectorAssignment couples restoration to one ownership incarnation.
type DetectorAssignment struct {
	Token      uint64
	Checkpoint *detection.Checkpoint
}

type detectorClient interface {
	CommitOffsetsSync(context.Context, map[string]map[int32]kgo.EpochOffset,
		func(*kgo.Client, *kmsg.OffsetCommitRequest, *kmsg.OffsetCommitResponse, error))
	PollRecords(context.Context, int) kgo.Fetches
	GroupMetadata() (string, int32)
	AllowRebalance()
	Ping(context.Context) error
	CloseAllowingRebalance()
}

// DetectorConsumer owns metadata-aware state-pin commits.
type DetectorConsumer struct {
	client      detectorClient
	rules       *detection.Rules
	settings    config.WriterConfig
	mu          sync.Mutex
	assignments map[detection.TopicPartition]DetectorAssignment
	sequence    uint64
	fatal       error
	onFatal     func(error)
	lifetime    context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	closeDone   chan struct{}
}

// CommitCheckpoints atomically commits pins and their metadata.
func (*DetectorConsumer) CommitCheckpoints(context.Context, map[detection.TopicPartition]detection.Checkpoint) error {
	return errors.New("detector adapter not extracted")
}

func (*DetectorConsumer) fetched(context.Context, *kgo.Client, *kmsg.OffsetFetchResponse) error {
	return errors.New("detector adapter not extracted")
}

func (*DetectorConsumer) restorePartition(
	detection.TopicPartition, kmsg.OffsetFetchResponseTopicPartition, kadm.ListedOffsets, kadm.ListedOffsets,
) error {
	return errors.New("detector adapter not extracted")
}

func attachMetadata(*kmsg.OffsetCommitRequest, map[detection.TopicPartition]detection.Checkpoint) error {
	return errors.New("detector adapter not extracted")
}
