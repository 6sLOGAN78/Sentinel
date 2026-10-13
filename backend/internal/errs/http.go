// Package errs defines the typed HTTP error shape returned by the API and
// constructors for the standard status classes.
package errs

import (
	"strings"
)

// FieldError describes one invalid request field.
type FieldError struct {
	Field string `json:"field"`
	Error string `json:"error"`
}

// ActionType names the client-side action attached to an error.
type ActionType string

const (
	// ActionTypeRedirect asks the client to redirect.
	ActionTypeRedirect ActionType = "redirect"
)

// Action is an optional follow-up the client should perform for an error.
type Action struct {
	Type    ActionType `json:"type"`
	Message string     `json:"message"`
	Value   string     `json:"value"`
}

// ErrorBody is the spec section 8 error payload.
type ErrorBody struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	RequestID string       `json:"request_id"`
	Details   []FieldError `json:"details,omitempty"`
}

// ErrorResponse is the only error shape written to the wire.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// HTTPError is the internal API error type. Only the envelope built by
// Response reaches clients, so every field here is hidden from JSON.
type HTTPError struct {
	Code     string `json:"-"`
	Message  string `json:"-"`
	Status   int    `json:"-"`
	Override bool   `json:"-"`
	// field level errors
	Errors []FieldError `json:"-"`
	// action to be taken
	Action *Action `json:"-"`
	// RetryAfter is the Retry-After header value in seconds (429 and 503).
	RetryAfter int `json:"-"`
}

// Response builds the spec section 8 envelope for this error.
func (e *HTTPError) Response(requestID string) ErrorResponse {
	return ErrorResponse{Error: ErrorBody{
		Code:      e.Code,
		Message:   e.Message,
		RequestID: requestID,
		Details:   e.Errors,
	}}
}

func (e *HTTPError) Error() string {
	return e.Message
}

// Is reports whether target is an *HTTPError with the same code and status.
func (e *HTTPError) Is(target error) bool {
	t, ok := target.(*HTTPError)

	return ok && t != nil && t.Code == e.Code && t.Status == e.Status
}

// WithMessage returns a copy of the error carrying message.
func (e *HTTPError) WithMessage(message string) *HTTPError {
	return &HTTPError{
		Code:     e.Code,
		Message:  message,
		Status:   e.Status,
		Override: e.Override,
		Errors:   e.Errors,
		Action:   e.Action,

		RetryAfter: e.RetryAfter,
	}
}

// MakeUpperCaseWithUnderscores converts text such as "Not Found" to "NOT_FOUND".
func MakeUpperCaseWithUnderscores(str string) string {
	return strings.ToUpper(strings.ReplaceAll(str, " ", "_"))
}
