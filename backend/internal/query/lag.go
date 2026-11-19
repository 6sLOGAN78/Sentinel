package query

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/errs"
)

// The fixed PromQL of the lag endpoint. Position-based lag is published per
// owned partition; the maximum per group is the one number an analyst may
// see, and only when every partition of the group's topic has a reporter.
// No request input is ever interpolated into a query, and every result is
// aggregated to the group label before it leaves Prometheus.
const (
	qLag = `max by (group) (sentinel_kafka_consumer_lag)`
	// qLagReported counts distinct reported partitions, so two processes
	// briefly reporting one partition during a handover cannot hide a
	// missing one.
	qLagReported = `count by (group) (count by (group, topic, partition) (sentinel_kafka_consumer_lag))`
	// qLagPartitions is the coverage denominator: the broker-listed
	// partition count of each group's topic, exported by every member.
	qLagPartitions = `sum by (group) (max by (group, topic) (sentinel_kafka_topic_partitions))`
)

// lagForbiddenMessage is the refusal shown to viewers (UI-SPEC copy).
const lagForbiddenMessage = "Consumer lag requires the analyst role."

// SystemMetric states and unavailability reasons. An unavailable metric
// never carries a value, so a missing source is never rendered as zero.
const (
	MetricAvailable   = "available"
	MetricUnavailable = "unavailable"

	ReasonPrometheusUnavailable = "prometheus_unavailable"
	ReasonNoData                = "no_data"
	ReasonNotConfigured         = "not_configured"
	ReasonNotReachable          = "not_reachable"
	// ReasonOperatorOnly marks lag withheld from a non-operator tenant: the
	// value is platform-wide, so only operator tenants see it (ADR 0014).
	ReasonOperatorOnly = "operator_only"
	// ReasonPartialCoverage marks a group lag withheld because at least one
	// partition of its topic has no live position reporter (rebalance, dead
	// member before its session timeout, unseeded assignment); the maximum
	// of the reported partitions would understate lag.
	ReasonPartialCoverage = "partial_coverage"
)

// SystemMetric is one global metric value with explicit availability.
type SystemMetric struct {
	State  string   `json:"state"`
	Value  *float64 `json:"value,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

// ConsumerLagRow is the lag of one pipeline consumer group.
type ConsumerLagRow struct {
	Group string       `json:"group"`
	Lag   SystemMetric `json:"lag"`
}

// ConsumerLag is the complete lag-only response. It carries no throughput,
// volumes, errors, partitions or tenant data.
type ConsumerLag struct {
	ObservedAt time.Time        `json:"observed_at"`
	Rows       []ConsumerLagRow `json:"consumer_lag"`
}

type lagGroup struct {
	display    string
	configured string
}

// LagHandler serves GET /api/v1/metrics/lag to analysts and admins. Values
// are shown only to operator tenants; other tenants get operator_only rows.
type LagHandler struct {
	metrics   SystemMetrics
	auth      *auth.Verifier
	operators auth.OperatorTenants
	groups    []lagGroup
	now       func() time.Time
}

// NewLagHandler maps the configured consumer group names to their fixed
// display names. A nil clock uses the real clock.
func NewLagHandler(metrics SystemMetrics, verifier *auth.Verifier, groups config.KafkaConfig,
	operators auth.OperatorTenants, now func() time.Time,
) *LagHandler {
	if now == nil {
		now = time.Now
	}
	return &LagHandler{
		metrics: metrics, auth: verifier, operators: operators, now: now, groups: pipelineGroups(groups),
	}
}

// pipelineGroups maps the configured consumer group names to their fixed
// display names, in the fixed pipeline order.
func pipelineGroups(groups config.KafkaConfig) []lagGroup {
	return []lagGroup{
		{display: "event-writer", configured: groups.EventWriterGroup},
		{display: "detector", configured: groups.DetectorGroup},
		{display: "alert-writer", configured: groups.AlertWriterGroup},
	}
}

// Register authenticates every lag read; the role rule runs in GetLag.
func (h *LagHandler) Register(e *echo.Echo) {
	e.GET("/api/v1/metrics/lag", h.GetLag, h.auth.Middleware)
}

// GetLag returns per-group availability with HTTP 200 even when Prometheus is
// down; only query-api's own failures produce errors.
func (h *LagHandler) GetLag(c echo.Context) error {
	p, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	if auth.RequireTriage(p) != nil {
		auth.Audit(c, "consumer_lag", "denied", "role_forbidden", p)
		return errs.NewForbiddenError(lagForbiddenMessage, false)
	}
	if c.Request().URL.RawQuery != "" {
		return queryNotAccepted()
	}
	now := h.now().UTC()
	rows := make([]ConsumerLagRow, len(h.groups))
	if !h.operators.Includes(p) {
		// Prometheus is never asked for a tenant that may not see the value.
		for i, g := range h.groups {
			rows[i] = ConsumerLagRow{Group: g.display, Lag: unavailableMetric(ReasonOperatorOnly)}
		}
		return c.JSON(http.StatusOK, ConsumerLag{ObservedAt: now, Rows: rows})
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), prometheusTimeout)
	defer cancel()
	results := make(map[string]queryResult, len(lagQueries()))
	for _, promql := range lagQueries() {
		series, qerr := h.metrics.Query(ctx, promql, now)
		results[promql] = queryResult{series: series, err: qerr}
	}
	for i, g := range h.groups {
		rows[i] = ConsumerLagRow{Group: g.display, Lag: lagMetric(results, g.configured)}
	}
	return c.JSON(http.StatusOK, ConsumerLag{ObservedAt: now, Rows: rows})
}

// lagQueries is the complete allowlist of one lag read.
func lagQueries() []string {
	return []string{qLag, qLagReported, qLagPartitions}
}

// lagMetric is the coverage-aware lag of one group. Any failed query makes
// it unavailable with that failure's reason. An unknown denominator is
// no_data; fewer reported partitions than the topic has is partial_coverage;
// otherwise it is the maximum lag over the group's partitions.
func lagMetric(r map[string]queryResult, group string) SystemMetric {
	lag, reported, expected := r[qLag], r[qLagReported], r[qLagPartitions]
	for _, q := range []queryResult{lag, reported, expected} {
		if q.err != nil {
			return unavailableFor(q.err)
		}
	}
	partitions, ok := expected.series[group]
	if !ok || !(partitions >= 1) {
		return unavailableMetric(ReasonNoData)
	}
	if covered, found := reported.series[group]; !found || covered < partitions {
		return unavailableMetric(ReasonPartialCoverage)
	}
	return lag.metric(group)
}

// seriesMetric reads one label value of a query result. A failed query is
// unavailable with the failure reason; an absent series is no_data.
func seriesMetric(series map[string]float64, err error, key string) SystemMetric {
	if err != nil {
		return unavailableFor(err)
	}
	value, ok := series[key]
	if !ok {
		return unavailableMetric(ReasonNoData)
	}
	return SystemMetric{State: MetricAvailable, Value: &value}
}

// unavailableFor maps a Prometheus failure to its reason; the error text is
// never exposed.
func unavailableFor(err error) SystemMetric {
	if errors.Is(err, ErrPrometheusNotConfigured) {
		return unavailableMetric(ReasonNotConfigured)
	}
	return unavailableMetric(ReasonPrometheusUnavailable)
}

func unavailableMetric(reason string) SystemMetric {
	return SystemMetric{State: MetricUnavailable, Reason: reason}
}
