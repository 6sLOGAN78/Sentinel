// Package query serves bounded, authenticated dashboard queries.
package query

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/errs"
)

// ErrInvalidQuery never exposes cursor contents or database errors.
var ErrInvalidQuery = errors.New("invalid dashboard query")

const (
	defaultPageLimit = 50
	maxCursorBytes   = 2048
	detailTimeout    = 5 * time.Second
)

var (
	utcTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3})?Z$`)
	ruleID       = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	alertID      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	tenantID     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
)

// Page is finite; its cursor is not a delivery sequence.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// ListOptions has a half-open UTC range and bounded page size.
type ListOptions struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Limit  int       `json:"limit"`
	Cursor string    `json:"-"`
}

// AlertFilters applies entirely before storage ordering and limiting.
type AlertFilters struct {
	ListOptions
	Severity string `json:"severity"`
	Status   string `json:"status"`
	Rule     string `json:"rule"`
	Search   string `json:"search"`
}

// WorkflowAlert contains only the fifteen public PG003 workflow columns.
type WorkflowAlert struct {
	AlertID     string    `json:"alert_id"`
	TenantID    string    `json:"tenant_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	RuleID      string    `json:"rule_id"`
	RuleVersion int64     `json:"rule_version"`
	Severity    string    `json:"severity"`
	EntityKey   string    `json:"entity_key"`
	Summary     string    `json:"summary"`
	EventCount  int64     `json:"event_count"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Status      string    `json:"status"`
	Assignee    *string   `json:"assignee"`
	Version     int64     `json:"version"`
}

// Position uses exactly the same millisecond tuple as the PG query.
type Position struct {
	CreatedAt time.Time `json:"created"`
	AlertID   string    `json:"id"`
}

// AlertLister returns at most Limit+1 tenant rows, already filtered.
type AlertLister interface {
	ListAlerts(context.Context, string, AlertFilters, *Position) ([]WorkflowAlert, error)
}

// Dependencies are real adapters; query does not import stores.
type Dependencies struct {
	Alerts    AlertLister
	Workflows WorkflowReader
	Details   AlertDetailReader
	Auth      *auth.Verifier
	Cursors   *CursorCodec
}

// Handler owns dashboard route dependencies.
type Handler struct{ deps Dependencies }

// NewHandler constructs the focused dashboard handler.
func NewHandler(deps Dependencies) *Handler { return &Handler{deps: deps} }

// Register registers each delivered route once.
func (h *Handler) Register(e *echo.Echo) {
	e.GET("/api/v1/alerts", h.ListAlerts, h.deps.Auth.Middleware)
	e.GET("/api/v1/alerts/:alert_id", h.GetAlert, h.deps.Auth.Middleware)
}

// GetAlert establishes PG ownership before any CH read; neither orphans nor
// URL parameters can grant authority. One deadline bounds the whole detail.
func (h *Handler) GetAlert(c echo.Context) error {
	p, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	id := c.Param("alert_id")
	if !alertID.MatchString(id) || c.Request().URL.RawQuery != "" {
		return invalidQuery()
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), detailTimeout)
	defer cancel()
	workflow, err := h.deps.Workflows.GetWorkflow(ctx, p.TenantID, id)
	if err != nil {
		return detailError(err)
	}
	detection, err := h.deps.Details.Detection(ctx, p.TenantID, id)
	if err != nil {
		return detailError(err)
	}
	evidence, missing, err := h.deps.Details.Evidence(ctx, p.TenantID, detection.SampleEventIDs)
	if err != nil {
		return detailError(err)
	}
	explanation, err := h.deps.Workflows.RuleExplanation(ctx, p.TenantID, id, detection.RuleID, detection.RuleVersion)
	if err != nil {
		return detailError(err)
	}
	return c.JSON(http.StatusOK, AlertDetail{Workflow: *workflow, Detection: *detection,
		Evidence: evidence, MissingEvidenceIDs: missing, RuleExplanation: explanation,
		History: []StatusChange{}})
}

func detailError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return errs.NewNotFoundError("Alert not found", false, nil)
	}
	return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
}

// ListAlerts verifies identity before parsing and querying its tenant.
func (h *Handler) ListAlerts(c echo.Context) error {
	p, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	values, err := url.ParseQuery(c.Request().URL.RawQuery)
	if err != nil {
		return invalidQuery()
	}
	f, err := parseAlertFilters(values)
	if err != nil {
		return invalidQuery()
	}
	var after *Position
	if f.Cursor != "" {
		var position Position
		f, position, err = h.deps.Cursors.Decode(f.Cursor, "alerts", p.TenantID, f)
		after = &position
	} else {
		err = normalizeAlertFilters(&f, time.Now().UTC().Truncate(time.Millisecond))
	}
	if err != nil {
		return invalidQuery()
	}
	items, err := h.deps.Alerts.ListAlerts(c.Request().Context(), p.TenantID, f, after)
	if err != nil {
		return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
	}
	page := Page[WorkflowAlert]{Items: items}
	if len(items) > f.Limit {
		page.HasMore, page.Items = true, items[:f.Limit]
		last := page.Items[f.Limit-1]
		next, encodeErr := h.deps.Cursors.Encode("alerts", p.TenantID, f, Position{
			CreatedAt: last.CreatedAt, AlertID: last.AlertID,
		})
		if encodeErr != nil {
			return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
		}
		page.NextCursor = &next
	}
	return c.JSON(http.StatusOK, page)
}
func invalidQuery() error {
	return errs.NewBadRequestError("Invalid alert query", false, nil, nil, nil)
}

func parseAlertFilters(values url.Values) (AlertFilters, error) {
	f := AlertFilters{ListOptions: ListOptions{Limit: defaultPageLimit}}
	for key, fields := range values {
		if len(fields) != 1 || fields[0] == "" {
			return f, ErrInvalidQuery
		}
		value := fields[0]
		switch key {
		case "from", "to":
			parsed, err := parseQueryTime(value)
			if err != nil {
				return f, ErrInvalidQuery
			}
			if key == "from" {
				f.From = parsed
			} else {
				f.To = parsed
			}
		case "limit":
			limit, err := strconv.Atoi(value)
			if err != nil || limit < 1 || limit > 200 {
				return f, ErrInvalidQuery
			}
			f.Limit = limit
		case "cursor":
			f.Cursor = value
		case "status":
			f.Status = value
		case "severity":
			f.Severity = value
		case "rule_id":
			f.Rule = value
		case "search":
			f.Search = value
		default:
			return f, ErrInvalidQuery
		}
	}
	if len(f.Cursor) > maxCursorBytes || !validAlertFilterFields(f) {
		return f, ErrInvalidQuery
	}
	// Validate the original bytes before normalization can remove controls.
	f.Search = strings.TrimSpace(f.Search)
	return f, nil
}

func parseQueryTime(value string) (time.Time, error) {
	if !utcTimestamp.MatchString(value) {
		return time.Time{}, ErrInvalidQuery
	}
	return time.Parse(time.RFC3339Nano, value)
}
func validAlertFilterFields(f AlertFilters) bool {
	return f.Limit >= 1 && f.Limit <= 200 &&
		(f.Status == "" || f.Status == "open" || f.Status == "acknowledged" || f.Status == "resolved") &&
		(f.Severity == "" || f.Severity == "low" || f.Severity == "medium" ||
			f.Severity == "high" || f.Severity == "critical") &&
		(f.Rule == "" || ruleID.MatchString(f.Rule)) && validSearch(f.Search)
}
func validSearch(search string) bool {
	return len(search) <= 128 && utf8.ValidString(search) && !strings.ContainsFunc(search, unicode.IsControl)
}
func normalizeAlertFilters(f *AlertFilters, now time.Time) error {
	if !validAlertFilterFields(*f) {
		return ErrInvalidQuery
	}
	if f.To.IsZero() {
		f.To = now
	}
	if f.From.IsZero() {
		f.From = f.To.Add(-24 * time.Hour)
	}
	if f.From.IsZero() || !f.To.After(f.From) || f.To.Sub(f.From) > 7*24*time.Hour ||
		!f.From.Equal(f.From.Truncate(time.Millisecond)) || !f.To.Equal(f.To.Truncate(time.Millisecond)) {
		return ErrInvalidQuery
	}
	f.From, f.To, f.Cursor = f.From.UTC(), f.To.UTC(), ""
	return nil
}
