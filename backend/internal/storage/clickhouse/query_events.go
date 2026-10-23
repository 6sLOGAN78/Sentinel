package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/query"
)

// Keep each representative whole (including nullable fields). Earliest received
// wins, matching Get; equal received times use the whole canonical tuple to make
// ties deterministic. No range/filter/page predicate may precede this dedupe.
const uniqueEventRowsSQL = `SELECT event_id,tenant_id,
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

// QueryEventStore reads tenant event history through the native connection.
type QueryEventStore struct{ conn Conn }

// NewQueryEventStore creates the finite event-search adapter.
func NewQueryEventStore(conn Conn) *QueryEventStore { return &QueryEventStore{conn: conn} }

// ListEvents applies all predicates before stable ordering and Limit+1, but
// after canonical identity selection. It never relies on background merges.
func (s *QueryEventStore) ListEvents(
	ctx context.Context, tenant string, f query.EventFilters,
) ([]contracts.Event, error) {
	if f.Limit < 1 || f.Limit > 200 {
		return nil, query.ErrInvalidQuery
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	// Explicit epoch milliseconds avoid driver/server DateTime64 text-format
	// differences while preserving native parameter binding and exact precision.
	predicates := []string{"occurred_at >= fromUnixTimestamp64Milli({from:Int64},'UTC')",
		"occurred_at < fromUnixTimestamp64Milli({to:Int64},'UTC')"}
	args := []any{driverclickhouse.Named("tenant", tenant), driverclickhouse.Named("from", f.From.UnixMilli()),
		driverclickhouse.Named("to", f.To.UnixMilli()), driverclickhouse.Named("page_size", uint64(f.Limit)+1)}
	for _, filter := range []struct{ column, name, value string }{
		{"source_ip", "ip", f.SourceIP}, {"event_type", "type", f.EventType}, {"outcome", "outcome", f.Outcome},
	} {
		if filter.value != "" {
			predicate := filter.column + " = {" + filter.name + ":String}"
			if filter.column == "source_ip" {
				// Stored Event bytes retain valid alternate IPv6 spellings.
				// Normalize only the selected whole identity, never before dedupe.
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
	sql := uniqueEventRowsSQL + " WHERE " + strings.Join(predicates, " AND ") +
		" ORDER BY occurred_at DESC,event_id DESC LIMIT {page_size:UInt64}"
	rows, err := s.conn.Query(boundedQueryContext(ctx), sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]contracts.Event, 0, f.Limit+1)
	for rows.Next() {
		var event contracts.Event
		var occurred, received time.Time
		var port *uint16
		var attributes string
		if err = rows.Scan(&event.EventID, &event.TenantID, &occurred, &received, &event.Source, &event.EventType,
			&event.SourceIP, &event.DestinationIP, &port, &event.Protocol, &event.Principal,
			&event.Action, &event.Outcome, &attributes); err != nil {
			return nil, fmt.Errorf("scan event page: %w", err)
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
			return nil, fmt.Errorf("decode event page: %w", err)
		}
		items = append(items, event)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read event page: %w", err)
	}
	return items, nil
}
