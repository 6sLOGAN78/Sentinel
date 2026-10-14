---
phase: 02-ingestion-durable-event-storage
plan: "07"
subsystem: event-storage
tags: [kafka, franz-go, dlq, clickhouse, offset-commits]

requires:
  - phase: 02-ingestion-durable-event-storage
    provides: manual commit-after-insert event writer from plan 02-06
provides:
  - bounded all-ISR poison record publishing to security.events.dlq.v1
  - partition-contiguous source commits after ClickHouse or DLQ durability
  - real Kafka failure/recovery acceptance for missing DLQ topics
affects: [event-writer, operations, replay, phase-03-detection]

tech-stack:
  added: []
  patterns: [stable poison reason enums, bounded quarantine context, per-partition durable prefixes]

key-files:
  created:
    - backend/internal/kafkax/dlq.go
  modified:
    - backend/internal/eventwriter/writer.go
    - backend/cmd/event-writer/main.go
    - backend/internal/config/config.go
    - backend/test/writer_test.go
    - docs/event-storage.md

key-decisions:
  - "Insert valid events before publishing poison records so ClickHouse failures never create DLQ traffic."
  - "Commit each partition only through its own contiguous durable prefix; a failed partition does not block safe commits in another partition."
  - "Bound each DLQ record to the configured value cap plus an 8 KiB metadata allowance and reject untrusted dlq.* header overrides."

patterns-established:
  - "Poison classification uses only decode_error, validation_error, and missing_received_at."
  - "DLQ acknowledgement is a durable effect equivalent to a successful ClickHouse insert for source-offset advancement."

requirements-completed: [STOR-04, STOR-01]

duration: 22min
completed: 2026-10-14
---

# Phase 02 Plan 07: Durable Poison Quarantine Summary

**Bounded all-ISR DLQ publishing with fixed reason metadata and partition-contiguous source commits that remain replay-safe under Kafka or ClickHouse failure**

## Performance

- **Duration:** 22 min
- **Started:** 2026-10-14T12:54:31Z
- **Completed:** 2026-10-14T13:16:11Z
- **Tasks:** 3
- **Files modified:** 10

## Accomplishments

- Added an independent, bounded franz-go DLQ producer that preserves small source context, redacts oversized values, caps copied headers, and waits for all-ISR acknowledgement.
- Reworked writer effects into per-partition durable prefixes, preventing later records from committing past a failed poison while allowing unrelated partitions to advance safely.
- Proved missing-DLQ-topic failure and recovery with a dedicated real Kafka cluster with auto-creation disabled, alongside the existing real ClickHouse insert-failure regression.
- Documented reason enums, access controls, recovery ordering, and at-least-once DLQ duplicate expectations.

## Task Commits

Each task was committed atomically:

1. **Task 1: Write poison and failed-DLQ offset acceptance** - `3a87753` (test)
2. **Task 2: Publish bounded quarantine records and commit contiguous success** - `1fc7e49` (feat)
3. **Task 3: Document DLQ inspection and replay guarantees** - `241828f` (docs)

Review fixes:

- `91ac0c5` (fix) - expose the defaulted DLQ topic through runtime configuration
- `b6b6726` (refactor) - satisfy writer complexity, error, comment, and test-isolation quality gates

## Files Created/Modified

- `backend/internal/kafkax/dlq.go` - Builds bounded quarantine records and synchronously publishes them with an independent all-ISR producer.
- `backend/internal/kafkax/dlq_test.go` - Covers exact origin metadata, stable reasons, oversized-value redaction, and header-flood bounds.
- `backend/internal/eventwriter/writer.go` - Classifies poison, persists valid events, publishes DLQ effects, and calculates independent durable commit prefixes.
- `backend/internal/eventwriter/writer_test.go` - Covers fixed reason classification, failed poison boundaries, and mixed-partition independence.
- `backend/test/writer_test.go` - Exercises poison round trips and missing-topic recovery using real Kafka and ClickHouse dependencies.
- `backend/cmd/event-writer/main.go` - Owns the DLQ producer lifecycle and injects it into the writer.
- `backend/internal/config/config.go` - Adds the defaulted, required Kafka DLQ topic setting.
- `backend/internal/config/config_test.go` - Locks the production DLQ topic default.
- `.env.example` - Documents the DLQ topic override.
- `docs/event-storage.md` - Documents inspection sensitivity, bounded encoding, offset ordering, and replay procedures.

## Decisions Made

- Valid records are inserted before any poison is published from a mixed batch. This guarantees that a ClickHouse outage remains an infrastructure failure and never produces DLQ records.
- DLQ failures stop the worker after committing only already-durable prefixes. Later effects in the failed partition remain replayable, while healthy partitions may commit independently.
- Source `dlq.*` headers are not copied, so untrusted records cannot shadow trusted reason and origin metadata.
- The DLQ topic defaults to `security.events.dlq.v1` and can be overridden with `SENTINEL_KAFKA.DLQ_TOPIC` for isolated deployments and tests.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Wired the DLQ producer into the production event-writer lifecycle**
- **Found during:** Task 2
- **Issue:** The plan's file list omitted `backend/cmd/event-writer/main.go`; implementing only the library would leave the production writer unable to quarantine poison.
- **Fix:** Constructed, injected, and lifecycle-closed the independent DLQ producer.
- **Files modified:** `backend/cmd/event-writer/main.go`
- **Verification:** Event-writer package build, full unit suite, and real poison integration passed.
- **Committed in:** `1fc7e49`

**2. [Rule 3 - Blocking] Used franz-go's minimum valid record delivery timeout in the failure fixture**
- **Found during:** Task 2 real integration verification
- **Issue:** franz-go rejects a 500 ms `RecordDeliveryTimeout`; the test fixture failed before exercising missing-topic behavior.
- **Fix:** Set the Kafka delivery timeout to one second while retaining the writer's 500 ms operation context as the effective attempt bound.
- **Files modified:** `backend/test/writer_test.go`
- **Verification:** `TestWriterDLQFailureNoCommit` failed on the absent topic, recovered after topic creation, and passed.
- **Committed in:** `1fc7e49`

**3. [Rule 2 - Missing Critical] Added production DLQ topic configuration**
- **Found during:** Final self-review
- **Issue:** The constructor supported explicit test topics, but production had no configuration path despite the plan requiring the default unless explicitly configured.
- **Fix:** Added `KafkaConfig.DLQTopic`, its default, environment template entry, config test, and runtime wiring.
- **Files modified:** `backend/internal/config/config.go`, `backend/internal/config/config_test.go`, `backend/cmd/event-writer/main.go`, `.env.example`
- **Verification:** `TestLoadEventWriterDefaults` was RED before the field existed and GREEN after wiring; full regression gates passed.
- **Committed in:** `91ac0c5`

---

**Total deviations:** 3 auto-fixed (2 missing critical, 1 blocking)
**Impact on plan:** All changes were required to make the planned DLQ behavior executable, configurable, and testable in production; no feature scope was added.

## Issues Encountered

- The first `make lint` run reported plan-owned cognitive complexity, static-error, exported-comment, and test-package issues. The pipeline was split into focused stages, tests were externalized, and the second lint fix attempt produced a clean full lint run (`b6b6726`).

## Verification Evidence

- `cd backend && go test -race ./internal/eventwriter/... ./internal/kafkax/... -timeout 60s -count=1` - passed.
- `cd backend && go test ./internal/config/... ./cmd/event-writer/... -count=1` - passed.
- `cd backend && go test -tags=integration ./test/... -run 'TestWriterPoisonToDLQ|TestWriterDLQFailureNoCommit|TestWriterInsertFailureNoCommit' -timeout 4m -p 1 -count=1` - passed against real Kafka and ClickHouse dependencies.
- `make lint` - passed backend, web, and generator quality gates with zero issues.
- `make test` - passed the complete backend race suite, 6 web tests, and 37 generator tests.

## Known Stubs

None.

## User Setup Required

None - the existing topic initialization already creates `security.events.dlq.v1`; optional overrides use `SENTINEL_KAFKA.DLQ_TOPIC`.

## Next Phase Readiness

- The event writer now safely drains poison without losing source ordering and is ready for later detection consumers to reuse the manual-commit discipline.
- No blockers remain for plan 02-08 or subsequent Phase 2 work.

## Self-Check: PASSED

- All key implementation, test, documentation, and summary files exist.
- All five task and review-fix commits are present in git history.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-14*
