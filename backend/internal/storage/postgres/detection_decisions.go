package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

const (
	maxDecisionPayload   = 64 << 10
	floodDecisionOrdinal = 2
	denyDecisionOrdinal  = 3
)

// DetectionOwnership binds an assignment to its configured Kafka group.
type DetectionOwnership struct {
	Group, Incarnation, Member string
	Generation                 int32
	Token                      uint64
	Source                     detection.TopicPartition
	Config                     string
}

// DetectionDecision is the stored first disposition and canonical payload.
type DetectionDecision struct {
	Allowed   bool
	Canonical []byte
}

// DetectionDecisionStore persists operational decisions separately from workflows.
type DetectionDecisionStore struct {
	pool     *pgxpool.Pool
	budgets  config.DetectorPGConfig
	rules    *detection.Rules
	begin    func(context.Context) (pgx.Tx, func(), error)
	boundary func(context.Context, string, detection.TopicPartition, int64) error
}

// NewDetectionDecisionStore uses the explicit bounded detector pool.
func NewDetectionDecisionStore(pool *pgxpool.Pool, budgets config.DetectorPGConfig,
	rules *detection.Rules,
	boundaries ...func(context.Context, string, detection.TopicPartition, int64) error) *DetectionDecisionStore {
	s := &DetectionDecisionStore{pool: pool, budgets: budgets, rules: rules}
	if len(boundaries) > 0 {
		s.boundary = boundaries[0]
	}
	return s
}

// DetectionGroupIncarnation is stable for a configured group, across processes.
// The fence primary key excludes this value, so changing it cannot reset epochs.
func DetectionGroupIncarnation(group string) string {
	digest := sha256.Sum256([]byte("sentinel.detector.group.v1\x00" + group))
	return hex.EncodeToString(digest[:])
}

func (s *DetectionDecisionStore) validateOwner(o DetectionOwnership) error {
	if o.Group == "" || len(o.Group) > 255 || o.Incarnation != DetectionGroupIncarnation(o.Group) ||
		o.Member == "" || len(o.Member) > 1024 || o.Generation < 0 || o.Token == 0 || o.Token > math.MaxInt64 ||
		o.Source.Topic == "" || len(o.Source.Topic) > 249 || o.Source.Partition < 0 || o.Config != s.rules.Fingerprint() {
		return errors.New("invalid detector ownership")
	}
	return nil
}

func (o DetectionOwnership) tokenValue() int64 {
	// All entry points validate the positive token against MaxInt64 first.
	return int64(o.Token) //nolint:gosec // validateOwner rejects overflow before SQL/fence comparisons.
}

func (s *DetectionDecisionStore) beginTransaction(ctx context.Context) (pgx.Tx, func(), error) {
	if s.begin != nil {
		return s.begin(ctx)
	}
	if s.pool == nil {
		return nil, nil, errors.New("detector postgres pool absent")
	}
	acquireCtx, cancel := context.WithTimeout(ctx, s.budgets.AcquireTimeout)
	conn, err := s.pool.Acquire(acquireCtx)
	cancel()
	if err != nil {
		return nil, nil, err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		return nil, nil, err
	}
	return tx, conn.Release, nil
}

// transaction never holds a connection across publication. Independent cleanup
// is mandatory because pgx Begin's context does not roll back canceled work.
func (s *DetectionDecisionStore) transaction(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	txCtx, cancel := context.WithTimeout(ctx, s.budgets.TransactionTimeout)
	defer cancel()
	tx, release, err := s.beginTransaction(txCtx)
	if err != nil {
		return errors.New("postgres decision begin failed")
	}
	defer release()
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), s.budgets.CleanupTimeout)
		defer cancelCleanup()
		_ = tx.Rollback(cleanup)
	}()
	ms := func(d time.Duration) string { return fmt.Sprintf("%dms", max(int64(1), d.Milliseconds())) }
	_, err = tx.Exec(txCtx, `SELECT set_config('lock_timeout',$1,true),set_config('statement_timeout',$2,true),
		set_config('idle_in_transaction_session_timeout',$3,true),set_config('transaction_timeout',$3,true)`,
		ms(s.budgets.LockTimeout), ms(s.budgets.StatementTimeout), ms(s.budgets.TransactionTimeout))
	if err == nil {
		err = fn(txCtx, tx)
	}
	if err != nil {
		return errors.New("postgres decision invariant or transaction failed")
	}
	if err = tx.Commit(txCtx); err != nil {
		// Outcome is uncertain: stop without publication/checkpoint. Restart looks
		// up the immutable row before the cursor and safely resolves either result.
		return errors.New("postgres decision commit uncertain; replay required")
	}
	return nil
}

type decisionFence struct {
	incarnation, member, config string
	generation                  int32
	token, offset               int64
	ordinal                     int
}

// configHandover retains both sides so a PG commit followed by a Kafka commit
// failure can resume the same authorized transition without inventing new gates.
type configHandover struct {
	Previous detection.Checkpoint `json:"previous"`
	Next     detection.Checkpoint `json:"next"`
}

// PrepareRestore binds unchanged rules or adopts an exactly acknowledged drain.
// transition is granted only by the Kafka adapter after verifying sole membership.
func (s *DetectionDecisionStore) PrepareRestore(ctx context.Context, o DetectionOwnership,
	cp *detection.Checkpoint, transition bool) (*detection.Checkpoint, error) {
	if err := s.validateOwner(o); err != nil {
		return nil, err
	}
	frontier, changed, err := s.restoreConfigFrontier(cp)
	if err != nil {
		return nil, err
	}
	if changed {
		if !transition {
			return nil, errors.New("configuration transition requires sole stopped-old-owner restart")
		}
		return s.transitionConfig(ctx, o, *cp)
	}
	if err = s.Register(ctx, o, frontier); err != nil {
		return nil, err
	}
	return cp, s.bindConfigManifest(ctx, o, cp)
}

func (s *DetectionDecisionStore) restoreConfigFrontier(
	cp *detection.Checkpoint,
) (detection.DecisionFrontier, bool, error) {
	if cp == nil {
		return detection.DecisionFrontier{None: true}, false, nil
	}
	m, err := detection.CheckpointMetadata(*cp)
	if err != nil {
		return detection.DecisionFrontier{}, false, err
	}
	if m.ConfigFingerprint != s.rules.Fingerprint() {
		return m.DecisionFrontier, true, nil
	}
	return m.DecisionFrontier, false, detection.NewPartitionState(s.rules).Restore(*cp)
}

func (s *DetectionDecisionStore) bindConfigManifest(ctx context.Context, o DetectionOwnership,
	cp *detection.Checkpoint) error {
	return s.transaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := lockedDecisionFence(ctx, tx, o)
		if err != nil || !f.matches(o) {
			return errors.New("configuration ownership lost")
		}
		var manifest, handover []byte
		if err = tx.QueryRow(ctx, `SELECT rules_manifest,handover_checkpoint FROM detection_partition_fences
			WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition).
			Scan(&manifest, &handover); err != nil {
			return err
		}
		if cp == nil && (len(manifest) > 0 || f.offset != -1) {
			return errors.New("existing detector group lost its Kafka checkpoint")
		}
		if len(manifest) > 0 && !matchingRulesManifest(manifest, o.Config) {
			return errors.New("saved semantic manifest mismatch")
		}
		if len(handover) > 0 {
			if cp == nil || !validHandoverGates(handover, *cp) {
				return errors.New("saved handover gates missing or changed")
			}
		}
		data, err := json.Marshal(s.rules)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE detection_partition_fences SET rules_manifest=$4
			WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition, data)
		return err
	})
}

func matchingRulesManifest(raw []byte, fingerprint string) bool {
	var saved detection.Rules
	return json.Unmarshal(raw, &saved) == nil && saved.Fingerprint() == fingerprint
}

func validHandoverGates(raw []byte, cp detection.Checkpoint) bool {
	var proof configHandover
	if json.Unmarshal(raw, &proof) != nil || cp.Offset < proof.Next.Offset {
		return false
	}
	expected, err := detection.CheckpointMetadata(proof.Next)
	if err != nil {
		return false
	}
	actual, err := detection.CheckpointMetadata(cp)
	if err != nil || actual.ConfigFingerprint != expected.ConfigFingerprint {
		return false
	}
	for id, gate := range expected.RuleEffectiveFrom {
		if actual.RuleEffectiveFrom[id] != gate {
			return false
		}
	}
	return true
}

type configFence struct {
	decisionFence
	source                    detection.TopicPartition
	manifest, drain, handover []byte
	drained                   bool
}

// Every changed-config transaction locks the same stable group rows in the
// same order, before inspecting drain proofs or changing any partition fence.
func lockedConfigFences(ctx context.Context, tx pgx.Tx, group string) ([]configFence, error) {
	rows, err := tx.Query(ctx, `SELECT topic,partition_id,incarnation,member_id,generation,assignment_token,
		config_fingerprint,frontier_offset,frontier_ordinal,rules_manifest,drain_checkpoint,handover_checkpoint,drained
		FROM detection_partition_fences WHERE group_id=$1 ORDER BY topic,partition_id FOR UPDATE`, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fences []configFence
	for rows.Next() {
		var f configFence
		if err = rows.Scan(&f.source.Topic, &f.source.Partition, &f.incarnation, &f.member, &f.generation, &f.token,
			&f.config, &f.offset, &f.ordinal, &f.manifest, &f.drain, &f.handover, &f.drained); err != nil {
			return nil, err
		}
		fences = append(fences, f)
	}
	return fences, rows.Err()
}

func sharedConfigFloor(fences []configFence, previous, next string) (int64, error) {
	var sealed int64
	for _, f := range fences {
		if !f.drained {
			return 0, errors.New("all old partitions must have acknowledged drains")
		}
		var cp detection.Checkpoint
		if f.config == previous {
			if json.Unmarshal(f.drain, &cp) != nil {
				return 0, errors.New("missing drain checkpoint")
			}
		} else {
			var proof configHandover
			if f.config != next || json.Unmarshal(f.handover, &proof) != nil {
				return 0, errors.New("mixed configuration owners")
			}
			cp = proof.Previous
		}
		m, err := detection.CheckpointMetadata(cp)
		if err != nil || !m.Drained || m.ConfigFingerprint != previous {
			return 0, errors.New("mixed drain metadata")
		}
		if m.SealedThrough != nil {
			sealed = max(sealed, *m.SealedThrough)
		}
	}
	return sealed, nil
}

func (s *DetectionDecisionStore) transitionConfig(ctx context.Context, o DetectionOwnership,
	cp detection.Checkpoint) (*detection.Checkpoint, error) {
	m, err := detection.CheckpointMetadata(cp)
	if err != nil || !m.Drained {
		return nil, errors.New("configuration checkpoint is active")
	}
	var adopted detection.Checkpoint
	err = s.transaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
		fences, lockErr := lockedConfigFences(ctx, tx, o.Group)
		if lockErr != nil {
			return lockErr
		}
		sealed, floorErr := sharedConfigFloor(fences, m.ConfigFingerprint, o.Config)
		if floorErr != nil {
			return floorErr
		}
		for _, f := range fences {
			if f.source == o.Source {
				return s.adoptConfigFence(ctx, tx, o, cp, f, sealed, &adopted)
			}
		}
		return errors.New("configuration fence missing; no fresh namespace adoption")
	})
	if err != nil {
		return nil, err
	}
	return &adopted, nil
}

func (s *DetectionDecisionStore) adoptConfigFence(ctx context.Context, tx pgx.Tx, o DetectionOwnership,
	cp detection.Checkpoint, f configFence, sealed int64, adopted *detection.Checkpoint) error {
	if f.incarnation != o.Incarnation || o.Generation <= f.generation &&
		(o.Generation != f.generation || o.Member != f.member || o.tokenValue() < f.token) {
		return errors.New("stale handover owner")
	}
	if f.config == o.Config {
		var proof configHandover
		if json.Unmarshal(f.handover, &proof) != nil || proof.Previous != cp ||
			!validHandoverGates(f.handover, proof.Next) {
			return errors.New("inconsistent pending handover")
		}
		*adopted = proof.Next
	} else if err := s.adoptOldConfig(ctx, tx, o, cp, f, sealed, adopted); err != nil {
		return err
	}
	manifest, err := json.Marshal(s.rules)
	if err != nil {
		return err
	}
	proof, err := json.Marshal(configHandover{Previous: cp, Next: *adopted})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE detection_partition_fences SET config_fingerprint=$4,rules_manifest=$5,
		handover_checkpoint=$6,member_id=$7,generation=$8,assignment_token=$9
		WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition,
		o.Config, manifest, proof, o.Member, o.Generation, o.tokenValue())
	return err
}

func (s *DetectionDecisionStore) adoptOldConfig(ctx context.Context, tx pgx.Tx, o DetectionOwnership,
	cp detection.Checkpoint, f configFence, sealed int64, adopted *detection.Checkpoint) error {
	var saved detection.Rules
	var acknowledged detection.Checkpoint
	if json.Unmarshal(f.manifest, &saved) != nil || json.Unmarshal(f.drain, &acknowledged) != nil ||
		acknowledged != cp || saved.Fingerprint() != f.config {
		return errors.New("handover does not match saved semantic manifest and acknowledged drain")
	}
	m, err := detection.CheckpointMetadata(cp)
	if err != nil || !fullyEffectedConfig(f, m, f.handover) {
		return errors.New("pending old decisions prevent handover")
	}
	oldOwner := o
	oldOwner.Config = f.config
	if err = verifyDecisionFrontier(ctx, tx, oldOwner, f.decisionFence, m.DecisionFrontier); err != nil {
		return err
	}
	*adopted, err = detection.TransitionCheckpoint(&saved, s.rules, cp, sealed)
	return err
}

func fullyEffectedConfig(f configFence, m detection.Metadata, handover []byte) bool {
	if !m.DecisionFrontier.None {
		return f.offset == m.DecisionFrontier.Offset && f.ordinal == m.DecisionFrontier.RuleOrdinal
	}
	if f.offset == -1 {
		return true
	}
	var proof configHandover
	return json.Unmarshal(handover, &proof) == nil && f.offset < proof.Next.Offset
}

// CheckpointCommitted is called only after the complete successful synchronous
// Kafka commit callback. A PG-ahead pending decision cannot certify a drain.
func (s *DetectionDecisionStore) CheckpointCommitted(ctx context.Context, o DetectionOwnership,
	cp detection.Checkpoint) error {
	if err := s.validateOwner(o); err != nil {
		return err
	}
	m, metadataErr := detection.CheckpointMetadata(cp)
	if metadataErr != nil || m.ConfigFingerprint != o.Config {
		return errors.New("invalid committed config checkpoint")
	}
	return s.transaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, err := lockedDecisionFence(ctx, tx, o)
		if err != nil || !f.matches(o) {
			return errors.New("committed checkpoint ownership lost")
		}
		var proof []byte
		if err = tx.QueryRow(ctx, `SELECT handover_checkpoint FROM detection_partition_fences
			WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition).
			Scan(&proof); err != nil {
			return err
		}
		var drain []byte
		drained := m.Drained && fullyEffectedConfig(configFence{decisionFence: f}, m, proof)
		if drained {
			drain, err = json.Marshal(cp)
			if err != nil {
				return err
			}
		}
		var sealed *time.Time
		if m.SealedThrough != nil {
			at := time.UnixMilli(*m.SealedThrough)
			sealed = &at
		}
		_, err = tx.Exec(ctx, `UPDATE detection_partition_fences SET drained=$4,sealed_through=$5,drain_checkpoint=$6
			WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition,
			drained, sealed, drain)
		return err
	})
}

func lockedDecisionFence(ctx context.Context, tx pgx.Tx, o DetectionOwnership) (decisionFence, error) {
	var f decisionFence
	err := tx.QueryRow(ctx, `SELECT incarnation,member_id,generation,assignment_token,config_fingerprint,
		frontier_offset,frontier_ordinal FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2 AND partition_id=$3 FOR UPDATE`, o.Group, o.Source.Topic, o.Source.Partition).
		Scan(&f.incarnation, &f.member, &f.generation, &f.token, &f.config, &f.offset, &f.ordinal)
	return f, err
}

func (f decisionFence) matches(o DetectionOwnership) bool {
	return f.incarnation == o.Incarnation && f.member == o.Member && f.generation == o.Generation &&
		f.token == o.tokenValue() && f.config == o.Config
}

func positionBefore(offset int64, ordinal int, otherOffset int64, otherOrdinal int) bool {
	return offset < otherOffset || offset == otherOffset && ordinal < otherOrdinal
}

// Register fences an assignment and verifies its restored durable frontier.
func (s *DetectionDecisionStore) Register(ctx context.Context, o DetectionOwnership,
	frontier detection.DecisionFrontier) error {
	if err := s.validateOwner(o); err != nil {
		return err
	}
	return s.transaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO detection_partition_fences
			(group_id,topic,partition_id,incarnation,member_id,generation,assignment_token,config_fingerprint)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, o.Group, o.Source.Topic, o.Source.Partition,
			o.Incarnation, o.Member, o.Generation, o.tokenValue(), o.Config)
		if err != nil {
			return err
		}
		f, err := lockedDecisionFence(ctx, tx, o)
		if err != nil {
			return err
		}
		if f.incarnation != o.Incarnation || f.config != o.Config || o.Generation < f.generation ||
			o.Generation == f.generation && (o.Member != f.member || o.tokenValue() < f.token) {
			return errors.New("stale detector assignment")
		}
		if err = verifyDecisionFrontier(ctx, tx, o, f, frontier); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE detection_partition_fences SET member_id=$4,generation=$5,assignment_token=$6
			WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition,
			o.Member, o.Generation, o.tokenValue())
		return err
	})
}

func verifyDecisionFrontier(ctx context.Context, tx pgx.Tx, o DetectionOwnership, f decisionFence,
	frontier detection.DecisionFrontier) error {
	if frontier.None {
		return nil
	}
	if positionBefore(f.offset, f.ordinal, frontier.Offset, frontier.RuleOrdinal) {
		return errors.New("postgres decision frontier behind checkpoint")
	}
	var fingerprint, configuration string
	var offset int64
	var ordinal int
	err := tx.QueryRow(ctx, `SELECT immutable_fingerprint,config_fingerprint,source_offset,rule_ordinal
		FROM detection_decisions WHERE alert_id=$1 AND group_id=$2 AND topic=$3 AND partition_id=$4`,
		frontier.AlertID, o.Group, o.Source.Topic, o.Source.Partition).Scan(&fingerprint, &configuration, &offset, &ordinal)
	if err != nil {
		return err
	}
	if fingerprint != frontier.Fingerprint || configuration != o.Config ||
		offset != frontier.Offset || ordinal != frontier.RuleOrdinal {
		return errors.New("postgres checkpoint decision mismatch")
	}
	return nil
}

type decisionCandidate struct {
	value    detection.Candidate
	alert    *contracts.Alert
	entity   string
	cooldown time.Duration
}

func (s *DetectionDecisionStore) decodeCandidate(o DetectionOwnership,
	c detection.Candidate) (decisionCandidate, error) {
	raw := c.CanonicalBytes()
	if len(raw) == 0 || len(raw) > maxDecisionPayload || c.Source() != o.Source ||
		c.Offset() < 0 || c.TriggerOccurredAt().IsZero() {
		return decisionCandidate{}, errors.New("invalid decision candidate")
	}
	a, err := contracts.DecodeAlert(bytes.NewReader(raw))
	if err != nil {
		return decisionCandidate{}, err
	}
	if err = a.Validate(); err != nil {
		return decisionCandidate{}, err
	}
	canonical, err := json.Marshal(a)
	if err != nil || !bytes.Equal(raw, canonical) || a.Status != "open" || a.DetectedAt != nil {
		return decisionCandidate{}, errors.New("noncanonical decision candidate")
	}
	entity, ordinal := decisionEntity(a)
	if entity == nil || ordinal != c.RuleOrdinal() ||
		a.AlertID != contracts.DedupeKey(a.TenantID, a.RuleID, entity, a.WindowStart.Time) ||
		c.TriggerOccurredAt().Before(a.WindowStart.Time) || !c.TriggerOccurredAt().Before(a.WindowEnd.Time) {
		return decisionCandidate{}, errors.New("invalid decision identity or trigger")
	}
	immutable := *a
	immutable.Status = ""
	content, _ := json.Marshal(&immutable)
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != c.Fingerprint() {
		return decisionCandidate{}, errors.New("candidate fingerprint mismatch")
	}
	for _, rule := range s.rules.Rules {
		if rule.ID == a.RuleID && rule.Enabled && rule.Version == a.RuleVersion {
			components, _ := json.Marshal(entity)
			return decisionCandidate{value: c, alert: a, entity: string(components), cooldown: rule.Cooldown}, nil
		}
	}
	return decisionCandidate{}, errors.New("candidate rule mismatch")
}

func decisionEntity(a *contracts.Alert) ([]string, int) {
	switch a.RuleID {
	case "AUTH_BRUTE_FORCE", "PORT_SCAN":
		ip, second, ok := strings.Cut(a.EntityKey, "|")
		if !ok || ip == "" || second == "" {
			return nil, -1
		}
		ordinal := 0
		if a.RuleID == "PORT_SCAN" {
			ordinal = 1
		}
		return []string{ip, second}, ordinal
	case "REQUEST_FLOOD":
		return []string{a.EntityKey}, floodDecisionOrdinal
	case "DENY_SPIKE":
		return []string{a.EntityKey}, denyDecisionOrdinal
	}
	return nil, -1
}

// Decide returns the immutable first allow/suppress and stored canonical bytes.
func (s *DetectionDecisionStore) Decide(ctx context.Context, o DetectionOwnership,
	c detection.Candidate) (DetectionDecision, error) {
	if err := s.validateOwner(o); err != nil {
		return DetectionDecision{}, err
	}
	candidate, err := s.decodeCandidate(o, c)
	if err != nil {
		return DetectionDecision{}, err
	}
	var result DetectionDecision
	err = s.transaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
		f, lockErr := lockedDecisionFence(ctx, tx, o)
		if lockErr != nil {
			return lockErr
		}
		if !f.matches(o) {
			return errors.New("detector ownership fence lost")
		}
		stored, found, findErr := findDecision(ctx, tx, o, candidate)
		if findErr != nil {
			return findErr
		}
		if found {
			result = stored
		} else {
			if !positionBefore(f.offset, f.ordinal, c.Offset(), c.RuleOrdinal()) {
				return errors.New("missing older immutable decision")
			}
			result, findErr = s.createDecision(ctx, tx, o, candidate)
			if findErr != nil {
				return findErr
			}
		}
		// Observe only a fully prepared transaction, while the fence/cursor
		// locks and uncommitted immutable row still belong to this transaction.
		return s.observeDecision(ctx, "before-decision-commit", o, c)
	})
	if err != nil {
		return DetectionDecision{}, err
	}
	// transaction has committed AND completed independent rollback/release.
	// A failed post-commit observation is uncertain to the caller: return no
	// publication authorization; immutable lookup resolves the durable outcome.
	if err = s.observeDecision(ctx, "after-decision-commit-before-publish", o, c); err != nil {
		return DetectionDecision{}, errors.New("postgres decision commit observation failed; replay required")
	}
	return result, nil
}

func (s *DetectionDecisionStore) observeDecision(ctx context.Context, boundary string,
	o DetectionOwnership, c detection.Candidate) error {
	if s.boundary == nil {
		return nil
	}
	return s.boundary(ctx, boundary, o.Source, c.Offset())
}

func findDecision(ctx context.Context, tx pgx.Tx, o DetectionOwnership,
	c decisionCandidate) (DetectionDecision, bool, error) {
	var result DetectionDecision
	var fingerprint, configuration, topic, group string
	var partition int32
	var offset int64
	var ordinal int
	var trigger time.Time
	err := tx.QueryRow(ctx, `SELECT allowed,canonical_payload,immutable_fingerprint,config_fingerprint,
		topic,partition_id,source_offset,rule_ordinal,trigger_occurred_at,group_id FROM detection_decisions
		WHERE alert_id=$1`, c.alert.AlertID).
		Scan(&result.Allowed, &result.Canonical, &fingerprint, &configuration,
			&topic, &partition, &offset, &ordinal, &trigger, &group)
	if errors.Is(err, pgx.ErrNoRows) {
		return DetectionDecision{}, false, nil
	}
	if err != nil {
		return DetectionDecision{}, false, err
	}
	if !bytes.Equal(result.Canonical, c.value.CanonicalBytes()) || fingerprint != c.value.Fingerprint() ||
		configuration != o.Config || group != o.Group || topic != o.Source.Topic || partition != o.Source.Partition ||
		offset != c.value.Offset() || ordinal != c.value.RuleOrdinal() ||
		!trigger.Equal(c.value.TriggerOccurredAt().Time) {
		return DetectionDecision{}, false, errors.New("immutable decision reconstruction mismatch")
	}
	return result, true, nil
}

func (s *DetectionDecisionStore) createDecision(ctx context.Context, tx pgx.Tx, o DetectionOwnership,
	c decisionCandidate) (DetectionDecision, error) {
	a := c.alert
	_, err := tx.Exec(ctx, `INSERT INTO detection_cooldown_cursors
		(group_id,config_fingerprint,tenant_id,rule_id,entity_components,topic,partition_id)
		VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, o.Group, o.Config, a.TenantID, a.RuleID, c.entity,
		o.Source.Topic, o.Source.Partition)
	if err != nil {
		return DetectionDecision{}, err
	}
	var previous, next *time.Time
	var topic string
	var partition int32
	err = tx.QueryRow(ctx, `SELECT max_candidate_time,next_eligible_at,topic,partition_id FROM detection_cooldown_cursors
		WHERE group_id=$1 AND config_fingerprint=$2 AND tenant_id=$3 AND rule_id=$4 AND entity_components=$5
		FOR UPDATE`, o.Group, o.Config, a.TenantID, a.RuleID, c.entity).Scan(&previous, &next, &topic, &partition)
	if err != nil {
		return DetectionDecision{}, err
	}
	if topic != o.Source.Topic || partition != o.Source.Partition {
		return DetectionDecision{}, errors.New("entity source changed")
	}
	var maxTime, eligible time.Time
	if previous != nil {
		maxTime = *previous
		eligible = *next
	}
	allowed, clock, eligible := detection.Cooldown(maxTime, eligible, c.value.TriggerOccurredAt().Time, c.cooldown)
	_, err = tx.Exec(ctx, `INSERT INTO detection_decisions (alert_id,group_id,tenant_id,rule_id,rule_version,
		entity_components,config_fingerprint,allowed,trigger_occurred_at,effective_clock,topic,partition_id,
		source_offset,rule_ordinal,canonical_payload,immutable_fingerprint,decision_generation)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, a.AlertID, o.Group, a.TenantID, a.RuleID,
		a.RuleVersion, c.entity, o.Config, allowed, c.value.TriggerOccurredAt().Time, clock,
		o.Source.Topic, o.Source.Partition,
		c.value.Offset(), c.value.RuleOrdinal(), c.value.CanonicalBytes(), c.value.Fingerprint(), o.Generation)
	if err != nil {
		return DetectionDecision{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE detection_cooldown_cursors SET max_candidate_time=$6,next_eligible_at=$7
		WHERE group_id=$1 AND config_fingerprint=$2 AND tenant_id=$3 AND rule_id=$4 AND entity_components=$5`,
		o.Group, o.Config, a.TenantID, a.RuleID, c.entity, clock, eligible)
	if err != nil {
		return DetectionDecision{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE detection_partition_fences SET frontier_offset=$4,frontier_ordinal=$5,
		drained=false,drain_checkpoint=NULL
		WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition,
		c.value.Offset(), c.value.RuleOrdinal())
	if err != nil {
		return DetectionDecision{}, err
	}
	return DetectionDecision{Allowed: allowed, Canonical: c.value.CanonicalBytes()}, nil
}
