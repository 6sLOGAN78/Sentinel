# Sentinel

Sentinel is a distributed security-event platform for authenticated ingestion,
durable Kafka delivery, ClickHouse storage, investigation, and alerting. The
current stack accepts scoped event batches, evaluates four deterministic
event-time rules, persists detections and workflow rows, and serves tenant-scoped
event lookups. Delivery is at-least-once with immediate visible deduplication.
Phase 3 is verified complete: 21/21 plans and all nine hosted CI jobs passed.
Dashboard login, alert query/triage APIs and SSE remain Phase 4 work, not started.

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

Start the five-application path:

Existing deployments must first follow the
[stopped-writer reservation rollout](docs/operations.md#reservation-protocol-rollout).
Do not mix old and new writer protocols or reconnect unresolved old native writes.

```sh
make dev-up
scripts/check-stack.sh
```

`make dev-up` is ordered and idempotent: it waits for Kafka, ClickHouse,
PostgreSQL, and Redis; creates the declared topics; applies PostgreSQL and
ClickHouse migrations; then builds and waits for `ingest`, `detector`,
`event-writer`, `alert-writer`, and `query-api`. Detector rules are mounted
read-only from `config/rules.yaml`. Repeating startup preserves existing volumes and reapplies the current
ClickHouse TTL and Kafka topic retention policies. `EVENTS_RETENTION_MS`,
`ALERTS_RETENTION_MS`, and `DLQ_RETENTION_MS` in `.env` (or overriding shell
environment values) apply to existing topics on every `make dev-up`.
Reducing retention can permanently delete data already eligible under the new
policy, so choose these values deliberately. `scripts/check-stack.sh` checks
the same effective Compose values, including environment precedence.

Existing topic partition counts are never changed by startup. Changing
`EVENTS_PARTITIONS` or `ALERTS_PARTITIONS` leaves the current layout in place
and makes stack acceptance report the mismatch; a fresh layout requires a
separately authorized destructive reset (`make reset CONFIRM=1`).

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

For exact malicious, benign, boundary and lateness expectations, follow the
[preserved-state demo](docs/demo.md). With other producers paused, it runs
`make seed` then two bounded `make seed-scenarios` commands on the same topics,
groups and volumes. Each normal scenario run expects eight current alerts,
preserves prior rows/workflows, and requires zero new late drops. Historical
seed traffic itself is not claimed to produce zero late drops.

## Endpoints and readiness

Every published port binds to `127.0.0.1` only.

| Service | Host endpoint |
| --- | --- |
| Ingest API | `http://127.0.0.1:8080` |
| Query API | `http://127.0.0.1:8081` |
| Ingest readiness | `http://127.0.0.1:9101/readyz` |
| Detector readiness / internal late counter | `http://127.0.0.1:9102/readyz` / `/metrics` |
| Event-writer readiness | `http://127.0.0.1:9103/readyz` |
| Alert-writer readiness | `http://127.0.0.1:9104/readyz` |
| Query readiness | `http://127.0.0.1:9105/readyz` |
| Kafka external listener | `127.0.0.1:29092` |
| PostgreSQL | `127.0.0.1:15432` |
| Redis | `127.0.0.1:16379` |
| ClickHouse HTTP / native | `127.0.0.1:18123` / `127.0.0.1:19000` |

Readiness is dependency-aware and returns HTTP 200 only when a service can do
useful work. Liveness remains process-local:

```sh
curl --fail --max-time 5 http://127.0.0.1:9101/healthz
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
`event-writer`, `alert-writer`, and `query-api`.

## Stop and verify

```sh
make dev-down     # stops containers; named volumes are preserved
make test         # Go, web, and generator unit tests
make test-integ   # full real-dependency suite; healthy dev stack required; 32m ceiling
make test-phase3  # each exact Phase 3 family; healthy dev stack required
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
prohibited-module, and independently nonempty Phase 3 behavioral-group guards,
while retaining every Phase 2 group. Ordinary families use six-minute ceilings,
cooldown eight minutes, scheduled scenarios sixteen minutes and preserved repeat
thirty-two minutes; relevant CI jobs are bounded to forty-five minutes. Local
gates passed, including the 46 exact families with affected-fixture delta
verification, repaired full integration and all seven retained Phase 2 selectors;
the scoped reservation R2 review is clean. The
[preserved-stack rollout check](.planning/phases/03-detection-alert-pipeline/03-RESERVATION-ROLLOUT-CHECK.md)
records retained-data reconciliation and additive migration. See the
[local acceptance record](.planning/phases/03-detection-alert-pipeline/03-ALERT-RESERVATION-EVIDENCE.md)
for actual exits and historical failures. The older hosted checkpoint failed;
the repaired source passed all nine jobs in
[CI39515550386](https://github.com/6sLOGAN78/Sentinel/actions/runs/39515550386).
[Final goal verification](.planning/phases/03-detection-alert-pipeline/03-VERIFICATION.md)
passes 59/59 with all 14 requirements satisfied. Phase 4 is not started.

## Documentation

- [Architecture](docs/architecture.md)
- [Architecture decisions](docs/decisions/)
- [Event storage operations](docs/event-storage.md)
- [Detection and alert operations](docs/operations.md)
- [Preserved-state demo](docs/demo.md)
- [Threat model](docs/threat-model.md)
- [Benchmarks](benchmarks/README.md)
