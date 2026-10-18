package kafkax

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

// DetectorAssignment couples restored metadata to one ownership incarnation.
type DetectorAssignment struct {
	Token         uint64
	Checkpoint    *detection.Checkpoint
	Group, Member string
	Generation    int32
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

// DetectorConsumer keeps callback state under a mutex; the reducer stays in the
// worker. DetectorAssignment tokens distinguish identical partitions after a rebalance.
type DetectorConsumer struct {
	client      detectorClient
	rules       *detection.Rules
	settings    config.WriterConfig
	mu          sync.Mutex
	assignments map[detection.TopicPartition]DetectorAssignment
	sequence    uint64
	group       string
	fatal       error
	onFatal     func(error)
	lifetime    context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	closeDone   chan struct{}
}

// NewDetectorConsumer configures owned polling and terminal metadata restoration.
func NewDetectorConsumer(
	ctx context.Context, kafka config.KafkaConfig, settings config.WriterConfig,
	rules *detection.Rules, onFatal func(error),
) (*DetectorConsumer, error) {
	brokers, err := config.ParseBrokers(kafka.Brokers)
	if err != nil {
		return nil, err
	}
	budgets, err := config.ComputeWriterBudgets(settings)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(ctx)
	c := &DetectorConsumer{
		rules: rules, settings: settings, assignments: make(map[detection.TopicPartition]DetectorAssignment),
		onFatal: onFatal, lifetime: lifetime, cancel: cancel, closeDone: make(chan struct{}), group: kafka.DetectorGroup,
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(kafka.EventsTopic), kgo.ConsumerGroup(kafka.DetectorGroup),
		kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(), kgo.ConsumeStartOffset(kgo.NewOffset().AtStart()),
		kgo.ConsumeResetOffset(kgo.NoResetOffset()), kgo.FetchMaxBytes(settings.FetchMaxBytes),
		kgo.FetchMaxPartitionBytes(settings.FetchMaxPartitionBytes), kgo.MaxConcurrentFetches(settings.MaxConcurrentFetches),
		kgo.FetchMaxWait(consumerFetchMaxWait), kgo.SessionTimeout(consumerSessionTimeout),
		kgo.RebalanceTimeout(budgets.Rebalance),
		kgo.OnPartitionsAssigned(c.assigned), kgo.OnPartitionsRevoked(c.revoked),
		kgo.OnPartitionsLost(c.revoked), kgo.OnOffsetsFetched(c.fetched),
	)
	if err != nil {
		cancel()
		return nil, err
	}
	c.client = client
	return c, nil
}

func (c *DetectorConsumer) fail(err error) error {
	c.mu.Lock()
	first := c.fatal == nil
	if first {
		c.fatal = err
	}
	c.mu.Unlock()
	if first {
		c.cancel()
		if c.onFatal != nil {
			c.onFatal(err)
		}
	}
	return err
}

func (c *DetectorConsumer) assigned(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	member, generation := c.client.GroupMetadata()
	c.mu.Lock()
	defer c.mu.Unlock()
	for topic, partitions := range topics {
		for _, id := range partitions {
			c.sequence++
			c.assignments[detection.TopicPartition{Topic: topic, Partition: id}] = DetectorAssignment{
				Token: c.sequence, Group: c.group, Member: member, Generation: generation,
			}
		}
	}
}

func (c *DetectorConsumer) revoked(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for topic, partitions := range topics {
		for _, id := range partitions {
			delete(c.assignments, detection.TopicPartition{Topic: topic, Partition: id})
		}
	}
}

// Assignments returns an ownership snapshot, including restored checkpoints.
func (c *DetectorConsumer) Assignments() map[detection.TopicPartition]DetectorAssignment {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[detection.TopicPartition]DetectorAssignment, len(c.assignments))
	for tp, a := range c.assignments {
		out[tp] = a
	}
	return out
}

func (c *DetectorConsumer) fetched(ctx context.Context, client *kgo.Client, response *kmsg.OffsetFetchResponse) error {
	partitions, err := fetchedTopics(response)
	if err != nil {
		return c.fail(err)
	}
	checkCtx, cancel := context.WithTimeout(ctx, c.settings.OperationTimeout)
	defer cancel()
	admin := kadm.NewClient(client)
	topics := make([]string, 0, len(partitions))
	for _, t := range partitions {
		topics = append(topics, t.Topic)
	}
	if len(topics) == 0 {
		return c.fail(errors.New("detector offset response incomplete"))
	}
	starts, err := admin.ListStartOffsets(checkCtx, topics...)
	if err != nil {
		return c.fail(errors.New("detector retained history unavailable"))
	}
	ends, err := admin.ListEndOffsets(checkCtx, topics...)
	if err != nil {
		return c.fail(errors.New("detector history end unavailable"))
	}
	seen := make(map[detection.TopicPartition]bool)
	for _, topic := range partitions {
		for _, p := range topic.Partitions {
			tp := detection.TopicPartition{Topic: topic.Topic, Partition: p.Partition}
			if p.ErrorCode != 0 || seen[tp] {
				return c.fail(errors.New("detector partition offset fetch failed"))
			}
			seen[tp] = true
			if err = c.restorePartition(tp, p, starts, ends); err != nil {
				return c.fail(err)
			}
		}
	}
	for tp := range c.Assignments() {
		if !seen[tp] {
			return c.fail(errors.New("detector restore response incomplete"))
		}
	}
	return nil
}

func fetchedTopics(response *kmsg.OffsetFetchResponse) ([]kmsg.OffsetFetchResponseTopic, error) {
	if response == nil || response.ErrorCode != 0 {
		return nil, errors.New("detector offset fetch failed")
	}
	if len(response.Groups) == 0 {
		return response.Topics, nil
	}
	var partitions []kmsg.OffsetFetchResponseTopic
	for _, group := range response.Groups {
		if group.ErrorCode != 0 {
			return nil, errors.New("detector group offset fetch failed")
		}
		for _, topic := range group.Topics {
			t := kmsg.OffsetFetchResponseTopic{Topic: topic.Topic}
			for _, p := range topic.Partitions {
				t.Partitions = append(t.Partitions, kmsg.OffsetFetchResponseTopicPartition{
					Partition: p.Partition, Offset: p.Offset, LeaderEpoch: p.LeaderEpoch,
					Metadata: p.Metadata, ErrorCode: p.ErrorCode,
				})
			}
			partitions = append(partitions, t)
		}
	}
	return partitions, nil
}

func (c *DetectorConsumer) restorePartition(
	tp detection.TopicPartition, p kmsg.OffsetFetchResponseTopicPartition, starts, ends kadm.ListedOffsets,
) error {
	assignment, owned := c.Assignments()[tp]
	if !owned {
		return errors.New("detector restore ownership lost")
	}
	if p.Offset >= 0 {
		start, okStart := starts.Lookup(tp.Topic, tp.Partition)
		end, okEnd := ends.Lookup(tp.Topic, tp.Partition)
		if !okStart || !okEnd || start.Err != nil || end.Err != nil ||
			p.Offset < start.Offset || p.Offset > end.Offset || p.Metadata == nil {
			return errors.New("detector checkpoint history lost")
		}
		cp := detection.Checkpoint{Offset: p.Offset, Metadata: *p.Metadata}
		if err := detection.NewPartitionState(c.rules).Restore(cp); err != nil {
			return err
		}
		assignment.Checkpoint = &cp
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.assignments[tp]
	if !ok || current.Token != assignment.Token {
		return errors.New("detector assignment changed during restore")
	}
	c.assignments[tp] = assignment
	return nil
}

// PollRecords returns bounded owned records and propagates terminal hook errors.
func (c *DetectorConsumer) PollRecords(ctx context.Context, limit int) ([]*kgo.Record, error) {
	pollCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.lifetime, cancel)
	defer stop()
	defer cancel()
	fetches := c.client.PollRecords(pollCtx, limit)
	c.mu.Lock()
	fatal := c.fatal
	c.mu.Unlock()
	if fatal != nil {
		return nil, fatal
	}
	if err := fetches.Err(); err != nil {
		return nil, fmt.Errorf("detector poll failed: %w", err)
	}
	return fetches.Records(), nil
}

// CommitCheckpoints atomically commits resume offsets and bounded metadata.
func (c *DetectorConsumer) CommitCheckpoints(
	ctx context.Context, checkpoints map[detection.TopicPartition]detection.Checkpoint,
) error {
	assigned := c.Assignments()
	offsets := make(map[string]map[int32]kgo.EpochOffset)
	for tp, cp := range checkpoints {
		if _, ok := assigned[tp]; !ok {
			return errors.New("detector commit ownership lost")
		}
		if cp.Offset < 0 || len(cp.Metadata) == 0 || len(cp.Metadata) > detection.MaxMetadataBytes {
			return errors.New("invalid detector commit")
		}
		if offsets[tp.Topic] == nil {
			offsets[tp.Topic] = make(map[int32]kgo.EpochOffset)
		}
		offsets[tp.Topic][tp.Partition] = kgo.EpochOffset{Epoch: -1, Offset: cp.Offset}
	}
	commitCtx, cancel := context.WithTimeout(ctx, c.settings.OperationTimeout)
	defer cancel()
	commitCtx = kgo.PreCommitFnContext(commitCtx, func(req *kmsg.OffsetCommitRequest) error {
		return attachMetadata(req, checkpoints)
	})
	var result error
	c.client.CommitOffsetsSync(commitCtx, offsets, func(
		_ *kgo.Client, _ *kmsg.OffsetCommitRequest, response *kmsg.OffsetCommitResponse, err error,
	) {
		result = err
		if err != nil {
			return
		}
		result = checkCommitResponse(response, checkpoints)
	})
	return result
}

func attachMetadata(
	req *kmsg.OffsetCommitRequest, checkpoints map[detection.TopicPartition]detection.Checkpoint,
) error {
	seen := make(map[detection.TopicPartition]bool)
	for ti := range req.Topics {
		t := &req.Topics[ti]
		for pi := range t.Partitions {
			p := &t.Partitions[pi]
			tp := detection.TopicPartition{Topic: t.Topic, Partition: p.Partition}
			cp, ok := checkpoints[tp]
			if !ok || seen[tp] || p.Offset != cp.Offset {
				return errors.New("detector commit request mismatch")
			}
			p.Metadata = kmsg.StringPtr(cp.Metadata)
			seen[tp] = true
		}
	}
	if len(seen) != len(checkpoints) {
		return errors.New("detector commit request incomplete")
	}
	return nil
}

func checkCommitResponse(
	response *kmsg.OffsetCommitResponse, checkpoints map[detection.TopicPartition]detection.Checkpoint,
) error {
	if response == nil {
		return errors.New("detector commit response missing")
	}
	var result error
	seen := make(map[detection.TopicPartition]bool)
	for _, topic := range response.Topics {
		for _, p := range topic.Partitions {
			tp := detection.TopicPartition{Topic: topic.Topic, Partition: p.Partition}
			if _, ok := checkpoints[tp]; !ok || seen[tp] {
				result = errors.Join(result, errors.New("detector commit response mismatch"))
			}
			seen[tp] = true
			result = errors.Join(result, kerr.ErrorForCode(p.ErrorCode))
		}
	}
	if len(seen) != len(checkpoints) {
		result = errors.Join(result, errors.New("detector commit response incomplete"))
	}
	return result
}

// AllowRebalance releases the batch ownership gate after all effects finish.
func (c *DetectorConsumer) AllowRebalance() { c.client.AllowRebalance() }

// Ping reports terminal restoration failures as unhealthy.
func (c *DetectorConsumer) Ping(ctx context.Context) error {
	c.mu.Lock()
	fatal := c.fatal
	c.mu.Unlock()
	if fatal != nil {
		return fatal
	}
	return c.client.Ping(ctx)
}

// Close cancels polling and leaves the group under a bounded caller deadline.
func (c *DetectorConsumer) Close(ctx context.Context) error {
	c.closeOnce.Do(func() { c.cancel(); go func() { defer close(c.closeDone); c.client.CloseAllowingRebalance() }() })
	select {
	case <-c.closeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GroupMetadata exposes coordinator membership and generation for ownership fencing.
func (c *DetectorConsumer) GroupMetadata() (string, int32) { return c.client.GroupMetadata() }
