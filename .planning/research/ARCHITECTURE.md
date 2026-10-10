# Architecture Research

**Domain:** Kafka-based real-time security event ingestion, detection and alerting (Go workers, ClickHouse + PostgreSQL)
**Researched:** 2026-10-10
**Confidence:** MEDIUM-HIGH. The franz-go lifecycle claims are verified against its docs (Context7). The ClickHouse dedup behavior and the stream-processing designs come from established practice and from reasoning about the spec's constraints. Items marked "benchmark" must be measured (spec §7.2, §18), not assumed.

---

## 1. Recommended Architecture

Five Go binaries from one module. Kafka is the only inter-service transport. Postgres is the only coordination point between the writers and the query API.

```
 generator (Python) ──HTTPS──> [ingest] ──(ProduceSync, acks=all, idempotent)──> security.events.v1
                                 │  Redis token bucket                          key = tenant|source_ip
                                 │  Postgres api_keys (hashed)                         │
                                 │                                  ┌──────────────────┴───────────────┐
                                 │                          group: sentinel-detector            group: sentinel-event-writer
                                 │                                  │                                  │
                                 │                           [detector]                          [event-writer]
                                 │                   per-partition goroutine owns state          batch -> CH INSERT
                                 │                   in-memory windows, replay-rebuilt           commit after insert
                                 │                                  │ ProduceSync                      │
                                 │                                  v                                  v
                                 │                        security.alerts.v1               ClickHouse: events
                                 │                          key = tenant|alert_id           (ReplacingMergeTree)
                                 │                                  │
                                 │                       group: sentinel-alert-writer
                                 │                                  v
                                 │                           [alert-writer]
                                 │              1) CH INSERT alerts   2) PG txn: workflow row ON CONFLICT DO NOTHING
                                 │                 + pg_notify only if row newly inserted   3) commit offset
                                 │                                  │
                                 │          ClickHouse: alerts      Postgres: alert_workflow, audit, api_keys
                                 │                      └────────┬─────────┘
                                 │                               v
                                 │                         [query-api]  REST + SSE
                                 │                 app-level join; LISTEN sentinel_alerts -> SSE hub
                                 │                               v
                                 └───────────────────────── React dashboard
 Any consumer ──poison/undecodable──> security.events.dlq.v1 (then commit)
```

### Component Boundaries

| Component | Responsibility | Reads | Writes | Must NOT |
|-----------|---------------|-------|--------|----------|
| `ingest` | AuthN (API key -> tenant), strict validation, Redis rate limit, produce to Kafka, return 202 only after ack | Postgres (api_keys, cached), Redis | Kafka events topic | Touch ClickHouse. Trust a tenant from the body. |
| `detector` | Windowed event-time rule evaluation, alert emission, state rebuild by replay | Kafka events | Kafka alerts (and DLQ) | Write to any DB. Hold durable state. Use wall-clock for windows. |
| `event-writer` | Batch events into ClickHouse | Kafka events | ClickHouse events (and DLQ) | Do detection or enrichment. |
| `alert-writer` | Persist alerts to ClickHouse, create the workflow row, emit the notification | Kafka alerts | ClickHouse alerts, Postgres workflow, NOTIFY | Mutate status of existing rows (it only inserts). |
| `query-api` | Tenant-scoped REST, triage PATCH + audit, SSE fan-out, rule metadata, metrics summary | ClickHouse, Postgres (LISTEN) | Postgres (status, audit) | Consume Kafka. Run arbitrary SQL. |
| `internal/detection` (library) | Pure rule engine: `Process(event) -> []Alert`, watermark, eviction | none (no I/O) | none | Import Kafka, DB or time.Now(). |

Each of the three consumers (detector, event-writer, alert-writer) uses a separate consumer group. The detector and event-writer both read the events topic independently, which is the fan-out in spec §3. A slow ClickHouse never slows detection.

### Data Flow (direction is always left to right, nothing flows back)

1. `ingest` -> `security.events.v1` (key `tenant|source_ip`, so one source IP's events stay in order on one partition).
2. `security.events.v1` -> `detector` -> `security.alerts.v1`.
3. `security.events.v1` -> `event-writer` -> ClickHouse `events`.
4. `security.alerts.v1` -> `alert-writer` -> ClickHouse `alerts` -> Postgres `alert_workflow` + `pg_notify`.
5. ClickHouse + Postgres -> `query-api` -> browser. Postgres NOTIFY -> `query-api` SSE hub -> browser.

---

## 2. Streaming Detection Design

### 2.1 Partition ownership and state

- Use franz-go (`github.com/twmb/franz-go`) with the default cooperative-sticky balancer. Verified in its docs: the client manages the group independently of consuming, so a revoke can happen at any time.
- **One goroutine per assigned partition**, started in `OnPartitionsAssigned` and stopped and awaited in `OnPartitionsRevoked`. This is the pattern in franz-go's `goroutine_per_partition_consuming` examples. Each goroutine exclusively owns one `PartitionState`, so there are no locks on window state. Ownership is enforced by the Kafka group, not by a mutex.
- Use `kgo.BlockRebalanceOnPoll()` + `PollRecords(ctx, N)` + `AllowRebalance()`. A rebalance cannot start while a batch is in flight. Keep processing short relative to the rebalance timeout (franz-go warns about this explicitly).
- Key invariant: rules group by `tenant + source_ip (+ target_user | dest_ip)`. The partition key `tenant|source_ip` is a prefix of every rule's grouping key, so every window's events land on one partition. **No repartitioning topic is needed for the four v1 rules.** A future rule grouped by something else (for example, by destination) requires a repartition topic. Document this in an ADR.
- Never change the events topic's partition count while detector state matters. Key->partition mapping changes and windows split. Treat it as a reset and document it.

### 2.2 Window semantics (choose order-independent math)

Recommended: **tumbling windows aligned to the epoch**, `window_start = floor(occurred_at / size) * size`. Reasons:

- Counting into buckets is commutative and idempotent when each event is deduplicated by `event_id`. The same set of events gives the same result in any arrival order inside the lateness bound. That makes replay deterministic.
- It matches the spec's deterministic `dedupe_key`, which includes the window start (§6.2).
- Cost: a burst that straddles a boundary can be split and missed. Document this as a known limitation with a test. If it is unacceptable, use hopping windows (slide = size/5) with the dedupe key = rule + entity + hop start. This yields more alerts and needs a cooldown.

Per-key window state (bounded by design):
- `AUTH_BRUTE_FORCE`, `REQUEST_FLOOD`, `DENY_SPIKE`: a set of `event_id`s until the threshold is hit, so the set size is at most the threshold. After firing, keep only `{fired: true, count}`.
- `PORT_SCAN`: a set of distinct ports (at most 65,535, in practice at most the threshold before firing), plus the event IDs for samples.
- Samples: the first 20 event IDs by arrival, from a bounded array.
- Hard cap on live keys per partition (config). When exceeded, drop new keys, increment `sentinel_detection_state_dropped_total`, and fire an operator alert. Do not grow unbounded (spec §11.1).

**Fire on threshold cross, not on window close.** Emitting at close adds window size + lateness (more than 5 min for brute force) to detection delay and makes the live demo poor. Firing on crossing gives low latency. The `alert_id` depends only on the dedupe key (not on content), so the first-N samples differing across replays is harmless: one row wins after dedup. `event_count` at fire time equals the threshold. Trade-off to document: evidence is a snapshot at fire time, not the final window count.

**Cooldown** is stored in per-`(rule, entity)` state as `last_alert_window_end` in event time. It is a deterministic function of replayed events.

### 2.3 Watermarks and lateness

- Watermark is **per partition**: `wm = max(occurred_at seen) - allowed_lateness` (default 30s). Per-partition is correct because a key never spans partitions.
- An event is **accepted** if its window end > `wm`. Otherwise it is **late**: drop and increment `sentinel_detection_late_events_total{rule_id}`. Do not reprocess. This is the deterministic "drop-with-metric" policy and it must be tested at the exact boundary (`==wm`, `wm-1ms`).
- Windows with `window_end + lateness <= wm` are evicted. Cooldown entries are evicted at `last_alert_window_end + cooldown`.
- **Watermark poisoning:** one event with `occurred_at` far in the future would jump the watermark and drop all legitimate traffic as late. Defenses: ingest rejects `occurred_at > received_at + skew`. In addition, the detector advances `wm` only from events with `occurred_at <= kafka_record_timestamp + skew`; otherwise treat the event as invalid (DLQ or metric).
- **Idle partitions:** the watermark stalls and state is not evicted. Because alerts fire on threshold cross, this affects only memory and the offset pin (see 2.5). Do not advance by wall-clock: synthetic generators produce historical timestamps and wall-clock advancement would make late-drop decisions nondeterministic. Expose a gauge for state size.

### 2.4 Rebuild versus persist: rebuild by replay

Recommended for v1: **in-memory state, rebuilt by replaying Kafka. Do not persist.** State is a pure function of the partition prefix, so it is derivable, and persisting it creates a new problem (snapshot atomic with offset). It also fits the spec's stated acceptable approach (§5.1) and the rule that Redis must not be the sole source (§7.4).

Mechanism that makes replay automatic: **the detector commits a "state pin" offset, not the processed offset.**

```
commit_offset(partition) = min(
    offset of the oldest record still needed to rebuild open state,
    next offset after the last record whose alerts were produce-acked)
```

Needed = records with `occurred_at >= wm - horizon`, where `horizon = max over rules(window + lateness + cooldown)`. Implement as a per-partition deque of `(offset, occurred_at)`, pruned from the front while `occurred_at < wm - horizon`. Pruning only from the front is conservative with out-of-order data.

Effect: on restart, rebalance or a crash, the new owner resumes at the committed pin and re-reads at most `horizon` of event time. State is rebuilt as a side effect of normal processing. There is no seek-back logic, no timestamp lookup, and no snapshot.

Costs and rules to record in an ADR:
- Restart catch-up is proportional to `horizon x partition rate`. Measure it in the benchmark (Phase 5).
- Events topic retention must exceed `horizon` plus the longest tolerated outage. The default 24h is fine.
- Replay re-emits alerts for already-alerted windows. This is expected and safe (see 3).
- Committed-offset lag for the detector group is **permanently inflated** by roughly the horizon. Do not alert on committed lag for this group. Export position-based lag instead: `FetchPartition.HighWatermark - last_processed_offset`. This is a genuine pitfall for `sentinel_kafka_consumer_lag` (see PITFALLS in the summary).
- On `OnPartitionsLost` (session timeout, fenced): drop state, do not commit.
- On `OnPartitionsRevoked`: stop the partition goroutine, commit the pin once synchronously, drop state.

When to revisit: if `horizon x rate` makes restart too slow, add snapshots of `(state, offset)` to a local file or Postgres keyed by `(partition, offset)`. Not needed for v1.

### 2.5 Ordering of effects and crash analysis

Per poll batch, per partition, the loop is:

```
1. for each record: decode (bad -> DLQ produce, treat as processed)   [pure]
2. engine.Process(event) -> alerts; update in-memory state            [memory]
3. ProduceSync(alerts) to security.alerts.v1 (idempotent, acks=all)   [external effect]
4. compute pin offset; CommitOffsetsSync(map{topic:{partition: EpochOffset{epoch, pin}}})
5. AllowRebalance()
```

Use `kgo.DisableAutoCommit()` and commit explicitly. A franz-go commit takes the next offset (record offset + 1 semantics; `CommitRecords` adds 1 internally), so compute the pin as an explicit next-offset.

| Crash point | State after crash | Recovery result |
|-------------|-------------------|-----------------|
| After step 2, before 3 | Memory lost. No alert published. Pin not advanced. | Replay from the old pin rebuilds state and re-fires. The alert is published once. |
| During 3 (some of the alerts produced) | Partial alerts in topic. Pin not advanced. | Replay re-fires all. Re-produced alerts are duplicates with identical `alert_id`. Safe by 3.1. |
| After 3, before 4 | Alerts in topic. Pin not advanced. | Replay re-fires the same `alert_id`s. Duplicates are collapsed downstream. |
| After 4 | Alerts acked, pin advanced. | Later events for the same window rebuild partial state. If a re-cross occurs, it re-fires the same `alert_id`. Safe. |

Invariants (each needs a test, spec §5.1 and §15 require them):
1. Never commit past a record whose alert was not produce-acked.
2. Never commit past a record still needed to rebuild open or cooldown state.
3. Alert content that matters for identity is a pure function of the dedupe key.

To test crash points deterministically, add a tiny **failpoint** hook (`FAILPOINT=detector.before_publish|after_publish|before_commit`, compiled in via an interface or build tag) in Phase 3. Killing a container at random does not reliably hit the window between steps.

### 2.6 Idempotent alert publishing

- `dedupe_key = tenant|rule_id|entity_key|window_start(RFC3339 UTC)` in one canonical function in `internal/contracts`. `alert_id = hex(sha256(dedupe_key))`. Both detector tests and query tests use that single function. Do not include `rule_version` in the key (spec example excludes it). Decide explicitly what a rule version change means for replay, in the ADR.
- **`created_at` must be deterministic.** Set it from the triggering event's `occurred_at` (or the window end), never `time.Now()`. If it were wall-clock, replay would produce rows that differ in `created_at`. Since `created_at` is in the ClickHouse sort key and the cursor `(created_at, alert_id)`, those rows would not dedupe and pagination would show duplicates. Keep wall-clock time in a separate non-key column (`detected_at`) if needed for the delay metric, and do not display or sort by it.
- Use franz-go's idempotent producer (on by default) with `acks=all`. That protects against producer-retry duplicates only. It does not protect against replay after a crash, which is why downstream idempotency (ReplacingMergeTree + `ON CONFLICT DO NOTHING`) is mandatory.
- Do not use Kafka transactions. They would give atomic consume-produce-commit, but the pin-offset design intentionally commits a different offset than the one processed, and `read_committed` consumers add complexity. The spec states at-least-once (§3).

---

## 3. Storage Design

### 3.1 ClickHouse duplicate handling

| Option | Verdict | Why |
|--------|---------|-----|
| ReplacingMergeTree with `ORDER BY` containing the full identity | **Use for storage** | Convergent. Duplicates that collapse at merge time need no application logic. Merges are asynchronous, so never rely on them alone. |
| `FINAL` at query time | Use selectively | Correct before merges. Recent ClickHouse versions have improved FINAL a lot, but the cost is data-dependent. **Benchmark** (spec requires it). Acceptable for small bounded lookups and rollups. |
| `LIMIT 1 BY` on the sort key | **Use for paginated list queries** | Rows are sorted by `(tenant_id, occurred_at, event_id)`, so duplicates are adjacent and the dedup is cheap. Correct before merges. |
| `argMax(col, version)` | Not needed | Only for rows that change. Both tables here are immutable, and mutable status lives in Postgres. Adding versions invents a problem. |
| `insert_deduplication_token` per batch | Optional, never sufficient | Only dedups identical retried blocks (on non-replicated MergeTree it needs `non_replicated_deduplication_window`). After a crash the batch boundaries differ, so it does not deduplicate. |

Concrete shape:
- `events`: `ENGINE = ReplacingMergeTree`, `PARTITION BY toYYYYMM(occurred_at)`, `ORDER BY (tenant_id, occurred_at, event_id)`. Duplicates share `occurred_at`, so they share the partition and sort key and can collapse. Lists use `... WHERE tenant_id=? AND occurred_at BETWEEN ? AND ? ORDER BY occurred_at, event_id LIMIT 1 BY tenant_id, event_id LIMIT n+1`.
- `alerts`: `ReplacingMergeTree`, `ORDER BY (tenant_id, created_at, alert_id)`. This requires the deterministic `created_at` above.
- Point lookup by `event_id` (not in the sort prefix): scan is bounded by tenant + time range. Require or derive a time range, or add a skip index. Do not scan whole tables.
- Counts and the metrics summary: `uniqExact(event_id)` over a capped range, or FINAL. Pick by benchmark. Document that numbers are exact only for the deduped read path.
- Event-writer: `PrepareBatch` / native batch inserts of 5k-50k rows or 1s, whichever first. Never single-row inserts. Avoid `async_insert` for correctness tests, because acknowledgement semantics are less obvious than a synchronous batch (MEDIUM confidence).

### 3.2 Postgres workflow and the application-level join

Tables: `alert_workflow(tenant_id, alert_id, status, assignee, version, updated_at, seq bigserial, created_at, severity, rule_id, PRIMARY KEY(tenant_id, alert_id))` and `alert_audit(...)`.

**Alert-writer order: ClickHouse first, then Postgres, then commit.**

```
CH INSERT (dups collapse)  ->  PG txn { INSERT workflow ... ON CONFLICT DO NOTHING RETURNING seq;
                                          if inserted: pg_notify('sentinel_alerts', payload) }  ->  commit offset
```

- Any crash replays the batch. CH duplicates collapse. PG conflicts are no-ops and emit no second notification. That gives deduplicated SSE for free.
- Because CH is written first, any notification points at an alert that is already readable. Writing PG first would let SSE clients hydrate an alert not yet in CH.
- The unavoidable gap: an alert visible in CH before its PG row exists. The API handles it: the join is a LEFT JOIN, and a missing row means `status=open, version=0`. PATCH on a missing row performs `INSERT ... ON CONFLICT DO NOTHING` after verifying the alert exists in CH for that tenant, then applies the version-checked update. This is the "defined behavior if detection arrives before or after the workflow row" that spec §7.3 asks for.

**Join mechanics** (application-level, spec §7.3):
1. Query ClickHouse for a page of alerts (tenant, filters, cursor `(created_at, alert_id)`, limit n+1).
2. `SELECT ... FROM alert_workflow WHERE tenant_id=$1 AND alert_id = ANY($2)`.
3. Merge in memory. Missing -> defaults.

**Pitfall: filtering by status.** Status lives in Postgres, pagination in ClickHouse. Filtering after paging returns short or empty pages. Recommendation (decide in Phase 4): denormalize the immutable `created_at, severity, rule_id` into `alert_workflow` and **drive status-filtered lists from Postgres**, hydrating evidence from CH. All other list filters drive from CH. Two code paths, but both correct and paginated.

---

## 4. SSE Fan-out: use Postgres LISTEN/NOTIFY, not a Kafka consumer in query-api

| Criterion | Kafka consumer in query-api | LISTEN/NOTIFY from alert-writer |
|-----------|------------------------------|---------------------------------|
| Emits before data is queryable | **Yes**: stream sees the alert before alert-writer persists it, so detail fetch and backfill can 404 | No: emitted after the CH insert and PG commit |
| Replay duplicates | Reach the stream; need an LRU dedupe | Suppressed: notify only when the workflow row was newly inserted |
| Each replica gets every alert | Needs a per-instance, group-less consumer (no commits) | Each replica holds one dedicated `LISTEN` connection |
| Extra dependency for query-api | Kafka client, ACLs | None (Postgres is already required) |
| Delivery | Replayable by offset | At-most-once. Lost when the connection drops |
| Scale limit | High | NOTIFY serializes commits; fine for low-volume alerts |

Design:
- Payload is small: `{seq, tenant_id, alert_id, severity}`. Postgres caps payloads at 8000 bytes. The client fetches details over REST.
- SSE `id:` is the `seq` of the workflow row (insertion order), **not** `(created_at, alert_id)`. `created_at` is event-time, so a late alert has an older timestamp than ones already delivered, and a time-based `Last-Event-ID` would miss it.
- Reconnect: `Last-Event-ID: seq` -> backfill `WHERE tenant_id=$1 AND seq > $last ORDER BY seq` from Postgres, then join the live hub. Sequence values can commit out of order, so backfill with a small overlap and let the client dedupe by `alert_id`. Do not claim gap-free delivery.
- Hub: per replica, one `LISTEN` goroutine (reconnect with backoff; on reconnect, clients must backfill) and a broadcaster. Each client has a **bounded buffered channel**. If full, close that client, who reconnects with `Last-Event-ID`. Filter by the client's tenant at fan-out. Send keep-alive comments. `sentinel_sse_clients` gauge.
- Auth via `Authorization` header (use `fetch`-based SSE on the web side, since `EventSource` cannot set headers). Never put tokens in the URL (spec §8).
- Note: this makes `query-api` independent of Kafka. Readiness checks only CH and PG.

---

## 5. Backpressure

Principle: every queue is bounded and the overflow behavior is explicit. In a pull-based pipeline the cheapest backpressure is to stop polling.

| Stage | Mechanism |
|-------|-----------|
| HTTP -> ingest | In-flight request semaphore + `MaxBufferedRecords`/`MaxBufferedBytes` on the producer. `ProduceSync` under a context deadline. Overflow or timeout -> `503` (+ `Retry-After`). Redis bucket -> `429`. Redis down -> configured fail-open or fail-closed, logged and metered. |
| Partial batch failure | A 500-event batch is many Kafka records. A failure midway leaves some published. The API is "atomic" in response only. Clients must send a stable `event_id` (require it, or derive it deterministically), so a retry creates duplicates that the writers collapse. Say this explicitly in the contract. |
| Kafka -> event-writer | Single loop: poll -> build batch -> insert -> commit. While ClickHouse is slow or down, the loop is blocked in retry (exponential backoff + jitter, bounded), does not poll, and Kafka holds the backlog. Cap batch size. Do not buffer unboundedly in a channel. |
| Kafka -> detector | Bounded channel per partition goroutine. If full, `PauseFetchPartitions` for that partition (or block the poll loop for simplicity at first) so one hot partition does not exhaust memory. State is capped per partition (2.2). |
| detector -> alerts topic | `ProduceSync` per batch. A Kafka outage blocks the batch and so blocks the commit. Correct. |
| alert-writer | Same loop shape as event-writer. |
| query-api SSE | Bounded per-client channel and drop-and-reconnect (4). |
| Poison messages | DLQ produce, then commit. A DLQ failure blocks the partition. Never skip silently. |
| Shutdown | Cancel context, stop polling, finish or abort the in-flight batch within a deadline, commit completed offsets only (franz-go: perform a blocking commit before leaving). |

---

## 6. Repository Layout and Module Decision

### Verdict: keep `backend/` and put binaries in `backend/cmd/*`. Do not move to the spec's root-level `cmd/`.

Reasons:
1. The Go module already lives in `backend/` (`github.com/6sLOGAN78/sentinel`), sibling to a Turborepo/bun setup (`packages/*`). A root `go.mod` would mix Go module resolution into the JS workspace root and require moving all of `backend/internal`, `go.sum`, taskfile and templates for no functional gain. Spec §19.4 explicitly allows the simplest consistent choice if recorded in `docs/decisions/`.
2. The spec's layout is a sketch of responsibilities (§13), not an interface. Nothing external depends on the path.
3. Go's `internal/` rule means only code under the module's parent of `internal` can import it. Putting integration and chaos tests at the repo root (`tests/integration`) would fail to compile if they import `internal/*`. Place Go tests in `backend/test/integration` and `backend/test/chaos` (build-tagged). Keep root `tests/` only for language-neutral contract fixtures.
4. `go:embed` cannot reference `..`. The canonical JSON schemas and example fixtures should live inside the module (`backend/internal/contracts/schema/`) and be read by the generator (Python) and web tests by relative path. Otherwise the repo ends up with two copies that drift.

Mapping:

```
sentinel/
├── backend/                          # Go module (existing)
│   ├── cmd/{ingest,detector,event-writer,alert-writer,query-api}/main.go   # replaces cmd/sentinel
│   ├── internal/
│   │   ├── contracts/                # event/alert types, validation, dedupe_key + alert_id, schema/*.json
│   │   ├── config/                   # reuse koanf; per-binary config structs
│   │   ├── kafkax/                   # franz-go wrappers: producer, partitioned consumer loop, DLQ, failpoints
│   │   ├── detection/                # PURE engine + rules loader (config/rules.yaml), no I/O
│   │   ├── detectorapp/              # glue: Kafka partition runner + pin-offset tracker around detection
│   │   ├── storage/{clickhouse,postgres}/
│   │   ├── ingest/  auth/  ratelimit/
│   │   ├── api/                      # query-api: handler/service/repository layering (reuse boilerplate pattern)
│   │   ├── sse/                      # hub + LISTEN loop
│   │   └── telemetry/                # Prometheus, OTEL, zerolog setup
│   ├── migrations/{clickhouse,postgres}/
│   └── test/{integration,chaos}/
├── api/openapi.yaml   config/rules.yaml
├── web/   generator/   bench/   benchmarks/
├── deployments/{compose,k8s}/   observability/   docs/decisions/
```

Adaptations of the existing scaffold (see `.planning/codebase/ARCHITECTURE.md`):
- `server.Server` is an HTTP-oriented container that constructs asynq, New Relic and Clerk. **Workers must not import it.** Give each binary its own small wiring in `main.go`. The handler/service/repository layering and typed `Handle*` wrappers stay for `query-api` and `ingest` only.
- Remove Clerk, asynq, Resend and New Relic (not in the spec stack, §19.8). Fix the known defects (Recover middleware order, Shutdown nil pointer, Redis ping failure continuing, unescaped DB password) on the code that is kept.
- One multi-stage Dockerfile with `ARG BINARY`, one image per binary.
- Dependency direction: `cmd -> *app/ingest/api -> {kafkax, storage, detection, contracts}`. `detection` and `contracts` import nothing internal except each other.

---

## 7. Patterns to Follow

1. **Pure core, thin shell.** `detection.Engine` takes events and returns alerts with no clock, no Kafka and no DB. All determinism tests (replay, permutation within lateness, duplicate injection, boundaries) are plain table-driven Go tests that run in milliseconds. The Kafka runner is a thin adapter.
2. **Commit-after-effect via a commit tracker.** One `kafkax` abstraction per consumer shape: a batch consumer (writers: process -> effect -> commit) and a partition-owning consumer (detector: per-partition state + pin). Don't hand-roll offset logic in each `main.go`.
3. **Idempotent sinks everywhere.** ReplacingMergeTree plus a deduped read path, `ON CONFLICT DO NOTHING`, and deterministic IDs. Treat "replay the whole topic" as a normal operation and test it.
4. **Failpoints for crash tests.** A small interface injected at the boundary between steps.
5. **Position-based lag.** Compute lag from fetch high watermarks and the last processed offset.

## 8. Anti-Patterns to Avoid

| Anti-pattern | Why bad | Instead |
|--------------|---------|---------|
| Wall-clock windows or `time.Now()` in rules | Non-deterministic replay, violates spec §5.1 | Event time with an injected clock only for metrics |
| Shared mutex-guarded global window map across partitions | Races, state for partitions you no longer own | State owned by the partition goroutine, dropped on revoke |
| Auto-commit (franz-go default every 5s) | Commits before effects complete | `DisableAutoCommit` + explicit commit after effect |
| Committing the processed offset in the detector | State is lost on restart; windows silently undercount | Commit the state pin |
| `time.Now()` in `alert.created_at` | Duplicates do not collapse, cursor repeats | Derive from event time |
| Redis-only window counters | Non-durable source of truth (spec §7.4) | Rebuild from Kafka |
| Relying on ReplacingMergeTree merges for correctness | Merges are asynchronous | Dedupe at read (`LIMIT 1 BY` / FINAL) |
| Kafka consumer in query-api feeding SSE | Alert visible before persisted, duplicate replays | LISTEN/NOTIFY after persistence |
| Unbounded channels between poll and process | Memory growth under load | Bounded channels + pause/block |
| Single-row ClickHouse inserts | Part explosion | Batch inserts |

## 9. Scalability Considerations

| Concern | Small (local/compose) | Multi-replica (benchmark) | Larger |
|---------|----------------------|----------------------------|--------|
| Detector parallelism | 1 replica, all partitions | Replicas <= partitions; use 6-12 partitions for the benchmark | More partitions up front; changing count resets keys |
| Detector restart time | Seconds | `horizon x partition rate`; measure | Add state snapshots |
| ClickHouse inserts | 1s/10k batches | Tune batch size; measure parts count | Batch across writers, tune `max_insert_block_size` |
| SSE | One LISTEN per replica | Same | NOTIFY commit serialization; coalesce bursts or move to a Kafka-fed hub |
| Hot key | Skewed `source_ip` overloads one partition | Visible in per-partition lag | Out of v1 scope; document |
| Compose | RF=1, not HA (spec §6.3) | Say so in the report | K8s needs multi-broker RF=3, `min.insync.replicas=2` |

---

## 10. Suggested Build Order (dependencies)

Aligned with spec §14. Dependencies are noted and a few refinements are suggested.

1. **Foundations (Phase 1).** `contracts` (types, validation, `dedupe_key`/`alert_id` function, JSON schemas + fixtures), `config`, `telemetry` skeleton, Compose (Kafka KRaft, ClickHouse, Postgres, Redis), migrations, topic creation script with explicit partition counts. Decide the layout ADR (keep `backend/`) and strip the non-spec boilerplate here, before building on it. *Everything depends on `contracts`.*
2. **Ingest + event path (Phase 2).** `kafkax` producer -> `ingest` (API key table in Postgres, hashed; Redis limiter) -> `event-writer` + `kafkax` batch consumer + DLQ -> minimal `query-api` events endpoint -> Python generator. Build the batch-consumer commit abstraction and the DLQ here because the alert-writer and detector reuse them. Put the ReplacingMergeTree + `LIMIT 1 BY` read path and the duplicate-event test here.
3. **Detection (Phase 3), in this internal order.**
   a. `internal/detection` pure engine with all four rules, watermark, lateness, cooldown, eviction, and the determinism/property tests (no Kafka yet).
   b. Failpoint hooks and the detector's Kafka runner (partition goroutines, pin tracker, `ProduceSync`, explicit commit), plus crash-point tests at the three boundaries.
   c. `alert-writer` (CH, then PG with notify, then commit) and the `alert_workflow` migration, including `seq`.
   Splitting the engine from the runner is what makes the spec's "crash between state update, alert publish and offset commit" requirement testable.
4. **Query API + dashboard (Phase 4).** Roles/JWT session auth, tenant scoping, alerts list with the join (and the status-filter path), PATCH with version checks and audit, then SSE hub with LISTEN, `Last-Event-ID` backfill, bounded clients. Web pages follow the OpenAPI client.
5. **Observability + scale (Phase 5).** Position-based lag, delay histogram, replica scaling, restart-recovery and overload tests, the benchmark with dedup cost (FINAL vs `LIMIT 1 BY` vs `uniqExact`).
6. **K8s and hardening (Phase 6).** RF/ISR settings, probes, resource limits, threat model.

Pull earlier than the spec implies: metrics counters (`late_events`, `state_dropped`, `dlq`) with the code that emits them, rather than a Phase 5 retrofit. The DLQ and the dedupe read path in Phase 2. Failpoints in Phase 3.

---

## Sources

- franz-go docs, Context7 `/twmb/franz-go` (HIGH): consumer-group lifecycle (cooperative-sticky default, revoke at any time, must commit in `OnPartitionsRevoked` when committing manually), `BlockRebalanceOnPoll` + `AllowRebalance` + `PollRecords`, `CommitRecords` offset+1 semantics, `goroutine_per_partition_consuming` examples. https://github.com/twmb/franz-go/blob/master/docs/producing-and-consuming.md and `examples/goroutine_per_partition_consuming`.
- `/home/logan78/Desktop/Sentinel/spec.md` §3, §5.1, §6, §7, §8, §11, §13, §14 (authoritative constraints).
- `/home/logan78/Desktop/Sentinel/.planning/codebase/ARCHITECTURE.md` (existing scaffold).
- Not independently verified in this pass (MEDIUM/LOW, validate in phase research): ClickHouse `FINAL` performance in the pinned version, `non_replicated_deduplication_window` behavior, `CommitOffsetsSync` exact signature, and pgx `WaitForNotification` reconnect behavior.
