---
phase: 02-ingestion-durable-event-storage
plan: "03"
subsystem: ingestion-api
tags: [go, echo, kafka, openapi, typescript, validation]

requires:
  - phase: 02-ingestion-durable-event-storage
    provides: authenticated bounded Kafka intake with synchronous all-ISR acknowledgement
provides:
  - atomic zero-publish rejection with tenant-first mixed-error ordering
  - bounded indexed field details in the Go and OpenAPI error envelopes
  - API-key or bearer alternatives for event lookup
affects: [02-04-clickhouse-query, 02-08-rate-limits, public-api-consumers]

tech-stack:
  added: []
  patterns: [duplicate-safe raw JSON normalization, two-pass authorization and validation, generated contract drift tests]

key-files:
  created: []
  modified:
    - backend/internal/ingest/handler.go
    - backend/internal/errs/http.go
    - backend/test/ingest_test.go
    - api/openapi.yaml
    - web/src/api/types.test.ts
    - docs/decisions/0003-atomic-batch-semantics.md

key-decisions:
  - "Resolve every reliably parsed tenant before field validation so cross-tenant writes return 403 without publishing."
  - "Strip the server-owned received_at field through duplicate-safe raw-object normalization, then apply one UTC-millisecond timestamp to the accepted batch."
  - "Expose validation details as optional arrays capped at 50 so ordinary errors retain their three-field envelope."

patterns-established:
  - "Batch authorization pass: normalize every raw event and inspect tenant identity before typed validation."
  - "Safe field errors: return events[i].field paths and fixed messages without raw request fragments."

requirements-completed: [ING-02, ING-05]

duration: 8 min
completed: 2026-10-14
---

# Phase 2 Plan 03: Atomic Rejection and Public Contract Summary

**Strict duplicate-safe batch decoding now rejects invalid input without Kafka side effects and returns at most 50 indexed details, with the same bounds represented in OpenAPI and generated TypeScript consumers.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-10-13T19:15:38Z
- **Completed:** 2026-10-13T19:23:22Z
- **Tasks:** 3
- **Files modified:** 10

## Accomplishments

- Added real Kafka acceptance matrices for invalid credentials, missing scope, body/count limits, malformed and duplicate envelopes, tenant mismatches, and indexed field failures; every rejected case verifies zero records on a dedicated topic.
- Implemented exact, duplicate-safe envelope parsing plus a two-pass event flow that gives a resolved tenant mismatch precedence over all field errors and publishes only after the complete batch passes.
- Added optional `error.details` with `events[i].field` paths and a hard 50-item cap while preserving the ordinary code/message/request_id envelope.
- Updated OpenAPI, generated TypeScript assertions, and ADR 0003 for error detail bounds, `getEvent` API-key-or-bearer security, and verified literal-colon routing.

## Task Commits

1. **Task 1: Specify atomic invalid batches and mixed-error ordering** - `b61f145` (RED tests)
2. **Task 2: Return strict bounded indexed validation errors** - `cae48f9` (GREEN implementation), `c34c1b4` (refactor)
3. **Task 3: Align public OpenAPI, generated types and routing ADR** - `2a911b6` (RED tests), `fa2b8fe` (GREEN contract)

## Files Created/Modified

- `backend/internal/ingest/handler.go` - Strict envelope parser, duplicate-safe event normalization, tenant-first scan, bounded indexed validation, and atomic publish boundary.
- `backend/internal/ingest/handler_test.go` - Route, precedence, detail cap, and server-owned timestamp tests.
- `backend/internal/errs/http.go` - Optional field detail serialization.
- `backend/internal/errs/errs_test.go` - Detailed and ordinary envelope shape checks.
- `backend/test/ingest_test.go` - Real PostgreSQL/Kafka negative acceptance with zero-publish assertions.
- `api/openapi.yaml` - Bounded strict details schema and separate `getEvent` auth alternatives.
- `backend/test/openapi_test.go` - Public schema and security-alternative checks.
- `backend/test/openapi_drift_test.go` - Strict detail structure drift guard.
- `web/src/api/types.test.ts` - Generated optional detail consumer assertion.
- `docs/decisions/0003-atomic-batch-semantics.md` - Verified colon routing and mixed-error ordering.

## Decisions Made

- Tenant mismatch is evaluated across all reliably parsed event tenants before field validation; ambiguous duplicate `tenant_id` values remain a 400 decode error.
- Client `received_at` values, including malformed values, are removed before canonical decoding and replaced by one server timestamp after validation.
- Count overflow and byte overflow both use 413; strict envelope defects use 400; missing scope is rejected immediately after authentication.
- Quota precedence remains assigned to plan 02-08 and is documented without claiming it is already accepted.

## Verification Evidence

- `cd backend && go test -race ./internal/ingest/... ./internal/errs/... -timeout 60s` — passed.
- `cd backend && go test -tags=integration ./test/... -run 'TestIngestInvalidBatch|TestIngestStatusCodes' -timeout 3m -p 1` — passed against real PostgreSQL and Kafka in 23.085s; every rejected case observed zero Kafka records.
- `cd backend && go test ./test/... -run 'TestOpenAPI' -timeout 60s` — passed.
- `cd backend && go vet ./internal/ingest/... ./internal/errs/... ./test/...` — passed.
- `cd backend && go vet -tags=integration ./test/...` — passed.
- Regular and `--build-tags integration` golangci-lint targets — 0 issues.
- `cd web && npm run gen:api && npm run typecheck && npm test && npm run lint` — passed; 2 files and 6 tests passed.
- `make lint test` — passed across backend race tests, web generation/typecheck/tests, and generator Ruff/format/mypy/37 pytest cases.

## Security Evidence

- Invalid credentials and missing scopes are rejected before any body-derived response content or publication.
- Duplicate envelope/event keys cannot be interpreted differently by downstream parsers.
- Response details use fixed validation messages and never include raw bodies, credentials, or broker failures.
- Cross-tenant input produces 403 before field errors and before Kafka publication.

## Interface Signatures for Later Plans

- `errs.ErrorBody.Details []errs.FieldError` serializes as `details,omitempty` and is bounded to 50 by ingestion.
- `POST /api/v1/events:batch` keeps its 202 response ordering and durable `PublishBatch` acknowledgment boundary.
- `GET /api/v1/events/{event_id}` declares separate `ApiKeyAuth` and `BearerAuth` alternatives.
- Plan 02-08 must insert quota handling after strict envelope checks and before tenant/field responses, while missing scope remains an immediate 403.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Refactored validation helpers to satisfy mandatory lint gates**
- **Found during:** Wave-boundary verification
- **Issue:** The first correct implementation exceeded the configured cognitive-complexity limit, and new table tests needed parallel/line-length cleanup.
- **Fix:** Split normalization into focused batch and builder helpers and formatted the affected tests without changing behavior.
- **Files modified:** `backend/internal/ingest/handler.go`, `backend/internal/ingest/handler_test.go`, `backend/test/ingest_test.go`
- **Verification:** Both regular and integration-tag golangci-lint report 0 issues; race and real Kafka tests pass.
- **Committed in:** `c34c1b4`

**Total deviations:** 1 auto-fixed (1 blocking issue).
**Impact on plan:** The refactor was required by the repository quality gate and preserved the planned behavior and scope.

## Issues Encountered

- The initial RED suite intentionally demonstrated duplicate-envelope publication, 400 instead of 413 for count overflow, field-before-tenant precedence, and missing response details. The GREEN commits resolve each failure.

## User Setup Required

None - no external service configuration required.

## Known Stubs

None.

## Next Phase Readiness

- ClickHouse storage/query work can consume the documented API-key lookup contract and the canonical accepted event stream.
- Rate limiting can add the reserved quota step without changing authentication, strict envelope, tenant, or field-error behavior.
- No blockers remain for plan 02-04.

## Self-Check: PASSED

- All ten modified contract/runtime/test files exist.
- Commits `b61f145`, `cae48f9`, `2a911b6`, `fa2b8fe`, and `c34c1b4` exist in history.
- No known stubs or unplanned threat surfaces remain.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-14*
