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
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

const stateAdmissionBytes = 4096

// Assignment couples restored metadata to one ownership incarnation.
type Assignment = kafkax.DetectorAssignment

// Consumer exposes owned batches and atomic recovery checkpoints.
type Consumer interface {
	PollRecords(context.Context, int) ([]*kgo.Record, error)
	CommitCheckpoints(context.Context, map[detection.TopicPartition]detection.Checkpoint) error
	Assignments() map[detection.TopicPartition]Assignment
	GroupMetadata() (string, int32)
	AllowRebalance()
}

// Publisher acknowledges every frozen alert before returning success.
type Publisher interface {
	PublishCanonical(context.Context, []byte) error
}
type partition struct {
	token uint64
	state *detection.PartitionState
	owner postgres.DetectionOwnership
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
	decisions  *postgres.DetectionDecisionStore
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
	w.decisions = postgres.NewDetectionDecisionStore(w.pool, pg, w.rules)
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
		if ctx.Err() != nil {
			//nolint:nilerr // Parent cancellation during idle PG restoration is normal shutdown.
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) process(ctx context.Context, records []*kgo.Record) error {
	assigned := w.consumer.Assignments()
	if err := w.syncAssignments(ctx, assigned); err != nil {
		return err
	}
	checkpoints := make(map[detection.TopicPartition]detection.Checkpoint)
	for _, record := range records {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		tp := detection.TopicPartition{Topic: record.Topic, Partition: record.Partition}
		state, err := w.partitionState(ctx, tp, assigned)
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
	return w.commitEffects(ctx, checkpoints)
}

func (w *Worker) syncAssignments(ctx context.Context, assigned map[detection.TopicPartition]Assignment) error {
	for tp := range w.partitions {
		if _, ok := assigned[tp]; !ok {
			delete(w.partitions, tp)
		}
	}
	// Restore every assignment before processing any record. Changed-config
	// adoption must also run on an idle drained partition.
	for tp, assignment := range assigned {
		if !w.assignmentCurrent(tp, assignment) {
			continue
		}
		if _, err := w.partitionState(ctx, tp, assigned); err != nil && w.assignmentCurrent(tp, assignment) {
			return err
		}
	}
	return nil
}

// Empty polls do not hold Kafka's rebalance gate. Defer idle restoration when
// the captured assignment actually changes; record-bearing polls retain their
// strict ownership checks before any new decision or source commit.
func (w *Worker) assignmentCurrent(tp detection.TopicPartition, assignment Assignment) bool {
	current, owned := w.consumer.Assignments()[tp]
	member, generation := w.consumer.GroupMetadata()
	return owned && current.Token == assignment.Token && member == assignment.Member && generation == assignment.Generation
}

func (w *Worker) commitEffects(ctx context.Context,
	checkpoints map[detection.TopicPartition]detection.Checkpoint) error {
	if len(checkpoints) == 0 {
		return nil
	}
	for tp := range checkpoints {
		if err := w.validateOwnership(w.partitions[tp].owner); err != nil {
			return err
		}
	}
	if err := w.consumer.CommitCheckpoints(ctx, checkpoints); err != nil {
		return err
	}
	for tp, cp := range checkpoints {
		w.partitions[tp].state.Committed(cp)
		if err := w.decisions.CheckpointCommitted(ctx, w.partitions[tp].owner, cp); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) partitionState(
	ctx context.Context, tp detection.TopicPartition, assigned map[detection.TopicPartition]Assignment,
) (*detection.PartitionState, error) {
	a, owned := assigned[tp]
	if !owned {
		return nil, errors.New("detector partition ownership lost")
	}
	p, ok := w.partitions[tp]
	if !ok || p.token != a.Token {
		var err error
		p, err = w.restorePartition(ctx, tp, a)
		if err != nil {
			return nil, err
		}
		w.partitions[tp] = p
	}
	if err := w.validateOwnership(p.owner); err != nil {
		return nil, err
	}
	return p.state, nil
}

func (w *Worker) restorePartition(ctx context.Context, tp detection.TopicPartition, a Assignment) (partition, error) {
	p := partition{token: a.Token, state: detection.NewPartitionState(w.rules)}
	p.owner = postgres.DetectionOwnership{Group: a.Group, Incarnation: postgres.DetectionGroupIncarnation(a.Group),
		Member: a.Member, Generation: a.Generation, Token: a.Token, Source: tp, Config: w.rules.Fingerprint()}
	if err := w.validateOwnership(p.owner); err != nil {
		return partition{}, err
	}
	adopted, err := w.decisions.PrepareRestore(ctx, p.owner, a.Checkpoint, a.TransitionAllowed)
	if err != nil {
		return partition{}, err
	}
	if adopted == nil {
		if err = p.state.BeginAt(a.HistoryStart); err != nil {
			return partition{}, err
		}
		cp, checkpointErr := p.state.Checkpoint()
		if checkpointErr != nil {
			return partition{}, checkpointErr
		}
		adopted = &cp
	}
	if err = w.finishRestore(ctx, p, *adopted, a.Checkpoint); err != nil {
		return partition{}, err
	}
	return p, nil
}

func (w *Worker) finishRestore(ctx context.Context, p partition,
	cp detection.Checkpoint, previous *detection.Checkpoint) error {
	if err := p.state.Restore(cp); err != nil {
		return err
	}
	if previous == nil || cp != *previous {
		if err := w.validateOwnership(p.owner); err != nil {
			return err
		}
		checkpoints := map[detection.TopicPartition]detection.Checkpoint{p.owner.Source: cp}
		if err := w.consumer.CommitCheckpoints(ctx, checkpoints); err != nil {
			return err
		}
	}
	return w.decisions.CheckpointCommitted(ctx, p.owner, cp)
}

func (w *Worker) validateOwnership(o postgres.DetectionOwnership) error {
	a, ok := w.consumer.Assignments()[o.Source]
	member, generation := w.consumer.GroupMetadata()
	if !ok || a.Token != o.Token || a.Group != o.Group || a.Member != o.Member || a.Generation != o.Generation ||
		member != o.Member || generation != o.Generation {
		return errors.New("detector partition ownership lost")
	}
	return nil
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
	source := detection.TopicPartition{Topic: record.Topic, Partition: record.Partition}
	outcome, err := state.ApplySource(source, record.Offset, event)
	if err != nil {
		return err
	}
	owner := w.partitions[source].owner
	for _, candidate := range outcome.Candidates {
		if err = w.validateOwnership(owner); err != nil {
			return err
		}
		decision, decisionErr := w.decisions.Decide(ctx, owner, candidate)
		if decisionErr != nil {
			return decisionErr
		}
		if err = w.validateOwnership(owner); err != nil {
			return err
		}
		if decision.Allowed {
			if err = w.publisher.PublishCanonical(ctx, decision.Canonical); err != nil {
				return err
			}
		}
		if err = state.EffectDecision(candidate); err != nil {
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
