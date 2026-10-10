# Phase 1: Foundations & Contracts - Context

**Gathered:** 2026-10-10
**Status:** Ready for planning
**Mode:** `--auto`. Every gray area was resolved with the recommended option. See `01-DISCUSSION-LOG.md` for the audit trail.

<domain>
## Phase Boundary

From a clean checkout a developer runs `make dev-up` and gets a healthy Kafka (KRaft), ClickHouse, PostgreSQL and Redis stack with explicitly created topics. The repo is one Go 1.26.x module (`backend/`) that builds five binaries. Non-spec boilerplate is removed and the known defects in retained code are fixed.

The phase also delivers:
- Versioned event and alert contracts (JSON Schema plus Go types), with a single canonical `dedupe_key` → `alert_id` function.
- An OpenAPI 3.1 skeleton.
- Pinned versions and a placeholder-only `.env.example`.
- Make targets and a CI skeleton.
- ADRs that later phases build on.

Requirements: FND-01..FND-07.

**Not in this phase:**
- Ingest endpoint logic, Kafka produce/consume code paths, ClickHouse writes (Phase 2).
- Rules (Phase 3).
- Query API and dashboard pages (Phase 4).
- Metrics, benchmarks (Phase 5).
- K8s, hardened CI (Phase 6).

The binaries exist and start, but carry no business logic yet.
</domain>

<decisions>
## Implementation Decisions

### Repo layout and boilerplate removal (FND-03)
- **D-01:** Keep the single Go module at `backend/` (module `github.com/6sLOGAN78/sentinel`) and bump it to Go 1.26.x. Binaries go in `backend/cmd/{ingest,detector,event-writer,alert-writer,query-api}/main.go`. `backend/cmd/sentinel` is deleted.
- **D-02:** Spec §13 layout adapted to this module. Non-Go assets stay at the repo root:
  - `api/openapi.yaml`
  - `config/rules.yaml` (placeholder schema only; rules come in Phase 3)
  - `web/`
  - `generator/`
  - `deployments/compose/docker-compose.yml`
  - `observability/` (dir only)
  - `docs/{architecture.md,decisions/}`
  - `benchmarks/` (README only)
  - root `Makefile`
  - `.env.example`
  - `.github/workflows/ci.yml`

  Go-embedded assets live inside the module, because `go:embed` cannot leave it:
  - JSON schemas in `backend/internal/contracts/schema/`.
  - SQL migrations in `backend/migrations/{postgres,clickhouse}/`.

  Go integration and contract tests live in `backend/test/` behind build tags. Root `tests/` holds only non-Go assets (fixtures shared with the generator).
- **D-03:** Remove completely:
  - Clerk (`middleware/auth.go`, `service/auth.go`).
  - asynq (`internal/lib/job`).
  - Resend and email (`internal/lib/email`, `backend/templates`, `apps/`, `packages/emails`).
  - New Relic plus all nr* integrations (logger and middleware/tracing rewritten without them).
  - `pkg/errors` (use stdlib).
  - `packages/zod`, `packages/openapi`, `backend/static/openapi.*`.
  - Root `package.json`, `turbo.json`, `bun.lock`, `package-lock.json`, root `node_modules`.
  - `backend/taskfile.yml`.
  - The in-memory hardcoded rate limiter.
  - `internal/lib/utils` (fmt.Println debug helper).
  - The empty `001_setup.sql`.
  - `backend/.env.sample` (replaced by root `.env.example`).
- **D-04:** Keep, then refactor:
  - `internal/config` (koanf, `SENTINEL_` prefix).
  - `internal/logger` (zerolog, New Relic stripped).
  - `internal/errs` (HTTP error envelope matching spec §8 `{error:{code,message,request_id}}`).
  - `internal/validation`.
  - `internal/database` (pgx pool and tern).
  - `internal/middleware/request_id.go`, `context.go`, `global.go` (CORS, secure, error handler).
  - `internal/sqlerr`.
  - `internal/testing` (testcontainers helpers).

  The HTTP-oriented `server.Server` dependency container must not be imported by worker binaries. Split it into small shared constructors (`config.Load`, `logger.New`, `database.New`, `redisx.New`), and keep an HTTP server helper only for ingest and query-api.
- **D-05:** Fix the known defects in retained code, each with a test:
  - Recover middleware registered first.
  - `Shutdown` nil-guards the http server and closes Redis.
  - A Redis ping failure at startup is fatal. The runtime fail-closed policy comes in Phase 2.
  - The Postgres DSN is built with `url.URL`/`pgx.ParseConfig`, so special characters in the password are escaped.
  - `godotenv` loads only when `SENTINEL_PRIMARY.ENV=local`.
  - Error paths close already-opened resources.
- **D-06:** Every Phase 1 binary loads and validates its config, initializes the logger, connects to (or validates the reachability of) its dependencies, and blocks until SIGINT/SIGTERM, then shuts down gracefully. No HTTP routes or business logic yet, and no stub endpoints pretending to be features.

### Local infrastructure (FND-01, FND-02)
- **D-07:** Compose file at `deployments/compose/docker-compose.yml`, with pinned images: `apache/kafka:4.3.1` (single-node KRaft, combined broker+controller), ClickHouse 26.3 LTS (exact patch tag and digest to be resolved in research), `postgres:18.6`, `redis:8.10.2`. Every service has a healthcheck. Data goes in named volumes.
- **D-08:** Kafka configuration:
  - Dual listeners: internal `kafka:9092` for containers, external `localhost:29092` for the host.
  - Fixed `CLUSTER_ID`.
  - `auto.create.topics.enable=false`.
  - RF=1 overrides for the internal topics (offsets, transaction state).
- **D-09:** A one-shot `topic-init` service creates topics idempotently after Kafka is healthy:

  | Topic | Partitions | Retention |
  |---|---|---|
  | `security.events.v1` | 6 | 24h |
  | `security.alerts.v1` | 3 | 7d |
  | `security.events.dlq.v1` | 1 | 7d |

  RF is 1 locally and documented as non-HA. Partition counts and retention are env-configurable. Changing the events partition count later is documented as unsafe while detector state matters.
- **D-10:** Make targets:
  - `dev-up` (compose up --wait plus topic-init).
  - `migrate` (Postgres via tern, ClickHouse via the runner in D-11, both idempotent).
  - `test` (Go unit and contract tests, web tests, Python tests).
  - `test-integ` (Testcontainers/Compose-backed, build tag `integration`).
  - `lint`, `dev-down` (preserves volumes), `reset` (destructive and local-only, confirms by requiring `CONFIRM=1`).
  - `seed` and `bench` exist but print "available from Phase 2 / Phase 5" and exit non-zero. They must not pretend to succeed.
- **D-11:** Migrations:
  - **PostgreSQL:** keep tern, with files in `backend/migrations/postgres/`. Phase 1 creates only what the contracts need now: a `schema_version` bookkeeping table via tern. Alert workflow tables come in Phase 3, auth tables in Phase 2/4.
  - **ClickHouse:** a minimal in-repo Go migration runner (ordered `.sql` files, a `schema_migrations` table, `IF NOT EXISTS` DDL). This avoids the unverified golang-migrate/clickhouse-go v2.49 compatibility. Phase 1 creates the database only. The events table comes in Phase 2.

### Contracts and canonical IDs (FND-04)
- **D-12:** JSON Schema (draft 2020-12) files for event v1 and alert v1 are the canonical contract, embedded via `go:embed` from `backend/internal/contracts/schema/`. Hand-written Go structs mirror them. A contract test asserts the struct and the schema agree on valid and invalid fixtures. jsonschema/v6 runs in tests only. The hot path (Phase 2) uses typed decode with `DisallowUnknownFields` plus explicit checks.
- **D-13:** Event fields per spec §6.1.
  - Required: `schema_version`, `event_id`, `tenant_id`, `occurred_at`, `source`, `event_type`.
  - Optional fields are explicit nullable values.
  - Unknown root-level fields are rejected. `attributes` is a string→scalar map with bounded key count and total size (defaults 32 keys / 4 KiB, configurable).
  - `occurred_at` is RFC 3339 UTC, normalized to millisecond precision.
  - `tenant_id` is constrained to `^[a-z0-9][a-z0-9_-]{0,62}$`, which also keeps `|` out of composite keys.
  - IPs are validated with `net/netip` (IPv4 and IPv6). Ports are 0–65535.
  - Enums cover `protocol`, `action`, `outcome` and `event_type`. Initial `event_type` set: `auth.success`, `auth.failure`, `net.connection`, `net.request`, `policy.decision`. Extending it is a schema version bump.
  - Timestamp skew bounds are config values (default: no more than 5 min in the future, no more than 7 days in the past).
- **D-14:** event_id policy (ADR). `event_id` is **required and client-supplied** (spec §6.1 lists it as required), validated as a UUID (v7 recommended). The server never generates per-attempt random IDs. Retrying the same batch with the same event_ids is the documented idempotency contract. The generator produces deterministic UUIDv7-shaped IDs from its seed.
- **D-15:** A single function, `contracts.DedupeKey(tenant, ruleID, entityKey, windowStart)`, builds the canonical key. It uses length-prefixed fields, UTC, millisecond precision, and returns lowercase SHA-256 hex as `alert_id`.
  - `entity_key` components are length-prefixed too, so a `|` inside a value cannot collide.
  - The human-readable `dedupe_key` string from spec §6.2 is kept as a display field only.
  - The function is fuzz-tested (`go test -fuzz`) for injectivity on `|`-containing inputs.
  - It lives in Phase 1 even though the rules come in Phase 3.
- **D-16:** Alert schema per spec §6.2.
  - `created_at` is defined as event-time-derived (window end or triggering event time).
  - An optional non-key `detected_at` (wall clock) is reserved for the delay metric.
  - `sample_event_ids` holds at most 20 entries.
  - `severity` enum: `low`, `medium`, `high`, `critical`.
  - `status` enum: `open`, `acknowledged`, `resolved`.

### API contract (FND-05)
- **D-17:** `api/openapi.yaml` is a hand-authored OpenAPI 3.1 document covering every spec §8 endpoint.
  - It includes the error envelope, cursor pagination shape (`items`, `next_cursor`, `has_more`), security schemes (API key header for ingest, bearer JWT for dashboard/query), and the 202/400/401/403/413/429/503 responses with `Retry-After`.
  - Event and alert schemas are referenced from (or generated to match) the JSON Schema files, so there is no drift. A contract test checks this.
- **D-18:** CI validates the OpenAPI document with a Go test. Use libopenapi, or kin-openapi if research confirms good 3.1 support. No server code generation in Phase 1. The TypeScript client generation (openapi-typescript) is wired in `web/` but only needs to compile.

### Frontend and generator scaffolds
- **D-19:** `web/`: Vite + React 19 + TypeScript (~6.0), with its own `package-lock.json` (npm, no workspaces, no bun/Turborepo).
  - ESLint, Prettier, Vitest.
  - A single placeholder route that renders the app shell and a "not yet implemented" banner. It is not wired to fake data.
  - `npm run typecheck`, `lint`, `test` and `build` must pass.
  - Exact peer-compatible versions of Vite 8 / Vitest 5 / TypeScript are resolved in research.
- **D-20:** `generator/`: Python 3.13 project managed by uv (`pyproject.toml` plus `uv.lock`; a `requirements.txt` export for spec compatibility).
  - Ruff and mypy.
  - pytest with one seed-reproducibility test of an event-building function, using reserved documentation IP ranges.
  - No HTTP sending yet (that's Phase 2).

### Pinning, secrets and CI skeleton (FND-06, FND-02, FND-05)
- **D-21:** Pinning:
  - Image tags are pinned, plus digests where research resolves them.
  - `go.sum`, `web/package-lock.json` and `generator/uv.lock` are committed.
  - Go tools (golangci-lint v2, tern) are pinned via `go tool` directives or Makefile-pinned versions.
- **D-22:** Root `.env.example` contains placeholders only (e.g. `SENTINEL_DATABASE.PASSWORD=change-me-local`). `.env` is gitignored. CI runs a secret scan (gitleaks) in the skeleton. The existing `backend/.env` is left untouched and never read or committed.
- **D-23:** `.github/workflows/ci.yml` skeleton. Every job fails on a non-zero exit, with no `|| true`.
  - Go: fmt check, vet, golangci-lint, unit and contract tests, race tests.
  - Web: typecheck, lint, test, build.
  - Python: ruff, mypy, pytest.
  - Schema: OpenAPI and JSON Schema validation.
  - Secret scan.
  - Integration and Compose smoke tests are deferred to the hardened CI in Phase 6, but the job exists in the skeleton if it can run within a bounded timeout.

### ADRs (FND-07)
- **D-24:** `docs/decisions/` uses short MADR-style ADRs, numbered `0001-…`. Phase 1 writes:
  1. Repo layout: `backend/` module, binaries and embedded assets (D-01, D-02).
  2. event_id policy (D-14).
  3. Atomic-batch semantics.
  4. Metrics-label policy.
  5. Redis failure policy.
  6. Alert storage split (ClickHouse immutable plus Postgres workflow/list index, per research Decision 2). Recorded now as the intended model, with the details finalized in Phase 3.
- **D-25:** Atomic-batch semantics ADR:
  - Validate the whole batch before publishing anything. Any invalid event → 400 with zero publishes.
  - 202 only after every record is acked.
  - If Kafka fails mid-batch, return 503 and document that 0..N records may have been published. Clients retry the identical batch, and duplicates are absorbed by event_id dedup downstream.
  - "Atomic" refers to response semantics, not a Kafka transaction.
- **D-26:** Metrics-label policy ADR.
  - Allowed label dimensions: `result`, `status`, `rule_id`, `severity`, `reason`, `group`, `topic`, `partition`, `service`.
  - Forbidden: IP, principal, event_id, alert_id, request_id, tenant_id.
  - Consumer lag is position-based (high watermark minus last processed offset), not committed-offset lag.
- **D-27:** Redis failure policy ADR.
  - Default **fail-closed**: ingest returns 503 when the rate limiter can't reach Redis.
  - `SENTINEL_RATELIMIT.FAIL_MODE=open` allows fail-open with a metric and warning log.
  - Startup with Redis unreachable is fatal (D-05).

### Claude's Discretion
- Internal package names beyond those listed (e.g. `redisx`, `kafkax` naming), Makefile internals, and Compose port numbers (avoid host collisions; document them).
- golangci-lint rule selection (start from the existing `backend/.golangci.yml`).
- Exact attribute size limits and skew defaults, within the stated bounds, recorded in config and the ADR.
- Whether the OpenAPI event/alert schemas are `$ref`s to the JSON Schema files or generated copies, as long as a test prevents drift.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Product and engineering spec
- `spec.md` §2: mandated stack and the single-Go-module rule
- `spec.md` §6: event and alert schemas, Kafka topics, keys and retention
- `spec.md` §7.3–§7.4: alert lifecycle storage choice and Redis policy requirement
- `spec.md` §8: HTTP API contract, error envelope, status codes, pagination (drives `api/openapi.yaml`)
- `spec.md` §10: security requirements (secrets, `.env.example`, tenant from identity)
- `spec.md` §13: target project layout (adapted per D-02)
- `spec.md` §14 Phase 1: deliverables and exit checks
- `spec.md` §15–§16: CI required checks and make targets
- `spec.md` §18–§19: Definition of Done and agent instructions (ADRs in `docs/decisions/`)

### Planning artifacts
- `.planning/PROJECT.md`: core value, constraints, key decisions
- `.planning/REQUIREMENTS.md`: FND-01..FND-07
- `.planning/ROADMAP.md`: Phase 1 success criteria and research flags
- `.planning/research/SUMMARY.md`: resolved conflicts, boilerplate removal list, Phase 1 research flags
- `.planning/research/STACK.md`: pinned versions (Go 1.26.x, apache/kafka 4.3.1, ClickHouse 26.3, postgres 18.6, redis 8.10.2, Vite 8, React 19.3, TS ~6.0, Python 3.13)
- `.planning/research/PITFALLS.md`: KRaft Compose gotchas, Testcontainers stability (Phase 1 items)
- `.planning/research/ARCHITECTURE.md`: layout reasoning (`internal/` rule, `go:embed` boundary, workers must not import `server.Server`)

### Existing code map
- `.planning/codebase/CONCERNS.md`: defects to fix (D-05)
- `.planning/codebase/STRUCTURE.md`, `.planning/codebase/CONVENTIONS.md`: retained patterns
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `backend/internal/config/config.go`: koanf env loading with the `SENTINEL_` prefix and validator. Extend with per-binary sections (kafka, clickhouse, redis, ratelimit, contracts limits).
- `backend/internal/logger/logger.go`: zerolog setup. Strip New Relic and add `service` field defaults.
- `backend/internal/database/database.go`, `migrator.go`: pgx pool and tern runner. Fix DSN escaping and move migrations to `backend/migrations/postgres/`.
- `backend/internal/errs/`: typed HTTP errors. Reshape to the spec §8 envelope.
- `backend/internal/middleware/request_id.go`, `global.go`, `context.go`: reusable for ingest and query-api HTTP servers.
- `backend/internal/testing/container.go`: Postgres testcontainer helper. Pattern for adding Kafka, ClickHouse and Redis containers.
- `backend/.golangci.yml`: existing lint config to carry forward.

### Established Patterns
- Constructor-based DI (`NewX(...)`), aggregator structs, generic `handler.Handle*` wrappers (useful for later HTTP phases).
- Env-nested config keys (`SENTINEL_DATABASE.HOST`).
- Lowercase single-word packages and snake_case files.

### Integration Points
- `backend/cmd/sentinel/main.go` is replaced by five `backend/cmd/*/main.go`.
- `backend/internal/router/router.go`: the `/api/v1` group is discarded today. It becomes ingest/query-api specific in later phases. In Phase 1 the router remains only as far as the HTTP helper needs it.
- `backend/internal/server/server.go`: split up (D-04). It currently starts the asynq worker and New Relic.
</code_context>

<specifics>
## Specific Ideas

- Don't fake readiness or features. Placeholder make targets (`seed`, `bench`) must exit non-zero with a clear message (spec §18).
- Use reserved documentation IP ranges in every fixture (spec §10).
- `make reset` must never run without an explicit confirmation flag.
</specifics>

<deferred>
## Deferred Ideas

- Hardened CI (integration smoke with a bounded timeout as a required gate, dependency scanning): Phase 6 (DEP-03).
- `/healthz` and `/readyz` endpoints: Phase 2 (OBS-01).
- Prometheus metrics and OTel wiring: Phase 5 (OBS-02..05). Only the label-policy ADR lands now.
- Auth tables and producer API keys: Phase 2 (ING-01). Dashboard users/JWT: Phase 4 (AUTH-01).
</deferred>

---

*Phase: 01-foundations-contracts*
*Context gathered: 2026-10-10*
