---
phase: 05-observability-resilience-scale
plan: "11"
subsystem: resilience
tags: [chaos, lag, overload, kafka, producer, rel-03, rel-04]
requirements: [REL-03, REL-04]
status: complete
dependency-graph:
  requires: [05-01 IngestMetrics/RegisterProducerBuffer/BufferedRecords, 05-05 LagTracker/WithLagTracker/ConsumerMetrics, 05-07 check-chaos.sh/check-resources.sh/FaultContainer, 05-09 consumer option shape, 05-10 tracing (gate family)]
  provides: [TestChaosSlowConsumerLag, TestChaosLagAfterRebalance, TestChaosOverloadBounded, kafkax.ErrBufferFull, whole-batch producer reservation, non-idempotent producers (ADR 0013), check-chaos.sh slow-consumer + overload families (9 total)]
  affects: [05-12 (producer/kafkax state), 05-13 (buffered_records gauge semantics and help text), 05-17 (make test-chaos), 05-18/05-20 bench overload scenario]
tech-stack:
  added: []
  patterns: [whole-batch admission reservation in front of franz-go, gauge reports the admission counter the cap enforces, gate sheds derived as requests minus Authenticate calls, kadm-independent backlog cross-check for lag]
key-files:
  created: [backend/test/chaos_slow_consumer_test.go, backend/test/chaos_overload_test.go, backend/internal/kafkax/producer_options_test.go, docs/decisions/0013-non-idempotent-bounded-producers.md]
  modified: [backend/internal/kafkax/producer.go, backend/internal/kafkax/dlq.go, backend/internal/kafkax/alerts.go, docs/decisions/README.md, backend/internal/kafkax/producer_test.go, backend/internal/telemetry/ingest.go, scripts/check-chaos.sh, scripts/test_check_chaos.py]
decisions:
  - "D-11 amended by user decision (Option A, ADR 0013): event, DLQ and alert producers use kgo.DisableIdempotentWrite() instead of kgo.AllowIdempotentProduceCancellation(); acks=all, delivery timeout, retries and buffer caps unchanged"
  - "A batch that does not fit MaxBufferedRecords is shed with ErrBufferFull (503 + Retry-After) before any record reaches franz-go, instead of blocking in Produce until the delivery timeout"
  - "sentinel_kafka_producer_buffered_records reports admitted, unresolved records (the reservation), not franz-go's BufferedProduceRecords, which also counts callers blocked in Produce and decrements only after a record's promise runs"
  - "In-flight bound is evidenced by exact peak concurrent publishes (<= 8) plus gate sheds = requests - Authenticate calls (> 0). An outer-middleware in-flight counter was rejected because its decrement runs after the semaphore release and can over-count"
metrics:
  duration: "~65 min of execution (17:37Z to 18:20Z, then 18:21Z to 18:42Z after the decision, 2026-11-17)"
  completed: 2026-11-17
---

# Phase 5 Plan 11: Slow-consumer and overload chaos slice Summary

Slow-consumer lag is proven on real Kafka and ClickHouse. It rises while producers outpace a slowed writer, returns to 0 after the slowdown, and still sums to the true backlog after a rebalance. The overload family found two defects, both fixed tests-first. The first was the buffered-records gauge exceeding its limit (2004/2000) while ingest blocked instead of shedding. The second was pre-existing: ingest sometimes acknowledged (202) batches that never reached Kafka (3 of 8 runs). The user chose Option A, which amends D-11 (ADR 0013), and all 9 chaos families now pass.

## D-11 amendment (user decision, Option A, ADR 0013)

The user chose Option A in chat, relayed by the coordinator. `kgo.AllowIdempotentProduceCancellation()` is replaced by `kgo.DisableIdempotentWrite()` on every producer client. The review found the same hazard in all three:

| Client | File | Hazard | Fixed |
|---|---|---|---|
| Event producer (ingest, also the alert readiness probe) | kafkax/producer.go | Yes. A 202 can be returned for records never written | Yes |
| DLQ publisher (event-writer, detector, alert-writer) | kafkax/dlq.go | Yes. A lost acknowledged DLQ record lets the poison record's source offset commit, so the record is skipped silently | Yes |
| Detector alert producer | kafkax/alerts.go | Yes. A lost acknowledged alert lets the detector commit past it, so the alert is lost | Yes |

`producer_options_test.go` (RED 2f94b39, GREEN 70864fe) pins four settings on every client through `kgo.Client.OptValue`: DisableIdempotentWrite = true, AllowIdempotentProduceCancellation = false, MaxProduceRequestsInflightPerBroker = 1 (preserves order) and RequiredAcks = AllISR. ADR 0013 (88fb119) records the rationale, evidence, rejected alternatives and consequences. `TestIngestRetryNoDuplicates` passes in a normal single run, so it was not changed. Its `-count>1` limitation (a once-per-process barrier) remains a harness note.

After the fix, the overload family passed 5 of 5 separate selector runs plus the full-selector run, with no acknowledged loss. Each run had 500 ambiguous events land, which is allowed under at-least-once.

## Original finding (pre-decision record)

`TestChaosOverloadBounded` failed intermittently on `acknowledged events missing from ClickHouse` (2000 = exactly 4 batches of 500). This is not caused by this plan's change. It is the interaction of locked decision D-11 (idempotent producer) with Phase 2 Pitfall 3 (`kgo.AllowIdempotentProduceCancellation()`).

**Evidence (diagnostic run, local, not committed):** `DIAG end=20000 records=20000 distinct ids in topic=20000 missing-from-CH: not in topic=2000 in topic=0 failed-overlap=0`. The 2000 acknowledged events are absent from the Kafka topic itself, and none overlap a failed publish.

**Mechanism (franz-go v1.22.1 source):**
- With cancellation allowed, in-flight batches to the paused broker are failed client-side (`sink.go` 1398, `failAllRecords`). `resetBatchDrainIdx` then rewinds `recBuf.seq` to `batch0Seq` without bumping the producer epoch.
- After unpause, the broker writes the cancelled batches; these are the "ambiguous landed" events.
- New batches reuse the same sequence ranges. Every publish here is a 500-record batch, so the ranges match exactly. Kafka answers a batch matching a cached sequence range with the cached offset and writes nothing (franz-go also treats `DuplicateSequenceNumber` as success, `sink.go` 1249).
- The new batch is therefore acknowledged without being written, and ingest returns 202.

**Run history:** 3 of 8 overload runs lost exactly 2000 acknowledged events (17:55 run, run 4 of the `-count=5` diagnostic run, the 18:11 selector run). The other 5 passed, each with 2000 cancelled events landing, which is allowed.

**Experiment (local, reverted):** replace `kgo.AllowIdempotentProduceCancellation()` with `kgo.DisableIdempotentWrite()`. franz-go then defaults to 1 in-flight produce request per broker, keeps no sequence state, and may fail in-flight records.
- `TestChaosOverloadBounded`: 5/5 PASS with no acknowledged loss (21,000 accepted, 21,500 unique, 500 ambiguous landed).
- `TestIngestKafkaTimeout503` and `TestIngestAcceptedAfterAck`: 5/5 PASS.
- `TestIngestRetryNoDuplicates`: PASS on the first iteration. Iterations 2 to 5 fail only because its prefix barrier is once-per-process (`prefixBarrierUsed` in failpoint_integration.go) and `-count=5` reuses the process, which is a harness artifact.

### Options presented (A chosen)

| Option | Effect | Cost |
|---|---|---|
| A. `DisableIdempotentWrite()` instead of `AllowIdempotentProduceCancellation()` (recommended) | Removes acknowledged-but-unwritten loss. ProduceTimeout stays honored. 1 in-flight request per broker preserves order | Changes D-11. Broker-side duplicate suppression is lost, so franz-go internal retries can duplicate. That is already true in practice with cancellation on, and duplicates are absorbed by ADR 0003 event_id dedupe |
| B. Drop `AllowIdempotentProduceCancellation()` and keep idempotence | No loss and no sequence rewind | ProduceSync can block past ProduceTimeout while the broker stalls (franz-go bypasses the retry limit while unsure). This breaks the bounded 503 of TestIngestKafkaTimeout503 (Phase 2 Pitfall 3) |
| C. Recreate the franz-go client (new producer ID) after any ambiguous failure | Keeps idempotence and bounded latency | Concurrency-sensitive client swap under live publishes. Significant new code |


## Observed values (from t.Logf, real containers)

**Slow consumer** (identical in two runs, 17:39Z and 18:10Z):
- 5000 events published over about 5 s against a writer slowed to 1 s per 250-event batch (lag tracker tick 1 s, constructor argument).
- Peak lag 3500 across 5 rising ticks. Lag at release was 3500, and it returned to 0 in 2.31 s.
- unique = committed = 5000.

**Lag after rebalance:**
- Writer A owned [0 1 2 3]. After B joined, A owns [0 1] and B owns [2 3], so [2 3] were revoked from A and are absent from A's Gather.
- Per-partition lag equals the kadm end minus committed: {0:200 1:500 2:500 3:500}. sum(lag) = backlog = 1700.
- Both replicas reached 0 3.01 s after the slowdown ended. unique = 2000.

**Overload** (passing run, GREEN code, 18:11Z selector run excluded because it failed on the loss assertion):

| Quantity | Value |
|---|---|
| Status histogram | 202: 20, 429: 2846, 503: 28080 (30,946 requests in 20 s, 64 clients) |
| Other statuses / transport errors / missing Retry-After | 0 / 0 / 0 |
| Gate sheds (requests minus Authenticate calls) | 28,017 |
| Buffer-full sheds (ErrBufferFull) | 51 |
| Max buffered / limit (from the registry) | 2000 / 2000 |
| Peak concurrent publishes / max_in_flight | 6 / 8 |
| Max HeapInuse (100 ms sampling, 201 samples) | 68.4 MiB (bound 256 MiB) |
| Accepted events / CH unique / ambiguous landed | 10,000 / 12,000 / 2000 |

Across all 13 overload runs (pre-fix, post-fix and experiment), max HeapInuse was 61.5 to 68.7 MiB, every status was 202, 429 or 503, and Retry-After was never missing. Pre-fix (17:47Z), the buffered gauge read **2004** against its limit of 2000.

## Per-gate results

| Gate | Time (UTC) | Result |
|---|---|---|
| `check-resources.sh` before every real-container run | 17:37 to 18:13 | PASS each time. Min 19.59 GiB free, MemAvailable 5.19 to 7.47 GiB |
| Task 1: `go-test-nonempty.sh … '^TestChaos(SlowConsumerLag\|LagAfterRebalance)$' -race` | 17:39 to 17:40 | PASS (9.32 s, 11.51 s) on first run |
| Causal check: revoke hook disabled (local) | 17:41 | TestChaosLagAfterRebalance FAIL: "rebalance did not split the partitions without overlap". Restored, `cmp` clean |
| Causal check: no `recordProcessed` after CommitRecords (local) | 17:43 | TestChaosSlowConsumerLag FAIL: "lag did not return to 0 with every record committed". Restored, `cmp` clean. This closes the 05-05 open item "CommitRecords → Processed has no real-broker test" |
| RED Task 2: `test_check_chaos.py` | 17:46 | FAILED (3): unknown family slow-consumer/overload |
| RED Task 2: overload on real containers | 17:47 to 17:50 | FAIL: `"2004" is not less than or equal to "2000"`, producer buffer exceeded its limit |
| RED: `go test ./internal/kafkax/` | 17:52 | build failed: `ErrBufferFull`, `maxBuffered` undefined |
| RED 2: `TestBufferedRecordsReportsAdmittedRecordsWithinTheLimit` | 17:58 | FAIL: expected 6, actual 0 |
| GREEN: `go test -race` kafkax, ingest, telemetry, cmd/ingest; `test_check_chaos.py` | 18:00 | ok; 8 tests OK |
| `gofmt -l`, `go vet` (both tags), golangci-lint `./...` and `--build-tags=integration ./...`, `go mod tidy -diff` | 18:09 | clean, 0 issues, 0 issues, clean |
| `CHAOS_FAMILIES=slow-consumer bash scripts/check-chaos.sh` | 18:10:13 to 18:10:53 | PASS exit 0, 40 s (ceiling 10m) |
| `CHAOS_FAMILIES=overload bash scripts/check-chaos.sh` | 18:11:05 to 18:11:59 | **FAIL** exit 1, 54 s (ceiling 6m): 2000 acknowledged events missing (pre-existing D-11 defect above) |
| `CHAOS_FAMILIES=chaos-broker` (uses BufferedRecords) | 18:12:09 to 18:12:55 | PASS, 46 s. `broker restart: total records=40 unique=40 max buffered=20` |
| `CHAOS_FAMILIES=rate-limit` (TestIngestShedsUnderLoad) | 18:12:58 to 18:13:22 | PASS, 24 s |
| 05-10-named family: `'^(TestIngest(AcceptedAfterAck\|RetryNoDuplicates\|KafkaTimeout503)\|TestTracingEdgePropagation\|TestDetectorCapacity)$'` | 18:13:32 to 18:14:10 | PASS: DetectorCapacity 7.23 s, RetryNoDuplicates 7.00 s, KafkaTimeout503 4.69 s, AcceptedAfterAck 0.10 s, TracingEdgePropagation 0.22 s |
| `make test` | 18:14 to 18:15 | exit 0 (39 Go packages ok, web 174, generator 262) |
| `make lint` | 18:15 to 18:16 | exit 0 |
| `python3 -m unittest discover -s scripts -p 'test_*.py'` | 18:16 to 18:19 | 93 tests, 1 error: pre-existing `test_seed_scenarios.py` `from datetime import UTC` on Python 3.10.12 (not touched) |

Pre-decision note (superseded by the post-fix gates below): the full 9-family run was deferred until the decision. Overload is blocked by the decision, and the 7 earlier families were last run green at d9c89fc (05-07). chaos-broker and rate-limit, the two that exercise changed code, were rerun above. sse-reconnect needs an isolated compose stack and was not needed for this plan's changes. No compose project was created, and no Docker resource outside testcontainers was touched. At the end, `docker ps -a --filter label=org.testcontainers=true` was empty.

## Post-fix gates (code at 88fb119, resource gate before each)

| Gate | Time (UTC) | Result |
|---|---|---|
| RED: `go test -run CannotRewindSequences ./internal/kafkax/` | 18:21 | FAIL x3, "idempotent write must be disabled" (expected true, actual false) |
| GREEN: `go test -race` kafkax, ingest, detector, cmd/...; golangci-lint kafkax | 18:22 | ok, 0 issues |
| `CHAOS_FAMILIES=overload bash scripts/check-chaos.sh` x5 (separate runs) | 18:24:16 to 18:28:30 | 5/5 PASS (53, 49, 48, 48, 50 s). Accepted 22,000 / 21,000 / 21,500 / 21,000 / 21,500, each with CH unique = accepted + 500 ambiguous. Max HeapInuse 60.5 to 68.6 MiB, max buffered 2000/2000, peak publishes 5 to 6 of 8, 31 to 33 buffer-full sheds |
| `TestIngest(KafkaTimeout503\|AcceptedAfterAck\|RetryNoDuplicates)` | 18:28:38 to 18:29:09 | PASS (4.81 s, 0.13 s, 6.77 s) |
| Isolated stack: infra part of `make dev-up` (kafka, clickhouse, postgres, redis, topic-init, migrate) on `COMPOSE_PROJECT_NAME=sentinel-chaoscheck` | 18:29:32 to 18:29:54 | 4 containers healthy, 3 topics, CH migrations applied. Ports 15432, 16379, 29092, 19000 and 18123 were free beforehand |
| Full selector: `COMPOSE_PROJECT_NAME=sentinel-chaoscheck bash scripts/check-chaos.sh` | 18:30:30 to 18:39:32 | **EXIT 0, 9/9 PASS, total 542 s** (table below). Gate: MemAvailable 6.47 GiB |
| Teardown: `COMPOSE_PROJECT_NAME=sentinel-chaoscheck make reset CONFIRM=1` | 18:39:40 | 4 containers, 3 volumes and the network removed. Afterwards there were no sentinel-chaoscheck containers or volumes and 0 testcontainers |
| `make test` | 18:40 | exit 0 (39 Go packages ok, web 174, generator 262) |
| `make lint`; golangci-lint `--build-tags=integration ./...`; `go mod tidy -diff`; gofmt; `check-adrs.sh` | 18:41 | exit 0; 0 issues; clean; clean; 11 OK |

| Family | Result | Wall (s) | Ceiling | Evidence |
|---|---|---|---|---|
| chaos-broker | PASS | 41 | 8m | `total records=40 unique=40 max buffered=20` |
| chaos-clickhouse | PASS | 44 | 10m | `short outage absorbed after 1 retry waits` |
| chaos-shutdown | PASS | 31 | 10m | SIGTERM to clean exit in 95.28 ms, 9.98 ms, 9.27 ms and 9.48 ms |
| writer-restart | PASS | 225 | 12m | |
| dlq | PASS | 36 | 10m | |
| rate-limit | PASS | 23 | 8m | |
| sse-reconnect | PASS | 55 | 10m | TestPhase4Delivery 13.65 s, TestPhase4Stream 38.39 s (project sentinel-chaoscheck) |
| slow-consumer | PASS | 41 | 10m | Peak lag 4000 over 5 rising ticks, 0 after 2.62 s, unique 5000. After the rebalance, sum = backlog = 1700 |
| overload | PASS | 46 | 6m | 202: 42, 429: 3475, 503: 24145. Buffered 2000/2000, peak publishes 6/8, HeapInuse 63.1 MiB, accepted 21,000, CH unique 21,500, 500 ambiguous landed |

## Selector table (`scripts/check-chaos.sh`, 9 families)

| Family | Packages | Tests | Tag | Ceiling |
|---|---|---|---|---|
| chaos-broker | ./test/ | TestChaosKafkaBrokerRestart | integration | 8m |
| chaos-clickhouse | ./test/ | TestChaosClickHouse{ShortOutageAbsorbed,LongOutageRestart} | integration | 10m |
| chaos-shutdown | ./test/ | TestChaosGracefulShutdown{EventWriter,Detector} | integration | 10m |
| writer-restart | ./test/ | TestWriterCrashRestart, TestWriterCommitAfterInsert, TestWriterInsertFailureNoCommit, TestDetectorCrashBoundaries | integration | 12m |
| dlq | ./test/ | TestWriterPoisonToDLQ, TestWriterDLQFailureNoCommit, TestDetectorPoison, TestAlertWriterPoison | integration | 10m |
| rate-limit | ./test/ | TestRateLimit.*, TestIngestShedsUnderLoad | integration | 8m |
| sse-reconnect | ./testphase4/... | TestPhase4Stream, TestPhase4Delivery | phase4 | 10m |
| slow-consumer (new) | ./test/ | TestChaosSlowConsumerLag, TestChaosLagAfterRebalance | integration | 10m |
| overload (new) | ./test/ | TestChaosOverloadBounded | integration | 6m |

The per-name `-list` guard is unchanged. `test_selector_covers_every_spec_family` pins the 9 names.

## Commits

| Hash | Message |
|---|---|
| 39b4aac | test(05-11): slow-consumer lag rise/recovery and rebalance-sum chaos tests |
| 31b70b1 | test(05-11): add failing overload-bounded, producer reservation and selector tests |
| 56d3dca | test(05-11): add failing test that the buffered gauge reports admitted records |
| 7f472b1 | feat(05-11): shed batches that do not fit the producer buffer; add chaos families |
| 9f922b1 | docs(05-11): record slow-consumer/overload results and the blocking acked-loss finding |
| 2f94b39 | test(05-11): add failing tests that producers cannot rewind sequence numbers |
| 70864fe | fix(05-11): disable idempotent write on every producer instead of cancelling |
| 88fb119 | docs(05-11): ADR 0013 non-idempotent bounded producers (amends D-11) |

## Deviations from Plan

**1. [Rule 1 - Bug] Buffered gauge exceeded its limit and ingest blocked instead of shedding**
- Found during: Task 2 real-container run (2004 > 2000).
- Issue: `Producer.BufferedRecords` returned franz-go `BufferedProduceRecords()`, which is `bufferedRecords + blocked`. ProduceSync callers blocked in Produce on a full buffer were counted, and they waited up to ProduceTimeout instead of shedding. franz-go also calls a record's promise before decrementing its count, so releasing a reservation on ProduceSync return could not make franz-go's own sum exact.
- Fix:
  - Whole-batch reservation against MaxBufferedRecords. A batch that does not fit returns `ErrBufferFull` before any record is produced (503 + Retry-After via the existing handler path).
  - `BufferedRecords` returns the reservation. Help text: "Records admitted to the Kafka producer and not yet acknowledged or failed."
  - Production defaults cannot fill the reservation (64 × 500 = 32,000 < 50,000).
- Files: backend/internal/kafkax/producer.go, backend/internal/telemetry/ingest.go
- Commits: 31b70b1 and 56d3dca (RED), 7f472b1 (GREEN)

**2. [Plan parameter] 500-event batches instead of 200.** With 200-event batches and 8 in flight, at most 1600 records can be buffered, so the 2000 limit could never bind and the buffer assertion would be vacuous. 500 is ingest's MaxBatchSize, so 8 × 500 exceeds the limit. Limiter: capacity 2000, refill 2000/s (the plan says "e.g."), with ProduceTimeout 3 s. Broker paused from t=4 s to t=14 s of the 20 s run.

**3. [Assertion design] In-flight evidence.** The plan allows a test hook or derivation. I used both:
- The exact peak of concurrent publishes, at most 8.
- Gate sheds = requests minus Authenticate calls, which must be greater than 0.

I first required peak publishes == 8. That only held while full-buffer callers blocked. After the GREEN change, rejected publishes return immediately, so I replaced it with the exact gate-shed derivation, plus `bufferFull > 0` and "the buffer filled to within one batch of the cap". No plan bound was relaxed.

**4. No-loss assertion form.** The plan says "accepted == CH unique". A 503 publish whose records were in flight can still land (at-least-once), so the test asserts three things:
- Every accepted ID is in ClickHouse.
- Every extra stored ID belongs to a failed (503) publish recorded server-side.
- When there are no extras, accepted == unique.

This is stricter on loss and does not count legal ambiguous duplicates as failures.

**5. [Rule 4, resolved] D-11 amended.** The acknowledged-loss defect needed a decision. The user chose Option A, and it is implemented in all three producers (2f94b39 RED, 70864fe GREEN, 88fb119 ADR).

## Deferred Issues

- Pre-existing: `scripts/test_seed_scenarios.py` fails to import on Python 3.10 (`datetime.UTC`), which is outside this plan's scope.
- Pre-existing: `TestIngestRetryNoDuplicates` cannot run with `-count>1` (once-per-process prefix barrier). It is a harness limitation, harmless in CI (`-count=1`).
- Pre-existing, unchanged: 22 phase4-tag lint findings (deferred-items.md).

## Interface notes for 05-12 and 05-13

- `kafkax.ErrBufferFull` is exported. `Producer` has new unexported fields `maxBuffered` and `reserved`, and white-box struct literals must set `maxBuffered`, otherwise every batch is shed. `NewProducer` sets it from `MaxBufferedRecords`.
- `sentinel_kafka_producer_buffered_records` now means admitted, unresolved records, and it can never exceed `_limit`. Dashboards and alerts (05-13) can use `buffered / limit` as saturation, where 1.0 means the next batch sheds. Producer shedding appears as `sentinel_ingest_requests_total{status="503"}` and `sentinel_ingest_events_total{result="unavailable"}`.
- All producers are non-idempotent (ADR 0013). Any new producer, such as a worker that publishes in 05-12, must use `RequiredAcks(AllISRAcks())` and `DisableIdempotentWrite()`, must not use `AllowIdempotentProduceCancellation()`, and must be added to `producer_options_test.go`. franz-go retries can duplicate records, so downstream must stay idempotent by `event_id` / `alert_id`.
- The 05-18/05-20 bench should measure, not assume, throughput with one in-flight produce request per broker.
- Lag tests show `LagTracker` interval 1 s works as a constructor argument. Production stays at 5 s.

## Known Stubs

None.

## Threat Flags

None. No new endpoint or trust-boundary surface. T-05-11-01 is mitigated (tested bounds, no acknowledged loss). T-05-11-02 is mitigated (kadm cross-check). T-05-11-03 is mitigated (finite ceilings, a 3 min test budget and bounded clients).

## Self-Check: PASSED

- FOUND: backend/test/chaos_slow_consumer_test.go, backend/test/chaos_overload_test.go, scripts/check-chaos.sh (slow-consumer, overload)
- FOUND commits: 39b4aac, 31b70b1, 56d3dca, 7f472b1, 9f922b1, 2f94b39, 70864fe, 88fb119
- FOUND: backend/internal/kafkax/producer_options_test.go, docs/decisions/0013-non-idempotent-bounded-producers.md (README row present; check-adrs.sh OK)
- `grep -c "^func TestChaos" backend/test/chaos_slow_consumer_test.go` = 2. `HeapInuse` and `buffered_records_limit` both match in chaos_overload_test.go.
- STATE.md and ROADMAP.md not touched. .planning/config.json and .serena/ not staged.
