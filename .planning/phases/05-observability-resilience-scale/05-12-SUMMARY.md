---
phase: 05-observability-resilience-scale
plan: "12"
subsystem: observability
tags: [opentelemetry, tracing, kafka-headers, detector, event-writer, alert-writer]
requirements: [OBS-04, OBS-03]
status: complete (real-container gate passed)
dependency-graph:
  requires: [05-10 telemetry.NewTracing/HeaderCarrier/allowlist, 05-09 detector WithMetrics/WithPositions, 05-05 writer ObserveWrite, 05-11 non-idempotent producers]
  provides: [detector.Worker WithTracing/WithLogger, kafkax.AlertProducer WithPropagator, kafkax.SampledRecordContext, eventwriter.Writer WithTracing (store span, maxStoreLinks=32), alertwriter.Writer WithTracing (store_alert span), telemetry.SampledTraceLogger, worker binaries wired with tracing]
  affects: [05-13 (Jaeger overlay must set the TRACING env keys on detector, event-writer and alert-writer too), 05-14 (query-api tracing reuses the same helpers), 05-18/05-20 bench (sampled-only worker spans), 05-19 docs]
tech-stack:
  added: []
  patterns: [span only for sampled parents (extract then IsSampled), batch span parented by first sampled record plus <=32 deduplicated links, span start and end in one function (spancheck), trace_id on worker logs only for sampled context]
key-files:
  created: [backend/test/tracing_pipeline_test.go]
  modified: [backend/internal/detector/worker.go, backend/internal/detector/worker_test.go, backend/internal/kafkax/alerts.go, backend/internal/kafkax/alerts_test.go, backend/internal/kafkax/headers.go, backend/internal/eventwriter/writer.go, backend/internal/eventwriter/writer_test.go, backend/internal/alertwriter/writer.go, backend/internal/alertwriter/poison.go, backend/internal/alertwriter/writer_test.go, backend/internal/telemetry/tracing.go, backend/internal/telemetry/tracing_test.go, backend/internal/telemetry/consumer.go, backend/internal/telemetry/detector.go, backend/internal/telemetry/registry.go, backend/cmd/detector/main.go, backend/cmd/event-writer/main.go, backend/cmd/alert-writer/main.go]
decisions:
  - "The event-writer store span is parented by the first sampled record context (in partition/offset order) and also links it. A parentless root would be sampled by TraceIDRatioBased, so at bench ratios most store spans for sampled traces would be dropped"
  - "store links are deduplicated by (trace ID, span ID): every record of one ingest request carries the same traceparent"
  - "AlertProducer gets WithPropagator(prop), not WithTracing(tp, prop). It starts no span of its own (detect covers the publish), so a provider argument would be unused"
  - "The detector had no logger. WithLogger was added, and each acknowledged publication logs `alert published` (rule_id, partition, offset, plus trace_id when sampled). No tenant, IP or principal"
  - "Worker span attribute keys added to the export allowlist: rule_id, partition, offset, batch.size, batch.partitions"
  - "If one record fires more than one rule, rule_id on its detect span holds the last candidate. Alert counts by rule stay correct in metrics"
metrics:
  duration: ~16 min (18:45Z to 19:01Z)
  completed: 2026-11-17
---

# Phase 5 Plan 12: Worker tracing slice Summary

A sampled ingest request is now traced through the workers. The trace runs ingest server span → `publish` → `detect` (a child via the event record's `traceparent`) → `store_alert` (a child via the alert record's `traceparent`). The event-writer adds one `store` span per ClickHouse batch, linked to the sampled record contexts. Workers start spans only for sampled parents. Worker log lines for sampled records carry `trace_id`. All three worker binaries build `telemetry.Tracing` and flush it after the worker and Kafka clients stop. TestTracingPipelinePropagation proves the full chain on real Kafka, ClickHouse and PostgreSQL.

## Spans

| Span | Service | Parent | Attributes | Notes |
|---|---|---|---|---|
| `POST /api/v1/events:batch` | ingest | new root (public endpoint) | from 05-10 | unchanged |
| `publish` | ingest | server span | from 05-10 | its context is the record `traceparent` |
| `detect` | detector | record header (remote) | `partition`, `offset`, `rule_id` on the firing record | one per sampled record. Kind consumer. Error status `detect failed` |
| `store` | event-writer | first sampled record in the batch (remote) | `batch.size` (events inserted), `batch.partitions` | links ≤ `maxStoreLinks` = 32, deduplicated. Each retry is a span event `retry` with `attempt`. Terminal failure gets status `store failed` |
| `store_alert` | alert-writer | alert record header (remote) | `partition`, `offset` | wraps CH + PG effects and any quarantine. Error status `store_alert failed` |

Span status descriptions are fixed strings, and driver error text never reaches a span. No span carries tenant, IP, principal or key material. The export allowlist drops anything else.

## Sampling follow

`kafkax.SampledRecordContext(ctx, prop, record)` extracts the header and reports `IsValid && IsSampled`. If the record is not sampled, the worker uses the original ctx and starts nothing, so it exports no span and puts no traceparent on the alert record. All three unit suites run with ratio 1, where a root span would be sampled, and still export nothing for unsampled or missing headers. That shows the skip comes from the worker, not from the sampler. Trace context is observability-only. Detection, ordering, reachBoundary and commit code never read it (T-05-12-03), and the regression families below are green.

## Logs (OBS-03)

`telemetry.SampledTraceLogger(ctx, log)` adds `trace_id` only for a sampled span context. Lines that use it:
- detector `alert published` (new, Info, one per acknowledged alert publication)
- event-writer `event insert failed; retrying` (inside the store span)
- alert-writer `alert effect failed; retrying` (inside store_alert)

## Binary wiring and shutdown order

Detector, event-writer and alert-writer each call `telemetry.NewTracing(ctx, cfg.Observability.Tracing, config.Service*)` and register `lc.Add("tracing", tr.Shutdown)` right after `ops-http`. LIFO close therefore runs it after the worker, the consumers and the producers, and Shutdown flushes (T-05-12-04). The detector passes `.WithTracing(...).WithLogger(log)` to the worker and `WithPropagator` to the alert producer. The writers pass `.WithTracing(...)`. In the detector, `openTracing` and `openPublisher` were extracted to keep `setup` under funlen.

## Integration test evidence

`TestTracingPipelinePropagation` (backend/test/tracing_pipeline_test.go, `//go:build integration`, 4 min budget):
- In-process traced ingest (05-10 fixture) feeds real Kafka. The in-process detector worker writes to a unique alerts topic, which the in-process alert-writer reads. An in-process event-writer reads the events topic. ClickHouse and PostgreSQL are real.
- Each service has its own provider and `service.name`, sharing one in-memory exporter, at ratio 1.
- Traffic is nine `auth.failure` events in one request, the tenth (threshold-crossing) in a second request, then an `auth.success` window-advance marker in a third.
- The test reads the threshold trace ID from the tenth event's Kafka `traceparent`. It asserts that trace has the server span, `publish`, `detect` (with `rule_id=AUTH_BRUTE_FORCE`) and `store_alert`, with parent links server→publish→detect→store_alert. It also asserts that a `store` span links that trace ID.
- Mutation check: removing `p.inject` from `PublishCanonical` makes it fail with `missing store_alert span`. The source was restored before commit.

## Gates

| Gate | Command | Result |
|---|---|---|
| Resource gate (before each run) | `bash scripts/check-resources.sh` | PASS (18.86 to 18.90 GiB free, MemAvailable 7.05 to 7.33 GiB) |
| New test alone | `go-test-nonempty.sh './test/...' '^TestTracingPipelinePropagation$' -tags=integration -timeout=15m -p=1` | PASS (1.81s, package 17.4s) |
| Plan gate | `go-test-nonempty.sh './test/...' '^(TestTracingPipelinePropagation\|TestDetectorCrashBoundaries\|TestWriterCommitAfterInsert\|TestAlertWriterPipeline)$' -tags=integration -timeout=15m -p=1` | PASS. TestTracingPipelinePropagation 1.49s, TestDetectorCrashBoundaries 148.52s, TestWriterCommitAfterInsert 0.34s, TestAlertWriterPipeline 0.84s. 7 PASS lines, 0 FAIL, package 169.4s |
| Extra related families | `'^(TestTracingEdgePropagation\|TestDetectionPipelineAuth\|TestDetectorPoison)$'` | PASS (0.17s / 4.21s / 2.44s) |
| Unit (race) | `go test -race ./internal/... ./cmd/...` | PASS |
| `make test` | Go race, web 174, generator 262 | exit 0 |
| `make lint` | golangci-lint, web lint + typecheck, ruff, mypy | exit 0 |
| Lint with integration tag | `golangci-lint run --build-tags=integration ./...` | 0 issues |
| Module tidy | `go mod tidy -diff` | clean |

No compose project was used. All containers came from testcontainers, and none were left running afterwards.

## TDD Gate Compliance

- RED `afcbc57` (tests only): the four worker packages failed to build on the missing builders (`WithTracing`, `WithPropagator`). `TestTracingExportKeepsWorkerAttributes` failed because the allowlist dropped all five worker keys.
- GREEN `bd6ac09`: implementation only, no test files changed.

## Deviations from Plan

1. **[API shape] `AlertProducer.WithPropagator(prop)` instead of `WithTracing(tp, prop)`.** The producer starts no span, so a provider argument would be unused. The default is W3C TraceContext, and injection happens only when ctx holds a valid span.
2. **[Rule 2 - Correctness] store span parent.** Research said "links, not parent". A parentless root goes through `ParentBased(TraceIDRatioBased)`, so at bench ratios most store spans for sampled traces would be dropped. The first sampled record now parents the span and is also linked, which gives the same ≤32-link set.
3. **[Rule 2 - Bounded size] Link deduplication** by (trace ID, span ID), since one ingest request shares one traceparent. Covered by `TestWriterStoreSpanDeduplicatesLinks`.
4. **[Rule 3 - Missing dependency] Detector logger.** The worker had no logger, so the plan's "per-record logger" had nothing to attach to. Added `WithLogger` and an `alert published` Info line per acknowledged publication. `canonicalLabels` now parses rule_id and severity once for both metrics and the log line. Metric behavior is unchanged: nothing is recorded when the rule is unparsable or metrics are nil.
5. **Shared helpers.** `kafkax.SampledRecordContext` (extract + sampled check, used by all three workers) and `telemetry.SampledTraceLogger`.
6. **[Lint] `labelRuleID` constant** in telemetry (goconst across detector.go, registry.go and tracing.go). Span starts were moved into `processTraced`, `insertTraced` and `persistTraced`, where the span starts and ends in one function (spancheck). The detector main gained `openTracing` and `openPublisher` (funlen 54 > 50).
7. **Extra test** `TestTracingExportKeepsWorkerAttributes` in telemetry/tracing_test.go (not in the plan's file list). It pins the allowlist extension and shows tenant_id, source_ip and principal are still dropped on the same span.
8. **Integration traffic.** The detector fires on the tenth failure (threshold crossing), so the marker is not needed for the alert. It is sent as a third request at current time, as the plan asked. The window is the previous fully elapsed 5 min window, so every event is in the past.

## Deferred Issues

- Pre-existing, not fixed: `scripts/test_seed_scenarios.py` cannot import under the host's Python 3.10.12 (`datetime.UTC` is 3.11+). Re-confirmed: `unittest discover -p test_seed_scenarios.py` gives 1 error. This plan did not touch the file.

## Known Stubs

None.

## Threat Flags

None. There is no new endpoint or trust boundary. Kafka headers → workers is in the plan's threat model, and the header is parsed only by the W3C propagator.
- T-05-12-01: spans are sampled-only, there is one store span per batch with ≤32 links, and the BSP queue is bounded at 2048.
- T-05-12-02: allowlist plus the acceptance grep (empty).
- T-05-12-03: trace context is never read by detection or commit code, and the regression families pass.
- T-05-12-04: the tracing closer is registered after ops-http, so it runs last.

## Notes for 05-13

- The Jaeger/observability overlay must set `SENTINEL_OBSERVABILITY.TRACING.ENABLED`, `.SAMPLE_RATIO` and `.OTLP_ENDPOINT` on **detector, event-writer and alert-writer** as well as ingest. Workers follow the edge decision (ParentBased), so their own ratio only matters for roots, and workers start no roots. Keep the ratio identical across services anyway.
- Service names in Jaeger are `config.ServiceIngest`, `ServiceDetector`, `ServiceEventWriter` and `ServiceAlertWriter`. Span names are bounded: `POST /api/v1/events:batch` (or `HTTP <method>` / `_OTHER`), `publish`, `detect`, `store`, `store_alert`.
- A sampled request yields one `detect` span per event in it. At ratio r and N events per request, worker span volume is about r × events/s (detect) plus ≤ 1 store span per CH batch. Size Jaeger `max_traces` and the 05-18 bench preflight from that, and measure it rather than assuming.
- The detector logs one Info line per acknowledged alert publication (`alert published`). Log volume equals the publication rate, and replays log again. Do not build an alert-count panel on it. Use `sentinel_detection_alerts_total` or PG/CH.
- No new Prometheus metrics or labels in this plan. The span attribute allowlist lives in `telemetry.spanAttributeAllowed`. Any attribute added later must go there deliberately.
- query-api (05-14) is still untraced. `kafkax.SampledRecordContext` and `telemetry.SampledTraceLogger` can be reused, and query-api should use the ingest otelhttp options.

## Self-Check: PASSED

- FOUND: backend/test/tracing_pipeline_test.go (first line `//go:build integration`, contains TestTracingPipelinePropagation)
- FOUND commits: afcbc57 (RED, tests only), bd6ac09 (GREEN)
- `grep 'lc.Add("tracing"'` matches all three worker mains. `maxStoreLinks` is in eventwriter/writer.go. The tenant/source_ip/principal attribute grep is empty.
- STATE.md and ROADMAP.md were not touched. .planning/config.json and .serena/ were not staged.
