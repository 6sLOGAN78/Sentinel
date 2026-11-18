package query_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
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
	lagPath = "/api/v1/metrics/lag"
	// wantLagQuery is the only PromQL the handler may send. It is spelled out
	// here instead of read from the package so a changed constant fails.
	wantLagQuery     = `max by (group) (sentinel_kafka_consumer_lag)`
	lagViewerMessage = "Consumer lag requires the analyst role."
	// operatorTenant is the fixtures' default operator allowlist entry;
	// otherTenant is a tenant that is never an operator.
	operatorTenant  = "tenant-a"
	otherTenant     = "tenant-b"
	lagTimeoutBound = 2 * time.Second
	// principalContextKey mirrors the auth package's unexported key. If it
	// ever changes, the unknown-role test gets 401 instead of 403 and fails.
	principalContextKey = "sentinel.verified-principal"
)

// lagGroups uses non-default names so the test proves the handler maps the
// configured groups rather than hardcoding the defaults.
func lagGroups() config.KafkaConfig {
	return config.KafkaConfig{
		EventWriterGroup: "grp-ew", DetectorGroup: "grp-det", AlertWriterGroup: "grp-aw",
	}
}

type lagCall struct {
	promql   string
	at       time.Time
	deadline time.Duration
	hasLimit bool
}

// fakeMetrics is a func-type SystemMetrics fake that records every call.
type fakeMetrics struct {
	mu     sync.Mutex
	calls  []lagCall
	result func() (map[string]float64, error)
}

func (f *fakeMetrics) Query(ctx context.Context, promql string, at time.Time) (map[string]float64, error) {
	deadline, ok := ctx.Deadline()
	f.mu.Lock()
	f.calls = append(f.calls, lagCall{promql: promql, at: at, deadline: time.Until(deadline), hasLimit: ok})
	f.mu.Unlock()
	return f.result()
}

func (f *fakeMetrics) recorded() []lagCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]lagCall(nil), f.calls...)
}

type lagFixture struct {
	e       *echo.Echo
	metrics *fakeMetrics
	key     []byte
	logs    *syncBuffer
	now     time.Time
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) lines() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Split(bytes.TrimSpace(b.buf.Bytes()), []byte("\n"))
}

func newLagFixture(t *testing.T, result func() (map[string]float64, error)) *lagFixture {
	t.Helper()
	return newLagFixtureWith(t, result, []string{operatorTenant})
}

func newLagFixtureWith(t *testing.T, result func() (map[string]float64, error), operators []string) *lagFixture {
	t.Helper()
	key := make([]byte, 48)
	_, err := rand.Read(key)
	require.NoError(t, err)
	verifier, err := auth.NewVerifier(key, "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	f := &lagFixture{
		e: echo.New(), metrics: &fakeMetrics{result: result}, key: key, logs: &syncBuffer{},
		now: time.Date(2026, time.November, 17, 12, 30, 15, 0, time.FixedZone("x", 3600)),
	}
	f.e.HTTPErrorHandler = middleware.NewGlobalMiddlewares(nil).GlobalErrorHandler
	logger := zerolog.New(f.logs)
	f.e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(middleware.LoggerKey, &logger)
			return next(c)
		}
	})
	query.NewLagHandler(f.metrics, verifier, lagGroups(), auth.NewOperatorTenants(operators),
		func() time.Time { return f.now }).Register(f.e)
	return f
}

func (f *lagFixture) token(t *testing.T, role, tenant string) string {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": role + "-1", "tenant": tenant,
		"role": role, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
	}).SignedString(f.key)
	require.NoError(t, err)
	return token
}

func (f *lagFixture) get(t *testing.T, target, role string) *httptest.ResponseRecorder {
	t.Helper()
	return f.getAs(t, target, role, operatorTenant)
}

// getAs calls target with a token for role in tenant, or without a
// credential when role is empty.
func (f *lagFixture) getAs(t *testing.T, target, role, tenant string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if role != "" {
		req.Header.Set("Authorization", "Bearer "+f.token(t, role, tenant))
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

// audits returns every structured security_audit event written so far.
func (f *lagFixture) audits(t *testing.T) []map[string]any {
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

func available() func() (map[string]float64, error) {
	return func() (map[string]float64, error) {
		return map[string]float64{"grp-ew": 12345, "grp-det": 0, "grp-aw": 7, "other-group": 99}, nil
	}
}

type lagBody struct {
	ObservedAt string `json:"observed_at"`
	Rows       []struct {
		Group string         `json:"group"`
		Lag   map[string]any `json:"lag"`
	} `json:"consumer_lag"`
}

func decodeLag(t *testing.T, rec *httptest.ResponseRecorder) lagBody {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body lagBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestLagAnalystAndAdminAllowed(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"analyst", "admin"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			f := newLagFixture(t, available())
			rec := f.get(t, lagPath, role)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			assert.Len(t, f.metrics.recorded(), 1)
			assert.Empty(t, f.audits(t), "allowed reads are not security outcomes")
		})
	}
}

func TestLagViewerForbiddenAndAudited(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, available())
	rec := f.get(t, lagPath, "viewer")
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, "FORBIDDEN", errorCode(t, rec))
	assert.Contains(t, rec.Body.String(), lagViewerMessage)
	assert.Empty(t, f.metrics.recorded(), "a denied caller must not reach Prometheus")
	assertLagDenialAudited(t, f, "viewer", "viewer-1")
}

func TestLagUnknownRoleForbiddenAndAudited(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, available())
	verifier, err := auth.NewVerifier(f.key, "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	handler := query.NewLagHandler(f.metrics, verifier, lagGroups(), auth.NewOperatorTenants([]string{operatorTenant}), nil)
	// The verifier rejects unknown roles in tokens, so inject a principal the
	// way the middleware would to prove the handler fails closed on its own.
	f.e.GET("/unknown-role", handler.GetLag, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(principalContextKey, auth.Principal{Subject: "operator-1", TenantID: "tenant-a", Role: "operator"})
			return next(c)
		}
	})
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/unknown-role", nil))
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Empty(t, f.metrics.recorded())
	assertLagDenialAudited(t, f, "operator", "operator-1")
}

func assertLagDenialAudited(t *testing.T, f *lagFixture, role, actor string) {
	t.Helper()
	events := f.audits(t)
	require.Len(t, events, 1, "the refusal must produce exactly one security_audit event")
	assert.Equal(t, "consumer_lag", events[0]["action"])
	assert.Equal(t, "denied", events[0]["outcome"])
	assert.Equal(t, "role_forbidden", events[0]["reason"])
	assert.Equal(t, role, events[0]["role"])
	assert.Equal(t, actor, events[0]["actor"])
	assert.Equal(t, "tenant-a", events[0]["tenant_id"])
}

func TestLagUnauthenticatedIs401(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, available())
	for _, header := range []string{"", "Bearer invalid", "Basic abc"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, lagPath, nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		f.e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, header)
		assert.Equal(t, "UNAUTHORIZED", errorCode(t, rec), header)
	}
	assert.Empty(t, f.metrics.recorded())
}

func TestLagRejectsQueryParameters(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"?group=x", "?x", "?query=up", "?time=0"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			f := newLagFixture(t, available())
			rec := f.get(t, lagPath+suffix, "analyst")
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Equal(t, "BAD_REQUEST", errorCode(t, rec))
			assert.Empty(t, f.metrics.recorded(), "request input must never reach PromQL")
		})
	}
}

func TestLagAvailableRowsInFixedOrder(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, available())
	body := decodeLag(t, f.get(t, lagPath, "analyst"))
	assert.Equal(t, "2026-11-17T11:30:15Z", body.ObservedAt, "observed_at is the captured now in UTC")
	require.Len(t, body.Rows, 3, "unmapped groups are dropped; exactly one row per pipeline group")
	want := []struct {
		group string
		value float64
	}{{"event-writer", 12345}, {"detector", 0}, {"alert-writer", 7}}
	for i, w := range want {
		assert.Equal(t, w.group, body.Rows[i].Group)
		assert.Equal(t, map[string]any{"state": "available", "value": w.value}, body.Rows[i].Lag, w.group)
	}
}

func TestLagPrometheusErrorIsUnavailableNeverZero(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, func() (map[string]float64, error) {
		return nil, errors.New("dial tcp: connection refused")
	})
	rec := f.get(t, lagPath, "admin")
	body := decodeLag(t, rec)
	require.Len(t, body.Rows, 3)
	for _, row := range body.Rows {
		assert.Equal(t, map[string]any{"state": "unavailable", "reason": "prometheus_unavailable"}, row.Lag, row.Group)
		assert.NotContains(t, row.Lag, "value", "an unavailable value must never be rendered as a number")
	}
	assert.NotContains(t, rec.Body.String(), `"value"`)
	assert.NotContains(t, rec.Body.String(), "connection refused", "dependency errors are not echoed")
}

func TestLagNotConfiguredReason(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, func() (map[string]float64, error) {
		return nil, fmt.Errorf("query: %w", query.ErrPrometheusNotConfigured)
	})
	body := decodeLag(t, f.get(t, lagPath, "analyst"))
	require.Len(t, body.Rows, 3)
	for _, row := range body.Rows {
		assert.Equal(t, map[string]any{"state": "unavailable", "reason": "not_configured"}, row.Lag, row.Group)
	}
}

func TestLagAbsentSeriesIsNoData(t *testing.T) {
	t.Parallel()
	for name, series := range map[string]map[string]float64{
		"empty":   {},
		"nil":     nil,
		"partial": {"grp-det": 42},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newLagFixture(t, func() (map[string]float64, error) { return series, nil })
			body := decodeLag(t, f.get(t, lagPath, "analyst"))
			require.Len(t, body.Rows, 3)
			for _, row := range body.Rows {
				if name == "partial" && row.Group == "detector" {
					assert.Equal(t, map[string]any{"state": "available", "value": float64(42)}, row.Lag)
					continue
				}
				assert.Equal(t, map[string]any{"state": "unavailable", "reason": "no_data"}, row.Lag, row.Group)
			}
		})
	}
}

func TestLagSendsOnlyTheFixedQuery(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, available())
	decodeLag(t, f.get(t, lagPath, "analyst"))
	calls := f.metrics.recorded()
	require.Len(t, calls, 1, "one query per request")
	assert.Equal(t, wantLagQuery, calls[0].promql)
	assert.True(t, calls[0].at.Equal(f.now), "the query is evaluated at the captured now")
	require.True(t, calls[0].hasLimit, "the Prometheus call must be bounded")
	assert.LessOrEqual(t, calls[0].deadline, lagTimeoutBound)
}

func TestLagResponseCarriesOnlyLag(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, available())
	rec := f.get(t, lagPath, "analyst")
	require.Equal(t, http.StatusOK, rec.Code)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	assert.Equal(t, []string{"consumer_lag", "observed_at"}, sortedRawKeys(raw))
	var rows []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["consumer_lag"], &rows))
	require.Len(t, rows, 3)
	for _, row := range rows {
		assert.Equal(t, []string{"group", "lag"}, sortedRawKeys(row))
		var lag map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(row["lag"], &lag))
		assert.Equal(t, []string{"state", "value"}, sortedRawKeys(lag))
	}
	for _, forbidden := range []string{"tenant", "partition", "topic", "throughput", "volume", "error", "rate"} {
		assert.NotContains(t, rec.Body.String(), forbidden)
	}
}

func sortedRawKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// assertOperatorOnly checks every row is unavailable for operator_only with
// no value and that Prometheus was never asked.
func assertOperatorOnly(t *testing.T, f *lagFixture, rec *httptest.ResponseRecorder) {
	t.Helper()
	body := decodeLag(t, rec)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Len(t, body.Rows, 3, "the panel still gets one row per pipeline group")
	for i, group := range []string{"event-writer", "detector", "alert-writer"} {
		assert.Equal(t, group, body.Rows[i].Group)
		assert.Equal(t, map[string]any{"state": "unavailable", "reason": "operator_only"}, body.Rows[i].Lag, group)
	}
	assert.NotContains(t, rec.Body.String(), `"value"`, "a non-operator tenant never sees a lag number")
	assert.Empty(t, f.metrics.recorded(), "a non-operator tenant must not reach Prometheus")
}

func TestLagOperatorTenantAnalystGetsValues(t *testing.T) {
	t.Parallel()
	f := newLagFixtureWith(t, available(), []string{"ops", operatorTenant})
	body := decodeLag(t, f.getAs(t, lagPath, "analyst", operatorTenant))
	require.Len(t, body.Rows, 3)
	assert.Equal(t, map[string]any{"state": "available", "value": float64(12345)}, body.Rows[0].Lag)
	assert.Len(t, f.metrics.recorded(), 1)
}

func TestLagNonOperatorTenantRowsOperatorOnly(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"analyst", "admin"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			f := newLagFixture(t, available())
			assertOperatorOnly(t, f, f.getAs(t, lagPath, role, otherTenant))
			assert.Empty(t, f.audits(t), "a redacted read is an allowed read, not a refusal")
		})
	}
}

func TestLagEmptyAllowlistIsOperatorOnlyForEveryone(t *testing.T) {
	t.Parallel()
	for name, operators := range map[string][]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newLagFixtureWith(t, available(), operators)
			for _, role := range []string{"analyst", "admin"} {
				assertOperatorOnly(t, f, f.getAs(t, lagPath, role, operatorTenant))
			}
		})
	}
}

func TestLagNonOperatorViewerStillForbidden(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, available())
	rec := f.getAs(t, lagPath, "viewer", otherTenant)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), lagViewerMessage)
	events := f.audits(t)
	require.Len(t, events, 1)
	assert.Equal(t, "role_forbidden", events[0]["reason"])
	assert.Equal(t, otherTenant, events[0]["tenant_id"])
}

func TestLagNonOperatorQueryStringIs400(t *testing.T) {
	t.Parallel()
	f := newLagFixture(t, available())
	rec := f.getAs(t, lagPath+"?group=x", "analyst", otherTenant)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Empty(t, f.metrics.recorded())
}
