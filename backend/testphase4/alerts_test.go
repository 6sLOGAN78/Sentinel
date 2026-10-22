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

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/query"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
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
