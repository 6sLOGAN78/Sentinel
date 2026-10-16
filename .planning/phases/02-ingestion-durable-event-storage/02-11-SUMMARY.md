---
phase: 02-ingestion-durable-event-storage
plan: "11"
subsystem: recovery-testing
tags: [go, kafka, clickhouse, crash-recovery, failpoints, integration-testing, deduplication]

requires:
  - phase: 02-09
    provides: observable service lifecycle and drain ordering
  - phase: 02-10
    provides: deterministic retrying event generation
provides:
  - Compile-isolated barriers at post-insert/pre-commit and acknowledged-prefix boundaries
  - Real SIGKILL replay proof with offset and complete event-ID assertions
  - Real partial-publication 503 and byte-identical retry proof without visible duplicates
affects: [02-12, 02-13, ingestion-recovery, event-writer-recovery]

tech-stack:
  added: []
  patterns: [integration-build fault barriers, causal file handshakes, protocol-state recovery waits]

key-files:
  created:
    - backend/internal/eventwriter/failpoint.go
    - backend/internal/eventwriter/failpoint_integration.go
    - backend/internal/kafkax/failpoint.go
    - backend/internal/kafkax/failpoint_integration.go
    - backend/test/recovery_test.go
  modified:
    - backend/internal/eventwriter/writer.go
    - backend/internal/kafkax/producer.go
    - backend/test/ingest_test.go
    - backend/test/writer_test.go

key-decisions:
  - "Compile failure controls only with the integration tag; default builds contain unconditional no-ops and read no failure environment settings."
  - "Use private once-only file barriers to prove the exact durable boundary before pausing or killing test-owned resources."
  - "Treat Kafka topic-leader metadata, not broker-only Ping, as producer readiness after an outage."

patterns-established:
  - "Crash tests wait through Kafka's real consumer-session expiry rather than assuming immediate group reassignment after SIGKILL."
  - "Ambiguous retry tests recover the ingest process on the same explicit topic and retry identical serialized bytes exactly once."

requirements-completed: [ING-04, STOR-01, STOR-03, ING-07]

duration: 20 min
completed: 2026-10-16
---

# Phase 02 Plan 11: Causal Crash and Ambiguous-Publish Recovery Summary

**Integration-only durable-boundary barriers now prove that writer death and partial Kafka publication preserve every acknowledged event ID without visible duplicates.**

## Performance

- **Duration:** 20 min
- **Started:** 2026-10-16T02:05:30Z
- **Completed:** 2026-10-16T02:25:04Z
- **Tasks:** 3
- **Files modified:** 9

## Accomplishments

- Added compile-isolated, once-only barriers after real ClickHouse insertion/before Kafka commit and after a real acknowledged Kafka prefix/before remaining publication.
- Proved a real event-writer SIGKILL leaves offsets unchanged, then replays physical effects after the Kafka session timeout while every acknowledged ID remains immediately queryable once.
- Proved a paused broker causes bounded sanitized 503/Retry-After, followed by a byte-identical same-topic retry that creates expected physical duplicates but exactly N visible IDs without `OPTIMIZE`.

## Task Commits

Each task was committed atomically:

1. **Task 1: Expose causal failure barriers in integration builds only** - `0afc080` (feat)
2. **Task 2: Prove writer crash and restart preserve every acknowledged ID** - `4ffc1de` (test)
3. **Task 3: Prove timeout and partially published identical retry behavior** - `d1d9ed3` (test)

## Files Created/Modified

- `backend/internal/eventwriter/failpoint*.go` - Default no-op and integration-only post-insert/pre-commit barrier.
- `backend/internal/kafkax/failpoint*.go` - Default no-op and integration-only acknowledged-prefix publication barrier.
- `backend/internal/eventwriter/writer.go` - Invokes the barrier only after a successful durable insert.
- `backend/internal/kafkax/producer.go` - Invokes prefix publication control and verifies usable topic-leader metadata in readiness.
- `backend/test/recovery_test.go` - Pre-insert kill, post-insert SIGKILL, offset, replay, repeated restart, and per-ID recovery acceptance.
- `backend/test/ingest_test.go` - Dedicated-broker timeout, identical retry, physical duplicate, and immediate ClickHouse uniqueness acceptance.
- `backend/test/writer_test.go` - Offset polling tolerates transient coordinator recovery during bounded causal waits.

## Decisions Made

- Barriers use mode-0600 marker files under each test's private temporary directory; there is no HTTP control plane or production environment switch.
- A killed consumer is allowed up to 70 seconds for reassignment because the configured Kafka session timeout is 45 seconds.
- After an ambiguous producer timeout, the retry uses a fresh ingest instance on the same topic, matching process recovery while preserving identical request bytes and event IDs.
- Producer readiness now verifies metadata and leaders for its actual topic after broker protocol reachability succeeds.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Made crash recovery wait longer than the Kafka group session**
- **Found during:** Task 2 GREEN run
- **Issue:** The existing 20-second offset helper expired before Kafka's 45-second dead-member session timeout after SIGKILL.
- **Fix:** Added a crash-specific 70-second causal offset wait with child-process diagnostics.
- **Files modified:** `backend/test/recovery_test.go`
- **Verification:** Uncached `TestWriterCrashRestart` and `TestWriterInsertFailureNoCommit` passed together.
- **Committed in:** `4ffc1de`

**2. [Rule 1 - Bug] Preserved dedicated producer bounds in the ingest fixture**
- **Found during:** Task 3 timeout acceptance
- **Issue:** The fixture overwrote the dedicated Kafka config's one-second delivery timeout with the shared ten-second default.
- **Fix:** Preserve caller-supplied producer bounds and fill only zero-valued defaults.
- **Files modified:** `backend/test/ingest_test.go`
- **Verification:** `TestIngestKafkaTimeout503` returns 503 with Retry-After inside the scheduling margin.
- **Committed in:** `d1d9ed3`

**3. [Rule 1 - Bug] Waited for publish capability and transient coordinator recovery**
- **Found during:** Task 3 combined causal run
- **Issue:** Broker reachability recovered before topic publication and group coordination were ready.
- **Fix:** Require a real probe-topic publication, validate target-topic leaders in producer readiness, and treat transient coordinator errors as retryable within bounded offset polling.
- **Files modified:** `backend/internal/kafkax/producer.go`, `backend/test/ingest_test.go`, `backend/test/writer_test.go`
- **Verification:** The four-case uncached causal suite passed in 72.113 seconds.
- **Committed in:** `d1d9ed3`

---

**Total deviations:** 3 auto-fixed bugs.
**Impact on plan:** The fixes align waits with actual Kafka protocol state and preserve the intended causal evidence; no production failure control was exposed.

## Verification

- `go test -race ./internal/eventwriter/... ./internal/kafkax/... -timeout 60s` — passed.
- `go list -f '{{.GoFiles}}' ./internal/eventwriter ./internal/kafkax` — default files are `[failpoint.go writer.go]` and `[consumer.go dlq.go failpoint.go producer.go]`; integration controls are excluded.
- `go build ./cmd/...` — passed for default production binaries.
- `go test -tags=integration ./test/... -run 'TestWriterCrashRestart|TestWriterInsertFailureNoCommit' -timeout 5m -p 1 -count=1` — passed in 83.931 seconds.
- `go test -tags=integration ./test/... -run 'TestIngestKafkaTimeout503|TestIngestRetryNoDuplicates|TestIngestShedsUnderLoad|TestWriterCrashRestart' -timeout 6m -p 1 -count=1` — passed in 72.113 seconds.
- `gsd-sdk query verify.key-links .../02-11-PLAN.md` — 2/2 key links verified.
- `make lint test` — passed with zero lint issues, all Go race tests, web checks/tests, and 37 generator tests.

## Issues Encountered

- RED runs failed at the intended missing barriers before implementation.
- Kafka's real group and topic recovery states required causal protocol checks rather than fixed sleeps or broker-only reachability.

## User Setup Required

None - all failure controls are integration-build-only and private to the test harness.

## Next Phase Readiness

- Docker Compose can now package the verified ingestion/storage path in plan 02-12.
- End-to-end seed validation can rely on proven retry/replay semantics in plan 02-13.
- No unresolved recovery blocker remains.

## Self-Check: PASSED

- Confirmed all five created failpoint/recovery files exist.
- Confirmed commits `0afc080`, `4ffc1de`, and `d1d9ed3` exist in history.
- Confirmed default Go file lists exclude integration controls and production binaries build.
- Confirmed 2/2 key links, uncached causal tests, and the fresh full repository gate pass.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-16*
