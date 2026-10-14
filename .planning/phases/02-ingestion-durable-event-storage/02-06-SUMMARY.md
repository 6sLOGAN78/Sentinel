---
phase: 02-ingestion-durable-event-storage
plan: "06"
subsystem: event-writer
tags: [kafka, clickhouse, manual-offsets, retries, lifecycle]

requires:
  - phase: 02-ingestion-durable-event-storage
    provides: Native ClickHouse event storage and authenticated durable Kafka ingestion
provides:
  - Bounded Kafka-to-ClickHouse event writer with commit-after-insert ordering
  - Replay-safe persisted-event validation and partition-contiguous manual commits
  - Fatal worker failure propagation through orderly lifecycle shutdown
affects: [02-07-dlq, 02-08-writer-config, 02-09-operations, replay-recovery]

tech-stack:
  added: []
  patterns: [manual Kafka offsets, bounded retry loop, cancellation-cause lifecycle, dedicated fault containers]

key-files:
  created:
    - backend/internal/eventwriter/writer.go
    - backend/internal/eventwriter/writer_test.go
    - backend/internal/kafkax/consumer.go
    - backend/internal/kafkax/consumer_test.go
    - backend/test/writer_test.go
  modified:
    - .env.example
    - backend/cmd/event-writer/main.go
    - backend/internal/app/app.go
    - backend/internal/app/app_test.go
    - backend/internal/config/config.go
    - backend/internal/config/config_test.go
    - backend/internal/testing/containers.go

key-decisions:
  - "Disable Kafka auto-commit and commit only the last contiguous record per partition after ClickHouse InsertBatch succeeds."
  - "Validate replayed events relative to their immutable occurred_at timestamp while still requiring nonzero occurred_at and received_at."
  - "Preserve the first background failure through cleanup and bound consumer close, rebalance processing, fetch memory, retries, and shutdown."

requirements-completed: [STOR-01, ING-04]

duration: 7 min
completed: 2026-10-14
---

# Phase 2 Plan 06: Durable Event Writer Summary

**A bounded manual-offset Kafka worker now persists canonical events to ClickHouse before committing source offsets, replays safely after storage failure, and exits nonzero on fatal background errors.**

## Performance

- **Duration:** 7 min recovery and closeout window
- **Started:** 2026-10-14T12:39:43Z
- **Completed:** 2026-10-14T12:46:43Z
- **Tasks:** 3
- **Files modified:** 12

## Accomplishments

- Added a bounded event-writer loop that flushes on size or wall-clock interval, retries ClickHouse inserts without polling more Kafka data, and commits only successfully persisted partition-contiguous offsets.
- Configured franz-go with explicit manual commits, blocked rebalances while records are owned, bounded fetch concurrency and memory, and context-bounded close behavior.
- Validated persisted events without reapplying wall-clock ingestion skew, while redacting untrusted values from writer failures.
- Propagated the first fatal worker error through cancellation-cause lifecycle shutdown so supervisors receive a nonzero exit even when the failure arrives during cleanup.
- Proved commit ordering, unchanged offsets during exhausted storage retries, and successful replay using real Kafka and a dedicated pausable ClickHouse container.

## Task Commits

Each implementation stage and recovery refinement was committed atomically:

1. **Task 1: Write real commit-order and storage-failure acceptance** - `545e10b` (test)
2. **Task 2: Run bounded native batches with manual offsets** - `8387380` (feat)
3. **Task 3: Propagate worker failure and flush within shutdown deadline** - `a7812fc` (feat)
4. **Plan quality refactor** - `a2835d2` (refactor)
5. **Recovery refinement: harden shutdown, redaction, and resource bounds** - `9b2d21b` (fix)
6. **Recovery refinement: bound real-dependency consumer cleanup** - `a83622d` (test)
7. **Recovery refinement: satisfy final lint gates** - `bb7051b` (refactor)

## Files Created/Modified

- `backend/internal/eventwriter/writer.go` - Bounded poll/decode/insert/commit loop with retry and shutdown flush behavior.
- `backend/internal/kafkax/consumer.go` - Manual group consumer, bounded fetches, dynamic rebalance budget, and deadline-aware close.
- `backend/internal/app/app.go` - First-error lifecycle failure propagation and nonzero shutdown result.
- `backend/cmd/event-writer/main.go` - Real ClickHouse store, Kafka consumer, worker goroutine, and ordered lifecycle wiring.
- `backend/internal/config/config.go` and `.env.example` - Validated writer defaults and hard upper bounds.
- `backend/internal/app/app_test.go`, `backend/internal/config/config_test.go`, `backend/internal/eventwriter/writer_test.go`, and `backend/internal/kafkax/consumer_test.go` - Race-safe lifecycle, bounds, retry, redaction, and shutdown coverage.
- `backend/test/writer_test.go` and `backend/internal/testing/containers.go` - Real commit-order, failure, process-exit, and replay acceptance with dedicated fault injection.

## Decisions Made

- Manual source offsets are part of the storage transaction boundary: ClickHouse insertion completes before any next offset is committed.
- Rebalance ownership remains blocked across decode, insertion retries, and commit; it is released only after the batch finishes or fails.
- A replay uses `occurred_at` as the validation clock, because ingestion already enforced wall-clock skew and old acknowledged records must remain recoverable.
- Storage failures are fatal worker failures, never DLQ records; poison-record handling remains scoped to 02-07.
- Consumer shutdown uses the lifecycle context, and the broker rebalance deadline covers maximum insert attempts, retry waits, and the final commit operation.

## Verification Evidence

- `cd backend && go test -race ./internal/app/... ./internal/eventwriter/... ./internal/kafkax/... -timeout 60s` - passed.
- `cd backend && go test -race ./internal/eventwriter/... ./internal/kafkax/... ./internal/config/... -timeout 60s` - passed.
- `cd backend && go test -race ./internal/app/... ./internal/eventwriter/... -timeout 60s` - passed.
- `cd backend && go test -tags=integration ./test/... -run 'TestWriterCommitAfterInsert|TestWriterInsertFailureNoCommit' -timeout 4m -p 1 -count=1` - passed twice against real Kafka/ClickHouse; the final recorded run completed in 36.092s.
- `cd backend && go test -tags=integration ./test/... -run 'TestWriter|TestLifecycle' -timeout 4m -p 1 -count=1` - passed against real dependencies in 54.202s.
- `cd backend && go build ./... && go vet ./...` - passed.
- `make lint test` - passed: backend golangci-lint reported 0 issues; all Go race tests passed; web ESLint, typecheck, and 6 Vitest tests passed; generator Ruff, format, mypy, and 37 pytest tests passed.

## Security Evidence

- Auto-commit is explicitly disabled, and exhausted or cancelled ClickHouse inserts cannot advance source offsets.
- Kafka fetch bytes, per-partition bytes, concurrent fetches, aggregate fetch memory, batch size, retry attempts, operation time, shutdown time, and future DLQ record size are all bounded.
- Persisted-event errors retain field/location diagnostics without logging raw untrusted values or payloads.
- Fatal storage errors retain the first cause and result in exit status 1 after bounded cleanup.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Preserved failures discovered during lifecycle cleanup**
- **Found during:** Recovery verification of Task 3
- **Issue:** `RunContext` captured the background failure before closing workers, so a failure reported by a closer could be mistaken for a clean shutdown.
- **Fix:** Re-read the lifecycle failure after cleanup and retain nonzero exit status.
- **Files modified:** `backend/internal/app/app.go`, `backend/internal/app/app_test.go`
- **Verification:** App race tests and `TestFailureReportedDuringCleanupExitsNonZero` passed.
- **Commit:** `9b2d21b`

**2. [Rule 2 - Missing Critical Functionality] Bounded writer memory, consumer close, and rebalance work**
- **Found during:** Recovery verification of Tasks 2 and 3
- **Issue:** Writer fetch/DLQ settings had no hard upper bounds, aggregate fetch memory was unchecked, and consumer close could outlive lifecycle shutdown.
- **Fix:** Added validated ceilings, a 128 MiB aggregate fetch cap, processing-derived rebalance timeout, and idempotent context-bounded close.
- **Files modified:** `backend/internal/config/config.go`, `backend/internal/config/config_test.go`, `backend/internal/kafkax/consumer.go`, `backend/internal/kafkax/consumer_test.go`, `backend/cmd/event-writer/main.go`
- **Verification:** Targeted race tests, full lint/test, build, and vet passed.
- **Commit:** `9b2d21b`

**3. [Rule 1 - Security] Redacted untrusted persisted-event values**
- **Found during:** Recovery verification of Task 2 threat mitigation T-02-06-4
- **Issue:** Contract validation errors could include an attacker-controlled enum value in fatal logs.
- **Fix:** Return stable `decode_error` / `validation_error` classifications with record location and safe field name only.
- **Files modified:** `backend/internal/eventwriter/writer.go`, `backend/internal/eventwriter/writer_test.go`
- **Verification:** `TestWriterValidationErrorRedactsUntrustedValue` and the event-writer race suite passed.
- **Commit:** `9b2d21b`

**4. [Rule 3 - Blocking] Updated real-dependency cleanup for the bounded Close API**
- **Found during:** First integration verification run
- **Issue:** Three acceptance-test call sites still used the former zero-argument consumer close and failed to compile.
- **Fix:** Added a five-second test cleanup helper and passed explicit contexts at every writer-consumer close site.
- **Files modified:** `backend/test/writer_test.go`
- **Verification:** Both real commit-order/failure cases passed after the update.
- **Commit:** `a83622d`

**5. [Rule 1 - Quality] Restored repository lint compliance**
- **Found during:** Full `make lint test`
- **Issue:** Writer bounds pushed shared config validation over its cognitive-complexity limit, and the intentional white-box consumer test lacked its package-lint rationale.
- **Fix:** Extracted `validateWriterConfig` and documented the narrow white-box test exception.
- **Files modified:** `backend/internal/config/config.go`, `backend/internal/kafkax/consumer_test.go`
- **Verification:** `make lint test` passed with 0 backend lint issues and all project tests green.
- **Commit:** `bb7051b`

**Total deviations:** 5 auto-fixed (3 bugs/quality issues, 1 missing critical bound, 1 blocking call-site update). **Impact:** All changes tighten correctness, security, and bounded shutdown without expanding the planned architecture.

## Issues Encountered

- The first integration invocation exposed obsolete consumer-close call sites; they were corrected with explicit deadlines and the real dependency tests then passed.
- The first full lint invocation exposed one complexity and one test-package issue; both were corrected before closeout.

## User Setup Required

None - writer settings have safe defaults and optional overrides are documented in `.env.example`.

## Known Stubs

None. The empty slices and strings found by the mechanical scan are normal initialized collections/config checks, and `.env.example` intentionally contains placeholder-only local values; no plan-goal behavior is stubbed.

## Threat Flags

None. The Kafka consumer, ClickHouse writer, failure propagation, and configuration boundaries are the planned threat surfaces covered by T-02-06-1 through T-02-06-4; no additional endpoint, authentication path, file-access boundary, or schema change was introduced.

## Next Phase Readiness

- Plan 02-07 can add poison-record DLQ handling on top of the bounded consumer while keeping storage failures fatal and uncommitted.
- Manual offset ownership, safe rebalance release, replay validation, and future DLQ record-size bounds are in place.
- No blockers remain for 02-07.

## Self-Check: PASSED

- All 12 plan-owned files exist.
- Commits `545e10b`, `8387380`, `a7812fc`, `a2835d2`, `9b2d21b`, `a83622d`, and `bb7051b` exist in history.
- Exact task race tests, real Kafka/ClickHouse acceptance, build, vet, lint, and full project tests passed.
- Acceptance links confirm explicit manual commits, insert-before-commit ordering, bounded polling/retries/cleanup, and late-failure exit propagation.
- No blocking stubs or unplanned threat surfaces were found.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-14*
