# Pitfalls Research

**Domain:** Real-time security event ingestion, detection and alerting (Go + Kafka KRaft + ClickHouse + PostgreSQL + Redis + React SSE)
**Researched:** 2026-10-10
**Confidence:** MEDIUM

Method note: this file synthesizes established streaming and ClickHouse practice, then checks it against `spec.md` and `.planning/codebase/CONCERNS.md`. No live documentation lookups were run in this session. Client-library defaults and ClickHouse setting defaults (marked **[verify]**) change between versions. Confirm each against pinned-version docs when the Phase 1 stack is chosen. Phase numbers refer to `spec.md` §14.

---

## Critical Pitfalls

### Pitfall 1: Offset committed before effects (library auto-commit)

**What goes wrong:**
Records are marked consumed before the ClickHouse insert, alert publish or DLQ publish completes. A crash then silently loses events or alerts. This breaks the core value ("no lost acknowledged events").

**Why it happens:**
- Some Kafka clients commit by default. franz-go auto-commits polled offsets on a timer unless `DisableAutoCommit` is set **[verify]**. Sarama consumer groups auto-commit marked offsets by default.
- Worker pools finish out of order. Committing "latest seen" commits offset N+1 while N is still in flight.
- A rebalance callback commits whatever was polled rather than what completed.

**How to avoid:**
- Disable auto-commit. Commit explicitly through one per-partition "contiguous completed watermark" tracker. Commit offset X only when every offset below X has finished its effect.
- Make "effect complete" explicit per binary:
  - event-writer: ClickHouse insert acknowledged.
  - detector: state updated, then alert publish acknowledged, then commit.
  - alert-writer: ClickHouse evidence write AND Postgres row both done.
  - DLQ path: DLQ publish acknowledged, then commit.
- Commit synchronously in the partition-revoked callback, and block the rebalance until in-flight work for revoked partitions has drained (franz-go `BlockRebalanceOnPoll`, or equivalent).
- Wrap offset logic in one shared `internal/kafka` consumer abstraction so all three consumers cannot diverge.

**Warning signs:**
- Consumer config has no explicit `DisableAutoCommit` / commit mode.
- Commits happen inside `Poll` loops or on timers.
- A chaos test that kills the writer mid-batch shows `count(distinct event_id)` below the number of accepted events.
- Committed offset is ahead of the last ClickHouse-persisted offset.

**Phase to address:** Phase 2 (event-writer and shared consumer abstraction), reused in Phase 3. Failure test: kill between insert and commit, then between publish and commit.

---

### Pitfall 2: Rebalance or restart loses in-memory window state, and the commit strategy makes it unrecoverable

**What goes wrong:**
Detector windows live in memory. After a restart or rebalance, the new owner starts from the committed offset, which is already past the events that formed the open window. Example: 9 of 10 brute-force failures were counted before the crash; the 10th arrives after restart; the count is 1, and the alert is silently missed. This is the worst kind of bug because there is no error.

**Why it happens:**
- Teams commit normally ("after effects") and assume replay rebuilds state. It only does if the committed offset is held back.
- Cooperative rebalancing leaves stale state for partitions that moved away. When the partition returns, events are counted twice.

**How to avoid:**
- Use a hold-back commit rule. The committable offset per partition is `min(last completed offset, offset of the oldest event still needed by any open window or the lateness horizon)`. After a restart, replay then reconstructs every open window. Cost: roughly (longest window + lateness) of re-read per restart. For the spec rules that is about 5 min 30 s.
- Alternative: periodic checkpoint of window state, which adds complexity. Prefer hold-back for v1, as spec §5.1 allows.
- Evict all per-partition state in `OnPartitionsRevoked` and `OnPartitionsLost`. Rebuild on assign.
- Keep state keyed per partition (`map[partition]map[key]window`) so eviction is O(partition), not a scan.
- Bound the state: cap keys per partition and count per key (spec §11.1). Evict on watermark, never on wall-clock timers. Emit a metric when capped.
- The Phase 3 recovery test must run the exact "9 then kill then 10th" case, and also a rebalance case with a third replica joining.

**Warning signs:**
- State map not partitioned by assignment.
- The recovery test only kills between alerts, not mid-window.
- Committed offsets equal the log end while windows are still open.
- Alert counts differ between a straight run and a kill/restart run over the same seeded input.

**Phase to address:** Phase 3 (design before coding rules). Re-verified in Phase 5 with multi-replica chaos.

---

### Pitfall 3: Event-time versus processing-time mixing, and watermark bugs

**What goes wrong:**
- Windows expire using `time.Now()`. When the benchmark or a replay feeds old `occurred_at` values, every event looks late and is dropped, or windows never close.
- A watermark computed as the global max across partitions makes events on slow partitions look late.
- An idle partition never advances its watermark, so its windows never close and alerts are delayed indefinitely.
- Out-of-order data within a partition: multiple ingest replicas and producer batching mean `occurred_at` is not monotonic in offset order.
- Unit tests that use `time.Now()` pass today and fail at the boundary tomorrow.

**Why it happens:**
Windowing is easy to prototype with a ticker. Event time requires a deterministic clock abstraction, and tests rarely force skew.

**How to avoid:**
- The detector never calls `time.Now()` in rule logic. Inject a clock only for metrics such as detection delay.
- Watermark is per partition: `max(occurred_at seen on that partition) - lateness` (30 s default). Windows close only when that watermark passes `window_end`.
- Decide idle-partition behavior explicitly and document it. For v1, an idle partition's windows may stay open. Surface that as a metric (`oldest_open_window_age`) rather than faking a wall-clock advance that breaks replay determinism.
- Define late events deterministically. Recommended: events within lateness are counted; events beyond it are dropped with a `late_dropped_total{rule_id}` counter (bounded labels), never reprocessed.
- Make detection output a pure function of the ordered partition input. Replaying the same input must produce identical alerts.
- Test late events at exactly `lateness`, at `lateness + 1 ms`, and out-of-order by one offset.

**Warning signs:**
- `time.Now`, `time.AfterFunc` or `time.Tick` anywhere under `internal/detection`.
- Benchmarks with historical timestamps trigger zero alerts or all-late metrics.
- Alert output differs across two runs of the same fixture.

**Phase to address:** Phase 3.

---

### Pitfall 4: Window boundary off-by-ones and attack-straddles-boundary misses

**What goes wrong:**
- Inclusive versus exclusive ends are inconsistent. An event at exactly `window_start + 5m` is counted in two windows or none.
- A tumbling window splits an attack across two buckets: 5 failures at 09:59:58 plus 5 at 10:00:02 never reach 10 in either bucket, so the alert is missed. The "9 vs 10" threshold fencepost (`>` versus `>=`) is wrong in the rule table versus the code.
- Switching to sliding windows fixes the miss, but then one attack emits many alerts with different `window_start`. That defeats deterministic dedupe and cooldown.

**Why it happens:**
Spec §5.1 and §6.2 mix a window-based `dedupe_key` (tumbling-flavored) with cooldown (sliding-flavored). The two models are not reconciled in the spec.

**How to avoid:**
- Decide once, in `docs/decisions/`: all intervals are half-open `[start, end)`. Rule thresholds are `>=` as the spec table states.
- Recommended model: sliding evaluation per key, with the alert's dedupe bucket being an aligned bucket (`floor(trigger_event_time / cooldown_or_window)`) derived from event time of the *triggering event*. This catches boundary-straddling attacks and stays deterministic. Keep bucket alignment independent of arrival order.
- Boundary table tests per rule: count = threshold - 1, = threshold, = threshold + 1; events at `start`, `end - 1ns`, `end`; and straddling-attack fixtures.
- PORT_SCAN counts distinct ports, so test duplicates of the same port (must not count twice) and port 0 / 65535.

**Warning signs:**
- Rule tests only assert "fires" and "does not fire", with no events at the exact edges.
- Seeded attack fixture fires only because it was generated mid-window.
- One attack produces more than one visible alert.

**Phase to address:** Phase 3 (decision before rule code).

---

### Pitfall 5: Duplicate (or divergent) alerts on replay

**What goes wrong:**
After a crash between alert publish and offset commit, the alert is regenerated. Duplicates appear if:
- `alert_id` depends on arrival order, wall clock, a random UUID or a non-canonical string.
- The dedupe key is built by joining fields with `|` while values (tenant, principal) can contain `|` (ambiguous or collidable).
- Timestamp formatting differs (precision, timezone) between runs.
- `rule_version` is in the key, so a rule upgrade re-alerts on all replayed data. Decide this deliberately.
- Cooldown state is in memory and lost on restart, so the replay produces a *different* alert in a new window.
- The alert content (evidence count, sample IDs) differs between original and replay because arrival order differed. Two payloads then exist for one `alert_id`.

**How to avoid:**
- Canonical dedupe key: fixed-order, length-prefixed or JSON-canonical fields, UTC, fixed (millisecond) precision, aligned window start. Hash it with SHA-256 hex. Fuzz-test with `|` and unicode in values.
- Constrain `tenant_id` charset at validation (also protects the `tenant|source_ip` Kafka key from delimiter ambiguity).
- Derive cooldown purely from event time and persisted alert history, not process memory.
- Alerts are immutable, first-writer-wins: Postgres `INSERT ... ON CONFLICT (alert_id) DO NOTHING` and ClickHouse `ReplacingMergeTree` plus read-time dedupe. Never `ON CONFLICT DO UPDATE SET status = 'open'`, which would reopen a resolved alert on replay.
- Idempotent producers do not protect against duplicates across process restarts, so consumers of `security.alerts.v1` must be idempotent anyway.
- UI also dedupes live and fetched alerts by `alert_id`.

**Warning signs:**
- `alert_id` assembled with `fmt.Sprintf("%s|%s|...")`.
- The "simulate failure between publish and commit" test asserts alert count only after a merge, or only checks Kafka.
- Resolving an alert, then restarting the detector, re-opens it.

**Phase to address:** Phase 3 (ID and cooldown design, failure test). Phase 4 verifies visible behavior through the API.

---

### Pitfall 6: ReplacingMergeTree dedup treated as synchronous

**What goes wrong:**
Replayed events appear twice in `/events` and counts until a background merge runs, possibly never within a test's lifetime. Tests that pass after `OPTIMIZE ... FINAL` hide this. Related traps:
- `FINAL` and `argMax` are correct but slow at 1M+ rows. The spec requires benchmarking the cost.
- Dedup only happens within the same partition and the same full `ORDER BY` key. If `occurred_at` is part of the key and differs on re-send (re-normalized or precision-truncated), the "duplicate" survives forever.
- Insert-level dedup tokens do not work as assumed. `insert_deduplication_token` needs the table-level dedup window, which for non-replicated MergeTree is off by default (`non_replicated_deduplication_window` **[verify]**). Batch boundaries after a crash differ from the original batch, so token-based dedup fails anyway.
- `count()` in benchmark accounting overstates persisted events.

**How to avoid:**
- Treat dedup as a read-time guarantee. All user-visible queries use `LIMIT 1 BY event_id` (scoped by tenant and time range) or `FINAL` with tenant/time predicates, and counts use `uniqExact(event_id)` or equivalent.
- Normalize `occurred_at` to the column's precision (DateTime64(3, 'UTC')) at validation, before Kafka, so replays have the identical sort key.
- Tests: insert the same batch twice, query immediately without OPTIMIZE, assert single rows and correct counts. Pagination across duplicate rows must not repeat items.
- `GET /events/{id}` needs a lookup path that does not scan the whole tenant range. Use a bloom-filter skip index on `event_id`, or require a time hint. Check with `EXPLAIN indexes = 1` **[verify]**.
- Benchmark dedup-on versus dedup-off and publish the cost, as the spec requires.

**Warning signs:**
- Integration tests call `OPTIMIZE TABLE ... FINAL` before asserting.
- Reported "persisted" counts come from plain `count()`.
- Dashboard event counts jump down after merges.

**Phase to address:** Phase 2 (schema, dedupe tests). Phase 5 (cost benchmark).

---

### Pitfall 7: Tiny ClickHouse inserts ("too many parts")

**What goes wrong:**
Each Kafka poll or HTTP request becomes its own insert, creating one part per insert. Merges fall behind, inserts are delayed, then rejected (`Too many parts`). The default thresholds (`parts_to_delay_insert`, `parts_to_throw_insert`) differ across versions **[verify]**. It shows up under load or in a long soak, not in Phase 2 demos with 1000 events.

**How to avoid:**
- The event-writer batches by whichever comes first: N rows (target tens of thousands, tuned by benchmark), a byte cap, or a flush interval (about 1 s). Each flush is one insert, committing offsets only after it succeeds (Pitfall 1).
- Cap batch size too (spec §11.1), to avoid memory blow-ups and oversized retries.
- Bound partitions per insert. Monthly partitions plus replayed historical data can touch many partitions per block (`max_partitions_per_insert_block`).
- Server-side `async_insert=1` is a fallback, not a substitute. Use `wait_for_async_insert=1`; fire-and-forget would break "commit after effect".
- Never insert once per event from the alert-writer either. Batch alerts, with a short flush interval for latency.
- Watch `system.parts` active count per table and `ClickHouse` merge metrics in the Phase 5 dashboard.

**Warning signs:**
- Insert rate per second greater than a few per writer.
- Active parts per partition climbing.
- Write latency histogram bimodal. Logs mention "Too many parts" or delayed inserts.

**Phase to address:** Phase 2 (batcher), Phase 5 (soak test and parts monitoring).

---

### Pitfall 8: Kafka client misconfiguration (acks, idempotence, in-flight, buffers, start offset)

**What goes wrong:**
- Sarama's default is `RequiredAcks = WaitForLocal` (acks=1) **[verify]**. The spec says acks=all, and `202` after Kafka ack. Leader failure can then lose acknowledged data.
- Sarama idempotent producer requires `acks=all`, `Net.MaxOpenRequests = 1` and Kafka version >= 0.11, otherwise config validation fails or reordering occurs on retry. With `SyncProducer`, `Return.Successes` must be true **[verify]**.
- franz-go defaults to idempotent, acks=all producing **[verify]**, but you still need to set record delivery timeout and buffer limits.
- Unbounded producer buffering: if Kafka is slow, the buffered records grow without bound, or `Produce` blocks the HTTP handler indefinitely. franz-go has `MaxBufferedRecords` and `MaxBufferedBytes` **[verify]**.
- The "atomic all-or-nothing batch" is NOT atomic at the Kafka level. A 500-event batch spans partitions. Some records may be durable while the response is a 5xx. The client retries and duplicates arrive.
- A nil or empty record key (for example, missing `source_ip`) breaks partitioner locality. The default partitioner spreads nil keys, so per-key ordering and detection correctness are lost.
- Sarama's default initial consumer offset is newest, so a fresh group skips pre-existing messages **[verify]**. This causes flaky integration tests and silently skipped history.
- Changing the partition count later remaps keys, which strands in-flight window state and ordering.

**How to avoid:**
- Pick one client library in Phase 1 STACK and encode a single `internal/kafka` config constructor with a test asserting the effective values: acks=all, idempotence on, bounded buffer, delivery timeout, compression, `max.message.bytes` sufficient for the largest record.
- Handler flow: validate whole batch, produce synchronously with a context deadline, map buffer-full or timeout to `503` (with `Retry-After`), and document precisely: "5xx means zero, some or all records may be published; retry is safe because `event_id` is a client-supplied idempotency key."
- Require a non-empty partition key. Validate `source_ip` presence for the events topic, or define a fallback key (`tenant|event_id`) and document that detection ignores such events.
- Create topics explicitly in an init job with the chosen partition counts, and disable auto-create. Choose partition count up front and write it in an ADR (consumer parallelism ceiling equals partition count).
- Set `auto.offset.reset` / initial offset to earliest explicitly for every consumer group.

**Warning signs:**
- No test reads effective client config.
- Topics created by auto-create show 1 partition, so multi-replica benchmarks show no scaling.
- Ingest memory grows under overload instead of returning 503.

**Phase to address:** Phase 1 (Compose topics, stack choice). Phase 2 (producer path). Phase 5 (overload test).

---

### Pitfall 9: KRaft Compose setup gotchas

**What goes wrong:**
- Single-node KRaft needs internal topics' replication factor set to 1 (`offsets.topic.replication.factor`, `transaction.state.log.replication.factor`, `transaction.state.log.min.isr`), or consumer groups and idempotent producers hang or fail.
- Advertised listeners: containers need `kafka:29092`, the host needs `localhost:9092`. Using one listener breaks either Compose services or host-run tests and the generator.
- Reusing a data volume with a changed `CLUSTER_ID` fails to start ("cluster ID mismatch").
- `depends_on` without `condition: service_healthy` starts consumers before the broker is ready. A healthcheck that only tests the TCP port reports ready before metadata is usable.
- Auto-create topics masks configuration errors (Pitfall 8).
- Image sourcing: the widely used Bitnami images changed their registry and availability policy in 2025 **[verify]**; prefer the official `apache/kafka` image and pin by digest.
- Memory: Kafka, ClickHouse, Postgres, Redis, Prometheus, Grafana plus Go services strain Docker Desktop's default memory. ClickHouse can OOM. Set explicit memory limits and `KAFKA_HEAP_OPTS`.
- Group coordinator `group.initial.rebalance.delay.ms` (default 3 s) slows tests. Set to 0 for dev/test brokers.

**How to avoid:**
- One reviewed Compose file with: dual listeners, fixed `CLUSTER_ID`, single-node replication overrides, `kafka-broker-api-versions` (or equivalent) healthcheck, an idempotent `topic-init` job, and `service_healthy` dependencies.
- `make reset` removes volumes. `make dev-down` preserves them. Document the CLUSTER_ID gotcha in troubleshooting.
- Document that Compose is RF=1, not HA (spec §6.3). The K8s phase explains multi-broker settings.

**Warning signs:**
- "Works from host, fails in container" (or reverse).
- First `make dev-up` after a pull fails, but a retry succeeds.
- Consumers log `coordinator not available`.

**Phase to address:** Phase 1 (exit check: `make dev-up` healthy from clean checkout, twice). Phase 6 (multi-broker K8s).

---

### Pitfall 10: Alert list / filter split across ClickHouse and Postgres

**What goes wrong:**
Spec §7.3 stores immutable alerts in ClickHouse and mutable `status` in Postgres, joined "at application level after tenant scope and pagination". A `status=open` filter then cannot be applied in ClickHouse. Filtering after pagination returns short or empty pages while `has_more` is true, or the cursor skips rows. Related:
- Race: the alert appears in SSE or ClickHouse before its Postgres workflow row exists, so `PATCH` returns 404 and the detail page breaks.
- Dual write is non-atomic; a crash between the two leaves an alert visible without evidence, or the reverse.

**How to avoid:**
- Make Postgres the listing index. The alert writer inserts a Postgres row with the filterable columns (tenant, rule, severity, created_at, alert_id, status). The list endpoint queries Postgres and paginates there. ClickHouse holds evidence and is fetched by `alert_id` for detail.
- Write order in alert-writer: ClickHouse evidence first, then the Postgres row. An alert is visible only when its Postgres row exists, so visible always implies evidence exists. Both steps idempotent; commit the offset after both.
- Push SSE only after persistence (Pitfall 12), and `PATCH` of a missing alert returns a retryable condition, not a mystery 404.
- Record this in an ADR as the §7.3 "single source of truth" decision.

**Warning signs:**
- A list handler that calls ClickHouse, then loops over Postgres lookups to drop rows.
- Test: filter by `status=open` with 100 alerts of which 90 are resolved, then page with limit 10 returns uneven pages.

**Phase to address:** Phase 3 (schemas, alert-writer ordering), Phase 4 (API).

---

### Pitfall 11: Cursor pagination with ties and precision loss

**What goes wrong:**
- Cursor on `occurred_at` alone skips or repeats rows with equal timestamps. The spec already prescribes `(occurred_at, event_id)`, but implementations break it:
  - Comparing the tuple wrongly (`occurred_at < ? AND event_id < ?` instead of a lexicographic tuple comparison).
  - Encoding the timestamp with lower precision than stored (ms vs ns), so the boundary row repeats or vanishes.
  - Mixed sort directions between `ORDER BY` and the cursor predicate.
  - Duplicate rows pre-merge (Pitfall 6) repeat across pages.
  - Cursor not tenant-bound, so a cursor from tenant A used by tenant B leaks timing or content.
  - Unbounded `OFFSET`-style fallback or missing range cap.

**How to avoid:**
- Use `ORDER BY occurred_at DESC, event_id DESC` and the predicate `(occurred_at, event_id) < (?, ?)` as a tuple comparison with identical precision as the column. Fetch `limit + 1` to compute `has_more`.
- The cursor is opaque (base64 of a versioned structure) and includes tenant ID plus filter hash, authenticated with an HMAC so it cannot be forged or reused with different filters.
- Property test: generate many rows with colliding timestamps, page with random page sizes, and assert exact set equality with no duplicates and no gaps. Include rows inserted during pagination.
- Alerts use `(created_at, alert_id)` in Postgres.

**Warning signs:**
- Fixtures have unique timestamps only.
- Page N's last item reappears at N+1 in the UI.

**Phase to address:** Phase 4 (unit and property tests alongside cursor encoder).

---

### Pitfall 12: SSE behind proxies, slow clients, and auth without query tokens

**What goes wrong:**
- **Buffering:** nginx (`proxy_buffering`), ingress, gzip middleware, and Echo's Gzip or Timeout middleware buffer the stream, so alerts arrive in bursts or never. Flushing requires `http.Flusher`, which wrappers can hide.
- **Server timeouts:** `http.Server.WriteTimeout` kills long-lived streams. Set write deadlines per connection (`http.NewResponseController`) rather than globally.
- **Idle timeouts:** proxies and load balancers drop silent connections (commonly 60 s). Send a comment heartbeat every 15-30 s.
- **Graceful shutdown hangs:** `Server.Shutdown` waits for open SSE connections forever. Cancel stream contexts on shutdown (`RegisterOnShutdown`).
- **Slow clients:** a blocking broadcast to a stalled client stalls everyone and the buffer grows without bound (`sentinel_sse_clients` gauge hides it).
- **Auth:** the browser `EventSource` cannot set an `Authorization` header, which pushes people to put tokens in the URL (spec forbids). Options: an HttpOnly, SameSite cookie session (requires CSRF handling for `PATCH` and credentialed CORS, not `*`), or fetch-based SSE (for example `@microsoft/fetch-event-source`) with a bearer header. A short-lived single-use ticket also works.
- **Multi-replica fan-out:** if every query-api replica joins the same Kafka consumer group, each replica sees only a share of alerts and each client misses the rest. Each replica needs its own group (unique group ID, start at latest), or fan-out from a persisted source.
- **Resume:** `Last-Event-ID` needs ordered IDs. `alert_id` is a hash with no order. Use a cursor-style ID (`created_at` + `alert_id`, encoded), and make the reconnect path "REST backfill since that cursor, then stream", deduped by `alert_id`.
- **Tenant leak:** a global hub that broadcasts without tenant/role filtering (Pitfall 14).
- Browser limit of 6 HTTP/1.1 connections per origin: multiple tabs each holding a stream can starve other requests. HTTP/2 mitigates.
- Frontend: reconnect storms without jittered backoff, and an unbounded live-feed array.

**How to avoid:**
- Set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`. Exclude the route from gzip and timeout middleware. Flush after the headers and after each event.
- Per client: a bounded channel (for example 64), a non-blocking send, and on overflow close the connection so the client resumes via backfill. Defer cleanup so gauge and goroutines are released on `r.Context().Done()`.
- Cap connections per user and tenant.
- Test through a real proxy in Compose (an nginx or Caddy service in the dev stack, even if only for the SSE test) with a slow-client test and a disconnect/reconnect test. Mock-free.
- Drive SSE from the persisted alert (after the Postgres row exists) so a click-through never 404s.

**Warning signs:**
- Alerts appear only after the connection closes, or in batches.
- Streams die at exactly 30 or 60 seconds.
- SSE token appears in access logs.
- Shutdown takes the full grace period.
- Two query-api replicas: only half the live alerts reach a given browser.

**Phase to address:** Phase 4 (handler, auth, backfill, tests). Phase 6 (ingress annotations, K8s idle timeouts).

---

### Pitfall 13: Benchmark dishonesty

**What goes wrong:**
The spec forbids invented numbers (§1, §18). Common ways honest teams still mislead:
- **Coordinated omission:** a closed-loop generator waits for each response before sending the next, so server stalls reduce offered load and hide the latency. Measure latency from the *intended* send time on a fixed-rate schedule (open-loop, wrk2 style), not from actual send time.
- **Conflated counts:** "events/sec" mixes client-sent, HTTP-202-accepted (Kafka-acked) and ClickHouse-persisted. Spec §15 requires them separated. 429/503 responses are not accepted events.
- **Persisted count inflated** by pre-merge duplicates (`count()` versus `uniqExact(event_id)`).
- **Generator is the bottleneck:** a single Python process (GIL) tops out at modest rates. Run multi-process or batch-heavy load and report whether the generator was saturated (generator CPU).
- **Everything on one machine:** generator, Kafka, ClickHouse and Docker VM compete for cores. State it in the report; do not extrapolate.
- **Warm-up and drain included** in "sustained rate". Report steady-state window separately, with end-to-end drain time.
- **Percentiles averaged** across intervals. Compute from raw samples or mergeable histograms.
- **Deterministic seeds versus timestamp bounds:** the validator rejects stale timestamps (skew), so a seeded fixture with fixed 2026 timestamps fails later, and one with timestamps anchored to "now" yields different alert IDs per run. Use a relative-offset generator with a run-start anchor, and compare alerts structurally (rule, entity, count) rather than by absolute ID across runs.
- **Missing loss/duplicate accounting:** report expected alerts, actual alerts, missing and duplicate counts, not only throughput.
- **Latency "event-time-to-alert"** measured with synthetic timestamps in the past is meaningless. Tie it to generator-assigned send time on one host clock.

**How to avoid:**
- `bench/report.py` consumes raw per-request samples and ClickHouse/Kafka final counts, computes the three-way accounting table and the reconciliation `sent = accepted + rejected + errored; persisted_unique = accepted - loss`, and refuses to emit a report when reconciliation fails.
- Template headers per spec §12 (hardware, partitions, replicas, event size distribution, limitations). Mark the limitations section mandatory.
- Never put a throughput number in the README or the portfolio without the linked result file.

**Warning signs:**
- README quotes a rate with no `benchmarks/results/*.md` link.
- p99 looks suspiciously flat while 429s are present.
- Sent equals persisted exactly, with duplicates and no retries in the run.

**Phase to address:** Phase 2 (generator design: seeded, relative time, multi-process). Phase 5 (report, accounting). Phase 6 (README claim audit).

---

### Pitfall 14: Tenant leakage

**What goes wrong:**
In a multi-tenant analytics system leakage is usually an omission, not a bypass:
- A ClickHouse query or helper that forgets `tenant_id = {tenant}` (especially `GET /events/{id}`, metrics summary, rule hit breakdowns, top-N).
- Tenant taken from the request body, query string or cursor instead of the verified identity (spec §10).
- Redis cache or rate-limit keys without a tenant or API-key prefix.
- The SSE hub broadcasting every alert to every client.
- `PATCH` on `alert_id` without a tenant predicate (IDOR). Returning 403 vs 404 reveals existence; prefer 404.
- Forgeable or reusable cursors (Pitfall 11).
- Postgres queries scoped only by `alert_id`.
- Logs, DLQ records and metrics labels containing tenant data. (Tenant as a Prometheus label is also banned, see Pitfall 17.)
- Ambiguity in the Kafka key `tenant|source_ip` if the tenant contains `|` (Pitfall 5).
- Admin role implicitly cross-tenant.

**How to avoid:**
- Single choke point: a request-scoped `TenantScope` obtained only from the auth middleware, required as an argument by every storage method (compile-time forcing). No repository method accepts a raw tenant string from handlers.
- Use ClickHouse query parameters for tenant, never string concatenation. Consider a dedicated read-only ClickHouse user for the query API.
- SSE hub keyed per tenant.
- Table-driven test that runs every route and SSE as tenant A with tenant B's IDs, cursors and filters, and asserts 404/empty. Seed two tenants with overlapping `event_id` shapes and identical source IPs. Phase 4 exit check already requires two demo tenants.
- Add a lint or grep test: any SQL mentioning the events or alerts tables must contain the tenant predicate.

**Warning signs:**
- A repository function signature without a tenant parameter touching event/alert tables.
- Tests use a single tenant.
- Cache keys built from `alert_id` alone.

**Phase to address:** Phase 2 (key format, schema, scope type), Phase 4 (API/SSE tests), Phase 5 (metrics summary).

---

### Pitfall 15: Dead-letter and retry loops that block or drop

**What goes wrong:**
- Retrying a poison record in place blocks the whole partition (head-of-line), so lag grows and detection stops for that key range.
- Skipping to DLQ on any error (including transient ClickHouse outages) loses valid events into the DLQ at scale.
- DLQ publish failure followed by commit loses the record.
- Retries without backoff or jitter hammer a recovering ClickHouse. Retry queues are unbounded.

**How to avoid:**
- Classify errors: transient (retry with exponential backoff and jitter, bounded; pause consumption and mark readiness degraded when exhausted, do not skip) versus permanent (schema invalid, unparseable: DLQ with reason, original topic/partition/offset, redacted context; commit only after DLQ ack).
- DLQ topic is bounded by retention. Metric `sentinel_dlq_events_total{reason}` with an enumerated reason set (not free text).
- Test with a malformed message in the middle of a batch: valid messages around it must still persist, and the offset must advance past it exactly once.

**Warning signs:**
- A `for { err := process(); if err != nil { continue } }` loop.
- DLQ reason label derived from `err.Error()`.

**Phase to address:** Phase 3 (spec lists DLQ in Phase 3; event-writer needs the poison path from Phase 2).

---

### Pitfall 16: Redis token bucket and boilerplate rate-limiter carry-over

**What goes wrong:**
- The boilerplate limiter is in-memory, per instance, IP-keyed and hardcoded at 20 (CONCERNS.md). Behind a proxy `RealIP` can be spoofed. With N replicas the effective limit is N times as large.
- A naive Redis implementation (GET, compute, SET) races under concurrency. Using app-server time for refill causes drift across replicas.
- Redis outage behavior is unspecified. Boilerplate swallows Redis ping failure at startup (CONCERNS.md), so the limiter silently fails open.
- Rate limiting keyed by IP rather than by authenticated producer or API key.

**How to avoid:**
- Atomic Lua script (or `EVALSHA`) implementing the token bucket using Redis `TIME`. Key per API key (and tenant), not IP. Limits come from config.
- Document and test the chosen failure mode (spec §7.4): fail-closed returns 503, or fail-open with a loud metric and degraded readiness. Never silent.
- Fix the boilerplate defects while touching this: Redis startup failure policy, `Recover` middleware registered first, nil guard in `Shutdown`, close Redis on shutdown, trusted-proxy `IPExtractor`, skip health endpoints in rate limit and logging.
- 429 includes `Retry-After`, computed from the bucket.

**Warning signs:**
- Two replicas, each allowing the full limit.
- Redis killed in a test and ingestion still returns 202 with no metric.

**Phase to address:** Phase 2 (ingest path, boilerplate defect fixes). Phase 5 (overload/failure tests).

---

### Pitfall 17: High-cardinality Prometheus labels, and misleading lag/latency metrics

**What goes wrong:**
- Labels such as tenant, IP, user, request ID, raw URL path or `err.Error()` blow up series counts. A raw `r.URL.Path` containing `{event_id}` creates unbounded series.
- `sentinel_kafka_consumer_lag{group,topic,partition}` is bounded, but the **detector's committed lag is intentionally inflated** by the hold-back commit rule (Pitfall 2), so the "sustained lag" alert fires permanently. Report lag from the *processed* position versus log end, and report committed lag as a separate, differently named series.
- `sentinel_detection_delay_seconds` (detection time minus event time) is huge for replayed or backfilled events and default histogram buckets (ms scale) are wrong for second-to-minute delays.
- Both OpenTelemetry metrics and Prometheus client metrics registered for the same signal doubles series and confuses dashboards.
- `/metrics` exposed publicly (spec says internal only).

**How to avoid:**
- Use the route template (`c.Path()`), enumerated status classes, enumerated `reason`/`result` values. Add a unit test that scrapes the registry after a synthetic load and asserts a series-count ceiling.
- Define explicit bucket layouts per histogram in code review.
- Keep per-tenant debugging in logs and traces, not metrics.
- Serve `/metrics` on a separate internal port.

**Warning signs:**
- `curl /metrics | wc -l` grows with traffic.
- A permanent lag alert in a healthy system.

**Phase to address:** Phase 5 (metrics), with the label policy stated in Phase 1 conventions.

---

### Pitfall 18: Flaky Testcontainers integration tests in CI

**What goes wrong:**
- Waiting on an open port rather than readiness: Kafka accepts TCP before metadata and the group coordinator are usable; ClickHouse accepts connections before init scripts finish.
- Advertised-listener mismatch with random mapped ports (Pitfall 9). Use the testcontainers-go Kafka module, which handles it, rather than hand-rolled env **[verify]**.
- Container-per-test is slow, and 2-vCPU, ~7 GB GitHub runners run out of memory with Kafka, ClickHouse and Postgres together.
- Shared topics, groups and tables across tests leak state. Fixed `Sleep` calls instead of `Eventually` with a deadline.
- Docker Hub rate limits and image pull time; unpinned image tags change underneath CI.
- The consumer group join delay (`group.initial.rebalance.delay.ms`, session timeout) makes tests slow or timing-sensitive.
- Chaos tests that kill containers are nondeterministic if the kill point is a sleep rather than a hook.
- Go test packages running in parallel start duplicate heavy containers.
- Boilerplate currently has only a Postgres container helper and no tests (CONCERNS.md).

**How to avoid:**
- One shared environment per test package via `TestMain` (or one Compose stack for the smoke suite), with unique topic, group and table names per test (`t.Name()` plus random suffix).
- Readiness probes that do real work (produce and consume a probe record; `SELECT 1` plus migrations applied).
- `require.Eventually` with explicit timeouts, never bare sleeps. Failure-injection via test hooks (kill between publish and commit) rather than timing.
- Put integration tests behind a build tag, run with `-p 1`, with `-timeout` and a CI job timeout. Pin images by digest, cache them, and dump container logs on failure as CI artifacts.
- Keep the PR gate small (the smoke path) and run the full chaos suite on a schedule or label, but never `|| true` (spec §15).
- Track flake rate: re-run policy documented, not hidden retries.

**Warning signs:**
- A test passes locally and fails first run in CI.
- `time.Sleep` greater than one second in test files.
- CI times vary by more than 2x.

**Phase to address:** Phase 1 (test harness and CI skeleton, since the boilerplate has none). Every later phase extends it.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Library auto-commit offsets | Zero commit code | Silent data loss on crash | Never |
| In-memory windows with normal commit | Simple detector | Missed detections after restart (Pitfall 2) | Never; use hold-back commit |
| `FINAL` everywhere instead of designed dedupe queries | Correct counts quickly | Slow queries past ~1M rows | Phase 2 only, replaced by tuned `LIMIT 1 BY` / benchmark-driven choice |
| One insert per poll | Simple writer | "Too many parts" under load | Never beyond the first spike |
| Global SSE broadcast hub | Fast demo | Tenant leak, slow-client stalls | Never |
| Tokens in SSE query string | EventSource works | Credentials in logs and history; spec violation | Never |
| Hardcoded thresholds/limits (inherited from boilerplate rate limiter) | None | Not tunable, wrong across replicas | Never |
| Single-partition topics | Easy ordering | No scaling, key remap later | Local smoke only; set real count before Phase 2 exit |
| Skipping the proxy in SSE tests | Faster tests | Buffering bugs found in K8s | Never for the SSE test |
| Keeping Clerk/asynq/New Relic from boilerplate | No rewrite | Startup requires unrelated secrets (CONCERNS.md), unmanaged global state | Only until Phase 1 replaces auth; remove by Phase 2 |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Kafka producer | Default acks, unbounded buffer, nil key | acks=all, idempotent, bounded buffer, deadline to 503, mandatory key |
| Kafka consumer | Auto-commit, newest start offset, no revoke handler | Manual contiguous commit, explicit earliest, revoke drains and evicts state |
| Kafka KRaft (Compose) | Single advertised listener, no RF=1 overrides, volume/CLUSTER_ID mismatch | Dual listeners, RF overrides, fixed ID, healthcheck + topic-init |
| ClickHouse insert | Small inserts, no wait on async | Batched by rows/bytes/time; `wait_for_async_insert=1` if async |
| ClickHouse query | Missing tenant predicate; counting before merge; string-built SQL | Query parameters, mandatory tenant scope, `LIMIT 1 BY`, `uniqExact` |
| ClickHouse time | DateTime64 precision mismatch between validator, cursor, dedupe | Normalize to one precision/UTC at validation |
| PostgreSQL | `ON CONFLICT DO UPDATE` reopening alerts; status change and audit in separate transactions | `DO NOTHING` for detections; one transaction for transition + audit + version check; DSN built with `url.URL` (CONCERNS.md) |
| Redis | Non-atomic bucket; per-IP key; silent failure | Lua atomic, per-API-key, documented fail mode, readiness reflects it |
| SSE via proxy | Buffering, idle timeout, global write timeout | `X-Accel-Buffering: no`, heartbeat, per-conn deadlines, test through real proxy |
| Prometheus/OTel | Duplicate instrumentation, label explosion | One metrics path, enumerated labels, series ceiling test |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Hot partition from one attacker IP (REQUEST_FLOOD is exactly this) | One partition lagging, others idle, no scaling with replicas | Benchmark with skewed keys, document the limit, report per-partition lag | First realistic flood scenario |
| Replicas exceed partition count | Idle replicas, flat throughput | Set partitions at least max replica count up front | Multi-replica benchmark (Phase 5) |
| `FINAL` / `GROUP BY event_id` over wide ranges | Slow `/events`, `/metrics/summary` | Tenant+time predicates, range cap, primary-key-aligned ordering, rollup table or materialized view for summary | Roughly 10M+ rows in range |
| Query by `source_ip` or `event_id` not in sort prefix | Full scans of a tenant range | Skip indexes (bloom filter) or projection, verify with `EXPLAIN` | Grows with retained data |
| Unbounded per-key window state | Detector RSS climbs during scans from many sources | Cap keys per partition, cap distinct-port set (bitset), metric on cap hits | Cardinality attack, high-volume benchmark |
| Detector re-read after restart | Long recovery, lag spike | Hold-back only as large as window + lateness; measure recovery time | Large windows or high rate |
| Monthly partitions with 7-day TTL | Expired rows linger until whole-part TTL merges | Consider daily partitions for demo TTL, or `ttl_only_drop_parts`; verify TTL behavior **[verify]** | Long-running demo |
| SSE connection count | File descriptor and goroutine growth | Per-user caps, defer cleanup, gauge | Hundreds of tabs |
| Python generator saturation | Throughput plateaus below server capacity | Multi-process, check generator CPU, report it | A few thousand req/s per process (order of magnitude, verify locally) |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Tenant from body/query/cursor | Cross-tenant read or write (High) | Tenant only from verified identity; HMAC-bound cursors |
| Logging request bodies or auth headers | Secret/PII leak (spec §6.1, §10) | Redaction; remove `fmt.Println` debug helper (CONCERNS.md); no body logging in request logger |
| Unescaped event attributes rendered in UI | Stored XSS from attacker-controlled event fields (High; attackers control attribute content by design) | Render as text/JSON tree; never `dangerouslySetInnerHTML`; CSP |
| User-controlled strings in ClickHouse SQL | Injection | Query parameters only; no concatenation, including `ORDER BY` columns (allowlist) |
| Unbounded JSON bodies / attributes | Memory DoS | `http.MaxBytesReader` (1 MiB), bounded attribute size, strict decode with unknown-field rejection |
| API keys stored plaintext or compared with `==` | Credential leak, timing | Hash at rest, constant-time compare, per-key tenant binding |
| CORS `*` or credentialed wildcard | Cross-origin abuse (CONCERNS.md) | Explicit origins; reject `*` in production config |
| `/metrics`, ClickHouse, Redis, Kafka ports published to host/public | Information disclosure | Bind internal networks; only dev-mode host ports, documented |
| `sslmode=disable` permitted in production (CONCERNS.md) | Plaintext DB traffic | Config validation rejects it in production |
| Role checks only in the UI | Viewer mutates alerts | Server-side role middleware plus tests for each route/role |
| Spoofable `X-Forwarded-For` for rate limiting | Limit bypass | Trusted proxy config; key by API key |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Live feed grows without bound | Browser memory blowup | Cap list (for example 500), virtualize |
| Duplicate alert rows after reconnect | Analyst confusion, double triage | Dedupe by `alert_id`, backfill by cursor |
| Silent SSE disconnect | Analyst believes no alerts | Visible connected/reconnecting state; backoff with jitter |
| Status change racing the version check | Lost update or opaque error | Surface 409 with refreshed state |
| Events page with huge unfiltered range | Timeouts | Default range, range cap error message, debounce |
| Pagination that jumps (Pitfall 11) | Missing or repeated rows | Tie-safe cursors, stable sort |
| Lag charts showing hold-back as problem | Operator false alarms | Separate "processing lag" from "committed lag" |
| Fake/hardcoded dashboard values | Violates spec §18 | Real endpoints only; empty/error states tested |

## "Looks Done But Isn't" Checklist

- [ ] **Ingestion 202:** Often missing the "Kafka acked" guarantee under broker slowness. Verify with a stalled-broker test returning 503 within the deadline and bounded memory.
- [ ] **Atomic batch:** Often assumed atomic. Verify the documented retry semantics (partial publish on 5xx) with a test that duplicates are absorbed by `event_id`.
- [ ] **Event-writer:** Often missing poison handling and flush-on-shutdown. Verify kill mid-batch yields `uniqExact(event_id) == accepted`.
- [ ] **Dedup:** Often only correct after `OPTIMIZE`. Verify queries immediately after a double insert.
- [ ] **Rules:** Often missing edge-of-window and straddling cases. Verify threshold-1/threshold/threshold+1 and attack across the boundary.
- [ ] **Recovery:** Often tested between alerts only. Verify mid-window kill ("9 then crash then 10th") and rebalance with a new replica.
- [ ] **Duplicate-safe alerts:** Often checked only in Kafka. Verify visible alerts through the API after replay, and that a resolved alert stays resolved.
- [ ] **Late events:** Often undocumented. Verify behavior at exactly lateness and lateness + 1 ms, plus `late_dropped` metric.
- [ ] **Tenant isolation:** Often tested on REST only. Verify SSE, `GET /events/{id}`, PATCH, cursors, metrics summary.
- [ ] **SSE:** Often tested without a proxy. Verify through nginx/Caddy with heartbeat, slow client, reconnect + backfill, and shutdown within the grace period.
- [ ] **Readiness:** Often a bare HTTP 200. Verify it flips when Kafka, ClickHouse, Postgres, or Redis is stopped (spec §11.1).
- [ ] **Graceful shutdown:** Often commits unfinished work or hangs on SSE. Verify SIGTERM with in-flight batch.
- [ ] **Metrics:** Often unbounded labels. Verify series ceiling test and `/metrics` not publicly bound.
- [ ] **Benchmark report:** Often missing reconciliation. Verify sent/accepted/rejected/persisted/duplicate counts add up and `report.py` fails otherwise.
- [ ] **CI:** Often has `|| true` or skips integration. Verify a deliberately broken test turns CI red.
- [ ] **Clean checkout:** Often depends on local state. Verify `make dev-up` twice from scratch, including after `make dev-down` with volumes preserved.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Offsets committed early, events lost | HIGH | Events topic retention (24h) allows reset of the consumer group to a timestamp; re-run writer; dedupe absorbs overlap. Beyond retention: data is gone |
| Missed detections from lost window state | MEDIUM | Reset detector group to a time before the gap; idempotent alert IDs absorb duplicates; requires the hold-back design to be correct |
| Duplicate visible alerts | MEDIUM | Fix canonical key, backfill dedupe by `alert_id` in Postgres (unique constraint), rebuild ClickHouse alerts table with the corrected ordering |
| Too many parts | LOW-MEDIUM | Pause writers, wait for merges (or `OPTIMIZE` carefully), then increase batch size/interval |
| Wrong partition count | HIGH | New topic with correct count, dual-consume or replay, repoint services; state remap implies temporary missed windows |
| ClickHouse sort key / partition design wrong | HIGH | New table, backfill from Kafka retention or old table via `INSERT SELECT`, swap |
| Tenant leak found | HIGH | Disable endpoint, audit access logs, add the missing predicate and regression test, rotate cursor HMAC key |
| Misleading benchmark published | MEDIUM | Retract the number, re-run with fixed accounting, add the limitation to the report |
| Flaky CI | MEDIUM | Quarantine with a tracked issue (not `|| true`), fix readiness or isolation, restore as required check |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1 Offset committed before effects | Phase 2 (reused 3) | Kill-mid-batch test; unique persisted equals accepted |
| 2 Rebalance/restart loses window state | Phase 3 (re-verified 5) | "9 then crash then 10th" and rebalance tests |
| 3 Event vs processing time, watermarks | Phase 3 | No `time.Now` in detection; late-event tests at the boundary; deterministic replay output |
| 4 Window boundary off-by-ones | Phase 3 | Boundary tables; straddle fixture; one alert per attack |
| 5 Duplicate alerts on replay | Phase 3 (visible in 4) | Replay + resolved-stays-resolved test; ID fuzz test |
| 6 ReplacingMergeTree async dedupe | Phase 2 (cost in 5) | Immediate post-double-insert query correct, no OPTIMIZE |
| 7 Too many parts | Phase 2 (soak in 5) | Parts count stable in soak; insert rate per writer low |
| 8 Kafka client misconfig | Phase 1 (topics, stack), Phase 2 (producer) | Effective-config test; stalled-broker 503; key-required test |
| 9 KRaft Compose gotchas | Phase 1 (multi-broker Phase 6) | `make dev-up` from clean twice; host and container clients both work |
| 10 CH/PG alert split | Phase 3 (API in 4) | Status-filter pagination test; write-order crash test |
| 11 Cursor ties | Phase 4 | Property test with colliding timestamps and concurrent inserts |
| 12 SSE | Phase 4 (ingress in 6) | Proxy-fronted test: heartbeat, slow client, reconnect/backfill, shutdown, multi-replica |
| 13 Benchmark dishonesty | Phase 2 (generator), Phase 5 (report), Phase 6 (README audit) | `report.py` reconciliation gate; open-loop latency method documented |
| 14 Tenant leakage | Phase 2 (scope type), Phase 4 (tests) | Cross-tenant matrix over all routes and SSE |
| 15 DLQ and retry loops | Phase 2 (basic), Phase 3 (full) | Poison-in-the-middle test; broker/ClickHouse outage test |
| 16 Redis limiter and boilerplate defects | Phase 2 (limiter and defect fixes), Phase 5 | Multi-replica limit test; Redis-down behavior test |
| 17 Prometheus cardinality and lag semantics | Phase 5 (policy noted Phase 1) | Series ceiling test; no permanent lag alert in healthy run |
| 18 Flaky Testcontainers | Phase 1 (harness), all later | Repeat-run flake check (for example 10 consecutive green CI runs) |

## Phase-Specific Warnings

| Phase | Pitfalls to Brief the Planner On | Research Flag |
|-------|----------------------------------|---------------|
| 1 Foundations | 8 (topics/stack choice), 9 (Compose), 18 (harness), boilerplate carry-over (Clerk/New Relic hard requirements block local startup) | Pin library choice (franz-go vs sarama) with a config-defaults check; low need for deeper research otherwise |
| 2 Ingest and storage | 1, 6, 7, 8, 14, 15, 16, 13 (generator) | Needs deeper research: ClickHouse dedupe/ORDER BY design and event_id lookup index |
| 3 Detection | 2, 3, 4, 5, 10, 15 | Needs deeper research: hold-back commit tracker and sliding-vs-tumbling dedupe model; write an ADR before coding |
| 4 Query API and dashboard | 10, 11, 12, 14 | SSE through proxy and cookie-vs-fetch auth choice need a short spike |
| 5 Observability and scale | 13, 17, 7 (soak), 2 (multi-replica) | Benchmark methodology (open-loop load) needs a design pass |
| 6 K8s and hardening | 9 (multi-broker), 12 (ingress timeouts), 13 (claim audit), probes reflecting real readiness | Ingress SSE annotations and Kafka multi-broker settings need current docs |

## Sources

- `spec.md` (v1.0), sections 3, 5.1, 6, 7, 8, 11, 12, 14, 15: requirement and contract constraints; the spec's own ambiguities (window model, atomic batch, SSE resume) are flagged above.
- `.planning/codebase/CONCERNS.md`: boilerplate defects that interact with these pitfalls (Redis swallow, Recover order, in-memory limiter, DSN escaping, no tests, no CI).
- Established practice and well-known behavior from Kafka client documentation (franz-go, Sarama), ClickHouse documentation (ReplacingMergeTree, insert deduplication, async inserts, MergeTree parts limits), the Apache Kafka KRaft docs, the Prometheus instrumentation guidelines, wrk2/HdrHistogram coordinated-omission write-ups, and Testcontainers documentation. These were drawn from prior knowledge, not re-fetched this session; confidence is MEDIUM overall. Items marked **[verify]** are version-sensitive defaults to confirm against pinned versions during Phase 1.

---
*Pitfalls research for: real-time security event ingestion/detection/alerting platform (Sentinel)*
*Researched: 2026-10-10*
