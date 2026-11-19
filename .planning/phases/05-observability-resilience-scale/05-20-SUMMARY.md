---
phase: 05-observability-resilience-scale
plan: "20"
subsystem: bench
tags: [benchmark, sustained-restart, overload, results]
requirements: [BENCH-01, BENCH-02, BENCH-03, BENCH-04, REL-04, OBS-05, UI-05]
status: "DRAFT - Tasks 1-2 done; stopped at Task 3 (checkpoint:human-verify); Task 4 not started"
---

# Phase 5 Plan 20: Real benchmark run and live verification Summary (DRAFT)

> Draft. Tasks 1 and 2 are recorded below. Task 3 (live Grafana/Jaeger/System check) and
> Task 4 (ADR 0011, benchmarks/README.md) have not run. The final summary, closing and
> self-check come after them.

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
