# Roadmap: Sentinel — Real-Time Threat Detection Platform

## Overview

Sentinel is built as vertical slices following spec §14. Each phase leaves the repo runnable. Phase 1 lays contracts and a clean single-module foundation. Phase 2 makes ingestion durable (HTTP to Kafka to ClickHouse). Phase 3 adds the replay-safe detection and alert pipeline, which carries the core correctness claim. Phase 4 exposes it through a tenant-scoped query API and live React dashboard. Phase 5 proves it with observability, failure tests and a reproducible 1M-event benchmark. Phase 6 ships Kubernetes manifests, hardened CI and documentation. Optional spec Phase 7 (Aegis adapter, ML scorer) is deferred to v2.

**Granularity:** standard (6 phases, derived from spec §14 and confirmed against requirement clusters)

## Phases

- [x] **Phase 1: Foundations & Contracts** - Clean Go module with five binaries, Compose stack, schemas, OpenAPI skeleton, ADRs, CI skeleton (completed 2026-10-11)
- [ ] **Phase 2: Ingestion & Durable Event Storage** - Authenticated, rate-limited batch ingest to Kafka and ClickHouse with read-time dedupe and DLQ
- [ ] **Phase 3: Detection & Alert Pipeline** - Four event-time rules, deterministic alerts, crash-safe recovery, alert persistence
- [ ] **Phase 4: Query API & Analyst Dashboard** - Auth/RBAC, tenant-scoped REST, triage workflow, SSE live alerts, React pages
- [ ] **Phase 5: Observability, Resilience & Scale** - Metrics, tracing, Grafana, failure suites, overload behavior, benchmark report
- [ ] **Phase 6: Kubernetes, Hardening & Docs** - Kustomize manifests, hardened CI, threat model, docs, end-to-end demo

## Phase Details

### Phase 1: Foundations & Contracts

**Goal**: A developer can clone the repo, bring up the full dependency stack with one command, and rely on versioned contracts and decisions that later phases build on
**Mode:** mvp
**Depends on**: Nothing (first phase)
**Requirements**: FND-01, FND-02, FND-03, FND-04, FND-05, FND-06, FND-07
**Success Criteria** (what must be TRUE):

  1. From a clean checkout, `make dev-up` yields healthy Kafka (KRaft), ClickHouse, PostgreSQL and Redis, with topics created explicitly (auto-create off), and `make migrate`, `make test`, `make lint`, `make dev-down` and `make reset` all run idempotently
  2. The repo builds as one Go 1.26.x module with five binaries under `backend/cmd/*`. Non-spec boilerplate is gone and the known defects (Recover order, Shutdown nil pointer, Redis ping failure, DSN escaping) are fixed and tested
  3. Valid and invalid event/alert fixtures (IPv4/IPv6, timestamp skew) pass or fail contract tests as expected, and one canonical function yields the SHA-256 `alert_id` (fuzzed with `|` in inputs)
  4. `api/openapi.yaml` (3.1) describes every v1 endpoint, error envelope and pagination shape, and CI validates it. The CI skeleton runs lint, test and schema validation on push
  5. Pinned versions and a placeholder-only `.env.example` are in place, and ADRs in `docs/decisions/` record layout, event_id policy, atomic-batch semantics, metrics-label policy and Redis fail-closed/open policy

**Plans**: 13 plans
Plans:
**Wave 1**

- [x] 01-01-PLAN.md — Remove boilerplate, Go 1.26 module, pinned tools, config and logger refactor
- [x] 01-02-PLAN.md — Compose stack, topic-init, Makefile, .env.example, stack and pin check scripts
- [x] 01-05-PLAN.md — Six ADRs, ADR check, architecture doc, readme, placeholders

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 01-03-PLAN.md — Python generator scaffold with seeded deterministic event builder
- [x] 01-04-PLAN.md — Web scaffold (Vite 8, React 19.3, TS 6) with vitest legitimacy checkpoint
- [x] 01-06-PLAN.md — errs envelope, validation, sqlerr, Recover-first middleware, httpx
- [x] 01-07-PLAN.md — database/tern, migrations embed, redisx, ClickHouse runner and chmigrate tool
- [x] 01-08-PLAN.md — Event JSON Schema, Go type, fixtures, skew and attribute validation

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 01-09-PLAN.md — DedupeKey (golden and fuzz), alert schema and Go type
- [x] 01-10-PLAN.md — Container test helpers, app lifecycle, five binaries, lifecycle integration tests

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 01-11-PLAN.md — OpenAPI 3.1 document, validation and drift tests, TS type generation
- [x] 01-13-PLAN.md — Migration, DSN and Redis integration tests against real containers

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 01-12-PLAN.md — CI skeleton, gitleaks, strict go mod tidy, full phase gate

**Research flag**: Needs research. OpenAPI 3.1 Go toolchain (oapi-codegen v2.8.0 / kin-openapi, libopenapi fallback), exact ClickHouse 26.3 image tag and digest, Vite 8 / Vitest 5 / MSW 3 / TypeScript peer compatibility. Compose, Makefile and CI skeleton are standard patterns.
**Note**: DEP-03 (hardened CI) is assigned to Phase 6. The Phase 1 CI skeleton is covered by FND-02 and FND-05.

### Phase 2: Ingestion & Durable Event Storage

**Goal**: A producer with a scoped API key can submit event batches that are durably acknowledged only after Kafka accepts them, and the events end up queryable and duplicate-safe in ClickHouse
**Mode:** mvp
**Depends on**: Phase 1
**Requirements**: ING-01, ING-02, ING-03, ING-04, ING-05, ING-06, ING-07, STOR-01, STOR-02, STOR-03, STOR-04, GEN-01, GEN-03, OBS-01
**Success Criteria** (what must be TRUE):

  1. A producer POSTing a valid batch (≤500 events, ≤1 MiB) with a valid API key gets 202 with `accepted_count`, `event_ids` and `request_id` only after Kafka acks. Invalid batches get 400 all-or-nothing, and 401/403/413/503 each return the JSON error envelope
  2. `make seed` submits the seeded benign demo events (1000) through the ingest API, and they all appear in ClickHouse and in a minimal events lookup
  3. Retrying a batch after a 5xx, or replaying the same events twice, never shows duplicate rows in user-visible queries or counts, asserted immediately after a double insert with no `OPTIMIZE`
  4. A producer over quota gets 429 with `Retry-After`. With Redis down, intake follows the documented policy (503 by default) and never silently bypasses limits. Under excess load the HTTP layer sheds with 429/503 instead of buffering without bound
  5. A poison message lands in `security.events.dlq.v1` with reason and origin offset, and the source offset is committed only after the DLQ publish. Each service's `/readyz` reflects real dependency state

**Plans**: 14 plans across 10 dependency waves


**Wave 1**

- [x] 02-00-PLAN.md — Harden public HTTP defaults and configure listener binds
- [x] 02-01-PLAN.md — Provision scoped producer keys and private reusable demo credentials
- [x] 02-10-PLAN.md — Send deterministic seeded batches with bounded identical-byte retries

**Wave 2** *(blocked on prior dependency waves)*

- [x] 02-02-PLAN.md — Accept authenticated batches only after bounded Kafka acknowledgements

**Wave 3** *(blocked on prior dependency waves)*

- [x] 02-03-PLAN.md — Return indexed atomic rejection details and exact public contracts

**Wave 4** *(blocked on prior dependency waves)*

- [x] 02-04-PLAN.md — Retrieve tenant-scoped events with immediate read-time dedupe

**Wave 5** *(blocked on prior dependency waves)*

- [x] 02-05-PLAN.md — Change event retention idempotently through migration tooling
- [x] 02-06-PLAN.md — Persist events before committing offsets and propagate worker failure

**Wave 6** *(blocked on prior dependency waves)*

- [x] 02-07-PLAN.md — Quarantine poison with acknowledged DLQ effects and safe offsets
- [x] 02-08-PLAN.md — Enforce Redis event quotas and explicit outage policy

**Wave 7** *(blocked on prior dependency waves)*

- [ ] 02-09-PLAN.md — Expose real health/readiness and drain all five services

**Wave 8** *(blocked on prior dependency waves)*

- [ ] 02-11-PLAN.md — Prove timeout, partial-publish retry and writer crash recovery

**Wave 9** *(blocked on prior dependency waves)*

- [ ] 02-12-PLAN.md — Start the healthy local pipeline through pinned Compose apps

**Wave 10** *(blocked on prior dependency waves)*

- [ ] 02-13-PLAN.md — Verify 1000 current-run events and enforce CI coverage guards

**Research**: Complete (02-RESEARCH.md, 2026-10-12); plan verification passed (02-PLAN-CHECK.md). Read-time dedupe, native ClickHouse, Kafka timeout/manual commits and Redis Lua behavior have implementation guidance.
**Note**: GEN-02 (scenario injection) lands with detection fixtures in Phase 3.

### Phase 3: Detection & Alert Pipeline

**Goal**: Malicious traffic produces exactly one deterministic, evidence-backed alert per dedupe key, and that holds across detector crashes, restarts and replays
**Mode:** mvp
**Depends on**: Phase 2
**Requirements**: DET-01, DET-02, DET-03, DET-04, DET-05, DET-06, DET-07, DET-08, DET-09, ALRT-01, ALRT-02, ALRT-03, ALRT-04, GEN-02
**Success Criteria** (what must be TRUE):

  1. Generator scenarios (brute force, port scan, request flood, deny spike) each trigger their rule exactly as expected, benign traffic triggers none, and boundary and lateness fixtures (exactly at the watermark limit and at +1ms) behave as the window ADR specifies
  2. Each alert carries severity, summary, event_count, window start/end, ≤20 sample event IDs, rule_id/version and a deterministic SHA-256 `alert_id`. Cooldown suppresses repeats for the same entity while separate windows remain investigable
  3. Killing the detector after 9 of 10 failures and then sending the 10th event still produces the alert once, including across a rebalance when a third replica joins
  4. Failpoint tests at each crash boundary (before publish, between publish and offset commit, after commit) show exactly one visible alert per dedupe key. A replayed detection never reopens or duplicates a resolved alert
  5. Alerts reach `security.alerts.v1`, immutable detections land in ClickHouse, and a workflow row with unique `alert_id`, `seq` and denormalized list fields exists in PostgreSQL. Rule thresholds load from `config/rules.yaml` at startup

**Plans**: TBD
**Research flag**: Deepest research need. Window and state-pin ADR, list-source ADR (write both before rule code), `CommitOffsetsSync` signature, failpoint mechanism.

### Phase 4: Query API & Analyst Dashboard

**Goal**: Analysts can sign in, investigate events and alerts within their own tenant, triage alerts, and see new alerts appear live in the browser
**Mode:** mvp
**Depends on**: Phase 3
**Requirements**: AUTH-01, AUTH-02, AUTH-03, AUTH-04, API-01, API-02, API-03, API-04, API-05, API-06, API-07, API-08, LIVE-01, LIVE-02, LIVE-03, UI-01, UI-02, UI-03, UI-04, UI-06
**Success Criteria** (what must be TRUE):

  1. A user signs in with the development JWT flow, and a viewer attempting a mutation gets 403. Two demo tenants cannot read or mutate each other's events or alerts across the full cross-tenant test matrix
  2. An analyst can filter and cursor-paginate events and alerts (alert pages stay full under status filters), open an alert with evidence, rule explanation and history, and fetch rules and summary stats
  3. An analyst can acknowledge and resolve an alert. A stale version returns a conflict, and each transition writes an audit row in the same transaction
  4. A new alert appears in the Alerts page over SSE (no token in the URL) without refresh. After a dropped connection or killed LISTEN link, the client reconnects and backfills via `seq` with no permanent gaps, and every query-api replica delivers every alert, including through a real proxy
  5. Overview, Alerts, Events and Rules pages work with loading, empty, error and reconnecting states, a typed OpenAPI client, safe event JSON rendering, and keyboard-operable controls (viewers see mutation controls hidden or disabled)

**Plans**: TBD
**UI hint**: yes
**Research flag**: Needs research. pgx `WaitForNotification` reconnect, SSE behind a real proxy, auth spike (header JWT vs cookie). REST layering and React pages are standard patterns.
**Note**: UI-05 (System page) moves to Phase 5 because it depends on Phase 5 lag and health metrics. The Overview consumer-lag panel (part of UI-01) is wired to real lag data when OBS-02 lands in Phase 5.

### Phase 5: Observability, Resilience & Scale

**Goal**: Operators can see pipeline health, and failure and scale behavior is proven by tests and a reproducible benchmark report rather than claimed
**Mode:** mvp
**Depends on**: Phase 4
**Requirements**: OBS-02, OBS-03, OBS-04, OBS-05, REL-01, REL-02, REL-03, REL-04, BENCH-01, BENCH-02, BENCH-03, BENCH-04, UI-05
**Success Criteria** (what must be TRUE):

  1. An operator sees Grafana dashboards (as code) for ingest rate, error rate, position-based consumer lag, detection delay, write latency and DLQ, and the System page shows pipeline health, throughput, lag and errors. Metrics carry no high-cardinality labels and logs are JSON with request/trace IDs and no secrets
  2. A trace spans ingest, publish, detect and store through Kafka headers, with configurable sampling
  3. Failure suites pass for event-writer restart mid-batch, unreachable ClickHouse, Kafka broker restart, slow consumers, malformed messages to the DLQ, SSE reconnect and rate-limit overload. Services retry with backoff and jitter and shut down gracefully, committing only completed offsets
  4. Under excess load, memory and queues stay bounded and clients receive the expected 429/503
  5. `make bench` runs a multi-replica run with ≥1,000,000 generated events and writes `benchmarks/results/<date>-<scenario>.md` (hardware, config, throughput, p50/p95/p99 latency, resources, lag, loss/duplicate counts, limitations). Counts reconcile (sent, accepted, rejected, persisted, expected vs observed rule hits), the report refuses to emit when they don't, and a detector restart under load recovers with measured recovery time

**Plans**: TBD
**UI hint**: yes
**Research flag**: Needs research. Open-loop benchmark method, cost of `FINAL` at the pinned ClickHouse version, detector restart catch-up (horizon × partition rate).

### Phase 6: Kubernetes, Hardening & Docs

**Goal**: A reviewer can deploy the system to a local Kubernetes cluster, trust CI to catch regressions, and reproduce the full demo from the documentation alone
**Mode:** mvp
**Depends on**: Phase 5
**Requirements**: DEP-01, DEP-02, DEP-03, DEP-04, DEP-05
**Success Criteria** (what must be TRUE):

  1. `kubectl apply -k` on the kind/minikube overlay deploys all services with working liveness/readiness probes, resource limits, and secrets separate from config
  2. K8s docs explain multi-broker Kafka replication (RF, min ISR), and Prometheus, ClickHouse, Redis and Kafka are not exposed publicly
  3. CI runs Go fmt/lint/vet/unit/race, frontend typecheck/lint/test/build, Python tests/lint, schema validation, dependency and secret scanning, and a bounded integration smoke suite on real dependencies, with no `|| true`, and is green
  4. README, `docs/architecture.md`, `docs/operations.md`, `docs/threat-model.md` and `docs/demo.md` match the implementation, and every performance claim links to a benchmark result file
  5. The spec §17 demo script completes end-to-end from a clean checkout

**Plans**: TBD
**Research flag**: Needs research. Kafka on K8s (plain StatefulSet vs Strimzi), kindest/node tag, ingress SSE annotations.
**Note**: DEP-03 is assigned here. The earlier CI skeleton is delivered under FND-02 and FND-05 in Phase 1.

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Foundations & Contracts | 13/13 | Complete    | 2026-10-11 |
| 2. Ingestion & Durable Event Storage | 10/14 | In Progress|  |
| 3. Detection & Alert Pipeline | 0/TBD | Not started | - |
| 4. Query API & Analyst Dashboard | 0/TBD | Not started | - |
| 5. Observability, Resilience & Scale | 0/TBD | Not started | - |
| 6. Kubernetes, Hardening & Docs | 0/TBD | Not started | - |

## Coverage

| Phase | Count | Requirements |
|-------|-------|--------------|
| 1 | 7 | FND-01..07 |
| 2 | 14 | ING-01..07, STOR-01..04, GEN-01, GEN-03, OBS-01 |
| 3 | 14 | DET-01..09, ALRT-01..04, GEN-02 |
| 4 | 20 | AUTH-01..04, API-01..08, LIVE-01..03, UI-01..04, UI-06 |
| 5 | 13 | OBS-02..05, REL-01..04, BENCH-01..04, UI-05 |
| 6 | 5 | DEP-01..05 |

Total: 73/73 mapped.
