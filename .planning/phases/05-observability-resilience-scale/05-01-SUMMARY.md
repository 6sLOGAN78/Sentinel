---
phase: 05-observability-resilience-scale
plan: "01"
subsystem: observability
tags: [prometheus, ingest, metrics]
requirements: [OBS-02, REL-04]
key-files:
  created: [backend/internal/telemetry/registry.go, backend/internal/telemetry/ingest.go, backend/internal/telemetry/metrics_test.go]
  modified: [backend/go.mod, backend/go.sum, backend/internal/opshttp/server.go, backend/internal/opshttp/metrics.go, backend/internal/middleware/global.go, backend/internal/ingest/handler.go, backend/internal/kafkax/producer.go, backend/cmd/ingest/main.go]
completed: 2026-11-14
---

# Phase 5 Plan 01: Ingest metrics slice Summary

Ingest now serves sentinel_ingest_events_total{result}, sentinel_ingest_requests_total{status}, sentinel_ingest_request_duration_seconds and the producer buffer depth/limit gauges on its ops-only /metrics, from an injected per-process Prometheus registry (client_golang v1.25.0), with an allowlist guard test.

## Package legitimacy approval

Recorded before any go.mod change. Question put to the user: "Plan 05-01 asks you to approve the new Phase 5 third-party packages before anything is installed. Approve them?"

User reply (verbatim): "Approve all (Recommended)"

Provenance (read-only, 2026-11-14):

| Module@version | Origin |
|---|---|
| github.com/prometheus/client_golang@v1.25.0 | https://github.com/prometheus/client_golang (0d88875bc7) |
| go.opentelemetry.io/otel@v1.47.0 | https://github.com/open-telemetry/opentelemetry-go (66cfc9520e) |
| go.opentelemetry.io/otel/sdk@v1.47.0 | same repo, 66cfc9520e |
| go.opentelemetry.io/otel/trace@v1.47.0 | same repo, 66cfc9520e |
| go.opentelemetry.io/otel/metric@v1.47.0 | same repo, 66cfc9520e |
| go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.47.0 | same repo, 66cfc9520e |
| go.opentelemetry.io/otel/exporters/otlp/otlptrace@v1.47.0 | lookup timed out three times on proxy.golang.org; approved on the basis of same repository and v1.47.0 tag as its siblings. Origin to be re-verified when `go get` resolves it (see below) |
| go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.72.0 | https://github.com/open-telemetry/opentelemetry-go-contrib (0c76f6137a) |

Images (index digests; not pulled in this plan):

- prom/prometheus:v3.15.1@sha256:fa3302279710be2f21a8301ec17a8b4f2301fd829bd113839b3b6b02b6f191a2
- grafana/grafana:13.2.5@sha256:9f7aa1e1e3246aade5792d35a738049f7f32f89cd51d1956cf426a4e86268a84
- jaegertracing/jaeger:2.22.0@sha256:836b967b5abcf1f7758066aa6b348619267a2f1459f72d472840570f9bd88a17

These match the RESEARCH table.

Re-verification of go.opentelemetry.io/otel/exporters/otlp/otlptrace@v1.47.0: `go list -m -json` still timed out against proxy.golang.org (i/o timeout) after 3 tries on 2026-11-14. This plan does not install it (only client_golang was added), so its Origin is NOT yet verified. The plan that first adds it must re-check Origin when it runs `go get` and stop if it is not github.com/open-telemetry/opentelemetry-go. Only client_golang (and its transitive client_model, common, procfs, perks, goautoneg, go-abnf, protobuf) entered go.mod here; the otel modules and images were not installed or pulled.

## Commits

- 64711fb docs(05-01): approval note, before any go.mod change
- da580c6 RED: test(05-01) failing tests (tests plus go.mod/go.sum only)
- 2aecb7e feat(05-01): GREEN implementation

## Verification (all run for real, from backend/ unless noted)

- RED: `go test ./internal/telemetry/... ./internal/opshttp/... ./internal/ingest/...` 00:19:05, FAIL (build failed: telemetry has no non-test files; opshttp.WithMetricsHandler undefined). This proves identifier absence only.
- Mutation checks (behavioral RED): removing the accepted Events call, neutralizing Events in fail(), and removing the Request recording each made TestPostBatchCountsEveryExitPathByResult and TestInFlightShedIsCounted fail; handler restored afterwards.
- `go vet ./...` exit 0; `go test -race -count=1 ./...` exit 0 (00:21:49 to 00:21:56 for the earlier run; final full run all ok); `go tool -modfile=tools.mod golangci-lint run ./...` final: 0 issues (exit 0); `go build ./cmd/...` exit 0; `go mod tidy -diff` exit 0; `bash scripts/check-go-dependencies.sh` PASS exit 0; `go test ./internal/opshttp -run 'Late|WithMetricsHandler'` pass (detector path unchanged).
- Not run: `make test`/`make lint` wave gate (Makefile is Phase 4 owned and the equivalent go commands above were run), no Docker or live /metrics curl (plan needs none; the ingest test drives the real Echo router and the ops handler via httptest).

## Exported telemetry API

NewRegistry, Handler, AllowedLabelNames, LabelNamesOutsideAllowlist (skips go_/process_ collector families, since go_info carries a `version` label), NewIngestMetrics, (*IngestMetrics).Events/Request (nil-safe), RegisterProducerBuffer, Result* consts. Also opshttp.WithMetricsHandler, middleware.ResponseStatus, (*kafkax.Producer).BufferedRecords, (*ingest.Handler).WithMetrics.

## Decisions

- Shed counting: the in-flight gate trips before the body is read, so batch size is unknown. Events(shed, 0) creates the series without inventing a count; requests_total{status="503"} carries the shed rate. The test asserts the series exists with value 0.
- Auth-unavailable also uses count 0 for the same reason. 401/403/413 and tenant-mismatch paths are visible only in requests_total, not events_total (plan lists no result for them). Envelope decode failures (400/413) count as rejected with 0.

## Deviations from Plan

1. [Rule 3 - lint] PostBatch exceeded funlen (51 statements); extracted `decodeEvents`, `fail` and `unavailableError` helpers. As a result `grep -c "h.metrics.Events(" handler.go` is 2 (inside `fail` and the accepted path) rather than the >= 5 literal; every exit path still calls it through `h.fail(` (9 matches), and mutation checks confirm coverage.
2. go get upgraded unrelated indirect modules (x/crypto, x/net, x/sync, x/sys, x/text, klauspost/compress) as required by client_golang's graph; recorded in go.mod, all gates pass.
3. LabelNamesOutsideAllowlist skips go_/process_ families (needed because go_info{version}).
4. Added the extra TestPublicRouterDoesNotServeMetrics and producer gauge tests per plan behavior.

## Known Stubs

None.

## Self-Check: PASSED

Files and commits verified present (git log, files created).
