# Walking Skeleton — Sentinel

**Phase:** 1
**Generated:** 2026-10-10

## Capability Proven End-to-End

From a clean checkout, `make dev-up && make migrate` brings up Kafka, ClickHouse, PostgreSQL and Redis; all five Go binaries start against them and shut down cleanly; migrations perform a real write and read (Postgres `schema_version` via tern, ClickHouse `schema_migrations`); contracts validate fixtures; and the web shell and the generator build and test.

Phase 1 intentionally exposes no HTTP route and no fake UI data (CONTEXT D-06, spec §18 no-stub rule). The user-visible "slice" is the developer experience of the whole stack, not an analyst feature.

## Architectural Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Framework / language | One Go 1.26.x module at `backend/` (module `github.com/6sLOGAN78/sentinel`) with five binaries under `backend/cmd/{ingest,detector,event-writer,alert-writer,query-api}`; Echo v4 only for the HTTP helper used by ingest and query-api | Spec §2 mandates a single module; workers must not import an HTTP-oriented dependency container (D-01, D-04) |
| Data layer | Kafka (KRaft) as the log, ClickHouse for immutable event/alert history, PostgreSQL for mutable alert workflow and audit, Redis for rate limiting; Postgres migrations via tern, ClickHouse via an in-repo runner at `backend/tools/chmigrate` | Spec §2/§7; avoids unverified golang-migrate/clickhouse-go compatibility (D-11, D-29) |
| Contracts | JSON Schema 2020-12 as canonical, embedded with `go:embed`; hand-written Go structs; one `DedupeKey` function producing `alert_id`; hand-authored OpenAPI 3.1 tied to the schemas by drift tests | Prevents contract drift and key collisions before any producer exists (D-12, D-15, D-17) |
| Auth | Deferred. Producer API keys in Phase 2, dashboard JWT/RBAC in Phase 4. Clerk removed | Boundary of Phase 1 (CONTEXT deferred list) |
| Deployment target | Docker Compose for local dev (`deployments/compose/docker-compose.yml`, images pinned by digest, ports bound to 127.0.0.1); Kubernetes in Phase 6 | Spec §16; documented local full-stack run command satisfies the deployment item |
| Directory layout | Go module and embedded assets inside `backend/`; non-Go assets at repo root (`api/`, `config/`, `web/`, `generator/`, `deployments/`, `docs/decisions/`, `observability/`, `benchmarks/`, `scripts/`, `Makefile`) | Spec §13 adapted to the existing module; `go:embed` cannot leave the module (D-02) |
| Frontend | Vite 8 + React 19.3 + TypeScript ~6.0 in `web/`, own npm lockfile, no workspaces or Turborepo | Removes inherited monorepo tooling (D-19) |
| Generator | Python 3.13 + uv, ruff, mypy, pytest, deterministic seeded event builder | Reproducible synthetic data (D-20) |
| Delivery semantics | At-least-once, offsets committed after effects, idempotent consumers (recorded in ADRs, implemented from Phase 2) | Spec §3 key decision |

## Stack Touched in Phase 1

- [x] Project scaffold (Go module, five binaries, web, generator, Makefile, CI skeleton, lint and test runners)
- [ ] Routing — **deferred to Phase 2** (the ingest route `POST /api/v1/events:batch` and the minimal events lookup). Reason: D-06 and spec §18 forbid stub endpoints pretending to be features, and the route needs auth, Kafka producer and rate limiting that arrive in Phase 2. The contract for every route already exists in `api/openapi.yaml`.
- [x] Database — real write and real read: tern creates and reads `schema_version` in PostgreSQL; the ClickHouse runner creates the database, writes and reads `schema_migrations`; both verified idempotent by running `make migrate` twice
- [ ] UI wired to the API — **deferred to Phase 4** (dashboard pages). Reason: no endpoint exists to call until Phase 2/4, and fake data is forbidden. The web shell builds, type-checks, lints and tests, and TypeScript types are generated from the real OpenAPI document.
- [x] Deployment — documented local full-stack run command: `make dev-up && make migrate`, then `make run SVC=<ingest|detector|event-writer|alert-writer|query-api>` (runs from `backend/` with `SENTINEL_PRIMARY.ENV=local` and `SENTINEL_ENV_FILE=../.env`, i.e. the root `.env`, never `backend/.env`); each binary starts against the stack and shuts down on SIGINT/SIGTERM (integration tests `TestBinaryLifecycle`, `TestBinaryLifecycleEnvFile`)

## Out of Scope (Deferred to Later Slices)

- Ingest endpoint logic, Kafka produce and consume code paths, ClickHouse writes and the events table (Phase 2)
- `/healthz` and `/readyz` endpoints, producer API keys, Redis rate-limit runtime behavior (Phase 2)
- Detection rules, `config/rules.yaml` contents, alert pipeline and PostgreSQL alert tables (Phase 3)
- Query API, dashboard pages, JWT/RBAC, SSE (Phase 4)
- Prometheus metrics, OpenTelemetry, Grafana, failure suites, benchmarks (Phase 5); `make seed` and `make bench` exit non-zero until then
- Kubernetes manifests, hardened CI with required integration gate, threat model and full docs (Phase 6)
- Aegis adapter and ML scorer (v2)

## Subsequent Slice Plan

Each later phase adds one vertical slice on top of this skeleton without altering its architectural decisions:

- Phase 2: A producer with a scoped API key submits an event batch that Kafka acknowledges and that becomes queryable and duplicate-safe in ClickHouse (adds the first real HTTP route and the first data table).
- Phase 3: Malicious traffic produces exactly one deterministic, evidence-backed alert per dedupe key, surviving detector crashes.
- Phase 4: An analyst signs in, investigates and triages alerts within their tenant, and sees new alerts live in the browser.
- Phase 5: An operator sees pipeline health, failure behavior is proven, and a reproducible 1M-event benchmark backs the performance claims.
- Phase 6: The system deploys on Kubernetes, CI is hardened and the end-to-end demo runs from a clean checkout.
