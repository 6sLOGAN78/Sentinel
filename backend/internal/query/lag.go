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

// qLag is the only PromQL the lag endpoint runs. Position-based lag is
// published per owned partition; the maximum per group is the one number an
// analyst may see. No request input is ever interpolated into a query.
const qLag = `max by (group) (sentinel_kafka_consumer_lag)`

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

// LagHandler serves GET /api/v1/metrics/lag to analysts and admins.
type LagHandler struct {
	metrics SystemMetrics
	auth    *auth.Verifier
	groups  []lagGroup
	now     func() time.Time
}

// NewLagHandler maps the configured consumer group names to their fixed
// display names. A nil clock uses the real clock.
func NewLagHandler(metrics SystemMetrics, verifier *auth.Verifier, groups config.KafkaConfig,
	now func() time.Time,
) *LagHandler {
	if now == nil {
		now = time.Now
	}
	return &LagHandler{metrics: metrics, auth: verifier, now: now, groups: []lagGroup{
		{display: "event-writer", configured: groups.EventWriterGroup},
		{display: "detector", configured: groups.DetectorGroup},
		{display: "alert-writer", configured: groups.AlertWriterGroup},
	}}
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
	ctx, cancel := context.WithTimeout(c.Request().Context(), prometheusTimeout)
	defer cancel()
	series, err := h.metrics.Query(ctx, qLag, now)
	rows := make([]ConsumerLagRow, len(h.groups))
	for i, g := range h.groups {
		rows[i] = ConsumerLagRow{Group: g.display, Lag: lagMetric(series, err, g.configured)}
	}
	return c.JSON(http.StatusOK, ConsumerLag{ObservedAt: now, Rows: rows})
}

func lagMetric(series map[string]float64, err error, group string) SystemMetric {
	switch {
	case errors.Is(err, ErrPrometheusNotConfigured):
		return SystemMetric{State: MetricUnavailable, Reason: ReasonNotConfigured}
	case err != nil:
		return SystemMetric{State: MetricUnavailable, Reason: ReasonPrometheusUnavailable}
	}
	value, ok := series[group]
	if !ok {
		return SystemMetric{State: MetricUnavailable, Reason: ReasonNoData}
	}
	return SystemMetric{State: MetricAvailable, Value: &value}
}
