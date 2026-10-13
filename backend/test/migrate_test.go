//go:build integration

package test_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
	"github.com/6sLOGAN78/sentinel/migrations"
)

const (
	specialPassword = "p@ss:w/rd?#% +"
	testTimeout     = 2 * time.Minute
)

func schemaVersion(ctx context.Context, t *testing.T, cfg config.DatabaseConfig) int {
	t.Helper()

	conn, err := pgx.Connect(ctx, database.ConnString(cfg))
	require.NoError(t, err)

	defer conn.Close(ctx)

	var v int
	require.NoError(t, conn.QueryRow(ctx, "SELECT version FROM schema_version").Scan(&v))

	return v
}

func TestMigratePostgresIdempotent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cfg := sentineltesting.RequirePostgres(t)

	var first bytes.Buffer

	require.NoError(t, database.Migrate(ctx, cfg, zerolog.New(&first)))
	assert.Contains(t, first.String(), "migrated database schema from version 0")
	assert.Equal(t, latestPostgresVersion(t), schemaVersion(ctx, t, cfg))

	var second bytes.Buffer

	require.NoError(t, database.Migrate(ctx, cfg, zerolog.New(&second)))
	assert.Contains(t, second.String(), "database schema up to date")
	assert.NotContains(t, second.String(), "migrated database schema from")
	assert.Equal(t, latestPostgresVersion(t), schemaVersion(ctx, t, cfg))
}

func latestPostgresVersion(t *testing.T) int {
	t.Helper()
	entries, err := fs.ReadDir(migrations.Postgres(), ".")
	require.NoError(t, err)
	var latest int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, parseErr := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		require.NoError(t, parseErr)
		latest = max(latest, version)
	}
	require.Positive(t, latest)
	return latest
}

func TestConnectSpecialPassword(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cfg := sentineltesting.RequirePostgres(t)

	admin, err := database.New(ctx, cfg, zerolog.Nop())
	require.NoError(t, err)

	defer admin.Close()

	var stmt string

	require.NoError(t, admin.Pool.QueryRow(ctx,
		"SELECT format('CREATE ROLE special LOGIN PASSWORD %L', $1::text)", specialPassword).Scan(&stmt))

	_, err = admin.Pool.Exec(ctx, stmt)
	require.NoError(t, err)

	special := cfg
	special.User = "special"
	special.Password = specialPassword

	db, err := database.New(ctx, special, zerolog.Nop())
	require.NoError(t, err)

	defer db.Close()

	var who string

	require.NoError(t, db.Pool.QueryRow(ctx, "SELECT current_user").Scan(&who))
	assert.Equal(t, "special", who)

	wrong := special
	wrong.Password = "wrong"
	_, err = database.New(ctx, wrong, zerolog.Nop())
	require.Error(t, err)
}

func chQuery(ctx context.Context, t *testing.T, cfg config.ClickHouseConfig, query string) string {
	t.Helper()

	u := url.URL{Scheme: "http", Host: cfg.HTTPAddress, Path: "/"}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(query))
	require.NoError(t, err)
	req.SetBasicAuth(cfg.User, cfg.Password)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	return strings.TrimSpace(string(body))
}

func TestMigrateClickHouseIdempotent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cfg := sentineltesting.RequireClickHouse(t)
	// A name distinct from the container's pre-created database proves the
	// runner itself creates the database.
	const dbName = "sentinel_migrated"

	runner := chmigrate.New(cfg.HTTPAddress, cfg.User, cfg.Password, dbName, nil)
	fsys := migrations.ClickHouse()

	applied, err := runner.Run(ctx, fsys)
	require.NoError(t, err)
	assert.NotEmpty(t, applied)

	assert.Equal(t, dbName, chQuery(ctx, t, cfg,
		fmt.Sprintf("SELECT name FROM system.databases WHERE name = '%s' FORMAT TabSeparated", dbName)))
	assert.Equal(t, strconv.Itoa(len(applied)), chQuery(ctx, t, cfg,
		"SELECT count() FROM "+dbName+".schema_migrations FORMAT TabSeparated"))

	again, err := runner.Run(ctx, fsys)
	require.NoError(t, err)
	assert.Empty(t, again)
	assert.Equal(t, strconv.Itoa(len(applied)), chQuery(ctx, t, cfg,
		"SELECT count() FROM "+dbName+".schema_migrations FORMAT TabSeparated"))
}

// TestMigrateClickHouseUsesConfiguredDatabase proves unqualified names in a
// migration land in the runner's database rather than in "default" (WR-09).
func TestMigrateClickHouseUsesConfiguredDatabase(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cfg := sentineltesting.RequireClickHouse(t)

	const dbName = "sentinel_dbparam"

	fsys := fstest.MapFS{
		"001_probe.sql": {Data: []byte("CREATE TABLE IF NOT EXISTS probe (x Int8) ENGINE = MergeTree ORDER BY x;\n")},
	}

	applied, err := chmigrate.New(cfg.HTTPAddress, cfg.User, cfg.Password, dbName, nil).Run(ctx, fsys)
	require.NoError(t, err)
	assert.Equal(t, []string{"001_probe.sql"}, applied)

	assert.Equal(t, dbName, chQuery(ctx, t, cfg,
		"SELECT database FROM system.tables WHERE name = 'probe' FORMAT TabSeparated"))
}

func TestRedisIntegration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	client, err := redisx.New(ctx, redisCfg)
	require.NoError(t, err)

	require.NoError(t, client.Ping(ctx).Err())
	require.NoError(t, client.Close())
	require.Error(t, client.Ping(ctx).Err())
}
