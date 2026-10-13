// Package eventquery serves tenant-scoped event lookup requests.
package eventquery

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/errs"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

const apiKeyHeader = "X-Api-Key" //nolint:gosec // Public HTTP header name, not a credential.

type verifier interface {
	Authenticate(context.Context, string) (apikeys.Principal, error)
}

type eventStore interface {
	Get(context.Context, string, string) (*contracts.Event, error)
}

// Handler owns authenticated event lookup dependencies.
type Handler struct {
	verifier verifier
	store    eventStore
}

// NewHandler constructs a tenant-scoped lookup handler.
func NewHandler(v verifier, store eventStore) *Handler { return &Handler{verifier: v, store: store} }

// Register installs only the single-event route delivered in this phase.
func (h *Handler) Register(e *echo.Echo) { e.GET("/api/v1/events/:event_id", h.GetEvent) }

// GetEvent authenticates a read key and binds its tenant to the storage query.
func (h *Handler) GetEvent(c echo.Context) error {
	principal, err := h.verifier.Authenticate(c.Request().Context(), c.Request().Header.Get(apiKeyHeader))
	if err != nil {
		if errors.Is(err, apikeys.ErrUnavailable) {
			return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
		}

		return errs.NewUnauthorizedError(http.StatusText(http.StatusUnauthorized), false)
	}
	if !principal.HasScope(apikeys.ScopeRead) {
		return errs.NewForbiddenError(http.StatusText(http.StatusForbidden), false)
	}

	eventID := c.Param("event_id")
	parsed, err := uuid.Parse(eventID)
	if err != nil || len(eventID) != 36 || parsed.String() != strings.ToLower(eventID) {
		return errs.NewBadRequestError("Invalid event ID", false, nil, nil, nil)
	}

	event, err := h.store.Get(c.Request().Context(), principal.TenantID, eventID)
	if errors.Is(err, storageclickhouse.ErrNotFound) {
		return errs.NewNotFoundError("Event not found", false, nil)
	}
	if err != nil {
		return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
	}

	return c.JSON(http.StatusOK, event)
}
