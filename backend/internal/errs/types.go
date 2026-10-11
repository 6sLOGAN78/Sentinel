package errs

import (
	"net/http"
)

// NewUnauthorizedError builds a 401 error.
func NewUnauthorizedError(message string, override bool) *HTTPError {
	return &HTTPError{
		Code:     MakeUpperCaseWithUnderscores(http.StatusText(http.StatusUnauthorized)),
		Message:  message,
		Status:   http.StatusUnauthorized,
		Override: override,
	}
}

// NewForbiddenError builds a 403 error.
func NewForbiddenError(message string, override bool) *HTTPError {
	return &HTTPError{
		Code:     MakeUpperCaseWithUnderscores(http.StatusText(http.StatusForbidden)),
		Message:  message,
		Status:   http.StatusForbidden,
		Override: override,
	}
}

// NewBadRequestError builds a 400 error with an optional code, field errors and action.
func NewBadRequestError(message string, override bool, code *string, errors []FieldError, action *Action) *HTTPError {
	formattedCode := MakeUpperCaseWithUnderscores(http.StatusText(http.StatusBadRequest))

	if code != nil {
		formattedCode = *code
	}

	return &HTTPError{
		Code:     formattedCode,
		Message:  message,
		Status:   http.StatusBadRequest,
		Override: override,
		Errors:   errors,
		Action:   action,
	}
}

// NewNotFoundError builds a 404 error.
func NewNotFoundError(message string, override bool, code *string) *HTTPError {
	formattedCode := MakeUpperCaseWithUnderscores(http.StatusText(http.StatusNotFound))

	if code != nil {
		formattedCode = *code
	}

	return &HTTPError{
		Code:     formattedCode,
		Message:  message,
		Status:   http.StatusNotFound,
		Override: override,
	}
}

// NewInternalServerError builds a 500 error.
func NewInternalServerError() *HTTPError {
	return &HTTPError{
		Code:     MakeUpperCaseWithUnderscores(http.StatusText(http.StatusInternalServerError)),
		Message:  http.StatusText(http.StatusInternalServerError),
		Status:   http.StatusInternalServerError,
		Override: false,
	}
}

// ValidationError builds a 400 error for failed request validation.
func ValidationError(err error) *HTTPError {
	return NewBadRequestError("Validation failed: "+err.Error(), false, nil, nil, nil)
}

// NewConflictError builds a 409 error with an optional code.
func NewConflictError(message string, override bool, code *string) *HTTPError {
	formattedCode := MakeUpperCaseWithUnderscores(http.StatusText(http.StatusConflict))

	if code != nil {
		formattedCode = *code
	}

	return &HTTPError{
		Code:     formattedCode,
		Message:  message,
		Status:   http.StatusConflict,
		Override: override,
	}
}

// NewPayloadTooLargeError builds a 413 error.
func NewPayloadTooLargeError(message string, override bool) *HTTPError {
	return &HTTPError{
		Code:     MakeUpperCaseWithUnderscores(http.StatusText(http.StatusRequestEntityTooLarge)),
		Message:  message,
		Status:   http.StatusRequestEntityTooLarge,
		Override: override,
	}
}

// NewTooManyRequestsError builds a 429 carrying a Retry-After in seconds.
func NewTooManyRequestsError(message string, retryAfterSeconds int) *HTTPError {
	return &HTTPError{
		Code:       MakeUpperCaseWithUnderscores(http.StatusText(http.StatusTooManyRequests)),
		Message:    message,
		Status:     http.StatusTooManyRequests,
		RetryAfter: retryAfterSeconds,
	}
}

// NewServiceUnavailableError builds a 503 carrying an optional Retry-After.
func NewServiceUnavailableError(message string, retryAfterSeconds int) *HTTPError {
	return &HTTPError{
		Code:       MakeUpperCaseWithUnderscores(http.StatusText(http.StatusServiceUnavailable)),
		Message:    message,
		Status:     http.StatusServiceUnavailable,
		RetryAfter: retryAfterSeconds,
	}
}
