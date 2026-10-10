# Technology Stack

**Analysis Date:** 2026-10-10

## Languages

**Primary:**
- Go 1.25.5 - Backend API, background jobs (`backend/`, module `github.com/6sLOGAN78/sentinel`)

**Secondary:**
- TypeScript (^5.8 in packages, ^6.0.3 at root) - Shared Zod schemas, OpenAPI generation, email templates (`packages/`)
- TSX/React 19 - Email templates (`packages/emails/src/templates/welcome.tsx`)
- SQL - Postgres migrations (`backend/internal/database/migrations/001_setup.sql`)

## Runtime

**Environment:**
- Go runtime for backend (`backend/cmd/sentinel/main.go`)
- Node >=22 (`package.json` engines)

**Package Manager:**
- Bun 1.2.13 (`packageManager` in `package.json`); workspaces `apps/*`, `packages/*`
- Go modules (`backend/go.mod`)
- Lockfiles: `bun.lock`, `package-lock.json` (both at root), `packages/emails/bun.lock`, `backend/go.sum`
- Note: `apps/` contains only `backend/templates` (exported email HTML target); `package.json` workspaces reference `apps/*`.

## Frameworks

**Core:**
- Echo v4.15.2 (`github.com/labstack/echo/v4`) - HTTP server/router (`backend/internal/router/`, `backend/internal/server/server.go`)
- Asynq v0.26.0 - Redis-backed job queue (`backend/internal/lib/job/`)
- Turborepo ^2.9.6 - JS monorepo task orchestration (`turbo.json`)
- ts-rest ^3.52 (`@ts-rest/core`, `@ts-rest/open-api`) + `@anatine/zod-openapi` - API contracts and OpenAPI generation (`packages/openapi/`)
- React Email (`react-email` ^6.3.3, `@react-email/components` 0.0.34) - Email template authoring (`packages/emails/`)

**Testing:**
- testify v1.11.1 - assertions (`backend/internal/testing/assertions.go`)
- testcontainers-go v0.42.0 - Postgres container (`postgres:15-alpine`) (`backend/internal/testing/container.go`)

**Build/Dev:**
- Task (`backend/taskfile.yml`) - `task run`, `task migrations:new`, `task migrations:up`, `task tidy`
- tern v2 (`github.com/jackc/tern/v2`) - DB migrations (`backend/internal/database/migrator.go`)
- golangci-lint v2 config (`backend/.golangci.yml`)
- tsc + tsc-alias, tsx, concurrently, wait-on - TS package builds (`packages/*/package.json`)

## Key Dependencies

**Critical:**
- `github.com/jackc/pgx/v5` v5.9.2 - Postgres driver/pool
- `github.com/clerk/clerk-sdk-go/v2` v2.6.0 - Authentication
- `github.com/redis/go-redis/v9` v9.14.1 - Redis client
- `github.com/resend/resend-go/v2` v2.28.0 - Transactional email
- `github.com/knadh/koanf/v2` v2.3.4 + `providers/env` - Config loading (`backend/internal/config/config.go`)
- `github.com/go-playground/validator/v10` v10.30.2 - Config/request validation
- `github.com/rs/zerolog` v1.35.1 + `pgx-zerolog` - Logging (`backend/internal/logger/logger.go`)
- `github.com/newrelic/go-agent/v3` v3.43.3 (+ nrecho-v4, nrpgx5, nrredis-v9, nrpkgerrors) - APM
- `zod` ^3.24.2 - Schema definitions (`packages/zod/`)

**Infrastructure:**
- `golang.org/x/time` - rate limiting (`backend/internal/middleware/rate_limit.go`)
- `github.com/joho/godotenv` - autoloads `.env`
- `github.com/pkg/errors`, `github.com/google/uuid`

## Configuration

**Environment:**
- Env vars prefixed `SENTINEL_`, nested with `.` (e.g. `SENTINEL_DATABASE.HOST`), parsed by koanf into `Config` and validated (`backend/internal/config/config.go`, `backend/internal/config/observability.go`)
- `backend/.env` present (not read); `backend/.env.sample` exists but is empty
- Key groups: `PRIMARY.ENV`, `SERVER.*`, `DATABASE.*`, `AUTH.SECRET_KEY`, `INTEGRATION.RESEND_API_KEY`, `REDIS.ADDRESS`, `OBSERVABILITY.*` (logging, new_relic, health_checks)

**Build:**
- `turbo.json`, root `package.json`, `packages/*/tsconfig.json`, `backend/taskfile.yml`, `backend/.golangci.yml`, `.vscode/settings.json`

## Platform Requirements

**Development:**
- Go 1.25+, Bun 1.2.13, Node 22+, Task, tern, PostgreSQL, Redis, Docker (for testcontainers)

**Production:**
- Not defined (no Dockerfile or CI config detected). Backend reads `templates/emails/*.html` and `static/openapi.*` from working directory at runtime.

---

*Stack analysis: 2026-10-10*
