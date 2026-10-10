# Codebase Concerns

**Analysis Date:** 2026-10-10

## Tech Debt

**Boilerplate scaffolding not yet filled in:**
- Issue: The repo was initialized from go-boilerplate. Core layers are empty stubs: `Repositories` is an empty struct, `AuthService` only sets the Clerk key, and no business routes exist.
- Files: `backend/internal/repository/repositories.go`, `backend/internal/service/auth.go`, `backend/internal/router/router.go` (`router.Group("/api/v1")` result is discarded), `backend/internal/model/base.go`
- Impact: No domain logic exists. `RequireAuth` in `backend/internal/middleware/auth.go` is never attached to any route.
- Fix approach: Register versioned route groups, wire `RequireAuth` onto them, and implement repositories.

**Empty initial migration:**
- Issue: `backend/internal/database/migrations/001_setup.sql` contains only the template comments.
- Impact: The schema is empty and the migrator runs a no-op.
- Fix approach: Add real schema migrations, or remove this one.

**Debug helper using fmt.Println:**
- Files: `backend/internal/lib/utils/utils.go` (`fmt.Println` on JSON marshal)
- Impact: It bypasses zerolog structured logging and could leak data to stdout.
- Fix approach: Delete it or switch it to the logger.

**Placeholder docs and generated artifacts:**
- Files: `backend/static/openapi.json` and `packages/openapi/openapi.json` are duplicated generated outputs. `readme.md` is generic boilerplate text.
- Impact: The two specs can drift from each other.
- Fix approach: Pick one canonical output path in `packages/openapi/src/gen.ts`.

## Known Bugs

**Redis failure is swallowed at startup:**
- Symptoms: In `backend/internal/server/server.go`, a failed Redis ping logs an error and startup continues. Later Redis use will fail at runtime.
- Fix approach: Make this fail-fast in production, or add a health indicator and a nil-safe fallback.

**Shutdown can nil-deref:**
- Files: `backend/internal/server/server.go` (`Shutdown` calls `s.httpServer.Shutdown` without a nil check, while `Start` does check). The Redis client is also never closed.
- Fix approach: Add the nil guard and call `s.Redis.Close()`.

**Job server started before the Server struct is complete, and leaked on error:**
- Files: `backend/internal/server/server.go`. `jobService.Start()` runs before DB and Redis are attached to the Server. If `Start` returns an error, the DB connection is not closed.
- Fix approach: Add cleanup on error paths.

**Auth failure handler logs at Error level and logs a misleading message on success of the JSON write:**
- Files: `backend/internal/middleware/auth.go`. The `else` branch logs "could not get session claims" for every 401.
- Impact: Noisy logs, and unauthenticated traffic looks like server errors.
- Fix approach: Log at Warn level.

## Security Considerations

**Rate limiting is in-memory and IP-keyed per instance:**
- Files: `backend/internal/router/router.go` (`NewRateLimiterMemoryStore(rate.Limit(20))`)
- Risk: The limit is not shared across replicas and is hardcoded. A Redis client exists but is unused for this. Behind a proxy, `RealIP` can be spoofed unless the trusted proxy is configured.
- Recommendations: Move the limit into config, use a Redis-backed store, and set `IPExtractor`.

**CORS and security headers use defaults:**
- Files: `backend/internal/middleware/global.go`. CORS has only `AllowOrigins` from config, and `middleware.Secure()` has no HSTS or CSP configuration.
- Recommendations: Validate that `CORSAllowedOrigins` is never `*` in production, and set explicit methods, headers and HSTS.

**Auth context values are unvalidated:**
- Files: `backend/internal/middleware/auth.go` sets `user_id`, `user_role` and `permissions` using string keys in the echo context.
- Recommendations: Use typed accessors, and enforce authorization (role/permission checks) when routes are added.

**DB SSL mode is configurable with no production guard:**
- Files: `backend/internal/database/database.go`, `backend/internal/database/migrator.go`. The DSN is built with `fmt.Sprintf` and no URL-escaping of the password.
- Recommendations: Use `url.URL`/`url.UserPassword` to build the DSN so special characters in passwords do not break it, and reject `sslmode=disable` when env is production.

**Secrets:**
- `backend/.env.sample` exists as a template. Secrets (Clerk key, Redis, DB, integration keys) come from env via koanf in `backend/internal/config/config.go`. Keep real `.env` files out of git (confirm the `.gitignore` entries).

## Performance Bottlenecks

**Redis client has no pool or timeout tuning:**
- Files: `backend/internal/server/server.go` (`redis.Options{Addr}` only, no password, TLS or DB selection).
- Improvement path: Add auth/TLS options and timeouts via `RedisConfig`.

**Request logger and tracing run on every request including health checks:**
- Files: `backend/internal/middleware/global.go`, `backend/internal/middleware/tracing.go`
- Improvement path: Skip health endpoints in logging and rate limiting.

## Fragile Areas

**Middleware ordering:**
- Files: `backend/internal/router/router.go`. `Recover()` is registered last, so panics in earlier middleware (rate limiter, CORS, tracing, request ID) are not recovered.
- Safe modification: Put `Recover` first, and keep `RequestID` before the logger.

**Hard dependency of Server on New Relic and Clerk:**
- Files: `backend/internal/logger/logger.go`, `backend/internal/config/observability.go`. The config requires many integration values (`validate:"required"`), so local startup fails without them.
- Safe modification: Make the observability and integration config optional in non-production.

**Global Clerk key:**
- Files: `backend/internal/service/auth.go` calls `clerk.SetKey` (process-global state) inside a constructor.

**Panic on config load:**
- Files: `backend/cmd/sentinel/main.go` uses `panic` instead of a structured fatal log, before the logger exists.

## Scaling Limits

**Single-process job and HTTP server:**
- Files: `backend/internal/lib/job/job.go`. The Asynq worker is embedded in the API process, so workers cannot scale independently of the API.
- Scaling path: Split worker into a separate `cmd/` entrypoint.

## Dependencies at Risk

**Module path / ownership:**
- Imports use `github.com/6sLOGAN78/sentinel`. Import blocks in several files are not gofmt/goimports ordered (for example `server.go` and `router.go`). Verify with `.golangci.yml`.

**Dual JS lockfiles:**
- `bun.lock`, `package-lock.json` and `packages/emails/bun.lock` coexist at the root and in packages.
- Migration plan: Standardize on one package manager.

## Missing Critical Features

**No readiness/liveness depth beyond the base health handler, no graceful drain of the job queue ordering, and no CI pipeline:**
- Files: `backend/internal/handler/health.go`. No `.github/` workflows detected.
- Blocks: Automated lint, test and build validation.

## Test Coverage Gaps

**No tests at all:**
- What's not tested: Everything. There are zero `*_test.go` files, although test helpers exist in `backend/internal/testing/` (`container.go`, `server.go`, `transaction.go`, `assertions.go`, `helpers.go`).
- Files: `backend/internal/sqlerr/handler.go`, `backend/internal/errs/`, `backend/internal/middleware/global.go` (error handler mapping), `backend/internal/validation/utils.go`, `backend/internal/config/config.go`.
- Risk: Error mapping, auth middleware and config validation can regress unnoticed.
- Priority: High

**JS packages untested:**
- `packages/zod/src/`, `packages/openapi/src/`, `packages/emails/src/` have no tests.
- Priority: Low

---

*Concerns audit: 2026-10-10*
