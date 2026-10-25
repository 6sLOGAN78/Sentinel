package query

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/errs"
)

// SummaryBucketWidth is the fixed UTC trend resolution.
const SummaryBucketWidth = 5 * time.Minute

const summaryBuckets = 288

// SummaryCount is a sparse native aggregate, never a physical-row count.
type SummaryCount struct {
	Start time.Time
	Count uint64
}

// EventSummary contains canonical identities from the shared snapshot ranges.
type EventSummary struct {
	RateCount uint64
	Trend     []SummaryCount
}

// SeverityCount counts currently open authoritative workflows.
type SeverityCount struct {
	Severity string `json:"severity"`
	Count    uint64 `json:"count"`
}

// RuleCount counts workflows independently of their current status.
type RuleCount struct {
	RuleID string `json:"rule_id"`
	Count  uint64 `json:"count"`
}

// AlertSummary is derived exclusively from PG workflow authority.
type AlertSummary struct {
	Severity []SeverityCount
	Rules    []RuleCount
	Trend    []SummaryCount
}

// SummaryEvents receives exactly the same captured now as SummaryAlerts.
type SummaryEvents interface {
	SummarizeEvents(context.Context, string, time.Time) (EventSummary, error)
}

// SummaryAlerts supplies bounded aggregates, not an unbounded workflow list.
type SummaryAlerts interface {
	SummarizeAlerts(context.Context, string, time.Time) (AlertSummary, error)
}

// SummaryHandler owns its focused adapters and clock; production uses time.Now.
type SummaryHandler struct {
	events SummaryEvents
	alerts SummaryAlerts
	auth   *auth.Verifier
	now    func() time.Time
}

// NewSummaryHandler permits deterministic native boundary tests without a
// production clock environment override. A nil clock uses the real UTC clock.
func NewSummaryHandler(events SummaryEvents, alerts SummaryAlerts, verifier *auth.Verifier,
	now func() time.Time,
) *SummaryHandler {
	if now == nil {
		now = time.Now
	}
	return &SummaryHandler{events: events, alerts: alerts, auth: verifier, now: now}
}

// Register authenticates every summary read, including all verified roles.
func (h *SummaryHandler) Register(e *echo.Echo) {
	e.GET("/api/v1/metrics/summary", h.GetSummary, h.auth.Middleware)
}

type trendPoint struct {
	Start  time.Time `json:"bucket_start"`
	Events uint64    `json:"event_count"`
	Alerts uint64    `json:"alert_count"`
}

// GetSummary only fills known buckets after both dependencies succeeded.
func (h *SummaryHandler) GetSummary(c echo.Context) error {
	p, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	now := h.now().UTC()
	ctx, cancel := context.WithTimeout(c.Request().Context(), detailTimeout)
	defer cancel()
	events, err := h.events.SummarizeEvents(ctx, p.TenantID, now)
	if err != nil {
		return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
	}
	alerts, err := h.alerts.SummarizeAlerts(ctx, p.TenantID, now)
	if err != nil {
		return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
	}
	end := now.Truncate(SummaryBucketWidth)
	start := end.Add(-24 * time.Hour)
	trend := summaryTrend(start, end, events.Trend, alerts.Trend)
	names := []string{"critical", "high", "medium", "low"}
	severity := make([]SeverityCount, len(names))
	counts := make(map[string]uint64, len(alerts.Severity))
	var open uint64
	for _, count := range alerts.Severity {
		counts[count.Severity] = count.Count
	}
	for i, name := range names {
		severity[i] = SeverityCount{Severity: name, Count: counts[name]}
		open += counts[name]
	}
	if alerts.Rules == nil {
		alerts.Rules = []RuleCount{}
	}
	return c.JSON(http.StatusOK, struct {
		Rate     float64         `json:"event_rate_per_sec"`
		Open     uint64          `json:"open_alerts"`
		Severity []SeverityCount `json:"alerts_by_severity"`
		Rules    []RuleCount     `json:"top_rules"`
		Trend    []trendPoint    `json:"trend_24h"`
	}{float64(events.RateCount) / 60, open, severity, alerts.Rules, trend})
}

func summaryTrend(start, end time.Time, events, alerts []SummaryCount) []trendPoint {
	trend := make([]trendPoint, summaryBuckets)
	for i := range trend {
		trend[i].Start = start.Add(time.Duration(i) * SummaryBucketWidth)
	}
	for _, series := range []struct {
		counts []SummaryCount
		events bool
	}{{events, true}, {alerts, false}} {
		for _, point := range series.counts {
			if point.Start.Before(start) || !point.Start.Before(end) {
				continue
			}
			i := int(point.Start.Sub(start) / SummaryBucketWidth)
			if series.events {
				trend[i].Events = point.Count
			} else {
				trend[i].Alerts = point.Count
			}
		}
	}
	return trend
}
