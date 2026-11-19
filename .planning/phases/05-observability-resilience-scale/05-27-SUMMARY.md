---
phase: 05-observability-resilience-scale
plan: "27"
subsystem: bench
tags: [bench, recovery, partition-coverage, report-refusal, gap-closure, GAP-1, GAP-2]
requires:
  - phase: 05-observability-resilience-scale
    provides: "05-18 harness (collect/scenarios/report, RAW_FORMAT format_version 1), 05-19 report wiring, 05-20 run 20261119a findings"
provides:
  - "collect.LAG_COVERAGE_QUERY (fixed literal) and lag_samples[].partitions_reported"
  - "restart.partitions_expected measured from kafka-get-offsets; kill gated on full coverage"
  - "report.recovery_from_samples(samples, kill_ns, baseline, partitions_expected, stable_s, timeout_s, *, before_ns=None) for 05-28 live reuse"
  - "refusals recovery_coverage_unknown, recovery_coverage_baseline, api_non_2xx"
  - "summary/markdown: partitions_expected, min_partitions_reported_after_kill, coverage_lost_s"
  - "optional restart.restarted_ns in the schema (05-28 fills it)"
affects: [05-28 harness restart-after-recovery, 05-20 re-run, benchmarks/results]
tech-stack:
  added: []
  patterns: [coverage-gated stable-run search, refusal when coverage data is absent instead of a lag-only fallback for the floored scenario]
key-files:
  created: []
  modified:
    - bench/collect.py
    - bench/report.py
    - bench/scenarios.py
    - bench/RAW_FORMAT.md
    - generator/tests/test_bench_collect.py
    - generator/tests/test_bench_report.py
    - generator/tests/test_bench_scenarios.py
decisions:
  - "LAG_QUERY stays topic-unrestricted: alert-writer consumes security.alerts.v1 and drain() needs its lag; only the coverage query is restricted to security.events.v1"
  - "Coverage is required for sustained-restart (and any raw that records partitions_expected); other scenarios without coverage data keep the lag-only definition and get a Limitations line"
  - "Coverage figures and recovery use only samples before restart.restarted_ns when present"
metrics:
  duration: "~35 min"
  completed: 2026-11-19
  tasks: 2
  files: 7
---

# Phase 5 Plan 27: Coverage-aware bench recovery and API refusal Summary

The bench now counts recovery only when every events partition has a live detector lag reporter. The partition count is measured from Kafka. report.py refuses any sustained-restart raw that lacks coverage data, has incomplete pre-kill coverage, or has any non-2xx API-phase response. The 20261119a raw that once produced the 0.438 s artifact now exits 1.

## Tasks

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | RED: coverage sampling, coverage-aware recovery and API refusal tests | 63241f9 | generator/tests/test_bench_{collect,report,scenarios}.py |
| 2 | GREEN: coverage sampling, recovery, refusals, raw format docs | dfc57ae | bench/{collect,report,scenarios}.py, bench/RAW_FORMAT.md, generator/tests/test_bench_collect.py |

## What changed

- **collect.py**: new literal `LAG_COVERAGE_QUERY = 'count by (group) (count by (group, partition) (sentinel_kafka_consumer_lag{topic="security.events.v1"}))'`. `lag_sample` runs both queries and returns `{t_ns, group, max_lag, partitions_reported}`. A group missing from the coverage result gets 0.
- **scenarios.py**: `_kill_when_steady(..., partitions_expected)` with `partitions_expected = len(offsets_before)` (kafka-get-offsets lines). A new helper `_covered()` requires every detector sample in the steady window to be at full coverage before the kill, within the existing 90% bound. `restart.partitions_expected` is recorded.
- **report.py**:
  - `recovery_from_samples()` is the pure eligibility search. A sample counts only if lag <= baseline and coverage is full. An incomplete sample or a gap breaks the run. The search stops at `before_ns`.
  - `recovery()` adds `partitions_expected`, `coverage_lost_s` and `min_partitions_reported_after_kill`, all computed over `[kill, restarted_ns)`.
  - New refusals: `recovery_coverage_unknown`, `recovery_coverage_baseline` and `api_non_2xx` (sustained-restart only; the message names each endpoint with its count, e.g. "/api/v1/events 150 of 150").
  - When restarted_ns is present, the `recovery_timeout` message says recovery did not happen before the restart.
  - The new rows appear in the markdown, the recovery definition sentence is updated, and the "coverage did not drop" limitation is added.
- **RAW_FORMAT.md**: documents the new optional keys, the 4 equations (3 new plus the updated `recovery_timeout`), the coverage derivation and the updated Pattern 11. format_version stays 1.

## Verification

- RED (63241f9): 15 new tests failed and 98 pre-existing passed. Every failure was for the right reason: `AttributeError` for `LAG_COVERAGE_QUERY`/`recovery_from_samples`, `KeyError` for `coverage_lost_s`/`partitions_expected`/`api_non_2xx`, a lag-only recovery of 0.0 where 55.0 was expected, missing refusal ids, and a kill at +415 s while coverage was still incomplete.
- GREEN: `pytest tests/test_bench_report.py tests/test_bench_collect.py tests/test_bench_scenarios.py tests/test_bench_stack.py` gave 161 passed. All bench tests (`-k bench`): 259 passed. `ruff check . ../bench`, `ruff format --check`, and `mypy src tests` are clean. `python3 bench/report.py --selftest` printed "selftest ok".
- Private raw 20261119a (scratchpad, not committed): `report.py --raw .../raw/raw-sustained-restart.json --out <fresh scratch dir>` exited 1. FAILED.json lists `recovery_coverage_unknown` (partitions_expected None, 525 detector samples without coverage) and `api_non_2xx` ({"/api/v1/events": 150}). `raw-overload.json` still reports (exit 0), because it has no restart and is not sustained-restart.
- `grep partitions_reported` matches in RAW_FORMAT.md (7), report.py (7) and collect.py (1).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] LAG_QUERY not restricted to the events topic**
- **Found during:** Task 2
- **Issue:** The plan says to restrict LAG_QUERY to `topic="security.events.v1"`. alert-writer consumes `security.alerts.v1`, so the restriction would remove alert-writer from lag_samples. `_Harness.drain()` requires all three groups (detector, event-writer, alert-writer) to reach 0, so every run would time out at drain and be refused with `drained`.
- **Fix:** LAG_QUERY is unchanged. Only the coverage query is topic-restricted. The detector consumes only the events topic, so its max_lag is the same either way. This is documented in the RAW_FORMAT Collection table.
- **Files modified:** bench/collect.py, bench/RAW_FORMAT.md
- **Commit:** dfc57ae

**2. [Rule 3 - Blocking] Pre-existing collect test updated in the GREEN commit**
- **Found during:** Task 2
- **Issue:** `test_lag_sampler_polls_every_5s_and_maps_group_labels` asserted `set(seen) == {LAG_QUERY}`. The second query is intended, so this assertion had to change. It should have been changed in RED.
- **Fix:** It now asserts `{LAG_QUERY, LAG_COVERAGE_QUERY}`. This is a one-line change committed with GREEN.
- **Commit:** dfc57ae

**3. [Design choice within plan scope] Non-floored scenarios without coverage data**
- The selftest fixture (`generator/tests/fixtures/bench_raw_reconciled.json`, api-latency, outside files_modified) has a restart but no coverage data, and existing tests expect its recovery of 20.0 s.
- Coverage is required for sustained-restart, and for any raw that records `partitions_expected`. Other scenarios without coverage data keep the lag-only definition, and the report adds a Limitations line: "recovery did not check events partition coverage ...". Only sustained-restart publishes the BENCH-04 recovery claim.

**4. [Added] Schema key `restart.restarted_ns`**
- Added as optional and type-checked, with a check that it is >= kill_ns, so 05-28 only needs to fill it in. The plan's recovery bound depends on it.

## Notes

- Pre-existing `prometheus_snapshot` builds range PromQL with f-strings from computed integer windows (05-18 code, not touched here). Every query added by 05-27 is a module-level literal.
- 05-22 ran concurrently in this checkout (backend Go files, its scratch file `backend/test/zz_scratch_0522_test.go`). Only 05-27 paths were staged, using explicit pathspecs.

## Known Stubs

None.

## Threat Flags

None. No new network surface: the coverage query is one more read-only instant query to the same isolated Prometheus.

## Self-Check: PASSED

- FOUND: bench/collect.py, bench/report.py, bench/scenarios.py, bench/RAW_FORMAT.md, generator/tests/test_bench_{collect,report,scenarios}.py
- FOUND commits: 63241f9, dfc57ae
