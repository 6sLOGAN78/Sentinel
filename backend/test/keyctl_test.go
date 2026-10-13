//go:build integration

package test_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

func keyctlFixture(t *testing.T) (context.Context, *database.Database, func(...string) (string, error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	cfg := sentineltesting.RequirePostgres(t)
	require.NoError(t, database.Migrate(ctx, cfg, zerolog.Nop()))
	db, err := database.New(ctx, cfg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	path := filepath.Join(t.TempDir(), "keyctl")
	build := exec.CommandContext(ctx, "go", "build", "-o", path, "./tools/keyctl")
	build.Dir = ".."
	// Compiler diagnostics cannot contain runtime credentials.
	buildOutput, err := build.CombinedOutput()
	require.NoError(t, err, "keyctl build failed: %s", buildOutput)
	return ctx, db, func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Env = keyctlEnv(cfg)
		commandOutput, commandErr := cmd.CombinedOutput()
		return string(commandOutput), commandErr
	}
}

func keyctlEnv(cfg config.DatabaseConfig) []string {
	return []string{
		"PGHOST=" + cfg.Host, "PGPORT=" + strconv.Itoa(cfg.Port),
		"PGUSER=" + cfg.User, "PGPASSWORD=" + cfg.Password,
		"PGDATABASE=" + cfg.Name, "PGSSLMODE=disable",
	}
}

func keyParts(t *testing.T, output string) (string, string) {
	t.Helper()
	raw := strings.TrimSpace(output)
	canonical := regexp.MustCompile(`^snt_[a-z2-7]{12}_[A-Za-z0-9_-]{43}$`)
	require.True(t, canonical.MatchString(raw), "key must match canonical format")
	parts := strings.SplitN(raw, "_", 3)
	return parts[1], parts[2]
}

func TestKeyctlCreateListRevoke(t *testing.T) {
	ctx, db, run := keyctlFixture(t)
	output, err := run("create", "--tenant", "demo", "--scopes", "events:read,events:write", "--name", "producer")
	require.NoError(t, err)
	id, secret := keyParts(t, output)
	var hash []byte
	var tenant string
	var scopes []string
	query := "SELECT tenant_id, secret_hash, scopes FROM producer_keys WHERE key_id=$1"
	require.NoError(t, db.Pool.QueryRow(ctx, query, id).Scan(&tenant, &hash, &scopes))
	require.Equal(t, "demo", tenant)
	require.ElementsMatch(t, []string{"events:read", "events:write"}, scopes)
	expected := sha256.Sum256([]byte(secret))
	require.True(t, bytes.Equal(expected[:], hash), "database stores SHA-256 of the encoded secret only")
	var row []byte
	query = "SELECT row_to_json(p)::text FROM producer_keys p WHERE key_id=$1"
	require.NoError(t, db.Pool.QueryRow(ctx, query, id).Scan(&row))
	require.False(t, bytes.Contains(row, []byte(secret)), "row must not contain secret")
	listed, err := run("list", "--tenant", "demo")
	require.NoError(t, err)
	require.NotContains(t, listed, secret, "list must omit secret")
	require.NotContains(t, listed, "secret_hash")
	var metadata []map[string]any
	require.NoError(t, json.Unmarshal([]byte(listed), &metadata))
	require.Len(t, metadata, 1)
	require.Equal(t, id, metadata[0]["key_id"])
	store := apikeys.NewStore(db.Pool)
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	verifier := apikeys.NewVerifier(store, 2, 30*time.Second, func() time.Time { return now })
	principal, err := verifier.Authenticate(ctx, strings.TrimSpace(output))
	require.NoError(t, err)
	require.Equal(t, "demo", principal.TenantID)
	require.True(t, principal.HasScope(apikeys.ScopeRead))
	require.True(t, principal.HasScope(apikeys.ScopeWrite))
	_, err = run("revoke", "--key-id", id)
	require.NoError(t, err)
	var revoked bool
	query = "SELECT revoked_at IS NOT NULL FROM producer_keys WHERE key_id=$1"
	require.NoError(t, db.Pool.QueryRow(ctx, query, id).Scan(&revoked))
	require.True(t, revoked)
	_, err = apikeys.NewVerifier(store, 2, 30*time.Second, time.Now).Authenticate(ctx, strings.TrimSpace(output))
	require.ErrorIs(t, err, apikeys.ErrUnauthorized, "fresh verification rejects the persisted revocation")
	now = now.Add(29 * time.Second)
	_, err = verifier.Authenticate(ctx, strings.TrimSpace(output))
	require.NoError(t, err, "documented cache delay can retain a recently revoked key")
	now = now.Add(time.Second)
	_, err = verifier.Authenticate(ctx, strings.TrimSpace(output))
	require.ErrorIs(t, err, apikeys.ErrUnauthorized, "revocation is effective at 30-second expiry")
	for _, args := range [][]string{
		{"create", "--tenant", "Bad Tenant", "--scopes", "events:read", "--name", "invalid"},
		{"create", "--tenant", "demo", "--scopes", "admin", "--name", "invalid"},
		{"create", "--tenant", "demo", "--scopes", "", "--name", "invalid"},
	} {
		_, runErr := run(args...)
		require.Error(t, runErr)
	}
}

func TestKeyctlEnsurePrivateFile(t *testing.T) {
	ctx, db, run := keyctlFixture(t)
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "demo.key")
	args := []string{"ensure", "--tenant", "demo", "--scopes", "events:read,events:write", "--name", "seed", "--out", path}
	output, err := run(args...)
	require.NoError(t, err)
	require.Empty(t, output)
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	id, _ := keyParts(t, string(first))
	file, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), file.Mode().Perm())
	parent, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), parent.Mode().Perm())
	output, err = run(args...)
	require.NoError(t, err)
	require.Empty(t, output)
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, bytes.Equal(first, again), "ensure reuses a matching active key")
	_, err = run("revoke", "--key-id", id)
	require.NoError(t, err)
	output, err = run(args...)
	require.NoError(t, err)
	require.Empty(t, output)
	repaired, err := os.ReadFile(path)
	require.NoError(t, err)
	require.False(t, bytes.Equal(first, repaired), "revoked key must be replaced")
	// A valid foreign tenant/scope key is never reused, nor revoked by demo ensure.
	foreign, err := run("create", "--tenant", "demo2", "--scopes", "events:read", "--name", "seed")
	require.NoError(t, err)
	foreignID, _ := keyParts(t, foreign)
	require.NoError(t, os.WriteFile(path, []byte(foreign), 0o600))
	output, err = run(args...)
	require.NoError(t, err)
	require.Empty(t, output)
	var active bool
	query := "SELECT revoked_at IS NULL FROM producer_keys WHERE key_id=$1"
	require.NoError(t, db.Pool.QueryRow(ctx, query, foreignID).Scan(&active))
	require.True(t, active, "ensure must not revoke another tenant's credential")
	var count int
	query = `SELECT count(*) FROM producer_keys
		WHERE tenant_id=$1 AND name=$2 AND revoked_at IS NULL`
	require.NoError(t, db.Pool.QueryRow(ctx, query, "demo", "seed").Scan(&count))
	require.Equal(t, 1, count)
	// Same tenant with different scopes must rotate as well.
	changed := append([]string(nil), args...)
	changed[4] = "events:read"
	output, err = run(changed...)
	require.NoError(t, err)
	require.Empty(t, output)
	current, err := os.ReadFile(path)
	require.NoError(t, err)
	currentID, _ := keyParts(t, string(current))
	var scopes []string
	require.NoError(t, db.Pool.QueryRow(ctx, "SELECT scopes FROM producer_keys WHERE key_id=$1", currentID).Scan(&scopes))
	require.Equal(t, []string{"events:read"}, scopes)
	// Stale malformed files are repaired; broad file modes are secured on reuse.
	require.NoError(t, os.WriteFile(path, []byte("stale-invalid-key"), 0o600))
	output, err = run(changed...)
	require.NoError(t, err)
	require.Empty(t, output)
	require.NoError(t, os.Chmod(path, 0o644))
	output, err = run(changed...)
	require.NoError(t, err)
	require.Empty(t, output)
	file, err = os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), file.Mode().Perm())
	// A symlink must never cause ensure to read or replace another file.
	link := filepath.Join(dir, "link.key")
	require.NoError(t, os.Symlink(path, link))
	unsafeArgs := append([]string(nil), changed...)
	unsafeArgs[len(unsafeArgs)-1] = link
	_, err = run(unsafeArgs...)
	require.Error(t, err)
}
