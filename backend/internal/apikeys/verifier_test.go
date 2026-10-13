package apikeys_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
)

type memoryStore struct {
	mu      sync.Mutex
	records map[string]apikeys.Record
	calls   int
	err     error
}

func (s *memoryStore) Lookup(_ context.Context, id string) (apikeys.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return apikeys.Record{}, s.err
	}
	row, ok := s.records[id]
	if !ok {
		return apikeys.Record{}, apikeys.ErrUnauthorized
	}
	return row, nil
}

func storedKey(t *testing.T, s *memoryStore) string {
	t.Helper()
	raw, err := apikeys.Generate()
	require.NoError(t, err)
	id, secret, err := apikeys.Parse(raw)
	require.NoError(t, err)
	s.records[id] = apikeys.Record{
		KeyID: id, TenantID: "demo", SecretHash: apikeys.Hash(secret),
		Scopes: []string{apikeys.ScopeRead, apikeys.ScopeWrite},
	}
	return raw
}

func TestAPIKeyAuthentication(t *testing.T) {
	t.Parallel()
	s := &memoryStore{records: make(map[string]apikeys.Record)}
	raw := storedKey(t, s)
	v := apikeys.NewVerifier(s, 2, 30*time.Second, time.Now)
	p, err := v.Authenticate(t.Context(), raw)
	require.NoError(t, err)
	require.Equal(t, "demo", p.TenantID)
	require.True(t, p.HasScope(apikeys.ScopeRead))
	require.True(t, p.HasScope(apikeys.ScopeWrite))
	require.False(t, p.HasScope("admin"))
	id, _, err := apikeys.Parse(raw)
	require.NoError(t, err)
	wrong, err := apikeys.Generate()
	require.NoError(t, err)
	_, otherSecret, err := apikeys.Parse(wrong)
	require.NoError(t, err)
	for _, presented := range []string{"", "bad", wrong, "snt_" + id + "_" + otherSecret} {
		_, err = v.Authenticate(t.Context(), presented)
		require.ErrorIs(t, err, apikeys.ErrUnauthorized)
		require.Equal(t, "unauthorized", err.Error())
	}
	require.Equal(t, 2, s.calls, "wrong cached secret does not trigger DB query; unknown does")
	s.err = errors.New("database SQL secret-sentinel")
	_, err = v.Authenticate(t.Context(), wrong)
	require.ErrorIs(t, err, apikeys.ErrUnavailable)
	require.NotContains(t, err.Error(), "secret-sentinel")
}

func TestAPIKeyCacheBounded(t *testing.T) {
	t.Parallel()
	s := &memoryStore{records: make(map[string]apikeys.Record)}
	keys := []string{storedKey(t, s), storedKey(t, s), storedKey(t, s)}
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	v := apikeys.NewVerifier(s, 2, time.Hour, func() time.Time { return now })
	for _, raw := range keys[:2] {
		_, err := v.Authenticate(t.Context(), raw)
		require.NoError(t, err)
	}
	unknown, err := apikeys.Generate()
	require.NoError(t, err)
	for range 3 {
		_, err = v.Authenticate(t.Context(), unknown)
		require.ErrorIs(t, err, apikeys.ErrUnauthorized)
	}
	require.Equal(t, 5, s.calls, "negative results are never cached")
	_, err = v.Authenticate(t.Context(), keys[0])
	require.NoError(t, err)
	require.Equal(t, 5, s.calls, "negative lookups cannot evict positive entries")
	_, err = v.Authenticate(t.Context(), keys[2])
	require.NoError(t, err)
	_, err = v.Authenticate(t.Context(), keys[0])
	require.NoError(t, err)
	require.Equal(t, 7, s.calls, "third positive ID evicts from two-entry cache")
	now = now.Add(30 * time.Second)
	s.records = make(map[string]apikeys.Record)
	_, err = v.Authenticate(t.Context(), keys[0])
	require.ErrorIs(t, err, apikeys.ErrUnauthorized, "TTL must be capped at 30 seconds")
	_, err = v.Authenticate(t.Context(), keys[0])
	require.ErrorIs(t, err, apikeys.ErrUnauthorized)
	require.Equal(t, 9, s.calls)
}

func TestAPIKeyCacheCopiesMutableData(t *testing.T) {
	t.Parallel()
	s := &memoryStore{records: make(map[string]apikeys.Record)}
	raw := storedKey(t, s)
	v := apikeys.NewVerifier(s, 1, time.Second, time.Now)
	p, err := v.Authenticate(t.Context(), raw)
	require.NoError(t, err)
	p.Scopes[0] = "admin"
	id, _, err := apikeys.Parse(raw)
	require.NoError(t, err)
	s.records[id].SecretHash[0] ^= 1
	s.records[id].Scopes[0] = "admin"
	p, err = v.Authenticate(t.Context(), raw)
	require.NoError(t, err, "cache must own a copy of the original hash")
	require.True(t, p.HasScope(apikeys.ScopeRead))
	require.False(t, p.HasScope("admin"))
}

func TestAPIKeyConcurrentAuthentication(t *testing.T) {
	t.Parallel()
	s := &memoryStore{records: make(map[string]apikeys.Record)}
	raw := storedKey(t, s)
	v := apikeys.NewVerifier(s, 0, 0, nil)
	results := make(chan error, 64)
	for range cap(results) {
		go func() { _, err := v.Authenticate(t.Context(), raw); results <- err }()
	}
	for range cap(results) {
		require.NoError(t, <-results)
	}
}
