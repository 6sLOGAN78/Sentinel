---
phase: 05-observability-resilience-scale
plan: "28"
subsystem: bench
tags: [bench, restart, replicas, shed-paths, threshold-inputs, supersede, gap-closure, GAP-4]
requires:
  - phase: 05-observability-resilience-scale
    provides: "05-27 report.recovery_from_samples, optional restart.restarted_ns, coverage figures over [kill, restarted_ns)"
provides:
  - "scenarios._await_live_recovery / _restart_killed_detector: docker start of the killed detector after live recovery or the recovery timeout, label-verified, bounded 180 s rejoin wait"
  - "raw run.replicas_running{start,end}, kafka.main_load, prometheus.pin_retention_margin_steady_min, restart.rejoined_ns"
  - "report: replicas_degraded refusal, Running at start/end rows, Exercised shed paths table + chaos test sentence, steady threshold rows, --supersede"
  - "report.recovery_baseline shared by recovery() and the live restart decision"
  - "collect: running_replicas, container_ready, pin_margin_steady_min (PIN_MARGIN_STEADY_QUERY), main_load_offsets"
  - "run.py --supersede passthrough; make bench SUPERSEDE=1"
affects: [05-20 re-run and threshold tuning, benchmarks/results]
tech-stack:
  added: []
  patterns: [live and offline recovery share one pure search and baseline, concurrent restarter task bounded before drain, byte-for-byte supersede with run-id guard]
key-files:
  created: []
  modified:
    - bench/scenarios.py
    - bench/collect.py
    - bench/report.py
    - bench/run.py
    - bench/RAW_FORMAT.md
    - Makefile
    - generator/tests/test_bench_scenarios.py
    - generator/tests/test_bench_report.py
    - generator/tests/test_bench_collect.py
decisions:
  - "The restart flow runs as a spawned task after the kill, so the main-load end-offset snapshot is taken when the sender finishes; the task is awaited (bounded) before drain"
  - "restarted_ns is forced strictly after the newest lag sample that confirmed recovery, so report.recovery(before_ns=restarted_ns) sees the same confirming sample"
  - "Rejoin = docker inspect Running (and Health healthy when a healthcheck exists) AND the newest detector sample at/after restarted_ns reports every events partition"
  - "replicas_running is optional (older raws); when present, start.detector < config.replicas.detector refuses every scenario"
  - "No 'restart' entry was added to the phase list (it runs concurrently with main load); progress is emitted via ctx.emit"
metrics:
  duration: "~50 min"
  completed: 2026-11-19
  tasks: 3
  files: 9
---

# Phase 5 Plan 28: Detector restart, replica snapshots, shed paths, steady thresholds and supersede Summary

After the kill, the bench now watches live for recovery with `report.recovery_from_samples` (05-27). Once recovery is seen, or the 600 s timeout passes, it label-checks the killed container and runs `docker start` on that container id only. It then waits up to 180 s for the container to run with full partition coverage, so later scenarios run on all configured detectors.

The raw file now records running replicas per service at the start and end of each scenario. report.py refuses any scenario that starts short of detectors. The overload report says which shed paths the run actually exercised. When 503 was not exercised, it names the chaos tests that prove the 503 paths. A steady-state pin margin and a measured per-partition append rate are added as threshold inputs. `SUPERSEDE=1` moves a same-date result from a different run, unedited, into `benchmarks/results/superseded/`.

## Tasks

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | RED: restart, replicas, shed paths, thresholds, supersede tests | c31c1c3 | generator/tests/test_bench_{scenarios,report,collect}.py |
| 2 | GREEN: harness restart, replica snapshots, threshold windows | 88dcf82 | bench/{scenarios,collect}.py, bench/RAW_FORMAT.md, bench/report.py (recovery_baseline only) |
| 3 | GREEN: report facts, supersede, run.py/Makefile | 3204b30 | bench/{report,run}.py, Makefile, generator/tests/test_bench_report.py |

## What changed

- **scenarios.py**
  - `_kill_detector` records the killed container id.
  - After the kill, a spawned `_recover_then_restart` task polls every `LAG_INTERVAL_S`. Each poll computes `report.recovery_baseline` and `report.recovery_from_samples` over the samples collected so far, using the restart object's windows, until recovery is reported or `timeout_s` passes.
  - Next, `verify_bench_container(id, "detector")` runs, then `docker start <id>`. `restarted_ns` is strictly after the confirming sample.
  - The rejoin loop polls `collect.container_ready` and coverage until `rejoined_ns` is set, or `RESTART_REJOIN_TIMEOUT_S` = 180 s passes. On timeout, `rejoined_ns` stays null and a limitation is added.
  - The old "not restarted by its restart policy" limitation is gone.
  - Both scenarios record `run.replicas_running` (start before load, end at collection) and `kafka.main_load`:
    - sustained-restart: from the main-load start to the sender finishing
    - overload: the load phase
  - `pin_retention_margin_steady_min` covers different windows per scenario:
    - sustained-restart: from the main-load start to collection
    - overload: the scenario window after its first 10 s
- **collect.py**: added `running_replicas`, `container_ready` (shares the label check with `verify_bench_container` via `_verified`), `PIN_MARGIN_STEADY_QUERY` (literal template; only the integer window is substituted), `pin_margin_steady_min` and `main_load_offsets`.
- **report.py**:
  - New schema keys, all optional and type-checked: `run.replicas_running`, `kafka.main_load` (requires `end_ns > start_ns`), `prometheus.pin_retention_margin_steady_min`, `restart.rejoined_ns` (must be >= `restarted_ns`).
  - `replicas_degraded` refusal and `Running at start/end: <service>` rows.
  - `overload.shed_paths{429,503}`, an "Exercised shed paths" table and `CHAOS_503_NOTE` when 503 = 0.
  - Threshold rows: `steady-state min pin retention margin` and `max per-partition append rate, main load`. The old row is relabelled "min pin retention margin, whole run incl. fresh topic (offsets)".
  - `--supersede`.
- **run.py / Makefile**: `run --supersede` and `_report_args()`. `make bench ... SUPERSEDE=1` passes `--supersede`, and the help text mentions it.
- **RAW_FORMAT.md**: documents every new key, the `replicas_degraded` equation, the shed-path and partition-rate derivations, the restart flow and the `--supersede` semantics. format_version stays 1.

## Verification

- RED (c31c1c3): 30 new tests failed and 115 passed, including every pre-existing test. Each failure had the expected cause:
  - `AttributeError` for the missing collect functions
  - `KeyError` for `restarted_ns`, `replicas_running`, `pin_retention_margin_steady_min` and `shed_paths`
  - argparse `SystemExit: 2` for `--supersede`
  - no `docker start` call
  - the label-check refusal not raised
  - the make dry-run without `--supersede`
- GREEN:
  - The `report`/`scenarios`/`collect`/`stack` bench tests: 193 passed.
  - All bench tests (`-k bench`): 291 passed.
  - `ruff check . ../bench` and `ruff format --check . ../bench` are clean.
  - `mypy src tests` is clean (mypy on the 4 bench modules is also clean).
  - `python3 bench/report.py --selftest` printed "selftest ok".
  - `make -n bench SCENARIO=overload RUN=x SUPERSEDE=1` contains `--supersede`.
  - `git diff --stat HEAD~3 -- benchmarks/results` is empty. The committed 2026-11-19 results were not moved or touched.
- Ordering evidence (fake runner): `docker inspect <killed id>` runs after `docker kill` and before `docker start <killed id>`. `docker start` runs at a `restarted_ns` later than recovered_at plus the stable window. Every compose call carries `-p sentinel-bench-<run>`. kill and start only target project detector ids.
- No live docker stack was used. All tests are unit/fixture tests.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `report.recovery_baseline` committed in Task 2**
- **Found during:** Task 2
- **Issue:** The live restart decision needs exactly the baseline that report.py computes. Duplicating the computation in scenarios.py could make live and offline recovery disagree.
- **Fix:** Extracted `recovery_baseline()` into report.py. `recovery()` now uses it. It is committed with Task 2 because scenarios.py depends on it. Behaviour is unchanged, and all pre-existing report tests pass.
- **Commit:** 88dcf82

**2. [Rule 1 - Bug] RED test wrote into a missing tmp dir**
- **Found during:** Task 3
- **Issue:** `test_degraded_detector_replicas_at_start_refuse_both_scenarios` called `_failure_ids(tmp_path / "b")` without creating the directory.
- **Fix:** Added a one-line `mkdir`, committed with GREEN.
- **Commit:** 3204b30

**3. [Test fake realism] docker kill no longer increments RestartCount in the scenario fake**
- The fake used to model a policy restart on kill. It now leaves the container exited and absent from `docker ps`, which matches how `docker kill` bypasses `unless-stopped`. `docker start` brings it back without a RestartCount increment.
- The pre-existing assertion `restart_delta.detector == 1` became `== 0` in the RED commit. It passes on the old code too.

## Notes

- Other executors (05-24, 05-23) committed in this checkout concurrently: c-hashes 7622ff7 and cb5bc90 sit between this plan's commits. Only 05-28 paths were staged, using explicit pathspecs. `backend/test/events_scale_test.go` (theirs), `.planning/config.json` and `.serena/` were left alone.
- 05-20 uses `SUPERSEDE=1` when it re-runs on the same date. Its threshold tuning should read `pin_retention_margin_steady_min` and `partition_rate_eps_max`, not the whole-run minimum.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model. T-05-28-01: the label check runs before `docker start`, which targets the recorded killed id only. T-05-28-02: shed paths are derived from measured counts. T-05-28-03: supersede moves files byte-for-byte, refuses the same run id and validates the old run id against `RUN_ID_RE` before using it in a path. T-05-28-04: the rejoin wait is bounded by `RESTART_REJOIN_TIMEOUT_S`, and the restarter task by `_bounded`.

## Self-Check: PASSED

- FOUND: bench/scenarios.py, bench/collect.py, bench/report.py, bench/run.py, bench/RAW_FORMAT.md, Makefile, generator/tests/test_bench_{scenarios,report,collect}.py
- FOUND commits: c31c1c3, 88dcf82, 3204b30
