package apikeys

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Record contains stored metadata. SecretHash is excluded from JSON output.
type Record struct {
	KeyID      string     `json:"key_id"`
	TenantID   string     `json:"tenant_id"`
	SecretHash []byte     `json:"-"`
	Scopes     []string   `json:"scopes"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// Store uses an explicitly supplied PostgreSQL pool for credential CRUD.
type Store struct{ pool *pgxpool.Pool }

// NewStore constructs a credential store; migrations are applied separately.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Lookup returns active credential data; absent and revoked IDs are identical.
func (s *Store) Lookup(ctx context.Context, keyID string) (Record, error) {
	var row Record
	err := s.pool.QueryRow(ctx, `SELECT key_id, tenant_id, secret_hash, scopes, name, created_at
		FROM producer_keys WHERE key_id=$1 AND revoked_at IS NULL`, keyID).
		Scan(&row.KeyID, &row.TenantID, &row.SecretHash, &row.Scopes, &row.Name, &row.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrUnauthorized
	}
	if err != nil {
		return Record{}, ErrUnavailable
	}
	return row, nil
}

// Create persists only the secret hash and returns the full credential once.
func (s *Store) Create(ctx context.Context, tenant string, scopes []string, name string) (string, error) {
	if err := ValidateMetadata(tenant, scopes, name); err != nil {
		return "", err
	}
	raw, err := Generate()
	if err != nil {
		return "", err
	}
	id, secret, err := Parse(raw)
	if err != nil {
		return "", err
	}
	normalized := slices.Clone(scopes)
	slices.Sort(normalized)
	normalized = slices.Compact(normalized)
	_, err = s.pool.Exec(ctx, `INSERT INTO producer_keys (key_id,tenant_id,secret_hash,scopes,name)
		VALUES ($1,$2,$3,$4,$5)`, id, tenant, Hash(secret), normalized, name)
	if err != nil {
		return "", ErrUnavailable
	}
	return raw, nil
}

// Revoke idempotently disables a credential without exposing its existence.
func (s *Store) Revoke(ctx context.Context, keyID string) error {
	if !validKeyID(keyID) {
		return ErrInvalidMetadata
	}
	_, err := s.pool.Exec(ctx, `UPDATE producer_keys SET revoked_at=COALESCE(revoked_at,now()) WHERE key_id=$1`, keyID)
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// List returns tenant metadata without retrieving secret hashes.
func (s *Store) List(ctx context.Context, tenant string) ([]Record, error) {
	if err := ValidateMetadata(tenant, []string{ScopeRead}, "list"); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT key_id,tenant_id,scopes,name,created_at,revoked_at
		FROM producer_keys WHERE tenant_id=$1 ORDER BY created_at,key_id`, tenant)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	result := make([]Record, 0)
	for rows.Next() {
		var row Record
		if err = rows.Scan(&row.KeyID, &row.TenantID, &row.Scopes, &row.Name, &row.CreatedAt, &row.RevokedAt); err != nil {
			return nil, ErrUnavailable
		}
		result = append(result, row)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	return result, nil
}
