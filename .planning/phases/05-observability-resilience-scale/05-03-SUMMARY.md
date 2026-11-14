---
phase: 05-observability-resilience-scale
plan: "03"
subsystem: bench
tags: [benchmark, reconciliation, report, python, stdlib]
requires: []
provides:
  - "bench/report.py: stdlib-only reconciliation and Markdown/JSON report generator (--selftest)"
  - "bench/RAW_FORMAT.md: raw-run contract format_version 1"
affects: [05-08, 05-17, 05-18, 05-20]
tech-stack:
  added: []
  patterns:
    - "refuse-on-mismatch: any failed equation means exit 1, a FAILED.json only and no .md"
    - "schema-driven raw validation with display-safe ReportError"
    - "test loads bench/report.py by path through importlib spec_from_file_location and a typed Protocol"
key-files:
  created:
    - bench/report.py
    - bench/RAW_FORMAT.md
    - generator/tests/test_bench_report.py
    - generator/tests/fixtures/bench_raw_reconciled.json
    - generator/tests/fixtures/bench_raw_mismatch.json
  modified: []
decisions:
  - "format_version mismatch is reported as a FAILED.json equation (not a load error), so the refusal names the equation"
  - "postgres.benign_hits added to the raw contract: the harness counts alerts whose entity is outside the attack ranges"
  - "schedule_sum (generated == sum of request event_count) and ch_physical >= ch_unique added as extra equations"
  - "Recovery streak is broken by a sample gap greater than half the stable window"
  - "report.py runs on system python3 3.10 as well as the 3.13 uv venv (no datetime.UTC, manual RFC 3339 parse)"
metrics:
  duration: "~40 min"
  completed: 2026-11-14
  tasks: 2
  files: 5
---

# Phase 5 Plan 03: Refuse-on-mismatch benchmark report Summary

`bench/report.py` is a stdlib-only script. It reads one raw-run JSON file (format_version 1), checks 17 reconciliation equations and recomputes every number from the raw data: nearest-rank percentiles, Prometheus-style histogram quantiles with the bucket bounds stated, 10 s sustained-rate windows and Pattern 11 recovery. It writes `<date>-<scenario>.md` and `.json` only when every equation holds. Otherwise it writes `<date>-<scenario>.FAILED.json` and exits 1.

## Raw contract

`bench/RAW_FORMAT.md` defines format_version 1 with every key the plan lists. Contract details:

- Exit codes: 0 written, 1 refused, 2 provenance refusal, 3 the target file already exists.
- The date in output file names comes from `run.started_at` (UTC), not the wall clock.
- Prometheus counter pairs must be reset-corrected by the harness. A negative delta is a refusal.
- The digest is `groupBitXor(reinterpretAsUInt64(substring(MD5(toString(event_id)), 1, 8)))` over `SELECT DISTINCT event_id`. The client side computes the same value with `report.id_digest`: first 8 md5 bytes read little-endian, XOR over distinct IDs. 05-18 must still confirm the byte order against a real ClickHouse.
- Unknown extra keys are accepted and listed under Limitations.

## Equations (failure ids)

| id | Check |
|----|-------|
| `format_version` | must be 1 |
| `generated_floor` | `sustained-restart` requires at least 1,000,000 events |
| `generated_accounting` | generated = accepted + rejected + unaccepted |
| `schedule_sum` | generated = sum of `event_count` |
| `kafka_records` | Kafka records ≥ accepted |
| `ch_unique` | ClickHouse unique = accepted |
| `ch_physical` | ClickHouse physical ≥ ClickHouse unique |
| `id_digest` | ClickHouse digest = client digest |
| `rule_pg:<RULE>` | Postgres count = expected |
| `rule_ch:<RULE>` | ClickHouse count = Postgres count |
| `benign_hits` | benign hits = 0 |
| `dlq_delta` | DLQ delta = expected |
| `late_delta` | late-event delta = expected |
| `detection_histogram` | no counter reset, cumulative |
| `restart_missing` | `sustained-restart` must have a restart |
| `recovery_baseline` | a pre-kill baseline exists |
| `recovery_timeout` | recovered within the timeout |

## Tasks

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Raw contract and RED report tests | 3dd7da8 | bench/RAW_FORMAT.md, generator/tests/test_bench_report.py, generator/tests/fixtures/bench_raw_reconciled.json, generator/tests/fixtures/bench_raw_mismatch.json |
| 2 | GREEN stdlib-only bench/report.py | d456f4d | bench/report.py |

## Gate evidence (all run for real on 2026-11-14)

| Command (cwd) | Result |
|---------------|--------|
| RED: `uv run --locked pytest tests/test_bench_report.py -q` (generator) | 43 failed, exit 1. All 43 failures were `FileNotFoundError` for bench/report.py, so the RED was causal. The RED commit contains no report.py. |
| GREEN plan verify (generator), 00:35:14Z–00:35:15Z: pytest test_bench_report.py, `python ../bench/report.py --selftest`, ruff check, ruff format --check, mypy (strict) on the test and report.py | 43 passed, selftest ok, all clean, chain exit 0 |
| `uv run --locked pytest -q` (generator, the Makefile `test` generator step) | 164 passed, exit 0 |
| `ruff check . && ruff format --check . && mypy src tests` (generator, the Makefile `lint` generator step) | exit 0 |
| `python3 bench/report.py --raw generator/tests/fixtures/bench_raw_reconciled.json --out benchmarks/results` (repo root, system Python 3.10) | exit 2, benchmarks/results not created |
| `python3 bench/report.py --selftest` (system Python 3.10) | exit 0 |
| stdlib-only grep (requests/numpy/pandas) | no matches |
| `ls benchmarks` after the tests | only README.md (no results directory, no fixture output) |

Test count: 26 `def test_` functions, 43 collected cases. These include 13 named mismatch mutations from bench_raw_mismatch.json and 6 malformed-input cases.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical] benign_hits had no raw source**
- **Found during:** Task 1
- **Issue:** The plan requires a `benign_hits > 0` refusal, but the listed `postgres` contract only had `alerts_by_rule`.
- **Fix:** Added `postgres.benign_hits` to RAW_FORMAT.md (alerts whose entity is outside the attack plan's entity ranges).
- **Commit:** 3dd7da8

**2. [Rule 2 - Missing critical] Extra integrity checks**
- **Found during:** Task 1
- **Fix:** Added three checks:
  - `schedule_sum`: generated must match the recorded requests.
  - `ch_physical`: physical rows must be ≥ unique rows.
  - `detection_histogram`: refuses on a counter reset instead of reporting a wrong quantile.
- **Commit:** 3dd7da8, d456f4d

**3. [Rule 1 - Bug avoided] Python 3.10 compatibility**
- **Found during:** Task 2
- **Issue:** The acceptance command runs plain `python3`, which is 3.10 on this host. `datetime.UTC` and `fromisoformat("...Z")` would crash there.
- **Fix:** RFC 3339 timestamps are parsed manually, with no tz-aware APIs.
- **Commit:** d456f4d

**4. Output key rename for the secret scan**
- **Found during:** Task 2
- **Issue:** The secret test forbids the substring `sk_`, and the raw key `disk_free_bytes_start` contains it.
- **Fix:** The report JSON exposes it as `hardware.free_space_bytes_start`. The raw contract is unchanged.
- **Commit:** d456f4d

**5. Provenance refusal writes no files**
- Task 1's behaviour list implied a FAILED.json for every refusal, but Task 2 says the provenance refusal exits 2 with no files. Task 2 was followed, and the test asserts that benchmarks/results is unchanged.

## Limitations / Not run

- Only the generator parts of the full `make test && make lint` were run. The backend Go and web parts were skipped because plan 05-02 was editing backend/ in the same tree at the same time, and this plan touches no Go or web code.
- The fixture numbers are synthetic test data (provenance "fixture"). No measured benchmark exists yet; that is 05-20.
- The ClickHouse digest byte order is documented but not yet checked against a live ClickHouse (05-18).

## Known Stubs

None.

## Threat Flags

None. The threat model mitigations are implemented: the provenance gate (T-05-03-01), schema validation with ReportError (T-05-03-02), secret refusal with a test (T-05-03-03) and exit 3 instead of overwriting (T-05-03-04).

## Self-Check: PASSED

- FOUND: bench/report.py, bench/RAW_FORMAT.md, generator/tests/test_bench_report.py, both fixtures
- FOUND commits: 3dd7da8, d456f4d
