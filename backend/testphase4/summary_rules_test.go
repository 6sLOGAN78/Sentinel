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
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/query"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// The real command must load the configured file once, before opening listeners.
func TestPhase4SummaryRules(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	material := make([]byte, 48)
	_, err = rand.Read(material)
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(material)
	t.Setenv("SENTINEL_AUTH.JWT_SECRET", secret)
	t.Setenv("SENTINEL_PRIMARY.ENV", "local")
	t.Setenv("SENTINEL_ENV_FILE", filepath.Join(root, ".env"))
	raw, err := os.ReadFile(filepath.Join(root, "config/rules.yaml"))
	require.NoError(t, err)
	updated := strings.ReplaceAll(string(raw), "version: 1\n    enabled:", "version: 12\n    enabled:")
	updated = strings.ReplaceAll(updated, "threshold: 10", "threshold: 17")
	raw = []byte(strings.ReplaceAll(updated, "window: 300s", "window: 301.5s"))
	raw = []byte(strings.Replace(string(raw), "enabled: true", "enabled: false", 1))
	path := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	t.Setenv("SENTINEL_DETECTOR.RULES_PATH", path)
	definitions, err := detection.LoadRules(strings.NewReader(string(raw)))
	require.NoError(t, err, "valid fixture is setup, not RED")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "query-api")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/query-api")
	build.Dir, build.Stdout, build.Stderr = filepath.Join(root, "backend"), io.Discard, io.Discard
	require.NoError(t, build.Run(), "build must pass before behavioral RED")
	base, _ := startQueryAPI(t, binary, root, secret, "")
	tenant := "p4rules-" + uuid.NewString()
	tokenFor := func(role string) string {
		now := time.Now()
		return signToken(t, map[string]any{
			"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "rules-operator", "tenant": tenant,
			"role": role, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix()}, secret, "HS256")
	}
	t.Run("summary_actual_binary_authenticated_route", func(t *testing.T) {
		status, _ := getJSON(t, base+"/api/v1/metrics/summary", "", "")
		require.Equal(t, 401, status, "summary must require a verified principal")
		status, body := getJSON(t, base+"/api/v1/metrics/summary", tokenFor("viewer"), "")
		require.Equal(t, 200, status, "actual binary must register the native summary adapters")
		var summary struct {
			Rate  float64 `json:"event_rate_per_sec"`
			Open  int64   `json:"open_alerts"`
			Trend []struct {
				Events int64 `json:"event_count"`
				Alerts int64 `json:"alert_count"`
			} `json:"trend_24h"`
		}
		require.NoError(t, json.Unmarshal(body, &summary))
		require.Zero(t, summary.Rate)
		require.Zero(t, summary.Open)
		require.Len(t, summary.Trend, 288)
	})
	read := func(t *testing.T, url, token string) []byte {
		t.Helper()
		status, body := getJSON(t, url+"/api/v1/rules", token, "")
		require.Equal(t, 200, status, "configured startup rules must be available on the actual binary")
		return body
	}
	for _, role := range []string{"viewer", "analyst", "admin"} {
		t.Run("exact_startup_definitions_"+role, func(t *testing.T) {
			var page struct {
				Rules []struct {
					ID          string `json:"rule_id"`
					Version     int    `json:"version"`
					Enabled     bool   `json:"enabled"`
					Name        string `json:"name"`
					Description string `json:"description"`
					Severity    string `json:"severity"`
					Parameters  struct {
						Threshold  int   `json:"threshold"`
						WindowMS   int64 `json:"window_ms"`
						CooldownMS int64 `json:"cooldown_ms"`
					} `json:"parameters"`
				} `json:"rules"`
			}
			require.NoError(t, json.Unmarshal(read(t, base, tokenFor(role)), &page))
			require.Len(t, page.Rules, 4)
			for i, want := range definitions.Rules {
				got := page.Rules[i]
				require.Equal(t, want.ID, got.ID)
				require.Equal(t, want.Version, got.Version)
				require.Equal(t, want.Enabled, got.Enabled)
				require.Equal(t, want.Severity, got.Severity)
				require.Equal(t, want.Threshold, got.Parameters.Threshold)
				require.Equal(t, want.Window.Milliseconds(), got.Parameters.WindowMS)
				require.Equal(t, want.Cooldown.Milliseconds(), got.Parameters.CooldownMS)
				require.NotEmpty(t, got.Name)
				require.NotEmpty(t, got.Description)
			}
		})
	}
	t.Run("unauthenticated_and_read_only", func(t *testing.T) {
		status, _ := getJSON(t, base+"/api/v1/rules", "", "")
		require.Equal(t, 401, status)
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			req, requestErr := http.NewRequestWithContext(ctx, method,
				base+"/api/v1/rules", strings.NewReader(`{"enabled":true}`))
			require.NoError(t, requestErr)
			req.Header.Set("Authorization", "Bearer "+tokenFor("admin"))
			response, requestErr := (&http.Client{Timeout: 5 * time.Second}).Do(req)
			require.NoError(t, requestErr)
			require.NoError(t, response.Body.Close())
			require.Equal(t, 405, response.StatusCode)
		}
	})
	t.Run("immutable_until_restart", func(t *testing.T) {
		before := read(t, base, tokenFor("viewer"))
		changed := strings.ReplaceAll(string(raw), "version: 12", "version: 13")
		require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
		require.JSONEq(t, string(before), string(read(t, base, tokenFor("viewer"))))
		restarted, _ := startQueryAPI(t, binary, root, secret, "")
		require.Contains(t, string(read(t, restarted, tokenFor("viewer"))), `"version":13`)
		require.NoError(t, os.Remove(path))
		require.JSONEq(t, string(before), string(read(t, base, tokenFor("viewer"))))
	})
	t.Run("retained_version_is_not_current_registry", func(t *testing.T) {
		cfg, loadErr := config.Load(config.ServiceQueryAPI)
		require.NoError(t, loadErr)
		db, openErr := database.New(ctx, cfg.Database, zerolog.Nop())
		require.NoError(t, openErr)
		defer db.Close()
		history := *definitions
		history.Rules = append([]detection.Rule(nil), definitions.Rules...)
		for i := range history.Rules {
			history.Rules[i].Version = 77
			history.Rules[i].Threshold = 13
		}
		manifest, marshalErr := json.Marshal(history)
		require.NoError(t, marshalErr)
		group, id := "p4rules-"+uuid.NewString(), strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
		fingerprint := history.Fingerprint()
		_, insertErr := db.Pool.Exec(ctx, `INSERT INTO detection_partition_fences
		(group_id,topic,partition_id,incarnation,member_id,generation,assignment_token,
		config_fingerprint,rules_manifest)
		VALUES ($1,'phase4-rules',0,$2,'fixture',1,1,$3,$4)`, group, strings.Repeat("1", 64), fingerprint, manifest)
		require.NoError(t, insertErr)
		_, insertErr = db.Pool.Exec(ctx, `INSERT INTO detection_decisions
		(alert_id,group_id,tenant_id,rule_id,rule_version,entity_components,config_fingerprint,
		allowed,trigger_occurred_at,effective_clock,topic,partition_id,source_offset,rule_ordinal,
		canonical_payload,immutable_fingerprint,decision_generation)
		VALUES ($1,$2,$3,'AUTH_BRUTE_FORCE',77,'[]',$4,true,now(),now(),
		'phase4-rules',0,0,0,'{}',$1,1)`, id, group, tenant, fingerprint)
		require.NoError(t, insertErr)
		explanation, explanationErr := storagepostgres.NewAlertStore(db.Pool, 5*time.Second).
			RuleExplanation(ctx, tenant, id, "AUTH_BRUTE_FORCE", 77)
		require.NoError(t, explanationErr)
		require.Contains(t, explanation, "threshold 13")
		body := string(read(t, base, tokenFor("viewer")))
		require.Contains(t, body, `"version":12`)
		require.NotContains(t, body, `"version":77`)
	})
	t.Run("native_frozen_summary", func(t *testing.T) { testNativeSummary(t, secret) })
	verifyRulesFailClosed(ctx, t, binary, root, path, raw)
}

type nativeSummary struct {
	Rate     float64               `json:"event_rate_per_sec"`
	Open     uint64                `json:"open_alerts"`
	Severity []query.SeverityCount `json:"alerts_by_severity"`
	Rules    []query.RuleCount     `json:"top_rules"`
	Trend    []struct {
		Start  time.Time `json:"bucket_start"`
		Events uint64    `json:"event_count"`
		Alerts uint64    `json:"alert_count"`
	} `json:"trend_24h"`
}

func testNativeSummary(t *testing.T, secret string) {
	t.Helper()
	ctx := t.Context()
	cfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	db, err := database.New(ctx, cfg.Database, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	conn, err := storageclickhouse.Open(ctx, cfg.ClickHouse)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	now := time.Now().UTC().Truncate(5 * time.Minute).Add(2*time.Minute + 30*time.Second)
	end := now.Truncate(5 * time.Minute)
	start := end.Add(-24 * time.Hour)
	tenant, foreign := "p4summary-"+uuid.NewString(), "p4summary-"+uuid.NewString()
	verifier, err := auth.NewVerifier([]byte(secret), "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	token := func(owner string) string {
		actual := time.Now()
		return signToken(t, map[string]any{"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "summary-native",
			"tenant": owner, "role": "viewer", "iat": actual.Unix(), "nbf": actual.Unix(),
			"exp": actual.Add(10 * time.Minute).Unix()}, secret, "HS256")
	}
	read := func(t *testing.T, events query.SummaryEvents, alerts query.SummaryAlerts,
		owner string, want int,
	) nativeSummary {
		t.Helper()
		e := echo.New()
		log := zerolog.Nop()
		middleware.Apply(e, &log, nil)
		calls := 0
		query.NewSummaryHandler(events, alerts, verifier, func() time.Time {
			calls++
			return now.In(time.FixedZone("fixture", 19800))
		}).Register(e)
		r := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/summary", nil)
		r.Header.Set("Authorization", "Bearer "+token(owner))
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		require.Equal(t, want, w.Code, w.Body.String())
		require.Equal(t, 1, calls, "capture shared now exactly once")
		var result nativeSummary
		if want == 200 {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		} else {
			require.NotContains(t, w.Body.String(), "event_rate_per_sec")
			require.NotContains(t, w.Body.String(), "SELECT")
		}
		return result
	}
	event := func(id string, owner string, at time.Time, received time.Time) *contracts.Event {
		r := contracts.NewTimestamp(received)
		return &contracts.Event{SchemaVersion: 1, EventID: id, TenantID: owner,
			OccurredAt: contracts.NewTimestamp(at), ReceivedAt: &r, Source: "native-summary",
			EventType: "auth.failure", Attributes: map[string]any{}}
	}
	rows := []*contracts.Event{}
	for _, at := range []time.Time{now.Add(-time.Minute - time.Millisecond), now.Add(-time.Minute),
		now.Add(-time.Millisecond), now, start.Add(-time.Millisecond), start, end.Add(-time.Millisecond), end} {
		rows = append(rows, event(uuid.NewString(), tenant, at, now.Add(-time.Hour)))
	}
	// Duplicate identities with representations crossing both trend and rate
	// boundaries must retain the complete earliest-received canonical tuple.
	rows = append(rows, event(rows[0].EventID, tenant, now.Add(-time.Second), now),
		event(rows[4].EventID, tenant, start.Add(time.Second), now),
		event(rows[5].EventID, tenant, start.Add(time.Second), now.Add(-time.Hour)),
		event(uuid.NewString(), foreign, now.Add(-time.Second), now))
	store := storageclickhouse.NewEventStore(conn)
	require.NoError(t, store.InsertBatch(ctx, rows))
	require.NoError(t, store.InsertBatch(ctx, rows))
	var physical, unique uint64
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(),uniqExact(event_id) FROM events
	WHERE tenant_id={tenant:String}`, driverclickhouse.Named("tenant", tenant)).Scan(&physical, &unique))
	require.Greater(t, physical, unique, "query immediately while physical duplicates are retained")
	orphan := &contracts.Alert{SchemaVersion: 1,
		AlertID: strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", ""), TenantID: tenant,
		RuleID: "CH_ORPHAN", RuleVersion: 1, Severity: "critical", Status: "open",
		CreatedAt:   contracts.NewTimestamp(end.Add(-time.Second)),
		WindowStart: contracts.NewTimestamp(end.Add(-time.Minute)), WindowEnd: contracts.NewTimestamp(end),
		EntityKey: "fixture", Summary: "CH only orphan", EventCount: 1,
		SampleEventIDs: []string{rows[0].EventID}, DedupeKey: "summary-orphan"}
	require.NoError(t, orphan.Validate())
	require.NoError(t, storageclickhouse.NewAlertStore(conn).InsertBatch(ctx, []*contracts.Alert{orphan, orphan}))
	insert := func(owner, rule, severity, status string, at time.Time) string {
		t.Helper()
		id := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
		_, e := db.Pool.Exec(ctx, `INSERT INTO alert_workflows
		(alert_id,tenant_id,status,created_at,updated_at,rule_id,rule_version,severity,entity_key,
		summary,event_count,window_start,window_end,immutable_fingerprint)
		VALUES ($1,$2,$3,$4,$4,$5,1,$6,'fixture','Summary native',1,$4::timestamptz-interval '1 second',$4,$1)`,
			id, owner, status, at, rule, severity)
		require.NoError(t, e)
		return id
	}
	for i := range 22 {
		insert(tenant, fmt.Sprintf("RULE_%02d", i), "high", "resolved", end.Add(-time.Second))
	}
	changed := insert(tenant, "WINNER", "critical", "open", end.Add(-time.Second))
	insert(tenant, "WINNER", "high", "acknowledged", end.Add(-time.Second))
	insert(tenant, "AT_START", "low", "open", now.Add(-24*time.Hour))
	insert(tenant, "BEFORE_START", "medium", "resolved", now.Add(-24*time.Hour-time.Millisecond))
	insert(tenant, "AT_NOW", "low", "open", now)
	insert(tenant, "AT_TREND_START", "medium", "resolved", start)
	insert(tenant, "BEFORE_TREND", "medium", "resolved", start.Add(-time.Millisecond))
	insert(tenant, "AT_TREND_END", "medium", "resolved", end)
	insert(foreign, "FOREIGN", "critical", "open", end.Add(-time.Second))
	events, alerts := storageclickhouse.NewQueryEventStore(conn), storagepostgres.NewAlertStore(db.Pool, 5*time.Second)
	t.Run("exact_half_open_ranges_dedupe_tenants_and_top20", func(t *testing.T) {
		got := read(t, events, alerts, tenant, 200)
		require.InDelta(t, 2.0/60, got.Rate, 1e-12)
		require.Equal(t, uint64(3), got.Open)
		require.Equal(t, []query.SeverityCount{{Severity: "critical", Count: 1}, {Severity: "high", Count: 0},
			{Severity: "medium", Count: 0}, {Severity: "low", Count: 2}}, got.Severity)
		require.Len(t, got.Trend, 288)
		var eventTotal, alertTotal uint64
		for i, p := range got.Trend {
			require.Equal(t, start.Add(time.Duration(i)*5*time.Minute), p.Start)
			eventTotal += p.Events
			alertTotal += p.Alerts
		}
		require.Equal(t, uint64(2), eventTotal)
		require.Equal(t, uint64(27), alertTotal)
		require.Equal(t, uint64(1), got.Trend[0].Events)
		require.Equal(t, uint64(3), got.Trend[0].Alerts)
		require.Equal(t, uint64(1), got.Trend[287].Events)
		require.Equal(t, uint64(24), got.Trend[287].Alerts)
		require.Len(t, got.Rules, 20)
		require.Equal(t, query.RuleCount{RuleID: "WINNER", Count: 2}, got.Rules[0])
		require.Equal(t, query.RuleCount{RuleID: "AT_START", Count: 1}, got.Rules[1])
		require.Equal(t, query.RuleCount{RuleID: "AT_TREND_END", Count: 1}, got.Rules[2])
		for i := 3; i < 20; i++ {
			require.Equal(t, fmt.Sprintf("RULE_%02d", i-3), got.Rules[i].RuleID)
		}
		other := read(t, events, alerts, foreign, 200)
		require.InDelta(t, 1.0/60, other.Rate, 1e-12)
		require.Equal(t, uint64(1), other.Open)
		require.Equal(t, []query.RuleCount{{RuleID: "FOREIGN", Count: 1}}, other.Rules)
		empty := read(t, events, alerts, "p4empty-"+uuid.NewString(), 200)
		require.Zero(t, empty.Rate)
		require.Zero(t, empty.Open)
		require.Empty(t, empty.Rules)
		require.Len(t, empty.Trend, 288)
	})
	t.Run("current_status_changes_preserve_historical_counts", func(t *testing.T) {
		before := read(t, events, alerts, tenant, 200)
		_, err = db.Pool.Exec(ctx, `UPDATE alert_workflows SET status='acknowledged',version=version+1
		WHERE alert_id=$1 AND tenant_id=$2`, changed, tenant)
		require.NoError(t, err)
		after := read(t, events, alerts, tenant, 200)
		require.Equal(t, uint64(2), after.Open)
		require.Zero(t, after.Severity[0].Count)
		require.Equal(t, before.Rules, after.Rules)
		require.Equal(t, before.Trend, after.Trend)
	})
	t.Run("native_dependency_faults_return503_never_zero", func(t *testing.T) {
		badCH, e := storageclickhouse.Open(ctx, cfg.ClickHouse)
		require.NoError(t, e)
		require.NoError(t, badCH.Close())
		read(t, storageclickhouse.NewQueryEventStore(badCH), alerts, tenant, 503)
		badPG, e := database.New(ctx, cfg.Database, zerolog.Nop())
		require.NoError(t, e)
		badPG.Close()
		read(t, events, storagepostgres.NewAlertStore(badPG.Pool, 5*time.Second), tenant, 503)
	})
	t.Run("caller_cancellation_reaches_both_native_adapters", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		began := time.Now()
		_, eventErr := events.SummarizeEvents(cancelled, tenant, now)
		_, alertErr := alerts.SummarizeAlerts(cancelled, tenant, now)
		require.Error(t, eventErr)
		require.Error(t, alertErr)
		require.Less(t, time.Since(began), time.Second)
	})
	t.Run("rolling_pg_nanosecond_bounds", func(t *testing.T) {
		originalNow := now
		defer func() { now = originalNow }()
		testSummaryNanosecondBounds(t, func(t *testing.T, at time.Time, owner string) nativeSummary {
			now = at
			return read(t, events, alerts, owner, 200)
		}, insert)
	})
}

func testSummaryNanosecondBounds(t *testing.T,
	read func(*testing.T, time.Time, string) nativeSummary,
	insert func(string, string, string, string, time.Time) string,
) {
	t.Helper()
	base := time.Now().UTC().Truncate(5 * time.Minute)
	tenant, foreign := "p4precision-"+uuid.NewString(), "p4precision-"+uuid.NewString()
	for _, fixture := range []struct {
		rule  string
		at    time.Time
		count int
	}{
		{"LOWER_FLOOR", base.Add(-24 * time.Hour), 2},
		{"LOWER_NEXT", base.Add(-24*time.Hour + time.Millisecond), 1},
		{"MIDDLE", base.Add(-time.Hour), 2},
		{"UPPER_FLOOR", base, 3},
		{"UPPER_NEXT", base.Add(time.Millisecond), 1},
	} {
		for range fixture.count {
			insert(tenant, fixture.rule, "high", "open", fixture.at)
		}
	}
	insert(foreign, "FOREIGN", "critical", "open", base.Add(-time.Hour))
	aligned := read(t, base, tenant)
	for _, fixture := range []struct {
		name   string
		offset time.Duration
	}{
		{"aligned_millisecond", 0},
		{"one_nanosecond", time.Nanosecond},
		{"sub_microsecond", 999 * time.Nanosecond},
		{"exact_microsecond", time.Microsecond},
		{"next_aligned_millisecond", time.Millisecond},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			got := read(t, base.Add(fixture.offset), tenant)
			require.Equal(t, uint64(9), got.Open, "current open count has no rolling time filter")
			require.Equal(t, aligned.Severity, got.Severity)
			require.Equal(t, aligned.Trend, got.Trend, "completed trend bounds remain exactly aligned")
			require.Len(t, got.Trend, 288)
			require.Equal(t, uint64(3), got.Trend[0].Alerts)
			require.Equal(t, uint64(2), got.Trend[276].Alerts)
			require.Zero(t, got.Rate)
			want := []query.RuleCount{{RuleID: "LOWER_FLOOR", Count: 2},
				{RuleID: "MIDDLE", Count: 2}, {RuleID: "LOWER_NEXT", Count: 1}}
			if fixture.offset > 0 {
				want = []query.RuleCount{{RuleID: "UPPER_FLOOR", Count: 3},
					{RuleID: "MIDDLE", Count: 2}, {RuleID: "LOWER_NEXT", Count: 1}}
			}
			require.Equal(t, want, got.Rules,
				"valid millisecond rows must follow exact captured nanosecond bounds, including rule ordering")
		})
	}
	other := read(t, base.Add(time.Nanosecond), foreign)
	require.Equal(t, uint64(1), other.Open)
	require.Equal(t, []query.RuleCount{{RuleID: "FOREIGN", Count: 1}}, other.Rules)
}

func verifyRulesFailClosed(ctx context.Context, t *testing.T, binary, root, path string, raw []byte) {
	t.Helper()
	for _, fixture := range []struct{ name, body string }{
		{"malformed", "rules: ["},
		{"invalid_settings", strings.Replace(string(raw), "threshold: 17", "threshold: 0", 1)},
		{"missing", ""},
	} {
		t.Run("fail_closed_startup_"+fixture.name, func(t *testing.T) {
			if fixture.body != "" {
				require.NoError(t, os.WriteFile(path, []byte(fixture.body), 0o600))
			} else {
				_ = os.Remove(path)
			}
			httpAddress, opsAddress := freeAddress(t), freeAddress(t)
			_, hp, _ := strings.Cut(httpAddress, ":")
			_, op, _ := strings.Cut(opsAddress, ":")
			childCtx, stop := context.WithTimeout(ctx, 8*time.Second)
			defer stop()
			cmd := exec.CommandContext(childCtx, binary)
			cmd.Dir = filepath.Join(root, "backend")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "SENTINEL_SERVER.PORT=") && !strings.HasPrefix(entry, "SENTINEL_OPS.PORT=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "SENTINEL_SERVER.PORT="+hp, "SENTINEL_OPS.PORT="+op)
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			require.Error(t, cmd.Run(), "invalid startup rules must exit nonzero")
			require.NoError(t, childCtx.Err(), "a timeout is not failclosed startup proof")
			for _, address := range []string{httpAddress, opsAddress} {
				response, requestErr := (&http.Client{Timeout: time.Second}).Get("http://" + address + "/healthz")
				if response != nil {
					_ = response.Body.Close()
				}
				require.Error(t, requestErr, "no public or ops listener after invalid startup")
			}
		})
	}
}
