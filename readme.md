# Sentinel

Sentinel is a distributed security event platform for ingestion, detection, investigation and alerting. Simulated network and identity events are validated, streamed through Kafka, checked against deterministic detection rules, stored in ClickHouse and PostgreSQL, and surfaced through an authenticated API and a React dashboard. Delivery is at-least-once with idempotent consumers.

## Status

Phase 1 provides infrastructure, contracts and scaffolds only. The binaries start, load configuration and shut down gracefully, but have no HTTP routes yet. `make seed` and `make bench` exist but intentionally fail until Phase 2 and Phase 5 respectively. No performance numbers are claimed anywhere in this repository; any future claim will link to a reproducible benchmark.

## Prerequisites

- Docker with Compose
- Go 1.26 or newer (the toolchain is downloaded automatically via `GOTOOLCHAIN`)
- Node 22.12 or newer
- uv (Python package manager)
- make
- Network access on a clean checkout

## Version matrix

| Component | Version |
| --- | --- |
| Apache Kafka (KRaft) | apache/kafka 4.3.1 |
| ClickHouse | 26.3.47.6 |
| PostgreSQL | 18.6 |
| Redis | 8.10.2 |
| Go | 1.26.9 |
| Vite | 8.3.4 |
| React | 19.3.0 |
| TypeScript | ~6.0.3 |
| Python | 3.13 |

## Host ports

All ports are bound to 127.0.0.1 and chosen to avoid collisions with locally installed services.

| Service | Host port |
| --- | --- |
| Kafka (external listener) | 29092 |
| ClickHouse HTTP | 18123 |
| ClickHouse native | 19000 |
| PostgreSQL | 15432 |
| Redis | 16379 |

## Quickstart

```sh
make dev-up      # copies .env.example to .env if missing, starts the stack, creates topics
make migrate     # PostgreSQL (tern) and ClickHouse (tools/chmigrate), both idempotent
make test        # Go, web and Python tests
make lint        # Go, web and Python linters
make dev-down    # stops the stack, keeps volumes
make reset CONFIRM=1   # destructive, local only: removes volumes
```

`make test` and `make lint` install their own prerequisites on a clean checkout (`npm ci` in `web/` and `uv sync --locked` in `generator/`), so Node 22.12+, uv and network access are required. `make test-integ` runs the Compose/Testcontainers-backed tests behind the build tag `integration`.

### Running a binary locally

After `make dev-up`:

```sh
make run SVC=ingest
```

`SVC` is one of `ingest`, `detector`, `event-writer`, `alert-writer`, `query-api`. The target runs from `backend/` with `SENTINEL_PRIMARY.ENV=local` and `SENTINEL_ENV_FILE=../.env`, so the root `.env` is loaded and `backend/.env` is never read. Ctrl-C shuts the binary down gracefully. The binaries have no HTTP routes yet.

### Configuration keys

Configuration keys are dotted, for example `SENTINEL_DATABASE.HOST`. Bash cannot `export` names containing dots. Either pass them with `env 'SENTINEL_DATABASE.HOST=value' command`, or put them in the root `.env` file. godotenv loads that file only when `SENTINEL_PRIMARY.ENV=local`, from the path in `SENTINEL_ENV_FILE`.

## Documentation

- [Architecture](docs/architecture.md)
- [Architecture decisions](docs/decisions/)
- [Benchmarks](benchmarks/README.md)
