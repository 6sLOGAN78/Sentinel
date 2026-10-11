---
phase: 01-foundations-contracts
fixed_at: 2026-10-11T16:13:00Z
review_path: .planning/phases/01-foundations-contracts/01-REVIEW.md
iteration: 1
findings_in_scope: 16
fixed: 16
skipped: 0
status: all_fixed
---

# Phase 1: Code Review Fix Report

**Fixed at:** 2026-10-11T16:13:00Z
**Source review:** .planning/phases/01-foundations-contracts/01-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 16 (scope critical_warning: 0 critical, 16 warnings; the 14 Info findings are out of scope)
- Fixed: 16
- Skipped: 0

Final gates, run from the repo root with `GOTOOLCHAIN=go1.26.9`, all passed: `make lint`, `make test`, `make test-integ`, `go vet` with and without `-tags=integration`, golangci-lint with `--build-tags=integration`, `go mod tidy -diff`, the CI `go-test-nonempty.sh` guards, `scripts/check-pins.sh`, `scripts/check-adrs.sh` and `scripts/check-makefile-dotenv.sh`. `make migrate` against the running local stack also succeeded and reported "clickhouse schema up to date".

Three fixes change parsing or matching logic and should get a human look before verification: WR-06, WR-08 and WR-11. Each is covered by tests, but tests only check the cases someone thought to write.

## Fixed Issues

### WR-01: Request logger records 200/info for errors that are not HTTPError or echo.HTTPError

**Files modified:** `backend/internal/middleware/global.go`, `backend/internal/middleware/middleware_test.go`
**Commit:** 40f9821
**Applied fix:** `LogValuesFunc` now gets the status from `toHTTPError(v.Error).Status`, the same mapping `GlobalErrorHandler` uses, so the logged status and level always match the response. New tests check that a plain `errors.New` logs as 500/error and a 429 logs as warn.

### WR-02: RealIP trusts client-supplied X-Forwarded-For / X-Real-IP

**Files modified:** `backend/internal/middleware/chain.go`, `backend/internal/middleware/middleware_test.go`
**Commit:** f18d652
**Applied fix:** `Apply` sets `e.IPExtractor = echo.ExtractIPDirect()` unless the caller has already set an extractor. That lets Phase 2 install `echo.ExtractIPFromXFFHeader(echo.TrustIPRange(...))` for configured proxies. Tests check that spoofed XFF and X-Real-IP headers do not reach the logged `ip`, and that a pre-configured trusted-proxy extractor is kept.

### WR-03: CORS allowed origins cannot be configured with more than one value from env

**Files modified:** `backend/internal/config/config.go`, `backend/internal/config/config_test.go`
**Commit:** a88222b
**Applied fix:** The koanf env provider now uses `env.ProviderWithValue`. List keys (currently `server.cors_allowed_origins`) are split on commas, trimmed, and blank entries are dropped. A test covers several origins, blank entries and a single origin. W-07 (an empty list allows all origins) is still the separately tracked Phase 2 hand-off.

### WR-04: HTTPError.Is matches every HTTPError regardless of code/status

**Files modified:** `backend/internal/errs/http.go`, `backend/internal/errs/errs_test.go`
**Commit:** f0b1e2c
**Applied fix:** `Is` now matches only when both `Code` and `Status` are equal, and it is nil-safe. The existing assertion `ErrorIs(cp, &HTTPError{})` depended on the bug, so it was replaced with matching and non-matching cases: 404 vs 401, a zero value, a nil pointer, and the same status with a different code.

### WR-05: Unique-constraint violations map to 400 instead of 409

**Files modified:** `backend/internal/errs/types.go`, `backend/internal/errs/errs_test.go`, `backend/internal/sqlerr/handler.go`, `backend/internal/sqlerr/handler_test.go` (new)
**Commit:** 7114ad1
**Applied fix:** `NewConflictError` takes an optional code pointer, matching `NewNotFoundError`. `UniqueViolation` now returns 409 with the generated `<ENTITY>_ALREADY_EXISTS` code. The new sqlerr tests check 23505 → 409, FK/not-null/check violations → 400, and unknown errors → 500.

### WR-06: Strict decoder silently accepts duplicate JSON keys (last wins)

**Files modified:** `backend/internal/contracts/jsonkeys.go` (new), `backend/internal/contracts/event.go`, `backend/internal/contracts/event_test.go`, `backend/internal/contracts/alert_test.go`
**Commit:** 25f8d75
**Status:** fixed: requires human verification (new token-walking parser)
**Applied fix:** `strictDecode` now gets the root keys from a token-level walk (`rootKeys`) instead of `json.Unmarshal` into a map. The walk rejects:
- a non-object root, so `null` is also rejected (IN-01 for free);
- a repeated key in any object at any depth, so `attributes` is covered too;
- nesting deeper than 32 levels.

The unknown-field allow-list check is unchanged. Tests cover duplicate `tenant_id` and `event_id` at the root, a duplicate attribute key, the same key name in sibling positions (accepted), non-object roots, deep nesting, and a duplicate key in an alert.

### WR-07: Attribute byte limit is computed on HTML-escaped JSON

**Files modified:** `backend/internal/contracts/event.go`, `backend/internal/contracts/event_test.go`, `backend/internal/contracts/schema/event.v1.json`, `api/openapi.yaml`
**Commit:** d4832dc
**Applied fix:** A new `attributesSize` function measures compact UTF-8 JSON with `SetEscapeHTML(false)`. encoding/json still escapes U+2028/U+2029 even with that setting, so the function also subtracts 3 bytes per occurrence. The encoding rule is documented on `attributes` in event.v1.json and the OpenAPI spec (the drift test ignores descriptions). Boundary tests cover 4096 and 4097 bytes of `<>&` characters and of line separators.

### WR-08: JSON Schema / OpenAPI accept IPv4-mapped IPv6 forms that Go rejects

**Files modified:** `backend/internal/contracts/schema/event.v1.json`, `api/openapi.yaml`, `backend/internal/contracts/ip_schema_test.go` (new), 4 new fixtures in `backend/internal/contracts/testdata/event/invalid_schema/` (`source_ip_v4_mapped_{expanded,partial,upper_padded,trailing_zeros}.json`)
**Commit:** 85aa54b
**Status:** fixed: requires human verification (regex logic)
**Applied fix:** The `not` pattern no longer uses the `(?i:...)` inline flag, which is not ECMA-262 compatible. It now matches every hex-only spelling where groups 1-5 are zero and group 6 is `ffff`: any placement of `::` before `ffff`, fully expanded or zero-padded forms, upper case, and `::` in the last two groups. The reviewer's suggested pattern was not used because it is incomplete. The old pattern also wrongly rejected `::ffff:1`, which is not mapped and which Go accepts; the new pattern accepts it. `TestSchemaIPv6AgreesWithGo` enumerates every padded, unpadded, upper-case and `::`-compressed spelling of mapped and near-mapped addresses and requires the schema and Go `optIP` to agree on each. The OpenAPI drift test passes with the new fixtures.

### WR-09: ClickHouse migrations ignore the configured database

**Files modified:** `backend/internal/chmigrate/chmigrate.go`, `backend/internal/chmigrate/chmigrate_test.go`, `backend/migrations/clickhouse/001_create_database.sql`, `backend/migrations/migrations_test.go`, `backend/test/migrate_test.go`
**Commit:** a837ff3
**Applied fix:**
- `exec` takes a session database and sends it as the `?database=` query parameter. Credentials still go only in the Authorization header.
- Bootstrap statements (CREATE DATABASE / schema_migrations) run without a session database. Migration, listing and record statements run with it.
- Migration 001 no longer hardcodes `sentinel`. It is now a comment-only file, kept so the applied-version history stays stable, and the bootstrap creates the configured database.

Tests:
- The fake ClickHouse server now allows only the `database` query parameter and checks the per-statement database.
- A migrations test forbids `CREATE DATABASE` and `sentinel.` qualification in ClickHouse migrations.
- A new integration test (`TestMigrateClickHouseUsesConfiguredDatabase`) proves an unqualified `CREATE TABLE` lands in the runner's database.

### WR-10: ClickHouse migration apply is non-atomic and unlocked

**Files modified:** `backend/internal/chmigrate/chmigrate.go`, `backend/internal/chmigrate/chmigrate_test.go`, `backend/migrations/migrations_test.go`, `docs/decisions/0001-repo-layout-and-binaries.md`
**Commit:** f1f3ae9
**Applied fix:**
- A new `schema_migrations` table is created as `ReplacingMergeTree`. The version listing uses `SELECT DISTINCT`, so duplicate rows are harmless, including on tables already created as MergeTree.
- `TestClickHouseMigrationsAreIdempotent` requires `IF [NOT] EXISTS` on every ClickHouse migration statement.
- `TestChmigrateReplaysInterruptedMigration` shows a run that fails mid-file records no version and replays cleanly.
- The package doc and ADR 0001 document idempotent DDL, unqualified names and single-runner execution (`make migrate` locally, one Job in a cluster). ClickHouse has no locks to enforce single-runner execution.
- `splitStatements` is exported as `SplitStatements` so the migrations test can reuse it.

### WR-11: splitStatements breaks on trailing comments and on semicolons inside comments or strings

**Files modified:** `backend/internal/chmigrate/chmigrate.go`, `backend/internal/chmigrate/chmigrate_test.go`
**Commit:** 9c96c22 (committed before WR-09, because the comment-only migration 001 depends on it)
**Status:** fixed: requires human verification (new lexer)
**Applied fix:** The line-suffix splitter is replaced by a small lexer. It splits on `;` only outside `'...'`, `"..."` and backtick literals (honouring backslash escapes and doubled quotes), `--` line comments and `/* */` block comments, and it drops chunks that contain only comments or whitespace. The test covers semicolons inside comments and strings, escaped and doubled quotes, a quoted identifier containing `;`, a trailing comment, and a comment-only file (which records its version and sends no query).

### WR-12: Generator emits wrong or host-dependent timestamps for non-UTC base_time

**Files modified:** `generator/src/sentinel_generator/events.py`, `generator/tests/test_events.py`
**Commit:** c073628
**Applied fix:** `build_event` raises `ValueError` for a naive `base_time` and converts the occurrence time to UTC before formatting. The event_id timestamp is now computed with exact integer arithmetic (`(occurred - EPOCH) // 1ms`) instead of `float(timestamp())`. New tests cover a naive base (rejected), a `+05:30` base (output byte-identical to the equivalent UTC base), and the event_id timestamp matching `occurred_at` to the millisecond. The tests pass under `TZ=Asia/Kolkata`.

### WR-13: Integration tests never run in CI

**Files modified:** `.github/workflows/ci.yml`
**Commit:** e27e7fc
**Applied fix:** A new `integration` job runs `make test-integ` on `ubuntu-24.04` (Docker available) with pinned action SHAs and `needs: [go]`. It is bounded by `timeout-minutes: 20` and by the Makefile's `go test -timeout 10m -p 1`. `scripts/check-pins.sh ci` passes. Locally, `make test-integ` takes about 24 s.

### WR-14: Makefile `include .env` parses the dotenv file as Makefile syntax

**Files modified:** `Makefile`, `scripts/check-makefile-dotenv.sh` (new), `.github/workflows/ci.yml`
**Commit:** b4569f8
**Applied fix:** The `include .env` and `export` lines are removed. A `dotenv_or_env` macro reads a key from `$(ENV_FILE)` (default `$(CURDIR)/.env`) literally with `grep | tail -n1 | cut -d= -f2-`, falls back to the environment, and is used only inside the `migrate` recipe's env assignments, so passwords never reach argv (T-02-06 still holds). The new `check-makefile-dotenv.sh` (wired into the CI hygiene job) uses a throwaway env file to prove that `pa$$w0rd#1 x=y` survives byte-for-byte, that the environment fallback works, and that the Makefile no longer includes `.env`. The real `.env` is never read by the script.

### WR-15: Kafka brokers list passes validation with no usable address

**Files modified:** `backend/internal/config/config.go`, `backend/internal/config/config_test.go`, `backend/internal/app/app.go`, `backend/internal/app/app_test.go`
**Commit:** 86a8715
**Applied fix:** A new `config.ParseBrokers` splits and trims the list, requires at least one entry, and validates each with `net.SplitHostPort` (non-empty host, port 1-65535). It runs during Kafka section validation through a new `validateSection` helper, which also keeps `validateSSLMode`. `DialBrokers` uses `ParseBrokers`, so a list that yields zero addresses is now an error instead of a silent pass. Tests cover `,`, ` , `, a missing port, a missing host, a non-numeric port, ports 0 and 70000, and valid hostname and IPv6 broker entries.

### WR-16: Redis client cannot be configured with AUTH or TLS

**Files modified:** `backend/internal/config/config.go`, `backend/internal/config/config_test.go`, `backend/internal/redisx/redisx.go`, `backend/internal/redisx/redisx_test.go`, `.env.example`
**Commit:** 2e91dc5
**Applied fix:**
- `RedisConfig` gains `Username`, `Password`, `TLS` and `CACertFile`.
- A new `redisx.Options` function sets AUTH and, when TLS is on, TLS 1.2+ with `ServerName` taken from the address host. It verifies against the system roots or the PEM CA bundle (an empty or missing bundle is an error), and never sets `InsecureSkipVerify`.
- `validateRedisSecurity` mirrors `validateSSLMode`: outside local/test it requires `tls=true` and a password, and in every environment it rejects `ca_cert_file` without TLS.
- `.env.example` documents the new keys as commented placeholders only. No new infrastructure was added: the local Compose Redis stays plaintext under `PRIMARY.ENV=local`.

Tests cover option mapping, the TLS config, CA loading, bad and missing CA files, a TLS client failing against a plaintext server, and config acceptance or rejection per environment. The integration lifecycle test "production ignores the file" still passes, because the error contains "validate".

## Notes for the orchestrator

- Every finding has its own commit, ending with the required Co-Authored-By trailer. REVIEW-FIX.md is not committed.
- A few out-of-scope Info items were partly addressed as a side effect of the in-scope fixes: IN-01 (`null` root now rejected, via WR-06) and part of IN-09's whitespace-splitting concern (the listing still uses `strings.Fields`, but versions now come through `SELECT DISTINCT`). All Info findings remain open for a later `--fix --all` pass.
- Existing state that the fixes interact with:
  - The running local stack's `sentinel.schema_migrations` table is still MergeTree. `CREATE TABLE IF NOT EXISTS` does not alter it, and `SELECT DISTINCT` makes this harmless.
  - The CI `integration` job is new. It has not run on GitHub yet; it was verified locally only.

---

_Fixed: 2026-10-11T16:13:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
