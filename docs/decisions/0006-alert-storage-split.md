# 0006. Alert storage split

## Status

Accepted, 2026-10-10. Decisions: D-24. Recorded as the intended model; details are finalized in Phase 3.

## Context

Alerts have two kinds of data: immutable detection evidence produced by the detector, and mutable analyst workflow state (status, assignment, notes, audit trail). ClickHouse suits immutable append-heavy data; PostgreSQL suits transactional updates.

## Decision

- ClickHouse holds immutable detections.
- PostgreSQL holds mutable workflow state, audit rows and the alert list index, with a unique `alert_id`.
- The query API joins the two at application level.

## Consequences

- Alert writes are idempotent through the unique `alert_id`.
- No cross-database transactions; the join is in the application and must tolerate a detection that has no workflow row yet.
- Phase 3 defines the concrete tables and the join strategy.
