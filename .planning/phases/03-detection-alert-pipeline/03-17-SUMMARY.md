---
phase: 03-detection-alert-pipeline
plan: "17"
subsystem: database
tags: [go, clickhouse, postgres, replay, native-driver, tdd]
requires:
  - phase: 03-02
    provides: Real CH-to-PG persistence and resolved-workflow replay acceptance
provides:
  - Extracted native immutable ClickHouse alert store and bounded PostgreSQL workflow store
  - Independent fresh-batch, tenant-query and writer effect-order preservation tests
affects: [03-10, 03-11, 03-18, 03-19, phase-4]
tech-stack:
  added: []
  patterns: [fresh Abort-safe native blocks, bounded global-ID transaction lock, insert-only workflow replay]
key-files:
  created:
    - backend/internal/storage/clickhouse/alerts_test.go
    - backend/internal/alertwriter/writer_test.go
    - backend/internal/storage/clickhouse/alerts.go
    - backend/internal/storage/postgres/alerts.go
  modified:
    - backend/internal/alertwriter/writer.go
    - backend/cmd/alert-writer/main.go
key-decisions:
  - "03-17: Extract native stores without changing global alert-ID preflight lock, immutable winner, CH acknowledgement, insert-only PG commit or durable source-prefix order."
  - "03-17: Keep fingerprint/error identity shared through the immutable storage adapter with writer compatibility helpers; storage accepts contracts.Alert and never imports the writer."
requirements-completed: [ALRT-01, ALRT-02]
duration: ~6 min
completed: 2026-10-18
---

# Phase 3 Plan 17: Native Alert Store Extraction Summary

Native ClickHouse evidence and PostgreSQL workflow adapters now live in storage packages, preserving acknowledged CH → insert-only PG → Kafka source commit and unchanged resolved workflows.

## Performance

- Duration: approximately 6 minutes including context loading and verification.
- First proof commit: 2026-10-18T22:28:39Z.
- Implementation verified and committed: 2026-10-18T22:31:17Z (2026-10-19 local date).
- Tasks: 2/2; application/test files: 6.

## Accomplishments

- Added loadable, error-returning store declarations before new behavior tests. Fresh-batch and tenant-query selectors compiled and failed assertions against the declarations. Existing writer ordering passed before extraction; no old-runtime failure was manufactured.
- Moved the cohesive native helpers out of the writer. Main explicitly injects `clickhouse.NewAlertStore` and `postgres.NewAlertStore`; dependencies remain the pinned native driver and pgx pool. The writer keeps its existing interfaces, immutable-fingerprint helper and conflict identity without reverse imports or cycles.
- Each nonempty CH attempt prepares a fresh batch, aborts unsent blocks after append/send/nil-input errors, and acknowledges successful sends. Reads bind tenant/ID/time values, aggregate immutable variants/unique alert IDs across all parts immediately, and fail closed on inconsistent retained content. No TTL, FINAL, OPTIMIZE, migration or merge dependency was added.
- PG begins under a bounded pool-acquisition context and keeps transaction-local lock/statement/idle/transaction limits. The global alert-ID advisory lock still covers authoritative PG winner and retained CH orphan checks through CH acknowledgement, insert-only Ensure and PG commit. Every failure after Begin has an explicit rollback under an independent one-second context; cancellation alone is not treated as transaction cleanup.
- The final writer tests exercise public Run, preserving success ordering and source retention on CH, PG, PG-commit and preflight failures. The PG-failure case waits for operation-context expiry, then proves cleanup receives a fresh live deadline. The real blocked-PG pipeline and complete-row resolved replay both pass after extraction.

## Task Commits

1. **Task 1: Prove fresh native batches and writer effect order** — `362e6c5` (`test`).
2. **Task 2: Extract parameterized stores and wire main** — `475cbd1` (`refactor`, GREEN extraction).

## Recorded Test Results

Each named selector ran separately and nonempty. Unit selectors used `-timeout 60s`; real selectors used `-tags=integration -timeout 6m -p 1`. Go package/test durations below are the actual reported durations, not estimates of end-to-end shell wall time.

| Command | Actual result |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/storage/clickhouse/...' '^TestAlertStoreFreshBatch$' -timeout 60s` before extraction | Assertion RED, exit 1, package 0.005s. All three cases compile and report expected PrepareBatch count 1 versus actual 0. |
| `scripts/go-test-nonempty.sh './internal/storage/clickhouse/...' '^TestAlertStoreTenantQueries$' -timeout 60s` before extraction | Assertion RED, exit 1, package 0.005s. Detail/count reject the declaration's error; absence mapping assertion also fails. |
| `scripts/go-test-nonempty.sh './internal/alertwriter/...' '^TestAlertWriterOrdering$' -timeout 60s` before extraction | Existing behavior characterization PASS, exit 0, package 0.005s; five independent success/failure cases. |
| Same FreshBatch command, final code | GREEN, exit 0, package 0.006s; fresh retry batch, Abort on unsent errors, successful Send and empty-input no-allocation. |
| Same TenantQueries command, final code | GREEN, exit 0, package 0.006s; bound tenant/ID/since, exact identity aggregation SQL, retained conflict and not-found mapping. |
| Same WriterOrdering command, final public-Run tests | PASS, exit 0, package 0.032s; operation-expiry case 0.03s with independently live rollback. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterPipeline$' -tags=integration -timeout 6m -p 1` | PASS, exit 0, test 1.30s/package 20.243s; CH visible while PG INSERT blocked and source uncommitted, followed by both sinks/source completion. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWorkflowReplay$' -tags=integration -timeout 6m -p 1` | PASS, exit 0, test 1.45s/package 18.544s; entire resolved row remains equal, including assignee, version, seq and timestamps. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertsSchema$' -tags=integration -timeout 6m -p 1` | PASS, exit 0, test 0.24s/package 17.538s; real independent schema constraints/indexes. |
| `timeout 300s make test` | PASS, exit 0; complete backend Go race suite, web 6 tests, generator 99 tests. |
| `cd backend && go test -race ./internal/alertwriter ./internal/storage/... -timeout 60s` after final test changes | PASS, exit 0; writer 1.042s, CH 1.017s; PG compiles (real behavior covered by pipeline/replay). |
| `cd backend && go build ./... && go vet -tags=integration ./...` | PASS, exit 0. |
| `make lint` | Final PASS, exit 0; Go 0 issues plus web lint/typecheck and generator checks. Initial exit 2 found only owned-test formatting and internal-test-package convention issues; repaired before final verification. |
| `cd backend && go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | Exit 1; sole existing out-of-scope `test/recovery_test.go:162` unparam warning. No clean integration-lint claim. |
| `git diff --check` | PASS, exit 0. |

The query-driver tests prove the actual emitted aggregate SQL and parameter boundary; they do not claim a new real adversarial cross-partition/conflict scenario. Existing real pipeline/replay acceptance ran against the extracted implementations. Plan 03-10 retains adversarial concurrency/orphan/ambiguous-failure proofs.

## Decisions Made

Preserved the accepted protocol and existing SQL verbatim during extraction. The immutable storage adapter owns the fingerprint implementation and shared conflict sentinel; writer compatibility helpers preserve callers while both stores use neutral `contracts.Alert` values. PostgreSQL guidance and pgx documentation reaffirmed explicit bounded rollback after cancellation; no speculative interface or schema layer was introduced.

## Deviations from Plan

**1. [Rule 2 - Required convention] Move writer tests to the external test package.**
- Found during Task 2 standard lint: `testpackage` requires `alertwriter_test`; initial scaffolding exercised the private process boundary.
- Fix: exercise public Run with a finite consumer instead, and make the PG-failure fixture expire only the operation context. Wrapped owned long lines without weakening lint policy.
- Files: `backend/internal/alertwriter/writer_test.go`, `backend/internal/storage/clickhouse/alerts_test.go`.
- Verification: final named ordering and native selectors, affected race tests and standard lint all pass.
- Commit: `475cbd1`.

Total deviations: one scoped convention/test-boundary correction; no production protocol or feature expansion.

## Deferred Issues and Remaining Dependencies

- Existing integration-tag lint `backend/test/recovery_test.go:162` unparam remains the recorded 03-14 obligation; unchanged.
- 03-10 owns adversarial immutable conflict/concurrent replay/retained orphans/ambiguous acknowledgements. 03-11 owns poison quarantine and aggregate-prefix strengthening. 03-19 owns durable cross-window cooldown. Full Phase 3 integration/security/CI and live scheduling proofs remain later work.
- ALRT-01/ALRT-02 above identify this plan's slice; global requirement checkboxes remain pending. This extraction does not certify the full requirements or Phase 3.

## User Setup Required

None. No installation, authentication gate, remote write or user-volume/key change occurred. Existing config/.serena edits were preserved and excluded from commits.

## Known Stubs and Threat Surface

No implementation stub remains. Empty fingerprint status and nil detected_at are deliberate exclusion of mutable status and wall-clock detection time. No new endpoint, auth path, schema, file access or unplanned trust boundary was introduced. Registered threats retain parameter binding, immutable winner locking and insert-only replay protections; no new open high threat was found in the extraction.

## Self-Check: PASSED

- All six planned application/test files exist.
- `362e6c5` and `475cbd1` exist; neither commit deletes tracked files.
- Assertion-level adapter RED precedes extraction; all six final named selectors pass nonempty.
- Stub/TODO/FIXME scan is clear for implementation files; focused goal has no unwired dependency.
- Standard lint/build/vet and full tests pass; the existing integration-lint exception is recorded explicitly.
- SUMMARY is committed before serialized STATE/ROADMAP updates; Phase 3 remains in execution.
