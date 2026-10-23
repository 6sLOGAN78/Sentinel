package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/6sLOGAN78/sentinel/internal/detection"
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

// GetWorkflow is a direct tenant-and-ID lookup, independent of list time ranges.
func (s *AlertStore) GetWorkflow(ctx context.Context, tenant, id string) (*query.WorkflowAlert, error) {
	queryCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var a query.WorkflowAlert
	err := s.pool.QueryRow(queryCtx, `SELECT alert_id,tenant_id,created_at,updated_at,rule_id,rule_version,
		severity,entity_key,summary,event_count,window_start,window_end,status,assignee,version
		FROM alert_workflows WHERE tenant_id=$1 AND alert_id=$2`, tenant, id).Scan(
		&a.AlertID, &a.TenantID, &a.CreatedAt, &a.UpdatedAt, &a.RuleID, &a.RuleVersion,
		&a.Severity, &a.EntityKey, &a.Summary, &a.EventCount, &a.WindowStart, &a.WindowEnd,
		&a.Status, &a.Assignee, &a.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, query.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
	a.WindowStart, a.WindowEnd = a.WindowStart.UTC(), a.WindowEnd.UTC()
	return &a, nil
}

// RuleExplanation uses only a decision-bound retained manifest, not today's
// startup settings. Mutable fence manifests are not a historical archive.
func (s *AlertStore) RuleExplanation(ctx context.Context, tenant, id, rule string, version int) (string, error) {
	unavailable := fmt.Sprintf("This rule version is unavailable. "+
		"The detection retains its recorded rule ID and version. %s version %d.", rule, version)
	queryCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, `SELECT f.rules_manifest,d.config_fingerprint
		FROM detection_decisions d LEFT JOIN detection_partition_fences f
		ON f.group_id=d.group_id AND f.config_fingerprint=d.config_fingerprint
		WHERE d.tenant_id=$1 AND d.alert_id=$2 AND d.rule_id=$3 AND d.rule_version=$4 LIMIT 33`,
		tenant, id, rule, version)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var found *detection.Rule
	count := 0
	for rows.Next() {
		count++
		var raw []byte
		var fingerprint string
		if err = rows.Scan(&raw, &fingerprint); err != nil {
			return "", err
		}
		// One overflow sentinel proves the bounded sample is incomplete. Never
		// assert uniqueness from truncated candidates or missing/invalid payloads.
		if count > 32 || len(raw) == 0 || len(raw) > 8192 {
			return unavailable, nil
		}
		var rules detection.Rules
		if json.Unmarshal(raw, &rules) != nil || rules.SchemaVersion != 1 ||
			len(rules.Rules) != 4 || rules.Fingerprint() != fingerprint {
			return unavailable, nil
		}
		candidate := retainedRule(rules, rule, version)
		if candidate == nil || (found != nil && *found != *candidate) {
			return unavailable, nil
		}
		found = candidate
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if found == nil {
		return unavailable, nil
	}
	return fmt.Sprintf("%s version %d: recorded threshold %d; event-time window %s; cooldown %s; severity %s. "+
		"Definition from retained decision configuration.",
		found.ID, found.Version, found.Threshold, found.Window, found.Cooldown, found.Severity), nil
}

func retainedRule(rules detection.Rules, id string, version int) *detection.Rule {
	var found *detection.Rule
	for _, rule := range rules.Rules {
		if rule.ID == id && rule.Version == version {
			if found != nil || rule.Threshold < 1 || rule.Window < time.Second || rule.Cooldown < time.Millisecond {
				return nil
			}
			candidate := rule
			found = &candidate
		}
	}
	return found
}
