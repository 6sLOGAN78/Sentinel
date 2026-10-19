package detector

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

const poisonBackoffMultiplier = 2

func (w *Worker) classify(record *kgo.Record) (*contracts.Event, kafkax.DLQReason) {
	event, err := contracts.DecodeEvent(bytes.NewReader(record.Value))
	if err != nil {
		return nil, kafkax.DLQReasonDecode
	}
	if event.ReceivedAt == nil || event.ReceivedAt.IsZero() {
		return nil, kafkax.DLQReasonMissingReceivedAt
	}
	if event.OccurredAt.IsZero() || event.Validate(event.OccurredAt.Time, w.limits) != nil {
		return nil, kafkax.DLQReasonValidation
	}
	key := event.TenantID + "|"
	if event.SourceIP != nil {
		key += *event.SourceIP
	}
	if string(record.Key) != key {
		return nil, kafkax.DLQReasonValidation
	}
	return event, ""
}

func detectorRecords(records []*kgo.Record) (map[detection.TopicPartition][]*kgo.Record,
	[]detection.TopicPartition, error) {
	partitions := make(map[detection.TopicPartition][]*kgo.Record)
	for _, record := range records {
		if record == nil {
			return nil, nil, errors.New("nil detector source record")
		}
		tp := detection.TopicPartition{Topic: record.Topic, Partition: record.Partition}
		prior := partitions[tp]
		if len(prior) > 0 && record.Offset != prior[len(prior)-1].Offset+1 {
			return nil, nil, errors.New("noncontiguous detector source offsets")
		}
		partitions[tp] = append(prior, record)
	}
	keys := make([]detection.TopicPartition, 0, len(partitions))
	for tp := range partitions {
		keys = append(keys, tp)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Topic == keys[j].Topic {
			return keys[i].Partition < keys[j].Partition
		}
		return keys[i].Topic < keys[j].Topic
	})
	return partitions, keys, nil
}

// One sequence is shared by every quarantine in the owned batch. Successful
// publications do not replenish retry allowance or reset the backoff.
type poisonRetries struct {
	remaining int
	backoff   time.Duration
}

func (w *Worker) quarantine(ctx context.Context, record *kgo.Record, reason kafkax.DLQReason,
	retries *poisonRetries) error {
	if w.dlq == nil {
		return errors.New("detector quarantine unavailable")
	}
	for ctx.Err() == nil {
		opCtx, cancel := context.WithTimeout(ctx, w.settings.OperationTimeout)
		err := w.dlq.Publish(opCtx, record, reason)
		cancel()
		if err == nil {
			return nil
		}
		if retries.remaining == 0 {
			break
		}
		retries.remaining--
		timer := time.NewTimer(retries.backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("detector quarantine budget exhausted")
		case <-timer.C:
		}
		retries.backoff = min(retries.backoff*poisonBackoffMultiplier, w.settings.RetryMaxBackoff)
	}
	return errors.New("detector quarantine failed within bounded attempts")
}

func (w *Worker) processRecord(ctx context.Context, state *detection.PartitionState,
	record *kgo.Record, retries *poisonRetries) error {
	if ctx.Err() != nil {
		return errors.New("detector effects budget exhausted")
	}
	event, reason := w.classify(record)
	if reason == "" {
		return w.applyEvent(ctx, state, record, event)
	}
	owner := w.partitions[detection.TopicPartition{Topic: record.Topic, Partition: record.Partition}].owner
	if err := w.validateOwnership(owner); err != nil {
		return err
	}
	if err := w.quarantine(ctx, record, reason, retries); err != nil {
		return err
	}
	if err := w.validateOwnership(owner); err != nil {
		return err
	}
	return state.AcknowledgePoison(record.Offset)
}
