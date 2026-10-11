package errs_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/errs"
)

func TestEnvelopeShape(t *testing.T) {
	t.Parallel()

	e := errs.NewBadRequestError("bad", false, nil, []errs.FieldError{{Field: "a", Error: "b"}}, nil)
	raw, err := json.Marshal(e.Response("req-1"))
	require.NoError(t, err)

	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, map[string]map[string]string{
		"error": {"code": "BAD_REQUEST", "message": "bad", "request_id": "req-1"},
	}, got)

	// the Go error type itself must not leak internal fields either
	raw, err = json.Marshal(e)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(raw))
}

func TestConstructors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err    *errs.HTTPError
		code   string
		status int
	}{
		{errs.NewBadRequestError("m", false, nil, nil, nil), "BAD_REQUEST", http.StatusBadRequest},
		{errs.NewUnauthorizedError("m", false), "UNAUTHORIZED", http.StatusUnauthorized},
		{errs.NewForbiddenError("m", false), "FORBIDDEN", http.StatusForbidden},
		{errs.NewNotFoundError("m", false, nil), "NOT_FOUND", http.StatusNotFound},
		{errs.NewConflictError("m", false, nil), "CONFLICT", http.StatusConflict},
		{errs.NewPayloadTooLargeError("m", false), "REQUEST_ENTITY_TOO_LARGE", http.StatusRequestEntityTooLarge},
		{errs.NewTooManyRequestsError("m", 7), "TOO_MANY_REQUESTS", http.StatusTooManyRequests},
		{errs.NewInternalServerError(), "INTERNAL_SERVER_ERROR", http.StatusInternalServerError},
		{errs.NewServiceUnavailableError("m", 3), "SERVICE_UNAVAILABLE", http.StatusServiceUnavailable},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.code, tc.err.Code)
		assert.Equal(t, tc.status, tc.err.Status)
	}

	assert.Equal(t, 7, tests[6].err.RetryAfter)
}

func TestWithMessageAndIsAs(t *testing.T) {
	t.Parallel()

	orig := errs.NewTooManyRequestsError("slow", 9)
	cp := orig.WithMessage("slower")
	assert.Equal(t, "slower", cp.Message)
	assert.Equal(t, 9, cp.RetryAfter)
	assert.Equal(t, orig.Status, cp.Status)

	wrapped := errors.Join(errors.New("x"), cp)

	var target *errs.HTTPError
	require.ErrorAs(t, wrapped, &target)
	require.ErrorIs(t, cp, orig)
	require.ErrorIs(t, wrapped, errs.NewTooManyRequestsError("other", 1))
}

func TestIsComparesCodeAndStatus(t *testing.T) {
	t.Parallel()

	notFound := errs.NewNotFoundError("missing", false, nil)

	require.NotErrorIs(t, notFound, errs.NewUnauthorizedError("nope", false))
	require.NotErrorIs(t, notFound, &errs.HTTPError{})
	require.NotErrorIs(t, notFound, (*errs.HTTPError)(nil))
	require.ErrorIs(t, notFound, errs.NewNotFoundError("other message", true, nil))

	code := "ALERT_NOT_FOUND"
	require.NotErrorIs(t, notFound, errs.NewNotFoundError("missing", false, &code))
}
