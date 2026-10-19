---
phase: 03-detection-alert-pipeline
plan: "20"
subsystem: streaming
tags: [go, postgres, kafka, clickhouse, cooldown, recovery, fencing, tdd]
requires:
  - phase: 03-12
    provides: Private causal barriers, actual third-owner transfer and authoritative pending restore gates
  - phase: 03-19
    provides: Immutable allow/suppress ledger, bounded transactions and decision-frontier verification
provides:
  - Four observed decision-commit/publication/checkpoint crash cuts with canonical-byte recovery
  - Actual moved-target concurrent stale-owner rejection without operational row mutation
  - Real PG outage, commit-outcome replay and restore-behind-checkpoint fail-closed acceptance
affects: [03-13, 03-14]
tech-stack:
  added: []
  patterns: [transaction-scoped precommit observation, released postcommit observation, immutable recovery proof]
key-files:
  created:
    - backend/internal/detector/decision_test.go
  modified:
    - backend/test/detector_cooldown_test.go
    - backend/internal/storage/postgres/detection_decisions_test.go
    - backend/internal/storage/postgres/detection_decisions.go
    - backend/internal/detector/worker.go
key-decisions:
  - "03-20: Observe before decision Commit inside the transaction; observe after Commit only after independent rollback cleanup and connection release. Callback failure authorizes no publication and requires immutable replay."
  - "03-20: Preserve existing immutable decision-frontier and actual Kafka ownership fences; prove missing/older/mismatched PG recovery with unchanged source metadata rather than using processed_next as decision evidence."
requirements-completed: [DET-08, ALRT-03, ALRT-04]
duration: approximately 18 min
completed: 2026-10-19
---

# Phase 3 Plan 20: Durable Cooldown Failure Boundaries Summary

Four causal crash cuts now preserve original cooldown decisions and canonical alert bytes, while PostgreSQL outages, stale owners and a lost cooldown seed fail closed without advancing Kafka recovery metadata.

## Performance

- First explicit execution clock: 2026-10-19T12:11:51Z; final task commit: 12:29:01Z. Approximately 18 minutes including context loading and closeout, not a throughput measurement.
- Tasks: 3/3. Application/test files: five, within the seven declared paths. Existing generic integration hooks and the Kafka assignment adapter needed no changes.
- This is the nineteenth implemented Phase 3 plan. The parent owns the Wave 12 gate, preserved-stack scenario acceptance and phase-wide closeout. Global requirement checkboxes and Phase 3 completion remain pending.

## Accomplishments

- The store accepts an optional boundary callback without importing detector. The before-decision-commit callback runs after the complete locked decision operation and before Commit. The after-decision-commit-before-publish callback runs after Commit, independent bounded Rollback finalization and pool connection release. Either callback failing returns no canonical publication authorization; a committed outcome is resolved by subsequent immutable lookup. No PG transaction spans Kafka publication.
- The worker injects the existing reachBoundary function. Default builds retain its unconditional no-op and inspect no fault environment. Integration builds reuse the existing mode-0600 observations inside mode-0700 directories, exact partition/offset matching, once-only controls and cancellable waits of at most 30 seconds, additionally bounded by transaction/batch contexts. No new public endpoint or dependency was added.
- The four-cut real family targets source partition 1: before PG decision commit, after PG commit before publication, after acknowledged Kafka publication before checkpoint, and after the callback-confirmed successful checkpoint. Before commit, a second connection sees no decision and FOR UPDATE NOWAIT fails with 55P03, proving the observed barrier is inside the live transaction. After commit, the immutable row exists before Kafka publication. Every cut is observed before SIGKILL; restart republishes byte-identical canonical Kafka values and preserves immediate CH identity/detail and PG canonical bytes. Published cuts resolve and modify the workflow before the crash; every mutable field, including seq, assignee, version and timestamps, survives replay. The post-checkpoint cut also preserves the complete checkpoint bytes.
- Old allow and old suppress replay after a newer cursor returns their original decisions/canonical bytes without changing that cursor. The retained replay cases also cover equality at eligibility, monotone event-time clocks, entity/rule/tenant isolation, divergent reconstruction, missing older decisions, SQL update immutability, valid PG ahead and PG fence/missing-marker rejection. Suppressions remain operational-ledger-only, absent from Kafka, CH and workflow rows.
- The ownership family seeds nine AUTH contributors on twelve partitions under two actual processes, records their real PG fences, joins a third process, and identifies a target that actually moves. Broker assignment and private revoke/restore/commit observations precede the tenth event. After the new owner creates the target decision, concurrent delayed old-owner, older-generation, equal-generation wrong-member, wrong-token and invalid-incarnation submissions target that same partition. All fail; all decisions, cursors and fences remain byte-identical. A missing earlier source decision also fails without mutation. No fabricated successor generation replaces the actual transfer proof.
- A private real PG container is paused after a successful allow/checkpoint. Readiness reports PG unavailable, the worker exits nonzero within the existing bound, no new alert publishes and the exact broker checkpoint remains unchanged. Unpause/restart recovers the source and preserves suppression. A real committed transaction followed by an injected lost-acknowledgement error returns no successful decision; replay returns its already committed canonical bytes without changing operational rows. This is a deterministic post-commit acknowledgement seam, not a claim that a real TCP Commit response was dropped. The separate existing driver Commit-error test and actual PG row-lock timeout/rollback proof remain passing.
- The restore-behind case allows AUTH at 299s, commits a marker at 330s that seals the first window and advances the pin to 11, then transactionally deletes only its private decision/cursor rows and rewinds its PG fence while retaining Kafka's newer immutable decision_frontier. A candidate at 340s cannot create a fresh allow from the lost seed: restart terminates, readiness is not successful, PG rows and the entire Kafka checkpoint stay unchanged, no alert publishes, and an isolated actual DLQ topic remains at end offset zero. processed_next=11 alone is never treated as proof of the offset-9 decision.
- Pure worker tests forward populated and explicit-none metadata unchanged through the authoritative PG restore gate before restore/readiness/commit. Direct store tests independently reject missing rows, fingerprints/config/source offsets/ordinals and older fences, including an equal-offset older ordinal, while accepting equal or legitimately ahead fences.

## Task Commits

1. Task 1 — `4219745`: compiling test/declaration scaffolds and observed assertion RED. The only store scaffold was the optional callback declaration, without callback execution.
2. Task 2 — `5ffb8ca`: bounded commit observations/error handling, direct frontier guards and actual moved-target ownership proofs. Existing frontier/ownership behavior remained intact.
3. Task 3 — `3294fc3`: runtime hook injection, nonzero-partition crash proof, resolved-workflow/checkpoint preservation and actual no-DLQ restore assertion.

All commits exist in RED-before-GREEN order. No tracked file was deleted.

## Recorded Verification

Each named Go family has its own scripts/go-test-nonempty.sh invocation. Unit selectors use -timeout 60s; final focused units also use -race. Cooldown real selectors use -tags=integration -timeout 8m -p 1; retained ordinary real selectors use -tags=integration -timeout 6m -p 1. Package durations include real dependency startup/cleanup.

| Exact command / stage | Actual exit and evidence |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/storage/postgres/...' '^TestDetectionDecisionBoundaries$' -timeout 60s`, before behavior | RED, exit 1, package 0.007s: neither callback is observed; callback failure incorrectly returns a successful canonical decision. Compilation succeeds. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownCrash$' -tags=integration -timeout 8m -p 1`, before behavior | RED, exit 1, package 172.129s: both missing PG causal barriers fail their exact reached assertions. Existing ack/checkpoint cuts and old allow/suppress replay pass. |
| Independent real `^TestDetectorCooldownOutage$`, before callbacks | Intended ambiguity assertion fails after a real transaction: error expected, got nil. Initial whole-family exits 1 (47.663s and 41.810s) also include the separate fixture issues below; those fixture failures are not behavioral RED. |
| Independent baseline `^TestDetectorCooldownOwners$` and `^TestDetectorCooldownRestoreBehindCheckpoint$` | Existing invariant characterization, exit 0, packages 28.642s and 24.426s. No invented RED or unnecessary fence weakening. Later ownership proof was strengthened to submit stale requests on the same actual moved target. |
| `scripts/go-test-nonempty.sh './internal/detector/...' '^TestDecisionFrontierRestore$' -race -timeout 60s` | Final PASS, exit 0, package 1.027s. Metadata forwarding and PG gate ordering, including populated pin=11/next=11/frontier=9. |
| `scripts/go-test-nonempty.sh './internal/storage/postgres/...' '^TestDetectionDecisionFrontier$' -race -timeout 60s` | Final PASS, exit 0, package 1.025s. None/equal/ahead and all immutable marker/fence mismatch cases. |
| `scripts/go-test-nonempty.sh './internal/storage/postgres/...' '^TestDetectionDecisionBoundaries$' -race -timeout 60s` | Final PASS, exit 0, package 1.026s. Precommit uncommitted/unreleased; postcommit committed/rolled/released; callback failure blocks authorization. |
| `scripts/go-test-nonempty.sh './internal/storage/postgres/...' '^TestDetectionDecisionTransaction$' -race -timeout 60s` | Final PASS, exit 0, package 1.024s. Immutable allow/suppress before mutable cursor; driver Commit uncertainty, independent cleanup and foreign group rejection. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownOutage$' -tags=integration -timeout 8m -p 1` | Final PASS, exit 0, test 23.35s/package 44.656s. Earlier full corrected GREEN also passed, package 42.645s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownOwners$' -tags=integration -timeout 8m -p 1` | Final PASS, exit 0, test 8.72s/package 25.020s. Actual moved partition 10, original and current real owners differ; concurrent stale requests leave the complete PG operational snapshot unchanged. Earlier strengthened run also passed, package 25.337s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownRestoreBehindCheckpoint$' -tags=integration -timeout 8m -p 1` | Final strengthened actual no-DLQ PASS, exit 0, test 6.16s/package 31.248s. Preceding final matrix run also passed, package 24.123s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownCrash$' -tags=integration -timeout 8m -p 1` | Final partition-1/resolved-row/exact-checkpoint PASS, exit 0, test 208.59s/package 226.768s. Four cuts: 50.21s, 49.09s, 49.08s, 49.04s; old allow/suppress replay: 10.29s. Earlier complete GREEN packages 227.812s and 228.536s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCrashBoundaries$' -tags=integration -timeout 6m -p 1` | PASS, exit 0; all three retained exact publication/checkpoint cuts, canonical bytes and resolved workflows. The succeeding separate ThirdReplica command was gated on this command's success. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorThirdReplicaRebalance$' -tags=integration -timeout 6m -p 1` | PASS, exit 0, test 8.80s/package 25.509s; target partition 10 changes actual owner and restores nine before ten. |
| `timeout 300s make test` | PASS, exit 0: complete Go race suite, six web tests, 121 generator tests. Final affected detector 2.359s and PG 1.023s; unchanged packages can use cache. |
| `timeout 300s make lint` | PASS, exit 0: Go zero issues, web lint/typecheck, generator Ruff/format/mypy. |
| `cd backend && timeout 120s go build ./... && timeout 120s go vet -tags=integration ./...` | PASS, exit 0. |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | Final exit 1 with ONLY the existing test/recovery_test.go:162 unparam warning. All owned findings fixed; this is not a clean integration-lint claim. |
| Artifact/commit/deletion checks, `git diff --check`, changed-file stub/threat scan | PASS: all five artifacts and three commits exist; no tracked deletion, whitespace error, unfinished runtime stub or unplanned public surface. |

## Deviations from Plan

1. **[Rule 3 - Preserve already-correct invariants]** Register/Decide, restore-frontier validation and the Kafka adapter already enforce the required generation/member/token/config/source guards, authoritative pending-offset restoration and retained-owner re-registration. The new adversarial cases passed those guards before repairs. Kept them intact and added proof; did not make cosmetic adapter edits or replace membership with a database epoch. The generic integration barrier already accepts the two new names; reused it through the injected callback without editing failpoint_integration.go. Actual changed-file count is five rather than seven.
2. **[Rule 3 - Reuse causal store boundary across Tasks 2/3]** The bounded store callback execution and error propagation are committed with Task 2; Task 3 injects the runtime hook and verifies the complete cross-store crash cuts. This keeps the test declaration, actual store behavior and worker wiring reviewable in successive commits. No architecture or file-ownership expansion occurred.

Karpathy guidance kept implementation tied to the observed callback gap and preserved passing ownership/restore behavior. Pinned pgx v5.11.0 documentation was consulted through Context7 for explicit Commit/Rollback and post-commit cleanup semantics. No package installation occurred.

## Issues Encountered

- The first dedicated-PG outage fixture had no migrations and its alert writer still used the shared PG config. Corrected the private fixture with explicit idempotent migrations and matching writer PG settings before claiming the outage result. Cleanup initially tried to unpause an already restored container; a fixture-local paused flag now makes cleanup once-only. These setup/cleanup failures are separate from the assertion RED.
- Owned cognitive-complexity, shadow and assertion-style findings were corrected with focused helpers and distinct error names. Adding all-zero barrier calls exposed an unparam finding in the existing generic barrier helper; the final real crash fixture exercises actual partition 1 and preserves generic partition support. One gofmt invocation used a root-relative path from backend and failed before edits; the corrected command passed.
- SIGKILL leaves Kafka's existing 45-second session alive; bounded 70-second recovery observations remain inside the declared 8-minute family budget. No arbitrary sleep substitutes for a causal observation.
- No authentication gate, branch/worktree operation, destructive user-data cleanup or remote write occurred. Test-owned resources use existing integration fixtures. User config, .serena, environment, keys and preserved stack volumes were excluded from commits.

## Known Stubs and Threat Review

No goal-blocking stub remains. Optional absent callbacks, empty initial state and explicit frontier.none are intentional representations. Runtime always uses the real PG store.

T-03-20-01: immutable row identity plus at-least fence, actual advanced-pin/lost-seed restore refusal and zero actual DLQ records. T-03-20-02: real moved-target membership and concurrent stale generation/member/token/incarnation/source rejection without row mutation. T-03-20-03: existing private integration-only once/finite hooks, default no-op and released PG connection before publication. No unplanned endpoint, auth path, schema or file-access trust boundary was introduced; no open high finding was identified within this slice. This is not a phase-wide security/compliance verdict.

## Deferred Issues and Remaining Dependencies

- Shared SCAN fixture wording still expects `Distinct destination ports probed` while runtime emits `Distinct destination ports on one host`; no golden was silently rewritten. Reconciliation remains 03-13/03-14.
- Existing integration-lint recovery_test.go:162 remains 03-14 debt. The earlier 03-12 third-target timeout remains unexplained; the new passes do not retroactively establish its cause.
- Actual preserved-stack scenario/repeat acceptance, phase-wide security review and CI remain pending. The parent owns the fresh Wave 12 gate before 03-13. Nineteen of twenty-one Phase 3 plans and 54/56 overall plans are implemented (96% rounded), without a global requirement or phase-completion claim. Stop before Phase 4.

## User Setup Required

None.

## Self-Check: PASSED

- All five claimed application/test artifacts exist; 4219745, 5ffb8ca and 3294fc3 exist in proof-before-implementation order.
- Every required family compiled before behavior; new callback/barrier assertions reached RED, while existing passing invariants are identified as characterization.
- All four independent real cooldown families, the pure frontier family and both retained real families pass. Full tests, standard lint, bounded build and integration vet pass; the one pre-existing integration-lint debt is explicit.
- No tracked file deletion, unfinished runtime stub or unplanned public threat surface was found.
- This verified SUMMARY is committed before serialized STATE/ROADMAP updates.
