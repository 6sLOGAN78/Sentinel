package validation_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/validation"
)

type payload struct {
	Name string `json:"name"`
	err  error
}

func (p *payload) Validate() error { return p.err }

func bind(t *testing.T, body string, p validation.Validatable) error {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	c := echo.New().NewContext(req, httptest.NewRecorder())

	return validation.BindAndValidate(c, p)
}

func TestBindAndValidateMalformedDoesNotPanic(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"", `{"name":`, "x", `[1]`} {
		var err error
		require.NotPanics(t, func() { err = bind(t, body, &payload{}) })

		if err == nil {
			continue
		}

		var he *errs.HTTPError
		require.ErrorAs(t, err, &he)
		assert.Equal(t, http.StatusBadRequest, he.Status)
	}
}

func TestBindAndValidatePlainErrorDoesNotPanic(t *testing.T) {
	t.Parallel()

	var err error
	require.NotPanics(t, func() { err = bind(t, `{"name":"a"}`, &payload{err: errors.New("plain")}) })

	var he *errs.HTTPError
	require.ErrorAs(t, err, &he)
	assert.Equal(t, http.StatusBadRequest, he.Status)
}

func TestBindAndValidateCustomErrors(t *testing.T) {
	t.Parallel()

	p := &payload{err: validation.CustomValidationErrors{{Field: "name", Message: "bad"}}}
	err := bind(t, `{"name":"a"}`, p)

	var he *errs.HTTPError
	require.ErrorAs(t, err, &he)
	require.Len(t, he.Errors, 1)
	assert.Equal(t, "name", he.Errors[0].Field)
}

func TestBindAndValidateOK(t *testing.T) {
	t.Parallel()
	require.NoError(t, bind(t, `{"name":"a"}`, &payload{}))
}
