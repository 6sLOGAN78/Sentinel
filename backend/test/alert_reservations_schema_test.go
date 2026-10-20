//go:build integration

package test_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	tern "github.com/jackc/tern/v2/migrate"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/migrations"
)

// Missing admission constraints, mutable reservations, or workflow/sequence side
// effects must fail against real PostgreSQL, including an existing version-5 DB.
func TestAlertReservationsSchema(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade_from_5"
		}
		t.Run(name, func(t *testing.T) {
			testAlertReservationSchema(t, upgrade)
		})
	}
}

const alertReservationSchemaInsertSQL = `INSERT INTO alert_reservations
	(alert_id,tenant_id,immutable_fingerprint,canonical_payload) VALUES ($1,$2,$3,$4)`

func testAlertReservationSchema(t *testing.T, upgrade bool) {
	t.Helper()
	cfg, conn := newAlertReservationsSchemaDatabase(t)
	if upgrade {
		m, err := tern.NewMigrator(t.Context(), conn, "schema_version")
		require.NoError(t, err)
		require.NoError(t, m.LoadMigrations(migrations.Postgres()))
		require.NoError(t, m.MigrateTo(t.Context(), 5))
		require.Equal(t, 5, schemaVersion(t.Context(), t, cfg))
		_, err = conn.Exec(t.Context(), `INSERT INTO alert_workflows
					(alert_id,tenant_id,status,assignee,version,created_at,updated_at,rule_id,rule_version,
					severity,entity_key,summary,event_count,window_start,window_end,immutable_fingerprint)
					VALUES ($1,'tenant-a','resolved','operator-a',7,'2026-10-19T01:00:00Z',
					'2026-10-19T02:00:00Z','rule-a',3,'high','entity-a','preserve resolved workflow',
					4,'2026-10-19T00:00:00Z','2026-10-19T01:00:00Z',$2)`,
			strings.Repeat("a", 64), strings.Repeat("b", 64))
		require.NoError(t, err)
	}

	var beforeMigration reservationWorkflowSnapshot
	if upgrade {
		beforeMigration = readReservationWorkflowSnapshot(t, conn)
	}
	require.NoError(t, database.Migrate(t.Context(), cfg, zerolog.Nop()))
	var relation *string
	require.NoError(t, conn.QueryRow(t.Context(),
		"SELECT to_regclass('public.alert_reservations')::text").Scan(&relation))
	require.NotNil(t, relation, "immutable alert reservation relation is absent")
	require.Equal(t, 6, schemaVersion(t.Context(), t, cfg))
	baseline := readReservationWorkflowSnapshot(t, conn)
	if upgrade {
		require.Equal(t, beforeMigration, baseline, "forward migration changed the existing workflow or sequence")
	}

	var columns []string
	rows, err := conn.Query(t.Context(), `SELECT column_name FROM information_schema.columns
				WHERE table_schema='public' AND table_name='alert_reservations' ORDER BY ordinal_position`)
	require.NoError(t, err)
	for rows.Next() {
		var column string
		require.NoError(t, rows.Scan(&column))
		columns = append(columns, column)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, []string{
		"alert_id", "tenant_id", "immutable_fingerprint", "canonical_payload", "reserved_at",
	}, columns)
	var foreignKeys, indexes, ownedSequences int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM pg_constraint
				WHERE conrelid='alert_reservations'::regclass AND contype='f'`).Scan(&foreignKeys))
	require.Zero(t, foreignKeys, "reservation must exist independently of workflows and decisions")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM pg_indexes
				WHERE schemaname='public' AND tablename='alert_reservations'`).Scan(&indexes))
	require.Equal(t, 1, indexes, "only the global identity primary-key index is needed")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM pg_depend d
				JOIN pg_class c ON c.oid=d.objid WHERE c.relkind='S'
				AND d.refobjid='alert_reservations'::regclass`).Scan(&ownedSequences))
	require.Zero(t, ownedSequences)

	id, fingerprint := strings.Repeat("c", 64), strings.Repeat("d", 64)
	payload := []byte(`{"saved":"first"}`)
	var before, reserved, after time.Time
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&before))
	_, err = conn.Exec(t.Context(), alertReservationSchemaInsertSQL, id, "tenant-a", fingerprint, payload)
	require.NoError(t, err)
	var savedID, savedTenant, savedFingerprint string
	var savedPayload []byte
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT alert_id,tenant_id,immutable_fingerprint,
				canonical_payload,reserved_at FROM alert_reservations WHERE alert_id=$1`, id).
		Scan(&savedID, &savedTenant, &savedFingerprint, &savedPayload, &reserved))
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&after))
	require.Equal(t, id, savedID)
	require.Equal(t, "tenant-a", savedTenant)
	require.Equal(t, fingerprint, savedFingerprint)
	require.Equal(t, payload, savedPayload)
	require.False(t, reserved.Before(before) || reserved.After(after), "reserved_at must default to server time")

	_, err = conn.Exec(t.Context(), alertReservationSchemaInsertSQL,
		id, "tenant-b", strings.Repeat("e", 64), []byte("different"))
	requireReservationSQLState(t, err, "23505")
	var count int
	require.NoError(t, conn.QueryRow(t.Context(),
		"SELECT count(*) FROM alert_reservations WHERE alert_id=$1", id).Scan(&count))
	require.Equal(t, 1, count, "identity is global across tenants")

	for _, mutation := range []string{
		"UPDATE alert_reservations SET canonical_payload=canonical_payload WHERE alert_id=$1",
		"DELETE FROM alert_reservations WHERE alert_id=$1",
	} {
		_, err = conn.Exec(t.Context(), mutation, id)
		requireReservationSQLState(t, err, "P0001")
		require.ErrorContains(t, err, "alert reservations are immutable")
	}

	testAlertReservationStorageConstraints(t, conn, fingerprint)
	testAlertReservationRequiredFields(t, conn, id)
	require.Equal(t, baseline, readReservationWorkflowSnapshot(t, conn),
		"reservation insertion allocated or changed workflows/sequence")

	var beforeRerun, afterRerun string
	const snapshot = `SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY alert_id),'[]'::jsonb)::text
				FROM alert_reservations r`
	require.NoError(t, conn.QueryRow(t.Context(), snapshot).Scan(&beforeRerun))
	require.NoError(t, database.Migrate(t.Context(), cfg, zerolog.Nop()))
	require.Equal(t, 6, schemaVersion(t.Context(), t, cfg))
	require.NoError(t, conn.QueryRow(t.Context(), snapshot).Scan(&afterRerun))
	require.Equal(t, beforeRerun, afterRerun, "second migration changed reservations")
	require.Equal(t, baseline, readReservationWorkflowSnapshot(t, conn), "second migration changed workflows/sequence")
}

func testAlertReservationStorageConstraints(t *testing.T, conn *pgx.Conn, fingerprint string) {
	t.Helper()
	for i, tc := range []struct {
		name        string
		id          string
		tenant      string
		fingerprint string
		payload     []byte
		wantCode    string
	}{
		{name: "minimum_payload", payload: []byte{0}},
		{name: "maximum_payload", payload: bytes.Repeat([]byte{0}, 1048576)},
		{name: "empty_payload", payload: []byte{}, wantCode: "23514"},
		{name: "oversize_payload", payload: bytes.Repeat([]byte{0}, 1048577), wantCode: "23514"},
		{name: "null_payload", wantCode: "23502"},
		{name: "uppercase_id", id: strings.Repeat("A", 64), payload: []byte{1}, wantCode: "23514"},
		{name: "short_id", id: "abc", payload: []byte{1}, wantCode: "23514"},
		{name: "nonhex_id", id: strings.Repeat("g", 64), payload: []byte{1}, wantCode: "23514"},
		{name: "uppercase_fingerprint", fingerprint: strings.Repeat("D", 64), payload: []byte{1}, wantCode: "23514"},
		{name: "short_fingerprint", fingerprint: "abc", payload: []byte{1}, wantCode: "23514"},
		{name: "nonhex_fingerprint", fingerprint: strings.Repeat("g", 64), payload: []byte{1}, wantCode: "23514"},
		{name: "tenant_punctuation", tenant: "tenant.a", payload: []byte{1}, wantCode: "23514"},
		{name: "tenant_uppercase", tenant: "Tenant-a", payload: []byte{1}, wantCode: "23514"},
		{name: "tenant_invalid_start", tenant: "_tenant", payload: []byte{1}, wantCode: "23514"},
		{name: "tenant_too_long", tenant: strings.Repeat("a", 64), payload: []byte{1}, wantCode: "23514"},
		{name: "tenant_maximum", tenant: strings.Repeat("a", 63), payload: []byte{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.id == "" {
				tc.id = fmt.Sprintf("%064x", i+1)
			}
			if tc.tenant == "" {
				tc.tenant = "tenant-a"
			}
			if tc.fingerprint == "" {
				tc.fingerprint = fingerprint
			}
			_, insertErr := conn.Exec(t.Context(), alertReservationSchemaInsertSQL,
				tc.id, tc.tenant, tc.fingerprint, tc.payload)
			if tc.wantCode != "" {
				requireReservationSQLState(t, insertErr, tc.wantCode)
				return
			}
			require.NoError(t, insertErr)
			var stored []byte
			require.NoError(t, conn.QueryRow(t.Context(),
				"SELECT canonical_payload FROM alert_reservations WHERE alert_id=$1", tc.id).Scan(&stored))
			require.Equal(t, tc.payload, stored)
		})
	}
}

func testAlertReservationRequiredFields(t *testing.T, conn *pgx.Conn, id string) {
	t.Helper()
	for _, tc := range []struct {
		name     string
		column   string
		value    any
		wantCode string
	}{
		{"empty_id", "alert_id", "", "23514"},
		{"empty_tenant", "tenant_id", "", "23514"},
		{"empty_fingerprint", "immutable_fingerprint", "", "23514"},
		{"null_id", "alert_id", nil, "23502"},
		{"null_tenant", "tenant_id", nil, "23502"},
		{"null_fingerprint", "immutable_fingerprint", nil, "23502"},
		{"null_reserved_at", "reserved_at", nil, "23502"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// INSERT SELECT retains valid values for every other required column.
			values := []string{"alert_id", "tenant_id", "immutable_fingerprint", "canonical_payload", "reserved_at"}
			for i, column := range values {
				if column == tc.column {
					values[i] = "$2"
				}
			}
			_, insertErr := conn.Exec(t.Context(), `INSERT INTO alert_reservations
						(alert_id,tenant_id,immutable_fingerprint,canonical_payload,reserved_at)
						SELECT `+strings.Join(values, ",")+` FROM alert_reservations WHERE alert_id=$1`, id, tc.value)
			requireReservationSQLState(t, insertErr, tc.wantCode)
		})
	}
}

type reservationWorkflowSnapshot struct {
	rows     string
	lastSeq  int64
	isCalled bool
}

func readReservationWorkflowSnapshot(t *testing.T, conn *pgx.Conn) reservationWorkflowSnapshot {
	t.Helper()
	var snapshot reservationWorkflowSnapshot
	require.NoError(t, conn.QueryRow(t.Context(),
		`SELECT coalesce(jsonb_agg(to_jsonb(w) ORDER BY alert_id),'[]'::jsonb)::text
		FROM alert_workflows w`).Scan(&snapshot.rows))
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT last_value,is_called FROM alert_workflows_seq_seq").
		Scan(&snapshot.lastSeq, &snapshot.isCalled))
	return snapshot
}

func requireReservationSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, code, pgErr.Code)
}

// Create/drop only a unique database on TestMain's private PostgreSQL fixture.
func newAlertReservationsSchemaDatabase(t *testing.T) (config.DatabaseConfig, *pgx.Conn) {
	t.Helper()
	admin, err := pgx.Connect(t.Context(), database.ConnString(pgCfg))
	require.NoError(t, err)
	cfg := pgCfg
	cfg.Name = "reservation_schema_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{cfg.Name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+identifier)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		defer admin.Close(ctx)
		_, dropErr := admin.Exec(ctx, "DROP DATABASE "+identifier)
		require.NoError(t, dropErr)
	})
	conn, err := pgx.Connect(t.Context(), database.ConnString(cfg))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, conn.Close(ctx))
	})
	return cfg, conn
}
