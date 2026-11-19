//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/query"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

// oracleUniqueEventRowsSQL is the pre-05-22 whole-tenant dedupe, copied
// verbatim. Every ListEvents page must equal this query's page for the same
// tenant, filters, range and cursor.
const oracleUniqueEventRowsSQL = `SELECT event_id,tenant_id,
 retained.1 AS occurred_at,retained.2 AS received_at,retained.3 AS source,
 retained.4 AS event_type,retained.5 AS source_ip,retained.6 AS destination_ip,
 retained.7 AS destination_port,retained.8 AS protocol,retained.9 AS principal,
 retained.10 AS action,retained.11 AS outcome,retained.12 AS attributes_json
 FROM (
 SELECT event_id,tenant_id,
 argMin(tuple(occurred_at,received_at,source,event_type,source_ip,destination_ip,
 destination_port,protocol,principal,action,outcome,attributes_json),
 tuple(received_at,occurred_at,source,event_type,source_ip,destination_ip,
 destination_port,protocol,principal,action,outcome,attributes_json)) AS retained
 FROM events WHERE tenant_id={tenant:String} GROUP BY tenant_id,event_id
 )`

// seedScaleSQL generates rows server-side. Values are bound parameters only;
// event_id is deterministic in number so tenants can collide on purpose.
const seedScaleSQL = `INSERT INTO events (tenant_id,event_id,occurred_at,received_at,source,
 event_type,source_ip,destination_ip,destination_port,protocol,principal,action,outcome,attributes_json)
 SELECT {tenant:String},
 concat('00000000-0000-4000-8000-', leftPad(lower(hex(number)), 12, '0')),
 fromUnixTimestamp64Milli({newest:Int64} - toInt64(intDiv(number * {span:UInt64}, {total:UInt64})), 'UTC'),
 fromUnixTimestamp64Milli({received:Int64}, 'UTC'),
 'scale-test', if(number % 2 = 0, 'auth.failure', 'net.connection'),
 concat('10.0.', toString(intDiv(number % 65536, 256)), '.', toString(number % 256)),
 '192.0.2.10', toUInt16(443), 'tcp', concat('user-', toString(number % 1000)), 'login',
 if(number % 3 = 0, 'failure', 'success'), concat('{"n":', toString(number), '}')
 FROM numbers({count:UInt64})`

const (
	scaleRows        = 1_000_000
	scaleDuplicates  = 1_000
	scaleForeignRows = 100_000
	scaleSpan        = 40 * time.Minute
	scalePageLimit   = 50
	scaleCallBudget  = time.Second
	scaleReadDivisor = 4
	fixturePageLimit = 7
	fullOraclePage   = 1 << 20
)

func scaleEventID(n uint64) string { return fmt.Sprintf("00000000-0000-4000-8000-%012x", n) }

// seedScale inserts count generated rows for tenant spread over scaleSpan.
func seedScale(
	t *testing.T, conn storageclickhouse.Conn, tenant string, newest, received time.Time, count, total uint64,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	require.NoError(t, conn.Exec(ctx, seedScaleSQL,
		driverclickhouse.Named("tenant", tenant),
		driverclickhouse.Named("newest", newest.UnixMilli()),
		driverclickhouse.Named("span", uint64(scaleSpan.Milliseconds())),
		driverclickhouse.Named("total", total),
		driverclickhouse.Named("received", received.UnixMilli()),
		driverclickhouse.Named("count", count)))
}

// oraclePredicates mirrors the pre-05-22 ListEvents predicate builder exactly.
func oraclePredicates(f query.EventFilters) (string, []any) {
	predicates := []string{"occurred_at >= fromUnixTimestamp64Milli({from:Int64},'UTC')",
		"occurred_at < fromUnixTimestamp64Milli({to:Int64},'UTC')"}
	args := []any{driverclickhouse.Named("from", f.From.UnixMilli()), driverclickhouse.Named("to", f.To.UnixMilli())}
	for _, filter := range []struct{ column, name, value string }{
		{"source_ip", "ip", f.SourceIP}, {"event_type", "type", f.EventType}, {"outcome", "outcome", f.Outcome},
	} {
		if filter.value != "" {
			predicate := filter.column + " = {" + filter.name + ":String}"
			if filter.column == "source_ip" {
				predicate = "toIPv6OrNull(source_ip) = toIPv6OrNull({ip:String})"
			}
			predicates = append(predicates, predicate)
			args = append(args, driverclickhouse.Named(filter.name, filter.value))
		}
	}
	if f.After != nil {
		predicates = append(predicates,
			"(occurred_at,event_id) < (fromUnixTimestamp64Milli({after:Int64},'UTC'),{id:String})")
		args = append(args, driverclickhouse.Named("after", f.After.OccurredAt.UnixMilli()),
			driverclickhouse.Named("id", f.After.EventID))
	}
	return strings.Join(predicates, " AND "), args
}

// oracleList runs the whole-tenant dedupe with the same predicates. ctx may
// carry a query id so its read_bytes can be measured.
func oracleList(
	ctx context.Context, t *testing.T, conn storageclickhouse.Conn, tenant string, f query.EventFilters, pageSize uint64,
) []contracts.Event {
	t.Helper()
	where, args := oraclePredicates(f)
	args = append(args, driverclickhouse.Named("tenant", tenant), driverclickhouse.Named("page_size", pageSize))
	rows, err := conn.Query(ctx, oracleUniqueEventRowsSQL+" WHERE "+where+
		" ORDER BY occurred_at DESC,event_id DESC LIMIT {page_size:UInt64}", args...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	items := []contracts.Event{}
	for rows.Next() {
		var event contracts.Event
		var occurred, received time.Time
		var port *uint16
		var attributes string
		require.NoError(t, rows.Scan(&event.EventID, &event.TenantID, &occurred, &received, &event.Source,
			&event.EventType, &event.SourceIP, &event.DestinationIP, &port, &event.Protocol, &event.Principal,
			&event.Action, &event.Outcome, &attributes))
		event.SchemaVersion = contracts.EventSchemaVersion
		event.OccurredAt = contracts.NewTimestamp(occurred)
		receivedAt := contracts.NewTimestamp(received)
		event.ReceivedAt = &receivedAt
		if port != nil {
			value := int(*port)
			event.DestinationPort = &value
		}
		event.Attributes = decodeAttributes(t, attributes)
		items = append(items, event)
	}
	require.NoError(t, rows.Err())
	return items
}

func decodeAttributes(t *testing.T, raw string) map[string]any {
	t.Helper()
	var attributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &attributes))
	return attributes
}

// queryReadBytes sums read_bytes of every finished query whose id is prefix
// or prefix-<attempt>. It fails if no finished query was found.
func queryReadBytes(t *testing.T, conn storageclickhouse.Conn, prefix string) uint64 {
	t.Helper()
	require.NoError(t, conn.Exec(t.Context(), "SYSTEM FLUSH LOGS"))
	var finished, readBytes uint64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(), sum(read_bytes) FROM system.query_log
	 WHERE type = 'QueryFinish' AND (query_id = {prefix:String} OR query_id LIKE concat({prefix:String}, '-%'))`,
		driverclickhouse.Named("prefix", prefix)).Scan(&finished, &readBytes))
	require.Positive(t, finished, "no finished query found for id %s", prefix)
	return readBytes
}

func TestListEventsScale(t *testing.T) {
	_, conn := newDedupeStore(t)
	store := storageclickhouse.NewQueryEventStore(conn)
	now := time.Now().UTC().Truncate(time.Millisecond)
	newest := now.Add(-time.Minute)
	seedScale(t, conn, "scale-a", newest, now, scaleRows, scaleRows)
	seedScale(t, conn, "scale-a", newest, now, scaleDuplicates, scaleRows)
	seedScale(t, conn, "scale-b", newest, now.Add(-time.Second), scaleForeignRows, scaleForeignRows)

	middle := uint64(scaleRows / 2)
	pages := map[string]*query.EventPosition{
		"first": nil,
		"middle_cursor": {
			OccurredAt: newest.Add(-time.Duration(middle*uint64(scaleSpan.Milliseconds())/scaleRows) * time.Millisecond),
			EventID:    scaleEventID(middle),
		},
	}
	for name, after := range pages {
		t.Run(name, func(t *testing.T) {
			f := query.EventFilters{ListOptions: query.ListOptions{
				From: now.Add(-24 * time.Hour), To: now, Limit: scalePageLimit,
			}, After: after}
			prefix := "list-" + uuid.NewString()
			started := time.Now()
			got, err := store.ListEvents(driverclickhouse.Context(t.Context(), driverclickhouse.WithQueryID(prefix)),
				"scale-a", f)
			elapsed := time.Since(started)
			t.Logf("SCALE_LIST page=%s duration=%s err=%v", name, elapsed, err)
			require.NoError(t, err)

			oracleID := "oracle-" + uuid.NewString()
			oracleCtx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			want := oracleList(driverclickhouse.Context(oracleCtx, driverclickhouse.WithQueryID(oracleID)),
				t, conn, "scale-a", f, scalePageLimit+1)
			listBytes, oracleBytes := queryReadBytes(t, conn, prefix), queryReadBytes(t, conn, oracleID)
			t.Logf("SCALE_READ page=%s list_read_bytes=%d oracle_read_bytes=%d ratio=%.4f",
				name, listBytes, oracleBytes, float64(listBytes)/float64(oracleBytes))
			require.Equal(t, want, got, "scale page must equal the whole-tenant dedupe page")
			require.Len(t, got, scalePageLimit+1)
			require.Less(t, elapsed, scaleCallBudget, "ListEvents must finish with 5x headroom below queryTimeout")
			require.Less(t, listBytes*scaleReadDivisor, oracleBytes, "ListEvents must read under 1/4 of the oracle")
		})
	}
}

// fixtureEvent builds a deterministic representation for the equivalence fixture.
func fixtureEvent(tenant string, n int, occurred, received time.Time) *contracts.Event {
	types := []string{"auth.failure", "net.connection", "net.request"}
	outcomes := []*string{ptr("failure"), ptr("success"), nil}
	ips := []*string{ptr("2001:db8::1"), ptr("192.0.2.7"), nil, ptr("2001:db8:0:0:0:0:0:2")}
	port := 1000 + n%7
	return &contracts.Event{
		SchemaVersion: contracts.EventSchemaVersion, EventID: uuid.NewSHA1(uuid.NameSpaceOID,
			fmt.Appendf(nil, "fixture-%d", n)).String(),
		TenantID: tenant, OccurredAt: contracts.NewTimestamp(occurred), ReceivedAt: ptr(contracts.NewTimestamp(received)),
		Source: "equivalence", EventType: types[n%len(types)], SourceIP: ips[n%len(ips)],
		DestinationIP: ptr("198.51.100.9"), DestinationPort: &port, Protocol: ptr("tcp"),
		Outcome: outcomes[n%len(outcomes)], Attributes: map[string]any{"n": float64(n)},
	}
}

func cloneEvent(event *contracts.Event) *contracts.Event {
	clone := *event
	return &clone
}

// equivalenceFixture builds base rows plus conflicting, tied and retried
// representations. Occurrence times collide every 5 rows so event_id breaks ties.
func equivalenceFixture(tenant string, base time.Time, count int) ([]*contracts.Event, []*contracts.Event) {
	received := base.Add(time.Hour)
	events := make([]*contracts.Event, 0, count)
	for n := range count {
		events = append(events, fixtureEvent(tenant, n, base.Add(-time.Duration(n/5)*time.Second), received))
	}
	extra := []*contracts.Event{}
	for n := 0; n < count; n += 11 {
		// Later received conflict with different occurred_at and content: the
		// earlier representation keeps winning, the later key must never surface.
		later := cloneEvent(events[n])
		later.OccurredAt = contracts.NewTimestamp(events[n].OccurredAt.Add(-time.Duration(n%97+1) * time.Minute))
		later.ReceivedAt = ptr(contracts.NewTimestamp(received.Add(time.Minute)))
		later.EventType, later.Outcome = "conflict.later", ptr("success")
		// Earlier received conflict on every 33rd row moves the winner instead.
		if n%33 == 0 {
			later.ReceivedAt = ptr(contracts.NewTimestamp(received.Add(-time.Minute)))
		}
		extra = append(extra, later)
	}
	for n := 3; n < count; n += 17 {
		// Equal received tie with a different occurrence time and source.
		tie := cloneEvent(events[n])
		tie.OccurredAt = contracts.NewTimestamp(events[n].OccurredAt.Add(time.Duration(n%13+1) * time.Second))
		tie.Source = "a-tie"
		extra = append(extra, tie)
	}
	return events, extra
}

func insertFixture(t *testing.T, store *storageclickhouse.EventStore, events, extra []*contracts.Event) {
	t.Helper()
	for range 3 {
		require.NoError(t, store.InsertBatch(t.Context(), events), "identical retries")
	}
	require.NoError(t, store.InsertBatch(t.Context(), extra))
}

// walkPages compares every ListEvents page with the oracle, follows the
// cursor, rejects repeated identities and returns the walked identities.
func walkPages(
	t *testing.T, store *storageclickhouse.QueryEventStore, conn storageclickhouse.Conn, tenant string,
	f query.EventFilters,
) []contracts.Event {
	t.Helper()
	full := oracleList(t.Context(), t, conn, tenant, f, fullOraclePage)
	seen := map[string]bool{}
	walked := []contracts.Event{}
	for {
		got, err := store.ListEvents(t.Context(), tenant, f)
		require.NoError(t, err)
		want := oracleList(t.Context(), t, conn, tenant, f, uint64(f.Limit)+1)
		require.Equal(t, want, got, "page after %+v", f.After)
		page := got
		if len(got) > f.Limit {
			page = got[:f.Limit]
		}
		for _, event := range page {
			require.False(t, seen[event.EventID], "event_id %s repeated across pages", event.EventID)
			seen[event.EventID] = true
			require.Equal(t, tenant, event.TenantID)
		}
		walked = append(walked, page...)
		if len(got) <= f.Limit {
			break
		}
		last := page[len(page)-1]
		f.After = &query.EventPosition{OccurredAt: last.OccurredAt.Time, EventID: last.EventID}
	}
	require.Equal(t, full, walked, "union of walked pages must equal the oracle result")
	return walked
}

func TestListEventsOracleEquivalence(t *testing.T) {
	writer, conn := newDedupeStore(t)
	store := storageclickhouse.NewQueryEventStore(conn)
	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	const perTenant = 2_500
	for _, tenant := range []string{"equiv-a", "equiv-b"} {
		events, extra := equivalenceFixture(tenant, base, perTenant)
		insertFixture(t, writer, events, extra)
	}
	whole := query.ListOptions{From: base.Add(-24 * time.Hour), To: base.Add(time.Second), Limit: fixturePageLimit}
	// Boundaries land exactly on stored occurrence times (rows collide per second).
	boundary := query.ListOptions{From: base.Add(-200 * time.Second), To: base.Add(-100 * time.Second),
		Limit: fixturePageLimit}
	cases := map[string]query.EventFilters{
		"full_walk":           {ListOptions: whole},
		"source_ip_alternate": {ListOptions: whole, SourceIP: "2001:DB8:0::1"},
		"event_type":          {ListOptions: whole, EventType: "net.connection"},
		"conflict_type":       {ListOptions: whole, EventType: "conflict.later"},
		"outcome":             {ListOptions: whole, Outcome: "failure"},
		"exact_boundaries":    {ListOptions: boundary},
		"boundaries_filtered": {ListOptions: boundary, Outcome: "success", EventType: "auth.failure"},
		"single_row_pages":    {ListOptions: query.ListOptions{From: boundary.From, To: boundary.To, Limit: 1}},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			walked := walkPages(t, store, conn, "equiv-a", f)
			if name == "full_walk" || name == "exact_boundaries" {
				require.NotEmpty(t, walked)
			}
		})
	}
}

func TestListEventsTenantIsolation(t *testing.T) {
	writer, conn := newDedupeStore(t)
	store := storageclickhouse.NewQueryEventStore(conn)
	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	const rows = 300
	events, extra := equivalenceFixture("iso-a", base, rows)
	insertFixture(t, writer, events, extra)
	f := query.EventFilters{ListOptions: query.ListOptions{
		From: base.Add(-24 * time.Hour), To: base.Add(time.Second), Limit: fixturePageLimit,
	}}
	before := walkPages(t, store, conn, "iso-a", f)
	require.NotEmpty(t, before)

	// Same event_ids and keys, earlier received and different content: if the
	// foreign tenant leaked into candidate or winner selection it would win.
	foreign := make([]*contracts.Event, 0, len(events)+len(extra))
	for _, event := range append(append([]*contracts.Event{}, events...), extra...) {
		clone := cloneEvent(event)
		clone.TenantID, clone.Source = "iso-b", "foreign"
		clone.ReceivedAt = ptr(contracts.NewTimestamp(base.Add(-time.Hour)))
		foreign = append(foreign, clone)
	}
	require.NoError(t, writer.InsertBatch(t.Context(), foreign))
	after := walkPages(t, store, conn, "iso-a", f)
	require.Equal(t, before, after, "foreign tenant rows must not change tenant A pages")
	for _, filtered := range []query.EventFilters{
		{ListOptions: f.ListOptions, EventType: "net.request"}, {ListOptions: f.ListOptions, Outcome: "success"},
	} {
		for _, event := range walkPages(t, store, conn, "iso-a", filtered) {
			require.NotEqual(t, "foreign", event.Source)
		}
	}
}
