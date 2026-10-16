# Sentinel

Sentinel is a distributed security-event platform for authenticated ingestion,
durable Kafka delivery, ClickHouse storage, investigation, and alerting. The
current Phase 2 stack accepts scoped event batches, persists them with
at-least-once semantics, and serves tenant-scoped event lookups.

## Prerequisites

- Docker with Compose
- Go 1.26 or newer (the project selects Go 1.26.9)
- Node 22.12 or newer
- Python 3.13 and `uv`
- GNU Make

## Local quickstart

Create the untracked root environment file before the first start:

```sh
cp .env.example .env
```

Replace the `change-me-local` values with local-only passwords. Keep
`SENTINEL_POSTGRES_PASSWORD` equal to `SENTINEL_DATABASE.PASSWORD`, and keep
`SENTINEL_CLICKHOUSE_PASSWORD` equal to `SENTINEL_CLICKHOUSE.PASSWORD`; the
plain names configure Compose while the dotted names support host-run tools.
Never commit `.env`.

Start the complete Phase 2 path:

```sh
make dev-up
scripts/check-stack.sh
```

`make dev-up` is ordered and idempotent: it waits for Kafka, ClickHouse,
PostgreSQL, and Redis; creates the declared topics; applies PostgreSQL and
ClickHouse migrations; then builds and waits for `ingest`, `event-writer`, and
`query-api`. Repeating it preserves existing volumes and reapplies the current
ClickHouse TTL safely.

Migrations can also be rerun independently, including after changing
`SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS` in `.env`:

```sh
make migrate
```

See [event storage operations](docs/event-storage.md) for retention and
at-least-once recovery details.

## Provision a local producer key

`keyctl ensure` creates or reuses a scoped credential and writes it to a
private, ignored file. It reads the PostgreSQL password through the environment,
not a command-line argument:

```sh
cd backend
PGHOST=127.0.0.1 \
PGPORT=15432 \
PGUSER=sentinel \
PGPASSWORD="$(grep -E '^SENTINEL_POSTGRES_PASSWORD=' ../.env | tail -n 1 | cut -d= -f2-)" \
PGDATABASE=sentinel \
PGSSLMODE=disable \
go run ./tools/keyctl ensure \
  --tenant demo \
  --scopes events:read,events:write \
  --name local-generator \
  --out ../.local/keys/demo.key
cd ..
```

The key file is mode `0600` under a mode `0700` directory. Do not print, pass
on the command line, or commit its contents. Generator usage is documented in
[generator/README.md](generator/README.md).

## Seed and verify the complete path

```sh
make seed
```

This command runs `make dev-up` once, privately ensures `demo` and `demo2`
credentials, and sends 1,000 deterministic benign events in batches of 100.
It exits zero only after all 1,000 IDs from that specific run are visible in a
tenant-filtered ClickHouse `uniqExact` check, three authenticated API lookups
succeed, and the same event queried with the other tenant's key returns 404.
The storage wait is capped at 60 seconds; missing manifests, rejected events,
shortfalls, malformed responses, and command failures return nonzero.

Local credential files and the send manifest live under ignored `.local/`
paths with private permissions. Revocation can take up to the ingest key-cache
TTL of 30 seconds. Redis-backed rate limiting fails closed by default, so a
Redis outage rejects ingest instead of bypassing quota unless local config
explicitly selects the audited fail-open mode.

## Endpoints and readiness

Every published port binds to `127.0.0.1` only.

| Service | Host endpoint |
| --- | --- |
| Ingest API | `http://127.0.0.1:8080` |
| Query API | `http://127.0.0.1:8081` |
| Ingest readiness | `http://127.0.0.1:9101/readyz` |
| Event-writer readiness | `http://127.0.0.1:9103/readyz` |
| Query readiness | `http://127.0.0.1:9105/readyz` |
| Kafka external listener | `127.0.0.1:29092` |
| PostgreSQL | `127.0.0.1:15432` |
| Redis | `127.0.0.1:16379` |
| ClickHouse HTTP / native | `127.0.0.1:18123` / `127.0.0.1:19000` |

Readiness is dependency-aware and returns HTTP 200 only when a service can do
useful work. Liveness remains process-local:

```sh
curl --fail --max-time 5 http://127.0.0.1:9101/livez
curl --fail --max-time 5 http://127.0.0.1:9101/readyz
```

## Run a service from Go

The Compose application instance already owns its host ports. To debug one
service locally, stop only that application container, then run the binary with
the root environment file:

```sh
docker compose -f deployments/compose/docker-compose.yml --env-file .env stop ingest
make run SVC=ingest
```

`make run` forces public and operations listeners to `127.0.0.1` while
preserving `SENTINEL_ENV_FILE`. Valid service names are `ingest`, `detector`,
`event-writer`, `alert-writer`, and `query-api`; detector and alert-writer
containers are intentionally deferred to Phase 3.

## Stop and verify

```sh
make dev-down     # stops containers; named volumes are preserved
make test         # Go, web, and generator unit tests
make test-integ   # real dependency integration tests
make lint         # Go, web, and generator linters/type checks
```

`make reset CONFIRM=1` is intentionally separate and destructive. Normal
startup and shutdown never delete volumes.

## Local security boundary

The Compose stack is for development. Host publications are loopback-only, but
PostgreSQL, Redis, Kafka, ClickHouse, and the HTTP APIs use plaintext within the
local Docker network. The configuration allows those exceptions only in the
`local` environment. Do not expose this stack to an untrusted network or reuse
its settings as a production deployment; production packaging, TLS, and secret
distribution are later-phase work.

The repository CI definition runs unit, integration, seed-orchestration,
prohibited-module, and nonempty behavioral-group guards. These checks have
been run locally; hosted GitHub Actions status remains pending until a branch
is pushed and an actual workflow run completes.

## Documentation

- [Architecture](docs/architecture.md)
- [Architecture decisions](docs/decisions/)
- [Event storage operations](docs/event-storage.md)
- [Benchmarks](benchmarks/README.md)
