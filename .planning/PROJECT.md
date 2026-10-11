# Sentinel — Real-Time Threat Detection Platform

## What This Is

Sentinel is a distributed security event platform for ingestion, detection, investigation and alerting. It collects network and identity security events from simulated sources (and optionally from Aegis). Events are validated and streamed through Kafka, then checked against deterministic threat-detection rules. Event history is stored in ClickHouse for search, and an authenticated React dashboard shows real-time alerts. It serves three personas: security analysts triaging alerts, platform operators watching pipeline health, and developers/testers driving synthetic traffic. The project exists to demonstrate production-quality backend, distributed-systems, streaming-data and security engineering.

## Core Value

The end-to-end flow must work and be provably correct under failure: generator → ingest → Kafka → detector → ClickHouse/PostgreSQL → API → live dashboard. It must produce deterministic, deduplicated, evidence-backed alerts and recover from worker crashes without losing acknowledged events. Every performance claim must be backed by a reproducible benchmark.

## Requirements

### Validated

<!-- Phase 1 (Foundations & Contracts) replaced the go-boilerplate scaffold; see .planning/phases/01-foundations-contracts/01-VERIFICATION.md -->

- ✓ One-command local stack: `make dev-up` brings up pinned Kafka (KRaft), ClickHouse, PostgreSQL and Redis with explicitly created topics (6/3/1 partitions, auto-create off); verified from empty volumes: Phase 1 (FND-01)
- ✓ Idempotent make targets (`migrate`, `test`, `test-integ`, `lint`, `run`, `dev-down`, CONFIRM-gated `reset`; honest non-zero `seed`/`bench` placeholders): Phase 1 (FND-02)
- ✓ Single Go 1.26 module at `backend/` with five binaries (ingest, detector, event-writer, alert-writer, query-api); non-spec boilerplate removed; boilerplate defects fixed and tested: Phase 1 (FND-03)
- ✓ Versioned event/alert JSON Schemas plus Go types with strict decoding, and one canonical length-prefixed SHA-256 `alert_id` function (golden vectors and fuzzing): Phase 1 (FND-04)
- ✓ OpenAPI 3.1 contract for every spec §8 endpoint, validated in tests with drift checks against the JSON Schemas; TS types generated for `web/`: Phase 1 (FND-05)
- ✓ Pinned versions (image digests, lockfiles, tools.mod), placeholder-only `.env.example`, gitleaks secret scanning: Phase 1 (FND-06)
- ✓ Six ADRs in `docs/decisions/` (layout, event_id, atomic batch, metrics labels, Redis fail-closed, alert storage split): Phase 1 (FND-07)

### Active

<!-- Source: spec.md §5 (FR-01..FR-18). All hypotheses until shipped and verified. -->

- [ ] FR-01: Accept authenticated JSON event batches with strict validation (≤500 events, ≤1 MiB, atomic all-or-nothing)
- [ ] FR-02: Publish each accepted event durably to `security.events.v1` (acks=all, key `tenant_id|source_ip`), and return 202 only after Kafka ack
- [ ] FR-03: Persist events to ClickHouse with a documented deduplication strategy
- [ ] FR-04: Evaluate four built-in event-time streaming rules (AUTH_BRUTE_FORCE, PORT_SCAN, REQUEST_FLOOD, DENY_SPIKE)
- [ ] FR-05: Persist alerts with evidence, rule IDs and deterministic SHA-256 alert IDs
- [ ] FR-06: List, filter and cursor-paginate events and alerts via the tenant-scoped API
- [ ] FR-07: Show live alerts in the React UI over SSE, with reconnect and backfill
- [ ] FR-08: Acknowledge and resolve alerts with actor, time, optimistic versioning and audit history
- [ ] FR-09: Deterministic, seeded synthetic event generator with scenario injection
- [ ] FR-10: Prometheus metrics plus health/readiness endpoints
- [ ] FR-11: Replay-safe recovery from worker crashes
- [ ] FR-12: Rate-limit ingestion (Redis token bucket) with meaningful 429/503 errors
- [ ] FR-13: Single-command Docker Compose setup with demo data
- [ ] FR-14: Kubernetes manifests or Helm chart with probes and resource limits (P1)
- [ ] FR-15: Dashboard time series, rule breakdown and health/lag charts (P1)
- [ ] FR-16: Multi-replica load test and reproducible benchmark report covering ≥1M events (P1)

### Out of Scope

- Inline blocking or enforcement of traffic: Sentinel is detection only
- Real packet capture, TLS decryption, network interception or endpoint agents: synthetic or simulated sources only
- Autonomous remediation, offensive capabilities or attack automation: educational, authorized monitoring only
- SIEM feature parity, multi-region active-active, per-tenant billing: not a v1 portfolio goal
- Arbitrary SQL from UI users or user-defined executable detection scripts: security risk; rules are versioned YAML config
- Full ML detection in v1: deferred to optional Phase 7 behind the same alert interface (FR-18)
- Aegis adapter (FR-17): optional P2 integration, deferred
- Exactly-once end-to-end processing claims: the system is at-least-once with idempotent effects

## Context

- **Current state (after Phase 1, 2026-10-11):** the go-boilerplate (commit `9f63dce`) has been replaced:
  - One Go module at `backend/` (Go 1.26) builds five binaries. They load config, connect to their dependencies and shut down cleanly, but have no HTTP routes or business logic yet.
  - `web/` is a Vite + React 19 + TS 6 shell with no data. `generator/` is a uv-managed Python 3.13 project with a seeded event builder.
  - Contracts, migrations, OpenAPI, ADRs and a CI skeleton exist.
  - Clerk, asynq, Resend, New Relic, Turborepo/bun and the TS packages are gone.
  - The GitHub Actions CI has not yet run on GitHub (tracked in `01-HUMAN-UAT.md`).
- **Phase 2 hand-offs** (from `01-SECURITY.md` W-02..W-07 and `01-REVIEW.md`):
  - Set `IPExtractor` before any IP-keyed rate limiting.
  - Fix the empty-CORS-list allow-all default and the comma-separated origins parsing.
  - Stop logging the full request URI.
  - Call `AssertFormat` when compiling schemas in production.
  - Reject duplicate JSON keys in strict decoding.
  - Make chmigrate atomic and database-aware.
  - Add a CI job for the integration tests.
  - Add Redis AUTH/TLS settings.
- **Codebase map is stale:** `.planning/codebase/` describes the pre-Phase-1 boilerplate. Refresh it with `/gsd:map-codebase` before relying on it.
- **Source of truth:** `spec.md` (v1.0) defines the contracts (event/alert schemas, Kafka topics, HTTP API, ClickHouse/Postgres storage, metrics names), the phase plan (§14), the testing strategy (§15) and the Definition of Done (§18).
- **Claim discipline:** "Millions of events" means synthetic load volume, not a guaranteed rate. Never invent throughput, latency, test results or security guarantees.
- **Synthetic data:** use reserved documentation IP ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`) and pseudonymous principals only.

## Constraints

- **Tech stack:**
  - Go for ingest, detector, writers and query API, as a single Go module with multiple binaries.
  - Apache Kafka in KRaft mode, ClickHouse, PostgreSQL (alert workflow state and audit) and Redis (rate limiting).
  - Python for the generator and benchmark reports; React + TypeScript + Vite for the dashboard.
  - Prometheus, Grafana and OpenTelemetry for observability; GitHub Actions for CI; Docker Compose and Kubernetes for deployment.
  - Mandated by spec §2.
- **Delivery semantics:** at-least-once. Commit Kafka offsets only after downstream effects succeed, and make every consumer and store idempotent. This is the spec §3 key decision.
- **Event-time correctness:** all rule windows use `occurred_at`, with bounded per-key state and a documented lateness tolerance (~30s). Late-event handling must be deterministic. Spec §5.1.
- **Security:**
  - Tenant is resolved from the verified identity, never only from the request.
  - viewer/analyst/admin roles are enforced server-side.
  - API keys are hashed at rest, all SQL is parameterized, and no secrets are committed.
  - Spec §10.
- **Bounded resources:** no unbounded buffering. Shed load with 429/503 and cap producer queues, ClickHouse batches and per-key state. Spec §11.1.
- **Metrics hygiene:** no high-cardinality labels (IP, user, event ID, request ID, tenant) in Prometheus. Spec §11.2.
- **Definition of Done:** a phase is done only when its workflow completes end-to-end, tests pass (success, invalid input and failure cases), CI is green, and there are no stubs, mock-only adapters or invented numbers. Spec §18.
- **Pinned versions:** use current stable tool versions, pinned in lockfiles and container images. Spec §2.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Alert storage: immutable detections in ClickHouse, mutable workflow state and audit in PostgreSQL (unique `alert_id`) | Required v1 choice in spec §7.3; uses analytic search plus transactional triage | ✓ Good: recorded as ADR 0006 in Phase 1; details finalized in Phase 3 |
| At-least-once delivery with deterministic IDs (event_id, SHA-256 alert_id) and idempotent writes | Kafka+ClickHouse can't give end-to-end exactly-once (spec §3, §7.2) | — Pending |
| Atomic all-or-nothing batch ingestion | Spec §8 recommends it for v1, so there is no ambiguous partial acceptance | ✓ Recorded as ADR 0003: atomic at the response level; mid-batch Kafka failure gives 503 plus identical-batch retry with client-supplied event_id |
| Single Go module, multiple binaries (ingest, detector, event-writer, alert-writer, query-api) | Spec §2: avoid premature microservices | ✓ Good: five binaries under `backend/cmd/*` in Phase 1 |
| Reuse the existing `backend/` Go module and boilerplate infra (Echo, koanf, pgx, zerolog) where it fits; place binaries under `backend/cmd/*` instead of the spec's root-level `cmd/` | Simplest consistent path from the current repo (spec §19.4); record in `docs/decisions/` during Phase 1 | ✓ Good: ADR 0001; old HTTP layer deleted (D-28), retained infra refactored |
| Replace or remove non-spec boilerplate (Clerk, Resend email, asynq jobs, New Relic) in favor of spec-mandated API-key/JWT auth and Prometheus/OTEL | Spec §19.8: no unrelated services without a documented reason | ✓ Good: all removed in Phase 1; `go list -deps` banned-module check passes |
| Rules are versioned YAML loaded at startup; changes need a restart in v1 | Spec §5.1 explicitly allows it; keeps the rule engine simple | — Pending |
| Vertical MVP phases following spec §14 order | Spec §14: each phase leaves the repo runnable; vertical slices before broad abstractions | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check: still the right priority?
3. Audit Out of Scope: reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-10-11 after Phase 1 (Foundations & Contracts)*
