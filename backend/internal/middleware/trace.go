package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TraceRename names the server span started by otelhttp after the matched
// route template ("POST /api/v1/events:batch"), never the raw path, and
// records the template as http.route. Unmatched requests become "HTTP GET",
// and methods outside the standard set become "_OTHER", so span names stay
// bounded. Without a recording span it is a no-op.
func TraceRename() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)

			span := trace.SpanFromContext(c.Request().Context())
			if !span.IsRecording() {
				return err
			}

			method := spanMethod(c.Request().Method)
			// Echo escapes a literal colon in a static segment as "\:".
			route := strings.ReplaceAll(c.Path(), `\:`, ":")
			if route == "" {
				span.SetName("HTTP " + method)

				return err
			}

			span.SetName(method + " " + route)
			span.SetAttributes(attribute.String("http.route", route))

			return err
		}
	}
}

// spanMethod maps a client-supplied method onto a bounded set.
func spanMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "_OTHER"
	}
}
