// Package ingest implements the authenticated, bounded public event intake.
package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

const apiKeyHeader = "X-Api-Key" //nolint:gosec // Public HTTP header name, not a credential.

const maxErrorDetails = 50

type verifier interface {
	Authenticate(context.Context, string) (apikeys.Principal, error)
}

type limiter interface {
	Allow(context.Context, string, int) (bool, int, error)
}

// Handler owns intake dependencies and the before-auth concurrency gate.
type Handler struct {
	verifier  verifier
	publisher kafkax.Publisher
	limiter   limiter
	cfg       *config.Config
	inFlight  chan struct{}
	metrics   *telemetry.IngestMetrics
}

// NewHandler constructs a handler whose semaphore exists for the service lifetime.
func NewHandler(
	v verifier, publisher kafkax.Publisher, rateLimiter limiter, cfg *config.Config, _ zerolog.Logger,
) *Handler {
	return &Handler{
		verifier: v, publisher: publisher, limiter: rateLimiter, cfg: cfg,
		inFlight: make(chan struct{}, cfg.Ingest.MaxInFlight),
	}
}

// WithMetrics attaches ingest metrics. A nil value disables recording.
func (h *Handler) WithMetrics(m *telemetry.IngestMetrics) *Handler {
	h.metrics = m

	return h
}

// Register installs only the literal-colon v1 batch route.
func (h *Handler) Register(e *echo.Echo) {
	e.POST("/api/v1/events\\:batch", h.PostBatch, h.recordRequest)
}

// recordRequest observes the final client-visible status and duration.
func (h *Handler) recordRequest(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		start := time.Now()
		err := next(c)
		h.metrics.Request(middleware.ResponseStatus(err, c), time.Since(start))

		return err
	}
}

type batchEnvelope struct {
	Events []json.RawMessage `json:"events"`
}

type acceptedResponse struct {
	AcceptedCount int      `json:"accepted_count"`
	RejectedCount int      `json:"rejected_count"`
	EventIDs      []string `json:"event_ids"`
	RequestID     string   `json:"request_id"`
}

// PostBatch authenticates and validates the complete batch before publishing.
func (h *Handler) PostBatch(c echo.Context) error {
	select {
	case h.inFlight <- struct{}{}:
		defer func() { <-h.inFlight }()
	default:
		// The body is unread, so the batch size is unknown: create the series
		// without inventing a count; requests_total{status="503"} carries the rate.
		return h.fail(telemetry.ResultShed, 0, unavailableError())
	}

	principal, err := h.verifier.Authenticate(c.Request().Context(), c.Request().Header.Get(apiKeyHeader))
	if err != nil {
		if errors.Is(err, apikeys.ErrUnavailable) {
			auth.Audit(c, "producer_auth", "unavailable", "auth_unavailable", auth.Principal{})
			return h.fail(telemetry.ResultUnavailable, 0, unavailableError())
		}
		auth.Audit(c, "producer_auth", "denied", "invalid_credential", auth.Principal{})
		return errs.NewUnauthorizedError(http.StatusText(http.StatusUnauthorized), false)
	}
	identity := auth.Principal{Subject: principal.KeyID, TenantID: principal.TenantID}
	if !principal.HasScope(apikeys.ScopeWrite) {
		auth.Audit(c, "producer_auth", "denied", "scope_forbidden", identity)
		return errs.NewForbiddenError(http.StatusText(http.StatusForbidden), false)
	}

	envelope, err := h.decodeEnvelope(c)
	if err != nil {
		return h.fail(telemetry.ResultRejected, 0, err)
	}
	allowed, retryAfter, err := h.limiter.Allow(
		c.Request().Context(), principal.KeyID, len(envelope.Events),
	)
	if err != nil {
		return h.fail(telemetry.ResultUnavailable, len(envelope.Events), unavailableError())
	}
	if !allowed {
		return h.fail(telemetry.ResultRateLimited, len(envelope.Events),
			errs.NewTooManyRequestsError(http.StatusText(http.StatusTooManyRequests), retryAfter))
	}

	normalized, details, tenantMismatch := normalizeBatch(envelope.Events, principal.TenantID)
	if tenantMismatch {
		auth.Audit(c, "producer_auth", "denied", "tenant_forbidden", identity)
		return errs.NewForbiddenError(http.StatusText(http.StatusForbidden), false)
	}

	events, eventIDs, details := h.decodeEvents(normalized, details, len(envelope.Events))
	if len(details) > 0 {
		return h.fail(telemetry.ResultRejected, len(envelope.Events),
			errs.NewBadRequestError("Invalid event batch", false, nil, details, nil))
	}

	requestID := middleware.GetRequestID(c)
	if err = h.publisher.PublishBatch(c.Request().Context(), events, requestID); err != nil {
		return h.fail(telemetry.ResultUnavailable, len(events), unavailableError())
	}
	h.metrics.Events(telemetry.ResultAccepted, len(events))

	return c.JSON(http.StatusAccepted, acceptedResponse{
		AcceptedCount: len(events), RejectedCount: 0, EventIDs: eventIDs, RequestID: requestID,
	})
}

// decodeEvents canonically decodes and validates every normalized event,
// stamping the server-owned received_at, and appends per-event details.
func (h *Handler) decodeEvents(
	normalized [][]byte, details []errs.FieldError, size int,
) ([]*contracts.Event, []string, []errs.FieldError) {
	events := make([]*contracts.Event, 0, size)
	eventIDs := make([]string, 0, size)
	now := time.Now().UTC()
	receivedAt := contracts.NewTimestamp(now)
	limits := contracts.LimitsFromConfig(h.cfg.Contracts)
	for index, raw := range normalized {
		if raw == nil {
			continue
		}

		event, decodeErr := contracts.DecodeEvent(bytes.NewReader(raw))
		if decodeErr != nil {
			details = appendDetail(details, index, decodeErr)

			continue
		}
		if validationErr := event.Validate(now, limits); validationErr != nil {
			details = appendDetail(details, index, validationErr)

			continue
		}
		event.ReceivedAt = &receivedAt
		events = append(events, event)
		eventIDs = append(eventIDs, event.EventID)
	}

	return events, eventIDs, details
}

// fail records the outcome of a rejected request and returns err unchanged.
func (h *Handler) fail(result string, events int, err error) error {
	h.metrics.Events(result, events)

	return err
}

func unavailableError() error {
	return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
}

func (h *Handler) decodeEnvelope(c echo.Context) (*batchEnvelope, error) {
	body := http.MaxBytesReader(c.Response(), c.Request().Body, h.cfg.Ingest.MaxBodyBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errs.NewPayloadTooLargeError(http.StatusText(http.StatusRequestEntityTooLarge), false)
		}

		return nil, errs.NewBadRequestError("Invalid event batch", false, nil, nil, nil)
	}

	envelope, err := strictEnvelope(raw)
	if err != nil {
		return nil, errs.NewBadRequestError("Invalid event batch", false, nil, nil, nil)
	}
	if len(envelope.Events) == 0 || len(envelope.Events) > h.cfg.Ingest.MaxBatchSize {
		if len(envelope.Events) > h.cfg.Ingest.MaxBatchSize {
			return nil, errs.NewPayloadTooLargeError(http.StatusText(http.StatusRequestEntityTooLarge), false)
		}

		return nil, errs.NewBadRequestError("Invalid event batch", false, nil, nil, nil)
	}

	return envelope, nil
}

func strictEnvelope(raw []byte) (*batchEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("batch must be an object")
	}

	var envelope batchEnvelope
	seenEvents := false
	for decoder.More() {
		key, keyErr := decoder.Token()
		if keyErr != nil {
			return nil, errors.New("invalid batch object")
		}
		name, ok := key.(string)
		if !ok || name != "events" || seenEvents {
			return nil, errors.New("invalid batch field")
		}
		seenEvents = true
		if err = decoder.Decode(&envelope.Events); err != nil {
			return nil, errors.New("invalid events array")
		}
	}
	if _, err = decoder.Token(); err != nil {
		return nil, errors.New("unterminated batch object")
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing batch data")
	}
	if !seenEvents || envelope.Events == nil {
		return nil, errors.New("missing events array")
	}

	return &envelope, nil
}

type eventDecodeError struct {
	field   string
	message string
}

func (e *eventDecodeError) Error() string { return e.message }

func normalizeBatch(rawEvents []json.RawMessage, expectedTenant string) ([][]byte, []errs.FieldError, bool) {
	normalized := make([][]byte, len(rawEvents))
	details := make([]errs.FieldError, 0, min(len(rawEvents), maxErrorDetails))
	for index, raw := range rawEvents {
		clean, tenant, tenantKnown, normalizeErr := normalizeEvent(raw)
		normalized[index] = clean
		if tenantKnown && tenant != expectedTenant {
			return normalized, details, true
		}
		if normalizeErr != nil {
			details = appendDetail(details, index, normalizeErr)
		}
	}

	return normalized, details, false
}

type normalizedEventBuilder struct {
	seen            map[string]struct{}
	out             bytes.Buffer
	first           bool
	tenant          string
	tenantKnown     bool
	tenantAmbiguous bool
	firstErr        error
}

func newNormalizedEventBuilder() *normalizedEventBuilder {
	b := &normalizedEventBuilder{seen: make(map[string]struct{}), first: true}
	b.out.WriteByte('{')

	return b
}

func (b *normalizedEventBuilder) add(key string, value json.RawMessage) {
	if _, duplicate := b.seen[key]; duplicate {
		if key == "tenant_id" {
			b.tenantAmbiguous = true
		}
		if b.firstErr == nil {
			b.firstErr = &eventDecodeError{field: key, message: "duplicate field"}
		}
	}
	b.seen[key] = struct{}{}

	if key == "tenant_id" && !b.tenantAmbiguous && json.Unmarshal(value, &b.tenant) == nil {
		b.tenantKnown = true
	}
	if b.firstErr != nil || key == "received_at" {
		return
	}
	if !b.first {
		b.out.WriteByte(',')
	}
	encodedKey, _ := json.Marshal(key)
	b.out.Write(encodedKey)
	b.out.WriteByte(':')
	b.out.Write(value)
	b.first = false
}

// normalizeEvent removes the server-owned received_at value while preserving a
// duplicate-safe object for the canonical decoder. It also extracts tenant_id
// for the batch-wide authorization pass before field validation.
func normalizeEvent(raw []byte) ([]byte, string, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, "", false, &eventDecodeError{field: "$", message: "must be a JSON object"}
	}

	b := newNormalizedEventBuilder()
	for decoder.More() {
		keyToken, keyErr := decoder.Token()
		if keyErr != nil {
			return nil, b.tenant, b.tenantKnown, &eventDecodeError{field: "$", message: "invalid JSON object"}
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, b.tenant, b.tenantKnown, &eventDecodeError{field: "$", message: "invalid object key"}
		}

		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, b.tenant, b.tenantKnown, &eventDecodeError{field: key, message: "invalid JSON value"}
		}
		b.add(key, value)
	}
	if _, err = decoder.Token(); err != nil {
		return nil, b.tenant, b.tenantKnown, &eventDecodeError{field: "$", message: "unterminated JSON object"}
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, b.tenant, b.tenantKnown, &eventDecodeError{field: "$", message: "trailing JSON data"}
	}
	b.out.WriteByte('}')
	if b.firstErr != nil {
		return nil, b.tenant, b.tenantKnown && !b.tenantAmbiguous, b.firstErr
	}

	return b.out.Bytes(), b.tenant, b.tenantKnown, nil
}

func appendDetail(details []errs.FieldError, index int, err error) []errs.FieldError {
	if len(details) >= maxErrorDetails {
		return details
	}

	field := "$"
	message := "invalid event"
	var contractErr *contracts.FieldError
	var decodeErr *eventDecodeError
	switch {
	case errors.As(err, &contractErr):
		field = contractErr.Field
		message = contractErr.Message
	case errors.As(err, &decodeErr):
		field = decodeErr.field
		message = decodeErr.message
	}

	return append(details, errs.FieldError{
		Field: "events[" + strconv.Itoa(index) + "]." + field,
		Error: message,
	})
}
