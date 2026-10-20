// Package alertwriter persists immutable detections before insert-only workflows
// and commits only the durable consumed-next source prefix.
package alertwriter

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

const (
	rollbackTimeout   = time.Second
	backoffMultiplier = 2
)

// ErrConflict marks differing immutable content for an existing alert identity.
var ErrConflict = storageclickhouse.ErrConflict

// Consumer is the owned, manually committed Kafka batch lifecycle.
type Consumer interface {
	PollRecords(context.Context, int) ([]*kgo.Record, error)
	CommitRecords(context.Context, ...*kgo.Record) error
	AllowRebalance()
}

// DetectionStore is immutable evidence storage with immediately deduplicated reads.
type DetectionStore interface {
	InsertBatch(context.Context, []*contracts.Alert) error
	Get(context.Context, string, string) (*contracts.Alert, error)
}

// WorkflowStore commits immutable authority before the locked sink effects.
type WorkflowStore interface {
	BeginLocked(context.Context, string) (pgx.Tx, error)
	Find(context.Context, pgx.Tx, string) (string, string, error)
	Ensure(context.Context, pgx.Tx, *contracts.Alert, string) error
	FindReservation(context.Context, pgx.Tx, string) (string, string, []byte, error)
	InsertReservation(context.Context, pgx.Tx, string, string, string, []byte) error
}

// QuarantinePublisher acknowledges the existing bounded, sanitized DLQ envelope.
type QuarantinePublisher interface {
	Publish(context.Context, *kgo.Record, kafkax.DLQReason) error
}

// Writer owns one finite alert batch at a time.
type Writer struct {
	consumer   Consumer
	detections DetectionStore
	workflows  WorkflowStore
	settings   config.WriterConfig
	log        zerolog.Logger
	dlq        QuarantinePublisher
}

// WithQuarantine binds the publisher before Run. Without it permanent failures
// remain unacknowledged, allowing existing controlled callers to fail closed.
func (w *Writer) WithQuarantine(publisher QuarantinePublisher) *Writer {
	w.dlq = publisher
	return w
}

// New binds explicit dependencies without creating alternate persistence paths.
func New(consumer Consumer, detections DetectionStore, workflows WorkflowStore,
	settings config.WriterConfig, log zerolog.Logger) *Writer {
	return &Writer{consumer: consumer, detections: detections, workflows: workflows, settings: settings, log: log}
}

// Run processes each poll under one aggregate deadline, releasing ownership on
// all exits. Parent cancellation leaves uncommitted input available for replay.
func (w *Writer) Run(ctx context.Context) error {
	budgets, err := config.ComputeWriterBudgets(w.settings)
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		records, pollErr := w.consumer.PollRecords(ctx, w.settings.BatchSize)
		if pollErr != nil {
			w.consumer.AllowRebalance()
			if ctx.Err() != nil {
				//nolint:nilerr // Parent cancellation is normal lifecycle shutdown; source remains replayable.
				return nil
			}
			return errors.New("poll alert records failed")
		}
		if len(records) == 0 {
			w.consumer.AllowRebalance()
			continue
		}
		batchCtx, cancel := context.WithTimeout(ctx, budgets.Processing)
		err = w.process(batchCtx, records, budgets)
		cancel()
		w.consumer.AllowRebalance()
		if err != nil {
			if ctx.Err() != nil {
				//nolint:nilerr // Cancellation never acknowledges incomplete source work.
				return nil
			}
			return err
		}
	}
	return nil
}

type topicPartition struct {
	topic     string
	partition int32
}

func groupRecords(records []*kgo.Record) (map[topicPartition][]*kgo.Record, []topicPartition, error) {
	partitions := make(map[topicPartition][]*kgo.Record)
	for _, record := range records {
		if record == nil {
			return nil, nil, errors.New("nil alert source record")
		}
		key := topicPartition{record.Topic, record.Partition}
		prior := partitions[key]
		if len(prior) > 0 && record.Offset != prior[len(prior)-1].Offset+1 {
			return nil, nil, errors.New("noncontiguous alert source offsets")
		}
		partitions[key] = append(prior, record)
	}
	keys := make([]topicPartition, 0, len(partitions))
	for key := range partitions {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].topic == keys[j].topic {
			return keys[i].partition < keys[j].partition
		}
		return keys[i].topic < keys[j].topic
	})
	return partitions, keys, nil
}

func (w *Writer) process(ctx context.Context, records []*kgo.Record, budgets config.WriterBudgets) error {
	partitions, keys, err := groupRecords(records)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(budgets.Retry)
	if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Add(-w.settings.OperationTimeout).Before(deadline) {
		deadline = parentDeadline.Add(-w.settings.OperationTimeout)
	}
	effectsCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	retries := &effectRetries{remaining: w.settings.MaxAttempts - 1, backoff: w.settings.RetryMinBackoff}
	committable := make([]*kgo.Record, 0, len(keys))
	var effectErr error
	for _, key := range keys {
		var last *kgo.Record
		for _, record := range partitions[key] {
			if err = w.persistRecord(effectsCtx, record, retries); err != nil {
				effectErr = errors.Join(effectErr, err)
				break
			}
			last = record
		}
		if last != nil {
			committable = append(committable, last)
		}
	}
	if len(committable) > 0 && ctx.Err() == nil {
		commitCtx, cancelCommit := context.WithTimeout(ctx, w.settings.OperationTimeout)
		defer cancelCommit()
		if err = w.consumer.CommitRecords(commitCtx, committable...); err != nil {
			return errors.New("commit durable alert source prefix failed")
		}
	}
	return effectErr
}

func (w *Writer) persist(ctx context.Context, alert *contracts.Alert, fingerprint string) error {
	attempt, err := w.prepareReservation(ctx, alert, fingerprint)
	if err != nil {
		return err
	}
	// prepareReservation has returned (and released its pool connection) before
	// another transaction begins, under the same original operation deadline.
	if err = afterReservationBeforeInsert(ctx, alert.AlertID); err != nil {
		return err
	}
	return w.persistReserved(ctx, attempt, fingerprint)
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}
