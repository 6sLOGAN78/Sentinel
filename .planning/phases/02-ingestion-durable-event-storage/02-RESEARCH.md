# Phase 2: Ingestion & Durable Event Storage - Research

**Researched:** 2026-10-12
**Domain:** Authenticated HTTP ingest, franz-go Kafka producer/consumer, ClickHouse ReplacingMergeTree with read-time dedupe, Redis token bucket, Docker/Compose app services, Python generator client
**Confidence:** HIGH for every item that was run against the pinned images (Echo routing, franz-go, clickhouse-go, ClickHouse DDL and SQL, Lua script, Dockerfile build). MEDIUM for design choices that are mine (writer loop shape, ops listener, config names).

## Summary

All twelve research questions were resolved by running code against the pinned stack (apache/kafka 4.3.1, clickhouse 26.3.47.6, redis 8.10.2, Echo v4.16.0, franz-go v1.22.1, clickhouse-go v2.49.0, go-redis v9.23.0) in a scratch module outside the repo. Nothing in D-01..D-25 needs to change. Four findings change how the plan should be written and are worth reading first:

1. **Echo treats an unescaped `:batch` as a path parameter** (it also matches `/events:xyz`). Register the route as `"/api/v1/events\\:batch"` (a backslash-escaped colon in the Go string, i.e. the characters `\:`). That matches the literal `/api/v1/events:batch` and nothing else. No pre-router is needed. (A3 resolved.)
2. **`FINAL` is NOT a valid dedupe mechanism for this table.** The ORDER BY includes `occurred_at`, so rows with the same `event_id` but different `occurred_at` are never collapsed by `FINAL` or by merges. Use `LIMIT 1 BY` / `uniqExact(event_id)` for every user-visible read. Also, **do not set `non_replicated_deduplication_window` on the table**: with it enabled, an identical second insert is silently dropped at insert time and the "double insert, no OPTIMIZE" test stops proving read-time dedupe. The default is `0` (verified), so leave it unset.
3. **franz-go refuses to cancel in-flight idempotent records by default**, so `ProduceSync(ctx-with-timeout)` can block past the deadline. Add `kgo.AllowIdempotentProduceCancellation()` (duplicates on retry are already absorbed by ADR 0003 / read-time dedupe). Verified: with the broker paused, `ProduceSync` returned `context deadline exceeded` at exactly the 3 s deadline, and the same client recovered after unpause.
4. **Rate-limit cost needs the event count before full validation.** D-19 says "after auth, before validation", and cost = number of events. Resolve by reading the bounded body and doing a cheap shallow parse (`{"events":[json.RawMessage...]}`) to get N, then rate-limit, then run per-event `DecodeEvent`/`Validate`.

Also found: `scripts/check-pins.sh` greps every `image:` line in the Compose file and requires `@sha256:`, so app services must use `build:` only (no `image:` line) or the hygiene CI job fails. The existing integration `serviceEnv` and `TestMain` must also be updated (ingest now needs `database`, event-writer/query-api need `NATIVE_ADDRESS`, and parallel lifecycle tests need distinct ops ports).

**Primary recommendation:** Build `internal/{kafkax,storage/clickhouse,apikeys,ratelimit,opshttp,ingest}` plus `tools/{keyctl,healthprobe}`. Use the exact snippets below. Verify with an integration suite that uses real Kafka, ClickHouse, Redis and Postgres containers and fault injection by `docker pause` (no mock-only adapters).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Producer API keys (ING-01, ING-05)**
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

**Ingest request handling (ING-01..05, ING-07)**
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

**Kafka publishing (ING-03, ING-04, ING-07)**
- **D-11:** The producer is franz-go with the idempotent producer, `acks=all` and bounded retries.
  - The partition key is `tenant_id|source_ip`, with an empty source_ip when it's null.
  - The batch is produced with `ProduceSync` under a configurable produce timeout (default 10 s). Any record failure or timeout returns `503`.
  - Per ADR 0003, 0..N records may already be published; the client retries the identical batch and dedup absorbs duplicates.
  - The producer buffer is capped with `MaxBufferedRecords` and `MaxBufferedBytes`, both configurable.
  - The message value is the canonical event JSON. Headers carry `schema_version` and `request_id`.
- **D-12:** A shared `internal/kafkax` package holds the franz-go client constructor, the producer helper and the batch-consumer helper with manual commits. Phase 3 reuses it, but only what Phase 2 needs is built now.

**Minimal event lookup (success criterion 2, STOR-03)**
- **D-13:** query-api gains one route, `GET /api/v1/events/{event_id}`.
  - It requires an API key with `events:read` (the D-01 keys).
  - It is tenant-scoped through the key's tenant and returns the deduplicated row or a `404` envelope.
  - This proves the lookup without pulling Phase 4's JWT/RBAC or list API forward. Phase 4 replaces the auth for dashboard callers and adds `GET /api/v1/events` (API-01).
  - query-api gets ClickHouse access through the native client (D-17).

**ClickHouse event storage (STOR-01..03)**
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

**Dead-letter queue (STOR-04)**
- **D-18:** Records the event-writer can't decode or validate go to `security.events.dlq.v1`.
  - The original key and value are kept, with the value redacted if over a size cap.
  - Headers carry `dlq.reason`, `dlq.origin.topic`, `dlq.origin.partition`, `dlq.origin.offset` and `dlq.at`.
  - The source offset is committed only after the DLQ produce is acked.
  - A record that fails DLQ publish is not committed, and is retried.
  - Ingest never publishes invalid events, so DLQ traffic comes from poison or replay cases only. Tests inject poison directly into the topic.

**Rate limiting (ING-06)**
- **D-19:** A Redis token bucket, implemented as a Lua script that uses Redis `TIME`, keyed per `key_id`.
  - The cost of a request is the number of events in the batch.
  - Defaults are a capacity of 10,000 events and a refill of 2,000 events per second, configurable globally.
  - Over-quota requests get `429` with `Retry-After` computed from the refill rate.
  - If Redis is unreachable, behaviour follows `SENTINEL_RATELIMIT.FAIL_MODE`: `closed` (the default) returns `503`; `open` admits the request with a warning log and a counter (ADR 0005).
  - The limiter runs after authentication and before validation and publishing.

**Health and readiness (OBS-01)**
- **D-20:** Every binary gets a small ops HTTP listener, separate from public traffic, serving `GET /healthz` and `GET /readyz`.
  - `/healthz` returns 200 while the process is running.
  - `/readyz` runs dependency checks with short timeouts and returns `503` plus a per-dependency status body when any required dependency is down. Kafka is checked with a metadata request, ClickHouse with a ping, Postgres with a ping, Redis with a PING.
  - Ops ports are configurable per binary, defaulting to `9101`-`9105` in service order. Public ports default to ingest `8080` and query-api `8081`.
  - `/metrics` joins the ops listener in Phase 5.
  - D-06 of Phase 1 (no listeners) is superseded for Phase 2 by this decision and D-06/D-13 above.

**Generator and seeding (GEN-01, GEN-03, success criterion 2)**
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

**Running the apps locally**
- **D-23:** A multi-stage `deployments/docker/Dockerfile` builds all five Go binaries into a small runtime image.
  - Compose gains `ingest`, `event-writer` and `query-api` services, with healthchecks hitting `/readyz` and `depends_on` on healthy infrastructure. Their ports bind to `127.0.0.1` like the infrastructure ports.
  - `make dev-up` now starts the infrastructure, runs migrations, then starts these apps. `make run SVC=` stays available for running one service from source.
  - The detector and alert-writer containers are added in Phase 3.

**Carry-over hardening from Phase 1 (SECURITY W-05/W-07, REVIEW hand-offs)**
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

### Deferred Ideas (OUT OF SCOPE)
- Admin key-management HTTP API: v2 TRI-02.
- JWT/RBAC dashboard auth and the full events list API: Phase 4.
- Prometheus counters for rate-limit fail-open, DLQ volume and ingest results: Phase 5 (OBS-02). Phase 2 only logs.
- Multi-replica event-writer load test and dedup cost benchmark: Phase 5.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ING-01 | Scoped, hashed API key; up to 500 events / 1 MiB; tenant from key | Key format and store (Pattern 4), Echo `\:` route (Pattern 1), body/shallow-parse order (Pattern 2) |
| ING-02 | All-or-nothing 400 with structured error | Pattern 2 step order; `contracts.DecodeEvent`/`Validate`; writer-side validation uses `received_at` as "now" (Pitfall 9) |
| ING-03 | 202 only after Kafka acks; acks=all, idempotent, key `tenant_id\|source_ip` | franz-go producer options (Pattern 3), `AllowIdempotentProduceCancellation` (Pitfall 3) |
| ING-04 | Retry after 5xx creates no visible duplicates | Read-time dedupe SQL (Pattern 6), partial-publish fault-injection test |
| ING-05 | 401/403/413/503 in the envelope | Pattern 2; `errs` constructors already exist (401, 403, 413, 503 with Retry-After) |
| ING-06 | Redis token bucket, 429 + Retry-After, fail-closed 503 | Verified Lua script and go-redis `Script.Run` (Pattern 5) |
| ING-07 | Load shedding, capped producer buffers | In-flight semaphore (Pattern 7), `MaxBufferedRecords`/`MaxBufferedBytes`, sizing rule |
| STOR-01 | Batch writer, commit after successful insert | Writer loop (Pattern 8), clickhouse-go native batch |
| STOR-02 | Monthly partition, ORDER BY, TTL configurable | Verified DDL and TTL mechanism (Pattern 6) |
| STOR-03 | No visible dupes without merges | `LIMIT 1 BY` / `uniqExact`, FINAL pitfall, double-insert test |
| STOR-04 | DLQ with reason/origin, commit after DLQ ack | DLQ flow in Pattern 8, header names |
| GEN-01 | Seeded, reproducible generator | Already exists (`events.py`); `send` anchoring rule (Pattern 10) |
| GEN-03 | `make seed` publishes through ingest | Generator `send`, keyctl `ensure`, verify via `clickhouse-client` in container |
| OBS-01 | `/healthz` + `/readyz` reflect real dependencies | Ops listener pattern (Pattern 9), readiness tests |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- Go single module, multiple binaries. Pinned versions in lockfiles and image digests. No secrets committed.
- At-least-once delivery; commit offsets only after downstream effects succeed; every consumer/store idempotent.
- Tenant resolved from verified identity, never only from the request. API keys hashed at rest. All SQL parameterized.
- Bounded resources everywhere; shed load with 429/503. No high-cardinality Prometheus labels (metrics themselves are Phase 5).
- Definition of Done: end-to-end workflow, tests for success, invalid input and failure, CI green, no stubs or mock-only adapters.
- Lint: golangci-lint v2 strict set (`funlen` 100/50, `cyclop` 30, `gocognit` 20, `mnd` no magic numbers, `gochecknoglobals`, `gochecknoinits`, `nonamedreturns`, `testpackage`, `tparallel`, `forbidigo`, `errcheck` with type assertions). `nolint` needs a named linter and a reason. Write constants for every number; keep functions short; no `init()`; no globals.
- Errors: return wrapped errors (`%w`); HTTP errors via `errs.NewXxxError`; handlers return errors to the central handler.
- Logging: structured zerolog fields, lower-case messages; never log `X-API-Key`, secrets or request bodies.
- Work must go through a GSD workflow (this research is part of one). Never read `backend/.env` or the root `.env`. Use `GOTOOLCHAIN=go1.26.9` locally (local Go is 1.25.5).
- The CLAUDE.md "Technology Stack" section describes the pre-Phase-1 boilerplate (Clerk, asynq, New Relic). It is stale; Phase 1 removed those and CI bans them. Follow `go.mod` and CONTEXT instead.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| API key verification, scope and tenant checks | API / Backend (ingest, query-api) | Database (Postgres `producer_keys`) | Trust decision must be server-side; Postgres is the source of truth, in-process cache only accelerates |
| Request shaping: size, count, validation, shedding | API / Backend (ingest) | — | Edge of the trust boundary; bounded before anything is buffered or published |
| Rate limiting | API / Backend (ingest) | Redis (token bucket state) | Decision in ingest, shared state in Redis so replicas share quota |
| Durable acceptance | Kafka (`security.events.v1`) | API / Backend (producer) | 202 means Kafka acked, nothing more |
| Batching into ClickHouse, DLQ routing, offset commits | event-writer (consumer) | Kafka (offsets), ClickHouse | Commit-after-effects lives in exactly one place |
| Dedupe | Database / Storage (ClickHouse read SQL) | event-writer (no dedupe at write) | Read-time, never relies on merges |
| Event lookup | API / Backend (query-api) | ClickHouse | Tenant scope from the key, parameterized query |
| Health/readiness | Each binary (ops listener) | Compose/K8s probes | Probe must see real dependency state |
| Key provisioning | Tooling CLI (`keyctl`) | Postgres | No admin HTTP route (v2) |
| Seeding | Python generator + Make | Compose stack | Goes through the public ingest API only |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/twmb/franz-go` (`pkg/kgo`) | v1.22.1 (proxy.golang.org, 2026-09-27) | Kafka producer + consumer group | Locked by D-11/D-12. Idempotent producer on by default; documented manual-commit APIs [VERIFIED: `go doc` + ran against apache/kafka 4.3.1] |
| `github.com/twmb/franz-go/pkg/kadm` | v1.19.0 (2026-09-18) | Topic create/list in integration tests only | Same repo; needed to create per-test topics because auto-create is off [VERIFIED: ran] |
| `github.com/ClickHouse/clickhouse-go/v2` | v2.49.0 (2026-10-08) | Native-protocol batch insert and parameterized reads | Locked by D-17 [VERIFIED: ran against 26.3.47.6] |
| `github.com/redis/go-redis/v9` | v9.23.0 (already in go.mod) | `Script.Run` for the Lua bucket | Already pinned; `Script.Run` does EVALSHA then falls back to EVAL on NOSCRIPT [VERIFIED: cold-script run succeeded] |
| `github.com/labstack/echo/v4` | v4.16.0 (already in go.mod) | Ingest and query-api HTTP | Existing; route `\:` behaviour verified |
| `github.com/jackc/pgx/v5` | v5.11.0 (already) | `producer_keys` lookups | Existing `database.New` pool |
| `httpx` (Python) | 0.28.1 (latest stable on PyPI; only 1.0.dev pre-releases are newer) | Generator HTTP client | Locked by D-21 [VERIFIED: PyPI JSON API; slopcheck OK] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| stdlib `crypto/rand`, `crypto/sha256`, `crypto/subtle`, `encoding/base32`, `encoding/base64` | Go 1.26.9 | Key generation, hashing, compare | All of D-01; no third-party crypto |
| stdlib `net/http`, `context`, `sync` | — | Ops listener, in-flight semaphore, TTL cache | Everything in Patterns 4, 7, 9 |
| `argparse` (stdlib, Python) | — | `sentinel-generator send` CLI | Avoids adding click/typer |
| `httpx.MockTransport` | bundled in httpx | Generator unit tests | Avoids adding respx |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| In-process TTL map | `hashicorp/golang-lru/v2/expirable` | Adds a dependency for ~40 lines of code; D-04 wants no heavy dependency |
| `-healthcheck` flag in each binary | `tools/healthprobe` static binary | Chosen: probe tool. Keeps `app.Run` untouched and gives K8s `exec` probes later too |
| `FINAL` | `LIMIT 1 BY` / `uniqExact` | FINAL dedupes on the full sort key only (see Pitfall 1); rejected |
| `insert_deduplication_token` | read-time dedupe | Token only works when `non_replicated_deduplication_window > 0`; hides the read-path test. Not used in Phase 2 |

**Installation:**
```bash
cd backend
export GOTOOLCHAIN=go1.26.9
go get github.com/twmb/franz-go@v1.22.1 github.com/twmb/franz-go/pkg/kadm@v1.19.0 github.com/ClickHouse/clickhouse-go/v2@v2.49.0
go mod tidy
cd ../generator
uv add "httpx==0.28.1"   # then regenerate requirements.txt with the uv export command in its header
```

**Version verification:** versions confirmed against proxy.golang.org (`.../@v/<ver>.info`) and PyPI JSON on 2026-10-12. franz-go and kadm are separate modules in one repo (kadm needs its own `go get`).

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| httpx | PyPI | years (0.28.1 is Dec 2024) | very high | github.com/encode/httpx | [OK] | Approved |
| github.com/twmb/franz-go | Go proxy | first tag 2021-09 | n/a | github.com/twmb/franz-go | [SUS] false positive: "FRESH_PACKAGE 14 days" measures the latest version tag, not the module | Approved: locked decision D-11, in project STACK.md, module exists since v1.0.0 (2021-09-03), `go get` resolved from proxy |
| github.com/twmb/franz-go/pkg/kadm | Go proxy | first tag 2022-05 | n/a | same repo (`Subdir: pkg/kadm`) | [SUS] same false positive (23 days = latest tag) | Approved, test-only |
| github.com/ClickHouse/clickhouse-go/v2 | Go proxy | years | n/a | github.com/ClickHouse/clickhouse-go (official org) | [SLOP] false positive: slopcheck queried the un-escaped mixed-case path (`NOT_FOUND`). proxy.golang.org with the `!c`/`!h` escaping returns v2.49.0 with VCS origin github.com/ClickHouse/clickhouse-go | Approved: locked decision D-17; `go get` resolved it |

**Packages removed due to slopcheck [SLOP] verdict:** none (the one [SLOP] is a tool limitation, evidence above).
**Packages flagged as suspicious [SUS]:** franz-go, kadm (false positives, see table). The planner should NOT add a checkpoint for these because they are locked decisions already used by research; do confirm `go.sum` is committed and `go list -deps` banned-module check passes (it did in the scratch module: zero hits for newrelic, pkg/errors, clerk, asynq, resend across 327 deps).
No `postinstall` scripts apply (Go and PyPI wheels).

## Architecture Patterns

### System Architecture Diagram

```
 Python generator / any producer
        | POST /api/v1/events:batch  (X-API-Key)
        v
 +---------------------------- ingest (:8080, ops :9101) ----------------------------+
 | Recover > RequestID > Context > CORS(deny if empty) > Secure > RequestLogger       |
 |   route middleware: InFlight(64) --full--> 503 + Retry-After:1                     |
 |   1. auth: parse key, cache(30s) -> Postgres producer_keys   --bad--> 401          |
 |   2. scope events:write                                      --no---> 403          |
 |   3. MaxBytesReader 1 MiB --over--> 413; shallow parse N     --0/bad-> 400; >500 413|
 |   4. rate limit (Redis Lua, cost=N)  --over--> 429 + Retry-After; Redis down:       |
 |        closed -> 503 | open -> warn + admit                                        |
 |   5. DecodeEvent+Validate each (set received_at)             --bad--> 400 (indexed) |
 |   6. tenant == key tenant                                    --no---> 403          |
 |   7. ProduceSync(ctx 10s) acks=all idempotent                --err--> 503          |
 |   8. 202 {accepted_count, rejected_count:0, event_ids, request_id}                 |
 +--------------------------------------+---------------------------------------------+
                                        v
                          Kafka  security.events.v1  (key tenant|source_ip)
                                        v
 +------------------------- event-writer (ops :9103) --------------------------------+
 | group sentinel-event-writer, auto-commit off, BlockRebalanceOnPoll                  |
 | poll -> batch (5000 | 1s | hard cap) -> decode+validate                            |
 |   poison --> DLQ producer (acks=all) security.events.dlq.v1 + dlq.* headers        |
 |   good   --> clickhouse native PrepareBatch/Append/Send (retry w/ backoff+jitter)  |
 |   all of the above done --> CommitRecords(last record per partition) --> Allow...   |
 +--------------------------------------+---------------------------------------------+
                                        v
        ClickHouse  events  (ReplacingMergeTree, month partition, TTL, bloom idx)
                                        ^
 +------------------------- query-api (:8081, ops :9105) ----------------------------+
 | GET /api/v1/events/{event_id}: key(events:read) -> tenant -> parameterized         |
 | SELECT ... WHERE tenant_id AND event_id ORDER BY received_at LIMIT 1 -> 200 | 404  |
 +--------------------------------------------------------------------------------------+
 Every binary: ops listener (127.0.0.1 by default) /healthz /readyz (parallel checks, per-check timeout)
```

### Recommended Project Structure
```
backend/
├── cmd/{ingest,event-writer,query-api,detector,alert-writer}/main.go   # wiring only
├── internal/
│   ├── kafkax/          # NewClient(brokers,...opts), Producer (ProduceSync wrapper), BatchConsumer, DLQ record builder, Ping
│   ├── storage/clickhouse/  # Open(cfg), EventWriter.InsertBatch, EventReader.Get/CountUnique
│   ├── apikeys/         # Generate, Parse, Hash, Store (pgx), Cache (bounded TTL), Verifier
│   ├── ratelimit/       # Limiter (Lua script), Decision, fail-mode policy
│   ├── opshttp/         # Server (healthz/readyz), Check type, draining flag
│   ├── ingest/          # Handler, shallow parse, middleware (InFlight), request/response types
│   ├── config/          # + Ops, Ingest, Writer, RateLimit fields, Kafka topics
│   └── testing/         # + FreePort exported, container handle helpers
├── tools/{chmigrate,keyctl,healthprobe}/
└── migrations/{postgres/002_producer_keys.sql, clickhouse/002_events.sql}
deployments/docker/Dockerfile        # + .dockerignore at repo root
```

### Pattern 1: Echo route with a literal colon (A3, RESOLVED)
**What:** Echo parses `:name` anywhere in a path as a parameter, including mid-segment. Escape it.
**Verified results (Echo v4.16.0, httptest):**

| Registered route | Request path | Result |
|---|---|---|
| `/api/v1/events:batch` | `/api/v1/events:batch` | 200 (works, but see next row) |
| `/api/v1/events:batch` | `/api/v1/events:xyz` | **200, wrong: `:batch` is a param** |
| `/api/v1/events\:batch` | `/api/v1/events:batch` | 200 |
| `/api/v1/events\:batch` | `/api/v1/events:xyz` | 404 (correct) |
| `/api/v1/events\:batch` | `/api/v1/events%3Abatch` | 404 (clients must send a literal colon; acceptable) |
| `/api/v1/events/:id` coexisting with the escaped route | `/api/v1/events/abc` | routes to `:id` correctly |

```go
// Source: scratch test run against echo v4.16.0
const routeEventsBatch = `/api/v1/events\:batch` // backslash escapes the colon; public path stays /api/v1/events:batch

e.POST(routeEventsBatch, h.PostBatch, middleware.InFlight(cfg.MaxInFlight))
```
Add a test that asserts both `/api/v1/events:batch` (POST reaches handler) and `/api/v1/events:xyz` (404 envelope). Update ADR 0003's implementation note to say it was verified.

### Pattern 2: Ingest handler step order
Order matters for both correctness and bounded work. Each step maps to one status code.

1. InFlight middleware (route level, before auth so floods of bad keys cannot exhaust the Postgres pool either).
2. `apikeys.Verifier.Authenticate(ctx, header)` returns `Principal{KeyID, TenantID, Scopes}`; any failure is the same 401 message.
3. Scope check `events:write` -> 403.
4. `c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxBody)`; read all; `*http.MaxBytesError` -> 413. Also fast-reject `Content-Length > maxBody`.
5. Shallow parse with `DisallowUnknownFields` into `struct{ Events []json.RawMessage \`json:"events"\` }`: malformed JSON or unknown top-level key -> 400; `len==0` -> 400; `len>500` -> 413 (D-06).
6. Rate limit with `cost = len(Events)`. (This is how D-19's "before validation" is satisfied while cost still equals the event count.)
7. For each raw event: `contracts.DecodeEvent(bytes.NewReader(raw))`, set `ReceivedAt = &ts` (server `time.Now()` once per request, `contracts.NewTimestamp`), `Validate(now, limits)`. Collect `[]errs.FieldError` with field `events[i].<field>`; any -> 400 with details, nothing published. (Discretion: validation errors win over tenant 403; recommended because a malformed batch cannot be attributed.)
8. Tenant check: every `ev.TenantID == principal.TenantID`, else 403 for the whole batch.
9. Build records, `ProduceSync`. Any error -> `errs.NewServiceUnavailableError("...", 1)`; log the cause, never return it.
10. 202 body per D-09; `event_ids` in request order.

`RetryAfter` on `errs.HTTPError` is already turned into the `Retry-After` header by `GlobalErrorHandler` for 429/503.

### Pattern 3: franz-go producer (D-11) — verified options
```go
// Source: go doc github.com/twmb/franz-go/pkg/kgo (v1.22.1) + scratch run against apache/kafka 4.3.1
cl, err := kgo.NewClient(
    kgo.SeedBrokers(brokers...),                 // from config.ParseBrokers
    kgo.ClientID("sentinel-ingest"),
    kgo.RequiredAcks(kgo.AllISRAcks()),          // default already; set explicitly
    // idempotent writes are ON by default; do NOT pass DisableIdempotentWrite()
    kgo.MaxBufferedRecords(cfg.MaxBufferedRecords),
    kgo.MaxBufferedBytes(cfg.MaxBufferedBytes),
    kgo.RecordRetries(cfg.RecordRetries),        // e.g. 5 (bounded; default is unlimited)
    kgo.RecordDeliveryTimeout(cfg.ProduceTimeout),
    kgo.ProduceRequestTimeout(5*time.Second),
    kgo.AllowIdempotentProduceCancellation(),    // see Pitfall 3
    kgo.ProducerBatchCompression(kgo.SnappyCompression()),
)
// startup: fail fast, NewClient does not dial
pctx, cancel := context.WithTimeout(ctx, app.DialTimeout); defer cancel()
if err := cl.Ping(pctx); err != nil { return fmt.Errorf("kafka: %w", err) }

// request path
rec := &kgo.Record{
    Topic: topic, Key: []byte(tenant + "|" + srcIP), Value: canonicalJSON,
    Headers: []kgo.RecordHeader{
        {Key: "schema_version", Value: []byte("1")},
        {Key: "request_id", Value: []byte(reqID)},
    },
}
pctx, cancel := context.WithTimeout(c.Request().Context(), cfg.ProduceTimeout)
defer cancel()
if err := cl.ProduceSync(pctx, recs...).FirstErr(); err != nil { /* 503 */ }
```
Verified behaviours: topic create via `kadm`; `Ping` honours its context (returned `context deadline exceeded` after 2.0 s against a paused broker); `ProduceSync` results expose `Record.Partition/Offset`; headers round-trip; default partitioner hashes keyed records (keyed records with the same key land on one partition).

### Pattern 4: API keys (D-01..D-04)
```go
// Source: stdlib; format per D-01. Verified lengths: key_id 12 chars [a-z2-7], secret 43 chars RawURLEncoding.
const (
    keyPrefix    = "snt"
    keyIDLen     = 12
    secretBytes  = 32
    secretEncLen = 43 // base64.RawURLEncoding of 32 bytes
)
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func Generate() (full string, keyID string, hash [32]byte, err error) { // golangci nonamedreturns: use a struct return instead
    idRaw := make([]byte, 8); secRaw := make([]byte, secretBytes)
    // crypto/rand.Read never returns an error on supported platforms in Go >= 1.24, still check/handle
    ...
    keyID = strings.ToLower(b32.EncodeToString(idRaw))[:keyIDLen]  // 8 bytes -> 13 chars, take 12
    secret := base64.RawURLEncoding.EncodeToString(secRaw)
    full = keyPrefix + "_" + keyID + "_" + secret
    hash = sha256.Sum256([]byte(secret))
}

// Parse: SplitN(key, "_", 3) is REQUIRED. base64url secrets may contain '_' and '-'.
parts := strings.SplitN(raw, "_", 3) // ["snt", keyID, secret]
// reject unless len==3, parts[0]=="snt", len(keyID)==12 && all in [a-z2-7], len(secret)==43 -- BEFORE any DB access

// Verify: always do the hash and a constant-time compare, even for unknown keys (dummy hash), then check revoked.
got := sha256.Sum256([]byte(secret))
ok := subtle.ConstantTimeCompare(got[:], rec.SecretHash) == 1
```
- Cache: `map[string]entry` guarded by `sync.RWMutex`, entry `{tenant, scopes, hash, expires}`, `maxEntries` cap (default 1024). On insert at cap: sweep expired; if still full, evict one arbitrary/oldest entry. Only successful lookups are inserted (negatives never cached, per D-04). The cached hash is still compared on every request, so a wrong secret for a cached key_id never authenticates. Inject a `now func() time.Time` for TTL tests.
- Postgres lookup: `SELECT tenant_id, secret_hash, scopes FROM producer_keys WHERE key_id = $1 AND revoked_at IS NULL`.
- Migration `backend/migrations/postgres/002_producer_keys.sql` (tern format; keep the `---- create above / drop below ----` separator):
```sql
CREATE TABLE producer_keys (
  key_id      text PRIMARY KEY CHECK (key_id ~ '^[a-z2-7]{12}$'),
  tenant_id   text NOT NULL CHECK (tenant_id <> ''),
  secret_hash bytea NOT NULL CHECK (octet_length(secret_hash) = 32),
  scopes      text[] NOT NULL DEFAULT '{}' CHECK (scopes <@ ARRAY['events:write','events:read']),
  name        text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  revoked_at  timestamptz
);
CREATE INDEX producer_keys_tenant_idx ON producer_keys (tenant_id);
CREATE UNIQUE INDEX producer_keys_tenant_name_active_idx ON producer_keys (tenant_id, name) WHERE revoked_at IS NULL;

---- create above / drop below ----

DROP TABLE IF EXISTS producer_keys;
```
- `keyctl` (tooling, pgx directly, password from `PGPASSWORD`, flags like `make migrate`): `create`, `revoke`, `list` per D-05, plus recommended `ensure --tenant --name --scopes --out <file>`: if the file exists and its key_id is active, do nothing; otherwise create (revoking a stale same-name key) and write the full key to the file with mode 0600. The secret can never be recovered from the DB, so `ensure` is the only safe idempotent form for `make seed`. `create` prints the key to stdout once and nothing else secret-bearing; `list` must never print hashes.

### Pattern 5: Redis token bucket (D-19) — verified on redis 8.10.2
```lua
-- KEYS[1]=bucket, ARGV[1]=capacity, ARGV[2]=refill per second, ARGV[3]=cost
local t = redis.call('TIME')
local now_ms = t[1] * 1000 + math.floor(t[2] / 1000)
local cap  = tonumber(ARGV[1]); local rate = tonumber(ARGV[2]); local cost = tonumber(ARGV[3])
local d = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(d[1]); local ts = tonumber(d[2])
if tokens == nil or ts == nil then tokens = cap; ts = now_ms end
local elapsed = now_ms - ts
if elapsed < 0 then elapsed = 0 end
tokens = math.min(cap, tokens + elapsed * rate / 1000)
local allowed = 0; local retry_ms = 0
if cost > cap then retry_ms = -1                       -- can never succeed; config validation prevents this
elseif tokens >= cost then tokens = tokens - cost; allowed = 1
else retry_ms = math.ceil((cost - tokens) * 1000 / rate) end
redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'ts', now_ms)
redis.call('PEXPIRE', KEYS[1], math.ceil(cap * 1000 / rate) + 1000)  -- idle bucket = full bucket; expire after one full refill + 1s
return {allowed, retry_ms, math.floor(tokens)}
```
Verified: first call (cold script, NOSCRIPT fallback) `[1 0 400]` for cap 1000/rate 200/cost 600; immediate second call `[0 999 400]`; after 1.1 s `[1 0 20]`; `cost>cap` `[0 -1 20]`; PTTL 6 s (cap/rate=5 s + 1 s); 300 concurrent cost-1 calls against capacity 100 allowed exactly 100.

```go
var script = redis.NewScript(luaSrc) // package-level var needs //nolint:gochecknoglobals with reason, or hold it in the Limiter struct (preferred: struct field)
res, err := script.Run(ctx, rdb, []string{"rl:" + keyID}, cap, rate, cost).Int64Slice()
// Retry-After seconds = max(1, ceil(res[1]/1000.0))
```
- Key: `rl:<key_id>` (key_id is public and low-risk; do not include secrets). Redis Cluster hash tags not needed (single key per call).
- Bound latency: call with `context.WithTimeout(ctx, cfg.RateLimit.Timeout)` (recommend 250 ms). go-redis defaults (5 s dial, 3 retries) otherwise make a dead Redis cost seconds per request. When Redis is unreachable, go-redis logged repeated "failed to dial after 5 attempts" lines in the scratch run, so also set a short `DialTimeout` and `MaxRetries` on the limiter's client options (tune in the Redis-down test); the per-call context timeout is the guaranteed bound.
- Failure policy: error from `Run` -> `fail_mode=closed`: 503 with `Retry-After`; `open`: log warn (`event=ratelimit_bypass`, no metric until Phase 5) and admit.
- Config rule: `capacity >= ingest.max_batch_events` (validated at load), else a max batch could never be admitted.
- `TIME` inside Lua is allowed on Redis 8 (effects replication) [VERIFIED: ran].

### Pattern 6: ClickHouse events table, reads, TTL
`backend/migrations/clickhouse/002_events.sql` (unqualified names; idempotent; passes `TestClickHouseMigrationsTargetConfiguredDatabase` and the idempotency test). **Verified on 26.3.47.6:**
```sql
CREATE TABLE IF NOT EXISTS events (
  tenant_id        LowCardinality(String),
  event_id         String,
  occurred_at      DateTime64(3, 'UTC'),
  received_at      DateTime64(3, 'UTC'),
  source           LowCardinality(String),
  event_type       LowCardinality(String),
  source_ip        Nullable(String),
  destination_ip   Nullable(String),
  destination_port Nullable(UInt16),
  protocol         LowCardinality(Nullable(String)),
  principal        Nullable(String),
  action           LowCardinality(Nullable(String)),
  outcome          LowCardinality(Nullable(String)),
  attributes_json  String,
  INDEX idx_event_id event_id TYPE bloom_filter(0.01) GRANULARITY 4
) ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, occurred_at, event_id)
TTL toDateTime(occurred_at) + INTERVAL 7 DAY;
```
Do not add `SETTINGS non_replicated_deduplication_window`. `EXPLAIN indexes=1` lists the `Skip idx_event_id bloom_filter` index and the primary key narrows on `tenant_id`.

**Configurable TTL (decision):** keep 7 DAY in the migration (immutable history). Add a flag to `tools/chmigrate`: `--events-ttl-days` (default from plain env `SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS`, like the password, so Make's `dotenv_or_env` works; default 7). After migrations it runs, over the existing HTTP runner, one statement `ALTER TABLE <db>.events MODIFY TTL toDateTime(occurred_at) + INTERVAL <N> DAY` with `N` parsed as an int in `1..3650` (never string-interpolate user text). Verified that repeating the identical ALTER is a no-op that succeeds, so it is safe on every `make migrate`. `MODIFY TTL` applies to new parts and to old parts on merge; `ALTER TABLE events MATERIALIZE TTL` rewrites existing parts and is a documented manual step, not automatic. Document in `docs/` and `.env.example`.

**Read SQL (exact, parameterized, tenant-scoped; verified):**
```sql
-- single-event lookup (D-13). First-received row wins, matching ADR 0002 ("later event dropped").
SELECT event_id, tenant_id, occurred_at, received_at, source, event_type, source_ip,
       destination_ip, destination_port, protocol, principal, action, outcome, attributes_json
FROM events
WHERE tenant_id = {tenant:String} AND event_id = {id:String}
ORDER BY received_at ASC
LIMIT 1
```
```sql
-- count used by the double-insert test and make seed
SELECT uniqExact(event_id) FROM events WHERE tenant_id = {tenant:String}
-- seed verification adds:  AND received_at >= {since:DateTime64(3)}
-- generic dedupe for future list reads (Phase 4):
SELECT ... FROM events WHERE tenant_id = {tenant:String} AND ... ORDER BY occurred_at, event_id, received_at LIMIT 1 BY event_id
```
Go: `conn.QueryRow(ctx, sql, clickhouse.Named("tenant", t), clickhouse.Named("id", id)).Scan(...)`. Scan `Nullable` columns into `*string` / `*uint16`, `DateTime64` into `time.Time` (location UTC; verified).

**Verified dedupe numbers** (same 3 events inserted twice, no OPTIMIZE): raw `count()` = 6, `uniqExact(event_id)` = 3, `LIMIT 1 BY event_id` returns 3 rows, `count()` over the `LIMIT 1 BY` subquery = 3. With identical `occurred_at`, `FINAL` also gave 3 on a table that had the dedup window enabled (the window dropped the second block entirely), and on the default table `FINAL` over rows with different `occurred_at` gave 6 vs `uniqExact` 3 (Pitfall 1).

clickhouse-go native batch insert, verified:
```go
conn, err := clickhouse.Open(&clickhouse.Options{
    Addr: []string{cfg.NativeAddress},
    Auth: clickhouse.Auth{Database: cfg.Database, Username: cfg.User, Password: cfg.Password},
    DialTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
    MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Hour,
    Compression: &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
})
_ = conn.Ping(ctx)                                   // readiness + startup; honours ctx
b, err := conn.PrepareBatch(ctx, "INSERT INTO events") // column order = table order
err = b.Append(tenant, id, occurredAt /*time.Time*/, receivedAt, source, eventType,
               srcIP /*nil or *string*/, dstIP, dstPort /**uint16*/, protocol, principal, action, outcome, attrsJSON)
err = b.Send()                                        // abort a failed batch with b.Abort() before retrying; re-prepare per attempt
// per-query settings: clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"max_execution_time": 5}))
```
Pass `*string` nil for NULL. A 5000-row batch is one block (below `max_insert_block_size` 1,048,449), so a `Send()` is all-or-nothing; a retry after an ambiguous failure can produce duplicates, which read-time dedupe absorbs.

**`insert_deduplication_token`:** usable only via `clickhouse.WithSettings`, and only meaningful when the table sets `non_replicated_deduplication_window` (default `0`, verified) or is replicated. Verified: with the window set to 1000, two identical-token inserts stored 3 rows, not 6; without it, no effect. Decision: do not use in Phase 2 (keeps the STOR-03 test honest). Revisit in Phase 5 as a measured optimization.

### Pattern 7: In-flight load shedder (D-10, verified)
```go
func InFlight(limit int) echo.MiddlewareFunc {
    sem := make(chan struct{}, limit)
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            select {
            case sem <- struct{}{}:
                defer func() { <-sem }()
                return next(c)
            default:
                return errs.NewServiceUnavailableError("server is busy, retry shortly", 1) // GlobalErrorHandler sets Retry-After: 1
            }
        }
    }
}
```
Verified: with limit 2 and a blocked handler, the 3rd concurrent request got 503 immediately with `Retry-After: 1`. Sizing rule for ING-07: worst-case request memory is about `max_in_flight * max_body (1 MiB)` plus decoded structures (estimate 3-6x, LOW confidence on the multiplier). Set `MaxBufferedBytes` default at least `max_in_flight * max_body` (64 MiB) and `MaxBufferedRecords` at least `max_in_flight * 500` (32,000; default 50,000). Validate `MaxBufferedRecords >= max_batch_events` at config load, otherwise a full batch can never be buffered.

### Pattern 8: Event-writer loop (D-16, D-18)
Consumer client options (verified names): `kgo.ConsumerGroup(group)`, `kgo.ConsumeTopics(topic)`, `kgo.DisableAutoCommit()`, `kgo.BlockRebalanceOnPoll()`, `kgo.FetchMaxBytes`, `kgo.FetchMaxPartitionBytes`, `kgo.MaxConcurrentFetches(2)` (bounds fetch memory; the default is unbounded), `kgo.SessionTimeout`, `kgo.RebalanceTimeout`. New groups start at the beginning of partitions by default.

```go
// Source: go doc kgo + scratch run (consumed 5 of 5, CommitRecords nil)
for ctx.Err() == nil {
    pollCtx, cancel := context.WithDeadline(ctx, batchDeadline) // flush interval counted from the first record
    fs := cl.PollRecords(pollCtx, room)                          // room = hardCap - len(batch)
    cancel()
    if fs.IsClientClosed() { return }
    fs.EachError(func(t string, p int32, err error) {
        // context.DeadlineExceeded/Canceled from our own pollCtx = timer tick, NOT an error; others: log, count, continue
    })
    fs.EachRecord(func(r *kgo.Record) { batch = append(batch, r) })
    if len(batch) >= flushSize || timerExpired { flush(); cl.AllowRebalance() }
}
```
`flush()` order (all before the commit):
1. Decode each record with `contracts.DecodeEvent` and `Validate(*ev.ReceivedAt, limits)` (use the event's own `received_at` as "now"; see Pitfall 9). Missing `received_at` or any failure = poison.
2. Poison: `producer.ProduceSync(ctx, dlqRecord).FirstErr()`; failure = treat as transient (retry with backoff, do NOT commit).
3. Good rows: `PrepareBatch/Append/Send` with bounded retries (e.g. 5 attempts, base 200 ms, cap 10 s, full jitter). On exhaustion return an error and let the process exit non-zero (restart reprocesses; nothing was committed). Do not route a failed insert batch to the DLQ.
4. `cl.CommitRecords(commitCtx, lastRecordPerPartition...)` — pass only the last record per partition (it commits offset+1), in order.
5. `cl.AllowRebalance()`.

Shutdown: when `ctx` is cancelled, stop polling, finish the in-flight flush with a fresh `context.WithTimeout(context.Background(), flushDeadline)` below the 30 s lifecycle budget (e.g. 20 s), commit, then `cl.CloseAllowingRebalance()` (plain `Close()` hangs after a poll with `BlockRebalanceOnPoll`). The closer registered in `app.Lifecycle` must wait on a `done` channel with the closer's ctx deadline.

DLQ record: `Topic: dlqTopic, Key: orig.Key, Value: orig.Value (or empty if len > cap)`, headers `dlq.reason` (short enum string, e.g. `decode_error`, `validation_error`, `missing_received_at`; no raw payload fragments), `dlq.origin.topic`, `dlq.origin.partition`, `dlq.origin.offset`, `dlq.at` (RFC 3339 UTC). Oversize or sensitive handling (discretion): drop the value and add `dlq.value.redacted=true`, `dlq.value.bytes=<n>`, `dlq.value.sha256=<hex>`. The DLQ producer is a second `kgo.Client` (acks=all, idempotent) so its lifecycle is independent.

### Pattern 9: Ops listener (D-20)
- `opshttp.Server` is a plain `http.Server` built with `httpx.New` plus a `net/http` mux (Echo not needed): `GET /healthz` -> 200 `{"status":"ok"}`; `GET /readyz` -> 200/503 with `{"status":"ready|not_ready","checks":{"kafka":{"ok":true,"latency_ms":3},...}}`.
- Run checks in parallel, each with its own timeout (default 2 s, `ops.check_timeout`); cache the last result for about 1 s so Compose/K8s probes cannot hammer dependencies. Return only a fixed word (`"unavailable"`) per failing check; log the detail (it may contain hostnames or credentials).
- Checks: Kafka `client.Ping(ctx)` (metadata request, verified), ClickHouse `conn.Ping(ctx)`, Postgres `pool.Ping(ctx)`, Redis `rdb.Ping(ctx)`. Per binary: ingest = kafka+redis+postgres; event-writer = kafka+clickhouse; query-api = clickhouse+postgres+redis; detector = kafka; alert-writer = kafka+postgres+clickhouse. Detector/alert-writer need a lightweight `kafkax.NewClient` just for `Ping` (they currently only TCP-dial).
- Bind: `ops.bind` default `127.0.0.1`, Compose sets `0.0.0.0` inside the container (the host publish stays `127.0.0.1:910x`). Ports by service via a default table in `config.setDefaults`: ingest 9101, detector 9102, event-writer 9103, alert-writer 9104, query-api 9105; public `server.port` defaults ingest 8080, query-api 8081.
- Start: `net.Listen` synchronously in `setup` (a port clash fails startup with exit 1), then `go srv.Serve(l)`. Registration order for LIFO shutdown: register the ops server closer FIRST (closes last), dependencies next, public HTTP server, and a final "draining" closer that flips `readyz` to 503 (runs first). This gives probes a clean drain signal.
- Fatal background errors (consumer loop gave up) cannot currently stop `app.Run` (it only waits on ctx). Recommend a small `app` change: `RunContext` derives `ctx, cancel := context.WithCancelCause`, and `Lifecycle.Fail(err)` cancels it so the process exits 1 and Compose restarts it. Needs a unit test in `app_test.go`.
- The existing `httpx.Server` binds `":"+port` (all interfaces). Compose publishes to 127.0.0.1 only; for `make run` consider adding `server.bind` (default `127.0.0.1`). Optional.

### Pattern 10: Generator `send` (D-21)
- `pyproject.toml`: `dependencies = ["httpx==0.28.1"]`, `[project.scripts] sentinel-generator = "sentinel_generator.cli:main"` (works with the `uv_build` backend). Then `uv lock` and regenerate `generator/requirements.txt` with the `uv export` command quoted in that file's header (the CI `lockfiles` check requires it to exist, and `uv lock --check` must pass).
- Anchoring (important): `build_event` places event `i` at `base_time + i seconds`. With 1000 events a `base_time = now` run ends 16 minutes in the future and fails the +5 m skew. Use `run_start = datetime.now(UTC)` and `base_time = run_start - timedelta(seconds=count)`; the last event is at `run_start - 1 s`, all within the 7-day past window. `event_id` embeds the timestamp, so a given seed yields different IDs on different runs; make the batch bytes once (`json.dumps(..., separators=(",", ":"))`) and resend those exact bytes on retry.
- Client: `httpx.Client(timeout=httpx.Timeout(10.0, connect=3.0), headers={"X-API-Key": key})`. Retry on 429/503 and on `httpx.TransportError`/timeouts; wait `min(Retry-After, 30)` seconds (parse int seconds; fallback exponential backoff with jitter); max attempts default 5; fail fast on 400/401/403/413. Never log headers or the key; ruff `S113` requires explicit timeouts.
- Key: env `SENTINEL_API_KEY` first, else file `.local/keys/<tenant>.key` (path override flag is fine; the key itself is never a flag).
- Output: totals `sent`, `accepted`, `rejected`, `retries`, plus `run_start` (UTC ms) and the first few event IDs; write the same to `.local/last-send.json` for `make seed` verification. Exit non-zero if `accepted != count`.
- Tests: `httpx.MockTransport` for retry/Retry-After/idempotent resend; assert events pass a skew check (`max(occurred_at) <= now`).

### Pattern 11: Dockerfile and Compose (D-23) — build verified
Verified: this Dockerfile built successfully with the repo's `backend/` (image size 25.97 MB), the distroless entrypoint ran and failed config validation as expected, and the probe binary exits 1 on a dead URL.
```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.26.9@sha256:d7722066f0b60ceccb6c0643cbed1f5f9e15506ac237146c95333504d7805d89 AS build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-trimpath
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    for s in ingest detector event-writer alert-writer query-api; do \
      go build -ldflags="-s -w" -o /out/$s ./cmd/$s || exit 1; \
    done && \
    go build -ldflags="-s -w" -o /out/healthprobe ./tools/healthprobe

FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/ /app/
USER 65532:65532
ENTRYPOINT ["/app/ingest"]
```
Digests resolved 2026-10-12: golang:1.26.9 index `sha256:d7722066...805d89` (tag updated 2026-10-10; amd64 `cf7e928c...`), distroless/static:nonroot index `sha256:e2e927ec...f555e3` (gcr.io registry API). Add a root `.dockerignore` (exclude `.git`, `.env`, `.local`, `web/node_modules`, `.planning`, `**/.env`) because the build context is the repo root and `backend/.env` exists locally; verify the image contains no `.env` (`docker run --rm --entrypoint /app/healthprobe` cannot list files; check with `docker create` + `docker cp` or a build-time `RUN test ! -e` in the build stage).

`tools/healthprobe` is about 20 lines: `GET os.Args[1]` with a 3 s timeout, exit 0 only on HTTP 200 (code verified in the build).

Compose rules (all verified against `scripts/check-pins.sh`):
- Do NOT add an `image:` line to app services (the script requires every `image:` to carry `@sha256:`). Use `build: {context: ../.., dockerfile: deployments/docker/Dockerfile}` only; share it via a YAML anchor.
- Published ports must be quoted `"127.0.0.1:8080:8080"` (the script's regex only matches quoted strings and requires the `127.0.0.1` prefix).
- Per-service `entrypoint: ["/app/ingest"]`, `restart: unless-stopped`, `depends_on` with `condition: service_healthy` for kafka/clickhouse/postgres/redis, and `healthcheck: {test: ["CMD", "/app/healthprobe", "http://127.0.0.1:9101/readyz"], interval: 5s, timeout: 5s, retries: 20, start_period: 10s}`.
- Environment inside the network uses service DNS: `SENTINEL_KAFKA.BROKERS=kafka:9092` (the INTERNAL advertised listener), `SENTINEL_CLICKHOUSE.HTTP_ADDRESS=clickhouse:8123`, `SENTINEL_CLICKHOUSE.NATIVE_ADDRESS=clickhouse:9000`, `SENTINEL_DATABASE.HOST=postgres`, `SENTINEL_REDIS.ADDRESS=redis:6379`, `SENTINEL_PRIMARY.ENV=local`, `SENTINEL_DATABASE.SSL_MODE=disable`, `SENTINEL_OPS.BIND=0.0.0.0`. Passwords: `${SENTINEL_POSTGRES_PASSWORD:?set in .env}` and `${SENTINEL_CLICKHOUSE_PASSWORD:?set in .env}`. Dotted env names are fine in the Compose `environment:` map (Docker passes them verbatim; the Phase 1 lifecycle tests already exec binaries with dotted names).
- `make dev-up`: infra `up -d --wait`, `topic-init`, `make migrate` (host side), then `$(COMPOSE) up -d --build --wait ingest event-writer query-api`. Dotenv loading for `SENTINEL_PRIMARY.ENV=local` tries `../.env` and tolerates `ErrNotExist`, so containers without a `.env` start.

### Pattern 12: Config additions (consistent with `config.Load(service)`)
New sections/keys (all via `setDefaults`, validated with `validate` tags, `.env.example` updated with placeholders, `check-pins.sh env` rule: keys ending in PASSWORD/SECRET/TOKEN/API_KEY/_KEY must start with `change-me`):

| Key | Default | Notes |
|---|---|---|
| `SENTINEL_SERVER.PORT` | ingest 8080, query-api 8081 | per-service default table |
| `SENTINEL_OPS.PORT` | 9101..9105 by service order | `ops.bind` default `127.0.0.1`; `ops.check_timeout` 2s |
| `SENTINEL_INGEST.MAX_BATCH_EVENTS` | 500 | max 500 (contract) |
| `SENTINEL_INGEST.MAX_BODY_BYTES` | 1048576 | |
| `SENTINEL_INGEST.MAX_IN_FLIGHT` | 64 | min 1 |
| `SENTINEL_INGEST.PRODUCE_TIMEOUT` | 10s | must be < `server.write_timeout` (30s default) |
| `SENTINEL_INGEST.KEY_CACHE_TTL` / `KEY_CACHE_SIZE` | 30s / 1024 | |
| `SENTINEL_KAFKA.EVENTS_TOPIC` / `DLQ_TOPIC` | `security.events.v1` / `security.events.dlq.v1` | must be configurable so integration tests use unique topics |
| `SENTINEL_KAFKA.MAX_BUFFERED_RECORDS` / `MAX_BUFFERED_BYTES` | 50000 / 67108864 | `records >= max_batch_events` |
| `SENTINEL_WRITER.GROUP` | `sentinel-event-writer` | tests override for isolation |
| `SENTINEL_WRITER.BATCH_SIZE` / `BATCH_HARD_CAP` / `FLUSH_INTERVAL` | 5000 / 20000 / 1s | |
| `SENTINEL_WRITER.MAX_ATTEMPTS` / `BACKOFF_BASE` / `BACKOFF_MAX` | 5 / 200ms / 10s | |
| `SENTINEL_WRITER.DLQ_MAX_VALUE_BYTES` | 65536 | |
| `SENTINEL_RATELIMIT.CAPACITY` / `REFILL_PER_SEC` / `TIMEOUT` | 10000 / 2000 / 250ms | `capacity >= ingest.max_batch_events` |
| `SENTINEL_CLICKHOUSE.NATIVE_ADDRESS` | (existing, optional) | now REQUIRED for event-writer, query-api, alert-writer: add a check in `validateSection` (the struct tag is shared and `omitempty`) |
| `SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS` | 7 | plain-name env for chmigrate/Make only |

`requiredSections()` change: `ServiceIngest: {redis, kafka, database}`. koanf already unmarshals `time.Duration` values from env strings (contracts.future_skew=5m works today).

### Anti-Patterns to Avoid
- **`FINAL` or `OPTIMIZE` for correctness:** neither collapses same-`event_id`/different-`occurred_at` rows (Pitfall 1), and merges are asynchronous.
- **Committing in `OnPartitionsRevoked` without `BlockRebalanceOnPoll`:** pick one strategy. This plan uses `BlockRebalanceOnPoll` + `AllowRebalance` after the commit; no custom revoke commit is needed.
- **Creating a `kgo.Client` per request:** one long-lived producer; per-request `ProduceSync` only.
- **Routing a failed ClickHouse insert to the DLQ:** the DLQ is for undecodable/invalid records only; infrastructure failures must stop and retry.
- **Writing to `c.Response()` ad hoc in handlers:** return `errs.*` and let `GlobalErrorHandler` write the envelope.
- **Interpolating anything into SQL:** use `{name:Type}` server-side params (ClickHouse) and `$n` (Postgres). The TTL ALTER is the sole constructed statement and takes a validated integer.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Kafka protocol, idempotent producer, group management | Anything on raw sockets / own offset tracking | `kgo` | Sequence numbers, retries, rebalances |
| ClickHouse native protocol / block encoding | HTTP `INSERT ... FORMAT` writer | `clickhouse-go` `PrepareBatch` | Typed Nullable/DateTime64 mapping, compression |
| Token bucket arithmetic across replicas | In-memory limiter per replica | the Lua script on Redis | Shared quota, atomic with Redis `TIME`, no clock skew |
| Constant-time compare, hashing, random | Custom byte loops / `math/rand` | `crypto/subtle`, `crypto/sha256`, `crypto/rand` | Timing and entropy |
| Event validation | A second validator for ingest | `contracts.DecodeEvent` + `Validate` | Single source of truth, drift-tested against OpenAPI |
| Error envelope, Retry-After header | New response writers | `errs.New*Error` + `GlobalErrorHandler` | Already conformant to spec §8 |
| Server lifecycle | Own signal handling | `app.Run` / `Lifecycle` | LIFO closers, 30 s budget |
| Migration runners | Ad hoc DDL in main | tern (`database.Migrate`) and `chmigrate` | Idempotent, tested |
| Readiness HTTP semantics | Per-binary one-off handlers | one `opshttp` package | Same JSON and timeouts everywhere |

**Key insight:** the risky parts of this phase are all ordering rules (validate before publish, insert before commit, DLQ-ack before commit, count before limit). Use library primitives for everything else and spend the effort on tests that break those orderings.

## Runtime State Inventory

Not a rename/refactor phase. Greenfield additions only; the one existing-state concern is the Phase 1 integration harness: `serviceEnv` in `backend/test/lifecycle_test.go` and the shared `TestMain` containers must be extended (ingest needs `SENTINEL_DATABASE.*`; event-writer, query-api, alert-writer need `SENTINEL_CLICKHOUSE.NATIVE_ADDRESS`; each parallel subtest needs a unique `SENTINEL_OPS.PORT` and `SENTINEL_SERVER.PORT`, allocated with an exported `FreePort`). Stored data / live config / OS-registered state / secrets / build artifacts: none affected (verified by reading the repo; existing Compose volumes hold only empty schemas from Phase 1).

## Common Pitfalls

### Pitfall 1: `FINAL`/merges do not dedupe by `event_id` here
**What goes wrong:** Reads using `FINAL` (or relying on merges) still show duplicates when the same `event_id` arrives with a different `occurred_at`.
**Why:** ReplacingMergeTree collapses rows with an identical full sort key `(tenant_id, occurred_at, event_id)`. Verified: same ids inserted at two different `occurred_at` -> `FINAL` count 6, `uniqExact(event_id)` 3.
**How to avoid:** every read uses `LIMIT 1 BY event_id` (lists) or `uniqExact(event_id)` (counts) or the ordered `LIMIT 1` lookup. Add a test with the same id and different `occurred_at`.
**Warning signs:** a PR that adds `FINAL` or `OPTIMIZE` anywhere in non-test code.

### Pitfall 2: Table-level dedup window hides the bug the test is meant to catch
**What goes wrong:** With `non_replicated_deduplication_window > 0` an identical second insert is dropped, so the "double insert" test passes without exercising read-time dedupe (verified: raw count stayed 3).
**How to avoid:** never set it in `002_events.sql`; in the test, assert `SELECT count()` raw is 2N (proves the duplicates were really stored) and `uniqExact`/lookup shows N.

### Pitfall 3: `ProduceSync` ignores your deadline for in-flight idempotent records
**What goes wrong:** By default franz-go waits for the outcome of in-flight idempotent records instead of cancelling, so HTTP handlers can overrun `produce_timeout`.
**How to avoid:** `kgo.AllowIdempotentProduceCancellation()` + `RecordDeliveryTimeout` + `RecordRetries` (Pattern 3). Cost: an application-level retry of a cancelled record may duplicate, which ADR 0003 already requires downstream to absorb. Test: pause the Kafka container, assert 503 within about `produce_timeout + 2 s`, unpause, resend the identical batch, assert one visible copy per `event_id`.

### Pitfall 4: Echo `:batch` is a parameter
Covered in Pattern 1. Also: tests must send a literal colon (httptest `NewRequest("POST", "/api/v1/events:batch", ...)` works; `%3A` 404s).

### Pitfall 5: Empty CORS list means allow `*`
**Verified in Echo source:** `if len(config.AllowOrigins) == 0 { config.AllowOrigins = DefaultCORSConfig.AllowOrigins }` and the default is `["*"]`. `middleware.CORS()` currently passes the config list straight through. **Fix:** when the list is empty, return an Echo middleware that is a no-op (or set `Skipper: func(echo.Context) bool { return true }`) so no `Access-Control-*` headers are emitted. Test: request with `Origin: https://evil.example` gets no `Access-Control-Allow-Origin`.

### Pitfall 6: Request logger records the query string and the global error handler logs everything at Error
`RequestLogger` logs `v.URI` (includes the query string). Switch to `v.URIPath` (already enabled via `LogURIPath: true`) and drop `LogURI`. `GlobalErrorHandler` logs every error with `.Error().Stack()`, which floods logs during 401/429/503 storms; downgrade levels for status below 500 to warn (keep stack only for 5xx). Never log `X-API-Key` or bodies (no change needed unless a future debug logger is added; add a regression test that a request with a distinctive key leaves no occurrence of it in captured log output).

### Pitfall 7: `ProducerBatchMaxBytes`/`max.message.bytes` vs DLQ values
Events are at most about 1 MiB; Kafka's default `max.message.bytes` is 1 MiB. The DLQ cap (64 KiB, with redaction beyond) keeps DLQ records well below the limit.

### Pitfall 8: check-pins and Compose
`image:` lines without digests fail `scripts/check-pins.sh images`; unquoted or non-loopback published ports fail it too. See Pattern 11. Also `check-makefile-dotenv.sh` exists: keep new Makefile targets reading secrets through `dotenv_or_env`, not `include .env`.

### Pitfall 9: Writer skew validation against wall clock
If the writer calls `Validate(time.Now(), ...)`, a legitimate event whose `occurred_at` was 6.9 days old at ingest becomes invalid after a lagging consumer crosses 7 days, or a consumer replay after a long outage DLQs good data. Validate with `now = event.ReceivedAt` (the skew rule was applied at ingest), and treat a missing `received_at` as poison.

### Pitfall 10: Parallel lifecycle tests and fixed ops ports
`TestBinaryLifecycle` runs several subtests per service in parallel; fixed ports 9101-9105 collide. Allocate ports (export `FreePort` from `internal/testing`) and set `SENTINEL_OPS.PORT` / `SENTINEL_SERVER.PORT` in `serviceEnv`. Also `go vet`/lint run with `-tags=integration` in CI; keep the new tests lint-clean (`testpackage`, `tparallel`).

### Pitfall 11: Generator `occurred_at` runs into the future
See Pattern 10 (anchor to `run_start - count seconds`). Without it, batches past about the 300th event are rejected with 400 skew errors.

### Pitfall 12: `make seed` idempotency and key secrecy
A key secret cannot be re-derived, so a re-run must reuse `.local/keys/<tenant>.key` (keyctl `ensure`). Add `/.local/` to `.gitignore` (it is currently NOT ignored) and to `.dockerignore`. Do not echo keys; pass the key to the generator through the environment or the file path; avoid `curl -H "X-API-Key: $(cat ...)"` on the host because argv is visible in `ps` (use `curl -H @file` or let the generator do the lookup spot-check). Seed verification should count only this run: `uniqExact(event_id) WHERE tenant_id='demo' AND received_at >= <run_start from .local/last-send.json>`, otherwise a second `make seed` sees 2000.

### Pitfall 13: Cooperative rebalances and partial commits
With `BlockRebalanceOnPoll`, the batch must be flushed and `AllowRebalance()` called on a bounded cadence (1 s interval + insert time, worst case retries about 30 s, below the default 60 s `RebalanceTimeout`). Keep `MAX_ATTEMPTS * BACKOFF_MAX` under that bound (validate or document).

## Code Examples

### Double-insert, no-OPTIMIZE test (write first, per CONTEXT specifics)
```go
// Source: derived from the verified scratch run; integration tag, real ClickHouse container
func TestDedupeDoubleInsertNoOptimize(t *testing.T) {
    t.Parallel()
    cfg := sentineltesting.RequireClickHouse(t)    // own container; or use shared chCfg with a unique tenant
    // apply migrations.ClickHouse() via chmigrate.New(...).Run(ctx, fs)
    conn := mustOpen(t, cfg)
    ids := insertEvents(t, conn, "t-dedupe", 50)   // helper inserts N rows, returns ids
    insertEvents(t, conn, "t-dedupe", 50, withSameIDs(ids)) // second insert, same ids
    require.EqualValues(t, 100, scalar(t, conn, `SELECT count() FROM events WHERE tenant_id = 't-dedupe'`)) // duplicates really stored
    require.EqualValues(t, 50, scalar(t, conn, `SELECT uniqExact(event_id) FROM events WHERE tenant_id = 't-dedupe'`))
    row, err := reader.Get(ctx, "t-dedupe", ids[0])
    require.NoError(t, err) // exactly one row, no multi-row error
    // no OPTIMIZE anywhere
}
```

### Crash and restart of the event-writer (no loss, no visible dupes)
```go
// Black box with the real binary: startProc(t, "event-writer", ...) from lifecycle_test.go
// 1. unique topic + group per test (kadm.CreateTopic), BATCH_SIZE=200, FLUSH_INTERVAL=200ms
// 2. produce N=20000 valid events with kgo (unique tenant per test)
// 3. start writer; poll CH until 0 < uniqExact < N; p.cmd.Process.Kill() (SIGKILL, no flush)
// 4. start a second writer with the same group; eventually (require.Eventually, no sleeps) uniqExact == N
// 5. assert: uniqExact == N; `SELECT count() FROM (SELECT event_id ... LIMIT 1 BY event_id)` == N;
//    raw count() >= N (re-delivery after the kill is allowed and expected to be absorbed)
```

### Partial publish then identical retry (ING-04)
```go
// Real Kafka + real writer + real ClickHouse; fault injection through the Publisher seam, not a mock sink:
// faultyPublisher wraps the real kafkax producer, publishes records[:k] for real, then returns an error.
// POST batch -> 503. POST the identical batch with the real publisher -> 202.
// Wait for the writer; assert uniqExact(event_id) == N and the lookup for each id returns one row.
// Second variant: pause the Kafka container (docker pause <GetContainerID()>) -> 503 within produce_timeout+2s;
// unpause -> identical retry -> one visible copy.
```

### 429 and fail-closed Redis
```go
// Config: capacity=500, refill=1/s. POST 500 events -> 202; POST 1 event -> 429 with integer Retry-After >= 1.
// Redis down: dedicated redis container; `docker pause`/stop it -> POST valid batch -> 503 envelope and
// NOTHING published (consume the topic: zero new records). FAIL_MODE=open -> 202 and a warn log line.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Sarama / segmentio kafka-go | franz-go (kgo) | locked D-11 | Idempotent producer by default, richer group control |
| clickhouse-go v1 / database/sql batches | clickhouse-go v2 native `PrepareBatch` | v2.x | Typed batches, no database/sql overhead |
| ReplacingMergeTree + `FINAL` everywhere | `LIMIT 1 BY` / `uniqExact` with explicit read semantics | this design | FINAL does not match the sort-key mismatch case here |
| `docker compose healthcheck` using curl | static probe binary in distroless | this design | Image has no shell, curl, or wget |

**Deprecated/outdated:** CLAUDE.md's Technology Stack section (Clerk, asynq, New Relic, ts-rest) describes the removed boilerplate. Phase 1 deleted these and CI forbids them.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `HSET`/`PEXPIRE` on an idle bucket with `PEXPIRE = cap/rate + 1 s` correctly equals a full bucket after expiry (a missing key restarts at full capacity) | Pattern 5 | A bucket could be granted more than steady-state quota only if the TTL were shorter than a full refill; the chosen TTL is not. Low |
| A2 | Memory-per-request multiplier of 3-6x over body size | Pattern 7 | Mis-sized `MAX_IN_FLIGHT`/buffers; mitigated by caps and by Phase 5 load testing |
| A3 | go-redis connection retries/dial behaviour: bounding latency with a per-call context timeout (250 ms) is sufficient to keep fail-closed 503s fast | Pattern 5 | Slow 503s under Redis outage; verify in the Redis-down integration test and, if needed, set `opts.MaxRetries`/`DialTimeout` in the limiter's own client |
| A4 | Validation errors (400) take precedence over tenant mismatch (403) when both exist in one batch | Pattern 2 | Only an ordering/UX choice; either satisfies D-07/D-08 (planner may flip) |
| A5 | DLQ redaction for oversize values = empty value + `dlq.value.*` headers | Pattern 8 | CONTEXT says only "redacted if over a size cap"; confirm shape with user if they care |
| A6 | `clickhouse-client` inside the container picks up `CLICKHOUSE_PASSWORD` from its environment so `make seed` need not pass the password in argv | Pitfall 12 | Verified here as: `docker exec scr-ch clickhouse-client --user sentinel --query "select 1"` returned 1 without a password flag. Treat as verified for this image; re-check on image bump |
| A7 | `LowCardinality(Nullable(String))` is acceptable for protocol/action/outcome in Phase 3/4 queries | Pattern 6 | Verified to create and accept NULLs; query-shape implications for Phase 4 are unproven (LOW) |
| A8 | Compose builds three services from the same `build:` block fast enough thanks to the BuildKit cache | Pattern 11 | Slower `make dev-up`; fallback `docker compose build ingest` once and `pull_policy: never` for the others |

## Open Questions (RESOLVED)

1. **How should a permanently failing ClickHouse insert (non-transient error) behave?**
   - Known: D-16 bounds retries; DLQ is for undecodable records only.
   - Unclear: whether to crash-loop (recommended, loud, nothing committed) or park the batch.
   - Recommendation: exit non-zero after `MAX_ATTEMPTS`; Compose restarts; Phase 5 adds alerts on restart loops.
   - **RESOLVED:** D-26 in CONTEXT. Exit non-zero after MAX_ATTEMPTS with nothing committed; Compose restarts; never DLQ an insert failure.

2. **Ingest 403-versus-400 precedence and exact `error.details` shape.**
   - Known: `errs.FieldError` and `NewBadRequestError(message, override, code, []FieldError, action)` exist.
   - Recommendation: field path `events[<i>].<field>`; keep messages from `contracts.FieldError`; cap the number of reported errors (e.g. 50) to bound response size.
   - **RESOLVED:** D-27. Tenant mismatch (403) takes precedence over field errors (400); details use `events[<i>].<field>`, capped at 50.

3. **OpenAPI doc says `GET /api/v1/events/{event_id}` uses `BearerAuth` only.**
   - Known: D-13 uses the API key. `backend/test/openapi_drift_test.go` and CI validate the document.
   - Recommendation: edit `api/openapi.yaml` so `getEvent` accepts `ApiKeyAuth` (and keep `BearerAuth` as the Phase 4 alternative), regenerate `web` types, and let `TestOpenAPI*` confirm. Update ADR 0003's `:batch` note.
   - **RESOLVED:** D-28. `getEvent` accepts ApiKeyAuth or BearerAuth; regenerate web types; update the ADR 0003 `:batch` note.

4. **Should `server.bind` be added to `httpx` for `make run`?** It listens on all interfaces today. Optional hardening; Compose is already loopback-published.
   - **RESOLVED:** D-29. Add an optional bind address (default all interfaces in containers; `make run` binds 127.0.0.1).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker | Compose, testcontainers, Dockerfile build | yes | 29.1.3 | none (required) |
| Go | build/test | yes (local 1.25.5; toolchain download) | 1.25.5, use `GOTOOLCHAIN=go1.26.9` | — |
| uv | generator | yes | 0.9.18 | — |
| Apache Kafka / ClickHouse / Redis / Postgres images | stack + tests | yes (pulled and run in this session) | 4.3.1 / 26.3.47.6 / 8.10.2 / 18.6 | — |
| `golang:1.26.9`, distroless/static images | Dockerfile | yes (pulled, built OK) | per digests above | — |
| curl, python3 | seed verification scripts | yes | — | — |
| slopcheck | package audit | installed | — | marked as limited for Go modules |

The `sentinel` Compose stack was not running when checked (only other projects' containers were present); no existing containers were touched. Scratch containers (`scr-kafka`, `scr-ch`, `scr-redis`) were removed.

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Go framework | `go test` + testify v1.12.1; unit tests next to code (`*_test.go`, `testpackage`), integration tests in `backend/test/` with `//go:build integration` and shared containers from `TestMain` |
| Python framework | pytest 9.1.1, mypy strict, ruff (`generator/`) |
| Config files | `backend/.golangci.yml`, `generator/pyproject.toml`, `scripts/go-test-nonempty.sh` |
| Quick run command | `cd backend && GOTOOLCHAIN=go1.26.9 go test -race ./internal/...` and `cd generator && uv run --locked pytest` |
| Full suite command | `make test && make lint && make test-integ` (integration: `go test -tags=integration -timeout 10m -p 1 ./test/...`) |

### Phase Requirements -> Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ING-01 | valid key + batch -> 202; tenant from key; key hashed at rest; cache TTL, cap, negatives uncached | unit + integration | `go test ./internal/apikeys/... ./internal/ingest/... -run 'TestAPIKey\|TestIngestAuth'` | Wave 0 |
| ING-01 | route literal colon: `:batch` matches, `:xyz` 404 | unit | `go test ./internal/ingest/... -run TestRouteLiteralColon` | Wave 0 |
| ING-02 | all-or-nothing 400 with indexed details, nothing published | integration | `go test -tags=integration ./test/... -run TestIngestInvalidBatch` | Wave 0 |
| ING-03 | 202 only after ack; response fields; key `tenant\|ip`; headers | integration | `... -run TestIngestAcceptedAfterAck` | Wave 0 |
| ING-04 | partial publish + 503 + identical retry = one visible copy; paused-broker timeout 503 | integration | `... -run 'TestIngestRetryNoDuplicates\|TestIngestKafkaTimeout503'` | Wave 0 |
| ING-05 | 401 (missing/malformed/unknown/revoked identical body), 403 tenant + scope, 413 body and >500, 503 envelope | integration | `... -run 'TestIngestStatusCodes'` | Wave 0 |
| ING-06 | 429 + Retry-After; Redis down closed -> 503 and zero published; open -> 202 + warn | integration + unit (Lua) | `... -run 'TestRateLimit'` | Wave 0 |
| ING-07 | in-flight shedding returns 503 immediately; producer buffer config bounded | unit + integration | `go test ./internal/ingest/... -run TestInFlightShed`; `... -run TestIngestShedsUnderLoad` | Wave 0 |
| STOR-01 | commit only after insert; no commit on insert failure | integration | `... -run 'TestWriterCommitAfterInsert\|TestWriterInsertFailureNoCommit'` | Wave 0 |
| STOR-01 | SIGKILL mid-batch + restart: no loss, no visible dupes | integration | `... -run TestWriterCrashRestart` | Wave 0 |
| STOR-02 | partition key, ORDER BY, TTL, bloom index present; TTL ALTER takes effect | integration | `... -run 'TestEventsSchema\|TestEventsTTLConfigurable'`; unit `go test ./migrations/... -run TestMigrations` | Wave 0 |
| STOR-03 | double insert, no OPTIMIZE: raw 2N, `uniqExact` N, lookup one row; same id different `occurred_at` | integration | `... -run 'TestDedupeDoubleInsertNoOptimize\|TestDedupeDifferentOccurredAt'` | Wave 0 (write first) |
| STOR-04 | poison -> DLQ with headers; commit only after DLQ ack; DLQ publish failure -> no commit | integration | `... -run 'TestWriterPoisonToDLQ\|TestWriterDLQFailureNoCommit'` | Wave 0 |
| GEN-01 | seeded reproducibility, doc IP ranges, skew-safe anchoring | unit (pytest) | `cd generator && uv run --locked pytest tests -k 'send or events'` | partial (`test_events.py`) |
| GEN-03 | `make seed`: 1000 events visible, lookup spot-check, non-zero exit on shortfall | e2e (Compose) | `make dev-up && make seed` | Wave 0; manual-in-CI-optional (needs Compose stack; run in `make verify`-style job or by hand) |
| OBS-01 | `/healthz` always 200; `/readyz` 200 -> 503 on `docker pause` of each dependency -> 200 after unpause; draining flip on SIGTERM | integration | `... -run 'TestReadyz\|TestHealthz'` | Wave 0 |
| D-24 | empty CORS = no ACAO header; no query string or key in logs | unit | `go test ./internal/middleware/... -run 'TestCORSEmptyDenies\|TestRequestLoggerNoQuery\|TestNoKeyInLogs'` | Wave 0 |
| D-25 | banned-module check | CI step | `! (cd backend && go list -deps -test ./... \| grep -E 'newrelic\|pkg/errors\|clerk\|asynq\|resend')` | Wave 0 (add to ci.yml go job) |

### Sampling Rate
- **Per task commit:** `cd backend && GOTOOLCHAIN=go1.26.9 go test -race ./internal/<pkg>/...` plus `go vet` for touched packages.
- **Per wave merge:** `make test` and `make lint` (both include the `-tags=integration` vet/lint in CI).
- **Phase gate:** `make test-integ` green, `scripts/go-test-nonempty.sh './test/...' 'TestDedupe|TestWriter|TestIngest|TestRateLimit|TestReadyz'` (extend the CI "must not disappear" filter list), `make dev-up && make seed` exit 0, then `/gsd:verify-work`.

### Wave 0 Gaps
- [ ] `backend/internal/testing`: export `FreePort`; add container-handle helpers (return `GetContainerID()` so tests can `docker pause/unpause`; the shared TestMain containers must NOT be paused by tests, use dedicated ones for outage tests); helper to create unique topics with `kadm`.
- [ ] `backend/test/lifecycle_test.go` `serviceEnv`: add database to ingest, `NATIVE_ADDRESS` to ClickHouse users, unique ops/server ports; fix existing tests accordingly.
- [ ] New unit test files: `internal/apikeys/*_test.go`, `internal/ratelimit/ratelimit_test.go` (Lua semantic cases against a Redis container), `internal/opshttp/*_test.go`, `internal/ingest/*_test.go` (route, shed, step order with fakes only for unit; integration uses real deps), `internal/kafkax/*_test.go`, `internal/storage/clickhouse/*_test.go`.
- [ ] Integration files in `backend/test/` (tag `integration`): `ingest_test.go`, `writer_test.go`, `dedupe_test.go`, `ratelimit_test.go`, `readyz_test.go`.
- [ ] Generator: `tests/test_send.py` using `httpx.MockTransport`; `uv lock` and `requirements.txt` regeneration after adding httpx.
- [ ] CI: banned-module step (D-25) and extended `go-test-nonempty.sh` regex lists; `.gitignore` `/.local/`; `.dockerignore`.
- [ ] Framework install: none (testcontainers, testify already present).

## Security Domain

### Applicable ASVS Categories (L1)

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes (API keys) | 12-char key_id + 32-byte random secret, SHA-256 at rest, `subtle.ConstantTimeCompare`, uniform 401, key shown once, revocation within 30 s cache TTL |
| V3 Session Management | no (stateless keys, no cookies) | — |
| V4 Access Control | yes | Scopes `events:write` / `events:read` enforced server-side; tenant taken from the key, mismatch -> 403 (batch) and tenant-scoped SQL (lookup) |
| V5 Input Validation | yes | 1 MiB `MaxBytesReader`, 500-event cap, strict `DecodeEvent` (unknown/duplicate keys rejected), Go-typed validation (no JSON Schema compilation in production), parameterized SQL (`{p:Type}` / `$n`), UUID check on the lookup path param |
| V6 Cryptography | yes | `crypto/rand`, `crypto/sha256`; no custom primitives |
| V7 Error handling & logging | yes | Envelope only, no internals in responses; request path without query; never log `X-API-Key`, secrets or bodies; DLQ `dlq.reason` is an enum, not payload |
| V8 Data protection | yes | Synthetic data only; DLQ value redaction above the cap; no secrets in Compose (`${VAR:?}`) |
| V9 Communications | partially | Phase 2 binds loopback only and has no TLS terminator (Phase 6). Document that production requires TLS in front; Redis/Postgres TLS rules already enforced by config |
| V11 Business logic / resource limits | yes | Redis token bucket per key_id, in-flight limiter, producer buffer caps, bounded cache and per-key state |
| V13 API | yes | Spec §8 status codes; OpenAPI updated to include `ApiKeyAuth` on `getEvent` |
| V14 Configuration | yes | Distroless nonroot runtime, digest-pinned images, `go.sum`/`uv.lock` committed, banned-module CI, empty CORS = deny |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Key brute force / enumeration (valid vs invalid key_id) | Spoofing / Info disclosure | Identical 401 body and code for malformed/unknown/revoked; always hash+compare (dummy hash when not found); 256-bit secret; cheap format check before DB; in-flight limiter in front of auth |
| Postgres exhaustion by unknown-key floods (negatives uncached by D-04) | DoS | Format check, in-flight cap (64) bounds concurrent lookups, pgxpool limit; consider a tiny IP-level limiter in Phase 5 |
| Cross-tenant write (body `tenant_id` differs from key) | Tampering / Elevation | 403 for whole batch, tenant taken from the key |
| Cross-tenant read via event_id | Info disclosure | SQL always includes `tenant_id = {tenant}` from the key; 404 (not 403) for other tenants' ids |
| Oversized / deeply nested payloads | DoS | `MaxBytesReader` 1 MiB, 500 events, per-event limits (32 attrs / 4 KiB), strict decoder depth guards in `jsonkeys.go` |
| Replay of the same batch | Repudiation / Tampering | By design idempotent: dedupe by `event_id`; documented client contract |
| Rate-limit bypass when Redis is down | Elevation | Fail-closed default (503), fail-open only by explicit config with a warn log |
| Spoofed client IP via `X-Forwarded-For` | Spoofing | `echo.ExtractIPDirect()` already default (WR-02); only trusted proxy config may change it |
| Cross-origin browser abuse of the API | Tampering | Empty CORS list = no CORS headers (Pitfall 5); API key in a header, not cookies, so CSRF does not apply |
| Secret leakage via logs, process list, image layers | Info disclosure | No key logging; `.local/` gitignored and dockerignored; `curl -H @file`; `.dockerignore` excludes `.env`; gitleaks already in CI |
| Poison message blocks the partition forever | DoS | DLQ then commit; infrastructure failures never DLQ (crash loop is visible instead) |
| SQL injection | Tampering | Server-side parameters only; the single constructed DDL (TTL) uses a validated integer |
| Supply chain (new deps) | Tampering | Pinned versions + `go.sum`, `go mod tidy -diff`, banned-module check, digest-pinned images |

## Sources

### Primary (HIGH confidence)
- Ran against pinned images in this session (scratch module and containers, all removed afterwards): Echo v4.16.0 routing; franz-go v1.22.1 produce/consume/commit/Ping/pause behaviour on apache/kafka 4.3.1; clickhouse-go v2.49.0 batch/params/Ping/settings and DDL/SQL/TTL/dedup on clickhouse 26.3.47.6; Lua script and go-redis v9.23.0 `Script.Run` on redis 8.10.2; Docker build of the proposed Dockerfile with repo sources.
- `go doc github.com/twmb/franz-go/pkg/kgo` (v1.22.1): `RequiredAcks`, `AllISRAcks`, `MaxBufferedRecords`, `MaxBufferedBytes`, `RecordRetries`, `RecordDeliveryTimeout`, `AllowIdempotentProduceCancellation`, `ConsumerGroup`, `DisableAutoCommit`, `BlockRebalanceOnPoll`, `AllowRebalance`, `OnPartitionsRevoked/Lost`, `PollRecords`, `CommitRecords`, `CommitUncommittedOffsets`, `CommitOffsetsSync`, `CloseAllowingRebalance`, `Ping`.
- Echo source `middleware/cors.go` (empty `AllowOrigins` -> `*`).
- proxy.golang.org module metadata (franz-go v1.22.1, kadm v1.19.0, clickhouse-go v2.49.0); PyPI JSON API (httpx 0.28.1); Docker Hub API (golang:1.26.9 digest); gcr.io registry API (distroless/static:nonroot digest).
- Repo: `02-CONTEXT.md`, `backend/internal/{app,httpx,middleware,config,errs,redisx,contracts,chmigrate,testing}`, `backend/test/*`, `scripts/check-pins.sh`, `.github/workflows/ci.yml`, `deployments/compose/docker-compose.yml`, ADRs 0002/0003/0005, 01-SECURITY.md (W-03/W-05/W-07).

### Secondary (MEDIUM confidence)
- slopcheck output for httpx (OK) and the Go false positives (explained in the audit table).

### Tertiary (LOW confidence)
- Memory multiplier estimate (A2); go-redis retry/dial bounding (A3).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH. Versions resolved from the proxy/PyPI and exercised.
- Architecture: MEDIUM-HIGH. Library behaviour verified; loop/ops/app-lifecycle shapes are recommended designs that the tests in Validation Architecture must prove.
- Pitfalls: HIGH for Pitfalls 1-5, 8, 11 (reproduced); MEDIUM for 9, 12, 13 (reasoned from code and docs).

**Research date:** 2026-10-12
**Valid until:** about 2026-11-10 (ClickHouse 26.3.x and tag-updated base images move quickly; re-resolve the golang and distroless digests when planning if more than a few weeks pass).
