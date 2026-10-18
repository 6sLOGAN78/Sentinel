// Package alertwriter persists immutable detections before insert-only workflows
// and commits only the durable consumed-next source prefix.
package alertwriter

import (
	"bytes"
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

// WorkflowStore holds the per-ID transaction lock until both sinks are durable.
type WorkflowStore interface {
	BeginLocked(context.Context, string) (pgx.Tx, error)
	Find(context.Context, pgx.Tx, string) (string, string, error)
	Ensure(context.Context, pgx.Tx, *contracts.Alert, string) error
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
	effectsCtx, cancel := context.WithTimeout(ctx, budgets.Retry)
	defer cancel()
	committable := make([]*kgo.Record, 0, len(keys))
	var effectErr error
	for _, key := range keys {
		var last *kgo.Record
		for _, record := range partitions[key] {
			if err = w.persistRecord(effectsCtx, record); err != nil {
				effectErr = errors.Join(effectErr, err)
				break
			}
			last = record
		}
		if last != nil {
			committable = append(committable, last)
		}
	}
	if len(committable) > 0 {
		commitCtx, cancelCommit := context.WithTimeout(ctx, w.settings.OperationTimeout)
		defer cancelCommit()
		if err = w.consumer.CommitRecords(commitCtx, committable...); err != nil {
			return errors.New("commit durable alert source prefix failed")
		}
	}
	return effectErr
}

func (w *Writer) persistRecord(ctx context.Context, record *kgo.Record) error {
	if ctx.Err() != nil {
		return errors.New("alert persistence budget exhausted")
	}
	alert, err := contracts.DecodeAlert(bytes.NewReader(record.Value))
	if err != nil {
		return w.quarantine(ctx, record, kafkax.DLQReasonDecode)
	}
	if err = ValidateIdentity(alert, record.Key); err != nil {
		return w.quarantine(ctx, record, kafkax.DLQReasonValidation)
	}
	fingerprint, err := ImmutableFingerprint(alert)
	if err != nil {
		return errors.New("alert fingerprint failed")
	}
	backoff := w.settings.RetryMinBackoff
	for attempt := 1; attempt <= w.settings.MaxAttempts; attempt++ {
		operationCtx, cancel := context.WithTimeout(ctx, w.settings.OperationTimeout)
		err = w.persist(operationCtx, alert, fingerprint)
		cancel()
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrConflict) {
			return w.quarantine(ctx, record, kafkax.DLQReasonValidation)
		}
		if attempt == w.settings.MaxAttempts || ctx.Err() != nil {
			break
		}
		w.log.Warn().Int("attempt", attempt).Msg("alert persistence failed; retrying")
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("alert persistence budget exhausted")
		case <-timer.C:
		}
		backoff = min(backoff*backoffMultiplier, w.settings.RetryMaxBackoff)
	}
	// Driver errors may contain payloads/SQL arguments; terminal logs are sanitized.
	return errors.New("alert persistence failed within bounded attempts")
}

func (w *Writer) quarantine(ctx context.Context, record *kgo.Record, reason kafkax.DLQReason) error {
	if w.dlq == nil {
		return errors.New("alert quarantine unavailable")
	}
	backoff := w.settings.RetryMinBackoff
	for attempt := 1; attempt <= w.settings.MaxAttempts && ctx.Err() == nil; attempt++ {
		publishCtx, cancel := context.WithTimeout(ctx, w.settings.OperationTimeout)
		err := w.dlq.Publish(publishCtx, record, reason)
		cancel()
		if err == nil {
			return nil
		}
		if attempt == w.settings.MaxAttempts {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("alert quarantine budget exhausted")
		case <-timer.C:
		}
		backoff = min(backoff*backoffMultiplier, w.settings.RetryMaxBackoff)
	}
	return errors.New("alert quarantine failed within bounded attempts")
}

func (w *Writer) persist(ctx context.Context, alert *contracts.Alert, fingerprint string) error {
	tx, err := w.workflows.BeginLocked(ctx, alert.AlertID)
	if err != nil {
		return err
	}
	defer rollback(tx)
	tenant, winner, err := w.workflows.Find(ctx, tx, alert.AlertID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && (tenant != alert.TenantID || winner != fingerprint) {
		return ErrConflict
	}
	hasWinner := err == nil
	retained, getErr := w.detections.Get(ctx, alert.TenantID, alert.AlertID)
	if getErr != nil && !errors.Is(getErr, storageclickhouse.ErrNotFound) {
		return getErr
	}
	if getErr == nil {
		retainedFingerprint, fpErr := ImmutableFingerprint(retained)
		if fpErr != nil {
			return fpErr
		}
		if retainedFingerprint != fingerprint {
			return ErrConflict
		}
	} else if err = w.detections.InsertBatch(ctx, []*contracts.Alert{alert}); err != nil {
		return err
	}
	// Equal winners already own the workflow. Avoid even ON CONFLICT INSERT:
	// defaults allocate seq before conflict detection, and replay needs no write.
	if !hasWinner {
		if err = w.workflows.Ensure(ctx, tx, alert, fingerprint); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}
