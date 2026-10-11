---
phase: 01-foundations-contracts
reviewed: 2026-10-11T13:57:12Z
depth: standard
files_reviewed: 90
files_reviewed_list:
  - .env.example
  - .github/workflows/ci.yml
  - .gitignore
  - .gitleaks.toml
  - Makefile
  - api/openapi.yaml
  - backend/.golangci.yml
  - backend/cmd/alert-writer/main.go
  - backend/cmd/detector/main.go
  - backend/cmd/event-writer/main.go
  - backend/cmd/ingest/main.go
  - backend/cmd/query-api/main.go
  - backend/go.mod
  - backend/internal/app/app.go
  - backend/internal/app/app_test.go
  - backend/internal/chmigrate/chmigrate.go
  - backend/internal/chmigrate/chmigrate_test.go
  - backend/internal/config/config.go
  - backend/internal/config/config_test.go
  - backend/internal/config/observability.go
  - backend/internal/contracts/alert.go
  - backend/internal/contracts/alert_test.go
  - backend/internal/contracts/dedupe.go
  - backend/internal/contracts/dedupe_test.go
  - backend/internal/contracts/event.go
  - backend/internal/contracts/event_test.go
  - backend/internal/contracts/helpers_test.go
  - backend/internal/contracts/schema.go
  - backend/internal/contracts/schema/alert.v1.json
  - backend/internal/contracts/schema/event.v1.json
  - backend/internal/database/database.go
  - backend/internal/database/database_test.go
  - backend/internal/database/migrator.go
  - backend/internal/errs/errs_test.go
  - backend/internal/errs/http.go
  - backend/internal/errs/types.go
  - backend/internal/httpx/httpx.go
  - backend/internal/httpx/httpx_test.go
  - backend/internal/logger/logger.go
  - backend/internal/logger/logger_test.go
  - backend/internal/middleware/chain.go
  - backend/internal/middleware/context.go
  - backend/internal/middleware/global.go
  - backend/internal/middleware/middleware_test.go
  - backend/internal/middleware/request_id.go
  - backend/internal/redisx/redisx.go
  - backend/internal/redisx/redisx_test.go
  - backend/internal/sqlerr/err.go
  - backend/internal/sqlerr/handler.go
  - backend/internal/testing/assertions.go
  - backend/internal/testing/containers.go
  - backend/internal/validation/utils.go
  - backend/internal/validation/utils_test.go
  - backend/migrations/clickhouse/001_create_database.sql
  - backend/migrations/embed.go
  - backend/migrations/migrations_test.go
  - backend/migrations/postgres/001_baseline.sql
  - backend/test/lifecycle_test.go
  - backend/test/main_test.go
  - backend/test/migrate_test.go
  - backend/test/openapi_drift_test.go
  - backend/test/openapi_test.go
  - backend/tools.mod
  - backend/tools/chmigrate/main.go
  - config/rules.yaml
  - deployments/compose/docker-compose.yml
  - generator/.python-version
  - generator/pyproject.toml
  - generator/requirements.txt
  - generator/src/sentinel_generator/__init__.py
  - generator/src/sentinel_generator/events.py
  - generator/tests/test_events.py
  - scripts/check-adrs.sh
  - scripts/check-pins.sh
  - scripts/check-stack.sh
  - scripts/go-test-nonempty.sh
  - web/.prettierignore
  - web/.prettierrc.json
  - web/eslint.config.js
  - web/index.html
  - web/package.json
  - web/src/App.test.tsx
  - web/src/App.tsx
  - web/src/api/types.test.ts
  - web/src/api/types.ts
  - web/src/main.tsx
  - web/src/test/setup.ts
  - web/src/vite-env.d.ts
  - web/tsconfig.json
  - web/vite.config.ts
findings:
  critical: 0
  warning: 16
  info: 14
  total: 30
status: issues_found
---

# Phase 1: Code Review Report

**Reviewed:** 2026-10-11T13:57:12Z
**Depth:** standard
**Files Reviewed:** 90
**Status:** issues_found

## Summary

I reviewed the Phase 1 foundations: the five Go service skeletons, the shared lifecycle/config/contracts/middleware/errs/sqlerr/chmigrate packages, migrations, Makefile, Compose, CI, the JSON Schemas and OpenAPI spec, scripts, the generator and the web shell. Baseline checks were clean: `go vet`, `golangci-lint` (0 issues), `gofmt -l` and `go test -short ./internal/... ./migrations/...` all pass.

I found no BLOCKER defects. Phase 1 exposes no HTTP listener and no ingest path, so none of the defects below can be exploited yet. Several will become real bugs or security gaps as soon as Phase 2 wires in routes, ingest and ClickHouse tables. I confirmed the most important findings by running probe tests through `go test -overlay` and a Python one-liner. No source files were modified. The confirmed findings are:

- With `middleware.Apply`, a plain `errors.New` returned from a handler is logged as `status:200` at info level, while the client receives a 500 (WR-01).
- `X-Forwarded-For` is trusted for `ip` without restriction (WR-02).
- `SENTINEL_SERVER.CORS_ALLOWED_ORIGINS=http://a,http://b` becomes the single entry `["http://a,http://b"]` (WR-03).
- `errors.Is(NotFound, Unauthorized)` returns true (WR-04).
- The strict decoder accepts duplicate `tenant_id` keys, and the last value wins (WR-06).
- 700 `<` characters in an attribute exceed the 4096-byte attribute limit (WR-07).
- The JSON Schema accepts `0:0:0:0:0:ffff:c000:201` but Go rejects it (WR-08).
- A `+05:30` `base_time` in the generator produces an `occurred_at` that is wrong by 5.5 hours (WR-12).

Items already tracked in 01-SECURITY.md are not re-reported: W-02 to W-07, including the CORS empty-list allow-all fallback and full-URI logging.

## Warnings

### WR-01: Request logger records 200/info for errors that are not HTTPError or echo.HTTPError

**File:** `backend/internal/middleware/global.go:43-56`
**Issue:** `LogValuesFunc` runs before the central error handler (`HandleError` is false), so `v.Status` is still 200. The code corrects the status only when the error is `*errs.HTTPError` or `*echo.HTTPError`. Any other error stays at status 200 and is logged at info level, even though the client gets a 500 (or a 4xx after `sqlerr.HandleError` maps it). Examples are a plain `fmt.Errorf`, a pgx error, or `pgx.ErrNoRows`. A probe confirmed this: a handler returning `errors.New("boom")` logged `"level":"info","status":200`. This corrupts access logs and any error-rate derived from them.
**Fix:** Derive the status from the same mapping the error handler uses:
```go
if v.Error != nil {
    statusCode = toHTTPError(v.Error).Status
}
```

### WR-02: RealIP trusts client-supplied X-Forwarded-For / X-Real-IP

**File:** `backend/internal/middleware/context.go:49`, `backend/internal/middleware/global.go:86`
**Issue:** `e.IPExtractor` is never set, so `c.RealIP()` returns the first `X-Forwarded-For` or `X-Real-IP` value sent by any client. A probe confirmed this: `X-Forwarded-For: 1.2.3.4` was logged as `"ip":"1.2.3.4"`. Request logs can therefore be forged today. Once Phase 2 adds Redis rate limiting keyed by client IP, the same helper becomes a rate-limit bypass. This is not covered by W-07.
**Fix:** In `Apply`, set `e.IPExtractor = echo.ExtractIPDirect()` by default. Use `echo.ExtractIPFromXFFHeader(echo.TrustIPRange(...))` only for configured, trusted proxy CIDRs.

### WR-03: CORS allowed origins cannot be configured with more than one value from env

**File:** `backend/internal/config/config.go:77`
**Issue:** koanf's env provider produces a string, and the weakly typed decode wraps it as a one-element slice. A probe confirmed this: `SENTINEL_SERVER.CORS_ALLOWED_ORIGINS=http://a,http://b` produced `["http://a,http://b"]` with length 1. Echo then compares the whole comma string against `Origin`, so no origin matches. Combined with W-07 (empty list means `*`), an operator has two options: a single origin, or allow-all.
**Fix:** Split comma-separated values in the env provider, for example with `env.ProviderWithValue` returning `strings.Split` for keys ending in `cors_allowed_origins`. Alternatively, add a `mapstructure.StringToSliceHookFunc(",")` decode hook via `k.UnmarshalWithConf`. Add a config test for multiple origins.

### WR-04: HTTPError.Is matches every HTTPError regardless of code/status

**File:** `backend/internal/errs/http.go:69-75`
**Issue:** The doc comment says "same code and status", but the implementation returns true for any `*HTTPError` target. A probe confirmed `errors.Is(NewNotFoundError(...), NewUnauthorizedError(...)) == true`. Any future `errors.Is(err, someSentinel)` branch, for example "is this a 404?", will take the wrong path.
**Fix:**
```go
func (e *HTTPError) Is(target error) bool {
    t, ok := target.(*HTTPError)
    return ok && t.Code == e.Code && t.Status == e.Status
}
```

### WR-05: Unique-constraint violations map to 400 instead of 409

**File:** `backend/internal/sqlerr/handler.go:175-180`
**Issue:** `UniqueViolation` returns `NewBadRequestError`, even though `errs.NewConflictError` exists and the OpenAPI spec defines `409 Conflict`. Clients that retry idempotently cannot tell "duplicate" apart from "malformed".
**Fix:** `return errs.NewConflictError(userMessage, true)`. Carry the generated code if needed by adding a code parameter to `NewConflictError`.

### WR-06: Strict decoder silently accepts duplicate JSON keys (last wins)

**File:** `backend/internal/contracts/event.go:185-203`
**Issue:** Both `json.Unmarshal(raw, &keys)` and `dec.Decode` accept duplicate object keys, and the last value wins. A probe confirmed that `{"tenant_id":"a","tenant_id":"b"}` decodes with `TenantID == "b"` and no error. This creates a parser differential on security-relevant fields (`tenant_id`, `event_id`): other consumers such as the Python tooling, ClickHouse JSON functions or a gateway may pick the first value. The decoder is documented as "strict", and spec §10 requires tenant isolation.
**Fix:** Walk the root object with `json.Decoder.Token()` and reject a repeated key. The `allowed` check can be done in the same pass:
```go
seen := map[string]struct{}{}
// for each key token at depth 1:
if _, dup := seen[k]; dup { return nil, fieldErr(k, "duplicate field") }
seen[k] = struct{}{}
```

### WR-07: Attribute byte limit is computed on HTML-escaped JSON

**File:** `backend/internal/contracts/event.go:385`
**Issue:** `json.Marshal` escapes `<`, `>`, `&`, U+2028 and U+2029 as 6-byte `\uXXXX` sequences. The 4096-byte limit is therefore enforced on an inflated size that no other producer computes. Python `json.dumps` and the JSON Schema description ("encoded size") do not escape these characters. A probe confirmed that an attribute of 700 `<` characters (about 710 raw bytes) is rejected as "exceeds 4096 bytes". The generator or a client could pre-check sizes and still be rejected.
**Fix:**
```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false)
if err := enc.Encode(e.Attributes); err != nil { ... }
size := buf.Len() - 1 // trailing newline
```
Document the exact encoding rule in event.v1.json.

### WR-08: JSON Schema / OpenAPI accept IPv4-mapped IPv6 forms that Go rejects

**File:** `backend/internal/contracts/schema/event.v1.json:16-20`, `api/openapi.yaml:549-554`
**Issue:** The `not` pattern `^(?i:::ffff:)` only catches the compressed `::ffff:` prefix. A probe confirmed that `0:0:0:0:0:ffff:c000:201` and `0::ffff:c000:201` pass the canonical schema but are rejected by Go `optIP` (`Is4In6`). The canonical schema and OpenAPI therefore advertise a contract that ingest will reject. The drift tests do not catch this because both documents share the same hole. Note also that `(?i:...)` inline modifiers are not supported by every ECMA-262 regex engine, so some JS validators (such as ajv on older Node) fail to compile the schema.
**Fix:** Express the rule without inline flags and cover the expanded forms, for example a `not` pattern of `^(0{0,4}:){0,5}(0{0,4}:)?:?[fF]{4}:` (or the equivalent). Add `0:0:0:0:0:ffff:c000:201` to `testdata/event/invalid_schema` so the schema test, the OpenAPI drift test and Go all agree.

### WR-09: ClickHouse migrations ignore the configured database

**File:** `backend/internal/chmigrate/chmigrate.go:162-176`, `backend/migrations/clickhouse/001_create_database.sql:1`
**Issue:** `Runner` is parameterised by `database` (the `-database` flag), but `exec` never sends `?database=` or the `X-ClickHouse-Database` header. Migration 001 also hardcodes `sentinel`. With `-database other`, the runner bootstraps `other.schema_migrations` while migrations create objects in `sentinel`. Unqualified `CREATE TABLE` statements in future migrations will land in ClickHouse's `default` database.
**Fix:** In `exec`, set `q := url.Values{"database": {r.database}}` on the request URL, after bootstrap creates the DB. Write migrations with unqualified names so they always target the configured database.

### WR-10: ClickHouse migration apply is non-atomic and unlocked

**File:** `backend/internal/chmigrate/chmigrate.go:112-131`
**Issue:** A multi-statement migration is applied statement by statement, and the version row is inserted last. If statement N or the version INSERT fails, the earlier statements stay applied but unrecorded. The next run replays them, and any DDL without `IF NOT EXISTS` fails permanently. Concurrent `make migrate` runs (CI plus a developer, or two pods) have no lock and can double-apply. `schema_migrations` is a plain MergeTree, so duplicate version rows are also possible.
**Fix:** Require idempotent DDL (`IF NOT EXISTS`/`IF EXISTS`) and enforce it with a test that greps migrations. Alternatively, keep one statement per migration file. Use `ReplacingMergeTree` for `schema_migrations`, and document or enforce single-runner execution (for example a Kubernetes Job only).

### WR-11: splitStatements breaks on trailing comments and on semicolons inside comments or strings

**File:** `backend/internal/chmigrate/chmigrate.go:133-160`
**Issue:** Any line ending in `;` ends a statement, even inside a `--` comment or a multi-line string literal. A comment after the last statement, or a comment-only file section, is flushed as its own "statement". ClickHouse rejects that as an empty query, so the migration fails. For example, `CREATE ...;\n-- trailing note` sends `-- trailing note` as a query.
**Fix:** Drop chunks that contain only comments or whitespace before sending them. Document or enforce "no `;` at end of line inside comments or strings". The more robust option is one statement per file, or ClickHouse's `multiquery` via `clickhouse-client`.

### WR-12: Generator emits wrong or host-dependent timestamps for non-UTC base_time

**File:** `generator/src/sentinel_generator/events.py:52-68`
**Issue:** `_format_time` formats wall-clock fields and appends `Z` without converting to UTC. `occurred.timestamp()` interprets a naive datetime in the host's local zone. A probe with `TZ=Asia/Kolkata` confirmed both problems:
- An aware `+05:30` base of `2026-01-01T00:00` yields `occurred_at = 2026-01-01T00:00:00.000Z`, 5.5 h off the real instant.
- A naive base yields a different `event_id` timestamp than a UTC base, which breaks the "same seed gives byte-identical output" guarantee across machines.
Tests only use a UTC base.
**Fix:**
```python
if base_time.tzinfo is None:
    raise ValueError("base_time must be timezone-aware")
occurred = (base_time + timedelta(seconds=index)).astimezone(UTC)
timestamp_ms = (occurred - EPOCH).days * 86_400_000 + ...  # or int(occurred.timestamp() * 1000) after astimezone
```
Add tests for a naive base (rejected) and a non-UTC aware base (normalised).

### WR-13: Integration tests never run in CI

**File:** `.github/workflows/ci.yml:36-45, 145-160`
**Issue:** `-tags=integration` is used only for `go vet` and `golangci-lint`. No job runs `go test -tags=integration ./test/...` (`make test-integ`). The binary lifecycle tests, the ClickHouse/Postgres migration tests, and the evidence cited by 01-SECURITY.md (T-07-04, T-07-05, T-10-01) never execute in CI. This conflicts with the Definition of Done ("tests pass ... CI is green").
**Fix:** Add a job, or extend `compose-smoke`, on `ubuntu-24.04`, which has Docker available:
```yaml
- name: integration tests
  working-directory: backend
  run: go test -tags=integration -timeout 10m -p 1 ./test/...
```

### WR-14: Makefile `include .env` parses the dotenv file as Makefile syntax

**File:** `Makefile:8-12`
**Issue:** Make expands `$` in values and treats `#` as a comment. A password such as `pa$$w0rd#1` reaches `PGPASSWORD`/`SENTINEL_CLICKHOUSE_PASSWORD` truncated or altered, while Docker Compose reads the literal value from the same file. The result is an authentication failure in `make migrate` that is hard to diagnose. Every other line in `.env` also becomes a Make variable.
**Fix:** Do not `include` the file. Read the two values in the recipe instead, for example: `set -a; . ./.env; set +a` does not work because of the dotted keys, so use `export SENTINEL_POSTGRES_PASSWORD="$$(grep -E '^SENTINEL_POSTGRES_PASSWORD=' .env | cut -d= -f2-)"`, the same approach `check-stack.sh` already uses.

### WR-15: Kafka brokers list passes validation with no usable address

**File:** `backend/internal/config/config.go:100-102`, `backend/internal/app/app.go:176-189`
**Issue:** `Brokers` is only `required`. A value such as `","` or `" , "` passes validation. `DialBrokers` then skips every blank entry and returns nil, and the service logs `dependencies=kafka` as started with zero brokers. Broker entries are also not validated as `host:port`, unlike Redis and ClickHouse addresses.
**Fix:** Parse brokers once in config validation. Reject the list if no non-empty entry remains, and validate each entry with `net.SplitHostPort`. Have `DialBrokers` return an error when it dialled zero addresses.

### WR-16: Redis client cannot be configured with AUTH or TLS

**File:** `backend/internal/redisx/redisx.go:19`, `backend/internal/config/config.go:94-97`
**Issue:** `RedisConfig` has only `Address`, validated as `hostname_port`, so `rediss://` URLs are also impossible. Any non-local deployment must run Redis unauthenticated and in plaintext. Postgres was given the opposite treatment (verify-full required outside local/test, W-01). Redis backs rate limiting, which is a security control under spec §10/§11.1.
**Fix:** Add `Username`, `Password` and `TLS bool` (plus an optional CA path) to `RedisConfig` and pass them to `redis.Options`. Mirror `validateSSLMode` by rejecting non-TLS or passwordless Redis outside local/test.

## Info

### IN-01: DecodeEvent/DecodeAlert accept a bare `null` document

**File:** `backend/internal/contracts/event.go:185-198`
**Issue:** `null` unmarshals into a nil map with no error, then decodes into a zero `T`. A probe confirmed it returns `(&Event{}, nil)`. `Validate` catches it later, but a "strict" decoder should reject a non-object root.
**Fix:** After `json.Unmarshal(raw, &keys)`, reject `keys == nil` with `fieldErr("$", "must be a JSON object")`.

### IN-02: Schema `integer` vs Go `int` decoding diverge for `1.0`, `80.0`, `1e2`

**File:** `backend/internal/contracts/event.go:94-104`, `backend/internal/contracts/schema/event.v1.json:43,54`
**Issue:** JSON Schema 2020-12 treats `80.0` as an integer, so `schema_version: 1.0` and `destination_port: 80.0` validate. `encoding/json` rejects them when decoding into `int`. This is another schema/Go drift in the same class as WR-08.
**Fix:** Document "integers must be written without a fraction or exponent" in the schema description, or add an invalid_schema fixture policy that matches Go.

### IN-03: Service skeletons register no-op closers and do not verify credentials

**File:** `backend/cmd/*/main.go` (for example `backend/cmd/alert-writer/main.go:25,31`)
**Issue:** `lc.Add("kafka"/"clickhouse", func…{ return nil })` closes nothing, yet shutdown logs `closed kafka`, and `service started dependencies=...` lists connections the process does not hold. `DialTCP` checks only TCP reachability, so a wrong ClickHouse password or a dead Kafka broker that still accepts TCP passes startup.
**Fix:** Acceptable for a skeleton. Phase 2 should replace these with real clients plus an authenticated ping (ClickHouse `SELECT 1`, Kafka metadata request).

### IN-04: A second SIGINT/SIGTERM during shutdown is swallowed

**File:** `backend/internal/app/app.go:100-103, 131-135`
**Issue:** `stop()` is deferred until `Run` returns, so `NotifyContext` keeps capturing signals throughout the shutdown window of up to 30 s. An operator pressing Ctrl+C a second time cannot force an exit.
**Fix:** Pass `stop` into `RunContext`, or call it right after `<-ctx.Done()`, so a second signal gets the default behaviour.

### IN-05: Logger ignores `logging.format`; the pgx logger always writes console output to stdout

**File:** `backend/internal/logger/logger.go:39-42, 56-69, 74-77`
**Issue:**
- `LoggingConfig.Format` is defaulted and set in `.env.example` (`FORMAT=console`) but never read. The output format depends only on `Environment == "local"`.
- `NewPgxLogger` always uses `ConsoleWriter` on `os.Stdout`, so debug level in a JSON environment mixes non-JSON lines into the log stream.
- `formatFieldValue` truncates with `v[:200]`, which can split a multi-byte UTF-8 rune.
- `GetLogLevel`'s environment fallback is dead code, because the level always has a default.
**Fix:** Honour `Format` (json/console) for both loggers. Truncate on a rune boundary. Remove the dead branch or the default.

### IN-06: Request log lines contain duplicate JSON keys

**File:** `backend/internal/middleware/global.go:72-90`, `backend/internal/middleware/context.go:45-50`
**Issue:** The context logger already carries `request_id`, `method` and `ip`, and `LogValuesFunc` adds them again. A probe log line showed `"request_id"`, `"method"` and `"ip"` twice each. Some log pipelines reject or mishandle duplicate keys.
**Fix:** Drop the repeated `Str` calls in `LogValuesFunc`.

### IN-07: Config values that are validated but unused, and duplicated defaults

**File:** `backend/internal/config/config.go:89, 33-38`, `backend/internal/contracts/event.go:27-30`
**Issue:**
- `MaxIdleConns` is `required` but pgxpool has no equivalent, so the value is ignored.
- `defaultAttrKeys`, `defaultAttrBytes` and the skew defaults are defined both in `config` and in `contracts`, so a drift risk exists.
- `contracts` imports `config` only for `LimitsFromConfig`, which couples the wire-contract package to process config.
**Fix:** Remove `MaxIdleConns`, or map it to `MinConns`. Keep the defaults in one place, either by having `config` reference `contracts.DefaultLimits()` or by moving `LimitsFromConfig` into the caller.

### IN-08: Field-level validation errors never reach clients

**File:** `backend/internal/errs/http.go:51-56`, `backend/internal/validation/utils.go:38-44`
**Issue:** `HTTPError.Errors` is `json:"-"` and `Response` drops it, so `BindAndValidate` computes per-field errors that are thrown away. Clients only see "Validation failed". `nonValidatorErrors` would also copy raw `err.Error()` text into them if they were ever serialised.
**Fix:** Either extend the spec §8 envelope with an optional `details` array, or stop building field errors that are discarded.

### IN-09: chmigrate records versions via string concatenation and parses the list loosely

**File:** `backend/internal/chmigrate/chmigrate.go:84, 124, 174`
**Issue:**
- The version name is concatenated into SQL (`VALUES ('" + name + "')`). Names come from the embedded FS today, but a filename containing `'` would break or inject.
- `strings.Fields` splits names that contain whitespace.
- The response cap reuses `maxErrBody*maxErrBody` (256 KiB). A truncated listing would silently trigger re-application.
- ClickHouse error bodies are dropped entirely (T-07-06), so failures show only a status code.
**Fix:** Use ClickHouse query parameters (`INSERT ... VALUES ({v:String})` with `param_v=`). Split on `\n`. Use a dedicated constant and treat a hit on the cap as an error. Log a sanitised `Code: NNN` prefix of the error body.

### IN-10: Alert and Event invariants that are not enforced

**File:** `backend/internal/contracts/alert.go:165-200`, `backend/internal/contracts/event.go:357-371`
**Issue:**
- `sample_event_ids` may contain duplicates or more entries than `event_count`.
- `created_at` and `detected_at` have no range check: year 0000 passes because only the year-1 zero value is rejected.
- The client-supplied `received_at` is accepted unvalidated and unbounded.
**Fix:** Reject duplicate samples and `len(SampleEventIDs) > EventCount`. Apply `ValidWindow` to `created_at`/`detected_at`. Have ingest overwrite `received_at`.

### IN-11: OpenAPI has no root-level security default, and ingest accepts client tenant_id/received_at

**File:** `api/openapi.yaml:1-23, 503-510`
**Issue:** Each operation declares its own `security`, so a future path added without one is documented as anonymous. `EventBatchRequest.events[]` reuses `Event`, which requires a client-sent `tenant_id` and allows `received_at`, but the spec does not say what happens when `tenant_id` differs from the key's tenant.
**Fix:** Add a root `security: [{BearerAuth: []}]` and keep `security: []` on the ops endpoints. Document "tenant_id must equal the API key's tenant, otherwise 403", and "received_at is server-assigned and ignored on input".

### IN-12: Integration harness reads the process output buffer while the process is running

**File:** `backend/test/lifecycle_test.go:86-96, 145-150`
**Issue:** `cmd.Stdout`/`Stderr` is a `bytes.Buffer` written by exec's copy goroutine. On the timeout path, `t.Fatalf(..., p.output.String())` reads it while the child is still running. That is a data race under `-race` and can hide the real failure. `freePort` in `internal/testing/containers.go:221-235` is also a TOCTOU race and a source of occasional flakes.
**Fix:** Wrap the buffer in a mutex-guarded writer, or kill and `Wait` before printing output. Accept the freePort race, or retry container start on bind failure.

### IN-13: Inconsistent ClickHouse user defaults and a narrow port-binding check

**File:** `backend/internal/config/config.go:40`, `backend/tools/chmigrate/main.go:24`, `scripts/check-pins.sh:35`
**Issue:** The config default `clickhouse.user` is `default`, but Compose creates only `sentinel` and chmigrate defaults to `sentinel`. `check-pins.sh` detects only quoted short-syntax port mappings, so `- 8123:8123` (unquoted) or long syntax bypasses the 127.0.0.1 check.
**Fix:** Align the default user to `sentinel`, or make the user required. Parse ports with `docker compose config --format json` and check every `published` entry's `host_ip`.

### IN-14: Kafka EXTERNAL listener advertises `localhost` while the port is bound only on 127.0.0.1

**File:** `deployments/compose/docker-compose.yml:15,21`
**Issue:** Clients bootstrap to `127.0.0.1:29092` and are then redirected to `localhost:29092`. On hosts where `localhost` resolves to `::1` first, and the client does not fall back to IPv4, the connection fails after bootstrap.
**Fix:** Advertise `EXTERNAL://127.0.0.1:29092`.

---

_Reviewed: 2026-10-11T13:57:12Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
