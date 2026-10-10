# Codebase Structure

**Analysis Date:** 2026-10-10

## Directory Layout

```
Sentinel/
├── backend/                 # Go service (module github.com/6sLOGAN78/sentinel)
│   ├── cmd/sentinel/        # main.go entry point
│   ├── internal/
│   │   ├── config/          # koanf config structs + loading
│   │   ├── database/        # pgx pool, migrator, migrations/*.sql
│   │   ├── errs/            # HTTPError types + constructors
│   │   ├── handler/         # HTTP handlers, generic wrappers
│   │   ├── lib/{job,email,utils}/ # asynq jobs, email sending, helpers
│   │   ├── logger/          # zerolog + New Relic
│   │   ├── middleware/      # echo middlewares
│   │   ├── model/           # domain models (base.go)
│   │   ├── repository/      # data access
│   │   ├── router/          # echo router + route registration
│   │   ├── server/          # Server dependency container
│   │   ├── service/         # business logic
│   │   ├── sqlerr/          # postgres error mapping
│   │   ├── testing/         # test helpers (container, server, tx)
│   │   └── validation/      # bind + validate
│   ├── static/              # openapi.json / openapi.html
│   ├── templates/emails/    # rendered email HTML (welcome.html)
│   ├── taskfile.yml         # task runner (run, migrations:*)
│   └── .golangci.yml        # lint config
├── apps/backend/templates/emails/ # email template output dir
├── packages/
│   ├── zod/src/             # shared zod schemas (health.ts)
│   ├── openapi/src/         # contracts/, gen.ts (OpenAPI generation)
│   └── emails/src/templates/# React email templates (welcome.tsx)
├── package.json, turbo.json, bun.lock # monorepo (workspaces apps/*, packages/*)
└── .planning/               # GSD docs
```

## Key File Locations

**Entry Points:** `backend/cmd/sentinel/main.go`
**Configuration:** `backend/internal/config/config.go`, `backend/.env.sample` (`backend/.env` exists, do not read), `turbo.json`
**Core Logic:** `backend/internal/service/`, `backend/internal/handler/`, `backend/internal/router/router.go`
**Migrations:** `backend/internal/database/migrations/001_setup.sql` (tern)
**Testing:** `backend/internal/testing/` (helpers only; no `_test.go` files detected)

## Naming Conventions

**Files:** lowercase snake_case Go files by concern (`rate_limit.go`, `email_tasks.go`); aggregator file named plural (`handlers.go`, `services.go`, `repositories.go`, `middlewares.go`).
**Directories:** lowercase single-word Go packages.
**Types:** `XHandler`, `XService`, `XMiddleware`; constructors `NewX(s *server.Server, ...)`.
**TS:** lowercase files, `index.ts` barrels per package.

## Where to Add New Code

**New API endpoint:** handler in `backend/internal/handler/<name>.go`, add field to `Handlers` in `handlers.go`; register in `backend/internal/router/` under the `/api/v1` group (create `<name>.go` with a `register...Routes` function); wrap with `handler.Handle`.
**New business logic:** `backend/internal/service/<name>.go`, add to `Services` in `services.go`.
**New data access:** `backend/internal/repository/<name>.go`, add to `Repositories`.
**New migration:** `task migrations:new name=<name>` in `backend/` (writes to `internal/database/migrations`).
**New background task:** `backend/internal/lib/job/<name>_tasks.go`, register in `handlers.go`.
**New email:** React template in `packages/emails/src/templates/`, HTML in `backend/templates/emails/`.
**Shared API schemas/contracts:** `packages/zod/src`, `packages/openapi/src/contracts`.
**Utilities:** `backend/internal/lib/utils/utils.go`.

## Special Directories

**`backend/static/`:** OpenAPI docs served at `/static` and `/docs`; generated from `packages/openapi`; committed.
**`node_modules/`:** generated, ignored.
**`.serena/`:** tooling, untracked.

---

*Structure analysis: 2026-10-10*
