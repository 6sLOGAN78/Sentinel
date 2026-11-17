# 0013. Non-idempotent bounded Kafka producers

## Status

Accepted, 2026-11-17. The user chose this in plan 05-11 ("Option A"). It amends Phase 2 decision D-11 ("franz-go with the idempotent producer, `acks=all` and bounded retries"): `acks=all` and bounded retries stay, and idempotent write is turned off. It is implemented in `backend/internal/kafkax/producer.go`, `dlq.go` and `alerts.go`, and guarded by `producer_options_test.go` and the `overload` chaos family (`TestChaosOverloadBounded`).

## Context

Ingest may return 202 only after Kafka acknowledges the batch (ING-03), and a publish must give up within `PRODUCE_TIMEOUT` so ingest can shed with 503. franz-go refuses to cancel in-flight idempotent records by default, so Phase 2 (Pitfall 3) added `kgo.AllowIdempotentProduceCancellation()` to the event producer. The DLQ publisher and the detector alert producer copied it.

franz-go documents that this option breaks idempotency's duplicate avoidance. Plan 05-11 found that it also loses acknowledged records:

- While a broker stalls, in-flight batches are failed client-side. franz-go then rewinds the partition's sequence number to the first unacknowledged batch (`failAllRecords` then `resetBatchDrainIdx` in v1.22.1) without bumping the producer epoch.
- After the stall the broker writes the cancelled batches anyway.
- The next batches reuse the same sequence ranges. Kafka answers a batch that matches a cached sequence range with the cached offset and writes nothing, and franz-go also treats `DUPLICATE_SEQUENCE_NUMBER` as success. The new batch is acknowledged but never stored.

Evidence: `TestChaosOverloadBounded` sends 64 clients' 500-event batches for 20 s, with the broker paused for 10 s. In 3 of 8 runs, ingest returned 202 for 2000 events (4 batches) that were absent from the topic itself. The topic held 20,000 distinct records, and the missing IDs overlapped no failed publish. With idempotent write disabled, 5 of 5 runs lost nothing.

## Decision

Every Sentinel producer (event ingest, DLQ, detector alerts) uses `kgo.DisableIdempotentWrite()` instead of `kgo.AllowIdempotentProduceCancellation()`. These stay unchanged:

- `RequiredAcks(AllISRAcks())`
- `RecordDeliveryTimeout(PRODUCE_TIMEOUT)` and `RecordRetries`
- the buffer caps, and the whole-batch admission reservation in front of the event producer, which sheds with 503 (05-11)

Without idempotence, franz-go keeps no sequence state, so a cancelled batch cannot make a later batch disappear. It also defaults to one in-flight produce request per broker, which keeps per-partition order. In-flight records can still be failed, so `PRODUCE_TIMEOUT` is still honored. `producer_options_test.go` pins these four settings on all three clients.

Rejected alternatives:

- Keeping idempotence without cancellation: `ProduceSync` can block past `PRODUCE_TIMEOUT` while a broker stalls, because franz-go bypasses the retry limit when it is unsure whether a batch was written. This breaks the bounded 503.
- Recreating the client after any ambiguous failure: a client swap under concurrent publishes, with substantial new code.

## Consequences

- No more acknowledged-but-unwritten records from sequence rewinds. A failed or timed-out publish is ambiguous: it may still land, and the caller sees 503 or an error and retries. This is at-least-once, as spec §3 requires.
- franz-go's internal retries can write a batch twice. Duplicates are absorbed by event identity (ADR 0002, read-time dedupe by `event_id`), alert identity (`alert_id`, ADR 0006) and DLQ origin headers. No exactly-once claim is made.
- One in-flight produce request per broker caps per-broker pipelining. Batches are still large, and the overload test and later benchmarks measure throughput instead of assuming it.
- Any new producer must use the same options and be added to `producer_options_test.go`.
