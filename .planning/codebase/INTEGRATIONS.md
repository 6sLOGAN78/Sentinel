# External Integrations

**Analysis Date:** 2026-10-10

## APIs & External Services

**Email:**
- Resend - transactional email
  - SDK/Client: `github.com/resend/resend-go/v2` (`backend/internal/lib/email/client.go`)
  - Auth: `SENTINEL_INTEGRATION.RESEND_API_KEY`
  - Renders Go `html/template` files from `templates/emails/` (generated from `packages/emails` via `bun run export`); sender currently hardcoded `onboarding@resend.dev`
  - Sending is queued via Asynq tasks (`backend/internal/lib/job/email_tasks.go`, `handlers.go`)

**API Docs:**
- Scalar API Reference loaded from CDN `https://cdn.jsdelivr.net/npm/@scalar/api-reference` in `backend/static/openapi.html`, served at `/docs` (`backend/internal/router/system.go`, `backend/internal/handler/openapi.go`)

## Data Storage

**Databases:**
- PostgreSQL (tests use `postgres:15-alpine`)
  - Connection: `SENTINEL_DATABASE.*` (host, port, user, password, name, ssl_mode, pool settings)
  - Client: `pgx/v5` pool with New Relic `nrpgx5` and `pgx-zerolog` tracing (`backend/internal/database/database.go`)
  - Migrations: tern, `backend/internal/database/migrations/001_setup.sql`, run via `task migrations:up` (needs `SENTINEL_DB_DSN`) or `backend/internal/database/migrator.go`

**File Storage:**
- Local filesystem only

**Caching / Queue backing:**
- Redis via `SENTINEL_REDIS.ADDRESS`; used by Asynq (queues, concurrency 10) in `backend/internal/lib/job/job.go` and go-redis with `nrredis-v9` instrumentation

## Authentication & Identity

**Auth Provider:**
- Clerk
  - Implementation: `clerkhttp.WithHeaderAuthorization` middleware validating bearer session tokens, claims read via `clerk.SessionClaimsFromContext` (`backend/internal/middleware/auth.go`); key set with `clerk.SetKey` (`backend/internal/service/auth.go`)
  - Auth: `SENTINEL_AUTH.SECRET_KEY` (Clerk secret key)
  - User id/role propagated into request context (`backend/internal/middleware/context.go`)

## Monitoring & Observability

**Error Tracking / APM:**
- New Relic Go agent (`backend/internal/middleware/tracing.go`, `backend/internal/logger/logger.go`)
  - Auth: `SENTINEL_OBSERVABILITY.NEW_RELIC.LICENSE_KEY`; flags for log forwarding, distributed tracing, debug logging

**Logs:**
- zerolog structured logging (json/console), level via `SENTINEL_OBSERVABILITY.LOGGING.*`, slow-query threshold; request IDs (`backend/internal/middleware/request_id.go`)
- Health checks: `GET /status` (`backend/internal/handler/health.go`), configured by `OBSERVABILITY.HEALTH_CHECKS.*`

## CI/CD & Deployment

**Hosting:**
- Not detected

**CI Pipeline:**
- None detected (no `.github/` directory)

## Environment Configuration

**Required env vars (all prefixed `SENTINEL_`):**
- `PRIMARY.ENV`, `SERVER.PORT`, `SERVER.READ_TIMEOUT`, `SERVER.WRITE_TIMEOUT`, `SERVER.IDLE_TIMEOUT`, `SERVER.CORS_ALLOWED_ORIGINS`
- `DATABASE.HOST/PORT/USER/PASSWORD/NAME/SSL_MODE/MAX_OPEN_CONNS/MAX_IDLE_CONNS/CONN_MAX_LIFETIME/CONN_MAX_IDLE_TIME`
- `AUTH.SECRET_KEY`, `INTEGRATION.RESEND_API_KEY`, `REDIS.ADDRESS`
- `OBSERVABILITY.*` (optional block; service name, environment, logging, new_relic, health_checks)

**Secrets location:**
- `backend/.env` (present, gitignored per `backend/.gitignore`; contents not inspected). `backend/.env.sample` is empty.

## Webhooks & Callbacks

**Incoming:**
- None (routes: `/status`, `/docs`, `/api/v1` group in `backend/internal/router/`)

**Outgoing:**
- None

---

*Integration audit: 2026-10-10*
