package kafkax

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const (
	consumerSessionTimeout = 45 * time.Second
	consumerFetchMaxWait   = 500 * time.Millisecond
	minRebalanceTimeout    = 15 * time.Second
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
}

// NewConsumer creates a bounded group consumer with auto-commit disabled.
func NewConsumer(_ context.Context, kafka config.KafkaConfig, writer config.WriterConfig) (*Consumer, error) {
	brokers, err := config.ParseBrokers(kafka.Brokers)
	if err != nil {
		return nil, err
	}

	client, err := kgo.NewClient(
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
		kgo.RebalanceTimeout(rebalanceTimeout(writer)),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka consumer: %w", err)
	}

	return &Consumer{
		client:    client,
		closeDone: make(chan struct{}),
		closeFn:   client.CloseAllowingRebalance,
	}, nil
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

// rebalanceTimeout covers the maximum insert attempts and retry waits plus a
// final commit operation. A floor avoids an unrealistically short group join
// deadline when unit-sized development timeouts are configured.
func rebalanceTimeout(settings config.WriterConfig) time.Duration {
	processing := time.Duration(settings.MaxAttempts)*settings.OperationTimeout +
		time.Duration(max(settings.MaxAttempts-1, 0))*settings.RetryMaxBackoff +
		settings.OperationTimeout

	return max(processing, minRebalanceTimeout)
}
