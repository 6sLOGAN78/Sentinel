package kafkax

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

// DetectorAssignment couples restored metadata to one ownership incarnation.
type DetectorAssignment struct {
	Token             uint64
	Checkpoint        *detection.Checkpoint
	HistoryStart      int64
	RestorePending    bool
	TransitionAllowed bool
	Group, Member     string
	Generation        int32
}

// DetectorAssignmentObserver observes bounded ownership/checkpoint transitions.
// The worker supplies a production no-op or integration-only private observer.
type DetectorAssignmentObserver func(context.Context, string, detection.TopicPartition, DetectorAssignment) error

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
	client                detectorClient
	rules                 *detection.Rules
	settings              config.WriterConfig
	mu                    sync.Mutex
	assignments           map[detection.TopicPartition]DetectorAssignment
	sequence              uint64
	group                 string
	allowConfigTransition bool
	transitionVerified    bool
	fatal                 error
	onFatal               func(error)
	lifetime              context.Context
	cancel                context.CancelFunc
	closeOnce             sync.Once
	closeDone             chan struct{}
	assignmentObserved    bool
	assignmentMember      string
	assignmentGeneration  int32
	observer              DetectorAssignmentObserver
}

// DetectorReadiness identifies one callback-observed group assignment.
type DetectorReadiness struct {
	Member     string
	Generation int32
	Observed   bool
	Tokens     map[detection.TopicPartition]uint64
}

// ReadinessSnapshot is a bounded snapshot; it performs no network operation.
func (c *DetectorConsumer) ReadinessSnapshot() DetectorReadiness {
	member, generation := c.client.GroupMetadata()
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot := DetectorReadiness{Member: member, Generation: generation,
		Observed: c.assignmentObserved && c.fatal == nil && member != "" && generation >= 0 &&
			member == c.assignmentMember && generation == c.assignmentGeneration,
		Tokens: make(map[detection.TopicPartition]uint64, len(c.assignments))}
	for tp, assignment := range c.assignments {
		snapshot.Tokens[tp] = assignment.Token
		if assignment.RestorePending {
			snapshot.Observed = false
		}
	}
	return snapshot
}

// EnableConfigTransition opts into a stopped-old-replica handover at startup.
// A changed checkpoint still requires actual sole Kafka membership and PG proof.
func (c *DetectorConsumer) EnableConfigTransition(allowed bool) {
	c.allowConfigTransition = allowed
}

// VerifyStoppedDetectorGroup checks the deployment boundary before constructing
// a consuming group member. An admin-only client cannot trigger a rebalance or
// make an old owner leave while admission is being checked.
func VerifyStoppedDetectorGroup(ctx context.Context, kafka config.KafkaConfig) error {
	brokers, err := config.ParseBrokers(kafka.Brokers)
	if err != nil {
		return err
	}
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return errors.New("configuration transition membership client unavailable")
	}
	defer client.Close()
	groups, err := kadm.NewClient(client).DescribeGroups(ctx, kafka.DetectorGroup)
	group, present := groups[kafka.DetectorGroup]
	if errors.Is(group.Err, kerr.GroupIDNotFound) && err == nil {
		return nil
	}
	if err != nil || !present || group.Err != nil {
		return errors.New("configuration transition membership unavailable")
	}
	if len(group.Members) != 0 {
		return errors.New("configuration transition requires exactly one detector member; stop old replicas before startup")
	}
	return nil
}

// NewDetectorConsumer configures owned polling and terminal metadata restoration.
func NewDetectorConsumer(
	ctx context.Context, kafka config.KafkaConfig, settings config.WriterConfig,
	rules *detection.Rules, onFatal func(error),
	observers ...DetectorAssignmentObserver,
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
	if len(observers) > 0 {
		c.observer = observers[0]
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(kafka.EventsTopic), kgo.ConsumerGroup(kafka.DetectorGroup),
		kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(), kgo.ConsumeStartOffset(kgo.NewOffset().AtStart()),
		kgo.ConsumeResetOffset(kgo.NoResetOffset()), kgo.FetchMaxBytes(settings.FetchMaxBytes),
		kgo.FetchMaxPartitionBytes(settings.FetchMaxPartitionBytes), kgo.MaxConcurrentFetches(settings.MaxConcurrentFetches),
		kgo.FetchMaxWait(consumerFetchMaxWait), kgo.SessionTimeout(consumerSessionTimeout),
		kgo.RebalanceTimeout(budgets.Rebalance),
		kgo.OnPartitionsAssigned(c.assigned), kgo.OnPartitionsRevoked(c.revoked),
		kgo.OnPartitionsLost(c.lost), kgo.OnOffsetsFetched(c.fetched),
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
	c.assignmentObserved = false
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

func (c *DetectorConsumer) assigned(ctx context.Context, _ *kgo.Client, topics map[string][]int32) {
	member, generation := c.client.GroupMetadata()
	c.mu.Lock()
	c.assignmentMember, c.assignmentGeneration, c.assignmentObserved = member, generation, true
	// Cooperative callbacks contain additions only. Retained partitions keep
	// their lifetime token and state but must use the actual new group fence.
	for tp, assignment := range c.assignments {
		assignment.Member, assignment.Generation = member, generation
		c.assignments[tp] = assignment
	}
	for topic, partitions := range topics {
		for _, id := range partitions {
			c.sequence++
			c.assignments[detection.TopicPartition{Topic: topic, Partition: id}] = DetectorAssignment{
				Token: c.sequence, Group: c.group, Member: member, Generation: generation, RestorePending: true,
			}
		}
	}
	c.mu.Unlock()
	if err := c.observe(ctx, "assigned", c.Assignments()); err != nil {
		_ = c.fail(err)
	}
}

func (c *DetectorConsumer) revoked(ctx context.Context, _ *kgo.Client, topics map[string][]int32) {
	c.releaseAssignments(ctx, "revoked", topics)
}

func (c *DetectorConsumer) lost(ctx context.Context, _ *kgo.Client, topics map[string][]int32) {
	c.releaseAssignments(ctx, "lost", topics)
}

func (c *DetectorConsumer) releaseAssignments(ctx context.Context, boundary string, topics map[string][]int32) {
	released := make(map[detection.TopicPartition]DetectorAssignment)
	c.mu.Lock()
	c.assignmentObserved = false
	for topic, partitions := range topics {
		for _, id := range partitions {
			tp := detection.TopicPartition{Topic: topic, Partition: id}
			if a, ok := c.assignments[tp]; ok {
				released[tp] = a
			}
			delete(c.assignments, tp)
		}
	}
	c.mu.Unlock()
	// Every completed batch checkpoint already precedes AllowRebalance. Neither
	// revoke nor loss can manufacture a newer consumed-next commit.
	if err := c.observe(ctx, boundary, released); err != nil {
		_ = c.fail(err)
	}
}

func (c *DetectorConsumer) observe(ctx context.Context, boundary string,
	assignments map[detection.TopicPartition]DetectorAssignment) error {
	if c.observer == nil {
		return nil
	}
	for tp, assignment := range assignments {
		if err := c.observer(ctx, boundary, tp, assignment); err != nil {
			return err
		}
	}
	return nil
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
	expected := c.Assignments()
	checkCtx, cancel := context.WithTimeout(ctx, c.settings.OperationTimeout)
	defer cancel()
	admin := kadm.NewClient(client)
	topics := make([]string, 0, len(partitions))
	for _, t := range partitions {
		topics = append(topics, t.Topic)
	}
	if len(topics) == 0 && len(c.Assignments()) != 0 {
		return c.fail(errors.New("detector offset response incomplete"))
	}
	if len(topics) == 0 {
		return c.finishOffsetFetch(checkCtx, client, expected)
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
	return c.finishOffsetFetch(checkCtx, client, expected)
}

func (c *DetectorConsumer) finishOffsetFetch(ctx context.Context, client *kgo.Client,
	expected map[detection.TopicPartition]DetectorAssignment) error {
	if err := c.finishConfigRestore(ctx, client); err != nil {
		return err
	}
	if err := c.completeOffsetFetch(expected); err != nil {
		return c.fail(err)
	}
	return nil
}

func (c *DetectorConsumer) completeOffsetFetch(expected map[detection.TopicPartition]DetectorAssignment) error {
	member, generation := c.client.GroupMetadata()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.assignments) != len(expected) {
		return errors.New("detector assignment changed during offset fetch")
	}
	for tp, previous := range expected {
		current, ok := c.assignments[tp]
		if !ok || current.Token != previous.Token || current.Member != previous.Member ||
			current.Generation != previous.Generation || member != previous.Member || generation != previous.Generation {
			return errors.New("detector ownership changed during offset fetch")
		}
	}
	for tp, assignment := range c.assignments {
		assignment.RestorePending = false
		c.assignments[tp] = assignment
	}
	return nil
}

func (c *DetectorConsumer) finishConfigRestore(ctx context.Context, client *kgo.Client) error {
	if err := c.authorizeConfigTransition(ctx, client); err != nil {
		return c.fail(err)
	}
	return nil
}

func (c *DetectorConsumer) authorizeConfigTransition(ctx context.Context, client *kgo.Client) error {
	changed := make(map[detection.TopicPartition]DetectorAssignment)
	for tp, assignment := range c.Assignments() {
		if assignment.Checkpoint == nil {
			continue
		}
		m, err := detection.CheckpointMetadata(*assignment.Checkpoint)
		if err != nil {
			return err
		}
		if m.ConfigFingerprint != c.rules.Fingerprint() {
			changed[tp] = assignment
		}
	}
	if len(changed) == 0 && !c.allowConfigTransition {
		return nil
	}
	groups, err := kadm.NewClient(client).DescribeGroups(ctx, c.group)
	group, present := groups[c.group]
	member, _ := c.client.GroupMetadata()
	if err != nil || !present || group.Err != nil || len(group.Members) != 1 || group.Members[0].MemberID != member {
		return errors.New("configuration transition requires exactly one detector member; stop old replicas")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for tp, assignment := range changed {
		current, owned := c.assignments[tp]
		if !owned || current.Token != assignment.Token {
			return errors.New("configuration transition assignment changed")
		}
		current.TransitionAllowed = true
		c.assignments[tp] = current
	}
	c.transitionVerified = true
	return nil
}

// A member with zero assigned partitions does not necessarily receive the
// offset-fetch hook. Verify the explicit transition deployment after joining
// even in that case, so an old-config peer cannot coexist unnoticed.
func (c *DetectorConsumer) verifyTransitionOnPoll(ctx context.Context) error {
	c.mu.Lock()
	verified := c.transitionVerified
	c.mu.Unlock()
	if !c.allowConfigTransition || verified {
		return nil
	}
	member, generation := c.client.GroupMetadata()
	if member == "" || generation < 0 {
		return nil
	}
	client, ok := c.client.(*kgo.Client)
	if !ok {
		return c.fail(errors.New("configuration transition requires real group membership"))
	}
	checkCtx, cancel := context.WithTimeout(ctx, c.settings.OperationTimeout)
	defer cancel()
	return c.finishConfigRestore(checkCtx, client)
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
	start, okStart := starts.Lookup(tp.Topic, tp.Partition)
	end, okEnd := ends.Lookup(tp.Topic, tp.Partition)
	if !okStart || !okEnd || start.Err != nil || end.Err != nil || start.Offset < 0 || end.Offset < start.Offset {
		return errors.New("detector history bounds unavailable")
	}
	if p.Offset < -1 {
		return errors.New("invalid detector absent offset")
	}
	assignment.HistoryStart = start.Offset
	if p.Offset >= 0 {
		if p.Offset < start.Offset || p.Offset > end.Offset || p.Metadata == nil {
			return errors.New("detector checkpoint history lost")
		}
		cp := detection.Checkpoint{Offset: p.Offset, Metadata: *p.Metadata}
		if err := c.validateConfigCheckpoint(cp); err != nil {
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

func (c *DetectorConsumer) validateConfigCheckpoint(cp detection.Checkpoint) error {
	m, err := detection.CheckpointMetadata(cp)
	if err != nil {
		return err
	}
	if m.ConfigFingerprint == c.rules.Fingerprint() {
		return detection.NewPartitionState(c.rules).Restore(cp)
	}
	if !c.allowConfigTransition || !m.Drained {
		return errors.New("configuration change requires explicit drained restart")
	}
	return nil // Semantic/version authorization follows against the locked PG manifest.
}

// PollRecords returns bounded owned records and propagates terminal hook errors.
func (c *DetectorConsumer) PollRecords(ctx context.Context, limit int) ([]*kgo.Record, error) {
	// Wake periodically to validate and adopt idle assigned checkpoints, even
	// when no source record arrives after the old configuration drained.
	pollCtx, cancel := context.WithTimeout(ctx, min(c.settings.OperationTimeout, time.Second))
	// The caller owns cancellation. A sibling lifetime can observe service
	// cancellation first; linking it here fabricates a poll error before the
	// worker's own context is canceled. Fatal restore still wins below, and
	// this existing one-second deadline bounds idle/closed lifetime checks.
	defer cancel()
	fetches := c.client.PollRecords(pollCtx, limit)
	c.mu.Lock()
	fatal := c.fatal
	c.mu.Unlock()
	if fatal != nil {
		return nil, fatal
	}
	if err := c.verifyTransitionOnPoll(ctx); err != nil {
		return nil, err
	}
	if errors.Is(pollCtx.Err(), context.DeadlineExceeded) && fetches.NumRecords() == 0 {
		return nil, nil
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
		if assignment, ok := assigned[tp]; !ok || assignment.RestorePending {
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
	result := errors.New("detector commit callback missing")
	c.client.CommitOffsetsSync(commitCtx, offsets, func(
		_ *kgo.Client, _ *kmsg.OffsetCommitRequest, response *kmsg.OffsetCommitResponse, err error,
	) {
		result = err
		if err != nil {
			return
		}
		result = checkCommitResponse(response, checkpoints)
	})
	if result != nil {
		return result
	}
	// Callback completion is authoritative. Do not overwrite a later ownership
	// incarnation if it changed while the bounded commit was in flight.
	c.mu.Lock()
	for tp := range checkpoints {
		current, ok := c.assignments[tp]
		if !ok || current.Token != assigned[tp].Token {
			c.mu.Unlock()
			return errors.New("detector commit assignment changed")
		}
	}
	committed := make(map[detection.TopicPartition]DetectorAssignment, len(checkpoints))
	for tp, cp := range checkpoints {
		a := c.assignments[tp]
		a.Checkpoint = &cp
		c.assignments[tp] = a
		committed[tp] = a
	}
	c.mu.Unlock()
	return c.observe(ctx, "committed", committed)
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
