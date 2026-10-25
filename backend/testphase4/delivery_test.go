//go:build phase4

package testphase4_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	tern "github.com/jackc/tern/v2/migrate"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/query"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
	"github.com/6sLOGAN78/sentinel/migrations"
)

type deliveryPage struct {
	Items []struct {
		Seq   string              `json:"seq"`
		Alert query.WorkflowAlert `json:"alert"`
	} `json:"items"`
	Next string `json:"next_seq"`
	More bool   `json:"has_more"`
}

// Each synthetic counter/fault/cutover uses this newly owned database, never
// changes the NEW demo's durable counter, history, workflows or reservations.
func deliveryDatabase(t *testing.T, ctx context.Context, cfg config.DatabaseConfig, version int32) config.DatabaseConfig {
	t.Helper()
	admin, err := pgx.Connect(ctx, database.ConnString(cfg))
	require.NoError(t, err)
	defer func() { _ = admin.Close(context.Background()) }()
	cfg.Name = "p4delivery_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{cfg.Name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, connectErr := pgx.Connect(cleanup, database.ConnString(func() config.DatabaseConfig { c := cfg; c.Name = "sentinel"; return c }()))
		if connectErr != nil {
			t.Error("owned database cleanup connection failed")
			return
		}
		defer func() { _ = conn.Close(cleanup) }()
		_, dropErr := conn.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{cfg.Name}.Sanitize())
		require.NoError(t, dropErr, "drop only this newly created test database after children join")
	})
	conn, err := pgx.Connect(ctx, database.ConnString(cfg))
	require.NoError(t, err)
	defer func() { _ = conn.Close(context.Background()) }()
	m, err := tern.NewMigrator(ctx, conn, "schema_version")
	require.NoError(t, err)
	require.NoError(t, m.LoadMigrations(migrations.Postgres()))
	require.NoError(t, m.MigrateTo(ctx, version))
	return cfg
}

func deliveryInsert(t *testing.T, ctx context.Context, tx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, tenant string) string {
	t.Helper()
	id := strings.ReplaceAll(uuid.NewString(), "-", "") + strings.Repeat("d", 32)
	_, err := tx.Exec(ctx, `INSERT INTO alert_workflows
	 (alert_id,tenant_id,created_at,updated_at,rule_id,rule_version,severity,entity_key,
	 summary,event_count,window_start,window_end,immutable_fingerprint)
	 VALUES ($1,$2,now(),now(),'AUTH_BRUTE_FORCE',1,'high','192.0.2.44',
	 'Delivery native fixture',2,now()-interval '1 minute',now(),$1)`, id, tenant)
	require.NoError(t, err)
	return id
}

func TestPhase4Delivery(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	key := make([]byte, 48)
	_, err = rand.Read(key)
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(key)
	t.Setenv("SENTINEL_AUTH.JWT_SECRET", secret)
	cfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 9*time.Minute)
	defer cancel()
	// On RED this runs the existing seven migrations successfully. An absent
	// route is then a behavioral assertion; a missing table is never our RED.
	version := int32(7)
	if _, statErr := os.Stat(filepath.Join(root, "backend/migrations/postgres/008_alert_delivery.sql")); statErr == nil {
		version = 8
	}
	owned := deliveryDatabase(t, ctx, cfg.Database, version)
	t.Setenv("SENTINEL_DATABASE.NAME", owned.Name)
	db, err := database.New(ctx, owned, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	binary := filepath.Join(t.TempDir(), "query-api")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/query-api")
	build.Dir, build.Stdout, build.Stderr = filepath.Join(root, "backend"), io.Discard, io.Discard
	require.NoError(t, build.Run(), "actual current binary build is setup")
	base, _ := startQueryAPI(t, binary, root, secret, freeAddress(t)) // CH deliberately unreachable.
	tenant, foreign := "p4delivery-"+uuid.NewString(), "p4foreign-"+uuid.NewString()
	now := time.Now()
	token := func(owner string) string {
		return signToken(t, map[string]any{"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "delivery-reader", "tenant": owner, "role": "viewer", "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix()}, secret, "HS256")
	}
	credential := token(tenant)
	t.Run("native_workflow_max_counterexample", func(t *testing.T) {
		a, e := db.Pool.Begin(ctx)
		require.NoError(t, e)
		defer func() { _ = a.Rollback(context.Background()) }()
		idA := deliveryInsert(t, ctx, a, "counterexample")
		idB := deliveryInsert(t, ctx, db.Pool, "counterexample")
		var seqB int64
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT seq FROM alert_workflows WHERE alert_id=$1", idB).Scan(&seqB))
		require.NoError(t, a.Commit(ctx))
		var missed int
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT count(*) FROM alert_workflows WHERE alert_id=$1 AND seq>$2", idA, seqB).Scan(&missed))
		require.Zero(t, missed, "actual late committed A is absent from max-workflow-seq backfill")
		t.Log("NATIVE counterexample: committed earlier-allocation A is permanently absent after B workflow cursor")
	})
	t.Run("actual_authenticated_backfill_exists_with_ch_down", func(t *testing.T) {
		code, _ := getJSON(t, base+"/api/v1/alerts/backfill?after_seq=0", credential, "")
		require.Equal(t, 200, code, "real current API must return the PG delivery envelope, not the alert-id route")
	})
	// Fail closed before schema-dependent assertions if the actual feature is absent.
	code, _ := getJSON(t, base+"/api/v1/alerts/backfill?after_seq=0", credential, "")
	if code != 200 {
		t.FailNow()
	}
	read := func(t *testing.T, after string, limit int) deliveryPage {
		t.Helper()
		status, raw := getJSON(t, base+"/api/v1/alerts/backfill?after_seq="+after+"&limit="+strconv.Itoa(limit), credential, "")
		require.Equal(t, 200, status)
		var page deliveryPage
		require.NoError(t, json.Unmarshal(raw, &page))
		return page
	}
	waitForID := func(t *testing.T, id, after string) deliveryPage {
		t.Helper()
		var page deliveryPage
		require.Eventually(t, func() bool {
			page = read(t, after, 200)
			for _, item := range page.Items {
				if item.Alert.AlertID == id {
					return true
				}
			}
			return false
		}, 8*time.Second, 25*time.Millisecond, "real projector must persist the committed alert")
		return page
	}
	t.Run("reverse_commit_workflow_max_is_not_delivery_cursor", func(t *testing.T) {
		a, beginErr := db.Pool.Begin(ctx)
		require.NoError(t, beginErr)
		defer func() { _ = a.Rollback(context.Background()) }()
		idA := deliveryInsert(t, ctx, a, tenant)
		idB := deliveryInsert(t, ctx, db.Pool, tenant)
		var seqA, seqB int64
		require.NoError(t, a.QueryRow(ctx, "SELECT seq FROM alert_workflows WHERE alert_id=$1", idA).Scan(&seqA))
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT seq FROM alert_workflows WHERE alert_id=$1", idB).Scan(&seqB))
		require.Less(t, seqA, seqB, "A really allocated before B")
		first := waitForID(t, idB, "0")
		require.Len(t, first.Items, 1)
		require.NoError(t, a.Commit(ctx))
		var naive int
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT count(*) FROM alert_workflows WHERE tenant_id=$1 AND seq>$2", tenant, seqB).Scan(&naive))
		require.Zero(t, naive, "actual max-workflow-seq algorithm permanently misses late A")
		second := waitForID(t, idA, first.Next)
		require.Len(t, second.Items, 1)
		require.Equal(t, idA, second.Items[0].Alert.AlertID)
		x, e := strconv.ParseInt(second.Items[0].Seq, 10, 64)
		require.NoError(t, e)
		y, e := strconv.ParseInt(first.Next, 10, 64)
		require.NoError(t, e)
		require.Greater(t, x, y, "commit-safe delivery cursor differs from workflow allocation")
		// Equal writer replay must preserve seq, mutable status/version and audit.
		store := storagepostgres.NewAlertStore(db.Pool, 5*time.Second)
		_, e = store.Transition(ctx, tenant, idA, "delivery-actor", 1, "acknowledged")
		require.NoError(t, e)
		workflow, e := store.GetWorkflow(ctx, tenant, idA)
		require.NoError(t, e)
		replay := &contracts.Alert{AlertID: idA, TenantID: tenant, CreatedAt: contracts.NewTimestamp(workflow.CreatedAt), RuleID: workflow.RuleID, RuleVersion: int(workflow.RuleVersion), Severity: workflow.Severity, EntityKey: workflow.EntityKey, Summary: workflow.Summary, EventCount: int(workflow.EventCount), WindowStart: contracts.NewTimestamp(workflow.WindowStart), WindowEnd: contracts.NewTimestamp(workflow.WindowEnd)}
		tx, e := store.BeginLocked(ctx, idA)
		require.NoError(t, e)
		require.NoError(t, store.Ensure(ctx, tx, replay, idA))
		require.NoError(t, tx.Commit(ctx))
		var count int
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT count(*) FROM alert_deliveries WHERE alert_id=$1", idA).Scan(&count))
		require.Equal(t, 1, count)
		var status string
		var seq, version int64
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT status,seq,version FROM alert_workflows WHERE alert_id=$1", idA).Scan(&status, &seq, &version))
		require.Equal(t, "acknowledged", status)
		require.Equal(t, seqA, seq)
		require.Equal(t, int64(2), version)
		history, e := store.History(ctx, tenant, idA)
		require.NoError(t, e)
		require.Len(t, history, 1)
	})
	t.Run("tenant_auth_strict_cursor_and_bounded_pages", func(t *testing.T) {
		foreignID := deliveryInsert(t, ctx, db.Pool, foreign)
		for range 202 {
			deliveryInsert(t, ctx, db.Pool, tenant)
		}
		require.Eventually(t, func() bool {
			var n int
			e := db.Pool.QueryRow(ctx, "SELECT count(*) FROM alert_delivery_pending").Scan(&n)
			return e == nil && n == 0
		}, 8*time.Second, 25*time.Millisecond)
		defaultCode, raw := getJSON(t, base+"/api/v1/alerts/backfill", credential, "")
		require.Equal(t, 200, defaultCode)
		var defaultPage deliveryPage
		require.NoError(t, json.Unmarshal(raw, &defaultPage))
		require.Len(t, defaultPage.Items, 50)
		page := read(t, "0", 200)
		require.Len(t, page.Items, 200)
		require.True(t, page.More)
		seen := map[string]bool{}
		for _, item := range page.Items {
			require.Equal(t, tenant, item.Alert.TenantID)
			require.NotEqual(t, foreignID, item.Alert.AlertID)
			seen[item.Alert.AlertID] = true
		}
		next := read(t, page.Next, 200)
		require.Len(t, next.Items, 4)
		require.False(t, next.More)
		for _, item := range next.Items {
			require.False(t, seen[item.Alert.AlertID])
		}
		for _, q := range []string{"after_seq=-1", "after_seq=+1", "after_seq=01", "after_seq=1.0", "after_seq=9223372036854775808", "after_seq=9223372036854775807", "after_seq=0&after_seq=1", "after_seq=", "limit=0", "limit=201", "limit=1&limit=2", "tenant_id=forged"} {
			status, _ := getJSON(t, base+"/api/v1/alerts/backfill?"+q, credential, "")
			require.Equal(t, 400, status, q)
		}
		status, _ := getJSON(t, base+"/api/v1/alerts/backfill?after_seq=0", "", "")
		require.Equal(t, 401, status)
		status, raw = getJSON(t, base+"/api/v1/alerts/backfill?after_seq=0", token(foreign), "")
		require.Equal(t, 200, status)
		var other deliveryPage
		require.NoError(t, json.Unmarshal(raw, &other))
		require.Len(t, other.Items, 1)
		require.Equal(t, foreignID, other.Items[0].Alert.AlertID)
		require.NotContains(t, string(raw), "dedupe_key")
		require.NotContains(t, string(raw), "sample_event_ids")
	})
	t.Run("atomic_projection_rollback_retry_and_two_replicas", func(t *testing.T) {
		var before int64
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT next_seq FROM alert_delivery_counter WHERE singleton").Scan(&before))
		_, e := db.Pool.Exec(ctx, `ALTER TABLE alert_deliveries ADD CONSTRAINT owned_delivery_fault CHECK (false) NOT VALID`)
		require.NoError(t, e)
		id := deliveryInsert(t, ctx, db.Pool, tenant)
		// Both production replicas retry actual transaction failure; no CH traffic.
		startQueryAPI(t, binary, root, secret, freeAddress(t))
		time.Sleep(1300 * time.Millisecond)
		var after int64
		var pending int
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT next_seq FROM alert_delivery_counter WHERE singleton").Scan(&after))
		require.Equal(t, before, after)
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT count(*) FROM alert_delivery_pending WHERE alert_id=$1", id).Scan(&pending))
		require.Equal(t, 1, pending)
		_, e = db.Pool.Exec(ctx, "ALTER TABLE alert_deliveries DROP CONSTRAINT owned_delivery_fault")
		require.NoError(t, e)
		page := waitForID(t, id, strconv.FormatInt(before, 10))
		require.Len(t, page.Items, 1)
		// Replay a page after losing its HTTP acknowledgement: stable identity/seq.
		require.Equal(t, page, read(t, strconv.FormatInt(before, 10), 200))
		for range 110 {
			deliveryInsert(t, ctx, db.Pool, tenant)
		}
		require.Eventually(t, func() bool {
			var n int
			e := db.Pool.QueryRow(ctx, "SELECT count(*) FROM alert_delivery_pending").Scan(&n)
			return e == nil && n == 0
		}, 8*time.Second, 25*time.Millisecond)
		var count, distinct int64
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT count(*),count(DISTINCT delivery_seq) FROM alert_deliveries WHERE delivery_seq>$1", before).Scan(&count, &distinct))
		require.Equal(t, int64(111), count)
		require.Equal(t, count, distinct)
		require.NoError(t, db.Pool.QueryRow(ctx, "SELECT next_seq FROM alert_delivery_counter WHERE singleton").Scan(&after))
		require.Equal(t, before+111, after)
	})
	t.Run("large_decimal_cursor_is_exact", func(t *testing.T) {
		_, e := db.Pool.Exec(ctx, "UPDATE alert_delivery_counter SET next_seq=9007199254740992 WHERE singleton")
		require.NoError(t, e)
		id := deliveryInsert(t, ctx, db.Pool, tenant)
		page := waitForID(t, id, "9007199254740992")
		require.Equal(t, "9007199254740993", page.Next)
		require.Equal(t, page.Next, page.Items[0].Seq)
	})
	t.Run("preexisting_and_concurrent_trigger_cutover", func(t *testing.T) { deliveryCutover(ctx, t, cfg.Database) })
	t.Run("projector_uncertain_commit_ack_retries_without_duplicate", func(t *testing.T) {
		isolated := deliveryDatabase(t, ctx, cfg.Database, 8)
		conn, e := pgx.Connect(ctx, database.ConnString(isolated))
		require.NoError(t, e)
		defer func() { _ = conn.Close(context.Background()) }()
		first := deliveryInsert(t, ctx, conn, tenant)
		address, lost := deliveryAckProxy(t, net.JoinHostPort(isolated.Host, strconv.Itoa(isolated.Port)))
		_, port, e := net.SplitHostPort(address)
		require.NoError(t, e)
		t.Setenv("SENTINEL_DATABASE.PORT", port)
		t.Setenv("SENTINEL_DATABASE.NAME", isolated.Name)
		api, _ := startQueryAPI(t, binary, root, secret, freeAddress(t))
		require.Eventually(t, lost.Load, 5*time.Second, 10*time.Millisecond, "real PostgreSQL COMMIT acknowledgement was dropped after server success")
		second := deliveryInsert(t, ctx, conn, tenant)
		require.Eventually(t, func() bool {
			var n int
			e := conn.QueryRow(ctx, "SELECT count(*) FROM alert_deliveries WHERE alert_id=ANY($1)", []string{first, second}).Scan(&n)
			return e == nil && n == 2
		}, 8*time.Second, 25*time.Millisecond)
		status, raw := getJSON(t, api+"/api/v1/alerts/backfill?after_seq=0", credential, "")
		require.Equal(t, 200, status)
		var page deliveryPage
		require.NoError(t, json.Unmarshal(raw, &page))
		require.Len(t, page.Items, 2)
		require.Equal(t, "1", page.Items[0].Seq)
		require.Equal(t, "2", page.Next)
	})
	t.Run("counter_overflow_fails_managed_process", func(t *testing.T) {
		// Another fresh DB confines synthetic overflow and terminal lifecycle proof.
		overflow := deliveryDatabase(t, ctx, cfg.Database, 8)
		conn, e := pgx.Connect(ctx, database.ConnString(overflow))
		require.NoError(t, e)
		defer func() { _ = conn.Close(context.Background()) }()
		_, e = conn.Exec(ctx, "UPDATE alert_delivery_counter SET next_seq=$1 WHERE singleton", int64(math.MaxInt64))
		require.NoError(t, e)
		deliveryInsert(t, ctx, conn, tenant)
		t.Setenv("SENTINEL_DATABASE.NAME", overflow.Name)
		t.Setenv("SENTINEL_SERVER.PORT", strings.Split(freeAddress(t), ":")[1])
		t.Setenv("SENTINEL_OPS.PORT", strings.Split(freeAddress(t), ":")[1])
		process := exec.CommandContext(ctx, binary)
		process.Dir = filepath.Join(root, "backend")
		process.Stdout, process.Stderr = io.Discard, io.Discard
		require.NoError(t, process.Start())
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		select {
		case e = <-done:
			require.Error(t, e, "overflow must fail the service, not silently stop projection")
		case <-time.After(8 * time.Second):
			_ = process.Process.Kill()
			<-done
			t.Fatal("overflow did not terminate managed lifecycle")
		}
	})
}

// Transparent local PostgreSQL wire proxy drops exactly the first successful
// COMMIT response. The server has committed; the production pgx client sees EOF.
// Startup/auth and all other messages pass through, without being logged.
func deliveryAckProxy(t *testing.T, target string) (string, *atomic.Bool) {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, e)
	lost := &atomic.Bool{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	connections := []net.Conn{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			server, e := net.DialTimeout("tcp", target, time.Second)
			if e != nil {
				_ = client.Close()
				continue
			}
			mu.Lock()
			connections = append(connections, client, server)
			mu.Unlock()
			wg.Add(2)
			go func() {
				defer wg.Done()
				defer func() { _ = client.Close(); _ = server.Close() }()
				_, _ = io.Copy(server, client)
			}()
			go func() {
				defer wg.Done()
				defer func() { _ = client.Close(); _ = server.Close() }()
				for {
					header := make([]byte, 5)
					if _, e := io.ReadFull(server, header); e != nil {
						return
					}
					length := binary.BigEndian.Uint32(header[1:])
					if length < 4 || length > 1<<20 {
						return
					}
					body := make([]byte, int(length)-4)
					if _, e := io.ReadFull(server, body); e != nil {
						return
					}
					if header[0] == 'C' && string(body) == "COMMIT\x00" && lost.CompareAndSwap(false, true) {
						return
					}
					if _, e := client.Write(append(header, body...)); e != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		for _, c := range connections {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return listener.Addr().String(), lost
}

func deliveryCutover(ctx context.Context, t *testing.T, cfg config.DatabaseConfig) {
	t.Helper()
	owned := deliveryDatabase(t, ctx, cfg, 7)
	conn, e := pgx.Connect(ctx, database.ConnString(owned))
	require.NoError(t, e)
	defer func() { _ = conn.Close(context.Background()) }()
	old := deliveryInsert(t, ctx, conn, "cutover")
	a, e := conn.Begin(ctx)
	require.NoError(t, e)
	defer func() { _ = a.Rollback(context.Background()) }()
	held := deliveryInsert(t, ctx, a, "cutover")
	done := make(chan error, 1)
	go func() { done <- database.Migrate(ctx, owned, zerolog.Nop()) }()
	// Observe the real migration's conflicting relation lock before releasing A.
	observer, e := pgx.Connect(ctx, database.ConnString(owned))
	require.NoError(t, e)
	defer func() { _ = observer.Close(context.Background()) }()
	require.Eventually(t, func() bool {
		var n int
		e := observer.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE relation='alert_workflows'::regclass AND NOT granted`).Scan(&n)
		return e == nil && n > 0
	}, 3*time.Second, 10*time.Millisecond)
	writer := make(chan string, 1)
	go func() {
		_, e := observer.Exec(ctx, `INSERT INTO alert_workflows (alert_id,tenant_id,created_at,updated_at,rule_id,rule_version,severity,entity_key,summary,event_count,window_start,window_end,immutable_fingerprint) VALUES ($1,'cutover',now(),now(),'PORT_SCAN',1,'high','192.0.2.1','Across cutover',1,now()-interval '1 minute',now(),$1)`, strings.Repeat("c", 64))
		if e != nil {
			writer <- e.Error()
		} else {
			writer <- ""
		}
	}()
	select {
	case <-writer:
		t.Fatal("writer crossed the queued migration lock")
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, a.Commit(ctx))
	require.NoError(t, <-done)
	require.Empty(t, <-writer)
	var n int
	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM alert_delivery_pending WHERE alert_id=ANY($1)", []string{old, held, strings.Repeat("c", 64)}).Scan(&n))
	require.Equal(t, 3, n, "seed + held writer + installed trigger cover actual concurrent cutover")
}
