---
phase: 03-detection-alert-pipeline
plan: "16"
subsystem: kafka
tags: [go, kafka, franz-go, checkpoints, bounded-publishing, tdd, refactor]
requires:
  - phase: 03-01
    provides: Working AUTH worker, canonical publication and atomic format-2 checkpoints
  - phase: 03-15
    provides: Extracted strict rules loader and independently verified AUTH behavior
provides:
  - Dedicated bounded all-ISR alert publisher with exact wire-contract tests
  - Dedicated metadata-aware detector consumer with callback and terminal-error tests
  - Compatible detector constructors and coordinator member/generation access
affects: [03-07, 03-12, 03-18, 03-19, 03-20, 03-21]
tech-stack:
  added: []
  patterns: [explicit transport interfaces, callback-authoritative checkpoint commits, compatible extraction wrappers]
key-files:
  created:
    - backend/internal/kafkax/alerts.go
    - backend/internal/kafkax/alerts_test.go
    - backend/internal/kafkax/detector_consumer.go
    - backend/internal/kafkax/detector_consumer_test.go
  modified:
    - backend/internal/detector/worker.go
key-decisions:
  - "03-16 extracts the existing adapters through explicit transport interfaces and detector compatibility wrappers; checkpoint/topic types remain owned by detection."
patterns-established:
  - "CommitOffsetsSync returns void: only its completed callback determines success, including every requested partition."
requirements-completed: [DET-01, ALRT-01, ALRT-03]
duration: 5 min
completed: 2026-10-19
---

# Phase 3 Plan 16: Bounded Kafka Detector Adapter Extraction Summary

Canonical all-ISR publication and metadata-aware state-pin commits now live in dedicated Kafka adapters, with independent bounds, callback, terminal-failure and real AUTH acceptance proofs.

## Performance

- Execution timestamp recorded: 2026-10-18T22:11:30Z (2026-10-19 local).
- Implementation and verification completed: approximately 2026-10-18T22:16:00Z.
- Duration: approximately 5 min, excluding subsequent tracking bookkeeping.
- Tasks: 2/2.
- Application/test files changed: 5.

## Accomplishments

- `backend/internal/kafkax/alerts.go` owns the former worker producer. It preserves the exact JSON bytes, tenant|alert_id key, all-ISR acknowledgements, four-alert batch cap, 64 KiB payload cap, finite retry/delivery/buffer limits, aggregate publish deadline and topic-leader readiness probe.
- `backend/internal/kafkax/detector_consumer.go` owns the former worker consumer. It preserves disabled autocommit, owned bounded fetches, finite rebalance/operation budgets, retained-earliest start for new groups and NoResetOffset for existing pins, assignment incarnation tokens, subset-only revoke/loss cleanup, atomic PreCommitFnContext metadata attachment, complete callback response validation and terminal restore propagation through the existing onFatal/lc.Fail wiring.
- The callback family exercises transport, partition, nil, missing-partition, duplicate and unexpected-partition results. Failure leaves the previous successful checkpoint untouched. Terminal fetched-metadata failure cancels the consumer lifetime, reports the failure once, and remains visible through Ping.
- Detector aliases and small constructor wrappers retain detector.New/main compatibility without duplicate client implementations. `detection.TopicPartition` and `detection.Checkpoint` retain ownership of recovery types; the extracted adapter exposes GroupMetadata membership/generation for later PG fencing.
- Additional characterization guards cover new versus trimmed/out-of-range existing pins, reassign token changes, unaffected partition ownership, finite consumer options and separate start/reset policies. No PostgreSQL cooldown mechanism is introduced.

## Task Commits

1. **Task 1: Expose early publisher and checkpoint callback behavior** — `4f33c10` (`test`). Loadable declaration scaffolds and both independent test families preceded adapter extraction.
2. **Task 2: Extract metadata consumer and bounded publisher** — `7803da9` (`refactor`). Existing runtime implementations moved, test transport interfaces added, constructor compatibility retained, and guards completed.

The GREEN commit is a refactor because the runtime behavior already existed in 03-01; it does not claim a newly repaired old-runtime regression.

## Verification Evidence

Commands run from the repository root unless an explicit directory is shown. Each named family uses its own nonempty selector.

| Command | Actual result |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/kafkax/...' '^TestAlertPublisherBounds$' -timeout 60s` against declaration scaffolds | Assertion RED, exit 1; package compiled and ran in 0.005s. Canonical publication and construction assertions rejected the absent adapter implementation. Negative guards alone were not counted as RED proof. |
| `scripts/go-test-nonempty.sh './internal/kafkax/...' '^TestDetectorCommitMetadata$' -timeout 60s` against declaration scaffolds | Assertion RED, exit 1; package compiled and ran in 0.004s. Every callback case required reaching the authoritative transport callback; success, metadata attachment and terminal-reporting assertions rejected absent behavior. No compile/import failure was used as RED. |
| Same independent publisher command after extraction | Final GREEN, exit 0; package 0.005s, all four subtests passed. |
| Same independent callback command after extraction and added characterization guards | Final GREEN, exit 0; package 0.005s, all twelve subtests passed. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` | PASS, exit 0; real Kafka AUTH test 4.53s, package including container setup/cleanup 26.586s. Exact canonical alert/key/evidence, tenth-event threshold, committed pin/metadata and no-repeat remained passing. |
| `scripts/go-test-nonempty.sh './internal/kafkax/...' '^TestAlertPublisherBounds$' -race -count=10 -timeout 60s` | PASS, exit 0; ten independent repetitions, package 1.037s. |
| `scripts/go-test-nonempty.sh './internal/kafkax/...' '^TestDetectorCommitMetadata$' -race -count=10 -timeout 60s` | PASS, exit 0; ten independent repetitions including final ownership/start/reset guards, package 1.046s. |
| `timeout 300s make test` | PASS, exit 0; backend `go test -race ./...`, web 6 tests, generator 99 tests. Added final consumer-only guards were then independently repeated under race as recorded above. |
| `timeout 180s make lint` | PASS, exit 0; default Go lint 0 issues, web ESLint/typecheck, generator ruff/format/mypy. |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run ./...` after final added guards | PASS, exit 0; 0 issues. |
| `cd backend && timeout 120s go build ./...` | PASS, exit 0; all binaries/packages compiled within the finite ceiling. |
| In-memory source-block comparison against `4f33c10:backend/internal/detector/worker.go` | PASS, exit 0. Entire producer and consumer helper blocks match modulo names, explicit client interfaces, shared constant names and whitespace; new GroupMetadata forwarding is outside the compared block. |
| `git diff --check` and post-task deletion check | PASS, exit 0; no tracked file deletions. |

Context7 resolved `/twmb/franz-go`; pinned local v1.22.1 source additionally confirmed CommitOffsetsSync's void signature/completed callback contract, PreCommitFnContext, GroupMetadata and OptValue representation. No install or dependency change occurred.

## TDD / Extraction Gate

The new extracted declarations had real assertion-level RED before runtime bodies moved. This demonstrates that the new adapter tests are live; it does not assert that the previous worker runtime lacked the behavior. That implementation already checked callbacks, canonical publication and terminal failures. The source-block comparison, passing guards and real AUTH acceptance verify preservation. The original feature's separate historical assertion RED remains documented in `.planning/phases/03-detection-alert-pipeline/03-01-SUMMARY.md`.

## Files Created/Modified

- `backend/internal/kafkax/alerts.go`: bounded canonical publisher and external transport interface.
- `backend/internal/kafkax/alerts_test.go`: independent literal wire bytes/key, all-ISR options, deadline, delivery-failure and admission guards.
- `backend/internal/kafkax/detector_consumer.go`: extracted metadata/ownership consumer, complete callback checks and GroupMetadata forwarding.
- `backend/internal/kafkax/detector_consumer_test.go`: callback errors/completeness, atomic metadata attachment, terminal failures, ownership and bounded-consumer characterization.
- `backend/internal/detector/worker.go`: retained reducer/effect orchestration and constructor wrappers; removed moved Kafka implementations.

## Decisions Made

Followed the approved extraction scope. Small internal transport interfaces permit test-only response drivers while real constructors always use the pinned kgo clients. Event-writer CommitRecords semantics, checkpoint format, canonical ID function, main wiring and rule behavior remain unchanged.

## Deviations from Plan

None in runtime behavior or ownership scope. The plan-prescribed extraction GREEN uses a refactor commit instead of pretending to implement a previously missing runtime feature. Additional ownership/start/reset tests characterize existing behavior after the required initial families were committed. Global requirement checkboxes remain pending as directed by the orchestrator because later plans own full requirement proof.

## Issues Encountered

- Initial GREEN publisher options assertions compared Go int literals with OptValue's int64 values. Two narrowly scoped test-comparison repairs use numeric equality; expected limits and production options did not change. These were test-type mismatches, not accepted behavioral RED.
- Initial scoped lint identified import grouping, a moved 126-character options line and the renamed Assignments comment. Those formatting/comment issues were repaired before the refactor commit; final full default lint passed.
- The previously recorded integration-tag lint finding at `backend/test/recovery_test.go:162` (unparam) remains outside this plan's ownership for 03-14. Integration-tag lint was not rerun here; no integration-lint-clean claim is made.

## Known Stubs

None preventing this plan's extraction goal. Declaration-only scaffolds were completely replaced. Disabled remaining rules, actual PG cooldown/fencing, exhaustive recovery/crash/poison/config-handover proofs, live scheduling and final CI remain later plans.

## Threat Review

T-03-16-01 is covered by complete callback checks, unchanged successful checkpoint on failures, atomic request metadata and terminal reporting guards. T-03-16-02 is covered by preserved finite producer/fetch/rebalance/operation limits and executed option/deadline tests. No new endpoint, auth flow, database schema or unplanned trust boundary was introduced; no open high finding was identified in this extraction. Broader adversarial broker/rebalance/recovery proofs remain later execution obligations.

## User Setup Required

None. No package install, branch/worktree operation, remote write, user-data reset or volume cleanup occurred. Original `.planning/config.json` changes and untracked `.serena/` were preserved and excluded from commits.

## Next Phase Readiness

Ready for the parent Wave 2 cross-plan gate and dependent 03-18 PG lifecycle wiring. Actual cross-window suppression belongs to 03-19, extensive restart/rebalance/crash tests to 03-07/12/20, and phase-wide integration/security/CI evidence to 03-14. Requirement frontmatter copies the plan IDs for traceability and does not certify those entire requirements or Phase 3.

## Self-Check: PASSED

All four created adapter/test files and this SUMMARY exist; commits `4f33c10` and `7803da9` resolve. Final selectors, real AUTH, race repetitions, default suite/lint, finite build and diff checks passed. Earlier RED and intermediate test/lint findings above are historical evidence, not an outstanding self-check failure.
