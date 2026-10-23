package query

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/errs"
)

const (
	eventCursorDomain  = "sentinel/dashboard/event-page/v1"
	eventEndpoint      = "events"
	eventFromParameter = "from"
)

// EventFilters is a normalized finite query; After is an authenticated tuple.
type EventFilters struct {
	ListOptions
	SourceIP  string         `json:"source_ip"`
	EventType string         `json:"event_type"`
	Outcome   string         `json:"outcome"`
	After     *EventPosition `json:"-"`
}

// EventPosition follows the native millisecond (occurred_at,event_id) ordering.
type EventPosition struct {
	OccurredAt time.Time `json:"occurred_at"`
	EventID    string    `json:"event_id"`
}

type eventCursorPayload struct {
	Version  int           `json:"v"`
	Endpoint string        `json:"endpoint"`
	Tenant   string        `json:"tenant"`
	Filters  EventFilters  `json:"filters"`
	Position EventPosition `json:"after"`
}

// EncodeEvents reuses the codec's AEAD with disjoint authenticated event AAD.
// Alert payloads and event payloads can never be interpreted as one another.
func (c *CursorCodec) EncodeEvents(tenant string, f EventFilters, after EventPosition) (string, error) {
	f.Cursor, f.After = "", nil
	if !validEventCursor(tenant, f, after) {
		return "", ErrInvalidQuery
	}
	raw, err := json.Marshal(eventCursorPayload{Version: 1, Endpoint: eventEndpoint,
		Tenant: tenant, Filters: f, Position: after})
	if err != nil {
		return "", ErrInvalidQuery
	}
	// NewGCMWithRandomNonce generates its own nonce.
	sealed := c.aead.Seal(nil, nil, raw, []byte(eventCursorDomain)) //nolint:gosec // G407: random nonce AEAD.
	token := base64.RawURLEncoding.EncodeToString(sealed)
	if len(token) > maxCursorBytes {
		return "", ErrInvalidQuery
	}
	return token, nil
}

// DecodeEvents authenticates normalized filters and freezes omitted time bounds.
func (c *CursorCodec) DecodeEvents(token, tenant string, f EventFilters) (EventFilters, error) {
	invalid := func() (EventFilters, error) { return EventFilters{}, ErrInvalidQuery }
	if token == "" || len(token) > maxCursorBytes {
		return invalid()
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return invalid()
	}
	plain, err := c.aead.Open(nil, nil, raw, []byte(eventCursorDomain))
	if err != nil {
		return invalid()
	}
	var payload eventCursorPayload
	if json.Unmarshal(plain, &payload) != nil || payload.Version != 1 ||
		payload.Endpoint != eventEndpoint || payload.Tenant != tenant ||
		!validEventCursor(payload.Tenant, payload.Filters, payload.Position) {
		return invalid()
	}
	if f.From.IsZero() {
		f.From = payload.Filters.From
	}
	if f.To.IsZero() {
		f.To = payload.Filters.To
	}
	if normalizeEventFilters(&f, payload.Filters.To) != nil || f != payload.Filters {
		return invalid()
	}
	f.After = &payload.Position
	return f, nil
}

func validEventCursor(tenant string, f EventFilters, after EventPosition) bool {
	id, err := uuid.Parse(after.EventID)
	return tenantID.MatchString(tenant) && f.Cursor == "" && f.After == nil &&
		!f.From.IsZero() && !f.To.IsZero() && normalizeEventFilters(&f, f.To) == nil &&
		!after.OccurredAt.Before(f.From) && after.OccurredAt.Before(f.To) &&
		after.OccurredAt.Equal(after.OccurredAt.Truncate(time.Millisecond)) && err == nil && id.String() == after.EventID
}

// EventLister returns at most Limit+1 canonical identities, filtered after dedupe.
type EventLister interface {
	ListEvents(context.Context, string, EventFilters) ([]contracts.Event, error)
}

// EventHandler keeps event-list dependencies independent of workflow reads.
type EventHandler struct {
	store   EventLister
	auth    *auth.Verifier
	cursors *CursorCodec
}

// NewEventHandler binds only the authenticated event-search increment.
func NewEventHandler(store EventLister, verifier *auth.Verifier, cursors *CursorCodec) *EventHandler {
	return &EventHandler{store: store, auth: verifier, cursors: cursors}
}

// Register owns exactly one bearer list route; legacy lookup remains separate.
func (h *EventHandler) Register(e *echo.Echo) {
	e.GET("/api/v1/events", h.ListEvents, h.auth.Middleware)
}

// ListEvents derives tenant authority before parsing or storage access.
func (h *EventHandler) ListEvents(c echo.Context) error {
	principal, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	values, err := url.ParseQuery(c.Request().URL.RawQuery)
	if err != nil {
		return invalidEventQuery()
	}
	filters, err := parseEventFilters(values)
	if err != nil {
		return invalidEventQuery()
	}
	if filters.Cursor != "" {
		filters, err = h.cursors.DecodeEvents(filters.Cursor, principal.TenantID, filters)
	} else {
		err = normalizeEventFilters(&filters, time.Now().UTC().Truncate(time.Millisecond))
	}
	if err != nil {
		return invalidEventQuery()
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), detailTimeout)
	defer cancel()
	items, err := h.store.ListEvents(ctx, principal.TenantID, filters)
	if err != nil {
		return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
	}
	page := Page[contracts.Event]{Items: items}
	if len(items) > filters.Limit {
		page.Items, page.HasMore = items[:filters.Limit], true
		last := page.Items[filters.Limit-1]
		next, encodeErr := h.cursors.EncodeEvents(principal.TenantID, filters,
			EventPosition{OccurredAt: last.OccurredAt.Time, EventID: last.EventID})
		if encodeErr != nil {
			return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
		}
		page.NextCursor = &next
	}
	return c.JSON(http.StatusOK, page)
}

func invalidEventQuery() error {
	return errs.NewBadRequestError("Invalid event query", false, nil, nil, nil)
}

func parseEventFilters(values url.Values) (EventFilters, error) {
	f := EventFilters{ListOptions: ListOptions{Limit: defaultPageLimit}}
	for key, fields := range values {
		if len(fields) != 1 || fields[0] == "" {
			return f, ErrInvalidQuery
		}
		if setEventFilter(&f, key, fields[0]) != nil {
			return f, ErrInvalidQuery
		}
	}
	if len(f.Cursor) > maxCursorBytes || !validEventFields(f) {
		return f, ErrInvalidQuery
	}
	return f, nil
}

func setEventFilter(f *EventFilters, key, value string) error {
	switch key {
	case eventFromParameter, "to":
		parsed, err := parseQueryTime(value)
		if err != nil {
			return ErrInvalidQuery
		}
		if key == eventFromParameter {
			f.From = parsed
		} else {
			f.To = parsed
		}
	case "limit":
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 200 || strconv.Itoa(limit) != value {
			return ErrInvalidQuery
		}
		f.Limit = limit
	case "cursor":
		f.Cursor = value
	case "source_ip":
		ip, err := netip.ParseAddr(value)
		if err != nil || ip.Zone() != "" || ip.Is4In6() || len(value) > 45 {
			return ErrInvalidQuery
		}
		f.SourceIP = ip.String()
	case "event_type":
		f.EventType = value
	case "outcome":
		f.Outcome = value
	default:
		return ErrInvalidQuery
	}
	return nil
}

func validEventFields(f EventFilters) bool {
	validType := f.EventType == "" || f.EventType == "auth.success" || f.EventType == "auth.failure" ||
		f.EventType == "net.connection" || f.EventType == "net.request" || f.EventType == "policy.decision"
	validIP := f.SourceIP == ""
	if ip, err := netip.ParseAddr(f.SourceIP); err == nil {
		validIP = ip.Zone() == "" && !ip.Is4In6() && ip.String() == f.SourceIP
	}
	return validType && validIP && f.Limit >= 1 && f.Limit <= 200 &&
		(f.Outcome == "" || f.Outcome == "success" || f.Outcome == "failure")
}

func normalizeEventFilters(f *EventFilters, now time.Time) error {
	if !validEventFields(*f) {
		return ErrInvalidQuery
	}
	end := f.To
	if end.IsZero() {
		end = now
	}
	start := f.From
	if start.IsZero() {
		start = end.Add(-24 * time.Hour)
	}
	duration := end.Sub(start)
	if start.IsZero() || end.IsZero() || duration <= 0 || duration > 7*24*time.Hour ||
		start.Nanosecond()%int(time.Millisecond) != 0 || end.Nanosecond()%int(time.Millisecond) != 0 {
		return ErrInvalidQuery
	}
	f.ListOptions = ListOptions{From: start.UTC(), To: end.UTC(), Limit: f.Limit}
	return nil
}
