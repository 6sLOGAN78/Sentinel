# Benchmark raw-run format (format_version 1)

`bench/report.py` reads exactly one raw-run JSON file in this format and recomputes
every reported number from it. The bench harness (`bench/run.py`, plans 05-08 / 05-18)
produces it. The report never takes a number from anywhere else, so a report can be
rebuilt offline from the saved raw file:

```sh
python3 bench/report.py --raw <raw.json> --out benchmarks/results
python3 bench/report.py --selftest
```

Exit codes:

| Exit | Meaning | Files written |
|------|---------|---------------|
| 0 | every reconciliation equation held | `<date>-<scenario>.md` and `<date>-<scenario>.json` (0644) |
| 1 | at least one equation failed, or the raw file is malformed | `<date>-<scenario>.FAILED.json` for failed equations; nothing for malformed input |
| 2 | provenance refusal: non-`measured` data aimed at `benchmarks/results` | nothing |
| 3 | a target file already exists (reports are never overwritten) | nothing |

`<date>` is the UTC calendar date of `run.started_at`, never the wall clock of the
machine running `report.py`.

## Conventions

- All timestamps inside samples are integer nanoseconds since the Unix epoch (`*_ns`).
- `run.started_at` / `run.ended_at` are RFC 3339 UTC strings ending in `Z`.
- Counts are non-negative integers. Booleans are never used as integers.
- Prometheus counter pairs (`*_before` / `*_after`, `detection_delay_buckets_*`) must be
  reset-corrected by the harness. If a replica restarted during the run, the harness
  supplies `sum(increase(...[run]))` as `after` with zero `before`. A negative delta is
  a refusal, not something the report repairs.
- Unknown extra keys are allowed; `report.py` lists them under Limitations so nothing
  silently disappears.
- The format has no key, token or password fields. `report.py` refuses to emit output
  that contains secret-like substrings (`sk_`, `snt_`, `Bearer `, `password`,
  `PRIVATE KEY`).

## Top-level keys

| Key | Type | Notes |
|-----|------|-------|
| `format_version` | int | must be `1` |
| `provenance` | `"measured"` \| `"fixture"` | `"fixture"` is test data and is refused for `benchmarks/results` |
| `run` | object | see below |
| `config` | object | see below |
| `hardware` | object | see below |
| `client` | object | see below |
| `attack_plan` | object | see below |
| `kafka` | object | see below |
| `clickhouse` | object | see below |
| `postgres` | object | see below |
| `prometheus` | object | see below |
| `sse` | object | see below |
| `api` | object | see below |
| `resources` | object | see below |
| `restart` | object \| null | null when the scenario has no kill |
| `limitations` | list of strings | harness-recorded limitations, copied into the report |

### `run`

`run_id`, `scenario` (`sustained-restart` \| `overload` \| `api-latency`), `tenant`
(`bench-<run_id>`), `started_at`, `ended_at` (RFC 3339 UTC), `git_sha`, `image_tag`,
`compose_project`.

### `config`

`events_partitions`, `replication_factor`,
`replicas{ingest, event_writer, detector, alert_writer, query_api}`, `target_rate_eps`,
`batch_size`, `max_in_flight_requests`, `rate_limit{capacity, refill_per_second}`,
`retry_budget_seconds`, `lateness_seconds`,
`detector_limits{windows, ledger_entries, bytes}` (per-process caps).

### `hardware`

`cpu_model`, `logical_cpus`, `ram_bytes`, `disk_free_bytes_start`, `kernel`,
`docker_version`, `compose_version`, `host_load_note`.

### `client`

| Field | Meaning |
|-------|---------|
| `generated` | events in the precomputed schedule |
| `event_size_bytes{min, p50, p95, max}` | serialized event size distribution, summarized by the harness (1M per-event sizes are not stored) |
| `requests[]` | one record per scheduled batch, final outcome: `{batch_seq, intended_ns, sent_ns, done_ns, status, attempts, event_count, accepted_count}` |
| `accepted_unique` | distinct `event_id`s returned in 202 `event_ids` |
| `accepted_id_digest` | 16 lowercase hex chars: XOR over the distinct accepted IDs of the first 8 bytes of `md5(event_id)` read as a little-endian uint64 |
| `rejected_4xx` | events finally rejected with 4xx (expected 0) |
| `unaccepted_final` | events never accepted after the bounded retry budget |
| `schedule_slip_ms_max` | harness-reported max slip (the report recomputes slip from `requests`) |
| `harness_cpu_percent_max` | peak harness CPU, proves the sender was not the bottleneck |

`intended_ns` is the open-loop schedule time, `sent_ns` the first send attempt, `done_ns`
the final response. `event_id` strings are the canonical lowercase UUID text, exactly
what ClickHouse `toString(event_id)` returns.

### `attack_plan`

`rules{RULE_ID: {expected: int}}` (rule IDs such as `AUTH_BRUTE_FORCE`, `PORT_SCAN`,
`REQUEST_FLOOD`, `DENY_SPIKE`), `expected_dlq`, `expected_late`.

### `kafka`

`events_records_delta`: sum of end-offset deltas on the fresh events topic.

### `clickhouse`

`unique` (`uniqExact(event_id)` for the run tenant), `physical` (`count()` for the run
tenant), `id_digest` (16 lowercase hex chars), `alerts_unique_by_rule{RULE_ID: int}`
(`uniqExact(alert_id)`).

Digest query (run tenant only), converted by the harness to 16-char lowercase hex:

```sql
SELECT groupBitXor(reinterpretAsUInt64(substring(MD5(toString(event_id)), 1, 8)))
FROM (SELECT DISTINCT event_id FROM events WHERE tenant_id = {tenant:String})
```

The inner `DISTINCT` matters: physical duplicates would otherwise XOR out in pairs.
`reinterpretAsUInt64` reads the bytes little-endian, which matches
`int.from_bytes(md5(id).digest()[:8], "little")` in `report.id_digest`. Plan 05-18
confirms the byte order against a 3-ID fixture on a real ClickHouse before relying on it.

### `postgres`

`alerts_by_rule{RULE_ID: int}` (alert_workflows rows for the run tenant), `benign_hits`
(alerts for the run tenant whose entity is outside the attack plan's entity ranges).

### `prometheus`

| Field | Meaning |
|-------|---------|
| `detection_delay_buckets_before{le: count}` / `_after` | cumulative `sentinel_detection_delay_seconds_bucket` summed over replicas; `le` strings include `"+Inf"` |
| `dlq_total_before` / `dlq_total_after` | DLQ record counter |
| `late_total_before` / `late_total_after` | `sentinel_detection_late_events_total` |
| `detector_state_peak{windows, ledger_entries, bytes}` | max per-process detector state gauges during the run |
| `lag_samples[]` | `{t_ns, group, max_lag}` position-based consumer lag samples |

### `sse`

`arrivals[]`: `{alert_id, rule_id, trigger_intended_ns, received_ns}`. The first arrival
per `alert_id` counts.

### `api`

`samples[]`: `{endpoint, intended_ns, done_ns, status}` from the fixed-rate open-loop
query phase; `concurrency`; `dataset_note`. Empty `samples` means the scenario did not
measure the API.

### `resources`

`samples[]`: `{t_ns, container, service, rss_bytes, cpu_percent}` from `docker stats`.

### `restart`

`{kill_ns, killed_container, baseline_window_s, stable_window_s: 30, timeout_s}` or
`null`. Required (non-null) for scenario `sustained-restart`.

## Reconciliation equations

Every equation is checked on every run. Any failure means exit 1, no `.md` and a
`FAILED.json` listing each failing equation with `id`, `message`, `expected` and
`observed`.

| id | Equation | Refusal message |
|----|----------|-----------------|
| `format_version` | `format_version == 1` (checked first; nothing else is evaluated on mismatch) | unsupported raw format_version |
| `generated_floor` | scenario `sustained-restart` ⇒ `generated >= 1,000,000` | sustained-restart generated fewer than 1,000,000 events |
| `generated_accounting` | `generated == accepted_unique + rejected_4xx + unaccepted_final` | generated events do not equal accepted + rejected + unaccepted |
| `schedule_sum` | `generated == Σ requests.event_count` | generated events do not match the recorded request schedule |
| `kafka_records` | `kafka.events_records_delta >= accepted_unique` | Kafka holds fewer records than accepted events |
| `ch_unique` | `clickhouse.unique == accepted_unique` (loss = 0) | ClickHouse unique events differ from accepted events |
| `ch_physical` | `clickhouse.physical >= clickhouse.unique` | ClickHouse physical rows below unique rows |
| `id_digest` | `clickhouse.id_digest == client.accepted_id_digest` | ClickHouse event-id digest differs from accepted-id digest |
| `rule_pg:<RULE>` | `postgres.alerts_by_rule[RULE] == attack_plan.rules[RULE].expected` (over the union of rule IDs; missing = 0) | PostgreSQL alerts differ from expected hits |
| `rule_ch:<RULE>` | `clickhouse.alerts_unique_by_rule[RULE] == postgres.alerts_by_rule[RULE]` | ClickHouse alerts differ from PostgreSQL alerts |
| `benign_hits` | `postgres.benign_hits == 0` | benign traffic produced alerts |
| `dlq_delta` | `dlq_total_after − dlq_total_before == expected_dlq` | DLQ delta differs from expected poison count |
| `late_delta` | `late_total_after − late_total_before == expected_late` | late-event delta differs from expected late count |
| `detection_histogram` | bucket deltas are non-negative and cumulative counts are non-decreasing in `le` | detection delay histogram reset or non-cumulative |
| `restart_missing` | scenario `sustained-restart` ⇒ `restart != null` | sustained-restart has no recorded detector kill |
| `recovery_baseline` | at least one detector lag sample in the baseline window before the kill | no pre-kill lag baseline samples |
| `recovery_timeout` | recovery reached with `t_recovered − kill_ns <= timeout_s` | detector lag did not recover within the timeout |

## Derived values (all recomputed from the raw file)

- **Percentiles** are exact nearest-rank over sorted samples: `rank = ceil(q·n/100)`,
  value = `sorted[rank − 1]`.
- **Ingest latency** per request: `done − intended` (coordinated-omission corrected) and
  `done − sent` (service time), in ms, p50/p95/p99, batch size stated.
- **Schedule slip** per request: `sent − intended`. Slip p99 > 1000 ms adds
  "generator saturated" to Limitations.
- **Detection delay**: Prometheus-style `histogram_quantile` over the bucket deltas with
  linear interpolation inside the bucket; the report states the bucket bounds used as the
  resolution.
- **SSE notification latency**: `received − trigger_intended`, first arrival per alert.
  This is an upper bound on persistence latency: there is no wall-clock persistence
  column, and PG persistence precedes SSE delivery.
- **p99 sample floor**: p99 of detection delay and SSE latency is reported only when
  n >= 100, otherwise `n/a (n=N)`.
- **Sustained accepted rate**: accepted events summed per complete 10 s window of
  `done_ns`, anchored at the first `intended_ns`; p50/p95 over windows. The offered rate
  uses the same windows over `intended_ns` and `event_count`.
- **Recovery** (Pattern 11): baseline = nearest-rank p95 of the per-sample max detector
  lag (`group == "detector"`) in `[kill − baseline_window_s, kill)`. `t_recovered` is the
  first sample time at or after the kill that starts a run of samples all `<= baseline`
  spanning `stable_window_s` seconds; a gap between consecutive samples larger than half
  the stable window breaks the run. `recovery_seconds = (t_recovered − kill_ns) / 1e9`.
- **Detector state %**: `detector_state_peak / config.detector_limits` per dimension.
