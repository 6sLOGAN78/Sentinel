package middleware

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/sqlerr"
)

// GlobalMiddlewares builds the middleware shared by every HTTP service.
type GlobalMiddlewares struct {
	allowedOrigins []string
}

// NewGlobalMiddlewares returns the shared middleware for the given server.
func NewGlobalMiddlewares(allowedOrigins []string) *GlobalMiddlewares {
	return &GlobalMiddlewares{allowedOrigins: allowedOrigins}
}

// CORS returns the CORS middleware.
func (global *GlobalMiddlewares) CORS() echo.MiddlewareFunc {
	return middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: global.allowedOrigins,
	})
}

// RequestLogger returns the structured request logging middleware.
func (global *GlobalMiddlewares) RequestLogger() echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:     true,
		LogStatus:  true,
		LogError:   true,
		LogLatency: true,
		LogHost:    true,
		LogMethod:  true,
		LogURIPath: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			statusCode := v.Status

			// the status code is not set yet as it gets picked up by the global err handler
			// see here: https://github.com/labstack/echo/issues/2310#issuecomment-1288196898
			// Use the same mapping as GlobalErrorHandler so the logged status
			// always matches the status the client receives.
			if v.Error != nil {
				statusCode = toHTTPError(v.Error).Status
			}

			logger := GetLogger(c)

			var e *zerolog.Event

			switch {
			case statusCode >= http.StatusInternalServerError:
				e = logger.Error().Err(v.Error)
			case statusCode >= http.StatusBadRequest:
				e = logger.Warn()
			default:
				e = logger.Info()
			}

			if requestID := GetRequestID(c); requestID != "" {
				e = e.Str("request_id", requestID)
			}

			if userID := GetUserID(c); userID != "" {
				e = e.Str("user_id", userID)
			}

			e.
				Dur("latency", v.Latency).
				Int("status", statusCode).
				Str("method", v.Method).
				Str("uri", v.URI).
				Str("host", v.Host).
				Str("ip", c.RealIP()).
				Str("user_agent", c.Request().UserAgent()).
				Msg("API")

			return nil
		},
	})
}

// Recover returns the panic recovery middleware.
func (global *GlobalMiddlewares) Recover() echo.MiddlewareFunc {
	return middleware.Recover()
}

// Secure returns the security headers middleware.
func (global *GlobalMiddlewares) Secure() echo.MiddlewareFunc {
	return middleware.Secure()
}

// toHTTPError maps any error to a sanitized *errs.HTTPError.
func toHTTPError(err error) *errs.HTTPError {
	var httpErr *errs.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr
	}

	var echoErr *echo.HTTPError
	if errors.As(err, &echoErr) {
		message := http.StatusText(echoErr.Code)
		if msg, ok := echoErr.Message.(string); ok && msg != "" {
			message = msg
		}

		if echoErr.Code == http.StatusNotFound {
			message = "Route not found"
		}

		return &errs.HTTPError{
			Code:    errs.MakeUpperCaseWithUnderscores(http.StatusText(echoErr.Code)),
			Message: message,
			Status:  echoErr.Code,
		}
	}

	// database errors become application errors; everything else is a 500
	var converted *errs.HTTPError
	if errors.As(sqlerr.HandleError(err), &converted) {
		return converted
	}

	return errs.NewInternalServerError()
}

// GlobalErrorHandler writes the spec section 8 envelope for every error and
// logs the original error while returning only the sanitized one.
func (global *GlobalMiddlewares) GlobalErrorHandler(err error, c echo.Context) {
	httpErr := toHTTPError(err)

	GetLogger(c).Error().Stack().
		Err(err).
		Int("status", httpErr.Status).
		Str("error_code", httpErr.Code).
		Msg(httpErr.Message)

	if c.Response().Committed {
		return
	}

	if httpErr.RetryAfter > 0 &&
		(httpErr.Status == http.StatusTooManyRequests || httpErr.Status == http.StatusServiceUnavailable) {
		c.Response().Header().Set(echo.HeaderRetryAfter, strconv.Itoa(httpErr.RetryAfter))
	}

	_ = c.JSON(httpErr.Status, httpErr.Response(GetRequestID(c)))
}
