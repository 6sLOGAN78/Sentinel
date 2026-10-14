---
phase: 02-ingestion-durable-event-storage
plan: "08"
subsystem: ingestion
tags: [go, redis, lua, token-bucket, rate-limiting, kafka]

requires:
  - phase: 02-06
    provides: authenticated bounded intake and durable Kafka publication
provides:
  - Atomic per-key event-cost quotas using Redis TIME
  - Explicit bounded fail-closed and observable fail-open outage policies
  - Verified quota, validation, tenant, and saturation ordering
affects: [02-09, 02-12, 02-13, observability, ingest-readiness]

tech-stack:
  added: []
  patterns: [embedded Redis Lua script, event-count charging, atomic fail-open counter]

key-files:
  created:
    - backend/internal/ratelimit/ratelimit.go
    - backend/internal/ratelimit/ratelimit_test.go
    - backend/test/ratelimit_test.go
  modified:
    - backend/internal/ingest/handler.go
    - backend/internal/config/config.go
    - backend/internal/redisx/redisx.go
    - backend/cmd/ingest/main.go
    - .env.example

key-decisions:
  - "Keep fail-open handling inside each limiter so every admitted Redis bypass increments exactly one process-local atomic counter and emits one sanitized warning."
  - "Apply one 250 ms limiter deadline on top of bounded Redis dial/read/write timeouts and retries while preserving shared AUTH/TLS client construction."

patterns-established:
  - "Quota order: in-flight gate, authentication and scope, bounded envelope/count, quota debit, tenant authorization, field validation, Kafka publish."
  - "Redis buckets use only rl:<public-key-id>; secrets, tenant IDs, request bodies, and headers never enter keys or bypass logs."

requirements-completed: [ING-06, ING-07, ING-05]

duration: 14min
completed: 2026-10-14
---

# Phase 02 Plan 08: Redis Event Quotas Summary

**Atomic Redis TIME token buckets now charge each authenticated batch by event count, with bounded outage latency, explicit fail-open observability, and preserved intake error precedence.**

## Performance

- **Duration:** 14 min
- **Started:** 2026-10-14T13:24:25Z
- **Completed:** 2026-10-14T13:38:08Z
- **Tasks:** 3
- **Files modified:** 12

## Accomplishments

- Added an atomic Lua token bucket using Redis server time, cold-script fallback, per-public-key isolation, computed Retry-After, refill TTL, and N-event costs.
- Wired quota enforcement into intake after bounded envelope parsing and before tenant/field validation, while keeping the in-flight shed gate outermost.
- Made Redis outages fail closed by default; explicit fail-open admits with one sanitized warning and one atomic bypass increment per request.
- Proved concurrency, refill, outage latency, no-publication guarantees, retry charging, and mixed-error ordering against real Redis and Kafka.

## Task Commits

Each task was committed atomically:

1. **Task 1: Write real quota, concurrency and outage cases** - `0ae980b` (test)
2. **Task 2: Enforce atomic event-cost quotas in the intake path** - `704ec2a` (feat)
3. **Task 3: Prove ordering and saturation remain correct with quotas** - `77b6b51` (test)
4. **Quality-gate cleanup** - `0aa7101` (refactor)

## Files Created/Modified

- `backend/internal/ratelimit/ratelimit.go` - Redis TIME token bucket and explicit failure policy.
- `backend/internal/ratelimit/ratelimit_test.go` - Unit coverage for closed/open policy, sanitized logging, and bypass accounting.
- `backend/test/ratelimit_test.go` - Real Redis/Kafka quota, concurrency, refill, TTL, ordering, and paused-dependency tests.
- `backend/internal/ingest/handler.go` - Event-count quota charge in the agreed request order.
- `backend/internal/ingest/handler_test.go` - Mixed-error, retry-debit, and saturation call-boundary proofs.
- `backend/internal/config/config.go` - Limiter defaults, Redis operation bounds, and capacity-to-batch validation.
- `backend/internal/config/config_test.go` - Default, override, and unsafe-bound validation.
- `backend/internal/redisx/redisx.go` - Bounded dial/read/write/retry options with context deadlines enabled.
- `backend/internal/redisx/redisx_test.go` - Redis option propagation tests preserving TLS/AUTH behavior.
- `backend/cmd/ingest/main.go` - Production limiter construction and intake injection.
- `backend/test/ingest_test.go` - Existing ingest integration fixtures now include the real limiter.
- `.env.example` - Documented Redis and rate-limit defaults plus explicit failure policy.

## Decisions Made

- Failure policy lives in the limiter rather than the handler, so fail-open accounting and warning emission cannot diverge across callers.
- Client-level Redis timeouts and retries complement the per-call 250 ms context; the call context remains the hard request-path bound.
- Invalid tenant and field payloads are not refunded after quota debit, and every retry is a new quota attempt.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Corrected the testcontainers mapped-port conversion**
- **Found during:** Task 2 integration compilation
- **Issue:** The test used a nonexistent `Port.Int` method.
- **Fix:** Used the mapped port's supported string accessor and supplied explicit Redis timeout/retry bounds to the dedicated outage fixture.
- **Files modified:** `backend/test/ratelimit_test.go`
- **Verification:** `go test -tags=integration ./test/... -run TestRateLimit -timeout 4m -p 1`
- **Committed in:** `704ec2a`

**2. [Rule 1 - Bug] Resolved plan-introduced lint violations**
- **Found during:** Overall `make lint test` verification
- **Issue:** Static analysis flagged the embedded Lua as a false-positive credential, plus line length, magic number, named return, and float assertion style issues.
- **Fix:** Added a scoped gosec rationale, named protocol constants, simplified returns, wrapped cases, and used tolerant float assertions.
- **Files modified:** `backend/internal/ratelimit/ratelimit.go`, `backend/internal/ingest/handler.go`, `backend/internal/ingest/handler_test.go`, `backend/internal/config/config_test.go`
- **Verification:** `make lint test` completed with zero lint issues and all Go/web/generator tests passing.
- **Committed in:** `0aa7101`

---

**Total deviations:** 2 auto-fixed bugs.
**Impact on plan:** Both fixes were required for compilable integration coverage and repository quality gates; there was no scope expansion.

## Verification

- `go test -race ./internal/ratelimit/... ./internal/redisx/... ./internal/config/... ./internal/ingest/... -timeout 60s` — passed.
- `go test -tags=integration ./test/... -run 'TestRateLimit|TestIngestStatusCodes|TestIngestShedsUnderLoad' -timeout 4m -p 1` — passed with real Redis and Kafka.
- `make lint test` — passed: zero Go lint issues, all race tests, web lint/typecheck/tests, and generator lint/typecheck/tests.
- Closeout re-verification repeated the focused race suite, an uncached real Redis/Kafka integration run, and the full repository quality gate before tracking state advanced.

## Issues Encountered

- The first integration compile exposed a testcontainers API mismatch; it was corrected before any behavioral verification was recorded.
- The first full lint pass exposed only plan-introduced static-analysis findings; the second pass was clean.

## User Setup Required

None - documented defaults are present in `.env.example` and fail-closed remains the default.

## Next Phase Readiness

- Intake quota behavior is ready for readiness/operations wiring in plan 02-09 and end-to-end phase validation.
- No blockers or unresolved security findings remain.

## Self-Check: PASSED

- Confirmed `backend/internal/ratelimit/ratelimit.go` exists.
- Confirmed task and cleanup commits `0ae980b`, `704ec2a`, `77b6b51`, and `0aa7101` exist in git history.
- Confirmed no goal-blocking stubs in created or modified production files.
- Confirmed final race, uncached integration, lint, web, and generator gates pass during closeout.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-14*
