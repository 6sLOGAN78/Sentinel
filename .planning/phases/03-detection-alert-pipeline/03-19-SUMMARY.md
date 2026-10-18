---
phase: 03-detection-alert-pipeline
plan: "19"
subsystem: detection
tags: [go, postgres, kafka, cooldown, replay, fencing, tdd]
requires:
  - phase: 03-05
    provides: Immutable source-aware canonical candidates and fixed rule ordinals
  - phase: 03-18
    provides: Real PostgreSQL pool, lifecycle/readiness and bounded PG budgets
  - phase: 03-17
    provides: Existing immutable alert/workflow persistence
provides:
  - Durable immutable allow/suppress decisions, monotone entity cursors and partition ownership fences
  - Stored-byte publication after PG commit and fully effected checkpoint decision frontiers
  - Independent real visibility, equality, replay, schema and PG lock-failure proof
affects: [03-07, 03-08, 03-09, 03-20, 03-13, 03-14]
tech-stack:
  added: []
  patterns: [Fence then immutable lookup then cursor, bounded independent rollback, stable configured group incarnation]
key-files:
  created:
    - backend/internal/storage/postgres/detection_decisions.go
    - backend/internal/storage/postgres/detection_decisions_test.go
    - backend/test/detector_cooldown_test.go
    - backend/test/detection_decisions_schema_test.go
    - backend/migrations/postgres/004_detection_decisions.sql
    - backend/internal/detection/cooldown.go
  modified:
    - backend/internal/detector/worker.go
    - backend/internal/detection/reducer.go
    - backend/internal/kafkax/alerts.go
    - backend/internal/kafkax/detector_consumer.go
    - backend/test/detector_pipeline_test.go
key-decisions:
  - "03-19: Every runtime candidate commits an immutable PG allow/suppress before any allowed stored-byte publication; suppressions have no Kafka/CH/workflow effect."
  - "03-19: Lock the current partition fence, read the globally unique alert_id decision before any cursor access, then atomically create a missing ordered decision/cursor/frontier."
  - "03-19: Bind group incarnation deterministically to the configured group; the fence primary key excludes incarnation, and generation/member/token rollback fails closed."
  - "03-19: Checkpoint the last fully effected decision and verify its immutable row plus a PG fence at least that far on restore; legitimately newer PG state is accepted."
requirements-completed: [DET-08, DET-07, ALRT-03, ALRT-01]
duration: ~13 min timed verification/implementation plus initial context preparation
completed: 2026-10-19
---

# Phase 3 Plan 19: Durable Cross-Window Cooldown Summary

Adjacent AUTH windows now produce one visible alert and one durable suppression, with immutable replay, Kafka membership fences and a PostgreSQL-verified checkpoint frontier.

## Performance

- Earliest recorded real RED fixture startup: 2026-10-18T23:22:55Z; explicit clock observation: 23:23:30Z. Initial context preparation preceded these observations and was not separately timed.
- RED commit: 23:24:30Z; verified implementation commit: 23:35:19Z. Observed verification/implementation window: approximately 13 minutes, plus initial context preparation.
- Tasks: 2/2; implementation/test/schema files: 11 (eight planned, three required compatibility/frontier extensions).

## Accomplishments

- Added operational fences keyed by configured group/topic/partition, cursors keyed by group/config/tenant/rule/injectively encoded entity components, and globally unique immutable alert_id decisions. Decisions retain canonical bytes, immutable fingerprint, source offset/ordinal, original rule/config, trigger time, effective clock, disposition and generation. Source-position uniqueness and a SQL update-rejection trigger protect first decisions. No automatic cleanup/TTL is introduced.
- The transaction validates the locked ownership fence, looks up the globally unique alert_id before any cursor access, and returns an equal stored allow or suppress unchanged. Different group/source/config/bytes/fingerprint/trigger fails closed. An absent decision at/before the durable ordered frontier also fails closed. New decisions lock the cursor and atomically write disposition, monotone cursor and partition frontier.
- Cooldown uses max(previous maximum candidate time, trigger occurred_at). A clock below next eligibility suppresses; equality allows; suppression leaves eligibility unchanged. AUTH principals retain their entire pipe-containing suffix. Canonical IDs, first-threshold evidence, first 20 samples, event counts and window-end created_at remain unchanged.
- Assignment callbacks capture the actual Kafka member/generation, configured group and increasing assignment token. The worker validates that capture against current assignments and GroupMetadata before registration, decisions, publication and checkpoint commit. Durable registration rejects generation rollback, equal-generation wrong member, delayed lower assignment token and caller-chosen incarnation changes. Incarnation is a stable domain-separated hash of configured group; it cannot create another primary-key namespace.
- Every runtime candidate uses the real PG store. Allow ordering is PG commit → exact stored-byte all-ISR publication → effect acknowledgement/checkpoint. Suppression commits without publication or CH/workflow creation. No PG transaction spans Kafka publication. There is no feature flag, in-memory alternative or final no-op store.
- Acquisition/transaction/lock/statement/cleanup budgets from 18 are enforced through bounded contexts and transaction-local SQL settings. Every begun transaction has an independent bounded Rollback finalizer. Uncertain commit returns no successful decision and terminates without source advancement; restart resolves its outcome through immutable lookup/replay.
- Format-2 metadata now records the last fully effected offset/ordinal/ID/fingerprint. Restore validates the bounded marker and verifies its immutable PG row plus fence frontier before source effects. PG ahead is allowed, while a missing/mismatched row or older fence fails. The existing 1024-byte cap, pin, pre-pin maximum, sealed floor and processed_next semantics remain enforced.

## Task Commits

1. Task 1 — compiling behavioral/schema scaffolds and independent assertion RED: `99e19cb` (`test`).
2. Task 2 — real fenced immutable decision subsystem and publication/frontier integration: `b385283` (`feat`).

Both commits exist in RED-before-GREEN order. No tracked files were deleted. Original dirty .planning/config.json and untracked .serena/ were preserved and excluded.

## RED Evidence

All five named families existed and compiled before production behavior. Each command ran independently through the nonempty runner, with unit timeout 60s, cooldown integration timeout 8m and schema integration timeout 6m. Integration commands used -tags=integration -p 1 and real default detector binaries/containers.

| Independent selector | Exit | Actual assertion / package duration |
| --- | ---: | --- |
| `scripts/go-test-nonempty.sh './internal/storage/postgres/...' '^TestDetectionDecisionTransaction$' -timeout 60s` | 1 | Loadable store returned absent durable transaction error; immutable replay assertion failed; 0.005s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownVisible$' -tags=integration -timeout 8m -p 1` | 1 | Running detector published two nearby-window alerts where one is required; 22.300s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownExpiry$' -tags=integration -timeout 8m -p 1` | 1 | Running detector published three alerts where allow/suppress/equality requires two; 21.305s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownReplay$' -tags=integration -timeout 8m -p 1` | 1 | Successful running pipeline had no durable allow/suppress ledger; 21.296s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionDecisionsSchema$' -tags=integration -timeout 6m -p 1` | 1 | Real PG lacked detection_partition_fences; 25.165s |

A compile-only integration build also passed before the RED commit. No missing import/type/constructor or setup failure is counted as behavioral RED.

## GREEN Verification

Every named selector below ran independently, not only as a broad regex. Durations are Go-reported test/package durations. The three cooldown families were rerun after the final global-ID lookup refinement; schema was rerun after its final schema assertions.

| Command / selector | Exit | Actual result |
| --- | ---: | --- |
| `scripts/go-test-nonempty.sh './internal/storage/postgres/...' '^TestDetectionDecisionTransaction$' -timeout 60s` | 0 | Final package 0.005s: old allow/suppress returns before cursor, independently bounded cleanup, uncertain commit returns no decision, globally existing foreign group fails before cursor |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownVisible$' -tags=integration -timeout 8m -p 1` | 0 | Final test 7.60s / package 27.541s: 299s/301s one Kafka/CH/workflow identity plus durable suppress, later eligible distinct ID, actual restart/current generation and byte-identical stored replay |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownExpiry$' -tags=integration -timeout 8m -p 1` | 0 | Final test 5.54s / package 27.271s: exact expiry allows fresh short window; suppressed first decision never retries |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownReplay$' -tags=integration -timeout 8m -p 1` | 0 | Final expanded test 9.12s / package 28.039s: immutable old allow/suppress after newer cursor, exact canonical bytes, SQL mutation rejection, rule/tenant/IP/principal isolation, monotone out-of-order clock, divergent/missing old decisions, generation/member/incarnation/token rejection, PG-ahead/behind/missing marker checks and real lock-failure checkpoint retention |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionDecisionsSchema$' -tags=integration -timeout 6m -p 1` | 0 | Final test 0.06s / package 23.743s: additive/idempotent migration, global ID/source-position uniqueness, cursor/fence keys, ownership/frontier columns and immutable trigger |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` | 0 | Test 4.54s / package 28.159s; original 9/10/11, canonical evidence, pin and processed_next retained; real nonempty frontier matches PG immutable row |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineScan$' -tags=integration -timeout 6m -p 1` | 0 | Test 5.03s / package 29.242s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineVolume$' -tags=integration -timeout 6m -p 1` | 0 | Test 3.35s / package 24.089s; FLOOD/DENY regression |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorPGDependency$' -tags=integration -timeout 6m -p 1` | 0 | Test 7.50s / package 33.170s; real PG readiness/startup/paused-PG source retention |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWorkflowReplay$' -tags=integration -timeout 6m -p 1` | 0 | Test 1.36s / package 21.874s; resolved workflow replay remains insert-only |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterPipeline$' -tags=integration -timeout 6m -p 1` | 0 | Test 1.26s / package 24.084s; original CH→workflow→source order |
| `scripts/go-test-nonempty.sh './test/...' '^TestReadyzDependencies$' -tags=integration -timeout 6m -p 1` | 0 | Test 38.25s / package 57.462s; actual dependency outage/recovery |
| `scripts/go-test-nonempty.sh './internal/contracts/...' '^TestDedupeKey' -timeout 60s` | 0 | Unchanged identity goldens/collision/normalization guards; 0.010s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestFrozenEvidence$' -timeout 60s` | 0 | AUTH/SCAN complete evidence and sample guards; 0.004s |
| `timeout 300s make test` | 0 | Complete Go race suite, web 6 tests and generator 99 tests |
| `timeout 300s make lint` | 0 | Standard Go 0 issues, web lint/typecheck, generator Ruff/format/mypy |
| `cd backend && go test -race ./internal/storage/postgres/... ./internal/detection/... ./internal/kafkax/... ./internal/detector/... -timeout 60s` | 0 | Repeated after final global-ID delta; PG 1.021s, detection 1.527s, Kafka 1.166s |
| `cd backend && timeout 180s go build ./... && timeout 180s go vet -tags=integration ./... && timeout 180s go tool -modfile=tools.mod golangci-lint run ./...` | 0 | Final source build, integration vet and standard Go lint after final global-ID delta |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | 1 | Final only pre-existing test/recovery_test.go:162 unparam; explicitly deferred to 03-14, no clean integration-lint claim |
| `git diff --check`; created-file/commit/deletion checks | 0 | No whitespace error, missing claimed artifact or tracked deletion |

The real lock-failure subtest first obtains a successful checkpoint with nine contributors in the next window, locks that partition's actual PG fence, then publishes the single crossing event. The process exits nonzero within five seconds; broker offset and complete metadata remain byte-identical to the saved successful checkpoint, no alert publishes, and the ledger retains only the first decision. This is focused failure proof, not the complete 03-20 crash/rebalance matrix.

Context7 documentation for pinned pgx v5.11.0 confirmed explicit Commit/Rollback requirements and independent cleanup after canceled/uncertain operations. Existing pinned dependencies were reused; no installation occurred.

## Deviations from Plan

**1. [Rule 2 - Missing critical frontier/ownership plumbing] Extend reducer and Kafka assignment adapter.**
- Existing reducer always wrote frontier.none and rejected every populated marker; the worker could not satisfy restore consistency without changing that contract.
- Added effected-frontier persistence/validation in reducer.go and cooldown.go. Captured actual configured group/member/generation in detector_consumer.go assignments; worker interface now exposes real GroupMetadata.
- The parent explicitly approved reducer.go and detector_consumer.go before editing. No fabricated membership, caller-selected fresh incarnation, no-op store or in-memory substitute was introduced.
- Files: backend/internal/detection/reducer.go, backend/internal/kafkax/detector_consumer.go (plus planned files); commit b385283. Real restart/canonical replay, stale registration and PG frontier tests pass.

**2. [Rule 3 - Required fixture compatibility] Upgrade AUTH's intermediate empty-frontier assertion.**
- detector_pipeline_test.go previously asserted frontier.none=true as an explicitly intermediate contract. The parent approved replacing it with actual offset=9/ordinal=0/ID/fingerprint and a real PG allowed-row match.
- All prior negative 9/10/11, exact canonical evidence, live pin and processed_next assertions remain. Commit b385283; independent AUTH acceptance passes.

No architecture/package expansion or public API was needed. Karpathy guidance kept changes confined to the durable decision contract and these necessary links.

## Issues Encountered

- Initial expanded replay expected a whole-second cursor; canonicalWriterEvents advances each of ten events by 1ms, so the actual crossing is +9ms. Corrected the fixture to assert the frozen candidate's actual trigger and +cooldown. Equality behavior itself passed.
- The first added restart proof terminated after seeing new PG registration but before active-window replay finished; old broker metadata had satisfied its catch-up check. It now observes the exact republished canonical payload before termination. The final real restart proof passes without relaxing production ownership/PG behavior.
- Existing transport-driver callback tests pass nil for the callback's concrete client. Capturing membership through the adapter's existing client interface preserves the same real runtime GroupMetadata source and those tests; no fallback identity was added.
- Owned lint findings were corrected (bounded token conversion documentation, line lengths, unit variable shadows and helper extraction). Several gofmt calls initially used root-relative paths while already in backend and failed before testing; corrected immediately. These command errors are not RED/verification evidence.
- Final global-ID review removed the group filter from immutable lookup and validates stored group explicitly, ensuring a foreign-group collision also fails before cursor reads. A focused scripted proof and final real cooldown/race/build/vet/lint reruns pass.
- No auth gate, install, remote write, branch/worktree operation or existing user-stack/data reset occurred. Only test-owned containers were managed by existing fixtures.

## Known Stubs and Threat Review

No runtime scaffold or unfinished stub remains for this plan. Fresh cursor zero timestamps, a none frontier before the first decision, and empty nonmatching outcomes are intentional states. All actual runtime candidates use the real durable store. Configuration changes continue to fail closed until the separately planned 03-08 drained handover; schema drained/sealed fields reserve that additive protocol and do not bypass current config checks.

T-03-19-01: immutable first decisions return before cursor access, divergent reconstruction/missing older positions fail, and real old allow/suppress replay leaves the newer cursor unchanged. T-03-19-02: stable configured-group identity plus actual captured/current Kafka membership and durable generation/member/token checks fence new decisions; focused stale/rollback tests pass. T-03-19-03: transaction-local SQL bounds, independent Rollback and no PG transaction during publication are implemented; real blocked-fence failure preserves the exact last successful Kafka checkpoint and makes the service terminate. No new public endpoint, auth path or unplanned trust boundary was introduced. No open high finding was identified within this slice; full security/crash/ownership verification remains later work.

## Next Plan Readiness

- 03-07 can exercise populated frontiers together with broader pin/sealed-prefix replay. 03-08 owns coordinated drained config/version handover and new-generation cursor adoption. Existing configs cannot silently change.
- 03-20 still owns the full before/after decision commit/publication/checkpoint crash matrix, actual multi-replica target ownership transfer and delayed-owner races, datastore-restore-behind worker scenario, commit/outage failpoints and repeated stress. This plan's focused store/frontier/lock/restart proofs do not claim that entire matrix.
- Decisions have no automatic GC; operational disk growth and Kafka/PG backup/restore consistency require documentation. A stopped explicit maintenance migration is required for a deliberate group-generation reset; random incarnation substitution cannot reset the fence.
- Broader poison/prefix/state bounds/late metrics/live scheduling/Compose preserved-data acceptance and full phase security/CI remain pending. Global requirement checkboxes stay incomplete; frontmatter IDs identify this plan's slice. Phase 3 is not complete.

## Self-Check: PASSED

- All six created files and five modified implementation/compatibility files exist.
- Commits 99e19cb and b385283 exist in RED-before-GREEN order; neither deletes tracked files.
- All five independent named families reach assertion RED before implementation and pass after it; affected real regressions and final static/race checks are recorded above.
- Stub/security surface scan found no unfinished runtime capability or unplanned public surface. Known deferred integration lint and remaining 03-20 proof obligations are explicit.
- SUMMARY is committed before serialized STATE/ROADMAP updates.
