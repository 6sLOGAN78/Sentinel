package clickhouse

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/query"
)

// fullWindowConn answers every attempt with a full stage-1 window (count equals
// the bound cap) and the scripted number of winners, never a complete page.
type fullWindowConn struct {
	driver.Conn

	caps    []uint64
	winners int
	fail    error
}

func (c *fullWindowConn) Query(_ context.Context, _ string, args ...any) (driver.Rows, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	var limit uint64
	for _, arg := range args {
		if named, ok := arg.(driver.NamedValue); ok && named.Name == "cap" {
			limit, _ = named.Value.(uint64)
		}
	}
	c.caps = append(c.caps, limit)
	return &scriptedRows{count: limit, winners: c.winners}, nil
}

// scriptedRows yields one kind-0 metadata row followed by winner rows.
type scriptedRows struct {
	driver.Rows

	count   uint64
	winners int
	row     int
}

func (r *scriptedRows) Next() bool {
	r.row++
	return r.row <= r.winners+1
}

func (r *scriptedRows) Scan(dest ...any) error {
	kind, ok := dest[0].(*uint8)
	if !ok {
		return errors.New("kind destination")
	}
	count, ok := dest[1].(*uint64)
	if !ok {
		return errors.New("count destination")
	}
	if r.row == 1 {
		*kind, *count = 0, r.count
		return nil
	}
	*kind = 1
	attributes, ok := dest[len(dest)-1].(*string)
	if !ok {
		return errors.New("attributes destination")
	}
	*attributes = "{}"
	return nil
}

func (r *scriptedRows) Close() error { return nil }
func (r *scriptedRows) Err() error   { return nil }

func scriptedFilters(limit int) query.EventFilters {
	now := time.Now().UTC()
	return query.EventFilters{ListOptions: query.ListOptions{From: now.Add(-time.Hour), To: now, Limit: limit}}
}

func TestListEventsWideningExhaustedReturnsUnavailable(t *testing.T) {
	t.Parallel()

	conn := &fullWindowConn{}
	store := &QueryEventStore{conn: conn, maxCandidates: 10}
	items, err := store.ListEvents(t.Context(), "demo", scriptedFilters(1))
	require.ErrorIs(t, err, ErrCandidateWindowExceeded)
	require.Nil(t, items)
	// 2 (page size) -> 8 -> 10 (clamped max cap), then stop.
	require.Equal(t, []uint64{2, 8, 10}, conn.caps)
}

func TestListEventsWideningStopsOnFullPage(t *testing.T) {
	t.Parallel()

	conn := &fullWindowConn{winners: 3}
	store := &QueryEventStore{conn: conn, maxCandidates: 10}
	items, err := store.ListEvents(t.Context(), "demo", scriptedFilters(2))
	require.NoError(t, err)
	require.Len(t, items, 3)
	require.Equal(t, []uint64{3}, conn.caps)
}

func TestListEventsQueryErrorIsWrapped(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	store := &QueryEventStore{conn: &fullWindowConn{fail: sentinel}, maxCandidates: 10}
	_, err := store.ListEvents(t.Context(), "demo", scriptedFilters(1))
	require.ErrorIs(t, err, sentinel)
}

func TestListEventsRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	store := NewQueryEventStore(&fullWindowConn{})
	for _, limit := range []int{0, maxEventPageLimit + 1} {
		_, err := store.ListEvents(t.Context(), "demo", scriptedFilters(limit))
		require.ErrorIs(t, err, query.ErrInvalidQuery)
	}
}

func TestUniqueEventRowsSQLUnchanged(t *testing.T) {
	t.Parallel()

	// SummarizeEvents still depends on the whole-tenant text (05-23 owns it).
	require.Contains(t, uniqueEventRowsSQL,
		"FROM events WHERE tenant_id={tenant:String} GROUP BY tenant_id,event_id\n )")
}
