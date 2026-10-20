---
phase: 03-detection-alert-pipeline
reviewed: 2026-10-20T15:04:27Z
depth: standard
review_kind: remaining_source_final_delta
diff_base: 7b77246e52412b3ca331f726d1163875daa5a609
review_head: 62099b20c2a98a1966a0d6f90e5b1d653fc1e855
files_reviewed: 73
files_reviewed_list:
  - .env.example
  - .github/workflows/ci.yml
  - Makefile
  - README.md
  - backend/go.mod
  - backend/internal/alertwriter/identity_test.go
  - backend/internal/app/app_test.go
  - backend/internal/config/config_test.go
  - backend/internal/config/detection_pg_test.go
  - backend/internal/config/detection_test.go
  - backend/internal/contracts/scenario_identity_test.go
  - backend/internal/detection/auth_test.go
  - backend/internal/detection/bounds_test.go
  - backend/internal/detection/candidate_test.go
  - backend/internal/detection/checkpoint_test.go
  - backend/internal/detection/config_test.go
  - backend/internal/detection/evidence_test.go
  - backend/internal/detection/loader_test.go
  - backend/internal/detection/replay_test.go
  - backend/internal/detection/scan_test.go
  - backend/internal/detection/transition_test.go
  - backend/internal/detection/volume_test.go
  - backend/internal/detection/window_test.go
  - backend/internal/detector/decision_test.go
  - backend/internal/detector/failpoint_test.go
  - backend/internal/detector/readiness_test.go
  - backend/internal/detector/worker_test.go
  - backend/internal/httpx/httpx_test.go
  - backend/internal/kafkax/alerts_test.go
  - backend/internal/kafkax/detector_consumer_test.go
  - backend/internal/opshttp/metrics_test.go
  - backend/internal/storage/clickhouse/alerts_test.go
  - backend/internal/storage/postgres/detection_decisions_test.go
  - backend/test/alerts_schema_test.go
  - backend/test/alertwriter_conflict_test.go
  - backend/test/alertwriter_dlq_test.go
  - backend/test/alertwriter_partial_test.go
  - backend/test/alertwriter_pipeline_test.go
  - backend/test/detection_decisions_schema_test.go
  - backend/test/detector_capacity_test.go
  - backend/test/detector_checkpoint_test.go
  - backend/test/detector_config_test.go
  - backend/test/detector_cooldown_test.go
  - backend/test/detector_dlq_test.go
  - backend/test/detector_pg_test.go
  - backend/test/detector_pipeline_test.go
  - backend/test/detector_rebalance_test.go
  - backend/test/detector_recovery_test.go
  - backend/test/detector_scan_test.go
  - backend/test/detector_volume_test.go
  - backend/test/lifecycle_test.go
  - backend/test/main_test.go
  - backend/test/readyz_test.go
  - backend/test/recovery_test.go
  - backend/test/scenarios_pipeline_test.go
  - backend/test/scenarios_preserved_test.go
  - backend/tools/check-alerts/main_test.go
  - backend/tools/detection-state/main_test.go
  - docs/architecture.md
  - docs/decisions/0007-event-time-state-pin-recovery.md
  - docs/decisions/0008-alert-list-source-and-conflicts.md
  - docs/demo.md
  - docs/operations.md
  - docs/rules.md
  - docs/threat-model.md
  - generator/tests/test_live_schedule.py
  - generator/tests/test_scenarios.py
  - scripts/check-phase3-tests.sh
  - scripts/check-stack.sh
  - scripts/test_phase3_guards.py
  - scripts/test_seed_scenarios.py
  - scripts/test_stack_config.py
  - testdata/detection/scenarios.json
coverage_methods:
  full_contents: 64
  complete_baseline_diffs_with_context: 8
  exhaustive_structured_fixture_inspection: 1
unreviewed_scope: []
findings:
  critical: 0
  warning: 1
  info: 0
  total: 1
status: issues_found
---

# Phase 3: Remaining Source Final Delta Review

**Reviewed:** 2026-10-20T15:04:27Z
**Depth:** standard
**Files Reviewed:** 73
**Status:** issues_found

## Narrative Findings (AI reviewer)

### Summary

One WARNING affects the causal reliability of negative generator CLI tests. No new runtime BLOCKER was established in this remaining scope. This is not an execution result or a clean verdict for the complete phase.

The scope is exactly `03-FINAL-REVIEW-SCOPE.md`, reviewed at frozen head `62099b20c2a98a1966a0d6f90e5b1d653fc1e855` against baseline `7b77246e52412b3ca331f726d1163875daa5a609`. Source reads used the frozen Git objects rather than the evolving worktree. All 73 scoped artifacts received the coverage specified below. No optional fallow structural pre-pass was supplied.

The original production review, R1, and R2 were read as complementary historical coverage and dispositions. Their 41-file original production review and 16-file whole-reservation repair review are not attributed to this reviewer. The Plan 14 contract, reservation evidence and rollout check supplied intended behavior and proof boundaries. Their recorded passes were not substitutes for reviewing these tests and guards, and historical unexplained failures were not converted into new code findings.

## Warnings

### WR-01: WARNING — Generic CLI failures let schedule rejection tests pass after the intended check is broken

**File:** `/home/logan78/Desktop/Sentinel/generator/tests/test_scenarios.py:297-334`

**Also affected:** `/home/logan78/Desktop/Sentinel/generator/tests/test_live_schedule.py:295-327`

**Issue:** The parameterized raw-schedule refusal test deletes `SENTINEL_API_KEY` and asserts only return code 1 plus absence of an output manifest. The live tampered-schedule test similarly asserts generic failure and no output without establishing a usable credential or installing a fail-on-access key-loader sentinel. Neither test asserts that rejection precedes credential loading or that the intended validation caused the failure.

**Concrete counterexample:** In the `ids` subcase, remove the exact event-ID comparison in `_scheduled_batches` while retaining the other checks. The private schedule still has valid scenario/tenant/seed, caught-up source snapshots, batch count, timestamps and deadline. Execution can pass that broken validation and reach `_load_api_key` at `generator/src/sentinel_generator/cli.py:445`. On a clean CI checkout there is neither the deleted environment credential nor the ignored fallback `.local/keys/demo.key`. The resulting file error is caught by `main`, which returns 1 before writing the manifest: exactly the assertions the negative test accepts. Thus an invalid schedule can pass the rejection boundary while the test stays green.

The v2 test has the same ordering blind spot: bypassing the derived-schedule equality rejection can reach credential loading and fail there before the later wait/send path. The current production validation is present; this finding concerns a false-pass regression guard, not a demonstrated runtime admission vulnerability.

**Complementary coverage checked:** The successful authenticated schedule/retry tests exercise valid schedules. Direct `build_live_schedule` refusal tests validate scheduler inputs, not rejection of externally supplied raw CLI schedules or the requirement to reject before loading a key. The separate historical/unscheduled test asserts a specific diagnostic, but these raw-mutation tests do not. No other frozen generator test installs a key-loader sentinel for these cases.

**Fix:** Patch the CLI module's `_load_api_key` in both negative test families with a sentinel that calls `pytest.fail` if reached, and assert the validation-specific diagnostic for each mutation. This makes accepting the tampered schedule an immediate assertion failure without making a real HTTP request. For example:

```python
from sentinel_generator import cli

def forbidden_key_load(**_kwargs: object) -> str:
    pytest.fail("invalid schedule reached credential loading")

monkeypatch.setattr(cli, "_load_api_key", forbidden_key_load)
```

Add `monkeypatch`/capture fixtures where needed. Retain the nonzero/no-manifest assertions, and verify that disabling the exact-ID check or derived-v2 equality check now produces an assertion failure after successful setup.

**Confidence:** High from the test assertions and the CLI's validation → key loading → delivery → manifest sequence. No mutation, test execution or source edit was performed during review.

## Exact coverage and method

The frontmatter list preserves the exact 73-file scope.

- **64 files:** full frozen contents read through EOF, including test helpers, negative and positive assertions, cleanup and child-process paths, CI jobs, Make targets, scripts, environment template, README and operator/architecture/decision documents. Truncated display batches were followed by the missing portions.
- **8 existing files:** complete baseline-to-frozen diffs read through EOF with relevant caller/helper context: `backend/go.mod`, `backend/internal/app/app_test.go`, `backend/internal/config/config_test.go`, `backend/internal/httpx/httpx_test.go`, `backend/test/lifecycle_test.go`, `backend/test/main_test.go`, `backend/test/readyz_test.go`, and `backend/test/recovery_test.go`. This is standard change review, not a new review of every unchanged line in those existing files.
- **1 large fixture:** `testdata/detection/scenarios.json` contains 45,311 formatted lines. It was parsed completely from the frozen object and inspected through exhaustive normalized summaries rather than a visual read of every repeated JSON line. All nine cases, 2,327 events and 20 expected payloads were included. Inspection covered root/rule/case keys and policy, all event field shapes and value domains, source/entity/type/time/port groups, duplicate identity/content, accepted/dropped IDs, source-key sets and final marker suffixes. A standalone standard-library read of the frozen data independently recomputed length-prefixed SHA-256 alert IDs, aligned window bounds, first-threshold unique evidence/count/sample order and canonical payload bytes; no mismatches were reported. It did not import or run the generator/reducer, regenerate the fixture, or rewrite it. This structural/data inspection is not a fresh project test or live delivery proof.

No scoped file was excluded by the checked Git ignore rules. No root `AGENTS.md`, project `.codex/skills/` / `.agents/skills/`, or `.codexignore` was present. The GSD code-review skill/assigned reviewer contract supplied the review procedure; no additional project skill rules were available.

### Interfaces and causal obligations checked

The independent family registry in `scripts/test_phase3_guards.py` enumerates 46 package/name pairs. The shell entry point gives each pair a separate exact selector through the real nonempty runner; its fake-Go characterization checks empty selection and nonzero exit even when a misleading PASS line exists. Ordinary/integration, cooldown, pure, scheduled and preserved-repeat budgets remain explicit. CI retains the seven Phase 2 guarded groups and full integration invocation, alongside the separate Phase 3 and preserved-stack jobs. Default worker file-list checks do not enable integration hooks globally. These are source observations, not evidence that hosted jobs or all final families have executed successfully.

Review traced test helper calls for worker/reducer thresholds, canonical evidence, checkpoint fields, immutable decisions and ownership changes; real PG/CH/Kafka storage/offset assertions; causal crash markers; actual partition transfer/retained-owner observations; isolated fault-container pause/unpause; child termination/join; UUID-owned topics/databases and preservation of shared stack volumes; manifest capture/ack/verify ordering; purpose-specific child credentials; and original-run timestamp and exact payload assertions. The fixture's static expectations are also consumed by independent Go contract/replay checks and generator builder-equality checks. No passing-test claim was inferred merely from that consumption.

The docs retain at-least-once semantics, permanent reservation authority, unchanged workflow replay, operational-ledger growth, retention/restore failure, stopped-owner configuration handover, private caught-up live scheduling, pending final acceptance, and the Phase 4 sequence-allocation-versus-commit-order limitation. Native ACK loss, controlled deferred visibility, source review, rollout reconciliation and final acceptance remain distinguishable proof classes.

## Limits and handoff

This review made no source edits, ran no project tests, compilation, stack/live operations, staging or commits, and spawned no nested agents. Only this new report was created. Read-only structured inspection of the large JSON fixture is described explicitly above.

Final full matrix/operator executions were pending during this review; hosted CI remains unclaimed. Their later evidence updates do not change this frozen source verdict. The report does not certify deployment, full phase completion, historical failure causes, exact native pre-visibility delay, throughput/RSS, or later-phase APIs/UI/SSE. Existing reservation R2 closure is preserved rather than reopened merely because its model and native proof have different stated limits.

The concrete repair scope for WR-01 is the two generator test files listed in the finding. Root/orchestrator owns triage, any authorized repair and the later aggregate Phase 3 report.

---

_Reviewer: remaining-source review specialist (gsd-code-reviewer)_
_Depth: standard; frozen source review only_

