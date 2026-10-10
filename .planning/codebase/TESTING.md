# Testing Patterns

**Analysis Date:** 2026-10-10

## Current State

No test files exist (`*_test.go`, `*.test.ts`, `*.spec.ts` not detected). Only shared test infrastructure exists in `backend/internal/testing/`. No coverage requirement and no test task in `backend/taskfile.yml`. No CI config detected. TypeScript packages have no test runner.

## Test Framework

**Runner:**
- Go standard `go test` (Go module `backend/go.mod`)
- Assertions: `github.com/stretchr/testify` v1.11.1 (`require`, `assert`)
- Integration infra: `github.com/testcontainers/testcontainers-go` v0.42.0 (Postgres `postgres:15-alpine`)
- Config: lint rules for tests in `backend/.golangci.yml` (`testpackage`, `tparallel`, `testifylint`, `testableexamples`)

**Run Commands:**
```bash
cd backend && go test ./...              # Run all tests (requires Docker)
cd backend && go test -race ./...        # Race detector
cd backend && go test -cover ./...       # Coverage
```

## Test File Organization

**Location:** Place `xxx_test.go` next to the code. Because `testpackage` linter is enabled, use external package names (`package handler_test`).

**Naming:** `TestXxx` functions, table-driven with `t.Run`.

**Shared helpers:** `backend/internal/testing/` (package name `testing`; import with an alias such as `testutil` to avoid clashing with stdlib `testing`).

## Test Helpers (existing)

- `backend/internal/testing/container.go`: `SetupTestDB(t) (*TestDB, func())` starts a Postgres container with a random DB name, retries connection up to 5 times, applies migrations, registers `t.Cleanup` to terminate the container. `TestDB` holds `Pool`, `Container`, `Config`.
- `backend/internal/testing/server.go`: `CreateTestServer(&logger, testDB)` builds a `*server.Server` for tests.
- `backend/internal/testing/helpers.go`: `SetupTest(t)` returns `(testDB, testServer, cleanup)`; `MustMarshalJSON`, `ProjectRoot`, `Ptr[T]`.
- `backend/internal/testing/assertions.go`: `AssertTimestampsValid`, `AssertValidUUID`, and similar domain assertions.
- `backend/internal/testing/transaction.go`: transaction-scoped DB helpers for isolating test data.

**Usage pattern:**
```go
func TestSomething(t *testing.T) {
    testDB, srv, cleanup := testutil.SetupTest(t)
    defer cleanup()
    // build handler from srv, use httptest + echo, assert with require/assert
}
```

## Mocking

No mock framework in use. Prefer real Postgres via testcontainers over mocking the DB. Redis is configured as `localhost:6379` in test config and is not containerized; external services use dummy keys (`ResendAPIKey: "test-key"`, `Auth.SecretKey: "test-secret"`). `testify/mock` is allowed by lint config if needed.

**What to Mock:** Clerk auth, Resend email, New Relic.
**What NOT to Mock:** Postgres (use container), pure helpers.

## Fixtures and Factories

None yet. Create factories in `backend/internal/testing/` and use `Ptr[T]` for optional fields.

## Coverage

**Requirements:** None enforced.

## Test Types

**Unit Tests:** Not present; add for `internal/errs`, `internal/sqlerr`, `internal/validation`.
**Integration Tests:** Infrastructure ready (testcontainers + Echo test server); needs Docker. Container start adds ~2s+ sleep per attempt.
**E2E Tests:** Not used.

## Common Patterns

**Helper failures:** use `require.NoError(t, err, "message")`; call `t.Helper()` in helpers.

**Cleanup:** `t.Cleanup(func() {...})` for containers.

**Error Testing:**
```go
var he *errs.HTTPError
require.ErrorAs(t, err, &he)
assert.Equal(t, http.StatusNotFound, he.Status)
```

---

*Testing analysis: 2026-10-10*
