# Stack Research

**Domain:** Real-time security event ingestion / streaming detection / alerting platform (Kafka + Go + ClickHouse + PostgreSQL + Redis + React)
**Researched:** 2026-10-10
**Confidence:** MEDIUM-HIGH. Library and image versions were checked against the Go module proxy, npm, PyPI, GitHub releases and Docker Hub on 2026-10-10 (HIGH for "this is the latest version"). Behavioural claims come from training knowledge of the libraries and were not re-verified against docs (MEDIUM). Items marked LOW need confirmation in the phase that uses them.

The technology layers are mandated by spec.md section 2. This file only chooses libraries and versions within them.

## Headline Decisions

1. **Bump the Go toolchain from 1.25.5 to 1.26.x.** `franz-go v1.22.1` declares `go 1.26.0` in its go.mod (verified). Use `golang:1.26.9` images. `golang:1.27.2` also exists, but 1.26 is the safer pin.
2. **Kafka client is franz-go (`twmb/franz-go`).** It is pure Go, has an idempotent producer with acks=all by default, gives explicit control over offset commits, and ships `kadm`, `kotel` and `kprom` add-ons.
3. **Remove Clerk, asynq, Resend, New Relic and the whole TS `packages/` and Turborepo/bun layer.** Details are in the Boilerplate Disposition section.
4. **Rate limiter is a small custom Lua token bucket on go-redis.** `redis_rate` is stale (last release 2023).
5. **The OpenAPI 3.1 toolchain is the main risk.** Use the OpenAPI document as the contract and for the TS client. Validate the event hot path with typed decoding plus JSON Schema 2020-12 in contract tests. Do not assume oapi-codegen has full 3.1 support (see below, LOW).

## Recommended Stack

### Core Technologies (backend)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| Go | 1.26.x (image `golang:1.26.9`) | All services, single module, multiple binaries | Required by franz-go v1.22.1 (go 1.26.0). clickhouse-go needs only 1.25. |
| `github.com/twmb/franz-go` (kgo) | v1.22.1 (2026-09-27) | Kafka producer and consumers | Pure Go, so no cgo and static distroless binaries. Idempotent producer and acks=all are on by default. `DisableAutoCommit` with `CommitRecords` / `CommitMarkedOffsets` gives commit-after-effect. `BlockRebalanceOnPoll` makes cooperative-rebalance safety explicit. `MaxBufferedRecords` / `MaxBufferedBytes` give the bounded producer queue the spec requires. Actively maintained. |
| `github.com/twmb/franz-go/pkg/kadm` | v1.19.0 | Lag computation (`Lag`), topic creation, group admin | Feeds `sentinel_kafka_consumer_lag{group,topic,partition}` and the System page. |
| `github.com/twmb/franz-go/plugin/kotel` | v1.7.1 | OTel tracing and metrics hooks for kgo | Trace propagation through Kafka headers (ingest to detector). |
| `github.com/ClickHouse/clickhouse-go/v2` | v2.49.0 (2026-10-08) | ClickHouse native-protocol client | Use the native API (`clickhouse.Open`, `conn.PrepareBatch`, `batch.Append`, `batch.Send`), not `database/sql`. Native batches are columnar and faster, and they allow per-insert settings via `clickhouse.Context(ctx, clickhouse.WithSettings(...))`. |
| `github.com/jackc/pgx/v5` (+ `pgxpool`) | v5.11.0 | PostgreSQL: alert workflow state and audit | Already in the scaffold (5.9.2). Bump. Supports transactions and `pgx.CollectRows`, which fit the optimistic version check on `UPDATE ... WHERE version = $n`. |
| `github.com/jackc/tern/v2` | v2.4.3 | Postgres migrations | Already wired. Keep it rather than swapping runners. |
| `github.com/golang-migrate/migrate/v4` | v4.20.1 | ClickHouse migrations (`migrations/clickhouse/`) | Has a ClickHouse driver. Only add it if a tiny custom runner (ordered `.sql` files plus a `schema_migrations` table) is not preferred. MEDIUM. |
| `github.com/redis/go-redis/v9` | v9.23.0 (2026-10-05) | Redis client, Lua token bucket | Already in the scaffold (9.14.1). Bump. `redis.NewScript` handles EVALSHA with a fallback. |
| `github.com/labstack/echo/v4` | v4.16.0 (2026-09-27) | HTTP framework for ingest and query-api | Already in the scaffold (4.15.2). Keep it, because swapping frameworks buys nothing. |
| `github.com/prometheus/client_golang` | v1.25.0 (2026-10-08) | `/metrics` and the spec section 11.2 metric names | Use a dedicated `prometheus.Registry` (not the global default). Wire it with `promhttp.HandlerFor`. Register Go and process collectors. Use `NewHistogramVec` with explicit buckets. |
| OpenTelemetry Go | `go.opentelemetry.io/otel` + `sdk` + `otel/trace` v1.47.0 | Distributed tracing | Traces only through OTel. Metrics stay on client_golang to avoid two metric pipelines. Add `exporters/otlp/otlptrace/otlptracegrpc` at the same version. |

### Supporting Libraries (backend)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho` | v0.71.0 | Echo server spans | Ingest and query-api middleware. Keep it after the request-ID and recover middleware. |
| `github.com/exaring/otelpgx` | latest (version NOT verified, LOW) | pgx query tracing | Replaces `nrpgx5`. |
| `github.com/redis/go-redis/extra/redisotel/v9` | match go-redis v9.x (LOW) | Redis tracing | Replaces `nrredis-v9`. |
| `github.com/getkin/kin-openapi` | v0.149.0 (2026-08-28) | OpenAPI document loading, request validation middleware, spec self-check in CI | Use the `openapi3` loader in tests to prove `api/openapi.yaml` parses. Use `openapi3filter` only if it is confirmed to handle 3.1 (see Risks). |
| `github.com/oapi-codegen/oapi-codegen/v2` | v2.8.0 (2026-07-17) | Generates Go server interface and types from OpenAPI | Run as a pinned `go tool` (Go 1.24+ `tool` directive). Use the `echo` server plus strict-server option. Confirm 3.1 handling in Phase 1 (LOW). |
| `github.com/oapi-codegen/echo-middleware` | v1.1.0 | Request validation in Echo using kin-openapi | Optional. |
| `github.com/santhosh-tekuri/jsonschema/v6` | v6.0.3 | JSON Schema 2020-12 validation of `event.v1.json` and `alert.v1.json` | Contract tests (`tests/contracts/`) and optionally the ingest path. Compile schemas once. |
| `github.com/pb33f/libopenapi` | v0.41.5 (2026-10-07) | Full OpenAPI 3.1 document model | Fallback if kin-openapi or oapi-codegen cannot handle 3.1. Do not use it by default, since it needs more glue code. |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | Dashboard session JWTs | Short-lived access token, HS256 or EdDSA, with explicit `WithValidMethods`. |
| `golang.org/x/crypto` | latest | argon2id or bcrypt for dashboard user passwords | Only if local users are stored in Postgres. |
| Stdlib `crypto/sha256`, `crypto/subtle` | n/a | API-key hashing and constant-time comparison | Hash the key (keys are high-entropy, so a plain SHA-256 lookup index is fine). Never store the plaintext. |
| `github.com/google/uuid` | v1.6.0 | UUIDv7 `event_id` (`uuid.NewV7`) | Time-ordered IDs match the spec example (`018ff000-...-7000-...`). |
| `github.com/knadh/koanf/v2` | v2.3.8 | Config. Add the YAML file provider for `config/rules.yaml`. | Keep it. Env provider for services, `file`+`yaml` provider for the rules. |
| `github.com/rs/zerolog` | v1.35.1 | JSON logs | Keep it. Add the `trace_id` / `request_id` hook. |
| `github.com/go-playground/validator/v10` | v10.30.2 | Config validation and field checks | Keep it. Use it for the config struct and the rules YAML. |
| `github.com/stretchr/testify` | v1.12.1 | Assertions | Keep it. |
| `github.com/testcontainers/testcontainers-go` (+ `modules/kafka`, `modules/clickhouse`, `modules/postgres`, `modules/redis`) | v0.44.0 | Real-dependency integration tests | Spec section 15 forbids mock-only testing. Pin all modules to one version. |
| `golang.org/x/time/rate` | latest | In-process throttles only (for example DLQ publish or log sampling) | Not for ingest rate limiting. |

### Core Technologies (generator, dashboard)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| Python | 3.13 (`python:3.13-slim-trixie`) | Generator and `bench/report.py` | 3.13 has the widest wheel coverage. 3.14.8 exists, so use it only if every dependency has wheels. |
| `httpx` | 0.28.1 | Async HTTP client for the generator (`httpx.AsyncClient` with limits) | Simple async API, and connection pooling and timeouts are explicit. A 1M-event run at 500 events per batch is only about 2,000 requests, so aiohttp's speed edge is irrelevant. aiohttp stays as the fallback if the generator itself becomes the bottleneck (its version was NOT verified). |
| `pydantic` | 2.14.0 | Scenario YAML models and the event model mirroring the JSON Schema | Validates scenarios up front. Generator output is checked against the same schema as the Go side. |
| `orjson` | 3.13.0 | Fast JSON encoding | Keeps the generator off the critical path. |
| `random.Random(seed)` (stdlib) | n/a | Deterministic traffic | Use a seeded stdlib RNG with explicit per-stream sub-seeds. Do not depend on numpy for reproducibility. |
| `typer` | 0.27.3 | Generator CLI (`--scenario`, `--seed`, `--rate`) | Optional. argparse is fine. |
| `uv` | latest | Lockfile and environment | Gives pinned, reproducible `uv.lock`. Replaces a bare `requirements.txt`, though the spec lists one, so export it with `uv export`. |
| `pytest` 9.1.1, `ruff` 0.17.0, `mypy` | pytest/ruff as listed; mypy version NOT verified | Python tests and lint | Seed-reproducibility tests are required by spec section 15. |
| React | 19.3.0 | UI | Matches the scaffold's React 19. |
| Vite | 8.3.4 with `@vitejs/plugin-react` 6.1.2 | Build and dev server | Current majors (MEDIUM). Check peer-dependency compatibility at scaffold time. |
| TypeScript | pin `~6.0.x` | Typecheck | npm shows `typescript 7.0.2` as latest (the native-compiler line). The root of this repo already uses ^6.0.3. Stay on 6.x until typescript-eslint and the Vite/Vitest toolchain are confirmed on 7 (LOW). Re-evaluate in Phase 6. |
| `@tanstack/react-query` | 5.104.1 | Server state | Spec section 9 asks for React Query or equivalent. Use `placeholderData: keepPreviousData` for paging. |
| `openapi-typescript` | 7.13.0 | Generates `schema.d.ts` from `api/openapi.yaml` | Supports OpenAPI 3.0 and 3.1 and is type-only, so there is no runtime generator to trust. |
| `openapi-fetch` | 0.17.0 | Typed `fetch` client | Tiny. Typed paths, bodies and errors. |
| `openapi-react-query` | 0.5.4 | TanStack Query bindings over openapi-fetch | Keeps the typed client in one place. Pre-1.0, so be ready to call openapi-fetch from your own hooks. |
| `react-router-dom` | 7.18.4 | Routes (`/`, `/alerts`, `/events`, `/rules`, `/system`) | The five fixed routes do not need TanStack Router's type-safe route machinery. |
| `recharts` | 3.10.1 | Overview trend, rule breakdown, lag charts | React-native components with accessible markup, fine for the small series (24-hour trend, per-rule counts). Use `uplot` 1.6.32 only if a panel needs more than a few thousand points. |
| `eventsource-parser` | 4.1.1 | SSE parsing over `fetch` streaming | Native `EventSource` cannot send an `Authorization` header, and the spec forbids tokens in the URL. Stream with `fetch` and parse with this library. Handle reconnect, `Last-Event-ID` and REST backfill in a small hook. `@microsoft/fetch-event-source` 2.0.1 does the same but is unmaintained, so do not use it. |
| Tailwind CSS | 4.3.3 | Styling | Optional but quick for the responsive and accessible UI. |
| Vitest 5.0.3, Testing Library, MSW 3.0.3, Playwright 1.64.0 | as listed | Unit, API error-state and e2e tests | MSW and Playwright versions are the latest published. Peer compatibility with Vitest 5 and Vite 8 is unconfirmed (LOW). |
| ESLint | 10.12.0 | Lint | Use flat config only. |

### Infrastructure (images and local K8s)

Pin by exact tag plus digest in Compose and the manifests.

| Technology | Pin | Purpose | Notes |
|------------|-----|---------|-------|
| Apache Kafka | `apache/kafka:4.3.1` | Event and alert transport, KRaft | Official image, KRaft only (no ZooKeeper). 4.3.1 is the latest non-RC tag (4.3.2 and 4.4.0 are RCs; 4.2.2 is the previous line). Compose uses one combined broker/controller node, with `KAFKA_PROCESS_ROLES=broker,controller`, `OFFSETS_TOPIC_REPLICATION_FACTOR=1`, `TRANSACTION_STATE_LOG_*=1`, and `KAFKA_MIN_INSYNC_REPLICAS=1`. That is not HA, as the spec says. K8s uses three nodes, RF=3, `min.insync.replicas=2`. |
| ClickHouse | `clickhouse/clickhouse-server:26.3` (LTS line) | Event and alert analytics | The `26.3` tag exists (verified). The GitHub release is `v26.3.47.6-lts`, but the full tag `26.3.47.6` returned 404 on Docker Hub, so resolve the exact tag and digest at implementation time. 26.9 is the newest non-LTS line. Prefer LTS for a reproducible demo. Use the `-alpine` variant if image size matters. |
| PostgreSQL | `postgres:18.6` | Alert workflow state and audit | 18.6 is the newest stable (19 is beta). 17.11 is the conservative alternative. pgx works with both. |
| Redis | `redis:8.10.2` | Token-bucket rate limiting | Verified tag. Redis 8 is multi-licensed (including AGPLv3), which is fine for a local portfolio demo. Valkey would also work (same protocol) but is not mandated, so it is not recommended. |
| Prometheus | `prom/prometheus:v3.15.0` | Metrics | Verified. |
| Grafana | `grafana/grafana:13.2.3` | Dashboards | Verified. Provision datasources and dashboards from `observability/grafana/` as code. |
| OpenTelemetry Collector (contrib) | `otel/opentelemetry-collector-contrib:0.162.0` | Receives OTLP traces | Release verified; confirm the image tag. |
| Trace backend | Grafana Tempo or Jaeger (version NOT verified, LOW) | Trace viewing | Pick one. Tempo keeps Grafana as the single UI. |
| kind | v0.33.0 (2026-08-26) | Local K8s | Use the node image listed in the kind v0.33.0 release notes (kindest/node tags must match the kind release; NOT verified, LOW). Latest Kubernetes is v1.37.1. |
| kubectl + Kustomize | match the kind node | Manifests | Use plain manifests with Kustomize overlays (`compose`-like base plus `kind` overlay). Helm v4.3.0 is current if a chart is wanted instead, but spec FR-14 accepts either, and Kustomize has fewer moving parts. |
| Kafka on K8s | Plain StatefulSet of `apache/kafka:4.3.1` (3 nodes), or the Strimzi operator (version NOT verified, LOW) | Multi-broker demonstration | A plain StatefulSet is lighter on kind. Strimzi is the production-standard route but is a heavy install. Needs phase-specific research in Phase 6. |
| ClickHouse on K8s | Single-node StatefulSet | Demo | The Altinity operator or the ClickHouse operator is overkill for kind. |
| Node | 26.11.1 (`node:26-alpine3.23`) or 22 LTS | Frontend build only | The scaffold requires >=22. Use an LTS line in CI. |

## Per-Question Findings

### Kafka client: franz-go, not confluent-kafka-go or segmentio

| Criterion | franz-go v1.22.1 | confluent-kafka-go v2.16.0 | segmentio/kafka-go v0.4.51 |
|-----------|------------------|----------------------------|----------------------------|
| cgo / static binary | Pure Go | Needs librdkafka (cgo, slow builds, breaks distroless/static images) | Pure Go |
| Idempotent producer | Yes, on by default | Yes | No (not supported as of my knowledge; LOW) |
| acks=all | Default `AllISRAcks` | Yes | Yes (`RequireAll`) |
| Manual commit after effects | Yes (`DisableAutoCommit`, `CommitRecords`, `CommitUncommittedOffsets`) | Yes | Yes (`FetchMessage` + `CommitMessages`) |
| Cooperative rebalancing | Yes | Yes | Weak |
| Bounded produce buffer | `MaxBufferedRecords`, `MaxBufferedBytes` | `queue.buffering.max.*` | Limited |
| OTel / Prometheus plugins | `kotel`, `kprom` | Third party | Third party |
| Release cadence | Frequent (latest 2026-09-27) | Frequent, tracks librdkafka | Slow (last release 2026-04-23) |

Producer config for ingest (the spec requires 202 only after Kafka ack):
- Use `ProduceSync` or a callback wait on each batch, with `RequiredAcks(kgo.AllISRAcks())`.
- Leave idempotency enabled. Do not call `DisableIdempotentWrite`; it is required for acks=all and bounded retries.
- Set `RecordRetries`, `RecordDeliveryTimeout` and `ProduceRequestTimeout` explicitly.
- Set `ProducerLinger` and `ProducerBatchMaxBytes` from benchmarks, not guesses.
- Set `MaxBufferedRecords` to apply backpressure, mapped to 503/429 when `kgo.ErrMaxBuffered` is returned.
- Set `RecordPartitioner` to a key partitioner (default sticky-key hashing is consistent per key).
- Atomic all-or-nothing batches: produce all records, and if any record fails, return 503. Retries are safe because the event IDs are stable and the idempotent producer and the downstream dedupe cover duplicates.

Consumer config for detector, event-writer and alert-writer:
- `ConsumerGroup`, `ConsumeTopics`, `DisableAutoCommit()`, `BlockRebalanceOnPoll()`, and `kgo.OnPartitionsRevoked` for flushing and committing before the handoff.
- Poll with `PollFetches` or `PollRecords(n)`, process, then `CommitRecords` only after the ClickHouse insert, alert publish or DLQ publish succeeds.
- Use `kgo.FetchIsolationLevel(ReadCommitted())` only if you adopt transactions. You do not need them.
- Prefer the cooperative-sticky balancer, which is the default in recent franz-go.
- Do not use franz-go's exactly-once transactional sessions. The spec forbids an end-to-end exactly-once claim.
- Lag metric: use `kadm.Client.Lag` (group offsets vs end offsets) sampled on an interval. Label only by group, topic and partition.

Confidence: HIGH on versions and the Go 1.26 requirement (read from go.mod). MEDIUM on API names (from memory of the library; check pkg.go.dev when writing the code).

### ClickHouse: clickhouse-go v2 native batches

- Use `clickhouse.Open(&clickhouse.Options{Protocol: clickhouse.Native, ...})`, a connection pool with `MaxOpenConns`, `DialTimeout`, `ReadTimeout`, and LZ4 compression.
- Use `conn.PrepareBatch(ctx, "INSERT INTO events (...)")`, `batch.Append(...)`, `batch.Send()`. A batch is not safe for concurrent use. Build one per insert.
- Type `source_ip` / `destination_ip` as `IPv6` or `String`, `occurred_at` as `DateTime64(3, 'UTC')`, enums as `LowCardinality(String)`, `attributes_json` as `String` (cap its size on ingest).
- Dedupe pattern (spec 7.2). Choose in Phase 2:
  - Table: `ReplacingMergeTree` ordered by `(tenant_id, occurred_at, event_id)`, partition `toYYYYMM(occurred_at)`. Merges are asynchronous, so read queries must dedupe (`LIMIT 1 BY event_id`, or `FINAL` with a benchmarked cost).
  - Insert retries: set `insert_deduplication_token` per batch, derived deterministically from `topic-partition-firstOffset-lastOffset`. Non-replicated MergeTree only deduplicates inserts when `non_replicated_deduplication_window` is set on the table, so set it (check the setting name against the ClickHouse docs, MEDIUM). Replicated tables dedupe by default.
  - Because a re-poll after a crash may form different batch boundaries, the token alone is not sufficient. That is why the read-time dedupe remains mandatory.
- Do not use `async_insert` for the event-writer. The writer already batches, and async insert makes error handling and the batch-token semantics less clear. It is a fallback if the writer is the bottleneck.
- Batch size: cap rows and bytes (for example 5-10k rows or 1 s flush), then measure.
- Tenant-scope every query, cap time ranges, and use named or positional parameters (`clickhouse.Named`), never string interpolation.

Confidence: HIGH on version. MEDIUM on dedupe-setting details (verify against ClickHouse docs in Phase 2).

### PostgreSQL: pgx v5.11.0

- `pgxpool.New` with explicit `MaxConns`, `MinConns`, `HealthCheckPeriod`, `MaxConnLifetime`. Build the DSN with `pgx.ParseConfig` / `url.URL` with escaping (the boilerplate builds the connection string unescaped, which is a known defect).
- Tables: `alert_workflow(alert_id PRIMARY KEY, tenant_id, status, assignee, version, updated_at)` and `alert_audit(...)`.
- Create the workflow row lazily and idempotently with `INSERT ... ON CONFLICT (alert_id) DO NOTHING`. Update with `UPDATE ... SET version = version + 1 WHERE alert_id = $1 AND tenant_id = $2 AND version = $3`, and treat zero affected rows as a 409.
- Keep tern for migrations. Add `otelpgx` for tracing and keep `pgx-zerolog` for query logging.

Confidence: HIGH.

### Redis rate limiting: custom Lua on go-redis v9.23.0

- Implement the token bucket as one Lua script (`HMGET` tokens/ts, refill from `redis.call('TIME')`, deduct, `HSET`, `PEXPIRE`), loaded with `redis.NewScript`. Return `allowed`, `remaining` and `retry_after_ms` so the 429 can carry `Retry-After`.
- Key per API key (not per IP): `rl:{tenant}:{key_id}`. Use `redis.call('TIME')` inside the script, not the client clock, so replicas agree.
- Why not `go-redis/redis_rate/v10` (v10.0.1, 2023-04-02): it implements GCRA, not a token bucket, and has had no release in over three years. It still works and would be acceptable, but a ~30-line script gives the exact semantics the spec describes and is easy to unit test with the testcontainers Redis module.
- Redis outage policy: make fail-open versus fail-closed a config value. Recommend fail-closed with 503 for ingest by default, since the spec says an outage must not silently bypass controls. Use a short client timeout (for example 50-100 ms) and a circuit breaker so ingest latency is not tied to Redis health.
- Fix the boilerplate defect: startup must not continue when the Redis ping fails (unless fail-open is explicitly configured).

Confidence: HIGH on versions. MEDIUM on the script design (standard pattern).

### Prometheus client_golang v1.25.0

- Use a private registry and a `Metrics` struct per binary, constructed once and injected.
- Metric names are fixed by spec section 11.2. Histograms need explicit buckets (ingest latency: 5 ms to 5 s; ClickHouse write: 10 ms to 30 s; detection delay: 0.1 s to several minutes, since it includes event-time lateness).
- Never label by tenant, IP, user, event ID or request ID. Normalise HTTP status into a bounded set and use route templates, not raw paths.
- Serve `/metrics` on a separate internal port or behind network policy, not on the public listener.
- Add kprom (franz-go) for client-level Kafka metrics if useful.

Confidence: HIGH.

### OpenTelemetry Go SDK v1.47.0

- Traces only through OTLP/gRPC to the collector. Set a `ParentBased(TraceIDRatioBased(x))` sampler configured via env. Set `service.name` per binary.
- Propagate context through Kafka headers with `kotel`, so one trace spans ingest, publish, consume, detect and write. This is the spec's "spans around ingestion, publishing, detection, storage".
- Add the `trace_id` to zerolog lines via a hook that reads the span context.
- ClickHouse has no official auto-instrumentation, so add manual spans around `PrepareBatch`/`Send`.
- Package versions for the OTel contrib and instrumentation modules move independently (otelecho is v0.71.0 while the core is v1.47.0). Let `go mod tidy` resolve them and commit the result.

Confidence: HIGH on core and otelecho versions. LOW on otelpgx / redisotel versions (not verified).

### JSON Schema and OpenAPI 3.1 validation

This is the area with the most uncertainty.

| Tool | Version | Role | Notes |
|------|---------|------|-------|
| `api/openapi.yaml` (hand-authored, 3.1) | n/a | Source of truth for REST contract | Spec section 8 requires a published and validated OpenAPI 3.1 document. |
| `santhosh-tekuri/jsonschema/v6` | v6.0.3 | JSON Schema 2020-12 validator | 3.1 is aligned with 2020-12, so this validates the event and alert schemas exactly. HIGH confidence it supports 2020-12. |
| `oapi-codegen/v2` | v2.8.0 | Go types and Echo handlers | Historically built on kin-openapi, which targets 3.0. Native 3.1 support is not confirmed (LOW). Check release notes in Phase 1. If it cannot read a 3.1 file, author the types by hand for the five routes plus use `openapi-typescript` for the client, or keep the spec in the 3.0-compatible subset of 3.1 (avoid `type: [x, "null"]`, `const`, `$schema` dialect keywords). |
| `kin-openapi` | v0.149.0 | Spec loading, `openapi3filter` request validation | Same 3.1 caveat (LOW). |
| `libopenapi` (+ `libopenapi-validator`) | v0.41.5 | True 3.1 model and validation | Use it as the fallback if the above cannot handle 3.1. |

Recommendation:
1. Event ingest hot path: decode into a typed struct with `json.Decoder.DisallowUnknownFields()` behind `http.MaxBytesReader` (1 MiB), then run explicit field validation (IP parsing with `net/netip`, enum sets, attribute size, timestamp skew). This is fast, gives precise per-field errors, and is the most predictable path for 500 events per request.
2. Contract tests (`tests/contracts/`): validate valid and invalid example payloads against the JSON Schema with `jsonschema/v6`, and assert that the Go validators and the schema agree on the same corpus. That meets the spec's "use real JSON schema/OpenAPI validation or Go validators consistently".
3. Prove the OpenAPI file in CI: lint with Redocly CLI or vacuum (version NOT verified, LOW), and run `openapi-typescript` on it, which fails on invalid documents.

### SSE in Go

- No library. Implement it in an Echo handler with `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`, and `http.NewResponseController(w).Flush()`.
- Fan-out hub: one goroutine reads new alerts, and each client has a bounded buffered channel (for example 64). If the channel is full, drop that client and let it reconnect. Track the `sentinel_sse_clients` gauge.
- Send a `:keepalive` comment every 15-25 seconds. Emit `id:` as the `(created_at, alert_id)` cursor so `Last-Event-ID` works with the REST backfill endpoint.
- Detection of new alerts in query-api: consume `security.alerts.v1` with a per-instance consumer (a unique group, or direct partition assignment without commits), because every query-api replica must see all alerts. Do not use one shared group for this.
- Auth: standard `Authorization: Bearer` header, which works because the client uses fetch streaming (see the frontend). Disable the server write timeout for this route only (Echo/`http.Server.WriteTimeout` will otherwise kill long streams; use `ResponseController.SetWriteDeadline`).
- Do not use third-party SSE packages (tmaxmax/go-sse and similar); the handler is under 100 lines and the dependency adds nothing.

Confidence: HIGH on the approach (stdlib features). 

### Auth

- Producers: API keys with a prefix plus secret, stored as SHA-256 hash plus a `tenant_id`, `scopes` and `role`. Look up by key id, compare with `subtle.ConstantTimeCompare`.
- Dashboard: local user login issuing a short-lived JWT (golang-jwt v5.3.1), with roles `viewer`, `analyst`, `admin` in claims, and tenant derived from the verified token or key. A refresh flow or httpOnly cookie is a Phase 4 decision.
- Both paths resolve `tenant_id` in middleware and put it on the context. Handlers never read tenant from the body or query.

## Boilerplate Disposition

| Dependency / asset | Decision | Reason | Replacement |
|--------------------|----------|--------|-------------|
| `clerk-sdk-go/v2` | **Remove** | External SaaS identity. Spec requires scoped API keys or JWT, with tenant and roles from verified identity. | API-key and JWT middleware (`internal/auth`) |
| `asynq` | **Remove** | Redis job queue. Kafka is the transport. Spec says Redis is not a second bus, and its use is limited to rate limiting and ephemeral helpers. | Kafka consumers |
| `resend-go/v2`, `packages/emails` (React Email), `backend/templates` | **Remove** | No email in the spec. | None |
| `newrelic/go-agent/v3` (+ nrecho-v4, nrpgx5, nrredis-v9, nrpkgerrors) | **Remove** | Vendor APM. Spec mandates Prometheus + OTel. | `otelecho`, `otelpgx`, `redisotel`, `kotel`, client_golang |
| `pkg/errors` | **Remove** | Deprecated. | Stdlib `errors` and `fmt.Errorf("%w")` |
| `packages/zod`, `packages/openapi` (ts-rest, `@anatine/zod-openapi`), Turborepo, bun workspaces | **Remove** | The spec's contract flow is `api/openapi.yaml` to `openapi-typescript`. A second contract definition in Zod would drift from it. | `web/` with its own `package.json` and npm (or pnpm) lockfile |
| Echo v4 (4.15.2) | **Keep, bump to v4.16.0** | Works and is already wired. | n/a |
| koanf v2 (2.3.4) | **Keep, bump to v2.3.8; add YAML provider** | Config and rules YAML. | n/a |
| pgx v5 (5.9.2) | **Keep, bump to v5.11.0** | See above. | n/a |
| tern v2 | **Keep (v2.4.3)** | Postgres migrations. | n/a |
| zerolog (1.35.1), `pgx-zerolog` | **Keep** | Spec requires JSON logs. | n/a |
| go-redis v9 (9.14.1) | **Keep, bump to v9.23.0** | Rate limiting. | n/a |
| validator/v10 | **Keep** | Config and input validation. | n/a |
| testify, testcontainers-go (0.42.0) | **Keep, bump to v0.44.0; add kafka, clickhouse, redis modules** | Real integration tests. | n/a |
| `golang.org/x/time` + `middleware/rate_limit.go` (in-memory, hardcoded) | **Replace** | In-memory limiter is per replica and hardcoded. | Redis token bucket |
| `godotenv` | **Keep for local dev only** | Autoloading `.env` in production is risky. Gate on `SENTINEL_PRIMARY.ENV=local`. | n/a |
| `google/uuid` | **Keep** | UUIDv7 via `NewV7`. | n/a |
| Taskfile | **Replace** | Spec section 16 requires a root `Makefile` with `dev-up`, `test`, `lint`, `bench`, and so on. | Makefile |
| golangci-lint config | **Keep; run v2.14.0 in CI** | Spec requires lint. | n/a |

Also fix the known defects while touching these files: Recover middleware order, the `Shutdown` nil pointer, the Redis-ping-continue behaviour, and DSN escaping.

## Installation

```bash
# Go (from backend/, after bumping go.mod to go 1.26)
go get github.com/twmb/franz-go@v1.22.1 \
       github.com/twmb/franz-go/pkg/kadm@v1.19.0 \
       github.com/twmb/franz-go/plugin/kotel@v1.7.1 \
       github.com/ClickHouse/clickhouse-go/v2@v2.49.0 \
       github.com/jackc/pgx/v5@v5.11.0 \
       github.com/redis/go-redis/v9@v9.23.0 \
       github.com/labstack/echo/v4@v4.16.0 \
       github.com/prometheus/client_golang@v1.25.0 \
       go.opentelemetry.io/otel@v1.47.0 \
       go.opentelemetry.io/otel/sdk@v1.47.0 \
       go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho@v0.71.0 \
       github.com/santhosh-tekuri/jsonschema/v6@v6.0.3 \
       github.com/golang-jwt/jwt/v5@v5.3.1 \
       github.com/getkin/kin-openapi@v0.149.0 \
       github.com/golang-migrate/migrate/v4@v4.20.1
go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
# test-only
go get github.com/testcontainers/testcontainers-go@v0.44.0 \
       github.com/testcontainers/testcontainers-go/modules/kafka@v0.44.0 \
       github.com/testcontainers/testcontainers-go/modules/clickhouse@v0.44.0
go mod tidy
# Remove: clerk-sdk-go, asynq, resend-go, newrelic/go-agent, pkg/errors, nr* integrations

# Dashboard (web/)
npm install react@19.3.0 react-dom@19.3.0 react-router-dom@7.18.4 \
  @tanstack/react-query@5.104.1 openapi-fetch@0.17.0 openapi-react-query@0.5.4 \
  recharts@3.10.1 eventsource-parser@4.1.1
npm install -D vite@8.3.4 @vitejs/plugin-react@6.1.2 typescript@~6.0 \
  openapi-typescript@7.13.0 vitest@5.0.3 @playwright/test@1.64.0 eslint@10.12.0 \
  tailwindcss@4.3.3 msw@3.0.3
# Types:  npx openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts

# Generator (generator/)
uv add httpx==0.28.1 pydantic==2.14.0 orjson==3.13.0 typer==0.27.3
uv add --dev pytest==9.1.1 ruff==0.17.0 mypy
```

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| franz-go | confluent-kafka-go v2.16.0 | If you must use librdkafka-specific features or a Confluent-supported contract. Costs cgo and non-static images. |
| franz-go | segmentio/kafka-go v0.4.51 | Small, simple consumer-only tools. It lacks the idempotent producer, so it does not fit the producer requirement here. |
| Custom Lua token bucket | `redis_rate/v10` v10.0.1 | If GCRA semantics are acceptable and you want zero custom Lua. Treat it as unmaintained. |
| Typed decode + custom validation on the hot path | JSON Schema validation per request | If the schema becomes the single source and throughput tests show validation is not the bottleneck. |
| oapi-codegen + kin-openapi | libopenapi + libopenapi-validator | If 3.1-only features are needed and oapi-codegen cannot handle them. |
| httpx | aiohttp | If the generator cannot saturate the target ingest rate. |
| Recharts | uPlot 1.6.32 / ECharts 6.1.0 | Very dense time series (uPlot) or complex charts (ECharts). |
| react-router-dom | TanStack Router 1.170.42 | If you want fully type-safe search params for the filter UIs. |
| fetch + eventsource-parser | Native `EventSource` | Only if auth moves to an httpOnly cookie, which would let `EventSource` work without headers. |
| Kustomize manifests | Helm v4.3.0 chart | If a parameterised chart is a portfolio goal. FR-14 accepts either. |
| Plain Kafka StatefulSet | Strimzi operator | A production-style multi-broker story, at the cost of a heavier kind install. |
| ClickHouse 26.3 LTS | 26.9 (latest) | If a feature unique to 26.9 is needed. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| confluent-kafka-go | cgo and librdkafka make static and distroless builds, cross-compiling and CI slower | franz-go |
| Kafka transactions / exactly-once in franz-go | The spec forbids an end-to-end exactly-once claim, and transactions add complexity without helping the ClickHouse side | Idempotent producer + idempotent consumers |
| Auto-commit on any consumer | Violates "commit only after downstream effects" | `DisableAutoCommit` + explicit commits |
| ZooKeeper-based Kafka images (Bitnami/Confluent legacy) | Spec requires KRaft. Bitnami's free image catalogue was restructured in 2025 (training knowledge, LOW) | `apache/kafka` official |
| `clickhouse-go` through `database/sql` | Slower row-wise inserts and fewer features | Native `clickhouse.Open` API |
| ClickHouse `async_insert` for the event-writer | Obscures errors and batch-token behaviour | Writer-side batching |
| `FINAL` on every query without a benchmark | Cost grows with data | `LIMIT 1 BY event_id` or measured `FINAL` |
| Redis lists/streams as a second event bus | Spec: Redis is not a primary bus | Kafka |
| asynq | See boilerplate table | Kafka consumers |
| Native browser `EventSource` with token in query string | Spec forbids tokens in URLs | fetch streaming + eventsource-parser |
| `@microsoft/fetch-event-source` | Unmaintained | eventsource-parser |
| Prometheus metrics with tenant, IP, user or request ID labels | Cardinality blow-up and spec violation | Bounded labels only |
| OTel SDK metrics pipeline alongside client_golang | Two sources of truth for the same metrics | client_golang for metrics, OTel for traces |
| New Relic | Vendor lock-in, not in the spec | OTel + Prometheus + Grafana |
| `latest` image tags | Breaks reproducibility, and spec section 2 requires pins | Exact tag + digest |
| Grafana/Prometheus/ClickHouse/Redis/Kafka ports published publicly | Spec section 10 | Internal network, localhost-bound ports in Compose |
| numpy for deterministic generation | Extra dependency and version-dependent RNG streams | stdlib `random.Random(seed)` |

## Stack Patterns by Variant

**If the event-writer cannot keep up in benchmarks:**
- Add partitions and replicas first, then tune batch size and linger, then consider `async_insert`.
- Because the consumer group scales by partition count, set partition counts per environment up front (for example 6 locally, 12 on K8s) and record them in the benchmark report.

**If the Go-side OpenAPI 3.1 toolchain fails in Phase 1:**
- Keep `api/openapi.yaml` as 3.1 for the TS client and docs. Hand-write the Go request and response types and add a contract test that diffs them against the spec using libopenapi.
- Because the spec requires a published 3.1 document, but does not require generated Go server code.

**If Redis is down:**
- Default fail-closed (503 with `Retry-After`) for ingest. Allow fail-open only by explicit config, and expose a metric and a log line when it is in effect.

**If Kafka is single-broker (Compose):**
- Use RF=1 and `min.insync.replicas=1`, and say clearly in the README that this is not HA. K8s uses RF=3 and `min.insync.replicas=2`, with acks=all in both.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| franz-go v1.22.1 | Go >= 1.26.0 | Verified from go.mod. Forces the toolchain bump. |
| clickhouse-go v2.49.0 | Go >= 1.25.0 | Verified from go.mod. |
| Kafka client against `apache/kafka:4.3.1` | franz-go 1.22.x | franz-go negotiates API versions, so it works with current brokers (expected, not verified against 4.3.1). |
| `otelecho` v0.71.0 | `otel` v1.47.0 | Contrib versions track core releases. Let `go mod tidy` resolve. |
| testcontainers-go v0.44.0 and its modules | Same version for all modules | Mixing module versions causes build errors. |
| openapi-typescript 7.13.0 | openapi-fetch 0.17.0 | Keep them upgraded together. |
| Vite 8.3.4 | `@vitejs/plugin-react` 6.1.2, Vitest 5.0.3 | Peer ranges NOT checked, so confirm at scaffold time (LOW). |
| TypeScript 7.0.2 | typescript-eslint | Likely not yet supported (LOW). Stay on ~6.0. |
| ClickHouse 26.3 | clickhouse-go v2.49.0 | Recent client against an LTS server. The client supports the native protocol of current servers (expected). |
| golang-migrate v4.20.1 | ClickHouse 26.3 | Its ClickHouse driver depends on clickhouse-go v2. Confirm it compiles against v2.49.0, or use the custom runner (LOW). |

## Open Items to Verify Before or During Implementation

1. oapi-codegen v2.8.0 and kin-openapi v0.149.0 handling of OpenAPI 3.1 documents (Phase 1).
2. Exact ClickHouse 26.3 patch tag and digest. Check the non-replicated MergeTree dedup window setting name (Phase 1-2).
3. Kafka-on-K8s approach (plain StatefulSet vs Strimzi) and the kindest/node tag for kind v0.33.0 (Phase 6).
4. Versions of otelpgx, redisotel, Tempo/Jaeger and aiohttp/mypy (not verified here).
5. Vite 8 / Vitest 5 / MSW 3 / TypeScript peer compatibility (Phase 1 frontend scaffold).
6. Whether franz-go's current API names match the ones above (`CommitRecords`, `BlockRebalanceOnPoll`, `kgo.ErrMaxBuffered`). Check pkg.go.dev when writing Phase 2 code.

## Sources

- Go module proxy (`proxy.golang.org/<module>/@latest`), queried 2026-10-10: franz-go v1.22.1, kadm v1.19.0, kotel v1.7.1, clickhouse-go v2.49.0, pgx v5.11.0, go-redis v9.23.0, redis_rate v10.0.1 (2023-04-02), client_golang v1.25.0, otel v1.47.0, otelecho v0.71.0, kin-openapi v0.149.0, oapi-codegen v2.8.0, echo-middleware v1.1.0, libopenapi v0.41.5, jsonschema v6.0.3, echo v4.16.0, testcontainers-go v0.44.0, golang-migrate v4.20.1, golang-jwt v5.3.1, tern v2.4.3, koanf v2.3.8, zerolog v1.35.1, testify v1.12.1, golangci-lint v2.14.0, confluent-kafka-go v2.16.0, segmentio/kafka-go v0.4.51. HIGH confidence for version numbers.
- `raw.githubusercontent.com/twmb/franz-go/.../go.mod` and the proxy `.mod` for v1.22.1 (`go 1.26.0`), `ClickHouse/clickhouse-go` go.mod (`go 1.25.0`). HIGH.
- npm registry (`registry.npmjs.org/<pkg>/latest`), 2026-10-10: react 19.3.0, vite 8.3.4, typescript 7.0.2, @tanstack/react-query 5.104.1, openapi-typescript 7.13.0, openapi-fetch 0.17.0, openapi-react-query 0.5.4, recharts 3.10.1, uplot 1.6.32, react-router-dom 7.18.4, vitest 5.0.3, eslint 10.12.0, tailwindcss 4.3.3, playwright 1.64.0, msw 3.0.3, eventsource-parser 4.1.1, @microsoft/fetch-event-source 2.0.1. HIGH for versions.
- PyPI JSON API, 2026-10-10: httpx 0.28.1, pydantic 2.14.0, orjson 3.13.0, ruff 0.17.0, pytest 9.1.1, typer 0.27.3, confluent-kafka 2.16.0. aiohttp and mypy lookups failed (response parsing error), so their versions are unverified.
- GitHub releases API, 2026-10-10: kind v0.33.0, ClickHouse v26.3.47.6-lts, Prometheus v3.15.0, Grafana v13.2.3, OTel Collector releases v0.162.0, Redis 8.10.2, Kubernetes v1.37.1, Helm v4.3.0.
- Docker Hub tags API, 2026-10-10: apache/kafka (4.3.1 stable; 4.3.2 and 4.4.0 are RCs), clickhouse/clickhouse-server (26.3 exists; 26.9.14.10 latest line), postgres 18.6 (19beta4), redis 8.10.2, prom/prometheus v3.15.0, grafana/grafana 13.2.3, golang 1.26.9 and 1.27.2, python 3.13.16 and 3.14.8, node 26.11.1. HIGH.
- Library behaviour (franz-go options, clickhouse-go batch API and dedupe settings, SSE handling, Lua token bucket): training knowledge, not re-verified against docs in this session. MEDIUM-LOW, listed under Open Items.

---
*Stack research for: real-time security event ingestion, detection and alerting platform*
*Researched: 2026-10-10*
