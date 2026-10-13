//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/eventquery"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	"github.com/6sLOGAN78/sentinel/migrations"
)

func newDedupeStore(t *testing.T) (*storageclickhouse.EventStore, storageclickhouse.Conn) {
	t.Helper()

	cfg := chCfg
	cfg.Database = "dedupe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := chmigrate.New(cfg.HTTPAddress, cfg.User, cfg.Password, cfg.Database, nil).
		Run(t.Context(), migrations.ClickHouse())
	require.NoError(t, err)

	conn, err := storageclickhouse.Open(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.NoError(t, conn.Exec(t.Context(), "SYSTEM STOP MERGES events"))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, conn.Exec(ctx, "SYSTEM START MERGES events"))
	})

	return storageclickhouse.NewEventStore(conn), conn
}

func storedEvent(id string, occurredAt, receivedAt time.Time) *contracts.Event {
	port := 443
	protocol := "tcp"
	return &contracts.Event{
		SchemaVersion:   1,
		EventID:         id,
		TenantID:        "demo",
		OccurredAt:      contracts.NewTimestamp(occurredAt),
		ReceivedAt:      ptr(contracts.NewTimestamp(receivedAt)),
		Source:          "integration-test",
		EventType:       "net.connection",
		SourceIP:        nil,
		DestinationIP:   ptr("2001:db8::10"),
		DestinationPort: &port,
		Protocol:        &protocol,
		Attributes:      map[string]any{"count": float64(2), "nested": map[string]any{"ok": true}},
	}
}

func ptr[T any](value T) *T { return &value }

func TestDedupeDoubleInsertNoOptimize(t *testing.T) {
	store, conn := newDedupeStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	events := []*contracts.Event{
		storedEvent(uuid.NewString(), now.Add(-3*time.Minute), now),
		storedEvent(uuid.NewString(), now.Add(-2*time.Minute), now),
		storedEvent(uuid.NewString(), now.Add(-time.Minute), now),
	}

	require.NoError(t, store.InsertBatch(t.Context(), events))
	require.NoError(t, store.InsertBatch(t.Context(), events))
	var rawCount uint64
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count() FROM events").Scan(&rawCount))
	require.Equal(t, uint64(2*len(events)), rawCount)
	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(len(events)), unique)

	got, err := store.Get(t.Context(), "demo", events[0].EventID)
	require.NoError(t, err)
	require.Equal(t, events[0], got)
}

func TestEventsSchema(t *testing.T) {
	_, conn := newDedupeStore(t)
	var ddl string
	require.NoError(t, conn.QueryRow(t.Context(), "SHOW CREATE TABLE events").Scan(&ddl))
	for _, clause := range []string{
		"ENGINE = ReplacingMergeTree",
		"PARTITION BY toYYYYMM(occurred_at)",
		"ORDER BY (tenant_id, occurred_at, event_id)",
		"TTL toDateTime(occurred_at) + toIntervalDay(7)",
		"INDEX idx_event_id event_id TYPE bloom_filter(0.01) GRANULARITY 4",
	} {
		require.Contains(t, ddl, clause)
	}
	require.NotContains(t, ddl, "non_replicated_deduplication_window")
}

func TestDedupeDifferentOccurredAt(t *testing.T) {
	store, conn := newDedupeStore(t)
	id := uuid.NewString()
	received := time.Now().UTC().Truncate(time.Millisecond)
	first := storedEvent(id, time.Date(2026, time.January, 31, 23, 59, 0, 0, time.UTC), received)
	second := storedEvent(id, time.Date(2026, time.February, 1, 0, 1, 0, 0, time.UTC), received)
	second.Source = "conflicting-content"
	require.NoError(t, store.InsertBatch(t.Context(), []*contracts.Event{first, second}))

	var rawCount uint64
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count() FROM events").Scan(&rawCount))
	require.Equal(t, uint64(2), rawCount)
	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(1), unique)
	got, err := store.Get(t.Context(), "demo", id)
	require.NoError(t, err)
	require.Equal(t, id, got.EventID)
	require.Equal(t, "demo", got.TenantID)
}

func TestEventLookupTenantScope(t *testing.T) {
	store, _ := newDedupeStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	event := storedEvent(uuid.NewString(), now.Add(-time.Minute), now)
	require.NoError(t, store.InsertBatch(t.Context(), []*contracts.Event{event}))

	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	keys := apikeys.NewStore(db.Pool)
	readKey, err := keys.Create(t.Context(), "demo", []string{apikeys.ScopeRead}, "query-read-"+uuid.NewString())
	require.NoError(t, err)
	otherKey, err := keys.Create(t.Context(), "other", []string{apikeys.ScopeRead}, "query-other-"+uuid.NewString())
	require.NoError(t, err)
	writeKey, err := keys.Create(t.Context(), "demo", []string{apikeys.ScopeWrite}, "query-write-"+uuid.NewString())
	require.NoError(t, err)

	verifier := apikeys.NewVerifier(keys, 32, time.Second, nil)
	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)
	eventquery.NewHandler(verifier, store).Register(e)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)

	request := func(key, id string) *http.Response {
		t.Helper()
		req, requestErr := http.NewRequestWithContext(
			t.Context(), http.MethodGet, server.URL+"/api/v1/events/"+id, nil,
		)
		require.NoError(t, requestErr)
		if key != "" {
			req.Header.Set("X-Api-Key", key)
		}
		response, requestErr := http.DefaultClient.Do(req)
		require.NoError(t, requestErr)
		t.Cleanup(func() { _ = response.Body.Close() })

		return response
	}

	response := request(readKey, event.EventID)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var got contracts.Event
	require.NoError(t, json.NewDecoder(response.Body).Decode(&got))
	require.Equal(t, event, &got)

	require.Equal(t, http.StatusNotFound, request(otherKey, event.EventID).StatusCode)
	require.Equal(t, http.StatusBadRequest, request(readKey, "invalid").StatusCode)
	require.Equal(t, http.StatusUnauthorized, request("", event.EventID).StatusCode)
	require.Equal(t, http.StatusUnauthorized, request("invalid", event.EventID).StatusCode)
	require.Equal(t, http.StatusForbidden, request(writeKey, event.EventID).StatusCode)
}
