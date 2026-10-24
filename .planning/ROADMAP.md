# Roadmap: Sentinel — Real-Time Threat Detection Platform

## Overview

Sentinel is built as vertical slices following spec §14. Each phase leaves the repo runnable. Phase 1 lays contracts and a clean single-module foundation. Phase 2 makes ingestion durable (HTTP to Kafka to ClickHouse). Phase 3 adds the replay-safe detection and alert pipeline, which carries the core correctness claim. Phase 4 exposes it through a tenant-scoped query API and live React dashboard. Phase 5 proves it with observability, failure tests and a reproducible 1M-event benchmark. Phase 6 ships Kubernetes manifests, hardened CI and documentation. Optional spec Phase 7 (Aegis adapter, ML scorer) is deferred to v2.

**Granularity:** standard (6 phases, derived from spec §14 and confirmed against requirement clusters)

## Phases

- [x] **Phase 1: Foundations & Contracts** - Clean Go module with five binaries, Compose stack, schemas, OpenAPI skeleton, ADRs, CI skeleton (completed 2026-10-11)
- [x] **Phase 2: Ingestion & Durable Event Storage** - Authenticated, rate-limited batch ingest to Kafka and ClickHouse with read-time dedupe and DLQ (22/22 plans; verified complete 2026-10-17)
- [x] **Phase 3: Detection & Alert Pipeline** - 21/21 numbered plans; replacement CI39613182464 all10jobs and final59/59verification passed (completed 2026-10-21)
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

**Goal**: As a producer with a scoped API key, I want to submit event batches acknowledged only after Kafka accepts them, so that my events become queryable and duplicate-safe in ClickHouse.
**Mode:** mvp
**Depends on**: Phase 1
**Requirements**: ING-01, ING-02, ING-03, ING-04, ING-05, ING-06, ING-07, STOR-01, STOR-02, STOR-03, STOR-04, GEN-01, GEN-03, OBS-01
**Success Criteria** (what must be TRUE):

  1. A producer POSTing a valid batch (≤500 events, ≤1 MiB) with a valid API key gets 202 with `accepted_count`, `event_ids` and `request_id` only after Kafka acks. Invalid batches get 400 all-or-nothing, and 401/403/413/503 each return the JSON error envelope
  2. `make seed` submits the seeded benign demo events (1000) through the ingest API, and they all appear in ClickHouse and in a minimal events lookup
  3. Retrying a batch after a 5xx, or replaying the same events twice, never shows duplicate rows in user-visible queries or counts, asserted immediately after a double insert with no `OPTIMIZE`
  4. A producer over quota gets 429 with `Retry-After`. With Redis down, intake follows the documented policy (503 by default) and never silently bypasses limits. Under excess load the HTTP layer sheds with 429/503 instead of buffering without bound
  5. A poison message lands in `security.events.dlq.v1` with reason and origin offset, and the source offset is committed only after the DLQ publish. Each service's `/readyz` reflects real dependency state

**Plans**: 22 plans across 14 dependency waves (22 executed; final verification and hosted CI passed)

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

- [x] 02-09-PLAN.md — Expose real health/readiness and drain all five services

**Wave 8** *(blocked on prior dependency waves)*

- [x] 02-11-PLAN.md — Prove timeout, partial-publish retry and writer crash recovery

**Wave 9** *(blocked on prior dependency waves)*

- [x] 02-12-PLAN.md — Start the healthy local pipeline through pinned Compose apps

**Wave 10** *(blocked on prior dependency waves)*

- [x] 02-13-PLAN.md — Verify 1000 current-run events and enforce CI coverage guards

**Wave 11** *(gap repairs; depends on executed 02-13)*

- [x] 02-14-PLAN.md — Bound encoded DLQ records and adversarial headers
- [x] 02-16-PLAN.md — Match event-lookup authentication contract to runtime
- [x] 02-17-PLAN.md — Reject healthprobe redirects
- [x] 02-18-PLAN.md — Preserve manifest parent permissions and isolate seed credentials
- [x] 02-20-PLAN.md — Reapply configured topic retention on repeat startup

**Wave 12** *(blocked on 02-14)*

- [x] 02-15-PLAN.md — Bound aggregate poison processing and prove safe rebalance recovery

**Wave 13** *(blocked on 02-15's named rebalance regression)*

- [x] 02-19-PLAN.md — Exclude build credentials and guard both Go dependency graphs

**Wave 14** *(blocked on every gap repair)*

- [x] 02-21-PLAN.md — Revalidate the complete slice and record truthful phase status

**Verification:** Passed: 5/5 roadmap criteria and 14/14 requirements. After repeat 38972246356 exposed a readiness deadline gap, RED 2afe6b8 / repair eb7f6e4 independently bounded responses with retained sixteen-worker admission. Unit stress, full make test, lint and three real readiness/lifecycle race repetitions passed. Hosted [run 38974983287](https://github.com/6sLOGAN78/Sentinel/actions/runs/38974983287) then passed all eight jobs, full integration, seven nonempty groups and Compose at 5e6a826, including the previously failing readiness repeat. Root final verification/delta review and zero-open-threat records are explicit; original failed reports remain preserved. UAT is complete. Phase 3 execution has not started.

**Research**: Complete (02-RESEARCH.md, 2026-10-12); plan verification passed (02-PLAN-CHECK.md). Read-time dedupe, native ClickHouse, Kafka timeout/manual commits and Redis Lua behavior have implementation guidance.
**Note**: GEN-02 (scenario injection) lands with detection fixtures in Phase 3.

### Phase 3: Detection & Alert Pipeline

**Goal**: As a system operator, I want to receive deterministic, evidence-backed alerts from malicious traffic, so that crashes, restarts and replay cannot lose or duplicate detections.
**Mode:** mvp
**Depends on**: Phase 2
**Requirements**: DET-01, DET-02, DET-03, DET-04, DET-05, DET-06, DET-07, DET-08, DET-09, ALRT-01, ALRT-02, ALRT-03, ALRT-04, GEN-02
**Success Criteria** (what must be TRUE):

  1. Generator scenarios (brute force, port scan, request flood, deny spike) each trigger their rule exactly as expected, benign traffic triggers none, and boundary and lateness fixtures (exactly at the watermark limit and at +1ms) behave as the window ADR specifies
  2. Each alert carries severity, summary, event_count, window start/end, ≤20 sample event IDs, rule_id/version and a deterministic SHA-256 `alert_id`. Cooldown suppresses repeats for the same entity while separate windows remain investigable
  3. Killing the detector after 9 of 10 failures and then sending the 10th event still produces the alert once, including across a rebalance when a third replica joins
  4. Failpoint tests at each crash boundary (before publish, between publish and offset commit, after commit) show exactly one visible alert per dedupe key. A replayed detection never reopens or duplicates a resolved alert
  5. Alerts reach `security.alerts.v1`, immutable detections land in ClickHouse, and a workflow row with unique `alert_id`, `seq` and denormalized list fields exists in PostgreSQL. Rule thresholds load from `config/rules.yaml` at startup

**Plans**: 21 plans in 14 waves

**Final acceptance (2026-10-21):** Replacement CI39613182464 at7a72028 passed
all10jobs: fullintegration/all7retainedgroups, exact44core+2scenariofamilies and
preservedCompose acceptance. Repairedlocalfull passed1650.77s; final51script
tests and scanner passed; independentreview clean across131uniqueartifacts,
78registeredthreatdispositionsclosed/0open. ReleasedgoalverificationPASSED59/59,
72artifactdeclarations/68paths,46links,all14requirements. Earlierfailed/cancelled/
UNKNOWN attempts retainactualprovenance. Registeredphase.complete3 succeeded;
rootcorrected its supplemental-planmiscount and historical-statusregexwarning
against actual21numberedplans/summaries and currentpassedreport. No gate waived.
Phase4 is not started.

Plans:
**Wave 1**

- [x] 03-01-PLAN.md — Let an operator consume a real AUTH_BRUTE_FORCE alert from Kafka after ten authenticated failure events
- [x] 03-06-PLAN.md — Generate exact seeded malicious, benign, edge and late scenarios with a current-run expected-alert manifest

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 03-02-PLAN.md — Persist the live AUTH alert into immutable ClickHouse evidence and one PostgreSQL workflow row
- [x] 03-15-PLAN.md — Extract and test the strict AUTH rules loader without changing the real Kafka slice
- [x] 03-16-PLAN.md — Extract the live Kafka detector adapters and test acknowledgements/metadata before adding other rules

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 03-03-PLAN.md — Detect a port scan as twenty exact distinct destination ports in one event-time minute
- [x] 03-17-PLAN.md — Extract and prove native alert stores immediately after the first two-store path
- [x] 03-18-PLAN.md — Make real PostgreSQL capability part of detector startup, shutdown and local worker readiness before cooldown activation

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 03-04-PLAN.md — Add request-flood and deny-spike alerts to the live detector
- [x] 03-10-PLAN.md — Repair partial alert writes and quarantine conflicting immutable identities under concurrency

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 03-05-PLAN.md — Freeze evidence and first-threshold candidate source/clock for durable cross-window decisions

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 03-19-PLAN.md — Deliver actual cross-window alert cooldown through durable PG decisions on the existing events→alerts path

**Wave 7** *(blocked on Wave 6 completion)*

- [x] 03-07-PLAN.md — Prove checkpoint reconstruction across arbitrary restart cuts and advancing state pins

**Wave 8** *(blocked on Wave 7 completion)*

- [x] 03-08-PLAN.md — Allow rule changes only through a strict startup config and drained version handover

**Wave 9** *(blocked on Wave 8 completion)*

- [x] 03-09-PLAN.md — Bound detector state and expose deterministic late drops without weakening readiness
- [x] 03-21-PLAN.md — Schedule live scenarios after a private caught-up partition snapshot within legal ingest skew

**Wave 10** *(blocked on Wave 9 completion)*

- [x] 03-11-PLAN.md — Quarantine detector and writer poison within one owned-batch budget without skipping durable prefixes

**Wave 11** *(blocked on Wave 10 completion)*

- [x] 03-12-PLAN.md — Prove mid-window restart, actual third-replica ownership transfer and all three crash boundaries

**Wave 12** *(blocked on Wave 11 completion)*

- [x] 03-20-PLAN.md — Prove cooldown decisions across PG failure, stale ownership and all four PG/Kafka checkpoint crash cuts

**Wave 13** *(blocked on Wave 12 completion)*

- [x] 03-13-PLAN.md — Run a healthy preserved-volume Compose detector/writer stack and verify exact current-run scenario alerts

**Wave 14** *(blocked on Wave 13 completion)*

- [x] 03-14-PLAN.md — Guard every Phase 3 acceptance family in CI and document verified operator/replay behavior

**Research**: Complete targeted amendment (03-RESEARCH.md, 2026-10-17). USER option1 actual cross-window cooldown is planned through durable PG decisions/cursors/ownership fences, format2 state-pin metadata, aligned config handover and private watermark-aware live scheduling. Decision-frontier restore-behind proof is assigned; independent revision checking and all execution results remain pending. Original checker report is historical; both ADRs precede rule code in03-01-T01.

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

**Plans**: 17 plans in 17 ordered waves. Original16 approvedR2; source-reviewrepair04-17insertedafter04-01before04-02. Plans01/17/02/03/04/05/06/07accepted after actual scoped gates, clean independent reviews and root post-wave checks; 9plans remain.

- [x] 04-01: Sign in and read real tenant alerts
- [x] 04-17: Repair private JWT launch wiring, Vite API reachability and security negative controls (unblocks04-02)
- [x] 04-02: Safe development token CLI and auth configuration
- [x] 04-03: Typed session-isolated browser shell
- [x] 04-04: Full filtered alert pages and stable cursors
- [x] 04-05: Alert evidence details and honest retention — accepted2026-10-23 after clean12-pathreview androotpostwavePASS
- [x] 04-06: Unique event search — accepted2026-10-23 after IPv6/deep-linkrepair,cleanR1/rootpostwavePASS
- [x] 04-07: Safe event JSON and legacy lookup compatibility — accepted2026-10-24 after authorized isolated demo recovery and independent root checks; old history remains held
- [ ] 04-08: Acknowledge with atomic optimistic audit
- [ ] 04-09: Resolve/history and redacted security logging
- [ ] 04-10: Read-only active Rules page
- [ ] 04-11: Real tenant Overview aggregates
- [ ] 04-12: Gap-safe durable delivery catch-up
- [ ] 04-13: Header-authenticated live SSE
- [ ] 04-14: LISTEN recovery and bounded stream resource proofs
- [ ] 04-15: Full tenant matrix and real browser/proxy acceptance
- [ ] 04-16: Complete fail-closed CI/local acceptance and blocking publication approval

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
| 2. Ingestion & Durable Event Storage | 22/22 | Complete | 2026-10-17 |
| 3. Detection & Alert Pipeline | 21/21 | Complete    | 2026-10-21 |
| 4. Query API & Analyst Dashboard | 8/17 accepted | 04-08 atomic acknowledge/audit | - |
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
