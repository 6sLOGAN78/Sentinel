package test_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// consumerLagMetrics is a func-type query.SystemMetrics fake.
type consumerLagMetrics func() (map[string]float64, error)

func (f consumerLagMetrics) Query(context.Context, string, time.Time) (map[string]float64, error) {
	return f()
}

func consumerLagResults() map[string]consumerLagMetrics {
	return map[string]consumerLagMetrics{
		"available": func() (map[string]float64, error) {
			return map[string]float64{
				"sentinel-event-writer": 12, "sentinel-detector": 0, "sentinel-alert-writer": 3,
			}, nil
		},
		"prometheus_unavailable": func() (map[string]float64, error) { return nil, errors.New("refused") },
		"not_configured":         func() (map[string]float64, error) { return nil, query.ErrPrometheusNotConfigured },
		"no_data":                func() (map[string]float64, error) { return map[string]float64{}, nil },
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
	query.NewLagHandler(metrics, verifier, groups, nil).Register(e)
	return consumerLagFixture{e: e, secret: secret}
}

// get calls the lag route with a signed token for role, or without a
// credential when role is empty.
func (f consumerLagFixture) get(t *testing.T, role string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathConsumerLag, nil)
	if role != "" {
		now := time.Now().UTC().Truncate(time.Second)
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "contract-" + role, "tenant": "demo",
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
		role   string
		status int
	}{
		{role: "", status: http.StatusUnauthorized},
		{role: "viewer", status: http.StatusForbidden},
		{role: "analyst", status: http.StatusOK},
		{role: "admin", status: http.StatusOK},
	} {
		status, raw := f.get(t, tc.role)
		require.Equal(t, tc.status, status, "role %q: %s", tc.role, raw)
		require.NotNil(t, op.Responses.Value(strconv.Itoa(status)), "status %d must be documented", status)
	}
}
