package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/stretchr/testify/require"
)

type eventBatchQuery struct {
	query string
	args  []any
	rows  *eventBatchRows
}

func (q *eventBatchQuery) Query(_ context.Context, query string, args ...any) (driver.Rows, error) {
	q.query, q.args = query, args
	return q.rows, nil
}

type eventBatchRows struct {
	driver.Rows
	events []*contracts.Event
	next   int
}

func (r *eventBatchRows) Next() bool { r.next++; return r.next <= len(r.events) }
func (*eventBatchRows) Close() error { return nil }
func (*eventBatchRows) Err() error   { return nil }
func (r *eventBatchRows) Scan(dest ...any) error {
	e := r.events[r.next-1]
	*dest[0].(*string) = e.EventID
	*dest[1].(*string) = e.TenantID
	*dest[2].(*time.Time) = e.OccurredAt.Time
	*dest[3].(*time.Time) = e.ReceivedAt.Time
	*dest[4].(*string) = e.Source
	*dest[5].(*string) = e.EventType
	*dest[6].(**string) = e.SourceIP
	*dest[7].(**string) = e.DestinationIP
	*dest[8].(**uint16) = nil
	*dest[9].(**string) = e.Protocol
	*dest[10].(**string) = e.Principal
	*dest[11].(**string) = e.Action
	*dest[12].(**string) = e.Outcome
	raw, err := json.Marshal(e.Attributes)
	if err != nil {
		return err
	}
	*dest[13].(*string) = string(raw)
	return nil
}

func TestCheckAlertsEventBatch(t *testing.T) {
	at := contracts.NewTimestamp(time.Now().UTC())
	e := &contracts.Event{SchemaVersion: 1, EventID: uuid.NewString(), TenantID: "demo", OccurredAt: at,
		ReceivedAt: &at, Source: "fixture", EventType: "auth.success", Attributes: map[string]any{"service": "example"}}
	raw, err := json.Marshal(e)
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	want := map[string]string{e.EventID: hex.EncodeToString(digest[:])}
	query := &eventBatchQuery{rows: &eventBatchRows{events: []*contracts.Event{e}}}
	actual, err := eventSnapshots(t.Context(), query, "demo", []string{e.EventID}, time.Time{})
	require.NoError(t, err)
	require.Equal(t, want, actual)
	require.Contains(t, query.query, "tenant_id = {tenant:String}")
	require.Contains(t, query.query, "event_id IN {ids:Array(String)}")
	require.Contains(t, query.query, "ORDER BY received_at ASC")
	require.Contains(t, query.query, "LIMIT 1 BY event_id")
	require.NotContains(t, query.query, e.EventID)
	require.Len(t, query.args, 2)
	for _, tc := range []struct {
		name string
		rows []*contracts.Event
	}{
		{"missing", nil}, {"duplicate", []*contracts.Event{e, e}},
		{"extra", []*contracts.Event{e, {SchemaVersion: 1, EventID: uuid.NewString(),
			TenantID: "demo", OccurredAt: at, ReceivedAt: &at}}},
		{"wrong tenant", []*contracts.Event{{SchemaVersion: 1, EventID: e.EventID,
			TenantID: "demo2", OccurredAt: at, ReceivedAt: &at}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query.rows = &eventBatchRows{events: tc.rows}
			_, readErr := eventSnapshots(t.Context(), query, "demo", []string{e.EventID}, time.Time{})
			require.Error(t, readErr)
		})
	}
	m := manifest{PriorEvents: want, PriorCH: map[string]string{}, PriorPG: map[string]string{}}
	require.True(t, priorSnapshotsMatch(m, actual, map[string]string{}, map[string]string{}))
	m.PriorEvents = map[string]string{e.EventID: "corrupt"}
	require.False(t, priorSnapshotsMatch(m, actual, map[string]string{}, map[string]string{}),
		"corrupt prior full-row hash")
}

func TestCheckAlertsCurrentEventRange(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Millisecond)
	old := contracts.NewTimestamp(start.Add(-time.Second))
	e := &contracts.Event{SchemaVersion: 1, EventID: uuid.NewString(), TenantID: "demo",
		OccurredAt: old, ReceivedAt: &old, Attributes: map[string]any{}}
	for _, name := range []string{"malicious", "benign zero"} {
		t.Run(name, func(t *testing.T) {
			query := &eventBatchQuery{rows: &eventBatchRows{events: []*contracts.Event{e}}}
			_, err := eventSnapshots(t.Context(), query, "demo", []string{e.EventID}, start)
			require.Error(t, err, "historical rows cannot satisfy current acknowledged IDs")
			require.Contains(t, query.query, "received_at >= {since:DateTime64(3)}")
			require.Len(t, query.args, 3)
		})
	}
	query := &eventBatchQuery{rows: &eventBatchRows{events: []*contracts.Event{e}}}
	_, err := eventSnapshots(t.Context(), query, "demo", []string{e.EventID}, time.Time{})
	require.NoError(t, err, "prior preservation keeps unbounded historical full-row hashing")
}

func TestCheckAlertsManifest(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/detection/scenarios.json")
	require.NoError(t, err)
	var fixtures struct {
		Cases []struct {
			Expected []struct {
				Payload json.RawMessage `json:"payload"`
			} `json:"expected_alerts"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	var canonical bytes.Buffer
	require.NoError(t, json.Compact(&canonical, fixtures.Cases[0].Expected[0].Payload))
	payload := canonical.Bytes()
	m := manifest{Tenant: "demo", SourceKeys: []string{"demo|192.0.2.11"},
		EventIDs: []string{"current-source"}, Expected: []string{string(payload)}}
	t.Run("exact across three independent stores", func(t *testing.T) {
		require.NoError(t, reconcile(m, [][]byte{payload}, [][]byte{payload}, [][]byte{payload}))
	})
	for _, tc := range []struct {
		name          string
		kafka, ch, pg [][]byte
	}{
		{"historical only", nil, [][]byte{payload}, [][]byte{payload}},
		{"missing CH", [][]byte{payload}, nil, [][]byte{payload}},
		{"missing workflow", [][]byte{payload}, [][]byte{payload}, nil},
		{"extra", [][]byte{payload, payload}, [][]byte{payload}, [][]byte{payload}},
		{"wrong tenant", [][]byte{bytes.Replace(payload,
			[]byte(`"tenant_id":"demo"`), []byte(`"tenant_id":"demo2"`), 1)}, [][]byte{payload}, [][]byte{payload}},
		{"wrong window", [][]byte{bytes.Replace(payload,
			[]byte("00:05:00"), []byte("00:10:00"), 1)}, [][]byte{payload}, [][]byte{payload}},
		{"wrong payload", [][]byte{payload}, [][]byte{bytes.Replace(payload,
			[]byte("Repeated failed"), []byte("Unexpected failed"), 1)}, [][]byte{payload}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Error(t, reconcile(m, tc.kafka, tc.ch, tc.pg)) })
	}
	t.Run("benign zero", func(t *testing.T) {
		m.Expected = nil
		require.NoError(t, reconcile(m, nil, nil, nil))
		require.Error(t, reconcile(m, [][]byte{payload}, nil, nil))
	})
	t.Run("strict private input", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "private.json")
		m.SchemaVersion = 1
		m.EventIDs = nil
		body, marshalErr := json.Marshal(m)
		require.NoError(t, marshalErr)
		require.NoError(t, os.WriteFile(path, body, 0o600))
		_, loadErr := loadManifest(path)
		require.NoError(t, loadErr)
		for _, bad := range [][]byte{
			bytes.Replace(body, []byte(`"tenant":"demo"`), []byte(`"tenant":"demo","tenant":"demo"`), 1),
			bytes.Replace(body, []byte(`"tenant":"demo"`), []byte(`"tenant":"demo","unexpected":true`), 1),
			bytes.Replace(body, []byte(`"source_keys":["demo|192.0.2.11"]`), []byte(`"source_keys":null`), 1),
			append(bytes.Clone(body), []byte(` {}`)...),
		} {
			require.NoError(t, os.WriteFile(path, bad, 0o600))
			_, loadErr = loadManifest(path)
			require.Error(t, loadErr)
		}
		require.NoError(t, os.WriteFile(path, body, 0o600))
		link := filepath.Join(t.TempDir(), "link")
		require.NoError(t, os.Symlink(path, link))
		_, loadErr = loadManifest(link)
		require.Error(t, loadErr)
		require.NoError(t, os.Chmod(path, 0o644))
		_, loadErr = loadManifest(path)
		require.Error(t, loadErr)
	})
}
