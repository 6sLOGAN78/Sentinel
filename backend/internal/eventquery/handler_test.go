package eventquery_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/eventquery"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

type verifierFunc func(context.Context, string) (apikeys.Principal, error)

func (f verifierFunc) Authenticate(ctx context.Context, key string) (apikeys.Principal, error) {
	return f(ctx, key)
}

type eventStoreFunc func(context.Context, string, string) (*contracts.Event, error)

func (f eventStoreFunc) Get(ctx context.Context, tenantID, eventID string) (*contracts.Event, error) {
	return f(ctx, tenantID, eventID)
}

func lookupEcho(v verifierFunc, store eventStoreFunc) *echo.Echo {
	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)
	eventquery.NewHandler(v, store).Register(e)

	return e
}

func getEvent(e *echo.Echo, id, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+id, nil)
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

func TestEventLookupAuthAndTenantBinding(t *testing.T) {
	t.Parallel()

	id := uuid.NewString()
	event := &contracts.Event{
		SchemaVersion: 1, EventID: id, TenantID: "demo",
		OccurredAt: contracts.NewTimestamp(time.Now()), Source: "test", EventType: "auth.success",
	}
	principal := apikeys.Principal{TenantID: "demo", Scopes: []string{apikeys.ScopeRead}}
	var gotTenant, gotID string
	e := lookupEcho(
		func(context.Context, string) (apikeys.Principal, error) { return principal, nil },
		func(_ context.Context, tenant, eventID string) (*contracts.Event, error) {
			gotTenant, gotID = tenant, eventID

			return event, nil
		},
	)

	rec := getEvent(e, id, "credential")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "demo", gotTenant)
	require.Equal(t, id, gotID)
	var got contracts.Event
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, id, got.EventID)
}

func TestEventLookupRejections(t *testing.T) {
	t.Parallel()

	id := uuid.NewString()
	tests := []struct {
		name      string
		id        string
		principal apikeys.Principal
		verifyErr error
		storeErr  error
		status    int
	}{
		{name: "invalid key", id: id, verifyErr: apikeys.ErrUnauthorized, status: http.StatusUnauthorized},
		{name: "missing scope", id: id, principal: apikeys.Principal{TenantID: "demo"}, status: http.StatusForbidden},
		{
			name: "invalid uuid", id: "not-a-uuid",
			principal: apikeys.Principal{TenantID: "demo", Scopes: []string{apikeys.ScopeRead}},
			status:    http.StatusBadRequest,
		},
		{
			name: "other tenant hidden", id: id,
			principal: apikeys.Principal{TenantID: "demo", Scopes: []string{apikeys.ScopeRead}},
			storeErr:  storageclickhouse.ErrNotFound, status: http.StatusNotFound,
		},
		{
			name: "clickhouse failure", id: id,
			principal: apikeys.Principal{TenantID: "demo", Scopes: []string{apikeys.ScopeRead}},
			storeErr:  errors.New("secret backend detail"), status: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := lookupEcho(
				func(context.Context, string) (apikeys.Principal, error) { return tc.principal, tc.verifyErr },
				func(context.Context, string, string) (*contracts.Event, error) { return nil, tc.storeErr },
			)
			rec := getEvent(e, tc.id, "credential")
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.NotContains(t, rec.Body.String(), "secret backend detail")
		})
	}
}

func TestEventLookupCredentialMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, key, bearer string
		status            int
		storeCalled       bool
	}{
		{name: "no credentials", status: http.StatusUnauthorized},
		{name: "bearer only", bearer: "Bearer fixture", status: http.StatusUnauthorized},
		{name: "invalid key plus bearer", key: "invalid", bearer: "Bearer fixture", status: http.StatusUnauthorized},
		{name: "read key", key: "read", status: http.StatusOK, storeCalled: true},
		{name: "write only", key: "write", status: http.StatusForbidden},
		{name: "cross tenant", key: "other", status: http.StatusNotFound, storeCalled: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkLookupCredential(t, tc.key, tc.bearer, tc.status, tc.storeCalled)
		})
	}
}

func checkLookupCredential(t *testing.T, key, bearer string, status int, wantStoreCall bool) {
	t.Helper()

	id := uuid.NewString()
	storeCalled := false
	e := lookupEcho(
		func(_ context.Context, credential string) (apikeys.Principal, error) {
			switch credential {
			case "read":
				return apikeys.Principal{TenantID: "demo", Scopes: []string{apikeys.ScopeRead}}, nil
			case "write":
				return apikeys.Principal{TenantID: "demo", Scopes: []string{apikeys.ScopeWrite}}, nil
			case "other":
				return apikeys.Principal{TenantID: "demo2", Scopes: []string{apikeys.ScopeRead}}, nil
			default:
				return apikeys.Principal{}, apikeys.ErrUnauthorized
			}
		},
		func(_ context.Context, tenant, eventID string) (*contracts.Event, error) {
			storeCalled = true
			require.Equal(t, id, eventID)
			if tenant == "demo2" {
				return nil, storageclickhouse.ErrNotFound
			}
			require.Equal(t, "demo", tenant)
			return &contracts.Event{EventID: id, TenantID: tenant}, nil
		},
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+id, nil)
	req.Header.Set("X-Api-Key", key)
	req.Header.Set("Authorization", bearer)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, status, rec.Code, rec.Body.String())
	require.Equal(t, wantStoreCall, storeCalled, "authentication/scope failures must not query storage")
	if status == http.StatusOK {
		return
	}
	var envelope errs.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, errs.MakeUpperCaseWithUnderscores(http.StatusText(status)), envelope.Error.Code)
	require.NotEmpty(t, envelope.Error.RequestID)
	require.NotEmpty(t, envelope.Error.Message)
	require.Empty(t, envelope.Error.Details)
	require.NotContains(t, rec.Body.String(), "fixture")
}
