---
phase: 01-foundations-contracts
plan: 06
subsystem: http
tags: [go, echo, errors, middleware, httpx]
requires: [01-01]
provides:
  - spec section 8 error envelope (errs.ErrorResponse)
  - Recover-first middleware chain (middleware.Apply / Chain)
  - httpx.Server with nil-safe Shutdown
affects: [ingest, query-api]
key-files:
  created:
    - backend/internal/errs/errs_test.go
    - backend/internal/validation/utils_test.go
    - backend/internal/middleware/chain.go
    - backend/internal/middleware/middleware_test.go
    - backend/internal/httpx/httpx.go
    - backend/internal/httpx/httpx_test.go
  modified:
    - backend/internal/errs/http.go
    - backend/internal/errs/types.go
    - backend/internal/validation/utils.go
    - backend/internal/middleware/context.go
    - backend/internal/middleware/global.go
    - backend/internal/middleware/request_id.go
decisions:
  - "Added middleware.Chain returning named middleware so Recover-first order is testable; Apply iterates it"
  - "NewTooManyRequestsError and NewServiceUnavailableError take retryAfterSeconds instead of an override flag"
metrics:
  tasks: 3
  completed: 2026-10-11
---

# Phase 1 Plan 06: HTTP error, middleware and httpx layer Summary

Spec section 8 error envelope, Recover-first middleware chain free of server.Server/New Relic/Clerk/pkg-errors, and a tested httpx wrapper with nil-guarded Shutdown.

## Commits
- 12268ef: errs envelope, validation panic fixes
- 649b79e: middleware chain and envelope error handler
- e19b821: httpx server helper

## Deviations from Plan
- Implementation was written alongside tests rather than with a separately committed RED step (tests pass; no separate failing-test commits).
- sqlerr needed no changes; it compiles against the new errs unchanged.
- Added `middleware.Chain`/`Named` (not in plan) to make the Recover-first ordering assertable.
- Validation of non-validator errors in extractValidationErrors now yields a single field error with the error text instead of panicking.

## Verification
- `go test -race` passes for errs, validation, sqlerr, middleware, httpx; `go vet` clean; gofmt clean; no banned imports.
- TestRecover* and TestShutdown* pass via scripts/go-test-nonempty.sh.
- golangci-lint NOT run: config fails to load ("goimports is a formatter"); plan 01-12 owns the fix.

## Known Stubs
None.

## Self-Check: PASSED
