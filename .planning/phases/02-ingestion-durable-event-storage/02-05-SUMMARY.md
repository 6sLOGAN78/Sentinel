---
phase: 02-ingestion-durable-event-storage
plan: "05"
subsystem: clickhouse-operations
tags: [clickhouse, retention, ttl, migrations, operator-tooling]

requires:
  - phase: 02-ingestion-durable-event-storage
    provides: Native ClickHouse events table, immutable seven-day migration default, and duplicate-safe reads
provides:
  - Validated 1..3650-day event retention updates through the migration tool
  - Idempotent TTL application after both pending and no-op migration runs
  - Operator documentation for asynchronous TTL deletion and duplicate visibility
affects: [02-06-event-writer, 02-12-compose, 02-13-seed-ci, operations]

tech-stack:
  added: []
  patterns: [fixed validated operator DDL, flag-over-env-over-default configuration, immutable migration history]

key-files:
  created:
    - backend/tools/chmigrate/main_test.go
    - docs/event-storage.md
  modified:
    - Makefile
    - backend/internal/chmigrate/chmigrate.go
    - backend/internal/chmigrate/chmigrate_test.go
    - backend/test/migrate_test.go
    - backend/tools/chmigrate/main.go

key-decisions:
  - "Keep the seven-day migration default immutable and apply configured retention after every successful migration run."
  - "Accept only validated integer retention from 1 through 3650 days and a validated database identifier in fixed TTL DDL."
  - "Do not automatically MATERIALIZE TTL; existing-part deletion remains asynchronous to avoid an unbounded rewrite."

patterns-established:
  - "Operator configuration precedence: explicit flag, then plain environment value, then a safe local default."
  - "Schema maintenance pattern: run versioned migrations first, then apply idempotent mutable operational policy even when no migrations are pending."

requirements-completed: [STOR-02]

duration: 2 min
completed: 2026-10-14
---

# Phase 2 Plan 05: Configurable Event Retention Summary

**Validated ClickHouse event TTL updates now run idempotently against the configured database after every migration invocation, while immutable history retains its seven-day default.**

## Performance

- **Duration:** 2 min recorded execution window
- **Started:** 2026-10-14T08:35:23Z
- **Completed:** 2026-10-14T08:37:19Z
- **Tasks:** 3
- **Files modified:** 7

## Accomplishments

- Added a narrow `Runner.SetEventsTTL` operation that validates both the database identifier and the 1..3650-day range before issuing fixed, database-qualified ClickHouse DDL.
- Made the migration CLI resolve `--events-ttl-days` over `SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS` over the seven-day default, and apply TTL even when schema migrations are already up to date.
- Proved the operator flow with unit and real ClickHouse coverage for no-op migrations, repeated updates, alternate databases, invalid input, sanitized errors, and unchanged migration history.
- Documented asynchronous TTL behavior, the deliberate absence of automatic `MATERIALIZE TTL`, and the read-time uniqueness limits of physical duplicate storage.

## Task Commits

Each task was committed atomically:

1. **Task 1: Prove retention updates after a no-op migration** - `a78eb26` (test)
2. **Task 2: Implement validated retention operation and make forwarding** - `ac17168` (feat)
3. **Task 3: Document retention and repeatability** - `37e36dd` (docs)

## Files Created/Modified

- `backend/internal/chmigrate/chmigrate.go` - Validated, fixed-statement `SetEventsTTL` operation scoped to the runner database.
- `backend/internal/chmigrate/chmigrate_test.go` - TTL range, database injection, HTTP failure sanitization, and context cancellation tests.
- `backend/tools/chmigrate/main.go` - Flag/env/default precedence and unconditional post-migration TTL application.
- `backend/tools/chmigrate/main_test.go` - CLI precedence, invalid-input no-request behavior, and no-op migration TTL coverage.
- `backend/test/migrate_test.go` - Real ClickHouse acceptance for alternate-database, idempotent 14-day TTL changes with stable migration history.
- `Makefile` - Plain retention environment forwarding through the existing dotenv-or-environment helper.
- `docs/event-storage.md` - Operator retention workflow and duplicate-visibility contract.

## Decisions Made

- Mutable retention policy is kept out of versioned migration history: `002_events.sql` remains the immutable seven-day baseline, while the tool applies the current operator choice after migrations.
- TTL DDL is intentionally narrow rather than accepting arbitrary SQL; only a validated identifier and bounded decimal integer enter the fixed statement.
- The tool does not run `MATERIALIZE TTL`, because changing the rule need not synchronously rewrite existing parts or create uncontrolled load.

## Verification Evidence

- `cd backend && go test -tags=integration ./test/... -run 'TestEventsTTLConfigurable|TestMigrations' -count=1` - passed against real ClickHouse in 25.077s after the human-action checkpoint was resolved.
- `cd backend && go test -race ./internal/chmigrate/... ./tools/chmigrate/... -timeout 60s` - passed; both packages completed in approximately 1.0s.
- `make lint test` - passed: backend golangci-lint reported 0 issues, all Go race tests passed, web ESLint/typecheck and 6 Vitest tests passed, and generator Ruff/format/mypy plus 37 pytest tests passed.

## Security Evidence

- Invalid TTL values and database identifiers fail before any ClickHouse request is sent.
- Credentials remain in the environment and HTTP Authorization header; errors do not expose passwords, backend bodies beyond the existing sanitized bound, or arbitrary SQL.
- The existing two-minute tool context and bounded HTTP client govern migration plus retention operations.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

- Closeout paused at a human-action checkpoint because filesystem and git access were unavailable. After full access was restored, the previously blocked real integration command passed in 25.077s and closeout continued without reimplementing or amending any task.

## User Setup Required

None - operators can set the documented optional environment value in their existing private `.env` file.

## Known Stubs

None in the seven plan-owned files. Existing out-of-scope placeholder targets elsewhere in the shared `Makefile` were not introduced or changed by this plan.

## Threat Flags

None. The only new trust-boundary operation is the planned, validated ClickHouse TTL DDL covered by the plan threat model; no new network endpoint, authentication path, file access pattern, or schema boundary was introduced.

## Next Phase Readiness

- Plan 02-06 can persist event-writer batches against the same events table without changing the default migration history.
- Plan 02-12 can expose the optional retention environment in local stack configuration, as intentionally deferred by the plan dependency graph.
- No blockers remain for the next Phase 2 plan.

## Self-Check: PASSED

- All seven plan-owned files exist.
- Commits `a78eb26`, `ac17168`, and `37e36dd` exist in history in the expected test, feature, and documentation order.
- Targeted race tests, the recorded real integration test, and the full lint/test gate all passed.
- No blocking stubs or unplanned threat surfaces were found.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-14*
