package alertstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/query"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

const (
	streamClients      = 100
	streamLifetime     = 10 * time.Minute
	streamWriteTimeout = 5 * time.Second
	streamHeartbeat    = 15 * time.Second
	streamHintQueue    = 32
)

// Handler independently replays the durable log for each admitted client.
// Notifications are bounded hints; unconditional polling owns recovery.
type Handler struct {
	store       *Store
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	closed      bool
	clients     chan struct{}
	active      sync.WaitGroup
	settings    config.StreamConfig
	subscribers map[chan struct{}]context.CancelFunc
	sse         *telemetry.SSEMetrics
}

// NewHandler binds streams to the service lifetime and existing delivery store.
func NewHandler(ctx context.Context, store *Store, options ...config.StreamConfig) *Handler {
	lifetime, cancel := context.WithCancel(ctx)
	settings := config.StreamConfig{Clients: streamClients, Queue: streamHintQueue,
		WriteTimeout: streamWriteTimeout, Lifetime: streamLifetime}
	if len(options) > 0 {
		settings = options[0]
	}
	// Direct constructors receive the same hard safety caps as config.Load.
	if settings.Clients <= 0 || settings.Clients > streamClients {
		settings.Clients = streamClients
	}
	if settings.Queue <= 0 || settings.Queue > streamHintQueue {
		settings.Queue = streamHintQueue
	}
	if settings.WriteTimeout <= 0 || settings.WriteTimeout > streamWriteTimeout {
		settings.WriteTimeout = streamWriteTimeout
	}
	if settings.Lifetime <= 0 || settings.Lifetime > streamLifetime {
		settings.Lifetime = streamLifetime
	}
	return &Handler{store: store, ctx: lifetime, cancel: cancel, clients: make(chan struct{}, settings.Clients),
		settings: settings, subscribers: make(map[chan struct{}]context.CancelFunc)}
}

// WithMetrics counts admitted streams in sentinel_sse_clients. Call it before
// Register; nil disables counting. It observes only and changes no limit.
func (h *Handler) WithMetrics(sse *telemetry.SSEMetrics) *Handler {
	h.sse = sse
	return h
}

func (h *Handler) subscribe(cancel context.CancelFunc) (<-chan struct{}, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	hints := make(chan struct{}, h.settings.Queue)
	if h.closed || len(h.subscribers) >= h.settings.Clients {
		cancel()
		return hints, func() {}
	}
	h.subscribers[hints] = cancel
	return hints, func() { h.mu.Lock(); delete(h.subscribers, hints); h.mu.Unlock() }
}

func (h *Handler) notify() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for hints, cancel := range h.subscribers {
		select {
		case hints <- struct{}{}:
		default:
			delete(h.subscribers, hints)
			cancel()
		}
	}
}

// Register installs the literal authenticated route before alert-ID routes.
func (h *Handler) Register(e *echo.Echo, verifier *auth.Verifier) {
	e.GET("/api/v1/alerts/stream", h.stream, verifier.Middleware)
}

// Close cancels and joins admitted handlers before HTTP shutdown/pool closure.
func (h *Handler) Close(ctx context.Context) error {
	h.mu.Lock()
	h.closed = true
	h.cancel()
	for hints, cancel := range h.subscribers {
		delete(h.subscribers, hints)
		cancel()
	}
	h.mu.Unlock()
	done := make(chan struct{})
	go func() { h.active.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Handler) admit() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	select {
	case h.clients <- struct{}{}:
		h.active.Add(1)
		h.sse.Inc()
		return true
	default:
		return false
	}
}

func streamCursor(c echo.Context) (int64, error) {
	if c.Request().URL.RawQuery != "" {
		return 0, query.ErrInvalidQuery
	}
	values := c.Request().Header.Values("Last-Event-ID")
	if len(values) == 0 {
		return 0, nil
	}
	if len(values) != 1 || len(values[0]) > 19 {
		return 0, query.ErrInvalidQuery
	}
	after, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || after < 0 || strconv.FormatInt(after, 10) != values[0] {
		return 0, query.ErrInvalidQuery
	}
	return after, nil
}

func (h *Handler) stream(c echo.Context) error {
	p, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	if !p.ExpiresAt.After(time.Now()) {
		return errs.NewUnauthorizedError(http.StatusText(http.StatusUnauthorized), false)
	}
	after, err := streamCursor(c)
	if err != nil {
		return errs.NewBadRequestError("Invalid delivery cursor", false, nil, nil, nil)
	}
	if !h.admit() {
		return errs.NewTooManyRequestsError(http.StatusText(http.StatusTooManyRequests), 1)
	}
	// Dec before Done: once Close has joined every stream the gauge reads 0.
	defer func() { <-h.clients; h.sse.Dec(); h.active.Done() }()
	deadline := h.deadline(time.Now(), p.ExpiresAt)
	ctx, cancel := context.WithDeadline(c.Request().Context(), deadline)
	defer cancel()
	stop := context.AfterFunc(h.ctx, cancel)
	defer stop()
	hints, unsubscribe := h.subscribe(cancel)
	defer unsubscribe()
	page, err := h.store.Read(ctx, p.TenantID, after, maxPageLimit)
	if errors.Is(err, query.ErrInvalidQuery) {
		return errs.NewBadRequestError("Invalid delivery cursor", false, nil, nil, nil)
	}
	if err != nil {
		return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
	}
	return h.replay(ctx, c, p.TenantID, page, hints)
}

func (h *Handler) replay(ctx context.Context, c echo.Context, tenant string,
	page BackfillPage, hints <-chan struct{},
) error {
	writer := c.Response().Writer
	controller := http.NewResponseController(writer)
	// Replace the inherited ordinary HTTP deadline only for this response.
	if err := controller.SetWriteDeadline(time.Now().Add(h.settings.WriteTimeout)); err != nil {
		return err
	}
	header := c.Response().Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	if !writeStream(ctx, controller, writer, []byte(": connected\n\n"), h.settings.WriteTimeout) {
		return nil
	}
	poll := time.NewTicker(time.Second)
	heartbeat := time.NewTicker(streamHeartbeat)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		if !writeDeliveries(ctx, controller, writer, page.Items, h.settings.WriteTimeout) {
			return nil
		}
		after, err := strconv.ParseInt(page.NextSeq, 10, 64)
		if err != nil {
			return nil
		}
		if !page.HasMore {
			select {
			case <-ctx.Done():
				return nil
			case <-poll.C:
			case <-hints:
			case <-heartbeat.C:
				if !writeStream(ctx, controller, writer, []byte(": heartbeat\n\n"), h.settings.WriteTimeout) {
					return nil
				}
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		page, err = h.store.Read(ctx, tenant, after, maxPageLimit)
		if err != nil {
			return nil
		}
	}
}

func writeDeliveries(ctx context.Context, controller *http.ResponseController,
	writer http.ResponseWriter, items []Delivery, budget time.Duration,
) bool {
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			return false
		}
		frame := []byte("id: " + item.Seq + "\nevent: alert\ndata: " + string(data) + "\n\n")
		if !writeStream(ctx, controller, writer, frame, budget) {
			return false
		}
	}
	return true
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (h *Handler) deadline(now, expires time.Time) time.Time {
	return minTime(now.Add(h.settings.Lifetime), expires)
}

func writeStream(ctx context.Context, controller *http.ResponseController,
	writer http.ResponseWriter, frame []byte, budget time.Duration,
) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline := time.Now().Add(budget)
	if expires, ok := ctx.Deadline(); ok && expires.Before(deadline) {
		deadline = expires
	}
	if controller.SetWriteDeadline(deadline) != nil {
		return false
	}
	if _, err := writer.Write(frame); err != nil {
		return false
	}
	if controller.Flush() != nil {
		return false
	}
	// Idle periods exceed the finite write budget. Cancellation/expiry and the
	// heartbeat own idle lifetime; every subsequent write gets a new deadline.
	return controller.SetWriteDeadline(time.Time{}) == nil
}
