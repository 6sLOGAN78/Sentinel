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
	pool    *pgxpool.Pool
	budgets config.DetectorPGConfig
	rules   *detection.Rules
	begin   func(context.Context) (pgx.Tx, func(), error)
}

// NewDetectionDecisionStore uses the explicit bounded detector pool.
func NewDetectionDecisionStore(pool *pgxpool.Pool, budgets config.DetectorPGConfig,
	rules *detection.Rules) *DetectionDecisionStore {
	return &DetectionDecisionStore{pool: pool, budgets: budgets, rules: rules}
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
			return nil
		}
		if !positionBefore(f.offset, f.ordinal, c.Offset(), c.RuleOrdinal()) {
			return errors.New("missing older immutable decision")
		}
		result, findErr = s.createDecision(ctx, tx, o, candidate)
		return findErr
	})
	if err != nil {
		return DetectionDecision{}, err
	}
	return result, nil
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
	_, err = tx.Exec(ctx, `UPDATE detection_partition_fences SET frontier_offset=$4,frontier_ordinal=$5
		WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition,
		c.value.Offset(), c.value.RuleOrdinal())
	if err != nil {
		return DetectionDecision{}, err
	}
	return DetectionDecision{Allowed: allowed, Canonical: c.value.CanonicalBytes()}, nil
}
