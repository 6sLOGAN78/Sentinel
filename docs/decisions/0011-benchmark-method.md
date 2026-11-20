# 0011. Benchmark method: open-loop load, exact reconciliation and refusing reports

## Status

Accepted, 2026-11-20. Plans 05-08, 05-17, 05-18 and 05-27/05-28 built the harness and report, and plan 05-20 ran it. The first run (20261119a, 2026-11-19) was superseded because GET /api/v1/events returned 503 at 1M events and its recovery time was measured without partition coverage (findings closed by 05-22..05-28). The current measured results are [2026-11-20-sustained-restart](../../benchmarks/results/2026-11-20-sustained-restart.md) and [2026-11-20-overload](../../benchmarks/results/2026-11-20-overload.md), run 20261120a at git `8424348f217194499ab45bdeb1559b1f51fbb1eb`. Implemented in `bench/` (run.py, stack.py, scenarios.py, collect.py, report.py, RAW_FORMAT.md) and `generator/src/sentinel_generator/bench/`.

## Context

Spec §18 forbids invented numbers, and BENCH-01..04 and REL-04 need a reproducible run of at least 1,000,000 events across several replicas, with a detector killed under load. A benchmark that is closed-loop, uses synthetic past timestamps or counts "sent" instead of "stored" can look good while hiding loss, duplicates or stalls. The detector's per-process admission caps (10,000 windows, 100,000 ledger entries, 64 MiB, `detection/reducer.go`) are Phase 3 contracts and must not be raised for a benchmark. So the workload has to fit them on purpose, not by luck.

## Decision

**Open-loop schedule with intended-time latency.** Batch *k* has the intended send time `T0 + k·(B/R)`. An asyncio task per batch starts at that time whatever earlier responses did, and a semaphore caps in-flight requests (64 sustained, 256 overload). Latency is reported as `done − intended`, which is corrected for coordinated omission, and as `done − sent` (service time), plus schedule slip. Slip p99 above 1 s marks the generator as saturated. Retries resend the same bytes (idempotent `event_id`) within a 15 s budget, and final non-acceptance is counted as `unaccepted`, never dropped. Overload sends one attempt per request.

**Live event time and bounded entity pools.** Every event in batch *k* carries `occurred_at` = its intended send time, so ingest skew checks hold for a 45-minute run and event-time latency includes client queueing. Benign traffic draws from bounded deterministic pools (default 200 sources, 200 principals, 2 destinations, 16 ports) disjoint from the attack network. The generator computes the maximum per-key count per rule window exactly, so "0 benign hits" is a checked fact. Attacks are distinct entities, one window each, so the expected hits per rule are known in advance (30 per rule in sustained-restart).

**Capacity preflight.** Before sending, the harness predicts per-process detector state after N−1 failover over the 330 s ledger horizon (pin to head) and refuses above 80% of any cap. The static plan assumes even partitions. A recheck after warm-up uses the measured partition skew. In run 20261120a the warm-up max partition share was 0.18258064516129033 (above the even 1/6), giving `f_proc` 0.547741935483871 for a survivor owning three partitions, `r_proc` 219.09677419354838 ev/s and a predicted ledger of 72302 entries: 72.3% of the 100000-entry cap, below the 80000-entry (80%) preflight refusal limit ([JSON](../../benchmarks/results/2026-11-20-sustained-restart.json) `preflight.recheck`).

**Exact reconciliation, and refusal instead of a report.** `report.py` recomputes every number from one raw JSON file (`bench/RAW_FORMAT.md`) and checks these equations on every run. Any failure exits 1 and writes only `<date>-<scenario>.FAILED.json`, never a `.md`:

- `generated == accepted_unique + rejected_4xx + unaccepted_final`, and generated equals the schedule sum
- Kafka records ≥ accepted; ClickHouse unique == accepted (loss = 0); physical ≥ unique
- the event-ID digest of ClickHouse equals the client's accepted-ID digest
- per rule: PostgreSQL alerts == expected, ClickHouse unique alerts == PostgreSQL alerts; benign hits == 0
- DLQ and late-event counter deltas equal the deliberately injected counts (0)
- sustained-restart: ≥ 1,000,000 generated, a recorded kill, full partition coverage in the pre-kill baseline, recovery within the timeout, and every API-phase response 2xx (05-27)
- every scenario: running detectors at start ≥ configured (05-28); consumers drained before collection
- overload: statuses ⊆ {202, 429, 503}, producer buffer max ≤ limit, no OOM kill, no ingest restart

Non-`measured` data aimed at `benchmarks/results` is refused (exit 2), and an existing report is never overwritten (exit 3). `--supersede` moves a same-date report from another run, byte-for-byte, to `benchmarks/results/superseded/`.

**md5-xor digest.** The digest is `groupBitXor(reinterpretAsUInt64(substring(MD5(toString(event_id)), 1, 8)))` over distinct event IDs in ClickHouse, compared to the same fold computed client-side over accepted IDs. It is order-independent and catches a swapped ID that counts alone would miss. The expression is checked against the client implementation on a fixed 3-ID literal before any count is taken.

**Recovery time is gated on partition coverage.** `recovery_seconds = t_recovered − t_kill`, where `t_recovered` is the first sample that starts 30 consecutive seconds of 5 s samples in which the max detector lag is ≤ the pre-kill p95 baseline **and** every events partition has a live lag reporter (05-27). Lag alone is not enough: the killed member's partitions have no reporter until the group's 45 s session timeout expires and the survivors rebalance, so a lag-only measure reads 0 exactly while those partitions are orphaned (the 20261119a artifact). The session timeout is therefore a component of recovery. Measured in 20261120a: coverage lost 45.107 s (minimum 4 of 6 partitions reported), recovery time 53.759 s, peak lag after kill 33. After recovery, the harness restarts the killed container (`docker kill` bypasses the restart policy) and waits for it to rejoin with full coverage, so later scenarios run on all configured detectors.

**SSE arrival is the persistence-latency upper bound.** There is no wall-clock persistence column (PostgreSQL `created_at` is event-time derived), and PostgreSQL persistence precedes SSE delivery. The report therefore measures `received − trigger_intended` per alert on its first SSE arrival and labels it an upper bound on persistence latency.

**Publications versus unique alerts.** Counters and the `sentinel_detection_delay_seconds` histogram count acknowledged publications, including crash-recovery replays. The stores hold unique alerts, and the reconciliation reads unique counts from the stores. In 20261120a the histogram has n=125 for 120 unique alerts.

**Isolated-project safety rails.** Every Docker command targets `sentinel-bench-<RUN>` with a private generated env (`.local/bench/<RUN>/`, 0700/0600) and fixed loopback ports. Projects `sentinel` and `sentinel-p4-demo-*` are refused, as are a busy port, another running bench project, or an existing run directory. Floors are 15 GiB free disk and 6 GiB MemAvailable, which may only be raised. Purge needs `CONFIRM=1`, and nothing ever prunes Docker. The run tenant `bench-<RUN>` is the stack's only operator tenant (ADR 0014 note).

## Consequences

- **Detection-delay tail explained.** The sustained-restart report shows detection delay p50 0.036 s, p95 0.175 s and p99 281.6 s (bucket 204.8-409.6 s). The p99 is not a slow first detection. A read-only diagnosis of run 20261120a (detector logs, PostgreSQL `detection_decisions`, per-instance Prometheus deltas) found:
  - Five of the 125 observations lie above 51.2 s. All five are replays of alerts already published, after the kill: the kill came at 02:07:53Z, ownership moved after the 45 s session timeout (takeover 02:08:38Z), and the restarted detector-1 rejoined at 02:09:21Z.
  - AUTH_BRUTE_FORCE (300 s) and DENY_SPIKE (120 s) windows were still unsealed, so the recovery pin held back and the stored decisions republished their original bytes. REQUEST_FLOOD windows sealed before the kill were not replayed (control case).
  - The p99 is interpolated inside the 204.8-409.6 s bucket from two AUTH_BRUTE_FORCE replays (about 218 s and 261 s).
  - Only one unique alert was late on first publication: PORT_SCAN `1ff8161132` reached SSE 38.424 s after its trigger, the remaining ownership gap. The raw SSE arrivals show the other 119 unique alerts within 1.136984 s. The report's nearest-rank SSE p99 (rank 119 of 120) does not show that one value.
  - Follow-ups: label the histogram row "per publication, incl. crash replays", and add a unique-alert first-detection delay to the report.
- **Honest scaling limit.** The bottleneck is the per-process detector admission ceiling, not throughput: up to 330 s of events from pin to head must fit in a 100,000-entry ledger (80% admission limit). The measured peak detector state in sustained-restart was ledger 52.877% of the per-process cap (windows 8.3%, bytes 9.634%). In overload it was ledger 45.413%. More throughput needs more partitions and detector replicas, not larger caps.
- **Query scaling limits (05-22, 05-23).** Events-list stage 2 resolves winners by reading `event_id` across the tenant's 7-day retention, with only bloom-filter skipping. Its cost grows with retention and false positives, which is fine at the 1M target (/api/v1/events p50 43.159 ms, p95 57.291 ms, 0 non-2xx in 20261120a). The Overview summary is O(tenant identities over the 7-day TTL).
- **Shed paths.** Overload exercised only 429 (the Redis limiter sheds everything above refill, so the producer buffer peaked at 0 of 50000). The 503 paths are proven deterministically by `TestChaosOverloadBounded` and `TestChaosKafkaBrokerRestart`, not by the benchmark.
- **Pin threshold inputs.** The sustained-restart steady-state pin margin minimum is 0 because the 05-28 window opens at main-load start, while the fresh topic still reads 0. The pin alert threshold therefore uses the overload steady minimum 134793 as its ceiling (operations.md, Pin retention hazard). A log-start-offset metric would let the window and the alert exclude fresh topics (follow-up).
- Results depend on one host (i5-13500H, 16 logical CPUs, 15.31 GiB, Docker 29.9.2) with replication factor 1. They show behaviour and correctness under load, not production capacity.
