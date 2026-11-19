package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/query"
)

// canonicalRowsPrefixSQL selects each identity's representative whole
// (including nullable fields). Earliest received wins, matching Get; equal
// received times use the whole canonical tuple to make ties deterministic.
// Callers append the identity scope and GROUP BY; no range, filter or page
// predicate may run inside it, because it would change which row wins.
const canonicalRowsPrefixSQL = `SELECT event_id,tenant_id,
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
 FROM events WHERE tenant_id={tenant:String} `

// uniqueEventRowsSQL is the whole-tenant canonical selection used by
// SummarizeEvents (unchanged text; 05-23 owns its bounding).
const uniqueEventRowsSQL = canonicalRowsPrefixSQL + `GROUP BY tenant_id,event_id
 )`

const (
	maxEventPageLimit = 200
	// candidateWidenFactor multiplies the stage-1 key cap when duplicate or
	// conflicting rows consumed the window before a full page survived.
	candidateWidenFactor = 4
	// defaultMaxCandidates bounds one attempt's stage-1 keys and the IN set
	// of stage 2 (about 100 full 200-row pages of slack). Reaching it without a
	// proven page is reported as unavailable, never as a short page.
	defaultMaxCandidates = 20_000
	// cursorBoundPredicate is redundant with the tuple cursor but lets the
	// primary key and partition pruning use the cursor time.
	cursorBoundPredicate = "occurred_at <= fromUnixTimestamp64Milli({after:Int64},'UTC')"
	listColumnsSQL       = `kind,candidate_count,event_id,tenant_id,occurred_at,received_at,source,
 event_type,source_ip,destination_ip,destination_port,protocol,principal,action,outcome,attributes_json`
)

// ErrCandidateWindowExceeded means the bounded candidate window could not
// prove a complete page; the query API maps it to 503 like any store error.
var ErrCandidateWindowExceeded = errors.New("event list candidate window exceeded")

// QueryEventStore reads tenant event history through the native connection.
type QueryEventStore struct {
	conn          Conn
	maxCandidates uint64
}

// NewQueryEventStore creates the finite event-search adapter.
func NewQueryEventStore(conn Conn) *QueryEventStore {
	return &QueryEventStore{conn: conn, maxCandidates: defaultMaxCandidates}
}

// ListEvents returns up to Limit+1 canonical identities ordered
// (occurred_at,event_id) DESC, exactly equal to applying the predicates after
// whole-tenant dedupe, without deduping the whole tenant. It never relies on
// background merges.
//
// Stage 1 reads raw rows of the tenant that satisfy range, cursor and filters
// in table key order and keeps the first cap keys. Stage 2 resolves winners
// (the unchanged argMin) over all tenant rows of only those event_ids, then
// applies the same predicates to each winner. When stage 1 was full, only
// winners whose key is >= the smallest stage-1 key B are kept.
//
// Exactness: a winner that satisfies the predicates is one of the raw rows
// that satisfy them, so if its key is >= B its row was read by stage 1 (rows
// equal to B share B's event_id) and its identity was resolved. Losers only
// consume slots; they never add an identity, and a winner outside the
// predicates or below B is dropped. So the result is the whole-tenant dedupe
// restricted to keys >= B, an exact prefix of it. This holds when duplicates
// have different occurred_at (ADR 0002). A full prefix of Limit+1 is the exact
// page; otherwise the cap widens, and a non-full stage 1 is complete.
func (s *QueryEventStore) ListEvents(
	ctx context.Context, tenant string, f query.EventFilters,
) ([]contracts.Event, error) {
	if f.Limit < 1 || f.Limit > maxEventPageLimit {
		return nil, query.ErrInvalidQuery
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	sql, args := listEventsSQL(f)
	args = append(args, driverclickhouse.Named("tenant", tenant))
	pageSize := uint64(f.Limit) + 1
	for limit := pageSize; ; limit = min(limit*candidateWidenFactor, s.maxCandidates) {
		items, candidates, err := s.listAttempt(ctx, sql, args, limit, pageSize, f.Limit)
		if err != nil {
			return nil, err
		}
		if candidates < limit || uint64(len(items)) >= pageSize {
			return items, nil
		}
		if limit >= s.maxCandidates {
			return nil, fmt.Errorf("list events after %d candidate keys: %w", limit, ErrCandidateWindowExceeded)
		}
	}
}

// eventPredicates returns the winner predicates, the stage-1 predicates and
// their shared named arguments. SQL text uses constant fragments only.
func eventPredicates(f query.EventFilters) (string, string, []any) {
	// Explicit epoch milliseconds avoid driver/server DateTime64 text-format
	// differences while preserving native parameter binding and exact precision.
	predicates := []string{"occurred_at >= fromUnixTimestamp64Milli({from:Int64},'UTC')",
		"occurred_at < fromUnixTimestamp64Milli({to:Int64},'UTC')"}
	args := []any{driverclickhouse.Named("from", f.From.UnixMilli()), driverclickhouse.Named("to", f.To.UnixMilli())}
	for _, filter := range []struct{ column, name, value string }{
		{"source_ip", "ip", f.SourceIP}, {"event_type", "type", f.EventType}, {"outcome", "outcome", f.Outcome},
	} {
		if filter.value != "" {
			predicate := filter.column + " = {" + filter.name + ":String}"
			if filter.column == "source_ip" {
				// Stored Event bytes retain valid alternate IPv6 spellings.
				// Compare normalized values on raw rows and on winners alike.
				predicate = "toIPv6OrNull(source_ip) = toIPv6OrNull({ip:String})"
			}
			predicates = append(predicates, predicate)
			args = append(args, driverclickhouse.Named(filter.name, filter.value))
		}
	}
	candidate := predicates
	if f.After != nil {
		cursor := "(occurred_at,event_id) < (fromUnixTimestamp64Milli({after:Int64},'UTC'),{id:String})"
		predicates = append(predicates, cursor)
		candidate = append(append([]string{}, predicates...), cursorBoundPredicate)
		args = append(args, driverclickhouse.Named("after", f.After.OccurredAt.UnixMilli()),
			driverclickhouse.Named("id", f.After.EventID))
	}
	return strings.Join(predicates, " AND "), strings.Join(candidate, " AND "), args
}

// listEventsSQL builds one attempt: the stage-1 scalar (candidate ids, count,
// boundary key) is evaluated once per query, so the IN set, count and boundary
// come from the same read. Row kind 0 carries the count and boundary even when
// no winner survives; kind 1 rows are the ordered winners.
func listEventsSQL(f query.EventFilters) (string, []any) {
	winner, candidate, args := eventPredicates(f)
	return `WITH (SELECT tuple(groupArray(event_id),count(),min(tuple(occurred_at,event_id))) FROM (
 SELECT occurred_at,event_id FROM events WHERE tenant_id={tenant:String} AND ` + candidate + `
 ORDER BY occurred_at DESC,event_id DESC LIMIT {cap:UInt64})) AS candidates
 SELECT ` + listColumnsSQL + ` FROM (
 SELECT toUInt8(0) AS kind,candidates.2 AS candidate_count,candidates.3.2 AS event_id,
 {tenant:String} AS tenant_id,candidates.3.1 AS occurred_at,candidates.3.1 AS received_at,
 '' AS source,'' AS event_type,NULL AS source_ip,NULL AS destination_ip,NULL AS destination_port,
 NULL AS protocol,NULL AS principal,NULL AS action,NULL AS outcome,'{}' AS attributes_json
 UNION ALL
 SELECT toUInt8(1) AS kind,toUInt64(0) AS candidate_count,* FROM (` + canonicalRowsPrefixSQL +
		`AND event_id IN candidates.1 GROUP BY tenant_id,event_id
 ) WHERE ` + winner + ` AND (candidates.2 < {cap:UInt64} OR (occurred_at,event_id) >= candidates.3)
 ORDER BY occurred_at DESC,event_id DESC LIMIT {page_size:UInt64})
 ) ORDER BY kind,occurred_at DESC,event_id DESC`, args
}

// listAttempt runs one bounded attempt and returns the surviving winners and
// the stage-1 key count. All attempts share the caller's queryTimeout.
func (s *QueryEventStore) listAttempt(
	ctx context.Context, sql string, shared []any, limit, pageSize uint64, pageLimit int,
) ([]contracts.Event, uint64, error) {
	args := append(append([]any{}, shared...),
		driverclickhouse.Named("cap", limit), driverclickhouse.Named("page_size", pageSize))
	rows, err := s.conn.Query(boundedQueryContext(ctx), sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]contracts.Event, 0, pageLimit+1)
	var candidates uint64
	for rows.Next() {
		var kind uint8
		var count uint64
		var event contracts.Event
		var occurred, received time.Time
		var port *uint16
		var attributes string
		if err = rows.Scan(&kind, &count, &event.EventID, &event.TenantID, &occurred, &received, &event.Source,
			&event.EventType, &event.SourceIP, &event.DestinationIP, &port, &event.Protocol, &event.Principal,
			&event.Action, &event.Outcome, &attributes); err != nil {
			return nil, 0, fmt.Errorf("scan event page: %w", err)
		}
		if kind == 0 {
			candidates = count
			continue
		}
		event.SchemaVersion = contracts.EventSchemaVersion
		event.OccurredAt = contracts.NewTimestamp(occurred)
		receivedAt := contracts.NewTimestamp(received)
		event.ReceivedAt = &receivedAt
		if port != nil {
			value := int(*port)
			event.DestinationPort = &value
		}
		event.Attributes, err = unmarshalAttributes(attributes)
		if err != nil {
			return nil, 0, fmt.Errorf("decode event page: %w", err)
		}
		items = append(items, event)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read event page: %w", err)
	}
	return items, candidates, nil
}
