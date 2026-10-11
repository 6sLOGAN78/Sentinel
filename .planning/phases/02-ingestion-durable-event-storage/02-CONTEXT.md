# Phase 2: Ingestion & Durable Event Storage - Context

**Gathered:** 2026-10-12
**Status:** Ready for planning
**Mode:** `--auto`. Every gray area was resolved with the recommended option. See `02-DISCUSSION-LOG.md` for the audit trail.

<domain>
## Phase Boundary

A producer with a scoped API key POSTs event batches to the ingest service. Each accepted event is published durably to `security.events.v1`, and the producer gets `202` only after Kafka acknowledges every record. The event-writer consumes that topic and batches events into ClickHouse, committing offsets only after a successful insert. Poison messages go to the DLQ. Replays never show duplicates in user-visible reads. A minimal, tenant-scoped event lookup proves the data landed. The seeded Python generator sends real traffic through the ingest API (`make seed`, 1000 benign events). Every binary exposes `/healthz` and a `/readyz` that reflects real dependency state.

Requirements: ING-01..07, STOR-01..04, GEN-01, GEN-03, OBS-01.

**Not in this phase:**
- Detection rules, alerts, the alert-writer and scenario injection (GEN-02): Phase 3.
- Dashboard auth (JWT/RBAC), the full events/alerts list API, SSE and React pages: Phase 4.
- Prometheus metrics, tracing, multi-replica load tests and benchmarks: Phase 5.
- K8s and hardened CI: Phase 6.
- The detector, alert-writer and query-api keep their Phase 1 lifecycle, except that query-api gains the minimal event lookup (D-13).
</domain>

<decisions>
## Implementation Decisions

### Producer API keys (ING-01, ING-05)
- **D-01:** Key format is `snt_<key_id>_<secret>`.
  - `key_id` is 12 lowercase base32 characters (public, used for lookup and logs). `secret` is 32 random bytes in base64url.
  - Only `key_id` and `SHA-256(secret)` are stored. The secret is high-entropy, so a fast hash is enough; no bcrypt.
  - Comparison is constant-time (`subtle.ConstantTimeCompare`).
  - The full key is shown exactly once, at creation.
- **D-02:** Keys live in PostgreSQL via a new tern migration.
  - Table `producer_keys`: `key_id` PK, `tenant_id`, `secret_hash bytea`, `scopes text[]`, `name`, `created_at`, `revoked_at`.
  - Scopes are `events:write` (ingest) and `events:read` (the D-13 lookup).
  - The ingest service now requires the `database` config section in addition to redis and kafka.
- **D-03:** Keys are sent in the `X-API-Key` header, matching the Phase 1 OpenAPI `ApiKeyAuth` scheme. Missing, malformed, unknown or revoked keys all return `401` in the error envelope, without revealing which case applied.
- **D-04:** Lookups use a small bounded in-process cache with a TTL of 30 s, so ingest doesn't hit Postgres on every request.
  - Revocation therefore takes effect within 30 s. Document this.
  - Cache size is capped. Negative results are not cached, so unknown keys can't evict good ones.
- **D-05:** Keys are provisioned through a tooling CLI `backend/tools/keyctl` with `create --tenant --scopes --name`, `revoke --key-id` and `list --tenant`. It is not a sixth service binary (same pattern as chmigrate, D-29).
  - There is no admin HTTP route; that's v2 TRI-02.
  - `make seed` uses keyctl idempotently to create demo keys for tenants `demo` and `demo2`. Generated keys are written to a gitignored file under `.local/`, never committed and never printed in CI logs.

### Ingest request handling (ING-01..05, ING-07)
- **D-06:** The route is `POST /api/v1/events:batch` on the ingest binary.
  - The body is `{"events": [...]}`, from 1 to 500 events.
  - The body is wrapped in `http.MaxBytesReader` at 1 MiB, giving `413` when exceeded. Batches over 500 events also return `413`.
  - Echo's handling of a literal `:` in a path is a research item (RESEARCH A3). If Echo can't match it, register the route with an escaped colon or a small pre-router exact-path match. The public path must stay exactly `/api/v1/events:batch`.
- **D-07:** Validation is all-or-nothing (ADR 0003).
  - Every event is decoded with `contracts.DecodeEvent` and `Validate` using the configured limits.
  - Any invalid event gives `400`, with per-index field errors in `error.details`, and nothing is published.
  - The server sets `received_at` to the server time in UTC with millisecond precision. A client-supplied `received_at` is overwritten.
- **D-08:** The tenant comes from the key.
  - Each event's `tenant_id` must equal the key's tenant. Any mismatch returns `403` for the whole batch, with nothing published.
  - A key without `events:write` also gets `403`.
- **D-09:** Responses follow the contract.
  - Success is `202` with `accepted_count`, `rejected_count` (always 0 under all-or-nothing), `event_ids` in request order, and `request_id`.
  - Every non-2xx response uses the spec §8 envelope from Phase 1 `internal/errs`.
- **D-10:** Load shedding is a bounded in-flight request limiter on the ingest route, defaulting to 64 concurrent batch requests (configurable).
  - Saturation returns `503` with `Retry-After: 1` immediately; there is no queueing.
  - This is in addition to the httpx server timeouts. Nothing is buffered without bound (ING-07).

### Kafka publishing (ING-03, ING-04, ING-07)
- **D-11:** The producer is franz-go with the idempotent producer, `acks=all` and bounded retries.
  - The partition key is `tenant_id|source_ip`, with an empty source_ip when it's null.
  - The batch is produced with `ProduceSync` under a configurable produce timeout (default 10 s). Any record failure or timeout returns `503`.
  - Per ADR 0003, 0..N records may already be published; the client retries the identical batch and dedup absorbs duplicates.
  - The producer buffer is capped with `MaxBufferedRecords` and `MaxBufferedBytes`, both configurable.
  - The message value is the canonical event JSON. Headers carry `schema_version` and `request_id`.
- **D-12:** A shared `internal/kafkax` package holds the franz-go client constructor, the producer helper and the batch-consumer helper with manual commits. Phase 3 reuses it, but only what Phase 2 needs is built now.

### Minimal event lookup (success criterion 2, STOR-03)
- **D-13:** query-api gains one route, `GET /api/v1/events/{event_id}`.
  - It requires an API key with `events:read` (the D-01 keys).
  - It is tenant-scoped through the key's tenant and returns the deduplicated row or a `404` envelope.
  - This proves the lookup without pulling Phase 4's JWT/RBAC or list API forward. Phase 4 replaces the auth for dashboard callers and adds `GET /api/v1/events` (API-01).
  - query-api gets ClickHouse access through the native client (D-17).

### ClickHouse event storage (STOR-01..03)
- **D-14:** Events table schema.
  - Engine: `ReplacingMergeTree`.
  - `PARTITION BY toYYYYMM(occurred_at)`, `ORDER BY (tenant_id, occurred_at, event_id)`.
  - `occurred_at` and `received_at` are `DateTime64(3, 'UTC')`. `attributes_json` is `String`. Nullable columns follow the spec §7.1 minimum column list.
  - A `bloom_filter` skip index on `event_id` supports the D-13 lookup.
  - `TTL toDateTime(occurred_at) + INTERVAL 7 DAY` by default. The TTL is changeable with a documented `ALTER TABLE ... MODIFY TTL` run through a config-driven chmigrate step or make target (research decides). The default stays 7 days locally.
  - Created through chmigrate migration `002_events.sql` in the configured database (WR-09 behaviour).
- **D-15:** Dedup is read-time and never depends on background merges (STOR-03).
  - Every user-visible read collapses duplicates on `event_id`, using `LIMIT 1 BY event_id` (or `FINAL` for small bounded lookups if research shows it's cheaper).
  - Counts use `uniqExact(event_id)`.
  - A test inserts the same events twice and asserts immediately, with no `OPTIMIZE`, that both the lookup and the counts show exactly one.
  - `insert_deduplication_token` may be used as an optimization but is never the correctness mechanism.
- **D-16:** The event-writer is a franz-go consumer group `sentinel-event-writer` with auto-commit off.
  - It flushes a batch at 5,000 records or 1 s, whichever comes first (configurable, with a hard cap). It inserts with a native batch, then commits the batch's offsets.
  - On a transient ClickHouse error it retries with exponential backoff and jitter, up to a bounded number of attempts. It does not commit and stops polling meanwhile, so there is natural backpressure.
  - On shutdown it flushes or aborts the in-flight batch within the Phase 1 30 s deadline and commits only what was inserted.
- **D-17:** The ClickHouse client is clickhouse-go v2.49.0 over the native protocol (port 19000 locally), a new pinned dependency. chmigrate stays on HTTP.

### Dead-letter queue (STOR-04)
- **D-18:** Records the event-writer can't decode or validate go to `security.events.dlq.v1`.
  - The original key and value are kept, with the value redacted if over a size cap.
  - Headers carry `dlq.reason`, `dlq.origin.topic`, `dlq.origin.partition`, `dlq.origin.offset` and `dlq.at`.
  - The source offset is committed only after the DLQ produce is acked.
  - A record that fails DLQ publish is not committed, and is retried.
  - Ingest never publishes invalid events, so DLQ traffic comes from poison or replay cases only. Tests inject poison directly into the topic.

### Rate limiting (ING-06)
- **D-19:** A Redis token bucket, implemented as a Lua script that uses Redis `TIME`, keyed per `key_id`.
  - The cost of a request is the number of events in the batch.
  - Defaults are a capacity of 10,000 events and a refill of 2,000 events per second, configurable globally.
  - Over-quota requests get `429` with `Retry-After` computed from the refill rate.
  - If Redis is unreachable, behaviour follows `SENTINEL_RATELIMIT.FAIL_MODE`: `closed` (the default) returns `503`; `open` admits the request with a warning log and a counter (ADR 0005).
  - The limiter runs after authentication and before validation and publishing.

### Health and readiness (OBS-01)
- **D-20:** Every binary gets a small ops HTTP listener, separate from public traffic, serving `GET /healthz` and `GET /readyz`.
  - `/healthz` returns 200 while the process is running.
  - `/readyz` runs dependency checks with short timeouts and returns `503` plus a per-dependency status body when any required dependency is down. Kafka is checked with a metadata request, ClickHouse with a ping, Postgres with a ping, Redis with a PING.
  - Ops ports are configurable per binary, defaulting to `9101`–`9105` in service order. Public ports default to ingest `8080` and query-api `8081`.
  - `/metrics` joins the ops listener in Phase 5.
  - D-06 of Phase 1 (no listeners) is superseded for Phase 2 by this decision and D-06/D-13 above.

### Generator and seeding (GEN-01, GEN-03, success criterion 2)
- **D-21:** The generator gains `send`, using httpx.
  - Example: `uv run sentinel-generator send --seed 42 --count 1000 --batch-size 100 --url http://127.0.0.1:8080 --tenant demo`.
  - The API key is read from an env var or the `.local/` key file, never from a flag.
  - `occurred_at` is anchored at run start minus small seeded offsets, so events pass the skew validator.
  - It retries `503` and `429` by honoring `Retry-After`, with bounded attempts, resending the identical batch.
  - It prints per-run totals for sent, accepted and rejected.
- **D-22:** `make seed` does the following:
  - ensures the stack and apps are up
  - creates the demo keys idempotently (D-05)
  - sends 1000 benign seeded events for tenant `demo`
  - verifies all 1000 are visible by polling ClickHouse with tenant-scoped `uniqExact(event_id)` up to a timeout, and spot-checks some IDs through the D-13 lookup
  - exits non-zero on any shortfall
  - The Phase 1 placeholder behaviour is removed. `make bench` stays a placeholder until Phase 5.

### Running the apps locally
- **D-23:** A multi-stage `deployments/docker/Dockerfile` builds all five Go binaries into a small runtime image.
  - Compose gains `ingest`, `event-writer` and `query-api` services, with healthchecks hitting `/readyz` and `depends_on` on healthy infrastructure. Their ports bind to `127.0.0.1` like the infrastructure ports.
  - `make dev-up` now starts the infrastructure, runs migrations, then starts these apps. `make run SVC=` stays available for running one service from source.
  - The detector and alert-writer containers are added in Phase 3.

### Carry-over hardening from Phase 1 (SECURITY W-05/W-07, REVIEW hand-offs)
- **D-24:** Before the first public route goes live, do the following:
  - Make sure an empty CORS origin list means no cross-origin access (deny), not Echo's allow-all default.
  - Log the request path without the query string, and never log the `X-API-Key` header or request bodies.
  - Keep IP extraction on the TCP peer unless trusted proxies are configured (the WR-02 behaviour).
  - Ingest validation is Go-typed (D-12 of Phase 1), so W-05's `AssertFormat` applies only if production code ever compiles JSON Schema. It must not.
- **D-25:** Add the banned-module `go list -deps` check to the CI go job (W-03), since Phase 2 adds dependencies (franz-go, clickhouse-go).

### Claude's Discretion
- Internal package layout under `backend/internal/` (for example `ingest`, `kafkax`, `storage/clickhouse`, `apikeys`, `ratelimit`, `opshttp`), and handler and middleware structure.
- Exact config key names (following the existing `SENTINEL_<SECTION>.<KEY>` convention), default timeouts, and retry/backoff constants within the stated bounds.
- Whether `/readyz` responses carry per-dependency latency.
- Dockerfile base image choice (distroless or static), as long as it is pinned by digest.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Spec
- `spec.md` §3: architecture and the commit-after-effects key decision
- `spec.md` §6.1, §6.3: event schema; topics, keys and retention
- `spec.md` §7.1, §7.2, §7.4: ClickHouse table, duplicate-safe ingestion, Redis
- `spec.md` §8: ingest endpoint, status codes and response semantics
- `spec.md` §10, §11.1: security requirements, failure handling and backpressure
- `spec.md` §14 Phase 2: deliverables and exit checks

### Phase 1 artifacts (what exists)
- `docs/decisions/0002-event-id-policy.md`, `0003-atomic-batch-semantics.md`, `0005-redis-failure-policy.md`, `0004-metrics-label-policy.md`, `0001-repo-layout-and-binaries.md`
- `api/openapi.yaml`: `/api/v1/events:batch`, `EventBatchRequest`, `EventBatchAccepted`, `ApiKeyAuth` (`X-API-Key`), `Error`
- `backend/internal/contracts/`: `DecodeEvent`, `Validate`, `Limits`, schemas, fixtures
- `backend/internal/{config,app,httpx,middleware,errs,database,redisx,chmigrate}`: retained infrastructure. Read `.planning/phases/01-foundations-contracts/01-0{1,6,7,8,10}-SUMMARY.md` for signatures.
- `.planning/phases/01-foundations-contracts/01-SECURITY.md` (W-03, W-05, W-07, AR-04), `01-REVIEW.md`, `01-REVIEW-FIX.md`: hand-offs into D-24/D-25

### Research
- `.planning/research/SUMMARY.md`, `STACK.md` (franz-go v1.22.1, clickhouse-go v2.49.0, Lua token bucket), `ARCHITECTURE.md` (event-writer, kafkax, dedupe read path), `PITFALLS.md` (ReplacingMergeTree async, small inserts, producer buffers, commit-after-effects)
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `contracts.DecodeEvent` / `Event.Validate` / `Limits`: strict, duplicate-key-rejecting validation, ready for the ingest hot path.
- `app.Run` / `Lifecycle`: binary lifecycle with LIFO closers, a 30 s shutdown, and SIGINT/SIGTERM handling. Extend the binaries rather than replace them.
- `httpx` server helper with timeouts and nil-safe Shutdown. `middleware.Apply` registers Recover first, plus request ID and the envelope error handler.
- `errs` envelope constructors, including 413/429/503 with retry-after and 409.
- `redisx.New` with AUTH and TLS options. `database.New` / `Migrate`. `chmigrate` with database-aware, idempotent migrations.
- `internal/testing/containers.go`: postgres, redis, clickhouse and generic apache/kafka testcontainers.
- `generator/src/sentinel_generator/events.py`: seeded, UTC-correct event builder.

### Established Patterns
- `config.Load(service)` with per-service required sections. Add `database` to ingest and the new sections (ingest, writer, ops) here.
- Integration tests use `//go:build integration` in `backend/test/` and `scripts/go-test-nonempty.sh`. Test-name prefixes are listed in VALIDATION files.
- Every change keeps `make lint` (golangci-lint v2) and `make test` green. `make test-integ` runs in CI (WR-13).

### Integration Points
- `backend/cmd/ingest/main.go`, `cmd/event-writer/main.go` and `cmd/query-api/main.go` gain real work and listeners. `cmd/detector` and `cmd/alert-writer` only get the D-20 ops listener.
- `deployments/compose/docker-compose.yml` and the `Makefile` (`dev-up`, `seed`) gain the app services.
- `backend/migrations/postgres/002_producer_keys.sql` and `backend/migrations/clickhouse/002_events.sql`.
</code_context>

<specifics>
## Specific Ideas

- Write the "double insert, no OPTIMIZE, assert immediately" test before the read path exists.
- Kill the event-writer mid-batch in an integration test, restart it, and assert there's no loss (every acked event is stored) and no visible duplicates.
- Add a test proving that a 503 from a Kafka timeout followed by an identical retry yields exactly one visible copy of each event.
- Never print API key secrets in logs, test output or CI.
</specifics>

<deferred>
## Deferred Ideas

- Admin key-management HTTP API: v2 TRI-02.
- JWT/RBAC dashboard auth and the full events list API: Phase 4.
- Prometheus counters for rate-limit fail-open, DLQ volume and ingest results: Phase 5 (OBS-02). Phase 2 only logs.
- Multi-replica event-writer load test and dedup cost benchmark: Phase 5.
</deferred>

---

*Phase: 02-ingestion-durable-event-storage*
*Context gathered: 2026-10-12*
