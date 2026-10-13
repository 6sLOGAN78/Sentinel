package apikeys

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

const (
	defaultCacheCapacity = 1024
	maxCacheTTL          = 30 * time.Second
)

// Principal is the trusted identity obtained from a verified credential.
type Principal struct {
	KeyID    string
	TenantID string
	Scopes   []string
}

// HasScope checks a supported permission on the verified principal.
func (p Principal) HasScope(scope string) bool { return slices.Contains(p.Scopes, scope) }

// LookupStore supplies active metadata without authenticating a presented secret.
type LookupStore interface {
	Lookup(context.Context, string) (Record, error)
}

type cacheEntry struct {
	record   Record
	expires  time.Time
	sequence uint64
}

// Verifier caches positive credential data, never authentication decisions.
type Verifier struct {
	store    LookupStore
	capacity int
	ttl      time.Duration
	clock    func() time.Time
	mu       sync.Mutex
	entries  map[string]cacheEntry
	sequence uint64
}

// NewVerifier defaults capacity to 1024 and clamps TTL to at most 30 seconds.
// A nil clock uses time.Now. Expiry is absolute and does not slide on cache hits.
func NewVerifier(store LookupStore, capacity int, ttl time.Duration, clock func() time.Time) *Verifier {
	if capacity <= 0 {
		capacity = defaultCacheCapacity
	}
	if ttl <= 0 || ttl > maxCacheTTL {
		ttl = maxCacheTTL
	}
	if clock == nil {
		clock = time.Now
	}
	return &Verifier{store: store, capacity: capacity, ttl: ttl, clock: clock, entries: make(map[string]cacheEntry)}
}

// Authenticate verifies the secret even on cache hits. Invalid credentials
// share a fixed classification; infrastructure failures expose no SQL details.
func (v *Verifier) Authenticate(ctx context.Context, header string) (Principal, error) {
	id, secret, err := Parse(header)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	now := v.clock()
	row, cached := v.cached(id, now)
	if !cached {
		row, err = v.store.Lookup(ctx, id)
		if errors.Is(err, ErrUnauthorized) {
			// Hash and compare a fixed-size dummy for unknown/revoked IDs too.
			var dummy [secretBytes]byte
			_ = VerifyHash(secret, dummy[:])
			return Principal{}, ErrUnauthorized
		}
		if err != nil {
			return Principal{}, ErrUnavailable
		}
	}
	if !VerifyHash(secret, row.SecretHash) || row.RevokedAt != nil || row.KeyID != id {
		return Principal{}, ErrUnauthorized
	}
	if !cached {
		v.remember(row, now)
	}
	return Principal{KeyID: row.KeyID, TenantID: row.TenantID, Scopes: slices.Clone(row.Scopes)}, nil
}

func (v *Verifier) cached(id string, now time.Time) (Record, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.entries[id]
	if !ok {
		return Record{}, false
	}
	if !now.Before(entry.expires) {
		delete(v.entries, id)
		return Record{}, false
	}
	return copyRecord(entry.record), true
}

func (v *Verifier) remember(row Record, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	// Concurrent misses must not extend an already cached record's lifetime.
	if _, exists := v.entries[row.KeyID]; exists {
		return
	}
	for id, entry := range v.entries {
		if !now.Before(entry.expires) {
			delete(v.entries, id)
		}
	}
	if len(v.entries) >= v.capacity {
		var oldestID string
		oldest := ^uint64(0)
		for id, entry := range v.entries {
			if entry.sequence < oldest {
				oldestID, oldest = id, entry.sequence
			}
		}
		delete(v.entries, oldestID)
	}
	v.sequence++
	v.entries[row.KeyID] = cacheEntry{record: copyRecord(row), expires: now.Add(v.ttl), sequence: v.sequence}
}

func copyRecord(row Record) Record {
	row.SecretHash = slices.Clone(row.SecretHash)
	row.Scopes = slices.Clone(row.Scopes)
	return row
}
