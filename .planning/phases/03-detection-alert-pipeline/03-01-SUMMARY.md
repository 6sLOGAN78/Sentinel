---
phase: 03-detection-alert-pipeline
plan: "01"
subsystem: detection
tags: [go, kafka, event-time, auth, replay, yaml, tdd]
requires:
  - phase: 02-ingestion-durable-event-storage
    provides: Strict canonical contracts, bounded Kafka publishing, lifecycle and real-container fixtures
provides:
  - Accepted recovery/cooldown and alert-list/conflict ADRs before rule code
  - Live AUTH events-to-canonical-alert path with acknowledged publication and state-pin metadata
  - Strict capped startup rules and a causal real-Kafka threshold/checkpoint acceptance test
affects: [03-02, 03-15, 03-16, 03-07, 03-18, 03-19]
tech-stack:
  added: []
  patterns: [frozen first-threshold evidence, pre-pin maximum and sealed floor, atomic offset metadata commits]
key-files:
  created:
    - docs/decisions/0007-event-time-state-pin-recovery.md
    - docs/decisions/0008-alert-list-source-and-conflicts.md
    - backend/test/detector_pipeline_test.go
    - backend/internal/detection/reducer.go
    - backend/internal/detector/worker.go
  modified:
    - backend/cmd/detector/main.go
    - backend/internal/config/config.go
    - config/rules.yaml
key-decisions:
  - "AUTH startup slice retains fired-window pins and commits format-2 metadata only after acknowledged alert publication."
  - "Final cross-window cooldown requires the later durable PostgreSQL decision ledger; this intermediate slice does not claim DET-08."
patterns-established:
  - "Detector commits exact resume pins through PreCommitFnContext and validates transport, partition and completeness results."
  - "Reducer source effects are acknowledged before window retirement and sealed-floor adoption."
requirements-completed: [DET-01, DET-05, DET-06, DET-07, ALRT-01, ALRT-03]
duration: 9 min
completed: 2026-10-17
---

# Phase 3 Plan 1: Live AUTH Publication Summary

Ten unique AUTH failures now produce a frozen canonical Kafka alert, with the fired window retained behind an atomic format-2 recovery pin.

## Performance

- Started: 2026-10-17T06:10:27Z
- Implementation completed: 2026-10-17T06:19:37Z
- Duration: 9 min
- Tasks: 2/2
- Application/test/ADR files changed: 8

## Accomplishments

- Both Accepted ADRs were committed before any rule implementation. They specify half-open event-time windows, pre-pin maxima, durable sealed floors, final PG cooldown/fencing/frontier policy, config handover, live scenario scheduling, and CH/PG list authority/conflict semantics.
- Startup rules use the installed pinned YAML v3 decoder with KnownFields, one-document EOF, a 64 KiB cap, four known versioned definitions and bounded settings. AUTH is enabled; the other three remain explicitly disabled for their feature plans. Detector settings reuse validated WriterConfig budgets independently of event-writer defaults.
- AUTH uses tenant/source-IP/principal grouping, canonical source keys, unique event IDs, a 30-second partition watermark and frozen first-threshold evidence. CreatedAt equals window end; DetectedAt is omitted; existing DedupeKey encoding/goldens are unchanged.
- The worker owns bounded batches and publishes all-ISR tenant|alert_id records before acknowledgement/checkpoint. Fired windows retain their first offset. Metadata includes explicit empty prefix, four versions/gates, processed_next, sealed floor and decision_frontier.none within 1024 bytes. Callback failures, lost history and malformed input fail closed; terminal restoration errors reach lc.Fail.
- The real broker acceptance checks nine events produce no alert, ten produce the exact key/count/times/evidence, a fired window commits pin 0 with processed_next 10, and an eleventh event produces no repeat.

## Task Commits

1. **03-01-T01: Record ADRs and expose missing AUTH path** — `71b05a8` (`test`)
2. **03-01-T02: Run the first real AUTH events-to-alert slice** — `aff6c34` (`feat`)

## Verification Evidence

| Command | Actual result |
| --- | --- |
| `cd backend && go test ./internal/contracts ./internal/kafkax ./internal/config ./internal/app ./internal/opshttp -timeout 120s` | Baseline PASS, exit 0; all five existing packages passed before implementation. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` before implementation | Expected RED, exit 1; compilation, containers, source publication and default detector startup succeeded. Named assertion failed: missing tenth-event AUTH alert, zero records. Test 22.49s; package 41.147s. |
| `cd backend && go test ./internal/detection ./internal/detector ./internal/config ./cmd/detector -timeout 60s` | PASS, exit 0. New packages compiled; config tests passed. |
| Same named real-Kafka command after implementation | GREEN, exit 0; test 4.54s, package 31.552s. |
| `make test` | PASS, exit 0: complete Go race suite, web 6 tests, generator 54 tests. |
| `make lint` | Final PASS, exit 0: Go 0 issues, web ESLint/typecheck, Python ruff/format/mypy. Initial findings in new code were resolved with narrow helpers, comments, formatting and named constants. |
| Same named real-Kafka command after refactoring and broker metadata assertions | Final GREEN, exit 0; test 4.46s, package 32.346s. Broker pin and complete initial metadata independently observed. |
| `cd backend && go test -race ./... -timeout 120s` after final implementation edits | PASS, exit 0; all backend packages, contracts/goldens and existing regressions passed. |
| `git diff --check` | PASS, exit 0. |

No full Phase 3 integration/hosted CI, crash/rebalance/corruption proof, or cooldown proof is claimed. Test-owned containers were cleaned by their fixtures; user stack volumes and private files were untouched.

## Files Created/Modified

- ADRs 0007/0008: final event-time recovery/cooldown and alert-list authority policies.
- `backend/test/detector_pipeline_test.go`: default-binary real-Kafka AUTH threshold, payload, pin and no-repeat checks.
- `backend/internal/detection/reducer.go`: initial strict loader, AUTH reducer, state bounds, checkpoint/restore contracts.
- `backend/internal/detector/worker.go`: explicit interfaces and initial bounded Kafka helpers; narrow extraction remains assigned to 03-16.
- `backend/cmd/detector/main.go`: rules loading, worker lifecycle, dependency readiness and terminal failure wiring.
- `backend/internal/config/config.go`: topics/groups, rules path and independently validated detector writer budgets.
- `config/rules.yaml`: four researched version-1 defaults, AUTH enabled.

## Decisions Made

Followed the approved first-AUTH-slice scope. Final actual cross-window suppression is specified by ADR 0007 and remains dependent on 03-18/19. No in-memory or no-op decision store is presented as the final cooldown implementation.

## Deviations from Plan

None — both tasks and their prescribed RED/GREEN gates were executed. The acceptance additionally inspects real committed metadata. New-code lint findings were resolved before the implementation commit without changing lint policy.

## Issues Encountered

The first compile command was invoked from the repository root and exited because the Go module lives in backend; rerunning from backend passed. This was a command-location error, not an accepted RED or a build failure in the implementation. No authentication gate or package installation occurred.

## Known Stubs

None preventing this plan's AUTH publication goal. SCAN/FLOOD/DENY are deliberately disabled until their feature plans; poison quarantine, exhaustive loader/client validation, recovery/handover/capacity proofs, storage, operational cooldown and deployment remain later Phase 3 work.

## Requirement Scope and Next Plan Readiness

The frontmatter copies this plan's requirement IDs for workflow traceability; it does not certify the entire requirement beyond this slice. DET-05/06/07 and ALRT-01/03 still have later loader, all-rule, metric, storage and recovery obligations. Requirement checkboxes must remain pending until those proofs are complete.

Ready for 03-06 and the dependent 03-02/15/16 plans. In particular, legacy lifecycle/readiness integration fixtures still use a temporary cwd without the new absolute rules path or alerts-topic setup; their planned fixture updates remain in 03-18/14, and no unchanged full integration-suite pass is claimed here. Future adapters must preserve exact pin/prefix/sealed ordering and actual callback errors. No new endpoint, auth path or unplanned trust boundary was introduced. The rules file is a bounded local startup input within this plan's configuration surface.

## User Setup Required

None for automated acceptance. Deployment/scenario wiring is owned by subsequent plans.

## Self-Check: PASSED

- All five created files and three modified files exist.
- `71b05a8` and `aff6c34` exist in git history; neither commit deleted tracked files.
- Both prerequisite ADRs are in the RED commit before the first rule implementation commit.
- Final named real-Kafka test, complete backend race regression, full unit suite and lint have actual passing results above.
- `.planning/config.json` and `.serena/` remain excluded from all plan commits.
