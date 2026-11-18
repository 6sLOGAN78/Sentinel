package query

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/errs"
)

// The fixed PromQL allowlist of the System endpoint (plus qLag from lag.go).
// No request input is ever interpolated into a query, and every result is
// aggregated so no instance, tenant or other high-cardinality label leaves
// Prometheus.
const (
	qUp             = `count by (job) (up == 1)`
	qRestarts       = `sum by (job) (changes(process_start_time_seconds[15m]))`
	qIngestByResult = `sum by (result) (rate(sentinel_ingest_events_total[1m]))`
	qReqTotal       = `sum(rate(sentinel_ingest_requests_total[5m]))`
	q5xx            = `sum(rate(sentinel_ingest_requests_total{status=~"5.."}[5m]))`
	q429            = `sum(rate(sentinel_ingest_requests_total{status="429"}[5m]))`
	q503            = `sum(rate(sentinel_ingest_requests_total{status="503"}[5m]))`
	qDLQ            = `sum(rate(sentinel_dlq_events_total[5m]))`
	qDelayP95       = `histogram_quantile(0.95, sum by (le) (rate(sentinel_detection_delay_seconds_bucket[5m])))`
	qWriteP95       = `histogram_quantile(0.95, sum by (le) (rate(sentinel_clickhouse_write_duration_seconds_bucket[5m])))`
)

// Readiness states of one service.
const (
	ReadinessReady       = "ready"
	ReadinessDegraded    = "degraded"
	ReadinessNotReady    = "not_ready"
	ReadinessUnavailable = "unavailable"
)

const (
	// systemForbiddenMessage is the refusal for every non-admin role: the
	// System data is global and would leak other tenants' activity.
	systemForbiddenMessage = "System health requires the admin role."
	// readinessBudget bounds all readiness probes of one request. Probes not
	// finished by then count as not ready.
	readinessBudget = 2 * time.Second
	// unlabeled is the key of an aggregated sample without grouping labels.
	unlabeled = ""
)

// ReadinessProber probes every replica behind one configured ops address.
// ready counts replicas whose /readyz answered 200; probed counts replicas
// attempted. err is returned only when the replicas cannot be resolved.
type ReadinessProber interface {
	Probe(ctx context.Context, address string) (ready, probed int, err error)
}

// SystemReadiness is the readiness of one service from its /readyz probes.
// Counts are present only when probes ran; an unavailable readiness carries a
// reason and no counts.
type SystemReadiness struct {
	State  string `json:"state"`
	Ready  *int   `json:"ready_replicas,omitempty"`
	Probed *int   `json:"probed_replicas,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// SystemService is the health of one pipeline service.
type SystemService struct {
	Name             string          `json:"name"`
	ExpectedReplicas int             `json:"expected_replicas"`
	ReplicasUp       SystemMetric    `json:"replicas_up"`
	Restarts         SystemMetric    `json:"restarts_15m"`
	Readiness        SystemReadiness `json:"readiness"`
}

// MetricsSource reports whether Prometheus answered at all.
type MetricsSource struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// IngestByResult is the ingest events/s per outcome.
type IngestByResult struct {
	Accepted    SystemMetric `json:"accepted"`
	Rejected    SystemMetric `json:"rejected"`
	RateLimited SystemMetric `json:"rate_limited"`
	Shed        SystemMetric `json:"shed"`
	Unavailable SystemMetric `json:"unavailable"`
}

// IngestThroughput is the total ingest events/s and its breakdown.
type IngestThroughput struct {
	Total    SystemMetric   `json:"total"`
	ByResult IngestByResult `json:"by_result"`
}

// HTTPErrors holds ingest request rates by error status. Ratio is
// (5xx + 429) / all requests.
type HTTPErrors struct {
	Ratio     SystemMetric `json:"ratio"`
	Status5xx SystemMetric `json:"status_5xx"`
	Status429 SystemMetric `json:"status_429"`
	Status503 SystemMetric `json:"status_503"`
}

// SystemLatency holds the p95 latencies in seconds.
type SystemLatency struct {
	DetectionDelayP95  SystemMetric `json:"detection_delay_p95_seconds"`
	ClickHouseWriteP95 SystemMetric `json:"clickhouse_write_p95_seconds"`
}

// SystemStatus is the complete admin-only System response.
type SystemStatus struct {
	ObservedAt    time.Time        `json:"observed_at"`
	GrafanaURL    string           `json:"grafana_url,omitempty"`
	MetricsSource MetricsSource    `json:"metrics_source"`
	Services      []SystemService  `json:"services"`
	Ingest        IngestThroughput `json:"ingest"`
	HTTPErrors    HTTPErrors       `json:"http_errors"`
	DLQRate       SystemMetric     `json:"dlq_rate"`
	ConsumerLag   []ConsumerLagRow `json:"consumer_lag"`
	Latency       SystemLatency    `json:"latency"`
}

type systemTarget struct {
	name   string
	target config.ServiceTarget
}

type queryResult struct {
	series map[string]float64
	err    error
}

// SystemHandler serves GET /api/v1/system to admins only.
type SystemHandler struct {
	metrics    SystemMetrics
	prober     ReadinessProber
	auth       *auth.Verifier
	services   []systemTarget
	groups     []lagGroup
	grafanaURL string
	now        func() time.Time
}

// NewSystemHandler builds the handler from server configuration only. A nil
// clock uses the real clock.
func NewSystemHandler(metrics SystemMetrics, prober ReadinessProber, verifier *auth.Verifier,
	cfg config.ObservabilityConfig, kafka config.KafkaConfig, now func() time.Time,
) *SystemHandler {
	if now == nil {
		now = time.Now
	}
	return &SystemHandler{
		metrics: metrics, prober: prober, auth: verifier, groups: pipelineGroups(kafka),
		grafanaURL: cfg.GrafanaURL, now: now,
		// The Prometheus job names equal the display names.
		services: []systemTarget{
			{name: "ingest", target: cfg.System.Ingest},
			{name: "event-writer", target: cfg.System.EventWriter},
			{name: "detector", target: cfg.System.Detector},
			{name: "alert-writer", target: cfg.System.AlertWriter},
			{name: "query-api", target: cfg.System.QueryAPI},
		},
	}
}

// Register authenticates every System read; the role rule runs in GetSystem.
func (h *SystemHandler) Register(e *echo.Echo) {
	e.GET("/api/v1/system", h.GetSystem, h.auth.Middleware)
}

// GetSystem returns 200 with per-field availability even when Prometheus or
// some replicas are down; only query-api's own failures produce errors.
func (h *SystemHandler) GetSystem(c echo.Context) error {
	p, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	if auth.RequireAdmin(p) != nil {
		auth.Audit(c, "system_status", "denied", "role_forbidden", p)
		return errs.NewForbiddenError(systemForbiddenMessage, false)
	}
	if c.Request().URL.RawQuery != "" {
		return queryNotAccepted()
	}
	now := h.now().UTC()
	ctx := c.Request().Context()
	var (
		wg        sync.WaitGroup
		results   map[string]queryResult
		readiness []SystemReadiness
	)
	wg.Go(func() { results = h.runQueries(ctx, now) })
	wg.Go(func() { readiness = h.probeAll(ctx) })
	wg.Wait()
	return c.JSON(http.StatusOK, h.assemble(now, results, readiness))
}

// systemQueries is the complete allowlist one request evaluates.
func systemQueries() []string {
	return []string{
		qUp, qRestarts, qIngestByResult, qReqTotal, q5xx, q429, q503, qDLQ, qLag, qDelayP95, qWriteP95,
	}
}

// runQueries evaluates every allowlisted query concurrently at now under one
// shared Prometheus timeout.
func (h *SystemHandler) runQueries(ctx context.Context, now time.Time) map[string]queryResult {
	ctx, cancel := context.WithTimeout(ctx, prometheusTimeout)
	defer cancel()
	queries := systemQueries()
	out := make([]queryResult, len(queries))
	var wg sync.WaitGroup
	for i, promql := range queries {
		wg.Go(func() {
			series, err := h.metrics.Query(ctx, promql, now)
			out[i] = queryResult{series: series, err: err}
		})
	}
	wg.Wait()
	results := make(map[string]queryResult, len(queries))
	for i, promql := range queries {
		results[promql] = out[i]
	}
	return results
}

// probeAll probes every configured service concurrently. Prometheus plays no
// part, so readiness renders during a Prometheus outage.
func (h *SystemHandler) probeAll(ctx context.Context) []SystemReadiness {
	ctx, cancel := context.WithTimeout(ctx, readinessBudget)
	defer cancel()
	out := make([]SystemReadiness, len(h.services))
	var wg sync.WaitGroup
	for i, s := range h.services {
		if s.target.Address == "" {
			out[i] = SystemReadiness{State: ReadinessUnavailable, Reason: ReasonNotConfigured}
			continue
		}
		wg.Go(func() {
			ready, probed, err := h.prober.Probe(ctx, s.target.Address)
			out[i] = classifyReadiness(ready, probed, s.target.Expected, err)
		})
	}
	wg.Wait()
	return out
}

// classifyReadiness: ready when every probed replica is ready and at least
// the expected count (when declared) is ready; degraded when some but not all
// are; not_ready when none is.
func classifyReadiness(ready, probed, expected int, err error) SystemReadiness {
	if err != nil {
		return SystemReadiness{State: ReadinessUnavailable, Reason: ReasonNotReachable}
	}
	state := ReadinessDegraded
	switch {
	case ready == 0:
		state = ReadinessNotReady
	case ready == probed && ready >= expected:
		state = ReadinessReady
	}
	return SystemReadiness{State: state, Ready: &ready, Probed: &probed}
}

func (h *SystemHandler) assemble(now time.Time, r map[string]queryResult, readiness []SystemReadiness) SystemStatus {
	services := make([]SystemService, len(h.services))
	for i, s := range h.services {
		services[i] = SystemService{
			Name: s.name, ExpectedReplicas: s.target.Expected,
			ReplicasUp: r[qUp].metric(s.name), Restarts: r[qRestarts].metric(s.name),
			Readiness: readiness[i],
		}
	}
	lag := make([]ConsumerLagRow, len(h.groups))
	for i, g := range h.groups {
		lag[i] = ConsumerLagRow{Group: g.display, Lag: r[qLag].metric(g.configured)}
	}
	ingest := r[qIngestByResult]
	return SystemStatus{
		ObservedAt: now, GrafanaURL: h.grafanaURL, MetricsSource: metricsSource(r), Services: services,
		Ingest: IngestThroughput{Total: ingest.sum(), ByResult: IngestByResult{
			Accepted: ingest.metric("accepted"), Rejected: ingest.metric("rejected"),
			RateLimited: ingest.metric("rate_limited"), Shed: ingest.metric("shed"),
			Unavailable: ingest.metric("unavailable"),
		}},
		HTTPErrors: HTTPErrors{
			Ratio:     errorRatio(r[qReqTotal], r[q5xx], r[q429]),
			Status5xx: r[q5xx].metric(unlabeled), Status429: r[q429].metric(unlabeled),
			Status503: r[q503].metric(unlabeled),
		},
		DLQRate:     r[qDLQ].metric(unlabeled),
		ConsumerLag: lag,
		Latency: SystemLatency{
			DetectionDelayP95: r[qDelayP95].metric(unlabeled), ClickHouseWriteP95: r[qWriteP95].metric(unlabeled),
		},
	}
}

func (q queryResult) metric(key string) SystemMetric {
	return seriesMetric(q.series, q.err, key)
}

// sum totals every series of a grouped query; no series is no_data.
func (q queryResult) sum() SystemMetric {
	if q.err != nil {
		return unavailableFor(q.err)
	}
	if len(q.series) == 0 {
		return unavailableMetric(ReasonNoData)
	}
	var total float64
	for _, v := range q.series {
		total += v
	}
	return SystemMetric{State: MetricAvailable, Value: &total}
}

// errorRatio is (5xx + 429) / all requests. Any failed input makes it
// unavailable with that reason; any absent input or zero traffic is no_data,
// so it is never a partial or divide-by-zero value.
func errorRatio(total, s5xx, s429 queryResult) SystemMetric {
	inputs := []queryResult{total, s5xx, s429}
	values := make([]float64, len(inputs))
	for i, in := range inputs {
		m := in.metric(unlabeled)
		if m.State != MetricAvailable {
			return m
		}
		values[i] = *m.Value
	}
	if values[0] <= 0 {
		return unavailableMetric(ReasonNoData)
	}
	ratio := (values[1] + values[2]) / values[0]
	return SystemMetric{State: MetricAvailable, Value: &ratio}
}

// metricsSource is available when Prometheus answered at least one query.
func metricsSource(r map[string]queryResult) MetricsSource {
	var failure error
	for _, q := range r {
		if q.err == nil {
			return MetricsSource{State: MetricAvailable}
		}
		failure = q.err
	}
	if failure == nil {
		return MetricsSource{State: MetricAvailable}
	}
	return MetricsSource{State: MetricUnavailable, Reason: unavailableFor(failure).Reason}
}

const (
	// probeTimeout bounds one /readyz request.
	probeTimeout = time.Second
	// probeConcurrency bounds in-flight probes across all services.
	probeConcurrency = 8
	// maxProbeReplicas bounds the replicas probed per service.
	maxProbeReplicas = 16
	// probeBodyLimit bounds how much of a /readyz body is drained.
	probeBodyLimit = 4 << 10
	readinessPath  = "/readyz"
)

var errInvalidProbeTarget = errors.New("readiness target must be host:port")

// HostResolver resolves a service name to the addresses of its replicas.
// *net.Resolver satisfies it.
type HostResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// DNSReadinessProber resolves a configured service name to every replica and
// GETs the fixed /readyz path on each. Targets come only from server
// configuration; redirects are never followed.
type DNSReadinessProber struct {
	resolver HostResolver
	client   *http.Client
	sem      chan struct{}
}

// NewDNSReadinessProber copies client and disables redirects on the copy.
func NewDNSReadinessProber(resolver HostResolver, client *http.Client) *DNSReadinessProber {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &DNSReadinessProber{resolver: resolver, client: &c, sem: make(chan struct{}, probeConcurrency)}
}

// Probe resolves address and probes up to 16 distinct replica IPs, at most 8
// at a time across all callers, each bounded to 1 s.
func (p *DNSReadinessProber) Probe(ctx context.Context, address string) (int, int, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || strings.ContainsAny(host, "/@") {
		return 0, 0, errInvalidProbeTarget
	}
	if n, perr := strconv.ParseUint(port, 10, 16); perr != nil || n == 0 {
		return 0, 0, errInvalidProbeTarget
	}
	resolved, err := p.resolver.LookupHost(ctx, host)
	if err != nil {
		return 0, 0, fmt.Errorf("resolve readiness target: %w", err)
	}
	ips := replicaIPs(resolved)
	var (
		wg    sync.WaitGroup
		ready atomic.Int64
	)
	for _, ip := range ips {
		wg.Go(func() {
			if p.probeOne(ctx, ip, port) {
				ready.Add(1)
			}
		})
	}
	wg.Wait()
	return int(ready.Load()), len(ips), nil
}

// replicaIPs keeps distinct IP literals, sorted, capped at maxProbeReplicas.
func replicaIPs(resolved []string) []string {
	seen := make(map[string]struct{}, len(resolved))
	ips := make([]string, 0, len(resolved))
	for _, addr := range resolved {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		key := ip.String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		ips = append(ips, key)
	}
	sort.Strings(ips)
	if len(ips) > maxProbeReplicas {
		ips = ips[:maxProbeReplicas]
	}
	return ips
}

// probeOne reports whether one replica answered 200 within the probe timeout.
// A replica that cannot get a probe slot before ctx ends is not ready.
func (p *DNSReadinessProber) probeOne(ctx context.Context, ip, port string) bool {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	defer func() { <-p.sem }()
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	target := url.URL{Scheme: "http", Host: net.JoinHostPort(ip, port), Path: readinessPath}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.CopyN(io.Discard, resp.Body, probeBodyLimit)
	return resp.StatusCode == http.StatusOK
}
