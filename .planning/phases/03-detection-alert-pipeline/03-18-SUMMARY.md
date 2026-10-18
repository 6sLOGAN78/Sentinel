---
phase: 03-detection-alert-pipeline
plan: "18"
subsystem: database
tags: [go, postgres, pgxpool, lifecycle, readiness, compose, tdd]
requires:
  - phase: 03-16
    provides: Bounded Kafka adapters and live detector lifecycle
  - phase: 03-02
    provides: Existing PG pool, migrations and two-store persistence
provides:
  - Real bounded PostgreSQL startup, readiness and pre-processing dependency
  - Validated decision deadlines and explicit pool injection for plan 19
  - PG-capable Compose worker and compatible private integration fixtures
affects: [03-19, 03-20, 03-13, 03-14]
tech-stack:
  added: []
  patterns: [bounded pool lifecycle, aggregate ownership budget reservations, migration-before-worker dependency]
key-files:
  created:
    - backend/internal/config/detection_pg_test.go
    - backend/test/detector_pg_test.go
  modified:
    - backend/internal/config/config.go
    - backend/cmd/detector/main.go
    - backend/internal/detector/worker.go
    - deployments/compose/docker-compose.yml
    - backend/test/main_test.go
    - backend/test/readyz_test.go
    - backend/test/lifecycle_test.go
    - backend/internal/config/config_test.go
    - backend/internal/app/app_test.go
    - backend/internal/httpx/httpx_test.go
key-decisions:
  - "03-18: Require real PG at startup and before a nonempty source batch; expose the pool explicitly without activating cooldown decisions."
  - "03-18: Reserve acquisition and independent cleanup inside the existing processing budget; cap transaction, statement and lock deadlines to the remaining budget."
  - "03-18: Keep Compose workers profiled and gate detector startup on the existing pinned tern migration tool; dev-up activation remains plan 13."
requirements-completed: [DET-05, DET-08, ALRT-03]
duration: ~9 min
completed: 2026-10-19
---

# Phase 3 Plan 18: Detector PostgreSQL Dependency Summary

The running detector now requires a real PostgreSQL pool, reports actual PG readiness and fails before source advancement when that dependency is unavailable.

## Performance

- Recorded execution start: 2026-10-18T22:33:58Z; implementation verified/committed: 2026-10-18T22:42:32Z (2026-10-19 local).
- Duration: approximately 9 minutes, excluding subsequent serialized tracking.
- Tasks: 3/3; application/test/deployment files: 12 (nine planned plus three required fixture extensions).

## Accomplishments

- Detector config now requires the existing database section. Detector-specific validation rejects zero/negative/overflowing pool bounds, invalid ports, durations below a millisecond or over 30 seconds, and invalid lock/statement/transaction ordering. Integer lifetime bounds are checked before the existing seconds-to-duration conversion. Other services retain their old database/writer defaults.
- Decision defaults are acquisition 2s, transaction 5s, lock 1s, statement 2s and cleanup 1s. `ComputeDetectorPGBudgets` reserves acquisition and independent cleanup within `ComputeWriterBudgets.Processing`, caps transactions to the remainder, then caps SQL deadlines. A literal three-second ownership case verifies 750ms acquisition, 750ms cleanup and 1500ms transaction/statement budgets.
- Main calls existing `database.New` under a finite context, suppresses debug driver tracing, drops raw connection errors, registers bounded pool shutdown and exposes a real `postgres` Ping readiness check. Ops shutdown remains registered first, draining last, Kafka topic-leader checks remain intact, and worker failures retain `lc.Fail` propagation.
- `detector.New` receives the actual `*pgxpool.Pool` and PG budgets explicitly. Before processing a nonempty owned batch the worker checks PG under the acquisition and aggregate batch contexts. A private paused-PG process test proves not-ready, bounded nonzero failure after source injection, and absent committed source metadata.
- Compose now includes a profiled detector and one-shot `postgres-migrate` dependency. The migration service reuses the existing pinned Go build stage, its `/src/backend` working directory, copied migrations/tools.mod and existing tern command. Passwords stay in environment substitution; rules mount read-only. The existing Make/dev-up service list and all named volumes are unchanged.
- TestMain still builds the five default binaries. Shared fixtures now supply private PG settings, absolute rules paths, separate event/alert topics and independent worker groups. The private readiness PG fixture runs existing migrations before starting processes. AUTH and SCAN need no direct fixture-file changes.

## Task Commits

1. **Task 1: Expose PG/config dependency** — `28a3bcc` (`test`, assertion RED).
2. **Task 2: Wire bounded real PG pool** — `375c8a8` (`feat`, GREEN).
3. **Task 3: Supply deployment and fixture capability** — `fc2a334` (`chore`).

## Verification Evidence

Each named selector below ran separately through `scripts/go-test-nonempty.sh`. Pure selectors use `-timeout 60s`; integration selectors use `-tags=integration -timeout 6m -p 1`. Durations are Go-reported test/package durations, not invented shell timings.

| Command | Actual result |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/config/...' '^TestDetectorPGSettings$' -timeout 60s` before production changes | Assertion RED, exit 1; package 0.386s. Existing loader accepted unsafe PG deadlines/pool settings. No undefined type/import or setup failure counted as RED. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorPGDependency$' -tags=integration -timeout 6m -p 1` before production changes | Assertion RED, exit 1; test 20.51s/package 55.752s. Real containers, source publication and default process startup succeeded. Healthy readiness reported only `alerts:ok kafka:ok`; unavailable PG process remained running beyond five seconds. |
| Same settings selector, final code | GREEN, exit 0; package 0.111s. Includes direct defaults/aggregate reservation assertions, submillisecond guards and lock/statement ordering. |
| Same PG dependency selector after pool wiring | GREEN, exit 0; test 6.61s/package 23.724s. Healthy startup, unavailable startup and actual paused-PG source retention all passed. |
| Same PG dependency selector after shared fixture wiring | GREEN, exit 0; test 6.72s/package 22.922s. All three cases passed again. |
| `scripts/go-test-nonempty.sh './test/...' '^TestReadyzDependencies$' -tags=integration -timeout 6m -p 1` | Final PASS, exit 0; test 37.48s/package 52.718s, after adding private PG migration. PG/Redis/Kafka/CH outages recovered; PG includes detector. Earlier run also passed, test 43.91s/package 64.750s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` | PASS, exit 0; test 4.54s/package 23.868s. Exact alert, evidence and checkpoint behavior preserved. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineScan$' -tags=integration -timeout 6m -p 1` | PASS, exit 0; test 4.98s/package 31.572s. Real SCAN behavior preserved. |
| `scripts/go-test-nonempty.sh './test/...' '^TestBinaryLifecycle$' -tags=integration -timeout 6m -p 1` | PASS, exit 0; package 21.230s. All ten SIGTERM/SIGINT service cases passed (approximately 3s each, parallel). |
| `scripts/go-test-nonempty.sh './test/...' '^TestHealthz$' -tags=integration -timeout 6m -p 1` | PASS, exit 0; test 1.07s/package 19.050s. All five services ready and healthy. |
| `scripts/go-test-nonempty.sh './test/...' '^TestReadyzDraining$' -tags=integration -timeout 6m -p 1` | PASS, exit 0; test 1.94s/package 19.264s. All five services report draining before graceful exit. |
| `scripts/go-test-nonempty.sh './internal/opshttp/...' '^TestReadyzTimeoutAndConcurrencyBound$' -timeout 60s` | PASS, exit 0; package 0.011s. Existing sixteen-worker admission and independent response bounds unchanged. |
| `scripts/go-test-nonempty.sh './internal/opshttp/...' '^TestReadyzUsesOneBoundedRequestContext$' -timeout 60s` | PASS, exit 0; package 0.035s. |
| `cd backend && go test ./internal/config ./internal/detector ./cmd/detector -timeout 60s` | PASS, exit 0 after required old config fixture updates; config package 0.472s. |
| `timeout 300s make test` | Final PASS, exit 0 after all fixture changes: full backend race suite, web 6 tests, generator 99 tests. |
| `timeout 180s make lint` | Final PASS, exit 0 after all fixture changes: Go 0 issues, web lint/typecheck, generator ruff/format/mypy. |
| `cd backend && timeout 120s go build ./... && timeout 120s go vet -tags=integration ./...` | PASS, exit 0; final chained run reached integration lint. |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | Exit 1: only existing out-of-scope `test/recovery_test.go:162` unparam. No clean integration-lint claim. |
| `docker compose -f deployments/compose/docker-compose.yml --env-file .env --profile workers config --quiet` | PASS, exit 0. No resolved secrets printed. This verifies configuration, not migration/container runtime. |
| `git diff --check`; post-task deletion checks | PASS, exit 0; no tracked file deletions. |

Context7 documentation for pinned pgx v5.11.0 confirmed acquisition cancellation and blocking pool Close semantics. No dependency installation occurred.

## Deviations from Plan

**1. [Rule 1 - Required fixture compatibility] Extend three unit fixtures after detector PG becomes mandatory.**
- Full config tests showed the old detector-required-section and dotenv cases omitted PG. The complete race suite additionally exposed app/httpx fixtures using detector config with only Kafka; the app test waited unboundedly for setup that validation correctly prevented.
- Narrow parent-approved extensions update `config_test.go`, `app_test.go` and `httpx_test.go` with required PG settings and explicit missing-PG rejection. App startup observation now has the existing five-second test bound and detects early exit. Binding, lifecycle and production validation assertions remain intact.
- Commits: `375c8a8` and `fc2a334`. Final complete test/lint pass is recorded above.

**2. [Rule 3 - Deployment prerequisite] Add a profiled migration service because Compose had no detector or migration service.**
- The existing Makefile orders migrations before its explicit Phase 2 app list. A new profiled detector needed its own concrete dependency when started directly. Reuse the existing build stage and tern tool with private environment credentials; no new image pin/package or main auto-migration.
- File: `deployments/compose/docker-compose.yml`; commit `fc2a334`; quiet configuration validation passed. Plan 13 still owns worker activation/runtime acceptance.

Owned-code lint corrections introduced named budget constants, extracted PG connection lifecycle setup to meet function length, and specialized the ops-port fixture helper after new callers triggered unparam. No lint policy or readiness deadline was relaxed.

## Issues Encountered

- Initial `make test` failed on missing-PG unit fixtures and the app startup wait; the positively identified test-owned `app.test` process received SIGQUIT to obtain evidence/end that blocked run (make exit 2). A second run, started before the httpx repair, failed only that fixture (exit 2). Neither is RED proof or a passing suite. Final bounded full runs passed after all repairs.
- One gofmt command used root-relative paths from backend and exited 2 before testing; corrected paths immediately. Initial owned lint findings were fixed; the remaining recovery helper warning is explicitly deferred to plan 14.
- No authentication gate, package install, remote write, branch/worktree operation or user-stack/data reset occurred. Original dirty `.planning/config.json` and `.serena/` were preserved and excluded from commits.

## Known Stubs and Threat Review

No stub blocks this plan's real PG capability goal. Transaction/SQL timeout settings are validated operational inputs for plan 19; this plan does not execute cooldown transactions or claim suppression/fencing/frontier correctness. No in-memory/no-op final decision store is supplied.

T-03-18-01 has finite pool acquisition/startup/cleanup, aggregate budget reservations and actual paused-PG/no-source-advance evidence. T-03-18-02 uses environment-only credentials, stable sanitized dependency errors and suppressed debug connection tracing; tests assert no password/DSN output. Existing Kafka topic-leader readiness is preserved. No unplanned public endpoint, auth path or schema was introduced; no open high threat identified for this slice.

## Next Plan Readiness

Plan 19 can consume the explicit pool and `ComputeDetectorPGBudgets`; it still owns real transactional SQL deadline enforcement and durable cooldown decisions. Plans 19/20 own fenced decision/recovery/outage correctness, plan 13 owns actual Compose worker activation/migration runtime, and plan 14 owns the remaining integration lint warning, full integration/security/CI and preserved-data acceptance. Requirement IDs in frontmatter identify this slice; global requirement checkboxes remain pending and Phase 3 is not complete.

## Self-Check: PASSED

- All twelve application/test/deployment files exist; both newly created tests are tracked.
- Commits `28a3bcc`, `375c8a8` and `fc2a334` resolve; none delete tracked files.
- Independent behavioral RED preceded real PG wiring; all three required acceptance families and AUTH/SCAN/lifecycle/readiness regressions pass.
- Final full unit/lint/build/vet checks passed, with the separately reported integration-lint exception.
- Stub and threat-surface scans found no incomplete capability or unplanned trust boundary for this plan.
- SUMMARY is committed before serialized state updates.
