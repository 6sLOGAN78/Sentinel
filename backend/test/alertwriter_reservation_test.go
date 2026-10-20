//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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
}
