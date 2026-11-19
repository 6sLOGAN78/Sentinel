---
phase: 05-observability-resilience-scale
plan: "24"
subsystem: telemetry
tags: [kafka, consumer-lag, coverage, prometheus, gap-closure, GAP-2, OBS-02]
requires:
  - phase: 05-observability-resilience-scale
    provides: "05-19 docs drift guard, 05-21 position-based LagTracker and consumer lag metric"
provides:
  - "sentinel_kafka_topic_partitions{group,topic} gauge (ConsumerMetrics.SetTopicPartitions)"
  - "LagSink.SetTopicPartitions; LagTracker.Tick lists end offsets even with zero owned partitions"
  - "docs/operations.md coverage PromQL: count by (group) (lag) < max by (group) (topic_partitions)"
affects: [05-25 lag API/System coverage, 05-28 bench, 05-20 re-run]
tech-stack:
  added: []
  patterns: [coverage denominator exported by every member, never-shrink publication on partial listing]
key-files:
  created: []
  modified:
    - backend/internal/kafkax/lag.go
    - backend/internal/kafkax/lag_test.go
    - backend/internal/kafkax/detector_lag_test.go
    - backend/internal/kafkax/consumer_test.go
    - backend/internal/telemetry/consumer.go
    - backend/internal/telemetry/consumer_test.go
    - docs/operations.md
    - scripts/test_observability_config.py
decisions:
  - "Topic partition count is published only when the topic is present in the listing with at least one partition and every entry has no error and a non-negative offset; otherwise the previous value stays (a missing topic is not a zero-partition topic)"
  - "docs/operations.md says the lag endpoint and System do not check coverage yet (05-25 adds it) instead of claiming they already do"
metrics:
  duration: "~40 min"
  completed: 2026-11-19
  tasks: 2
  files: 8
---

# Phase 5 Plan 24: Topic partition coverage gauge Summary

Every consumer process (event-writer, detector, alert-writer) now exports `sentinel_kafka_topic_partitions{group,topic}`. The value is the partition count of its topic as the broker lists it, published even when the process owns no partitions. An orphaned partition during a failover therefore shows up as `count by (group) (sentinel_kafka_consumer_lag) < max by (group) (sentinel_kafka_topic_partitions)` and no longer as a false 0 lag.

## Tasks

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | RED: partition-count gauge and zero-owned publication tests | fd31d3c | kafkax/{lag,detector_lag,consumer}_test.go, telemetry/consumer_test.go |
| 2 | GREEN: gauge, Tick change, docs, drift inventory | bdabb01 | kafkax/lag.go, kafkax/lag_test.go, telemetry/consumer.go, docs/operations.md, scripts/test_observability_config.py |

## What changed

- **telemetry/consumer.go**: new GaugeVec `sentinel_kafka_topic_partitions` with labels {group, topic}. Help text: "Partitions of the consumed topic as listed by the broker; coverage denominator for sentinel_kafka_consumer_lag." `SetTopicPartitions` is nil-safe. A `labelGroup` constant was added because goconst flagged the third `"group"` literal.
- **kafkax/lag.go**: `LagSink` gains `SetTopicPartitions`. `Tick` no longer returns early when it owns nothing. It always runs one 2 s-bounded `ListEndOffsets`, publishes the count through `partitionCount()`, and then publishes per-partition lag exactly as before. A whole-listing error returns the error as before and leaves the count unchanged.
- **docs/operations.md**: a metric table row, plus a coverage paragraph and PromQL under "Consumer lag (position-based)". The paragraph lists when coverage is incomplete: rebalance, a dead member's 45 s session timeout, and an unseeded new assignment.
- **scripts/test_observability_config.py**: the inventory test now expects the new name. The DocsDriftTests table check passes with no other change.

## RED evidence (fd31d3c)

- kafkax failed on assertions, and only in the new cases: `TestLagTickWithoutOwnedPartitionsStillPublishesTopicPartitions`, `TestLagTickPublishesTopicPartitionCountIndependentOfOwnership`, `TestLagTopicPartitionsNeverShrinkOnListingErrors`, `TestConsumerLagHooksTrackAssignmentCommitAndRevoke` (new assertion) and `TestDetectorLagSeedsFromCommittedPinAndDropsOnLoss` (new zero-owned tail).
- telemetry failed to build: `metrics.SetTopicPartitions undefined`.
- To keep the kafkax RED at the assertion level, the test sink forwarded through an optional-interface assertion. GREEN replaced that with a direct call.

## Verification

- `go test -race ./...` (all default packages): pass.
- `go test -race ./internal/kafkax/... ./internal/telemetry/... ./cmd/...`: pass.
- golangci-lint (tools.mod): `./...` 0 issues, `--build-tags=integration ./...` 0 issues. gofmt clean.
- `python3 -B -m unittest discover -s scripts -p test_observability_config.py`: 32 tests OK.
- Integration tests on isolated testcontainers (resource gate PASS: 15.69 GiB disk, 7.23 GiB RAM): TestChaosSlowConsumerLag, TestChaosLagAfterRebalance, TestDetectorThirdReplicaRebalance and TestWriterSlowPoisonRebalance all pass (57.7 s).

## Deviations from Plan

1. **[Rule 1 - Correctness] Missing or empty topic listing does not publish 0.** The plan says to publish `len(ends[topic])` when every entry is clean. With an absent topic that length is 0, which would shrink the denominator. Such a listing is now skipped and tested.
2. **[Rule 1 - Accuracy] Docs wording.** The plan's text says the lag endpoint and System "report this as unavailable (05-25)", but 05-25 has not run. The docs say they do not check coverage yet and that 05-25 adds it.
3. **Replaced test.** `TestLagTickWithoutOwnedPartitionsSkipsQuery` asserted the old behaviour (no admin call when nothing is owned), which this plan deliberately removes. It was replaced by the zero-owned publication test, which still asserts the 2 s deadline.
4. **Lint fixes in GREEN.** Fixes for goconst (`labelGroup`) and unparam (the recovery step now tests a 6 to 7 partition increase). These went into the GREEN commit, so there is no separate refactor commit.

## Known Stubs

None.

## TDD Gate Compliance

RED `test(05-24)` fd31d3c, then GREEN `feat(05-24)` bdabb01. No separate refactor commit was needed.

## Self-Check: PASSED

- FOUND: backend/internal/kafkax/lag.go, backend/internal/telemetry/consumer.go, docs/operations.md
- FOUND: fd31d3c, bdabb01
