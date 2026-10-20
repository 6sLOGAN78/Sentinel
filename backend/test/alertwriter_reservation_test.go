//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

// The read uses an independent connection while the writer owns its effect
// transaction. An uncommitted insert (or a fake commit) cannot satisfy it.
type committedReservationStore struct {
	*storageclickhouse.AlertStore
	pool        *pgxpool.Pool
	observed    bool
	observation error
}

func (s *committedReservationStore) InsertBatch(ctx context.Context, alerts []*contracts.Alert) error {
	a := alerts[0]
	var tenant, fingerprint string
	var payload []byte
	err := s.pool.QueryRow(ctx, `SELECT tenant_id,immutable_fingerprint,canonical_payload
		FROM alert_reservations WHERE alert_id=$1`, a.AlertID).Scan(&tenant, &fingerprint, &payload)
	if err != nil {
		s.observation = err
		return errors.New("CH insertion has no independently visible committed reservation")
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	fp, err := alertwriter.ImmutableFingerprint(a)
	if err != nil {
		return err
	}
	if tenant != a.TenantID || fingerprint != fp || string(payload) != string(raw) {
		s.observation = errors.New("CH payload differs from committed canonical reservation")
		return s.observation
	}
	s.observed = true
	return s.AlertStore.InsertBatch(ctx, alerts)
}

func runNativeReservation(t *testing.T, f *alertReplayFixture, a *contracts.Alert,
	detections alertwriter.DetectionStore, workflows alertwriter.WorkflowStore,
) (*singleAlertInput, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	input := &singleAlertInput{cancel: cancel, record: &kgo.Record{Topic: f.kafka.AlertsTopic,
		Key: []byte(a.TenantID + "|" + a.AlertID), Value: raw}}
	err = alertwriter.New(input, detections, workflows, f.settings, zerolog.Nop()).WithQuarantine(f.dlq).Run(ctx)
	return input, err
}

// Moving the reservation commit after the native CH call fails this independent
// visibility assertion before any physical effect can be acknowledged.
func TestAlertWriterReservation(t *testing.T) {
	t.Run("committed before native insert", func(t *testing.T) {
		f := newAlertReplayFixture(t)
		d := &committedReservationStore{AlertStore: storageclickhouse.NewAlertStore(f.ch), pool: f.db.Pool}
		input, err := runNativeReservation(t, f, f.alert, d, storagepostgres.NewAlertStore(f.db.Pool, time.Second))
		require.NoError(t, d.observation, "native CH boundary must see committed reservation and canonical payload")
		require.NoError(t, err)
		require.True(t, d.observed)
		require.True(t, input.committed)
		f.rows(t, 1)
	})
	t.Run("single connection releases between phases", func(t *testing.T) {
		f := newAlertReplayFixture(t)
		cfg := f.db.Pool.Config()
		cfg.MaxConns, cfg.MinConns = 1, 0
		pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		d := &committedReservationStore{AlertStore: storageclickhouse.NewAlertStore(f.ch), pool: f.db.Pool}
		input, err := runNativeReservation(t, f, f.alert, d, storagepostgres.NewAlertStore(pool, time.Second))
		require.NoError(t, err, "phase two must acquire the sole pool connection within the unchanged operation budget")
		require.NoError(t, d.observation)
		require.True(t, d.observed)
		require.True(t, input.committed)
		require.Zero(t, pool.Stat().AcquiredConns())
		f.rows(t, 1)
	})
	t.Run("native reservation helpers are transactional and insert only", testNativeReservationHelpers)
	t.Run("saved payload survives CH failure and mutable replay", testNativeReservationReplay)
	t.Run("legacy resolved workflow and matching orphan survive", testNativeReservationLegacy)
}

func testNativeReservationHelpers(t *testing.T) {
	f := newAlertReplayFixture(t)
	s := storagepostgres.NewAlertStore(f.db.Pool, time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	raw, err := json.Marshal(f.alert)
	require.NoError(t, err)
	fp, err := alertwriter.ImmutableFingerprint(f.alert)
	require.NoError(t, err)
	tx, err := s.BeginLocked(ctx, f.alert.AlertID)
	require.NoError(t, err)
	defer rollbackLifetimeTx(tx)
	_, _, _, err = s.FindReservation(ctx, tx, f.alert.AlertID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, s.InsertReservation(ctx, tx, f.alert.AlertID, f.alert.TenantID, fp, raw))
	tenant, fingerprint, payload, err := s.FindReservation(ctx, tx, f.alert.AlertID)
	require.NoError(t, err)
	require.Equal(t, f.alert.TenantID, tenant)
	require.Equal(t, fp, fingerprint)
	require.Equal(t, raw, payload)
	var count int
	require.NoError(t, f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM alert_reservations WHERE alert_id=$1",
		f.alert.AlertID).Scan(&count))
	require.Zero(t, count, "helper must not implicitly commit")
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM alert_reservations WHERE alert_id=$1",
		f.alert.AlertID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM alert_workflows WHERE alert_id=$1",
		f.alert.AlertID).Scan(&count))
	require.Zero(t, count, "reservation alone must not allocate workflow")
	next, err := s.BeginLocked(ctx, f.alert.AlertID)
	require.NoError(t, err)
	defer rollbackLifetimeTx(next)
	require.NoError(t, s.InsertReservation(ctx, next, f.alert.AlertID, "other", fp, []byte(`{"different":true}`)))
	tenant, fingerprint, payload, err = s.FindReservation(ctx, next, f.alert.AlertID)
	require.NoError(t, err)
	require.Equal(t, f.alert.TenantID, tenant)
	require.Equal(t, fp, fingerprint)
	require.Equal(t, raw, payload, "insert-or-ignore must preserve selected bytes")
	require.NoError(t, next.Commit(ctx))
}

type failedReservationDetection struct {
	*storageclickhouse.AlertStore
}

func (*failedReservationDetection) InsertBatch(context.Context, []*contracts.Alert) error {
	return errors.New("test-owned CH failure")
}

func testNativeReservationReplay(t *testing.T) {
	f := newAlertReplayFixture(t)
	store := storagepostgres.NewAlertStore(f.db.Pool, time.Second)
	detections := storageclickhouse.NewAlertStore(f.ch)
	input, err := runNativeReservation(t, f, f.alert, &failedReservationDetection{detections}, store)
	require.Error(t, err)
	require.False(t, input.committed)
	var raw []byte
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT canonical_payload FROM alert_reservations WHERE alert_id=$1", f.alert.AlertID).Scan(&raw))
	want, err := json.Marshal(f.alert)
	require.NoError(t, err)
	require.Equal(t, want, raw)
	var workflows int
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM alert_workflows WHERE alert_id=$1", f.alert.AlertID).Scan(&workflows))
	require.Zero(t, workflows)
	_, err = detections.Get(t.Context(), f.alert.TenantID, f.alert.AlertID)
	require.ErrorIs(t, err, storageclickhouse.ErrNotFound)

	conflict := *f.alert
	conflict.Summary = "different immutable content"
	rejected := &committedReservationStore{AlertStore: detections, pool: f.db.Pool}
	input, err = runNativeReservation(t, f, &conflict, rejected, store)
	require.NoError(t, err)
	require.True(t, input.committed, "conflict is committable only after native DLQ acknowledgement")
	require.False(t, rejected.observed, "conflicting reserved identity must never reach CH insertion")
	_, err = detections.Get(t.Context(), f.alert.TenantID, f.alert.AlertID)
	require.ErrorIs(t, err, storageclickhouse.ErrNotFound)
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT canonical_payload FROM alert_reservations WHERE alert_id=$1", f.alert.AlertID).Scan(&raw))
	require.Equal(t, want, raw)

	replay := *f.alert
	replay.Status = "resolved"
	detected := contracts.NewTimestamp(replay.CreatedAt.Add(time.Hour))
	replay.DetectedAt = &detected
	observed := &committedReservationStore{AlertStore: detections, pool: f.db.Pool}
	input, err = runNativeReservation(t, f, &replay, observed, store)
	require.NoError(t, err)
	require.True(t, input.committed)
	require.True(t, observed.observed)
	stored, err := detections.Get(t.Context(), f.alert.TenantID, f.alert.AlertID)
	require.NoError(t, err)
	require.Equal(t, "open", stored.Status)
	require.Nil(t, stored.DetectedAt)
	f.rows(t, 1)
}

func testNativeReservationLegacy(t *testing.T) {
	f := newAlertReplayFixture(t)
	store := storagepostgres.NewAlertStore(f.db.Pool, time.Second)
	detections := storageclickhouse.NewAlertStore(f.ch)
	orphan := *f.alert
	orphan.Status = "acknowledged"
	require.NoError(t, detections.InsertBatch(t.Context(), []*contracts.Alert{&orphan}))
	fp, err := alertwriter.ImmutableFingerprint(f.alert)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tx, err := store.BeginLocked(ctx, f.alert.AlertID)
	require.NoError(t, err)
	defer rollbackLifetimeTx(tx)
	require.NoError(t, store.Ensure(ctx, tx, f.alert, fp))
	require.NoError(t, tx.Commit(ctx))
	_, err = f.db.Pool.Exec(ctx, "UPDATE alert_workflows SET status='resolved',version=version+1 WHERE alert_id=$1",
		f.alert.AlertID)
	require.NoError(t, err)
	before := f.workflow(t)
	input, err := runNativeReservation(t, f, f.alert, detections, store)
	require.NoError(t, err)
	require.True(t, input.committed)
	require.Equal(t, before, f.workflow(t), "lazy reservation must preserve complete legacy triage state")
	var raw []byte
	require.NoError(t, f.db.Pool.QueryRow(ctx, "SELECT canonical_payload FROM alert_reservations WHERE alert_id=$1",
		f.alert.AlertID).Scan(&raw))
	want, err := json.Marshal(&orphan)
	require.NoError(t, err)
	require.Equal(t, want, raw, "matching retained orphan supplies saved mutable representation")
	f.rows(t, 1)
}
