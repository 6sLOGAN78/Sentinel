package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/errs"
)

// ErrConflict covers stale versions and illegal forward transitions.
var ErrConflict = errors.New("alert transition conflict")

const maxTransitionBytes = 4096

// TriageStore commits workflow and audit together, independently of CH.
type TriageStore interface {
	Transition(context.Context, string, string, string, int64, string) (WorkflowAlert, error)
	History(context.Context, string, string) ([]StatusChange, error)
}

// PatchAlert takes identity only from verified middleware, never request JSON.
func (h *Handler) PatchAlert(c echo.Context) error {
	p, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	if auth.RequireTriage(p) != nil {
		return errs.NewForbiddenError(http.StatusText(http.StatusForbidden), false)
	}
	id := c.Param("alert_id")
	if !alertID.MatchString(id) || c.Request().URL.RawQuery != "" {
		return invalidQuery()
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request().Body, maxTransitionBytes+1))
	if len(raw) > maxTransitionBytes {
		return errs.NewPayloadTooLargeError("Alert update is too large", false)
	}
	if err != nil {
		return invalidQuery()
	}
	status, version, err := decodeTransition(raw)
	if err != nil {
		return invalidQuery()
	}
	workflow, err := h.deps.Triage.Transition(c.Request().Context(), p.TenantID, id, p.Subject, version, status)
	if errors.Is(err, ErrConflict) {
		return errs.NewConflictError("Alert changed or transition is not allowed", false, nil)
	}
	if err != nil {
		return detailError(err)
	}
	return c.JSON(http.StatusOK, workflow)
}

func decodeTransition(raw []byte) (string, int64, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return "", 0, ErrInvalidQuery
	}
	seen := map[string]bool{}
	var status string
	var version int64
	for dec.More() {
		key, tokenErr := dec.Token()
		if tokenErr != nil {
			return "", 0, ErrInvalidQuery
		}
		field, ok := key.(string)
		if !ok || seen[field] {
			return "", 0, ErrInvalidQuery
		}
		seen[field] = true
		switch field {
		case "status":
			err = dec.Decode(&status)
		case "version":
			err = dec.Decode(&version)
		default:
			return "", 0, ErrInvalidQuery
		}
		if err != nil {
			return "", 0, ErrInvalidQuery
		}
	}
	if _, err = dec.Token(); err != nil {
		return "", 0, ErrInvalidQuery
	}
	var extra any
	if dec.Decode(&extra) != io.EOF || len(seen) != 2 || version < 1 ||
		(status != "acknowledged" && status != "resolved") {
		return "", 0, ErrInvalidQuery
	}
	return status, version, nil
}
