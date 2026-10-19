---
phase: 03-detection-alert-pipeline
plan: "07"
subsystem: detection
tags: [go, kafka, checkpoints, replay, event-time, tdd]
requires:
  - phase: 03-05
    provides: Frozen source-aware canonical candidates
  - phase: 03-19
    provides: Durable cooldown decisions and PostgreSQL-verified checkpoint frontiers
  - phase: 03-17
    provides: Native immutable alert persistence
provides:
  - Exact canonical-byte and live-state equivalence at 3410 restart cuts and 156 successive restart points
  - Strict bounded format-2 metadata with explicit empty clocks and rejection of missing, duplicate or null scalar fields
  - Acknowledged checkpoint adoption tied to current assignment and verified retained history bounds
  - Real broker metadata, stored-byte restart, corruption, retention and shorter-history acceptance proofs
affects: [03-08, 03-09, 03-11, 03-12, 03-20, 03-13, 03-14]
tech-stack:
  added: []
  patterns: [Uninterrupted byte oracle, explicit nullable clocks, callback-authoritative checkpoint adoption]
key-files:
  created:
    - backend/internal/detection/checkpoint.go
    - backend/internal/detection/checkpoint_test.go
    - backend/internal/detection/replay_test.go
    - backend/test/detector_checkpoint_test.go
  modified:
    - backend/internal/detection/reducer.go
    - backend/internal/kafkax/detector_consumer.go
    - backend/internal/kafkax/detector_consumer_test.go
key-decisions:
  - "03-07: Require every format-2 field, explicit null clocks, unique JSON members and non-null scalar gates/frontiers before restoring source state."
  - "03-07: Adopt the complete checkpoint set only after successful synchronous callback and unchanged assignment tokens; retain verified assignment history start."
  - "03-07: Replay uses uninterrupted runtime canonical bytes plus independent shared fixture identity sets; the pre-existing SCAN summary mismatch remains unresolved phase acceptance debt."
requirements-completed: [ALRT-03, DET-06, DET-07]
duration: approximately 5h 15m elapsed including quota interruption; active work not independently timed
completed: 2026-10-19
---

# Phase 3 Plan 7: Checkpoint Reconstruction Summary

Bounded format-2 checkpoints now reject incomplete recovery context, and 3,410 tested restart cuts preserve exact canonical alert bytes and complete live window state.

## Performance

- Execution began after the Wave 6 commit at 2026-10-18T23:38:22Z. The first explicit clock observation was 23:42:05Z; final verification completed by 2026-10-19T04:52:29Z.
- Approximately 5h 15m elapsed, including the quota interruption and user-requested resume. Active execution duration was not separately measured; elapsed time is not a throughput result.
- Tasks: 3/3. Source/test files: 7. The existing worker required no edit.

## Accomplishments

- Added six deterministic 178-record partition streams, each restarted at all 179 initial/final cuts, plus all 2,336 cuts from nine checked-in shared fixture inputs: 3,410 cuts total. Another 26 successive restart points per seeded run (156 total) exercise multiple pin advances. Every restart compares the union of exact canonical bytes keyed by alert ID and the complete private live-window maps: first offsets, counts, dedupe IDs, exact ports, sample order, frozen evidence, maximum, sealed floor, decision frontier and final checkpoint.
- Explicit fixtures cover overlapping 60/120/300s windows, fired and unfired state, closed SCAN/DENY history whose first contributor precedes the new pin, a pre-pin maximum at 300000ms, active 280000ms events, 269999ms late input, watermark equality, duplicate IDs and out-of-order records. Unacknowledged effects cannot seal, retire state or checkpoint. Snapshotting cannot prune prefix entries; successful broker acknowledgement permits pruning below the pin.
- Extracted recovery types and strict bounded metadata validation into checkpoint.go. All nine fields must be present with exact names. Only the two clock fields permit explicit null. Duplicate root/nested fields, absent or null decision-none, null effective gates, aliases/unknown fields, trailing documents, unknown format/config/version, invalid time/frontier/drained context and payloads over 1024 bytes fail closed. The populated frontier plus all four versions and effective gates fit within the cap in the proof.
- Preserved the existing proven pre-pin maximum/sealed-floor reconstruction algorithm. Lateness starts from the maximum immediately before the pin; the saved durable floor suppresses closed windows. Valid nonmatching events advance time; poison/idle guards remain intact. Fired state retains its first offset through event-time expiry.
- Adapter commits use the pinned PreCommitFnContext and synchronous void CommitOffsetsSync callback. Existing transport/partition/nil/missing/duplicate/unexpected response validation is retained. Successful complete callbacks now record the requested checkpoint set only after all assignment tokens are rechecked; failed callbacks and stale completions leave previous checkpoints unchanged. New and existing groups both require finite successful start/end lookups. Only offset -1 means absent; the verified retained start is recorded on the assignment. Existing groups keep NoResetOffset.
- Real default-binary tests inspect actual OffsetFetch metadata, confirm a populated frontier and pre-pin/sealed clocks, restart with the same group/config and observe identical stored canonical bytes. Missing/malformed/unknown/oversized/omitted-prefix metadata exits nonzero, cannot remain ready, and preserves the exact broker offset/metadata. Real DeleteRecords trims only a newly created UUID test-owned topic to start=2; old pin=0 fails, while a separate new group consumes retained earliest. A checkpoint beyond an independently read end simulates restored shorter/truncated history and fails without reset. This last case does not physically truncate a broker log.
- Existing worker ordering remains PG frontier verification before records, PG immutable decision commit, stored-byte publication, source effect acknowledgement, then checkpoint. Existing assignment/revoke/loss handling and lifecycle propagation were reused; the bounded process-failure tests exercise terminal hook errors through the real binary. Full crash/owner/outage matrices remain later plans.

## Task Commits

1. Task 1: compiling checkpoint/replay/broker proofs and behavioral RED — `d255ef0`.
2. Task 2: strict bounded recovery model GREEN — `48dac29`; null-scalar hardening and lint-driven helper extraction — `b8351c6`.
3. Task 3: acknowledged owned checkpoint adoption and verified history boundaries — `0e93039`.

The proof commit precedes both model and adapter GREEN commits. No tracked file was deleted. Original dirty .planning/config.json and untracked .serena/ remain excluded.

## RED Evidence

Named families were invoked independently through scripts/go-test-nonempty.sh after successful compilation. No missing API/import or setup error counts as RED.

| Independent selector | Exit | Actual result |
| --- | ---: | --- |
| `^TestCheckpointPrefixAndSealed$` (`./internal/detection/...`, `-timeout 60s`) | 1 | Existing restore accepted omitted prefix, sealed floor, processed_next/drained fields and duplicate/null metadata; package 0.007s. Follow-up null-effective-gate assertion also failed, package 0.006s, before its fix. |
| `^TestReplayEveryRestartCut$` (`./internal/detection/...`, `-timeout 60s`) | 0 | Existing reducer passed seeded restart equivalence; final shared-input and repeated-pin extension also passed before production refinement, 5.555s. This is a preserved passing guard. |
| `^TestDetectorCommitMetadata$` (`./internal/kafkax/...`, `-timeout 60s`) | 1 | Complete successful callback did not update recorded checkpoints; fresh group accepted absent history bounds; package 0.006s. |
| `^TestDetectorCheckpoint$` (`./test/...`, `-tags=integration -timeout 6m -p 1`) | 1 | Actual detector remained running after committed metadata omitted its explicit prefix field; bounded 10s assertion failed. Real stored-byte replay and other corruption guards passed. Test 15.86s / package 31.059s. |
| `^TestDetectorRetention$` (same real flags) | 0 | Existing trimmed-pin and shorter-history rejection plus fresh retained-earliest behavior passed; test 1.54s / package 20.000s. |

An attempted full static fixture canonical comparison exposed an existing SCAN summary disagreement, detailed under Deferred Issues. It is not counted as a new replay-model RED and was not silently changed into a fixture pass.

## GREEN Verification

Every named selector below ran independently and nonempty. The two prescribed combined-package invocations also passed independently for each of the three pure families. Unit timeout is 60s; ordinary real timeout is 6m; cooldown replay timeout is 8m; all real invocations use -tags=integration -p 1. Durations below are Go-reported package/test values, not invented wall-clock timings.

| Command | Exit | Actual result |
| --- | ---: | --- |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestCheckpointPrefixAndSealed$' -timeout 60s` | 0 | Final 0.008s; strict field/null/duplicate guards, prefix/sealed/ack/prune behavior and populated bounded metadata. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestReplayEveryRestartCut$' -timeout 60s` | 0 | Final 6.381s; all 3,410 cuts and 156 successive restarts preserve byte union and live state. |
| `scripts/go-test-nonempty.sh './internal/kafkax/...' '^TestDetectorCommitMetadata$' -timeout 60s` | 0 | Final 0.006s; all callback error/completeness, stale completion, terminal fetch and ownership/start/reset guards. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestFrozenEvidence$' -timeout 60s` | 0 | Final 0.006s; AUTH/SCAN frozen evidence unchanged. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestWatermarkLimit$' -timeout 60s` | 0 | Final 0.006s; equality, late drop, nonmatch, poison/idle guards. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCheckpoint$' -tags=integration -timeout 6m -p 1` | 0 | Final test 5.94s / package 28.492s; actual metadata/stored-byte restart and all corrupt metadata terminal cases. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorRetention$' -tags=integration -timeout 6m -p 1` | 0 | Final test 1.60s / package 23.124s; actual test-topic trim, unchanged failed checkpoint, fresh group retained-earliest and shorter-end simulation. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` | 0 | Test 4.58s / package 27.527s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineScan$' -tags=integration -timeout 6m -p 1` | 0 | Test 5.07s / package 30.774s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineVolume$' -tags=integration -timeout 6m -p 1` | 0 | Test 3.14s / package 24.080s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownReplay$' -tags=integration -timeout 8m -p 1` | 0 | Test 9.07s / package 28.136s; old decisions, exact bytes, PG frontier ahead/behind/mismatch and bounded locked-fence source retention. |
| `timeout 300s make test` | 0 | Final complete Go race suite (detection 37.866s, Kafka 1.083s), web 6 tests, generator 99 tests. |
| `timeout 300s make lint` | 0 | Final standard Go 0 issues, web lint/typecheck and generator Ruff/format/mypy. |
| `cd backend && go test -race ./internal/detection/... ./internal/kafkax/... ./internal/detector/... -timeout 60s` | 0 | Detection 39.912s / Kafka 1.114s; final full make test above additionally covers later null-guard/helper refinements. |
| `cd backend && timeout 180s go build ./... && timeout 180s go vet -tags=integration ./...` | 0 | Production build and integration vet; successful again as the first two steps of the final integration-lint chain. |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | 1 | Final sole finding: pre-existing test/recovery_test.go:162 unparam, deferred to 03-14. No clean integration-lint claim. |
| `git diff --check`; file, commit and deletion checks | 0 | No whitespace errors, missing claimed files/commits or tracked deletions. |

Context7 franz-go documentation was consulted and exact 1.22.1 module source confirmed the PreCommitFnContext and void synchronous callback signatures. Existing pinned dependencies were reused; no install occurred.

## Deviations from Plan

No architecture or file-ownership expansion. The plan listed worker.go, but its existing lifecycle, ownership and PG frontier verification already satisfy this slice and required no edit. Refinement kept the already-passing reducer recovery algorithm and hardened its metadata representation. Lint-driven extraction keeps validation and reusable cut loops within project complexity limits. Karpathy guidance kept edits tied to these observed correctness gaps.

## Deferred Issues

- **Unresolved phase acceptance debt:** testdata/detection/scenarios.json expects SCAN summary `Distinct destination ports probed`; runtime evidence.go emits `Distinct destination ports on one host`. Full fixture canonical equality currently fails for SCAN/all inputs. This predates this plan and belongs to the later exact manifest/fixture acceptance in 03-13/03-14. Root was notified and explicitly retained this debt. No runtime wording or unowned fixture/golden was edited. This plan's shared-input proof compares every byte against uninterrupted runtime execution, while independently checking the exact fixture identity set; it does not claim static expected canonical fixture bytes pass.
- Existing integration lint recovery_test.go:162 unparam remains 03-14.
- 03-08 owns drained config/version transitions; 03-09 owns aggregate capacity/late metrics; 03-11 owns acknowledged poison effects; 03-12/03-20 own the full crash, actual ownership handoff, datastore restore-behind and outage matrices. The shorter-end simulation here is not a physical broker truncation experiment. Full phase integration, security, preserved-volume scenario and CI completion remain unclaimed.

## Issues Encountered

Initial standard lint found introduced duration construction, complexity, repeated-string and unused-nolint findings; helper extraction/formatting/constants resolved them. Integration lint also identified two introduced long lines, now fixed, while the historical unparam remains. Several gofmt attempts used root-relative paths with backend as cwd and exited before editing; corrected paths were formatted and rechecked. These command mistakes are not RED or successful verification evidence. The quota interruption preserved all owned deltas; execution resumed and fresh final named/static/full-test checks ran before closeout.

## Stub and Threat Review

No unfinished runtime scaffold, TODO/FIXME or mock-only recovery adapter remains. Empty output maps, initial none frontier and explicit null clocks are intentional state. New groups record the retained-history boundary; they do not claim deleted history. No new endpoint, auth route, schema or file access trust boundary was introduced.

T-03-07-01 is covered by exact canonical/live-state reconstruction and acknowledged retirement/pruning checks. T-03-07-02 is covered by the 1024-byte envelope, strict restore validation, bounded history requests and real terminal/nonready failure. T-03-07-03 is covered by transport/partition/response completeness and unchanged-token successful adoption tests. No open high finding was identified within this slice; deferred phase acceptance obligations remain explicit.

## Next Plan Readiness

03-08 can build drained handover on the tested format-2 model and existing PG frontiers. This is the thirteenth of twenty-one implemented Phase 3 plans; root's Wave 7 cross-plan gate is separate. Wave 6 already passed at a09cc92. Global requirement checkboxes remain incomplete, and Phase 3 is not complete.

## Self-Check: PASSED

- All four created files and three modified source/test files exist.
- d255ef0, 48dac29, b8351c6 and 0e93039 exist in proof-before-GREEN order and delete no tracked files.
- Every required named family passed its final independent nonempty invocation; known pre-existing fixture/integration-lint debt is explicit.
- This summary is committed before serialized STATE/ROADMAP updates.
