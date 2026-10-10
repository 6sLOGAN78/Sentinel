---
phase: 01-foundations-contracts
plan: 01
subsystem: infra
tags: [go, config, logger, koanf, zerolog, golangci-lint]
requires: []
provides:
  - "Single Go 1.26 module with boilerplate removed"
  - "config.Load(service) with per-binary validation and gated godotenv"
  - "logger.New / NewWithWriter without New Relic"
  - "tools.mod pinning golangci-lint v2.14.0 and tern v2.4.3"
affects: [01-02, 01-05, 01-06, 01-07, 01-10, 01-12]
tech-stack:
  added: [go-1.26, golangci-lint-v2.14.0, tern-v2.4.3]
  patterns: ["Load(service) returns errors, never exits", "dotenv only when PRIMARY.ENV=local from SENTINEL_ENV_FILE (default ../.env)"]
key-files:
  created: [backend/tools.mod, backend/tools.sum, backend/internal/config/config_test.go, backend/internal/logger/logger_test.go]
  modified: [backend/go.mod, backend/go.sum, backend/.golangci.yml, .gitignore, backend/internal/config/config.go, backend/internal/config/observability.go, backend/internal/logger/logger.go]
key-decisions:
  - "Defaults applied via koanf k.Set before env load (no confmap dependency)"
  - "Database ssl_mode defaults to prefer"
  - "ClickHouse user/database default to default/sentinel"
requirements-completed: [FND-03, FND-06]
duration: 25min
completed: 2026-10-10
---

# Phase 1 Plan 01: Boilerplate removal, Go 1.26, config and logger Summary

Boilerplate (Clerk, asynq, Resend, New Relic, Turborepo, Taskfile) deleted; module bumped to Go 1.26.0 / toolchain go1.26.9 with pinned tools.mod; config.Load(service) and logger.New implemented and tested.

## Tasks

1. Delete boilerplate - commit (see git log: "remove non-spec boilerplate")
2. Go 1.26 bump, tools.mod, lint config, gitignore - 7eef4f4
3. Config and logger refactor with tests - latest feat(01-01) commit

## Deviations from Plan

None in behavior. Notes:

- `git rm` aborted on the untracked `apps/` path, so deletions were done with `rm -rf` and then staged with `git rm --cached` on deleted files. Result is identical.
- `go mod tidy -e` still prints an error about `internal/server` imported by middleware (removed per plan; resolved in plans 01-06/01-07). go-redis and testcontainers dropped from go.mod because nothing imports them yet; later plans re-add them. newrelic and pkg/errors remain in go.mod as expected.
- Pre-existing lint config issue: `golangci-lint run` through tools.mod fails with "goimports is a formatter" (v2 requires moving goimports to the `formatters` section). `golangci-lint config verify` passes as the plan requires. Left for plan 01-12 (strict lint gate). The modfile approach itself works (no fallback needed).

## Verification

- `go test -race ./internal/config/... ./internal/logger/...` passes; TestDotenv* all pass; gofmt and go vet clean on both packages.
- Module as a whole does not build until wave 2 (expected).

## Known Stubs

None.

## Self-Check: PASSED
