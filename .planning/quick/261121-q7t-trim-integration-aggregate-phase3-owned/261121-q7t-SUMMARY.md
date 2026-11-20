---
quick_id: 261121-q7t
description: Trim the integration aggregate by skipping phase3-owned families
date: 2026-11-21
status: complete
decision: "Yes, trim it" (user, verbatim)
commit: 557cd43
---

# Quick Task 261121-q7t: Trim the integration aggregate

## Problem

Hosted run 44792806720 at f8b47ff was green, but the `integration` job took 44m58s of its
45m `timeout-minutes`. Its `make test-integ` step took 32m57s (package 1923.7 s against the
32m go timeout).

## Measurement (hosted, read-only logs of run 44792806720; no CI re-run)

The aggregate runs non-verbose, so per-test hosted times come from the verbose phase3 core
and scenarios jobs and the integration group steps. The same tests run with the same flags
there.

| Owner of the test (besides the aggregate) | Tests | Hosted seconds |
| --- | --- | --- |
| phase3 job (check-phase3-tests.sh, anchored nonempty per name) | 40 | 1597 |
| integration job's own nonempty group steps | 37 | 179 |
| none (aggregate only) + TestMain | 10 | about 148 (remainder) |

Top 15 hosted durations among tests in the old aggregate:

| s | Test | Owner |
| --- | --- | --- |
| 583.5 | TestDetectionScenariosPreservedRepeat | phase3 scenarios + compose-smoke |
| 325.5 | TestDetectionScenarios | phase3 scenarios |
| 208.3 | TestDetectorCooldownCrash | phase3 core |
| 148.7 | TestDetectorCrashBoundaries | phase3 core |
| 94.2 | TestAlertWriterReservationRestart | phase3 core |
| 51.3 | TestDetectorMidWindowRecovery | phase3 core |
| 47.5 | TestWriterCrashRestart | integration step (recovery) |
| 43.9* | TestTopicRetentionOverridesReapply | aggregate only |
| 38.2 | TestReadyzDependencies | integration step (readiness) |
| 22.7 | TestDetectorCooldownOutage | phase3 core |
| 22.3 | TestDetectorConfigRestart | phase3 core |
| 13.3 | TestWriterSlowPoisonRebalance | integration step (writer and DLQ) |
| 12.3 | TestDetectorCooperativeAcquisition | phase3 core |
| 11.4 | TestWriterFailureExitsNonzero | integration step (writer and DLQ) |
| 11.2* | TestWriterInsertFailureNoCommit | aggregate only |

`*` = local measurement, because no hosted job runs this test verbosely. Local and hosted
agree within a few percent on the tests both ran (for example TestWriterCrashRestart 46.7 s
local vs 47.5 s hosted, TestAlertWriterReservationRestart 92.6 s vs 94.2 s).

## Decision from the data

The heaviest families (scenarios, cooldown, crash boundaries, reservation restart) already
run in the `phase3` job, each as its own anchored, nonempty, unskippable selector under a
closed-policy job. Moving them into new phase5 groups would make them run twice, so no new
job or group was added. The aggregate now skips exactly those 40 names, and each still runs
once in CI (PreservedRepeat also keeps its existing compose-smoke run).

The tests that the integration group steps rerun stay in the aggregate. Those steps use broad
regexes that prove only one PASS each, and `make test-integ` should stay as complete as
possible locally. The ten aggregate-only tests also stay, including the 01-SECURITY.md
migration and Redis evidence.

## Change (557cd43)

- `scripts/check-phase5-tests.sh`: new non-runnable `phase3_owned` list (`--phase3-owned`).
  `--skip-regex` = anchored union of chaos, scale and phase3_owned. `phase3` is not a
  runnable group (usage error).
- Makefile: `PHASE5_SKIP` became `INTEG_SKIP`, still failing closed via `$(error)`. The
  `-timeout 32m -p 1` single recipe line is unchanged.
- CI: comments only. Every job, step, budget and matrix is unchanged, so the 8 integration
  group steps, the dev-up prerequisite (pinned by the phase3/phase4 guards), the phase3 and
  phase5 jobs and their budgets are all intact.
- Guards: `test_phase5_guards.py` grew from 15 to 18 tests, with new assertions as follows.
  - The phase3-owned list equals the `./test/...` integration families in
    `test_phase3_guards.EXPECTED`.
  - The phase3 job passes `assert_phase3_workflow` (closed policy: both groups, 45m, no
    skip or mask). A one-group mutation is rejected.
  - Every skipped name has exactly one owner.
  - Skipped and retained sets partition the suite, including the untagged OpenAPI tests.
  - The ten aggregate-only tests and every test matched by an integration group step are
    retained.
  - Lookalikes such as `TestDetectorCooldown`, `TestDetectionScenariosX`,
    `TestWriterCrashRestart` and `TestTopicRetentionOverridesReapply` are not matched.
  - The existing larger-budget and mask rejections are unchanged.
  - `test_phase4_guards.py`: the Makefile pin was updated deliberately.
- README and docs/operations.md describe the owner split and the full-coverage set.

## Predicted hosted times

- Aggregate step: the local package took 248.8 s (wall 251 s), and hosted per-test times
  are about the same. The step should take about 5-6 min including about 50 s of compile;
  with 40% margin, about 8 min against the 32m ceiling.
- Integration job: 2698 s - 1977 s + about 330 s = about 17.5 min (about 20 min with
  margin) against 45m. The ~3.5 min dev-up step stays because the phase3/phase4 guards
  require it.
- No other job's runtime changes. Observation: phase3 core already takes 35m36s of 45m on
  hosted (unchanged here; worth watching).

## Verification

- New aggregate, run locally with the exact rendered skip (testcontainers only, no dev
  stack): exit 0, 64/64 PASS, package 248.8 s.
- `go test -tags=integration -list .` gives 122 = 58 skipped (18 phase5 + 40 phase3, no
  duplicates) + 64 retained. The tests that ran equal the retained set exactly.
- Guards: phase3 12 OK, phase4 26 OK, phase5 18 OK. Five mutations of the runner list and
  ci.yml were each rejected.
- `pytest scripts/` (generator venv, py3.13): 142 passed, 658 subtests. test_seed_scenarios
  14 passed.
- check-pins, check-adrs and check-makefile-dotenv PASS. ci.yml parses. `make -n
  test-integ` renders a single line with the union skip.
- Not run: phase3 runner and scenario tests (they need the protected `sentinel` dev
  stack). Hosted confirmation is pending a push (not pushed).
