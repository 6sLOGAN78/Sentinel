# 0004. Metrics label policy

## Status

Accepted, 2026-10-10. Decisions: D-26.

## Context

High-cardinality Prometheus labels exhaust memory and make dashboards unusable. Spec section 11.2 forbids them.

## Decision

- Allowed label dimensions: `result`, `status`, `rule_id`, `severity`, `reason`, `group`, `topic`, `partition`, `service`.
- Forbidden labels: IP, principal, event_id, alert_id, request_id, tenant_id.
- Consumer lag is position-based: the partition high watermark minus the last processed offset. It is not committed-offset lag, which stalls when commits are batched or paused.

## Consequences

- Per-tenant or per-entity analysis uses logs, traces or ClickHouse, not metrics.
- New metrics must cite this ADR and use only allowed labels.
- Lag reflects actual processing progress.
