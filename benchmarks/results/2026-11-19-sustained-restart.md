# Sentinel benchmark: sustained-restart (2026-11-19)

| Field | Value |
|---|---|
| Run ID | 20261119a |
| Tenant | bench-20261119a |
| Started (UTC) | 2026-11-19T10:43:38.766Z |
| Ended (UTC) | 2026-11-19T11:28:47.482Z |
| Duration (s) | 2,708.716 |

## Hardware

| Field | Value |
|---|---|
| CPU model | 13th Gen Intel(R) Core(TM) i5-13500H |
| Logical CPUs | 16 |
| RAM (GiB) | 15.31 |
| Free disk at start (GiB) | 18.04 |
| Kernel | 6.8.0-138-generic |
| Docker | 29.9.2 |
| Compose | 5.6.1 |
| Host load | MemAvailable at preflight 7.58 GiB; other running compose projects: none |

## Configuration

| Setting | Value |
|---|---|
| Kafka events partitions | 6 |
| Kafka replication factor | 1 |
| Replicas: alert_writer | 1 |
| Replicas: detector | 3 |
| Replicas: event_writer | 2 |
| Replicas: ingest | 2 |
| Replicas: query_api | 2 |
| Target rate (ev/s) | 400 |
| Batch size (events/request) | 200 |
| Max in-flight requests | 64 |
| Rate limit capacity | 10,000 |
| Rate limit refill (/s) | 2,000 |
| Retry budget (s) | 15 |
| Lateness tolerance (s) | 30 |
| Detector windows cap | 10,000 |
| Detector ledger cap | 100,000 |
| Detector bytes cap | 67,108,864 |

## Workload

| Field | Value |
|---|---|
| Total events generated | 1,000,000 |
| Requests | 5,000 |
| Run duration (s) | 2,708.716 |
| Event size (bytes) min/p50/p95/max | 389 / 394 / 398 / 415 |
| Target input rate (ev/s) | 400 |
| Offered input rate | p50 400 / p95 400 ev/s over 249 windows |

## Throughput

| Metric | Value |
|---|---|
| Sustained accepted rate | p50 400 / p95 400 ev/s over 249 windows |
| Accepted unique events | 1,000,000 |
| Harness CPU max (%) | 3.02 |

## Latency

Ingest is per request of 200 events. Done-minus-intended is corrected for coordinated omission; done-minus-sent is service time.

| Metric | n | p50 | p95 | p99 |
|---|---|---|---|---|
| Ingest done-intended (ms) | 5,000 | 29.542 | 35.254 | 39.831 |
| Ingest done-sent (ms) | 5,000 | 20.667 | 25.475 | 30.916 |
| Detection delay (histogram) | 125 | 1.181 s (bucket 0.8-1.6 s) | 35.2 s (bucket 25.6-51.2 s) | 179.2 s (bucket 102.4-204.8 s) |
| SSE notification (ms) | 120 | 732.519 | 1,085.754 | 1,120.485 |

Schedule slip: max 6,356.03 ms, p99 12.135 ms.

Detection delay is interpolated within Prometheus histogram buckets; the bucket bounds shown are its resolution.

| API endpoint | n | p50 (ms) | p95 (ms) | non-2xx |
|---|---|---|---|---|
| /api/v1/alerts | 150 | 9,647.735 | 16,004.023 | 0 |
| /api/v1/events | 150 | 13,419.775 | 19,830.446 | 150 |

## Resources

| Container | Service | Peak RSS (MiB) | Peak CPU (%) |
|---|---|---|---|
| sentinel-bench-20261119a-alert-writer-1 | alert-writer | 33.2 | 4.95 |
| sentinel-bench-20261119a-clickhouse-1 | clickhouse | 1,430.528 | 42.94 |
| sentinel-bench-20261119a-detector-1 | detector | 20.31 | 6.83 |
| sentinel-bench-20261119a-detector-2 | detector | 23.68 | 8.73 |
| sentinel-bench-20261119a-detector-3 | detector | 22.18 | 8.23 |
| sentinel-bench-20261119a-event-writer-1 | event-writer | 20.91 | 6.17 |
| sentinel-bench-20261119a-event-writer-2 | event-writer | 42.98 | 7.12 |
| sentinel-bench-20261119a-grafana-1 | grafana | 368.9 | 14.85 |
| sentinel-bench-20261119a-ingest-1 | ingest | 37.23 | 7.36 |
| sentinel-bench-20261119a-ingest-2 | ingest | 14.15 | 8.74 |
| sentinel-bench-20261119a-jaeger-1 | jaeger | 19.03 | 0.6 |
| sentinel-bench-20261119a-kafka-1 | kafka | 698.7 | 174.34 |
| sentinel-bench-20261119a-postgres-1 | postgres | 95.29 | 9.73 |
| sentinel-bench-20261119a-prometheus-1 | prometheus | 136.3 | 6.82 |
| sentinel-bench-20261119a-query-api-1 | query-api | 16.36 | 5.01 |
| sentinel-bench-20261119a-query-api-2 | query-api | 39.7 | 5.12 |
| sentinel-bench-20261119a-redis-1 | redis | 36.88 | 4.98 |

Peak detector state: windows 9.83%, ledger 67.352%, bytes 11.677% of per-process caps (max 67.352%).

## Consumer lag and recovery

Recovery time is t_recovered - t_kill, where t_recovered is the first time the max detector consumer lag stays <= the pre-kill p95 baseline for 30 consecutive seconds of samples.

| Field | Value |
|---|---|
| Killed container | sentinel-bench-20261119a-detector-1 |
| Baseline window (s) | 60 |
| Baseline p95 lag | 0 |
| Peak lag after kill | 0 |
| Recovered | yes |
| Recovery time (s) | 0.438 |
| Timeout (s) | 600 |

## Loss and duplicates

| Count | Value |
|---|---|
| Generated | 1,000,000 |
| Accepted (unique event IDs) | 1,000,000 |
| Rejected 4xx | 0 |
| Unaccepted after retry budget | 0 |
| Kafka records | 1,000,000 |
| Producer-side duplicates | 0 |
| ClickHouse unique | 1,000,000 |
| ClickHouse physical | 1,000,000 |
| Physical duplicates | 0 |
| Loss | 0 |
| Event-ID digest match | yes |
| DLQ delta | 0 |
| Late-event delta | 0 |

## Rule hits

| Rule | Expected | Observed (PostgreSQL) | Observed (ClickHouse) |
|---|---|---|---|
| AUTH_BRUTE_FORCE | 30 | 30 | 30 |
| DENY_SPIKE | 30 | 30 | 30 |
| PORT_SCAN | 30 | 30 | 30 |
| REQUEST_FLOOD | 30 | 30 | 30 |

Benign hits: 0.

## Alert threshold inputs

| input | value |
|---|---|
| ingest requests by status | {'202': 5000} |
| ingest 5xx ratio | 0 |
| min pin retention margin (offsets) | 0 |
| warm-up max partition share | 0.18 |
| warm-up rate (events/s) | 410 |


## Limitations

- counter increments on the killed detector between its last scrape (5 s interval) and the SIGKILL are not observable.
- the killed detector was not restarted by its restart policy before collection; recovery ran on the surviving replicas.
- SSE subscriber: 0 malformed frames, 6 reconnects, fatal status None.
- resources.samples rss_bytes is docker stats MemUsage (cgroup memory usage without page cache), the closest per-container RSS docker exposes.
- SSE notification latency is an upper bound on persistence latency: there is no wall-clock persistence column, and PostgreSQL persistence precedes SSE delivery.
- Event size distribution is summarized by the harness; per-event sizes are not stored.

## Reproduction

- git SHA: `dec4a7e69fde274319c1be1c2fa9c41441b67273`
- image tag: `sentinel-bench-20261119a-<service> (compose build, revision dec4a7e69fde274319c1be1c2fa9c41441b67273)`
- Compose project: `sentinel-bench-20261119a`

```sh
make bench SCENARIO=sustained-restart RUN=20261119a
python3 bench/report.py --raw <raw.json> --out benchmarks/results
```
