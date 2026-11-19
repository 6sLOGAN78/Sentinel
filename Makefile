SHELL := bash
.DEFAULT_GOAL := help

COMPOSE := python3 -B scripts/dev_auth_env.py -- docker compose -f deployments/compose/docker-compose.yml --env-file .env
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

.PHONY: help dev-up dev-down reset migrate test test-integ test-phase3 test-phase4 test-chaos lint seed seed-scenarios bench bench-up bench-down bench-purge run detector-recover-history

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/'

dev-up: ## Start infrastructure, migrate, and launch all five application services
	@if [ ! -f .env ]; then cp .env.example .env && echo "created .env from .env.example"; fi
	$(COMPOSE) up -d --wait kafka clickhouse postgres redis
	$(COMPOSE) run --rm topic-init
	$(MAKE) migrate
	$(COMPOSE) up -d --build --wait ingest detector event-writer alert-writer query-api

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
	# chmigrate exposes --events-ttl-days and resolves it from this forwarded plain env.
	cd backend && SENTINEL_CLICKHOUSE_PASSWORD="$(call dotenv_or_env,SENTINEL_CLICKHOUSE_PASSWORD)" SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS="$(call dotenv_or_env,SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS)" go run ./tools/chmigrate --addr $(CH_ADDR) --user sentinel --database sentinel

web/node_modules: web/package-lock.json
	cd web && npm ci && touch node_modules

generator/.venv: generator/uv.lock generator/pyproject.toml
	cd generator && uv sync --locked && touch .venv

test: web/node_modules generator/.venv ## Run unit tests for backend, web and generator
	cd backend && go test -race ./...
	cd web && npm test
	cd generator && uv run --locked pytest

# The measured full suite exhausted 10m before scenarios. It now includes all
# causal crash cuts plus 16m scheduled / 32m preserved-repeat subtests, so the
# aggregate gate has a finite 32m ceiling. Individual family caps stay unchanged.
test-integ: ## Run all integration tests (needs Docker and make dev-up)
	cd backend && go test -tags=integration -timeout 32m -p 1 ./test/...

# Ordinary groups 6m, cooldown 8m, scheduled scenario 16m, preserved repeat 32m.
test-phase3: ## Run each exact Phase 3 acceptance family (needs healthy dev stack)
	bash scripts/check-phase3-tests.sh

# Eight exact Phase 4 native families (phase4 tag): ordinary 6m, delivery/stream 10m.
# The real-browser acceptance is separate: bash scripts/check-phase4-browser.sh.
test-phase4: ## Run each exact Phase 4 native family (needs healthy dev stack)
	bash scripts/check-phase4-tests.sh

# Named spec 15 failure families (testcontainers only); runs the resource gate first.
test-chaos: ## Run named failure suites (needs Docker)
	bash scripts/check-chaos.sh

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
	cd backend && $(if $(filter query-api,$(SVC)),python3 -B ../scripts/dev_auth_env.py --,env -u SENTINEL_AUTH_JWT_SECRET -u SENTINEL_AUTH.JWT_SECRET) env 'SENTINEL_PRIMARY.ENV=local' 'SENTINEL_ENV_FILE=$(CURDIR)/.env' 'SENTINEL_SERVER.BIND_ADDRESS=127.0.0.1' 'SENTINEL_OPS.BIND_ADDRESS=127.0.0.1' go run ./cmd/$(SVC)

# Offline, audited recovery for a detector group whose checkpoints fell below
# the retained log start (docs/operations.md, ADR 0012). Dry-run by default.
# ACK, OPERATOR and REASON reach the recipe through the environment and are
# quoted there, never interpolated or echoed; they apply only when all are set.
# Partial input fails before any go command runs.
RECOVER_APPLY_ARGS = if [ -z "$${ACK:-}" ] || [ -z "$${OPERATOR:-}" ] || [ -z "$${REASON:-}" ]; then \
		echo "refusing: apply needs ACK=<plan_digest> OPERATOR=<name> REASON=<text> together" >&2; \
		exit 1; \
	fi; \
	set -- --apply --acknowledge-loss="$$ACK" --operator="$$OPERATOR" --reason="$$REASON";
detector-recover-history: ## Plan (default) or apply an acknowledged expired-history detector recovery
	@set --; \
	$(if $(or $(value ACK),$(value OPERATOR),$(value REASON)),$(RECOVER_APPLY_ARGS)) \
	if [ ! -f .env ]; then echo ".env not found; run 'make dev-up' first" >&2; exit 1; fi; \
	cd backend && env -u SENTINEL_AUTH_JWT_SECRET -u SENTINEL_AUTH.JWT_SECRET env 'SENTINEL_PRIMARY.ENV=local' 'SENTINEL_ENV_FILE=$(CURDIR)/.env' go run ./tools/detector-recover "$$@"

seed: dev-up ## Seed and verify 1000 events through the real application path
	python3 -B scripts/seed.py

# Scheduling and delivery share 15 minutes; reconciliation owns the final 60s.
# The preserved repeat integration selector has a separate 32-minute ceiling.
SCENARIO_OUTPUT ?= $(CURDIR)/.local/last-scenarios.json
SCENARIO_PRESERVE ?=
seed-scenarios: dev-up ## Verify exact current-run scenario alerts in Kafka, CH and PG
	timeout 16m python3 -B scripts/seed_scenarios.py --output "$(SCENARIO_OUTPUT)" $(if $(SCENARIO_PRESERVE),--preserve-manifest "$(SCENARIO_PRESERVE)")

# One scenario on the isolated sentinel-bench-$(RUN) stack (reused from bench-up or
# started), then bench/report.py writes benchmarks/results or refuses. The stack is
# stopped afterwards unless KEEP_UP=1. SUPERSEDE=1 moves a same-date report from another
# run, unedited, to benchmarks/results/superseded/. Never $(COMPOSE), never the root .env.
bench: generator/.venv ## Run a bench scenario: make bench SCENARIO=<sustained-restart|overload> RUN=<id> [KEEP_UP=1] [SUPERSEDE=1]
	@if [ -z "$(RUN)" ]; then echo "refusing: set RUN=<id> (e.g. make bench SCENARIO=overload RUN=20261030a)" >&2; exit 1; fi
	cd generator && uv run --locked python ../bench/run.py run --scenario "$(or $(SCENARIO),sustained-restart)" --run "$(RUN)" $(if $(KEEP_UP),--keep-up) $(if $(SUPERSEDE),--supersede)

# Isolated sentinel-bench-$(RUN) project with its own ports, volumes and private
# generated env (.local/bench/$(RUN)/env). Never $(COMPOSE), never the root .env,
# never prune. bench/run.py validates RUN before any docker call.
bench-up: generator/.venv ## Start an isolated sentinel-bench-<RUN> stack
	cd generator && uv run --locked python ../bench/run.py up --run "$(RUN)"

bench-down: generator/.venv ## Stop the sentinel-bench-<RUN> stack (volumes kept)
	cd generator && uv run --locked python ../bench/run.py down --run "$(RUN)"

bench-purge: generator/.venv ## Remove sentinel-bench-<RUN> volumes and images (requires CONFIRM=1)
	cd generator && uv run --locked python ../bench/run.py purge --run "$(RUN)" $(if $(filter 1,$(CONFIRM)),--confirm)
