// Package ingest implements the authenticated, bounded public event intake.
package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
)

const apiKeyHeader = "X-Api-Key" //nolint:gosec // Public HTTP header name, not a credential.

type verifier interface {
	Authenticate(context.Context, string) (apikeys.Principal, error)
}

// Handler owns intake dependencies and the before-auth concurrency gate.
type Handler struct {
	verifier  verifier
	publisher kafkax.Publisher
	cfg       *config.Config
	inFlight  chan struct{}
}

// NewHandler constructs a handler whose semaphore exists for the service lifetime.
func NewHandler(v verifier, publisher kafkax.Publisher, cfg *config.Config, _ zerolog.Logger) *Handler {
	return &Handler{
		verifier: v, publisher: publisher, cfg: cfg,
		inFlight: make(chan struct{}, cfg.Ingest.MaxInFlight),
	}
}

// Register installs only the literal-colon v1 batch route.
func (h *Handler) Register(e *echo.Echo) {
	e.POST("/api/v1/events\\:batch", h.PostBatch)
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
		return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
	}

	principal, err := h.verifier.Authenticate(c.Request().Context(), c.Request().Header.Get(apiKeyHeader))
	if err != nil {
		if errors.Is(err, apikeys.ErrUnavailable) {
			return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
		}

		return errs.NewUnauthorizedError(http.StatusText(http.StatusUnauthorized), false)
	}
	if !principal.HasScope(apikeys.ScopeWrite) {
		return errs.NewForbiddenError(http.StatusText(http.StatusForbidden), false)
	}

	envelope, err := h.decodeEnvelope(c)
	if err != nil {
		return err
	}

	events := make([]*contracts.Event, 0, len(envelope.Events))
	eventIDs := make([]string, 0, len(envelope.Events))
	now := time.Now().UTC()
	receivedAt := contracts.NewTimestamp(now)
	limits := contracts.LimitsFromConfig(h.cfg.Contracts)
	for _, raw := range envelope.Events {
		event, decodeErr := contracts.DecodeEvent(bytes.NewReader(raw))
		if decodeErr != nil || event.Validate(now, limits) != nil {
			return errs.NewBadRequestError("Invalid event batch", false, nil, nil, nil)
		}
		if event.TenantID != principal.TenantID {
			return errs.NewForbiddenError(http.StatusText(http.StatusForbidden), false)
		}
		event.ReceivedAt = &receivedAt
		events = append(events, event)
		eventIDs = append(eventIDs, event.EventID)
	}

	requestID := middleware.GetRequestID(c)
	if err = h.publisher.PublishBatch(c.Request().Context(), events, requestID); err != nil {
		return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
	}

	return c.JSON(http.StatusAccepted, acceptedResponse{
		AcceptedCount: len(events), RejectedCount: 0, EventIDs: eventIDs, RequestID: requestID,
	})
}

func (h *Handler) decodeEnvelope(c echo.Context) (*batchEnvelope, error) {
	body := http.MaxBytesReader(c.Response(), c.Request().Body, h.cfg.Ingest.MaxBodyBytes)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	var envelope batchEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errs.NewPayloadTooLargeError(http.StatusText(http.StatusRequestEntityTooLarge), false)
		}

		return nil, errs.NewBadRequestError("Invalid event batch", false, nil, nil, nil)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, errs.NewPayloadTooLargeError(http.StatusText(http.StatusRequestEntityTooLarge), false)
		}

		return nil, errs.NewBadRequestError("Invalid event batch", false, nil, nil, nil)
	}
	if len(envelope.Events) == 0 || len(envelope.Events) > h.cfg.Ingest.MaxBatchSize {
		return nil, errs.NewBadRequestError("Invalid event batch", false, nil, nil, nil)
	}

	return &envelope, nil
}
