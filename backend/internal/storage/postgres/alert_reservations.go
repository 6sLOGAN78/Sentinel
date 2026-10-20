package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// FindReservation reads global immutable authority in the caller's transaction.
func (*AlertStore) FindReservation(ctx context.Context, tx pgx.Tx, id string) (string, string, []byte, error) {
	var tenant, fingerprint string
	var payload []byte
	err := tx.QueryRow(ctx, `SELECT tenant_id,immutable_fingerprint,canonical_payload
		FROM alert_reservations WHERE alert_id=$1`, id).Scan(&tenant, &fingerprint, &payload)
	return tenant, fingerprint, payload, err
}

// InsertReservation never replaces a winner or commits the caller's transaction.
// The caller must re-read in a fresh statement, including after a no-op conflict.
func (*AlertStore) InsertReservation(ctx context.Context, tx pgx.Tx,
	id, tenant, fingerprint string, payload []byte,
) error {
	_, err := tx.Exec(ctx, `INSERT INTO alert_reservations
		(alert_id,tenant_id,immutable_fingerprint,canonical_payload)
		VALUES ($1,$2,$3,$4) ON CONFLICT (alert_id) DO NOTHING`, id, tenant, fingerprint, payload)
	return err
}
