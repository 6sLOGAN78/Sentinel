package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

// WorkflowAlert is the public PG workflow, without invented CH detection fields.
type WorkflowAlert struct {
	AlertID     string    `json:"alert_id"`
	TenantID    string    `json:"tenant_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	RuleID      string    `json:"rule_id"`
	RuleVersion int64     `json:"rule_version"`
	Severity    string    `json:"severity"`
	EntityKey   string    `json:"entity_key"`
	Summary     string    `json:"summary"`
	EventCount  int64     `json:"event_count"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Status      string    `json:"status"`
	Assignee    *string   `json:"assignee"`
	Version     int64     `json:"version"`
}

// AlertPage bounds each list read. Its cursor is not a live-delivery sequence.
type AlertPage struct {
	Items      []WorkflowAlert `json:"items"`
	NextCursor *string         `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

// ErrInvalidAlertPage identifies invalid limits or mismatched cursors.
var ErrInvalidAlertPage = errors.New("invalid alert page")

type alertCursor struct {
	Version int       `json:"v"`
	Tenant  string    `json:"tenant"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Created time.Time `json:"created"`
	ID      string    `json:"id"`
}

// ListAlerts reads only authoritative tenant workflow rows, with a stable tuple
// and frozen 24-hour range. Rich filters are added in the next owned list slice.
func (s *AlertStore) ListAlerts(ctx context.Context, tenant string, limit int, cursor string) (AlertPage, error) {
	if limit < 1 || limit > 200 || len(cursor) > 2048 {
		return AlertPage{}, ErrInvalidAlertPage
	}
	to := time.Now().UTC().Truncate(time.Millisecond)
	c := alertCursor{Version: 1, Tenant: tenant, From: to.Add(-24 * time.Hour), To: to, Created: to, ID: ""}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &c) != nil || c.Version != 1 || c.Tenant != tenant ||
			c.From.IsZero() || !c.To.After(c.From) || c.To.Sub(c.From) > 7*24*time.Hour ||
			c.Created.Before(c.From) || !c.Created.Before(c.To) || len(c.ID) != 64 {
			return AlertPage{}, ErrInvalidAlertPage
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, `SELECT alert_id,tenant_id,created_at,updated_at,rule_id,rule_version,
		severity,entity_key,summary,event_count,window_start,window_end,status,assignee,version
		FROM alert_workflows WHERE tenant_id=$1 AND created_at >= $2 AND created_at < $3
		AND ($4::text='' OR (created_at,alert_id)<($5,$4))
		ORDER BY created_at DESC,alert_id DESC LIMIT $6`, tenant, c.From, c.To, c.ID, c.Created, limit+1)
	if err != nil {
		return AlertPage{}, err
	}
	defer rows.Close()
	page := AlertPage{Items: make([]WorkflowAlert, 0, limit), NextCursor: nil}
	for rows.Next() {
		var a WorkflowAlert
		if scanErr := rows.Scan(&a.AlertID, &a.TenantID, &a.CreatedAt, &a.UpdatedAt, &a.RuleID, &a.RuleVersion,
			&a.Severity, &a.EntityKey, &a.Summary, &a.EventCount, &a.WindowStart, &a.WindowEnd,
			&a.Status, &a.Assignee, &a.Version); scanErr != nil {
			return AlertPage{}, scanErr
		}
		a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
		a.WindowStart, a.WindowEnd = a.WindowStart.UTC(), a.WindowEnd.UTC()
		page.Items = append(page.Items, a)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return AlertPage{}, rowsErr
	}
	if len(page.Items) > limit {
		page.HasMore = true
		page.Items = page.Items[:limit]
		last := page.Items[limit-1]
		c.Created, c.ID = last.CreatedAt, last.AlertID
		raw, marshalErr := json.Marshal(c)
		if marshalErr != nil {
			return AlertPage{}, marshalErr
		}
		next := base64.RawURLEncoding.EncodeToString(raw)
		page.NextCursor = &next
	}
	return page, nil
}
