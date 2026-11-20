# Benchmarks

Every number on this page is copied verbatim from a committed results file written by
`bench/report.py`, and the file is linked next to it. The method, reconciliation
equations and recovery definition are in
[ADR 0011](../docs/decisions/0011-benchmark-method.md). `report.py` refuses to write a
report (exit 1, `FAILED.json` only) when any reconciliation equation fails, so a results
file exists only for a reconciled run.

## Reproduce

Prerequisites: Docker with Compose, `uv` (the generator venv), and the free-disk and
memory floors described in [docs/operations.md](../docs/operations.md#benchmark-tooling),
which also lists the loopback ports and safety rails. Check the floors first:

```sh
bash scripts/check-resources.sh
make bench-up RUN=<id>                                    # isolated sentinel-bench-<id> stack
make bench SCENARIO=sustained-restart RUN=<id> KEEP_UP=1
make bench SCENARIO=overload RUN=<id> KEEP_UP=1
make bench-down RUN=<id>                                  # stop (volumes kept)
make bench-purge RUN=<id> CONFIRM=1                       # remove volumes, images, private dir
```

A report can be rebuilt offline from its raw file:
`python3 bench/report.py --raw <raw.json> --out <dir>`. Add `SUPERSEDE=1` to `make bench`
when a new run falls on a date that already has results.

## Current results (run 20261120a)

Files: [2026-11-20-sustained-restart.md](results/2026-11-20-sustained-restart.md)
([json](results/2026-11-20-sustained-restart.json)) and
[2026-11-20-overload.md](results/2026-11-20-overload.md)
([json](results/2026-11-20-overload.json)). Git SHA
`8424348f217194499ab45bdeb1559b1f51fbb1eb`, one host (13th Gen Intel(R) Core(TM)
i5-13500H, 16 logical CPUs, 15.31 GiB RAM), Kafka replication factor 1.

### sustained-restart ([report](results/2026-11-20-sustained-restart.md))

Replicas: ingest 2, detector 3, event-writer 2, alert-writer 1, query-api 2; 6 events
partitions; open-loop target 400 ev/s in 200-event requests.

| Metric | Value |
|---|---|
| Total events generated | 1,000,000 |
| Accepted unique / Kafka records / ClickHouse unique / ClickHouse physical | 1,000,000 each |
| Loss / producer-side duplicates / physical duplicates | 0 / 0 / 0 |
| Event-ID digest match | yes |
| Rule hits (expected / PostgreSQL / ClickHouse), each of AUTH_BRUTE_FORCE, DENY_SPIKE, PORT_SCAN, REQUEST_FLOOD | 30 / 30 / 30 |
| Benign hits | 0 |
| Sustained accepted rate | p50 400 / p95 400 ev/s over 249 windows |
| Ingest done-intended (ms), coordinated-omission corrected | p50 28.781, p95 35.15, p99 42.938 |
| Ingest done-sent (ms), service time | p50 20.879, p95 26.549, p99 36.804 |
| Detection delay (histogram, n=125) | p50 0.036 s, p95 0.175 s, p99 281.6 s (bucket 204.8-409.6 s) |
| SSE notification (ms, n=120), upper bound on persistence latency | p50 596.802, p95 1,080.951, p99 1,136.984 |
| /api/v1/alerts (n=150) | p50 7.937 ms, p95 9.835 ms, non-2xx 0 |
| /api/v1/events (n=150) | p50 43.159 ms, p95 57.291 ms, non-2xx 0 |
| Killed container | sentinel-bench-20261120a-detector-1 |
| Partitions expected / min reported after kill | 6 / 4 |
| Coverage lost (s) | 45.107 |
| Recovery time (s), coverage-gated | 53.759 |
| Running detectors at start/end | 3 / 3 |
| Peak detector state (per-process caps) | windows 8.3%, ledger 52.877%, bytes 9.634% |

**About the detection-delay p99.** The histogram counts every acknowledged
publication, including crash-recovery replays: n=125 observations for 120 unique alerts
(30 per rule). The p99 does not describe first detection. It comes from replays,
after the detector kill, of alerts that had already been published from windows still
open at the kill. The stored decisions republish their original bytes, so the delay
counts from the original trigger. First-arrival SSE latency per unique alert is the
better user-facing figure (p99 1,136.984 ms). ADR 0011 (Consequences) gives the
per-alert evidence, including the single unique alert delayed by the ownership gap
after the kill.

### overload ([report](results/2026-11-20-overload.md))

Same stack, target 6,000 ev/s in 100-event requests against a rate limit refill of
2,000/s, one attempt per request.

| Metric | Value |
|---|---|
| Running detectors at start/end | 3 / 3 |
| Status counts | 202: 1,281; 429: 2,319 (no 503, no 5xx) |
| Exercised shed paths | 429 yes, 503 no |
| Producer buffer max / limit | 0 / 50000 |
| OOM-killed containers / restarts | none / 0 for every service |
| Generated / accepted unique | 360,000 / 128,100 |
| Loss / duplicates | 0 / 0 |
| Sustained accepted rate | p50 2,000 / p95 2,800 ev/s over 6 windows |
| Ingest done-intended (ms) | p50 9.704, p95 23.748, p99 514.023 |
| Peak detector state | windows 24.85%, ledger 45.413%, bytes 15.004% |

The 503 shed paths (ingest in-flight cap, producer buffer full, broker unavailable) were
not exercised by this run. They are proven deterministically by `TestChaosOverloadBounded`
and `TestChaosKafkaBrokerRestart`.

### Alert threshold inputs

| Input | Value | Source |
|---|---|---|
| ingest 5xx ratio | 0 (both runs) | `threshold_inputs.ingest_5xx_ratio` |
| max per-partition append rate, main load (events/s) | 73.943 | [sustained-restart](results/2026-11-20-sustained-restart.md) |
| steady-state min pin retention margin (offsets) | 134,793 | [overload](results/2026-11-20-overload.md) |
| steady-state min pin retention margin (offsets) | 0 (fresh topic at main-load start) | [sustained-restart](results/2026-11-20-sustained-restart.md) |

`SentinelPinRetentionLow` and `SentinelIngestErrorRate` derive from these inputs; see
[docs/operations.md](../docs/operations.md#alert-examples).

## Limitations

Copied from the reports:

- counter increments on the killed detector between its last scrape (5 s interval) and the SIGKILL are not observable.
- resources.samples rss_bytes is docker stats MemUsage (cgroup memory usage without page cache), the closest per-container RSS docker exposes.
- SSE notification latency is an upper bound on persistence latency: there is no wall-clock persistence column, and PostgreSQL persistence precedes SSE delivery.
- Event size distribution is summarized by the harness; per-event sizes are not stored.
- overload sends one attempt per request (no retries), so client.status_counts are exact per request; whether in-flight 503s occur is measured, not assumed.
- API latency was not measured in this scenario. (overload)
- No detector kill in this scenario; recovery not measured. (overload)

Also: one host, replication factor 1. These runs show correctness and behaviour under
load and failure, not production capacity. The scaling limit is the per-process detector
admission ceiling (ADR 0011).

## Superseded results (run 20261119a)

[2026-11-19-sustained-restart](results/2026-11-19-sustained-restart.md) and
[2026-11-19-overload](results/2026-11-19-overload.md) are kept unedited as history and
are **superseded**. GET /api/v1/events returned 503 at 1M events in that run, and its
recovery time was measured without partition coverage, so it could not see the
session-timeout gap. Gap plans 05-22..05-28 closed both, and `report.py` now refuses such
a raw. No number from those files is claimed.
