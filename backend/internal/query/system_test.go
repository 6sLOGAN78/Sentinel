package query_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/query"
)

const (
	systemPath = "/api/v1/system"
	// The fixed PromQL allowlist, spelled out so a changed package constant
	// fails here instead of silently changing what the System page shows.
	wantUp          = `count by (job) (up == 1)`
	wantRestarts    = `sum by (job) (changes(process_start_time_seconds[15m]))`
	wantIngest      = `sum by (result) (rate(sentinel_ingest_events_total[1m]))`
	wantReqTotal    = `sum(rate(sentinel_ingest_requests_total[5m]))`
	want5xx         = `sum(rate(sentinel_ingest_requests_total{status=~"5.."}[5m]))`
	want429         = `sum(rate(sentinel_ingest_requests_total{status="429"}[5m]))`
	want503         = `sum(rate(sentinel_ingest_requests_total{status="503"}[5m]))`
	wantDLQ         = `sum(rate(sentinel_dlq_events_total[5m]))`
	wantDelayP95    = `histogram_quantile(0.95, sum by (le) (rate(sentinel_detection_delay_seconds_bucket[5m])))`
	wantWriteP95    = `histogram_quantile(0.95, sum by (le) (rate(sentinel_clickhouse_write_duration_seconds_bucket[5m])))`
	systemTimeBound = 2 * time.Second
	// systemOperatorMessage is the refusal for admins outside the operator
	// allowlist; it differs from the role refusal on purpose.
	systemOperatorMessage = "System status is limited to operator administrators."
	systemRoleMessage     = "System health requires the admin role."
)

func wantSystemQueries() []string {
	return []string{
		wantUp, wantRestarts, wantIngest, wantReqTotal, want5xx, want429, want503, wantDLQ,
		wantLagQuery, wantDelayP95, wantWriteP95,
	}
}

type promResult struct {
	series map[string]float64
	err    error
}

// systemMetrics answers each fixed query from a table and records every call.
type systemMetrics struct {
	mu      sync.Mutex
	calls   []lagCall
	results map[string]promResult
	fail    error
}

func (f *systemMetrics) Query(ctx context.Context, promql string, at time.Time) (map[string]float64, error) {
	deadline, ok := ctx.Deadline()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, lagCall{promql: promql, at: at, deadline: time.Until(deadline), hasLimit: ok})
	if f.fail != nil {
		return nil, f.fail
	}
	r, found := f.results[promql]
	if !found {
		return map[string]float64{}, nil
	}
	return r.series, r.err
}

func (f *systemMetrics) recorded() []lagCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]lagCall(nil), f.calls...)
}

type probeResult struct {
	ready, probed int
	err           error
}

// fakeProber answers readiness per configured address.
type fakeProber struct {
	mu        sync.Mutex
	addresses []string
	deadlines []bool
	results   map[string]probeResult
}

func (p *fakeProber) Probe(ctx context.Context, address string) (int, int, error) {
	_, ok := ctx.Deadline()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.addresses = append(p.addresses, address)
	p.deadlines = append(p.deadlines, ok)
	r := p.results[address]
	return r.ready, r.probed, r.err
}

func (p *fakeProber) called() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := append([]string(nil), p.addresses...)
	sort.Strings(out)
	return out
}

func systemObservability() config.ObservabilityConfig {
	return config.ObservabilityConfig{
		ServiceName: "query-api",
		System: config.SystemConfig{
			Ingest:      config.ServiceTarget{Address: "ingest:9101", Expected: 2},
			EventWriter: config.ServiceTarget{Address: "event-writer:9103", Expected: 2},
			Detector:    config.ServiceTarget{Address: "detector:9102", Expected: 3},
			AlertWriter: config.ServiceTarget{Address: "alert-writer:9104", Expected: 1},
			QueryAPI:    config.ServiceTarget{Address: "query-api:9105", Expected: 2},
		},
	}
}

func allReady() map[string]probeResult {
	return map[string]probeResult{
		"ingest:9101":       {ready: 2, probed: 2},
		"event-writer:9103": {ready: 2, probed: 2},
		"detector:9102":     {ready: 3, probed: 3},
		"alert-writer:9104": {ready: 1, probed: 1},
		"query-api:9105":    {ready: 2, probed: 2},
	}
}

func allSeries() map[string]promResult {
	return map[string]promResult{
		wantUp: {series: map[string]float64{
			"ingest": 2, "event-writer": 2, "detector": 3, "alert-writer": 1, "query-api": 2, "prometheus": 1,
		}},
		wantRestarts: {series: map[string]float64{
			"ingest": 0, "event-writer": 1, "detector": 0, "alert-writer": 0, "query-api": 2,
		}},
		wantIngest: {series: map[string]float64{
			"accepted": 100, "rejected": 2, "rate_limited": 3, "shed": 0, "unavailable": 1,
		}},
		wantReqTotal: {series: map[string]float64{"": 10}},
		want5xx:      {series: map[string]float64{"": 0.5}},
		want429:      {series: map[string]float64{"": 1}},
		want503:      {series: map[string]float64{"": 0.25}},
		wantDLQ:      {series: map[string]float64{"": 0.1}},
		wantLagQuery: {series: map[string]float64{"grp-ew": 5, "grp-det": 0, "grp-aw": 1, "other": 9}},
		wantDelayP95: {series: map[string]float64{"": 0.8}},
		wantWriteP95: {series: map[string]float64{"": 0.05}},
	}
}

type systemFixture struct {
	e       *echo.Echo
	metrics *systemMetrics
	prober  *fakeProber
	key     []byte
	logs    *syncBuffer
	now     time.Time
}

func newSystemFixture(t *testing.T, cfg config.ObservabilityConfig, metrics *systemMetrics,
	probes map[string]probeResult,
) *systemFixture {
	t.Helper()
	return newSystemFixtureWith(t, cfg, metrics, probes, []string{operatorTenant})
}

func newSystemFixtureWith(t *testing.T, cfg config.ObservabilityConfig, metrics *systemMetrics,
	probes map[string]probeResult, operators []string,
) *systemFixture {
	t.Helper()
	key := make([]byte, 48)
	_, err := rand.Read(key)
	require.NoError(t, err)
	verifier, err := auth.NewVerifier(key, "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	f := &systemFixture{
		e: echo.New(), metrics: metrics, prober: &fakeProber{results: probes}, key: key, logs: &syncBuffer{},
		now: time.Date(2026, time.November, 18, 9, 15, 30, 0, time.FixedZone("x", -7200)),
	}
	f.e.HTTPErrorHandler = middleware.NewGlobalMiddlewares(nil).GlobalErrorHandler
	logger := zerolog.New(f.logs)
	f.e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(middleware.LoggerKey, &logger)
			return next(c)
		}
	})
	query.NewSystemHandler(f.metrics, f.prober, verifier, cfg, lagGroups(), auth.NewOperatorTenants(operators),
		func() time.Time { return f.now }).Register(f.e)
	return f
}

func defaultSystemFixture(t *testing.T) *systemFixture {
	t.Helper()
	return newSystemFixture(t, systemObservability(), &systemMetrics{results: allSeries()}, allReady())
}

func (f *systemFixture) get(t *testing.T, target, role string) *httptest.ResponseRecorder {
	t.Helper()
	return f.getAs(t, target, role, operatorTenant)
}

func (f *systemFixture) getAs(t *testing.T, target, role, tenant string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if role != "" {
		now := time.Now().UTC().Truncate(time.Second)
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": role + "-1", "tenant": tenant,
			"role": role, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
		}).SignedString(f.key)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

func (f *systemFixture) audits(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range f.logs.lines() {
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		require.NoError(t, json.Unmarshal(line, &event), string(line))
		if event["event"] == "security_audit" {
			out = append(out, event)
		}
	}
	return out
}

type systemService struct {
	Name      string         `json:"name"`
	Expected  int            `json:"expected_replicas"`
	Up        map[string]any `json:"replicas_up"`
	Restarts  map[string]any `json:"restarts_15m"`
	Readiness map[string]any `json:"readiness"`
}

type systemBody struct {
	ObservedAt    string          `json:"observed_at"`
	GrafanaURL    *string         `json:"grafana_url"`
	MetricsSource map[string]any  `json:"metrics_source"`
	Services      []systemService `json:"services"`
	Ingest        struct {
		Total    map[string]any            `json:"total"`
		ByResult map[string]map[string]any `json:"by_result"`
	} `json:"ingest"`
	HTTPErrors  map[string]map[string]any `json:"http_errors"`
	DLQRate     map[string]any            `json:"dlq_rate"`
	ConsumerLag []struct {
		Group string         `json:"group"`
		Lag   map[string]any `json:"lag"`
	} `json:"consumer_lag"`
	Latency map[string]map[string]any `json:"latency"`
}

func decodeSystem(t *testing.T, rec *httptest.ResponseRecorder) systemBody {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body systemBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func avail(v float64) map[string]any { return map[string]any{"state": "available", "value": v} }

func unavail(reason string) map[string]any {
	return map[string]any{"state": "unavailable", "reason": reason}
}

// promFields lists every Prometheus-backed metric in a decoded body.
func promFields(b systemBody) map[string]map[string]any {
	out := map[string]map[string]any{
		"ingest.total": b.Ingest.Total, "dlq_rate": b.DLQRate,
	}
	for k, v := range b.Ingest.ByResult {
		out["ingest.by_result."+k] = v
	}
	for k, v := range b.HTTPErrors {
		out["http_errors."+k] = v
	}
	for k, v := range b.Latency {
		out["latency."+k] = v
	}
	for _, row := range b.ConsumerLag {
		out["consumer_lag."+row.Group] = row.Lag
	}
	for _, s := range b.Services {
		out["services."+s.Name+".replicas_up"] = s.Up
		out["services."+s.Name+".restarts_15m"] = s.Restarts
	}
	return out
}

func TestSystemAdminAllFieldsAvailable(t *testing.T) {
	t.Parallel()
	f := defaultSystemFixture(t)
	rec := f.get(t, systemPath, "admin")
	body := decodeSystem(t, rec)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "2026-11-18T11:15:30Z", body.ObservedAt, "observed_at is the captured now in UTC")
	assert.Nil(t, body.GrafanaURL, "grafana_url is omitted when not configured")
	assert.Equal(t, map[string]any{"state": "available"}, body.MetricsSource)

	wantServices := []struct {
		name               string
		expected           int
		up, restarts, pool float64
	}{
		{"ingest", 2, 2, 0, 2}, {"event-writer", 2, 2, 1, 2}, {"detector", 3, 3, 0, 3},
		{"alert-writer", 1, 1, 0, 1}, {"query-api", 2, 2, 2, 2},
	}
	require.Len(t, body.Services, len(wantServices))
	for i, w := range wantServices {
		s := body.Services[i]
		assert.Equal(t, w.name, s.Name)
		assert.Equal(t, w.expected, s.Expected, w.name)
		assert.Equal(t, avail(w.up), s.Up, w.name)
		assert.Equal(t, avail(w.restarts), s.Restarts, w.name)
		assert.Equal(t, map[string]any{"state": "ready", "ready_replicas": w.pool, "probed_replicas": w.pool},
			s.Readiness, w.name)
	}

	assert.Equal(t, avail(106), body.Ingest.Total)
	assert.Equal(t, map[string]map[string]any{
		"accepted": avail(100), "rejected": avail(2), "rate_limited": avail(3), "shed": avail(0),
		"unavailable": avail(1),
	}, body.Ingest.ByResult)
	assert.Equal(t, map[string]map[string]any{
		"ratio": avail(0.15), "status_5xx": avail(0.5), "status_429": avail(1), "status_503": avail(0.25),
	}, body.HTTPErrors, "ratio = (5xx + 429) / all requests")
	assert.Equal(t, avail(0.1), body.DLQRate)
	require.Len(t, body.ConsumerLag, 3)
	for i, w := range []struct {
		group string
		lag   float64
	}{{"event-writer", 5}, {"detector", 0}, {"alert-writer", 1}} {
		assert.Equal(t, w.group, body.ConsumerLag[i].Group)
		assert.Equal(t, avail(w.lag), body.ConsumerLag[i].Lag)
	}
	assert.Equal(t, map[string]map[string]any{
		"detection_delay_p95_seconds": avail(0.8), "clickhouse_write_p95_seconds": avail(0.05),
	}, body.Latency)
	assert.Empty(t, f.audits(t), "allowed reads are not security outcomes")
}

func TestSystemNonAdminForbiddenAndAudited(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"viewer", "analyst"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			f := defaultSystemFixture(t)
			// A query string must not turn a role refusal into a 400.
			for _, target := range []string{systemPath, systemPath + "?q=up"} {
				rec := f.get(t, target, role)
				require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
				assert.Equal(t, "FORBIDDEN", errorCode(t, rec))
			}
			assert.Empty(t, f.metrics.recorded(), "a denied caller must not reach Prometheus")
			assert.Empty(t, f.prober.called(), "a denied caller must not trigger probes")
			events := f.audits(t)
			require.Len(t, events, 2, "every refusal produces one security_audit event")
			for _, event := range events {
				assert.Equal(t, "system_status", event["action"])
				assert.Equal(t, "denied", event["outcome"])
				assert.Equal(t, "role_forbidden", event["reason"])
				assert.Equal(t, role, event["role"])
				assert.Equal(t, role+"-1", event["actor"])
				assert.Equal(t, "tenant-a", event["tenant_id"])
			}
		})
	}
}

func TestSystemUnauthenticatedIs401(t *testing.T) {
	t.Parallel()
	f := defaultSystemFixture(t)
	for _, header := range []string{"", "Bearer invalid", "Basic abc"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, systemPath, nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		f.e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, header)
		assert.Equal(t, "UNAUTHORIZED", errorCode(t, rec), header)
	}
	assert.Empty(t, f.metrics.recorded())
	assert.Empty(t, f.prober.called())
}

func TestSystemRejectsQueryParameters(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"?q=up", "?query=up", "?x", "?time=0", "?service=ingest"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			f := defaultSystemFixture(t)
			rec := f.get(t, systemPath+suffix, "admin")
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Equal(t, "BAD_REQUEST", errorCode(t, rec))
			assert.Empty(t, f.metrics.recorded(), "request input must never reach PromQL")
			assert.Empty(t, f.prober.called())
		})
	}
}

func TestSystemPrometheusDownKeepsReadinessAndNeverZero(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err    error
		reason string
	}{
		"unreachable":    {err: errors.New("dial tcp: connection refused"), reason: "prometheus_unavailable"},
		"not_configured": {err: fmt.Errorf("q: %w", query.ErrPrometheusNotConfigured), reason: "not_configured"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newSystemFixture(t, systemObservability(), &systemMetrics{fail: tc.err}, allReady())
			rec := f.get(t, systemPath, "admin")
			body := decodeSystem(t, rec)
			assert.Equal(t, unavail(tc.reason), body.MetricsSource)
			fields := promFields(body)
			assert.Len(t, fields, 2+5+4+2+3+10, "every Prometheus field is present")
			for field, metric := range fields {
				assert.Equal(t, unavail(tc.reason), metric, field)
			}
			assert.NotContains(t, rec.Body.String(), `"value"`, "an unavailable value is never a number")
			assert.NotContains(t, rec.Body.String(), "connection refused", "dependency errors are not echoed")
			for _, s := range body.Services {
				assert.Equal(t, "ready", s.Readiness["state"], "readiness does not depend on Prometheus: %s", s.Name)
			}
		})
	}
}

func TestSystemEmptyVectorsAreNoData(t *testing.T) {
	t.Parallel()
	f := newSystemFixture(t, systemObservability(), &systemMetrics{results: map[string]promResult{}}, allReady())
	rec := f.get(t, systemPath, "admin")
	body := decodeSystem(t, rec)
	assert.Equal(t, map[string]any{"state": "available"}, body.MetricsSource, "Prometheus answered")
	for field, metric := range promFields(body) {
		assert.Equal(t, unavail("no_data"), metric, field)
	}
	assert.NotContains(t, rec.Body.String(), `"value"`)
}

func TestSystemErrorRatioWithoutTrafficIsNoData(t *testing.T) {
	t.Parallel()
	for name, override := range map[string]map[string]promResult{
		"zero_requests": {wantReqTotal: {series: map[string]float64{"": 0}}},
		"no_5xx_series": {want5xx: {series: map[string]float64{}}},
		"429_failed":    {want429: {err: errors.New("timeout")}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			series := allSeries()
			for k, v := range override {
				series[k] = v
			}
			f := newSystemFixture(t, systemObservability(), &systemMetrics{results: series}, allReady())
			body := decodeSystem(t, f.get(t, systemPath, "admin"))
			ratio := body.HTTPErrors["ratio"]
			assert.Equal(t, "unavailable", ratio["state"], "no division by zero and no partial ratio")
			assert.NotContains(t, ratio, "value")
			if name == "429_failed" {
				assert.Equal(t, unavail("prometheus_unavailable"), ratio)
				assert.Equal(t, unavail("prometheus_unavailable"), body.HTTPErrors["status_429"])
				assert.Equal(t, avail(0.5), body.HTTPErrors["status_5xx"], "one failed query does not hide others")
			} else {
				assert.Equal(t, unavail("no_data"), ratio)
			}
		})
	}
}

func TestSystemPartialSeries(t *testing.T) {
	t.Parallel()
	series := allSeries()
	series[wantUp] = promResult{series: map[string]float64{"ingest": 2}}
	series[wantIngest] = promResult{series: map[string]float64{"accepted": 4}}
	f := newSystemFixture(t, systemObservability(), &systemMetrics{results: series}, allReady())
	body := decodeSystem(t, f.get(t, systemPath, "admin"))
	assert.Equal(t, avail(2), body.Services[0].Up)
	for _, s := range body.Services[1:] {
		assert.Equal(t, unavail("no_data"), s.Up, "an absent job is no data, never zero replicas: %s", s.Name)
	}
	assert.Equal(t, avail(4), body.Ingest.Total)
	assert.Equal(t, avail(4), body.Ingest.ByResult["accepted"])
	assert.Equal(t, unavail("no_data"), body.Ingest.ByResult["shed"])
}

func TestSystemReadinessClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		expected      int
		ready, probed int
		err           error
		want          map[string]any
	}{
		{"all_ready", 2, 2, 2, nil, map[string]any{"state": "ready", "ready_replicas": 2.0, "probed_replicas": 2.0}},
		{"scaled_up", 2, 3, 3, nil, map[string]any{"state": "ready", "ready_replicas": 3.0, "probed_replicas": 3.0}},
		{"undeclared", 0, 1, 1, nil, map[string]any{"state": "ready", "ready_replicas": 1.0, "probed_replicas": 1.0}},
		{"fewer_than_expected", 2, 1, 1, nil,
			map[string]any{"state": "degraded", "ready_replicas": 1.0, "probed_replicas": 1.0}},
		{"one_unready", 2, 2, 3, nil,
			map[string]any{"state": "degraded", "ready_replicas": 2.0, "probed_replicas": 3.0}},
		{"none_ready", 2, 0, 2, nil,
			map[string]any{"state": "not_ready", "ready_replicas": 0.0, "probed_replicas": 2.0}},
		{"nothing_probed", 2, 0, 0, nil,
			map[string]any{"state": "not_ready", "ready_replicas": 0.0, "probed_replicas": 0.0}},
		{"dns_failure", 2, 0, 0, errors.New("lookup ingest: no such host"), unavail("not_reachable")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := systemObservability()
			cfg.System.Ingest.Expected = tc.expected
			probes := allReady()
			probes["ingest:9101"] = probeResult{ready: tc.ready, probed: tc.probed, err: tc.err}
			f := newSystemFixture(t, cfg, &systemMetrics{results: allSeries()}, probes)
			rec := f.get(t, systemPath, "admin")
			body := decodeSystem(t, rec)
			assert.Equal(t, tc.want, body.Services[0].Readiness)
			assert.NotContains(t, rec.Body.String(), "no such host", "probe errors are not echoed")
		})
	}
}

func TestSystemUnconfiguredAddressIsNotProbed(t *testing.T) {
	t.Parallel()
	cfg := systemObservability()
	cfg.System.Detector = config.ServiceTarget{}
	f := newSystemFixture(t, cfg, &systemMetrics{results: allSeries()}, allReady())
	body := decodeSystem(t, f.get(t, systemPath, "admin"))
	assert.Equal(t, "detector", body.Services[2].Name)
	assert.Equal(t, unavail("not_configured"), body.Services[2].Readiness)
	assert.Equal(t, avail(3), body.Services[2].Up, "Prometheus fields still render")
	assert.Equal(t, []string{"alert-writer:9104", "event-writer:9103", "ingest:9101", "query-api:9105"},
		f.prober.called(), "only configured addresses are probed, each once")
	for _, bounded := range f.prober.deadlines {
		assert.True(t, bounded, "every probe runs under a deadline")
	}
}

func TestSystemGrafanaURLOnlyWhenConfigured(t *testing.T) {
	t.Parallel()
	cfg := systemObservability()
	cfg.GrafanaURL = "http://127.0.0.1:13000"
	f := newSystemFixture(t, cfg, &systemMetrics{results: allSeries()}, allReady())
	body := decodeSystem(t, f.get(t, systemPath, "admin"))
	require.NotNil(t, body.GrafanaURL)
	assert.Equal(t, "http://127.0.0.1:13000", *body.GrafanaURL)

	rec := defaultSystemFixture(t).get(t, systemPath, "admin")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "grafana_url")
}

func TestSystemSendsOnlyTheFixedQueries(t *testing.T) {
	t.Parallel()
	f := defaultSystemFixture(t)
	decodeSystem(t, f.get(t, systemPath, "admin"))
	calls := f.metrics.recorded()
	got := make([]string, 0, len(calls))
	for _, call := range calls {
		got = append(got, call.promql)
		assert.True(t, call.at.Equal(f.now), "every query is evaluated at the captured now")
		require.True(t, call.hasLimit, "every Prometheus call must be bounded")
		assert.LessOrEqual(t, call.deadline, systemTimeBound)
	}
	want := wantSystemQueries()
	sort.Strings(got)
	sort.Strings(want)
	assert.Equal(t, want, got, "exactly the fixed allowlist, each query once")
}

func TestSystemResponseKeys(t *testing.T) {
	t.Parallel()
	rec := defaultSystemFixture(t).get(t, systemPath, "admin")
	require.Equal(t, http.StatusOK, rec.Code)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	assert.Equal(t, []string{
		"consumer_lag", "dlq_rate", "http_errors", "ingest", "latency", "metrics_source", "observed_at", "services",
	}, sortedRawKeys(raw))
	for _, forbidden := range []string{"tenant", "partition", "instance", "promql", "sentinel_"} {
		assert.NotContains(t, rec.Body.String(), forbidden)
	}
}

// fakeResolver maps a host to fixed addresses and records every lookup.
type fakeResolver struct {
	mu      sync.Mutex
	lookups []string
	hosts   map[string][]string
}

func (r *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups = append(r.lookups, host)
	addrs, ok := r.hosts[host]
	if !ok {
		return nil, errors.New("lookup " + host + ": no such host")
	}
	return addrs, nil
}

func (r *fakeResolver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lookups)
}

// loopbackServer serves handler on 127.0.0.1 and returns its port.
func loopbackServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	return port
}

func TestDNSProberReadyAndNotReady(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var paths []string
	record := func(r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
	}
	readyPort := loopbackServer(t, func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(http.StatusOK)
	})
	unreadyPort := loopbackServer(t, func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	resolver := &fakeResolver{hosts: map[string][]string{"svc": {"127.0.0.1"}}}
	prober := query.NewDNSReadinessProber(resolver, &http.Client{})

	ready, probed, err := prober.Probe(t.Context(), "svc:"+readyPort)
	require.NoError(t, err)
	assert.Equal(t, [2]int{1, 1}, [2]int{ready, probed})

	ready, probed, err = prober.Probe(t.Context(), "svc:"+unreadyPort)
	require.NoError(t, err)
	assert.Equal(t, [2]int{0, 1}, [2]int{ready, probed})
	assert.Equal(t, []string{"GET /readyz", "GET /readyz"}, paths, "only the fixed readiness path is fetched")
}

func TestDNSProberDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	var hit atomic.Bool
	port := loopbackServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			hit.Store(true)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	resolver := &fakeResolver{hosts: map[string][]string{"svc": {"127.0.0.1"}}}
	ready, probed, err := query.NewDNSReadinessProber(resolver, &http.Client{}).Probe(t.Context(), "svc:"+port)
	require.NoError(t, err)
	assert.Equal(t, [2]int{0, 1}, [2]int{ready, probed}, "a redirect is not ready")
	assert.False(t, hit.Load(), "the prober never follows a redirect to another target")
}

func TestDNSProberResolveFailureAndBadAddress(t *testing.T) {
	t.Parallel()
	resolver := &fakeResolver{hosts: map[string][]string{}}
	prober := query.NewDNSReadinessProber(resolver, &http.Client{})
	ready, probed, err := prober.Probe(t.Context(), "missing:9101")
	require.Error(t, err)
	assert.Equal(t, [2]int{0, 0}, [2]int{ready, probed})
	assert.Equal(t, 1, resolver.count())

	for _, bad := range []string{"", "ingest", "ingest:", ":9101", "ingest:http", "ingest:0", "http://ingest:9101"} {
		_, _, err = prober.Probe(t.Context(), bad)
		require.Error(t, err, bad)
	}
	assert.Equal(t, 1, resolver.count(), "malformed addresses never reach the resolver")
}

func TestDNSProberCapsReplicasAndDedupes(t *testing.T) {
	t.Parallel()
	port := loopbackServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	addrs := []string{"127.0.0.1", "127.0.0.1"}
	for i := 2; i <= 21; i++ {
		addrs = append(addrs, fmt.Sprintf("127.0.0.%d", i))
	}
	resolver := &fakeResolver{hosts: map[string][]string{"svc": addrs}}
	ready, probed, err := query.NewDNSReadinessProber(resolver, &http.Client{}).Probe(t.Context(), "svc:"+port)
	require.NoError(t, err)
	assert.Equal(t, 16, probed, "at most 16 replicas per service are probed")
	assert.Equal(t, 1, ready, "only 127.0.0.1 listens; duplicates are probed once")
}

func TestDNSProberPerProbeTimeout(t *testing.T) {
	t.Parallel()
	port := loopbackServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	})
	resolver := &fakeResolver{hosts: map[string][]string{"svc": {"127.0.0.1"}}}
	start := time.Now()
	ready, probed, err := query.NewDNSReadinessProber(resolver, &http.Client{}).Probe(t.Context(), "svc:"+port)
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Equal(t, [2]int{0, 1}, [2]int{ready, probed}, "a probe past its deadline is not ready")
	assert.Less(t, elapsed, 1500*time.Millisecond, "each probe is bounded to about 1 s")
}

func TestDNSProberBoundedConcurrency(t *testing.T) {
	t.Parallel()
	first, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(first.Addr().String())
	require.NoError(t, err)
	var mu sync.Mutex
	inFlight, peak := 0, 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		time.Sleep(150 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: handler}
	listeners := []net.Listener{first}
	addrs := []string{"127.0.0.1"}
	for i := 2; i <= 12; i++ {
		ln, listenErr := net.Listen("tcp", fmt.Sprintf("127.0.0.%d:%s", i, port))
		require.NoError(t, listenErr)
		listeners = append(listeners, ln)
		addrs = append(addrs, fmt.Sprintf("127.0.0.%d", i))
	}
	for _, ln := range listeners {
		go func() { _ = srv.Serve(ln) }()
	}
	t.Cleanup(func() { _ = srv.Close() })

	resolver := &fakeResolver{hosts: map[string][]string{"a": addrs, "b": addrs}}
	prober := query.NewDNSReadinessProber(resolver, &http.Client{})
	var wg sync.WaitGroup
	results := make([][2]int, 2)
	for i, host := range []string{"a", "b"} {
		wg.Go(func() {
			ready, probed, probeErr := prober.Probe(t.Context(), host+":"+port)
			assert.NoError(t, probeErr)
			results[i] = [2]int{ready, probed}
		})
	}
	wg.Wait()
	assert.Equal(t, [][2]int{{12, 12}, {12, 12}}, results)
	mu.Lock()
	defer mu.Unlock()
	assert.LessOrEqual(t, peak, 8, "probes share one bound of 8 across services")
	assert.Greater(t, peak, 1, "probes run concurrently")
}

func TestSystemOperatorAdminAllowed(t *testing.T) {
	t.Parallel()
	f := newSystemFixtureWith(t, systemObservability(), &systemMetrics{results: allSeries()}, allReady(),
		[]string{"ops", operatorTenant})
	body := decodeSystem(t, f.getAs(t, systemPath, "admin", operatorTenant))
	assert.Len(t, body.Services, 5)
	assert.Len(t, f.metrics.recorded(), len(wantSystemQueries()))
	assert.Empty(t, f.audits(t))
}

// assertOperatorRefusal checks one non-operator refusal: the distinct
// message, no Prometheus or probe traffic and an operator_forbidden audit.
func assertOperatorRefusal(t *testing.T, f *systemFixture, rec *httptest.ResponseRecorder, tenant string) {
	t.Helper()
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, "FORBIDDEN", errorCode(t, rec))
	assert.Contains(t, rec.Body.String(), systemOperatorMessage)
	assert.NotContains(t, rec.Body.String(), systemRoleMessage, "the operator refusal is not the role refusal")
	assert.Empty(t, f.metrics.recorded(), "a non-operator admin must not reach Prometheus")
	assert.Empty(t, f.prober.called(), "a non-operator admin must not trigger probes")
	events := f.audits(t)
	require.Len(t, events, 1, "the refusal must produce exactly one security_audit event")
	assert.Equal(t, "system_status", events[0]["action"])
	assert.Equal(t, "denied", events[0]["outcome"])
	assert.Equal(t, "operator_forbidden", events[0]["reason"], "the audit tells operator and role refusals apart")
	assert.Equal(t, "admin", events[0]["role"])
	assert.Equal(t, "admin-1", events[0]["actor"])
	assert.Equal(t, tenant, events[0]["tenant_id"])
}

func TestSystemNonOperatorAdminForbiddenAndAudited(t *testing.T) {
	t.Parallel()
	f := defaultSystemFixture(t)
	assertOperatorRefusal(t, f, f.getAs(t, systemPath, "admin", otherTenant), otherTenant)
}

func TestSystemEmptyAllowlistDeniesEveryone(t *testing.T) {
	t.Parallel()
	for name, operators := range map[string][]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newSystemFixtureWith(t, systemObservability(), &systemMetrics{results: allSeries()}, allReady(),
				operators)
			assertOperatorRefusal(t, f, f.getAs(t, systemPath, "admin", operatorTenant), operatorTenant)
			for _, role := range []string{"viewer", "analyst"} {
				rec := f.getAs(t, systemPath, role, operatorTenant)
				require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
				assert.Contains(t, rec.Body.String(), systemRoleMessage)
			}
			assert.Empty(t, f.metrics.recorded())
		})
	}
}

func TestSystemOperatorTenantNonAdminKeepsRoleRefusal(t *testing.T) {
	t.Parallel()
	f := defaultSystemFixture(t)
	rec := f.getAs(t, systemPath, "analyst", otherTenant)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), systemRoleMessage)
	events := f.audits(t)
	require.Len(t, events, 1)
	assert.Equal(t, "role_forbidden", events[0]["reason"])
}

// TestSystemCheckOrder pins 401, then 400 for any query string, then the
// operator 403; the role 403 still precedes the query check.
func TestSystemCheckOrder(t *testing.T) {
	t.Parallel()
	f := defaultSystemFixture(t)
	rec := f.getAs(t, systemPath+"?q=up", "", otherTenant)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	rec = f.getAs(t, systemPath+"?q=up", "admin", otherTenant)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Empty(t, f.audits(t), "a malformed request is rejected before the operator rule")
	assertOperatorRefusal(t, f, f.getAs(t, systemPath, "admin", otherTenant), otherTenant)
}
