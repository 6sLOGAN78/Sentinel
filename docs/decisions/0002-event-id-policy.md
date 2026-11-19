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
- List reads (`ListEvents`, 05-22) resolve winners only for candidate identities, not for the whole tenant. Candidates are raw rows of the tenant selected by range, cursor and filters in table key order, up to a bounded cap. Winners are then re-checked against the same predicates and kept only at or above the smallest candidate key. The cap widens by a fixed factor up to 20,000 keys. Past that limit the read fails with 503 and never returns a silently short page. `TestListEventsOracleEquivalence` proves the result equals the previous whole-tenant dedupe page for page, including conflicting payloads with different `occurred_at`, equal-received ties and identical retries. `TestListEventsScale` proves it scales to 1,000,000 events per tenant.
- Known scaling limit: the winner stage reads the `event_id` column across all of the tenant's retained rows, because a conflicting representation may have any `occurred_at`. Only the `idx_event_id` bloom filter skips granules. Its cost therefore grows with 7-day retention volume and with bloom false positives, which rise with the candidate count. This is acceptable at the 1M-per-tenant benchmark target.
- The Overview event summary (`SummarizeEvents`, 05-23) makes at most two round trips and never runs a whole-tenant tuple argMin. The main pass runs one `GROUP BY event_id` over the tenant and reads only `tenant_id`, `event_id` and `occurred_at`, computing `lo = min(occurred_at)` and `hi = max(occurred_at)`. A winner is one of its identity's rows, so its `occurred_at` lies in `[lo, hi]`. When `lo = hi`, the winner's time is `lo` whatever the tie-break, so the identity is bucketed directly. Identities with `lo != hi` that can still land in the window (`hi >= start` and `lo < now`) are conflicting payloads. Only those identities get the canonical argMin, in a separate correction query bound as `event_id IN {ids:Array(String)}`. That query runs only when such identities exist. At most 10,000 conflicting identities are corrected; more return 503, never a partial summary. `TestSummarizeEventsOracleEquivalence` proves the result equals the previous whole-tenant summary, including conflicts across the 24 h start and the 1-minute rate boundary. `TestSummarizeEventsScale` proves it at 1,000,000 events per tenant, and `TestSummarizeEventsConflictCap` covers the cap.
- Summary snapshot semantics: the result reflects the main pass's read point. Rows inserted between the two round trips may be missing from it. This is benign because ClickHouse events are append-only, and the correction only refines identities the main pass already counted as conflicting, so it never adds an identity.
- Known summary scaling limit: the main pass aggregates every identity the tenant retains (7-day TTL), not only the 24 h window, because a conflicting representation may have any `occurred_at`. Its cost is O(tenant identities over the 7-day TTL) in hash-aggregation memory and in reading three columns. At 1M identities it took about 0.1 s and 250 MB on an idle test container.
- Clients must be able to produce UUIDs.
