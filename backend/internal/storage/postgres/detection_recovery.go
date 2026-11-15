package postgres

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/6sLOGAN78/sentinel/internal/detection"
)

const (
	recoveryCleanupTimeout = 2 * time.Second
	maxRecoveryOperator    = 128
	maxRecoveryReason      = 1024
	recoveryDigestHexLen   = 64
)

// ErrRecoveryAudit is the fixed audit failure. PostgreSQL error details can
// echo row values (operator, reason), so the cause is never wrapped.
var ErrRecoveryAudit = errors.New("detection history recovery audit failed")

// DetectionRecoveryStore records operator-acknowledged detection loss. It has
// no write path to detection_partition_fences; it only counts them.
type DetectionRecoveryStore struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewDetectionRecoveryStore bounds every audit transaction by timeout.
func NewDetectionRecoveryStore(pool *pgxpool.Pool, timeout time.Duration) *DetectionRecoveryStore {
	return &DetectionRecoveryStore{pool: pool, timeout: timeout}
}

func validRecoveryText(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value)
}

func (s *DetectionRecoveryStore) transaction(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	if s.pool == nil {
		return ErrRecoveryAudit
	}
	txCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return ErrRecoveryAudit
	}
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), recoveryCleanupTimeout)
		defer cancelCleanup()
		_ = tx.Rollback(cleanup)
	}()
	if err = fn(txCtx, tx); err != nil {
		return err
	}
	if tx.Commit(txCtx) != nil {
		return ErrRecoveryAudit
	}
	return nil
}

// RecordPending inserts one pending row per plan entry in a single
// transaction. A retried apply of the same digest inserts nothing new.
func (s *DetectionRecoveryStore) RecordPending(ctx context.Context, plan detection.RecoveryPlan,
	operator, reason string) error {
	if plan.Group == "" || len(plan.Entries) == 0 || !validRecoveryText(operator, maxRecoveryOperator) ||
		!validRecoveryText(reason, maxRecoveryReason) {
		return errors.New("invalid detection history recovery audit")
	}
	digest := plan.Digest()
	return s.transaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, e := range plan.Entries {
			if _, err := tx.Exec(ctx, `INSERT INTO detection_history_recoveries
				(group_id, topic, partition_id, committed_offset, log_start, log_end, never_evaluated_from,
				 never_evaluated_to, open_windows_discarded, config_fingerprint, plan_digest, repinned_offset,
				 operator, reason, status)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'pending')
				ON CONFLICT (group_id, topic, partition_id, plan_digest) DO NOTHING`,
				plan.Group, e.Topic, e.Partition, e.Committed, e.Start, e.End, e.NeverEvaluatedFrom,
				e.NeverEvaluatedTo, e.OpenWindowsDiscarded, plan.ConfigFingerprint, digest, e.Repinned.Offset,
				operator, reason); err != nil {
				return ErrRecoveryAudit
			}
		}
		var rows int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM detection_history_recoveries
			WHERE group_id=$1 AND plan_digest=$2`, plan.Group, digest).Scan(&rows); err != nil || rows != len(plan.Entries) {
			return ErrRecoveryAudit
		}
		return nil
	})
}

// MarkApplied moves every pending row of the digest to applied in a single
// transaction and requires the digest to be fully applied afterwards.
func (s *DetectionRecoveryStore) MarkApplied(ctx context.Context, group, digest string) error {
	if group == "" || len(digest) != recoveryDigestHexLen {
		return errors.New("invalid detection history recovery digest")
	}
	return s.transaction(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE detection_history_recoveries SET status='applied', applied_at=now()
			WHERE group_id=$1 AND plan_digest=$2 AND status='pending'`, group, digest); err != nil {
			return ErrRecoveryAudit
		}
		var total, applied int
		if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status='applied')
			FROM detection_history_recoveries WHERE group_id=$1 AND plan_digest=$2`, group, digest).
			Scan(&total, &applied); err != nil || total == 0 || total != applied {
			return ErrRecoveryAudit
		}
		return nil
	})
}

// CountPartitionFences is a read-only count of the group's fences for topic.
func (s *DetectionRecoveryStore) CountPartitionFences(ctx context.Context, group, topic string) (int64, error) {
	if s.pool == nil {
		return 0, ErrRecoveryAudit
	}
	readCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var count int64
	if err := s.pool.QueryRow(readCtx, `SELECT count(*) FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2`, group, topic).Scan(&count); err != nil {
		return 0, ErrRecoveryAudit
	}
	return count, nil
}
