//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

// This is an explicitly controlled delayed-visibility model. It saves the
// accepted request, returns uncertainty, and exposes it only when release is
// called. It does not claim a stock ClickHouse server paused before publication.
type deferredReservationDetection struct {
	*storageclickhouse.AlertStore
	accepted []byte
}

func (d *deferredReservationDetection) InsertBatch(_ context.Context, alerts []*contracts.Alert) error {
	if len(alerts) != 1 || d.accepted != nil {
		return errors.New("unexpected second deferred request")
	}
	var err error
	d.accepted, err = json.Marshal(alerts[0])
	if err != nil {
		return err
	}
	return errors.New("controlled model: remote request accepted but not yet visible")
}

func (d *deferredReservationDetection) release(ctx context.Context) error {
	var a contracts.Alert
	if err := json.Unmarshal(d.accepted, &a); err != nil {
		return err
	}
	return d.AlertStore.InsertBatch(ctx, []*contracts.Alert{&a})
}

// An independent connection acquiring the same lock is affirmative evidence
// that A's transaction ended, rather than an elapsed-time inference.
func requireReservationTransactionEnded(t *testing.T, f *alertReplayFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := storagepostgres.NewAlertStore(f.db.Pool, time.Second)
	tx, err := store.BeginLocked(ctx, f.alert.AlertID)
	require.NoError(t, err, "independent connection must acquire A's per-ID transaction lock")
	require.NoError(t, tx.Rollback(ctx))
}

// B uses its own real Kafka source and a fresh default writer process, while A's
// group remains uncommitted. It is attempted before any A workflow can mask the
// reservation-only admission check.
func rejectIndependentReservationConflict(t *testing.T, f *alertReplayFixture) {
	t.Helper()
	other := independentReservationInput(t, f)
	observed := startObservedDLQ(t.Context(), t, other.kafka, other.kafka.DLQTopic)
	b := *f.alert
	b.Summary = "B differs from the reserved immutable winner"
	source := other.publish(t, &b)
	p := other.binary(t)
	require.Eventually(t, func() bool {
		records, err := observed.snapshot()
		return err == nil && records[poisonOrigin{source.Partition, source.Offset}] != nil
	}, 6*time.Second, 25*time.Millisecond, "B must be durably quarantined before A replay")
	records, err := observed.snapshot()
	require.NoError(t, err)
	quarantined := records[poisonOrigin{source.Partition, source.Offset}]
	require.Equal(t, source.Value, quarantined.Value)
	require.Equal(t, source.Key, quarantined.Key)
	require.Equal(t, other.kafka.AlertsTopic, testHeaderValue(quarantined.Headers, "dlq.origin.topic"))
	require.Equal(t, "validation_error", testHeaderValue(quarantined.Headers, "dlq.reason"))
	other.waitOffset(t, source.Offset+1)
	require.Zero(t, f.committed(t), "B must not acknowledge A's independent source")
	require.Zero(t, p.terminate(t), p.output.String())
}

// Every selected wrapper actually commits or rolls back its native transaction
// before returning an uncertain answer. A generic error-only fake could not
// distinguish these durable outcomes.
type reservationCommitFault struct {
	*storagepostgres.AlertStore
	phase    int
	target   int
	lands    bool
	injected bool
}

func (s *reservationCommitFault) BeginLocked(ctx context.Context, id string) (pgx.Tx, error) {
	tx, err := s.AlertStore.BeginLocked(ctx, id)
	if err != nil {
		return nil, err
	}
	s.phase++
	if s.phase == s.target {
		return &reservationCommitFaultTx{Tx: tx, owner: s}, nil
	}
	return tx, nil
}

type reservationCommitFaultTx struct {
	pgx.Tx
	owner *reservationCommitFault
}

func (tx *reservationCommitFaultTx) Commit(ctx context.Context) error {
	var err error
	if tx.owner.lands {
		err = tx.Tx.Commit(ctx)
	} else {
		err = tx.Rollback(ctx)
	}
	if err != nil {
		return err
	}
	tx.owner.injected = true
	return errors.New("test-owned native PG transaction outcome answer lost")
}

// These are characterization tests of Task 2's durable authority, not new RED
// claims. Removing reservation-before-effect or consulting workflow alone lets
// B insert while A is still invisible and fails the physical variant checks.
func TestAlertWriterReservationAmbiguity(t *testing.T) {
	t.Run("controlled delayed visibility with independent B before A release", func(t *testing.T) {
		f := newAlertReplayFixture(t)
		preserveReservationPhysicalRows(t, f)
		raw, err := json.Marshal(f.alert)
		require.NoError(t, err)
		store := storagepostgres.NewAlertStore(f.db.Pool, time.Second)
		detection := &deferredReservationDetection{AlertStore: storageclickhouse.NewAlertStore(f.ch)}
		f.publish(t, f.alert)
		require.Error(t, f.run(t, detection, store))
		require.Equal(t, raw, detection.accepted)
		requireReservationTransactionEnded(t, f)
		requireReservationState(t, f, raw, 0)
		requireReservationPhysical(t, f, nil, 0)
		require.Zero(t, f.committed(t))
		rejectIndependentReservationConflict(t, f)
		requireReservationState(t, f, raw, 0)
		requireReservationPhysical(t, f, nil, 0)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.NoError(t, detection.release(ctx))
		requireReservationPhysical(t, f, raw, 1)
		p := f.binary(t)
		f.waitOffset(t, 1)
		requireReservationState(t, f, raw, 1)
		requireReservationPhysical(t, f, raw, 1)
		require.Zero(t, p.terminate(t), p.output.String())
	})
	for _, tc := range []struct {
		name          string
		target        int
		lands         bool
		wantClaims    int
		wantWorkflows int
		wantPhysical  uint64
	}{
		{"reservation committed answer lost", 1, true, 1, 0, 0},
		{"reservation rolled back answer lost", 1, false, 0, 0, 0},
		{"workflow committed answer lost", 2, true, 1, 1, 1},
		{"workflow rolled back answer lost", 2, false, 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAlertReplayFixture(t)
			preserveReservationPhysicalRows(t, f)
			raw, err := json.Marshal(f.alert)
			require.NoError(t, err)
			fault := &reservationCommitFault{AlertStore: storagepostgres.NewAlertStore(f.db.Pool, time.Second),
				target: tc.target, lands: tc.lands}
			f.publish(t, f.alert)
			require.Error(t, f.run(t, storageclickhouse.NewAlertStore(f.ch), fault))
			require.True(t, fault.injected, "must reach the selected real native transaction outcome")
			requireReservationTransactionEnded(t, f)
			require.Zero(t, f.committed(t))
			var claims, workflows int
			require.NoError(t, f.db.Pool.QueryRow(t.Context(),
				"SELECT count(*) FROM alert_reservations WHERE alert_id=$1", f.alert.AlertID).Scan(&claims))
			require.NoError(t, f.db.Pool.QueryRow(t.Context(),
				"SELECT count(*) FROM alert_workflows WHERE alert_id=$1", f.alert.AlertID).Scan(&workflows))
			require.Equal(t, tc.wantClaims, claims)
			require.Equal(t, tc.wantWorkflows, workflows)
			requireReservationPhysical(t, f, raw, tc.wantPhysical)
			before := ""
			if workflows == 1 {
				_, err = f.db.Pool.Exec(t.Context(), `UPDATE alert_workflows
					SET status='resolved',assignee='triager',version=9 WHERE alert_id=$1`, f.alert.AlertID)
				require.NoError(t, err)
				before = f.workflow(t)
			}
			p := f.binary(t)
			f.waitOffset(t, 1)
			requireReservationState(t, f, raw, 1)
			requireReservationPhysical(t, f, raw, 1)
			if before != "" {
				require.Equal(t, before, f.workflow(t), "ambiguous workflow replay preserves seq and triage")
			}
			require.Zero(t, p.terminate(t), p.output.String())
		})
	}
}
