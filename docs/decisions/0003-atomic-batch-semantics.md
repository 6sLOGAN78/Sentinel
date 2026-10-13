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
- Echo registers the route with an escaped colon (`/api/v1/events\\:batch`). A literal request path ending in `events:batch` dispatches to ingest. An unrelated suffix, a wrong method, and a percent-encoded `/api/v1/events%3Abatch` request do not dispatch; the encoded-colon form returns 404.
- Load shedding may return 503 before authentication. Otherwise invalid credentials return 401 first, and an authenticated key missing `events:write` returns 403 before body or quota work. For a valid scoped key, body/count size returns 413, malformed envelopes return 400, quota returns 429, any reliably parsed cross-tenant event returns 403, and remaining event validation returns 400 with at most 50 indexed details. Phase 2 plan 02-08 owns implementation and acceptance of the quota combinations.

## Consequences

- Clients get a simple rule: 202 means all accepted, 400 means nothing published, 503 means retry the same batch.
- Duplicate records in Kafka are possible after 503 and must be tolerated by consumers.
- No Kafka transactions are needed, keeping the producer path simple.
