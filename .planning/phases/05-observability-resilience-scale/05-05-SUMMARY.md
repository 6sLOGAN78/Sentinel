---
phase: 05-observability-resilience-scale
plan: "05"
subsystem: observability
tags: [prometheus, kafka, lag, clickhouse, dlq, eventwriter, alertwriter]
requirements: [OBS-02]
dependency-graph:
  requires: [05-01 telemetry registry/handler/allowlist guard, 05-02 retry loops]
  provides: [kafkax.LagTracker, kafkax.WithLagTracker, telemetry.ConsumerMetrics, writer WithMetrics builders, DLQPublisher.WithMetrics]
  affects: [05-09 detector lag (reuse LagTracker with processedNext), 05-11 chaos lag rise/fall, 05-13 dashboards, 05-15/05-16 lag endpoint and System page, 05-18 bench recovery]
tech-stack:
  added: []
  patterns: [position-based lag polled with kadm ListEndOffsets independently of the fetch loop, DeleteLabelValues on revoke/lost, decorator store for write timing]
key-files:
  created: [backend/internal/kafkax/lag.go, backend/internal/kafkax/lag_test.go, backend/internal/kafkax/export_test.go, backend/internal/telemetry/consumer.go, backend/internal/telemetry/consumer_test.go, backend/internal/eventwriter/metrics_test.go, backend/internal/alertwriter/metrics_test.go]
  modified: [backend/internal/kafkax/consumer.go, backend/internal/kafkax/consumer_test.go, backend/internal/kafkax/alert_consumer.go, backend/internal/kafkax/dlq.go, backend/internal/kafkax/dlq_test.go, backend/internal/telemetry/ingest.go, backend/internal/telemetry/registry.go, backend/internal/eventwriter/writer.go, backend/internal/alertwriter/writer.go, backend/cmd/event-writer/main.go, backend/cmd/alert-writer/main.go]
decisions:
  - "NewConsumer binds the tracker internally: when the tracker has no lister, NewConsumer sets it to kadm.NewClient(consumer client). Mains pass nil as the lister, and no Admin() accessor or second Kafka client is added"
  - "NewConsumer rejects a tracker whose group/topic differ from the consumer's (after NewAlertConsumer's rewrite), which catches miswiring"
  - "When the committed offset and start offset cannot be resolved, a partition stays unseeded and exports no series until its first commit. The tracker does not invent a lag value"
  - "Processed ignores partitions this process does not own, so a commit that arrives after OnPartitionsLost cannot recreate a deleted series"
  - "Run returns nil on cancel and an error only when it has no offset source or sink. Transient ListEndOffsets errors keep the last gauge values"
  - "alertwriter observes CH writes through an observedDetections decorator installed by WithMetrics, so reservation.go is unchanged"
metrics:
  duration: ~10 min (00:50Z to 01:00Z)
  completed: 2026-11-14
---

# Phase 5 Plan 05: Writer consumer lag, CH write latency and DLQ metrics Summary

The event-writer and alert-writer now serve these on their ops `/metrics`:

- `sentinel_kafka_consumer_lag{group,topic,partition}`, computed as broker end offset minus the next offset the process has durably processed. A kadm `ListEndOffsets` poll (every 5 s, 2 s timeout) runs independently of the fetch loop, so the value keeps rising while ClickHouse retries stall polling.
- `sentinel_clickhouse_write_duration_seconds{result=ok|error}` for every insert attempt.
- `sentinel_dlq_events_total{reason}`, counted only after the all-ISR acknowledgement.

Revoke and loss delete the lag series, so `sum()` across replicas equals the backlog.

## Commits

| Hash | Message |
|---|---|
| 88ceebd | test(05-05) RED: lag, rebalance cleanup, write latency and DLQ count tests (only `*_test.go`: lag_test, consumer_test, dlq_test, export_test, telemetry consumer_test) |
| 4d4195b | feat(05-05) GREEN: LagTracker, ConsumerMetrics, consumer hooks, DLQ counter |
| 2146144 | test(05-05) RED: writer CH write observation tests (only `*_test.go`) |
| 22aedf6 | feat(05-05) GREEN: writer instrumentation and binary wiring |

## LagTracker API (`backend/internal/kafkax/lag.go`)

- `type EndOffsetLister interface { ListEndOffsets(ctx, topics ...string) (kadm.ListedOffsets, error) }`
- `type CommittedOffsetSource interface { FetchOffsets(ctx, group) (kadm.OffsetResponses, error); ListStartOffsets(ctx, topics ...string) (kadm.ListedOffsets, error) }`
- `type LagSink interface { SetLag(group, topic, partition, lag); DeleteLag(group, topic, partition) }`
- `*kadm.Client` satisfies both offset interfaces, and `*telemetry.ConsumerMetrics` satisfies `LagSink`.
- `NewLagTracker(lister, sink, group, topic, interval) *LagTracker`. If interval is 0 or less it uses `defaultLagInterval` (5 s). The query timeout is `lagQueryTimeout` (2 s).
- `Assigned(map[int32]int64)`: a negative value marks the partition unseeded.
- `SeedAssigned(ctx, src, []int32)`: takes the committed offset, falls back to the start offset (the consumer resets AtStart), and otherwise leaves the partition unseeded. Each query has a 2 s timeout.
- `Processed(partition, next)`: only moves forward and only for owned partitions.
- `Revoked([]int32)`: removes the partitions from the map and calls `sink.DeleteLag`.
- `Tick(ctx) error`: makes one `ListEndOffsets` call. It skips the call when nothing is owned. It sets `max(0, end-next)` under the same mutex as `Revoked`. A per-partition load error keeps that partition's last value.
- `Run(ctx) error`: ticks immediately, then once per interval.
- It does not use `kadm` committed-lag APIs (grep `kadm.*Lag(` in lag.go: 0 matches).

## Consumer option and binding choice

- `type ConsumerOption func(*consumerOptions)` and `WithLagTracker(t *LagTracker) ConsumerOption`.
- `NewConsumer(ctx, kafka, writer, options ...ConsumerOption)` and `NewAlertConsumer(..., options...)` forward the options. All existing kgo options and budgets are unchanged.
- With a tracker set, the consumer registers `kgo.OnPartitionsAssigned` (calls `SeedAssigned` through the consumer's kadm client) and `kgo.OnPartitionsRevoked` / `kgo.OnPartitionsLost` (both call `Revoked`). After a successful commit, `CommitRecords` calls `Processed(partition, maxOffset+1)` for each partition.
- **Binding:** NewConsumer binds `kadm.NewClient(client)` as the tracker's lister when the lister is nil. Mains build `kafkax.NewLagTracker(nil, cm, group, topic, 0)`. There is no `Admin()` accessor.

## Binary wiring and lifecycle order

Both mains create `telemetry.NewRegistry()` and `telemetry.NewConsumerMetrics(reg)`, then wire them in:

- The tracker's group/topic is EventWriterGroup/EventsTopic in the event-writer and AlertWriterGroup/AlertsTopic in the alert-writer.
- `dlq.WithMetrics(cm)` and the writer's `.WithMetrics(cm)` are attached, and the ops server gets `opshttp.WithMetricsHandler(telemetry.Handler(reg))`.
- `startLagTracker` follows the startDeliveryProjector shape: cancel, then a done channel, then `lc.Add("lag-tracker", …)` waiting on done or closeCtx. If Run returns an error while ctx is still live, it calls `draining.Begin()` and then `lc.Fail`.

Registration order (`lc.Names()`); close runs in reverse:

- event-writer: `ops-http, clickhouse, kafka-consumer, lag-tracker, kafka-dlq, event-writer, draining`
- alert-writer: `ops-http, kafka-consumer, lag-tracker, kafka-dlq, clickhouse, postgres, alert-writer, draining`

In both binaries, LIFO close stops the lag tracker before the Kafka consumer closes. There is no `lc.Names()` unit test: `setup` opens real ClickHouse, Kafka and PG connections, and the cmd packages have no test harness. The order above was read from the code.

## Verification (all run for real, from backend/)

| Gate | Time (UTC) | Result |
|---|---|---|
| RED 1: `go test ./internal/kafkax/... ./internal/telemetry/...` | 00:51:50 to 00:52:00 | exit 1, build failed (`NewConsumerMetrics`, `NewLagTracker`, `Consumer.lag/admin/onAssigned` undefined). This proves the identifiers were absent, nothing more. |
| GREEN 1: `go test -race -count=1 ./internal/kafkax/... ./internal/telemetry/...` | 00:53:05 to 00:53:25 | exit 0 |
| `golangci-lint run ./internal/kafkax/... ./internal/telemetry/...` | 00:53:30 | exit 1 (3 goconst, 1 unparam), fixed. Rerun 00:54: 0 issues, exit 0 |
| RED 2: `go test ./internal/eventwriter/... ./internal/alertwriter/...` | 00:55:08 to 00:55:13 | exit 1, build failed (`Writer.WithMetrics` undefined) |
| Task 3 verify: `go test -race -count=1 ./internal/eventwriter/... ./internal/alertwriter/... ./internal/kafkax/... ./cmd/...`, plus `go build ./cmd/...`, `go build -tags=integration ./...`, `go vet -tags=integration ./test/...` and lint on the writers and both cmds | 00:56:30 to 00:56:58 | all exit 0, 0 issues |
| Full module: `go vet ./...`, then `go test -race -count=1 ./...` | 00:57:21 to 00:58:09 | exit 0, exit 0 |
| `golangci-lint run ./...` and `--build-tags=integration ./...`, plus `go mod tidy -diff` | 00:58:13 to 00:58:20 | 0 issues, 0 issues, exit 0 |

**Behavioral mutation checks** (each mutation was reverted and `cmp` confirmed the file was restored):

| Mutation | Test(s) that failed |
|---|---|
| No `DeleteLag` in Revoked | TestLagRevokedDeletesSeriesAndStaysAbsent, TestLagSumAcrossReplicasEqualsBacklogAfterRebalance, TestConsumerLagHooksTrackAssignmentCommitAndRevoke |
| Non-monotonic Processed | TestLagProcessedIsMonotonic |
| Query without the 2 s timeout | TestLagTickBoundsEveryQueryWithTimeout |
| No `IncDLQ` | TestDLQPublishCountsOnlyAcknowledgedRecords |
| No revoke hook | TestConsumerLagHooksTrackAssignmentCommitAndRevoke |
| No kadm bind | TestNewConsumerRejectsLagTrackerForAnotherGroupOrTopic |
| No ObserveWrite in eventwriter | TestWriterObservesEveryInsertAttemptIncludingRetries |
| No ObserveWrite in alertwriter | TestAlertWriterObservesClickHouseInsertByResult |

**Acceptance greps:**

- consumer.go matches both `OnPartitionsRevoked` and `OnPartitionsLost`.
- lag.go matches `ListEndOffsets`.
- lag_test.go has 14 `func Test` matches (at least 5 required).
- Both mains match `WithMetricsHandler` and `NewLagTracker`.
- `ObserveWrite(` appears once in each writer.go.
- Both RED commits contain only `*_test.go` files.

**Not run:**

- `make test` / `make lint`: the Makefile is owned by Phase 4. The equivalent go commands above were run instead.
- No Docker, no integration-tag test execution and no live `/metrics` curl against a running writer. The plan defers integration proof of lag rising and falling to 05-11.
- `CommitRecords → Processed` against a real broker has no direct test. The unit test covers `recordProcessed` and the hooks through the internal test, but the call site inside `CommitRecords` needs a broker. 05-11 should assert it.

## Deviations from Plan

1. **[Rule 3 - test seam]** I added `backend/internal/kafkax/export_test.go` (`NewDLQPublisherWithProducer`) and a `produce` func field on DLQPublisher. Without them, an acknowledged or failed publish can't be scripted: there is no kfake dependency, and installing one was not approved. The production path still calls `client.ProduceSync(...).FirstErr()`.
2. **Extra tests outside the plan's file list:**
   - Internal hook/binding tests in `consumer_test.go`, an existing white-box file.
   - Writer metric tests in new `eventwriter/metrics_test.go` and `alertwriter/metrics_test.go`.
   - Task 3 was split into RED and GREEN commits to follow the project convention.
3. **[Rule 3 - lint]**
   - goconst flagged `"result"`, which appeared 3 times across telemetry. I added an unexported `labelResult` const and used it in `ingest.go` and `registry.go` (05-01 files, not owned by Phase 4).
   - unparam flagged a lag test helper. I strengthened that test by revoking and asserting the absence of a second partition. This changed lag_test.go in the GREEN commit, adding assertions only.
4. **alertwriter:** I added a decorator in writer.go instead of editing the `InsertBatch` call site in `reservation.go`. The decorator observes every CH alert insert attempt the same way.
5. **Characterization test:** `TestDLQPublishCopiesTraceparent` passes on the existing header-copy code once it compiles (`dlqCopyHeaders` already copies non-`dlq.` headers). It locks in that behavior for OBS-04 and was not a causal RED test.
6. **Mains:** I passed a nil lister to `NewLagTracker` instead of `admin`. See "Binding" above. The `draining` construction moved to the top of `setup` so the tracker's failure path can use it.

## Known Stubs

None.

## Threat Flags

None. No new network endpoint: `/metrics` is on the existing internal ops listener. The admin calls use the existing consumer client, with one `ListEndOffsets` per tick and two seed queries per assignment, each with a 2 s timeout. Labels are limited to `group`, `topic` and `partition` for lag, `result` for write latency and `reason` for DLQ counts, and the allowlist guard tests pass.

## Self-Check: PASSED

- FOUND: backend/internal/kafkax/lag.go, backend/internal/telemetry/consumer.go, backend/internal/kafkax/lag_test.go, backend/internal/telemetry/consumer_test.go, backend/internal/kafkax/export_test.go, backend/internal/eventwriter/metrics_test.go, backend/internal/alertwriter/metrics_test.go
- FOUND commits: 88ceebd, 4d4195b, 2146144, 22aedf6
