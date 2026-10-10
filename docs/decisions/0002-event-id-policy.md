# 0002. Event ID policy

## Status

Accepted, 2026-10-10. Decisions: D-14, D-30.

## Context

Delivery is at-least-once, so every consumer and store must be idempotent. Idempotency needs a stable identity per event that survives client retries.

## Decision

- `event_id` is required and client-supplied.
- It is validated with JSON Schema `format: uuid` (asserted with `AssertFormat()`). Any UUID version is accepted; the generator emits deterministic UUIDv7-shaped IDs from its seed.
- The server never generates per-attempt IDs.
- Retrying the same batch with the same `event_id` values is the documented idempotency contract. Downstream stores deduplicate on `event_id`.

## Consequences

- Retries after a 503 or timeout are safe and are absorbed by dedup.
- Clients that reuse an `event_id` for different content will have the later event dropped as a duplicate; this is documented behavior.
- Clients must be able to produce UUIDs.
