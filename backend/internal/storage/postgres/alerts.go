// Package postgres provides bounded workflow persistence.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// AlertStore serializes immutable winners without updating workflow replays.
type AlertStore struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewAlertStore binds bounded workflow operations.
func NewAlertStore(pool *pgxpool.Pool, timeout time.Duration) *AlertStore {
	return &AlertStore{pool: pool, timeout: timeout}
}

// BeginLocked locks the global alert ID before any immutable effect.
func (*AlertStore) BeginLocked(context.Context, string) (pgx.Tx, error) {
	return nil, errors.New("workflow store not implemented")
}

// Find reads the winner within the locked transaction.
func (*AlertStore) Find(context.Context, pgx.Tx, string) (string, string, error) {
	return "", "", errors.New("workflow store not implemented")
}

// Ensure inserts only, preserving all existing workflow fields.
func (*AlertStore) Ensure(context.Context, pgx.Tx, *contracts.Alert, string) error {
	return errors.New("workflow store not implemented")
}
