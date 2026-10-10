# 0003. Atomic batch semantics

## Status

Accepted, 2026-10-10. Decisions: D-25.

## Context

Ingest accepts batches of events. Callers need clear semantics for partial failure, and Kafka cannot give a transaction across a whole HTTP request without extra cost.

## Decision

- The whole batch is validated before anything is published. Any invalid event returns 400 with zero publishes.
- 202 is returned only after every record has been acked by Kafka.
- If Kafka fails mid-batch, the response is 503. Between 0 and N records may already have been published. Clients retry the identical batch, and duplicates are absorbed by `event_id` dedup downstream (ADR 0002).
- "Atomic" describes response semantics, not a Kafka transaction.
- Implementation note: Echo may mishandle a literal `:batch` path segment (a custom-method style route such as `/v1/events:batch`). Phase 2 must verify routing for this path before relying on it.

## Consequences

- Clients get a simple rule: 202 means all accepted, 400 means nothing published, 503 means retry the same batch.
- Duplicate records in Kafka are possible after 503 and must be tolerated by consumers.
- No Kafka transactions are needed, keeping the producer path simple.
