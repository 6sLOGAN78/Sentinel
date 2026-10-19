---
phase: 03-detection-alert-pipeline
plan: "12"
subsystem: streaming
tags: [go, kafka, postgres, clickhouse, recovery, rebalance, tdd]
requires:
  - phase: 03-11
    provides: Bounded durable effect prefixes and poison bookkeeping
  - phase: 03-09
    provides: Current-assignment readiness and retained-owner reconstruction
  - phase: 03-07
    provides: Strict format-2 metadata and authoritative checkpoint adoption
provides:
  - Causal nine-of-ten SIGKILL and three exact publication/checkpoint crash-cut acceptance
  - Actual seeded target ownership transfer to a third detector with restore before event ten
  - Private integration-only observations and unconditional production no-op hooks
  - Authoritative offset-fetch completion gate for newly acquired assignment lifetimes
affects: [03-20, 03-13, 03-14]
tech-stack:
  added: []
  patterns: [private finite causal barriers, callback-authoritative observations, pending offset restoration]
key-files:
  created:
    - backend/test/detector_recovery_test.go
    - backend/test/detector_rebalance_test.go
    - backend/internal/detector/failpoint_test.go
    - backend/internal/detector/failpoint.go
    - backend/internal/detector/failpoint_integration.go
  modified:
    - backend/internal/detector/worker.go
    - backend/internal/kafkax/detector_consumer.go
    - backend/internal/kafkax/detector_consumer_test.go
key-decisions:
  - "03-12: Observe exact incorporated-record, acknowledged-publication and successful-checkpoint boundaries through private integration-only files; production hooks read no environment."
  - "03-12: Prove actual seeded target oldOwner != newOwner and restored broker metadata before the tenth event; live replay pin is not consumed-next."
  - "03-12: New assignment lifetimes remain pending until complete authoritative offset/history/config validation; stale token/member/generation completion cannot unlock processing, readiness or commit."
requirements-completed: [ALRT-03, ALRT-04, DET-06, DET-07]
duration: approximately 4h 10m elapsed including interruption
completed: 2026-10-19
---

# Phase 3 Plan 12: Causal Crash and Ownership Recovery Summary

Private causal crash barriers now prove nine-of-ten recovery, all three publish/checkpoint cuts and a seeded partition's actual third-replica transfer against Kafka and both native alert stores.

## Performance

- Tasks: 3/3; application/test files: eight, all within the declared ownership boundary.
- First explicit execution clock: 2026-10-19T07:59:32Z; resumed clock: 11:33:54Z; final verification/closeout approximately 12:10Z. Approximately 4h 10m elapsed from the first recorded clock, including an intentional interruption; earlier context-loading and active-only time were not independently timed.
- This slice completes implementation evidence only. The parent owns the Wave 11 gate; global requirement checkboxes and Phase 3 completion remain pending.

## Accomplishments

- Default `!integration` hooks are unconditional no-ops and never read failure environment settings. Both a production-binary unit guard and actual default-binary recovery/publication with fault settings prove no reached/control-file effects. TestMain continues to build default binaries; only dedicated fixtures request integration binaries.
- Integration hooks exclusively create mode-0600 observations inside actual mode-0700 directories, bound observation count, select a once-only exact partition/offset boundary, and wait cancellably for at most 30 seconds. No public debug endpoint or new dependency was added. Assignment observations flow through an optional injected callback, with no reverse adapter import.
- The after-record barrier proves nine events incorporated before SIGKILL. One-record batches independently expose the durable predecessor: broker pin 0 / processed_next 8 while event nine is volatile. Restart replays nine and ten; final processed_next 10 retains live pin 0. This is not a claim that the ninth event had already checkpointed.
- Before-publish occurs before PublishCanonical. After-ack occurs only after complete acknowledged publication, before reducer effect acknowledgement/checkpoint. After-successful-checkpoint occurs only after authoritative successful Kafka callback and successful PG checkpoint bookkeeping. Failed/partial callbacks and stale assignments cannot manufacture successful observations. The hook structure remains extensible for Plan 03-20's before/after PG-commit cuts.
- Each cut inspects broker metadata, exact canonical Kafka key/value bytes against an independently reduced expected alert, immediate CH CountUnique=1 plus complete canonical detail, PG canonical decision bytes and one workflow. Before-publish has no pre-cut Kafka/CH/workflow effects. The two published cuts resolve and modify the workflow before kill; replay preserves the entire row, including sequence, status, assignee, version and timestamps. After successful checkpoint, replay preserves the exact checkpoint bytes too.
- The third-member fixture seeds nine AUTH events on each of twelve partitions under two actual processes. Bounded broker membership/assignment queries and private per-process revoke/restore/commit observations identify a seeded target that actually moves to the third process. The old process revokes that target, the new process restores checkpoint nine and replays through nine before the tenth is sent. No unrelated assignment change or elapsed sleep substitutes for ownership proof.
- New assignment lifetime tokens now remain RestorePending until authoritative offset fetch, retained-history validation and config authorization all succeed. Idle and nonempty worker paths, readiness, PG restoration, state mutation, effects and commits cannot run early. Completion atomically checks exact token/member/generation and the current driver identity; stale revoked/reacquired completion cannot clear newer pending state. A successfully validated new group may still have nil checkpoint metadata. Retained cooperative tokens, reducer windows and owner-rebinding checks remain unchanged.

## Task Commits

1. Task 1 — `dcd085f`: compiled causal crash/seeded ownership assertion RED fixtures and production build-boundary guard.
2. Task 3 observer RED — `d832c62`: callback-authoritative assignment and successful-checkpoint observations.
3. Task 2 — `6574c22`: private finite integration barriers and exact worker boundaries.
4. Task 3 — `c14ffc7`: callback observations and restored actual-target acceptance.
5. Approved cancellation deviation RED — `7ae8cfe`; GREEN — `e01326d`.
6. Approved pending-fetch deviation RED — `8392c89`; GREEN — `cf9367b`.

All commits exist in this order. No tracked file was deleted.

## Recorded Verification

Each named family used its own `scripts/go-test-nonempty.sh` invocation. Unit selectors use `-timeout 60s`; real selectors use `-tags=integration -timeout 6m -p 1`. Package times include TestMain/container setup and teardown, not just the named test. Repetitions below are real executions, not inferred coverage.

| Stage / independent selector | Actual result |
| --- | --- |
| Initial `./test/...` `^TestDetectorMidWindowRecovery$` | Compiled assertion RED, exit 1: missing after-record observation; test 22.48s / package 38.753s. |
| Initial `^TestDetectorCrashBoundaries$` | Compiled assertion RED, exit 1: all three missing causal cut observations; test 63.17s / package 78.648s. |
| Initial `^TestDetectorThirdReplicaRebalance$` | Compiled assertion RED, exit 1 after all twelve real broker checkpoints reached pin 0 / next 9; missing assignment observations; test 22.94s / package 39.159s. Strengthened pre-observer repeat also RED, package 39.694s. |
| `./internal/kafkax/...` `^TestDetectorCommitMetadata$`, before observer implementation | Assertion RED, exit 1, package 0.008s: missing assigned/revoked and acknowledged-commit observations. Existing callback-error rejection remained a passing guard. |
| `./internal/detector/...` `^TestDetectorFailpointBuildBoundary$` | Existing/no-op characterization PASS, not RED: initial package 0.883s; later independent PASS 0.923s, exit 0. Final full suite includes this guard. |
| Final `./internal/kafkax/...` `^TestDetectorPendingOffsetFetch$ -race` | PASS, exit 0, package 1.023s: delayed validation, commit blocked, legitimate completed new-group nil metadata and stale token/member/generation completion. Before behavior: assertion RED exit 1, 0.019s, showing readiness and broker commit admitted before offset fetch. |
| Final `./internal/detector/...` `^TestDetectorPendingSource$ -race` | PASS, exit 0, package 1.019s: idle performs no PG restore and nonempty cannot mutate clocks/checkpoints or commit effects. Before behavior: assertion RED exit 1, 0.028s, showing premature PG restore and source mutation. |
| Final `./internal/kafkax/...` `^TestDetectorCommitMetadata$ -race` | PASS, exit 0, package 1.028s; callback completeness, ownership and authoritative-success observations preserved. |
| Final `^TestDetectorMidWindowRecovery$` after pending-fetch repair | PASS, exit 0, test 49.82s / package 70.695s. Earlier independent GREENs: test 50.78s / package 65.746s and 49.79s / package 66.141s. |
| Final `^TestDetectorCrashBoundaries$` after pending-fetch repair | All three PASS, exit 0, test 148.11s / package 174.420s; cuts 49.09s, 49.07s, 49.01s. Earlier complete GREEN runs: packages 163.026s, 165.449s and 167.438s. |
| Final `^TestDetectorThirdReplicaRebalance$ -count=3` after pending-fetch repair | All three PASS, exit 0, package 42.044s. Earlier strengthened GREENs: packages 25.457s, 24.425s, 23.716s; diagnostic count=5 all PASS, package 65.821s. The separately recorded failed run below is not erased by these passes. |
| Final independent real `^TestDetectorCheckpoint$` | PASS, exit 0, test 6.20s / package 35.457s. |
| Final independent real `^TestReadyzDependencies$` | Fresh exact-selector verification after correcting the summary's mistaken Detector prefix: PASS, exit 0, test 37.93s / package 53.357s, using `scripts/go-test-nonempty.sh './test/...' '^TestReadyzDependencies$' -tags=integration -timeout 6m -p 1`. The earlier recorded readiness result was test 38.42s / package 54.795s; its command text was not independently recoverable during the docs correction, so this fresh run establishes the exact selector. |
| Separate real retained `^TestDetectorCapacity$` / `^TestDetectorPoison$`, before pending-fetch repair | Both PASS, exit 0: test 7.21s / package 31.333s and test 2.43s / package 20.017s. These runs are not presented as post-repair real executions. |
| Separate `^TestDetectorPollCancellation$ -race` | GREEN exit 0, package 1.034s. Deterministic RED against current pre-fix code: exit 1, 0.029s; untouched 0578a61 archive plus only the test addition: same RED exit 1, 0.020s. Caller cancellation, genuine fatal priority and independent transport-error cases are covered. |
| Separate retained-owner race guards | `^TestDetectorRetainedOwnerRebinding$` PASS 1.020s and `^TestDetectorRetainedAssignment$` PASS 1.013s, exit 0, before pending-fetch repair; final full race suite rechecks both. |
| Untouched 0578a61 actual real `^TestDetectorCheckpoint$ -count=5` | All five PASS, exit 0, package 47.556s. This probabilistic baseline did not reproduce the cancellation race; the deterministic driver/caller proof above did. |
| Final `timeout 300s make test` | PASS, exit 0: full backend Go race suite, six web tests and 121 generator tests. Final detector package 2.562s / kafkax 1.106s; other Go packages may use cache. |
| Final `timeout 300s make lint` | PASS, exit 0: Go zero issues, web lint/typecheck, generator ruff/format/mypy. |
| Final backend `timeout 120s go build ./...` and `timeout 120s go vet -tags=integration ./...` | Both PASS, exit 0, before the subsequent integration lint invocation. |
| Final backend `timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | Exit 1 with ONLY existing `test/recovery_test.go:162` unparam warning; retained for Plan 03-14. No owned-file lint warning remains. |

### Exact durable observations and immutable-byte evidence

- Final crash-cut metadata uses format 2, live pin 0, null prefix maximum, drained=false, four rule versions/effective gates and config fingerprint `08f5a743e07cb0cfd6572235a5e6e3bc62cb2c975db454b1014b7d9075bf3776`. Before-publish and after-ack have processed_next=9, sealed_through=1792410871008 and frontier none. Successful checkpoint has processed_next=10, sealed_through=1792410871009 and frontier offset=9 with the exact PG alert ID/fingerprint. Tests log the complete actual broker metadata and compare the full post-commit metadata string after replay, not just these selected fields.
- Final before-publish ID `cfa37937c68de9efdeba98938659a53c2c5800e41a00186c92e5e7772bf37d53`, immutable fingerprint `d03f73a7f42dbf63b49eb4748f9dda06213e49b9677867363aa4d8db924856fc`.
- Final after-ack ID `d46579163871c11a504b511ae19a0bdbbcda53f1b76bae3b71cd20ac1c6462e9`, fingerprint `c821f7a1645e7672cb5ea3c0b68f8644d6854b65ccc59eeeaf69293547107e30`.
- Final successful-checkpoint ID `42cdea1a8592c4ff819eb4a72336d8a71e680b89e10ccf24f1f5657943180f82`, fingerprint `a1381f69bf20e8dd38ada436f7ddebac43e6cc6aebf5ed15cf17d355a16171d3`.
- The crash test logs each complete canonical JSON payload (`cut=... canonical=...`). `requireRecoveredAlert` compares every observed Kafka value byte-for-byte with independent reducer canonical bytes, verifies key `demo|<alert_id>`, then compares the complete immediate CH detail and PG canonical_payload to those same bytes. No reconstructed shortened payload, golden regeneration or hash-only check substitutes for this comparison.
- Final third-repeat example: target partition 10, old member `kgo-3a1665ec-95c5-4b70-9894-e56dce6d7303`, third/new member `kgo-5ec28a88-7366-4832-9526-c764614dc8a0`; checkpoint pin 0 / next 9 restored before event ten, then pin 0 / next 10. ID `7c8263fc4c9076ddef56491c6a9e3513583690cb158acc17e53ff1f03fb155f0`, fingerprint `ab41bac5f59cd5ea9dc9e60458870b518361641974647c603923b6a8e2a3a889`. Broker actual assignment and both process observations establish oldOwner != newOwner.

## Deviations from Plan

### Approved correctness repairs within the eight owned files

1. **[Rule 1 - Bug] Inherited sibling cancellation could escape as a poll transport failure.** Parent stop canceled sibling lifetime and caller contexts; the redundant lifetime-to-poll AfterFunc could cancel the driver's child before the caller cancellation became visible. Pinned franz-go v1.22.1 local source and deterministic driver ordering reproduced it against untouched 0578a61. Removed only that redundant link (`e01326d`); caller polling still has its existing at-most-one-second bound, caller cancellation terminates normally, genuine fatal errors retain priority, and unrelated transport cancellation/deadlines remain errors. No error-ignore, retry or fencing relaxation was added. Parent explicitly approved this narrow deviation before implementation.
2. **[Rule 1 - Bug] New assignment was visible before authoritative offset restoration completed.** Deterministic delayed-fetch RED proved a new nil-checkpoint/history-zero assignment could become ready, restore PG and commit an empty checkpoint before OnOffsetsFetched completed. Added the pending marker and exact completion checks (`cf9367b`) only after parent approval. Existing fatal fetch/history/config paths remain terminal. No fabricated checkpoint or generation fallback was introduced.

Karpathy guidelines kept both repairs surgical and required independent behavioral proof before runtime changes. No new API, schema, dependency or ownership scope was needed.

## Issues Encountered

- An earlier actual third-member run timed out awaiting restored target state after all twelve broker checkpoints reached nine (test 28.36s / package 44.502s, exit 1). That run lacked the subsequently added failure-only process/membership/observation diagnostics. Its exact cause remains unresolved; later passes do not explain it. The pending-fetch safety gap was independently proven, but is **not** claimed as a confirmed retrospective cause. Final count=3 passes and earlier diagnostic count=5 passes are reported separately.
- Fixture setup initially used the wrong batch-size environment key and stopped the initial detector before actual readiness. Corrected the key to `SENTINEL_DETECTOR.WRITER.BATCH_SIZE` and awaited actual /readyz before graceful stop. Those setup failures are not behavioral RED. A zsh unmatched optional fixture glob during closeout was corrected without file changes.
- SIGKILL leaves the broker's configured 45-second member session alive. Bounded checkpoint/publication polls allow 70 seconds within each existing six-minute real-family ceiling; barrier waits themselves remain at most 30 seconds. No sleep is used as causal proof, and no assertion deadline was extended to conceal the unexplained third-member failure.
- Official documentation lookup resolved `/twmb/franz-go`; pinned local v1.22.1 source was also checked for actual callback and poll cancellation semantics. No packages were installed.
- No authentication gate occurred. Test-owned resources use existing fixtures. No user environment, keys, volumes or preserved stack was reset, no OPTIMIZE was used, and no remote write occurred.

## Deferred Issues and Remaining Dependencies

- Existing integration lint `recovery_test.go:162` and shared SCAN expected-summary mismatch remain 03-13/03-14 debt. The expected golden was not regenerated.
- 03-20 still owns the complete durable cooldown PG-commit/outage/stale-owner/restore-behind worker matrix. These source/publication/checkpoint cuts do not substitute for its PG transaction cuts.
- Actual preserved-stack seed/scenario/repeat, phase-wide security review and hosted CI remain pending. Eighteen of twenty-one Phase 3 summaries and 53/56 overall plans exist after closeout (95% rounded). Global requirement and phase completion remain pending; stop before Phase 4.

## Stub and Threat Surface Review

No TODO/FIXME/placeholder or goal-blocking runtime stub was found in the eight owned files. Nil checkpoint after a completed new-group fetch is an intentional valid representation, never a stand-in for pending fetch. No new public endpoint, auth path, schema or unplanned trust boundary was introduced.

T-03-12-01 is covered by production no-op/default-binary proof and private bounded exclusive integration files. T-03-12-02 uses actual boundary/metadata plus full immutable-byte and immediate native-store comparisons. T-03-12-03 preserves current-owner fencing, revoked/lost state invalidation, authoritative successful commits and completed-fetch restoration before processing. No open high-severity finding was identified for this slice; no phase-wide security/compliance verdict is claimed.

## User Setup Required

None.

## Self-Check: PASSED

- All eight owned application/test artifacts exist; all eight listed proof/implementation commits exist in order.
- Compiling behavioral RED precedes each newly implemented contract; passing baseline guards are labeled characterization.
- Every required named recovery family ran independently and passed, including final three-repeat actual target transfer; final full tests, standard lint, build and integration vet passed. The one known integration-lint debt and earlier unexplained real timeout are explicit.
- No tracked deletion, unplanned public surface or goal-blocking stub was found.
- This verified SUMMARY is committed before serialized STATE/ROADMAP mutations; the parent retains the subsequent cross-plan gate.
