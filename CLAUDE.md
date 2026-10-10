<!-- GSD:project-start source:PROJECT.md -->
## Project

**Sentinel — Real-Time Threat Detection Platform**

Sentinel is a distributed security event platform for ingestion, detection, investigation and alerting. It collects network and identity security events from simulated sources (and optionally from Aegis). Events are validated and streamed through Kafka, then checked against deterministic threat-detection rules. Event history is stored in ClickHouse for search, and an authenticated React dashboard shows real-time alerts. It serves three personas: security analysts triaging alerts, platform operators watching pipeline health, and developers/testers driving synthetic traffic. The project exists to demonstrate production-quality backend, distributed-systems, streaming-data and security engineering.

**Core Value:** The end-to-end flow must work and be provably correct under failure: generator → ingest → Kafka → detector → ClickHouse/PostgreSQL → API → live dashboard. It must produce deterministic, deduplicated, evidence-backed alerts and recover from worker crashes without losing acknowledged events. Every performance claim must be backed by a reproducible benchmark.

### Constraints

- **Tech stack:**
  - Go for ingest, detector, writers and query API, as a single Go module with multiple binaries.
  - Apache Kafka in KRaft mode, ClickHouse, PostgreSQL (alert workflow state and audit) and Redis (rate limiting).
  - Python for the generator and benchmark reports; React + TypeScript + Vite for the dashboard.
  - Prometheus, Grafana and OpenTelemetry for observability; GitHub Actions for CI; Docker Compose and Kubernetes for deployment.
  - Mandated by spec §2.
- **Delivery semantics:** at-least-once. Commit Kafka offsets only after downstream effects succeed, and make every consumer and store idempotent. This is the spec §3 key decision.
- **Event-time correctness:** all rule windows use `occurred_at`, with bounded per-key state and a documented lateness tolerance (~30s). Late-event handling must be deterministic. Spec §5.1.
- **Security:**
  - Tenant is resolved from the verified identity, never only from the request.
  - viewer/analyst/admin roles are enforced server-side.
  - API keys are hashed at rest, all SQL is parameterized, and no secrets are committed.
  - Spec §10.
- **Bounded resources:** no unbounded buffering. Shed load with 429/503 and cap producer queues, ClickHouse batches and per-key state. Spec §11.1.
- **Metrics hygiene:** no high-cardinality labels (IP, user, event ID, request ID, tenant) in Prometheus. Spec §11.2.
- **Definition of Done:** a phase is done only when its workflow completes end-to-end, tests pass (success, invalid input and failure cases), CI is green, and there are no stubs, mock-only adapters or invented numbers. Spec §18.
- **Pinned versions:** use current stable tool versions, pinned in lockfiles and container images. Spec §2.
<!-- GSD:project-end -->

<!-- GSD:stack-start source:codebase/STACK.md -->
## Technology Stack

## Languages
- Go 1.25.5 - Backend API, background jobs (`backend/`, module `github.com/6sLOGAN78/sentinel`)
- TypeScript (^5.8 in packages, ^6.0.3 at root) - Shared Zod schemas, OpenAPI generation, email templates (`packages/`)
- TSX/React 19 - Email templates (`packages/emails/src/templates/welcome.tsx`)
- SQL - Postgres migrations (`backend/internal/database/migrations/001_setup.sql`)
## Runtime
- Go runtime for backend (`backend/cmd/sentinel/main.go`)
- Node >=22 (`package.json` engines)
- Bun 1.2.13 (`packageManager` in `package.json`); workspaces `apps/*`, `packages/*`
- Go modules (`backend/go.mod`)
- Lockfiles: `bun.lock`, `package-lock.json` (both at root), `packages/emails/bun.lock`, `backend/go.sum`
- Note: `apps/` contains only `backend/templates` (exported email HTML target); `package.json` workspaces reference `apps/*`.
## Frameworks
- Echo v4.15.2 (`github.com/labstack/echo/v4`) - HTTP server/router (`backend/internal/router/`, `backend/internal/server/server.go`)
- Asynq v0.26.0 - Redis-backed job queue (`backend/internal/lib/job/`)
- Turborepo ^2.9.6 - JS monorepo task orchestration (`turbo.json`)
- ts-rest ^3.52 (`@ts-rest/core`, `@ts-rest/open-api`) + `@anatine/zod-openapi` - API contracts and OpenAPI generation (`packages/openapi/`)
- React Email (`react-email` ^6.3.3, `@react-email/components` 0.0.34) - Email template authoring (`packages/emails/`)
- testify v1.11.1 - assertions (`backend/internal/testing/assertions.go`)
- testcontainers-go v0.42.0 - Postgres container (`postgres:15-alpine`) (`backend/internal/testing/container.go`)
- Task (`backend/taskfile.yml`) - `task run`, `task migrations:new`, `task migrations:up`, `task tidy`
- tern v2 (`github.com/jackc/tern/v2`) - DB migrations (`backend/internal/database/migrator.go`)
- golangci-lint v2 config (`backend/.golangci.yml`)
- tsc + tsc-alias, tsx, concurrently, wait-on - TS package builds (`packages/*/package.json`)
## Key Dependencies
- `github.com/jackc/pgx/v5` v5.9.2 - Postgres driver/pool
- `github.com/clerk/clerk-sdk-go/v2` v2.6.0 - Authentication
- `github.com/redis/go-redis/v9` v9.14.1 - Redis client
- `github.com/resend/resend-go/v2` v2.28.0 - Transactional email
- `github.com/knadh/koanf/v2` v2.3.4 + `providers/env` - Config loading (`backend/internal/config/config.go`)
- `github.com/go-playground/validator/v10` v10.30.2 - Config/request validation
- `github.com/rs/zerolog` v1.35.1 + `pgx-zerolog` - Logging (`backend/internal/logger/logger.go`)
- `github.com/newrelic/go-agent/v3` v3.43.3 (+ nrecho-v4, nrpgx5, nrredis-v9, nrpkgerrors) - APM
- `zod` ^3.24.2 - Schema definitions (`packages/zod/`)
- `golang.org/x/time` - rate limiting (`backend/internal/middleware/rate_limit.go`)
- `github.com/joho/godotenv` - autoloads `.env`
- `github.com/pkg/errors`, `github.com/google/uuid`
## Configuration
- Env vars prefixed `SENTINEL_`, nested with `.` (e.g. `SENTINEL_DATABASE.HOST`), parsed by koanf into `Config` and validated (`backend/internal/config/config.go`, `backend/internal/config/observability.go`)
- `backend/.env` present (not read); `backend/.env.sample` exists but is empty
- Key groups: `PRIMARY.ENV`, `SERVER.*`, `DATABASE.*`, `AUTH.SECRET_KEY`, `INTEGRATION.RESEND_API_KEY`, `REDIS.ADDRESS`, `OBSERVABILITY.*` (logging, new_relic, health_checks)
- `turbo.json`, root `package.json`, `packages/*/tsconfig.json`, `backend/taskfile.yml`, `backend/.golangci.yml`, `.vscode/settings.json`
## Platform Requirements
- Go 1.25+, Bun 1.2.13, Node 22+, Task, tern, PostgreSQL, Redis, Docker (for testcontainers)
- Not defined (no Dockerfile or CI config detected). Backend reads `templates/emails/*.html` and `static/openapi.*` from working directory at runtime.
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

## Naming Patterns
- Lowercase, single-word or snake_case: `rate_limit.go`, `request_id.go`, `email_tasks.go`.
- Plural aggregator files hold the container struct: `handlers.go`, `services.go`, `repositories.go`, `middlewares.go`.
- `base.go` holds the shared base type for a package (`internal/handler/base.go`, `internal/model/base.go`).
- Lowercase: `health.ts`, `index.ts`, `utils.ts`. Barrel `index.ts` per package/folder.
- Constructors: `NewXxx(s *server.Server)` returning a pointer (`NewHealthHandler`, `NewAuthService`, `NewAuthMiddleware`).
- Handlers/services/middleware are structs with unexported `server *server.Server` field (services/middleware) or embedded `Handler` (handlers).
- Handler methods are exported PascalCase verbs: `CheckHealth`, `RequireAuth`.
- Error constructors in `internal/errs/types.go`: `NewUnauthorizedError`, `NewBadRequestError`, etc.
- Package names are short lowercase (`errs`, `sqlerr`, `lib/email`).
- Zod schemas prefixed `Z`: `ZHealthResponse`, `ZHealthCheck` (`packages/zod/src/health.ts`).
- ts-rest contracts suffixed `Contract`: `healthContract` (`packages/openapi/src/contracts/health.ts`).
- Package scope `@sentinel/*` (e.g. `@sentinel/zod`); internal alias `@/utils.js` (ESM, `.js` extension in imports).
## Code Style
- Go: `gofmt`/`goimports` (enforced via `goimports` linter in `backend/.golangci.yml`). Some existing files (`internal/errs/types.go`, `internal/testing/container.go`) have formatting drift; run `gofmt` on touched files.
- TS: 2-space indent, double quotes, semicolons, trailing commas. Turbo scripts `format:check` / `format:fix` exist at root.
- Go: golangci-lint v2 config at `backend/.golangci.yml`, `default: none` with a long explicit enable list. Key limits: `funlen` 100 lines/50 statements, `cyclop` 30, `gocognit` 20, `lll`, `mnd` (no magic numbers), `gochecknoglobals`, `gochecknoinits`, `nonamedreturns`, `errorlint`, `govet` enable-all with strict shadow (no `fieldalignment`), `errcheck` with `check-type-assertions: true`, `testpackage`, `tparallel`, `testifylint`, `sloglint`, `forbidigo`.
- `nolint` directives require a specific linter name and an explanation (`nolintlint`), except for `funlen`, `gocognit`, `lll`.
- TS: root scripts `lint`, `lint:fix`, `typecheck` via Turbo; TypeScript ^6, strict via per-package `tsconfig.json`.
## Import Organization
## Error Handling
- Return errors up; wrap with `fmt.Errorf("...: %w", err)` (`internal/handler/health.go`). Use `errors.As` for typed errors (`internal/sqlerr/handler.go`).
- HTTP errors use `*errs.HTTPError` built through `errs.NewXxxError(message, override, ...)` in `internal/errs/types.go`; codes are produced by `MakeUpperCaseWithUnderscores(http.StatusText(...))`. Optional code/field errors passed as pointers/slices.
- Postgres errors are converted to a custom type via `sqlerr.ConvertPgError`; query codes with `sqlerr.ErrCode(err)`.
- Handlers `return` errors to Echo's central error handler (`internal/middleware/global.go`) rather than writing responses ad hoc (the auth failure handler in `internal/middleware/auth.go` is the exception because it runs outside Echo).
## Logging
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
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

## System Overview
```text
```
## Component Responsibilities
| Component | Responsibility | File |
|-----------|----------------|------|
| main | Wiring + graceful shutdown | `backend/cmd/sentinel/main.go` |
| Config | koanf env loading + validation | `backend/internal/config/config.go`, `observability.go` |
| Server | Holds shared deps, owns http.Server lifecycle | `backend/internal/server/server.go` |
| Database | pgx pool, tern migrations | `backend/internal/database/database.go`, `migrator.go` |
| Router | Echo setup, global middleware, route registration | `backend/internal/router/router.go`, `system.go` |
| Handlers | HTTP endpoints; generic `Handle*` wrappers | `backend/internal/handler/base.go`, `health.go`, `openapi.go`, `handlers.go` |
| Middleware | Auth (Clerk), tracing, request ID, rate limit, context | `backend/internal/middleware/` |
| Services | Business logic | `backend/internal/service/services.go`, `auth.go` |
| Repositories | Data access (empty scaffold) | `backend/internal/repository/repositories.go` |
| Jobs | asynq client/server, email tasks | `backend/internal/lib/job/` |
| Email | Email client + HTML templates | `backend/internal/lib/email/`, `backend/templates/emails/` |
| Errors | Typed HTTPError constructors | `backend/internal/errs/` |
| SQL errors | Postgres error mapping | `backend/internal/sqlerr/` |
| Validation | Bind + validate requests | `backend/internal/validation/utils.go` |
| Shared TS packages | zod schemas, OpenAPI contracts, React email templates | `packages/zod`, `packages/openapi`, `packages/emails` |
## Pattern Overview
- `*server.Server` is the dependency container passed into every layer constructor.
- Aggregator structs (`Handlers`, `Services`, `Repositories`, `Middlewares`) built by `NewX(s, ...)`.
- Generic typed handler wrappers add validation, logging, and New Relic tracing uniformly.
## Layers
## Data Flow
### Primary Request Path
### Background Jobs
## Key Abstractions
- `server.Server`: dependency container (`backend/internal/server/server.go`)
- `HandlerFunc[Req, Res]` / `ResponseHandler`: typed handlers (`backend/internal/handler/base.go`)
- `errs.HTTPError`: API error shape with Code/Message/Status/Override/Errors/Action (`backend/internal/errs/`)
## Entry Points
## Architectural Constraints
- **Threading:** net/http goroutine per request; asynq worker (concurrency 10) in same process.
- **Global state:** none significant; deps passed via `Server`.
- **Redis:** startup tolerates failed ping (logs error), but `jobService.Start()` errors abort startup.
- **Import direction:** router/handler/service/middleware import `server`; `server` must not import them.
- Config is read from env (`.env` via godotenv autoload); never read `backend/.env`.
## Anti-Patterns
### Bypassing handler wrappers
## Error Handling
## Cross-Cutting Concerns
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->
## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, `.github/skills/`, or `.codex/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->



<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
