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

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/query"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	"github.com/6sLOGAN78/sentinel/migrations"
)

type eventSearchPage struct {
	Items []contracts.Event `json:"items"`
	Next  *string           `json:"next_cursor"`
	More  bool              `json:"has_more"`
}

// Actual binary/native CH coverage. Inserts are queried immediately; no merge
// wait or OPTIMIZE can stand in for read-time identity deduplication.
func TestPhase4Events(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Setenv("SENTINEL_PRIMARY.ENV", "local")
	t.Setenv("SENTINEL_ENV_FILE", filepath.Join(root, ".env"))
	material := make([]byte, 48)
	_, err = rand.Read(material)
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(material)
	t.Setenv("SENTINEL_AUTH.JWT_SECRET", secret)
	cfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	require.NoError(t, database.Migrate(ctx, cfg.Database, zerolog.Nop()))
	db, err := database.New(ctx, cfg.Database, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	_, err = chmigrate.New(cfg.ClickHouse.HTTPAddress, cfg.ClickHouse.User, cfg.ClickHouse.Password,
		cfg.ClickHouse.Database, nil).Run(ctx, migrations.ClickHouse())
	require.NoError(t, err)
	conn, err := storageclickhouse.Open(ctx, cfg.ClickHouse)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	tenant, foreign := "p4events-"+uuid.NewString(), "p4other-"+uuid.NewString()
	key, err := apikeys.NewStore(db.Pool).Create(ctx, tenant, []string{apikeys.ScopeRead}, "event search fixture")
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Millisecond)
	from := now.Add(-time.Hour)
	t.Logf("EVENTS_FIXTURE tenant=%s from=%s to=%s", tenant, from.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	tokenFor := func(owner string) string {
		return signToken(t, map[string]any{"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "events-operator",
			"tenant": owner, "role": "viewer", "iat": now.Unix(), "nbf": now.Unix(),
			"exp": now.Add(10 * time.Minute).Unix()}, secret, "HS256")
	}
	token := tokenFor(tenant)
	binary := filepath.Join(t.TempDir(), "query-api")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, "./cmd/query-api")
	build.Dir, build.Stdout, build.Stderr = filepath.Join(root, "backend"), io.Discard, io.Discard
	require.NoError(t, build.Run(), "compilation is setup, not behavioral RED")
	base, _ := startQueryAPI(t, binary, root, secret, "")
	prefix := uuid.NewString()[:24]
	id := func(n int) string { return prefix + fmt.Sprintf("%012x", n) }
	str := func(s string) *string { return &s }
	makeEvent := func(n int, owner string, at time.Time) *contracts.Event {
		received := contracts.NewTimestamp(now.Add(-time.Minute))
		return &contracts.Event{SchemaVersion: 1, EventID: id(n), TenantID: owner,
			OccurredAt: contracts.NewTimestamp(at), ReceivedAt: &received, Source: "native-events", EventType: "auth.failure",
			SourceIP: str("192.0.2.44"), Outcome: str("failure"),
			Attributes: map[string]any{"safe": "<img src=x onerror=alert(1)>"}}
	}
	events := nativeEventFixtureRows(makeEvent, tenant, foreign, from, now)
	store := storageclickhouse.NewEventStore(conn)
	require.NoError(t, store.InsertBatch(ctx, events))
	require.NoError(t, store.InsertBatch(ctx, events), "double insert immediately before queries")
	// A later received representation must not move an identity across filters,
	// time bounds or page positions. Different ORDER BY keys also resist merges.
	later := makeEvent(1, tenant, from.Add(time.Second))
	later.EventType, later.Outcome = "net.request", str("success")
	lateReceived := contracts.NewTimestamp(now)
	later.ReceivedAt = &lateReceived
	moved := makeEvent(61, tenant, from.Add(2*time.Second))
	moved.ReceivedAt = &lateReceived
	tie := makeEvent(1, tenant, from.Add(3*time.Second))
	// Same received_at but different event time: deterministic tie chooses the
	// lexically first canonical tuple, preserving the original earliest time.
	require.NoError(t, store.InsertBatch(ctx, []*contracts.Event{later, moved, tie}))
	var physical, unique uint64
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(),uniqExact(event_id) FROM events WHERE tenant_id={tenant:String}`,
		driverclickhouse.Named("tenant", tenant)).Scan(&physical, &unique))
	require.Greater(t, physical, unique, "actual physical duplicates exist before the immediately following reads")
	t.Logf("EVENTS_PHYSICAL rows=%d identities=%d", physical, unique)
	t.Run("native_adapter_keeps_earliest_received_representation", func(t *testing.T) {
		items, listErr := storageclickhouse.NewQueryEventStore(conn).ListEvents(ctx, tenant, query.EventFilters{
			ListOptions: query.ListOptions{From: from, To: now, Limit: 200},
		})
		require.NoError(t, listErr)
		require.Len(t, items, 61)
		for _, item := range items {
			if item.EventID == id(1) {
				require.Equal(t, from, item.OccurredAt.Time)
				require.Equal(t, "auth.failure", item.EventType)
			}
		}
	})
	filters := func() url.Values {
		return url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {now.Format(time.RFC3339Nano)}, "limit": {"3"},
			"source_ip": {"192.0.2.44"}, "event_type": {"auth.failure"}, "outcome": {"failure"}}
	}
	getPage := func(t *testing.T, q url.Values, credential string) eventSearchPage {
		t.Helper()
		status, body := getJSON(t, base+"/api/v1/events?"+q.Encode(), credential, "")
		require.Equal(t, 200, status, "valid event search must reach native CH")
		var page eventSearchPage
		require.NoError(t, json.Unmarshal(body, &page))
		require.NotNil(t, page.Items, "empty pages are JSON arrays")
		return page
	}
	t.Run("immediate_dedupe_full_pages_ties_and_filters", func(t *testing.T) {
		testFullEventSearchPages(t, filters(), getPage, tenant, token, id)
	})
	t.Run("half_open_default_and_maximum_pages", func(t *testing.T) {
		q := url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {now.Format(time.RFC3339Nano)}, "limit": {"200"}}
		page := getPage(t, q, token)
		require.Len(t, page.Items, 61)
		for _, event := range page.Items {
			require.Equal(t, tenant, event.TenantID)
			require.NotEqual(t, id(61), event.EventID)
			require.NotEqual(t, id(62), event.EventID)
		}
		require.False(t, page.More)
		require.Nil(t, page.Next)
		defaults := getPage(t, url.Values{}, token)
		require.Len(t, defaults.Items, 50)
		require.True(t, defaults.More)
		q = filters()
		q.Set("source_ip", "2001:0db8:0:0:0:0:0:44")
		q.Set("event_type", "net.request")
		q.Set("outcome", "success")
		q.Set("limit", "200")
		ipv6 := getPage(t, q, token)
		require.Len(t, ipv6.Items, 15, "IPv6 filter is canonicalized")
		q = filters()
		q.Del("source_ip")
		q.Set("event_type", "policy.decision")
		q.Del("outcome")
		q.Set("limit", "200")
		require.Len(t, getPage(t, q, token).Items, 15)
		q.Set("source_ip", "192.0.2.99")
		require.Empty(t, getPage(t, q, token).Items)
	})
	t.Run("cursor_binding_freezing_and_tamper", func(t *testing.T) {
		q := filters()
		first := getPage(t, q, token)
		require.NotNil(t, first.Next)
		q.Set("cursor", *first.Next)
		for field, value := range map[string]string{
			"source_ip": "192.0.2.45", "event_type": "net.request", "outcome": "success", "limit": "4",
			"from": from.Add(-time.Millisecond).Format(time.RFC3339Nano),
			"to":   now.Add(time.Millisecond).Format(time.RFC3339Nano),
		} {
			changed, parseErr := url.ParseQuery(q.Encode())
			require.NoError(t, parseErr)
			changed.Set(field, value)
			status, _ := getJSON(t, base+"/api/v1/events?"+changed.Encode(), token, "")
			require.Equal(t, 400, status, field)
		}
		status, _ := getJSON(t, base+"/api/v1/events?"+q.Encode(), tokenFor(foreign), "")
		require.Equal(t, 400, status)
		status, _ = getJSON(t, base+"/api/v1/alerts?cursor="+url.QueryEscape(*first.Next), token, "")
		require.Equal(t, 400, status)
		raw, decodeErr := base64.RawURLEncoding.DecodeString(*first.Next)
		require.NoError(t, decodeErr)
		raw[len(raw)/2] ^= 1
		q.Set("cursor", base64.RawURLEncoding.EncodeToString(raw))
		status, _ = getJSON(t, base+"/api/v1/events?"+q.Encode(), token, "")
		require.Equal(t, 400, status)
		defaults := getPage(t, url.Values{"limit": {"3"}}, token)
		require.NotNil(t, defaults.Next)
		next := getPage(t, url.Values{"limit": {"3"}, "cursor": {*defaults.Next}}, token)
		require.Len(t, next.Items, 3)
		require.NotEqual(t, defaults.Items[0].EventID, next.Items[0].EventID)
	})
	t.Run("strict_invalid_queries", func(t *testing.T) {
		for _, invalid := range []string{
			"limit=201", "limit=0", "limit=-1", "limit=1.5", "limit=999999999999999999999999",
			"limit=1&limit=2", "limit=", "cursor=", "cursor=bad", "cursor=" + strings.Repeat("x", 2049),
			"unknown=x", "tenant_id=" + foreign, "event_type=unknown", "event_type=auth.failure&event_type=auth.success",
			"outcome=deny", "outcome=null", "source_ip=bad", "source_ip=192.0.2.1%27%20OR%201=1",
			"source_ip=fe80::1%25eth0", "source_ip=::ffff:192.0.2.1", "source_ip=", "from=bad",
			"from=2026-10-22T00:00:00%2B00:00", "to=2026-10-22T00:00:00.0001Z",
			"from=2026-10-20T00:00:00Z&to=2026-10-20T00:00:00Z",
			"from=2026-10-20T00:00:00Z&to=2026-10-19T00:00:00Z",
			"from=2026-10-01T00:00:00Z&to=2026-10-09T00:00:00Z",
			"from=2026-10-20T00:00:00Z&from=2026-10-21T00:00:00Z", "source_ip=%ZZ",
		} {
			t.Run(invalid[:min(len(invalid), 70)], func(t *testing.T) {
				status, _ := getJSON(t, base+"/api/v1/events?"+invalid, token, "")
				require.Equal(t, 400, status)
			})
		}
	})
	t.Run("tenant_and_legacy_auth_boundaries", func(t *testing.T) {
		foreignPage := getPage(t, url.Values{}, tokenFor(foreign))
		require.Len(t, foreignPage.Items, 1)
		require.Equal(t, foreign, foreignPage.Items[0].TenantID)
		for _, credential := range []string{"", "invalid"} {
			status, _ := getJSON(t, base+"/api/v1/events", credential, key)
			require.Equal(t, 401, status)
		}
		status, body := getJSON(t, base+"/api/v1/events/"+id(2), "", key)
		require.Equal(t, 200, status)
		var event contracts.Event
		require.NoError(t, json.Unmarshal(body, &event))
		require.Equal(t, id(2), event.EventID)
		status, _ = getJSON(t, base+"/api/v1/events/"+id(64), "", key)
		require.Equal(t, 404, status)
		status, _ = getJSON(t, base+"/api/v1/events/"+id(2), "invalid", key)
		require.Equal(t, 401, status)
	})
	t.Run("dependency_failure_is_not_empty_success", func(t *testing.T) {
		unavailable, _ := startQueryAPI(t, binary, root, secret, freeAddress(t))
		status, _ := getJSON(t, unavailable+"/api/v1/events", token, "")
		require.Equal(t, 503, status)
	})
}

func testFullEventSearchPages(
	t *testing.T, q url.Values, getPage func(*testing.T, url.Values, string) eventSearchPage,
	tenant, token string, id func(int) string,
) {
	t.Helper()
	seen := []string{}
	for pageNumber := range 5 {
		page := getPage(t, q, token)
		require.Len(t, page.Items, 3)
		for _, event := range page.Items {
			require.Equal(t, tenant, event.TenantID)
			require.Equal(t, "auth.failure", event.EventType)
			require.Equal(t, "192.0.2.44", *event.SourceIP)
			require.Equal(t, "failure", *event.Outcome)
			require.NotContains(t, seen, event.EventID)
			seen = append(seen, event.EventID)
		}
		require.Equal(t, pageNumber < 4, page.More)
		if pageNumber < 4 {
			require.NotNil(t, page.Next)
			require.LessOrEqual(t, len(*page.Next), 2048)
			q.Set("cursor", *page.Next)
		} else {
			require.Nil(t, page.Next)
		}
	}
	want := []string{}
	for n := 57; n >= 1; n -= 4 {
		want = append(want, id(n))
	}
	require.Equal(t, want, seen)
}

func nativeEventFixtureRows(
	makeEvent func(int, string, time.Time) *contracts.Event, tenant, foreign string, from, now time.Time,
) []*contracts.Event {
	str := func(s string) *string { return &s }
	events := make([]*contracts.Event, 0, 65)
	for n := 1; n <= 60; n++ {
		event := makeEvent(n, tenant, from.Add(time.Duration((n-1)/5)*time.Millisecond))
		switch n % 4 {
		case 2:
			event.SourceIP, event.EventType, event.Outcome = str("2001:db8::44"), "net.request", str("success")
		case 3:
			event.SourceIP, event.EventType, event.Outcome = str("198.51.100.1"), "policy.decision", nil
		case 0:
			event.SourceIP, event.EventType, event.Outcome = str("203.0.113.1"), "auth.success", str("success")
		}
		if n == 60 {
			event.Source = "<img src=x onerror=alert(1)>"
		}
		events = append(events, event)
	}
	events = append(events, makeEvent(61, tenant, from.Add(-time.Millisecond)), makeEvent(62, tenant, now))
	boundary := makeEvent(63, tenant, from)
	boundary.EventType = "net.connection"
	return append(events, boundary, makeEvent(64, foreign, from.Add(time.Second)))
}
