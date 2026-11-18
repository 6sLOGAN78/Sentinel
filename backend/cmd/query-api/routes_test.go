package main

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/query"
)

// routesFixture mounts the production query routes without any store
// connection; only the lag route is exercised, and it never touches PG or CH.
func routesFixture(t *testing.T, prometheusURL string) (*echo.Echo, []byte, error) {
	t.Helper()
	secret := make([]byte, 48)
	_, err := rand.Read(secret)
	require.NoError(t, err)
	verifier, err := auth.NewVerifier(secret, "sentinel-dev", "sentinel-dashboard")
	require.NoError(t, err)
	cursors, err := query.NewCursorCodec(secret)
	require.NoError(t, err)
	cfg := &config.Config{
		Observability: &config.ObservabilityConfig{PrometheusURL: prometheusURL},
		Kafka: config.KafkaConfig{
			EventWriterGroup: "sentinel-event-writer", DetectorGroup: "sentinel-detector",
			AlertWriterGroup: "sentinel-alert-writer",
		},
	}
	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)
	err = registerQueryRoutes(e, cfg, nil, nil, verifier, cursors, &detection.Rules{SchemaVersion: 1})
	return e, secret, err
}

func lagRequest(t *testing.T, e *echo.Echo, secret []byte, role string) *httptest.ResponseRecorder {
	t.Helper()
	return routeRequest(t, e, secret, "/api/v1/metrics/lag", role)
}

func routeRequest(t *testing.T, e *echo.Echo, secret []byte, path, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if role != "" {
		now := time.Now().UTC().Truncate(time.Second)
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"iss": "sentinel-dev", "aud": "sentinel-dashboard", "sub": "op-1", "tenant": "tenant-a",
			"role": role, "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
		}).SignedString(secret)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestRegisterQueryRoutesMountsConsumerLag(t *testing.T) {
	t.Parallel()
	e, secret, err := routesFixture(t, "")
	require.NoError(t, err)

	require.Equal(t, http.StatusUnauthorized, lagRequest(t, e, secret, "").Code)
	require.Equal(t, http.StatusForbidden, lagRequest(t, e, secret, "viewer").Code)

	rec := lagRequest(t, e, secret, "analyst")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Rows []struct {
			Group string         `json:"group"`
			Lag   map[string]any `json:"lag"`
		} `json:"consumer_lag"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Rows, 3)
	for _, row := range body.Rows {
		require.Equal(t, map[string]any{"state": "unavailable", "reason": "not_configured"}, row.Lag, row.Group)
	}
}

func TestRegisterQueryRoutesUnreachablePrometheusIsUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	e, secret, err := routesFixture(t, addr)
	require.NoError(t, err)
	rec := lagRequest(t, e, secret, "admin")
	require.Equal(t, http.StatusOK, rec.Code, "a Prometheus outage must not fail the request")
	require.NotContains(t, rec.Body.String(), `"value"`)
	require.Contains(t, rec.Body.String(), `"reason":"prometheus_unavailable"`)
}

func TestRegisterQueryRoutesRejectsInvalidPrometheusURL(t *testing.T) {
	t.Parallel()
	_, _, err := routesFixture(t, "http://[::1")
	require.Error(t, err)
}

func TestRegisterQueryRoutesMountsSystemAdminOnly(t *testing.T) {
	t.Parallel()
	e, secret, err := routesFixture(t, "")
	require.NoError(t, err)

	require.Equal(t, http.StatusUnauthorized, routeRequest(t, e, secret, "/api/v1/system", "").Code)
	require.Equal(t, http.StatusForbidden, routeRequest(t, e, secret, "/api/v1/system", "viewer").Code)
	require.Equal(t, http.StatusForbidden, routeRequest(t, e, secret, "/api/v1/system", "analyst").Code)
	require.Equal(t, http.StatusBadRequest, routeRequest(t, e, secret, "/api/v1/system?q=up", "admin").Code)

	rec := routeRequest(t, e, secret, "/api/v1/system", "admin")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		MetricsSource map[string]any `json:"metrics_source"`
		Services      []struct {
			Name      string         `json:"name"`
			Readiness map[string]any `json:"readiness"`
		} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, map[string]any{"state": "unavailable", "reason": "not_configured"}, body.MetricsSource)
	require.Len(t, body.Services, 5)
	for _, s := range body.Services {
		// No system targets configured: nothing is probed, nothing is invented.
		require.Equal(t, map[string]any{"state": "unavailable", "reason": "not_configured"}, s.Readiness, s.Name)
	}
	require.NotContains(t, rec.Body.String(), `"value"`)
}
