package clickhouse

import (
	"context"
	"errors"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// AlertStore persists immutable alert evidence through the native driver.
type AlertStore struct{ conn Conn }

// NewAlertStore binds alert operations to conn.
func NewAlertStore(conn Conn) *AlertStore { return &AlertStore{conn: conn} }

// InsertBatch sends a fresh native block on each call.
func (*AlertStore) InsertBatch(context.Context, []*contracts.Alert) error {
	return errors.New("alert store not implemented")
}

// Get returns the tenant's immediately deduplicated immutable identity.
func (*AlertStore) Get(context.Context, string, string) (*contracts.Alert, error) {
	return nil, errors.New("alert store not implemented")
}

// CountUnique counts identities across physical partitions.
func (*AlertStore) CountUnique(context.Context, string, time.Time) (uint64, error) {
	return 0, errors.New("alert store not implemented")
}
