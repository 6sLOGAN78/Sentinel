# Feature Landscape

**Domain:** Real-time security event ingestion / detection / alerting platform (portfolio-grade mini-SIEM, detection-only)
**Project:** Sentinel
**Researched:** 2026-10-10
**Confidence:** MEDIUM (feature categorization draws on spec.md plus general knowledge of Elastic Security, Wazuh, Sigma-based engines and Panther; no live competitor docs were re-verified in this pass)

Complexity key: Low (days), Med (about a week), High (multi-week or heavy test burden).

## Table Stakes

Features users (analysts, operators, reviewers) expect. Missing means the product feels incomplete or fails the "provably correct" core value.

### Ingestion

| Feature | Why Expected | Complexity | Spec | Notes |
|---------|--------------|------------|------|-------|
| Authenticated batch event intake (API key, hashed at rest) | Every SIEM has a collector endpoint with credentials | Med | FR-01, §8, §10 | Tenant resolved from the verified key, not the body |
| Strict schema validation (IPs, enums, skew, size caps) | Garbage-in protection; Elastic/Panther normalize and validate on intake | Med | FR-01, §6.1 | Reject unknown root fields; bounded attributes |
| Atomic all-or-nothing batches, 202 only after Kafka ack | Honest delivery semantics | Med | FR-01/02, §8 | Explicit 400/401/403/413/429/503 mapping |
| Rate limiting and overload errors (429 with Retry-After, 503) | Quotas are standard for multi-tenant intake | Med | FR-12, §7.4 | Redis token bucket; document fail-open/closed |
| Canonical normalized event schema, versioned | Elastic ECS, Panther and Sigma all rely on a normalized model | Low | §6.1 | `schema_version` field from day one |
| DLQ for poison messages | Standard pipeline hygiene | Med | §11.1, §6.3 | Reason, redacted context, source offset |

### Detection

| Feature | Why Expected | Complexity | Spec | Notes |
|---------|--------------|------------|------|-------|
| Threshold/windowed rules (four built-ins) | Core of every detection engine | High | FR-04, §5.1 | Event-time windows, bounded per-key state |
| Rule metadata: ID, version, severity, description, thresholds, enabled flag | Sigma and Elastic rules carry this; analysts need to know why an alert fired | Low | §5.1, §8 | Versioned YAML, restart to reload (acceptable v1) |
| Alert dedupe and cooldown | Alert fatigue is the top complaint about any SIEM | Med | FR-05, §5.1, §6.2 | Deterministic SHA-256 `alert_id` from canonical dedupe key |
| Evidence on every alert (event count, window, sample IDs capped at 20, summary) | Analysts will not trust an alert without evidence | Med | FR-05, §5.1 | Link evidence to event drill-down |
| Late-event/lateness policy (about 30s) | Event-time correctness is a stated differentiator of the spec but also a correctness requirement | High | §5.1 | Deterministic: count, drop-with-metric, or reprocess |
| Replay-safe detection (crash between state, publish, commit) | Core value of the project | High | FR-11, §5.1 | Needs failure-injection tests |

### Storage and Search

| Feature | Why Expected | Complexity | Spec | Notes |
|---------|--------------|------------|------|-------|
| Searchable event history with TTL | Basic SIEM function | Med | FR-03, §7.1 | Tenant scope, range caps, result limits always |
| Duplicate-safe writes | At-least-once delivery implies duplicates | Med | FR-03, §7.2 | Read-time correctness before merges |
| Filter and cursor pagination for events and alerts | Table stakes in any list UI/API | Med | FR-06, §8 | `(occurred_at, event_id)` and `(created_at, alert_id)` cursors |
| Event detail view | Pivot from alert to raw event | Low | §8, §9 | Safe rendering, never dangerously render attributes |

### Triage and Governance

| Feature | Why Expected | Complexity | Spec | Notes |
|---------|--------------|------------|------|-------|
| Alert triage workflow: open, acknowledged, resolved | Minimum case lifecycle in every SIEM | Med | FR-08, §7.3 | Postgres, optimistic version check |
| Audit log of status transitions (actor, time) | Compliance expectation, explicitly required | Med | FR-08, §10 | Also log auth failures (no secrets) and role changes |
| Tenant isolation (enforced server-side, tested) | Multi-tenant security product cannot leak | High | §10, §14 Ph4 | Cross-tenant ID returns 403/404; test with two demo tenants |
| RBAC: viewer, analyst, admin | Standard least privilege | Med | §10 | Server-side enforcement; viewer cannot mutate |
| Dashboard auth (session/JWT) | UI must be authenticated | Med | §10 | Replace boilerplate Clerk per PROJECT.md decision |

### Live Experience and Operations

| Feature | Why Expected | Complexity | Spec | Notes |
|---------|--------------|------------|------|-------|
| Live alert feed (SSE) with reconnect and REST backfill | Real-time alerting is the product promise | Med | FR-07, §8 | `Last-Event-ID`, keep-alives, bounded client buffers, no token in URL |
| Alerts, Events, Rules (read-only), Overview, System pages | Defined dashboard surface | Med | §9 | Loading/empty/error/reconnect states |
| Health and readiness endpoints reflecting real dependency state | Operator expectation, K8s probes | Low | FR-10, §8 | Readiness checks actual capability |
| Prometheus metrics with bounded labels | Operator expectation | Med | FR-10, §11.2 | No IP/user/tenant labels |
| Structured JSON logs with request_id and trace_id | Debuggability | Low | §11.3 | Scrub sensitive fields |
| Deterministic synthetic generator with scenario injection | Needed to demo and test detections | Med | FR-09 | Seeded, reserved doc IP ranges |
| One-command Compose setup with demo data | Reviewer experience | Med | FR-13 | `make dev-up`, `make seed` |
| OpenAPI 3.1 contract and typed client | Contract discipline | Med | §8, §9 | Contract tests |
| CI with unit, integration smoke, lint, scanning | Portfolio credibility | Med | §15 | No `|| true` |

## Differentiators

Not expected of a mini-SIEM, but they are what make this project stand out as an engineering portfolio piece.

| Feature | Value Proposition | Complexity | Spec | Notes |
|---------|-------------------|------------|------|-------|
| Demonstrable crash-recovery and chaos test suite | Proves at-least-once plus idempotency rather than claiming it | High | FR-11, §15 | Kill detector before/after publish and before commit |
| Reproducible benchmark report (1M+ synthetic events) with hardware, config, percentiles | Claim discipline is rare and valued | High | FR-16, §12 | `bench/report.py`; separate sent, Kafka-accepted, persisted counts |
| Multi-replica, partition-aware detection | Shows real distributed-systems skill | High | FR-16, §3 | Key `tenant|source_ip`; document repartitioning for other keys |
| Event-time watermark and lateness handling with tests | Most toy detectors use processing time | High | §5.1 | Boundary and lateness fixtures |
| Detection-delay histogram (event time to alert) | Quantifies pipeline latency | Low | §11.2 | `sentinel_detection_delay_seconds` |
| Consumer-lag and pipeline-health views in the UI and Grafana | Operator persona; rarely present in portfolio projects | Med | FR-15, UC-04 | Lag needs a Kafka admin poller |
| Dashboard time series, rule breakdown, top rules | Investigation context | Med | FR-15 | Backed by `/metrics/summary` |
| Investigation timeline: alert window with evidence events in order | Strong analyst UX | Med | UC-03, §9 | Reuse events query bounded by window and entity; no new infra |
| OpenTelemetry tracing across ingest, Kafka, detector, storage | End-to-end traceability | Med | §11.3 | Propagate trace context in Kafka headers |
| Kubernetes manifests/Helm with probes and limits | Deployability proof | Med | FR-14 | kind/minikube instructions |
| Threat model, ADRs, runbook | Engineering maturity | Low | §13 | `docs/decisions/` |
| Optional Aegis adapter (P2) | Ecosystem integration | Med | FR-17 | Defer |
| Optional offline ML anomaly scorer (P2) | Same alert interface | High | FR-18 | Defer; system must work without it |

## Anti-Features

Deliberately not built in v1.

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Inline blocking, enforcement or auto-remediation | Detection only; safety and scope | Alerts and evidence only |
| Packet capture, TLS decryption, endpoint agents | Out of scope and legally sensitive | Synthetic generator events |
| Arbitrary SQL or query-language UI (KQL/Lucene/SPL style) | Injection and cost risk; non-goal | Fixed, parameterized filters |
| User-defined executable detection scripts or in-UI rule editor | Code-execution risk | Versioned YAML, read-only Rules page |
| Hot rule reload / rule CRUD API | Complexity with no v1 payoff | Restart to apply; document it |
| Sigma rule import / full Sigma compiler | Large scope; parity chasing | Mention as future work; keep rule schema Sigma-inspired |
| Case management (multi-alert cases, comments, assignment workflows, SLAs) | SIEM parity creep | Status, optional assignee, audit trail |
| Alert correlation across rules, entity risk scoring | Heavy and speculative | Single-rule deterministic alerts |
| Threat intel feeds, enrichment (GeoIP, WHOIS) | External dependencies, privacy | None in v1 |
| Notification channels (email, Slack, PagerDuty) | Out of spec; boilerplate Resend is non-spec | SSE live feed only |
| Billing, per-tenant quotas UI, tenant self-service | Non-goal | Static demo tenants; admin key management minimal |
| Exactly-once or throughput/latency marketing claims | Violates claim discipline | Measured benchmarks only |
| Full ML detection in MVP | Non-goal | Optional P2 behind same interface |
| Multi-region active-active, HA Compose Kafka | Non-goal | Document single-broker limits |
| Extra datastores/services beyond spec stack | Spec §19.8 | Use Kafka, ClickHouse, Postgres, Redis only |
| Retaining boilerplate Clerk, Resend, asynq, New Relic | Not in mandated stack | Remove or replace |

## Feature Dependencies

```
Auth + tenant resolution (API keys, JWT, RBAC)
  -> Ingest API (FR-01) -> Kafka publish (FR-02)
       -> Event writer -> ClickHouse events (FR-03) -> Events API (FR-06) -> Events page
       -> Detector (FR-04; needs rules YAML + event-time windows)
            -> Alerts topic -> Alert writer -> ClickHouse alerts + Postgres workflow (FR-05)
                 -> Alerts API (FR-06) -> Triage PATCH + audit (FR-08)
                 -> SSE stream (FR-07) -> Live feed
Rate limiting (FR-12) depends on Redis; wraps ingest
Generator (FR-09) required to test everything above; build with Phase 2
DLQ depends on Kafka consumers existing
Replay safety (FR-11) depends on deterministic alert IDs + idempotent writers + manual commit
Metrics (FR-10) -> Lag/health views (FR-15) -> Grafana dashboards
Benchmarks (FR-16) depend on multi-replica deployment, generator, metrics
K8s (FR-14) depends on health/readiness endpoints
Investigation timeline depends on Events API + alert evidence
```

## MVP Recommendation

Prioritize, following spec §14 ordering:

1. Contracts, auth/tenant model, ingest, event writer, generator (Phases 1-2).
2. Four rules with dedupe, evidence, DLQ and replay-safe commit strategy (Phase 3).
3. Query API, triage with audit, RBAC, tenant isolation tests, SSE, core dashboard pages (Phase 4).
4. One differentiator woven in early: chaos/replay tests, since they define the core value.

Defer: benchmark report, multi-replica scale tests, Grafana polish (Phase 5); Kubernetes and hardening docs (Phase 6); Aegis adapter and ML scorer (Phase 7).

## Gaps and Flags

- Spec lists `assignee` in workflow state but no assign endpoint; treat as stored-only field in v1.
- Admin "manages keys/config" has no API routes in §8; decide whether key management is seed/CLI-only (recommended for v1).
- Dashboard login flow and `GET /api/v1/rules` enabled/disabled display need a small ADR.

## Sources

- /home/logan78/Desktop/Sentinel/spec.md (authoritative, HIGH)
- /home/logan78/Desktop/Sentinel/.planning/PROJECT.md (HIGH)
- General knowledge of Elastic Security, Wazuh, Sigma, Panther feature sets (LOW-MEDIUM, not re-verified against current docs)
