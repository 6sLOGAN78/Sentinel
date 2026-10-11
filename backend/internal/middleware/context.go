package middleware

import (
	"context"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
)

const (
	UserIDKey   = "user_id"
	UserRoleKey = "user_role"
	LoggerKey   = "logger"
)

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

func (ce *ContextEnhancer) EnhanceContext() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			contextLogger := ce.logger.With().
				Str("request_id", GetRequestID(c)).
				Str("method", c.Request().Method).
				Str("path", c.Path()).
				Str("ip", c.RealIP()).
				Logger()

			// Identity set by an upstream auth middleware, if any.
			if userID, ok := c.Get(UserIDKey).(string); ok && userID != "" {
				contextLogger = contextLogger.With().Str("user_id", userID).Logger()
			}

			if userRole, ok := c.Get(UserRoleKey).(string); ok && userRole != "" {
				contextLogger = contextLogger.With().Str("user_role", userRole).Logger()
			}

			c.Set(LoggerKey, &contextLogger)

			ctx := context.WithValue(c.Request().Context(), LoggerKey, &contextLogger)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

func GetUserID(c echo.Context) string {
	if userID, ok := c.Get(UserIDKey).(string); ok {
		return userID
	}

	return ""
}

func GetLogger(c echo.Context) *zerolog.Logger {
	if logger, ok := c.Get(LoggerKey).(*zerolog.Logger); ok {
		return logger
	}
	// Fallback to a no-op logger if not found
	nop := zerolog.Nop()

	return &nop
}
