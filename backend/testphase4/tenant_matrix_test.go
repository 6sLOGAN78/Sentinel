//go:build phase4

package testphase4_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	"github.com/6sLOGAN78/sentinel/migrations"
)

// matrixTenant owns every identifier one tenant may see. Nothing is shared with
// the other tenant, so any leaked identifier is attributable.
type matrixTenant struct {
	name     string
	detail   string            // alert with ClickHouse detection and evidence
	patch    map[string]string // role -> alert that role may try to triage
	all      []string          // every alert owned by the tenant
	events   []string
	evidence []string
	open     int
}

func matrixAlertID(n int) string {
	return strings.ReplaceAll(uuid.NewString(), "-", "") + fmt.Sprintf("%032x", n)
}

func matrixCall(t *testing.T, method, address, credential, body string, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, address, reader)
	require.NoError(t, err)
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, raw
}

func matrixIDs(t *testing.T, raw []byte, field string) []string {
	t.Helper()
	var page struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	ids := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		id, ok := item[field].(string)
		require.True(t, ok)
		ids = append(ids, id)
	}
	return ids
}

//nolint:gocognit,funlen // One exact native family retains the whole surfaces x tenants x roles matrix.
func TestPhase4TenantMatrix(t *testing.T) {
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
	require.NoError(t, database.Migrate(ctx, cfg.Database, zerolog.Nop()), "actual PG migrations are setup")
	_, err = chmigrate.New(cfg.ClickHouse.HTTPAddress, cfg.ClickHouse.User, cfg.ClickHouse.Password,
		cfg.ClickHouse.Database, nil).Run(ctx, migrations.ClickHouse())
	require.NoError(t, err, "actual CH migrations are setup")
	db, err := database.New(ctx, cfg.Database, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	conn, err := storageclickhouse.Open(ctx, cfg.ClickHouse)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	roles := []string{"viewer", "analyst", "admin"}
	now := time.Now().UTC().Truncate(time.Millisecond)
	from := now.Add(-time.Hour)
	seed := func(name string, extra int) *matrixTenant {
		t.Helper()
		owner := &matrixTenant{name: name, patch: map[string]string{}}
		serial := 0
		insert := func(rule, summary string) string {
			serial++
			id := matrixAlertID(serial)
			_, insertErr := db.Pool.Exec(ctx, `INSERT INTO alert_workflows
			(alert_id,tenant_id,created_at,updated_at,rule_id,rule_version,severity,entity_key,
			 summary,event_count,window_start,window_end,immutable_fingerprint)
			VALUES ($1,$2,$3,$3,$4,1,'high','192.0.2.44',$5,2,$6,$3,$1)`,
				id, name, now.Add(-time.Minute+time.Duration(serial)*time.Millisecond), rule, summary,
				now.Add(-2*time.Minute))
			require.NoError(t, insertErr)
			owner.all = append(owner.all, id)
			owner.open++
			return id
		}
		owner.detail = insert("AUTH_BRUTE_FORCE", "Matrix detail "+name)
		for _, role := range roles {
			owner.patch[role] = insert("PORT_SCAN", "Matrix triage "+role+" "+name)
		}
		for range extra {
			insert("DENY_SPIKE", "Matrix extra "+name)
		}
		received := contracts.NewTimestamp(now)
		events := make([]*contracts.Event, 0, 3)
		for i := range 3 {
			event := &contracts.Event{SchemaVersion: 1, EventID: uuid.NewString(), TenantID: name,
				OccurredAt: contracts.NewTimestamp(now.Add(-time.Duration(i+1) * time.Second)), ReceivedAt: &received,
				Source: "matrix", EventType: "auth.failure",
				Attributes: map[string]any{"payload": `<img src=x onerror="alert('matrix')">`}}
			events = append(events, event)
			owner.events = append(owner.events, event.EventID)
		}
		owner.evidence = owner.events[:2]
		require.NoError(t, storageclickhouse.NewEventStore(conn).InsertBatch(ctx, events))
		detections := make([]*contracts.Alert, 0, len(owner.all))
		for _, id := range owner.all {
			detection := &contracts.Alert{SchemaVersion: 1, AlertID: id, TenantID: name,
				RuleID: "AUTH_BRUTE_FORCE", RuleVersion: 1, Severity: "high", Status: "open", CreatedAt: received,
				WindowStart: contracts.NewTimestamp(now.Add(-2 * time.Minute)), WindowEnd: received,
				EntityKey: "192.0.2.44", Summary: "Matrix detection " + name, EventCount: 2,
				SampleEventIDs: owner.evidence, DedupeKey: "matrix-" + uuid.NewString()}
			require.NoError(t, detection.Validate())
			detections = append(detections, detection)
		}
		require.NoError(t, storageclickhouse.NewAlertStore(conn).InsertBatch(ctx, detections))
		return owner
	}
	tenants := []*matrixTenant{seed("p4mxa-"+uuid.NewString(), 0), seed("p4mxb-"+uuid.NewString(), 1)}
	other := func(i int) *matrixTenant { return tenants[1-i] }

	binary := filepath.Join(t.TempDir(), "query-api")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/query-api")
	build.Dir, build.Stdout, build.Stderr = filepath.Join(root, "backend"), io.Discard, io.Discard
	require.NoError(t, build.Run(), "binary compilation is setup, never behavioral RED")
	replicaA, _ := startQueryAPI(t, binary, root, secret, "")
	replicaB, _ := startQueryAPI(t, binary, root, secret, "")
	replicas := []string{replicaA, replicaB}

	token := func(tenant, role string, extra map[string]any) string {
		claims := map[string]any{"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "matrix-" + role,
			"tenant": tenant, "role": role, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(15 * time.Minute).Unix()}
		for k, v := range extra {
			claims[k] = v
		}
		return signToken(t, claims, secret, "HS256")
	}
	rangeQuery := func(extra url.Values) string {
		q := url.Values{"from": {from.Format("2006-01-02T15:04:05.000Z")},
			"to": {now.Add(time.Second).Format("2006-01-02T15:04:05.000Z")}}
		for k, v := range extra {
			q[k] = v
		}
		return q.Encode()
	}
	var rulesBody []byte

	// Phase 1 is read-only so the exact delivery/count expectations are stable.
	for i, owner := range tenants {
		foreign := other(i)
		for j, role := range roles {
			base := replicas[(i+j)%2]
			credential := token(owner.name, role, nil)
			t.Run("read/"+role+"/tenant"+fmt.Sprint(i), func(t *testing.T) {
				// session: identity is the verified token, never a header or extra claim.
				status, header, raw := matrixCall(t, http.MethodGet, base+"/api/v1/session", credential, "",
					map[string]string{"X-Tenant-ID": foreign.name, "X-Tenant": foreign.name, "X-Role": "admin",
						"X-Forwarded-For": "203.0.113.9"})
				require.Equal(t, 200, status)
				var session struct{ Tenant, Role, Subject string }
				var wire map[string]any
				require.NoError(t, json.Unmarshal(raw, &wire))
				session.Tenant, session.Role, session.Subject = wire["tenant_id"].(string), wire["role"].(string), wire["subject"].(string)
				require.Equal(t, owner.name, session.Tenant)
				require.Equal(t, role, session.Role)
				require.Contains(t, header.Get("Cache-Control"), "no-store")
				require.NotContains(t, string(raw), credential)
				confused := token(owner.name, role, map[string]any{"tenant_id": foreign.name, "tid": foreign.name,
					"tenants": []string{foreign.name}})
				_, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/session", confused, "", nil)
				require.Contains(t, string(raw), owner.name)
				require.NotContains(t, string(raw), foreign.name)

				// alert list: exact own identifiers, tampering and cursor binding.
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/alerts?"+rangeQuery(url.Values{"limit": {"200"}}), credential, "", nil)
				require.Equal(t, 200, status)
				require.ElementsMatch(t, owner.all, matrixIDs(t, raw, "alert_id"))
				for _, id := range foreign.all {
					require.NotContains(t, string(raw), id)
				}
				for _, bad := range []url.Values{{"tenant_id": {foreign.name}}, {"tenant": {foreign.name}}, {"role": {"admin"}}} {
					status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts?"+rangeQuery(bad), credential, "", nil)
					require.Equal(t, 400, status, "tenant or role query tampering is rejected, never honored")
				}
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/alerts?"+rangeQuery(url.Values{"limit": {"2"}}), credential, "", nil)
				require.Equal(t, 200, status)
				var page struct {
					Next *string `json:"next_cursor"`
					More bool    `json:"has_more"`
				}
				require.NoError(t, json.Unmarshal(raw, &page))
				require.True(t, page.More)
				require.NotNil(t, page.Next)
				cursor := *page.Next
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/alerts?"+url.Values{"cursor": {cursor}, "limit": {"2"}}.Encode(), credential, "", nil)
				require.Equal(t, 200, status, "own cursor continues")
				for _, id := range foreign.all {
					require.NotContains(t, string(raw), id)
				}
				foreignCredential := token(foreign.name, role, nil)
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts?"+url.Values{"cursor": {cursor}, "limit": {"2"}}.Encode(), foreignCredential, "", nil)
				require.Equal(t, 400, status, "a cursor minted for another tenant is rejected")
				tampered := cursor[:len(cursor)-2] + "AA"
				if tampered == cursor {
					tampered = cursor[:len(cursor)-2] + "BB"
				}
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts?"+url.Values{"cursor": {tampered}, "limit": {"2"}}.Encode(), credential, "", nil)
				require.Equal(t, 400, status)
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts?"+url.Values{"cursor": {cursor}, "limit": {"3"}}.Encode(), credential, "", nil)
				require.Equal(t, 400, status, "cursor is bound to its limit")

				// detail, evidence, history.
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/alerts/"+owner.detail, credential, "", nil)
				require.Equal(t, 200, status)
				var detail struct {
					Workflow struct {
						TenantID string `json:"tenant_id"`
					} `json:"workflow"`
					Evidence []contracts.Event `json:"evidence"`
					Missing  []string          `json:"missing_evidence_ids"`
					History  []any             `json:"history"`
				}
				require.NoError(t, json.Unmarshal(raw, &detail))
				require.Equal(t, owner.name, detail.Workflow.TenantID)
				require.Empty(t, detail.Missing)
				require.NotNil(t, detail.History)
				got := []string{}
				for _, event := range detail.Evidence {
					require.Equal(t, owner.name, event.TenantID)
					got = append(got, event.EventID)
				}
				require.ElementsMatch(t, owner.evidence, got)
				for _, hidden := range []string{foreign.detail, strings.ToUpper(foreign.detail), foreign.all[1]} {
					status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts/"+hidden, credential, "", nil)
					require.Contains(t, []int{404, 400}, status)
				}
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts/"+foreign.detail, credential, "", nil)
				require.Equal(t, 404, status, "foreign alert existence is not disclosed")
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts/"+owner.detail+"?tenant_id="+foreign.name, credential, "", nil)
				require.Equal(t, 400, status)

				// event search and lookup.
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/events?"+rangeQuery(url.Values{"limit": {"200"}}), credential, "", nil)
				require.Equal(t, 200, status)
				require.ElementsMatch(t, owner.events, matrixIDs(t, raw, "event_id"))
				for _, id := range foreign.events {
					require.NotContains(t, string(raw), id)
				}
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/events?"+rangeQuery(url.Values{"tenant_id": {foreign.name}}), credential, "", nil)
				require.Equal(t, 400, status)
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/events/"+owner.events[0], credential, "", nil)
				require.Equal(t, 200, status)
				require.Contains(t, string(raw), owner.name)
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/events/"+foreign.events[0], credential, "", nil)
				require.Equal(t, 404, status)

				// statistics are tenant scoped.
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/metrics/summary", credential, "", nil)
				require.Equal(t, 200, status)
				var summary struct {
					Open  int `json:"open_alerts"`
					Trend []any
				}
				require.NoError(t, json.Unmarshal(raw, &summary))
				require.Equal(t, owner.open, summary.Open)
				// The summary accepts no caller scope; a spoofed tenant is rejected, never honored.
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/metrics/summary?tenant_id="+foreign.name, credential, "", nil)
				require.Equal(t, 400, status, "tenant query tampering is rejected")
				require.NotContains(t, string(raw), foreign.name, "tenant query tampering cannot select another tenant")

				// rules are one global read-only policy, identical for every tenant and role.
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/rules", credential, "", nil)
				require.Equal(t, 200, status)
				if rulesBody == nil {
					rulesBody = raw
				}
				require.JSONEq(t, string(rulesBody), string(raw))
				for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
					status, _, _ = matrixCall(t, method, base+"/api/v1/rules", credential, `{"enabled":false}`, nil)
					require.Contains(t, []int{404, 405}, status, method+" cannot mutate rules")
				}

				// durable backfill and live stream expose only this tenant's deliveries.
				status, _, raw = matrixCall(t, http.MethodGet, base+"/api/v1/alerts/backfill?after_seq=0&limit=200", credential, "", nil)
				require.Equal(t, 200, status)
				var delivered struct {
					Items []struct {
						Alert struct {
							ID     string `json:"alert_id"`
							Tenant string `json:"tenant_id"`
						} `json:"alert"`
					} `json:"items"`
				}
				require.NoError(t, json.Unmarshal(raw, &delivered))
				seen := []string{}
				for _, item := range delivered.Items {
					require.Equal(t, owner.name, item.Alert.Tenant)
					seen = append(seen, item.Alert.ID)
				}
				require.ElementsMatch(t, owner.all, seen)
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts/backfill?after_seq=9223372036854775807", credential, "", nil)
				require.Equal(t, 400, status, "future cursors are rejected")
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts/stream?access_token="+credential, "", "", nil)
				require.Equal(t, 401, status, "a URL token is not a credential")
				status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/alerts/stream?access_token="+credential, credential, "", nil)
				require.Equal(t, 400, status, "URL parameters are rejected before the stream starts")
				streamCtx, stop := context.WithTimeout(ctx, 20*time.Second)
				defer stop()
				response := streamRequest(streamCtx, t, replicas[(i+j+1)%2], credential, "0", "")
				defer func() { _ = response.Body.Close() }()
				require.Equal(t, 200, response.StatusCode)
				live := []string{}
				for _, item := range streamRead(t, response, len(owner.all)) {
					require.Equal(t, owner.name, item.Alert.TenantID)
					live = append(live, item.Alert.AlertID)
				}
				require.ElementsMatch(t, owner.all, live)

				// missing or invalid credentials never reach data.
				for _, path := range []string{"/api/v1/alerts", "/api/v1/events", "/api/v1/metrics/summary", "/api/v1/rules", "/api/v1/session"} {
					status, _, _ = matrixCall(t, http.MethodGet, base+path, "", "", map[string]string{"X-Tenant-ID": owner.name})
					require.Equal(t, 401, status)
				}
				for name, extra := range map[string]map[string]any{
					"expired":   {"exp": now.Add(-time.Minute).Unix()},
					"audience":  {"aud": "other"},
					"issuer":    {"iss": "other"},
					"role":      {"role": "superuser"},
					"tenantcap": {"tenant": strings.ToUpper(owner.name)},
				} {
					status, _, _ = matrixCall(t, http.MethodGet, base+"/api/v1/session", token(owner.name, role, extra), "", nil)
					require.Equal(t, 401, status, name)
				}
			})
		}
	}

	// Phase 2 mutates triage state. Viewer is denied, other tenants are hidden, stale writes conflict.
	for i, owner := range tenants {
		foreign := other(i)
		for j, role := range roles {
			base := replicas[(i+j)%2]
			credential := token(owner.name, role, nil)
			t.Run("triage/"+role+"/tenant"+fmt.Sprint(i), func(t *testing.T) {
				mine, theirs := owner.patch[role], foreign.patch[role]
				state := func(tenant, id string) (string, int64) {
					var status string
					var version int64
					require.NoError(t, db.Pool.QueryRow(ctx, `SELECT status,version FROM alert_workflows
					 WHERE tenant_id=$1 AND alert_id=$2`, tenant, id).Scan(&status, &version))
					return status, version
				}
				patch := func(id, body string, headers map[string]string) int {
					status, _, _ := matrixCall(t, http.MethodPatch, base+"/api/v1/alerts/"+id, credential, body, headers)
					return status
				}
				if role == "viewer" {
					require.Equal(t, 403, patch(mine, `{"status":"acknowledged","version":1}`, map[string]string{"X-Role": "admin"}))
					require.Equal(t, 403, patch(theirs, `{"status":"acknowledged","version":1}`, nil))
					status, version := state(owner.name, mine)
					require.Equal(t, "open", status)
					require.Equal(t, int64(1), version)
					return
				}
				for _, body := range []string{`{"status":"acknowledged","version":1,"tenant_id":"` + foreign.name + `"}`,
					`{"status":"acknowledged"}`, `{"status":"acknowledged","version":1} {"x":1}`, `{"status":"closed","version":1}`} {
					require.Equal(t, 400, patch(mine, body, nil), body)
				}
				beforeStatus, beforeVersion := state(foreign.name, theirs)
				require.Equal(t, 404, patch(theirs, `{"status":"acknowledged","version":`+fmt.Sprint(beforeVersion)+`}`, nil), "foreign alert is hidden and untouched")
				foreignStatus, foreignVersion := state(foreign.name, theirs)
				require.Equal(t, beforeStatus, foreignStatus)
				require.Equal(t, beforeVersion, foreignVersion)
				require.Equal(t, 200, patch(mine, `{"status":"acknowledged","version":1}`, nil))
				require.Equal(t, 409, patch(mine, `{"status":"acknowledged","version":1}`, nil), "stale version conflicts")
				require.Equal(t, 400, patch(mine, `{"status":"open","version":2}`, nil), "reopen is not a valid target")
				require.Equal(t, 200, patch(mine, `{"status":"resolved","version":2}`, nil))
				require.Equal(t, 409, patch(mine, `{"status":"acknowledged","version":3}`, nil), "resolved cannot move backward")
				status, version := state(owner.name, mine)
				require.Equal(t, "resolved", status)
				require.Equal(t, int64(3), version)
				detailStatus, _, raw := matrixCall(t, http.MethodGet, base+"/api/v1/alerts/"+mine, credential, "", nil)
				require.Equal(t, 200, detailStatus)
				var detail struct {
					History []struct {
						Actor string `json:"actor"`
						To    string `json:"to_status"`
					} `json:"history"`
				}
				require.NoError(t, json.Unmarshal(raw, &detail))
				require.Len(t, detail.History, 2)
				for _, change := range detail.History {
					require.Equal(t, "matrix-"+role, change.Actor, "actor comes from the verified subject")
				}
				hiddenStatus, _, body := matrixCall(t, http.MethodGet, base+"/api/v1/alerts/"+mine, token(foreign.name, role, nil), "", nil)
				require.Equal(t, 404, hiddenStatus)
				require.False(t, bytes.Contains(body, []byte(owner.name)))
			})
		}
	}
}
