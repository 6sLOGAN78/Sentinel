//go:build phase4

package testphase4_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/query"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

// Actual production HTTP/PG assertions: missing PATCH, a split transaction,
// omitted CAS/tenant/role predicates or post-commit CH enrichment must fail.
func TestPhase4Triage(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	key := make([]byte, 48)
	_, err = rand.Read(key)
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(key)
	t.Setenv("SENTINEL_AUTH.JWT_SECRET", secret)
	cfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	require.NoError(t, database.Migrate(ctx, cfg.Database, zerolog.Nop()), "actual migrator is setup, not behavioral RED")
	db, err := database.New(ctx, cfg.Database, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := storagepostgres.NewAlertStore(db.Pool, 5*time.Second)
	tenant, foreign := "p4triage-"+uuid.NewString(), "p4foreign-"+uuid.NewString()
	now := time.Now().UTC().Truncate(time.Millisecond)
	token := func(owner, actor, role string) string {
		return signToken(t, map[string]any{"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": actor,
			"tenant": owner, "role": role, "iat": now.Unix(), "nbf": now.Unix(),
			"exp": now.Add(10 * time.Minute).Unix()}, secret, "HS256")
	}
	analyst := token(tenant, "triage-analyst", "analyst")
	newAlert := func(owner string) *contracts.Alert {
		t.Helper()
		a := &contracts.Alert{SchemaVersion: 1,
			AlertID:  strings.ReplaceAll(uuid.NewString(), "-", "") + strings.Repeat("1", 32),
			TenantID: owner, CreatedAt: contracts.NewTimestamp(now), RuleID: "AUTH_BRUTE_FORCE", RuleVersion: 1,
			Severity: "high", Status: "open", EntityKey: "192.0.2.44", Summary: "Triage native fixture", EventCount: 2,
			WindowStart: contracts.NewTimestamp(now.Add(-time.Minute)), WindowEnd: contracts.NewTimestamp(now),
			SampleEventIDs: []string{uuid.NewString()}, DedupeKey: "triage-" + uuid.NewString()}
		require.NoError(t, a.Validate())
		tx, beginErr := store.BeginLocked(ctx, a.AlertID)
		require.NoError(t, beginErr)
		require.NoError(t, store.Ensure(ctx, tx, a, a.AlertID))
		require.NoError(t, tx.Commit(ctx))
		return a
	}
	binary := filepath.Join(t.TempDir(), "query-api")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/query-api")
	build.Dir, build.Stdout, build.Stderr = filepath.Join(root, "backend"), io.Discard, io.Discard
	require.NoError(t, build.Run(), "binary compilation is setup, never behavioral RED")
	base, _ := startQueryAPI(t, binary, root, secret, "")
	patch := func(address, id, credential, body string) (int, []byte) {
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodPatch,
			address+"/api/v1/alerts/"+id, strings.NewReader(body))
		require.NoError(t, requestErr)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+credential)
		resp, requestErr := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		require.NoError(t, requestErr)
		defer resp.Body.Close()
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 65536))
		require.NoError(t, readErr)
		return resp.StatusCode, raw
	}
	state := func(id string) (string, int64, int64) {
		t.Helper()
		var status string
		var version, seq int64
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT status,version,seq FROM alert_workflows
			WHERE tenant_id=$1 AND alert_id=$2`, tenant, id).Scan(&status, &version, &seq))
		return status, version, seq
	}
	t.Run("race_one_winner_one_conflict_and_atomic_audit", func(t *testing.T) {
		a := newAlert(tenant)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		bodies := make([][]byte, 2)
		start := make(chan struct{})
		before := time.Now().UTC().Add(-time.Second)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				credential := token(tenant, fmt.Sprintf("analyst-%d", i), "analyst")
				codes[i], bodies[i] = patch(base, a.AlertID, credential, `{"status":"acknowledged","version":1}`)
			}()
		}
		close(start)
		wg.Wait()
		ordered := append([]int(nil), codes...)
		sort.Ints(ordered)
		require.Equal(t, []int{200, 409}, ordered, "two actual PATCH calls must produce exactly one winner")
		winner := 0
		if codes[1] == 200 {
			winner = 1
		}
		var workflow query.WorkflowAlert
		require.NoError(t, json.Unmarshal(bodies[winner], &workflow))
		require.Equal(t, a.AlertID, workflow.AlertID)
		require.Equal(t, "acknowledged", workflow.Status)
		require.Equal(t, int64(2), workflow.Version)
		require.NotContains(t, string(bodies[winner]), "dedupe_key")
		require.NotContains(t, string(bodies[winner]), "detection")
		var actor, from, to string
		var changed time.Time
		var count int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM alert_status_audit
			WHERE tenant_id=$1 AND alert_id=$2`, tenant, a.AlertID).Scan(&count))
		require.Equal(t, 1, count)
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT actor,from_status,to_status,changed_at FROM alert_status_audit
			WHERE tenant_id=$1 AND alert_id=$2 AND version=2`, tenant, a.AlertID).Scan(&actor, &from, &to, &changed))
		require.Equal(t, fmt.Sprintf("analyst-%d", winner), actor)
		require.Equal(t, "open", from)
		require.Equal(t, "acknowledged", to)
		require.WithinRange(t, changed, before, time.Now().UTC().Add(time.Second))
		require.WithinDuration(t, workflow.UpdatedAt, changed, time.Microsecond)
		status, version, seq := state(a.AlertID)
		require.Equal(t, "acknowledged", status)
		require.Equal(t, int64(2), version)
		tx, beginErr := store.BeginLocked(ctx, a.AlertID)
		require.NoError(t, beginErr)
		require.NoError(t, store.Ensure(ctx, tx, a, a.AlertID))
		require.NoError(t, tx.Commit(ctx))
		s2, v2, q2 := state(a.AlertID)
		require.Equal(t, status, s2)
		require.Equal(t, version, v2)
		require.Equal(t, seq, q2)
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM alert_status_audit
			WHERE tenant_id=$1 AND alert_id=$2`, tenant, a.AlertID).Scan(&count))
		require.Equal(t, 1, count)
		conn, openErr := storageclickhouse.Open(ctx, cfg.ClickHouse)
		require.NoError(t, openErr)
		defer conn.Close()
		require.NoError(t, storageclickhouse.NewAlertStore(conn).InsertBatch(ctx, []*contracts.Alert{a}))
		code, raw := getJSON(t, base+"/api/v1/alerts/"+a.AlertID, analyst, "")
		require.Equal(t, 200, code)
		var detail query.AlertDetail
		require.NoError(t, json.Unmarshal(raw, &detail))
		require.Len(t, detail.History, 1)
		require.Equal(t, actor, detail.History[0].Actor)
		require.Equal(t, "open", detail.Detection.Status)
		code, _ = getJSON(t, base+"/api/v1/alerts/"+a.AlertID, token(foreign, "other", "analyst"), "")
		require.Equal(t, 404, code)
	})
	t.Run("audit_insert_fault_rolls_back_workflow", func(t *testing.T) {
		a := newAlert(tenant)
		// A fixture-specific CHECK rejects just this random ID. No shared table
		// privileges, global trigger or unrelated writer behavior is changed.
		constraint := "p4fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, err = db.Pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE alert_status_audit
			ADD CONSTRAINT %s CHECK (alert_id <> '%s') NOT VALID`, constraint, a.AlertID))
		require.NoError(t, err, "audit schema existence is setup; missing migration is not causal RED")
		defer func() {
			_, dropErr := db.Pool.Exec(context.Background(), "ALTER TABLE alert_status_audit DROP CONSTRAINT "+constraint)
			require.NoError(t, dropErr)
		}()
		code, _ := patch(base, a.AlertID, analyst, `{"status":"acknowledged","version":1}`)
		require.Equal(t, 503, code)
		status, version, _ := state(a.AlertID)
		require.Equal(t, "open", status)
		require.Equal(t, int64(1), version)
		var count int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM alert_status_audit
			WHERE alert_id=$1`, a.AlertID).Scan(&count))
		require.Zero(t, count)
	})
	t.Run("authorization_tenant_and_strict_body", func(t *testing.T) {
		a, other := newAlert(tenant), newAlert(foreign)
		for _, tc := range []struct {
			id, credential, body string
			code                 int
		}{
			{a.AlertID, token(tenant, "reader", "viewer"), `{"status":"acknowledged","version":1}`, 403},
			{other.AlertID, analyst, `{"status":"acknowledged","version":1}`, 404},
			{strings.Repeat("0", 64), analyst, `{"status":"acknowledged","version":1}`, 404},
			{a.AlertID, analyst, `{"status":"resolved","version":1}`, 409},
			{a.AlertID, analyst, `{"status":"open","version":1}`, 400},
			{a.AlertID, analyst, `{"status":"acknowledged","version":0}`, 400},
			{a.AlertID, analyst, `{"status":"acknowledged","version":1.1}`, 400},
			{a.AlertID, analyst, `{"status":"acknowledged","version":1,"actor":"forged"}`, 400},
			{a.AlertID, analyst, `{"status":"acknowledged","version":1,"tenant_id":"other"}`, 400},
			{a.AlertID, analyst, `{"status":"acknowledged","status":"acknowledged","version":1}`, 400},
			{a.AlertID, analyst, `{"status":"acknowledged"}`, 400},
			{a.AlertID, analyst, `null`, 400}, {a.AlertID, analyst, `{} {}`, 400},
			{a.AlertID, analyst, strings.Repeat(" ", 4097) + `{}`, 413},
		} {
			code, _ := patch(base, tc.id, tc.credential, tc.body)
			require.Equal(t, tc.code, code, "strict triage case")
		}
		status, version, _ := state(a.AlertID)
		require.Equal(t, "open", status)
		require.Equal(t, int64(1), version)
	})
	t.Run("committed_patch_independent_of_unreachable_ch", func(t *testing.T) {
		a := newAlert(tenant)
		unavailable, _ := startQueryAPI(t, binary, root, secret, freeAddress(t))
		code, raw := patch(unavailable, a.AlertID, analyst, `{"status":"acknowledged","version":1}`)
		require.Equal(t, 200, code, "successful commit must not be reported as CH failure")
		var workflow query.WorkflowAlert
		require.NoError(t, json.Unmarshal(raw, &workflow))
		require.Equal(t, int64(2), workflow.Version)
		code, _ = patch(unavailable, a.AlertID, analyst, `{"status":"acknowledged","version":1}`)
		require.Equal(t, 409, code)
		code, _ = patch(unavailable, a.AlertID, analyst, `{"status":"acknowledged","version":2}`)
		require.Equal(t, 409, code)
		code, _ = getJSON(t, unavailable+"/api/v1/alerts/"+a.AlertID, analyst, "")
		require.Equal(t, 503, code, "independent GET detail may fail")
		var count int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM alert_status_audit
			WHERE alert_id=$1`, a.AlertID).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("history_is_tenant_scoped_version_ordered_and_bounded", func(t *testing.T) {
		a := newAlert(tenant)
		// Synthetic history tests the reader's bound and ordering separately
		// from the lifecycle mutation: resolve is still unavailable in plan08.
		for _, version := range []int{4, 3, 2} {
			_, insertErr := db.Pool.Exec(ctx, `INSERT INTO alert_status_audit
			(alert_id,tenant_id,version,actor,changed_at,from_status,to_status)
			VALUES ($1,$2,$3,$4,$5,'open','acknowledged')`, a.AlertID, tenant, version, fmt.Sprintf("history-%d", version), now)
			require.NoError(t, insertErr)
		}
		history, readErr := store.History(ctx, tenant, a.AlertID)
		require.NoError(t, readErr)
		require.Len(t, history, 2)
		require.Equal(t, []string{"history-2", "history-3"}, []string{history[0].Actor, history[1].Actor})
		foreignHistory, readErr := store.History(ctx, foreign, a.AlertID)
		require.NoError(t, readErr)
		require.Empty(t, foreignHistory)
		canceled, stop := context.WithCancel(ctx)
		stop()
		_, readErr = store.Transition(canceled, tenant, a.AlertID, "canceled", 1, "acknowledged")
		require.ErrorIs(t, readErr, context.Canceled)
		status, version, _ := state(a.AlertID)
		require.Equal(t, "open", status)
		require.Equal(t, int64(1), version)
	})

	t.Run("locked_row_is_bounded_and_rolls_back", func(t *testing.T) {
		a := newAlert(tenant)
		tx, beginErr := db.Pool.Begin(ctx)
		require.NoError(t, beginErr)
		defer func() { _ = tx.Rollback(context.Background()) }()
		_, lockErr := tx.Exec(ctx, `SELECT alert_id FROM alert_workflows WHERE alert_id=$1 FOR UPDATE`, a.AlertID)
		require.NoError(t, lockErr)
		start := time.Now()
		code, _ := patch(base, a.AlertID, analyst, `{"status":"acknowledged","version":1}`)
		require.Equal(t, 503, code)
		require.Less(t, time.Since(start), 7*time.Second)
		require.NoError(t, tx.Rollback(ctx))
		status, version, _ := state(a.AlertID)
		require.Equal(t, "open", status)
		require.Equal(t, int64(1), version)
	})
}
