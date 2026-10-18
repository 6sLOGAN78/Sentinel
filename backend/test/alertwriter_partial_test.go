//go:build integration

package test_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

// Only this dedicated test-owned store fails, after executing the real PG
// INSERT. The writer must roll back that transaction and retain its Kafka input.
type failedAlertWorkflow struct{ *storagepostgres.AlertStore }

func (s *failedAlertWorkflow) Ensure(ctx context.Context, tx pgx.Tx, a *contracts.Alert, fingerprint string) error {
	if err := s.AlertStore.Ensure(ctx, tx, a, fingerprint); err != nil {
		return err
	}
	return errors.New("test-owned PG failure after native INSERT")
}

// The native batch actually reaches ClickHouse. Only its acknowledgement is
// hidden, simulating response loss without a production fault configuration.
type lostAlertAckConn struct{ storageclickhouse.Conn }

func (c *lostAlertAckConn) PrepareBatch(ctx context.Context, query string, opts ...driver.PrepareBatchOption) (driver.Batch, error) {
	batch, err := c.Conn.PrepareBatch(ctx, query, opts...)
	if err != nil {
		return nil, err
	}
	return &lostAlertAckBatch{Batch: batch}, nil
}

type lostAlertAckBatch struct{ driver.Batch }

func (b *lostAlertAckBatch) Send() error {
	if err := b.Batch.Send(); err != nil {
		return err
	}
	return errors.New("test-owned native acknowledgement loss")
}

// Committing after CH alone, losing rollback, blindly reinserting an orphan,
// or treating a runtime sink failure as poison must fail these real effects.
func TestAlertWriterPartialStorage(t *testing.T) {
	for _, name := range []string{"CH success PG failure", "ambiguous native CH acknowledgement"} {
		t.Run(name, func(t *testing.T) {
			f := newAlertReplayFixture(t)
			f.publish(t, f.alert)
			workflows := storagepostgres.NewAlertStore(f.db.Pool, f.settings.OperationTimeout)
			detections := storageclickhouse.NewAlertStore(f.ch)
			var err error
			if name == "CH success PG failure" {
				err = f.run(t, detections, &failedAlertWorkflow{AlertStore: workflows})
			} else {
				err = f.run(t, storageclickhouse.NewAlertStore(&lostAlertAckConn{Conn: f.ch}), workflows)
			}
			require.Error(t, err, "the ambiguous/partial attempt must fail without source commit")
			require.NotContains(t, err.Error(), f.alert.EntityKey)
			require.Zero(t, f.committed(t), "CH alone is not a durable source effect")
			f.rows(t, 1, 0)
			// A new real consumer in the same group must replay the same offset.
			require.NoError(t, f.run(t, detections, workflows))
			require.Equal(t, int64(1), f.committed(t))
			f.rows(t, 1, 1)
			before := f.workflow(t)
			f.publish(t, f.alert)
			require.NoError(t, f.run(t, detections, workflows))
			require.Equal(t, int64(2), f.committed(t))
			f.rows(t, 1, 1)
			require.JSONEq(t, before, f.workflow(t))
		})
	}
}
