package clickhouse

import (
	"context"
	"errors"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/query"
)

// QueryAlertStore composes the existing canonical, tenant-scoped native reads.
type QueryAlertStore struct{ conn Conn }

const maxEvidenceIDs = 20

// NewQueryAlertStore binds immutable dashboard detail reads to the real pool.
func NewQueryAlertStore(conn Conn) *QueryAlertStore { return &QueryAlertStore{conn: conn} }

// Detection preserves the full canonical record, including its evidence IDs.
func (s *QueryAlertStore) Detection(ctx context.Context, tenant, id string) (*contracts.Alert, error) {
	queryCtx, cancel := context.WithTimeout(boundedQueryContext(ctx), queryTimeout)
	defer cancel()
	a, err := NewAlertStore(s.conn).Get(queryCtx, tenant, id)
	if errors.Is(err, ErrNotFound) {
		return nil, query.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = a.Validate(); err != nil {
		return nil, ErrConflict
	}
	return a, nil
}

// Evidence deduplicates IDs before the bounded reads, never physical rows after
// a global limit. Each of at most twenty lookups has its own tenant predicate;
// all share one deadline and return the first retained representation per ID.
func (s *QueryAlertStore) Evidence(
	ctx context.Context, tenant string, ids []string,
) ([]contracts.Event, []string, error) {
	if len(ids) > maxEvidenceIDs {
		return nil, nil, ErrConflict
	}
	queryCtx, cancel := context.WithTimeout(boundedQueryContext(ctx), queryTimeout)
	defer cancel()
	events, missing := make([]contracts.Event, 0, len(ids)), make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	store := NewEventStore(s.conn)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		event, err := store.Get(queryCtx, tenant, id)
		if errors.Is(err, ErrNotFound) {
			missing = append(missing, id)
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		events = append(events, *event)
	}
	return events, missing, nil
}
