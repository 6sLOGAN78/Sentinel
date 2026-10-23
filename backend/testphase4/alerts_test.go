//go:build phase4

package testphase4_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	detectionconfig "github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/query"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
	"github.com/6sLOGAN78/sentinel/migrations"
)

type alertListRow struct {
	ID       string    `json:"alert_id"`
	Tenant   string    `json:"tenant_id"`
	Created  time.Time `json:"created_at"`
	Status   string    `json:"status"`
	Severity string    `json:"severity"`
	Rule     string    `json:"rule_id"`
	Summary  string    `json:"summary"`
}

type alertListPage struct {
	Items []alertListRow `json:"items"`
	Next  *string        `json:"next_cursor"`
	More  bool           `json:"has_more"`
}

// Removing any PG predicate, limiting before status filtering, or rounding the
// cursor differently from ORDER BY must break these actual HTTP/PG assertions.
func TestPhase4Alerts(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Setenv("SENTINEL_PRIMARY.ENV", "local")
	t.Setenv("SENTINEL_ENV_FILE", filepath.Join(root, ".env"))
	key := make([]byte, 48)
	_, err = rand.Read(key)
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(key)
	t.Setenv("SENTINEL_AUTH.JWT_SECRET", secret)
	cfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	require.NoError(t, database.Migrate(ctx, cfg.Database, zerolog.Nop()), "execute actual migrations")
	db, err := database.New(ctx, cfg.Database, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	tenant, foreign := "p4alerts-"+uuid.NewString(), "p4other-"+uuid.NewString()
	now := time.Now().UTC().Truncate(time.Millisecond)
	baseTime := now.Add(-time.Hour)
	ids := map[int]string{}
	insert := func(n int, owner, status, severity, rule, summary string, created time.Time) {
		t.Helper()
		id := strings.ReplaceAll(uuid.NewString(), "-", "") + fmt.Sprintf("%032x", n)
		// A shared per-fixture prefix gives hand-checkable tie ordering.
		if n > 1 {
			id = ids[1][:32] + fmt.Sprintf("%032x", n)
		}
		ids[n] = id
		_, insertErr := db.Pool.Exec(ctx, `INSERT INTO alert_workflows
		(alert_id,tenant_id,status,created_at,updated_at,rule_id,rule_version,severity,entity_key,
		 summary,event_count,window_start,window_end,immutable_fingerprint)
		VALUES ($1,$2,$3,$4,$4,$5,1,$6,'192.0.2.44',$7,10,$8,$9,$1)`,
			id, owner, status, created, rule, severity, summary, baseTime.Add(-time.Minute), baseTime)
		require.NoError(t, insertErr)
	}
	for i := 1; i <= 24; i++ {
		status := []string{"resolved", "open"}[i%2]
		// PG003 is timestamptz (microseconds), not timestamptz(3). Reverse
		// microsecond order inside each millisecond to expose cursor mismatch.
		created := baseTime.Add(time.Duration((i-1)/4)*time.Millisecond + time.Duration(4-(i-1)%4)*100*time.Microsecond)
		insert(i, tenant, status, "high", "AUTH_BRUTE_FORCE", "Native filter fixture", created)
	}
	insert(25, foreign, "open", "high", "AUTH_BRUTE_FORCE", "Foreign filter fixture", baseTime.Add(time.Second))
	insert(26, tenant, "open", "low", "PORT_SCAN", `Literal %_\ marker`, baseTime.Add(time.Second))
	insert(27, tenant, "open", "low", "PORT_SCAN", `Literal XX marker`, baseTime.Add(time.Second))
	insert(28, tenant, "acknowledged", "critical", "DENY_SPIKE", "Critical fixture", baseTime.Add(time.Second))
	insert(29, tenant, "open", "high", "AUTH_BRUTE_FORCE", "At exclusive end", now)
	insert(30, tenant, "open", "high", "AUTH_BRUTE_FORCE", "Before inclusive start", baseTime.Add(-time.Millisecond))
	// Independent competing rows make omission of either severity or rule fail
	// the exact tuple/full-page assertion, even when the other filter is present.
	insert(32, tenant, "open", "high", "PORT_SCAN", "Competing rule", baseTime.Add(time.Second))
	insert(33, tenant, "open", "low", "AUTH_BRUTE_FORCE", "Competing severity", baseTime.Add(time.Second))
	var precision int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM alert_workflows WHERE tenant_id=$1
	 AND created_at <> date_trunc('milliseconds', created_at)`, tenant).Scan(&precision))
	require.Equal(t, 24, precision, "fixture really exercises PG submillisecond precision")
	binary := filepath.Join(t.TempDir(), "query-api")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/query-api")
	build.Dir, build.Stdout, build.Stderr = filepath.Join(root, "backend"), io.Discard, io.Discard
	require.NoError(t, build.Run(), "binary compilation is setup, never behavioral RED")
	base, _ := startQueryAPI(t, binary, root, secret, "")
	tokenFor := func(owner string) string {
		return signToken(t, map[string]any{"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "alerts-operator",
			"tenant": owner, "role": "analyst", "iat": now.Unix(), "nbf": now.Unix(),
			"exp": now.Add(10 * time.Minute).Unix()}, secret, "HS256")
	}
	token := tokenFor(tenant)
	t.Run("detail_canonical_evidence_and_tenant_boundaries", func(t *testing.T) {
		testAlertDetails(ctx, t, cfg, base, binary, root, secret, token, tokenFor(foreign), tenant, foreign, ids)
	})
	getPage := func(t *testing.T, q url.Values, credential string) alertListPage {
		t.Helper()
		status, body := getJSON(t, base+"/api/v1/alerts?"+q.Encode(), credential, "")
		require.Equal(t, 200, status, "valid alert query must reach PG list")
		var page alertListPage
		require.NoError(t, json.Unmarshal(body, &page))
		return page
	}
	filters := func() url.Values {
		return url.Values{"from": {baseTime.Format(time.RFC3339Nano)}, "to": {now.Format(time.RFC3339Nano)},
			"status": {"open"}, "severity": {"high"}, "rule_id": {"AUTH_BRUTE_FORCE"}, "limit": {"3"}}
	}
	t.Run("full_sparse_status_pages_and_exact_millisecond_tuples", func(t *testing.T) {
		q, seen := filters(), []string{}
		for pageNumber := range 4 {
			page := getPage(t, q, token)
			require.Len(t, page.Items, 3, "status predicates must run before LIMIT; enough matches remain")
			for _, row := range page.Items {
				require.Equal(t, tenant, row.Tenant)
				require.Equal(t, "open", row.Status)
				require.Equal(t, "high", row.Severity)
				require.Equal(t, "AUTH_BRUTE_FORCE", row.Rule)
				require.Equal(t, row.Created.Truncate(time.Millisecond), row.Created)
				require.NotContains(t, seen, row.ID, "no overlap at timestamp ties")
				seen = append(seen, row.ID)
			}
			require.Equal(t, pageNumber < 3, page.More)
			if pageNumber < 3 {
				require.NotNil(t, page.Next)
				require.LessOrEqual(t, len(*page.Next), 2048)
				q.Set("cursor", *page.Next)
			} else {
				require.Nil(t, page.Next)
			}
		}
		want := []string{
			ids[23], ids[21], ids[19], ids[17], ids[15], ids[13], ids[11], ids[9], ids[7], ids[5], ids[3], ids[1],
		}
		require.Equal(t, want, seen)
	})
	t.Run("authenticated_cursor_tamper_and_binding", func(t *testing.T) {
		q := filters()
		page := getPage(t, q, token)
		require.NotNil(t, page.Next)
		q.Set("cursor", *page.Next)
		for _, field := range []string{"status", "severity", "rule_id", "search", "from", "to", "limit"} {
			t.Run(field, func(t *testing.T) {
				changed, cloneErr := url.ParseQuery(q.Encode())
				require.NoError(t, cloneErr)
				values := map[string]string{
					"status": "resolved", "severity": "low", "rule_id": "PORT_SCAN", "search": "different",
					"from": baseTime.Add(-time.Millisecond).Format(time.RFC3339Nano),
					"to":   now.Add(time.Millisecond).Format(time.RFC3339Nano), "limit": "4",
				}
				changed.Set(field, values[field])
				status, _ := getJSON(t, base+"/api/v1/alerts?"+changed.Encode(), token, "")
				require.Equal(t, 400, status)
			})
		}
		status, _ := getJSON(t, base+"/api/v1/alerts?"+q.Encode(), tokenFor(foreign), "")
		require.Equal(t, 400, status, "foreign verified tenant cannot replay cursor")
		raw, decodeErr := base64.RawURLEncoding.DecodeString(*page.Next)
		require.NoError(t, decodeErr)
		raw[len(raw)/2] ^= 1
		q.Set("cursor", base64.RawURLEncoding.EncodeToString(raw))
		status, _ = getJSON(t, base+"/api/v1/alerts?"+q.Encode(), token, "")
		require.Equal(t, 400, status, "well-formed base64 mutation must fail authentication")
	})
	t.Run("literal_search_all_filters_and_halfopen_range", func(t *testing.T) {
		q := url.Values{
			"from": {baseTime.Format(time.RFC3339Nano)}, "to": {now.Format(time.RFC3339Nano)},
			"severity": {"low"}, "rule_id": {"PORT_SCAN"},
		}
		for _, search := range []string{"%", "_", `\`, `%_\`} {
			q.Set("search", search)
			page := getPage(t, q, token)
			require.Len(t, page.Items, 1)
			require.Equal(t, ids[26], page.Items[0].ID)
		}
		q.Set("search", "lItErAl")
		require.Len(t, getPage(t, q, token).Items, 2, "case-insensitive literal summary match")
		q.Set("search", "192.0.2.44")
		require.Len(t, getPage(t, q, token).Items, 2, "literal entity match")
		q.Set("search", "' OR 1=1 --")
		require.Empty(t, getPage(t, q, token).Items, "search text cannot become SQL")
		q = url.Values{"status": {"acknowledged"}, "severity": {"critical"}, "rule_id": {"DENY_SPIKE"}}
		page := getPage(t, q, token)
		require.Len(t, page.Items, 1)
		require.Equal(t, ids[28], page.Items[0].ID)
	})
	t.Run("frozen_default_range_and_bounded_limit", func(t *testing.T) {
		q := url.Values{"limit": {"2"}}
		first := getPage(t, q, token)
		require.Len(t, first.Items, 2)
		require.NotNil(t, first.Next)
		insert(31, tenant, "open", "high", "AUTH_BRUTE_FORCE", "Arrived after frozen end",
			time.Now().UTC().Add(time.Millisecond))
		q.Set("cursor", *first.Next)
		page := getPage(t, q, token)
		require.Len(t, page.Items, 2)
		for _, row := range page.Items {
			require.NotContains(t, []string{first.Items[0].ID, first.Items[1].ID, ids[31]}, row.ID)
		}
		require.LessOrEqual(t, len(getPage(t, url.Values{"limit": {"200"}}, token).Items), 200)
	})
	t.Run("pg_caller_cancellation", func(t *testing.T) {
		canceled, stop := context.WithCancel(ctx)
		stop()
		_, readErr := storagepostgres.NewAlertStore(db.Pool, 5*time.Second).ListAlerts(canceled, tenant,
			query.AlertFilters{ListOptions: query.ListOptions{From: baseTime, To: now, Limit: 3}}, nil)
		require.ErrorIs(t, readErr, context.Canceled, "caller cancellation reaches the actual PG adapter")
	})
	for _, query := range []string{
		"limit=0", "limit=201", "limit=-1", "limit=1.5", "limit=", "limit=3&limit=3", "status=open&status=open",
		"cursor=", "cursor=bad", "cursor=" + strings.Repeat("x", 2049),
		"severity=urgent", "status=closed", "rule_id=oops", "tenant_id=" + foreign,
		"from=2026-10-21T00:00:00.0001Z", "from=2026-10-21T00:00:00%2B01:00", "from=invalid",
		"from=2026-10-22T00:00:00Z&to=2026-10-21T00:00:00Z", "from=2026-10-01T00:00:00Z&to=2026-10-09T00:00:00Z",
		"search=" + strings.Repeat("x", 129), "search=" + url.QueryEscape(strings.Repeat("é", 65)),
		"search=%00", "search=%ff", "limit=%zz", "search=%0Atrimmed", "search=trimmed%0A",
	} {
		t.Run("invalid_"+query[:min(len(query), 48)], func(t *testing.T) {
			status, _ := getJSON(t, base+"/api/v1/alerts?"+query, token, "")
			require.Equal(t, 400, status)
		})
	}
}

func testAlertDetails(ctx context.Context, t *testing.T, cfg *config.Config,
	base, binary, root, secret, token, foreignToken, tenant, foreign string, ids map[int]string,
) {
	t.Helper()
	_, err := chmigrate.New(cfg.ClickHouse.HTTPAddress, cfg.ClickHouse.User, cfg.ClickHouse.Password,
		cfg.ClickHouse.Database, nil).Run(ctx, migrations.ClickHouse())
	require.NoError(t, err, "native CH migrations are setup, not behavioral RED")
	conn, err := storageclickhouse.Open(ctx, cfg.ClickHouse)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	now := contracts.NewTimestamp(time.Now().UTC())
	missing, foreignOnly := uuid.NewString(), uuid.NewString()
	events := make([]*contracts.Event, 0, 20)
	samples := []string{missing, foreignOnly}
	for range 18 {
		event := &contracts.Event{SchemaVersion: 1, EventID: uuid.NewString(), TenantID: tenant,
			OccurredAt: now, ReceivedAt: &now, Source: "phase4-detail", EventType: "auth.failure",
			Attributes: map[string]any{"payload": `<img src=x onerror="alert('unsafe')">`}}
		events = append(events, event)
		samples = append(samples, event.EventID)
	}
	other := *events[0]
	other.EventID, other.TenantID = foreignOnly, foreign
	events = append(events, &other, events[0]) // Real duplicate must not consume a result slot.
	require.NoError(t, storageclickhouse.NewEventStore(conn).InsertBatch(ctx, events))
	detection := &contracts.Alert{SchemaVersion: 1, AlertID: ids[1], TenantID: tenant,
		RuleID: "AUTH_BRUTE_FORCE", RuleVersion: 77, Severity: "high", Status: "open", CreatedAt: now,
		WindowStart: contracts.NewTimestamp(now.Add(-time.Minute)), WindowEnd: now, EntityKey: "192.0.2.44",
		Summary: "Canonical CH detection", EventCount: 20, SampleEventIDs: samples, DedupeKey: "actual-canonical-dedupe"}
	require.NoError(t, detection.Validate())
	orphan := *detection
	orphan.AlertID = strings.ReplaceAll(uuid.NewString(), "-", "") + strings.Repeat("0", 32)
	foreignDetection := *detection
	foreignDetection.AlertID, foreignDetection.TenantID = ids[25], foreign
	repeated := *detection
	repeated.AlertID, repeated.SampleEventIDs = ids[3], []string{samples[2], samples[2], missing, missing}
	require.NoError(t, storageclickhouse.NewAlertStore(conn).InsertBatch(ctx,
		[]*contracts.Alert{detection, detection, &orphan, &foreignDetection, &repeated}))
	read := func(t *testing.T, address, id, credential string, expected int) []byte {
		t.Helper()
		status, body := getJSON(t, address+"/api/v1/alerts/"+id, credential, "")
		require.Equal(t, expected, status, "actual alert-detail HTTP behavior")
		return body
	}
	t.Run("actual_canonical_and_missing_retained_evidence", func(t *testing.T) {
		body := read(t, base, ids[1], token, 200)
		var detail struct {
			Workflow    query.WorkflowAlert `json:"workflow"`
			Detection   contracts.Alert     `json:"detection"`
			Evidence    []contracts.Event   `json:"evidence"`
			Missing     []string            `json:"missing_evidence_ids"`
			Explanation string              `json:"rule_explanation"`
			History     []any               `json:"history"`
		}
		require.NoError(t, json.Unmarshal(body, &detail))
		require.Equal(t, tenant, detail.Workflow.TenantID)
		require.Equal(t, int64(1), detail.Workflow.RuleVersion, "PG metadata is not CH canonical authority")
		require.Equal(t, *detection, detail.Detection)
		require.Len(t, detail.Evidence, 18)
		require.Equal(t, []string{missing, foreignOnly}, detail.Missing)
		seen := map[string]bool{}
		for _, event := range detail.Evidence {
			require.Equal(t, tenant, event.TenantID)
			require.False(t, seen[event.EventID])
			seen[event.EventID] = true
		}
		require.NotNil(t, detail.History)
		require.Empty(t, detail.History, "no fabricated audit before 08/09")
		require.Contains(t, detail.Explanation, "unavailable")
		require.Contains(t, detail.Explanation, "AUTH_BRUTE_FORCE")
		require.Contains(t, detail.Explanation, "77")
		require.NotContains(t, detail.Explanation, "threshold", "current config cannot stand in for version77")
	})
	t.Run("repeated_sample_ids_are_unique_and_missing_detection_is_not_fabricated", func(t *testing.T) {
		var detail struct {
			Evidence []contracts.Event `json:"evidence"`
			Missing  []string          `json:"missing_evidence_ids"`
		}
		require.NoError(t, json.Unmarshal(read(t, base, ids[3], token, 200), &detail))
		require.Len(t, detail.Evidence, 1)
		require.Equal(t, []string{missing}, detail.Missing)
		read(t, base, ids[5], token, 404)
	})
	t.Run("foreign_orphan_unknown_and_invalid", func(t *testing.T) {
		read(t, base, ids[25], token, 404)
		read(t, base, ids[1], foreignToken, 404)
		read(t, base, orphan.AlertID, token, 404)
		read(t, base, strings.Repeat("0", 64), token, 404)
		read(t, base, "bad-id", token, 400)
		read(t, base, url.PathEscape("' OR 1=1 --"), token, 400)
		read(t, base, ids[1]+"?tenant_id="+foreign, token, 400)
		read(t, base, ids[1], "", 401)
	})
	t.Run("retained_exact_manifest_and_unavailable_ambiguity", func(t *testing.T) {
		db, openErr := database.New(ctx, cfg.Database, zerolog.Nop())
		require.NoError(t, openErr)
		defer db.Close()
		rules := detectionconfig.Rules{SchemaVersion: 1}
		for _, rule := range []string{"AUTH_BRUTE_FORCE", "PORT_SCAN", "REQUEST_FLOOD", "DENY_SPIKE"} {
			rules.Rules = append(rules.Rules, detectionconfig.Rule{ID: rule, Version: 77, Enabled: true,
				Threshold: 13, Window: time.Minute, Severity: "high", Cooldown: time.Minute})
		}
		manifest, marshalErr := json.Marshal(rules)
		require.NoError(t, marshalErr)
		group, fingerprint := "p4detail-"+uuid.NewString(), rules.Fingerprint()
		fence := func(partition int, raw []byte) {
			t.Helper()
			_, insertErr := db.Pool.Exec(ctx, `INSERT INTO detection_partition_fences
			(group_id,topic,partition_id,incarnation,member_id,generation,assignment_token,config_fingerprint,rules_manifest)
			VALUES ($1,'phase4-detail',$2,$3,'fixture',1,1,$4,$5)`, group, partition, strings.Repeat("1", 64), fingerprint, raw)
			require.NoError(t, insertErr)
		}
		fence(0, manifest)
		canonical, marshalErr := json.Marshal(detection)
		require.NoError(t, marshalErr)
		_, insertErr := db.Pool.Exec(ctx, `INSERT INTO detection_decisions
		(alert_id,group_id,tenant_id,rule_id,rule_version,entity_components,config_fingerprint,allowed,
		trigger_occurred_at,effective_clock,topic,partition_id,source_offset,rule_ordinal,canonical_payload,
		immutable_fingerprint,decision_generation) VALUES ($1,$2,$3,'AUTH_BRUTE_FORCE',77,'[]',$4,true,
		$5,$5,'phase4-detail',0,0,0,$6,$1,1)`, ids[1], group, tenant, fingerprint, now.Time, canonical)
		require.NoError(t, insertErr)
		explanation := func() string {
			var value struct {
				Explanation string `json:"rule_explanation"`
			}
			require.NoError(t, json.Unmarshal(read(t, base, ids[1], token, 200), &value))
			return value.Explanation
		}
		require.Contains(t, explanation(), "threshold 13", "actual retained exact-version definition")
		conflicting := rules
		conflicting.Rules = append([]detectionconfig.Rule(nil), rules.Rules...)
		conflicting.Rules[0].Threshold = 999
		badManifest, marshalErr := json.Marshal(conflicting)
		require.NoError(t, marshalErr)
		fence(1, badManifest)
		require.Contains(t, explanation(), "unavailable", "conflicting retained data must not invent old parameters")
		for partition := 2; partition <= 33; partition++ {
			fence(partition, manifest)
		}
		require.Contains(t, explanation(), "unavailable", "row cap cannot establish unique history from truncated candidates")
	})
	t.Run("pg_first_when_ch_unreachable", func(t *testing.T) {
		unavailable, _ := startQueryAPI(t, binary, root, secret, "127.0.0.1:1")
		read(t, unavailable, ids[1], token, 503)
		read(t, unavailable, ids[25], token, 404)
		read(t, unavailable, orphan.AlertID, token, 404)
		status, body := getJSON(t, unavailable+"/api/v1/alerts?limit=3", token, "")
		require.Equal(t, 200, status, "PG list remains independent of CH")
		require.NotContains(t, string(body), "dedupe_key")
		require.NotContains(t, string(body), "sample_event_ids")
	})
}
