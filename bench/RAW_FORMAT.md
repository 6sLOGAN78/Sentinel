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

`--supersede` (05-28, `make bench ... SUPERSEDE=1`): when `<date>-<scenario>.md` and
`.json` already exist and the existing JSON's `run.run_id` differs from the new run's,
both files are moved byte-for-byte (`Path.replace`, never edited) to
`superseded/<date>-<scenario>-<old run_id>.{md,json}` under the output directory (created
0755), and the new pair keeps the BENCH-03 name. The same run id, a lone `.md` without
its `.json`, an unreadable old JSON or an existing superseded target still exit 3 with
nothing moved.

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
| `preflight` | object, optional | 05-04 predictions the harness checked before sending (see below) |

### Optional keys (05-18)

format_version stays `1`: every key in this section is optional, and its type is checked
when present. Keys marked *overload* are required when `run.scenario` is `overload`; a
missing one refuses with `overload_recorded`. None of them is listed as an unknown key.

| Key | Type | Meaning |
|-----|------|---------|
| `run.drained` | bool | every consumer group reached lag 0 within the 900 s drain deadline. `false` refuses (`drained`) for every scenario; absence adds a Limitations line |
| `run.warmup` | `{partition_share_max: num, rate_eps: num}` | warm-up skew measurement: max per-partition share of the events-topic end-offset delta, and the measured warm-up event rate |
| `client.status_counts` | `{status: int}` *overload* | count of final request statuses (status `0` = no HTTP response). Must equal the counts recomputed from `client.requests` (`status_counts`) |
| `prometheus.producer_buffered_max` | int *overload* | `max(max_over_time(sentinel_kafka_producer_buffered_records[run]))` |
| `prometheus.producer_buffered_limit` | int *overload* | `max(sentinel_kafka_producer_buffered_records_limit)` |
| `prometheus.ingest_requests_by_status` | `{status: int}` | reset-corrected increase of `sentinel_ingest_requests_total` per `status` over the run (alert threshold input) |
| `prometheus.pin_retention_margin_min` | int \| null | `min(min_over_time(sentinel_detector_pin_retention_margin_offsets[run]))`; null when the gauge had no samples (alert threshold input) |
| `resources.oom_killed` | `{container: bool}` *overload* | `docker inspect` `State.OOMKilled` per project container at the end of the run |
| `resources.restart_delta` | `{service: int}` *overload* | increase of `docker inspect` `RestartCount` summed per compose service over the run |
| `prometheus.lag_samples[].partitions_reported` | int | 05-27: distinct events-topic partitions with a live `sentinel_kafka_consumer_lag` series for the group at `t_ns` (0 when the group reports none). Required on every detector sample for `sustained-restart` (`recovery_coverage_unknown`) |
| `restart.partitions_expected` | int > 0 | 05-27: events-topic partition count measured from `kafka-get-offsets` before the run (not assumed from config). Required for `sustained-restart` (`recovery_coverage_unknown`) |
| `restart.restarted_ns` | int >= `kill_ns` | 05-28: when the harness ran `docker start` on the killed detector (after live recovery or the recovery timeout). When present, recovery and the coverage figures use only samples before it |
| `restart.rejoined_ns` | int \| null | 05-28: when the restarted detector was running (and healthy, if it has a healthcheck) and the newest detector lag sample reported every events partition; null when that did not happen within 180 s (`RESTART_REJOIN_TIMEOUT_S`), which adds a Limitations line |
| `run.replicas_running` | `{start: {service: int}, end: {service: int}}` | 05-28: running containers per compose service (`docker ps` on the project labels, which lists running containers only) before the load and at collection. `start.detector < config.replicas.detector` refuses (`replicas_degraded`) for every scenario; absence adds no refusal (older raws) |
| `kafka.main_load` | `{start_ns: int, end_ns: int, partition_delta: {partition: int}}` | 05-28: per-partition events-topic end-offset delta over the main load (sustained-restart: from the end of the warm-up to the sender finishing; overload: the load phase). `end_ns > start_ns` is required when present. Threshold input `partition_rate_eps_max` |
| `prometheus.pin_retention_margin_steady_min` | int \| null | 05-28: `min(min_over_time(sentinel_detector_pin_retention_margin_offsets[w]))` at collection, where `w` is the main load to collection (sustained-restart) or the scenario window after its first 10 s (overload). Excludes the fresh-topic start, where the whole-run minimum is 0 |
| `preflight` | object | `static` / `recheck` (05-04 `capacity_preflight` output before the run and with the measured warm-up share), `benign_maxima` (`benign_threshold_check`), and for overload `overload{accept_bound_events, ...}`. Reported verbatim; no equation reads it |

### `run`

`run_id`, `scenario` (`sustained-restart` \| `overload` \| `api-latency`), `tenant`
(`bench-<run_id>`), `started_at`, `ended_at` (RFC 3339 UTC), `git_sha`, `image_tag`,
`compose_project`. Optional: `drained`, `warmup`, `replicas_running` (see above).

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

`events_records_delta`: sum of end-offset deltas on the fresh events topic. Optional
`main_load` (05-28, see above).

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
| `lag_samples[]` | `{t_ns, group, max_lag, partitions_reported}` position-based consumer lag samples; `partitions_reported` is optional in general and required for `sustained-restart` (05-27) |

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

`{kill_ns, killed_container, baseline_window_s, stable_window_s: 30, timeout_s,
partitions_expected, restarted_ns?, rejoined_ns?}` or `null`. Required (non-null) for scenario
`sustained-restart`, and for that scenario `partitions_expected` is required too (05-27).

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
| `recovery_coverage_unknown` | scenario `sustained-restart` (or any raw with `restart.partitions_expected`) ⇒ `restart.partitions_expected` present and every detector `lag_samples[]` entry has `partitions_reported` | partition coverage is not recorded (restart.partitions_expected or prometheus.lag_samples[].partitions_reported missing), so recovery cannot count partitions orphaned by the kill |
| `recovery_coverage_baseline` | every detector sample in `[kill − baseline_window_s, kill)` has `partitions_reported == partitions_expected` | pre-kill baseline samples do not report every events partition |
| `recovery_timeout` | recovery reached with `t_recovered − kill_ns <= timeout_s` (and before `restarted_ns` when present) | detector lag did not recover with full partition coverage within the timeout [and before the killed detector restarted] |
| `replicas_degraded` | `run.replicas_running` present ⇒ `replicas_running.start.detector >= config.replicas.detector` (missing = 0), every scenario | the scenario started with fewer running detectors than configured |
| `api_non_2xx` | scenario `sustained-restart` ⇒ every `api.samples[].status` is 2xx | API phase returned non-2xx responses: `<endpoint> <n> of <total>`, ... (observed `{endpoint: n}`) |
| `drained` | `run.drained` is not `false` | consumer lag did not drain to 0 before collection |
| `status_counts` | `client.status_counts` (when present) equals the counts of `client.requests[].status` | recorded status counts differ from the request records |
| `overload_recorded` | scenario `overload` ⇒ every *overload* key is present | overload measurements are missing |
| `overload_status` | scenario `overload` ⇒ every request status ∈ {202, 429, 503} | overload produced a status other than 202, 429 or 503 |
| `overload_buffer` | scenario `overload` ⇒ `producer_buffered_max <= producer_buffered_limit` | producer buffer exceeded its limit |
| `overload_oom` | scenario `overload` ⇒ no `resources.oom_killed` value is `true` | a container was OOM-killed under overload |
| `overload_ingest_restart` | scenario `overload` ⇒ `resources.restart_delta.ingest == 0` | an ingest container restarted under overload |

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
  first sample time at or after the kill that starts a run of samples spanning
  `stable_window_s` seconds in which every sample is `<= baseline` **and** reports every
  events partition (`partitions_reported == restart.partitions_expected`, 05-27). An
  incomplete sample breaks the run like an over-baseline one, and so does a gap between
  consecutive samples larger than half the stable window. When `restart.restarted_ns` is
  present only samples before it count, so recovery must be observed before the restart.
  `recovery_seconds = (t_recovered − kill_ns) / 1e9`. Max lag alone is not enough: the
  killed member's partitions have no lag reporter until the session-timeout rebalance,
  which is how run 20261119a showed a 0.438 s artifact. The search is
  `report.recovery_from_samples`, reused live by the harness.
- **Coverage after the kill** (from samples in `[kill_ns, restarted_ns)`, or all samples
  after the kill when `restarted_ns` is absent, so the restart's own expected drop is
  excluded): `min_partitions_reported_after_kill`, and `coverage_lost_s` = first
  incomplete sample to the first complete one after it (0 if coverage never dropped,
  n/a if it never came back). If coverage never dropped, Limitations says the killed
  member may have owned no partitions or the drop was shorter than the 5 s interval.
  A raw without coverage data in another scenario keeps the lag-only definition and says
  so in Limitations.
- **Detector state %**: `detector_state_peak / config.detector_limits` per dimension.
- **Alert threshold inputs** (05-13 placeholders, tuned in 05-20): the report copies
  `ingest_requests_by_status`, the derived 5xx ratio (`Σ 5xx / Σ all`), the
  `pin_retention_margin_min` (whole run, including the fresh-topic start) and the warm-up
  partition share into `threshold_inputs`. 05-28 adds `pin_retention_margin_steady_min`
  (copied, null when absent) and `partition_rate_eps_max =
  max(kafka.main_load.partition_delta) / ((end_ns − start_ns) / 1e9)` (null without
  `kafka.main_load`).
- **Exercised shed paths** (overload, 05-28): `shed_paths{429, 503}` is true when the
  recomputed status counts contain that status. When 503 is 0 the report names
  `TestChaosOverloadBounded` (`backend/test/chaos_overload_test.go`) and
  `TestChaosKafkaBrokerRestart` (`backend/test/chaos_broker_test.go`) as the
  deterministic proofs of the 503 paths. No 503 number is invented.
- **Restart of the killed detector** (05-28): `docker kill` bypasses the `unless-stopped`
  restart policy, so the harness polls `report.recovery_from_samples` (same baseline and
  windows as the report) every 5 s after the kill. When it reports recovery, or
  `timeout_s` passes, the harness runs `verify_bench_container` on the recorded killed id
  and only then `docker start <id>`, records `restarted_ns` strictly after the sample that
  confirmed recovery, and waits up to 180 s for `rejoined_ns`.

## Collection (05-18 harness)

All counts are read-only and scoped to the run tenant through parameters, never string
formatting. The ClickHouse digest expression is checked against `rawlog.id_digest` on a
3-ID literal before any count is taken (verified on ClickHouse 26.3.47.6 on 2026-11-17:
the 3 fixed IDs give 11115952024367422501 = `9a43cb78a8b45c25` on both sides).

| Source | Command |
|--------|---------|
| ClickHouse events | `exec -T clickhouse clickhouse-client --param_tenant=<t> --query "SELECT uniqExact(event_id), count(), (SELECT groupBitXor(...) FROM (SELECT DISTINCT event_id FROM sentinel.events WHERE tenant_id = {tenant:String})) FROM sentinel.events WHERE tenant_id = {tenant:String}"` |
| ClickHouse alerts | `SELECT rule_id, uniqExact(alert_id) FROM sentinel.alerts WHERE tenant_id = {tenant:String} GROUP BY rule_id` |
| PostgreSQL alerts | `exec -T postgres psql -X -A -t -F <tab> -v ON_ERROR_STOP=1 -U sentinel -d sentinel -c '\bind <t>' -c 'SELECT rule_id, count(*) FROM alert_workflows WHERE tenant_id = $1 GROUP BY rule_id ORDER BY rule_id'` (psql never interpolates `:'var'` inside `-c`, so the tenant is a bound parameter) |
| Benign hits | same binding, `SELECT rule_id, entity_key ... LIMIT 10000`; an alert counts as benign when the source IP before the first `|` is not an attack-plan source |
| Kafka | `exec -T kafka /opt/kafka/bin/kafka-get-offsets.sh --bootstrap-server kafka:9092 --topic security.events.v1 --time -1`, per-partition end offsets before and after |
| Lag | Prometheus `max by (group) (sentinel_kafka_consumer_lag)` every 5 s; group `sentinel-<name>` is recorded as `<name>`. Not topic-restricted, because `alert-writer` consumes the alerts topic and the drain check needs its lag |
| Coverage | same poll: `count by (group) (count by (group, partition) (sentinel_kafka_consumer_lag{topic="security.events.v1"}))` merged into `partitions_reported` (0 for a group absent from the result) |
| Counters | raw range-vector samples over the run plus 15 s; increase = positive deltas with reset correction; a series first seen after the start (restarted replica) counts from 0 |
| Resources | `docker stats --no-stream` every 10 s on the containers labelled with the project; `docker inspect` before and after for RestartCount and OOMKilled |
