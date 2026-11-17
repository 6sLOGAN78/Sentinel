---
phase: 05-observability-resilience-scale
plan: "14"
subsystem: observability
tags: [prometheus, sse, opentelemetry, otelhttp, retry, jitter, query-api, alertstream]
requirements: [OBS-02, OBS-04, REL-01]
status: complete (unit, real-container Phase 4 families and live operator check passed)
dependency-graph:
  requires: [04-16 Phase 4 closure, 05-02 internal/retry, 05-07 check-resources.sh, 05-10 NewTracing/otelhttp options, 05-13 PENDING_METRICS guard]
  provides: [telemetry.SSEMetrics (sentinel_sse_clients), alertstream Handler.WithMetrics, Listener/Store.WithRetryWaiter, query-api /metrics on ops listener, query-api server spans + trace_id logs]
  affects: [05-15, 05-16 System page (query-api scrape now up), 05-17/05-18/05-20 bench (SSE clients panel, query-api spans), 05-19 docs]
tech-stack:
  added: []
  patterns: [unlabeled per-replica gauge, Inc at admission/Dec in release defer before WaitGroup.Done, nil waiter defaults to retry.Wait, otelhttp public endpoint with explicit providers]
key-files:
  created: [backend/internal/telemetry/sse.go, backend/internal/telemetry/sse_test.go, backend/internal/alertstream/telemetry_test.go, backend/cmd/query-api/main_test.go]
  modified: [backend/internal/alertstream/handler.go, backend/internal/alertstream/delivery.go, backend/internal/alertstream/listener.go, backend/cmd/query-api/main.go, scripts/test_observability_config.py]
decisions:
  - "Gauge Inc runs inside admit() under the handler mutex right after the clients-channel send succeeds; Dec runs in the existing release defer BEFORE active.Done, so Handler.Close returning implies the gauge has reached 0"
  - "Stream loops keep their hand-doubled 100ms..5s ceiling and pass it to the waiter (retry.Wait by default), matching the 05-02 caller-owned-loop pattern; sequence equals retry.Policy{Min:100ms,Max:5s}.Ceiling(n)"
  - "The 1 s projector idle poll is not a retry and stays a fixed timer (now named idlePoll, extracted into Store.pause); SSE poll ticker (1 s), heartbeat ticker (15 s) and stream lifetime deadline are unchanged"
  - "query-api tracedHandler uses exactly the ingest otelhttp options (explicit TracerProvider/Propagator, noop MeterProvider, WithPublicEndpointFn always true) with operation name config.ServiceQueryAPI"
  - "No new span attributes were added, so telemetry.spanAttributeAllowed is unchanged"
metrics:
  duration: ~18 min (19:25Z to 19:43Z)
  completed: 2026-11-17
---

# Phase 5 Plan 14: query-api instrumentation Summary

query-api now serves its own Prometheus registry on the ops listener, including `sentinel_sse_clients`, an unlabeled gauge that counts admitted SSE streams on the replica. Public HTTP requests get otelhttp server spans named by route template, a client traceparent cannot force sampling, and request logs carry `trace_id`. The PG LISTEN reconnect loop and the delivery projector's failure backoff now sleep a crypto full-jitter delay through `retry.Wait`, with the same 100 ms..5 s ceiling sequence. Delivery semantics, stream limits, queue sizes, heartbeat and the ADR 0009 gap-safe contract are unchanged. That was confirmed by rerunning all nine Phase 4 real-container tests.

## Phase 4 closure evidence (checked before editing)

- `.planning/ROADMAP.md` line 14: `[x] Phase 4`. Line 277: `[x] 04-16`.
- `.planning/STATE.md` line 21: "Phase 4 COMPLETE 2026-11-16 (re-verified passed 6/6, cdae189; hosted mirror run 43714363178 at 7b15a68 19/19 green)".
- 04-14, 04-15, 04-16, 04-17 and 04-18 SUMMARY files are present.
- Discrepancy: the body of 04-16-SUMMARY.md still reads "Task 3 pending ... Phase 4 is NOT marked complete". It was never amended after the hosted run. ROADMAP and STATE are authoritative, so I proceeded. That SUMMARY was not edited.

## Gauge call sites

| Point | File | Code |
|---|---|---|
| Inc (successful admission only) | `backend/internal/alertstream/handler.go` `admit()` | `case h.clients <- struct{}{}: h.active.Add(1); h.sse.Inc()` |
| Dec (release) | `backend/internal/alertstream/handler.go` `stream()` | `defer func() { <-h.clients; h.sse.Dec(); h.active.Done() }()` |
| Wiring | `backend/cmd/query-api/main.go` `startAlertListener` | `alertstream.NewHandler(...).WithMetrics(sse)` with `sse = telemetry.NewSSEMetrics(reg)` |
| Exposure | `backend/cmd/query-api/main.go` `startOps` | `opshttp.Start(..., opshttp.WithMetricsHandler(telemetry.Handler(reg)))` |

Rejected admissions (cap full, or handler closed) return 429 from `admit()` before any Inc. Responses rejected earlier (401, 400 cursor) never reach admission. The plan's "→ 503" for a full cap was wrong: the existing code returns 429 `NewTooManyRequestsError`. The test asserts 429, and the code is unchanged.

## Timers remaining in delivery.go / listener.go

`grep -n "time.NewTimer\|time.After(" delivery.go listener.go` matches only `delivery.go: timer := time.NewTimer(idlePoll)`. That is the 1 s unconditional idle poll after an empty projection batch, not a retry. Outside these two files, handler.go's 1 s poll ticker, 15 s heartbeat ticker and the stream lifetime deadline are unchanged.

## query-api lifecycle (from the live run log)

`dependencies=ops-http,tracing,postgres,clickhouse,redis,alert-listener,http-serve,http,draining,alert-delivery,alert-streams`. Close is LIFO, so tracing shuts down (and flushes) after the streams, projector, HTTP server and pools. Only ops-http closes after it.

## Commits

| Hash | Message |
|---|---|
| 0173c14 | test(05-14): add failing SSE gauge, stream backoff jitter and query-api tracing tests |
| 9ed42cd | feat(05-14): sentinel_sse_clients, jittered stream backoff and query-api tracing |

RED was verified to fail for the right reasons. Go vet/test reported `undefined: telemetry.NewSSEMetrics`, `Handler has no field or method WithMetrics` and `undefined: tracedHandler`. The Python static test failed with `unregistered metric sentinel_sse_clients` (dashboard panel) and `'sentinel_sse_clients' not found in` the registered set. `git diff --stat HEAD~1 -- backend/internal/alertstream/stream_test.go` is empty.

## Gate table (all run for real; times UTC 2026-11-17)

| Gate | Time | Result |
|---|---|---|
| RED: `go vet ./internal/telemetry/ ./internal/alertstream/ ./cmd/query-api/`, observability unittest | 19:3x | fail (undefined identifiers; 2 Python failures), as intended |
| Task 2 verify: `go test -race ./internal/... ./cmd/...` | 19:3x | exit 0 |
| `python3 -B -m unittest discover -s scripts -p test_observability_config.py` | 19:3x | 22 tests OK |
| `golangci-lint run ./...` / `--build-tags=integration ./...` | 19:33 | 0 issues / 0 issues |
| `go mod tidy -diff` | 19:33 | clean |
| Flake check: `go test -race -count=20` on alertstream, telemetry, cmd/query-api | 19:33 | ok (13.3 s, 4.1 s, 1.1 s) |
| `bash scripts/check-resources.sh` (before containers) | 19:3x | PASS: 15.73 GiB free, MemAvailable 6.91 GiB |
| Infra on `COMPOSE_PROJECT_NAME=sentinel-chaoscheck` (kafka, clickhouse, postgres, redis, topic-init, migrate) | 19:33 | healthy; ports 15432/16379/29092/19000 |
| Plan gate: `go-test-nonempty.sh './testphase4/...' '^(TestPhase4Stream\|TestPhase4Delivery)$' -tags=phase4 -race -timeout=10m -p=1` (gate PASS 15.68 GiB / 6.52 GiB) | 19:34:24 to 19:35:17 | PASS: TestPhase4Delivery 15.37 s, TestPhase4Stream 35.93 s; ok 52.3 s |
| Extra: all Phase 4 tests `'^(TestPhase4.*\|TestHarnessEnvBootstrap)$'` | 19:35:27 to 19:37:00 | PASS 9/9: Alerts 4.85 s, Auth 13.89 s, Delivery 12.45 s, Events 4.66 s, HarnessEnvBootstrap 0.02 s, Stream 36.63 s, SummaryRules 4.66 s, TenantMatrix 4.98 s, Triage 9.15 s; ok 92.3 s |
| Live operator check (real query-api binary, ops :19605) | 19:37:30 | `sentinel_sse_clients 0` → 2 streams open `2` (first frame `: connected`) → one closed `1` → both closed `0`; no labels on the series; API log lines carry `trace_id`/`span_id` |
| Teardown: `COMPOSE_PROJECT_NAME=sentinel-chaoscheck make reset CONFIRM=1` | 19:37 | 0 containers, 0 volumes left for that project |
| `make test` (Go race 40 packages ok, web vitest 174 passed, generator pytest 262 passed) | 19:38:01 to 19:39:00 | exit 0 |
| `make lint` | 19:39 | exit 0 |
| `bash scripts/check-observability.sh` | 19:40 | PASS: observability config, alert rules and dashboard checks |

The operator check used a random per-run JWT secret held in a shell variable (never printed or written). Tokens came from `cmd/dev-jwt`, and the env file was the repo-root `.env` (backend/.env was not read). Tracing was disabled (the default), so the trace IDs in those logs are valid but unsampled, as 05-10 designed.

## Mutation checks (each reverted from a scratch copy)

| Mutation | Failing test(s) |
|---|---|
| Default waiter replaced by a fixed `time.NewTimer(ceiling)` | TestListenerDefaultWaiterIsJittered, TestDeliveryProjectorDefaultWaiterIsJittered ("100ms, 200ms, 401ms, 801ms") |
| No `h.sse.Inc()` | TestStreamClientsGaugeTracksAdmittedStreams, TestStreamClientsGaugeReleasedWhenLifetimeEnds |
| No `h.sse.Dec()` | both gauge tests |
| Inc before the admission check (counts rejected) | TestStreamClientsGaugeTracksAdmittedStreams |
| Listener ceiling fixed at 100 ms (not doubling) | TestListenerReconnectUsesInjectedJitteredWaiter |
| query-api public endpoint off | TestQueryTracedHandlerNamesSpanAndLogsTraceID (sampled, client_forced) |

## Deviations from Plan

1. **[Rule 1 - Test bug] pgx v5.11 opens two TCP connections per failed connect attempt.** When the first connection is reset, pgx dials again within about 0.4 ms (probed directly). The first version of the default-waiter jitter test measured that gap, so it passed even against a fixed backoff (caught by mutation M1). The fix groups accepts closer than 5 ms into one attempt (`attemptStarts`). Grouping can only hide a short jittered interval, never invent one, so a fixed backoff still fails deterministically. Fixed in the GREEN commit; no assertion was weakened.
2. **[Rule 2 - Coverage] Added `backend/cmd/query-api/main_test.go`** (not in files_modified). It pins the query-api span name by route template, public-endpoint sampling, and `trace_id` in the request log equal to the span's trace ID. It also checks that `http.ResponseController` SetWriteDeadline/Flush still reach the connection through otelhttp's httpsnoop wrapper. The SSE handler depends on that and would otherwise silently end every stream. It was written in RED.
3. **[Rule 3 - Lint] funlen in `setupWithRules`.** Adding the registry and tracing took it to 55 statements (limit 50). I extracted `openTracing` (same as the detector) and `startOps` (opshttp.Start plus the serve-failure watcher, with the same `ops http:` error wrapping). Route registration order is unchanged.
4. **[Rule 3 - Lint] `nilerr` in Store.Run.** The failure branch now records the ceiling and calls `Store.pause(ctx, failed, ceiling)`, keeping the original control shape. Run still returns nil on cancellation during a wait and `ctx.Err()` on cancellation after Project.
5. **Test-side lint fixes in GREEN:** `streamBackoff` became a func (gochecknoglobals), and the stream-controls test handler returns errors instead of nil (nilerr).
6. **Plan text: "rejected admission → 503"** is really 429 in the code. See the gauge call sites section above.

## Deferred / known issues (not fixed, out of scope)

- `scripts/test_seed_scenarios.py` fails to import on Python 3.10.12 (`cannot import name 'UTC' from 'datetime'`). Pre-existing and already in STATE.md.
- `golangci-lint run --build-tags=phase4 ./...` reports 22 findings, all in `backend/testphase4/tenant_matrix_test.go` (untouched). Pre-existing and already in STATE.md.
- While running the final gates I started a full `unittest discover -s scripts -p 'test_*.py'` and stopped it after about 3 min, because it goes beyond the required gates. No Docker resource was created (verified with `docker ps -a`).

## Known Stubs

None.

## Threat Flags

None beyond the plan's register:
- T-05-14-01: the gauge has no labels (tested in sse_test.go and on the live scrape).
- T-05-14-02: full jitter through retry.Wait, with the default proven by the interval test.
- T-05-14-03: Phase 4 families rerun 9/9.
- T-05-14-04: public endpoint, tested via client_forced.

No new endpoint: `/metrics` is on the existing internal ops listener, as for the other four services.

## Notes for 05-15

- `alertstream.Handler.WithMetrics`, `Listener.WithRetryWaiter` and `Store.WithRetryWaiter` are the only new exported alertstream APIs. `startAlertListener` in cmd/query-api now takes `sse *telemetry.SSEMetrics` before `lc`.
- cmd/query-api has new helpers `openTracing(ctx, cfg, lc)` and `startOps(cfg.Ops, draining, checks, reg, lc)`, and `startQueryHTTP` now takes an `http.Handler` (`tracedHandler(e, tr)`). `setupWithRules` is at the funlen limit (50 statements), so any further wiring there (for example the 05-16 System handler next to `query.NewSummaryHandler`) needs another extraction.
- `reg` (the query-api `*prometheus.Registry`) is in scope in `setupWithRules` for any further query-api metric.
- The sse-reconnect chaos family (TestPhase4Delivery/Stream) passes with the jittered listener. Reconnect after a PG outage is now uniform in [0, ceiling], so tests that assumed a minimum 100 ms reconnect delay would be wrong; none do today.
- The compose-backed procedure that worked: `COMPOSE_PROJECT_NAME=sentinel-chaoscheck bash -c '$COMPOSE up -d --wait kafka clickhouse postgres redis && $COMPOSE run --rm topic-init && make migrate'` (run through bash: zsh does not word-split `$COMPOSE`), then `make reset CONFIRM=1` with the same project name.

## Notes for 05-17

- All nine spec 11.2 metrics now exist, and `PENDING_METRICS` is empty, so the dashboard's "SSE clients" panel (`sum(sentinel_sse_clients)`) has data and the Prometheus `query-api` job (port 9105) will be `up`.
- The bench SSE subscriber can cross-check its own connection count against `sentinel_sse_clients` on query-api ops :9105. The gauge counts per replica from admission to release. A stream refused with 429 (cap 100 by default) is never counted.
- query-api server spans are named `GET /api/v1/alerts/stream` etc. An SSE span lasts the whole stream (up to 10 min lifetime) and is exported only when it ends. Account for that in any Jaeger span-size or RSS measurement: long streams hold one in-flight span each, and the batch processor queue is 2048.
- query-api honors the overlay's `SENTINEL_OBSERVABILITY.TRACING.*` env now. No new span attributes, so the export allowlist is unchanged.

## Self-Check: PASSED

- FOUND: backend/internal/telemetry/sse.go, backend/internal/telemetry/sse_test.go, backend/internal/alertstream/telemetry_test.go, backend/cmd/query-api/main_test.go
- FOUND commits: 0173c14, 9ed42cd
- STATE.md and ROADMAP.md not touched, as instructed. `.planning/config.json` and `.serena/` not staged.
