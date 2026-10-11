// Package middleware holds the Echo middleware used by Sentinel HTTP services:
// request IDs, context-scoped logging, CORS, recovery and security headers.
package middleware

import (
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
)

// Named pairs a middleware with a stable name so its position is testable.
type Named struct {
	Name string
	Func echo.MiddlewareFunc
}

// Chain returns the global middleware in registration order. Recover is
// always first so a panic in any later middleware is converted to a 500.
func Chain(logger *zerolog.Logger, allowedOrigins []string) []Named {
	global := NewGlobalMiddlewares(allowedOrigins)

	return []Named{
		{Name: "recover", Func: global.Recover()},
		{Name: "request_id", Func: RequestID()},
		{Name: "context", Func: NewContextEnhancer(logger).EnhanceContext()},
		{Name: "cors", Func: global.CORS()},
		{Name: "secure", Func: global.Secure()},
		{Name: "request_logger", Func: global.RequestLogger()},
	}
}

// Apply installs the envelope error handler and the global middleware chain.
// Unless the caller already configured e.IPExtractor (for example with
// echo.ExtractIPFromXFFHeader and explicit trusted proxy ranges), the client
// IP is taken from the TCP peer address so X-Forwarded-For and X-Real-IP
// sent by clients cannot spoof it.
func Apply(e *echo.Echo, logger *zerolog.Logger, allowedOrigins []string) {
	if e.IPExtractor == nil {
		e.IPExtractor = echo.ExtractIPDirect()
	}

	e.HTTPErrorHandler = NewGlobalMiddlewares(allowedOrigins).GlobalErrorHandler

	for _, m := range Chain(logger, allowedOrigins) {
		e.Use(m.Func)
	}
}
