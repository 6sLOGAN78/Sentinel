package middleware

import (
	"context"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// Echo context keys set by upstream middleware and read by this package.
const (
	// UserIDKey holds the authenticated user ID.
	UserIDKey = "user_id"
	// UserRoleKey holds the authenticated user role.
	UserRoleKey = "user_role"
	// LoggerKey holds the request-scoped logger.
	LoggerKey = "logger"
)

// contextKey is the type of keys this package stores in context.Context.
type contextKey string

const loggerContextKey contextKey = "logger"

// userIDLoggedKey marks that the request logger already carries user_id, so
// the request logger middleware does not add it a second time.
const userIDLoggedKey = "logger_has_user_id"

// ContextEnhancer attaches a request-scoped logger to every request.
type ContextEnhancer struct {
	logger *zerolog.Logger
}

// NewContextEnhancer builds the request-scoped logger middleware. A nil
// logger yields a no-op logger.
func NewContextEnhancer(logger *zerolog.Logger) *ContextEnhancer {
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}

	return &ContextEnhancer{logger: logger}
}

// EnhanceContext returns the middleware that stores the request logger.
func (ce *ContextEnhancer) EnhanceContext() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			contextLogger := ce.logger.With().
				Str("request_id", GetRequestID(c)).
				Str("method", c.Request().Method).
				Str("path", c.Path()).
				Str("ip", c.RealIP()).
				Logger()

			// Only a valid span context yields trace fields; never a fake ID.
			if sc := trace.SpanContextFromContext(c.Request().Context()); sc.IsValid() {
				contextLogger = contextLogger.With().
					Str("trace_id", sc.TraceID().String()).
					Str("span_id", sc.SpanID().String()).
					Logger()
			}

			// Identity set by an upstream auth middleware, if any.
			if userID, ok := c.Get(UserIDKey).(string); ok && userID != "" {
				contextLogger = contextLogger.With().Str("user_id", userID).Logger()
				c.Set(userIDLoggedKey, true)
			}

			if userRole, ok := c.Get(UserRoleKey).(string); ok && userRole != "" {
				contextLogger = contextLogger.With().Str("user_role", userRole).Logger()
			}

			c.Set(LoggerKey, &contextLogger)

			ctx := context.WithValue(c.Request().Context(), loggerContextKey, &contextLogger)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// GetUserID returns the authenticated user ID from the Echo context, or "".
func GetUserID(c echo.Context) string {
	if userID, ok := c.Get(UserIDKey).(string); ok {
		return userID
	}

	return ""
}

// userIDLogged reports whether EnhanceContext put user_id on the request logger.
func userIDLogged(c echo.Context) bool {
	logged, ok := c.Get(userIDLoggedKey).(bool)

	return ok && logged
}

// GetLogger returns the request-scoped logger, or a no-op logger when unset.
func GetLogger(c echo.Context) *zerolog.Logger {
	if logger, ok := c.Get(LoggerKey).(*zerolog.Logger); ok {
		return logger
	}
	// Fallback to a no-op logger if not found
	nop := zerolog.Nop()

	return &nop
}
