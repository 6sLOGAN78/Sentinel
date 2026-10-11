---
phase: 01-foundations-contracts
plan: 13
subsystem: testing
tags: [integration-test, testcontainers, migrations, redis, postgres, clickhouse]
requires: ["01-07", "01-10"]
provides: [backend/test/migrate_test.go]
key-files:
  created:
    - backend/test/migrate_test.go
requirements-completed: [FND-02, FND-03]
metrics:
  tasks: 1
  completed: 2026-10-11
---

# Phase 1 Plan 13: Migration, DSN and Redis integration tests Summary

Real-container tests prove idempotent Postgres and ClickHouse migrations, end-to-end DSN escaping for the password `p@ss:w/rd?#% +`, and Redis client behavior after Close.

## Commits
- 7f54905 test(01-13): integration tests for migrations, DSN escaping and Redis

## Tests (build tag `integration`)
- TestMigratePostgresIdempotent: fresh Postgres per test; first Migrate logs "migrated ... from version 0", `schema_version` = 1; second Migrate logs "up to date", no migration log, version still 1.
- TestConnectSpecialPassword: role created via `format(... %L)`, connects through database.New, `current_user` asserted; wrong password rejected.
- TestMigrateClickHouseIdempotent: runner given `host:port` with new database `sentinel_migrated`; database exists, `schema_migrations` row count equals applied count; second Run returns empty and count unchanged.
- TestRedisIntegration: redisx.New against the shared Redis container; PING ok, PING fails after Close.

## Verification
- `scripts/go-test-nonempty.sh ./test/... 'TestMigrate|TestConnectSpecialPassword|TestRedisIntegration' -tags=integration -timeout 10m -p 1` passes (~25s).
- gofmt and `go vet -tags=integration` clean; no `time.Sleep` in backend/test.
- golangci-lint not run: config fails to load ("goimports is a formatter"), fixed by plan 01-12.

## Deviations from Plan
- Postgres and ClickHouse tests use per-test containers (Require*) instead of the TestMain ones, so "fresh" is guaranteed; Redis uses the shared TestMain container.
- Migrate returns only an error, so "applies nothing" for Postgres is asserted via captured log output plus unchanged `schema_version`.
- Extra flags passed as separate args to go-test-nonempty.sh (as noted in 01-10).

## Known Stubs
None.

## Self-Check: PASSED
