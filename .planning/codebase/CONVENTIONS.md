# Coding Conventions

**Analysis Date:** 2026-10-10

Monorepo: Go backend (`backend/`) plus TypeScript packages (`packages/zod`, `packages/openapi`, `packages/emails`) managed by Turbo/Bun (`turbo.json`, `package.json`). Go module path: `github.com/6sLOGAN78/sentinel`. No project skills directory (`.claude/skills`, `.agents/skills`) exists.

## Naming Patterns

**Files (Go):**
- Lowercase, single-word or snake_case: `rate_limit.go`, `request_id.go`, `email_tasks.go`.
- Plural aggregator files hold the container struct: `handlers.go`, `services.go`, `repositories.go`, `middlewares.go`.
- `base.go` holds the shared base type for a package (`internal/handler/base.go`, `internal/model/base.go`).

**Files (TS):**
- Lowercase: `health.ts`, `index.ts`, `utils.ts`. Barrel `index.ts` per package/folder.

**Go Functions/Types:**
- Constructors: `NewXxx(s *server.Server)` returning a pointer (`NewHealthHandler`, `NewAuthService`, `NewAuthMiddleware`).
- Handlers/services/middleware are structs with unexported `server *server.Server` field (services/middleware) or embedded `Handler` (handlers).
- Handler methods are exported PascalCase verbs: `CheckHealth`, `RequireAuth`.
- Error constructors in `internal/errs/types.go`: `NewUnauthorizedError`, `NewBadRequestError`, etc.
- Package names are short lowercase (`errs`, `sqlerr`, `lib/email`).

**TS:**
- Zod schemas prefixed `Z`: `ZHealthResponse`, `ZHealthCheck` (`packages/zod/src/health.ts`).
- ts-rest contracts suffixed `Contract`: `healthContract` (`packages/openapi/src/contracts/health.ts`).
- Package scope `@sentinel/*` (e.g. `@sentinel/zod`); internal alias `@/utils.js` (ESM, `.js` extension in imports).

**JSON/API fields:** snake_case (`response_time`, `created_at`).

## Code Style

**Formatting:**
- Go: `gofmt`/`goimports` (enforced via `goimports` linter in `backend/.golangci.yml`). Some existing files (`internal/errs/types.go`, `internal/testing/container.go`) have formatting drift; run `gofmt` on touched files.
- TS: 2-space indent, double quotes, semicolons, trailing commas. Turbo scripts `format:check` / `format:fix` exist at root.

**Linting:**
- Go: golangci-lint v2 config at `backend/.golangci.yml`, `default: none` with a long explicit enable list. Key limits: `funlen` 100 lines/50 statements, `cyclop` 30, `gocognit` 20, `lll`, `mnd` (no magic numbers), `gochecknoglobals`, `gochecknoinits`, `nonamedreturns`, `errorlint`, `govet` enable-all with strict shadow (no `fieldalignment`), `errcheck` with `check-type-assertions: true`, `testpackage`, `tparallel`, `testifylint`, `sloglint`, `forbidigo`.
- `nolint` directives require a specific linter name and an explanation (`nolintlint`), except for `funlen`, `gocognit`, `lll`.
- TS: root scripts `lint`, `lint:fix`, `typecheck` via Turbo; TypeScript ^6, strict via per-package `tsconfig.json`.

## Import Organization

**Order (Go):**
1. Standard library
2. Internal packages `github.com/6sLOGAN78/sentinel/internal/...`
3. Third-party (`github.com/labstack/echo/v4`, etc.)
(Blank-line grouping is not strictly consistent across files; prefer stdlib, then internal, then third-party, as in `internal/handler/health.go`.)

**Path Aliases:** TS uses `@/` -> package `src/` (see `packages/openapi/src/contracts/health.ts`); workspace imports via `@sentinel/zod`.

## Error Handling

- Return errors up; wrap with `fmt.Errorf("...: %w", err)` (`internal/handler/health.go`). Use `errors.As` for typed errors (`internal/sqlerr/handler.go`).
- HTTP errors use `*errs.HTTPError` built through `errs.NewXxxError(message, override, ...)` in `internal/errs/types.go`; codes are produced by `MakeUpperCaseWithUnderscores(http.StatusText(...))`. Optional code/field errors passed as pointers/slices.
- Postgres errors are converted to a custom type via `sqlerr.ConvertPgError`; query codes with `sqlerr.ErrCode(err)`.
- Handlers `return` errors to Echo's central error handler (`internal/middleware/global.go`) rather than writing responses ad hoc (the auth failure handler in `internal/middleware/auth.go` is the exception because it runs outside Echo).

## Logging

**Framework:** zerolog (`github.com/rs/zerolog`), wrapped by `internal/logger/logger.go`, with New Relic integration.

**Patterns:**
- Get request-scoped logger: `middleware.GetLogger(c).With().Str("operation", "health_check").Logger()`.
- Structured fields only: `.Err(err)`, `.Dur("response_time", d)`, `.Str(...)`; end with `.Msg("lower-case message")`.
- Record New Relic custom events for failures via `h.server.LoggerService.GetApplication().RecordCustomEvent(...)`, guarded by nil checks.

## Comments

- Go doc comments on exported helpers start with the identifier name (`// SetupTestDB creates ...`, `// ErrCode reports ...`).
- Inline `//` comments explain steps within long functions. No trailing periods required.
- TS: no JSDoc used.

## Function Design

- Handlers receive `echo.Context` and return `error`.
- Dependencies are injected via `*server.Server` in constructors; no globals or `init()` (linted).
- Avoid long functions; `internal/handler/health.go` `CheckHealth` is at the funlen edge and has repeated blocks (do not copy its duplication).
- Use `t.Helper()` first line in test helpers.

## Module Design

**Exports:** Aggregator structs wire dependencies: `internal/handler/handlers.go`, `internal/service/services.go`, `internal/repository/repositories.go`, `internal/middleware/middlewares.go`. Add new handler/service/repo by creating a file, then registering it in the aggregator.

**Barrel Files:** TS only: `packages/zod/src/index.ts`, `packages/openapi/src/index.ts`, `packages/openapi/src/contracts/index.ts`. Add new schemas in `packages/zod/src/<name>.ts` and re-export from `index.ts`.

---

*Convention analysis: 2026-10-10*
