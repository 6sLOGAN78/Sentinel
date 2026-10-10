# 0001. Repo layout and binaries

## Status

Accepted, 2026-10-10. Decisions: D-01, D-02, D-04, D-28, D-29.

## Context

Sentinel is a multi-service platform (ingest, detector, two writers, query API) written in Go, plus a Python generator and a React dashboard. The repository started from a single-binary boilerplate (Echo handlers, services, repositories, New Relic, Clerk) that carries no domain logic. Phases 2 to 5 need a stable layout to build on.

## Decision

- One Go module in `backend/` (`github.com/6sLOGAN78/sentinel`, Go 1.26.x), with five binaries under `backend/cmd/`: `ingest`, `detector`, `event-writer`, `alert-writer` and `query-api`.
- Non-Go assets live at the repository root: `web/`, `generator/`, `deploy/`, `observability/`, `config/`, `benchmarks/`, `docs/`, `scripts/`.
- Assets that Go code needs at runtime are embedded with `go:embed` inside the module: JSON Schemas in `backend/internal/contracts/schema/`, and migrations in `backend/migrations/postgres/` and `backend/migrations/clickhouse/`.
- Go integration tests live in `backend/test/` behind the build tag `integration`.
- PostgreSQL migrations use tern, invoked as a Go tool via `go tool -modfile=tools.mod` so the version is pinned without polluting the main module.
- The ClickHouse migration runner is a tooling `main` at `backend/tools/chmigrate`. It is deliberately not under `cmd/`, so it is not a sixth service binary.
- The boilerplate `handler/`, `router/`, `model/`, `service/` and `repository/` packages are deleted. The shared HTTP helper (`httpx`) is used only by `ingest` and `query-api`; the other three binaries expose only metrics and health.

## Consequences

- Each service deploys and scales independently while sharing contracts and libraries.
- Contracts cannot drift from the binaries because schemas and migrations are embedded at build time.
- Phase 2 builds the ingest HTTP layer fresh instead of adapting boilerplate tied to New Relic and Clerk.
- The tools modfile adds a second dependency manifest to maintain.

## Alternatives

- One module per service: rejected, it duplicates contracts and complicates version pinning.
- ClickHouse migrations via golang-migrate: rejected, compatibility with the pinned clickhouse-go version was unverified.
