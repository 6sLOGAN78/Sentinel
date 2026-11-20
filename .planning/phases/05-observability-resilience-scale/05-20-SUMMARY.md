---
phase: 05-observability-resilience-scale
plan: "20"
subsystem: bench
tags: [benchmark, sustained-restart, overload, results]
requirements: [BENCH-01, BENCH-02, BENCH-03, BENCH-04, REL-04, OBS-05, UI-05]
status: "DRAFT - resume run 20261120a: Tasks 1-2 done (both reports exit 0, committed 4e3f355); stopped at Task 3 (checkpoint:human-verify) with the bench stack up; Tasks 4-6 not started"
---

# Phase 5 Plan 20: Real benchmark run and live verification Summary (DRAFT)

> Draft. The first run (20261119a) is kept below as history. Its findings were closed by gap
> plans 05-22..05-28, and Tasks 1-2 were re-run on RUN=20261120a per the amended resume
> protocol. Tasks 3-6 have not run. The final summary, closing and self-check come after them.
> Per the resume protocol, no number from the 20261119a reports may be reused in docs.

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

### Observations from the resume run (reported, not fixed)

1. **sustained-restart `pin_retention_margin_steady_min` is 0, which is not a steady-state value (affects Task 4).** The 05-28 window runs from main-load start to collection. Read-only Prometheus queries on the live bench stack showed `sentinel_detector_pin_retention_margin_offsets` at 0 on all 6 partitions from 01:56:22Z (main-load start) through 01:56:52Z, 7 samples at 5 s. That is the fresh-topic state, which the window was meant to exclude. After that every partition grows from tens of offsets to over 130,000. Task 4's check "candidate below the steady minimum" is therefore degenerate on the sustained report: every candidate ≥ 0 would be flagged. The overload steady minimum (134,793) is a genuine steady value. Possible remedies are a gap fix to the window start or using the overload input; that is a Task 4 decision. No value was computed or written here.
2. **Detection delay p99 is 281.6 s (bucket 204.8-409.6 s), while p95 is 0.175 s.** This is well beyond the 45.107 s coverage loss. The histogram's resolution at that tail is coarse (n=125). It is worth explaining in ADR 0011. Not diagnosed.
3. **Task 3 blocker: the bench stack cannot show System or Overview lag numbers to any bench identity.** `deployments/compose/docker-compose.bench.yml` (query-api) sets `SENTINEL_AUTH.OPERATOR_TENANTS: demo`. `bench/stack.py` `_check_tenant` only mints JWTs for `bench-<run>[-suffix]` tenants, so no bench identity is an operator tenant (ADR 0014). Through the web proxy, an admin token gets 403 "System status is limited to operator administrators." on /api/v1/system. Admin and analyst get `operator_only` for every group on /api/v1/metrics/lag. So Task 3 steps 3-4 (System replica counts, analyst Overview lag numbers) cannot pass as-is. Proposed fix (needs approval; not applied): have the bench override set the operator tenant to the run's tenant, for example from an env value written by `stack.write_env`, then recreate query-api.
4. **Task 3 step 2 may lack `store_alert`.** Jaeger holds 103 ingest traces. The richest contain `ingest POST /api/v1/events:batch`, `ingest publish`, `detector detect` and `event-writer store` (2 span links), but alert-writer reports no operations. alert-writer has tracing enabled. `store_alert` is emitted only for alerts whose detect span was sampled, and at ratio 0.01 with 120 alerts about one is expected, so zero is plausible sampling.
5. **Plan interface drift for Task 3.** web/vite.config.ts hardcodes the `/api` proxy to 127.0.0.1:8081 and has no env var. A scratch-only config (session scratchpad `vite.bench.config.mjs`, not committed) proxies to 38090. The Jaeger 2.22 UI is at http://127.0.0.1:36686/ (v3 API).

### Commits (resume run)

| Task | Commit | Content |
|---|---|---|
| 2 | 4e3f355 | benchmarks/results/2026-11-20-{sustained-restart,overload}.{md,json} (report.py output, unedited) |

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
