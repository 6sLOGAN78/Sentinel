# Sentinel — Real-Time Threat Detection Platform

**Document:** `spec.md`  
**Version:** 1.0  
**Status:** Implementation-ready product and engineering specification  
**Project type:** Distributed security event ingestion, detection, investigation, and alerting platform  
**Primary objective:** Demonstrate production-quality backend, distributed systems, streaming data, and security engineering.

---

## 1. Product Summary

Sentinel collects network and identity security events from simulated sources (and optionally from Aegis, validates and streams them through Kafka, evaluates deterministic threat-detection rules, persists searchable event history in ClickHouse, and serves real-time alerts and dashboards through an authenticated web UI.

The architecture must support multiple stateless ingest and detection replicas, partitioned event processing, recoverable consumer offsets, bounded processing during load spikes, idempotent alert generation, observable system health, and reproducible scale measurements.

**Important claim discipline:** "Millions of events" refers to a configurable synthetic load-test *volume*, not a guaranteed processing rate. Never claim a throughput or latency number without a reproducible benchmark report describing the hardware, event size, concurrency, configuration, duration, and observed results.

### 1.1 Goals

1. Fully functioning end-to-end flow: generator -> ingestion -> Kafka -> detector -> ClickHouse -> API -> live dashboard.
2. Detect clearly defined security behaviors and open actionable alerts with evidence.
3. Provide filtering and timeline-based investigation of alerts and events.
4. Demonstrate concurrency, partitioning, replay recovery, backpressure, and failure-handling behavior.
5. Be runnable locally via Docker Compose and deployable to Kubernetes.
6. Supply tests, CI, developer documentation, sample datasets, and measurable performance results.

### 1.2 Non-goals (v1)

- Inline blocking or enforcement of user traffic; Sentinel is **detection only**.
- Real packet capture, TLS decryption, network interception, or endpoint agents.
- Autonomous remediation, offensive security capabilities, or attack automation.
- Production-grade SIEM feature parity, multi-region active-active, and tenant-by-tenant billing.
- Arbitrary SQL access from UI users or user-defined executable detection scripts.
- Full ML detection in the MVP; optional later enhancement behind the same alert interface.

### 1.3 Personas

- **Security analyst:** monitors open alerts, inspects evidence, triages severity and status.
- **Platform operator:** monitors ingestion lag, service errors, latency, and infrastructure health.
- **Developer/tester:** creates synthetic event traffic and validates rules/failure recovery.

---

## 2. Mandatory Technology Stack

| Layer | Technology | Responsibility |
|---|---|---|
| Ingest API | Go | HTTP event intake, validation, authentication, quotas |
| Stream | Apache Kafka (KRaft mode) | Durable, partitioned event transport |
| Detection workers | Go | Windowed streaming detection and alert publication |
| Event/alert analytics | ClickHouse | Event and alert evidence storage, analytical search |
| Hot state and throttling | Redis | Rate limiting, ephemeral dedupe/window helpers (where needed) |
| Backend API | Go | Search, triage, rule metadata, live alert stream |
| Load generator | Python | Reproducible synthetic traffic and benchmark reports |
| Web dashboard | React + TypeScript + Vite | Analyst and operations interface |
| Runtime | Docker Compose, Kubernetes | Local and cluster deployments |
| Observability | Prometheus + Grafana + OpenTelemetry | Metrics, dashboards, tracing |
| CI | GitHub Actions | Build, lint, tests, integration smoke checks |

Prefer a **single Go module with multiple binaries** for ingestion, detection, and query services rather than prematurely creating many microservices. Use current supported stable tool versions and pin exact versions in lockfiles/container images before implementation.

---

## 3. High-Level Architecture

```text
Synthetic Generator / Optional Aegis Adapter
                  |
                  v
         [Go Ingest API] -- Redis quota counters
          validate + auth
                  |
                  v
         Kafka: security.events.v1
             (key: tenant|source_ip)
                  |
      +-----------+-------------+
      |                         |
      v                         v
[Go Detector Workers]    [Go Event Writer]
 - keyed windows           - batch consume
 - rules + severity        - ClickHouse inserts
 - dedupe                 
      |
      v
 Kafka: security.alerts.v1
      |
      v
[Go Alert Writer] --> ClickHouse alert records
      |
      v
[Go Query/API Service] <---- ClickHouse
     |         |
     |         +--> REST filtering, triage and evidence
     +------------> SSE live alert notifications
                         |
                         v
                  [React Dashboard]

Prometheus / OTEL scrape or collect from all Go services
Kafka / Redis / ClickHouse / services run in Compose or Kubernetes
```

**Key decisions:**

- **Kafka is the durable event and alert transport.** Redis is not a second primary event bus.
- All Kafka consumers manually commit offsets **only after successful downstream effects**. On retry, writes may repeat; consumers and storage must make this safe.
- Use **at-least-once delivery** semantics; do not claim exactly-once processing end-to-end.
- Use a consistent partition key (`tenant_id|source_ip`) for detections keyed by a source IP. For distinct key needs, explicitly document repartitioning.
- Event history and alerts are stored separately in ClickHouse for investigative queries. Updates to alert status may use a small PostgreSQL store *only if later needed*; v1 must choose and document one workable model (see §7.3).

---

## 4. Core Use Cases

### UC-01: Ingest events

A producer submits a batch of security events over HTTPS. Sentinel authenticates the producer, validates schema and timestamp bounds, applies limits, publishes accepted events to Kafka, and returns per-batch results and traceable identifiers.

### UC-02: Detect suspicious activity

The rule engine identifies anomalous or suspicious patterns from streaming events, creates one alert per deterministic dedupe key and detection window, and includes referenced event IDs and rule explanations.

### UC-03: Investigate an alert

An analyst filters alerts, opens a detail page, sees severity, trigger time, source identifiers, rule description and evidence, and changes status to acknowledged or resolved with an audit record.

### UC-04: Observe service health

An operator checks event ingest rate, failed requests, Kafka consumer lag, detection delay, queue/retry rate, and ClickHouse write failures.

### UC-05: Replay and stress test

A tester runs a seeded traffic generator, includes injected suspicious patterns, kills/restarts workers, and verifies eventual recovery with no lost acknowledged Kafka records.

---

## 5. Functional Requirements

Use identifiers below in issues, tests, PRs, and phase exit checks.

| ID | Requirement | Priority |
|---|---|---|
| FR-01 | Accept JSON event batches with authentication and strict validation | P0 |
| FR-02 | Publish each accepted event to `security.events.v1` durably | P0 |
| FR-03 | Persist events to ClickHouse with deduplication strategy | P0 |
| FR-04 | Evaluate at least four built-in streaming detection rules | P0 |
| FR-05 | Persist generated alerts with evidence and rule identifiers | P0 |
| FR-06 | List/filter/paginate events and alerts via API | P0 |
| FR-07 | Show live alerts in React UI using SSE | P0 |
| FR-08 | Acknowledge/resolve alerts with actor/time and audit history | P0 |
| FR-09 | Provide deterministic synthetic event generator and scenario injection | P0 |
| FR-10 | Expose Prometheus metrics and health/readiness endpoints | P0 |
| FR-11 | Recover from worker crashes with replay-safe processing | P0 |
| FR-12 | Rate-limit ingestion and return meaningful overload errors | P0 |
| FR-13 | Docker Compose single-command setup with demo data | P0 |
| FR-14 | Kubernetes manifests or Helm chart with probes and resource limits | P1 |
| FR-15 | Dashboard timeseries, rule breakdown, health/lag charts | P1 |
| FR-16 | Multi-replica load test and benchmark report | P1 |
| FR-17 | Optional Aegis-to-Sentinel adapter, documented as separate integration | P2 |
| FR-18 | Optional offline-trained Python anomaly scorer with evaluation | P2 |

### 5.1 Built-in detection rules

All windows use **event time** (`occurred_at`), not request receipt time. Document watermark/lateness behavior and demonstrate it in tests.

| Rule ID | Detection | Default example threshold | Grouping |
|---|---|---|---|
| `AUTH_BRUTE_FORCE` | Repeated failed authentications | >= 10 failures in 5 min | tenant + source IP + target user |
| `PORT_SCAN` | Distinct destination ports probed | >= 20 ports in 60 sec | tenant + source IP + destination IP |
| `REQUEST_FLOOD` | Excessive network/request volume | >= 300 requests in 60 sec | tenant + source IP |
| `DENY_SPIKE` | Bursts of denied policy decisions | >= 30 denies in 2 min | tenant + source IP |

Thresholds are configuration values loaded from versioned YAML at startup; document whether changing a rule requires restart (acceptable in v1). Rule hit must generate a severity, summary, evidence count, window start/end, and bounded example event IDs (maximum 20). Each rule has a cooldown to suppress repeats, but separate windows can still be investigated.

**Event-time correctness:** Use bounded per-key window state and a documented lateness tolerance (e.g., 30 sec). Define deterministic handling for late events (count/drop-with-metric/reprocess); do not silently mix event and processing times. Where Kafka replay or multiple workers occur, ensure partition ownership and persistent/replayable state behavior. A v1 acceptable approach is to reconstruct bounded windows from replay with idempotent alerts, while retaining a sufficiently conservative commit strategy; specify and test how recovery behaves when a crash occurs between state update, alert publish, and offset commit.

---

## 6. Event and Alert Contracts

### 6.1 Canonical event schema (`security.events.v1`)

```json
{
  "schema_version": 1,
  "event_id": "018ff000-0000-7000-8000-000000000001",
  "tenant_id": "demo",
  "occurred_at": "2026-10-09T10:00:00Z",
  "received_at": "2026-10-09T10:00:01Z",
  "source": "generator",
  "event_type": "auth.failure",
  "source_ip": "192.0.2.10",
  "destination_ip": "198.51.100.20",
  "destination_port": 443,
  "protocol": "tcp",
  "principal": "example-user",
  "action": "deny",
  "outcome": "failure",
  "attributes": {"service": "login", "reason": "invalid_credentials"}
}
```

Required fields: `schema_version`, `event_id`, `tenant_id`, `occurred_at`, `source`, `event_type`. Optional fields are explicit nullable values in the schema implementation; reject unknown root-level fields or validate via a documented extension policy. Use real JSON schema/OpenAPI validation or Go validators consistently. Validate IP addresses, bounded attribute size, supported enums, maximum batch size and request size, and timestamp skew.

**Privacy:** Synthetic/test data only by default; never log authentication secrets, session tokens, or request bodies. `principal` must be pseudonymous in demonstrations.

### 6.2 Alert schema (`security.alerts.v1`)

```json
{
  "schema_version": 1,
  "alert_id": "sha256-of-stable-dedupe-key",
  "tenant_id": "demo",
  "rule_id": "AUTH_BRUTE_FORCE",
  "rule_version": 1,
  "severity": "high",
  "status": "open",
  "created_at": "2026-10-09T10:05:01Z",
  "window_start": "2026-10-09T10:00:00Z",
  "window_end": "2026-10-09T10:05:00Z",
  "entity_key": "192.0.2.10|example-user",
  "summary": "Repeated failed logins against one user",
  "event_count": 12,
  "sample_event_ids": ["018ff000-0000-7000-8000-000000000001"],
  "dedupe_key": "demo|AUTH_BRUTE_FORCE|192.0.2.10|example-user|2026-10-09T10:00:00Z"
}
```

`alert_id` should be a deterministic SHA-256 hex representation of a canonical dedupe key. The example label above is explanatory; actual payloads must contain valid hex. Store timestamps in UTC and format API responses in ISO 8601. Do not rely on per-instance random IDs for alert deduplication.

### 6.3 Kafka topics

| Topic | Key | Retention / behavior |
|---|---|---|
| `security.events.v1` | `tenant_id|source_ip` | configurable, local default 24h |
| `security.alerts.v1` | `tenant_id|alert_id` | configurable, local default 7d |
| `security.events.dlq.v1` | source message key | bounded dead-letter records for permanently invalid/poison messages |

Define partition counts and replication factors per environment. Compose may use 1 broker/replication factor 1, **not HA**; Kubernetes production-like deployment must explain multi-broker replicas and replication-factor settings. Acks should be `all` and retries controlled. No lossless promise during complete single-broker disk failure.

---

## 7. Data Storage and State

### 7.1 ClickHouse event table

Minimum columns: `tenant_id`, `event_id`, `occurred_at`, `received_at`, `source`, `event_type`, `source_ip`, `destination_ip`, `destination_port`, `protocol`, `principal`, `action`, `outcome`, `attributes_json`.

Partition by calendar month of `occurred_at` and order primarily by `(tenant_id, occurred_at, event_id)` or a benchmark-justified query ordering. TTL configurable (local demo: 7 days). Ensure filters always include tenant scope, cap query ranges, and impose result limits.

### 7.2 Duplicate-safe ingestion

Kafka and ClickHouse do **not** guarantee arbitrary end-to-end exactly-once writes. Choose and document a practical dedupe pattern. Suggested v1 design: stable `event_id`, consumer batching and deterministic batch tokens where applicable, plus deduplication in read queries/tests; if using ReplacingMergeTree, explain that background merges are asynchronous and ensure user-visible queries are correct before merges. Benchmark the performance cost of deduplication. No claim of strict uniqueness unless enforced and tested.

### 7.3 Alert lifecycle persistence

**Required v1 choice:** Store immutable alert detections in ClickHouse, and store mutable workflow state (`status`, `assignee`, `updated_at`, version) and audit entries in **PostgreSQL** with unique `alert_id` constraints. Add PostgreSQL to the infrastructure stack for this purpose. Query API joins these datasets at application level after tenant scope and pagination. For the MVP, an alternative all-PostgreSQL alert store is also acceptable if explicitly chosen in the architecture decision record; the chosen single source of truth must be consistent.

Use database transactions and optimistic version checks on state transitions. Define behavior if a detection event arrives before or after its workflow-row creation, and on duplicates.

### 7.4 Redis

Use Redis for per-producer token-bucket rate limiting and optional ephemeral cache. Do not use non-durable Redis-only counters as the *sole* source of truth for recoverable critical detections. Redis outages should degrade intake predictably (configured fail-closed or fail-open, documented) rather than silently bypass controls.

---

## 8. HTTP API Contract (v1)

Prefix all public endpoints with `/api/v1`. Publish and validate an OpenAPI 3.1 document. JSON errors use `{ "error": { "code": "...", "message": "...", "request_id": "..." } }`.

| Method | Route | Description |
|---|---|---|
| `POST` | `/api/v1/events:batch` | Authenticate and publish up to 500 events; max payload 1 MiB |
| `GET` | `/api/v1/events` | Filter by time, source IP, type, outcome; cursor pagination |
| `GET` | `/api/v1/events/{event_id}` | Retrieve one event scoped to tenant |
| `GET` | `/api/v1/alerts` | Filter by severity/status/rule/time; cursor pagination |
| `GET` | `/api/v1/alerts/{alert_id}` | Details, evidence, history |
| `PATCH` | `/api/v1/alerts/{alert_id}` | Change status (`open`, `acknowledged`, `resolved`) with version check |
| `GET` | `/api/v1/alerts/stream` | SSE for new alerts, with reconnect behavior |
| `GET` | `/api/v1/rules` | Read-only active rule definitions |
| `GET` | `/api/v1/metrics/summary` | Rollup stats for dashboard |
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Readiness: required dependencies |
| `GET` | `/metrics` | Prometheus metrics (internal only) |

**Ingestion response semantics:** `202 Accepted` only after Kafka confirms the publish, with `accepted_count`, `rejected_count`, `event_ids`, and `request_id`. For mixed batches, explicitly specify atomic all-or-nothing (recommended for v1) rather than ambiguously partially accepting. Reject invalid batches with `400`, unauthenticated with `401`, forbidden tenant with `403`, too large with `413`, rate limited with `429` and `Retry-After`, dependency unavailable with `503`. `202` means Kafka-acknowledged, **not** persisted into ClickHouse or detected.

**Pagination:** deterministic `(occurred_at, event_id)` cursor for event history and `(created_at, alert_id)` for alerts; response includes `items`, `next_cursor`, and `has_more`. All timestamp filters use UTC and cap range by configuration.

**SSE:** authenticate, support `Last-Event-ID` or explicit documented reconnection + REST backfill, send keep-alives, use bounded per-client buffers and drop/reconnect slow clients, avoid embedding tokens in URL query parameters.

**API compatibility:** schema versioning, typed input/output contracts, tests for invalid/boundary requests, no direct SQL string interpolation from user input.

---

## 9. Web Dashboard Requirements

Pages/routes:

1. **Overview (`/`)** — event rate, number of open alerts, severities, 24-hour trend, top rules, consumer lag (operator view).
2. **Alerts (`/alerts`)** — live feed, severity/status filters, search, pagination, detail drawer or page, acknowledge/resolve controls.
3. **Events (`/events`)** — timestamp range, IP, type, outcome search, event detail JSON with safe rendering.
4. **Rules (`/rules`)** — read-only descriptions, thresholds, enabled/disabled display.
5. **System (`/system`)** — pipeline/consumer health, ingestion throughput, detection lag, errors (may link to Grafana).

Use React Query or equivalent for REST data, SSE for new alerts, a typed API client based on OpenAPI, and states for loading/empty/error/reconnect. Debounce searches and prevent unbounded page loads. Use responsive design, accessible labels and keyboard-operable controls. Never trust or dangerously render arbitrary event attributes.

---

## 10. Security Requirements

- Production-mode ingress uses TLS at the edge; development may use local HTTP with explicit documentation.
- Generator/client uses scoped API keys (hashed at rest) or JWT credentials. Dashboard uses secure session/JWT flow; no credentials committed to repository.
- All operations resolve `tenant_id` from the verified identity, **not only** from body/query parameters; reject cross-tenant IDs.
- Roles: `viewer`, `analyst`, `admin`. Viewer reads; analyst triages; admin manages keys/config. Enforce authorization server-side.
- Rate limit and max batch/payload limits; bounded JSON parsing; safe client timeouts.
- Parameterized SQL, escape untrusted UI text, configure CORS narrowly, do not expose Prometheus/ClickHouse/Redis/Kafka directly to public networks.
- `.env.example` with safe placeholders; use GitHub Actions secret scanning / dependency scanning where available.
- Audit: producer authentication errors (without secrets), alert status transitions, role-sensitive changes.
- Synthetic datasets use reserved documentation IP ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`) and fake identities.

---

## 11. Reliability, Backpressure and Observability

### 11.1 Failure handling

- Retry transient Kafka/ClickHouse failures with exponential backoff + jitter, bounded queues and bounded retries, and clear terminal handling.
- Do not acknowledge/commit Kafka offsets for messages whose required effects are incomplete.
- Send permanently unprocessable records to DLQ with reason, redacted context, original topic/partition/offset; then commit the originating offset after successful DLQ publish.
- Avoid unbounded in-memory buffering: shed HTTP load with `429` or `503`, cap Kafka producer queue depth, cap ClickHouse batch size, cap detection state per key, and alert on capacity limits.
- Graceful shutdown must stop accepting new work, finish/abort in-flight work within deadline, and commit only completed offsets.
- Health readiness checks reflect actual service capability, not just an HTTP server being alive.

### 11.2 Required metrics

- `sentinel_ingest_events_total{result}`
- `sentinel_ingest_requests_total{status}`
- `sentinel_ingest_request_duration_seconds` (histogram)
- `sentinel_kafka_consumer_lag{group,topic,partition}` (bounded labels)
- `sentinel_detection_alerts_total{rule_id,severity}`
- `sentinel_detection_delay_seconds` (histogram: detection time minus event time)
- `sentinel_clickhouse_write_duration_seconds` (histogram)
- `sentinel_dlq_events_total{reason}`
- `sentinel_sse_clients` (gauge)

Avoid high-cardinality labels such as IP, user, event ID, request ID, or tenant ID in Prometheus metrics. Include dashboards and alert examples for sustained consumer lag, ingest error rate, write errors, and restart loops.

### 11.3 Tracing/logging

JSON logs with `service`, `timestamp`, `level`, `request_id`, `trace_id`; OTEL spans around ingestion, publishing, detection, and storage where practical. Scrub sensitive fields and use configurable sampling.

---

## 12. Nonfunctional Targets and Measurement

These are **engineering targets to verify**, not promised performance.

| Attribute | Initial acceptance target / measurement |
|---|---|
| Local functionality | Compose demo boots and verifies an event-to-alert path |
| Synthetic scale | Generate and account for at least 1,000,000 events in a benchmark run (not necessarily 1M/sec) |
| Ingest throughput | Report sustained accepted events/sec at p50/p95, with hardware and request size |
| Detection latency | Measure p50/p95/p99 event-time-to-alert persistence and UI notification |
| API responsiveness | Record p95 for key filtered endpoints with defined dataset and concurrency |
| Recovery | Process a worker restart without unacknowledged-event loss under tested assumptions |
| Duplicate tolerance | Same replayed input does not create multiple visible alerts for one dedupe key |
| Overload | Bounded memory/queues and expected `429`/`503` under excess load |
| Deployment | Runs in Compose and a documented local K8s environment (kind/minikube) |
| CI | Lint/unit/integration/build checks pass on clean checkout |

**Benchmark output** (`benchmarks/results/<date>-<scenario>.md`): machine CPU/RAM/disk, Kafka partitions and replication, number of replicas, Docker/K8s versions, event size distribution, total events, run duration, error/loss/duplicate counts, input rate, sustained rate, p50/p95/p99 latency, memory/CPU, consumer lag, screenshots or dashboard links, limitations. Supply a Python `bench/report.py` script generating a Markdown/JSON summary from raw measurements.

---

## 13. Project Layout

```text
sentinel/
├── spec.md
├── README.md
├── Makefile
├── .env.example
├── .github/workflows/ci.yml
├── api/openapi.yaml
├── config/rules.yaml
├── cmd/
│   ├── ingest/main.go
│   ├── detector/main.go
│   ├── event-writer/main.go
│   ├── alert-writer/main.go
│   └── query-api/main.go
├── internal/
│   ├── auth/
│   ├── config/
│   ├── contracts/
│   ├── ingest/
│   ├── kafka/
│   ├── detection/
│   ├── storage/clickhouse/
│   ├── storage/postgres/
│   ├── api/
│   └── telemetry/
├── migrations/
│   ├── clickhouse/
│   └── postgres/
├── web/
│   ├── src/
│   └── package.json
├── generator/
│   ├── requirements.txt
│   ├── generate.py
│   └── scenarios/
├── deployments/
│   ├── compose/docker-compose.yml
│   └── k8s/
├── observability/
│   ├── prometheus.yml
│   └── grafana/
├── tests/
│   ├── contracts/
│   ├── integration/
│   └── chaos/
├── benchmarks/
│   ├── README.md
│   └── results/
└── docs/
    ├── architecture.md
    ├── decisions/
    ├── operations.md
    ├── threat-model.md
    └── demo.md
```

---

## 14. Implementation Phases (GSD/Codex Execution Plan)

Implement **in this order** and finish acceptance checks before moving on. Each phase must leave the repo runnable and tests passing. Do not build all infrastructure abstractions before an end-to-end vertical slice.

### Phase 1 — Foundations and Contracts

**Deliverables:** repo structure, Go module, React scaffold, Python generator skeleton, Compose for Kafka/ClickHouse/Redis/PostgreSQL, migrations, OpenAPI skeleton, event and alert schemas, pinned dependencies, Makefile, CI skeleton.

**Exit checks:** `make dev-up`, `make test`, `make lint` run; infrastructure healthy; contract/schema tests verify valid/invalid examples; no committed secrets.

### Phase 2 — Ingestion and Durable Event Storage

**Deliverables:** authenticated batch endpoint, strict validation, Kafka producer, partition key, consumer event-writer, ClickHouse schema, basic events query, synthetic generator.

**Exit checks:** Submit 1000 demo events and query them; invalid/over-limit/unauthorized requests fail correctly; producer only returns `202` after Kafka ack; crash-replay and duplicate-event behavior is tested/documented.

### Phase 3 — Detection and Alert Pipeline

**Deliverables:** four detection rules, partition-aware keyed window state, Kafka alerts topic, alert writer, PostgreSQL workflow table, severity/evidence, DLQ handling, deterministic alert IDs.

**Exit checks:** Seeded fixtures trigger each rule exactly as expected; benign fixtures avoid alerts; boundary/lateness tests pass; restart/replay does not create duplicate visible alerts; simulate failure between alert publish and offset commit.

### Phase 4 — Query API and Analyst Dashboard

**Deliverables:** events/alerts endpoints, cursor pagination, tenant authorization, triage PATCH with audit, alert detail/evidence, SSE, React overview, alerts, events and rules pages.

**Exit checks:** Analyst can see synthetic detections, filter/open evidence and acknowledge/resolve with audit; viewer cannot mutate; two demo tenants cannot read each other's data; SSE reconnection and REST backfill work.

### Phase 5 — Observability, Resilience and Scale

**Deliverables:** metrics, structured logs, Prometheus/Grafana panels, bounded queues/backoff, lag monitoring, multi-replica deployment, load tests, replay/restart/overload test suites, benchmark report.

**Exit checks:** Reproduce >=1M generated events with accounting; publish actual sustained throughput/latency; demonstrate restart recovery and backpressure; measure duplicate and loss characteristics; document limitations.

### Phase 6 — Kubernetes, Hardening, Documentation

**Deliverables:** K8s manifests/Helm, readiness/liveness, resource limits, secrets/config, hardened CI, diagrams, threat model, operation runbook, architecture decisions, recorded demo instructions, portfolio-ready README.

**Exit checks:** Clean checkout passes CI; kind/minikube deployment works per instructions; demo scenario completes; full security checklist reviewed; metrics/benchmarks and known tradeoffs documented.

### Optional Phase 7 — Aegis Integration and ML

- Add a versioned Aegis audit-log adapter that maps its events into the canonical Sentinel format with explicit schema tests.
- Add a Python inference microservice for anomaly *scoring* only after the rule-based system is stable; use a reproducible dataset, offline training, evaluation (precision/recall, false-positive rate), versioned model, and CPU-only inference benchmark.
- Preserve a fully functional system without the ML service.

---

## 15. Testing Strategy

**Unit tests:** Go rule engine, window boundaries, severity, normalization, auth, rate limiter, cursor encoders, idempotency keys, validation. Python generator seed reproducibility. React components and API error states.

**Contract tests:** JSON/OpenAPI compatibility, schema versions, missing/invalid fields, event-time skew and IPv4/IPv6 parsing, API status/error payloads.

**Integration tests:** Real Kafka/ClickHouse/PostgreSQL/Redis via Compose/test containers (mock-only testing is insufficient). Validate event-to-storage, alert-to-storage, filtered search, triage persistence, tenant separation.

**Failure tests:** terminate detector before/after Kafka alert publish and before source offset commit; restart event writer mid-batch; restart Kafka broker in dev; ClickHouse temporarily unreachable; slow consumer; duplicate events; malformed messages to DLQ; UI SSE disconnect/reconnect; rate-limit overload.

**Load tests:** seeded mix of benign traffic and known malicious patterns; track expected rule hits, accepted events, stored/deduped events, backlog, event/drop counts, rate limit responses, percentiles. Distinguish client-send count from Kafka-accepted and persisted counts.

**CI required checks:** Go formatting/lint/vet/unit/race tests (race tests can be separate), frontend typecheck/lint/tests/build, Python tests/lint, schema validation, dependency/security scanning where available, integration smoke suite with bounded timeout. Enforce failures on nonzero test exit codes; no unconditional `|| true`.

---

## 16. Local Developer Workflow

Document runnable `make` targets (names may map to scripts):

```bash
make dev-up        # start infrastructure and services
make migrate       # apply idempotent database migrations
make seed          # publish deterministic benign + threat demo fixtures
make test          # run fast unit and schema tests
make test-integ    # run real integration tests against Compose
make lint          # lint and typecheck all languages
make bench         # execute a bounded, configurable synthetic load scenario
make dev-down      # stop services, preserve data unless explicit reset
make reset         # explicit destructive local-only cleanup
```

`README.md` must include prerequisites, exact version matrix, ports, `.env.example` setup, start/stop commands, API usage with `curl`, demo login/keys (development-only), dashboard URL, common troubleshooting, known limitations, architecture diagram and benchmark reproduction.

---

## 17. Sample Demonstration Script

1. Start with `make dev-up` and verify service readiness.
2. Open React dashboard and display event/alert counts.
3. Run `make seed` and show ordinary network activity entering ClickHouse.
4. Inject a seeded authentication-failure burst and port scan using generator scenario flags.
5. Observe generated alerts appearing live via SSE; open an alert to inspect exact rule and evidence.
6. Acknowledge, then resolve an alert; show audit trail and role restrictions.
7. Stop/restart one detector replica and demonstrate consumer backlog recovery.
8. Run bounded performance scenario and open Grafana lag/rate/latency panels.
9. Show real benchmark report and explain at-least-once, idempotency, and the scaling bottleneck.

---

## 18. Definition of Done

A phase is **not done** because code exists or the dev server starts. It is done when:

- Its stated user-visible workflow completes end-to-end.
- Automated tests covering success, invalid input, and relevant failure behavior pass.
- All necessary components run from documented commands in a clean environment.
- Security controls and tenant checks have tests, not just comments.
- No stub endpoint, mock-only adapter, `TODO` core logic, fake analytics value, or hardcoded frontend sample stands in for a required feature.
- CI is green and code has meaningful errors, logs, instrumentation, and configuration.
- Performance numbers are measured, not invented, and failure caveats are documented.
- The README and architecture docs reflect the implementation rather than an intended future state.

**Final project acceptance:** A reviewer can clone the repository, run documented commands, generate events, observe rule-based alerts, search the underlying evidence, triage alert statuses, induce and recover from a worker failure, and reproduce the benchmark and test outputs.

---

## 19. Instructions to an Autonomous Coding Agent

When using GSD, Codex, or another coding agent:

1. Treat this file as the source of product intent, not as proof that functionality exists.
2. Write an implementation plan per phase listing files, schemas, edge cases, test cases, and measurable exit checks.
3. Work in vertical slices; do not implement later-phase complexity at the expense of a runnable MVP.
4. When a design decision is ambiguous, select the simplest consistent approach, record it in `docs/decisions/`, and update this spec if it changes an interface.
5. Before marking a phase complete, run its checks and report exact commands/results; if dependencies prevent tests, mark the phase blocked or incomplete.
6. Keep API contracts, event schemas, migrations, UI and integration tests aligned.
7. Never invent test successes, benchmark results, production guarantees, or security claims.
8. Do not introduce unrelated features or extra databases/services without a documented reason.
9. Design for educational, authorized synthetic security monitoring only.
10. Update `README.md` and `docs/operations.md` alongside operational changes.

**Suggested first action:** Implement **Phase 1**, then confirm that a clean checkout can start all local dependencies and pass schema/CI checks before advancing.
