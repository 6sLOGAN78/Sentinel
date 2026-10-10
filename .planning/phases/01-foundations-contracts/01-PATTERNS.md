# Phase 1: Foundations & Contracts - Pattern Map

**Mapped:** 2026-10-10
**Files analyzed:** 44 new/modified (grouped where they share a pattern)
**Analogs found:** 17 in-repo analogs / 44. The rest take their pattern from RESEARCH.md, which was executed and verified.

Context: `backend/` has 2,322 Go lines in the paths I scanned. It has **no `_test.go` files at all**, so there is no in-repo test analog. Every test in this phase is new, and test patterns come from RESEARCH.md. The only in-repo test-support code is `backend/internal/testing/{container,assertions,transaction}.go`.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `backend/cmd/{ingest,detector,event-writer,alert-writer,query-api}/main.go` | entrypoint | lifecycle (signal-driven) | `backend/cmd/sentinel/main.go` | role-match (rewrite) |
| `backend/internal/config/config.go` (modify) | config | transform (env to struct) | itself | exact (refactor) |
| `backend/internal/config/observability.go` (modify) | config | transform | itself | exact (refactor) |
| `backend/internal/config/config_test.go` | test | transform | none | no analog |
| `backend/internal/logger/logger.go` (modify) | utility | transform | itself | exact (refactor) |
| `backend/internal/errs/{http,types}.go` (modify) | model/util | request-response | itself | exact (refactor) |
| `backend/internal/database/database.go` (modify) | service | CRUD (pool) | itself | exact (refactor) |
| `backend/internal/database/migrator.go` (modify) | service | batch (migrations) | itself | exact (refactor) |
| `backend/migrations/embed.go` + `postgres/*.sql` | migration | file-I/O | `internal/database/migrator.go` embed | partial |
| `backend/internal/chmigrate/*.go` and `backend/tools/chmigrate/main.go` | service | batch (HTTP to ClickHouse) | `internal/database/migrator.go` | role-match |
| `backend/internal/redisx/redisx.go` | service | request-response | `internal/server/server.go` lines 31-60 | partial (fix defect) |
| `backend/internal/httpx/httpx.go` | service | request-response | `internal/server/server.go` lines 77-113 | role-match |
| `backend/internal/middleware/{request_id,context,global}.go` (modify) | middleware | request-response | themselves | exact (refactor) |
| `backend/internal/middleware/middlewares.go` | middleware | aggregator | itself | delete or rewrite |
| `backend/internal/contracts/{event,alert,dedupe}.go` | model | transform | `internal/validation/utils.go` (validation shape only) | weak |
| `backend/internal/contracts/schema/{event,alert}.v1.json` | config/schema | file-I/O (embed) | none | no analog |
| `backend/internal/contracts/*_test.go` and `testdata/` | test | transform | none | no analog |
| `backend/internal/testing/*.go` (rewrite) | test-util | CRUD | `internal/testing/container.go` | exact (refactor) |
| `backend/test/*_test.go` (build tag `integration`) | test | e2e | `internal/testing/container.go` | role-match |
| `backend/.golangci.yml` (modify) | config | n/a | itself | exact |
| `deployments/compose/docker-compose.yml`, `Makefile`, `.env.example`, `.github/workflows/ci.yml`, `.gitignore`, `.gitleaks.toml`, `scripts/check-*.sh` | infra/config | n/a | none in repo | no analog (use RESEARCH.md) |
| `api/openapi.yaml`, `config/rules.yaml`, `docs/decisions/0001..0006-*.md`, `docs/architecture.md`, `benchmarks/README.md` | docs/contract | n/a | none | no analog |
| `web/**`, `generator/**` | scaffolds | n/a | none | no analog (use RESEARCH.md versions) |

## Pattern Assignments

### `backend/cmd/*/main.go` (5 entrypoints, lifecycle)

**Analog:** `/home/logan78/Desktop/Sentinel/backend/cmd/sentinel/main.go` (80 lines). Keep the shape (config, logger, deps, signal wait, shutdown with timeout). Fix the following:
- It uses `panic` on config error and `log.Fatal` while holding resources.
- It listens only for `os.Interrupt`; D-06 needs SIGINT and SIGTERM.
- `stop()` and `cancel()` are called after the `Fatal` path.
- It starts an HTTP server; the Phase 1 binaries must not.

Keep the shutdown timeout const (lines 19, 62-63):
```go
const DefaultContextTimeout = 30
...
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
<-ctx.Done()
ctx, cancel := context.WithTimeout(context.Background(), DefaultContextTimeout*time.Second)
```
Target shape (from RESEARCH "Go binaries pattern"), with `run() error` returning errors and `main` doing a single `os.Exit(1)`:
```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
// connect only this binary's deps; defer-stack closes on every error path
<-ctx.Done()
shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
defer cancel()
```
Per-binary deps:

| Binary | Dependencies |
|---|---|
| ingest | redis + Kafka TCP dial |
| detector | kafka (+redis?) |
| event-writer | kafka + clickhouse |
| alert-writer | kafka + postgres + clickhouse |
| query-api | postgres + clickhouse + redis |

Kafka and ClickHouse reachability is a plain `net.Dialer` TCP dial in Phase 1. Workers must not import `internal/server` or `internal/httpx`.

---

### `backend/internal/config/config.go` (config, refactor)

**Analog:** itself. Keep the koanf env loader (lines 62-73) and the validator call:
```go
k := koanf.New(".")
err := k.Load(env.Provider("SENTINEL_", ".", func(s string) string {
    return strings.ToLower(strings.TrimPrefix(s, "SENTINEL_"))
}), nil)
...
validate := validator.New()
err = validate.Struct(mainConfig)
```
Change list (maps to D-05):
- **Line 6** `_ "github.com/joho/godotenv/autoload"` is an anti-pattern. Replace it with an explicit `godotenv.Load()` guarded by `os.Getenv("SENTINEL_PRIMARY.ENV") == "local"`, called before the koanf load.
- **Lines 57-98**: every `logger.Fatal()` becomes `return nil, fmt.Errorf("...: %w", err)`. `LoadConfig` currently always returns `nil` error, so nothing is testable. Rename it to `Load()`.
- **Lines 12-20** remove `Auth` (`secret_key` is Clerk) and `Integration` (`ResendAPIKey` is `validate:"required"`, so startup fails without a Resend key). Add `Kafka`, `ClickHouse`, `RateLimit{FailMode}`, `Contracts{MaxAttrKeys,MaxAttrBytes,FutureSkew,PastSkew}`. Add `Redis` fields as needed.
- Hardcoded `ServiceName = "sentinel"` (line 82) should be per binary: `logger.New(cfg, "ingest")`.
- **Line 4** has trailing whitespace and the import block is unsorted; run `gofmt`/`goimports`.

Struct tag convention to copy for new sections (lines 29-41):
```go
type RedisConfig struct {
	Address string `koanf:"address" validate:"required"`
}
```
Env key convention: nested keys use `.` (e.g. `SENTINEL_DATABASE.HOST`), and `.env.example` must use the same names.

---

### `backend/internal/config/observability.go` (config, refactor)

**Analog:** itself. Delete `NewRelicConfig` and the `NewRelic` field (lines 11, 20-25, 49-54). Keep `LoggingConfig`, `Validate()` (lines 63-80, the `validLevels` map pattern), `GetLogLevel`, `IsProduction`. `HealthChecksConfig` is for Phase 2 (`/healthz`); drop it or leave it unused. Prefer dropping it.

---

### `backend/internal/logger/logger.go` (utility, refactor)

**Analog:** itself. Delete `LoggerService`, `nrApp`, `NewLoggerService`, `Shutdown`, `GetApplication`, `WithTraceContext` (lines 15-70, 131-147) and the imports `newrelic` and `zerolog/pkgerrors` (`zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack` at line 94 depends on `pkg/errors`).

Keep `NewPgxLogger` and `GetPgxTraceLogLevel` verbatim (lines 150-222). They are used by `database.New`.

Replace `NewLoggerWithService` with `New(cfg, service string) zerolog.Logger`. Keep:
```go
logger := zerolog.New(writer).Level(logLevel).With().Timestamp().
    Str("service", cfg.ServiceName).Str("environment", cfg.Environment).Logger()
```
Behavior change: emit JSON to stdout in every environment except `local` (the current code only emits JSON when `IsProduction() && Format == "json"`). Keep `ConsoleWriter` for `local`. Drop `.Stack()` unless a stack marshaler is set.

---

### `backend/internal/errs/{http,types}.go` (model, refactor to spec section 8 envelope)

**Analog:** itself. Current `HTTPError` marshals `status`, `override`, `errors`, `action` at the top level (http.go lines 22-31). Target wire shape is `{"error":{"code","message","request_id"}}`.

Keep the helper constructors pattern (types.go):
```go
func NewUnauthorizedError(message string, override bool) *HTTPError {
	return &HTTPError{
		Code:     MakeUpperCaseWithUnderscores(http.StatusText(http.StatusUnauthorized)),
		Message:  message, Status: http.StatusUnauthorized, Override: override,
	}
}
```
Changes:
- Add `RequestID string` to the body and a wrapper response type `type ErrorResponse struct { Error ErrorBody `json:"error"` }`.
- Mark `Status` as `json:"-"`.
- Add constructors for 413, 429 (with `Retry-After`) and 503 (spec section 8 / D-17).
- Keep `Is`/`WithMessage`/`Error()`. `WithMessage` must copy the new fields.
- `types.go` line 1 has `package errs ` with trailing space (gofmt drift). Run `gofmt`.

---

### `backend/internal/database/database.go` (service, refactor)

**Analog:** itself. Replace the duplicated `fmt.Sprintf` DSN (lines 55-63; same block in migrator.go lines 21-30) with one exported builder:
```go
func ConnString(c config.DatabaseConfig) string {
	q := url.Values{"sslmode": {c.SSLMode}}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.User, c.Password),
		Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Path: "/" + c.Name, RawQuery: q.Encode()}
	return u.String()
}
```
Unit test with password `p@ss:w/rd?#% +` and round-trip it through `pgxpool.ParseConfig` to assert `ConnConfig.Password`.

Other changes:
- Delete `multiTracer` (lines 22-48), the `nrpgx5` import, and the `loggerService` parameter. New signature is `New(ctx, cfg, logger)`.
- Keep the local-only pgx tracelog block (lines 77-95), simplified to the `else` branch:
  ```go
  pgxPoolConfig.ConnConfig.Tracer = &tracelog.TraceLog{
      Logger: pgxzero.NewLogger(pgxLogger),
      LogLevel: tracelog.LogLevel(loggerConfig.GetPgxTraceLogLevel(globalLevel)),
  }
  ```
- **Resource leak (D-05):** the ping failure at lines 112-114 returns without `pool.Close()`. Add `pool.Close()` before returning.
- `DatabasePingTimeout = 10` is an untyped int multiplied by `time.Second` (and a `mnd` hit). Make it `10 * time.Second`.
- The pool config ignores `MaxOpenConns` and the other config fields. Wire `MaxConns`, `MaxConnLifetime` and `MaxConnIdleTime` (the config fields are already defined).
- `Close()` logs then calls `db.Pool.Close()`; keep it.

---

### `backend/internal/database/migrator.go` + `backend/migrations/` (migration runner)

**Analog:** itself. Keep the tern flow (lines 32-64):
```go
m, err := tern.NewMigrator(ctx, conn, "schema_version")
subtree, err := fs.Sub(migrations, "migrations")
if err := m.LoadMigrations(subtree); err != nil { ... }
from, err := m.GetCurrentVersion(ctx)
if err := m.Migrate(ctx); err != nil { return err }
if from == int32(len(m.Migrations)) { logger.Info().Msgf("database schema up to date, version %d", ...) }
```
Changes:
- `//go:embed migrations/*.sql` (line 18) cannot reach `backend/migrations`. Create `backend/migrations/embed.go` (`package migrations`, `//go:embed postgres/*.sql clickhouse/*.sql`) and have the migrator import it. A `//nolint:gochecknoglobals // go:embed requires a package-level variable` is needed; or expose `func Postgres() fs.FS`.
- Use `ConnString(cfg.Database)` instead of the duplicate DSN.
- Line 54 `fmt.Errorf("retreiving current database migration version")` drops the cause. Add `: %w`.
- `Migrate` returns the raw `err` from `pgx.Connect` and `m.Migrate`; wrap them.
- `defer conn.Close(ctx)` is fine.
- The second `make migrate` must print "up to date" (FND-02 idempotency).

New migration: `backend/migrations/postgres/001_schema_version.sql` in tern format. It carries only bookkeeping; D-11 says tern itself creates `schema_version`, so the file can be a no-op comment plus `---- create above / drop below ----`. Do not copy the empty `001_setup.sql`.

---

### `backend/internal/chmigrate/` + `backend/tools/chmigrate/main.go` (ClickHouse runner, ~100 lines)

**Analog:** the tern migrator above (same "load ordered files, record applied versions, log up to date vs migrated" logic). Role-match only. There is no ClickHouse code in the repo. Use `net/http` against port 8123 per RESEARCH; do not add clickhouse-go. Logic:
1. `CREATE DATABASE IF NOT EXISTS`.
2. `CREATE TABLE IF NOT EXISTS schema_migrations(version, applied_at)`.
3. For each embedded `.sql` in sorted order, skip applied versions and run the rest.

Pass credentials via HTTP basic auth or headers, never in the URL. Wrap errors with `%w`. Put the runner under `tools/`, not `cmd/` (five-binary rule).

---

### `backend/internal/redisx/redisx.go` (service, request-response)

**Analog:** `/home/logan78/Desktop/Sentinel/backend/internal/server/server.go` lines 40-56. Keep the client construction and 5s ping timeout, but fix the defect:
```go
redisClient := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address})
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := redisClient.Ping(ctx).Err(); err != nil {
    logger.Error().Err(err).Msg("Failed to connect to Redis, continuing without Redis")  // DEFECT: must be fatal
}
```
New: `func New(ctx context.Context, cfg RedisConfig) (*redis.Client, error)`. On ping error call `client.Close()` and `return nil, fmt.Errorf("pinging redis: %w", err)`. Drop `nrredis`. Test with a closed port, expecting an error, and with a testcontainers Redis, expecting success. The log message should be lower-case per CLAUDE.md.

---

### `backend/internal/httpx/httpx.go` (HTTP helper for ingest and query-api only)

**Analog:** `/home/logan78/Desktop/Sentinel/backend/internal/server/server.go` lines 77-113. Keep the timeout wiring:
```go
s.httpServer = &http.Server{
    Addr: ":" + s.Config.Server.Port, Handler: handler,
    ReadTimeout:  time.Duration(s.Config.Server.ReadTimeout) * time.Second,
    WriteTimeout: time.Duration(s.Config.Server.WriteTimeout) * time.Second,
    IdleTimeout:  time.Duration(s.Config.Server.IdleTimeout) * time.Second,
}
...
if s.httpServer == nil { return errors.New("HTTP server not initialized") }
```
Fix `Shutdown` (lines 100-113): `s.httpServer.Shutdown(ctx)` is called without a nil check (panics if `SetupHTTPServer` was never called). The new helper must nil-guard and must not own DB/Redis. Each binary closes its own deps in reverse order. Add a `ReadHeaderTimeout` (gosec/slowloris). Tests: Shutdown before Start returns nil without panic; Shutdown closes the listener.

Note: D-06 forbids routes in Phase 1, so the helper may be built and tested without being wired into any `main`. Ingest and query-api only call it from Phase 2 on.

---

### `backend/internal/middleware/{request_id,context,global}.go` (middleware, refactor)

**request_id.go** (33 lines) is already dependency-free. Keep as is. Optionally validate or limit the length of an inbound `X-Request-ID`; the current code echoes any client string into logs and headers (log injection risk):
```go
requestID := c.Request().Header.Get(RequestIDHeader)
if requestID == "" { requestID = uuid.New().String() }
c.Set(RequestIDKey, requestID)
c.Response().Header().Set(RequestIDHeader, requestID)
```

**context.go** (96 lines): constructor takes `*server.Server`. Change to `NewContextEnhancer(logger *zerolog.Logger)` and use `ce.logger.With()...` (line 33). Delete the `newrelic.FromContext` block (lines 41-44) and the `logger` import. Remove `extractUserID`/`extractUserRole` comments mentioning Clerk; the keys can stay (Phase 2 will set them). Keep `GetLogger` with its `zerolog.Nop()` fallback (lines 88-96).

**global.go** (173 lines):
- Replace `github.com/pkg/errors` with stdlib `errors` (line 9); `errors.As` is used at lines 49-53, 122, 134.
- `GlobalMiddlewares{server *server.Server}` becomes `NewGlobalMiddlewares(allowedOrigins []string)`. CORS uses `global.server.Config.Server.CORSAllowedOrigins` (line 28).
- `Recover()` (lines 103-105) exists, but D-05 says "Recover registered first". That ordering lives in the Phase 2 router, so in Phase 1 add an `httpx`/middleware test that asserts a panicking handler yields a 500 JSON envelope when `Recover` is first in the chain.
- `GlobalErrorHandler` (lines 111-172) must now emit `errs.ErrorResponse{Error: {Code, Message, RequestID: GetRequestID(c)}}`; today it writes `errs.HTTPError{...}` flat, which violates spec section 8. Keep the three-way switch (`*errs.HTTPError` / `*echo.HTTPError` / default 500) and the "log the original error, return the sanitized one" structure. The `c.Response().Committed` guard stays.
- `RequestLogger` logs `ip` and `user_agent` as log fields. That is fine for logs (the metrics-label ban applies only to Prometheus).

**middlewares.go**: the aggregator imports newrelic and `server`. Delete it or reduce it to a tiny struct of the three kept middlewares. `auth.go`, `rate_limit.go`, `tracing.go` are deleted.

Because no handler/router/service/repository/model packages are kept (research assumption A7), the echo dependency is only used by `middleware`, `validation` and `httpx`.

---

### `backend/internal/validation/utils.go` (keep) and `internal/sqlerr/*` (keep)

Both import only `errs` and echo. Verify they still compile after the `errs` reshape. Known fragile code in `BindAndValidate` (lines 28-31): `strings.Split(strings.Split(err.Error(), ",")[1], "message=")[1]` panics on short error strings; the plan should add a test for a malformed JSON body or fix it. `extractValidationErrors` uses an unchecked type assertion `err.(CustomValidationErrors)` (errcheck `check-type-assertions: true` will flag it).

`sqlerr` imports `golang.org/x/text/cases`, so `golang.org/x/text` stays in `go.mod`. Do not tidy it away.

---

### `backend/internal/contracts/{event,alert,dedupe}.go` (model + util, transform)

**Analog:** none for domain code; only convention analogs. Use these conventions:
- Package-level doc comments start with the identifier name (CLAUDE.md).
- Custom error types follow `validation.CustomValidationError{Field, Message}` (validation/utils.go lines 16-26) for field-level reporting.
- The embedded FS is wrapped in a function, not an exported global (gochecknoglobals).

Everything else comes from RESEARCH.md (executed code):
- `DedupeKey` implementation (length-prefixed, domain separator `"sentinel.alert.v1"`, big-endian lengths, `windowStart.UTC().UnixMilli()`, SHA-256 lowercase hex).
- jsonschema/v6 compile with `c.AssertFormat()` and `Draft2020`.
- Typed decode with `DisallowUnknownFields`.
- `net/netip.ParseAddr` for IPs.
- `Validate(now, skew)` for the +5m/-7d bounds.
- Tenant regex `^[a-z0-9][a-z0-9_-]{0,62}$`.

Test file packages must be `contracts_test` (the `testpackage` linter). Fuzz target: `FuzzDedupeKey` seeded with `("x|y","z")` vs `("x","y|z")`. Add golden-vector tests.

---

### `backend/internal/testing/*` (rewrite)

**Analog:** `/home/logan78/Desktop/Sentinel/backend/internal/testing/container.go` (161 lines). Reuse the shape of `SetupTestDB`: generic container request, wait strategy, `t.Helper()` first line, `t.Cleanup` for terminate, then build the config struct.

Copy:
```go
req := testcontainers.ContainerRequest{
    Image: "postgres:15-alpine",              // CHANGE to postgres:18.6 (match compose)
    ExposedPorts: []string{"5432/tcp"},
    Env: map[string]string{"POSTGRES_DB": dbName, "POSTGRES_USER": dbUser, "POSTGRES_PASSWORD": dbPassword},
    WaitingFor: wait.ForLog("database system is ready to accept connections").WithStartupTimeout(30 * time.Second),
}
pgContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
require.NoError(t, err, "failed to start postgres container")
t.Cleanup(func() { if err := pgContainer.Terminate(ctx); err != nil { t.Logf(...) } })
```
Do not copy:
- Image `postgres:15-alpine`; the pin must match compose (18.6).
- The `time.Sleep(2 * time.Second)` retry loop (lines 91-112); use a proper wait strategy (`wait.ForListeningPort` or SQL-based) with no sleeps (Pitfall 9).
- The `config.Config` literal containing `Integration`/`Auth` fields (lines 76-84); these fields no longer exist.
- The `database.New(cfg, &logger, nil)` signature (changes) and the container-per-test pattern (use `TestMain` per package).
- The `time.Sleep(2s)` also hides the fact that the log wait string fires twice on Postgres init.

Add siblings built the same way: `SetupRedis`, `SetupClickHouse`, and `SetupKafka` (generic `apache/kafka:4.3.1` container with the compose env block and a pre-chosen free host port for the advertised listener; the testcontainers kafka module targets confluent-local and does not apply).

Keep `assertions.go` as is. It already uses `t.Helper()`:
```go
func AssertTimestampsValid(t *testing.T, obj interface{}) {
	t.Helper()
```
Keep `transaction.go`, but it takes `*TestDB`; keep `TestDB` or change the param to a pool. `server.go` and `helpers.go` depend on `server.Server` and are rewritten or deleted. All files in this package are in package `testing`, so `testpackage` lint rules do not apply to them (non-test files).

---

### `backend/.golangci.yml` (modify)

**Analog:** itself. Delete the three lines `gosimple` (line 9), `typecheck` (line 13) and `stylecheck` from `linters.enable`; the `config verify` failure was reproduced in research. Everything else carries over. Add `//nolint:gochecknoglobals // <reason>` on embed vars only.

---

## Shared Patterns

### Error wrapping and no-fatal-in-library
**Source:** `backend/internal/database/database.go` lines 60-72 and `migrator.go` line 41.
**Apply to:** every new package (`redisx`, `httpx`, `chmigrate`, `config`, `contracts`).
```go
pgxPoolConfig, err := pgxpool.ParseConfig(dsn)
if err != nil {
    return nil, fmt.Errorf("failed to parse pgx pool config: %w", err)
}
```
Wrap with `%w`; only `main` decides to exit. Do not copy the places that drop the cause (`migrator.go` line 54, `config.go` `logger.Fatal`).

### Constructor DI without `*server.Server`
**Source:** `backend/internal/middleware/context.go` lines 19-25 (pattern), changed to take explicit deps.
**Apply to:** all middleware, `httpx`, binaries.
```go
type ContextEnhancer struct{ server *server.Server }
func NewContextEnhancer(s *server.Server) *ContextEnhancer { return &ContextEnhancer{server: s} }
```
Replace the `server` field with the concrete dependencies, for example `logger *zerolog.Logger`. No globals and no `init()`. The import direction rule: `config`, `logger`, `database`, `redisx` are leaves; `middleware`/`httpx` may import them; workers import only leaves.

### Structured logging
**Source:** `backend/internal/middleware/global.go` lines 61-98 and `context.go` lines 34-39.
**Apply to:** binaries, middleware.
```go
contextLogger := ce.server.Logger.With().
    Str("request_id", requestID).Str("method", c.Request().Method).
    Str("path", c.Path()).Str("ip", c.RealIP()).Logger()
```
Fields only (`.Err(err)`, `.Dur(...)`, `.Str(...)`), then `.Msg("lower-case message")`. Binaries get a `service` field from `logger.New(cfg, svc)`.

### Test conventions
No in-repo test analog exists. Conventions from CLAUDE.md and RESEARCH.md: `t.Helper()` first in helpers; testify `require`/`assert`; contract tests use `package contracts_test`; integration tests use `//go:build integration` and live in `backend/test/`; `tparallel` and `testifylint` are enabled; use `go test -race`.

### Config key naming
**Source:** `backend/internal/config/config.go` struct tags. `koanf:"snake_case"`, `SENTINEL_SECTION.KEY` env names. Apply to `.env.example` and compose `environment:` (compose project-level vars `SENTINEL_POSTGRES_PASSWORD` and `SENTINEL_CLICKHOUSE_PASSWORD` are compose interpolation inputs and must not be confused with koanf nested keys; `.env.example` should hold both sets, placeholders only, e.g. `change-me-local`).

## No Analog Found

Planner should use the RESEARCH.md blocks (executed 2026-10-10) instead of in-repo code.

| File | Role | Data Flow | Source to use |
|---|---|---|---|
| `deployments/compose/docker-compose.yml` | infra | n/a | RESEARCH "Verified Compose" (the Postgres volume `/var/lib/postgresql` and `${VAR:?}` bits are untested) |
| `Makefile` | build | n/a | RESEARCH "Makefile skeleton" |
| `.github/workflows/ci.yml` | CI | n/a | RESEARCH "CI skeleton" (action versions to be re-checked) |
| `backend/internal/contracts/schema/*.json` | contract | embed | RESEARCH "jsonschema/v6 usage" and D-13/D-16 |
| `backend/internal/contracts/*_test.go` | test | transform | RESEARCH "Fixture set"; fuzz block |
| `backend/test/openapi_test.go` | test | validate | RESEARCH "Validation test (kin-openapi)" |
| `api/openapi.yaml` | contract | n/a | spec section 8 plus RESEARCH OpenAPI section (3.1: `type: [x, "null"]`) |
| `web/**` | scaffold | n/a | RESEARCH web version table and `overrides` fragment |
| `generator/**` | scaffold | n/a | RESEARCH generator section (`uv init/add/export`) |
| `docs/decisions/0001-0006-*.md`, `docs/architecture.md` | docs | n/a | CONTEXT D-24..D-27 content seeds |
| `.env.example`, `.gitignore` (root), `.gitleaks.toml`, `scripts/check-*.sh` | config | n/a | RESEARCH Validation Architecture |

## Files to Delete (for planner checklist; no pattern needed)

`backend/cmd/sentinel/`, `backend/internal/{lib/job,lib/email,lib/utils,handler,router,service,repository,model,server}/`, `backend/internal/middleware/{auth,rate_limit,tracing}.go`, `backend/internal/database/migrations/001_setup.sql`, `backend/static/`, `backend/templates/`, `backend/taskfile.yml`, `backend/.env.sample`, `backend/readme.md` and `backend/internal/readme.md` (rewrite or delete), and the root `apps/`, `packages/`, `package.json`, `turbo.json`, `bun.lock`, `package-lock.json`, `node_modules/`. Never read or touch `backend/.env`.

## Metadata

**Analog search scope:** all of `/home/logan78/Desktop/Sentinel/backend` (about 50 Go files, 2,322 lines in the retained and server/cmd packages).
**Files read in full or in relevant part:** config (2), logger, errs (2), database (2), middleware (request_id, global, context), server, cmd/sentinel, testing (container, assertions, transaction), validation, sqlerr/handler, `.golangci.yml`, `go.mod` head.
**Test analogs:** none (no `_test.go` files exist).
**Pattern extraction date:** 2026-10-10
