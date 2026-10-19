// Package detector wires bounded Kafka effects to the event-time reducer.
package detector

import (
	"context"
	"errors"
	"maps"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

// Assignment couples restored metadata to one ownership incarnation.
type Assignment = kafkax.DetectorAssignment

// Consumer exposes owned batches and atomic recovery checkpoints.
type Consumer interface {
	PollRecords(context.Context, int) ([]*kgo.Record, error)
	CommitCheckpoints(context.Context, map[detection.TopicPartition]detection.Checkpoint) error
	Assignments() map[detection.TopicPartition]Assignment
	GroupMetadata() (string, int32)
	ReadinessSnapshot() kafkax.DetectorReadiness
	AllowRebalance()
}

// Ready reports whether the current assignment has completed required restore.
func (w *Worker) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current := w.consumer.ReadinessSnapshot()
	restored := w.restored.Load()
	if !current.Observed || restored == nil || current.Member != restored.Member ||
		current.Generation != restored.Generation || !maps.Equal(current.Tokens, restored.Tokens) {
		return errors.New("detector assignment restoration pending")
	}
	return nil
}

func (w *Worker) publishReadiness(assigned map[detection.TopicPartition]Assignment) {
	snapshot := w.consumer.ReadinessSnapshot()
	if !snapshot.Observed || len(snapshot.Tokens) != len(assigned) {
		w.restored.Store(nil)
		return
	}
	for tp, token := range snapshot.Tokens {
		assignment, exists := assigned[tp]
		part, restored := w.partitions[tp]
		if !exists || !restored || part.token != token || assignment.Token != token ||
			!w.assignmentCurrent(tp, assignment) {
			w.restored.Store(nil)
			return
		}
	}
	w.restored.Store(&snapshot)
}

// Publisher acknowledges every frozen alert before returning success.
type Publisher interface {
	PublishCanonical(context.Context, []byte) error
}

// QuarantinePublisher acknowledges the existing bounded DLQ envelope.
type QuarantinePublisher interface {
	Publish(context.Context, *kgo.Record, kafkax.DLQReason) error
}
type partition struct {
	token uint64
	state *detection.PartitionState
	owner postgres.DetectionOwnership
}

type decisionStore interface {
	PrepareRestore(context.Context, postgres.DetectionOwnership,
		*detection.Checkpoint, bool) (*detection.Checkpoint, error)
	CheckpointCommitted(context.Context, postgres.DetectionOwnership, detection.Checkpoint) error
	Register(context.Context, postgres.DetectionOwnership, detection.DecisionFrontier) error
	Decide(context.Context, postgres.DetectionOwnership, detection.Candidate) (postgres.DetectionDecision, error)
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
	decisions  decisionStore
	bounds     *detection.Bounds
	metrics    *opshttp.DetectionMetrics
	restored   atomic.Pointer[kafkax.DetectorReadiness]
	dlq        QuarantinePublisher
}

// WithQuarantine binds the publisher before Run.
func (w *Worker) WithQuarantine(publisher QuarantinePublisher) *Worker {
	w.dlq = publisher
	return w
}

// New constructs a worker from explicit dependencies and startup settings.
func New(
	consumer Consumer, publisher Publisher, rules *detection.Rules,
	settings config.WriterConfig, limits contracts.Limits,
	pool *pgxpool.Pool, pg config.DetectorPGConfig, metrics ...*opshttp.DetectionMetrics,
) *Worker {
	w := &Worker{
		consumer: consumer, publisher: publisher, rules: rules, settings: settings, limits: limits,
		partitions: make(map[detection.TopicPartition]partition),
		pool:       pool, pg: pg,
		bounds: detection.NewBounds(detection.StateLimits{}),
	}
	if len(metrics) > 0 {
		w.metrics = metrics[0]
	}
	return w
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
	budgets, err := config.ComputeWriterBudgets(w.settings)
	if err != nil {
		return err
	}
	ownedCtx, cancelOwned := context.WithTimeout(ctx, budgets.Processing)
	defer cancelOwned()
	assigned := w.consumer.Assignments()
	if err = w.syncAssignments(ownedCtx, assigned); err != nil {
		return err
	}
	partitions, keys, err := detectorRecords(records)
	if err != nil {
		return err
	}
	deadline, _ := ownedCtx.Deadline()
	effectsDeadline := minTime(time.Now().Add(budgets.Retry), deadline.Add(-w.settings.OperationTimeout))
	effectsCtx, cancelEffects := context.WithDeadline(ownedCtx, effectsDeadline)
	defer cancelEffects()
	retries := &poisonRetries{remaining: w.settings.MaxAttempts - 1, backoff: w.settings.RetryMinBackoff}
	checkpoints := make(map[detection.TopicPartition]detection.Checkpoint)
	var effectErr error
	for _, tp := range keys {
		for _, record := range partitions[tp] {
			cp, recordErr := w.processCheckpoint(effectsCtx, record, tp, assigned, retries)
			if recordErr != nil {
				effectErr = errors.Join(effectErr, recordErr)
				break
			}
			checkpoints[tp] = cp
		}
	}
	// Effects expiry leaves a separate source-commit reservation live. Parent
	// cancellation still forbids new commits and bounds shutdown ownership.
	if ownedCtx.Err() != nil {
		return errors.Join(effectErr, ownedCtx.Err())
	}
	commitCtx, cancelCommit := context.WithTimeout(ownedCtx, w.settings.OperationTimeout)
	defer cancelCommit()
	if err = w.commitEffects(commitCtx, checkpoints); err != nil {
		return err
	}
	if effectErr == nil {
		w.publishReadiness(assigned)
	}
	return effectErr
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (w *Worker) processCheckpoint(ctx context.Context, record *kgo.Record, tp detection.TopicPartition,
	assigned map[detection.TopicPartition]Assignment, retries *poisonRetries) (detection.Checkpoint, error) {
	state, err := w.partitionState(ctx, tp, assigned)
	if err != nil {
		return detection.Checkpoint{}, err
	}
	if err = w.processRecord(ctx, state, record, retries); err != nil {
		return detection.Checkpoint{}, err
	}
	if err = reachBoundary(ctx, "after-record", tp, record.Offset); err != nil {
		return detection.Checkpoint{}, err
	}
	return state.Checkpoint()
}

func (w *Worker) syncAssignments(ctx context.Context, assigned map[detection.TopicPartition]Assignment) error {
	for tp := range w.partitions {
		if _, ok := assigned[tp]; !ok {
			w.partitions[tp].state.Release()
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
		m, err := detection.CheckpointMetadata(cp)
		if err != nil {
			return err
		}
		if err = reachBoundary(ctx, "after-successful-checkpoint", tp, m.ProcessedNext-1); err != nil {
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
		if ok {
			w.partitions[tp].state.Release()
		}
		w.partitions[tp] = p
	}
	if p.owner.Member != a.Member || p.owner.Generation != a.Generation {
		owner := p.owner
		owner.Member, owner.Generation = a.Member, a.Generation
		if err := w.validateOwnership(owner); err != nil {
			return nil, err
		}
		// A same-token cooperative retention preserves fully effected reducer
		// state. Re-fence and verify its durable frontier before changing owner;
		// newly acquired tokens still take the complete restore path above.
		if err := w.decisions.Register(ctx, owner, p.state.DecisionFrontier()); err != nil {
			return nil, err
		}
		if err := w.validateOwnership(owner); err != nil {
			return nil, err
		}
		p.owner = owner
		w.partitions[tp] = p
	}
	if err := w.validateOwnership(p.owner); err != nil {
		return nil, err
	}
	return p.state, nil
}

func (w *Worker) restorePartition(ctx context.Context, tp detection.TopicPartition, a Assignment) (partition, error) {
	p := partition{token: a.Token, state: detection.NewPartitionStateWithBounds(w.rules, w.bounds)}
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
	a.Checkpoint = adopted
	if err = observeAssignment(ctx, "restored", tp, a); err != nil {
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

func (w *Worker) applyEvent(ctx context.Context, state *detection.PartitionState,
	record *kgo.Record, event *contracts.Event) error {
	source := detection.TopicPartition{Topic: record.Topic, Partition: record.Partition}
	outcome, err := state.ApplySource(source, record.Offset, event)
	if err != nil {
		return err
	}
	w.recordLateness(outcome.Late)
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
			if err = w.publishAllowed(ctx, decision.Canonical, source, record.Offset); err != nil {
				return err
			}
		}
		if err = state.EffectDecision(candidate); err != nil {
			return err
		}
	}
	return state.Acknowledge(record.Offset)
}

func (w *Worker) publishAllowed(ctx context.Context, canonical []byte,
	tp detection.TopicPartition, offset int64) error {
	if err := reachBoundary(ctx, "before-publish", tp, offset); err != nil {
		return err
	}
	if err := w.publisher.PublishCanonical(ctx, canonical); err != nil {
		return err
	}
	return reachBoundary(ctx, "after-ack-before-checkpoint", tp, offset)
}

func (w *Worker) recordLateness(late bool) {
	if late && w.metrics != nil {
		w.metrics.IncLate()
	}
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
	return kafkax.NewDetectorConsumer(ctx, kafka, settings, rules, onFatal, observeAssignment)
}
