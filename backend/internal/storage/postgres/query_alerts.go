package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/query"
)

// ListAlerts applies every filter before ORDER BY and LIMIT+1. PG003 permits
// microseconds, so selection, comparison and exposed created_at all truncate
// to milliseconds. This avoids losing or duplicating rows at cursor ties.
func (s *AlertStore) ListAlerts(
	ctx context.Context, tenant string, f query.AlertFilters, after *query.Position,
) ([]query.WorkflowAlert, error) {
	if f.Limit < 1 || f.Limit > 200 || !f.To.After(f.From) || f.To.Sub(f.From) > 7*24*time.Hour {
		return nil, query.ErrInvalidQuery
	}
	var lastID string
	lastTime := f.To
	if after != nil {
		lastID, lastTime = after.AlertID, after.CreatedAt
	}
	// Backslash, percent and underscore are literals, never user SQL fragments.
	search := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Search) + "%"
	queryCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, `SELECT alert_id,tenant_id,
		date_trunc('milliseconds',created_at),updated_at,rule_id,rule_version,
		severity,entity_key,summary,event_count,window_start,window_end,status,assignee,version
		FROM alert_workflows WHERE tenant_id=$1 AND created_at >= $2 AND created_at < $3
		AND ($4::text='' OR status=$4) AND ($5::text='' OR severity=$5) AND ($6::text='' OR rule_id=$6)
		AND ($7::text='' OR summary ILIKE $8 ESCAPE '\' OR entity_key ILIKE $8 ESCAPE '\')
		AND ($9::text='' OR (date_trunc('milliseconds',created_at),alert_id)<($10,$9))
		ORDER BY date_trunc('milliseconds',created_at) DESC,alert_id DESC LIMIT $11`,
		tenant, f.From, f.To, f.Status, f.Severity, f.Rule, f.Search, search, lastID, lastTime, f.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]query.WorkflowAlert, 0, f.Limit+1)
	for rows.Next() {
		var a query.WorkflowAlert
		if scanErr := rows.Scan(&a.AlertID, &a.TenantID, &a.CreatedAt, &a.UpdatedAt, &a.RuleID, &a.RuleVersion,
			&a.Severity, &a.EntityKey, &a.Summary, &a.EventCount, &a.WindowStart, &a.WindowEnd,
			&a.Status, &a.Assignee, &a.Version); scanErr != nil {
			return nil, scanErr
		}
		a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
		a.WindowStart, a.WindowEnd = a.WindowStart.UTC(), a.WindowEnd.UTC()
		items = append(items, a)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	return items, nil
}
