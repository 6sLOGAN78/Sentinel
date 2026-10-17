---
phase: 03
slug: detection-alert-pipeline
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-10-17
---

# Phase 3 — Validation Strategy

Planning contract only: no new Phase 3 tests have run or passed. Planner must replace the provisional owner IDs below with actual plan/task/wave bindings before plan verification. Missing tests are created in the first test task of their owning slice before its implementation. A failing assertion is RED evidence; a missing package, compile failure or empty selection is not.

## Test Infrastructure

| Property | Value |
|----------|-------|
| Framework | Existing Go testing/testify and locked Python pytest |
| Config | backend/go.mod; generator/pyproject.toml; backend/test integration build tag |
| Quick run | Affected package's one-shot `go test -count=1 -timeout 60s`; generator `uv run --locked pytest -q` |
| Full unit | `make test && make lint` |
| Full integration | `make test-integ` (existing 10-minute test timeout) |
| Estimated runtime | Pure tests target <30s warm; real-container setup/recovery can take minutes, not claimed <30s |

## Sampling Rate

- After every implementation task: affected pure tests, canonical contracts when identity/evidence changes, `git diff --check`.
- After every wave: unit/static gates and the affected nonempty integration family.
- Before phase verification: full units/lint, default and integration vet/lint, complete integrations, all Phase 2 regression groups and new independently nonempty Phase 3 families, preserved-volume Compose scenario acceptance.
- No watch flags; all subprocesses finite. Record actual runtimes and exits rather than inventing feedback timing.
- Container-test runtime is an explicit slow-gate exception; fast reducer/storage-policy tests provide per-task feedback.

## Per-Task Verification Map (provisional ownership)

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| W0-RULES | assign | assign | DET-01..04,07 | rule plan | Strict predicates, stable evidence and identity | unit | `cd backend && go test -count=1 -timeout 60s ./internal/detection -run 'TestRuleThresholds\|TestPredicates\|TestFrozenEvidence'` | No: owning task creates | pending |
| W0-CONFIG | assign | assign | DET-05 | config plan | Bounded YAML, no silent replay reinterpretation | unit | `cd backend && go test -count=1 -timeout 60s ./internal/detection -run 'TestRulesConfig\|TestConfigFingerprint\|TestConfigDrain'` | No: owning task creates | pending |
| W0-WINDOW | assign | assign | DET-06,08 | window plan | Late-drop, bounds, explicit cooldown semantics | unit | `cd backend && go test -count=1 -timeout 60s ./internal/detection -run 'TestWindow\|TestWatermark\|TestLate\|TestStateBounds\|TestCooldown'` | No: owning task creates | pending |
| W0-PIN | assign | assign | ALRT-03,DET-06 | recovery plan | Pin/prefix/sealed invariants, error propagation | unit | `cd backend && go test -count=1 -timeout 60s ./internal/detection ./internal/kafkax -run 'TestCheckpoint\|TestDetectorCommit'` | No: owning task creates | pending |
| W0-GEN | assign | assign | GEN-02,DET-09 | scenario plan | Synthetic only; exact reproducible expectations | Python | `cd generator && uv run --locked pytest tests/test_scenarios.py -q` | No: owning task creates | pending |
| W0-PIPELINE | assign | assign | ALRT-01,DET-01..04 | pipeline plan | Real Kafka key/ack and durable sinks | integration | `scripts/go-test-nonempty.sh './test/...' 'TestDetectionPipeline\|TestAlertWriterDedupe' -tags=integration -timeout 6m -p 1` | No: owning task creates | pending |
| W0-WORKFLOW | assign | assign | ALRT-02 | storage plan | Never reopen resolved row or mutate seq/version | integration | `scripts/go-test-nonempty.sh './test/...' 'TestAlertWorkflowReplay\|TestAlertWriterConflict' -tags=integration -timeout 6m -p 1` | No: owning task creates | pending |
| W0-RECOVERY | assign | assign | ALRT-03 | recovery plan | 9/10 kill and actual third-owner handoff | integration | `scripts/go-test-nonempty.sh './test/...' 'TestDetectorMidWindowRecovery\|TestDetectorThirdReplicaRebalance' -tags=integration -timeout 6m -p 1` | No: owning task creates | pending |
| W0-CRASH | assign | assign | ALRT-04 | failpoint plan | All three proven publish/commit barriers | integration | `scripts/go-test-nonempty.sh './test/...' 'TestDetectorCrashBoundaries' -tags=integration -timeout 6m -p 1` | No: owning task creates | pending |
| W0-RETENTION | assign | assign | ALRT-03,DET-06 | checkpoint plan | Lost history/corrupt metadata fail closed | integration | `scripts/go-test-nonempty.sh './test/...' 'TestDetectorCheckpoint\|TestDetectorRetention' -tags=integration -timeout 6m -p 1` | No: owning task creates | pending |
| W0-POISON | assign | assign | ALRT-01..04 | poison plan | Bounded quarantine; no premature commit | integration | `scripts/go-test-nonempty.sh './test/...' 'TestDetectorPoison\|TestAlertWriterPoison\|TestAlertWriterPartialStorage' -tags=integration -timeout 6m -p 1` | No: owning task creates | pending |

Run the nonempty runner from repository root (it owns backend directory selection). Planner must validate that convention against the script and correct any commands. Every final task needs its own concrete automated command and a named test created by that task or an explicit earlier dependency; provisional ownership is not final sign-off.

## Wave 0 Requirements

- [ ] Write accepted window/state-pin and alert-list-source ADRs before rule code; document all selected research policies, including the cooldown limitation and rule-version drain.
- [ ] Bind every missing Go/Python/integration test to its owning test-first slice and execution order.
- [ ] Add native ClickHouse/PostgreSQL schema behavior tests with repeatable migrations.
- [ ] Use integration-only private file barriers; default binaries have unconditional no-ops.
- [ ] Add bounded late-event metric tests on the internal ops listener; no tenant/entity/IP labels.
- [ ] Add nonempty CI guards for every Phase 3 acceptance family.

No new testing framework or dependency is needed. Existing infrastructure supplies the test harness, not the missing behavior tests.

## Manual-Only Verifications

None planned. All Phase 3 behavior is backend/CLI and can be automated. This does not waive any new blocking requirement discovered during execution.

## Validation Sign-Off

- [ ] All final tasks have automated verification and exact owner/wave bindings.
- [ ] Every missing test has an ordered creation task; no green empty test selections.
- [ ] All 14 phase requirements and all high-severity threat mitigations are bound.
- [ ] Sampling continuity and finite runtimes are checked; slow real-container exceptions explicit.
- [ ] nyquist_compliant true only after the plan checker approves the finalized map.
- [ ] wave_0_complete true only after the actual required scaffolds/tests exist.

Approval: pending plan verification. Execution status: not started.
