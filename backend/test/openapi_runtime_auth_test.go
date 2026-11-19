package test_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/eventquery"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/query"
)

type contractLookupVerifier struct{}

func (contractLookupVerifier) Authenticate(_ context.Context, key string) (apikeys.Principal, error) {
	if key == "contract-write-key" {
		return apikeys.Principal{TenantID: "demo", Scopes: []string{apikeys.ScopeWrite}}, nil
	}
	if key != "contract-read-key" {
		return apikeys.Principal{}, apikeys.ErrUnauthorized
	}

	return apikeys.Principal{TenantID: "demo", Scopes: []string{apikeys.ScopeRead}}, nil
}

type contractLookupStore struct {
	event  *contracts.Event
	tenant string
	id     string
}

func (s *contractLookupStore) Get(_ context.Context, tenant, id string) (*contracts.Event, error) {
	s.tenant, s.id = tenant, id
	return s.event, nil
}

func TestOpenAPIGetEventRuntimeSecurity(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)
	op := operations(doc)["GET /api/v1/events/{event_id}"]
	require.NotNil(t, op)
	require.NotNil(t, op.Security)
	require.NotEmpty(t, *op.Security, "event lookup must require authentication")
	for _, alternative := range *op.Security {
		require.NotEmpty(t, alternative, "anonymous lookup must not be advertised")
		for name := range alternative {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				scheme := doc.Components.SecuritySchemes[name]
				require.NotNil(t, scheme, "advertised scheme must be defined")
				require.NotNil(t, scheme.Value)
				checkLookupSecurityAlternative(t, scheme.Value)
			})
		}
	}
	// Exercise both required branches even before the document advertises JWT.
	// A missing contract alternative cannot silently skip runtime acceptance.
	t.Run("signed_bearer", func(t *testing.T) {
		checkLookupSecurityAlternative(t, &openapi3.SecurityScheme{Type: "http", Scheme: "bearer"})
	})
	t.Run("strict_credential_dispatch", checkLookupCredentialDispatch)
}

func contractLookupFixture(t *testing.T) (*echo.Echo, *contractLookupStore, string) {
	t.Helper()
	secret := make([]byte, 48)
	_, err := rand.Read(secret)
	require.NoError(t, err)
	verifier, err := auth.NewVerifier(secret, "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "contract-operator", "tenant": "demo",
		"role": "viewer", "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
	}).SignedString(secret)
	require.NoError(t, err)
	principal, err := verifier.Verify(t.Context(), token)
	require.NoError(t, err, "cryptographically valid JWT is setup, not behavioral RED")
	require.Equal(t, "demo", principal.TenantID)
	store := &contractLookupStore{event: &contracts.Event{
		SchemaVersion: 1, EventID: "d5903431-44e9-40ae-a83a-f27e2d5a5d94", TenantID: "demo",
		OccurredAt: contracts.NewTimestamp(time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)),
		Source:     "contract-test", EventType: "auth.success",
	}}
	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)
	eventquery.NewAuthenticatedHandler(contractLookupVerifier{}, verifier, store).Register(e)
	return e, store, token
}

func checkLookupCredentialDispatch(t *testing.T) {
	t.Helper()
	for _, tc := range []struct {
		name    string
		headers []string
		key     string
		status  int
	}{
		{name: "legacy read", key: "contract-read-key", status: 200},
		{name: "legacy write", key: "contract-write-key", status: 403},
		{name: "missing", status: 401},
		{name: "invalid key", key: "invalid", status: 401},
		{name: "invalid bearer no fallback", headers: []string{"Bearer invalid"}, key: "contract-read-key", status: 401},
		{name: "empty header no fallback", headers: []string{""}, key: "contract-read-key", status: 401},
		{name: "malformed header no fallback", headers: []string{"Basic invalid"}, key: "contract-read-key", status: 401},
		{name: "repeated header no fallback", headers: []string{"Bearer invalid", "Bearer invalid"},
			key: "contract-read-key", status: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, store, _ := contractLookupFixture(t)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+store.event.EventID, nil)
			if tc.key != "" {
				req.Header.Set("X-Api-Key", tc.key)
			}
			for _, value := range tc.headers {
				req.Header.Add("Authorization", value)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
			if tc.status != 200 {
				require.Empty(t, store.tenant, "rejected credentials must never read storage")
			}
		})
	}
}

func checkLookupSecurityAlternative(t *testing.T, scheme *openapi3.SecurityScheme) {
	t.Helper()

	e, store, token := contractLookupFixture(t)
	event := store.event
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+event.EventID, nil)
	switch {
	case scheme.Type == "apiKey" && scheme.In == "header":
		req.Header.Set(scheme.Name, "contract-read-key")
	case scheme.Type == "http" && scheme.Scheme == "bearer":
		req.Header.Set("Authorization", "Bearer "+token)
	default:
		t.Fatalf("unimplemented advertised security scheme: type=%q in=%q scheme=%q",
			scheme.Type, scheme.In, scheme.Scheme)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "every advertised alternative must work: %s", rec.Body.String())
	require.Equal(t, "demo", store.tenant)
	require.Equal(t, event.EventID, store.id)
	var got contracts.Event
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, event, &got)
}

// consumerLagMetrics is a func-type query.SystemMetrics fake answering by
// PromQL text.
type consumerLagMetrics func(promql string) (map[string]float64, error)

func (f consumerLagMetrics) Query(_ context.Context, promql string, _ time.Time) (map[string]float64, error) {
	return f(promql)
}

// The lag endpoint's coverage queries, spelled out like the System table.
const (
	contractLagQuery        = `max by (group) (sentinel_kafka_consumer_lag)`
	contractLagReported     = `count by (group) (count by (group, topic, partition) (sentinel_kafka_consumer_lag))`
	contractLagPartitions   = `sum by (group) (max by (group, topic) (sentinel_kafka_topic_partitions))`
	contractTopicPartitions = 6
)

// contractFullCoverage reports every pipeline group fully covered.
func contractFullCoverage() map[string]float64 {
	return map[string]float64{
		"sentinel-event-writer": contractTopicPartitions, "sentinel-detector": contractTopicPartitions,
		"sentinel-alert-writer": contractTopicPartitions,
	}
}

// lagTable answers the lag query with lag and the coverage queries with
// reported and expected.
func lagTable(lag, reported, expected map[string]float64) consumerLagMetrics {
	return func(promql string) (map[string]float64, error) {
		switch promql {
		case contractLagReported:
			return reported, nil
		case contractLagPartitions:
			return expected, nil
		default:
			return lag, nil
		}
	}
}

func consumerLagResults() map[string]consumerLagMetrics {
	lag := map[string]float64{"sentinel-event-writer": 12, "sentinel-detector": 0, "sentinel-alert-writer": 3}
	partial := contractFullCoverage()
	partial["sentinel-detector"] = contractTopicPartitions - 2
	return map[string]consumerLagMetrics{
		"available":        lagTable(lag, contractFullCoverage(), contractFullCoverage()),
		"partial_coverage": lagTable(lag, partial, contractFullCoverage()),
		"prometheus_unavailable": func(string) (map[string]float64, error) {
			return nil, errors.New("refused")
		},
		"not_configured": func(string) (map[string]float64, error) { return nil, query.ErrPrometheusNotConfigured },
		"no_data":        lagTable(map[string]float64{}, map[string]float64{}, map[string]float64{}),
	}
}

type consumerLagFixture struct {
	e      *echo.Echo
	secret []byte
}

func newConsumerLagFixture(t *testing.T, metrics consumerLagMetrics) consumerLagFixture {
	t.Helper()
	secret := make([]byte, 48)
	_, err := rand.Read(secret)
	require.NoError(t, err)
	verifier, err := auth.NewVerifier(secret, "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)
	groups := config.KafkaConfig{
		EventWriterGroup: "sentinel-event-writer", DetectorGroup: "sentinel-detector",
		AlertWriterGroup: "sentinel-alert-writer",
	}
	query.NewLagHandler(metrics, verifier, groups, auth.NewOperatorTenants([]string{contractOperatorTenant}), nil).
		Register(e)
	return consumerLagFixture{e: e, secret: secret}
}

// contractOperatorTenant is the only operator tenant of the contract
// fixtures; contractOtherTenant is a tenant that is never an operator.
const (
	contractOperatorTenant = "demo"
	contractOtherTenant    = "demo2"
)

// get calls the lag route with a signed token for role in the operator
// tenant, or without a credential when role is empty.
func (f consumerLagFixture) get(t *testing.T, role string) (int, []byte) {
	t.Helper()
	return f.getAs(t, role, contractOperatorTenant)
}

func (f consumerLagFixture) getAs(t *testing.T, role, tenant string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathConsumerLag, nil)
	if role != "" {
		now := time.Now().UTC().Truncate(time.Second)
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "contract-" + role, "tenant": tenant,
			"role": role, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
		}).SignedString(f.secret)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// TestOpenAPIConsumerLagRuntimeSecurity proves the advertised BearerAuth
// requirement and the documented role rule hold at runtime.
func TestOpenAPIConsumerLagRuntimeSecurity(t *testing.T) {
	t.Parallel()

	op := operations(loadOpenAPI(t))["GET "+pathConsumerLag]
	require.NotNil(t, op)
	require.NotNil(t, op.Security)
	require.Equal(t, openapi3.SecurityRequirements{{"BearerAuth": []string{}}}, *op.Security)

	f := newConsumerLagFixture(t, consumerLagResults()["available"])
	for _, tc := range []struct {
		role, tenant string
		status       int
		operatorOnly bool
	}{
		{role: "", tenant: contractOperatorTenant, status: http.StatusUnauthorized},
		{role: "viewer", tenant: contractOperatorTenant, status: http.StatusForbidden},
		{role: "analyst", tenant: contractOperatorTenant, status: http.StatusOK},
		{role: "admin", tenant: contractOperatorTenant, status: http.StatusOK},
		{role: "viewer", tenant: contractOtherTenant, status: http.StatusForbidden},
		{role: "analyst", tenant: contractOtherTenant, status: http.StatusOK, operatorOnly: true},
		{role: "admin", tenant: contractOtherTenant, status: http.StatusOK, operatorOnly: true},
	} {
		status, raw := f.getAs(t, tc.role, tc.tenant)
		require.Equal(t, tc.status, status, "role %q tenant %q: %s", tc.role, tc.tenant, raw)
		require.NotNil(t, op.Responses.Value(strconv.Itoa(status)), "status %d must be documented", status)
		if status != http.StatusOK {
			continue
		}
		require.Equal(t, tc.operatorOnly, strings.Contains(string(raw), `"operator_only"`),
			"role %q tenant %q: %s", tc.role, tc.tenant, raw)
		require.Equal(t, !tc.operatorOnly, strings.Contains(string(raw), `"value"`),
			"only operator tenants see lag values: %s", raw)
	}
}

// systemProber is a func-type query.ReadinessProber fake.
type systemProber func(address string) (int, int, error)

func (f systemProber) Probe(_ context.Context, address string) (int, int, error) {
	return f(address)
}

// systemMetricsTable answers each fixed System query by its PromQL text.
type systemMetricsTable struct {
	series map[string]map[string]float64
	err    error
}

func (s systemMetricsTable) Query(_ context.Context, promql string, _ time.Time) (map[string]float64, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.series[promql], nil
}

func systemAvailableSeries() map[string]map[string]float64 {
	jobs := map[string]float64{"ingest": 1, "event-writer": 1, "detector": 1, "alert-writer": 1, "query-api": 1}
	return map[string]map[string]float64{
		`count by (job) (up == 1)`:                                jobs,
		`sum by (job) (changes(process_start_time_seconds[15m]))`: jobs,
		`sum by (result) (rate(sentinel_ingest_events_total[1m]))`: {
			"accepted": 10, "rejected": 1, "rate_limited": 0, "shed": 0, "unavailable": 0,
		},
		`sum(rate(sentinel_ingest_requests_total[5m]))`:                {"": 4},
		`sum(rate(sentinel_ingest_requests_total{status=~"5.."}[5m]))`: {"": 0},
		`sum(rate(sentinel_ingest_requests_total{status="429"}[5m]))`:  {"": 1},
		`sum(rate(sentinel_ingest_requests_total{status="503"}[5m]))`:  {"": 0},
		`sum(rate(sentinel_dlq_events_total[5m]))`:                     {"": 0},
		contractLagQuery:      {"sentinel-event-writer": 3},
		contractLagReported:   contractFullCoverage(),
		contractLagPartitions: contractFullCoverage(),
		`histogram_quantile(0.95, sum by (le) (rate(sentinel_detection_delay_seconds_bucket[5m])))`: {
			"": 0.2,
		},
		`histogram_quantile(0.95, sum by (le) (rate(sentinel_clickhouse_write_duration_seconds_bucket[5m])))`: {
			"": 0.01,
		},
	}
}

type systemCase struct {
	metrics    systemMetricsTable
	prober     systemProber
	grafanaURL string
}

func systemCases() map[string]systemCase {
	ready := systemProber(func(string) (int, int, error) { return 1, 1, nil })
	return map[string]systemCase{
		"available": {
			metrics: systemMetricsTable{series: systemAvailableSeries()}, prober: ready,
			grafanaURL: "http://127.0.0.1:13000",
		},
		"prometheus_unavailable": {metrics: systemMetricsTable{err: errors.New("refused")}, prober: ready},
		"not_configured":         {metrics: systemMetricsTable{err: query.ErrPrometheusNotConfigured}, prober: ready},
		"no_data":                {metrics: systemMetricsTable{series: map[string]map[string]float64{}}, prober: ready},
		"readiness_mixed": {
			metrics: systemMetricsTable{series: systemAvailableSeries()},
			prober: func(address string) (int, int, error) {
				switch address {
				case "ingest:9101":
					return 0, 0, errors.New("no such host")
				case "detector:9102":
					return 1, 2, nil
				default:
					return 0, 1, nil
				}
			},
		},
	}
}

type systemFixture struct {
	e      *echo.Echo
	secret []byte
}

func newSystemFixture(t *testing.T, tc systemCase) systemFixture {
	t.Helper()
	secret := make([]byte, 48)
	_, err := rand.Read(secret)
	require.NoError(t, err)
	verifier, err := auth.NewVerifier(secret, "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)
	groups := config.KafkaConfig{
		EventWriterGroup: "sentinel-event-writer", DetectorGroup: "sentinel-detector",
		AlertWriterGroup: "sentinel-alert-writer",
	}
	obs := config.ObservabilityConfig{
		ServiceName: "query-api", GrafanaURL: tc.grafanaURL,
		System: config.SystemConfig{
			Ingest:      config.ServiceTarget{Address: "ingest:9101", Expected: 1},
			EventWriter: config.ServiceTarget{Address: "event-writer:9103", Expected: 1},
			Detector:    config.ServiceTarget{Address: "detector:9102", Expected: 2},
			AlertWriter: config.ServiceTarget{Address: "alert-writer:9104", Expected: 1},
			// query-api left unconfigured: readiness reports not_configured.
		},
	}
	query.NewSystemHandler(tc.metrics, tc.prober, verifier, obs, groups,
		auth.NewOperatorTenants([]string{contractOperatorTenant}), nil).Register(e)
	return systemFixture{e: e, secret: secret}
}

func (f systemFixture) get(t *testing.T, role string) (int, []byte) {
	t.Helper()
	return f.getAs(t, role, contractOperatorTenant)
}

func (f systemFixture) getAs(t *testing.T, role, tenant string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSystem, nil)
	if role != "" {
		now := time.Now().UTC().Truncate(time.Second)
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "contract-" + role, "tenant": tenant,
			"role": role, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
		}).SignedString(f.secret)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// TestOpenAPISystemRuntimeSecurity proves the advertised BearerAuth
// requirement and the operator-admin rule hold at runtime.
func TestOpenAPISystemRuntimeSecurity(t *testing.T) {
	t.Parallel()

	op := operations(loadOpenAPI(t))["GET "+pathSystem]
	require.NotNil(t, op)
	require.NotNil(t, op.Security)
	require.Equal(t, openapi3.SecurityRequirements{{"BearerAuth": []string{}}}, *op.Security)

	f := newSystemFixture(t, systemCases()["available"])
	for _, tc := range []struct {
		role, tenant string
		status       int
	}{
		{role: "", tenant: contractOperatorTenant, status: http.StatusUnauthorized},
		{role: "viewer", tenant: contractOperatorTenant, status: http.StatusForbidden},
		{role: "analyst", tenant: contractOperatorTenant, status: http.StatusForbidden},
		{role: "admin", tenant: contractOperatorTenant, status: http.StatusOK},
		{role: "analyst", tenant: contractOtherTenant, status: http.StatusForbidden},
		{role: "admin", tenant: contractOtherTenant, status: http.StatusForbidden},
	} {
		status, raw := f.getAs(t, tc.role, tc.tenant)
		require.Equal(t, tc.status, status, "role %q tenant %q: %s", tc.role, tc.tenant, raw)
		require.NotNil(t, op.Responses.Value(strconv.Itoa(status)), "status %d must be documented", status)
		if tc.role == "admin" && tc.tenant == contractOtherTenant {
			require.Contains(t, string(raw), "System status is limited to operator administrators.")
		}
	}
}
