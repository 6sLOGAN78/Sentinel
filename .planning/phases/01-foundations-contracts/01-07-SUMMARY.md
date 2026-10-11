---
phase: 01-foundations-contracts
plan: 07
subsystem: database
tags: [pgx, tern, clickhouse, redis, migrations]
requires: ["01-01", "01-02"]
provides: [database.ConnString/PoolConfig/New/Migrate, migrations embed package, redisx.New, chmigrate runner and tool]
key-files:
  created:
    - backend/migrations/embed.go
    - backend/migrations/migrations_test.go
    - backend/migrations/postgres/001_baseline.sql
    - backend/migrations/clickhouse/001_create_database.sql
    - backend/internal/database/database_test.go
    - backend/internal/redisx/redisx.go
    - backend/internal/redisx/redisx_test.go
    - backend/internal/chmigrate/chmigrate.go
    - backend/internal/chmigrate/chmigrate_test.go
    - backend/tools/chmigrate/main.go
  modified:
    - backend/internal/database/database.go
    - backend/internal/database/migrator.go
decisions:
  - Postgres DSN built with url.URL/UserPassword; ParseConfig errors are replaced by a static error so the DSN never leaks
  - pgx trace logger wrapped to strip args/password/connString keys
  - chmigrate uses stdlib net/http only (no clickhouse-go in Phase 1)
metrics:
  tasks: 3
  completed: 2026-10-11
---

# Phase 1 Plan 07: Database, redisx and ClickHouse migration runner Summary

Escaped, leak-free pgx pool and tern migrator reading a shared embed package, a Redis client with fatal startup ping, and an HTTP ClickHouse migration runner; `make migrate` is idempotent against the dev stack.

## Commits
- 33cb39d feat: database ConnString/PoolConfig, tern migrator, embedded migrations
- 8100aeb fix: redact query args and DSN from database logs
- ef90fd4 feat: redisx with fatal startup ping
- chmigrate commit: feat(01-07) ClickHouse HTTP migration runner and chmigrate tool

## Verification
- `go test -race` passes for internal/database, internal/redisx, internal/chmigrate, migrations; go vet and gofmt clean; `go build ./tools/...` ok.
- `make migrate` run twice: Postgres `schema_version` has 1 row; ClickHouse first run printed "applied: 1", second "schema up to date".
- golangci-lint not run: config fails to load until plan 01-12 (known).
- `grep clickhouse-go` finds nothing; no backend/cmd/chmigrate.

## Deviations from Plan

**1. [Rule 2 - Security] Redact query args and DSN from database logs** (requested by security review; commit 8100aeb). PoolConfig's debug tracer is wrapped in `RedactingLogger`, which drops `args`, `password`, `connString`-like keys; ParseConfig failures return a static `invalid database configuration` error without the cause. Tests: TestRedactPgxTraceArgs, TestConnStringParseFailureHidesPassword.

**2. Orchestrator commit ca4fa4e** added go-redis v9.23.0 to go.mod; go.mod/go.sum were not edited by this plan.

**3. Env note:** the sentinel compose containers were stopped (exited), so they were started with `docker start` (postgres, clickhouse, redis); root `.env` copied from `.env.example` (gitignored).

**4. Minor:** the ClickHouse HTTP error body is read (bounded) but not echoed into errors, only status code and file name. `Close()` on Database no longer returns an error. ClickHouse row count was confirmed via the runner's applied/up-to-date output rather than a direct query (tooling restriction).

## Known Stubs
None. Baseline migrations are intentionally minimal (D-11).

## Self-Check: PASSED
