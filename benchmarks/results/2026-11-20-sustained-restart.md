# Sentinel benchmark: sustained-restart (2026-11-20)

| Field | Value |
|---|---|
| Run ID | 20261120a |
| Tenant | bench-20261120a |
| Started (UTC) | 2026-11-20T01:55:24.976Z |
| Ended (UTC) | 2026-11-20T02:40:18.078Z |
| Duration (s) | 2,693.102 |

## Hardware

| Field | Value |
|---|---|
| CPU model | 13th Gen Intel(R) Core(TM) i5-13500H |
| Logical CPUs | 16 |
| RAM (GiB) | 15.31 |
| Free disk at start (GiB) | 14.513 |
| Kernel | 6.8.0-138-generic |
| Docker | 29.9.2 |
| Compose | 5.6.1 |
| Host load | MemAvailable at preflight 8.90 GiB; other running compose projects: none |

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
| Running at start/end: alert-writer | 1 / 1 |
| Running at start/end: clickhouse | 1 / 1 |
| Running at start/end: detector | 3 / 3 |
| Running at start/end: event-writer | 2 / 2 |
| Running at start/end: grafana | 1 / 1 |
| Running at start/end: ingest | 2 / 2 |
| Running at start/end: jaeger | 1 / 1 |
| Running at start/end: kafka | 1 / 1 |
| Running at start/end: postgres | 1 / 1 |
| Running at start/end: prometheus | 1 / 1 |
| Running at start/end: query-api | 2 / 2 |
| Running at start/end: redis | 1 / 1 |
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
| Run duration (s) | 2,693.102 |
| Event size (bytes) min/p50/p95/max | 389 / 394 / 398 / 415 |
| Target input rate (ev/s) | 400 |
| Offered input rate | p50 400 / p95 400 ev/s over 249 windows |

## Throughput

| Metric | Value |
|---|---|
| Sustained accepted rate | p50 400 / p95 400 ev/s over 249 windows |
| Accepted unique events | 1,000,000 |
| Harness CPU max (%) | 2.75 |

## Latency

Ingest is per request of 200 events. Done-minus-intended is corrected for coordinated omission; done-minus-sent is service time.

| Metric | n | p50 | p95 | p99 |
|---|---|---|---|---|
| Ingest done-intended (ms) | 5,000 | 28.781 | 35.15 | 42.938 |
| Ingest done-sent (ms) | 5,000 | 20.879 | 26.549 | 36.804 |
| Detection delay (histogram) | 125 | 0.036 s (bucket 0-0.05 s) | 0.175 s (bucket 0.1-0.2 s) | 281.6 s (bucket 204.8-409.6 s) |
| SSE notification (ms) | 120 | 596.802 | 1,080.951 | 1,136.984 |

Schedule slip: max 6,069.076 ms, p99 11.748 ms.

Detection delay is interpolated within Prometheus histogram buckets; the bucket bounds shown are its resolution.

| API endpoint | n | p50 (ms) | p95 (ms) | non-2xx |
|---|---|---|---|---|
| /api/v1/alerts | 150 | 7.937 | 9.835 | 0 |
| /api/v1/events | 150 | 43.159 | 57.291 | 0 |

## Resources

| Container | Service | Peak RSS (MiB) | Peak CPU (%) |
|---|---|---|---|
| sentinel-bench-20261120a-alert-writer-1 | alert-writer | 13.52 | 7.99 |
| sentinel-bench-20261120a-clickhouse-1 | clickhouse | 897.4 | 40.48 |
| sentinel-bench-20261120a-detector-1 | detector | 23.23 | 7.8 |
| sentinel-bench-20261120a-detector-2 | detector | 20.93 | 11.98 |
| sentinel-bench-20261120a-detector-3 | detector | 22.47 | 9.52 |
| sentinel-bench-20261120a-event-writer-1 | event-writer | 16.07 | 9.9 |
| sentinel-bench-20261120a-event-writer-2 | event-writer | 16.99 | 9.67 |
| sentinel-bench-20261120a-grafana-1 | grafana | 452.7 | 17.31 |
| sentinel-bench-20261120a-ingest-1 | ingest | 14.39 | 9.76 |
| sentinel-bench-20261120a-ingest-2 | ingest | 11.91 | 8.72 |
| sentinel-bench-20261120a-jaeger-1 | jaeger | 71.39 | 2.44 |
| sentinel-bench-20261120a-kafka-1 | kafka | 644.2 | 177.17 |
| sentinel-bench-20261120a-postgres-1 | postgres | 99.79 | 8.81 |
| sentinel-bench-20261120a-prometheus-1 | prometheus | 134.5 | 5.89 |
| sentinel-bench-20261120a-query-api-1 | query-api | 13.98 | 8.01 |
| sentinel-bench-20261120a-query-api-2 | query-api | 13.18 | 8.14 |
| sentinel-bench-20261120a-redis-1 | redis | 40.33 | 4.39 |

Peak detector state: windows 8.3%, ledger 52.877%, bytes 9.634% of per-process caps (max 52.877%).

## Consumer lag and recovery

Recovery time is t_recovered - t_kill, where t_recovered is the first time the max detector consumer lag stays <= the pre-kill p95 baseline with every events partition reported for 30 consecutive seconds of samples.

| Field | Value |
|---|---|
| Killed container | sentinel-bench-20261120a-detector-1 |
| Baseline window (s) | 60 |
| Baseline p95 lag | 0 |
| Peak lag after kill | 33 |
| Partitions expected | 6 |
| Min partitions reported after kill | 4 |
| Coverage lost (s) | 45.107 |
| Recovered | yes |
| Recovery time (s) | 53.759 |
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
| min pin retention margin, whole run incl. fresh topic (offsets) | 0 |
| steady-state min pin retention margin (offsets) | 0 |
| max per-partition append rate, main load (events/s) | 73.943 |
| warm-up max partition share | 0.183 |
| warm-up rate (events/s) | 413.333 |


## Limitations

- counter increments on the killed detector between its last scrape (5 s interval) and the SIGKILL are not observable.
- SSE subscriber: 0 malformed frames, 6 reconnects, fatal status None.
- resources.samples rss_bytes is docker stats MemUsage (cgroup memory usage without page cache), the closest per-container RSS docker exposes.
- SSE notification latency is an upper bound on persistence latency: there is no wall-clock persistence column, and PostgreSQL persistence precedes SSE delivery.
- Event size distribution is summarized by the harness; per-event sizes are not stored.

## Reproduction

- git SHA: `8424348f217194499ab45bdeb1559b1f51fbb1eb`
- image tag: `sentinel-bench-20261120a-<service> (compose build, revision 8424348f217194499ab45bdeb1559b1f51fbb1eb)`
- Compose project: `sentinel-bench-20261120a`

```sh
make bench SCENARIO=sustained-restart RUN=20261120a
python3 bench/report.py --raw <raw.json> --out benchmarks/results
```
