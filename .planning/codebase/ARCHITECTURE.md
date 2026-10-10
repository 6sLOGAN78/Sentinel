<!-- refreshed: 2026-10-10 -->
# Architecture

**Analysis Date:** 2026-10-10

## System Overview

```text
┌─────────────────────────────────────────────────────────────┐
│                  HTTP (Echo v4) `backend/internal/router`    │
│  middleware chain -> system routes (/status,/static,/docs)   │
│  `/api/v1` group (currently empty)                           │
├──────────────────┬──────────────────┬───────────────────────┤
│  Handlers        │  Middleware      │  Error handler        │
│ `internal/handler`│`internal/middleware`│ `internal/errs`     │
└────────┬─────────┴────────┬─────────┴──────────┬────────────┘
         ▼                  ▼                     ▼
┌─────────────────────────────────────────────────────────────┐
│ Services `internal/service` -> Repositories `internal/repository` │
└─────────────────────────────────────────────────────────────┘
         ▼
┌─────────────────────────────────────────────────────────────┐
│ `internal/server.Server` (shared deps): Postgres (pgx), Redis,│
│ asynq Job service, zerolog + New Relic, Config                │
└─────────────────────────────────────────────────────────────┘
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

**Overall:** Layered clean architecture in a Turborepo/bun monorepo (Go backend + TS packages), with manual constructor-based dependency injection.

**Key Characteristics:**
- `*server.Server` is the dependency container passed into every layer constructor.
- Aggregator structs (`Handlers`, `Services`, `Repositories`, `Middlewares`) built by `NewX(s, ...)`.
- Generic typed handler wrappers add validation, logging, and New Relic tracing uniformly.

## Layers

**Handler:** `backend/internal/handler`; depends on server, service, middleware, validation. Embeds `Handler` base.
**Service:** `backend/internal/service`; depends on server, repository, job.
**Repository:** `backend/internal/repository`; depends on server (DB).
**Infrastructure:** `backend/internal/server`, `database`, `logger`, `lib/*`.

## Data Flow

### Primary Request Path

1. Echo receives request; global middleware chain runs (rate limit, CORS, secure, request ID, New Relic, context enhancer, request logger, recover) (`backend/internal/router/router.go`)
2. Route handler wrapped with `handler.Handle/HandleFile/HandleNoContent` binds and validates request (`backend/internal/handler/base.go` `handleRequest`)
3. Handler calls service -> repository -> `Server.DB`
4. `ResponseHandler` writes JSON/no-content/file; errors go to `GlobalErrorHandler` (`backend/internal/middleware/global.go`)

### Background Jobs

1. Service enqueues via `Server.Job.Client` (asynq, Redis)
2. Worker started in `server.New` via `jobService.Start()` processes queues critical(6)/default(3)/low(1) (`backend/internal/lib/job/job.go`, `email_tasks.go`, `handlers.go`)

**State Management:** Stateless HTTP; state in Postgres and Redis.

## Key Abstractions

- `server.Server`: dependency container (`backend/internal/server/server.go`)
- `HandlerFunc[Req, Res]` / `ResponseHandler`: typed handlers (`backend/internal/handler/base.go`)
- `errs.HTTPError`: API error shape with Code/Message/Status/Override/Errors/Action (`backend/internal/errs/`)

## Entry Points

**Backend binary:** `backend/cmd/sentinel/main.go` - load config, logger, migrate (when env != "local"), build server, repos, services, handlers, router, serve; SIGINT triggers 30s graceful shutdown.

## Architectural Constraints

- **Threading:** net/http goroutine per request; asynq worker (concurrency 10) in same process.
- **Global state:** none significant; deps passed via `Server`.
- **Redis:** startup tolerates failed ping (logs error), but `jobService.Start()` errors abort startup.
- **Import direction:** router/handler/service/middleware import `server`; `server` must not import them.
- Config is read from env (`.env` via godotenv autoload); never read `backend/.env`.

## Anti-Patterns

### Bypassing handler wrappers
**What happens:** `HealthHandler.CheckHealth` and OpenAPI handler use raw `echo.HandlerFunc`.
**Why it's wrong:** Skips unified validation/tracing for new endpoints.
**Do this instead:** Use `handler.Handle(...)` from `backend/internal/handler/base.go` for API routes.

## Error Handling

**Strategy:** Return `*errs.HTTPError` (constructors in `backend/internal/errs/types.go`); central `GlobalErrorHandler` formats responses. DB errors go through `backend/internal/sqlerr`.

## Cross-Cutting Concerns

**Logging:** zerolog with New Relic log forwarding (`backend/internal/logger/logger.go`); context logger via `middleware.GetLogger(c)`.
**Validation:** go-playground/validator via `validation.Validatable` and `BindAndValidate`.
**Authentication:** Clerk (`backend/internal/middleware/auth.go` `RequireAuth`); not yet applied to any route.

---

*Architecture analysis: 2026-10-10*
