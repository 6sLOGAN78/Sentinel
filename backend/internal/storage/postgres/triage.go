package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/6sLOGAN78/sentinel/internal/query"
)

// Transition returns only committed PG metadata. No network enrichment may turn
// a positive COMMIT into a reported mutation failure.
func (s *AlertStore) Transition(
	ctx context.Context, tenant, id, actor string, expectedVersion int64, target string,
) (query.WorkflowAlert, error) {
	var a query.WorkflowAlert
	queryCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	tx, err := s.pool.Begin(queryCtx)
	if err != nil {
		return a, err
	}
	defer rollback(tx)
	// Foreign/unknown IDs share 404 even for an unsupported transition.
	var exists bool
	err = tx.QueryRow(queryCtx, `SELECT EXISTS(SELECT 1 FROM alert_workflows
		WHERE tenant_id=$1 AND alert_id=$2)`, tenant, id).Scan(&exists)
	if err != nil {
		return a, err
	}
	if !exists {
		return a, query.ErrNotFound
	}
	from := "open"
	if target == "resolved" {
		from = "acknowledged"
	} else if target != "acknowledged" {
		return a, query.ErrConflict
	}
	// Server time is shared by workflow and audit; caller cannot supply it.
	changed := time.Now().UTC()
	err = tx.QueryRow(queryCtx, `UPDATE alert_workflows SET status=$5,version=version+1,updated_at=$4
		WHERE tenant_id=$1 AND alert_id=$2 AND version=$3 AND status=$6
		RETURNING alert_id,tenant_id,created_at,updated_at,rule_id,rule_version,severity,entity_key,summary,
		event_count,window_start,window_end,status,assignee,version`,
		tenant, id, expectedVersion, changed, target, from).Scan(
		&a.AlertID, &a.TenantID, &a.CreatedAt, &a.UpdatedAt, &a.RuleID, &a.RuleVersion, &a.Severity, &a.EntityKey, &a.Summary,
		&a.EventCount, &a.WindowStart, &a.WindowEnd, &a.Status, &a.Assignee, &a.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, query.ErrConflict
	}
	if err != nil {
		return a, err
	}
	_, err = tx.Exec(queryCtx, `INSERT INTO alert_status_audit
		(tenant_id,alert_id,version,actor,changed_at,from_status,to_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, tenant, id, a.Version, actor, changed, from, target)
	if err != nil {
		return query.WorkflowAlert{}, err
	}
	if err = tx.Commit(queryCtx); err != nil {
		return query.WorkflowAlert{}, err
	}
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
	a.WindowStart, a.WindowEnd = a.WindowStart.UTC(), a.WindowEnd.UTC()
	return a, nil
}

// History is tenant scoped and bounded by the forward lifecycle's two changes.
func (s *AlertStore) History(ctx context.Context, tenant, id string) ([]query.StatusChange, error) {
	queryCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, `SELECT from_status,to_status,actor,changed_at FROM alert_status_audit
		WHERE tenant_id=$1 AND alert_id=$2 ORDER BY version ASC LIMIT 2`, tenant, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	const maxHistory = 2
	items := make([]query.StatusChange, 0, maxHistory)
	for rows.Next() {
		var change query.StatusChange
		if err = rows.Scan(&change.FromStatus, &change.ToStatus, &change.Actor, &change.ChangedAt); err != nil {
			return nil, err
		}
		change.ChangedAt = change.ChangedAt.UTC()
		items = append(items, change)
	}
	return items, rows.Err()
}
