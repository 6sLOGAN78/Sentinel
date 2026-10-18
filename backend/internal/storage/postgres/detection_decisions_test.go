package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

type decisionRow struct{ values []any }

func (r decisionRow) Scan(dst ...any) error {
	for i, value := range r.values {
		reflect.ValueOf(dst[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

type decisionTx struct {
	pgx.Tx
	t                 *testing.T
	owner             DetectionOwnership
	candidate         detection.Candidate
	queries           []string
	committed, rolled bool
}

func (tx *decisionTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.queries = append(tx.queries, sql)
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (tx *decisionTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, sql)
	switch {
	case strings.Contains(sql, "FROM detection_partition_fences"):
		return decisionRow{[]any{tx.owner.Incarnation, tx.owner.Member, tx.owner.Generation, int64(tx.owner.Token),
			tx.owner.Config, int64(99), 3}}
	case strings.Contains(sql, "FROM detection_decisions"):
		return decisionRow{[]any{true, tx.candidate.CanonicalBytes(), tx.candidate.Fingerprint(),
			tx.owner.Config, tx.candidate.Source().Topic, tx.candidate.Source().Partition,
			tx.candidate.Offset(), tx.candidate.RuleOrdinal(), tx.candidate.TriggerOccurredAt().Time}}
	default:
		tx.t.Fatalf("matching replay consulted mutable state: %s", sql)
		return nil
	}
}

func (tx *decisionTx) Commit(context.Context) error   { tx.committed = true; return nil }
func (tx *decisionTx) Rollback(context.Context) error { tx.rolled = true; return nil }

func transactionCandidate(t *testing.T) (*detection.Rules, detection.Candidate) {
	t.Helper()
	rules := &detection.Rules{SchemaVersion: 1, Rules: []detection.Rule{{ID: "AUTH_BRUTE_FORCE", Enabled: true,
		Version: 1, Window: time.Second, Threshold: 1, Cooldown: time.Minute, Severity: "high"}}}
	state := detection.NewPartitionState(rules)
	ip, principal := "192.0.2.1", "principal|suffix"
	event := &contracts.Event{SchemaVersion: 1, TenantID: "demo", EventID: "00000000-0000-4000-8000-000000000001",
		EventType: "auth.failure", SourceIP: &ip, Principal: &principal,
		OccurredAt: contracts.NewTimestamp(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))}
	out, err := state.ApplySource(detection.TopicPartition{Topic: "events", Partition: 0}, 9, event)
	require.NoError(t, err)
	require.Len(t, out.Candidates, 1)
	return rules, out.Candidates[0]
}

// Matching replay must return before any mutable cooldown cursor access.
func TestDetectionDecisionTransaction(t *testing.T) {
	rules, candidate := transactionCandidate(t)
	owner := DetectionOwnership{Group: "detector", Incarnation: "stable", Member: "member", Generation: 3, Token: 1,
		Source: candidate.Source(), Config: rules.Fingerprint()}
	tx := &decisionTx{t: t, owner: owner, candidate: candidate}
	store := NewDetectionDecisionStore(nil, config.DetectorPGConfig{AcquireTimeout: time.Second,
		TransactionTimeout: 5 * time.Second, LockTimeout: time.Second, StatementTimeout: 2 * time.Second,
		CleanupTimeout: time.Second}, rules)
	store.begin = func(context.Context) (pgx.Tx, func(), error) { return tx, func() {}, nil }
	decision, err := store.Decide(t.Context(), owner, candidate)
	require.NoError(t, err, "durable immutable decision replay is absent")
	require.True(t, decision.Allowed)
	require.Equal(t, candidate.CanonicalBytes(), decision.Canonical)
	require.True(t, tx.committed)
	require.True(t, tx.rolled, "explicit bounded rollback finalizer must run even after commit")
	require.Len(t, tx.queries, 3, "timeouts, locked fence, immutable decision only")
}
