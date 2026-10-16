package test_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/eventquery"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
)

type contractLookupVerifier struct{}

func (contractLookupVerifier) Authenticate(_ context.Context, key string) (apikeys.Principal, error) {
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
}

func checkLookupSecurityAlternative(t *testing.T, scheme *openapi3.SecurityScheme) {
	t.Helper()

	event := &contracts.Event{
		SchemaVersion: 1, EventID: "d5903431-44e9-40ae-a83a-f27e2d5a5d94", TenantID: "demo",
		OccurredAt: contracts.NewTimestamp(time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC)),
		Source:     "contract-test", EventType: "auth.success",
	}
	store := &contractLookupStore{event: event}
	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)
	eventquery.NewHandler(contractLookupVerifier{}, store).Register(e)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+event.EventID, nil)
	switch {
	case scheme.Type == "apiKey" && scheme.In == "header":
		req.Header.Set(scheme.Name, "contract-read-key")
	case scheme.Type == "http" && scheme.Scheme == "bearer":
		req.Header.Set("Authorization", "Bearer contract-bearer-fixture")
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
