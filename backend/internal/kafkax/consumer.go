package kafkax

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const (
	consumerSessionTimeout = 45 * time.Second
	consumerFetchMaxWait   = 500 * time.Millisecond
)

// ErrConsumerClosed indicates that polling stopped because Close was called.
var ErrConsumerClosed = errors.New("kafka consumer closed")

// Consumer owns one manually committed group client. BlockRebalanceOnPoll
// keeps every returned record assigned until the writer explicitly releases it.
type Consumer struct {
	client    *kgo.Client
	closeOnce sync.Once
	closeDone chan struct{}
	closeFn   func()
	lag       *LagTracker
	admin     CommittedOffsetSource
}

// ConsumerOption configures optional Consumer collaborators.
type ConsumerOption func(*consumerOptions)

type consumerOptions struct {
	lag *LagTracker
}

// WithLagTracker registers rebalance hooks and commit tracking for t. The
// tracker must target the consumer's group and topic. NewConsumer binds the
// consumer's own kadm client as t's end-offset source when t has none, so no
// second Kafka client is needed; the caller runs t.Run separately.
func WithLagTracker(t *LagTracker) ConsumerOption {
	return func(o *consumerOptions) { o.lag = t }
}

// NewConsumer creates a bounded group consumer with auto-commit disabled.
func NewConsumer(
	_ context.Context, kafka config.KafkaConfig, writer config.WriterConfig, options ...ConsumerOption,
) (*Consumer, error) {
	brokers, err := config.ParseBrokers(kafka.Brokers)
	if err != nil {
		return nil, err
	}
	budgets, err := config.ComputeWriterBudgets(writer)
	if err != nil {
		return nil, err
	}
	var opts consumerOptions
	for _, option := range options {
		option(&opts)
	}
	c := &Consumer{closeDone: make(chan struct{}), lag: opts.lag}
	hooks := []kgo.Opt(nil)
	if c.lag != nil {
		if c.lag.group != kafka.EventWriterGroup || c.lag.topic != kafka.EventsTopic {
			return nil, errors.New("lag tracker group/topic does not match the consumer")
		}
		hooks = append(hooks, kgo.OnPartitionsAssigned(c.onAssigned),
			kgo.OnPartitionsRevoked(c.onRevoked), kgo.OnPartitionsLost(c.onRevoked))
	}

	client, err := kgo.NewClient(append([]kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ConsumeTopics(kafka.EventsTopic),
		kgo.ConsumerGroup(kafka.EventWriterGroup),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.FetchMaxBytes(writer.FetchMaxBytes),
		kgo.FetchMaxPartitionBytes(writer.FetchMaxPartitionBytes),
		kgo.MaxConcurrentFetches(writer.MaxConcurrentFetches),
		kgo.FetchMaxWait(consumerFetchMaxWait),
		kgo.SessionTimeout(consumerSessionTimeout),
		kgo.RebalanceTimeout(budgets.Rebalance),
	}, hooks...)...)
	if err != nil {
		return nil, fmt.Errorf("create Kafka consumer: %w", err)
	}
	c.client, c.closeFn = client, client.CloseAllowingRebalance
	if c.lag != nil {
		admin := kadm.NewClient(client)
		c.admin = admin
		c.lag.bind(admin)
	}

	return c, nil
}

// onAssigned seeds the tracker for newly owned partitions from the group's
// committed offsets (bounded by the tracker's 2 s query timeout).
func (c *Consumer) onAssigned(ctx context.Context, _ *kgo.Client, topics map[string][]int32) {
	if c.lag == nil {
		return
	}
	c.lag.SeedAssigned(ctx, c.admin, topics[c.lag.topic])
}

// onRevoked handles both revoke and loss: the series must disappear from this
// process either way, or sum() across replicas double counts the backlog.
func (c *Consumer) onRevoked(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	if c.lag == nil {
		return
	}
	c.lag.Revoked(topics[c.lag.topic])
}

// recordProcessed advances processed next to max committed offset + 1 per
// partition. Writers commit right after effects succeed, so this is the
// processed position.
func (c *Consumer) recordProcessed(records []*kgo.Record) {
	if c.lag == nil {
		return
	}
	next := make(map[int32]int64)
	for _, record := range records {
		if record == nil || record.Topic != c.lag.topic {
			continue
		}
		if candidate := record.Offset + 1; candidate > next[record.Partition] {
			next[record.Partition] = candidate
		}
	}
	for partition, offset := range next {
		c.lag.Processed(partition, offset)
	}
}

// Ping verifies that the configured Kafka cluster is reachable.
func (c *Consumer) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx); err != nil {
		return fmt.Errorf("ping Kafka: %w", err)
	}

	return nil
}

// PollRecords returns at most maxRecords while preserving franz-go's poll gate.
func (c *Consumer) PollRecords(ctx context.Context, maxRecords int) ([]*kgo.Record, error) {
	fetches := c.client.PollRecords(ctx, maxRecords)
	if fetches.IsClientClosed() {
		return nil, ErrConsumerClosed
	}
	if err := fetches.Err(); err != nil {
		return nil, fmt.Errorf("poll Kafka records: %w", err)
	}

	records := make([]*kgo.Record, 0, fetches.NumRecords())
	fetches.EachRecord(func(record *kgo.Record) { records = append(records, record) })

	return records, nil
}

// CommitRecords synchronously commits each record's next offset.
func (c *Consumer) CommitRecords(ctx context.Context, records ...*kgo.Record) error {
	if err := c.client.CommitRecords(ctx, records...); err != nil {
		return fmt.Errorf("commit Kafka records: %w", err)
	}
	c.recordProcessed(records)

	return nil
}

// AllowRebalance releases all poll gates after the current batch is finished.
func (c *Consumer) AllowRebalance() { c.client.AllowRebalance() }

// Close permits any blocked rebalance before leaving the group and closing,
// while allowing lifecycle shutdown to remain bounded by ctx.
func (c *Consumer) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		go func() {
			defer close(c.closeDone)
			c.closeFn()
		}()
	})

	select {
	case <-c.closeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
