package sqlerr_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/sqlerr"
)

func handled(t *testing.T, err error) *errs.HTTPError {
	t.Helper()

	var httpErr *errs.HTTPError
	require.ErrorAs(t, sqlerr.HandleError(err), &httpErr)

	return httpErr
}

func TestHandleErrorUniqueViolationIsConflict(t *testing.T) {
	t.Parallel()

	pgErr := &pgconn.PgError{
		Code:           "23505",
		TableName:      "api_keys",
		ConstraintName: "api_keys_prefix_key",
	}

	got := handled(t, fmt.Errorf("insert api key: %w", pgErr))
	assert.Equal(t, http.StatusConflict, got.Status)
	assert.Equal(t, "API_KEY_ALREADY_EXISTS", got.Code)
	assert.Contains(t, got.Message, "already exists")
}

func TestHandleErrorOtherConstraintsStayBadRequest(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"23503", "23502", "23514"} {
		got := handled(t, &pgconn.PgError{Code: code, TableName: "alerts", ColumnName: "tenant_id"})
		assert.Equal(t, http.StatusBadRequest, got.Status, code)
	}
}

func TestHandleErrorUnknownIsInternal(t *testing.T) {
	t.Parallel()

	got := handled(t, errors.New("boom"))
	assert.Equal(t, http.StatusInternalServerError, got.Status)
}
