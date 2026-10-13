package clickhouse

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

const (
	insertEventsSQL = `INSERT INTO events (
		tenant_id,event_id,occurred_at,received_at,source,event_type,source_ip,
		destination_ip,destination_port,protocol,principal,action,outcome,attributes_json
	)`
	getEventSQL = `SELECT event_id,tenant_id,occurred_at,received_at,source,event_type,
		source_ip,destination_ip,destination_port,protocol,principal,action,outcome,attributes_json
		FROM events
		WHERE tenant_id = {tenant:String} AND event_id = {id:String}
		ORDER BY received_at ASC
		LIMIT 1`
	countUniqueSQL = `SELECT uniqExact(event_id) FROM events WHERE tenant_id = {tenant:String}`
	countSinceSQL  = countUniqueSQL + ` AND received_at >= {since:DateTime64(3)}`
	queryTimeout   = 5 * time.Second
)

// ErrNotFound means no event exists for the authenticated tenant and ID.
var ErrNotFound = errors.New("event not found")

// EventStore persists and queries canonical events through the native protocol.
type EventStore struct{ conn Conn }

// NewEventStore binds event operations to conn.
func NewEventStore(conn Conn) *EventStore { return &EventStore{conn: conn} }

// InsertBatch sends a fresh explicit-column native block. Callers retry by
// preparing a new batch; partial/ambiguous sends are made safe by read dedupe.
func (s *EventStore) InsertBatch(ctx context.Context, events []*contracts.Event) error {
	if len(events) == 0 {
		return nil
	}

	batch, err := s.conn.PrepareBatch(ctx, insertEventsSQL)
	if err != nil {
		return fmt.Errorf("prepare events batch: %w", err)
	}
	defer func() {
		if !batch.IsSent() {
			_ = batch.Abort()
		}
	}()

	for _, event := range events {
		if event == nil || event.ReceivedAt == nil {
			return errors.New("append event: received_at is required")
		}
		attributes, marshalErr := marshalAttributes(event.Attributes)
		if marshalErr != nil {
			return fmt.Errorf("append event attributes: %w", marshalErr)
		}
		port, validPort := nullablePort(event.DestinationPort)
		if !validPort {
			return errors.New("append event: destination_port is outside UInt16 range")
		}
		if err = batch.Append(
			event.TenantID, event.EventID, event.OccurredAt.Time, event.ReceivedAt.Time,
			event.Source, event.EventType, event.SourceIP, event.DestinationIP,
			port, event.Protocol, event.Principal,
			event.Action, event.Outcome, attributes,
		); err != nil {
			return fmt.Errorf("append event: %w", err)
		}
	}
	if err = batch.Send(); err != nil {
		return fmt.Errorf("send events batch: %w", err)
	}

	return nil
}

// Get returns one currently retained representation of an ID for tenant.
func (s *EventStore) Get(ctx context.Context, tenantID, eventID string) (*contracts.Event, error) {
	queryCtx := boundedQueryContext(ctx)
	var (
		event      contracts.Event
		occurredAt time.Time
		receivedAt time.Time
		port       *uint16
		attributes string
	)
	err := s.conn.QueryRow(
		queryCtx, getEventSQL,
		driverclickhouse.Named("tenant", tenantID),
		driverclickhouse.Named("id", eventID),
	).Scan(
		&event.EventID, &event.TenantID, &occurredAt, &receivedAt, &event.Source, &event.EventType,
		&event.SourceIP, &event.DestinationIP, &port, &event.Protocol, &event.Principal,
		&event.Action, &event.Outcome, &attributes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}

	event.SchemaVersion = contracts.EventSchemaVersion
	event.OccurredAt = contracts.NewTimestamp(occurredAt)
	received := contracts.NewTimestamp(receivedAt)
	event.ReceivedAt = &received
	if port != nil {
		value := int(*port)
		event.DestinationPort = &value
	}
	event.Attributes, err = unmarshalAttributes(attributes)
	if err != nil {
		return nil, fmt.Errorf("decode event attributes: %w", err)
	}

	return &event, nil
}

// CountUnique counts user-visible identities for tenant, optionally since a
// received_at lower bound. It never exposes the physical duplicate count.
func (s *EventStore) CountUnique(ctx context.Context, tenantID string, since time.Time) (uint64, error) {
	queryCtx := boundedQueryContext(ctx)
	query := countUniqueSQL
	args := []any{driverclickhouse.Named("tenant", tenantID)}
	if !since.IsZero() {
		query = countSinceSQL
		args = append(args, driverclickhouse.Named("since", since.UTC().Truncate(time.Millisecond)))
	}

	var count uint64
	if err := s.conn.QueryRow(queryCtx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unique events: %w", err)
	}

	return count, nil
}

func boundedQueryContext(ctx context.Context) context.Context {
	return driverclickhouse.Context(ctx, driverclickhouse.WithSettings(driverclickhouse.Settings{
		"max_execution_time": int(queryTimeout / time.Second),
	}))
}

func marshalAttributes(attributes map[string]any) (string, error) {
	raw, err := json.Marshal(attributes)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	return string(raw), nil
}

func unmarshalAttributes(raw string) (map[string]any, error) {
	var attributes map[string]any
	if err := json.Unmarshal([]byte(raw), &attributes); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return attributes, nil
}

func nullablePort(port *int) (*uint16, bool) {
	if port == nil {
		return nil, true
	}
	if *port < 0 || *port > int(^uint16(0)) {
		return nil, false
	}
	value := uint16(*port)

	return &value, true
}
