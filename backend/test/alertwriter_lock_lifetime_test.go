//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

func TestAlertWriterLockLifetime(t *testing.T) {
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	defer db.Close()
	store := storagepostgres.NewAlertStore(db.Pool, 10*time.Second)
	t.Run("requires live finite caller budget", func(t *testing.T) {
		canceled, cancel := context.WithTimeout(t.Context(), time.Second)
		cancel()
		expired, cancelExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		defer cancelExpired()
		overflow, cancelOverflow := context.WithTimeout(t.Context(), time.Duration(1<<31)*time.Millisecond)
		defer cancelOverflow()
		for _, invalid := range []context.Context{context.Background(), canceled, expired, overflow} {
			tx, beginErr := store.BeginLocked(invalid, uuid.NewString())
			if tx != nil {
				rollbackLifetimeTx(tx)
			}
			require.ErrorContains(t, beginErr, "live bounded operation deadline")
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	id := uuid.NewString()
	holder, err := store.BeginLocked(ctx, id)
	require.NoError(t, err)
	defer rollbackLifetimeTx(holder)
	var statement, lock, transaction, idle float64
	require.NoError(t, holder.QueryRow(ctx, `SELECT
		EXTRACT(EPOCH FROM current_setting('statement_timeout')::interval),
		EXTRACT(EPOCH FROM current_setting('lock_timeout')::interval),
		EXTRACT(EPOCH FROM current_setting('transaction_timeout')::interval),
		EXTRACT(EPOCH FROM current_setting('idle_in_transaction_session_timeout')::interval)`).
		Scan(&statement, &lock, &transaction, &idle))
	require.InDelta(t, 5.0, statement, 0.001)
	require.InDelta(t, 5.0, lock, 0.001)
	require.InDelta(t, 11.0, transaction, 0.1)
	require.InDelta(t, transaction, idle, 0.001)
	// Model sequential bounded native work still in flight after PG's old
	// five-second limit. The competing lock operation is itself bounded.
	require.NoError(t, lockLifetimeDelay(ctx, 6*time.Second))
	contenderCtx, cancelContender := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancelContender()
	contender, lockErr := store.BeginLocked(contenderCtx, id)
	if contender != nil {
		rollbackLifetimeTx(contender)
	}
	require.Error(t, lockErr, "PG must not release the winner lock while bounded CH work can still acknowledge")
	_, err = holder.Exec(ctx, "SELECT 1")
	require.NoError(t, err, "whole transaction and idle lifetimes must cover the caller's critical section")
}

func rollbackLifetimeTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

// Native stores do every read/write. The controlled delays reproduce two
// individually valid five-second CH operations inside the default ten-second
// persistence attempt; input delivery alone is replaced by a one-record poll.
type delayedWinnerStore struct {
	*storageclickhouse.AlertStore
	insertEntered chan struct{}
	acknowledged  chan struct{}
}

func (s *delayedWinnerStore) Get(ctx context.Context, tenant, id string) (*contracts.Alert, error) {
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := lockLifetimeDelay(op, 4200*time.Millisecond); err != nil {
		return nil, err
	}
	return s.AlertStore.Get(op, tenant, id)
}

func (s *delayedWinnerStore) InsertBatch(ctx context.Context, alerts []*contracts.Alert) error {
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	close(s.insertEntered)
	if err := lockLifetimeDelay(op, 4200*time.Millisecond); err != nil {
		return err
	}
	if err := s.AlertStore.InsertBatch(op, alerts); err != nil {
		return err
	}
	close(s.acknowledged)
	return nil
}

func lockLifetimeDelay(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type singleAlertInput struct {
	record    *kgo.Record
	cancel    context.CancelFunc
	committed bool
}

func (c *singleAlertInput) PollRecords(ctx context.Context, _ int) ([]*kgo.Record, error) {
	if c.record != nil {
		record := c.record
		c.record = nil
		return []*kgo.Record{record}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *singleAlertInput) CommitRecords(context.Context, ...*kgo.Record) error {
	c.committed = true
	c.cancel()
	return nil
}

func (*singleAlertInput) AllowRebalance() {}

func startLockLifetimeWriter(t *testing.T, f *alertReplayFixture, alert *contracts.Alert,
	detections alertwriter.DetectionStore, workflow ...alertwriter.WorkflowStore) (<-chan error, *singleAlertInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	raw, err := json.Marshal(alert)
	require.NoError(t, err)
	input := &singleAlertInput{cancel: cancel, record: &kgo.Record{Topic: f.kafka.AlertsTopic,
		Key: []byte(alert.TenantID + "|" + alert.AlertID), Value: raw}}
	settings := f.settings
	settings.OperationTimeout, settings.ShutdownTimeout = 10*time.Second, 20*time.Second
	var store alertwriter.WorkflowStore = storagepostgres.NewAlertStore(f.db.Pool, settings.OperationTimeout)
	if len(workflow) != 0 {
		store = workflow[0]
	}
	done := make(chan error, 1)
	go func() {
		done <- alertwriter.New(input, detections, store, settings, zerolog.Nop()).WithQuarantine(f.dlq).Run(ctx)
	}()
	return done, input
}

// Hold the local synchronous return after a real native acknowledgement. This
// checks cancellation ordering only, not remote abort after a disconnected send.
type returningDetectionStore struct {
	*storageclickhouse.AlertStore
	acknowledged chan struct{}
	release      chan struct{}
	returned     chan struct{}
}

func (s *returningDetectionStore) InsertBatch(ctx context.Context, alerts []*contracts.Alert) error {
	if err := s.AlertStore.InsertBatch(ctx, alerts); err != nil {
		return err
	}
	close(s.acknowledged)
	select {
	case <-s.release:
	case <-time.After(time.Second):
	}
	close(s.returned)
	return nil
}

type observedWorkflowStore struct {
	*storagepostgres.AlertStore
	returned <-chan struct{}
	rollback chan bool
}

func (s *observedWorkflowStore) BeginLocked(ctx context.Context, id string) (pgx.Tx, error) {
	tx, err := s.AlertStore.BeginLocked(ctx, id)
	if err != nil {
		return nil, err
	}
	return &observedLifetimeTx{Tx: tx, returned: s.returned, rollback: s.rollback}, nil
}

type observedLifetimeTx struct {
	pgx.Tx
	returned <-chan struct{}
	rollback chan bool
}

func (tx *observedLifetimeTx) Rollback(ctx context.Context) error {
	returned := false
	select {
	case <-tx.returned:
		returned = true
	default:
	}
	tx.rollback <- returned
	return tx.Tx.Rollback(ctx)
}

func TestAlertWriterCancellationLockLifetime(t *testing.T) {
	f := newAlertReplayFixture(t)
	detections := &returningDetectionStore{AlertStore: storageclickhouse.NewAlertStore(f.ch),
		acknowledged: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{})}
	store := storagepostgres.NewAlertStore(f.db.Pool, 10*time.Second)
	observed := &observedWorkflowStore{AlertStore: store, returned: detections.returned, rollback: make(chan bool, 1)}
	done, input := startLockLifetimeWriter(t, f, f.alert, detections, observed)
	select {
	case <-detections.acknowledged:
	case <-time.After(5 * time.Second):
		t.Fatal("native acknowledgement was not observed")
	}
	input.cancel()
	contenderCtx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	contender, err := store.BeginLocked(contenderCtx, f.alert.AlertID)
	if contender != nil {
		rollbackLifetimeTx(contender)
	}
	require.Error(t, err, "cancellation must not release a lock while the native call has not returned")
	select {
	case <-observed.rollback:
		t.Fatal("rollback began before the synchronous native return")
	default:
	}
	close(detections.release)
	require.NoError(t, <-done)
	require.True(t, <-observed.rollback, "independent rollback must follow local return/ack boundary")
	require.False(t, input.committed, "canceled persistence must not commit its source")
	ctx, cancelAfter := context.WithTimeout(t.Context(), time.Second)
	defer cancelAfter()
	after, err := store.BeginLocked(ctx, f.alert.AlertID)
	require.NoError(t, err, "independent cleanup must eventually release the lock")
	rollbackLifetimeTx(after)
	stored, err := detections.Get(ctx, f.alert.TenantID, f.alert.AlertID)
	require.NoError(t, err)
	require.Equal(t, f.alert.Summary, stored.Summary, "the acknowledged native effect really exists")
	var workflows int
	require.NoError(t, f.db.Pool.QueryRow(ctx,
		"SELECT count(*) FROM alert_workflows WHERE alert_id=$1", f.alert.AlertID).Scan(&workflows))
	require.Zero(t, workflows)
}

func TestAlertWriterDifferingPayloadLockLifetime(t *testing.T) {
	f := newAlertReplayFixture(t)
	store := storageclickhouse.NewAlertStore(f.ch)
	delayed := &delayedWinnerStore{AlertStore: store,
		insertEntered: make(chan struct{}), acknowledged: make(chan struct{})}
	first, firstInput := startLockLifetimeWriter(t, f, f.alert, delayed)
	select {
	case <-delayed.insertEntered:
	case <-time.After(8 * time.Second):
		t.Fatal("first writer never entered its native insert phase")
	}
	other := *f.alert
	other.Summary = "different immutable evidence from a concurrent writer"
	second, secondInput := startLockLifetimeWriter(t, f, &other, store)
	var firstErr, secondErr error
	var wg sync.WaitGroup
	wg.Go(func() { firstErr = <-first })
	wg.Go(func() { secondErr = <-second })
	wg.Wait()
	// Inspect physical variants, not deduplicated identity counts: a failed
	// holder must never leave a conflicting durable CH orphan behind a winner.
	var payloads, fingerprints []string
	require.NoError(t, f.ch.QueryRow(t.Context(), `SELECT groupUniqArray(canonical_payload),
		groupUniqArray(immutable_fingerprint) FROM alerts WHERE alert_id={id:String}`,
		driverclickhouse.Named("id", f.alert.AlertID)).Scan(&payloads, &fingerprints))
	wantRaw, err := json.Marshal(f.alert)
	require.NoError(t, err)
	wantFingerprint, err := alertwriter.ImmutableFingerprint(f.alert)
	require.NoError(t, err)
	require.Equal(t, []string{string(wantRaw)}, payloads,
		"every physical CH representation must belong to the lock holder")
	require.Equal(t, []string{wantFingerprint}, fingerprints)
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.True(t, firstInput.committed)
	require.True(t, secondInput.committed, "conflicting source may commit only after durable quarantine")
	var fingerprint string
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT immutable_fingerprint FROM alert_workflows WHERE alert_id=$1", f.alert.AlertID).Scan(&fingerprint))
	require.Equal(t, wantFingerprint, fingerprint)
	select {
	case <-delayed.acknowledged:
	default:
		t.Fatal("first native CH acknowledgement was not observed")
	}
}
