package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/query"
)

// SummarizeAlerts computes all workflow aggregates in one PG statement snapshot.
// CH detections cannot create a workflow count. Only current open counts use
// status; historical trend and rule counts survive acknowledgement/resolution.
func (s *AlertStore) SummarizeAlerts(ctx context.Context, tenant string, now time.Time) (query.AlertSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	end := now.UTC().Truncate(query.SummaryBucketWidth)
	rows, err := s.pool.Query(ctx, `WITH scoped AS MATERIALIZED (
	 SELECT status,severity,rule_id,created_at FROM alert_workflows WHERE tenant_id=$1
	 AND (status='open' OR created_at >= $2)
	), aggregates AS (
	 SELECT 'severity' AS kind,severity AS key,NULL::timestamptz AS bucket,count(*) AS n
	 FROM scoped WHERE status='open' GROUP BY severity
	 UNION ALL
	 SELECT 'rule',rule_id,NULL::timestamptz,n FROM (
	 SELECT rule_id,count(*) AS n FROM scoped WHERE created_at >= $3 AND created_at < $4
	 GROUP BY rule_id ORDER BY n DESC,rule_id ASC LIMIT 20) AS top_rules
	 UNION ALL
	 SELECT 'trend','',date_bin('5 minutes',created_at,'2000-01-01'::timestamptz),count(*)
	 FROM scoped WHERE created_at >= $2 AND created_at < $5 GROUP BY 3
	) SELECT kind,key,bucket,n FROM aggregates ORDER BY kind,n DESC,key ASC`,
		tenant, end.Add(-24*time.Hour), now.Add(-24*time.Hour), now, end)
	if err != nil {
		return query.AlertSummary{}, fmt.Errorf("workflow summary: %w", err)
	}
	defer rows.Close()
	result := query.AlertSummary{}
	for rows.Next() {
		var kind, key string
		var at *time.Time
		var n uint64
		if err = rows.Scan(&kind, &key, &at, &n); err != nil {
			return query.AlertSummary{}, fmt.Errorf("scan workflow summary: %w", err)
		}
		switch kind {
		case "severity":
			result.Severity = append(result.Severity, query.SeverityCount{Severity: key, Count: n})
		case "rule":
			result.Rules = append(result.Rules, query.RuleCount{RuleID: key, Count: n})
		case "trend":
			result.Trend = append(result.Trend, query.SummaryCount{Start: at.UTC(), Count: n})
		}
	}
	if err = rows.Err(); err != nil {
		return query.AlertSummary{}, fmt.Errorf("read workflow summary: %w", err)
	}
	return result, nil
}
