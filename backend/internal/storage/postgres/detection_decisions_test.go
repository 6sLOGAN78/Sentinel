package postgres //nolint:testpackage // inject the transaction boundary and inspect SQL order.

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
	allowed           bool
	commitErr         error
	storedGroup       string
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
		group := tx.owner.Group
		if tx.storedGroup != "" {
			group = tx.storedGroup
		}
		return decisionRow{[]any{tx.allowed, tx.candidate.CanonicalBytes(), tx.candidate.Fingerprint(),
			tx.owner.Config, tx.candidate.Source().Topic, tx.candidate.Source().Partition,
			tx.candidate.Offset(), tx.candidate.RuleOrdinal(), tx.candidate.TriggerOccurredAt().Time, group}}
	default:
		tx.t.Fatalf("matching replay consulted mutable state: %s", sql)
		return nil
	}
}

func (tx *decisionTx) Commit(context.Context) error { tx.committed = true; return tx.commitErr }
func (tx *decisionTx) Rollback(ctx context.Context) error {
	tx.rolled = true
	require.NoError(tx.t, ctx.Err(), "cleanup must have an independent live context")
	deadline, ok := ctx.Deadline()
	require.True(tx.t, ok)
	require.Positive(tx.t, time.Until(deadline))
	require.LessOrEqual(tx.t, time.Until(deadline), time.Second)
	return nil
}

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
	owner := DetectionOwnership{Group: "detector", Incarnation: DetectionGroupIncarnation("detector"),
		Member: "member", Generation: 3, Token: 1,
		Source: candidate.Source(), Config: rules.Fingerprint()}
	allowTx := &decisionTx{t: t, owner: owner, candidate: candidate, allowed: true}
	store := NewDetectionDecisionStore(nil, config.DetectorPGConfig{AcquireTimeout: time.Second,
		TransactionTimeout: 5 * time.Second, LockTimeout: time.Second, StatementTimeout: 2 * time.Second,
		CleanupTimeout: time.Second}, rules)
	store.begin = func(context.Context) (pgx.Tx, func(), error) { return allowTx, func() {}, nil }
	allowedDecision, allowErr := store.Decide(t.Context(), owner, candidate)
	require.NoError(t, allowErr, "durable immutable decision replay is absent")
	require.True(t, allowedDecision.Allowed)
	require.Equal(t, candidate.CanonicalBytes(), allowedDecision.Canonical)
	require.True(t, allowTx.committed)
	require.True(t, allowTx.rolled, "explicit bounded rollback finalizer must run even after commit")
	require.Len(t, allowTx.queries, 3, "timeouts, locked fence, immutable decision only")
	t.Run("old suppression does not become allow", func(t *testing.T) {
		tx := &decisionTx{t: t, owner: owner, candidate: candidate}
		store.begin = func(context.Context) (pgx.Tx, func(), error) { return tx, func() {}, nil }
		decision, err := store.Decide(t.Context(), owner, candidate)
		require.NoError(t, err)
		require.False(t, decision.Allowed)
		require.Equal(t, candidate.CanonicalBytes(), decision.Canonical)
		require.Len(t, tx.queries, 3)
	})
	t.Run("uncertain commit returns no successful decision", func(t *testing.T) {
		tx := &decisionTx{t: t, owner: owner, candidate: candidate, allowed: true, commitErr: context.DeadlineExceeded}
		store.begin = func(context.Context) (pgx.Tx, func(), error) { return tx, func() {}, nil }
		decision, err := store.Decide(t.Context(), owner, candidate)
		require.ErrorContains(t, err, "commit uncertain")
		require.Empty(t, decision.Canonical)
		require.True(t, tx.rolled)
	})
	t.Run("globally existing foreign group fails before cursor", func(t *testing.T) {
		tx := &decisionTx{t: t, owner: owner, candidate: candidate, allowed: true, storedGroup: "another-group"}
		store.begin = func(context.Context) (pgx.Tx, func(), error) { return tx, func() {}, nil }
		_, err := store.Decide(t.Context(), owner, candidate)
		require.Error(t, err)
		require.Len(t, tx.queries, 3)
		require.False(t, tx.committed)
		require.True(t, tx.rolled)
	})
}

func TestDetectionDecisionBoundaries(t *testing.T) {
	rules, candidate := transactionCandidate(t)
	o := DetectionOwnership{Group: "detector", Incarnation: DetectionGroupIncarnation("detector"),
		Member: "member", Generation: 3, Token: 1, Source: candidate.Source(), Config: rules.Fingerprint()}
	for _, stop := range []string{"", "before-decision-commit", "after-decision-commit-before-publish"} {
		t.Run("stop="+stop, func(t *testing.T) {
			tx := &decisionTx{t: t, owner: o, candidate: candidate, allowed: true}
			released := false
			var observed []string
			store := NewDetectionDecisionStore(nil, config.DetectorPGConfig{TransactionTimeout: time.Second,
				CleanupTimeout: time.Second}, rules,
				func(ctx context.Context, boundary string, source detection.TopicPartition, offset int64) error {
					require.NoError(t, ctx.Err())
					require.Equal(t, candidate.Source(), source)
					require.Equal(t, candidate.Offset(), offset)
					observed = append(observed, boundary)
					if boundary == "before-decision-commit" {
						require.False(t, tx.committed)
						require.False(t, released)
					} else {
						require.True(t, tx.committed)
						require.True(t, tx.rolled)
						require.True(t, released, "post-commit hook must not retain a PG connection across Kafka")
					}
					if boundary == stop {
						return context.DeadlineExceeded
					}
					return nil
				})
			store.begin = func(context.Context) (pgx.Tx, func(), error) {
				return tx, func() { released = true }, nil
			}
			decision, err := store.Decide(t.Context(), o, candidate)
			if stop == "" {
				require.NoError(t, err)
				require.Equal(t, []string{"before-decision-commit", "after-decision-commit-before-publish"}, observed)
				require.Equal(t, candidate.CanonicalBytes(), decision.Canonical)
			} else {
				require.Error(t, err, "failed boundary must not authorize publication")
				require.Empty(t, decision.Canonical)
				require.Equal(t, stop != "before-decision-commit", tx.committed)
			}
			require.True(t, tx.rolled)
			require.True(t, released)
		})
	}
}
