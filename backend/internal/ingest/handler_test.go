package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/ingest"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
)

const testCredential = "snt_aaaaaaaaaaaa_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type verifierFunc func(context.Context, string) (apikeys.Principal, error)

func (f verifierFunc) Authenticate(ctx context.Context, key string) (apikeys.Principal, error) {
	return f(ctx, key)
}

type publisherFunc func(context.Context, []*contracts.Event, string) error

func (f publisherFunc) PublishBatch(ctx context.Context, events []*contracts.Event, requestID string) error {
	return f(ctx, events, requestID)
}

func handlerConfig() *config.Config {
	return &config.Config{
		Ingest: config.IngestConfig{
			MaxBatchSize: 500,
			MaxBodyBytes: 1 << 20,
			MaxInFlight:  64,
		},
		Contracts: config.ContractsConfig{
			MaxAttrKeys:  32,
			MaxAttrBytes: 4096,
			FutureSkew:   5 * time.Minute,
			PastSkew:     168 * time.Hour,
		},
	}
}

func validEvent(sourceIP *string) map[string]any {
	return map[string]any{
		"schema_version": 1,
		"event_id":       uuid.NewString(),
		"tenant_id":      "demo",
		"occurred_at":    time.Now().UTC().Add(-time.Second).Format("2006-01-02T15:04:05.000Z"),
		"source":         "test",
		"event_type":     "net.connection",
		"source_ip":      sourceIP,
	}
}

func batchBody(t *testing.T, events ...map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"events": events})
	require.NoError(t, err)

	return body
}

func newHandlerEcho(
	t *testing.T,
	principal apikeys.Principal,
	verifyErr error,
	publisher publisherFunc,
	cfg *config.Config,
) *echo.Echo {
	t.Helper()
	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)
	verifier := verifierFunc(func(_ context.Context, _ string) (apikeys.Principal, error) {
		return principal, verifyErr
	})
	ingest.NewHandler(verifier, publisher, cfg, logger).Register(e)

	return e
}

func postBatch(t *testing.T, e *echo.Echo, body []byte, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events:batch", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}

func TestPostBatchWaitsForPublisherAndPreservesOrder(t *testing.T) {
	source := "192.0.2.8"
	first := validEvent(&source)
	second := validEvent(nil)
	published := make(chan []*contracts.Event, 1)
	publisher := publisherFunc(func(_ context.Context, events []*contracts.Event, requestID string) error {
		require.NotEmpty(t, requestID)
		published <- events

		return nil
	})
	e := newHandlerEcho(t, apikeys.Principal{
		TenantID: "demo", Scopes: []string{apikeys.ScopeWrite},
	}, nil, publisher, handlerConfig())

	rec := postBatch(t, e, batchBody(t, first, second), testCredential)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	events := <-published
	require.Equal(t, first["event_id"], events[0].EventID)
	require.Equal(t, second["event_id"], events[1].EventID)
	require.NotNil(t, events[0].ReceivedAt)
	require.Equal(t, events[0].ReceivedAt, events[1].ReceivedAt)

	var response struct {
		AcceptedCount int      `json:"accepted_count"`
		RejectedCount int      `json:"rejected_count"`
		EventIDs      []string `json:"event_ids"`
		RequestID     string   `json:"request_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, 2, response.AcceptedCount)
	require.Zero(t, response.RejectedCount)
	require.Equal(t, []string{first["event_id"].(string), second["event_id"].(string)}, response.EventIDs)
	require.Equal(t, rec.Header().Get(echo.HeaderXRequestID), response.RequestID)
}

func TestPostBatchAuthAndTenantGatesPrecedePublish(t *testing.T) {
	event := validEvent(nil)
	cases := []struct {
		name      string
		principal apikeys.Principal
		verifyErr error
		status    int
	}{
		{name: "missing key", verifyErr: apikeys.ErrUnauthorized, status: http.StatusUnauthorized},
		{name: "dependency failure", verifyErr: apikeys.ErrUnavailable, status: http.StatusServiceUnavailable},
		{name: "missing scope", principal: apikeys.Principal{TenantID: "demo"}, status: http.StatusForbidden},
		{
			name: "wrong tenant", principal: apikeys.Principal{
				TenantID: "other", Scopes: []string{apikeys.ScopeWrite},
			}, status: http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			e := newHandlerEcho(t, tc.principal, tc.verifyErr, func(context.Context, []*contracts.Event, string) error {
				calls.Add(1)

				return nil
			}, handlerConfig())
			key := testCredential
			if tc.name == "missing key" {
				key = ""
			}

			rec := postBatch(t, e, batchBody(t, event), key)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Zero(t, calls.Load())
			require.NotContains(t, rec.Body.String(), testCredential)
		})
	}
}

func TestPostBatchBoundsAndLiteralColonRoute(t *testing.T) {
	principal := apikeys.Principal{TenantID: "demo", Scopes: []string{apikeys.ScopeWrite}}
	var calls atomic.Int32
	e := newHandlerEcho(t, principal, nil, func(context.Context, []*contracts.Event, string) error {
		calls.Add(1)

		return nil
	}, handlerConfig())

	rec := postBatch(t, e, []byte(`{"events":[]}`), testCredential)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	events := make([]map[string]any, 501)
	for index := range events {
		events[index] = validEvent(nil)
	}
	rec = postBatch(t, e, batchBody(t, events...), testCredential)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec = postBatch(t, e, []byte(`{"events":["`+strings.Repeat("x", 1<<20)+`"]}`), testCredential)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Zero(t, calls.Load())

	req := httptest.NewRequest(
		http.MethodPost, "/api/v1/eventsXbatch", bytes.NewReader(batchBody(t, validEvent(nil))),
	)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRouteLiteralColon(t *testing.T) {
	t.Parallel()

	e := newHandlerEcho(t, apikeys.Principal{
		TenantID: "demo", Scopes: []string{apikeys.ScopeWrite},
	}, nil, func(context.Context, []*contracts.Event, string) error { return nil }, handlerConfig())
	body := batchBody(t, validEvent(nil))

	tests := []struct {
		name   string
		method string
		target string
		status int
	}{
		{name: "literal colon", method: http.MethodPost, target: "/api/v1/events:batch", status: http.StatusAccepted},
		{name: "unrelated suffix", method: http.MethodPost, target: "/api/v1/events:xyz", status: http.StatusNotFound},
		{name: "encoded colon", method: http.MethodPost, target: "/api/v1/events%3Abatch", status: http.StatusNotFound},
		{name: "wrong method", method: http.MethodGet, target: "/api/v1/events:batch", status: http.StatusMethodNotAllowed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			req.Header.Set("X-Api-Key", testCredential)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
		})
	}
}

func TestPostBatchTenantMismatchBeatsFieldErrors(t *testing.T) {
	t.Parallel()

	invalid := validEvent(nil)
	invalid["event_id"] = "not-a-uuid"
	mismatch := validEvent(nil)
	mismatch["tenant_id"] = "other"
	var calls atomic.Int32
	e := newHandlerEcho(t, apikeys.Principal{
		TenantID: "demo", Scopes: []string{apikeys.ScopeWrite},
	}, nil, func(context.Context, []*contracts.Event, string) error {
		calls.Add(1)

		return nil
	}, handlerConfig())

	rec := postBatch(t, e, batchBody(t, invalid, mismatch), testCredential)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Zero(t, calls.Load())
}

func TestInFlightShed(t *testing.T) {
	cfg := handlerConfig()
	cfg.Ingest.MaxInFlight = 1
	entered := make(chan struct{})
	release := make(chan struct{})
	publisher := publisherFunc(func(ctx context.Context, _ []*contracts.Event, _ string) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	e := newHandlerEcho(t, apikeys.Principal{
		TenantID: "demo", Scopes: []string{apikeys.ScopeWrite},
	}, nil, publisher, cfg)
	body := batchBody(t, validEvent(nil))
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- postBatch(t, e, body, testCredential) }()
	<-entered

	started := time.Now()
	second := postBatch(t, e, body, testCredential)
	require.Equal(t, http.StatusServiceUnavailable, second.Code)
	require.Less(t, time.Since(started), time.Second)
	require.Equal(t, "1", second.Header().Get(echo.HeaderRetryAfter))

	close(release)
	require.Equal(t, http.StatusAccepted, (<-firstDone).Code)
}

func TestPostBatchPublishFailureIsSanitized(t *testing.T) {
	e := newHandlerEcho(t, apikeys.Principal{
		TenantID: "demo", Scopes: []string{apikeys.ScopeWrite},
	}, nil, func(context.Context, []*contracts.Event, string) error {
		return errors.New("broker leaked " + testCredential)
	}, handlerConfig())

	rec := postBatch(t, e, batchBody(t, validEvent(nil)), testCredential)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, "1", rec.Header().Get(echo.HeaderRetryAfter))
	require.NotContains(t, rec.Body.String(), testCredential)
}
