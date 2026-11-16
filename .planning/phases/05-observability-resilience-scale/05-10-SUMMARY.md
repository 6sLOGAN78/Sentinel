---
phase: 05-observability-resilience-scale
plan: "10"
subsystem: observability
tags: [opentelemetry, tracing, otlp, kafka-headers, traceparent, otelhttp, ingest]
requirements: [OBS-04, OBS-03]
status: code complete, real-container gate pending
dependency-graph:
  requires: [05-01 package approvals, 05-06 trace_id log plumbing, 05-07 check-resources.sh (code only), 05-09 kafkax/telemetry state]
  provides: [config.TracingConfig, telemetry.NewTracing/WithExporter/Tracing, kafkax.HeaderCarrier, kafkax.BuildEventRecords, Producer.WithTracing, middleware.TraceRename, export-side span attribute allowlist]
  affects: [05-11 (TestTracingEdgePropagation pending in the same gate family), 05-12 (worker extract/detect/store spans must reuse HeaderCarrier/NewTracing and extend the allowlist), 05-13 (Jaeger overlay sets the TRACING env keys), 05-14 (query-api uses the same otelhttp options), 05-19 docs]
tech-stack:
  added: [go.opentelemetry.io/otel v1.47.0 (direct), go.opentelemetry.io/otel/sdk v1.47.0, go.opentelemetry.io/otel/metric v1.47.0 (direct, noop MeterProvider), go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.47.0, go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0 (indirect), go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.72.0 (direct)]
  patterns: [explicit provider/propagator injection with no OTel globals, one traceparent per batch copied to every record, export-time attribute allowlist via ReadOnlySpan embedding, public-endpoint server spans]
key-files:
  created: [backend/internal/telemetry/tracing.go, backend/internal/telemetry/tracing_test.go, backend/internal/kafkax/headers.go, backend/internal/kafkax/headers_test.go, backend/internal/middleware/trace.go, backend/cmd/ingest/main_test.go, backend/test/tracing_edge_test.go]
  modified: [backend/go.mod, backend/go.sum, backend/internal/config/observability.go, backend/internal/config/config_test.go, backend/internal/kafkax/producer.go, backend/internal/kafkax/producer_test.go, backend/internal/middleware/chain.go, backend/internal/middleware/middleware_test.go, backend/cmd/ingest/main.go]
decisions:
  - "otelhttp v0.72.0 has no WithPublicEndpoint; WithPublicEndpointFn(func(*http.Request) bool { return true }) gives the same new-root-plus-link behavior"
  - "Span attributes are filtered at export by an allowlist in telemetry (http.request.method, http.response.status_code, http.route, url.scheme, network.protocol.version, error.type, messaging.system, messaging.destination.name, messaging.batch.message_count). otelhttp records client.address (from X-Forwarded-For), network.peer.address, user_agent.original, url.path and the client Host as server.address, and none of them may leave the process"
  - "TraceRename maps methods outside the 9 standard ones to _OTHER and unmatched routes to \"HTTP <method>\", so span names stay bounded"
  - "BuildEventRecords takes an explicit topic argument (plan signature had none) because records carry Topic"
  - "Producer gets a produce func seam (default client.ProduceSync().FirstErr()) so PublishBatch, including its span, is unit tested without a broker"
  - "Publish failure sets status Error with the fixed description \"publish failed\"; the Kafka error text is not recorded on the span"
  - "otelhttp gets an explicit noop MeterProvider so no OTel global is read"
metrics:
  duration: ~16 min (13:20Z to 13:37Z)
  completed: 2026-11-16
---

# Phase 5 Plan 10: Edge tracing slice Summary

Ingest now has configurable OpenTelemetry tracing. Requests get an otelhttp server span named by the Echo route template (`POST /api/v1/events:batch`), and a child `publish` span wraps the Kafka produce. Every record of a batch carries the same W3C `traceparent` header. Export goes over OTLP/HTTP through a bounded batch processor with a `ParentBased(TraceIDRatioBased)` sampler. Client traceparents cannot force sampling (public endpoint), and request logs carry `trace_id` even when nothing is exported.

**Status: code complete, real-container gate pending.** `scripts/check-resources.sh` refused every time, so the ingest and `TestTracingEdgePropagation` integration families were not run.

## Config keys (zero-value defaults; config.go untouched)

| Env key | Field | Default | Validation |
|---|---|---|---|
| `SENTINEL_OBSERVABILITY.TRACING.ENABLED` | `Tracing.Enabled` | false | none |
| `SENTINEL_OBSERVABILITY.TRACING.SAMPLE_RATIO` | `Tracing.SampleRatio` | 0 | must be in [0,1]; NaN rejected |
| `SENTINEL_OBSERVABILITY.TRACING.OTLP_ENDPOINT` | `Tracing.OTLPEndpoint` | "" | required when enabled. Must be an http/https origin with a non-empty host, no userinfo, path "" or "/", and no query or fragment. Error messages never echo the value |

`config.TracingConfig.Validate()` is called from `ObservabilityConfig.Validate()` and from `NewTracing`.

## Tracing API (`backend/internal/telemetry/tracing.go`)

- `NewTracing(ctx, cfg config.TracingConfig, service string, opts ...TracingOption) (*Tracing, error)`
  - Enabled: `otlptracehttp.New(WithEndpointURL(origin + "/v1/traces"))`, or the exporter injected through `WithExporter`. The exporter is wrapped in the attribute allowlist, then in `NewBatchSpanProcessor` with MaxQueueSize 2048 (drops when full) and MaxExportBatchSize 512.
  - Disabled: `ParentBased(NeverSample())` with no processor or exporter. Spans still carry valid trace IDs (RESEARCH A2, now tested).
  - Resource: `service.name` only.
- `WithExporter(sdktrace.SpanExporter) TracingOption`: a test seam that is ignored while disabled.
- `(*Tracing).Provider() trace.TracerProvider`, `Propagator() propagation.TextMapPropagator` (returns `propagation.TraceContext{}`), `ForceFlush(ctx)`, and `Shutdown(ctx)` (runs once via `sync.Once`; repeat calls return the first result).
- No `otel.SetTracerProvider`, `SetTextMapPropagator` or `GetTracerProvider` anywhere in non-test code.

## Kafka propagation (`backend/internal/kafkax`)

- `HeaderCarrier{Record *kgo.Record}` implements `propagation.TextMapCarrier`. `Set` replaces an existing key instead of appending a duplicate.
- `BuildEventRecords(ctx, prop, topic, events, requestID) ([]*kgo.Record, error)` replaces `recordsFor`. It injects into the first record and copies each `prop.Fields()` value to the rest, so every record carries the same traceparent. `schema_version` and `request_id` are unchanged.
- `(*Producer).WithTracing(tp, prop) *Producer`. `PublishBatch` starts span `publish` (kind Producer) with exactly `messaging.system=kafka`, `messaging.destination.name=<topic>` and `messaging.batch.message_count=<n>`. It sets status Error on failure and ends the span after the ack or failure. Without WithTracing, a noop tracer keeps the caller's span context, which is still injected.
- The DLQ already copies `traceparent` (05-05 `TestDLQPublishCopiesTraceparent`).

## Middleware

The chain order is now `recover, request_id, trace, context, cors, secure, request_logger`. `TraceRename()` runs after `next`. It sets the span name to `<method> <route>`, unescaping Echo's `\:`, and adds `http.route`. Unmatched routes get `HTTP <method>`. Methods outside the standard set become `_OTHER`. Without a recording span it does nothing.

## Lifecycle position (cmd/ingest)

`lc.Names()` order, read from code: `ops-http, tracing, postgres, redis, kafka, http-serve, http, draining`. Closing is LIFO, so tracing shuts down after the HTTP server, the producer, Redis and Postgres, and only ops-http closes after it. The final publish and server spans are flushed. `tracedHandler(e, tr)` wraps Echo with `otelhttp.NewHandler(e, "ingest", WithTracerProvider, WithPropagators, WithMeterProvider(noop), WithPublicEndpointFn(always true))`. The ops endpoints are on the separate ops mux and are not traced.

## Commits

| Hash | Message |
|---|---|
| a79605b | test(05-10): add failing tests for tracing config, sampler, carrier, publish span and edge span (test files plus go.mod/go.sum only) |
| 47e759b | feat(05-10): tracing provider, Kafka header carrier, publish span and route-named edge span |
| 2fa012a | feat(05-10): wire edge tracing into ingest and add real-broker propagation test |

## Verification (all run for real, from backend/ unless noted)

| Command | Time (UTC) | Result |
|---|---|---|
| `bash scripts/check-resources.sh` (start) | 13:20:48 | exit 3, REFUSED (MemAvailable 2.85 GiB, floor 3 GiB) |
| `go list -m -json go.opentelemetry.io/otel/exporters/otlp/otlptrace@v1.47.0` | 13:20 | Origin `https://github.com/open-telemetry/opentelemetry-go`, Subdir `exporters/otlp/otlptrace`, Hash `66cfc9520e205b7d450183532772401bc2b6674c`, Ref `refs/tags/exporters/otlp/otlptrace/v1.47.0`. Same repo and commit as the approved siblings, so the 05-01 open item is closed |
| RED: `go test ./internal/config/... ./internal/telemetry/... ./internal/kafkax/... ./internal/middleware/...` | 13:25:07 | exit 1. Build failed: `config.TracingConfig`, `telemetry.NewTracing/WithExporter/Tracing`, `BuildEventRecords`, `Producer.produce/WithTracing` undefined. This proves only that the identifiers were absent; the behavioral proof is the mutation checks below |
| Task 2 verify: `go test -race` on the 4 packages, `go mod tidy -diff`, `check-go-dependencies.sh`, lint on the 4 packages | 13:28 to 13:30 | exit 0, PASS, 0 issues (after test-only lll/revive/staticcheck/gofmt fixes) |
| Task 3 verify: `go build ./cmd/... && go test -race ./cmd/... ./internal/...` and lint `./cmd/ingest/...` | 13:32 to 13:33 | all ok, 0 issues |
| `go vet ./...`, `go vet -tags=integration ./...`, `golangci-lint run ./...`, `golangci-lint run --build-tags=integration ./...`, `go mod tidy -diff`, `check-go-dependencies.sh` | 13:33:52 to 13:34:11 | exit 0, 0 issues, 0 issues, exit 0, PASS |
| `make test` (go test -race ./... with 39 packages ok, web vitest 174 passed, generator pytest 262 passed) | 13:34:17 to 13:36:19 | exit 0 |
| `make lint` (golangci-lint, web lint and typecheck, ruff, mypy) | 13:36:27 to 13:36:37 | exit 0 |

**Behavioral mutation checks.** Each mutation was reverted afterwards by restoring a copy.

| Mutation | Failing test(s) |
|---|---|
| Exporter not wrapped in the allowlist | TestTracingExportDropsAttributesOutsideAllowlist, TestEdgeServerSpanNamedByRouteTemplate |
| Sampler without ParentBased | TestTracingParentBasedFollowsSampledRemoteParent |
| Disabled sampler set to AlwaysSample | TestTracingDisabledExportsNothingButHasTraceID, TestPublishBatchDisabledTracingStillAddsUnsampledTraceparent |
| traceparent not copied to records 2..n | TestPublishBatchSpanCarriesSameTraceparentOnEveryRecord, TestPublishBatchDisabledTracingStillAddsUnsampledTraceparent |
| No error status on publish failure | TestPublishBatchFailureMarksPublishSpanError |
| No span rename | TestEdgeServerSpanNamedByRouteTemplate, TestEdgeServerSpanUsesTemplateNotRawID, TestEdgeServerSpanBoundsUnknownMethod |
| Raw method kept in the span name | TestEdgeServerSpanBoundsUnknownMethod |
| main `tracedHandler` with public endpoint off | TestTracedHandlerNamesSpanAndIgnoresClientSampling |

**Acceptance greps:**
- `tracetest.NewInMemoryExporter` appears in both tracing_test.go and producer_test.go.
- `WithPublicEndpoint` appears in middleware_test.go and main.go (as `WithPublicEndpointFn`).
- The RED commit has no non-test .go file.
- No OTel global calls, no kotel in go.mod, and `WithMaxQueueSize` appears in tracing.go.
- `git diff -- backend/internal/config/config.go` is empty.
- main.go contains `otelhttp.NewHandler` and `lc.Add("tracing"`.

## Plan-level gate: DEFERRED (real containers)

`bash scripts/check-resources.sh` refused at the start, before the gate and at the end. The command `go-test-nonempty.sh './test/...' '^(TestIngest(AcceptedAfterAck|RetryNoDuplicates)|TestTracingEdgePropagation)$' -tags=integration -timeout=8m -p=1` was **not run**. The floor was not lowered, and no Docker resource was touched or pruned.

Start (13:20:48Z):
```
resource gate: ok repo /home/logan78/Desktop/Sentinel free 11.12 GiB (floor 10 GiB)
resource gate: ok docker-root /var/lib/docker free 11.12 GiB (floor 10 GiB)
resource gate: REFUSED MemAvailable is 2.85 GiB; floor is 3 GiB. Sentinel will not prune; free space yourself
EXIT=3
```
Before the gate (13:33:52Z): REFUSED on repo and docker-root (8.23 GiB free, floor 10 GiB) and on MemAvailable (1.63 GiB), EXIT=3.
End (13:36:42Z): REFUSED on repo and docker-root (8.03 GiB free) and on MemAvailable (0.62 GiB), EXIT=3. Disk fell during `go get`/`make test` (module downloads and the build/test cache). Nothing was deleted.

`TestTracingEdgePropagation` compiles, and vet and lint pass under `-tags=integration`. Pending proof for whoever reruns it:
- `sampled` subtest: 3 records with an identical traceparent, flags 01, trace and parent IDs equal to the exported `publish` span, which is a child of the exported `POST /api/v1/events:batch` server span.
- `unsampled` subtest: 0 exported spans, while records still carry a traceparent with flags 00.

The edge behavior is covered in process by the middleware tests (real Echo, otelhttp, logger) and `cmd/ingest` `TestTracedHandlerNamesSpanAndIgnoresClientSampling`. Producer injection is covered through the produce seam. The broker round trip is the only part not yet exercised.

## Interface changes that 05-11 and 05-12 must know about

- `kafkax.Producer` has a new unexported `produce` field. Struct literals in white-box tests must set it (NewProducer sets it). `recordsFor` is gone; use `BuildEventRecords(ctx, prop, topic, events, requestID)`.
- **Span attribute allowlist** (`telemetry.spanAttributeAllowed`, unexported): any attribute a later plan adds (for example a `detect`/`store` span attribute such as rule_id or a link count) is silently dropped at export unless it is added to that switch. 05-12 must extend it deliberately and keep tenant, IP, principal and key material out.
- Workers (05-12) should build `telemetry.NewTracing(ctx, cfg.Observability.Tracing, service)`, register `lc.Add("tracing", tr.Shutdown)` before the Kafka clients (LIFO), and extract with `tr.Propagator().Extract(ctx, kafkax.HeaderCarrier{Record: r})`. Pass the provider explicitly and never set globals.
- query-api (05-14) should reuse the ingest otelhttp options: explicit TracerProvider, Propagators, noop MeterProvider and `WithPublicEndpointFn`. `TraceRename` is already in `middleware.Chain`, so query-api span names are route templates as soon as it is wrapped.
- `middleware.Chain` now has 7 entries. Any test that asserts the exact order must include `trace` after `request_id`.
- The 05-11 gate family should include `TestTracingEdgePropagation` alongside the pending 05-09 `TestDetectorCapacity`.

## Deviations from Plan

1. **[API drift] `WithPublicEndpoint` does not exist in otelhttp v0.72.0.** I used `WithPublicEndpointFn(func(*http.Request) bool { return true })`. otelhttp's handler then adds `trace.WithNewRoot()` and links any valid remote context, which is identical semantics.
2. **[Rule 2 - Security] Export-side span attribute allowlist.** otelhttp server spans carry `client.address` (taken from client `X-Forwarded-For`), `network.peer.address/port`, `user_agent.original`, `url.path` and the client-controlled `server.address`. These conflict with the no-IP/user rule and with T-05-10-02. `allowlistExporter` wraps every exporter, and tests assert the exact key set.
3. **[Rule 2 - Bounded cardinality] Method normalization in TraceRename.** An arbitrary client method string would otherwise go into the span name. Added `TestEdgeServerSpanBoundsUnknownMethod`.
4. **[Rule 3 - Test seam] `produce` func field on Producer** (mirrors the 05-05 DLQ seam). It lets PublishBatch and its span be tested without a broker. The production path is still `client.ProduceSync(...).FirstErr()`.
5. **`BuildEventRecords` takes `topic`.** The plan signature omitted it, but records need it.
6. **Explicit noop MeterProvider for otelhttp.** Without it, otelhttp reads `otel.GetMeterProvider()`, which is global state.
7. **Extra test `cmd/ingest/main_test.go`.** Written after the implementation, so it is a characterization test, not RED. It pins main's own handler options. The integration test has to copy them, and this test catches drift; it was mutation-checked.
8. **Test-side fixes inside the GREEN commit (no assertion weakened):**
   - Two RED helpers used `t.Context()` in `t.Cleanup`, where it is already canceled; they now use `context.Background()`.
   - `InMemoryExporter.Shutdown` resets its store, so the shutdown-flush test uses a `keepOnShutdown` wrapper.
   - Fixed lll/revive/staticcheck/gofmt findings.
9. **Dependency graph.** `go get` brought in google.golang.org/grpc v1.83.2 (was v1.67.0, indirect), go.opentelemetry.io/proto/otlp v1.11.0, grpc-ecosystem/grpc-gateway/v2 v2.30.0, genproto api/rpc, cenkalti/backoff/v5 v5.0.3, go-openapi/jsonpointer v1.0.0 and go-viper/mapstructure/v2 v2.5.0 as requirements of the approved modules. All are indirect. `check-go-dependencies.sh` passes, and `go mod tidy -diff` is clean.
10. **Ingest integration test fixture.** I wrote a compact traced fixture in the new file instead of changing `newIngestFixtureWithKafka`, so ingest_test.go is unchanged.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model:
- T-05-10-01: public endpoint, tested in middleware and cmd/ingest.
- T-05-10-02: attribute allowlist, tested.
- T-05-10-03: queue capped at 2048.
- T-05-10-04: URL validation, tested.
- T-05-10-05: malformed traceparent ignored, tested by `TestHeaderCarrierIgnoresMalformedTraceparent`.

The only new egress is OTLP/HTTP to the configured endpoint, and only when tracing is enabled.

## Self-Check: PASSED

- FOUND: backend/internal/telemetry/tracing.go, backend/internal/kafkax/headers.go, backend/internal/middleware/trace.go, backend/cmd/ingest/main_test.go, backend/test/tracing_edge_test.go
- FOUND commits: a79605b, 47e759b, 2fa012a
- STATE.md and ROADMAP.md were not touched, as instructed.

## Real-container gate (run 2026-11-17)

`scripts/check-resources.sh` PASS: 18.07 GiB free, MemAvailable 4.88 GiB. `go-test-nonempty.sh './test/...' '^(TestIngest(AcceptedAfterAck|RetryNoDuplicates)|TestTracingEdgePropagation)$' -tags=integration -timeout=8m -p=1` exit 0: TestIngestRetryNoDuplicates (7.68s), TestIngestAcceptedAfterAck (0.43s) and TestTracingEdgePropagation (0.24s) PASS; package ok in 26.95s at HEAD 9350868. The plan is now fully complete.
