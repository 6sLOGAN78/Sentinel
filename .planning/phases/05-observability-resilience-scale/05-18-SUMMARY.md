---
phase: 05-observability-resilience-scale
plan: "18"
subsystem: bench
tags: [benchmark, orchestration, collection, overload, detector-kill, makefile]
requirements: [BENCH-01, BENCH-02, BENCH-04, REL-04]
requires:
  - phase: 05-03
    provides: report.py and the format_version 1 raw contract
  - phase: 05-04
    provides: BenchConfig, AttackPlan, Schedule, capacity_preflight, benign_threshold_check
  - phase: 05-08
    provides: run_open_loop, subscribe_alerts, rawlog sections
  - phase: 05-17
    provides: isolated sentinel-bench-<run> stack (stack.py, run.py up/down/purge)
provides:
  - bench/collect.py (read-only, tenant-parameterized collectors and samplers)
  - bench/scenarios.py (sustained_restart, overload)
  - bench/run.py run --scenario ... and make bench SCENARIO=... RUN=... [KEEP_UP=1]
  - report.py overload equations, drained and status_counts checks, alert threshold inputs
affects: [05-20 (first real run), 05-13 placeholder thresholds (inputs now recorded)]
tech-stack:
  added: []
  patterns:
    - "CH --param_tenant/{tenant:String} and psql -c '\\bind <tenant>' -c '... $1': SQL constants are pure literals"
    - "injectable offload (asyncio.to_thread in production, inline in tests) keeps blocking docker calls off the event loop and the virtual clock deterministic"
    - "Prometheus counters read as raw range samples with exact reset correction, never increase() extrapolation"
key-files:
  created:
    - bench/collect.py
    - bench/scenarios.py
    - generator/tests/test_bench_collect.py
    - generator/tests/test_bench_scenarios.py
  modified:
    - bench/report.py
    - bench/run.py
    - bench/stack.py
    - bench/RAW_FORMAT.md
    - Makefile
    - generator/src/sentinel_generator/bench/rawlog.py
    - generator/tests/test_bench_report.py
    - generator/tests/test_bench_stack.py
decisions:
  - "psql never interpolates :'var' inside -c (verified on postgres:18.6); the tenant is bound with \\bind + $1 instead of the plan's -v/:'tenant' form"
  - "The ClickHouse digest runs over SELECT DISTINCT event_id (as RAW_FORMAT requires); the plan's flat groupBitXor would cancel physical duplicates in pairs"
  - "The warm-up is the first 60 s of the main schedule on the run tenant: the Kafka key is tenant|source_ip, so a separate warm-up tenant would measure a different partition skew"
  - "Prometheus group labels sentinel-<name> are recorded as <name>, because report.py reads group == detector"
  - "Overload sends one attempt per request (exact status counts), offers 3 x refill events/s with sender concurrency 256 > 2 x 64 ingest slots, uses source pool 1000 and no benign auth.failure, and is bounded by capacity + refill x 60 s accepted events over N-1 detectors instead of the steady-state capacity model"
  - "rawlog.client_section now emits status_counts so the client section keeps exactly the contract keys"
metrics:
  duration: "2026-11-17T21:31Z to 21:58Z, then resumed 2026-11-18 to 16:40Z (two usage-limit/session restarts in between)"
  completed: 2026-11-18
  tasks: 3
  files: 12
---

# Phase 5 Plan 18: Bench run orchestration, collection and scenarios Summary

`make bench SCENARIO=<sustained-restart|overload> RUN=<id>` now runs a scenario on the isolated `sentinel-bench-<id>` stack. It reuses a stack started by `make bench-up`, or starts one. Collection is read-only and parameterized, the raw JSON is written 0600 under `.local/bench/<id>/`, and the run hands that file to `report.py`, which writes `benchmarks/results` or refuses. The stack is stopped in a `finally` unless `KEEP_UP=1`. Everything was tested offline with fakes and a virtual clock. No stack was started and no benchmark was run (that is 05-20).

## Commits

| Task | Commit | Content |
|------|--------|---------|
| 1 RED | 9232d23 | tests only + RAW_FORMAT.md: 20 collector tests, 15 scenario/CLI/make tests, 9 report test functions (overload, drained, status counts, optional keys, threshold inputs), the 05-17 Makefile assertion updated |
| 2 GREEN | b8dfcd4 | bench/collect.py, bench/scenarios.py, stack.py dry-run read-only classification |
| 3 GREEN | 063907d | report.py equations/sections, run.py `run`, Makefile `bench`, rawlog `status_counts` |

RED evidence (21:36:59Z to 21:37:40Z): 20 failed and 32 errors. The 32 errors were `FileNotFoundError` for bench/collect.py (20) and bench/scenarios.py (12). The 20 failures were the missing report equations (KeyError `overload`/`threshold_inputs`, exit 0 instead of 1), `run.py: invalid choice: 'run'`, and the old `bench is available from Phase 5` recipe. The plan's RED command `! bash -c '... pytest ...'` exited 0. One test bug, a missing tmp subdirectory, was fixed before the RED commit.

## Tool behaviour verified on real containers (throwaway, `--network none`, `--rm`)

These ran after `bash scripts/check-resources.sh` passed at 21:3xZ (19.02 GiB free, MemAvailable 5.75 GiB). The containers were `sentinel-bench-toolcheck-pg` and `sentinel-bench-toolcheck-ch`, both removed, and no compose project was created.

| Check | Result |
|-------|--------|
| `psql -v tenant=bench-x -c "SELECT :'tenant'"` (postgres:18.6) | `ERROR: syntax error at or near ":"`. The plan's form does not work |
| `psql -c '\bind bench-x' -c 'SELECT $1::text'` | `bench-x`. A quote-injection attempt failed safely |
| `clickhouse-client --query "SELECT currentUser()"` with only the container env | `sentinel` (the password never goes on argv) |
| digest of the 3 fixed IDs on ClickHouse 26.3.47.6 | 11115952024367422501 = `9a43cb78a8b45c25`, equal to `rawlog.id_digest`. This settles the 05-03 byte-order item |

## Phase deadlines and windows (bench/scenarios.py)

| Constant | Value |
|----------|-------|
| start lead / warm-up | 5 s / 60 s (first 60 s of the main schedule) |
| kill gate | ≥ 30% of batches due and handed out, and detector lag stable for ≥ 60 s; skipped (restart = null → report refuses) once 90% are sent |
| stable band | max−min detector lag ≤ max(1000, 5 s × rate) over [now−65 s, now], samples spanning ≥ 60 s, no gap > 15 s |
| recovery (raw `restart`) | baseline 60 s, stable 30 s, timeout 600 s (report.py Pattern 11) |
| sender deadline | remaining schedule + 120 s |
| drain | every group's newest lag sample = 0, polled every 5 s, ≤ 900 s, otherwise `run.drained=false` → `drained` refusal |
| SSE settle | 120 s after drain; subscriber runs for the whole run with analyst JWTs (re-minted by subscribe_alerts) |
| API phase | 60 s at 5 req/s, in-flight cap 8, `GET /api/v1/events?limit=50` and `/api/v1/alerts?limit=50`, analyst JWT, round robin over 38090/38091 |
| overload | 60 s, 3 × refill events/s, batch 100, sender in-flight 256, max_attempts 1, tenant `bench-<id>-ovl` with its own key |

Sustained defaults: 1,000,000 events at 400 ev/s, batch 200 (5,000 batches, 2,500 s), sender in-flight 64, retry budget 15 s, 30 attacks per rule (120 expected alerts), and the 05-04 pools. The seed is sha256(run id), recorded in `preflight.seed`.

## Collector queries (all read-only, 30 s timeout, via `stack.compose_argv ... exec -T`)

| Source | Query |
|--------|-------|
| CH events | `--param_tenant=<t>`: `SELECT uniqExact(event_id), count(), (SELECT groupBitXor(reinterpretAsUInt64(substring(MD5(toString(event_id)), 1, 8))) FROM (SELECT DISTINCT event_id FROM sentinel.events WHERE tenant_id = {tenant:String})) FROM sentinel.events WHERE tenant_id = {tenant:String}` |
| CH alerts | `SELECT rule_id, uniqExact(alert_id) FROM sentinel.alerts WHERE tenant_id = {tenant:String} GROUP BY rule_id` |
| CH self-check | `--param_ids=[3 literals]`: digest over `SELECT DISTINCT arrayJoin({ids:Array(String)})`, compared with Python before any count |
| PG alerts | `psql ... -v ON_ERROR_STOP=1 -c '\bind <t>' -c 'SELECT rule_id, count(*) FROM alert_workflows WHERE tenant_id = $1 GROUP BY rule_id ORDER BY rule_id'` |
| PG benign hits | same binding, `SELECT rule_id, entity_key ... LIMIT 10000` (refuses at the limit); source IP before `|` not in the attack plan → benign |
| Kafka | `/opt/kafka/bin/kafka-get-offsets.sh --bootstrap-server kafka:9092 --topic security.events.v1 --time -1`, before, after warm-up, after drain |
| Prometheus | `max by (group) (sentinel_kafka_consumer_lag)` every 5 s. Range samples `[run+15s]` of the delay buckets, DLQ, late events and `sentinel_ingest_requests_total` by status, with exact reset correction (a series first seen after start counts from 0). `max(max_over_time(...))` of state gauges and producer buffered; `max(*_limit)`; `min(min_over_time(pin_margin))`. A missing limit is refused, never invented |
| docker | `docker ps --filter label=com.docker.compose.project=sentinel-bench-<id>` (name must also start with the project), `docker stats --no-stream` every 10 s, `docker inspect` RestartCount/OOMKilled before and after |

None of the SQL constants is an f-string, so nothing is interpolated. FINAL and OPTIMIZE appear nowhere, and a test asserts their absence in every captured query.

## Kill safety (T-05-18-02)

1. `docker ps` with both the project label and `com.docker.compose.service=detector`.
2. `docker inspect <id>`: refuse unless the labels are exactly `sentinel-bench-<id>` / `detector` (tested with project `sentinel`; no kill is issued).
3. Only then `docker kill --signal KILL <id>`.

`docker kill` appears in scenarios.py only after that call. Dry-run treats `docker kill` as mutating; compose exec reads and `docker ps/inspect/stats --no-stream` are read-only.

## Report additions (format_version stays 1)

These are optional keys, type-checked when present and not listed as unknown: `run.drained`, `run.warmup`, `client.status_counts`, `prometheus.producer_buffered_max/limit`, `prometheus.ingest_requests_by_status`, `prometheus.pin_retention_margin_min`, `resources.oom_killed`, `resources.restart_delta`, and `preflight`.

New equations:

| Equation | Applies to | Refuses when |
|----------|-----------|--------------|
| `drained` | every scenario | `run.drained` is false |
| `status_counts` | every scenario | `client.status_counts` differs from the recomputed request statuses |
| `overload_recorded` | overload | an overload key (or `restart_delta.ingest`) is missing |
| `overload_status` | overload | any status is outside {202, 429, 503} (status 0 = no response refuses) |
| `overload_buffer` | overload | buffered max > limit |
| `overload_oom` | overload | any OOM |
| `overload_ingest_restart` | overload | ingest restart delta > 0 |

The summary JSON and markdown gain `overload` and `threshold_inputs`, and the raw `preflight` is included.

## 05-13 placeholder thresholds: data now recorded for 05-20

- **Ingest 5xx ratio (placeholder 5%):** the server-side `ingest_requests_by_status` increase over each run, and the report derives `ingest_5xx_ratio`. The sustained run gives the normal-load ratio and the overload run gives the shedding ratio.
- **Pin margin (placeholder 100000):** `pin_retention_margin_min` over the run, plus the warm-up rate and partition share. Together with events retention these give the margin in time.
- **Jaeger:** RSS comes from the docker stats samples (container `...-jaeger-1`).

## Gate table (2026-11-18 unless noted)

| Gate | Window (UTC) | Result |
|------|--------------|--------|
| `bash scripts/check-resources.sh` before the tool-check containers (2026-11-17) | 21:3xZ | PASS (19.02 GiB, MemAvailable 5.75 GiB) |
| RED `pytest test_bench_collect/scenarios/report` (2026-11-17) | 21:36:59–21:37:40Z | 20 failed, 32 errors, causal; plan RED command exit 0 |
| Plan Task 3 verify chain: full generator pytest, `report.py --selftest`, `ruff check . ../bench`, `ruff format --check`, `mypy src tests`, `make -n bench ... \| grep "run.py run --scenario"`, `! make bench RUN=` | 16:29:05–16:30:46Z | exit 0 (363 passed) |
| `cd generator && uv run --locked pytest tests/test_bench_*.py` | 16:30:58–16:32:08Z | 242 passed |
| `uv run python ../bench/report.py --selftest` / `python3` 3.10.12 | 16:32Z | ok / ok |
| `mypy --strict` on bench/stack, run, collect, scenarios, report | 16:2xZ | no issues |
| `BENCH_DRY_RUN=1 run.py run --scenario overload --run dryrun18` (read-only probes) | 16:32Z | exit 1: `repo has 9.72 GiB free; free at least 15 GiB` (real gate refused; nothing written) |
| `... run --run sentinel` | 16:32Z | exit 1, refusal before any docker call |
| `make test` (after 05-15 GREEN 3a4e94f landed) | 16:32:19–16:33:33Z | exit 0 (Go 40 packages ok, web 174, generator 363) |
| `make lint` | 16:33:39–16:33:46Z | exit 0 |
| `bash scripts/check-pins.sh` | 16:33Z | exit 0 |
| `python3 -m unittest discover -s scripts` (host 3.10) | 16:33:46–16:36:52Z | 116 tests, errors=1: the only error is `test_seed_scenarios` (`ImportError: cannot import name 'UTC' from 'datetime'`, Python 3.10). This was already known before this plan and is not caused by it |
| leftovers | 16:32Z | no sentinel-bench containers, no `.local/bench` dirs |

Acceptance greps:
- `def test_` count is 20 + 15 = 35 (≥ 14).
- FINAL/OPTIMIZE in test_bench_collect.py appears only in the forbidden tuple.
- `param_tenant`/`{tenant:String}` match in collect.py, and the string-built tenant SQL grep is empty.
- `com.docker.compose.project=sentinel-bench-` matches in scenarios.py.
- `bench is available from Phase 5` is gone from the Makefile.
- `stack.down` is in the run path.

## Deviations from Plan

**1. [Rule 1 - Bug] psql `-v` + `-c` does no interpolation.** Verified (syntax error). The tenant is bound with `-c '\bind <t>' -c '... $1'`. Files: collect.py, tests. Commits: 9232d23, b8dfcd4.

**2. [Rule 1 - Bug] Digest without DISTINCT.** The plan's flat `groupBitXor` over all rows XORs physical duplicates out. A DISTINCT subquery is used, as RAW_FORMAT specifies.

**3. [Rule 1 - Bug] Group label mismatch.** Prometheus reports `sentinel-detector`, and report.py reads `detector`. Without the mapping, every recovery would refuse with `recovery_baseline`. collect.py strips the `sentinel-` prefix.

**4. [Rule 2] Counter resets.** The killed detector's counters reset. Exact reset-corrected increases from raw range samples replace the before/after pairs, with before = 0, as RAW_FORMAT allows. This avoids `increase()` extrapolation and non-integer counts.

**5. [Rule 3] Warm-up on the main tenant.** See decisions. The re-check refusal cancels the sender, writes `refused-sustained-restart.json` (0600) and raises `ScenarioRefused` with the numbers.

**6. [Rule 3] `rawlog.client_section` emits `status_counts`.** Without it, 05-08's `test_client_section_has_exactly_the_raw_format_keys` failed after the contract gained the key. Commit 063907d.

**7. [Rule 2] Extra guards.**
- Overload accepted-event bound over N−1 detectors.
- Kill skipped after 90% sent.
- Teardown also after a failed `up` (only when the private env exists), and a teardown error never masks the run's exit code.
- The PG entity read refuses at its row limit.
- `ENTITY_LIMIT` is derived from the SQL literal.

**8. [Scope] Files outside `files_modified`:**
- bench/stack.py: dry-run read-only list, requested by 05-17.
- generator/tests/test_bench_stack.py: Makefile assertion, requested by 05-17.
- generator/src/.../rawlog.py: deviation 6.

## Known limits / risks for 05-20

- **Overload `ch_unique == accepted` is strict.** A 503 from a produce timeout, as opposed to a gate or ErrBufferFull shed, can still land in Kafka (at-least-once, ADR 0013). That would refuse `ch_unique`/`id_digest`. Gate and buffer-full sheds never reach Kafka, so this needs a slow broker. If it happens, it is a real finding to decide on, not something to relax silently.
- **In-flight 503s may not occur.** Fast 429 responses keep concurrency low. The status counts show what happened, and the report does not require 503s.
- **Detector restart after `docker kill --signal KILL` is not guaranteed.** Docker may or may not apply `restart: unless-stopped`. The raw `restart_delta.detector` records it, and a limitation line is added if the container did not come back. Recovery is measured on the survivors either way (capacity is sized for N−1).
- **Unobservable counter increments.** Increments on the killed replica between its last 5 s scrape and the SIGKILL cannot be observed; this is recorded as a limitation.
- **`rss_bytes` is docker stats MemUsage** (cgroup usage without page cache); this is recorded as a limitation.

## Deferred Issues

- Pre-existing, not fixed: `scripts/test_seed_scenarios.py` fails on host Python 3.10 (`from datetime import UTC`).

## Notes for 05-20

**Command** (after the user frees disk; the agent never prunes):

```sh
bash scripts/check-resources.sh
make bench-up RUN=<id>                                    # build + start (first build up to 30 min)
make bench SCENARIO=sustained-restart RUN=<id> KEEP_UP=1  # keep up for the Grafana/System check
make bench SCENARIO=overload RUN=<id> KEEP_UP=1           # reuses the running stack, own tenant
make bench-down RUN=<id>
make bench-purge RUN=<id> CONFIRM=1
```

Without `KEEP_UP=1`, each `make bench` stops the stack at the end. Results go to `benchmarks/results/<date>-<scenario>.{md,json}`, or `.FAILED.json` with exit 1. Raw files go to `.local/bench/<id>/raw-<scenario>.json`.

**Resources:**
- Free space of at least 15 GiB on both the repo and the Docker root, and MemAvailable of at least 6 GiB (bench preflight floors).
- At 16:32Z the repo had 9.72 GiB free, so the preflight refuses today.
- Container limits add up to 8.1 GiB (caps, not usage).
- The harness holds about 100 MB for 1M accepted IDs.
- The Aegis `compose` project (14 containers) was running and will be recorded in `host_load_note`.

**Expected duration:**
- Sustained: about 42 min of load (1M / 400 ev/s = 2,500 s), plus preflight (benign check over 1M events, about 30 s), drain (normally short, at most 15 min), 2 min SSE settle, 1 min API phase and collection. That is about 47 min typical and at most about 62 min.
- Overload: about 1 min of load plus drain and collection, about 3 to 5 min.
- Add `bench-up` if the stack is not already running.

## Threat Flags

None beyond the register:
- T-05-18-01: parameters only; SQL constants are literals.
- T-05-18-02: label filter + inspect check.
- T-05-18-03: `provenance()` requires the real runner, sender and subscriber.
- T-05-18-04: constant deadlines.
- T-05-18-05: the key is read from its 0600 file into memory and the JWT is in a header only; a test asserts neither appears in the raw.
- T-05-18-06: SELECT-only; no FINAL/OPTIMIZE.

## Known Stubs

None.

## Self-Check: PASSED

- Commits 9232d23, b8dfcd4 and 063907d are all present.
- bench/collect.py, bench/scenarios.py, test_bench_collect.py and test_bench_scenarios.py all exist.
- No sentinel-bench containers are left and there is no `.local/bench`.
