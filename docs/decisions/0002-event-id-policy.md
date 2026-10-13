# 0002. Event ID policy

## Status

Accepted, 2026-10-10. Decisions: D-14, D-30.

## Context

Delivery is at-least-once, so every consumer and store must be idempotent. Idempotency needs a stable identity per event that survives client retries.

## Decision

- `event_id` is required and client-supplied.
- It is validated with JSON Schema `format: uuid` (asserted with `AssertFormat()`). Any UUID version is accepted; the generator emits deterministic UUIDv7-shaped IDs from its seed.
- The server never generates per-attempt IDs.
- Retrying the identical batch with the same `event_id` values is the documented idempotency contract. ClickHouse stores may retain multiple physical rows, while every user-visible lookup and count collapses them by `event_id` immediately without waiting for merges.
- Lookup selects the earliest currently retained row by `received_at`. Clients must never reuse an ID for different logical content. If conflicting payloads share an ID, the visible winner is unspecified: `ReplacingMergeTree` may retain another row after merges, and equal-millisecond `received_at` values do not establish immutable first-content-wins.

## Consequences

- Retries after a 503 or timeout are safe and are absorbed by dedup.
- D-15 guarantees one visible identity, not unique physical storage or an immutable payload winner.
- `TestDedupeDoubleInsertNoOptimize` proves identical retries collapse immediately at read time, and `TestDedupeDifferentOccurredAt` proves that guarantee spans monthly partitions without asserting which conflicting payload wins.
- Clients must be able to produce UUIDs.
