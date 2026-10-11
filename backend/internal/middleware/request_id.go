package middleware

import (
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const maxRequestIDLen = 64

// validRequestID accepts 1..64 chars of [A-Za-z0-9._-] so an inbound value
// cannot inject control characters into logs or response headers.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}

	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}

	return true
}

const (
	RequestIDHeader = "X-Request-ID"
	RequestIDKey    = "request_id"
)

func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			requestID := c.Request().Header.Get(RequestIDHeader)
			if !validRequestID(requestID) {
				requestID = uuid.New().String() // 4c90fc3f-39cc-4b04-af21-c83ee64aa67e
			}

			c.Set(RequestIDKey, requestID)
			c.Response().Header().Set(RequestIDHeader, requestID)

			return next(c)
		}
	}
}

func GetRequestID(c echo.Context) string {
	if requestID, ok := c.Get(RequestIDKey).(string); ok {
		return requestID
	}
	return ""
}
