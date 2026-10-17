---
phase: 03-detection-alert-pipeline
plan: "02"
subsystem: database
tags: [go, kafka, clickhouse, postgres, replay, tdd]
requires:
  - phase: 03-01
    provides: Canonical AUTH Kafka alerts, topic/group configuration and accepted immutable/workflow authority policy
provides:
  - Native alert-topic to ClickHouse evidence and PostgreSQL workflow persistence before consumed-next commit
  - Insert-only workflow replay preserving status, assignee, version, seq and timestamps
  - Additive alerts/workflow migrations and independently executed real schema/pipeline/replay proofs
affects: [03-17, 03-10, 03-11, 03-18, 03-19, phase-4]
tech-stack:
  added: []
  patterns: [per-alert advisory transaction lock, immutable fingerprint preflight, two-store durable source prefix]
key-files:
  created:
    - backend/test/alertwriter_pipeline_test.go
    - backend/test/alerts_schema_test.go
    - backend/migrations/clickhouse/003_alerts.sql
    - backend/migrations/postgres/003_alert_workflows.sql
    - backend/internal/alertwriter/writer.go
    - backend/internal/kafkax/alert_consumer.go
  modified:
    - backend/cmd/alert-writer/main.go
key-decisions:
  - "03-02: Per-alert transaction locks cover PG winner and retained CH orphan preflight through CH acknowledgement and insert-only PG commit."
  - "03-02: Preserve the complete first-pipe AUTH principal suffix and keep native store helpers cohesive until immediate 03-17 extraction."
patterns-established:
  - "Bound PG lock, statement, idle transaction, transaction and explicit rollback time; one aggregate effect/retry budget precedes source commit."
  - "Existing immutable content wins; replay does not update workflow state or allocate a replacement workflow seq."
requirements-completed: [ALRT-01, ALRT-02]
duration: 7 min
completed: 2026-10-17
---

# Phase 3 Plan 2: Two-Store Alert Persistence Summary

Canonical Kafka alerts now become immediately visible ClickHouse evidence and insert-only PostgreSQL workflows before the source offset advances; resolved replay preserves every workflow field.

## Performance

- Started: 2026-10-17T06:34:12Z
- Implementation completed: 2026-10-17T06:41:21Z
- Duration: 7 min
- Tasks: 2/2
- Application/test/migration files: 7

## Accomplishments

- Added database-relative immutable CH alert storage without an alert TTL, plus PG primary alert identity, unique bigint identity seq, workflow defaults, immutable list fields/fingerprint and all three tenant-list indexes. Signed bigint/Int64 rule/count storage preserves the positive Go contract range without narrowing overflow.
- The real alert consumer uses independent alerts topic/group settings through the existing bounded manual consumed-next adapter; event-writer behavior is unchanged.
- Writer validates strict alert/schema/tenant/source key and reconstructs the canonical ID using the unchanged DedupeKey primitive. AUTH preserves Unicode and every pipe after the first delimiter; SCAN accepts two IP components; volume rules accept one.
- A bounded per-ID PG advisory transaction lock serializes immutable winner/fingerprint preflight, retained CH orphan consistency, fresh native CH acknowledgement, insert-only PG ensure and PG commit. The source commits only the contiguous durable prefix. Failed sink/commit work remains replayable. Both native context limits and PG transaction-local lock/statement/idle/transaction timeouts apply; every unsuccessful transaction has an explicit one-second cleanup rollback.
- Fingerprints include immutable detection fields and ordered evidence while excluding status/detected_at. CH detail and counts use parameterized tenant/ID predicates and immediate identity aggregation across partitions, independent of merges. Fresh native blocks abort when unsent.
- Default alert-writer wiring now runs the concrete worker, retains real dependency readiness/lifecycle cleanup and reports sanitized terminal failures through lc.Fail.
- The pipeline test holds a real PG SHARE lock to block INSERT while CH evidence becomes visible, proves no workflow/source commit, then releases the lock and observes the complete durable result. The replay test resolves/assigns/versions the workflow and compares the entire row before/after identical Kafka bytes. Schema acceptance independently verifies unique alert_id and seq indexes and all tenant access indexes.

## Task Commits

1. **03-02-T01: Expose missing persistence and immutable workflow replay** — `366770b` (`test`)
2. **03-02-T02: Persist alerts before workflows and source commit** — `d9350ff` (`feat`)

## Verification Evidence

Each required selector was executed separately, with a six-minute Go ceiling and external `timeout 360s`; no short-circuit chain substituted for independent RED or GREEN results. TestMain retained all five default binaries and four real dependency containers.

| Command | Actual result |
| --- | --- |
| `cd backend && go test ./internal/contracts ./internal/kafkax ./internal/config -timeout 60s` | Baseline PASS, exit 0. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterPipeline$' -tags=integration -timeout 6m -p 1` before production | Expected assertion RED, exit 1; missing immutable alerts schema after compilation, real containers, default binary startup and Kafka publication. Package 19.573s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWorkflowReplay$' -tags=integration -timeout 6m -p 1` before production | Expected assertion RED, exit 1; same missing alerts persistence schema after successful setup/publication. Package 26.862s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertsSchema$' -tags=integration -timeout 6m -p 1` before production | Expected assertion RED, exit 1; SHOW CREATE TABLE alerts reports absent table after migrations/setup. Package 18.758s. |
| `cd backend && go test ./internal/alertwriter ./cmd/alert-writer ./internal/kafkax ./internal/config -timeout 60s` | PASS, exit 0; concrete implementation compiles and existing tests pass. |
| Same required Pipeline command, final code | GREEN, exit 0; real blocked-PG ordering and exact stores/offset/defaults, test 1.28s, package 22.605s. |
| Same required Replay command, final code | GREEN, exit 0; complete resolved workflow row unchanged, test 1.21s, package 21.565s. |
| Same required Schema command, final code | GREEN, exit 0; repeatable migrations, independent alert/seq uniqueness and tenant indexes, test 0.25s, package 20.920s. |
| `timeout 300s make test` | PASS, exit 0; complete backend Go race suite, web 6 tests, generator 99 tests. |
| `make lint` | Final PASS, exit 0; Go 0 issues, web lint/typecheck and generator checks. Earlier owned-code integer-conversion/formatting findings were repaired without lint policy changes. |
| `cd backend && go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | Exit 1; final sole finding is existing out-of-scope `test/recovery_test.go:162` unparam (`requireOutputEventually` text always receives service started). All owned test findings repaired. Later 03-14 retains the phase-wide integration lint obligation. |
| `scripts/go-test-nonempty.sh './test/...' '^TestWriterCommitAfterInsert$' -tags=integration -timeout 6m -p 1` | Existing Phase 2 real-writer regression PASS, exit 0; test 1.64s, package 19.699s. |
| `cd backend && go vet -tags=integration ./... && go build ./...` | PASS, exit 0. |
| `git diff --check` | PASS, exit 0. |

No full Phase 3 integration, hosted CI, concurrent conflict/ambiguous commit, poison quarantine or cooldown proof is claimed. Test-owned fixtures cleaned their containers; user resources/volumes were untouched.

## Files Created/Modified

- New real pipeline/replay and schema tests use the existing driver/pool fixtures without new store imports in RED.
- New `003_alerts.sql` and `003_alert_workflows.sql` migrations are additive through the existing embedded runners.
- New `internal/alertwriter/writer.go` contains the initial cohesive worker, identity/fingerprint and concrete native helpers for immediate 03-17 extraction.
- New alert consumer adapter and updated alert-writer main connect the default binary to the real stores and lifecycle.

## Decisions Made

Followed ADR 0008's immutable CH evidence and authoritative PG workflow/list split. PostgreSQL guidance informed the explicit tenant indexes, identity seq and bounded transaction settings; the approved per-alert serialization intentionally spans CH acknowledgement to prevent competing immutable writers.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Compare persisted timestamps in UTC**
- Found during: Task 2's first GREEN pipeline run.
- Issue: pgx returned the correct timestamptz instant in the host IST location, while the test compared time.Time representations against UTC. Pipeline reached correct stored content and source offset, then failed that assertion (package 21.745s).
- Fix: Normalize scanned timestamps to UTC before equality checks. Added a real blocked-PG ordering assertion and independent uniqueness checks while tightening the same acceptance files; no production contract was weakened.
- Files modified: `backend/test/alertwriter_pipeline_test.go`, `backend/test/alerts_schema_test.go`.
- Verification: All three independent final acceptance commands pass.
- Commit: `d9350ff`.

Total deviations: 1 auto-fixed test correctness issue; file scope and production architecture remained approved.

## Issues Encountered

The initial staging command used repository-relative paths from backend and exited 128 before staging anything; it was rerun from repository root. No authentication gate, dependency installation or infrastructure test failure occurred. The expected RED failures are missing production behavior, not compiler/setup failures.

## Deferred Issues

- Existing integration-tag lint warning: `backend/test/recovery_test.go:162` unparam. Preserved outside plan ownership as directed by the orchestrator; 03-14 owns later fixture/regression cleanup.
- Immediate 03-17 owns store extraction and focused native adapter/order proofs. 03-10 owns adversarial immutable conflicts/concurrent replay/partial-store and ambiguous acknowledgement proofs. 03-11 owns live bounded poison quarantine/prefix strengthening; current invalid/conflicting input fails closed without source acknowledgement. No unfinished placeholder prevents this plan's controlled canonical-alert and resolved-replay goal.

## User Setup Required

None for automated acceptance. Deployment/scenario wiring belongs to subsequent plans.

## Requirement Scope and Next Plan Readiness

Frontmatter copies ALRT-01/ALRT-02 for slice traceability; global requirement checkboxes remain pending because later native/failure/poison proofs are still required. Phase 3 remains in execution. Ready for 03-17 and the dependent 03-18/19 work. No new network endpoint, auth path, file access pattern or trust boundary outside the planned schema/store surface was introduced.

## Self-Check: PASSED

- All seven planned application/test/migration files exist.
- Task commits `366770b` and `d9350ff` exist and deleted no tracked files.
- Independently observed assertion RED precedes the implementation commit; final named acceptance selectors pass.
- Stub-pattern scan found no placeholder/TODO/FIXME or unwired data source preventing the focused goal. Empty fingerprint status is intentional exclusion of mutable workflow state, not a UI stub.
- `.planning/config.json` and `.serena/` remain excluded from every task/summary commit.
