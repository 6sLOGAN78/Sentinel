// Package detector wires bounded Kafka effects to the event-time reducer.
package detector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

const (
	maxAlertsPerRecord  = 4
	stateAdmissionBytes = 4096
	fetchMaxWait        = 500 * time.Millisecond
	sessionTimeout      = 45 * time.Second
)

// Assignment couples restored metadata to one ownership incarnation.
type Assignment struct {
	Token      uint64
	Checkpoint *detection.Checkpoint
}

// Consumer exposes owned batches and atomic recovery checkpoints.
type Consumer interface {
	PollRecords(context.Context, int) ([]*kgo.Record, error)
	CommitCheckpoints(context.Context, map[detection.TopicPartition]detection.Checkpoint) error
	Assignments() map[detection.TopicPartition]Assignment
	AllowRebalance()
}

// Publisher acknowledges every frozen alert before returning success.
type Publisher interface {
	Publish(context.Context, []*contracts.Alert) error
}
type partition struct {
	token uint64
	state *detection.PartitionState
}

// Worker serializes source records and their durable effects.
type Worker struct {
	consumer   Consumer
	publisher  Publisher
	rules      *detection.Rules
	settings   config.WriterConfig
	limits     contracts.Limits
	partitions map[detection.TopicPartition]partition
}

// New constructs a worker from explicit dependencies and startup settings.
func New(
	consumer Consumer, publisher Publisher, rules *detection.Rules,
	settings config.WriterConfig, limits contracts.Limits,
) *Worker {
	return &Worker{
		consumer: consumer, publisher: publisher, rules: rules, settings: settings, limits: limits,
		partitions: make(map[detection.TopicPartition]partition),
	}
}

// Run processes one bounded owned batch at a time until cancellation or failure.
func (w *Worker) Run(ctx context.Context) error {
	budgets, err := config.ComputeWriterBudgets(w.settings)
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		records, pollErr := w.consumer.PollRecords(ctx, w.settings.BatchSize)
		if ctx.Err() != nil {
			w.consumer.AllowRebalance()
			//nolint:nilerr // parent cancellation is normal shutdown, not a poll failure
			return nil
		}
		if pollErr != nil {
			w.consumer.AllowRebalance()
			return pollErr
		}
		batchCtx, cancel := context.WithTimeout(ctx, budgets.Processing)
		err = w.process(batchCtx, records)
		cancel()
		w.consumer.AllowRebalance()
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) process(ctx context.Context, records []*kgo.Record) error {
	assigned := w.consumer.Assignments()
	for tp := range w.partitions {
		if _, ok := assigned[tp]; !ok {
			delete(w.partitions, tp)
		}
	}
	checkpoints := make(map[detection.TopicPartition]detection.Checkpoint)
	for _, record := range records {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		tp := detection.TopicPartition{Topic: record.Topic, Partition: record.Partition}
		state, err := w.partitionState(tp, assigned)
		if err != nil {
			return err
		}
		if err = w.admit(); err != nil {
			return err
		}
		if err = w.applyRecord(ctx, state, record); err != nil {
			return err
		}
		cp, err := state.Checkpoint()
		if err != nil {
			return err
		}
		checkpoints[tp] = cp
	}
	if len(checkpoints) == 0 {
		return nil
	}
	if err := w.consumer.CommitCheckpoints(ctx, checkpoints); err != nil {
		return err
	}
	for tp, cp := range checkpoints {
		w.partitions[tp].state.Committed(cp)
	}
	return nil
}

func (w *Worker) partitionState(
	tp detection.TopicPartition, assigned map[detection.TopicPartition]Assignment,
) (*detection.PartitionState, error) {
	a, owned := assigned[tp]
	if !owned {
		return nil, errors.New("detector partition ownership lost")
	}
	p, ok := w.partitions[tp]
	if !ok || p.token != a.Token {
		p = partition{token: a.Token, state: detection.NewPartitionState(w.rules)}
		if a.Checkpoint != nil {
			if err := p.state.Restore(*a.Checkpoint); err != nil {
				return nil, err
			}
		}
		w.partitions[tp] = p
	}
	return p.state, nil
}

func (w *Worker) admit() error {
	windows, ledger, total := 0, 0, 0
	for _, part := range w.partitions {
		nw, nl, nb := part.state.Usage()
		windows += nw
		ledger += nl
		total += nb
	}
	if windows >= detection.MaxWindows || ledger >= detection.MaxLedgerEntries ||
		total > detection.MaxStateBytes-stateAdmissionBytes {
		return errors.New("detector state admission exhausted")
	}
	return nil
}

func (w *Worker) applyRecord(ctx context.Context, state *detection.PartitionState, record *kgo.Record) error {
	event, err := contracts.DecodeEvent(bytes.NewReader(record.Value))
	if err != nil {
		return errors.New("detector invalid source record")
	}
	if event.ReceivedAt == nil || event.ReceivedAt.IsZero() || event.Validate(event.OccurredAt.Time, w.limits) != nil {
		return errors.New("detector invalid source event")
	}
	key := event.TenantID + "|"
	if event.SourceIP != nil {
		key += *event.SourceIP
	}
	if string(record.Key) != key {
		return errors.New("detector source key mismatch")
	}
	outcome, err := state.Apply(record.Offset, event)
	if err != nil {
		return err
	}
	if len(outcome.Alerts) > 0 {
		if err = w.publisher.Publish(ctx, outcome.Alerts); err != nil {
			return err
		}
	}
	return state.Acknowledge(record.Offset)
}

// AlertProducer owns a separate bounded all-ISR client. Its readiness check
// reuses the established topic-leader check, including after broker outages.
type AlertProducer struct {
	client    *kgo.Client
	readiness *kafkax.Producer
	topic     string
	timeout   time.Duration
}

// NewAlertProducer creates the bounded canonical alert publisher.
func NewAlertProducer(ctx context.Context, cfg config.KafkaConfig) (*AlertProducer, error) {
	brokers, err := config.ParseBrokers(cfg.Brokers)
	if err != nil {
		return nil, err
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...), kgo.DefaultProduceTopic(cfg.AlertsTopic), kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.AllowIdempotentProduceCancellation(), kgo.RecordDeliveryTimeout(cfg.ProduceTimeout),
		kgo.RecordRetries(cfg.RecordRetries), kgo.MaxBufferedRecords(cfg.MaxBufferedRecords),
		kgo.MaxBufferedBytes(cfg.MaxBufferedBytes),
	)
	if err != nil {
		return nil, err
	}
	probeCfg := cfg
	probeCfg.EventsTopic = cfg.AlertsTopic
	probe, err := kafkax.NewProducer(ctx, probeCfg)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &AlertProducer{client: client, readiness: probe, topic: cfg.AlertsTopic, timeout: cfg.ProduceTimeout}, nil
}

// Publish waits for all-ISR acknowledgement of canonical tenant-keyed alerts.
func (p *AlertProducer) Publish(ctx context.Context, alerts []*contracts.Alert) error {
	if len(alerts) > maxAlertsPerRecord {
		return errors.New("alert batch exceeds bounds")
	}
	records := make([]*kgo.Record, 0, len(alerts))
	for _, alert := range alerts {
		if err := alert.Validate(); err != nil {
			return errors.New("invalid outgoing alert")
		}
		b, err := json.Marshal(alert)
		if err != nil {
			return err
		}
		if len(b) > 64<<10 {
			return errors.New("alert exceeds size bound")
		}
		records = append(records, &kgo.Record{Topic: p.topic, Key: []byte(alert.TenantID + "|" + alert.AlertID), Value: b})
	}
	publishCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if err := p.client.ProduceSync(publishCtx, records...).FirstErr(); err != nil {
		return errors.New("publish detector alerts failed")
	}
	return nil
}

// Ping verifies broker and alert-topic leaders under the caller's deadline.
func (p *AlertProducer) Ping(ctx context.Context) error { return p.readiness.Ping(ctx) }

// Close flushes pending effects and closes both clients.
func (p *AlertProducer) Close(ctx context.Context) error {
	err := p.client.Flush(ctx)
	p.client.Close()
	return errors.Join(err, p.readiness.Close(ctx))
}

// KafkaConsumer keeps callback state under a mutex; the reducer stays in the
// worker. Assignment tokens distinguish identical partitions after a rebalance.
type KafkaConsumer struct {
	client      *kgo.Client
	rules       *detection.Rules
	settings    config.WriterConfig
	mu          sync.Mutex
	assignments map[detection.TopicPartition]Assignment
	sequence    uint64
	fatal       error
	onFatal     func(error)
	lifetime    context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	closeDone   chan struct{}
}

// NewKafkaConsumer configures owned polling and terminal metadata restoration.
func NewKafkaConsumer(
	ctx context.Context, kafka config.KafkaConfig, settings config.WriterConfig,
	rules *detection.Rules, onFatal func(error),
) (*KafkaConsumer, error) {
	brokers, err := config.ParseBrokers(kafka.Brokers)
	if err != nil {
		return nil, err
	}
	budgets, err := config.ComputeWriterBudgets(settings)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(ctx)
	c := &KafkaConsumer{
		rules: rules, settings: settings, assignments: make(map[detection.TopicPartition]Assignment),
		onFatal: onFatal, lifetime: lifetime, cancel: cancel, closeDone: make(chan struct{}),
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(kafka.EventsTopic), kgo.ConsumerGroup(kafka.DetectorGroup),
		kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(), kgo.ConsumeStartOffset(kgo.NewOffset().AtStart()),
		kgo.ConsumeResetOffset(kgo.NoResetOffset()), kgo.FetchMaxBytes(settings.FetchMaxBytes),
		kgo.FetchMaxPartitionBytes(settings.FetchMaxPartitionBytes), kgo.MaxConcurrentFetches(settings.MaxConcurrentFetches),
		kgo.FetchMaxWait(fetchMaxWait), kgo.SessionTimeout(sessionTimeout), kgo.RebalanceTimeout(budgets.Rebalance),
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

func (c *KafkaConsumer) fail(err error) error {
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

func (c *KafkaConsumer) assigned(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for topic, partitions := range topics {
		for _, id := range partitions {
			c.sequence++
			c.assignments[detection.TopicPartition{Topic: topic, Partition: id}] = Assignment{Token: c.sequence}
		}
	}
}

func (c *KafkaConsumer) revoked(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for topic, partitions := range topics {
		for _, id := range partitions {
			delete(c.assignments, detection.TopicPartition{Topic: topic, Partition: id})
		}
	}
}

// Assignments returns an ownership snapshot, including restored checkpoints.
func (c *KafkaConsumer) Assignments() map[detection.TopicPartition]Assignment {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[detection.TopicPartition]Assignment, len(c.assignments))
	for tp, a := range c.assignments {
		out[tp] = a
	}
	return out
}

func (c *KafkaConsumer) fetched(ctx context.Context, client *kgo.Client, response *kmsg.OffsetFetchResponse) error {
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

func (c *KafkaConsumer) restorePartition(
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
func (c *KafkaConsumer) PollRecords(ctx context.Context, limit int) ([]*kgo.Record, error) {
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
func (c *KafkaConsumer) CommitCheckpoints(
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
func (c *KafkaConsumer) AllowRebalance() { c.client.AllowRebalance() }

// Ping reports terminal restoration failures as unhealthy.
func (c *KafkaConsumer) Ping(ctx context.Context) error {
	c.mu.Lock()
	fatal := c.fatal
	c.mu.Unlock()
	if fatal != nil {
		return fatal
	}
	return c.client.Ping(ctx)
}

// Close cancels polling and leaves the group under a bounded caller deadline.
func (c *KafkaConsumer) Close(ctx context.Context) error {
	c.closeOnce.Do(func() { c.cancel(); go func() { defer close(c.closeDone); c.client.CloseAllowingRebalance() }() })
	select {
	case <-c.closeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
