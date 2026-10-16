---
phase: 02-ingestion-durable-event-storage
plan: "09"
subsystem: observability
tags: [go, healthz, readyz, graceful-shutdown, kafka, postgres, redis, clickhouse]

requires:
  - phase: 02-07
    provides: bounded event-writer lifecycle and durable poison-event handling
  - phase: 02-08
    provides: Redis-backed intake quotas and bounded dependency behavior
provides:
  - Separate private liveness and live dependency readiness listeners for all five binaries
  - Bounded readiness concurrency, sanitized dependency states, and synchronous bind failure
  - Drain-first graceful shutdown with the ops listener closing last
affects: [02-11, 02-12, 02-13, compose-healthchecks, operational-observability]

tech-stack:
  added: []
  patterns: [shared bounded ops listener, live protocol readiness checks, drain-first LIFO shutdown]

key-files:
  created:
    - backend/internal/opshttp/server.go
    - backend/test/readyz_test.go
  modified:
    - backend/cmd/ingest/main.go
    - backend/cmd/detector/main.go
    - backend/cmd/event-writer/main.go
    - backend/cmd/alert-writer/main.go
    - backend/cmd/query-api/main.go
    - backend/internal/config/config.go
    - backend/test/lifecycle_test.go

key-decisions:
  - "Expose dependency state only on a separately bound ops listener, with loopback defaults and sanitized dependency names/states."
  - "Register ops shutdown first and draining last so LIFO cleanup advertises not-ready before stopping work and closes health endpoints after dependencies."
  - "Use real Kafka protocol Ping, PostgreSQL/Redis Ping, native ClickHouse Ping, and HTTP ClickHouse SELECT 1 rather than TCP reachability."

patterns-established:
  - "Readiness uses one bounded request context, a nonblocking 16-request admission semaphore, and no raw dependency errors in responses."
  - "Service setup synchronously binds listeners and registers resources in reverse desired shutdown order."

requirements-completed: [OBS-01]

duration: 26h 33m elapsed (resumed after quota reset)
completed: 2026-10-16
---

# Phase 02 Plan 09: Operational Health and Graceful Draining Summary

**All five services now expose bounded private liveness/readiness endpoints backed by real dependency protocols, with drain-first shutdown and outage recovery proofs.**

## Performance

- **Duration:** 26h 33m elapsed across resumed sessions
- **Started:** 2026-10-14T23:31:14Z
- **Completed:** 2026-10-16T02:04:22Z
- **Tasks:** 3
- **Files modified:** 15

## Accomplishments

- Added a shared ops server with synchronous bind errors, finite HTTP timeouts, bounded readiness concurrency, sanitized states, and idempotent shutdown.
- Wired ingest, detector, event-writer, alert-writer, and query-api to live Kafka, PostgreSQL, Redis, and ClickHouse protocol checks with distinct service defaults.
- Proved dependency outage/recovery and drain-before-exit behavior against dedicated real containers while preserving the repository-wide race, lint, web, and generator gates.

## Task Commits

Each task was committed atomically:

1. **Task 1: Write five-service readiness and draining acceptance** - `de20808` (test)
2. **Task 2: Serve bounded health checks and wire all binary listeners** - `e21d292` (feat)
3. **Task 3: Order drain, request/worker stop and resource cleanup** - `a4b22af` (test)
4. **Verification-gate correction** - `5dca6fd` (fix)

## Files Created/Modified

- `backend/internal/opshttp/server.go` - Shared private health/readiness listener, drain gate, admission bound, and graceful shutdown.
- `backend/internal/opshttp/server_test.go` - Sanitization, concurrency, timeout, bind, listener-release, and idempotence proofs.
- `backend/test/readyz_test.go` - Five-service real dependency outage, recovery, liveness, and draining acceptance.
- `backend/test/lifecycle_test.go` - Per-process distinct public/ops ports and bounded signal-driven lifecycle coverage.
- `backend/internal/config/config.go` - Per-service ops defaults and bounded listener validation.
- `backend/internal/chmigrate/chmigrate.go` - Real HTTP-protocol ClickHouse readiness query for alert-writer.
- `backend/cmd/{ingest,detector,event-writer,alert-writer,query-api}/main.go` - Live checks and LIFO drain/resource/ops shutdown wiring.
- `.env.example` - Private ops listener documentation without overriding service-specific ports.

## Decisions Made

- The ops listener binds after required clients are initialized but its nil-safe closer is registered first, avoiding startup races while guaranteeing that it closes last.
- Kafka-only skeleton services hold bounded franz-go clients so readiness executes a protocol Ping instead of treating a TCP connection as capability.
- Alert-writer uses the existing ClickHouse HTTP runner for `SELECT 1`; event-writer and query-api reuse their native driver connections.
- Public port `0` remains available only in the test environment for kernel-assigned bind tests; production and all ops ports require `1..65535`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added a real ClickHouse HTTP readiness operation**
- **Found during:** Task 2 (alert-writer wiring)
- **Issue:** Alert-writer owned only the HTTP ClickHouse configuration and had no reusable protocol-level readiness operation.
- **Fix:** Added `chmigrate.Runner.Ping`, which executes bounded `SELECT 1` with configured authentication and database selection.
- **Files modified:** `backend/internal/chmigrate/chmigrate.go`, `backend/internal/chmigrate/chmigrate_test.go`
- **Verification:** Focused race tests and real ClickHouse outage/recovery acceptance passed.
- **Committed in:** `e21d292`

**2. [Rule 1 - Bug] Preserved test-only kernel-assigned public ports**
- **Found during:** Repository-wide `make lint test`
- **Issue:** New shared listener validation rejected public port `0`, breaking established bind-address tests that intentionally request a kernel-assigned port.
- **Fix:** Allowed public port `0` only when `PRIMARY.ENV=test`; ops listeners and non-test public listeners retain the `1..65535` bound.
- **Files modified:** `backend/internal/config/config.go`, `backend/internal/config/config_test.go`
- **Verification:** `go test -race ./internal/config/... ./internal/httpx/...` and the subsequent full repository gate passed.
- **Committed in:** `5dca6fd`

---

**Total deviations:** 2 auto-fixed (1 missing critical capability, 1 regression bug).
**Impact on plan:** Both changes were required for live protocol readiness and compatibility with the existing test harness; no unrelated feature scope was added.

## Verification

- `go test -race ./internal/opshttp/... ./internal/config/... ./internal/chmigrate/... ./cmd/... -timeout 120s` — passed.
- `go test -race ./internal/app/... ./internal/opshttp/... -timeout 60s` — passed.
- `go test -tags=integration ./test/... -run 'TestReadyz|TestHealthz|TestLifecycle' -timeout 5m -p 1` — passed against real PostgreSQL, Redis, Kafka, and ClickHouse containers.
- `gsd-sdk query verify.key-links .../02-09-PLAN.md` — 2/2 key links verified.
- `make lint test` — passed with zero lint issues, all Go race tests, web lint/typecheck/tests, and 37 generator tests.

## Issues Encountered

- Two executor attempts stopped at the provider usage limit. The committed acceptance work and uncommitted implementation were audited and resumed without discarding or duplicating changes.
- The first repository-wide gate exposed the test-only ephemeral-port regression; systematic root-cause analysis produced the narrow environment-aware fix above.

## User Setup Required

None - ops defaults are documented in `.env.example`, bind to loopback for source runs, and can be explicitly overridden for containers.

## Next Phase Readiness

- Plan 02-11 can now exercise ambiguous publish and writer-crash recovery with observable service readiness.
- Plans 02-12 and 02-13 can consume stable health endpoints for Compose startup and end-to-end seed validation.
- No unresolved plan-level blockers remain.

## Self-Check: PASSED

- Confirmed created ops server and readiness acceptance files exist.
- Confirmed commits `de20808`, `e21d292`, `a4b22af`, and `5dca6fd` exist in history.
- Confirmed both declared key links verify.
- Confirmed focused race/integration checks and the fresh full repository quality gate pass.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-16*
