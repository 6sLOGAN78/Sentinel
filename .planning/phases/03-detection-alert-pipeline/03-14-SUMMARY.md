---
phase: 03-detection-alert-pipeline
plan: "14"
subsystem: testing
tags: [ci, integration, kafka, clickhouse, postgres, recovery, operations]
requires:
  - phase: 03-13
    provides: Exact current-run and preserved-state operator verification
  - phase: 03-20
    provides: Native cooldown crash, owner, outage and restore proofs
  - phase: 03-21
    provides: Caught-up source snapshots and bounded live scheduling
provides:
  - Forty-six independently nonempty Phase 3 acceptance selectors and retained Phase 2 CI gates
  - Recorded repaired full integration, preserved operator and final static acceptance results
  - Operator documentation for immutable reservations, cooldown, recovery, retention and sequence limitations
affects: [phase-3-verification, hosted-ci, phase-4-backfill]
tech-stack:
  added: []
  patterns: [exact nonempty selectors, isolated native fixtures, private persistent execution logs]
key-files:
  created:
    - scripts/test_phase3_guards.py
    - docs/operations.md
    - docs/demo.md
    - docs/threat-model.md
  modified:
    - scripts/check-phase3-tests.sh
    - .github/workflows/ci.yml
    - .gitleaksignore
    - Makefile
    - backend/test/main_test.go
    - backend/test/lifecycle_test.go
    - backend/test/readyz_test.go
    - backend/test/scenarios_pipeline_test.go
    - README.md
    - docs/architecture.md
    - .planning/phases/03-detection-alert-pipeline/03-ALERT-RESERVATION-EVIDENCE.md
key-decisions:
  - "Keep each exact Phase 3 family independently nonempty; retain the complete integration suite and all seven Phase 2 selectors."
  - "Use a finite 32m aggregate after measured 10m exhaustion; retain original family and 45m CI job budgets."
  - "Isolate static scenarios in their own native PostgreSQL database while preserving strict verification and unrelated workflow rows."
  - "Keep historical failures and interrupted results distinct from subsequent passing evidence."
  - "Root alone may push and monitor hosted CI after local gates pass; global verification remains pending and work stops before Phase 4."
patterns-established:
  - "Long local acceptance logs persist privately under ignored .local/phase3-acceptance with actual returned exits."
  - "A complete earlier exact-family run plus explicit affected-fixture verification is reported as delta verification, never as a second whole matrix."
requirements-completed: [DET-01, DET-02, DET-03, DET-04, DET-05, DET-06, DET-07, DET-08, DET-09, ALRT-01, ALRT-02, ALRT-03, ALRT-04, GEN-02]
duration: approximately 25h elapsed including interruptions and review holds
completed: 2026-10-20
---

# Phase 3 Plan 14: Guarded Acceptance and Operator Handoff Summary

**Forty-six exact Phase 3 families, repaired full integration and seven retained Phase 2 selectors have passing local evidence, with bounded operator contracts and hosted acceptance still pending.**

## Performance

- **Duration:** Approximately 25h elapsed across implementation, reviews, approved reservation repair and runtime interruptions; not continuous execution time.
- **Started:** First recorded plan commit at 2026-10-19T22:35:54Z; an earlier session-start timestamp is unavailable.
- **Completed:** Local closeout on 2026-10-20, after final static execution ended at 23:25:15Z.
- **Tasks:** 3/3 local plan tasks complete.
- **Files:** 29 distinct paths in the task/repair commits listed below, excluding this new summary and root-owned global tracking. The approved supplemental reservation implementation is separately attributed and is not a 22nd numbered plan.

## Accomplishments

- CI retains the complete integration suite and all seven Phase 2 groups while requiring each of 46 exact Phase 3 package/name families independently. Guards reject empty selection, failing commands even with misleading PASS output, missing integration/race flags and changed finite budgets.
- Existing lifecycle/readiness fixtures exercise all five real binaries with private alert topics/groups, absolute rules paths and required migrations. Default worker file lists exclude integration-only hooks.
- Strict static scenario verification now uses a private migrated PG database without deleting unrelated workflow rows. Native regression tests preserve that unrelated row in full; runtime, verifier predicates and canonical fixtures remain unchanged.
- Five operator documents explain actual cooldown, event-time/replay pins, retention/capacity refusal, stopped-owner configuration handover, permanent reservation authority and the Phase 4 sequence-allocation/backfill caveat.

## Task Commits

The original tasks and necessary repairs were committed separately; no completed work was recreated during this continuation.

1. **Task 1 — selector guards and real worker fixtures:** `3fda950` assertion RED; `f1ef3f8` initial GREEN; `6b1ea2a` assertion-preserving integration-lint repair; `6bb5762` fixture-collision RED; `01cf7a6` isolated fixtures and exact PG-outage checkpoint GREEN.
2. **Task 2 — CI wiring and complete regression gates:** `f83f4ba` CI/prerequisite characterization; `522ce1f` retained fixed failure-stage diagnostics; `0d6d82d` seven-reservation-family guard RED; `1a637d4` 46-family registry GREEN; `4bd0943` default alert-writer exclusion RED; `62099b2` final bounded CI/Make wiring; `b336634` native shared-workflow regression RED; `df0bfbb` private-PG GREEN. Root's generator-test-only causality repair is `26f3f8a`; the subsequent CI-interpreter assertion RED is `59b3498`, with implementation/scanner GREEN `defe62e` and evidence `855cb60`.
3. **Task 3 — operator contracts and observed evidence:** `3b0d814` architecture/operations/threat model; `0bad200` scoped-review disposition; `62099b2` README/demo wiring; `b9d5db2` measured gates/causal fixture evidence; `33417c0` interrupted-run/recovery provenance; `914e3d9` final measured local acceptance and hosted boundary.

The supplemental reservation schema/protocol/process/native/model commits and focused results are recorded in [reservation evidence](03-ALERT-RESERVATION-EVIDENCE.md). They are not silently counted as extra numbered-plan tasks. This summary commit precedes all SDK/global tracking updates.

## Recorded Verification

| Gate | Actual result and provenance |
| --- | --- |
| `make test-phase3` / `bash scripts/check-phase3-tests.sh` | Exit 0, 2,545.66s, exactly 46 top-level named PASS results. This complete run predates the later fixture-only PG isolation. |
| Affected `^TestDetectionScenarios$`, after `df0bfbb` | Exit 0, 282.50s; package 279.476s, family 247.18s. Boundary/lateness/all/live-edge children passed. The 45 unchanged families retain their prior independent evidence; no second complete 46-family execution is claimed. |
| Repaired full `make test-integ` | Exact `go test -tags=integration -timeout 32m -p 1 ./test/...`; persistent log starts 2026-10-20T17:16:37Z, package 1,845.917s, elapsed 1,849.21s, `EXIT=0` and `RETURNED_EXIT=0`, ends 17:47:26Z. |
| Documented legacy `timeout 10m make seed` | Exit 0, 43.83s; 1,000 sent/accepted/visible, three API checks, tenant isolation and zero retries. |
| Exact preserved repeat in the 46-family run | Exit 0; family 583.03s/package 599.067s. It actually executes legacy seed and both documented `timeout 16m make seed-scenarios` commands, checking current identities, full prior rows/workflows, drained checkpoints and zero new late drops. No redundant extra pair is inferred. |
| Latest standard preserved startup / stack check | `timeout 10m make dev-up`: 0/48.69s; `timeout 180s scripts/check-stack.sh`: 0/14.32s. Four dependencies, five applications, topic/retention, auto-create, ports, readiness and native checks passed. |
| Final bounded static bundle at `855cb60` | Exit 0, 20.59s, 23:24:55Z–23:25:15Z: `make test` 8.13s (Go race, web 6, generator 121), `make lint` 5.54s, default build 2.62s, default vet 0.21s, integration vet 0.23s, full integration lint 1.31s/zero issues, three-worker default hook exclusion 0.08s, web format 0.34s, web build 2.03s and whitespace. Later edits are documentation only. |
| Script regressions after CI repair | Repair worker recorded all 44 passing in 77.382s using existing generator Python; exact repaired CI scenario command passed 10/10. These results were reviewed by root and were not redundantly rerun by this executor. |
| Pinned full-history secret scanner | Repair worker recorded gitleaks 8.30.1 exit 0/zero findings after the exact historical prose-fingerprint exception and again after implementation commit. Rules/full history remain enabled. See [CI repair evidence](03-CI-REPAIR-EVIDENCE.md). |

All seven retained selectors were compared with committed CI and run serially through the nonempty runner, using `-tags=integration -p 1` and their original budgets:

| Selector | Budget | Elapsed / returned exit |
| --- | --- | --- |
| `TestKeyctl\|TestIngestAuth` | 4m | 32.08s / 0 |
| `TestIngest` | 6m | 41.64s / 0 |
| `TestDedupe\|TestEventLookupTenantScope\|TestEventsSchema\|TestEventsTTLConfigurable` | 4m | 35.67s / 0 |
| `TestWriterCommitAfterInsert\|TestWriterPoisonToDLQ\|TestWriterDLQFailureNoCommit\|TestWriterFailureExitsNonzero\|TestWriterSlowPoisonRebalance` | 5m | 54.63s / 0 |
| `TestWriterCrashRestart\|TestIngestRetryNoDuplicates\|TestIngestKafkaTimeout503` | 6m | 75.49s / 0 |
| `TestRateLimit` | 4m | 20.33s / 0 |
| `TestReadyz\|TestHealthz\|TestBinaryLifecycle` | 5m | 61.88s / 0 |

Aggregate actual exit 0/321.83s, 2026-10-20T23:19:13Z–23:24:35Z. Persistent raw logs are private under ignored `.local/phase3-acceptance/` (directory 0700, logs 0600): `full-integ-restored.log`, `phase2-selectors.log`, each `phase2-*.log`, `final-recovery-dev-up.log`, `final-check-stack.log` and `final-static.log`. Explicit returned exits were persisted immediately. Historical volatile logs that disappeared are not pretended available.

## Files Created/Modified

- `scripts/test_phase3_guards.py`, `scripts/check-phase3-tests.sh`, `.github/workflows/ci.yml`, `Makefile`: exact-family guarding, CI dependencies/build isolation, bounded full suite and preserved scenarios.
- Existing `backend/test/` fixture/recovery/scenario files: real worker setup, isolated configured identities, retained diagnostics, native unrelated-workflow regression and private static PG databases; no weakened verification.
- `generator/tests/test_scenarios.py`, `generator/tests/test_live_schedule.py`: root-owned test-only causality strengthening; no generator-runtime change.
- `.gitleaksignore`: one commit/path/rule/line fingerprint for historical policy prose, with no broad suppression.
- `README.md`, `docs/{architecture,operations,demo,threat-model}.md`: implemented operator contract and truthful local/hosted status.
- `03-ALERT-RESERVATION-EVIDENCE.md`, `03-CI-REPAIR-EVIDENCE.md`: durable results, repair sequence and proof boundaries.

## Decisions Made

Keep ordinary families at 6m, cooldown at 8m, pure families at 60s, scheduled scenarios at 16m, preserved repeat/full aggregate at 32m and relevant hosted jobs at 45m. The 42m26s exact-family result leaves tight fresh-setup CI headroom; no speculative budget increase was made.

Use fixture ownership to remove shared-PG contamination while preserving the strict all-source workflow query. Native ACK loss after visibility, controlled deferred visibility and process loss before CH effects remain distinct proof classes. No local cancellation is a remote-abort guarantee.

The user authorized root to push and monitor hosted CI after local checks pass. This executor performed no remote mutation. Global requirements, roadmap/state advancement, registered-audit summary reconciliation and phase completion remain root-owned. Stop before Phase 4.

## Deviations from Plan

### Auto-fixed issues and approved expansion

1. **[Rule 1 — Bug] Real fixture identity collisions.** Task 1 found fixed cross-fixture cooldown identities; `6bb5762` reproduces the collision and `01cf7a6` isolates affected real fixtures while retaining the exact PG-outage checkpoint assertions. Related integration-lint cleanup `6b1ea2a` preserves the assertion rather than discarding it.
2. **[Rule 3 — Blocking] Aggregate timeout measured too short.** Task 2's original 10m aggregate exhausted its budget in 620.17s before scenarios. The plan expressly allowed a measured adjustment; `62099b2` records a finite 32m aggregate without relaxing individual-family limits. Later full pass took 1,849.21s.
3. **[Rule 1 — Bug] Strict static scenario hit shared workflow state.** The later full run failed at `workflow_payload`. A native ConcurrentReplay→static-all command reproduced it (exit 1/84.37s); permanent regression `b336634` failed at the intended assertion (1/112.80s). `df0bfbb` adds one quoted UUID test database, normal migrations and consistent worker/key/verifier environment. Focused GREEN 0/51.89s, original combined command GREEN 0/43.02s, complete scenario GREEN and full aggregate GREEN followed. The complete unrelated workflow row remains preserved.
4. **[Rule 2 — Missing critical proof] Causality in generator refusal tests.** Root's `26f3f8a` makes two previously false-pass counterexamples fail at the credential boundary. Normal tests/static checks pass; independent `03-REVIEW-FINAL-DELTA-R1.md` is clean. No runtime changed.
5. **[Rule 1 — Bug] Hosted CI used the wrong Python.** `59b3498` assertion RED, `defe62e` GREEN use the already locked generator interpreter after `uv sync --locked`; no ambient dependency installation. The same repair adds only the demonstrated historical scanner prose fingerprint. Independent `03-REVIEW-CI-REPAIR.md` is clean with zero findings.
6. **[Rule 3 — Blocking] Interrupted runtime lost volatile evidence.** Session46545 remains UNKNOWN/INTERRUPTED. Preserved standard startup, read-only reconciliation and the replacement persistent full gate recover the execution path without data reset. New actual exits survive under ignored private workspace logs.

Separately, source review found a reservation-protocol architectural gap. The user explicitly approved its design and implementation plan; the supplemental repair was executed and independently reviewed, rather than being treated as an automatic Rule 1–3 architectural change. Whole-fix R2 closes CR-02-R1 for conforming writers. See the approved design, supplemental plan, [rollout check](03-RESERVATION-ROLLOUT-CHECK.md) and [reservation evidence](03-ALERT-RESERVATION-EVIDENCE.md).

## Issues Encountered and Evidence Limits

- Historical pre-reservation full run on `01cf7a6` returned 2/974.03s (package 972.784s), with static-all and preserved-repeat failures whose diagnostics were discarded. A later static-only pass does not retroactively explain them.
- The later full run returned 2/1,824.17s (package 1,822.485s), below its 32m limit, with static-all `workflow_payload` failure. The native contamination counterexample and regression establish the subsequent fixture repair; they are not proof of older discarded-diagnostic failures' causes.
- The post-fix full attempt beginning 16:45:49Z in session46545 has no surviving result/log after runtime restart. Its result remains UNKNOWN, separate from the replacement full PASS at 17:16:37Z–17:47:26Z.
- An initial regression setup hit `COORDINATOR_NOT_AVAILABLE`; that setup failure is not counted as behavioral RED. The later native regression reached the intended strict assertion before GREEN.
- An earlier 03-12 moved-target recovery timeout remains unexplained despite subsequent actual passing repetitions. Reservation Task 3 also lacks a separate tests-only RED commit; history was not reconstructed.
- Native ACK-loss observes physical A before its response is lost. Deferred pre-visibility completion is a controlled schedule/model. Exact stock-server pre-visibility pause remains unproved. The private-PG fixture cleanup is bounded, but unchanged process cleanup requests Kill without an observed join; independent fixture R2 records that limitation without establishing a concrete current bug.
- CI-repair evidence preserves the initial restrictive-umask script failures and an invalid older-host interpreter reproduction. The corrected all-script run uses runner-normal umask 022 while its redirected log remains private; no test assertion was changed to obtain the pass.
- The older hosted run `39457945227` at `19db519` failed. Local repairs, scanner success and clean review do not turn it into hosted acceptance; a new repaired-source hosted result remains pending.

## Known Stubs

`Makefile:90` retains the pre-existing `bench` target, which reports that benchmarking belongs to Phase 5 and exits nonzero. It is intentional later scope and does not block this plan's acceptance/operations goal. No new UI/mock-data or runtime placeholder was introduced by this plan. Phase 4 login/query/triage/UI/SSE and lossless late-lower-sequence backfill remain explicit future work.

## Threat Surface and Review Reconciliation

No additional production endpoint, auth path, runtime file-access boundary or schema was introduced by this final closeout. The approved reservation surface is already mapped to existing threats in `03-SECURITY.md`; the isolated fixture's quoted owned database and preserved workflow map to existing scenario ownership/acceptance obligations. The final CI-only repair changes no runtime boundary and its historical scanner exception is exact, independently reviewed and supported by zero-finding full-history scans. No unregistered threat flag was found.

The canonical source-review aggregate is clean for its stated scope, fixture R2 is clean, and the independent CI-repair review is clean. The registered static audit records 78 closed threats (57 mitigations, 21 original accepted risks), zero open threats and no certification. Root must reconcile this eventual summary with that audit and perform final goal verification. Static review is not substituted for execution, nor execution for hosted acceptance.

## Authentication Gates

None in the remaining local execution. Existing private credentials and pinned dependencies were reused; no secret is included in this summary.

## User Setup Required

None beyond the existing private local configuration. User configuration and `.serena/` were preserved unstaged. No data/volume reset, history rewrite, remote push or Phase 4 implementation was performed by this executor.

## Next Phase Readiness

Local Plan 03-14 execution is complete. This is the 21st numbered Phase 3 summary, not approval to begin Phase 4. Root next reviews this committed summary, reconciles source/security and roadmap evidence, performs serialized tracking and the already-authorized push/hosted-CI workflow. Global completion remains pending until those obligations are satisfied. Requirement IDs above describe this plan's local coverage; this executor has not modified global requirement checkboxes.

## Self-Check: PASSED

All four declared created artifacts and this summary exist. All 22 listed task/repair/evidence commits resolve to actual commit objects. Persistent full integration, seven individual retained selectors, selector aggregate and final static logs each contain actual `RETURNED_EXIT=0`. Owned whitespace checks pass. No task/global tracking was advanced before committing this summary.
