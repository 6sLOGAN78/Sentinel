//go:build integration

package test_test

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/migrations"
)

// Missing durable identity/sequence constraints or tenant-list indexes must fail.
func TestAlertsSchema(t *testing.T) {
	require.NoError(t, database.Migrate(t.Context(), pgCfg, zerolog.Nop()))
	require.NoError(t, database.Migrate(t.Context(), pgCfg, zerolog.Nop()))
	cfg, conn := newRecoveryDatabase(t)
	_, err := chmigrate.New(cfg.HTTPAddress, cfg.User, cfg.Password, cfg.Database, nil).
		Run(t.Context(), migrations.ClickHouse())
	require.NoError(t, err)
	var ddl string
	require.NoError(t, conn.QueryRow(t.Context(), "SHOW CREATE TABLE alerts").Scan(&ddl),
		"immutable alerts table is absent")
	require.NotContains(t, ddl, "TTL")
	require.Contains(t, ddl, "DateTime64(3, 'UTC')")
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	defer db.Close()
	var relation *string
	require.NoError(t, db.Pool.QueryRow(t.Context(), "SELECT to_regclass('alert_workflows')::text").Scan(&relation))
	require.NotNil(t, relation, "PostgreSQL workflow table is absent")
	rows, err := db.Pool.Query(t.Context(), `SELECT indexdef FROM pg_indexes
		WHERE schemaname='public' AND tablename='alert_workflows'`)
	require.NoError(t, err)
	defer rows.Close()
	var definitions []string
	for rows.Next() {
		var definition string
		require.NoError(t, rows.Scan(&definition))
		definitions = append(definitions, definition)
	}
	require.NoError(t, rows.Err())
	for _, suffix := range []string{"UNIQUE INDEX", "(alert_id)", "(seq)",
		"(tenant_id, created_at, alert_id)", "(tenant_id, status, created_at, alert_id)", "(tenant_id, seq)"} {
		require.Contains(t, strings.Join(definitions, "\n"), suffix)
	}
	var identity, kind string
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT is_identity,data_type FROM information_schema.columns
		WHERE table_schema='public' AND table_name='alert_workflows' AND column_name='seq'`).Scan(&identity, &kind))
	require.Equal(t, "YES", identity)
	require.Equal(t, "bigint", kind)
	for _, column := range []string{"alert_id", "seq"} {
		var unique bool
		require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT EXISTS (
			SELECT 1 FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid
			JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=ANY(i.indkey)
			WHERE c.oid='public.alert_workflows'::regclass AND i.indisunique
			AND i.indnkeyatts=1 AND a.attname=$1)`, column).Scan(&unique))
		require.True(t, unique, "%s must have its own unique identity constraint", column)
	}
}
