# Architecture

## Flow

```text
generator -> ingest -> Kafka -> detector -> ClickHouse / PostgreSQL -> query-api -> dashboard
```

1. The generator (Python) emits synthetic security events.
2. `ingest` validates batches and publishes them to Kafka (see ADR 0003).
3. `event-writer` consumes events into ClickHouse for search.
4. `detector` evaluates deterministic rules over event time and emits detections.
5. `alert-writer` persists detections: immutable evidence in ClickHouse, workflow state and list index in PostgreSQL (ADR 0006).
6. `query-api` serves authenticated search and alert workflow endpoints.
7. The React dashboard shows alerts in real time.

Delivery is at-least-once. Offsets are committed only after downstream effects succeed, and every consumer and store is idempotent.

## Phase 1 scope

Phase 1 delivers infrastructure (Compose stack, topics, migrations), contracts (event and alert JSON Schemas), binary scaffolds, tooling and the decision records. It does not implement ingestion, detection, storage writers or the API. Those arrive in later phases.

## Binaries

| Binary | Path | Role |
| --- | --- | --- |
| ingest | `backend/cmd/ingest` | HTTP ingestion, validation, Kafka producer |
| detector | `backend/cmd/detector` | Kafka consumer, rule evaluation |
| event-writer | `backend/cmd/event-writer` | Kafka consumer, ClickHouse writer |
| alert-writer | `backend/cmd/alert-writer` | Detection consumer, ClickHouse and PostgreSQL writer |
| query-api | `backend/cmd/query-api` | Authenticated read and workflow API |

`backend/tools/chmigrate` is a migration tool, not a service binary.

## Repository layout

```text
backend/
  cmd/{ingest,detector,event-writer,alert-writer,query-api}
  internal/contracts/schema/    embedded JSON Schemas
  migrations/{postgres,clickhouse}/
  test/                         integration tests (build tag: integration)
  tools/chmigrate/              ClickHouse migration runner
web/                            React + TypeScript + Vite dashboard
generator/                      Python event generator
deploy/                         Compose and Kubernetes manifests
observability/                  Prometheus, Grafana, OpenTelemetry config
config/                         rules.yaml (rule definitions)
benchmarks/                     reproducible benchmark harness and reports
docs/                           architecture and decisions
scripts/                        repository checks
```

The rationale is recorded in [ADR 0001](decisions/0001-repo-layout-and-binaries.md). All decisions are indexed in [decisions/](decisions/).
