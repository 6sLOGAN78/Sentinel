// Package postgres provides bounded insert-only workflow storage.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

const (
	queryTimeout    = 5 * time.Second
	rollbackTimeout = time.Second
)

// AlertStore creates workflows without mutating a winner on replay.
type AlertStore struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewAlertStore bounds native transactions independently of caller behavior.
func NewAlertStore(pool *pgxpool.Pool, timeout time.Duration) *AlertStore {
	return &AlertStore{pool: pool, timeout: min(max(timeout, time.Millisecond), queryTimeout)}
}

// BeginLocked acquires a transaction-scoped per-ID lock before any CH effect.
func (s *AlertStore) BeginLocked(ctx context.Context, id string) (pgx.Tx, error) {
	lifetime, err := alertTransactionLifetime(ctx)
	if err != nil {
		return nil, err
	}
	beginCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	tx, err := s.pool.Begin(beginCtx)
	if err != nil {
		return nil, err
	}
	limit := fmt.Sprintf("%dms", max(int64(1), s.timeout.Milliseconds()))
	if _, err = tx.Exec(beginCtx, `SELECT set_config('lock_timeout',$1,true),
		set_config('statement_timeout',$1,true),set_config('idle_in_transaction_session_timeout',$2,true),
		set_config('transaction_timeout',$2,true)`, limit, lifetime); err != nil {
		rollback(tx)
		return nil, err
	}
	digest := sha256.Sum256([]byte(id))
	// Hash collisions serialize unrelated identities; row uniqueness and full
	// fingerprint validation remain authoritative, so collisions cannot alias data.
	lockID := int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
	if _, err = tx.Exec(beginCtx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

// Compute before BEGIN, so acquisition/setup cannot shorten the server lease
// below the caller's full operation deadline plus independent rollback reserve.
// Cancellation does not release this lock: the writer rolls back only after its
// synchronous CH operation returns, including an uncertain acknowledgement.
func alertTransactionLifetime(ctx context.Context) (string, error) {
	deadline, bounded := ctx.Deadline()
	remaining := time.Until(deadline)
	// PostgreSQL integer timeout settings are milliseconds. Check before adding
	// cleanup and rounding up; neither an overflow nor zero may disable a lease.
	maximum := time.Duration(math.MaxInt32) * time.Millisecond
	if ctx.Err() != nil || !bounded || remaining <= 0 || remaining > maximum-rollbackTimeout {
		return "", errors.New("alert transaction requires a live bounded operation deadline")
	}
	lifetime := (remaining + rollbackTimeout + time.Millisecond - 1) / time.Millisecond
	return fmt.Sprintf("%dms", lifetime), nil
}

// Find reads the authoritative winner inside its already locked transaction.
func (*AlertStore) Find(ctx context.Context, tx pgx.Tx, id string) (string, string, error) {
	var tenant, fingerprint string
	err := tx.QueryRow(ctx, `SELECT tenant_id,immutable_fingerprint
		FROM alert_workflows WHERE alert_id=$1`, id).Scan(&tenant, &fingerprint)
	return tenant, fingerprint, err
}

// Ensure is deliberately insert-only: resolved workflow metadata and seq survive.
func (*AlertStore) Ensure(ctx context.Context, tx pgx.Tx, alert *contracts.Alert, fingerprint string) error {
	_, err := tx.Exec(ctx, `INSERT INTO alert_workflows (alert_id,tenant_id,created_at,updated_at,
		rule_id,rule_version,severity,entity_key,summary,event_count,window_start,window_end,immutable_fingerprint)
		VALUES ($1,$2,$3,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (alert_id) DO NOTHING`,
		alert.AlertID, alert.TenantID, alert.CreatedAt.Time, alert.RuleID, alert.RuleVersion, alert.Severity,
		alert.EntityKey, alert.Summary, alert.EventCount, alert.WindowStart.Time, alert.WindowEnd.Time, fingerprint)
	return err
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}
