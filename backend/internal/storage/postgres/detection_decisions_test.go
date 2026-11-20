package postgres //nolint:testpackage // inject the transaction boundary and inspect SQL order.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

type frontierTx struct {
	pgx.Tx
	row     pgx.Row
	queries int
}

func (tx *frontierTx) QueryRow(context.Context, string, ...any) pgx.Row {
	tx.queries++
	return tx.row
}

type missingDecisionRow struct{}

func (missingDecisionRow) Scan(...any) error { return pgx.ErrNoRows }

func TestDetectionDecisionFrontier(t *testing.T) {
	rules, candidate := transactionCandidate(t)
	var alert contracts.Alert
	require.NoError(t, json.Unmarshal(candidate.CanonicalBytes(), &alert))
	o := DetectionOwnership{Group: "detector", Config: rules.Fingerprint(), Source: candidate.Source()}
	frontier := detection.DecisionFrontier{Offset: candidate.Offset(), RuleOrdinal: candidate.RuleOrdinal(),
		AlertID: alert.AlertID, Fingerprint: candidate.Fingerprint()}
	for _, scenario := range []string{"none", "equal", "ahead", "missing", "fingerprint", "config",
		"offset", "ordinal", "fence behind", "equal offset older ordinal"} {
		t.Run(scenario, func(t *testing.T) {
			marker := frontier
			f := decisionFence{offset: marker.Offset, ordinal: marker.RuleOrdinal}
			values := []any{marker.Fingerprint, o.Config, marker.Offset, marker.RuleOrdinal}
			tx := &frontierTx{}
			switch scenario {
			case "none":
				marker = detection.DecisionFrontier{None: true}
			case "ahead":
				f.offset += 100
			case "missing":
				tx.row = missingDecisionRow{}
			case "fingerprint":
				values[0] = strings.Repeat("f", 64)
			case "config":
				values[1] = strings.Repeat("f", 64)
			case "offset":
				values[2] = marker.Offset + 1
			case "ordinal":
				values[3] = marker.RuleOrdinal + 1
			case "fence behind":
				f.offset--
			case "equal offset older ordinal":
				marker.RuleOrdinal++
			}
			if tx.row == nil {
				tx.row = decisionRow{values}
			}
			err := verifyDecisionFrontier(t.Context(), tx, o, f, marker)
			if scenario == "none" || scenario == "equal" || scenario == "ahead" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if scenario == "missing" {
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
			if scenario == "none" || scenario == "fence behind" || scenario == "equal offset older ordinal" {
				require.Zero(t, tx.queries)
			} else {
				require.Equal(t, 1, tx.queries)
			}
		})
	}
}

// classTx is a minimal transaction whose fence row and Exec failures are chosen
// by each classification case.
type classTx struct {
	pgx.Tx
	fence   []any
	execErr error
	rowErr  error
}

func (tx *classTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("SELECT 1"), tx.execErr
}

func (tx *classTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.rowErr != nil {
		return errorRow{tx.rowErr}
	}
	return decisionRow{tx.fence}
}

func (*classTx) Commit(context.Context) error   { return nil }
func (*classTx) Rollback(context.Context) error { return nil }

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }

func classStore(t *testing.T, tx *classTx) (*DetectionDecisionStore, DetectionOwnership) {
	t.Helper()
	_, candidate := transactionCandidate(t)
	// The shipped four-rule set, so checkpoints carry a complete manifest.
	raw, err := os.ReadFile("../../../../config/rules.yaml")
	require.NoError(t, err)
	rules, err := detection.LoadRules(bytes.NewReader(raw))
	require.NoError(t, err)
	store := NewDetectionDecisionStore(nil, config.DetectorPGConfig{AcquireTimeout: time.Second,
		TransactionTimeout: 5 * time.Second, LockTimeout: time.Second, StatementTimeout: 2 * time.Second,
		CleanupTimeout: time.Second}, rules)
	store.begin = func(context.Context) (pgx.Tx, func(), error) { return tx, func() {}, nil }
	owner := DetectionOwnership{Group: "detector", Incarnation: DetectionGroupIncarnation("detector"),
		Member: "member-b", Generation: 5, Token: 7, Source: candidate.Source(), Config: rules.Fingerprint()}
	return store, owner
}

func fenceRow(o DetectionOwnership) []any {
	return []any{o.Incarnation, o.Member, o.Generation, o.tokenValue(), o.Config, int64(-1), -1}
}

// Register keeps two distinct failure classes: a fence written by a newer or
// equal-generation peer is superseded ownership; a fence for another group
// incarnation or rules configuration is an invariant violation.
func TestRegisterStaleClassification(t *testing.T) {
	none := detection.DecisionFrontier{None: true}
	cases := []struct {
		name  string
		edit  func(row []any)
		class error
	}{
		{"current owner", func([]any) {}, nil},
		{"older fence generation", func(row []any) { row[2] = int32(4) }, nil},
		{"incarnation mismatch", func(row []any) { row[0] = DetectionGroupIncarnation("other") }, ErrDecisionInvariant},
		{"config mismatch", func(row []any) { row[4] = strings.Repeat("c", 64) }, ErrDecisionInvariant},
		{"newer fence generation", func(row []any) { row[2] = int32(6) }, ErrOwnershipSuperseded},
		{"same generation other member", func(row []any) { row[1] = "member-a" }, ErrOwnershipSuperseded},
		{"same generation higher token", func(row []any) { row[3] = int64(8) }, ErrOwnershipSuperseded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := &classTx{}
			store, owner := classStore(t, tx)
			tx.fence = fenceRow(owner)
			tc.edit(tx.fence)
			err := store.Register(t.Context(), owner, none)
			if tc.class == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.class)
			for _, other := range []error{ErrOwnershipSuperseded, ErrDecisionInvariant, ErrDecisionTransient} {
				if !errors.Is(tc.class, other) {
					require.NotErrorIs(t, err, other)
				}
			}
			require.Contains(t, err.Error(), tc.class.Error())
		})
	}
}

// Every fence-mismatch site is superseded ownership, while a lock failure on
// the same fence read keeps its SQLSTATE class instead.
func TestOwnershipLostClassification(t *testing.T) {
	_, candidate := transactionCandidate(t)
	sites := map[string]func(*DetectionDecisionStore, DetectionOwnership) error{
		"decide": func(s *DetectionDecisionStore, o DetectionOwnership) error {
			_, err := s.Decide(t.Context(), o, candidate)
			return err
		},
		"bind config manifest": func(s *DetectionDecisionStore, o DetectionOwnership) error {
			return s.bindConfigManifest(t.Context(), o, nil)
		},
		"checkpoint committed": func(s *DetectionDecisionStore, o DetectionOwnership) error {
			state := detection.NewPartitionState(s.rules)
			require.NoError(t, state.BeginAt(0))
			cp, err := state.Checkpoint()
			require.NoError(t, err)
			return s.CheckpointCommitted(t.Context(), o, cp)
		},
	}
	for name, call := range sites {
		t.Run(name+"/fence moved", func(t *testing.T) {
			tx := &classTx{}
			store, owner := classStore(t, tx)
			tx.fence = fenceRow(owner)
			tx.fence[2] = owner.Generation + 1
			err := call(store, owner)
			require.ErrorIs(t, err, ErrOwnershipSuperseded)
			require.NotErrorIs(t, err, ErrDecisionInvariant)
		})
		t.Run(name+"/lock timeout", func(t *testing.T) {
			tx := &classTx{rowErr: &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}}
			store, owner := classStore(t, tx)
			err := call(store, owner)
			require.ErrorIs(t, err, ErrDecisionTransient)
			require.NotErrorIs(t, err, ErrOwnershipSuperseded)
		})
	}
}

// transaction maps every body failure to exactly one class and never copies
// driver Detail/Message or row values into the returned error text.
func TestDecisionTransactionClassifier(t *testing.T) {
	const marker = "row-value-MARKER-198.51.100.7"
	leaky := func(code string) error {
		return &pgconn.PgError{Code: code, Severity: "ERROR", Message: "message " + marker,
			Detail: "Key (tenant_id)=(" + marker + ") already exists", Hint: marker, TableName: marker}
	}
	cases := []struct {
		name    string
		err     error
		class   error
		contain string
	}{
		{"lock timeout", leaky("55P03"), ErrDecisionTransient, "sqlstate=55P03"},
		{"serialization", leaky("40001"), ErrDecisionTransient, "sqlstate=40001"},
		{"deadlock", leaky("40P01"), ErrDecisionTransient, "sqlstate=40P01"},
		{"statement timeout", leaky("57014"), ErrDecisionTransient, "sqlstate=57014"},
		{"unique violation", leaky("23505"), ErrDecisionInvariant, "sqlstate=23505"},
		{"wrapped pg error", fmt.Errorf("scan: %w", leaky("40P01")), ErrDecisionTransient, "sqlstate=40P01"},
		{"package fault", decisionFaultError("missing older immutable decision"), ErrDecisionInvariant,
			"missing older immutable decision"},
		{"no rows", pgx.ErrNoRows, ErrDecisionInvariant, "no rows"},
		{"deadline", context.DeadlineExceeded, ErrDecisionInvariant, "deadline exceeded"},
		{"foreign text", errors.New("json: " + marker), ErrDecisionInvariant, "unclassified"},
		{"superseded", fmt.Errorf("%w: stale detector assignment", ErrOwnershipSuperseded),
			ErrOwnershipSuperseded, "stale detector assignment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := classStore(t, &classTx{})
			err := store.transaction(t.Context(), func(context.Context, pgx.Tx) error { return tc.err })
			require.ErrorIs(t, err, tc.class)
			require.Contains(t, err.Error(), tc.class.Error())
			require.Contains(t, err.Error(), tc.contain)
			require.NotContains(t, err.Error(), marker)
			require.NotContains(t, err.Error(), "transaction failed")
			var pgErr *pgconn.PgError
			require.NotErrorAs(t, err, &pgErr, "driver error must not be reachable from the class")
		})
	}
	t.Run("set_config failure is classified", func(t *testing.T) {
		store, _ := classStore(t, &classTx{execErr: leaky("57014")})
		err := store.transaction(t.Context(), func(context.Context, pgx.Tx) error { return nil })
		require.ErrorIs(t, err, ErrDecisionTransient)
		require.NotContains(t, err.Error(), marker)
	})
}
