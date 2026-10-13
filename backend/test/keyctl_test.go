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
	out, err := build.CombinedOutput()
	require.NoError(t, err, "keyctl build failed: %s", out)
	return ctx, db, func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Env = keyctlEnv(cfg)
		out, err := cmd.CombinedOutput()
		return string(out), err
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
	require.True(t, regexp.MustCompile(`^snt_[a-z2-7]{12}_[A-Za-z0-9_-]{43}$`).MatchString(raw), "key must match canonical format")
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
	require.NoError(t, db.Pool.QueryRow(ctx, "SELECT tenant_id, secret_hash, scopes FROM producer_keys WHERE key_id=$1", id).Scan(&tenant, &hash, &scopes))
	require.True(t, tenant == "demo")
	require.ElementsMatch(t, []string{"events:read", "events:write"}, scopes)
	expected := sha256.Sum256([]byte(secret))
	require.True(t, bytes.Equal(expected[:], hash), "database stores SHA-256 of the encoded secret only")
	var row []byte
	require.NoError(t, db.Pool.QueryRow(ctx, "SELECT row_to_json(p)::text FROM producer_keys p WHERE key_id=$1", id).Scan(&row))
	require.False(t, bytes.Contains(row, []byte(secret)), "row must not contain secret")
	listed, err := run("list", "--tenant", "demo")
	require.NoError(t, err)
	require.False(t, strings.Contains(listed, secret), "list must omit secret")
	require.NotContains(t, listed, "secret_hash")
	var metadata []map[string]any
	require.NoError(t, json.Unmarshal([]byte(listed), &metadata))
	require.Len(t, metadata, 1)
	require.Equal(t, id, metadata[0]["key_id"])
	_, err = run("revoke", "--key-id", id)
	require.NoError(t, err)
	var revoked bool
	require.NoError(t, db.Pool.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM producer_keys WHERE key_id=$1", id).Scan(&revoked))
	require.True(t, revoked)
	for _, args := range [][]string{
		{"create", "--tenant", "Bad Tenant", "--scopes", "events:read", "--name", "invalid"},
		{"create", "--tenant", "demo", "--scopes", "admin", "--name", "invalid"},
		{"create", "--tenant", "demo", "--scopes", "", "--name", "invalid"},
	} {
		_, err := run(args...)
		require.Error(t, err)
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
	require.NoError(t, db.Pool.QueryRow(ctx, "SELECT revoked_at IS NULL FROM producer_keys WHERE key_id=$1", foreignID).Scan(&active))
	require.True(t, active, "ensure must not revoke another tenant's credential")
	var count int
	require.NoError(t, db.Pool.QueryRow(ctx, "SELECT count(*) FROM producer_keys WHERE tenant_id=$1 AND name=$2 AND revoked_at IS NULL", "demo", "seed").Scan(&count))
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
}
