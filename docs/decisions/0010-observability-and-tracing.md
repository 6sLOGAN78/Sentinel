# 0010. Observability and tracing

## Status

Accepted, 2026-11-19. Records the Phase 5 observability decisions made in plans
05-01 to 05-15 and 05-21. ADR 0013 (non-idempotent bounded producers) and ADR
0014 (operator tenant allowlist) are separate records. This ADR uses ADR 0014's
narrowed access rules for the System and lag endpoints, which replace the
earlier "admin-only System" and "analyst lag" wording. Operator procedures are in
[operations](../operations.md#observability).

## Context

Spec §11 asks for Prometheus metrics, structured logs, OpenTelemetry traces,
dashboards and example alerts. These must not break three earlier constraints:
no globals or `init()` (lint), the metrics label policy (ADR 0004: no tenant,
IP, user, event, request or trace labels), and bounded resources (spec §11.1).
The pipeline is at-least-once (spec §3), so a metric that counts work can count
a replay twice, and "lag" must say which offset it is measured from. The
detector's committed offset is a replay pin (ADR 0007), not the next record to
process.

## Decision

1. **Injected per-process registry.** Each binary builds one
   `telemetry.NewRegistry()` (Go and process collectors plus its own metrics)
   and passes it to the components that record. `/metrics` is served only on
   the private ops listener (ports 9101-9105). There is no default registry and
   no package-level collector. Every label is in the ADR 0004 allowlist
   (`result`, `status`, `group`, `topic`, `partition`, `reason`, `rule_id`,
   `severity`), all bounded enums or partition numbers. No new label names
   were needed, so ADR 0004 is not amended.
2. **Position-based consumer lag.** `sentinel_kafka_consumer_lag` is the broker
   end offset minus the next offset this process has durably processed, clamped
   at 0. Event-writer and alert-writer report the position they commit after
   their store effects succeed. The detector reports `processed_next` from the
   committed checkpoint metadata, never the replay pin, which can lag far
   behind while a window is open. A partition reports nothing until it has a
   committed or retained start, so no value is invented. Series for revoked or
   lost partitions are deleted.
3. **Detection metrics count publications.**
   `sentinel_detection_alerts_total{rule_id,severity}` counts acknowledged alert
   publications. A replay after a crash counts the same alert again. Unique
   alerts come from PostgreSQL and ClickHouse. `sentinel_detection_late_events_total`
   can also count a replayed input again.
4. **JSON logs by default.** `SENTINEL_OBSERVABILITY.LOGGING.FORMAT` is `json`
   unless it is set to `console`, and any other value fails validation. Every
   line has a UTC RFC 3339 `timestamp`, `service` and `environment`. Request
   lines carry `request_id` and, when the span context is valid, `trace_id` and
   `span_id`. The client `ip` appears exactly once per request line. It is
   personal data kept for security investigation and is never a metric label.
   Workers add `trace_id` only for sampled records.
5. **OpenTelemetry with explicit providers.** `telemetry.NewTracing` builds one
   `TracerProvider` per process with `ParentBased(TraceIDRatioBased(ratio))`.
   It is passed explicitly with the W3C propagator, and no OTel global is set
   (otelhttp gets a noop MeterProvider). The HTTP edges (ingest, query-api)
   treat every request as a public endpoint, so an incoming `traceparent`
   becomes a link and the root sampling decision is made at Sentinel's edge.
   Kafka records carry `traceparent` through a small franz-go header carrier
   (`kafkax.HeaderCarrier`) instead of the kotel plugin, which would add a
   dependency and per-record spans. Workers start spans only for sampled
   parents. An event-writer store span links at most 32 deduplicated record
   contexts. Export goes through a bounded batch span processor (queue 2048,
   batch 512, drops when full) and an attribute allowlist, so client address,
   user agent, URL path and similar values never leave the process. With
   tracing disabled there is no exporter, but request logs still get a trace ID.
6. **Overlay backends.** Prometheus, Grafana and Jaeger are in a separate
   Compose overlay. Images are pinned by digest, ports are published on
   127.0.0.1 only (19090, 13000, 16686), and storage is bounded: TSDB 2 days or
   1 GB, Jaeger in-memory with 1000 traces, rotated json-file logs and 512m
   memory each. Grafana has anonymous access and sign-up disabled and needs a
   password from `.env`. The Jaeger OTLP receiver is not published.
7. **System and lag APIs with fixed queries.** `GET /api/v1/system` runs a
   fixed set of PromQL constants, each aggregated to at most one allowlisted
   label, under one 2 s timeout. Readiness comes from `/readyz` probes of
   configured ops addresses, independent of Prometheus. `GET /api/v1/metrics/lag`
   runs one fixed query. Every value is either `available` with a number or
   `unavailable` with a reason, and a missing value is never shown as 0. Access
   follows ADR 0014: System needs the admin role and an operator tenant. Lag
   values go to analysts and admins of operator tenants. Other analysts and
   admins get `operator_only` rows, and viewers get 403.
8. **One retry policy.** Retry loops wait with capped exponential backoff and
   full jitter (`internal/retry`: a uniform delay in `[0, ceiling]`), so
   replicas that fail together do not retry together. Callers keep their own
   attempt limits and budgets.
9. **Graceful shutdown inside a 40 s grace.** Compose gives every worker
   `stop_grace_period: 40s`. The lifecycle shutdown budget is 30 s and the
   writer `SHUTDOWN_TIMEOUT` is at most 30 s, so a worker finishes or abandons
   in-flight work and commits only completed offsets before Docker sends
   SIGKILL.

## Consequences

- Registry injection costs a constructor argument per component, but tests
  read exact values from a private registry and there is no global state.
- Lag measures durably processed work. A replica restoring state reports
  nothing rather than a guess. A detector holding an old pin can show lag 0,
  and the pin-retention gauge
  (`sentinel_detector_pin_retention_margin_offsets`) covers that hazard instead.
- Publication counters exceed unique alerts after a replay. Dashboards and
  the bench report read unique counts from the stores.
- JSON logs are harder to read by eye locally. `console` is a one-key opt-in.
  Keeping `ip` in logs is a deliberate privacy tradeoff for investigation.
- Edge sampling means a caller cannot force Sentinel to sample. A sampled
  ingest request is followed through detection and storage, while unsampled
  records cost a header copy and no spans. A full span queue drops spans
  rather than blocking the pipeline.
- The overlay is opt-in and not part of `make dev-up`, so the base stack has no
  Prometheus, and System/lag report `not_configured` there. Storage bounds mean
  history is short (2 days of metrics, 1000 traces).
- Fixed queries mean System cannot be extended through the API. A new panel
  needs a code change and a contract update. Readiness still works when
  Prometheus is down.
- Alert rules are examples. Two thresholds (ingest 5xx ratio 5%, pin margin
  100000 offsets) are placeholders until benchmark data exists.
