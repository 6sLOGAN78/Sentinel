# Requirements: Sentinel — Real-Time Threat Detection Platform

**Defined:** 2026-10-10
**Core Value:** The end-to-end flow (generator → ingest → Kafka → detector → ClickHouse/PostgreSQL → API → live dashboard) works and is provably correct under failure. It produces deterministic, deduplicated, evidence-backed alerts, recovers from worker crashes without losing acknowledged events, and backs every performance claim with a reproducible benchmark.

**Sources:** `spec.md` v1.0 (FR-01..FR-18, §5–§18) and `.planning/research/SUMMARY.md`. Spec FR IDs are given in brackets for traceability.

## v1 Requirements

### Foundations (FND)

- [x] **FND-01**: A developer can run `make dev-up` from a clean checkout and get healthy Kafka (KRaft), ClickHouse, PostgreSQL and Redis via Docker Compose, with topics created explicitly (auto-create off, defined partition counts) [FR-13]
- [x] **FND-02**: A developer can run `make migrate`, `make test`, `make lint`, `make dev-down` and `make reset` with documented, idempotent behavior [FR-13, §16]
- [x] **FND-03**: The repo is restructured as one Go module (`backend/`, Go 1.26.x) with five binaries (`ingest`, `detector`, `event-writer`, `alert-writer`, `query-api`). Non-spec boilerplate (Clerk, asynq, Resend/emails, New Relic, Turborepo/zod/ts-rest packages, Taskfile) is removed, and the known boilerplate defects are fixed (Recover order, Shutdown nil pointer, ignored Redis ping failure, DSN escaping) [§2, §13]
- [x] **FND-04**: Event (`security.events.v1`) and alert (`security.alerts.v1`) schemas exist as versioned JSON Schema plus Go types, with one canonical `dedupe_key` → SHA-256 `alert_id` function, and contract tests prove valid and invalid fixtures (including IPv4/IPv6 and timestamp skew) [§6, §15]
- [x] **FND-05**: An OpenAPI 3.1 document (`api/openapi.yaml`) describes every v1 endpoint, the error envelope and the pagination shape, and is validated in CI [§8]
- [x] **FND-06**: All dependency and container image versions are pinned (go.sum, web lockfile, generator lockfile, image tags), and `.env.example` contains only safe placeholders with no committed secrets [§2, §10]
- [x] **FND-07**: Architecture decisions are recorded in `docs/decisions/`: repo layout, event_id policy, atomic-batch semantics, metrics-label policy and Redis fail-closed/open policy [§19]

### Ingestion (ING)

- [x] **ING-01**: A producer authenticated with a scoped API key (hashed at rest) can POST up to 500 events / 1 MiB to `/api/v1/events:batch`. The tenant is resolved from the key, never from the body alone [FR-01, §10]
- [x] **ING-02**: Invalid batches are rejected all-or-nothing with 400 and a structured error. Checks cover required fields, unknown root fields, enums, IPs, attribute size and occurred_at skew [FR-01, §6.1]
- [x] **ING-03**: The producer receives `202 Accepted` with `accepted_count`, `rejected_count`, `event_ids` and `request_id` only after Kafka acknowledges every record (acks=all, idempotent producer, key `tenant_id|source_ip`) [FR-02, §8]
- [x] **ING-04**: Retrying a batch after a 5xx does not create duplicate stored events, because the event_id is client-supplied or derived deterministically [FR-02, §7.2]
- [x] **ING-05**: Unauthenticated requests get 401, cross-tenant requests 403, oversized payloads 413, and an unavailable Kafka gets 503, all in the JSON error envelope [§8]
- [x] **ING-06**: A producer exceeding its quota gets 429 with `Retry-After` from a Redis token bucket. When Redis is down, intake follows the documented fail-closed (503) or fail-open policy and never silently bypasses limits [FR-12, §7.4]
- [x] **ING-07**: The HTTP layer sheds load (429/503) instead of buffering without bound, and the Kafka producer buffer is capped [§11.1]

### Event Storage (STOR)

- [x] **STOR-01**: An event-writer consumer batches events from Kafka into the ClickHouse events table and commits offsets only after a successful insert [FR-03, §3]
- [x] **STOR-02**: The ClickHouse events table is partitioned by month of occurred_at, ordered by `(tenant_id, occurred_at, event_id)` or a benchmark-justified order, and has a configurable TTL (local default 7 days) [§7.1]
- [x] **STOR-03**: Replaying the same events never yields duplicate rows in user-visible queries or counts. Dedup is enforced at read time and tested immediately after a double insert, with no reliance on background merges [FR-03, §7.2]
- [x] **STOR-04**: Permanently invalid or poison messages go to `security.events.dlq.v1` with reason, redacted context and original topic/partition/offset, and the source offset is committed only after the DLQ publish succeeds [§6.3, §11.1]

### Detection (DET)

- [ ] **DET-01**: The detector evaluates AUTH_BRUTE_FORCE (≥10 failures / 5 min, by tenant+source_ip+principal) on event time [FR-04]
- [ ] **DET-02**: The detector evaluates PORT_SCAN (≥20 distinct ports / 60 s, by tenant+source_ip+destination_ip) on event time [FR-04]
- [ ] **DET-03**: The detector evaluates REQUEST_FLOOD (≥300 requests / 60 s, by tenant+source_ip) on event time [FR-04]
- [ ] **DET-04**: The detector evaluates DENY_SPIKE (≥30 denies / 2 min, by tenant+source_ip) on event time [FR-04]
- [ ] **DET-05**: Rule thresholds, windows, severities and cooldowns load from versioned `config/rules.yaml` at startup, and changing them requires a restart (documented) [§5.1]
- [ ] **DET-06**: The window model is documented in an ADR and tested: event-time windows with half-open boundaries, a per-partition watermark, a ~30 s lateness tolerance, deterministic late-event handling with a metric, and bounded per-key state [§5.1]
- [ ] **DET-07**: Each rule hit produces one alert per dedupe key and window, carrying severity, summary, event_count, window start/end, at most 20 sample event IDs, rule_id/rule_version and a deterministic SHA-256 alert_id. created_at is derived from event time [FR-05, §6.2]
- [ ] **DET-08**: Per-rule cooldown suppresses repeat alerts for the same entity, while separate windows remain investigable [§5.1]
- [ ] **DET-09**: Seeded malicious fixtures trigger each rule exactly as expected, benign fixtures produce no alerts, and boundary/lateness fixtures behave as specified [§14 Phase 3]

### Alert Pipeline (ALRT)

- [ ] **ALRT-01**: Alerts are published to `security.alerts.v1` (key `tenant_id|alert_id`) and the alert-writer persists immutable detections to ClickHouse [FR-05]
- [ ] **ALRT-02**: The alert-writer creates the mutable workflow row (status, assignee, version, seq, denormalized list fields) in PostgreSQL with a unique alert_id. A replayed detection never reopens or duplicates an existing alert [FR-05, §7.3]
- [ ] **ALRT-03**: The detector rebuilds window state after restart or rebalance by replaying from a committed state-pin offset. Killing the detector mid-window (e.g. after 9 of 10 failures) still produces the alert once the 10th event arrives [FR-11, §5.1]
- [ ] **ALRT-04**: A crash at each boundary (before alert publish, between publish and offset commit, after commit) is tested with failpoints and yields exactly one visible alert per dedupe key [FR-11, §14 Phase 3]

### Authentication and Access (AUTH)

- [ ] **AUTH-01**: Dashboard users sign in with a development-safe JWT/session flow, and no credentials are committed [§10]
- [ ] **AUTH-02**: The server enforces viewer/analyst/admin roles: viewers read, analysts triage, admins manage config. A viewer's mutation attempt is rejected with 403 [§10]
- [ ] **AUTH-03**: Every query is scoped to the tenant from the verified identity. Two demo tenants cannot read or mutate each other's events or alerts (cross-tenant test matrix) [§10, §14 Phase 4]
- [ ] **AUTH-04**: Producer authentication failures and role-sensitive actions are audit-logged without secrets [§10]

### Query API (API)

- [ ] **API-01**: An analyst can list events filtered by time range (capped), source IP, type and outcome, with deterministic `(occurred_at, event_id)` cursor pagination returning `items`, `next_cursor` and `has_more` [FR-06, §8]
- [ ] **API-02**: An analyst can fetch a single event by ID within their tenant [FR-06]
- [ ] **API-03**: An analyst can list alerts filtered by severity, status, rule and time, with `(created_at, alert_id)` cursor pagination that returns full pages under status filters [FR-06, §8]
- [ ] **API-04**: An analyst can open alert details including evidence (sample events), rule explanation and status history [FR-06, §8]
- [ ] **API-05**: An analyst can PATCH alert status (open → acknowledged → resolved) with an optimistic version check. A stale version returns a conflict, and every transition writes an audit row (actor, time, from/to) in the same transaction [FR-08, §7.3]
- [ ] **API-06**: Anyone authenticated can read the active rule definitions (thresholds, windows, enabled state) via `GET /api/v1/rules` [§8]
- [ ] **API-07**: The dashboard can fetch rollup stats (event rate, open alerts by severity, top rules, 24 h trend) via `GET /api/v1/metrics/summary`, computed from real data [§8]
- [ ] **API-08**: All SQL is parameterized, list endpoints enforce result limits and range caps, and invalid or boundary requests are covered by tests [§8]

### Live Alerts (LIVE)

- [ ] **LIVE-01**: An authenticated client receives new alerts over SSE at `/api/v1/alerts/stream` without tokens in URL query parameters [FR-07, §8]
- [ ] **LIVE-02**: After a disconnect, the client reconnects with Last-Event-ID (or a documented cursor) and backfills missed alerts via REST with no permanent gaps [FR-07, §8]
- [ ] **LIVE-03**: The SSE server sends keep-alives, uses bounded per-client buffers and drops slow clients, and every query-api replica delivers every alert [§8]

### Dashboard (UI)

- [ ] **UI-01**: An analyst sees the Overview page (`/`): event rate, open alert count, severity breakdown, 24 h trend, top rules and consumer lag [§9]
- [ ] **UI-02**: An analyst sees the Alerts page (`/alerts`): a live SSE feed, severity/status filters, search, pagination, a detail drawer with evidence and history, and acknowledge/resolve controls (hidden or disabled for viewers) [§9, FR-07, FR-08]
- [ ] **UI-03**: An analyst sees the Events page (`/events`): time range, IP, type and outcome search, plus event detail JSON rendered safely (no unsafe HTML) [§9]
- [ ] **UI-04**: An analyst sees the Rules page (`/rules`): read-only descriptions, thresholds and enabled/disabled state [§9]
- [ ] **UI-05**: An operator sees the System page (`/system`): pipeline and consumer health, ingest throughput, detection lag and errors, optionally linking to Grafana [§9]
- [ ] **UI-06**: The UI uses a typed client generated from OpenAPI and React Query, handles loading, empty, error and reconnecting states, debounces searches, prevents unbounded page loads, and has accessible labels and keyboard-operable controls [§9]

### Generator (GEN)

- [x] **GEN-01**: A tester can run a seeded Python generator that produces reproducible benign traffic using only reserved documentation IP ranges and pseudonymous principals [FR-09, §10]
- [ ] **GEN-02**: A tester can inject scenarios (auth brute force, port scan, request flood, deny spike) via generator flags, with expected rule hits recorded [FR-09, §17]
- [x] **GEN-03**: `make seed` publishes deterministic benign plus threat demo fixtures through the ingest API [§16]

### Observability (OBS)

- [x] **OBS-01**: Every service exposes `/healthz` (liveness) and `/readyz`, which reflects real dependency capability (Kafka, ClickHouse, Postgres, Redis as applicable) [FR-10, §11.1]
- [ ] **OBS-02**: Services expose the spec-required Prometheus metrics (ingest events/requests/duration, consumer lag, detection alerts, detection delay, ClickHouse write duration, DLQ events, SSE clients) on an internal `/metrics` with no high-cardinality labels. Consumer lag is position-based [FR-10, §11.2]
- [ ] **OBS-03**: Services emit JSON logs with service, timestamp, level, request_id and trace_id, scrub sensitive fields, and never log secrets or request bodies [§11.3]
- [ ] **OBS-04**: OpenTelemetry spans cover ingestion, publishing, detection and storage, propagate through Kafka headers, and use configurable sampling [§11.3]
- [ ] **OBS-05**: Grafana dashboards (as code) show ingest rate, error rates, consumer lag, detection delay, write latency and DLQ. Prometheus alert examples cover sustained lag, ingest error rate, write errors and restart loops [FR-15, §11.2]

### Resilience (REL)

- [ ] **REL-01**: Transient Kafka/ClickHouse failures are retried with exponential backoff, jitter and bounded retries, with clear terminal handling [§11.1]
- [ ] **REL-02**: Each service shuts down gracefully: it stops accepting work, finishes or aborts in-flight work within a deadline, and commits only completed offsets [§11.1]
- [ ] **REL-03**: Failure test suites cover event-writer restart mid-batch, temporarily unreachable ClickHouse, Kafka broker restart in dev, slow consumers, malformed messages to the DLQ, SSE disconnect/reconnect and rate-limit overload [§15]
- [ ] **REL-04**: Under excess load, memory and queues stay bounded and clients receive the expected 429/503 [§12]

### Scale and Benchmarks (BENCH)

- [ ] **BENCH-01**: A tester can run `make bench` for a bounded, configurable multi-replica load scenario (open-loop generator with benign plus malicious mix) [FR-16, §16]
- [ ] **BENCH-02**: A benchmark run with ≥1,000,000 generated events reconciles client-sent, Kafka-accepted, rejected and unique-persisted counts, plus expected vs observed rule hits [FR-16, §12]
- [ ] **BENCH-03**: `bench/report.py` writes `benchmarks/results/<date>-<scenario>.md` with hardware, configuration, sustained throughput, p50/p95/p99 ingest and detection latency, API p95, resources, lag, loss/duplicate counts and limitations, and refuses to emit a report when the counts don't reconcile [FR-16, §12]
- [ ] **BENCH-04**: A detector replica restart under load is demonstrated to recover the backlog without losing acknowledged events, and the measured recovery time is reported [§12, §17]

### Deployment and Docs (DEP)

- [ ] **DEP-01**: Kubernetes manifests (Kustomize) with a kind/minikube overlay deploy all services with liveness/readiness probes, resource limits and secrets/config separation [FR-14]
- [ ] **DEP-02**: The K8s docs explain multi-broker Kafka replication (RF, min ISR) and the services are not exposed publicly (Prometheus, ClickHouse, Redis, Kafka) [§6.3, §10]
- [ ] **DEP-03**: GitHub Actions CI runs Go fmt/lint/vet/unit/race tests, frontend typecheck/lint/test/build, Python tests/lint, schema validation, dependency/secret scanning, and a bounded integration smoke suite against real dependencies, with no `|| true` [§15]
- [ ] **DEP-04**: README, `docs/architecture.md`, `docs/operations.md`, `docs/threat-model.md` and `docs/demo.md` reflect the actual implementation, including version matrix, ports, curl examples, demo credentials (dev only), troubleshooting, limitations and benchmark reproduction [§16, §18]
- [ ] **DEP-05**: The spec §17 demo script completes end-to-end from a clean checkout [§17, §18]

## v2 Requirements

Deferred. Tracked but not in the current roadmap.

### Integrations

- **INTG-01**: Versioned Aegis audit-log adapter mapping into the canonical event format, with schema tests [FR-17]

### ML Scoring

- **ML-01**: Offline-trained Python anomaly scorer with reproducible dataset, evaluation (precision/recall, FPR), versioned model and CPU inference benchmark, behind the same alert interface [FR-18]

### Triage Extensions

- **TRI-01**: Assign alerts to an analyst via API (`assignee` is stored but read-only in v1)
- **TRI-02**: Admin API for producer key management (v1 provisions keys via seed/CLI)
- **TRI-03**: Rule hot-reload or rule CRUD (v1 requires a restart)

## Out of Scope

| Feature | Reason |
|---------|--------|
| Inline blocking or enforcement | Sentinel is detection-only (spec §1.2) |
| Real packet capture, TLS decryption, endpoint agents | Synthetic and simulated sources only |
| Autonomous remediation or offensive automation | Educational, authorized monitoring only |
| SIEM parity, multi-region active-active, per-tenant billing | Not a v1 portfolio goal |
| Arbitrary SQL from the UI or user-defined executable rules | Security risk; rules are versioned YAML |
| Exactly-once end-to-end claims | The system is at-least-once with idempotent effects (spec §3) |
| Sigma rule import, correlation, enrichment, case management, notification channels | Beyond spec scope; would dilute the core pipeline |
| Throughput or latency numbers without a linked benchmark result | Claim discipline (spec §1, §19.7) |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| FND-01 | Phase 1 | Complete |
| FND-02 | Phase 1 | Complete |
| FND-03 | Phase 1 | Complete |
| FND-04 | Phase 1 | Complete |
| FND-05 | Phase 1 | Complete |
| FND-06 | Phase 1 | Complete |
| FND-07 | Phase 1 | Complete |
| ING-01 | Phase 2 | Complete |
| ING-02 | Phase 2 | Complete |
| ING-03 | Phase 2 | Complete |
| ING-04 | Phase 2 | Complete |
| ING-05 | Phase 2 | Complete |
| ING-06 | Phase 2 | Complete |
| ING-07 | Phase 2 | Complete |
| STOR-01 | Phase 2 | Complete |
| STOR-02 | Phase 2 | Complete |
| STOR-03 | Phase 2 | Complete |
| STOR-04 | Phase 2 | Complete |
| GEN-01 | Phase 2 | Complete |
| GEN-03 | Phase 2 | Complete |
| OBS-01 | Phase 2 | Complete |
| DET-01 | Phase 3 | Pending |
| DET-02 | Phase 3 | Pending |
| DET-03 | Phase 3 | Pending |
| DET-04 | Phase 3 | Pending |
| DET-05 | Phase 3 | Pending |
| DET-06 | Phase 3 | Pending |
| DET-07 | Phase 3 | Pending |
| DET-08 | Phase 3 | Pending |
| DET-09 | Phase 3 | Pending |
| ALRT-01 | Phase 3 | Pending |
| ALRT-02 | Phase 3 | Pending |
| ALRT-03 | Phase 3 | Pending |
| ALRT-04 | Phase 3 | Pending |
| GEN-02 | Phase 3 | Pending |
| AUTH-01 | Phase 4 | Pending |
| AUTH-02 | Phase 4 | Pending |
| AUTH-03 | Phase 4 | Pending |
| AUTH-04 | Phase 4 | Pending |
| API-01 | Phase 4 | Pending |
| API-02 | Phase 4 | Pending |
| API-03 | Phase 4 | Pending |
| API-04 | Phase 4 | Pending |
| API-05 | Phase 4 | Pending |
| API-06 | Phase 4 | Pending |
| API-07 | Phase 4 | Pending |
| API-08 | Phase 4 | Pending |
| LIVE-01 | Phase 4 | Pending |
| LIVE-02 | Phase 4 | Pending |
| LIVE-03 | Phase 4 | Pending |
| UI-01 | Phase 4 | Pending |
| UI-02 | Phase 4 | Pending |
| UI-03 | Phase 4 | Pending |
| UI-04 | Phase 4 | Pending |
| UI-06 | Phase 4 | Pending |
| OBS-02 | Phase 5 | Pending |
| OBS-03 | Phase 5 | Pending |
| OBS-04 | Phase 5 | Pending |
| OBS-05 | Phase 5 | Pending |
| REL-01 | Phase 5 | Pending |
| REL-02 | Phase 5 | Pending |
| REL-03 | Phase 5 | Pending |
| REL-04 | Phase 5 | Pending |
| BENCH-01 | Phase 5 | Pending |
| BENCH-02 | Phase 5 | Pending |
| BENCH-03 | Phase 5 | Pending |
| BENCH-04 | Phase 5 | Pending |
| UI-05 | Phase 5 | Pending |
| DEP-01 | Phase 6 | Pending |
| DEP-02 | Phase 6 | Pending |
| DEP-03 | Phase 6 | Pending |
| DEP-04 | Phase 6 | Pending |
| DEP-05 | Phase 6 | Pending |

**Coverage:**
- v1 requirements: 73 total
- Mapped to phases: 73
- Unmapped: 0 ✓

---
*Requirements defined: 2026-10-10*
*Last updated: 2026-10-10 after roadmap creation*
