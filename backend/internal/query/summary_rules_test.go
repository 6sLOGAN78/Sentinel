package query_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/query"
)

const (
	summaryPath      = "/api/v1/metrics/summary"
	rulesPath        = "/api/v1/rules"
	trendBucketCount = 288
)

type countingSummary struct{ calls atomic.Int64 }

func (f *countingSummary) SummarizeEvents(context.Context, string, time.Time) (query.EventSummary, error) {
	f.calls.Add(1)
	return query.EventSummary{}, nil
}

func (f *countingSummary) SummarizeAlerts(context.Context, string, time.Time) (query.AlertSummary, error) {
	f.calls.Add(1)
	return query.AlertSummary{}, nil
}

type summaryRulesFixture struct {
	e     *echo.Echo
	fakes *countingSummary
	token string
}

func newSummaryRulesFixture(t *testing.T) summaryRulesFixture {
	t.Helper()
	key := make([]byte, 48)
	_, err := rand.Read(key)
	require.NoError(t, err)
	verifier, err := auth.NewVerifier(key, "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "viewer-1", "tenant": "tenant-a",
		"role": "viewer", "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
	}).SignedString(key)
	require.NoError(t, err)

	e := echo.New()
	e.HTTPErrorHandler = middleware.NewGlobalMiddlewares(nil).GlobalErrorHandler
	fakes := &countingSummary{}
	query.NewSummaryHandler(fakes, fakes, verifier, nil).Register(e)
	rules := &detection.Rules{SchemaVersion: 1, Rules: []detection.Rule{{
		ID: "PORT_SCAN", Version: 1, Enabled: true, Threshold: 20, Window: time.Minute,
		Severity: "medium", Cooldown: 5 * time.Minute,
	}}}
	query.NewRulesHandler(rules, verifier).Register(e)
	return summaryRulesFixture{e: e, fakes: fakes, token: token}
}

func (f summaryRulesFixture) do(t *testing.T, target string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if authenticated {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error.Code
}

func TestSummaryRejectsQueryString(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"?x=1", "?from=2026-01-01T00:00:00Z", "?x"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			f := newSummaryRulesFixture(t)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, summaryPath+suffix, nil)
			require.NotEmpty(t, req.URL.RawQuery)
			rec := f.do(t, summaryPath+suffix, true)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "BAD_REQUEST", errorCode(t, rec))
			assert.Zero(t, f.fakes.calls.Load())
		})
	}
}

func TestRulesRejectsQueryString(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"?x=1", "?from=2026-01-01T00:00:00Z", "?x"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			f := newSummaryRulesFixture(t)
			rec := f.do(t, rulesPath+suffix, true)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "BAD_REQUEST", errorCode(t, rec))
		})
	}
}

func TestSummaryRulesUnauthenticatedQueryIs401(t *testing.T) {
	t.Parallel()
	f := newSummaryRulesFixture(t)
	for _, path := range []string{summaryPath, rulesPath} {
		rec := f.do(t, path+"?x=1", false)
		require.Equal(t, http.StatusUnauthorized, rec.Code, path)
		assert.Equal(t, "UNAUTHORIZED", errorCode(t, rec), path)
	}
	assert.Zero(t, f.fakes.calls.Load())
}

func TestSummaryRulesWithoutQueryUnchanged(t *testing.T) {
	t.Parallel()
	f := newSummaryRulesFixture(t)

	rec := f.do(t, summaryPath, true)
	require.Equal(t, http.StatusOK, rec.Code)
	var summary struct {
		Open     uint64            `json:"open_alerts"`
		Severity []json.RawMessage `json:"alerts_by_severity"`
		Rules    []json.RawMessage `json:"top_rules"`
		Trend    []json.RawMessage `json:"trend_24h"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))
	assert.Len(t, summary.Trend, trendBucketCount)
	assert.Len(t, summary.Severity, 4)
	assert.NotNil(t, summary.Rules)
	assert.Zero(t, summary.Open)
	assert.Equal(t, int64(2), f.fakes.calls.Load())

	rec = f.do(t, rulesPath, true)
	require.Equal(t, http.StatusOK, rec.Code)
	var rules struct {
		Rules []struct {
			ID string `json:"rule_id"`
		} `json:"rules"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rules))
	require.Len(t, rules.Rules, 1)
	assert.Equal(t, "PORT_SCAN", rules.Rules[0].ID)
}
