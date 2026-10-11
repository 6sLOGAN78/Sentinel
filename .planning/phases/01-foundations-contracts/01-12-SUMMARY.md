---
phase: 01-foundations-contracts
plan: 12
subsystem: ci-and-quality-gate
tags: [ci, github-actions, gitleaks, golangci-lint, go-mod-tidy, phase-gate]
requires: ["01-01 through 01-11", "01-13"]
provides:
  - "CI skeleton (go, web, python, schema, hygiene, secrets, compose-smoke) with no masked failures"
  - "golangci-lint v2 configuration that loads, with the whole module (and the integration build tag) lint clean"
  - "Strictly tidy go.mod with no banned modules"
affects: [phase-2]
tech-stack:
  added: ["gitleaks 8.30.1 (CI, checksum-verified)"]
  patterns: ["actions pinned by commit SHA with the tag in a comment", "nolint only with a named linter and a reason"]
key-files:
  created: [".github/workflows/ci.yml", ".gitleaks.toml"]
  modified: ["backend/.golangci.yml", "and 37 Go files across backend/ for lint fixes (see commit e097efb)"]
key-decisions:
  - "gomodguard migrated to gomodguard_v2 (gomodguard is deprecated in golangci-lint v2.12)"
  - "compose-smoke is optional (timeout-minutes 10), documented as deferred-as-required to Phase 6"
requirements-completed: [FND-02, FND-05, FND-06, FND-03]
duration: ~1h
completed: 2026-10-11
---

# Phase 1 Plan 12: CI skeleton, strict tidy, lint-clean module and phase gate Summary

CI workflow with SHA-pinned actions and a checksum-verified gitleaks scan, a golangci-lint v2 config that loads and passes over the whole module, and a full phase gate run against the dev stack.

## Commits

- e097efb `fix(01-12)`: golangci-lint config and module-wide lint fixes
- 0d7fc26 `ci(01-12)`: ci.yml and .gitleaks.toml

(Task 3 produced no file changes: the gate passed without needing fixes. Makefile is unchanged.)

## Task 1: CI

Action versions re-verified at write time with `gh api repos/<owner>/<repo>/releases/latest` (assumption A4 resolved; the research candidates were all correct):

| Item | Version | Commit SHA used |
|------|---------|-----------------|
| actions/checkout | v7.0.1 | 3d3c42e5aac5ba805825da76410c181273ba90b1 |
| actions/setup-go | v7.0.0 | b7ad1dad31e06c5925ef5d2fc7ad053ef454303e |
| actions/setup-node | v7.1.0 | 949feb2413d6458794dcd2491c4babbbce0c15c1 |
| astral-sh/setup-uv | v10.3.0 | 1c37ad07a6a961277cf70c0d37d6f313000f5884 |
| gitleaks | v8.30.1 | asset `gitleaks_8.30.1_linux_x64.tar.gz`, checksums `gitleaks_8.30.1_checksums.txt` (names confirmed on the release; download plus `sha256sum -c` reproduced locally: OK) |

The gitleaks repo is `gitleaks/gitleaks` (the release API also answers under the old `zricethezav` name). Jobs: go, web, python, schema, hygiene, secrets (fetch-depth 0), compose-smoke (timeout-minutes 10). `permissions: contents: read`. `scripts/go-test-nonempty.sh` is invoked with the package list and regex as separate args, as required. `.gitleaks.toml` extends the default ruleset with no allowlist (the `change-me-local` placeholders do not trip any rule).

## Task 2: golangci-lint, tidy, banned modules

- `.golangci.yml` failed to load; `goimports` moved to `formatters:` and `gomodguard` migrated to `gomodguard_v2`.
- First real run reported 114 findings (shadow 29, revive 41 doc comments, lll 8, nolintlint 7, goconst 6, govet, exhaustive 4, dupl 2, others). All fixed in code; none suppressed except:
  - `//nolint:exhaustive` on 4 switches over driver enums where a default path is intended (each with reason)
  - the existing `//nolint:gosec` on int-length conversions in `contracts/dedupe.go` (verified used; one on the int64 to uint64 millisecond conversion was unused and was removed)
  - `//nolint:gochecknoglobals` for `redactedKeys` and the integration test `services` list, `//nolint:unparam` for one test closure matching a callback signature
  - six `//nolint:gochecknoglobals` directives that the linter reported as unused were removed.
- Notable real fixes: shared `strictDecode[T]` replaces duplicated `DecodeEvent`/`DecodeAlert` bodies (same error messages); `extractValidationErrors` split to cut cognitive complexity; typed context key for the request logger (`context.WithValue` no longer uses a plain string key); `net.Listen` replaced with `ListenConfig.Listen`; `Body.Close`/`conn.Close` errors handled explicitly; `fmt.Print*` in `tools/chmigrate` replaced with writes to stdout.
- `go mod tidy` made no change (go.mod/go.sum were already tidy and free of newrelic, pkg/errors, clerk, asynq, resend). `go mod tidy -diff` exit 0; `go list -deps ./...` for those modules prints nothing; `grep` of go.mod prints nothing.
- Lint through `go tool -modfile=tools.mod golangci-lint` works (assumption A5 confirmed), so Makefile and CI keep that form.

## Phase gate evidence (run from this worktree, Go via GOTOOLCHAIN=go1.26.9)

| Step | Command | Result |
|------|---------|--------|
| 0 | `rm -rf web/node_modules web/dist web/src/api/schema.d.ts generator/.venv` (worktree only), then `make test`/`make lint` reinstalled prerequisites | done |
| 1 | `make dev-up` twice, `scripts/check-stack.sh` | both exit 0; check-stack all PASS (3 topics, auto-create off, ports, clickhouse/pg/redis health) |
| 2 | `make migrate` twice | both exit 0; second run "clickhouse schema up to date"; Postgres `schema_version` = 1, ClickHouse `schema_migrations` = 1 row (`001_create_database.sql`) |
| 3 | `make reset` (no CONFIRM), `make seed`, `make bench` | each exits 2 (refuses or not available) as expected |
| 4 | `make test` | exit 0 (Go -race all packages, web vitest 6 passed, pytest 6 passed) |
| 5 | `make lint` | exit 0 (golangci-lint 0 issues, eslint, tsc, ruff, ruff format, mypy) |
| 6 | `make test-integ` | exit 0 (`ok .../test 26.487s`) |
| 7 | `scripts/check-pins.sh` (all sections), `scripts/check-adrs.sh` | exit 0 / exit 0; no `\|\| true` in workflows |
| 7b | `go-test-nonempty.sh` with TestRecover, TestShutdown, TestRedis, TestDSN\|TestConnString, TestDotenv, TestClose (combined run) | exit 0, each family printed PASS lines |
| 7b | contracts filter `TestSchema\|TestFixture\|TestSkew\|TestDedupeKey\|FuzzDedupeKey` | exit 0, 163 PASS lines |
| 7b | CI schema job filter over `./internal/contracts/... ./test/...` | exit 0, 290 PASS lines |
| 7c | `make run SVC=ingest`, SIGINT after about 7 s (includes go compile time) | exit 0; logs "service started", "shutdown signal received", redis and kafka "closed", "shutdown complete" |
| 8 | gitleaks 8.30.1 over history (64 commits) with `.gitleaks.toml` | "no leaks found"; `git ls-files` lists no `.env`; `backend/.env` was never read |
| 9 | `docker volume ls` | `sentinel_clickhouse-data`, `sentinel_kafka-data`, `sentinel_postgres-data` present |
| extra | `go vet` with and without `-tags=integration`, `golangci-lint run --build-tags=integration`, `gofmt -l`, `web format:check`, `web build` | all clean / exit 0 |

ROADMAP Phase 1 criteria evidenced: (1) steps 1, 2, 3, 9; (2) steps 4, 6, 7c and the tidy/banned-module checks; (3) steps 4, 7b contracts; (4) step 7b CI-schema filter, `npm run build`, ci.yml; (5) steps 7, 8.

## Deviations from Plan

**1. [Gate scope] No `make reset CONFIRM=1` and no `make dev-down` against the shared stack.** The orchestrator instruction forbids resetting or stopping the `sentinel` Compose project, whose volumes belong to the main checkout. The stack was in fact stopped (containers existed, not running) when I started, so `make dev-up` started the existing sentinel containers; they were left running. The "fresh volume" and "dev-down leaves volumes" halves of steps 1 and 9 are therefore evidenced only as: idempotent dev-up twice, migrate twice on existing volumes, and volumes present. A destructive reset-from-scratch was not executed here.

**2. [Rule 1/3] Lint fixes touched many earlier-plan files** (listed in commit e097efb); behavior and tests unchanged. Alert and event decoders now share a generic helper.

## Not run here

- The GitHub Actions run itself (no push performed); ci.yml was only validated as YAML, with `check-pins.sh ci`, and each command was run locally.
- The CI `compose-smoke` job and its `make dev-down` teardown.
- Integration tests are not part of the CI skeleton (the plan did not list them); `make test-integ` was run locally.

## Known Stubs

None.

## Self-Check: PASSED

Commits e097efb and 0d7fc26 exist; ci.yml and .gitleaks.toml exist.
