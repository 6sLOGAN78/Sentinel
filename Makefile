SHELL := bash
.DEFAULT_GOAL := help

COMPOSE := docker compose -f deployments/compose/docker-compose.yml --env-file .env
SERVICES := ingest detector event-writer alert-writer query-api

# Compose and migration passwords come from the root .env (plain names only).
# The file is not `include`d: Make would expand `$` and treat `#` as a comment,
# so a password such as pa$$w0rd#1 would differ from what Compose reads. Values
# are read literally (everything after the first `=`, last assignment wins),
# falling back to the environment when the key is absent.
ENV_FILE ?= $(CURDIR)/.env
dotenv_or_env = $$(v=$$(grep -E '^$(1)=' '$(ENV_FILE)' 2>/dev/null | tail -n 1 | cut -d= -f2-); printf '%s' "$${v:-$${$(1):-}}")

PG_HOST ?= 127.0.0.1
PG_PORT ?= 15432
CH_ADDR ?= http://127.0.0.1:18123

.PHONY: help dev-up dev-down reset migrate test test-integ lint seed bench run

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/'

dev-up: ## Start the pinned dependency stack and create topics (idempotent)
	@if [ ! -f .env ]; then cp .env.example .env && echo "created .env from .env.example"; fi
	$(COMPOSE) up -d --wait kafka clickhouse postgres redis
	$(COMPOSE) run --rm topic-init

dev-down: ## Stop the stack (volumes are preserved)
	$(COMPOSE) down

reset: ## Destroy the stack and its volumes (requires CONFIRM=1)
	@if [ "$(CONFIRM)" != "1" ]; then \
		echo "refusing: run 'make reset CONFIRM=1' (destroys local volumes)" >&2; \
		exit 1; \
	fi
	$(COMPOSE) down -v --remove-orphans

migrate: ## Apply PostgreSQL (tern) and ClickHouse migrations
	cd backend && PGPASSWORD="$(call dotenv_or_env,SENTINEL_POSTGRES_PASSWORD)" go tool -modfile=tools.mod tern migrate --migrations migrations/postgres --host $(PG_HOST) --port $(PG_PORT) --user sentinel --database sentinel --sslmode disable
	cd backend && SENTINEL_CLICKHOUSE_PASSWORD="$(call dotenv_or_env,SENTINEL_CLICKHOUSE_PASSWORD)" go run ./tools/chmigrate --addr $(CH_ADDR) --user sentinel --database sentinel

web/node_modules: web/package-lock.json
	cd web && npm ci && touch node_modules

generator/.venv: generator/uv.lock generator/pyproject.toml
	cd generator && uv sync --locked && touch .venv

test: web/node_modules generator/.venv ## Run unit tests for backend, web and generator
	cd backend && go test -race ./...
	cd web && npm test
	cd generator && uv run --locked pytest

test-integ: ## Run integration tests (needs Docker)
	cd backend && go test -tags=integration -timeout 10m -p 1 ./test/...

lint: web/node_modules generator/.venv ## Run linters for backend, web and generator
	cd backend && go tool -modfile=tools.mod golangci-lint run ./...
	cd web && npm run lint && npm run typecheck
	cd generator && uv run --locked ruff check . && uv run --locked ruff format --check . && uv run --locked mypy src tests

run: ## Run a service: make run SVC=<ingest|detector|event-writer|alert-writer|query-api>
	@case " $(SERVICES) " in \
		*" $(SVC) "*) ;; \
		*) echo "SVC must be one of: $(SERVICES)" >&2; exit 1 ;; \
	esac
	@if [ ! -f .env ]; then echo ".env not found; run 'make dev-up' first" >&2; exit 1; fi
	cd backend && env 'SENTINEL_PRIMARY.ENV=local' 'SENTINEL_ENV_FILE=$(CURDIR)/.env' go run ./cmd/$(SVC)

seed: ## Not available yet
	@echo "seed is available from Phase 2" >&2
	@exit 1

bench: ## Not available yet
	@echo "bench is available from Phase 5" >&2
	@exit 1
