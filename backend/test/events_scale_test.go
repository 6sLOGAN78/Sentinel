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

// oracleSummarySQL is the pre-05-23 SummarizeEvents query over the
// whole-tenant dedupe, copied verbatim. SummarizeEvents must equal it.
const oracleSummarySQL = `SELECT
	 toStartOfInterval(occurred_at, INTERVAL 5 MINUTE, 'UTC') AS bucket,
	 count(),countIf(occurred_at >= fromUnixTimestamp64Nano({rate:Int64},'UTC'))
	 FROM (` + oracleUniqueEventRowsSQL + `) WHERE
	 occurred_at >= fromUnixTimestamp64Nano({start:Int64},'UTC') AND
	 occurred_at < fromUnixTimestamp64Nano({now:Int64},'UTC')
	 GROUP BY bucket ORDER BY bucket`

// summaryCorrectionComment is the log_comment SummarizeEvents attaches to the
// conflict-correction round trip; every other logged query is the main pass.
const summaryCorrectionComment = "sentinel.summarize_events.correction"

// summaryMainColumn reports whether the main pass may read column.
func summaryMainColumn(column string) bool {
	return column == "tenant_id" || column == "event_id" || column == "occurred_at"
}

// oracleSummary evaluates the whole-tenant summary exactly like the old code.
func oracleSummary(
	ctx context.Context, t *testing.T, conn storageclickhouse.Conn, tenant string, now time.Time,
) query.EventSummary {
	t.Helper()
	end := now.UTC().Truncate(query.SummaryBucketWidth)
	rows, err := conn.Query(ctx, oracleSummarySQL, driverclickhouse.Named("tenant", tenant),
		driverclickhouse.Named("rate", now.Add(-time.Minute).UnixNano()),
		driverclickhouse.Named("start", end.Add(-24*time.Hour).UnixNano()),
		driverclickhouse.Named("now", now.UnixNano()))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	result := query.EventSummary{}
	for rows.Next() {
		var at time.Time
		var count, rate uint64
		require.NoError(t, rows.Scan(&at, &count, &rate))
		result.RateCount += rate
		if at.Before(end) {
			result.Trend = append(result.Trend, query.SummaryCount{Start: at.UTC(), Count: count})
		}
	}
	require.NoError(t, rows.Err())
	return result
}

type summaryQueryLog struct {
	comment             string
	durationMS, read    uint64
	memory, resultBytes uint64
	columns             []string
}

// summaryQueryLogs returns every finished query logged for prefix (the
// caller's query id, shared by sequential round trips) in start order.
func summaryQueryLogs(t *testing.T, conn storageclickhouse.Conn, prefix string) []summaryQueryLog {
	t.Helper()
	require.NoError(t, conn.Exec(t.Context(), "SYSTEM FLUSH LOGS"))
	rows, err := conn.Query(t.Context(), `SELECT toString(log_comment), toUInt64(query_duration_ms),
	 toUInt64(read_bytes), toUInt64(memory_usage), toUInt64(result_bytes), arrayMap(c -> toString(c), columns)
	 FROM system.query_log WHERE type = 'QueryFinish'
	 AND (query_id = {prefix:String} OR query_id LIKE concat({prefix:String}, '-%'))
	 ORDER BY event_time_microseconds`, driverclickhouse.Named("prefix", prefix))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	logs := []summaryQueryLog{}
	for rows.Next() {
		var entry summaryQueryLog
		require.NoError(t, rows.Scan(&entry.comment, &entry.durationMS, &entry.read, &entry.memory,
			&entry.resultBytes, &entry.columns))
		for i, column := range entry.columns {
			// query_log qualifies columns as database.table.column.
			entry.columns[i] = column[strings.LastIndex(column, ".")+1:]
		}
		logs = append(logs, entry)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, logs, "no finished query found for id %s", prefix)
	for _, entry := range logs {
		t.Logf("SUMMARY_QUERY id=%s comment=%q duration_ms=%d read_bytes=%d memory=%d result_bytes=%d columns=%v",
			prefix, entry.comment, entry.durationMS, entry.read, entry.memory, entry.resultBytes, entry.columns)
	}
	return logs
}

// splitSummaryLogs asserts every main-pass row reads only the allowed columns
// and returns the main-pass and correction row counts.
func splitSummaryLogs(t *testing.T, logs []summaryQueryLog) (int, int) {
	t.Helper()
	var mains, corrections int
	for _, entry := range logs {
		if entry.comment == summaryCorrectionComment {
			corrections++
			continue
		}
		mains++
		for _, column := range entry.columns {
			require.True(t, summaryMainColumn(column),
				"main summary pass must read only tenant_id,event_id,occurred_at; read %v", entry.columns)
		}
	}
	return mains, corrections
}

// timedSummary runs SummarizeEvents under a fresh query id and returns the
// result, elapsed time and the id.
func timedSummary(
	t *testing.T, store *storageclickhouse.QueryEventStore, tenant string, now time.Time,
) (query.EventSummary, time.Duration, string) {
	t.Helper()
	prefix := "summary-" + uuid.NewString()
	started := time.Now()
	got, err := store.SummarizeEvents(driverclickhouse.Context(t.Context(), driverclickhouse.WithQueryID(prefix)),
		tenant, now)
	elapsed := time.Since(started)
	t.Logf("SCALE_SUMMARY tenant=%s duration=%s err=%v rate=%d buckets=%d", tenant, elapsed, err,
		got.RateCount, len(got.Trend))
	require.NoError(t, err)
	return got, elapsed, prefix
}

func TestSummarizeEventsScale(t *testing.T) {
	_, conn := newDedupeStore(t)
	store := storageclickhouse.NewQueryEventStore(conn)
	now := time.Now().UTC().Truncate(time.Millisecond)
	newest := now.Add(-time.Minute / 2)
	seedScale(t, conn, "scale-a", newest, now, scaleRows, scaleRows)
	seedScale(t, conn, "scale-a", newest, now, scaleDuplicates, scaleRows)

	got, elapsed, prefix := timedSummary(t, store, "scale-a", now)
	oracleID := "oracle-" + uuid.NewString()
	oracleCtx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	oracleStarted := time.Now()
	want := oracleSummary(driverclickhouse.Context(oracleCtx, driverclickhouse.WithQueryID(oracleID)),
		t, conn, "scale-a", now)
	t.Logf("SCALE_SUMMARY_ORACLE duration=%s", time.Since(oracleStarted))
	summaryQueryLogs(t, conn, oracleID)
	logs := summaryQueryLogs(t, conn, prefix)

	require.Equal(t, want, got, "scale summary must equal the whole-tenant dedupe summary")
	var total uint64
	for _, bucket := range got.Trend {
		total += bucket.Count
	}
	require.LessOrEqual(t, total, uint64(scaleRows))
	require.Positive(t, got.RateCount)
	mains, corrections := splitSummaryLogs(t, logs)
	require.Equal(t, 1, mains, "exactly one main summary pass")
	require.Zero(t, corrections, "zero conflicting identities must skip the correction round trip")
	require.Less(t, elapsed, scaleCallBudget, "SummarizeEvents must finish with 5x headroom below queryTimeout")
}

// summaryBoundaryConflicts adds conflicting representations whose occurrence
// times straddle the 24 h window start and the 1-minute rate boundary.
// Alternating received times make either side win.
func summaryBoundaryConflicts(events []*contracts.Event, start, rate time.Time) []*contracts.Event {
	extra := []*contracts.Event{}
	for i := range 25 {
		for j, target := range []time.Time{start, rate} {
			conflict := cloneEvent(events[(i*2+j)*19+1])
			// Offsets -12..12 s around the boundary; offset 0 lands exactly on it.
			conflict.OccurredAt = contracts.NewTimestamp(target.Add(time.Duration(i-12) * time.Second))
			shift := time.Minute
			if i%2 == 0 {
				shift = -time.Minute
			}
			conflict.ReceivedAt = ptr(contracts.NewTimestamp(events[0].ReceivedAt.Add(shift)))
			conflict.Source = "boundary-conflict"
			extra = append(extra, conflict)
		}
	}
	return extra
}

func TestSummarizeEventsOracleEquivalence(t *testing.T) {
	writer, conn := newDedupeStore(t)
	store := storageclickhouse.NewQueryEventStore(conn)
	now := time.Now().UTC().Truncate(time.Millisecond)
	start := now.Truncate(query.SummaryBucketWidth).Add(-24 * time.Hour)
	events, extra := equivalenceFixture("sum-a", now.Add(-30*time.Second), 5_000)
	extra = append(extra, summaryBoundaryConflicts(events, start, now.Add(-time.Minute))...)
	insertFixture(t, writer, events, extra)

	got, _, prefix := timedSummary(t, store, "sum-a", now)
	t.Run("equal", func(t *testing.T) {
		want := oracleSummary(t.Context(), t, conn, "sum-a", now)
		require.Equal(t, want, got)
		require.Positive(t, got.RateCount)
		require.NotEmpty(t, got.Trend)
	})
	t.Run("correction_logged", func(t *testing.T) {
		mains, corrections := splitSummaryLogs(t, summaryQueryLogs(t, conn, prefix))
		require.Equal(t, 1, mains, "exactly one main summary pass")
		require.Equal(t, 1, corrections, "conflicting identities must run exactly one correction round trip")
	})
	t.Run("tenant_isolation", func(t *testing.T) {
		// Same event_ids, earlier received and moved occurrence times: if the
		// foreign tenant leaked into either round trip the counts would change.
		foreign := make([]*contracts.Event, 0, len(events)+len(extra))
		for _, event := range append(append([]*contracts.Event{}, events...), extra...) {
			clone := cloneEvent(event)
			clone.TenantID, clone.Source = "sum-b", "foreign"
			clone.OccurredAt = contracts.NewTimestamp(start.Add(-time.Hour))
			clone.ReceivedAt = ptr(contracts.NewTimestamp(now.Add(-48 * time.Hour)))
			foreign = append(foreign, clone)
		}
		require.NoError(t, writer.InsertBatch(t.Context(), foreign))
		after, _, _ := timedSummary(t, store, "sum-a", now)
		require.Equal(t, got, after, "foreign tenant rows must not change tenant A summary")
	})
}

func TestSummarizeEventsNoConflictsSkipsCorrection(t *testing.T) {
	writer, conn := newDedupeStore(t)
	store := storageclickhouse.NewQueryEventStore(conn)
	now := time.Now().UTC().Truncate(time.Millisecond)
	// 1,500 rows span 300 s, so at least one closed trend bucket and the rate window are covered.
	events, _ := equivalenceFixture("calm-a", now.Add(-30*time.Second), 1_500)
	insertFixture(t, writer, events, nil)

	got, _, prefix := timedSummary(t, store, "calm-a", now)
	require.Equal(t, oracleSummary(t.Context(), t, conn, "calm-a", now), got)
	require.Positive(t, got.RateCount)
	require.NotEmpty(t, got.Trend)
	mains, corrections := splitSummaryLogs(t, summaryQueryLogs(t, conn, prefix))
	require.Equal(t, 1, mains, "exactly one main summary pass")
	require.Zero(t, corrections, "no conflicts must skip the correction round trip")
}

func TestSummarizeEventsConflictCap(t *testing.T) {
	_, conn := newDedupeStore(t)
	store := storageclickhouse.NewQueryEventStore(conn)
	now := time.Now().UTC().Truncate(time.Millisecond)
	const conflictCap = 10_000
	// Two generated passes share event_ids but differ in occurred_at, so every
	// identity conflicts and both representations lie inside the window.
	for tenant, count := range map[string]uint64{"cap-at": conflictCap, "cap-over": conflictCap + 1} {
		seedScale(t, conn, tenant, now.Add(-time.Minute), now, count, count)
		seedScale(t, conn, tenant, now.Add(-2*time.Minute), now.Add(time.Second), count, count)
	}
	t.Run("at_cap", func(t *testing.T) {
		got, elapsed, prefix := timedSummary(t, store, "cap-at", now)
		require.Equal(t, oracleSummary(t.Context(), t, conn, "cap-at", now), got)
		mains, corrections := splitSummaryLogs(t, summaryQueryLogs(t, conn, prefix))
		require.Equal(t, 1, mains)
		require.Equal(t, 1, corrections)
		require.Less(t, elapsed, scaleCallBudget)
	})
	t.Run("over_cap", func(t *testing.T) {
		got, err := store.SummarizeEvents(t.Context(), "cap-over", now)
		require.ErrorIs(t, err, storageclickhouse.ErrSummaryConflictsExceeded)
		require.Equal(t, query.EventSummary{}, got, "never a partial summary")
	})
}
