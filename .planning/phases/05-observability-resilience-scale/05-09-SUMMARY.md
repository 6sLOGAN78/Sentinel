---
phase: 05-observability-resilience-scale
plan: "09"
subsystem: observability
tags: [prometheus, detector, lag, capacity, pin-retention, seed-scenarios]
requirements: [OBS-02]
status: code complete, real-container gate pending
dependency-graph:
  requires: [05-01 telemetry registry/handler/allowlist guard, 05-05 LagTracker/ConsumerMetrics/WithLagTracker, 05-07 check-resources.sh (code only)]
  provides: [telemetry.DetectorMetrics, detection.Bounds Usage()/Limits(), detector.Worker WithMetrics/WithPositions/Limits, detector.PositionSink, kafkax.NewDetectorConsumer WithLagTracker option, DetectorConsumer.PinMargins/RunPinMargins, kafkax.PinMarginSink]
  affects: [05-10/05-11 (same packages; integration tests compile against the new NewDetectorConsumer signature), 05-13 dashboards, 05-18 bench preflight/recovery, 05-19 docs rename]
tech-stack:
  added: []
  patterns: [processed-position lag fed from committed checkpoint metadata ProcessedNext, pin-margin poll deleting series of unowned partitions, injectable worker clock]
key-files:
  created: [backend/internal/telemetry/detector.go, backend/internal/telemetry/detector_test.go, backend/internal/kafkax/detector_lag_test.go]
  modified: [backend/internal/telemetry/consumer.go, backend/internal/telemetry/registry.go, backend/internal/detection/bounds.go, backend/internal/detection/bounds_test.go, backend/internal/detector/worker.go, backend/internal/detector/worker_test.go, backend/internal/kafkax/detector_consumer.go, backend/internal/kafkax/detector_consumer_test.go, backend/cmd/detector/main.go, backend/internal/opshttp/metrics.go, backend/internal/opshttp/metrics_test.go, backend/test/detector_capacity_test.go, scripts/seed_scenarios.py, scripts/test_seed_scenarios.py]
decisions:
  - "Processed position = ProcessedNext from the committed checkpoint metadata (last processed offset + 1), reported only after commitEffects succeeds. The pin (cp.Offset) is never used as the processed position. A mutation test proves the difference (pin 0 vs processed 11)"
  - "Lag seed on restore = committed pin, or the retained HistoryStart for a new group, because the worker replays from there. LagTracker.Processed is monotonic, so replay advances it from that seed"
  - "rule_id/severity labels come from the acknowledged canonical alert JSON, which is exactly what was published. Both are bounded enums (4 rules, fixed severities)"
  - "Pin margin uses the in-memory committed checkpoint (updated after every successful commit) minus a 2 s bounded ListStartOffsets. Partitions still restoring or with no committed pin export nothing"
  - "NewDetectorConsumer now takes (…, observer, options ...ConsumerOption) instead of variadic observers, so it can reuse the 05-05 WithLagTracker option"
  - "seed_scenarios keeps the existing docker container identity check (container_id/started_at/pid). process_start_time_seconds was not adopted"
metrics:
  duration: ~13 min (13:07Z to 13:20Z)
  completed: 2026-11-16
---

# Phase 5 Plan 09: Detector metrics slice Summary

The detector's ops `/metrics` now serves the shared per-process registry (`opshttp.WithMetricsHandler(telemetry.Handler(reg))`) with the full detector metric set. Consumer lag comes from the worker's processed position, and the pin retention margin is polled. The hand-rolled `opshttp.DetectionMetrics` and the unprefixed `detection_late_events_total` are gone from code and tests.

**Status: code complete, real-container gate pending.** The resource gate refused (details below), so the `TestDetectorCapacity` integration family was not run.

## Metric names (all on the detector ops listener)

| Metric | Type | Labels | Source |
|---|---|---|---|
| `sentinel_detection_alerts_total` | counter | rule_id, severity | `Worker.recordPublication`, after `PublishCanonical` acks. Help text: replays after a crash count again; unique alerts come from PG/CH |
| `sentinel_detection_delay_seconds` | histogram, ExponentialBuckets(0.05, 2, 14) | none | `w.now()` at publish ack minus the candidate's `TriggerOccurredAt` |
| `sentinel_detection_late_events_total` | counter | none | `Worker.recordLateness` (reducer `Outcome.Late`) |
| `sentinel_detector_state_windows`, `_ledger_entries`, `_bytes` | gauge | none | `Bounds.Usage()` after every successful commitEffects |
| `sentinel_detector_state_windows_limit`, `_ledger_entries_limit`, `_bytes_limit` | gauge | none | `Bounds.Limits()`, set once at startup in cmd/detector |
| `sentinel_detector_pin_retention_margin_offsets` | gauge | topic, partition | `DetectorConsumer.RunPinMargins` every 30 s |
| `sentinel_kafka_consumer_lag` | gauge | group, topic, partition | 05-05 LagTracker for DetectorGroup/EventsTopic, fed by `Worker.WithPositions(tracker)` |
| `sentinel_dlq_events_total` | counter | reason | `dlq.WithMetrics(consumerMetrics)` |

All label names are in the ADR 0004 allowlist. `TestDetectorMetricsLabelsInsideAllowlistAndNilSafe` asserts that `LabelNamesOutsideAllowlist` is empty.

## Processed-position call site

`backend/internal/detector/worker.go`:
- `process()` calls `w.reportProgress(checkpoints)` immediately after `w.commitEffects(...)` returns nil.
- `reportProgress` calls `w.metrics.SetState(w.bounds.Usage())` and then `w.positions.Processed(tp.Partition, m.ProcessedNext)` (line 269) for each committed partition.
- A failed commit returns before it, so nothing is reported (`TestWorkerReportsNoPositionWhenCommitFails`).

## Lifecycle (cmd/detector)

Registration order is `ops-http, postgres, kafka-consumer, lag-tracker, pin-margins, kafka-alerts, kafka-dlq, detector, draining`. LIFO close stops both pollers before the Kafka consumer closes. `startPoller` uses the 05-05 startLagTracker shape. `startWorker` was extracted to keep `setup` under funlen.

## Remaining doc consumers of the old name (owned by 05-19)

- `docs/architecture.md:61`
- `docs/operations.md:115`

`grep -rln "detection_late_events_total" docs | sort` lists exactly these two. `scripts/test_seed_scenarios.py` keeps the old name only in `test_late_metric_rejects_old_unprefixed_name`.

## Commits

| Hash | Message |
|---|---|
| b14b512 | test(05-09): add failing tests for detector metrics, bounds usage and late-metric rename (test files only) |
| 7f2e9b8 | feat(05-09): detector metrics, bounds usage accessor and late-metric rename |
| 8431477 | test(05-09): add failing detector lag hook and pin-margin tests (test files only) |
| 951ca8d | feat(05-09): detector position lag and pin retention margin polling |

## Verification (all run for real)

| Command | Time (UTC) | Result |
|---|---|---|
| `bash scripts/check-resources.sh` (start) | 13:07:06 | exit 3, REFUSED (MemAvailable 1.97 GiB) |
| RED 1: `go test ./internal/telemetry/... ./internal/detection/... ./internal/detector/... ./internal/opshttp/...` | 13:09:05 | exit 1. Build failed: `NewDetectorMetrics`, `Bounds.Usage/Limits`, `Worker.now/WithMetrics` undefined. opshttp ok (its test was only trimmed) |
| RED 1 Python: `generator/.venv/bin/python -B -m unittest discover -s scripts -p test_seed_scenarios.py` | 13:09 | FAILED (failures=1, errors=1). The 200 KiB body hit the 8 KiB cap, and the old name was still accepted. These are the right causes |
| GREEN 1: `go test -race -count=1` on detector/opshttp/telemetry/detection, plus lint on those packages and cmd/detector | ~13:11 | exit 0, 0 issues after the goconst/funlen fixes |
| GREEN 1 Python unittest | ~13:11 | 14 tests OK |
| RED 2: `go test ./internal/kafkax/...` | 13:13:55 | exit 1. Build failed: `DetectorConsumer.topic/lag/starts`, `trackRestored`, `PinMargins` undefined |
| GREEN 2: `go test -race -count=1 ./internal/{telemetry,detection,detector,kafkax,opshttp}/... ./cmd/...` | 13:15:23 to 13:16:15 | exit 0 |
| `go tool -modfile=tools.mod golangci-lint run ./...` and `--build-tags=integration ./...`; `go build ./... && go build -tags=integration ./...`; `go mod tidy -diff` | 13:15 to 13:16 | 0 issues, 0 issues, exit 0, exit 0 |
| `make test` (go test -race ./... with 38 packages ok, web vitest 174 passed, generator pytest 262 passed) | 13:16:49 to 13:18:28 | exit 0 |
| `make lint` (golangci-lint, web lint and typecheck, ruff check/format, mypy) | 13:18:37 to 13:18:45 | exit 0. Tree clean afterwards |

**Behavioral mutation checks.** Each mutation was reverted, and `cmp` confirmed the file was restored.

| Mutation | Failing test |
|---|---|
| No `reportProgress` call | TestWorkerMetricsAndProcessedPosition |
| `reportProgress` before commitEffects | TestWorkerReportsNoPositionWhenCommitFails |
| No `recordPublication` | TestWorkerMetricsAndProcessedPosition |
| No `IncLate` | TestWorkerMetricsAndProcessedPosition |
| Report the pin (`cp.Offset`) instead of ProcessedNext | TestWorkerMetricsAndProcessedPosition (expected 11, got 0) |
| No `lag.Revoked` in releaseAssignments | TestDetectorLagHooksSeedFromRestoredStartAndRevoke, TestDetectorLagSeedsFromCommittedPinAndDropsOnLoss |
| No `trackRestored` after fetch | TestDetectorLagHooksSeedFromRestoredStartAndRevoke |
| Seed ignores the committed pin | TestDetectorLagSeedsFromCommittedPinAndDropsOnLoss |
| No `DeletePinMargin` for unowned partitions | TestDetectorPinMarginTickPublishesAndDeletesRevoked |
| PinMargins query without the 2 s timeout | TestDetectorPinMargins |
| No tracker group/topic check | TestNewDetectorConsumerRejectsLagTrackerForAnotherGroupOrTopic |
| Pending restores included in margins | TestDetectorPinMargins |

**Acceptance greps:**
- `git diff 491beb0 -- backend/internal/detection/reducer.go` is empty.
- The bounds.go constant-change guard returns nothing.
- `Processed(` is present in worker.go.
- The old name appears nowhere outside `test_*.py`.
- `WithMetricsHandler` is present in cmd/detector/main.go.
- `METRICS_BODY_LIMIT` is present in seed_scenarios.py.
- `WithDetectionMetrics|DetectionMetrics{` returns nothing under backend/.
- Both RED commits contain only test files.

## Plan-level gate: DEFERRED (real containers)

`bash scripts/check-resources.sh` refused both times, so the `TestDetectorCapacity` integration family (`go-test-nonempty.sh './test/...' '^TestDetectorCapacity' -tags=integration`) was **not run**. The floor was not lowered and nothing was pruned.

Start of plan (13:07:06Z):
```
resource gate: ok repo /home/logan78/Desktop/Sentinel free 10.39 GiB (floor 10 GiB)
resource gate: ok docker-root /var/lib/docker free 10.39 GiB (floor 10 GiB)
resource gate: REFUSED MemAvailable is 1.97 GiB; floor is 3 GiB. Sentinel will not prune; free space yourself
EXIT=3
```
End of plan (13:18:50Z):
```
resource gate: REFUSED repo /home/logan78/Desktop/Sentinel has 8.67 GiB free; floor is 10 GiB. Sentinel will not prune; free space yourself
resource gate: REFUSED docker-root /var/lib/docker has 8.67 GiB free; floor is 10 GiB. Sentinel will not prune; free space yourself
resource gate: REFUSED MemAvailable is 1.42 GiB; floor is 3 GiB. Sentinel will not prune; free space yourself
EXIT=3
```
The disk drop between runs (10.39 to 8.67 GiB) happened during `make test`/`make lint`. The likely cause is the Go build/test cache and web/generator tool caches on the same filesystem. Nothing was deleted.

Pending proof for whoever re-runs once resources allow:
- `late_counter_runtime` must see a line exactly equal to `sentinel_detection_late_events_total 1` in the promhttp body. The old `!strings.Contains(body, "{")` guard was dropped because promhttp bodies always contain labeled series.
- A live `curl` of the detector `/metrics` showing the lag and pin-margin series.
- `make seed-scenarios` reading the renamed counter from a body larger than 8 KiB.

## Deviations from Plan

1. **[Rule 3 - TDD shape] Two RED/GREEN cycles.** Task 1's RED did not cover the kafkax hooks or PinMargins, so a second tests-only RED (8431477) preceded the kafkax GREEN (951ca8d). The detector package's `TestDetectorFailpointBuildBoundary` compiles cmd/detector. To keep that test passing, GREEN 1 also included the cmd/detector metrics wiring, the opshttp deletion and the seed_scenarios change (Task 3 work). GREEN 2 added the lag-tracker and pin-margin wiring.
2. **[Lint] Unnamed returns.** `Bounds.Usage()` returns `(int, int, int)` instead of named `(windows, ledger, bytes int)` because `nonamedreturns` is enabled. This matches the existing `PartitionState.Usage()`.
3. **[Rule 3 - lint] Shared label constants.** goconst flagged `"partition"` as repeated across telemetry. I added unexported `labelTopic` and `labelPartition` next to `labelResult` in consumer.go and used them in consumer.go, detector.go and registry.go (05-01/05-05 files, no behavior change).
4. **[Rule 3 - lint] Extracted helpers in cmd/detector.** I extracted `startWorker` and `startPoller` because `setup` hit funlen (53 > 50).
5. **Extra API, for testability:**
   - `DetectorConsumer.RunPinMargins(ctx, sink, interval)`, `kafkax.PinMarginSink`, and `Worker.Limits()`. Main only calls RunPinMargins.
   - `NewDetectorConsumer` changed from variadic observers to `(observer, options ...ConsumerOption)`. Callers updated: `detector.NewKafkaConsumer` (now forwards options) and the existing kafkax test.
   - `backend/test/detector_dlq_test.go` needed no change.
6. **seed_scenarios.** Counter values are parsed as `int(float(...))` so promhttp exponent formatting cannot break parsing. The over-limit body raises `SeedError("detector metrics body exceeds 1 MiB")`.
7. **Telemetry test.** `TestDetectorMetricsLateCounter` lost a negative assertion that named the old metric. It would have violated the "Go tests use only the new name" acceptance grep. Rejection of the old name is covered in Python.

## Known Stubs

None.

## Threat Flags

None. There is no new endpoint: `/metrics` is on the existing internal ops listener. The new admin traffic is read-only: one `ListEndOffsets` every 5 s and one `ListStartOffsets` every 30 s, on the consumer's existing client, each bounded at 2 s.
- T-05-09-01: labels are inside the allowlist (tested).
- T-05-09-02: reducer.go is unchanged and the bounds constants are untouched.
- T-05-09-03: 1 MiB cap with a test.
- T-05-09-04: the help text says "publications".

## Self-Check: PASSED

- FOUND: backend/internal/telemetry/detector.go, backend/internal/telemetry/detector_test.go, backend/internal/kafkax/detector_lag_test.go
- FOUND commits: b14b512, 7f2e9b8, 8431477, 951ca8d
- STATE.md and ROADMAP.md were not touched, as instructed.

## Real-container gate (run 2026-11-17)

`scripts/check-resources.sh` PASS: 18.07 GiB free on the repo and docker root, MemAvailable 4.88 GiB. `go-test-nonempty.sh './test/...' '^TestDetectorCapacity' -tags=integration -timeout=10m -p=1` exit 0: `--- PASS: TestDetectorCapacity (7.53s)`, package ok in 68.95s at HEAD 9350868. The plan is now fully complete.
