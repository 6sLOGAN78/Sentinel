---
phase: 05-observability-resilience-scale
plan: "20"
subsystem: bench
tags: [benchmark, sustained-restart, overload, results, alert-thresholds, adr-0011]
requirements: [BENCH-01, BENCH-02, BENCH-03, BENCH-04, REL-04, OBS-05, UI-05]
status: complete
requires:
  - phase: 05-observability-resilience-scale
    provides: "bench harness/report (05-08, 05-17, 05-18), gap closures 05-22..05-28, operator allowlist (05-21a, ADR 0014)"
provides:
  - "Measured, reconciled results benchmarks/results/2026-11-20-{sustained-restart,overload}.{md,json} (run 20261120a)"
  - "ADR 0011 benchmark method; benchmarks/README.md with linked results; 2026-11-19 results marked superseded"
  - "SentinelPinRetentionLow < 133098 (measured rate x R=1800) and SentinelIngestErrorRate > 1% (policy)"
  - "Bench stack operator tenant = run tenant (BENCH_OPERATOR_TENANTS)"
affects: [phase 5 verification, Phase 6 docs refresh]
tech-stack:
  added: []
  patterns: [threshold = ceil(measured per-partition rate x response policy) checked below measured steady minimum, policy thresholds labelled as policy, docs numbers verbatim from linked report files]
key-files:
  created:
    - benchmarks/results/2026-11-20-sustained-restart.md
    - benchmarks/results/2026-11-20-sustained-restart.json
    - benchmarks/results/2026-11-20-overload.md
    - benchmarks/results/2026-11-20-overload.json
    - docs/decisions/0011-benchmark-method.md
  modified:
    - benchmarks/README.md
    - docs/decisions/README.md
    - docs/decisions/0010-observability-and-tracing.md
    - docs/decisions/0014-operator-tenant-allowlist.md
    - docs/operations.md
    - docs/threat-model.md
    - observability/prometheus/alerts.yml
    - observability/prometheus/alerts_test.yml
    - bench/stack.py
    - deployments/compose/docker-compose.bench.yml
    - generator/tests/test_bench_stack.py
    - scripts/test_observability_config.py
decisions:
  - "SentinelPinRetentionLow threshold 133098 offsets = ceil(73.943224 x 1800); R=1800 s user policy; checked below overload steady min 134793 (largest valid R 1822 s)"
  - "SentinelIngestErrorRate 1% over 5m as a labelled policy value (measured 5xx ratio 0)"
  - "Bench run tenant bench-<run> is the bench stack's only operator tenant; unset fails closed"
  - "2026-11-19 results stay in place unedited and are marked superseded in benchmarks/README.md (different date, so no SUPERSEDE move)"
metrics:
  completed: 2026-11-20
  resume_run_active: "01:53Z-02:55Z (Tasks 1-3 prep), 12:37Z-12:50Z (Tasks 5-6)"
---

# Phase 5 Plan 20: Real benchmark run and live verification Summary

One measured run (20261120a) at git `8424348` reconciled exactly: 1,000,000 events, loss 0, 30/30/30 alerts per rule, events/alerts API 0 non-2xx, and a coverage-gated detector-kill recovery of 53.759 s. Overload stayed bounded, with only 202/429. The run produced ADR 0011, a linked benchmarks README, and two alert thresholds set from measured inputs and user policy. The user approved the live check, and the stack was purged.

The first run (20261119a) is kept at the end as history. Its findings were closed by gap plans 05-22..05-28, and Tasks 1-2 were re-run on RUN=20261120a per the amended resume protocol. No number from the 20261119a reports is reused in docs.

## Resume run 20261120a (2026-11-20 UTC)

### Task 1: host resources (resolved)

- User decision, as relayed by the orchestrator: free space and stop/delete devrag. devrag-stack was deleted entirely. **devrag=removed.** Run id `20261120a`. The Go build cache had already been cleared by the user. No cache was cleared by the agent.
- Read-only checks at 01:53:24Z:
  - `df -h /`: 183G size, 158G used, 16G avail (92%).
  - `MemAvailable: 10679544 kB`.
  - `docker compose ls -a`: no projects.
  - `docker system df`: images 11 (6.09GB), containers 0, local volumes 6 (0B), build cache 33 (2.775GB). Nothing pruned.
- `MIN_FREE_GIB=15 MIN_MEM_GIB=6 bash scripts/check-resources.sh`: exit 0.
  - repo 15.36 GiB, docker-root 15.36 GiB, MemAvailable 10.16 GiB, PASS.
- Plan Task 1 automated verify (df ≥ 15 GiB and MemAvailable ≥ 6 GiB): exit 0.

### Task 2: commands, exits and timings

| Command | Start | End | Exit |
|---|---|---|---|
| `make bench-up RUN=20261120a` (timeout 30m) | 01:53:42Z | 01:55:01Z | 0 |
| `make bench SCENARIO=sustained-restart RUN=20261120a KEEP_UP=1` (timeout 90m) | 01:55:13Z | 02:40:18Z | 0 (report.py wrote results) |
| `make bench SCENARIO=overload RUN=20261120a KEEP_UP=1` (timeout 30m) | 02:40:33Z | 02:41:53Z | 0 (report.py wrote results) |

- Git SHA and image tag: `8424348f217194499ab45bdeb1559b1f51fbb1eb`, images `sentinel-bench-20261120a-<service>`. `BENCH_GIT_DIRTY=1`; the only dirty items are the uncommitted `.planning/config.json` and the untracked `.serena/`.
- bench-up preflight: free 16,492,744,704 B on the repo and the docker root, MemAvailable 10,812,379,136 B. Replicas: ingest 2, detector 3, event-writer 2, alert-writer 1, query-api 2. 6 partitions, tracing sample ratio 0.01.
- After bench-up all 17 containers were healthy, and **every detector had RestartCount 0** (the 20261119a startup crash, Finding 3, did not recur; 05-26).
- No `SUPERSEDE=1`: the run date 2026-11-20 differs from the 2026-11-19 results, which stay in place as history (Task 6 marks them superseded).
- Plan Task 2 automated verify (nested keys): **exit 0**.
- Re-running `report.py` on both raw files into a scratch directory: exit 0 each, and all four files byte-identical to the committed ones.
- Raw files were also copied privately (0600) to the session scratchpad (`raw-20261120a/`). Nothing from them is committed.

### Results (copied from the committed reports)

- [benchmarks/results/2026-11-20-sustained-restart.md](../../../benchmarks/results/2026-11-20-sustained-restart.md) and `.json`
- [benchmarks/results/2026-11-20-overload.md](../../../benchmarks/results/2026-11-20-overload.md) and `.json`

**sustained-restart** (started 2026-11-20T01:55:24.976Z, duration 2,693.102 s)

- Generated, accepted unique, Kafka records, ClickHouse unique and physical: 1,000,000 each. Loss 0, producer duplicates 0, physical duplicates 0, rejected 4xx 0, unaccepted 0, digest match yes, DLQ delta 0, late delta 0.
- Rules: AUTH_BRUTE_FORCE, DENY_SPIKE, PORT_SCAN, REQUEST_FLOOD each 30 expected / 30 PG / 30 CH. Benign hits 0.
- Sustained accepted rate p50 400 / p95 400 ev/s over 249 windows.
- Ingest done-intended per 200-event request (n=5,000): p50 28.781 / p95 35.15 / p99 42.938 ms. Done-sent: 20.879 / 26.549 / 36.804 ms. Schedule slip max 6,069.076 ms, p99 11.748 ms.
- Detection delay histogram (n=125): p50 0.036 s (bucket 0-0.05 s), p95 0.175 s (bucket 0.1-0.2 s), p99 281.6 s (bucket 204.8-409.6 s).
- SSE notification (n=120): p50 596.802 / p95 1,080.951 / p99 1,136.984 ms. SSE: 0 malformed frames, 6 reconnects.
- API phase: /api/v1/alerts n=150, p50 7.937 ms, p95 9.835 ms, **non-2xx 0**. /api/v1/events n=150, p50 43.159 ms, p95 57.291 ms, **non-2xx 0** (GAP-1 closed by 05-22/05-23).
- Recovery (coverage-gated, 05-27): killed sentinel-bench-20261120a-detector-1. Baseline window 60 s, baseline p95 lag 0, peak lag after kill 33, partitions expected 6, min partitions reported after kill 4, **coverage lost 45.107 s**, recovered yes, **recovery time 53.759 s**, timeout 600 s. JSON: `coverage_checked` true, `coverage_dropped` true, `samples_without_coverage` 0, `baseline_incomplete_samples` 0.
- Restart (05-28): the harness restarted the killed detector after live recovery (`restarted_ns` and `rejoined_ns` both set; no rejoin-timeout limitation). Running at start/end: detector 3 / 3, ingest 2 / 2, event-writer 2 / 2, alert-writer 1 / 1, query-api 2 / 2.
- Peak detector state: windows 8.3%, ledger 52.877%, bytes 9.634% (max 52.877%).
- Threshold inputs: ingest requests {'202': 5000}, 5xx ratio 0, whole-run min pin margin 0, **steady-state min pin margin 0**, max per-partition append rate 73.943 ev/s, warm-up max partition share 0.183, warm-up rate 413.333 ev/s.

**overload** (started 2026-11-20T02:40:37.921Z, duration 75.048 s, tenant bench-20261120a-ovl)

- Running at start/end: detector 3 / 3 (all configured detectors; 05-28 `replicas_running`), every other service at its configured count.
- Status counts: 202 × 1,281, 429 × 2,319. No 503, no 5xx. Exercised shed paths: 429 yes, 503 no. The report names TestChaosOverloadBounded and TestChaosKafkaBrokerRestart as the deterministic 503 proofs.
- Producer buffer max/limit 0 / 50000 (ratio 0). OOM-killed none. Restart delta 0 for every service.
- Generated 360,000 at target 6,000 ev/s. Accepted unique 128,100 = Kafka records = ClickHouse unique = physical. Loss 0, duplicates 0, unaccepted 231,900, digest match yes.
- Accepted rate p50 2,000 / p95 2,800 ev/s over 6 windows.
- Ingest done-intended per 100-event request (n=3,600): p50 9.704 / p95 23.748 / p99 514.023 ms. Done-sent: 4.796 / 18.326 / 74.162 ms. Schedule slip max 915.796 ms, p99 401.53 ms.
- Peak detector state: windows 24.85%, ledger 45.413%, bytes 15.004% (max 45.413%).
- Threshold inputs: 5xx ratio 0, whole-run min pin margin 128,573, steady-state min pin margin 134,793, max per-partition append rate 403.496 ev/s.

### Observations from the resume run

1. **sustained-restart `pin_retention_margin_steady_min` is 0, which is not a steady-state value (affects Task 4).** The 05-28 window runs from main-load start to collection. Read-only Prometheus queries on the live bench stack showed `sentinel_detector_pin_retention_margin_offsets` at 0 on all 6 partitions from 01:56:22Z (main-load start) through 01:56:52Z, 7 samples at 5 s. That is the fresh-topic state, which the window was meant to exclude. After that every partition grows from tens of offsets to over 130,000. Task 4's check "candidate below the steady minimum" is therefore degenerate on the sustained report: every candidate ≥ 0 would be flagged. The overload steady minimum (134,793) is a genuine steady value. Possible remedies are a gap fix to the window start or using the overload input; that is a Task 4 decision. No value was computed or written here.
2. **Detection delay p99 is 281.6 s (bucket 204.8-409.6 s), while p95 is 0.175 s.** This is well beyond the 45.107 s coverage loss. The histogram's resolution at that tail is coarse (n=125). It is worth explaining in ADR 0011. Not diagnosed.
3. **Task 3 blocker: the bench stack cannot show System or Overview lag numbers to any bench identity.** `deployments/compose/docker-compose.bench.yml` (query-api) sets `SENTINEL_AUTH.OPERATOR_TENANTS: demo`. `bench/stack.py` `_check_tenant` only mints JWTs for `bench-<run>[-suffix]` tenants, so no bench identity is an operator tenant (ADR 0014). Through the web proxy, an admin token gets 403 "System status is limited to operator administrators." on /api/v1/system. Admin and analyst get `operator_only` for every group on /api/v1/metrics/lag. So Task 3 steps 3-4 (System replica counts, analyst Overview lag numbers) cannot pass as-is. **Resolved (coordinator relayed the user's standing permission; [Rule 3 - Blocking]), commit 535357b:** `bench/stack.py` `create_env` writes `BENCH_OPERATOR_TENANTS=bench-<run>`, and the bench overlay sets query-api `SENTINEL_AUTH.OPERATOR_TENANTS: "${BENCH_OPERATOR_TENANTS:-}"` (unset fails closed). ADR 0014 got a clarifying note, and docs/threat-model.md got one line. Tests: two new bench tests (the env pins the run tenant; the overlay maps it to query-api only) and a drift test that renders the bench stack with and without the key. Bench report/scenarios/collect/stack tests: 195 passed. All `-k bench`: 293 passed. ruff check/format on generator and bench: clean. mypy src tests and the 5 bench modules: clean. report.py selftest: ok. test_observability_config: 32 OK. check-adrs: OK. Applied live: appended `BENCH_OPERATOR_TENANTS=bench-20261120a` to the private env (still 0600), then `docker compose -p sentinel-bench-20261120a ... --env-file .local/bench/20261120a/env up -d --no-deps --no-build --force-recreate --wait query-api` (exit 0). Only the two query-api container IDs changed. Verified with fresh tokens: admin GET /api/v1/system 200 (ingest 2/2, event-writer 2/2, detector 3/3, alert-writer 1/1, query-api 2/2, all ready; query-api restarts_15m 2 is the recreate). Analyst GET /api/v1/metrics/lag 200, every group `available` value 0. Viewer lag 403 "Consumer lag requires the analyst role." Analyst System 403 (role). Admin of the suffixed tenant bench-20261120a-ovl: System 403 "limited to operator administrators", lag `operator_only`.
4. **Task 3 step 2 may lack `store_alert`.** Jaeger holds 103 ingest traces. The richest contain `ingest POST /api/v1/events:batch`, `ingest publish`, `detector detect` and `event-writer store` (2 span links), but alert-writer reports no operations. alert-writer has tracing enabled. `store_alert` is emitted only for alerts whose detect span was sampled, and at ratio 0.01 with 120 alerts about one is expected, so zero is plausible sampling.
5. **Plan interface drift for Task 3.** web/vite.config.ts hardcodes the `/api` proxy to 127.0.0.1:8081 and has no env var. A scratch-only config (session scratchpad `vite.bench.config.mjs`, not committed) proxies to 38090. The Jaeger 2.22 UI is at http://127.0.0.1:36686/ (v3 API).

### Task 3: live Grafana, Jaeger and System check (approved)

- Pre-checkpoint automation: printed loopback URLs and the private Grafana password path, never the password. Minted admin, analyst and viewer JWTs into `.local/bench/20261120a/tokens/*.jwt` (0600). Started Vite with a scratch-only config proxying `/api` to 127.0.0.1:38090.
- Blocker found and fixed before handoff: the bench stack's operator tenant (commit 535357b, Observation 3).
- Task 3 verify (`curl` Prometheus ready + Grafana health): exit 0.
- **User reply, verbatim: "Approved"**

### Task 4: alert threshold decision

- **User reply, verbatim: "R=1800, err=1%"**
- Computed only from committed reports:
  - `SentinelPinRetentionLow` threshold = ceil(73.943224 x 1800) = **133098 offsets**. Inputs: `benchmarks/results/2026-11-20-sustained-restart.json` `threshold_inputs.partition_rate_eps_max` = 73.943224 (normal-load per-partition rate).
  - Checked against `2026-11-20-overload.json` `threshold_inputs.pin_retention_margin_steady_min` = 134793: 133098 < 134793, so it passes. The largest R below 134793 is 1822 s.
  - The sustained steady minimum of 0 is a fresh-topic artifact. The margin is pin minus log start offset, which starts near 0 on a new topic. It is documented: a fresh deployment fires the alert for roughly its first R seconds, until each partition holds R s of data. Suppressing it needs a log-start-offset metric (follow-up).
  - `SentinelIngestErrorRate` = **1% over 5m, labelled policy**. The measured `ingest_5xx_ratio` is 0 in both runs, and overload sheds with 429, so any sustained 5xx indicates a fault.
- Task 4 verify (threshold_inputs present): satisfied (both fields non-null in the sustained JSON).

### Task 5: teardown and purge

- **User reply, verbatim: "Yes, purge"**
- Before teardown, the raws were re-copied to the session scratchpad `raw-20261120a/` (0600, byte-identical to `.local/bench/20261120a/raw-*.json`, not committed). Re-running report.py on them reproduces the committed files byte for byte. The Vite dev server was stopped.
- `make bench-down RUN=20261120a`: 12:37:25Z-12:37:32Z, **exit 0**.
- `make bench-purge RUN=20261120a CONFIRM=1`: 12:37:32Z, **exit 0**.
- After: `docker compose ls -a` was empty, with 0 containers, 0 volumes and 0 images for the project, and `.local/bench/20261120a` removed. The other 12 Docker volumes are identical before and after. No other project existed or was touched. `df -h /`: 15G avail.
- Task 5 verify (no sentinel-bench-* running): satisfied.

### Task 6: thresholds, docs and results links

- `observability/prometheus/alerts.yml`:
  - `SentinelPinRetentionLow` < 133098, with the derivation and input citations in its annotation.
  - `SentinelIngestErrorRate` > 0.01, with the "policy value" annotation.
  - The "example threshold, tune per deployment" line is kept (drift-test contract).
- `alerts_test.yml`:
  - annotations updated
  - error-rate fixtures moved to the 1% boundary (1.96% fires and neither status alone at 0.98% does; 0.66% with 429s does not fire)
  - new pin boundary case (133097 fires, 133098 does not)
- `docs/operations.md`:
  - Alert examples rows give the value, formula and linked inputs, or "Policy".
  - Pin retention hazard has the fresh-deployment note.
  - Benchmark tooling links ADR 0011 and benchmarks/README.md, and no longer says "has not run yet".
- ADR 0010 Consequences: the placeholder bullet now records the chosen values.
- ADR 0011 written (Status/Context/Decision/Consequences) and indexed. It covers:
  - the open-loop method, live event time and pools
  - capacity preflight with the measured skew (0.18258064516129033, f_proc 0.547741935483871, ledger_pred 72302 = 72.3%)
  - equations and refusals, and the md5-xor digest
  - the coverage-gated recovery definition with the 45 s session-timeout component, quoting coverage lost 45.107 s
  - the SSE upper bound and publications vs unique
  - the safety rails
  - the bottleneck section quoting the measured ledger peak 52.877%
  - the 05-22/05-23 query scaling limits
  - the detection-delay p99 explanation
- `benchmarks/README.md`:
  - reproduction steps; ports and floors are referenced in operations.md
  - results tables copied verbatim from the 2026-11-20 reports and linked
  - limitations copied from the reports
  - 2026-11-19 results listed as superseded with the reason, linked, and no numbers quoted
- Jaeger caps unchanged: `max_traces` is already 1000 (ADR 0010), and the plan gives no measured basis for changing it.
- **Detection-delay explanation**, from the coordinator's read-only diagnosis, cross-checked here against the preserved raw:
  - The histogram counts every acknowledged publication, including crash replays: 125 observations for 120 unique alerts.
  - From the raw bucket deltas: 119 observations ≤ 0.2 s, 1 in 25.6-51.2 s, 1 in 51.2-102.4 s, 2 in 102.4-204.8 s and 2 in 204.8-409.6 s.
  - From the raw SSE arrivals: 119 unique alerts within 1.136984 s and PORT_SCAN `1ff8161132` at 38.424 s, the remaining ownership gap.
  - The five slow observations are replays after the kill (kill 02:07:53Z, takeover 02:08:38Z after the 45 s session timeout, restarted detector-1 rejoined 02:09:21Z). They come from unsealed AUTH (300 s) and DENY (120 s) windows. REQUEST_FLOOD, sealed before the kill, was not replayed.
  - The p99 of 281.6 s is interpolated from two AUTH_BRUTE_FORCE replays (about 218 s and 261 s).
  - The explanation is in ADR 0011 Consequences and, using only report-sourced numbers, in benchmarks/README.md. The committed results files were not edited.
- README verbatim check: all 70 numeric tokens appear in the 2026-11-20 results files, except `0011` (ADR id) and `20261119` (run id). Grep spot-check, 7 numbers:
  - sustained-restart.md: 53.759, 45.107, 28.781, 52.877%, 57.291
  - overload.md: 1,281, 134,793

### Tests

- `bash scripts/check-observability.sh` (promtool rule tests + config + dashboard + drift): PASS.
- `python3 -B -m unittest ... test_observability_config.py`: 32 OK. `test_phase4_guards.py`: 26 OK.
- `bash scripts/check-adrs.sh`: OK (including 0011).
- Bench pytest (report/scenarios/collect/stack): 195 passed. `-k bench` earlier: 293 passed. ruff check/format clean. mypy clean. `report.py --selftest` ok.
- `make test` (plan Task 6 verify): exit 0. Go `-race`: 40 packages ok, none failed. Web: 10 files / 217 tests passed. Generator: 414 passed.

### Follow-ups (not done here)

1. report.py: label the detection-delay row "per publication, incl. crash replays" and add a unique-alert first-detection delay. Also consider an SSE max, since the nearest-rank p99 of 120 hid the 38.424 s arrival.
2. Optional: a DetectionDecision "already existed" flag, so replays are distinguishable in metrics.
3. A Kafka log-start-offset metric, so `SentinelPinRetentionLow` (and the 05-28 steady window) can exclude fresh topics.
4. The 05-28 sustained steady pin window still includes the ~30 s of fresh-topic zeros at main-load start.
5. Runbook entry for legacy partitions wedged by the old detector code (carried from 05-26).

## Deviations from Plan

1. **[Rule 3 - Blocking] Bench operator tenant (commit 535357b).** The bench overlay hardcoded `SENTINEL_AUTH.OPERATOR_TENANTS: demo`, which no bench identity can hold, so Task 3 steps 3-4 could not pass. The fix was made at the coordinator's instruction under the user's standing permission:
   - `create_env` writes `BENCH_OPERATOR_TENANTS=bench-<run>`; the overlay interpolates it, and unset fails closed.
   - Tests and an ADR 0014 note were added.
   - Applied live by appending the key to the private env and recreating only query-api.
   - Verified: admin System 200; analyst lag available; viewer 403; the `-ovl` admin still gets 403 / operator_only.
2. **Plan interface drift.** Vite has no API-base env var (the proxy is hardcoded to 8081), so a scratch config was used. Jaeger 2.22 serves its UI at `/` with the v3 API.
3. **Detection-delay numbers placed in ADR 0011, not the README.** The per-alert diagnosis values (218 s, 261 s, 38.424 s, timestamps) are not in a results file. The plan requires every README number to be verbatim from a linked results file, so the README carries the explanation with report numbers only and points to ADR 0011 for the evidence.
4. **Commit type.** The first results commit was amended from `bench(05-20)` to `feat(05-20)` before anything else was built on it (unpushed).

### Commits (resume run)

| Task | Commit | Content |
|---|---|---|
| 2 | 4e3f355 | benchmarks/results/2026-11-20-{sustained-restart,overload}.{md,json} (report.py output, unedited) |
| 3 prep | 535357b | fix: bench run tenant as the bench operator tenant (stack.py, bench overlay, tests, ADR 0014 note, threat model) |
| docs | 0387d0c, 60a2958 | draft SUMMARY/STATE updates |
| 6 | ba882b1 | feat: alert thresholds, promtool fixtures, operations.md, ADR 0010 |
| 6 | ef8e1b6 | docs: ADR 0011, ADR index, benchmarks/README.md |

## Self-Check: PASSED

- FOUND: benchmarks/results/2026-11-20-{sustained-restart,overload}.{md,json}, docs/decisions/0011-benchmark-method.md, benchmarks/README.md
- FOUND commits: 4e3f355, 0387d0c, 535357b, 60a2958, ba882b1, ef8e1b6
- Plan Task 2 verify: exit 0. Task 6 verify (no bench project running, 4 ADR headings, `results/` links, `make test`): exit 0.
- No sentinel-bench-* project exists. Nothing committed contains secrets (token/password files stayed under .local and were purged).

---

# First run 20261119a (history; superseded by the resume run)

## Task 1: host resources (checkpoint resolved)

- User reply, verbatim: "done RUN=20261119a devrag=stopped"
- The orchestrator stopped devrag-stack's 9 containers with `docker stop`, at the user's explicit request. Nothing was removed.
- Gate at the bench floors, as reported by the orchestrator: 15.93 GiB free on / and on the docker root, MemAvailable 9.43 GiB.
- `bash scripts/check-resources.sh` at 10:40:24Z: exit 0.
  - repo 15.93 GiB, docker-root 15.93 GiB, MemAvailable 9.37 GiB, PASS.
  - `df -h /`: 16G avail (91% used). `MemAvailable: 9825588 kB`.
  - `docker compose ls`: no projects running.

## Task 2: commands, exits and timings (2026-11-19 UTC)

| Command | Start | End | Exit |
|---|---|---|---|
| `make bench-up RUN=20261119a` | 10:40:28Z | 10:43:17Z | 0 |
| `make bench SCENARIO=sustained-restart RUN=20261119a KEEP_UP=1` | 10:43:26Z | 11:28:49Z | 0 (report.py wrote results) |
| `make bench SCENARIO=overload RUN=20261119a KEEP_UP=1` | 11:28:58Z | 11:30:16Z | 0 (report.py wrote results) |
| `make bench-down RUN=20261119a` | 11:31:01Z | 11:31:08Z | 0 |
| `make bench-purge RUN=20261119a CONFIRM=1` | 11:31:13Z | 11:31:14Z | 0 |

- Git SHA and image tag: `dec4a7e69fde274319c1be1c2fa9c41441b67273`.
  - Images: `sentinel-bench-20261119a-<service>`.
  - `BENCH_GIT_DIRTY=1`. The only dirty items are the uncommitted `.planning/config.json` and the untracked `.serena/`; no source changes.
- Host: i5-13500H, 16 logical CPUs, 15.31 GiB RAM, kernel 6.8.0-138-generic, Docker 29.9.2, Compose 5.6.1.
- Stack at bench-up:
  - Free space 17,099,591,680 B on the repo and the docker root; MemAvailable 10,065,334,272 B.
  - Replicas: ingest 2, detector 3, event-writer 2, alert-writer 1, query-api 2.
  - 6 partitions, tracing sample ratio 0.01.
- Teardown ran right after the overload run, as the orchestrator instructed: bench-down, then bench-purge with CONFIRM=1. The plan instead keeps the stack up for Task 3, so Task 3 needs a fresh `make bench-up` (see "For Task 3/4").
- After purge:
  - `docker compose ls -a`: empty.
  - 0 containers and 0 volumes for the project.
  - `.local/bench` is empty.
  - `df -h /`: 19G avail.
- Before the purge, the raw files and container logs were copied privately (0600) to the session scratchpad for diagnosis. Nothing from them is committed.

## Results (copied from the committed reports)

Files written by `bench/report.py`, with exit 0 for both:

- [benchmarks/results/2026-11-19-sustained-restart.md](../../../benchmarks/results/2026-11-19-sustained-restart.md) and `.json`
- [benchmarks/results/2026-11-19-overload.md](../../../benchmarks/results/2026-11-19-overload.md) and `.json`

Re-running `report.py` on the preserved raw files into a scratch directory gave byte-identical .md and .json output for both scenarios.

### sustained-restart

- Generated, accepted unique, Kafka records, ClickHouse unique and physical: 1,000,000 each.
- Loss 0, producer duplicates 0, physical duplicates 0, digest match yes, DLQ delta 0, late delta 0.
- Rules: AUTH_BRUTE_FORCE, DENY_SPIKE, PORT_SCAN and REQUEST_FLOOD each 30 expected / 30 PG / 30 CH. Benign hits 0.
- Sustained accepted rate: p50 400 / p95 400 ev/s over 249 windows. Duration 2,708.716 s.
- Ingest done-intended per 200-event request: p50 29.542 / p95 35.254 / p99 39.831 ms. Done-sent: 20.667 / 25.475 / 30.916 ms.
- Detection delay histogram (n=125): p50 1.181 s, p95 35.2 s, p99 179.2 s (bucket 102.4-204.8 s).
- SSE notification (n=120): p50 732.519 / p95 1,085.754 / p99 1,120.485 ms.
- API phase:
  - /api/v1/alerts: n=150, p50 9,647.735 ms, p95 16,004.023 ms, non-2xx 0.
  - /api/v1/events: n=150, p50 13,419.775 ms, p95 19,830.446 ms, non-2xx 150 (all 503; see Findings).
- Recovery:
  - Killed sentinel-bench-20261119a-detector-1. Baseline p95 lag 0, peak lag after kill 0.
  - Recovered yes, recovery time 0.438 s (see Findings: not a valid measure of the rebalance).
- Peak detector state: windows 9.83%, ledger 67.352%, bytes 11.677% (max 67.352%).

### overload

- Status counts: 202 × 1,299 and 429 × 2,301. No 503 and no 5xx.
- Producer buffer max/limit: 0 / 50000. No OOM; restart delta 0 for every service.
- Generated 360,000. Accepted unique 129,900, equal to Kafka records and to ClickHouse unique and physical.
- Loss 0, duplicates 0, unaccepted 230,100 (shed requests, one attempt each).
- Accepted rate: p50 2,000 / p95 2,990 ev/s over 5 windows.
- Ingest done-intended per 100-event request: p50 9.167 / p95 22.715 / p99 24.389 ms.
- Peak detector state: windows 36.96%, ledger 68.034%, bytes 24.869% (max 68.034%), on 2 detectors.

### Alert threshold inputs (for the placeholder replacement)

| Input | sustained-restart | overload |
|---|---|---|
| ingest requests by status | {'202': 5000} | {'202': 1299, '429': 2301} |
| ingest 5xx ratio | 0 | 0 |
| min pin retention margin (offsets) | 0 | 114,209 |
| warm-up max partition share | 0.18 | n/a |
| warm-up rate (events/s) | 410 | n/a |

The thresholds were not changed in Task 2. The plan gives Task 2 only benchmarks/results/. The measured 5xx ratio is 0 in both runs, which gives no basis for a new SLO value. The sustained minimum pin margin of 0 comes from the fresh topic (log start 0 at the first sample), so it is not a steady-state margin.

## Findings (reported, not fixed)

1. **GET /api/v1/events does not scale to 1M events per tenant (product bug).**
   - Symptom: all 150 requests returned 503. Server-side latency was about 5,000 ms (`error_code SERVICE_UNAVAILABLE` in the query-api logs).
   - Cause: `backend/internal/storage/clickhouse/query_events.go` `uniqueEventRowsSQL` runs `GROUP BY tenant_id,event_id` with argMin over the whole row across **every** tenant event before the time-range, filter and LIMIT predicates. Its own comment says "No range/filter/page predicate may precede this dedupe". At 1M events this exceeds `queryTimeout = 5 * time.Second`.
   - Side effect: /api/v1/alerts client latency (p50 9.6 s) is inflated by queueing behind the shared in-flight cap of 8, while the events requests time out.
   - This needs a design change, for example pruning by `occurred_at` before the dedupe, which is safe because the ORDER BY is (tenant_id, occurred_at, event_id) and duplicates share occurred_at. That is not a trivial fix, so it goes to gap planning.
   - report.py does not refuse on API non-2xx, so the report shows the failure but still passes.
2. **The restart-recovery measurement cannot see the rebalance gap (measurement validity).**
   - Detector lag stayed 0 at every 5 s sample from kill-240 s to kill+235 s, yet survivor logs show detector-2 later publishing for partition 5 and detector-3 for partition 2, both previously detector-1's.
   - Cause: `sentinel_kafka_consumer_lag` is self-reported per process, and series for lost partitions are deleted (docs/operations.md "Consumer lag"). While the dead member's partitions wait out the session timeout, nobody reports them, so max-by-group stays 0.
   - Result: the 0.438 s recovery is an artifact. It does not include the roughly 45 s session-timeout component that ADR 0011 is meant to document.
   - The detection-delay p99 of 179.2 s suggests real delay for alerts on the failed-over partitions.
   - Suggested gap: measure lag broker-side (group committed offsets against end offsets), or exclude the measurement until it can see unowned partitions.
3. **Startup crash on one detector.**
   - detector-3 exited once at 10:43:09Z, during bench-up and before the run, with `postgres decision invariant or transaction failed` (`background worker failed`, then `shutdown incomplete`). Docker restarted it (RestartCount 1) and it ran healthy for the whole run.
   - Probably a concurrent-startup race between the 3 replicas on PostgreSQL. Needs investigation.
4. **The killed detector was not restarted.**
   - detector-1 stayed `Exited (137)` because Docker does not apply `unless-stopped` after `docker kill`.
   - The overload scenario therefore ran on 2 detectors. Both reports record this as a limitation.
5. **Overload proved only the 429 path.**
   - The Redis rate limiter shed everything over refill, so the producer buffer peaked at 0 and no 503 (buffer-full or in-flight) occurred.
   - Bounded memory and queues held, but the 503 shed path was not exercised by this run.
6. **The plan's Task 2 automated verify reads top-level keys that report.py nests.**
   - The plan's command reads `d['generated']` and `d['loss']`. report.py nests these under `reconciliation`, and `recovered`/`recovery_seconds` under `recovery`.
   - The plan command fails with KeyError. The equivalent nested check passes every acceptance criterion: generated 1,000,000 ≥ 1M, loss 0, recovered true, numeric recovery_seconds, expected == observed for every rule, overload status keys ⊆ {202, 429, 503}, buffer 0 ≤ 50000.

## Commits so far

| Task | Commit | Content |
|---|---|---|
| 2 | a05f0c0 | benchmarks/results/2026-11-19-{sustained-restart,overload}.{md,json} (report.py output, unedited) |

## Tests run in Task 2

- `uv run --locked pytest tests/test_bench_report.py tests/test_bench_scenarios.py tests/test_bench_collect.py tests/test_bench_stack.py`: 144 passed.
- `report.py --selftest`: ok.
- Report re-derivation from the raw files: identical.
- No alert, doc or Go file was changed, so promtool and the doc-drift tests were not affected.

## For Task 3/4

- Task 3 needs a new bench stack (`make bench-up RUN=<new id>`) and some traffic for Grafana and Jaeger data. The 20261119a stack and its Prometheus/Jaeger data were purged, by the orchestrator's instruction.
- Task 4 must link the committed reports and quote the measured detector state peak (67.352% ledger in sustained-restart). The recovery figure needs the Finding 2 caveat.
- Placeholder thresholds (5%, 100000) are unchanged. Neither Task 2 nor Task 4 lists them; decide in Task 4 or a follow-up, given a measured 5xx ratio of 0.
- Findings 1-3 are candidates for `/gsd:plan-phase 5 --gaps`.
