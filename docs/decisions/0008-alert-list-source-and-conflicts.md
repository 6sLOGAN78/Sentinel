# 0008. Alert list authority and immutable conflicts

## Status

Accepted engineering policy, 2026-10-17. Refines ADR 0006 and spec §7.3.

## Decision

ClickHouse stores immutable detection evidence. PostgreSQL is authoritative for the tenant-scoped alert list and mutable workflow, including status, assignee, updated_at, optimistic version, audit and unique alert_id/seq. Store immutable filter/list fields and a canonical immutable fingerprint in the PG index; detail/evidence joins CH at application level. Introduce no alert TTL without a separate retention requirement; event TTL remains independent.

Serialize by alert identity with a transaction-scoped per-alert lock, validating tenant and existing immutable fingerprint before effects. Inspect existing ClickHouse content, including orphan detections left before PG creation. Existing immutable content wins; equal replay is safe, different content sharing an identity is quarantined and cannot overwrite either store. Use parameterized SQL and bounded lock/query/rollback deadlines. A failed dependency is retryable infrastructure failure, not poison.

Write CH immutable evidence first, then insert PG list/workflow rows transactionally, then commit the source Kafka position. PG creation is insert-only: equal replay never resets status, assignment, timestamps, version, seq or audit. An orphan CH row is absent from the authoritative list until matching PG creation succeeds; conflicting orphan content must be detected before creating workflow. No cross-database transaction is claimed. Durable cooldown allow/suppress decisions are operational recovery rows, separate from these workflow/list rows; suppressed candidates create no CH detection or PG workflow.

## Phase 4 handoff

List/detail reads enforce tenant scope and bounded pagination. Workflow changes use optimistic version checks and atomic audit transactions. Sequence allocation is unique and monotonic allocation, **not transaction commit order**: concurrent transactions may commit in reverse seq order. SSE backfill must explicitly handle that gap; a simple `seq > last_seen` cursor is insufficient proof of lossless delivery. Phase 3 preserves seq on replay and does not claim to implement Phase 4's stream algorithm.
