# Phase 1: Foundations & Contracts - Research

**Researched:** 2026-10-10
**Domain:** Repo restructuring (Go monorepo module), local infra (Compose: Kafka KRaft, ClickHouse, Postgres, Redis), JSON Schema / OpenAPI 3.1 contracts, tooling pinning, CI skeleton
**Confidence:** HIGH. Almost every command and version below was executed in a scratchpad on 2026-10-10 (Docker 29.1.3, Compose v5.0.0, Go 1.26.9 via GOTOOLCHAIN, Node 22.23.3, uv 0.9.18). Items not run are labelled MEDIUM, LOW or [ASSUMED].

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
D-01..D-27 are locked (see `01-CONTEXT.md`). Verbatim digest of the ones that bound this research:
- **D-01/D-02:** Single Go module at `backend/` (`github.com/6sLOGAN78/sentinel`) bumped to Go 1.26.x. Binaries in `backend/cmd/{ingest,detector,event-writer,alert-writer,query-api}/main.go`; `backend/cmd/sentinel` deleted. Non-Go assets at repo root (`api/openapi.yaml`, `config/rules.yaml`, `web/`, `generator/`, `deployments/compose/docker-compose.yml`, `observability/`, `docs/{architecture.md,decisions/}`, `benchmarks/`, `Makefile`, `.env.example`, `.github/workflows/ci.yml`). Go-embedded assets inside the module: JSON schemas in `backend/internal/contracts/schema/`, SQL migrations in `backend/migrations/{postgres,clickhouse}/`. Go integration/contract tests in `backend/test/` behind build tags.
- **D-03:** Remove completely: Clerk, asynq, Resend/email/templates/`apps/`/`packages/emails`, New Relic + all nr* integrations, `pkg/errors`, `packages/zod`, `packages/openapi`, `backend/static/openapi.*`, root `package.json`/`turbo.json`/`bun.lock`/`package-lock.json`/`node_modules`, `backend/taskfile.yml`, in-memory rate limiter, `internal/lib/utils`, empty `001_setup.sql`, `backend/.env.sample`.
- **D-04:** Keep then refactor: `internal/config`, `internal/logger`, `internal/errs` (envelope per spec §8), `internal/validation`, `internal/database`, `internal/middleware/{request_id,context,global}.go`, `internal/sqlerr`, `internal/testing`. `server.Server` must not be imported by worker binaries; split into `config.Load`, `logger.New`, `database.New`, `redisx.New`, plus an HTTP helper only for ingest and query-api.
- **D-05:** Fix defects with a test each: Recover registered first; `Shutdown` nil-guards http server and closes Redis; Redis ping failure fatal at startup; Postgres DSN built via `url.URL`/`pgx.ParseConfig`; `godotenv` only when `SENTINEL_PRIMARY.ENV=local`; error paths close opened resources.
- **D-06:** Every binary: load+validate config, init logger, connect/validate deps, block until SIGINT/SIGTERM, graceful shutdown. No routes, no stub endpoints.
- **D-07..D-11:** Compose pins (`apache/kafka:4.3.1`, ClickHouse 26.3 LTS, `postgres:18.6`, `redis:8.10.2`), healthchecks, named volumes; Kafka dual listeners (`kafka:9092` / `localhost:29092`), fixed `CLUSTER_ID`, auto-create off, RF=1 overrides; one-shot `topic-init` (events 6p/24h, alerts 3p/7d, dlq 1p/7d, env-configurable); Make targets `dev-up`, `migrate`, `test`, `test-integ`, `lint`, `dev-down`, `reset` (needs `CONFIRM=1`), `seed`/`bench` exit non-zero; tern for Postgres (`schema_version`), in-repo Go runner for ClickHouse (`schema_migrations`, `IF NOT EXISTS`, database only in Phase 1).
- **D-12..D-16:** JSON Schema 2020-12 embedded via `go:embed`, hand-written Go structs mirroring them, contract test, jsonschema/v6 in tests only; event/alert field rules; client-supplied UUID `event_id`; single `contracts.DedupeKey(tenant, ruleID, entityKey, windowStart)` -> length-prefixed -> lowercase SHA-256 hex, fuzz-tested.
- **D-17/D-18:** Hand-authored `api/openapi.yaml` (3.1) covering every spec §8 endpoint; CI validates with a Go test (libopenapi, or kin-openapi if 3.1 support is good); openapi-typescript wired in `web/` but only needs to compile; no server codegen.
- **D-19/D-20:** `web/` Vite + React 19 + TS ~6.0 with its own npm `package-lock.json`, ESLint, Prettier, Vitest, one placeholder route; `generator/` Python 3.13 uv project, ruff, mypy, pytest, `requirements.txt` export.
- **D-21..D-23:** Pin image tags (+digests), commit lockfiles, pin Go tools; placeholder-only `.env.example`; gitleaks in CI; CI jobs fail on non-zero (no `|| true`).
- **D-24..D-27:** Six ADRs under `docs/decisions/` (MADR, `0001-...`), including atomic-batch semantics, metrics-label policy, Redis fail-closed default.

### Claude's Discretion
- Internal package names beyond those listed (`redisx`, `kafkax`), Makefile internals, Compose host port numbers (avoid collisions; document).
- golangci-lint rule selection (start from existing `backend/.golangci.yml`).
- Exact attribute size limits and skew defaults within stated bounds.
- Whether OpenAPI event/alert schemas are `$ref`s to the JSON Schema files or generated copies, provided a drift test exists.

### Deferred Ideas (OUT OF SCOPE)
- Hardened CI (required integration smoke gate, dependency scanning): Phase 6.
- `/healthz`, `/readyz`: Phase 2.
- Prometheus metrics and OTel wiring: Phase 5 (only the label-policy ADR lands now).
- Auth tables and producer API keys: Phase 2. Dashboard users/JWT: Phase 4.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| FND-01 | `make dev-up` gives healthy Kafka/ClickHouse/Postgres/Redis, explicit topics | Verified Compose file (Standard Stack -> Compose), healthchecks, idempotent topic-init (tested twice) |
| FND-02 | `make migrate/test/lint/dev-down/reset` documented, idempotent | Makefile pattern, tern via `go tool`, ClickHouse runner design, `reset` guard |
| FND-03 | One Go module, five binaries, boilerplate removed, defects fixed | Boilerplate Disposition + import-graph table, refactor list, defect fixes |
| FND-04 | Event/alert JSON Schema + Go types + canonical `alert_id` + contract tests | jsonschema/v6 usage (verified), DedupeKey design + fuzz (verified), fixtures list |
| FND-05 | `api/openapi.yaml` 3.1 validated in CI | kin-openapi v0.149.0 verified on 3.1, openapi-typescript compile verified |
| FND-06 | All versions pinned, `.env.example` placeholders only | Pin table with digests, tool directives, gitleaks CLI |
| FND-07 | ADRs in `docs/decisions/` | ADR list and content seeds |
</phase_requirements>

## Summary

All ten research flags were resolved by running the commands. The stack in `STACK.md` holds up with five corrections the planner must honour. (1) Use **kin-openapi v0.149.0** for the CI OpenAPI test; it loads and validates 3.1 documents (tested with `type: [string, "null"]`) and rejects dangling `$ref`s. libopenapi v0.41.6 also works but needs more glue and `go 1.26.0`; keep it as fallback only. (2) ClickHouse **`26.3.47.6`** exists on Docker Hub (the STACK.md note that it 404'd is stale), with digest resolved. (3) `openapi-typescript@7.13.0` declares `peerDependencies: typescript ^5.x`, so `npm install` with TypeScript ~6.0 fails with ERESOLVE unless an `overrides` entry is added; with the override the full Vite 8 / Vitest 5 / ESLint 10 / React 19.3 scaffold installs, typechecks, lints, tests and builds. (4) golangci-lint v2's existing config fails verification because `gosimple`, `typecheck` and `stylecheck` are no longer linters; remove those three lines. (5) The testcontainers-go Kafka module (v0.44.0) targets `confluentinc/confluent-local`, not `apache/kafka`; so Kafka integration tests should use a generic container (or Compose) with the same env block that was verified here.

The boilerplate is small (3,148 Go lines). New Relic is imported by 7 files, Clerk by 2, `pkg/errors` by 2, asynq by 3. `server.Server` is imported by 12 packages, so the D-04 split (explicit constructor deps instead of `*server.Server`) touches every retained middleware file and the `internal/testing` helpers.

**Primary recommendation:** Build in this order: (a) boilerplate deletion + module bump + five empty-but-running binaries, (b) Compose + Makefile + migrations, (c) contracts package (schemas, structs, DedupeKey, fuzz), (d) OpenAPI + Go validation test, (e) web/generator scaffolds, (f) CI + gitleaks + ADRs. Use `go tool -modfile=tools.mod` to keep golangci-lint's roughly 250 transitive modules out of the product `go.mod`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Local dependency stack | Infra (Compose) | Makefile | Reproducible bring-up, not application code |
| Topic provisioning | Infra (`topic-init` one-shot) | — | Auto-create is off, so provisioning is a deploy-time step, never app code |
| Postgres migrations | Backend (tern, embedded SQL) | Makefile `migrate` | Versioned SQL embedded in the module |
| ClickHouse migrations | Backend (in-repo runner) | Makefile `migrate` | Avoids golang-migrate/clickhouse-go compatibility risk |
| Event/alert contracts | Backend `internal/contracts` | `api/openapi.yaml` (drift test) | JSON Schema is the canonical contract; Go structs mirror it |
| Canonical alert_id | Backend `internal/contracts` | — | Single function, imported by Phase 3 detector and Phase 4 API |
| OpenAPI document | API contract (`api/`) | CI Go test, `web/` type generation | Hand-authored, validated, consumed by the TS client |
| Config, logging, DB/Redis clients | Backend shared packages | — | Each binary composes small constructors; no HTTP-only container |
| Secret hygiene | CI (gitleaks) | `.env.example`, `.gitignore` | Prevents commits of secrets |

## Standard Stack

### Core (verified 2026-10-10)
| Library / Image | Version | Purpose | Verification |
|-----------------|---------|---------|--------------|
| Go | 1.26.x (`go 1.26.0` in go.mod; toolchain/image 1.26.9) | Module language | go.dev/dl lists 1.26.9 and 1.27.2 [VERIFIED: go.dev/dl?mode=json] |
| `apache/kafka` | `4.3.1@sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837` | Broker (KRaft) | Ran it, topics created, healthy [VERIFIED: docker hub + local run] |
| `clickhouse/clickhouse-server` | `26.3.47.6@sha256:55a55347b1b959ae51d40373901e81aa671d7fc9e6d05d0603d1e0ad3ac22c6d` | Analytics store | `select version()` returned 26.3.47.6; tags `26.3`, `26.3.47`, `26.3.47.6` share the digest [VERIFIED: docker hub API + local run] |
| `postgres` | `18.6@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336` | Alert workflow/audit DB | `PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2)` [VERIFIED] |
| `redis` | `8.10.2@sha256:2c2dff791878316e3b083188be0d578423b24813cdda5de0e80ea4f7b3e6bfd6` | Rate limiting | `redis-cli ping` -> PONG [VERIFIED] |
| `github.com/getkin/kin-openapi` | v0.149.0 | CI OpenAPI 3.1 validation test | Loaded+validated 3.1 doc; bad `$ref` rejected [VERIFIED: local test] |
| `github.com/santhosh-tekuri/jsonschema/v6` | v6.0.3 | Contract tests (2020-12) | Embedded compile + validate + format assertion tested [VERIFIED: local test] |
| `github.com/jackc/tern/v2` | v2.4.3 | Postgres migrations (also CLI via `go tool`) | `go tool tern version` -> v2.4.3 [VERIFIED] |
| golangci-lint | v2.14.0 | Lint (built with go1.26.9) | `config verify` passes after edit [VERIFIED] |
| testcontainers-go + modules `kafka`, `clickhouse`, `redis`, `postgres` | all v0.44.0 | Integration tests | Proxy `@latest` for all five [VERIFIED: proxy.golang.org] |

Retained dependency bumps (from STACK.md, proxy-verified there): pgx v5.11.0, go-redis v9.23.0, echo v4.16.0, koanf v2.3.8 (+`providers/env`), zerolog v1.35.1, testify v1.12.1, validator v10.30.2, google/uuid v1.6.0, godotenv v1.5.1, pgx-zerolog. Run `go mod tidy` after deletions; expect `golang.org/x/time` to disappear (only the in-memory limiter uses it).

### Web (`web/`) - exact set that installed, typechecked, linted, tested and built [VERIFIED: local run]
| Package | Version | Note |
|---------|---------|------|
| react / react-dom | 19.3.0 | |
| vite | 8.3.4 | engines `^20.19.0 \|\| >=22.12.0` |
| @vitejs/plugin-react | 6.1.2 | peer `vite ^8.0.0` |
| vitest | 5.0.3 | peer `vite ^6.4 \|\| ^7 \|\| ^8`; engines `^20.19 \|\| ^22.13 \|\| >=24` per scaffold install; add `jsdom` |
| typescript | `~6.0.3` (latest 6.0.x; npm `latest` is 7.0.2, do NOT take it) | |
| eslint | 10.12.0 (engines `^20.19 \|\| ^22.13 \|\| >=24`) | flat config only |
| @eslint/js | 10.0.1 | |
| typescript-eslint | 8.71.1 | peer `typescript >=4.8.4 <6.1.0`, `eslint ^8.57 \|\| ^9 \|\| ^10` (so TS 7 is not allowed) |
| eslint-plugin-react-hooks | 7.1.1 | `hooks.configs.recommended.rules` worked in flat config |
| globals | 17.13.0 | |
| prettier | 3.9.10 | |
| jsdom | 30.1.2 | |
| @testing-library/react | 16.3.3 (+ `@testing-library/dom` ^10) | |
| @types/react, @types/react-dom ^19; @types/node ^22 | | vite peer wants `@types/node ^20.19 \|\| >=22.12` |
| openapi-typescript | 7.13.0 | **peer `typescript ^5.x`; needs override, see below** |

Required `package.json` fragment (without it `npm install` fails with ERESOLVE, reproduced):
```json
"overrides": { "openapi-typescript": { "typescript": "$typescript" } }
```
`openapi-typescript` output then compiles under `tsc --noEmit --strict` with TS 6.0.3 (tested on a 3.1 document using `type: [string, "null"]`). Use `npm ci` in CI, commit `web/package-lock.json`. Do not run two `npm install`s concurrently in one directory (that corrupted `node_modules` during research).

### Generator (`generator/`) [VERIFIED: local uv run]
`uv init --package --python 3.13 --name sentinel-generator .`, then `uv add --dev pytest==9.1.1 ruff==0.17.0 mypy==2.4.0`. uv 0.9.18 locally (PyPI latest 0.13.0). Export: `uv export --format requirements-txt --no-dev --no-hashes --no-emit-project -o requirements.txt` (without `--no-emit-project` the file contains `-e .`). CI: `uv lock --check` (passes), `uv sync --locked`, `uv run ruff check`, `uv run ruff format --check`, `uv run mypy src tests`, `uv run pytest`. Phase 1 has no runtime deps (the seed test uses stdlib `random.Random`); httpx, pydantic and orjson arrive in Phase 2.

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| kin-openapi | libopenapi v0.41.6 (+ libopenapi-validator v0.16.0) | Full 3.0/3.1/3.2 model; also loaded the 3.1 doc and flagged the dangling ref, but logs errors via slog rather than returning them on `NewDocument`, requires `go 1.26.0`, and needs `BuildV3Model()` error inspection. Fallback only. |
| `go tool` in main `go.mod` | `go tool -modfile=tools.mod` | Tested: works (`tern v2.4.3`). Keeps ~250 lint-only modules out of `go.mod`/`go.sum`. **Recommended.** |
| gitleaks-action | gitleaks CLI binary | Action v3.0.0 needs a license only for organization accounts, none for personal accounts [CITED: github.com/gitleaks/gitleaks-action README]. CLI v8.30.1 has no license concern and is simpler to pin. **Recommended: CLI.** |
| testcontainers `kafka` module | Generic container running `apache/kafka:4.3.1` with the Compose env block | Module v0.44.0 source references `confluentinc/confluent-local` and `/etc/confluent/docker/...`, with no apache/kafka support found [VERIFIED by grep of v0.44.0 source]. |

**Installation (Go, from `backend/`):**
```bash
# go.mod: "go 1.26.0"; GOTOOLCHAIN=go1.26.9 locally if the host Go is older
go get github.com/getkin/kin-openapi@v0.149.0 github.com/santhosh-tekuri/jsonschema/v6@v6.0.3 \
  github.com/jackc/tern/v2@v2.4.3 github.com/testcontainers/testcontainers-go@v0.44.0 \
  github.com/testcontainers/testcontainers-go/modules/{postgres,redis,clickhouse}@v0.44.0
# tools in a separate modfile (backend/tools.mod, tools.sum committed)
cp go.mod tools.mod   # or create a minimal one: "module tools\n\ngo 1.26.0"
go get -modfile=tools.mod -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
go get -modfile=tools.mod -tool github.com/jackc/tern/v2@v2.4.3
# run: go tool -modfile=tools.mod golangci-lint run ./...   |   go tool -modfile=tools.mod tern ...
```
Note: `go get -tool` with `-modfile` was verified for tern (`go tool -modfile=tools.mod tern version`); the golangci-lint run path through `-modfile` was verified only for `config verify` using in-module `go tool`. Mark running full lint through `-modfile` as MEDIUM; if it misbehaves fall back to in-module `go tool` (verified), accepting the larger `go.mod`.

## Package Legitimacy Audit

slopcheck (installed via pip) `scan` run on the scratch `web/package.json`: all packages `OK` except `vitest` flagged `[SUS]` as "suspiciously close to vite". This is a false positive: vitest is the Vite-ecosystem test runner (`vitest-dev`), 5.0.3 installed and its peers reference vite. Go modules are not covered by slopcheck; they were verified on proxy.golang.org (`@latest` JSON with VCS origin) and are all long-established projects.

| Package | Registry | Source Repo | slopcheck | Disposition |
|---------|----------|-------------|-----------|-------------|
| vite, @vitejs/plugin-react, react, react-dom, eslint, @eslint/js, typescript, typescript-eslint, prettier, jsdom, openapi-typescript, @testing-library/react, globals | npm | well known | OK | Approved |
| vitest | npm | github.com/vitest-dev/vitest | SUS (typosquat heuristic, false positive) | Approved; planner may add a one-line human check of the npm publisher |
| pytest, ruff, mypy | PyPI | well known | not run (pyproject scan found 0 deps because dev group) | Approved, versions confirmed via PyPI JSON and `uv add` |
| kin-openapi, jsonschema/v6, tern, golangci-lint, testcontainers-go | Go proxy | github.com/getkin, santhosh-tekuri, jackc, golangci, testcontainers | n/a (Go) | Approved, proxy-verified |

**Removed [SLOP]:** none. **Flagged [SUS]:** vitest (false positive, see above).

## Architecture Patterns

### System Architecture Diagram (Phase 1 scope)
```
developer
   |  make dev-up
   v
Makefile --> docker compose up --wait ---> [kafka] --healthy--> [topic-init one-shot]
   |                                       [clickhouse] [postgres] [redis]   (all healthcheck-gated, named volumes)
   |  make migrate
   +--> migrate binary/target --> tern (backend/migrations/postgres) --> postgres (schema_version)
   |                          \-> CH runner (backend/migrations/clickhouse) --> clickhouse (schema_migrations, CREATE DATABASE IF NOT EXISTS)
   |  make test / lint / test-integ
   +--> go test ./... (contracts: schema+structs+fixtures, DedupeKey fuzz seeds, openapi validate, config/db tests)
   +--> npm ci/typecheck/lint/test/build (web)  +  uv run ruff/mypy/pytest (generator)

5 binaries (ingest, detector, event-writer, alert-writer, query-api):
   config.Load -> logger.New -> deps connect/ping (pg, redis; kafka/ch reachability) -> block on SIGINT/SIGTERM -> ordered close

contracts:  schema/*.json (embed) <-- test --> Go structs --> DedupeKey -> sha256 hex (alert_id)
            api/openapi.yaml --test (kin-openapi load+validate; schema drift check)--> web/ openapi-typescript
CI: gofmt/vet/golangci-lint/test/race | web | python | schema | gitleaks | (integ smoke job)
```

### Recommended Project Structure
```
backend/
  go.mod  go.sum  tools.mod  tools.sum  .golangci.yml
  cmd/{ingest,detector,event-writer,alert-writer,query-api}/main.go
  internal/
    config/        # per-binary sections; Load(); godotenv only if PRIMARY.ENV=local (explicit call, not autoload)
    logger/        # zerolog only; New(cfg, service)
    errs/ validation/ sqlerr/
    database/      # pgx pool (DSN via url.URL), tern migrator, embed migrations from backend/migrations/postgres
    chmigrate/     # ClickHouse runner (embed backend/migrations/clickhouse)
    redisx/        # New(ctx, cfg) -> client + fatal ping
    httpx/         # thin http.Server wrapper (timeouts, Shutdown nil-guard); used by ingest, query-api only
    middleware/    # request_id, context, global (CORS/secure/recover/error handler), explicit deps not *server.Server
    contracts/     # event.go alert.go dedupe.go schema/*.json (go:embed)
    testing/       # container helpers (postgres, redis, clickhouse, kafka-generic)
  migrations/{postgres,clickhouse}/
  test/            # build-tagged integration + contract tests (package-level tags: integration)
api/openapi.yaml  config/rules.yaml  web/  generator/  deployments/compose/  docs/decisions/  Makefile  .env.example
```
`go:embed` cannot reach outside the package directory. `migrations/` is a sibling of `internal/` under `backend/`, so a package that embeds it must live at `backend/migrations` (a tiny `package migrations` with `//go:embed postgres/*.sql clickhouse/*.sql`) rather than inside `internal/database`. Do that; do not use `..` paths.

### Verified Compose (use as the base file; ports bind to 127.0.0.1)
Tested end to end: `up -d --wait` twice (exit 0 both times), `run --rm topic-init` re-run (idempotent, `--if-not-exists`), `kafka-topics.sh --describe` shows 6/3/1 partitions, RF 1, retention 86400000/604800000/604800000, and producing to an undeclared topic yields `UNKNOWN_TOPIC_OR_PARTITION` with no topic created (auto-create off).
```yaml
name: sentinel
services:
  kafka:
    image: apache/kafka:4.3.1@sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837
    ports: ["127.0.0.1:29092:29092"]
    environment:
      CLUSTER_ID: "MkU3OEVBNTcwNTJENDM2Qk"          # fixed 22-char base64 UUID; keep identical for the life of the volume
      KAFKA_NODE_ID: "1"
      KAFKA_PROCESS_ROLES: broker,controller
      KAFKA_CONTROLLER_QUORUM_VOTERS: 1@kafka:9093
      KAFKA_LISTENERS: INTERNAL://:9092,EXTERNAL://:29092,CONTROLLER://:9093
      KAFKA_ADVERTISED_LISTENERS: INTERNAL://kafka:9092,EXTERNAL://localhost:29092
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT
      KAFKA_INTER_BROKER_LISTENER_NAME: INTERNAL
      KAFKA_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_AUTO_CREATE_TOPICS_ENABLE: "false"
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: "1"
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: "1"
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: "1"
      KAFKA_SHARE_COORDINATOR_STATE_TOPIC_REPLICATION_FACTOR: "1"   # Kafka 4.x share-group state topic; harmless, avoids surprises
      KAFKA_SHARE_COORDINATOR_STATE_TOPIC_MIN_ISR: "1"
      KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS: "0"
      KAFKA_LOG_DIRS: /var/lib/kafka/data
    volumes: ["kafka-data:/var/lib/kafka/data"]
    healthcheck:
      test: ["CMD-SHELL", "/opt/kafka/bin/kafka-broker-api-versions.sh --bootstrap-server kafka:9092 >/dev/null 2>&1"]
      interval: 5s
      timeout: 10s
      retries: 20
      start_period: 15s
  topic-init:
    image: apache/kafka:4.3.1@sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837
    depends_on: { kafka: { condition: service_healthy } }
    restart: "no"
    entrypoint: ["/bin/bash", "-c"]
    command:
      - |
        set -euo pipefail
        B=kafka:9092; T=/opt/kafka/bin/kafka-topics.sh
        $$T --bootstrap-server $$B --create --if-not-exists --topic security.events.v1 --partitions $${EVENTS_PARTITIONS:-6} --replication-factor 1 --config retention.ms=$${EVENTS_RETENTION_MS:-86400000}
        $$T --bootstrap-server $$B --create --if-not-exists --topic security.alerts.v1 --partitions $${ALERTS_PARTITIONS:-3} --replication-factor 1 --config retention.ms=$${ALERTS_RETENTION_MS:-604800000}
        $$T --bootstrap-server $$B --create --if-not-exists --topic security.events.dlq.v1 --partitions 1 --replication-factor 1 --config retention.ms=$${DLQ_RETENTION_MS:-604800000}
        $$T --bootstrap-server $$B --list
  clickhouse:
    image: clickhouse/clickhouse-server:26.3.47.6@sha256:55a55347b1b959ae51d40373901e81aa671d7fc9e6d05d0603d1e0ad3ac22c6d
    ports: ["127.0.0.1:18123:8123", "127.0.0.1:19000:9000"]
    environment:
      CLICKHOUSE_DB: sentinel
      CLICKHOUSE_USER: sentinel
      CLICKHOUSE_PASSWORD: ${SENTINEL_CLICKHOUSE_PASSWORD:?set in .env}
      CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT: "1"
    volumes: ["clickhouse-data:/var/lib/clickhouse"]
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:8123/ping | grep -q Ok."]
      interval: 5s
      timeout: 5s
      retries: 20
  postgres:
    image: postgres:18.6@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336
    ports: ["127.0.0.1:15432:5432"]
    environment: { POSTGRES_USER: sentinel, POSTGRES_DB: sentinel, POSTGRES_PASSWORD: "${SENTINEL_POSTGRES_PASSWORD:?set in .env}" }
    volumes: ["postgres-data:/var/lib/postgresql"]   # PG 18 images: mount the parent dir, not /var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U sentinel -d sentinel"]
      interval: 5s
      timeout: 5s
      retries: 20
  redis:
    image: redis:8.10.2@sha256:2c2dff791878316e3b083188be0d578423b24813cdda5de0e80ea4f7b3e6bfd6
    ports: ["127.0.0.1:16379:6379"]
    command: ["redis-server", "--save", "", "--appendonly", "no"]   # rate-limit state is ephemeral
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 20
volumes: { kafka-data: {}, clickhouse-data: {}, postgres-data: {} }
```
What was run vs. what is a recommendation: the Kafka, topic-init and ClickHouse blocks were run exactly as above (ClickHouse with literal credentials instead of `${...}`); Postgres and Redis were run as plain `docker run` containers and the two healthcheck commands (`pg_isready`, `redis-cli ping`) verified there. The `${...:?}` interpolation, the PG 18 volume path and Redis flags are [ASSUMED] untested in Compose. Verify in the first plan's acceptance step. PG 18 changed the image's `PGDATA` layout so the volume must be at `/var/lib/postgresql` [ASSUMED from docker-library release notes, MEDIUM].

Host ports 5432, 6379, 9009 and 8025 were occupied on the research machine; the 15432/16379/18123/19000/29092 set avoided all conflicts. Document in README.

Operational notes:
- `docker compose up --wait` with a one-shot service: it returned 0 both times; the service exits 0 and compose treats that as success on this version (Compose v5.0.0). Older Compose v2.x may report a failure for exited services. Mitigation: Makefile `dev-up` should run `docker compose up -d --wait kafka clickhouse postgres redis` then `docker compose run --rm topic-init` (the second command is verified idempotent and gives the correct exit code).
- Kafka warns that `.` and `_` topic names can collide in metrics; the names use only dots, so it is benign.
- Changing `CLUSTER_ID` against an existing volume breaks start-up (`make reset` removes volumes).

### Go binaries pattern (D-06)
Shared helper `internal/app` (or each main) does: `cfg := config.Load()` returning `(*Config, error)` (not `logger.Fatal` inside Load), `log := logger.New(cfg, "ingest")`, `ctx, stop := signal.NotifyContext(ctx, SIGINT, SIGTERM)`, connect only the dependencies that binary needs (ingest: redis+kafka-reachability; detector: kafka+redis?; event-writer: kafka+clickhouse; alert-writer: kafka+postgres+clickhouse; query-api: postgres+clickhouse+redis), then `<-ctx.Done()`, then close in reverse order with a timeout. Reachability for Kafka/ClickHouse in Phase 1 should be a plain TCP dial (`net.Dialer`), because franz-go and clickhouse-go are not yet dependencies. Keep this honest: it is a connect check, not a feature. Close already-opened resources on every error path (defer stack).

### Anti-Patterns to Avoid
- `_ "github.com/joho/godotenv/autoload"` (current config.go): loads `.env` unconditionally at import. Replace with an explicit `godotenv.Load()` guarded on `os.Getenv("SENTINEL_PRIMARY.ENV") == "local"` before koanf load.
- `logger.Fatal()` inside `LoadConfig` (current): library code that exits the process cannot be tested for the failure path; return errors.
- `fmt.Sprintf` DSNs (current database.go and migrator.go; duplicated). Use one `database.ConnString(cfg)` built with `url.URL{Scheme:"postgres", User:url.UserPassword(u,p), Host:net.JoinHostPort(h,port), Path:"/"+name, RawQuery:"sslmode=..."}.String()`. The current `url.QueryEscape` also turns spaces into `+`, which is wrong in the userinfo component. Add a test with a password containing `@ : / ? # % space +`.
- Docker `latest` tags, unpinned actions in CI. Pin actions by tag at minimum (SHA preferred; versions seen: checkout v7.0.1, setup-go v7.0.0, setup-node v7.1.0 [MEDIUM, from `gh api releases/latest`]; setup-uv v10.3.0 [MEDIUM, mapping of release query output order]. Planner: re-check at write time).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| JSON Schema validation in tests | Custom validators for the contract tests | `jsonschema/v6` with `AssertFormat()` | Format assertions (uuid, ipv4/ipv6, date-time) are annotation-only by default in 2020-12; without `AssertFormat()` the invalid-IP fixture passes (see pitfalls) |
| OpenAPI validity check | Custom YAML walker | `openapi3.Loader` + `doc.Validate(ctx)` | Catches dangling refs and structural errors (verified) |
| Postgres migrations | Own version table | tern (`schema_version`) | Already wired |
| Secret scanning | Regex grep in CI | gitleaks CLI | Maintained rule set |
| Lockfile export | Hand-written `requirements.txt` | `uv export` | Drift-free |
| IP parsing | Regex | `net/netip` (`ParseAddr`) | IPv4, IPv6, zone handling |
| Idempotent topic creation | Scripted describe+create | `kafka-topics.sh --create --if-not-exists` | Verified idempotent |
| SHA-256 key canonicalization | `strings.Join(parts, "|")` | Length-prefixed encoding (below) | `|` in entity values collides |

The ClickHouse migration runner is the one sanctioned hand-roll (D-11): ordered `.sql` files, a `schema_migrations(version, applied_at)` table, `CREATE TABLE IF NOT EXISTS`. Keep it under ~100 lines. clickhouse-go is not required in Phase 1 if the runner talks over the HTTP interface (port 8123) with `net/http`; this avoids adding the driver now. If the planner prefers the driver, `clickhouse-go/v2 v2.49.0` (go 1.25) is already chosen in STACK.md. Pick HTTP for Phase 1 to keep the dependency graph small; Phase 2 adds the native driver. [recommendation]

## Boilerplate Disposition and Import-Graph Impact

Measured on the working tree (3,148 Go lines in 50 files). `server.Server` importers: handler (base, handlers, health, openapi), middleware (auth, context, global, middlewares, rate_limit, tracing), repository, router, service (auth, services), testing (helpers, server).

| Path | Action | Why / consequence |
|------|--------|-------------------|
| `cmd/sentinel/main.go` | Delete; replace with 5 mains | |
| `internal/lib/job/*`, `lib/email/*`, `lib/utils/*` | Delete | asynq, resend, `pkg/errors`, `fmt.Println` helper |
| `internal/middleware/auth.go` | Delete | Clerk |
| `internal/middleware/rate_limit.go`, `tracing.go` | Delete | In-memory limiter; New Relic tracing. `middlewares.go` aggregator rewritten (it imports newrelic) |
| `internal/service/*`, `internal/repository/*` | Delete | Only auth (Clerk) and an empty scaffold. Phases 2-4 re-introduce under the real domain. Deleting them removes `service` -> `server` and `repository` -> `server` edges |
| `internal/handler/*` | Delete in Phase 1 | `base.go` uses `nrpkgerrors`/`newrelic` and `*server.Server`; `health.go` is `/healthz`-style endpoints deferred to Phase 2; `openapi.go` serves the removed `static/openapi.*`. D-06 forbids routes. Recreate generic wrappers in Phase 2 without NR. [recommendation; CONTEXT is silent on `handler`/`router`/`model`] |
| `internal/router/*` | Delete in Phase 1 | Router wires the deleted rate limiter, NR middleware, handlers. Phase 2 builds the ingest router |
| `internal/model/base.go` | Delete | `PaginatedResponse` uses page/limit; spec requires cursor pagination (`items`,`next_cursor`,`has_more`) |
| `internal/server/server.go` | Replace with `internal/httpx` (listen/serve/Shutdown nil-guard) + `redisx` | Current file: NR redis hook, asynq start, Redis ping error ignored, `Shutdown` derefs nil `httpServer` and never closes Redis (the four known defects) |
| `internal/logger/logger.go` | Refactor | Remove `LoggerService`, `nrApp`, `WithTraceContext(txn)`, newrelic import; `zerolog/pkgerrors` import also unneeded (stack marshaller relies on `pkg/errors`). Keep `NewPgxLogger`, `GetPgxTraceLogLevel`. Always emit JSON in non-local envs; keep console writer for `local` |
| `internal/config/{config,observability}.go` | Refactor | Drop `Integration.ResendAPIKey` (currently `validate:"required"`, so startup fails without a Resend key), `Auth.SecretKey` (Clerk; re-add in Phase 2/4 as needed), `ServerConfig` per binary, `NewRelic` sub-config in observability. Add `Kafka`, `ClickHouse`, `RateLimit.FailMode`, `Contracts` limits. Return errors instead of `logger.Fatal` |
| `internal/database/{database,migrator}.go` | Refactor | Remove nrpgx5 and `multiTracer`; single DSN builder; `//go:embed migrations/*.sql` must move to a `backend/migrations` package (embed cannot use `..`); migrator's `from == int32(len(...))` check is fine; `fmt.Errorf` without `%w` at "retreiving" |
| `internal/database/migrations/001_setup.sql` | Delete | Empty |
| `internal/middleware/{request_id,context,global}.go` | Refactor | `global.go` imports `github.com/pkg/errors` (swap to stdlib `errors`) and reads `s.Config`/`s.Logger` from `*server.Server`; `context.go` uses `newrelic.FromContext` and `s.Logger`. Change constructors to take `(cfg, *zerolog.Logger)` |
| `internal/errs/{http,types}.go` | Refactor | Envelope must be `{"error":{"code","message","request_id"}}`; `HTTPError` currently marshals `status`, `override`, `errors`, `action` at top level. Add `RequestID` and a wrapper type for the response body. `types.go` has gofmt drift |
| `internal/validation/utils.go` | Keep | Imports `errs` only |
| `internal/sqlerr/*` | Keep | Imports `errs` only |
| `internal/testing/{server,helpers}.go` | Rewrite | Depend on `server.Server`; `container.go` is reusable (config+database); `assertions.go`, `transaction.go` keep |
| `static/`, `templates/`, `taskfile.yml`, `.env.sample`, `readme.md` (backend) | Delete / rewrite | `.env.sample` is the only env file tracked in git |
| Repo root `apps/`, `packages/`, `package.json`, `turbo.json`, `bun.lock`, `package-lock.json`, `node_modules/` | Delete | Also `readme.md` references; root `.gitignore` already exists |
| `go.mod` | Remove requires | clerk-sdk-go, asynq, resend, newrelic (all 5 modules), pkg/errors, `golang.org/x/time` (after the limiter goes), `golang.org/x/text` (verify with tidy) |
| `.golangci.yml` | Edit | See below |

`backend/.env` is gitignored by `backend/.gitignore` (verified with `git check-ignore`) and was not read. The root `.gitignore` must also ignore `/.env`, `web/node_modules`, `web/dist`, `generator/.venv`, `**/__pycache__`.

### golangci-lint v2 config migration [VERIFIED]
`golangci-lint config verify` on the existing file fails with `typecheck is not a linter, it cannot be enabled or disabled`. Delete these three lines from `linters.enable`: `gosimple`, `typecheck`, `stylecheck`. (In v2, gosimple and stylecheck merged into `staticcheck`, which is already enabled; typecheck is built-in.) After removal `config verify` exits 0. Other settings (`exhaustruct` block is configured but the linter is not enabled; harmless) carry over. Expect `lll`, `mnd`, `gochecknoglobals`, `testpackage`, `varnamelen`-type findings on new code; `testpackage` forces `_test` packages, so contract tests must use `package contracts_test` and only exported API. `gochecknoglobals` conflicts with `//go:embed var schemaFS embed.FS` and compiled-schema vars: embed vars need `//nolint:gochecknoglobals // go:embed requires a package-level variable` (nolintlint requires a linter name and explanation, which this satisfies). Prefer a function that returns the FS from an unexported package var.

### tern as a tool
`go tool tern version` -> `tern v2.4.3`. Makefile `migrate` should prefer the in-repo binary path (a `migrate` subcommand or small `cmd`-less runner in `internal/database`) so that the same embedded files are used by tests and CLI. If the Phase 1 plan wants no sixth binary, implement `make migrate` as `go run ./cmd/query-api migrate`-style subcommand is discouraged by D-06 (binaries carry no logic). Recommended: a small `backend/cmd/migrate/main.go` is a sixth binary and conflicts with "five binaries". Therefore: put migration logic in `internal/migrate` and invoke it from `backend/test`-style `go run ./internal/migrate/cmd`? Not allowed by `internal` layout. **Decision for planner:** use `go tool -modfile=tools.mod tern migrate --migrations backend/migrations/postgres --conn-string "$$URL"` for Postgres in the Makefile, and a `go run ./tools/chmigrate` (directory `backend/tools/chmigrate`, outside `cmd/`, documented as tooling not a service binary) for ClickHouse. [recommendation, LOW-MEDIUM: choose one and record it in ADR 0001]

## Contracts (FND-04)

### jsonschema/v6 usage [VERIFIED: ran compile + validate + fuzz]
```go
// Source: tested in scratchpad; API per github.com/santhosh-tekuri/jsonschema v6.0.3
//go:embed schema/*.json
var schemaFS embed.FS

func compile(name string) (*jsonschema.Schema, error) {
	f, err := schemaFS.Open("schema/" + name)
	if err != nil { return nil, err }
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)        // preserves numbers correctly for validation
	if err != nil { return nil, err }
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()                               // REQUIRED: formats are not asserted by default in 2020-12
	url := "https://sentinel.local/schema/" + name // must equal the schema's $id
	if err := c.AddResource(url, doc); err != nil { return nil, err }
	return c.Compile(url)
}
// validate: v, _ := jsonschema.UnmarshalJSON(strings.NewReader(payload)); err := sch.Validate(v)
```
Observed errors are precise (`at '/event_id': 'x' is not valid uuid`, `additional properties 'extra' not allowed`). `format: uuid` accepts any UUID version, so "UUID v7" must be enforced by a regex `pattern` in the schema (`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`) if D-14 wants v7 strictly ("v7 recommended" in D-14, so use any-version `format: uuid` plus a Go-level warning is also acceptable; choose and record). Cross-file `$ref` between `event.v1.json` and `alert.v1.json` is not needed. For nullable optionals use `"type": ["string","null"]`. `occurred_at`: `format: date-time` plus a `pattern` requiring `Z` suffix (UTC only), because `date-time` also accepts offsets. Millisecond normalization is a Go-side rule (marshal with `.UTC().Truncate(time.Millisecond)` and a fixed layout `2006-01-02T15:04:05.000Z07:00`... use `time.RFC3339Nano`-style with exactly three digits). Skew bounds cannot be expressed in JSON Schema; implement in Go `Validate(now, skew)` and test the boundary (+5m, +5m+1ms, -7d, -7d-1ms).

`additionalProperties: false` at root rejects unknown fields in the schema, and the Go decoder uses `DisallowUnknownFields`. `attributes`: `type: object`, `additionalProperties: {type: [string, number, boolean]}`, `maxProperties: 32`; the 4 KiB total size is a Go check (schema cannot sum lengths); `null` attribute values rejected (scalar only).

### DedupeKey [VERIFIED: fuzz ran 580k execs/5 s with no injectivity violation]
```go
func lp(b []byte, s string) []byte { b = binary.BigEndian.AppendUint32(b, uint32(len(s))); return append(b, s...) }

func DedupeKey(tenant, ruleID string, entity []string, windowStart time.Time) string {
	b := []byte("sentinel.alert.v1")                         // domain separator / version
	b = lp(b, tenant); b = lp(b, ruleID)
	b = binary.BigEndian.AppendUint32(b, uint32(len(entity)))  // component count
	for _, e := range entity { b = lp(b, e) }
	b = binary.BigEndian.AppendUint64(b, uint64(windowStart.UTC().UnixMilli()))
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])                            // 64 lowercase hex chars = alert_id
}
```
Fuzz target used: two tuples (tenant, rule, entityA, entityB, windowMs) -> assert `(inputs equal) == (keys equal)`, seeded with the `|` collision pair (`"x|y","z"` vs `"x","y|z"`). `go test -fuzz=FuzzDedupeKey -fuzztime=5s .` ran clean. In CI run only the seed corpus (plain `go test`, fuzz targets execute their `f.Add` seeds and `testdata/fuzz` corpus automatically) and optionally `-fuzztime=10s` in a non-required job. Add golden-vector tests (fixed input -> fixed hex) so the encoding can never change silently; once Phase 3 ships alerts, changing it is a breaking change. `entityKey` in the schema/display (`192.0.2.10|example-user`) stays a human-readable string; the canonical form takes the entity component slice, so the `Alert` struct should carry `EntityParts` only inside the detector, not on the wire. Note the spec example shows `"alert_id": "sha256-of-stable-dedupe-key"`, which is explicitly not valid hex; fixtures must use real 64-char hex and the alert schema should pattern-check `^[0-9a-f]{64}$`. Also `time.UnixMilli` negative or far-future values: reject window_start outside year 2000-2100 in Go validation.

### Fixture set (contract tests)
Valid: minimal required-only; full example from spec §6.1; IPv4 and IPv6 (`2001:db8::1`, reserved ranges only: 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24, 2001:db8::/32); ports 0 and 65535. Invalid: missing each required field (one case each), unknown root field, bad enum, port 65536 and -1, bad IP (`999.1.1.1`, `1.2.3`), tenant violating regex (uppercase, `|`, 64+ chars, empty), non-UTC offset timestamp, timestamp > +5m and < -7d, attributes with 33 keys, nested attribute object, oversize attribute total, non-UUID `event_id`. The struct-vs-schema agreement test runs the same corpus through both validators and asserts identical accept/reject.

## OpenAPI (FND-05)

### Validation test (kin-openapi) [VERIFIED]
```go
func TestOpenAPIDocumentIsValid(t *testing.T) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromFile("../../../api/openapi.yaml") // adjust relative path
	require.NoError(t, err)
	require.NoError(t, doc.Validate(loader.Context))
	require.True(t, strings.HasPrefix(doc.OpenAPI, "3.1"))
}
```
Observed: 3.1 document with `type: [string, "null"]` loads and validates; a dangling `$ref` fails at load (`map key "components" not found`). kin-openapi v0.149.0 README states 3.1 support and documents that in 3.1 docs `nullable` is no longer honored by `VisitJSON` and `type` arrays must be used; therefore write the YAML with `type: [x, "null"]`, `const`, `examples` (not `example`) the 3.1 way. Additional assertions to include: every path/method in a literal expected list from spec §8 exists (guards against omission), `responses` include 202/400/401/403/413/429/503 for `events:batch` and `Retry-After` header on 429/503, `securitySchemes` has `apiKey` (header) and `http bearer`, error schema requires `error.code`, `error.message`, `error.request_id`, list responses require `items`, `next_cursor`, `has_more`. The path `/api/v1/events:batch` is valid OpenAPI; quote it in YAML.

Drift test (event/alert vs JSON Schema): simplest robust approach: the OpenAPI `Event`/`Alert` component schemas are `$ref` to `./../backend/internal/contracts/schema/event.v1.json`? kin-openapi would need `IsExternalRefsAllowed = true`, and openapi-typescript resolves relative refs too, but JSON Schema `$schema`/`$id` keywords in a referenced file can confuse OpenAPI tooling. Recommended: **inline copies in `api/openapi.yaml` and a Go test that loads both, marshals each component schema to JSON, strips `$schema`/`$id`/`title`/`description`, and compares to the JSON Schema `properties`/`required`/`additionalProperties`/enum sets** (compare property names, required list, enums, and key constraints rather than the whole tree). Practical alternative with less code: the drift test validates every valid fixture against the OpenAPI component schema using kin-openapi `VisitJSON(v, openapi3.EnableJSONSchema2020())` and every invalid fixture is rejected by it. Both are acceptable; the fixture-based one is easier and checks behavior. [recommendation, MEDIUM: `VisitJSON` with EnableJSONSchema2020 was not executed here]

### TS client compile
`npx openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts` (CI runs it and then `tsc --noEmit`; also `git diff --exit-code src/api/schema.d.ts` if the generated file is committed; recommended: generate in CI/`npm run gen:api` and commit nothing generated, but then typecheck needs a `pretypecheck` script).  Verified on a minimal 3.1 doc; it must be re-run against the real document by the planner's acceptance step. Do not add `openapi-fetch`/`react-query` in Phase 1.

## Common Pitfalls

### Pitfall 1: ERESOLVE from openapi-typescript + TypeScript 6
**What goes wrong:** `npm install` aborts (peer `typescript ^5.x`). **Why:** the package has not widened its peer range. **Avoid:** `overrides` block above (verified). **Signal:** CI `npm ci` failing on a clean runner. Re-check the peer range when bumping openapi-typescript.

### Pitfall 2: jsonschema formats silently not asserted
Without `c.AssertFormat()` `format: ipv4`/`uuid` fixtures pass when they should fail. Test an invalid-IP fixture explicitly.

### Pitfall 3: Kafka dual-listener mistakes
Containers must use `kafka:9092`, the host `localhost:29092`; advertise both. Single advertised listener breaks one side. Test both paths in the smoke check: `docker compose exec kafka ... --bootstrap-server kafka:9092` (internal, used by the healthcheck) and, from the host, a TCP dial to 127.0.0.1:29092. Changing `CLUSTER_ID` with an old volume fails startup; `make reset` handles it.

### Pitfall 4: `up --wait` and one-shot services
Run `topic-init` as a separate `docker compose run --rm topic-init` after `up --wait` of the long-running services (see Compose notes); verify `make dev-up` twice from a clean state.

### Pitfall 5: go:embed path limits
`//go:embed ../..` is illegal. Embedded SQL must live in or below the embedding package's directory: use a `backend/migrations` package that embeds `postgres/*.sql clickhouse/*.sql`. The same applies to schemas (already in `internal/contracts/schema`).

### Pitfall 6: Go toolchain on CI and dev
go.mod `go 1.26.0` makes older Go auto-download `go1.26.x` via GOTOOLCHAIN (the research host has Go 1.25.5 and used `GOTOOLCHAIN=go1.26.9`). In CI use `actions/setup-go` with `go-version-file: backend/go.mod` (or `1.26.9`) and `cache-dependency-path: backend/go.sum`. Set `toolchain go1.26.9` in go.mod to pin exactly. Also `docs` must state the minimum Go.

### Pitfall 7: `gochecknoglobals` and `testpackage` linters vs embed and internal tests
See lint section. Plan for `//nolint:gochecknoglobals // reason` on embed vars only; contract tests in `_test` packages.

### Pitfall 8: Echo and the `events:batch` route [ASSUMED, Phase 2 impact]
Echo treats `:` as a path-parameter marker, so a literal `/api/v1/events:batch` route may not register as intended. Not Phase 1 code, but the OpenAPI path keeps the spec's `:batch`; Phase 2 needs an Echo workaround (verify) or a `net/http` mux for ingest. Flag in the ADR notes.

### Pitfall 9: Testcontainers flakiness in CI
Pin images identically to Compose (`postgres:18.6`, `redis:8.10.2`, ClickHouse 26.3.47.6, apache/kafka:4.3.1); reuse the Ryuk reaper (do not set `TESTCONTAINERS_RYUK_DISABLED`); one container per test package via `TestMain`, not per test; wait strategies by log/healthcheck, not sleeps; bound each package with `go test -timeout 5m`; run integration tests with `-p 1` initially; keep them behind `//go:build integration` so `make test` stays fast. For Kafka via a generic container, the advertised listener must use the mapped host port: create the container with a fixed free host port chosen up front (listen on `:0`, close, reuse) and set `KAFKA_ADVERTISED_LISTENERS` to `EXTERNAL://localhost:<port>` before start; this is the main thing the Kafka module normally handles. [MEDIUM; not executed]

### Pitfall 10: Secrets in Compose
`${VAR:?}` forces failure when `.env` is missing, which is the desired fail-loud behavior; `make dev-up` should copy `.env.example` to `.env` only if absent and print that it did (never overwrite).

## Code Examples

### Makefile skeleton (key behaviours)
```make
SHELL := bash
COMPOSE := docker compose -f deployments/compose/docker-compose.yml --env-file .env
GO := cd backend && go
.PHONY: dev-up dev-down reset migrate test test-integ lint seed bench
dev-up: ; @test -f .env || (cp .env.example .env && echo "created .env from .env.example")
	$(COMPOSE) up -d --wait kafka clickhouse postgres redis
	$(COMPOSE) run --rm topic-init
dev-down: ; $(COMPOSE) down
reset: ; @[ "$(CONFIRM)" = "1" ] || (echo "refusing: run 'make reset CONFIRM=1' (destroys local volumes)"; exit 1)
	$(COMPOSE) down -v --remove-orphans
test: ; $(GO) test -race ./... && $(MAKE) -C . web-test py-test
test-integ: ; $(GO) test -tags=integration -timeout 10m -p 1 ./test/...
seed: ; @echo "seed is available from Phase 2"; exit 1
bench: ; @echo "bench is available from Phase 5"; exit 1
```
(Tabs required in the real Makefile. Use the Compose project name `sentinel`; the verified run used a different name but nothing depends on it.)

### CI skeleton (jobs; every step fails on non-zero)
- `go`: setup-go (`go-version-file: backend/go.mod`); `test -z "$(gofmt -l .)"`; `go vet ./...`; `go tool -modfile=tools.mod golangci-lint run`; `go test -race ./...`; fuzz seeds run as part of `go test`.
- `web`: setup-node 22; `npm ci`; `npm run typecheck && npm run lint && npm test && npm run build` in `web/`.
- `python`: setup-uv; `uv sync --locked`; ruff check, ruff format --check, mypy, pytest in `generator/`.
- `schema`: `go test ./internal/contracts/... ./test/... -run 'Schema|OpenAPI'` plus `npx openapi-typescript api/openapi.yaml` compile (from `web/` to reuse deps).
- `secrets`: download gitleaks CLI v8.30.1 release tarball, verify against the published `*_checksums.txt` (asset naming `gitleaks_8.30.1_linux_x64.tar.gz` [ASSUMED, check release page]), run `gitleaks git --redact --no-banner -v` with `fetch-depth: 0`; add `.gitleaks.toml` allowlisting only `.env.example` placeholder lines if its values trip rules (use obviously fake `change-me-local`, which does not).
- `compose-smoke` (optional, `timeout-minutes: 10`): `make dev-up`, assert health, `make dev-down`. Deferred as required gate to Phase 6.

## State of the Art

| Old | Current | Impact |
|-----|---------|--------|
| golangci-lint v1 `gosimple`/`stylecheck`/`typecheck` | v2: merged into `staticcheck`; `typecheck` removed | Config edit required |
| Taskfile/Turborepo | Makefile + per-language lockfiles | Matches spec §16 |
| ZooKeeper/Bitnami Kafka images | `apache/kafka` KRaft official image | Env prefix `KAFKA_`, `CLUSTER_ID` unprefixed |
| `tools.go` blank-import trick | `go get -tool` / `go tool` (Go 1.24+) | Verified with `-modfile` |
| TypeScript 7 native line (`latest` on npm) | Stay on `~6.0.3` | typescript-eslint peer `<6.1.0`; openapi-typescript peer `^5` already needs override |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | PG 18 image wants the volume at `/var/lib/postgresql` (not `/data`) | Compose | Postgres container refuses to start or data location wrong; caught by first `make dev-up` |
| A2 | `${VAR:?}` + `--env-file` interpolation and `redis-server --save ""` behave in Compose as written | Compose | Minor; fix in first plan's acceptance step |
| A3 | Echo mishandles a literal `:batch` path segment | Pitfall 8 | Phase 2 routing; may be a non-issue |
| A4 | GitHub Action versions (checkout v7.0.1, setup-go v7.0.0, setup-node v7.1.0, setup-uv v10.3.0) and the gitleaks tarball asset name | CI | CI step fails fast; planner should re-check on write |
| A5 | `go tool -modfile=tools.mod golangci-lint run` works for full lint runs | Installation | Fallback to in-module `go tool` (verified) |
| A6 | `VisitJSON(..., EnableJSONSchema2020())` for the OpenAPI drift fixtures | OpenAPI | Use the property/required/enum comparison alternative |
| A7 | Deleting `handler/`, `router/`, `model/`, `service/`, `repository/` entirely is acceptable (CONTEXT lists `handler` and `router` neither under remove nor keep) | Boilerplate | If the user wants them kept, the refactor effort grows (base.go + health.go + router NR/limiter removal) |
| A8 | Kafka integration tests via a generic `apache/kafka:4.3.1` container rather than the testcontainers kafka module | Pitfall 9 | If the module later supports apache/kafka, switch |

## Open Questions (RESOLVED)

1. **Migration invocation without a sixth binary.**
   - Known: D-01 fixes five binaries; D-10 needs `make migrate`; tern CLI works as a tool.
   - Unclear: where the ClickHouse runner executable lives.
   - Recommendation: `backend/tools/chmigrate` (tooling `main`, not under `cmd/`) plus tern via `go tool`; record in ADR 0001. Alternatively run migrations as a `-migrate` flag on one service binary (rejected: D-06).
   - **RESOLVED:** D-29. `backend/tools/chmigrate` tooling main plus tern via `go tool`; recorded in ADR 0001.

2. **Strict UUIDv7 enforcement.** D-14 says "validated as a UUID (v7 recommended)". Recommendation: schema `format: uuid` only, with generator emitting v7; do not reject other versions.
   - **RESOLVED:** D-30. Schema `format: uuid` with `AssertFormat()`; generator emits v7; other versions accepted.

3. **Handler/router/model deletion** (A7). Recommend delete; confirm during plan check.
   - **RESOLVED:** D-28. Delete `handler/`, `router/`, `model/`, `service/`, `repository/` entirely.

4. **OpenAPI schema drift strategy** (inline + fixture test vs `$ref`): left to planner per Discretion; fixture-based test recommended.
   - **RESOLVED:** Claude's Discretion, exercised in plan 01-11 (inline schemas plus fixture-based drift test).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|-------------|-----------|---------|----------|
| Docker + Compose | FND-01 | yes | 29.1.3 / v5.0.0 | none |
| Go | FND-03/04/05 | yes (1.25.5) | needs 1.26.x | `GOTOOLCHAIN=go1.26.9` auto-download worked |
| Node / npm | web | yes | 22.23.3 / npm bundled | none |
| uv | generator | yes | 0.9.18 (Python 3.13.11 provisioned) | none |
| golangci-lint, gitleaks | lint, secrets | not installed locally | — | `go tool` for lint; gitleaks CLI download in CI, optional locally |
| Network (registries) | all | yes | — | — |

Host ports 5432, 6379, 9009, 8025, 27017 are taken on the research machine (other projects); chosen host ports avoid them.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Go | `go test` (stdlib) + testify v1.12.1 + jsonschema/v6 + kin-openapi; integration under `//go:build integration` |
| Web | Vitest 5.0.3 (jsdom), ESLint 10, `tsc` (config in `web/vite.config.ts`, `eslint.config.js`) |
| Python | pytest 9.1.1, ruff 0.17.0, mypy 2.4.0 (config in `generator/pyproject.toml`) |
| Quick run | `make test` (Go unit+contract with `-race`, web test, py test) |
| Full | `make lint test && make test-integ` |

### Phase Requirements -> Test Map
| Req | Behavior | Type | Automated command | Exists |
|-----|----------|------|-------------------|--------|
| FND-01 | All four services healthy, topics 6/3/1 with retention, auto-create off, idempotent | smoke (shell+compose) | `make dev-up && make dev-up && scripts/check-stack.sh` (asserts `docker compose ps --format json` health, `kafka-topics.sh --describe` partition counts, produce-to-unknown-topic fails) | Wave 0 |
| FND-01 | Host listener reachable | smoke | `bash -c '</dev/tcp/127.0.0.1/29092'` | Wave 0 |
| FND-02 | `migrate` twice is a no-op; `reset` refuses without CONFIRM | integration | `make migrate && make migrate` (second prints up-to-date); `make reset; test $? -ne 0`; `make reset CONFIRM=1` | Wave 0 |
| FND-02 | `seed`/`bench` exit non-zero | unit-ish | `! make seed && ! make bench` | Wave 0 |
| FND-03 | Five binaries build; no banned imports | build+grep | `cd backend && go build ./cmd/... && ! go list -deps ./... \| grep -E 'newrelic\|clerk\|pkg/errors\|hibiken/asynq\|resend'` | Wave 0 |
| FND-03 | Each binary starts, exits 0 on SIGTERM; fails fast with Redis down | integration | `go test -tags=integration ./test -run TestBinaryLifecycle` | Wave 0 |
| FND-03 | Defects: recover first, Shutdown nil-guard + closes redis, Redis ping fatal, DSN escaping, godotenv gating, resource close on error | unit | `go test ./internal/... -run 'Recover\|Shutdown\|Redis\|DSN\|Dotenv\|Close'` | Wave 0 |
| FND-04 | Schema and struct accept/reject identical fixtures; IPv4/IPv6; skew boundaries | contract | `go test ./internal/contracts/... -run 'Schema\|Fixture\|Skew'` | Wave 0 |
| FND-04 | DedupeKey golden vectors and injectivity | unit+fuzz | `go test ./internal/contracts -run DedupeKey` and `go test ./internal/contracts -fuzz=FuzzDedupeKey -fuzztime=10s` | Wave 0 |
| FND-05 | OpenAPI valid 3.1, all spec §8 routes/status codes/envelope/pagination present, drift test vs schemas | contract | `go test ./test/... -run OpenAPI` ; `cd web && npx openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts && npm run typecheck` | Wave 0 |
| FND-06 | Pins: no `latest`/unpinned image; lockfiles exist; `.env.example` placeholders only; no secrets | script+scan | `scripts/check-pins.sh` (grep compose for `@sha256`, `test -f` lockfiles, `uv lock --check`, `npm ci`); `gitleaks git --redact` | Wave 0 |
| FND-07 | Six ADRs exist with required headings | script | `scripts/check-adrs.sh` (files `docs/decisions/000[1-6]-*.md` each containing Status, Context, Decision, Consequences) | Wave 0 |
| FND-03/06 | CI workflow has no `\|\| true` | script | `! grep -n '|| true' .github/workflows/*.yml` | Wave 0 |

### Sampling Rate
- Per task commit: `cd backend && go test ./internal/... -short` plus the task's own command.
- Per wave merge: `make lint test`.
- Phase gate: clean-checkout `make dev-up` twice, `make migrate` twice, `make test`, `make test-integ`, `make lint`, CI green.

### Wave 0 Gaps
- [ ] `scripts/check-stack.sh`, `scripts/check-pins.sh`, `scripts/check-adrs.sh` (small shell checks)
- [ ] `backend/internal/contracts/*_test.go` + `testdata/` fixtures (valid/invalid JSON)
- [ ] `backend/test/` build-tagged integration harness (container helpers for postgres/redis/clickhouse/kafka)
- [ ] OpenAPI validation test and expected-route table
- [ ] `web` Vitest+jsdom setup; `generator/tests/test_seed.py`
- [ ] Framework installs: `npm ci` (web), `uv sync --locked` (generator)

## Security Domain

### Applicable ASVS Categories
| Category | Applies | Control in Phase 1 |
|----------|---------|--------------------|
| V2 Authentication | no (deferred to Phases 2/4) | Clerk removed; no auth endpoints exist |
| V3 Session Management | no | — |
| V4 Access Control | no | Tenant regex in contracts prepares isolation |
| V5 Input Validation | yes | JSON Schema + typed decode `DisallowUnknownFields`, `net/netip`, bounded attributes, tenant regex |
| V6 Cryptography | yes (minor) | stdlib `crypto/sha256` for `alert_id`; no custom crypto |
| V14 Config | yes | Placeholder-only `.env.example`, `.env` gitignored, localhost-bound ports, gitleaks, no secrets in compose |

### Known Threat Patterns
| Pattern | STRIDE | Mitigation |
|---------|--------|------------|
| Secret committed to repo | Information disclosure | gitleaks in CI, `.env` ignored (root `.gitignore` must be updated), `${VAR:?}` in Compose |
| Services exposed on all interfaces | Info disclosure / elevation | Publish ports on `127.0.0.1` only |
| DSN injection via special characters in password | Tampering | `url.UserPassword` + `pgx.ParseConfig`, tested |
| Composite-key collision (`|` in tenant/entity) → alert suppression or cross-tenant dedupe | Tampering | Tenant regex excludes `|`; length-prefixed `DedupeKey`; fuzz test |
| Supply chain (typosquat packages, unpinned images) | Tampering | Lockfiles, image digests, slopcheck review |
| Unbounded attribute maps | DoS | 32 keys / 4 KiB limits in schema + Go |

## Sources

### Primary (HIGH, executed 2026-10-10)
- Local runs: Compose stack (kafka, topic-init x2, clickhouse; postgres/redis containers), kin-openapi v0.149.0 and libopenapi v0.41.6 load tests, jsonschema/v6 compile+validate, Go fuzz of DedupeKey, `go get -tool` / `go tool -modfile`, golangci-lint `config verify`, npm install/typecheck/lint/test/build of the web scaffold, uv init/add/export/lock --check, slopcheck scan.
- Docker Hub API (`hub.docker.com/v2/repositories/...`): digests for apache/kafka 4.3.1, clickhouse 26.3.47.6, postgres 18.6, redis 8.10.2.
- proxy.golang.org: versions of libopenapi (v0.41.6), kin-openapi, jsonschema/v6, golangci-lint v2.14.0, tern v2.4.3, testcontainers-go and kafka/clickhouse/redis/postgres modules (all v0.44.0), libopenapi-validator v0.16.0; `.mod` files for go directive minimums (tern 1.25.0, libopenapi 1.26.0, testcontainers kafka 1.25.0).
- npm registry (`npm view`): versions, peerDependencies, engines for vite, vitest, @vitejs/plugin-react, openapi-typescript, typescript-eslint, eslint.
- PyPI JSON: mypy 2.4.0, uv 0.13.0 (latest; 0.9.18 used locally).

### Secondary (MEDIUM)
- kin-openapi README (raw.githubusercontent.com/getkin/kin-openapi) 3.1 notes; libopenapi README ("full support for OpenAPI 3, 3.1 and 3.2"); gitleaks-action README license statement (no license for personal accounts); testcontainers-go v0.44.0 `modules/kafka/kafka.go` source (confluent-local); `gh api releases/latest` for action and gitleaks versions.

### Tertiary (LOW / [ASSUMED])
- A1-A8 above.

## Metadata

**Confidence breakdown:**
- Standard stack and versions: HIGH, executed or registry-queried today.
- Compose/Kafka/ClickHouse: HIGH for Kafka, topic-init, ClickHouse; MEDIUM for the Postgres volume path and Compose interpolation.
- Contracts and OpenAPI tooling: HIGH (executed); drift-test API MEDIUM.
- Boilerplate impact: HIGH (read from the code); deletion scope for handler/router/model is a recommendation (A7).
- Testcontainers Kafka approach and CI action versions: MEDIUM/LOW.

**Research date:** 2026-10-10
**Valid until:** about 2026-11-10 for image and npm pins (ClickHouse 26.3.x patch releases appear almost daily; the `26.3.47.6` digest stays valid but re-check before Phase 6).
