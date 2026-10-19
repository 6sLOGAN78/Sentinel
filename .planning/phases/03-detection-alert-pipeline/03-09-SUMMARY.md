---
phase: 03-detection-alert-pipeline
plan: "09"
subsystem: detection
tags: [go, kafka, bounded-state, metrics, readiness, replay, tdd]
requires:
  - phase: 03-08
    provides: Strict checkpoints, durable configuration handover and bounded idle restoration
  - phase: 03-19
    provides: Durable immutable decisions and current-generation PostgreSQL fences
provides:
  - Shared process aggregate admission before recoverable reducer mutation
  - Fixed process-local internal late counter without labels or dependencies
  - Current-assignment restoration readiness and retained cooperative ownership rebinding
affects: [03-11, 03-12, 03-20, 03-21, 03-13, 03-14]
tech-stack:
  added: []
  patterns: [Prospective scalar admission, immutable readiness snapshots, retained-state fenced rebinding]
key-files:
  created:
    - backend/internal/detection/bounds.go
    - backend/internal/detection/bounds_test.go
    - backend/internal/opshttp/metrics.go
    - backend/internal/opshttp/metrics_test.go
    - backend/test/detector_capacity_test.go
    - backend/internal/detector/readiness_test.go
  modified:
    - backend/internal/detection/reducer.go
    - backend/internal/config/config.go
    - backend/internal/opshttp/server.go
    - backend/internal/detector/worker.go
    - backend/cmd/detector/main.go
    - backend/internal/kafkax/detector_consumer.go
    - backend/internal/kafkax/detector_consumer_test.go
    - backend/test/readyz_test.go
key-decisions:
  - "03-09: Share one process budget across owned partitions; reserve all prospective windows, ledger entries, strings, sets and frozen bytes before changing recoverable state."
  - "03-09: The 64 MiB state and 256 MiB combined resource limits bound documented accounted resources, not Go heap/RSS or transient driver/encoding allocations."
  - "03-09: Readiness compares callback-observed current assignment with successfully restored immutable snapshots, including explicit joined-empty replicas."
  - "03-09: Cooperative retained partitions preserve lifetime tokens and reducer state while current-generation PG registration verifies the fully effected frontier before changing owner."
requirements-completed: [DET-06, ALRT-03]
duration: approximately 85 min elapsed including quota interruption
completed: 2026-10-19
---

# Phase 3 Plan 9: Aggregate State Admission and Late Metrics Summary

Detector partitions now share checked state budgets, reject over-capacity inputs without changing recovery checkpoints, and report late drops through one internal numeric counter.

## Performance

- First explicit execution clock: 2026-10-19T05:33:14Z; final capacity verification completed at 06:57:45Z. Approximately 85 minutes elapsed, including the quota interruption; active work was not independently timed.
- Tasks: 3/3. Source/test files: 14 (ten planned files changed, four approved readiness extensions). Planned checkpoint.go already supplies the required strict metadata contract and needed no edit.

## Accomplishments

- One Bounds object is shared across worker partitions: at most 10,000 active windows, 100,000 source prefix entries, 4096 unique IDs/window, at most 4096 exact ports/window, and 64 MiB of tracked retained state. Fixture overrides only lower these ceilings. Admission stages scalar/window/sample changes and first-threshold canonical bytes, then reserves the complete aggregate delta before changing prefix, maximum, processed_next, IDs, ports, samples or frozen payload. A failed admission leaves the previous checkpoint byte-for-byte unchanged and creates no candidate effects. The worker returns terminally without another poll or an unsafe commit.
- Accounting charges 1024 bytes/window, 128 bytes/ID, 32 bytes/exact port, 64 bytes/prefix entry, plus variable key/ID strings and frozen JSON bytes. The window allowance covers struct/map headers, growth slack and twenty sample string headers; sample backing capacity is fixed at twenty. No full-state or per-record ID/port map cloning is performed. At most four touched windows/scalar deltas and bounded sample headers are staged; an ID map copy and canonical encoding occur once at threshold crossing. These temporary objects, encoding/driver buffers, allocator/GC overhead and Go RSS are explicitly outside the retained-state accounting model. No measured memory/throughput guarantee is claimed.
- Fetch bytes times concurrency plus producer bytes plus the state reservation must fit 256 MiB, checked by subtraction/division before arithmetic can overflow. Startup environment validation uses actual detector fetch/producer settings. Existing rule loading already rejects thresholds beyond 4096 and duration/count overflow; those passing guards were retained.
- Acknowledged event-time retirement releases windows; prefix entries release only below the successfully committed pin. Snapshotting and unfinished effects cannot release the pinned prefix or evict live evidence to admit traffic. Ownership loss releases abandoned volatile state, while the durable Kafka pin remains authoritative. Idle partitions deliberately retain live pins; a time horizon alone does not bound cardinality, and retention must cover the oldest pin plus downtime/catch-up.
- A fixed atomic detection_late_events_total counter is exposed only through the detector's existing internal ops listener. Optional handler/server constructor arguments preserve old callers and their routes. There is no global registry, package installation, tenant/IP/event labels or public listener change. One successfully admitted valid late input increments once; replay can increase this process-local operational count again. Equality at the watermark is accepted, one millisecond earlier is late, valid nonmatches advance maximum and poison/idle do not.
- Fatal worker/recovery errors begin draining before lc.Fail. Readiness now also requires an actually observed current assignment and completed restore. Atomic restored snapshots are compared with bounded mutex-protected callback snapshots; checks do not wait on recovery/network work. Revoke/loss/fatal/member/generation/token changes invalidate readiness. A genuinely joined replica with zero assigned partitions becomes ready after its explicit empty assignment callback.
- Cooperative callbacks report additions, not all retained partitions. Retained assignments now capture the actual new member/generation while keeping their lifetime token. Before reusing their live state, the worker verifies ownership, registers the new PG fence with the fully effected immutable frontier, rechecks ownership and replaces only the owner. Reducer windows/prefix/checkpoint remain intact. Revoked/reacquired partitions receive fresh monotone tokens and full restore. PG failure or concurrent revoke/generation change preserves the old owner and fails closed.

## Task Commits

1. Task 1, compiling admission/metric/real-process assertion RED: 91fa195.
2. Task 2, shared checked admission GREEN: a8c9bb8; bounded scalar-delta refinement: 6cb4f89.
3. Task 3, worker budget/internal metric GREEN: 5401b1a; additional inherited readiness-race assertion RED: 5dcaed2; current restored-assignment and retained-state fencing GREEN: 433fb4b.

All commits exist in proof-before-implementation order. No tracked file was deleted. Original dirty .planning/config.json and .serena/ were preserved and excluded. The parent's resumed STATE continuity edits were preserved for serialized tracking after this summary commit.

## RED Evidence

Each named family compiled and reached assertions; setup/import failures do not count as RED.

| Command / exact family | Exit | Actual evidence |
| --- | ---: | --- |
| scripts/go-test-nonempty.sh './internal/detection/... ./internal/opshttp/...' '^TestStateBounds$' -timeout 60s | 1 | 0.408s: aggregate windows/ledger, byte/frozen, port and arithmetic bounds absent; 4097th ID rejection left unacknowledged mutated state |
| Same packages, '^TestLateMetric$' -timeout 60s | 1 | Existing reducer equality/late boundary passes; ops route assertion expected 200, received 404 |
| scripts/go-test-nonempty.sh './test/...' '^TestDetectorCapacity$' -tags=integration -timeout 6m -p 1 | 1 | Existing real 4097th-ID terminal/pin retention guard passes; new runtime late counter missing, test 6.39s |
| './internal/detector/...' '^TestWorkerReadiness$' -race -timeout 60s | 1 | 0.017s: dependency-only readiness returns success before restoration |
| './internal/kafkax/...' '^TestDetectorAssignmentReadiness$' -race -timeout 60s | 1 | 0.013s: explicit empty assignment callback is not represented |
| './internal/kafkax/...' '^TestDetectorRetainedAssignment$' -race -timeout 60s | 1 | 0.016s: actual generation advances 9→10, retained assignment stays at 9 |
| './internal/detector/...' '^TestRetainedOwnerRebinding$' -race -timeout 60s | 1 | 0.013s: no current-generation registration on success/error/concurrent revoke/generation branches |

Additional real readiness failure was independently reproduced on an untouched git archive of baseline 945c972: TestReadyzDependencies test 37.50s/package 56.284s, exit 1. Current-source diagnostic failure identified the detector's initial PG restoration racing dependency-only ready=200, followed by terminal postgres decision begin failure and closed ops. The repair does not retry or conceal true restore errors. A new real retained-state assertion separately showed the original owner remaining not-ready after another replica joined; the callback characterization identifies the stale retained generation.

## GREEN Verification

All exact named families were invoked independently through scripts/go-test-nonempty.sh. Pure limits are 60s; ops/adapter/worker guards use -race where shown. Ordinary real commands use -tags=integration -timeout 6m -p 1; cooldown uses -timeout 8m. Values below are actual Go package/test durations, not invented wall time.

| Exact command/family | Exit | Final observed result |
| --- | ---: | --- |
| './internal/detection/... ./internal/opshttp/...' '^TestStateBounds$' -timeout 60s | 0 | 0.475s; actual aggregate 10k-window, 100k same-time ledger, 64 MiB, 4096-ID, exact-port and unchanged-checkpoint proofs |
| './internal/detection/... ./internal/config/...' '^TestStateBounds$' -timeout 60s | 0 | 0.606s; safe retirement/committed pruning and aggregate arithmetic included |
| Same detection/config packages, '^TestCheckpointPrefixAndSealed$' -timeout 60s | 0 | 0.018s; complete prefix/sealed/ack/strict metadata guard |
| Same packages, '^TestRulesConfig$' -timeout 60s | 0 | 0.010s; existing threshold/count/duration overflow guards |
| Same packages, '^TestDetectorSettings$' -timeout 60s | 0 | config 0.016s; actual startup settings regression |
| './internal/detection/... ./internal/opshttp/...' '^TestLateMetric$' -timeout 60s | 0 | reducer 0.007s / ops 0.004s |
| './internal/opshttp/...' '^TestLateMetric$' -race -timeout 60s | 0 | 1.013s; exact fixed exposition, concurrent increments, per-instance isolation and old-caller route preservation |
| './internal/opshttp/...' '^TestReady' -race -timeout 60s | 0 | 1.389s; sixteen admitted check workers, context-ignoring dependencies, independent response deadline and panic cleanup |
| './internal/opshttp/...' '^TestHealth' -race -timeout 60s | 0 | 1.015s |
| './internal/opshttp/...' '^TestDrain' -race -timeout 60s | 0 | 1.017s |
| './internal/detector/...' '^TestWorkerReadiness$' -race -timeout 60s | 0 | 1.013s; prejoin, joined-empty, pending restore, revoke/token/generation invalidation |
| './internal/kafkax/...' '^TestDetectorAssignmentReadiness$' -race -timeout 60s | 0 | 1.023s; empty callback versus prejoin and fatal/revoke invalidation |
| './internal/detector/...' '^TestRetainedOwnerRebinding$' -race -timeout 60s | 0 | 1.014s; register failure/concurrent revoke/generation preserve owner/checkpoint and not-ready |
| './internal/kafkax/...' '^TestDetectorRetainedAssignment$' -race -timeout 60s | 0 | 1.025s; retained lifetime token, actual new generation |
| './internal/kafkax/...' '^TestDetectorCommitMetadata$' -race -timeout 60s | 0 | Existing callback completeness/ownership/history/commit guards pass |
| './test/...' '^TestDetectorCapacity$' ordinary real flags | 0 | Final test 7.12s/package 24.979s; 4097th ID nonzero/not-ready, exact retained broker metadata; exactly one runtime late count; real nine-event checkpoint before join, both replicas ready, peer leave, tenth event produces one alert with all ten original ordered IDs |
| './test/...' '^TestReadyzDependencies$' ordinary real flags | 0 | Final test 38.14s/package 53.813s; all actual dependency outage/recovery checks |
| './test/...' '^TestDetectorConfigRestart$' ordinary real flags | 0 | Final test 22.02s/package 38.452s; stopped-owner/sole-new-member procedure and unchanged multi-replica coexistence retained |
| './test/...' '^TestDetectorCooldownReplay$' cooldown real flags | 0 | Final package 29.855s; immutable decisions/current fencing/PG frontier and real lock failure retaining checkpoint |
| './test/...' '^TestDetectorCheckpoint$' ordinary real flags | 0 | Final package 23.858s; stored-byte restart and terminal corrupt recovery |
| './test/...' '^TestDetectorRetention$' ordinary real flags | 0 | Package 26.277s; trim/shorter-history fail closed |
| './test/...' '^TestDetectionPipelineAuth$' ordinary real flags | 0 | Package 31.070s |
| './test/...' '^TestDetectionPipelineScan$' ordinary real flags | 0 | Package 28.686s; independent shared SCAN wording debt remains |
| './test/...' '^TestDetectionPipelineVolume$' ordinary real flags | 0 | Package 24.267s |
| timeout 300s make test | 0 | Final complete Go race suite, web 6 tests, generator 99 tests; final worker 1.016s / Kafka 1.087s. Earlier uncached final reducer suite 62.837s under concurrent real/static load; explicit affected race run also passed under its 60s limit (55.375s). |
| timeout 300s make lint | 0 | Standard Go 0 issues, web lint/typecheck, Python Ruff/format/mypy |
| cd backend && timeout 180s go build ./... && timeout 180s go vet -tags=integration ./... | 0 | Actual production build and integration vet |
| cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./... | 1 | Final sole pre-existing test/recovery_test.go:162 unparam, deferred to 03-14; no clean integration-lint claim |
| git diff --check; artifact, commit, deletion and stub checks | 0 | No missing artifact, unexpected deletion, whitespace issue or unfinished runtime stub |

Context7 franz-go documentation was consulted, then pinned v1.22.1 consumer_group.go confirmed GroupMetadata's absent sentinel, generation timing and callbacks always occurring even with empty added sets. No dependency installation or remote write occurred.

## Deviations from Plan

1. [Rule 2 — Required restore readiness] The parent approved four extra files: detector/readiness_test.go, kafkax/detector_consumer.go, kafkax/detector_consumer_test.go and test/readyz_test.go. A dependency ping could advertise ready before required PG restore. Added actual observed/restored snapshot gating, focused assertion RED/GREEN and failure-only service/process diagnostics. No readiness deadline, assertion or timeout was weakened. Commit 5dcaed2 records the additional RED; 433fb4b implements the repair.
2. [Rule 1 — Retained generation ownership] Current readiness exposed an inherited cooperative-retention defect. The parent explicitly approved same-token current-generation PG re-registration and immutable-frontier verification while preserving the live reducer state, plus unit failure/race guards and real 9/join/leave/10 evidence. Fresh/reacquired tokens still restore fully. Commit 433fb4b; actual callback/worker assertion RED precedes it.
3. [Rule 3 — Reuse existing strict checkpoint implementation] checkpoint.go already provides the metadata validation this slice needs; no cosmetic edit was made. Existing rule caps/duration rejection and real per-window terminal behavior also passed before implementation and are recorded as preserved guards, not invented RED.

Karpathy guidance kept changes tied to these observed admission/readiness gaps. Systematic debugging and test-first guidance kept the inherited baseline failure, retained-generation RED and fixture correction distinct from production proof.

## Issues Encountered

- The first accounting implementation copied bounded touched ID maps per input. It was replaced by scalar deltas and a one-time threshold evidence copy; no full-state clone is used.
- Initial real readiness output was reduced with tail and lost the assertion detail. A full-output run and parent-approved failure diagnostics identified the detector's pending restore; an untouched baseline archive reproduced it. No repeated build was treated as a fix.
- The first new alert observer used one PollRecords call; the final guard loops under the same bounded context, as the existing pipeline tests do. A subsequent observed PG invariant failure was a fixture collision: capacity and retained-owner subcases reused the same global tenant/rule/entity/window identity in different groups. A unique pseudonymous retained principal preserves the immutable foreign-group rejection and the exact evidence assertion. These fixture failures are not counted as production RED.
- Owned lint complexity/line/format findings were fixed with focused helpers and formatting. Several read/gofmt commands used root-relative paths while cwd was backend and failed before edits; corrected immediately, with no successful-verification claim for those commands.
- The quota interruption preserved committed work and in-flight test output; the user resumed the same plan. No authentication gate, user-stack reset, branch/worktree switch, stash, destructive cleanup or unapproved installation occurred. Test fixtures managed only their own containers/topics.

## Known Stubs and Threat Review

No unfinished runtime scaffold remains. Empty new state, nil maximum, explicit none frontier, unconfigured metrics on old callers and pending readiness are intentional states. All actual runtime decisions still use the real PG store. No unplanned public endpoint, auth path, schema or file-access boundary was introduced.

T-03-09-01: prospective checked aggregate admission and immutable rejection; no eviction of pending/live state. T-03-09-02: one fixed numeric metric, no labels or event data. T-03-09-03: unchanged sixteen-worker/independent deadline handler plus current restored-assignment readiness and terminal lifecycle propagation. No open high finding was identified in this slice. The accounted limits are not an RSS guarantee or benchmark.

## Next Plan Readiness

- Fifteen of twenty-one Phase 3 plans are implemented after this summary; the parent owns Wave 9's cross-plan gate after 03-21. Global requirement checkboxes and phase completion remain untouched.
- Existing independent shared SCAN canonical-summary mismatch and recovery_test.go:162 integration-lint debt remain for 03-13/03-14. Nothing here claims the complete static canonical scenario fixture, full Phase 3 security/CI or preserved-stack acceptance passes.
- Poison/DLQ, complete crash/actual ownership transfer/datastore restore-behind/outage matrices, live scheduling and preserved-volume scenario acceptance remain later-plan obligations. The cooperative retained-state test is not the complete 03-12/03-20 ownership transfer matrix.

## Self-Check: PASSED

- All six created and eight modified source/test artifacts exist; checkpoint.go was intentionally unchanged.
- All six listed proof/implementation commits exist; RED precedes GREEN and no tracked file was deleted.
- Required independent named families and final strengthened capacity/readiness regressions pass; existing integration-lint debt is explicitly retained.
- Stub/threat scans found no unfinished runtime capability or unplanned public trust boundary.
- This verified summary is committed before serialized STATE/ROADMAP updates.
