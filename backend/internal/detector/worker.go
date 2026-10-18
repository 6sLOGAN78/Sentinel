// Package detector wires bounded Kafka effects to the event-time reducer.
package detector

import (
	"bytes"
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

const stateAdmissionBytes = 4096

// Assignment couples restored metadata to one ownership incarnation.
type Assignment = kafkax.DetectorAssignment

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
	pool       *pgxpool.Pool
	pg         config.DetectorPGConfig
}

// New constructs a worker from explicit dependencies and startup settings.
func New(
	consumer Consumer, publisher Publisher, rules *detection.Rules,
	settings config.WriterConfig, limits contracts.Limits,
	pool *pgxpool.Pool, pg config.DetectorPGConfig,
) *Worker {
	return &Worker{
		consumer: consumer, publisher: publisher, rules: rules, settings: settings, limits: limits,
		partitions: make(map[detection.TopicPartition]partition),
		pool:       pool, pg: pg,
	}
}

// Run processes one bounded owned batch at a time until cancellation or failure.
func (w *Worker) Run(ctx context.Context) error {
	budgets, err := config.ComputeWriterBudgets(w.settings)
	if err != nil {
		return err
	}
	pg, err := config.ComputeDetectorPGBudgets(w.pg, w.settings)
	if err != nil {
		return err
	}
	if w.pool == nil {
		return errors.New("detector requires postgres pool")
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
		if len(records) > 0 {
			pingCtx, cancelPing := context.WithTimeout(batchCtx, pg.AcquireTimeout)
			err = w.pool.Ping(pingCtx)
			cancelPing()
			if err != nil {
				err = errors.New("postgres unavailable")
			}
		}
		if err == nil {
			err = w.process(batchCtx, records)
		}
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

// AlertProducer preserves the detector constructor contract.
type AlertProducer = kafkax.AlertProducer

// NewAlertProducer creates the dedicated bounded Kafka adapter.
func NewAlertProducer(ctx context.Context, cfg config.KafkaConfig) (*AlertProducer, error) {
	return kafkax.NewAlertProducer(ctx, cfg)
}

// KafkaConsumer preserves the detector constructor contract.
type KafkaConsumer = kafkax.DetectorConsumer

// NewKafkaConsumer creates the dedicated metadata-aware Kafka adapter.
func NewKafkaConsumer(ctx context.Context, kafka config.KafkaConfig, settings config.WriterConfig,
	rules *detection.Rules, onFatal func(error),
) (*KafkaConsumer, error) {
	return kafkax.NewDetectorConsumer(ctx, kafka, settings, rules, onFatal)
}
