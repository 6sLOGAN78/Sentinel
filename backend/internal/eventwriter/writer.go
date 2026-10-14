// Package eventwriter implements bounded commit-after-insert event persistence.
package eventwriter

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

const backoffMultiplier = 2

// Consumer is the manual offset lifecycle required by the writer.
type Consumer interface {
	PollRecords(context.Context, int) ([]*kgo.Record, error)
	CommitRecords(context.Context, ...*kgo.Record) error
	AllowRebalance()
}

// EventStore durably inserts canonical events. Each call must prepare a fresh
// native batch so ambiguous failures can be retried safely.
type EventStore interface {
	InsertBatch(context.Context, []*contracts.Event) error
}

// Writer owns one bounded poll/decode/insert/commit loop.
type Writer struct {
	consumer Consumer
	store    EventStore
	settings config.WriterConfig
	limits   contracts.Limits
	log      zerolog.Logger
}

// New creates a writer from explicit dependencies and validated settings.
func New(
	consumer Consumer,
	store EventStore,
	settings config.WriterConfig,
	limits contracts.Limits,
	log zerolog.Logger,
) *Writer {
	return &Writer{consumer: consumer, store: store, settings: settings, limits: limits, log: log}
}

// Run polls no more than one in-memory batch, stops polling during storage
// retries, and commits only after the entire batch is durably inserted.
func (w *Writer) Run(ctx context.Context) error {
	records := make([]*kgo.Record, 0, w.settings.BatchSize)
	var flushAt time.Time

	for {
		if ctx.Err() != nil {
			return w.flushOnShutdown(records)
		}
		if len(records) >= w.settings.BatchSize {
			if err := w.processOwned(ctx, records); err != nil {
				return err
			}
			records = records[:0]
			flushAt = time.Time{}

			continue
		}

		result, err := w.poll(ctx, len(records), flushAt)
		if err != nil {
			return err
		}
		if result.shutdown {
			return w.flushOnShutdown(records)
		}
		if result.flush {
			if err = w.processOwned(ctx, records); err != nil {
				return err
			}
			records = records[:0]
			flushAt = time.Time{}
			continue
		}
		if len(result.records) > 0 && len(records) == 0 {
			flushAt = time.Now().Add(w.settings.FlushInterval)
		}
		records = append(records, result.records...)
	}
}

type pollResult struct {
	records  []*kgo.Record
	flush    bool
	shutdown bool
}

func (w *Writer) poll(ctx context.Context, pending int, flushAt time.Time) (pollResult, error) {
	pollCtx := ctx
	cancel := func() {}
	if pending > 0 {
		pollCtx, cancel = context.WithDeadline(ctx, flushAt)
	}
	defer cancel()

	records, err := w.consumer.PollRecords(pollCtx, w.settings.BatchSize-pending)
	if err != nil {
		return pollError(ctx, pending, err)
	}
	if len(records) > 0 {
		return pollResult{records: records}, nil
	}

	return pollResult{
		flush:    pending > 0 && !time.Now().Before(flushAt),
		shutdown: ctx.Err() != nil,
	}, nil
}

func pollError(ctx context.Context, pending int, err error) (pollResult, error) {
	switch {
	case ctx.Err() != nil, errors.Is(err, kafkax.ErrConsumerClosed):
		// Cancellation and an explicit consumer close are loop-control signals.
		//nolint:nilerr // translated into a bounded shutdown flush by the caller
		return pollResult{shutdown: true}, nil
	case pending > 0 && errors.Is(err, context.DeadlineExceeded):
		// The poll deadline is the wall-clock batch timer, not a consumer failure.
		return pollResult{flush: true}, nil
	default:
		return pollResult{}, err
	}
}

func (w *Writer) processOwned(ctx context.Context, records []*kgo.Record) error {
	err := w.process(ctx, records)
	if err != nil && ctx.Err() != nil {
		return w.flushOnShutdown(records)
	}

	return err
}

func (w *Writer) flushOnShutdown(records []*kgo.Record) error {
	if len(records) == 0 {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), w.settings.ShutdownTimeout)
	defer cancel()

	return w.process(shutdownCtx, records)
}

func (w *Writer) process(ctx context.Context, records []*kgo.Record) error {
	if len(records) == 0 {
		return nil
	}
	defer func() {
		// A parent cancellation transfers the still-owned records to the fresh
		// shutdown attempt. Keep the poll gate held until that attempt completes.
		if ctx.Err() == nil {
			w.consumer.AllowRebalance()
		}
	}()

	events, err := w.decode(records)
	if err != nil {
		return err
	}
	if err = w.insertWithRetry(ctx, events); err != nil {
		return err
	}

	last, err := lastContiguousRecords(records)
	if err != nil {
		return err
	}
	commitCtx, cancel := context.WithTimeout(ctx, w.settings.OperationTimeout)
	defer cancel()
	if err = w.consumer.CommitRecords(commitCtx, last...); err != nil {
		return err
	}

	return nil
}

func (w *Writer) decode(records []*kgo.Record) ([]*contracts.Event, error) {
	events := make([]*contracts.Event, 0, len(records))
	for _, record := range records {
		event, err := contracts.DecodeEvent(bytes.NewReader(record.Value))
		if err != nil {
			return nil, fmt.Errorf("decode persisted event at %s: %w", recordLocation(record), err)
		}
		if event.OccurredAt.IsZero() {
			return nil, fmt.Errorf("validate persisted event at %s: occurred_at is required", recordLocation(record))
		}
		if event.ReceivedAt == nil || event.ReceivedAt.IsZero() {
			return nil, fmt.Errorf("validate persisted event at %s: received_at is required", recordLocation(record))
		}
		// Ingest already checked wall-clock skew. Replay validates the same
		// structure relative to the immutable event timestamp, not today's clock.
		if err = event.Validate(event.OccurredAt.Time, w.limits); err != nil {
			return nil, fmt.Errorf("validate persisted event at %s: %w", recordLocation(record), err)
		}
		events = append(events, event)
	}

	return events, nil
}

func recordLocation(record *kgo.Record) string {
	return fmt.Sprintf("%s/%d/%d", record.Topic, record.Partition, record.Offset)
}

func (w *Writer) insertWithRetry(ctx context.Context, events []*contracts.Event) error {
	var lastErr error
	backoff := w.settings.RetryMinBackoff
	for attempt := 1; attempt <= w.settings.MaxAttempts; attempt++ {
		operationCtx, cancel := context.WithTimeout(ctx, w.settings.OperationTimeout)
		lastErr = w.store.InsertBatch(operationCtx, events)
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt == w.settings.MaxAttempts || ctx.Err() != nil {
			break
		}

		w.log.Warn().Int("attempt", attempt).Msg("event insert failed; retrying")
		wait := jitterDelay(backoff)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return fmt.Errorf("insert event batch: %w", errors.Join(lastErr, ctx.Err()))
		case <-timer.C:
		}
		backoff = min(backoff*backoffMultiplier, w.settings.RetryMaxBackoff)
	}

	return fmt.Errorf("insert event batch after %d attempts: %w", w.settings.MaxAttempts, lastErr)
}

func jitterDelay(maximum time.Duration) time.Duration {
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(maximum)+1))
	if err != nil {
		return maximum
	}

	return time.Duration(value.Int64())
}

type topicPartition struct {
	topic     string
	partition int32
}

func lastContiguousRecords(records []*kgo.Record) ([]*kgo.Record, error) {
	last := make(map[topicPartition]*kgo.Record)
	for _, record := range records {
		key := topicPartition{topic: record.Topic, partition: record.Partition}
		if previous := last[key]; previous != nil && record.Offset != previous.Offset+1 {
			return nil, fmt.Errorf(
				"non-contiguous Kafka batch at %s/%d: offset %d follows %d",
				record.Topic, record.Partition, record.Offset, previous.Offset,
			)
		}
		last[key] = record
	}

	keys := make([]topicPartition, 0, len(last))
	for key := range last {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].topic == keys[j].topic {
			return keys[i].partition < keys[j].partition
		}

		return keys[i].topic < keys[j].topic
	})
	result := make([]*kgo.Record, 0, len(keys))
	for _, key := range keys {
		result = append(result, last[key])
	}

	return result, nil
}
