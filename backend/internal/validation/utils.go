// Package validation binds and validates HTTP request payloads.
package validation

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// Validatable is implemented by request payloads that can validate themselves.
type Validatable interface {
	Validate() error
}

// CustomValidationError is one field-level failure from custom validation.
type CustomValidationError struct {
	Field   string
	Message string
}

// CustomValidationErrors is a list of custom field-level failures.
type CustomValidationErrors []CustomValidationError

func (c CustomValidationErrors) Error() string {
	return validationFailedMsg
}

// BindAndValidate binds the request body into payload and validates it.
func BindAndValidate(c echo.Context, payload Validatable) error {
	if err := c.Bind(payload); err != nil {
		return errs.NewBadRequestError(bindMessage(err), false, nil, nil, nil)
	}

	if msg, fieldErrors := validateStruct(payload); fieldErrors != nil {
		return errs.NewBadRequestError(msg, true, nil, fieldErrors, nil)
	}

	return nil
}

// bindMessage derives a client-safe message from a bind error without
// assuming any particular error string layout.
func bindMessage(err error) string {
	var echoErr *echo.HTTPError
	if errors.As(err, &echoErr) {
		if msg, ok := echoErr.Message.(string); ok && msg != "" {
			return msg
		}
	}

	return http.StatusText(http.StatusBadRequest)
}

func validateStruct(v Validatable) (string, []errs.FieldError) {
	if err := v.Validate(); err != nil {
		return extractValidationErrors(err)
	}
	return "", nil
}

const validationFailedMsg = "Validation failed"

func extractValidationErrors(err error) (string, []errs.FieldError) {
	var validationErrors validator.ValidationErrors
	if !errors.As(err, &validationErrors) {
		return validationFailedMsg, nonValidatorErrors(err)
	}

	fieldErrors := make([]errs.FieldError, 0, len(validationErrors))
	for _, ve := range validationErrors {
		fieldErrors = append(fieldErrors, errs.FieldError{
			Field: strings.ToLower(ve.Field()),
			Error: fieldMessage(ve),
		})
	}

	return validationFailedMsg, fieldErrors
}

// nonValidatorErrors converts custom or arbitrary errors to field errors.
func nonValidatorErrors(err error) []errs.FieldError {
	var customValidationErrors CustomValidationErrors
	if !errors.As(err, &customValidationErrors) {
		return []errs.FieldError{{Field: "", Error: err.Error()}}
	}

	fieldErrors := make([]errs.FieldError, 0, len(customValidationErrors))
	for _, cerr := range customValidationErrors {
		fieldErrors = append(fieldErrors, errs.FieldError{Field: cerr.Field, Error: cerr.Message})
	}

	return fieldErrors
}

// fieldMessage renders the user-facing message for one validator tag failure.
func fieldMessage(fe validator.FieldError) string {
	isString := fe.Type().Kind() == reflect.String

	switch fe.Tag() {
	case "required":
		return "is required"
	case "min":
		if isString {
			return fmt.Sprintf("must be at least %s characters", fe.Param())
		}

		return fmt.Sprintf("must be at least %s", fe.Param())
	case "max":
		if isString {
			return fmt.Sprintf("must not exceed %s characters", fe.Param())
		}

		return fmt.Sprintf("must not exceed %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", fe.Param())
	case "email":
		return "must be a valid email address"
	case "e164":
		return "must be a valid phone number with country code"
	case "uuid":
		return "must be a valid UUID"
	case "uuidList":
		return "must be a comma-separated list of valid UUIDs"
	case "dive":
		return "some items are invalid"
	default:
		field := strings.ToLower(fe.Field())
		if fe.Param() != "" {
			return fmt.Sprintf("%s: %s:%s", field, fe.Tag(), fe.Param())
		}

		return fmt.Sprintf("%s: %s", field, fe.Tag())
	}
}

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsValidUUID reports whether uuid is a canonical 8-4-4-4-12 hex UUID.
func IsValidUUID(uuid string) bool {
	return uuidRegex.MatchString(uuid)
}
