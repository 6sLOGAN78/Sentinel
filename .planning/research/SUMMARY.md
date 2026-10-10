# Project Research Summary

**Project:** Sentinel: Real-Time Threat Detection Platform
**Domain:** Real-time security event ingestion, detection and alerting (detection-only mini-SIEM)
**Researched:** 2026-10-10
**Confidence:** MEDIUM-HIGH

## Executive Summary

Sentinel is an at-least-once Kafka pipeline:
- HTTP ingest fans out to three consumer groups: detector, event-writer and alert-writer.
- ClickHouse holds immutable events and alerts.
- PostgreSQL holds mutable workflow state and audit.
- A tenant-scoped REST and SSE query API feeds a React dashboard.

The core value is provable correctness under failure, not feature breadth. The detection engine is a pure, deterministic library wrapped in a thin Kafka runner. Every sink is idempotent, so replaying the topic is a normal, tested operation.

Stack:
- Everything stays inside spec §2.
- franz-go for Kafka forces a Go 1.26 toolchain bump.
- clickhouse-go native batches, pgx, and a custom Lua token bucket on go-redis.
- The detector keeps in-memory state with one goroutine per owned partition, rebuilt by replay through a committed "state pin" offset.

The biggest risks are silent correctness traps:
- Offsets committed before their effects complete.
- Window state lost on restart.
- Wall-clock time leaking into rules.
- ClickHouse dedup assumed to be synchronous.
- Alerts filtered after pagination, which gives short pages.
- SSE that is wrong across replicas or behind proxies.
- Missing tenant scope.

## Resolved Conflicts (each becomes an ADR)

### 1. SSE fan-out: Postgres LISTEN/NOTIFY (overrides STACK.md / PITFALLS.md per-instance Kafka group)

How it works:
- The alert-writer calls `pg_notify` in the same transaction as `INSERT alert_workflow ... ON CONFLICT DO NOTHING RETURNING seq`, and only when a row was actually inserted.
- Each query-api replica holds its own `LISTEN` connection feeding a per-replica hub.
- The SSE `id:` is the Postgres `seq`.

Why:
- **Multi-replica:** every replica sees every notification, with no consumer-group tuning.
- **Replay duplicates:** replays hit `DO NOTHING` and never notify. A Kafka consumer would need an LRU dedupe.
- **Visible implies persisted:** the notify fires after the ClickHouse insert and the Postgres commit, so click-through can't 404.
- **Reconnect and backfill:**
  - `created_at` is event time, so a late alert would be missed by a time-based `Last-Event-ID`.
  - `seq` is insertion order. Backfill with `WHERE tenant_id=$1 AND seq > $last ORDER BY seq`, with a small overlap, and the client dedupes by `alert_id`.
  - `seq` can commit out of order, so do not claim gap-free delivery.
- **Dependency surface:** query-api needs no Kafka client. Its readiness depends only on ClickHouse and Postgres.

Costs:
- NOTIFY is at-most-once, so the backfill path is mandatory. It needs a test that drops the LISTEN connection mid-stream.
- The LISTEN loop needs reconnect with backoff.
- Keep the payload tiny: `{seq, tenant_id, alert_id, severity}`.

Fallback: a per-instance, group-less Kafka consumer in query-api, with the same `seq` backfill and emission gated on the Postgres row existing.

### 2. Alert list source: Postgres is the list index (confirmed)

- Denormalize `created_at`, `severity` and `rule_id` into `alert_workflow` next to `status`, `assignee`, `version` and `seq`.
- Drive all alert list endpoints from Postgres with the `(created_at, alert_id)` cursor. Use one path, not a two-path split.
- ClickHouse serves evidence, the detail view and aggregates.
- Spec §7.3 framing for the ADR: ClickHouse is the system of record for immutable detections. The Postgres row is a denormalized index of immutable facts plus workflow state, keyed by unique `alert_id`.
- Never use `ON CONFLICT DO UPDATE` for detections, because a replay would reopen resolved alerts.
- Write order: ClickHouse insert → Postgres transaction (workflow row plus notify) → offset commit.
- A PATCH or detail request that races ahead of the writer gets a defined retryable condition, not an opaque 404.
- Decide in Phase 3, because the schema and writer order depend on it. Phase 4 consumes it.

### 3. Window model (ADR before any rule code, Phase 3)

**State and recovery**
- In-memory per-partition state, rebuilt by replay.
- Commit a state pin: `min(oldest record needed for open window/cooldown state, next offset after the last produce-acked alert)`.
- Evict state on `OnPartitionsRevoked` and `OnPartitionsLost`.

**Evaluation**
- Epoch-aligned tumbling windows, `floor(occurred_at/size)*size`.
- Windows are half-open `[start, end)` and the threshold test is `>=`.
- Fire when the threshold is crossed, not at window close.

**Dedupe key**
- `dedupe_key = tenant|rule_id|entity_key|window_start`.
- Build it with one canonical function: length-prefixed or JSON-canonical, UTC, millisecond precision, SHA-256 hex.
- `rule_version` stays out of the key. The ADR must state what a rule upgrade means under replay.
- `created_at` comes from event time, never `time.Now()`.

**Cooldown:** a per-(rule, entity) `last_alert_window_end`, measured in event time.

**Lateness**
- Per-partition watermark, `max(occurred_at) - 30s`.
- Late events are dropped and counted in `late_events_total{rule_id}`.
- Test exactly at the boundary and at boundary+1ms.
- Idle partitions are not advanced by wall clock.

**Why tumbling over sliding:** taking the bucket from the triggering event makes `alert_id` depend on arrival order. Epoch-aligned tumbling gives the same result for any arrival order within the lateness bound.

**Known limitation:** a burst straddling a window boundary can be split and missed. Document it and test it. The fallback is hopping windows (slide = size/5) plus cooldown, and the ADR must decide this explicitly.

**Accepted consequences**
- Detector committed-offset lag is permanently inflated by about `max(window + lateness + cooldown)`.
  - Lag metrics must be position-based: `HighWatermark - last_processed_offset`.
  - Committed lag, if exported, gets a different name and never pages.
- Restart catch-up takes time proportional to `horizon × partition rate`. Measure it in Phase 5.
- Events-topic retention must exceed the horizon plus the longest tolerated outage.
- Never change the events topic partition count while detector state matters.

## Other Items to Carry Forward

- **Go bump:** franz-go v1.22.1 declares `go 1.26.0`. Move `go.mod` from 1.25.5 to 1.26.x and use `golang:1.26.9`. Phase 1.
- **Boilerplate removals:**
  - Clerk: replace with API-key and JWT middleware.
  - asynq: Kafka is the transport.
  - Resend, `packages/emails` and `backend/templates`: the spec has no email.
  - New Relic and its nr* integrations: replace with OTel and Prometheus.
  - `pkg/errors`: use the stdlib.
  - `packages/zod`, `packages/openapi`, Turborepo and bun: replace with `web/` and its own lockfile.
  - Taskfile: replace with a root Makefile.
  - The in-memory hardcoded rate limiter: replace with the Redis token bucket.
  - `cmd/sentinel`: replace with five binaries under `backend/cmd/*`.
  - Workers must not import the HTTP-oriented `server.Server`.
- **Boilerplate defects to fix on code that stays:** Recover middleware order, the `Shutdown` nil pointer, the Redis ping failure that startup ignores, DSN escaping, and `godotenv` gated to local only.
- **Boilerplate to keep and bump:** Echo, koanf, pgx, tern, zerolog, go-redis, validator, testify and testcontainers.
- **Atomic-batch caveat:**
  - A 500-event batch fans out across partitions, so "atomic" holds only for the response. A 5xx means zero, some or all of the records may be published.
  - Clients must send a stable `event_id`, or the server derives one deterministically.
  - Put this in the OpenAPI contract and the README. Decide it in Phase 1 contracts.
- **Event-time `created_at`:** use the triggering event time or the window end. Keep wall-clock time in a separate non-key `detected_at`, used only for the delay metric.
- **FEATURES gaps (small ADRs by Phase 4):**
  - `assignee` has no endpoint. Treat it as stored-only in v1.
  - There are no admin key-management routes. Recommend seed/CLI-only provisioning.
  - The dashboard login flow is undefined. Recommend a JWT in the header, consistent with fetch-based SSE.
  - `GET /rules` should show enabled/disabled state.

## Key Findings

### Recommended Stack (versions verified 2026-10-10)

**Go backend**
- Go 1.26.x.
- franz-go v1.22.1, with kadm v1.19.0 and kotel v1.7.1. Rejected: confluent-kafka-go (cgo) and segmentio (no idempotent producer).
- clickhouse-go v2.49.0 native `PrepareBatch`.
- pgx v5.11.0 with tern.
- go-redis v9.23.0 with a custom Lua token bucket using Redis `TIME`. Redis-down policy is configurable, default fail-closed 503.
- Echo v4.16.0.
- client_golang v1.25.0 on a private registry.
- OTel v1.47.0 for traces only.
- zerolog and koanf.

**Contracts and validation**
- Hand-authored OpenAPI 3.1 is the contract.
- The ingest hot path uses typed decode with `DisallowUnknownFields` plus explicit checks.
- `jsonschema/v6` runs in contract tests.

**Infrastructure pins:** `apache/kafka:4.3.1` (KRaft), ClickHouse 26.3 LTS, `postgres:18.6`, `redis:8.10.2`, Prometheus v3.15.0, Grafana 13.2.3, OTel Collector 0.162.0, kind v0.33.0, Kustomize.

**Frontend**
- React 19.3, Vite 8.3, TypeScript ~6.0 (not 7.x yet).
- TanStack Query 5, react-router-dom 7, Recharts 3.
- openapi-typescript and openapi-fetch.
- fetch plus `eventsource-parser` for SSE, because native EventSource can't send headers and tokens in URLs are forbidden.

**Generator:** Python 3.13, httpx, pydantic 2, orjson, uv, and a stdlib seeded RNG.

### Expected Features

**Must have (table stakes)**
- **Ingest:**
  - Hashed API-key batch ingest with strict validation, 429/503 limits and 202-after-ack.
  - Versioned event schema and a DLQ.
- **Detection:** four event-time rules (AUTH_BRUTE_FORCE, PORT_SCAN, REQUEST_FLOOD, DENY_SPIKE) with dedupe, cooldown, capped evidence, a lateness policy and replay safety.
- **Events:** event history with TTL and duplicate-safe writes, cursor pagination and event detail.
- **Triage and access:** triage with optimistic versioning and audit, server-side tenant isolation and RBAC, and dashboard auth.
- **Dashboard:** an SSE live feed with backfill, and Alerts, Events, Rules, Overview and System pages.
- **Operations:** real health/readiness, bounded-label metrics and JSON logs.
- **Tooling:** a seeded generator, one-command Compose, an OpenAPI contract, and CI with no `|| true`.

**Differentiators**
- Chaos and crash-recovery tests using failpoints.
- An honest benchmark report covering 1M+ events.
- Multi-replica detection and watermark handling.
- A detection-delay histogram, lag views, an investigation timeline and OTel traces across Kafka.
- K8s manifests, a threat model, ADRs and a runbook.

**Defer:** Aegis adapter (FR-17), ML scorer (FR-18), Sigma import, rule CRUD or hot reload, case management, correlation, enrichment, notification channels, tenant self-service, and any exactly-once or marketing throughput claim.

### Architecture Approach

Five Go binaries come from one module in `backend/cmd/*`. Keep the existing `backend/` module and record an ADR. Go tests go in `backend/test/*` behind build tags, and schemas live in `backend/internal/contracts/schema/` for `go:embed`.

**Major components**
1. `ingest`: resolve key to tenant, validate, Redis limit, then a synchronous idempotent produce.
2. `detector`: per-partition state, `ProduceSync`, commit the pin, no DB writes.
3. `event-writer`: batches of 5k-50k rows or 1s, then insert, then commit.
4. `alert-writer`: ClickHouse, then Postgres with notify, then commit.
5. `query-api`: REST, triage and a LISTEN-fed SSE hub. It does not consume Kafka.

**Libraries**
- `internal/detection`: pure, no I/O, no `time.Now()`.
- `internal/contracts`: the single `dedupe_key`/`alert_id` function.
- `internal/kafkax`: batch and partition consumers, pin tracker, DLQ, failpoints.

### Critical Pitfalls

1. **Offset committed before effects.**
   - Disable auto-commit and use one shared contiguous-watermark commit tracker.
   - Commit synchronously on revoke.
2. **Window state lost on restart.**
   - Use the state-pin commit and evict state on revoke and lost.
   - Test "9 events, kill, 10th event" and a rebalance when a third replica joins.
3. **Event/processing-time mixing and watermark poisoning.**
   - No `time.Now()` under `internal/detection`.
   - Per-partition watermark, guarded against far-future timestamps.
4. **ReplacingMergeTree treated as synchronous.**
   - Dedupe at read time: `LIMIT 1 BY`, a benchmarked `FINAL`, and `uniqExact` for counts.
   - Normalize `occurred_at` to `DateTime64(3)` before Kafka.
   - After a double insert, assert immediately without `OPTIMIZE`.
5. **List/status split and duplicate alerts on replay.**
   - Use the Postgres list index.
   - Fuzz the canonical key with `|`.
   - Resolved alerts must stay resolved after a detector restart.

Also required in the plan:
- SSE tested through a real proxy, with heartbeat, bounded client buffers and per-route write deadlines.
- Tenant scope as a compile-time required argument on every storage method.
- Batched ClickHouse inserts.
- Open-loop benchmarks with a reconciliation gate in `report.py`.
- Bounded Prometheus labels.
- A stable Testcontainers harness.

## Implications for Roadmap

Follow the spec §14 order. Phase 7 (Aegis and ML) stays deferred.

### Phase 1: Foundations and Contracts
- Go 1.26 bump, boilerplate removals and defect fixes.
- `internal/contracts` (canonical key, schemas, fixtures).
- Compose with KRaft: dual listeners, RF=1 overrides, fixed `CLUSTER_ID`, healthchecks, `topic-init` with explicit partitions, auto-create off.
- Migrations, Makefile, CI plus a Testcontainers harness, an OpenAPI skeleton, and the layout and metrics-label ADRs.
- Settle the event_id policy here.

### Phase 2: Ingestion and Durable Event Storage
- `kafkax` producer and batch consumer with DLQ.
- `ingest`: hashed keys, a tenant scope type and the Lua limiter.
- `event-writer` and the ClickHouse schema with read-time dedupe and an `event_id` lookup.
- A minimal events endpoint and the seeded generator.
- FR-01, 02, 03, 09, 12, 13.

### Phase 3: Detection and Alert Pipeline
1. Write the window-model and list-source ADRs first.
2. Build the pure engine, with determinism, boundary and straddle tests.
3. Add failpoints and the Kafka runner with the pin tracker and crash-point tests.
4. Add `alert-writer`, the `alert_workflow` migration with `seq`, DLQ completion, and the late/dropped/DLQ counters.

Covers FR-04, 05, 11.

### Phase 4: Query API and Analyst Dashboard
- JWT auth and RBAC, plus the login, assign, admin-key and rules ADRs.
- A Postgres-driven alert list with HMAC-bound cursors.
- PATCH with version check and audit in one transaction.
- LISTEN-fed SSE with `seq` backfill.
- React pages and a cross-tenant test matrix.
- FR-06, 07, 08.

### Phase 5: Observability, Resilience and Scale
- Metrics, position-based lag, the delay histogram, Grafana as code, and OTel through Kafka headers.
- Multi-replica, recovery and overload tests.
- A benchmark report with three-way accounting and a dedupe-cost comparison.
- FR-10, 15, 16.

### Phase 6: Kubernetes, Hardening, Documentation
- Kustomize with a kind overlay and a 3-node Kafka (RF=3, min ISR 2).
- Real-readiness probes, resource limits and ingress SSE settings.
- Threat model, runbook and a README claim audit.
- FR-14.

**Pulled earlier than the spec implies:** DLQ and read-time dedupe go in Phase 2, failpoints and the late/dropped/DLQ counters in Phase 3, and the tenant scope type in Phase 2.

### Research Flags

**Needs `--research-phase`:**
- **Phase 1:** the OpenAPI 3.1 Go toolchain (oapi-codegen v2.8.0 and kin-openapi; libopenapi as the fallback), the exact ClickHouse 26.3 tag and digest, and Vite 8, Vitest 5, MSW 3 and TypeScript peer compatibility.
- **Phase 2:** the ClickHouse sort key and dedupe design, the `event_id` index, the dedup window setting, and franz-go API names.
- **Phase 3:** the deepest need. The window and pin ADR, the `CommitOffsetsSync` signature and the failpoint mechanism.
- **Phase 4:** pgx `WaitForNotification` reconnect, SSE behind a real proxy, and an auth spike (header JWT vs cookie).
- **Phase 5:** the open-loop benchmark method and the cost of `FINAL` at the pinned version.
- **Phase 6:** Kafka on K8s (plain StatefulSet vs Strimzi), the kindest/node tag and ingress SSE annotations.

**Standard patterns, skip research:**
- The Compose, Makefile and CI skeleton in Phase 1.
- REST layering, optimistic updates and React pages in Phase 4, except SSE and auth.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | MEDIUM-HIGH | Versions verified. Library behavior from training knowledge. Unverified: otelpgx, redisotel, Tempo/Jaeger, aiohttp, mypy, kindest/node, OpenAPI 3.1 in Go tools |
| Features | MEDIUM | Anchored in the spec. Competitor comparisons not re-verified. Three spec gaps found |
| Architecture | MEDIUM-HIGH | franz-go lifecycle verified via Context7. ClickHouse dedupe, the pin design and NOTIFY semantics are reasoned and must be proven by Phase 3-5 tests |
| Pitfalls | MEDIUM | Established practice. Version-sensitive defaults marked [verify] |

**Overall confidence:** MEDIUM-HIGH

### Gaps to Address

- Whether a tumbling-window straddle miss is acceptable (hopping windows as the fallback).
- Rule-version change semantics under replay.
- Detector restart cost (`horizon × partition rate`), not yet measured.
- OpenAPI 3.1 Go toolchain.
- ClickHouse specifics:
  - `FINAL` vs `LIMIT 1 BY` cost.
  - `non_replicated_deduplication_window`.
  - TTL with monthly partitions (consider daily).
  - The exact image tag.
- Proof that NOTIFY reconnect plus backfill works.
- Client-supplied vs derived `event_id` policy.
- Open FEATURES gaps (assign, admin keys, dashboard login).
- Benchmark limits (single machine, generator saturation) to record in the report. No throughput number without a linked result file.

## Sources

- `.planning/research/STACK.md`: Go module proxy, npm, PyPI, GitHub releases and Docker Hub, checked 2026-10-10.
- `.planning/research/ARCHITECTURE.md`: franz-go docs via Context7, plus reasoning from the spec's constraints.
- `.planning/research/FEATURES.md`: spec mapping, with domain comparisons to Elastic Security, Wazuh, Sigma and Panther (not re-verified).
- `.planning/research/PITFALLS.md`: established streaming and ClickHouse practice, plus `.planning/codebase/CONCERNS.md`.

---
*Research completed: 2026-10-10*
*Ready for roadmap: yes*
