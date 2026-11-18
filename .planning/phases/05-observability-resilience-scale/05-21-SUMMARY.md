---
phase: 05-observability-resilience-scale
plan: "21"
subsystem: query-api
tags: [system-page, prometheus, readiness, rbac, audit, openapi, query-api, compose]
requires:
  - phase: 04-query-api-analyst-dashboard
    provides: "auth.Verifier/RequirePrincipal/Audit, query handlers, OpenAPI contract tests (04-16 closed)"
  - phase: 05-observability-resilience-scale
    provides: "SystemMetrics + NewPrometheusMetrics + SystemMetric/reasons + registerQueryRoutes (05-15), observability overlay and job names (05-13), bench overlay placeholder (05-17), resource gate (05-07)"
provides:
  - "GET /api/v1/system (operationId getSystemStatus): admin only, parameterless, audited 403 for analyst/viewer"
  - "auth.RequireAdmin; system_status in the auth.Audit action allowlist"
  - "config observability.grafana_url and observability.system.<svc>.{address,expected}"
  - "query.ReadinessProber, query.HostResolver, query.NewDNSReadinessProber (8 in flight, 1 s per probe, 16 replicas per service, no redirects)"
  - "OpenAPI SystemStatus, SystemService, SystemReadiness, MetricsSource; ConsumerLagRow extracted and shared with ConsumerLag"
  - "System env on query-api in the observability and bench overlays"
affects: [05-16 System page and Overview, 05-19 docs, 05-20 bench run]
tech-stack:
  added: []
  patterns: [fixed PromQL package const allowlist evaluated concurrently under one timeout, readiness independent of Prometheus, oneOf readiness schema that forbids counts when unavailable, ratio that is no_data instead of partial or divide-by-zero]
key-files:
  created:
    - backend/internal/query/system.go
    - backend/internal/query/system_test.go
  modified:
    - backend/internal/config/observability.go
    - backend/internal/config/config_test.go
    - backend/internal/auth/jwt.go
    - backend/internal/auth/jwt_test.go
    - backend/internal/auth/audit.go
    - backend/internal/query/lag.go
    - backend/cmd/query-api/main.go
    - backend/cmd/query-api/routes_test.go
    - api/openapi.yaml
    - backend/test/openapi_test.go
    - backend/test/openapi_drift_test.go
    - backend/test/openapi_runtime_auth_test.go
    - deployments/compose/docker-compose.observability.yml
    - deployments/compose/docker-compose.bench.yml
    - scripts/test_observability_config.py
    - generator/tests/test_bench_stack.py
key-decisions:
  - "System is admin only (RequireAdmin). Analysts and viewers get 403 'System health requires the admin role.' and a security_audit event action=system_status reason=role_forbidden. Order: authentication, then role, then query string, so a non-admin sending ?q gets 403, not 400"
  - "Readiness classification uses /readyz probe counts only, never Prometheus: ready = every probed replica ready and ready >= expected (expected 0 = not declared); degraded = some ready; not_ready = none ready. DNS failure = unavailable/not_reachable; empty address = unavailable/not_configured and no probe"
  - "http_errors.ratio = (5xx + 429) / all ingest requests. It is no_data when total traffic is 0 or any input series is absent, and carries the failure reason when any input query fails. It is never a partial ratio"
  - "An absent job in count by (job) (up == 1) is no_data, not 0 replicas"
  - "NewDNSReadinessProber takes a HostResolver interface (satisfied by *net.Resolver) instead of the concrete *net.Resolver the plan named, so the 16-replica cap and dedupe are testable without a DNS server"
  - "metrics_source is available when Prometheus answered at least one query; otherwise unavailable with prometheus_unavailable or not_configured"
requirements-completed: [UI-05, OBS-02]
duration: "about 25 min (2026-11-18T16:37Z to 17:01Z)"
completed: 2026-11-18
---

# Phase 5 Plan 21: Admin-only System Endpoint Summary

**`GET /api/v1/system` gives admins global pipeline health. It runs 11 fixed PromQL constants concurrently under one 2 s timeout and probes every replica's `/readyz` through a bounded DNS prober. Each value reports `available` with a number or `unavailable` with a reason, and a missing value is never shown as 0. Analysts and viewers get an audited 403.**

## Response schema

```json
{
  "observed_at": "2026-11-18T16:54:22Z",
  "grafana_url": "http://127.0.0.1:13000",
  "metrics_source": {"state": "available"},
  "services": [
    {"name": "ingest", "expected_replicas": 1,
     "replicas_up": {"state": "available", "value": 1},
     "restarts_15m": {"state": "available", "value": 0},
     "readiness": {"state": "ready", "ready_replicas": 1, "probed_replicas": 1}}
  ],
  "ingest": {"total": {"state": "available", "value": 54.55},
             "by_result": {"accepted": {...}, "rejected": {...}, "rate_limited": {...}, "shed": {...}, "unavailable": {...}}},
  "http_errors": {"ratio": {...}, "status_5xx": {...}, "status_429": {...}, "status_503": {...}},
  "dlq_rate": {"state": "unavailable", "reason": "no_data"},
  "consumer_lag": [{"group": "event-writer", "lag": {...}}, {"group": "detector", ...}, {"group": "alert-writer", ...}],
  "latency": {"detection_delay_p95_seconds": {...}, "clickhouse_write_p95_seconds": {"state": "available", "value": 0.154}}
}
```

- `services` always has 5 rows, in this order: ingest, event-writer, detector, alert-writer, query-api. `consumer_lag` always has 3 rows: event-writer, detector, alert-writer. Both are closed in the schema (minItems = maxItems).
- Every `{...}` is a `SystemMetric` (05-15 oneOf). It is either `{state: available, value}` or `{state: unavailable, reason: prometheus_unavailable|no_data|not_configured|not_reachable}`.
- `SystemReadiness` is either `{state: ready|degraded|not_ready, ready_replicas, probed_replicas}` (each 0..16) or `{state: unavailable, reason: not_reachable|not_configured}`. The schema rejects counts on an unavailable readiness.
- `grafana_url` is present only when it is configured (http/https only, no userinfo).
- Units: ingest and http_errors values are per-second rates over 1 m (ingest) and 5 m (requests, DLQ). Latencies are seconds. `restarts_15m` counts process restarts in the last 15 minutes.

| Condition | Status | Body |
|---|---|---|
| no or invalid token | 401 | Error envelope |
| analyst or viewer | 403 "System health requires the admin role." | audited `security_audit` action=system_status outcome=denied reason=role_forbidden; no Prometheus call, no probe |
| admin with any query string | 400 | no Prometheus call, no probe |
| Prometheus down or slow (2 s) | 200 | every Prometheus field `unavailable/prometheus_unavailable`; readiness still from probes |
| `prometheus_url` empty | 200 | every Prometheus field `unavailable/not_configured` |
| series absent, NaN or ±Inf | 200 | that field `unavailable/no_data` |

## PromQL allowlist (package consts in system.go, plus qLag from lag.go)

| Const | Query | Feeds |
|---|---|---|
| qUp | `count by (job) (up == 1)` | services[].replicas_up |
| qRestarts | `sum by (job) (changes(process_start_time_seconds[15m]))` | services[].restarts_15m |
| qIngestByResult | `sum by (result) (rate(sentinel_ingest_events_total[1m]))` | ingest.by_result, ingest.total (sum) |
| qReqTotal | `sum(rate(sentinel_ingest_requests_total[5m]))` | ratio denominator |
| q5xx | `sum(rate(sentinel_ingest_requests_total{status=~"5.."}[5m]))` | status_5xx, ratio |
| q429 | `sum(rate(sentinel_ingest_requests_total{status="429"}[5m]))` | status_429, ratio |
| q503 | `sum(rate(sentinel_ingest_requests_total{status="503"}[5m]))` | status_503 |
| qDLQ | `sum(rate(sentinel_dlq_events_total[5m]))` | dlq_rate |
| qLag | `max by (group) (sentinel_kafka_consumer_lag)` | consumer_lag |
| qDelayP95 | `histogram_quantile(0.95, sum by (le) (rate(sentinel_detection_delay_seconds_bucket[5m])))` | latency.detection_delay_p95_seconds |
| qWriteP95 | `histogram_quantile(0.95, sum by (le) (rate(sentinel_clickhouse_write_duration_seconds_bucket[5m])))` | latency.clickhouse_write_p95_seconds |

Every query is aggregated to at most one label (job, result or group), so no instance, tenant or other high-cardinality label leaves Prometheus. `TestSystemSendsOnlyTheFixedQueries` spells out all 11 strings and asserts that the handler sends exactly that set, each once, at the captured `now`, under a deadline of 2 s or less.

## Readiness prober

- Targets come only from `observability.system.<svc>.address`, validated as host:port. The prober rejects anything else before resolving.
- `LookupHost` resolves the target. The prober keeps distinct IP literals, sorts them, caps them at 16, and GETs `http://<ip>:<port>/readyz`. A probe counts as ready only on a 200. It never follows redirects and drains at most 4 KiB of the body.
- At most 8 probes are in flight, shared across all services, and each probe has 1 s. The whole readiness pass has a 2 s budget. A replica that cannot get a slot in time counts as not ready.
- In production, query-api builds the HTTP client with no environment proxy (`Proxy: nil`), keep-alives off, a 1 s dial timeout and an 8 KiB response-header cap.

## Commits

| Task | Gate | Commit | Files |
|---|---|---|---|
| 1 RED tests | test | f425b6a | system_test.go, jwt_test.go, config_test.go, openapi_test.go, openapi_drift_test.go, openapi_runtime_auth_test.go |
| 2 GREEN config/role/handler/prober | feat | 1cae457 | observability.go, jwt.go, audit.go, lag.go, system.go, system_test.go (one test closure reformatted for `lll`, no assertion changed) |
| 3 contract, wiring, overlays | feat | c0872ac | api/openapi.yaml, cmd/query-api/main.go, routes_test.go, both overlays, scripts/test_observability_config.py, generator/tests/test_bench_stack.py |

RED failed for the right reason. Every error was a compile error for a missing identifier: `auth.RequireAdmin`, `config.SystemConfig`, `config.ServiceTarget`, `ObservabilityConfig.GrafanaURL`/`.System` and `query.NewSystemHandler`. The RED commit contains only test files. After GREEN, every test passed on the first run, and repeated runs with `-count=5 -race` were stable.

## Gate evidence

| Gate | Result |
|---|---|
| `go test -race ./internal/query/... ./internal/auth/... ./internal/config/...` | PASS |
| `go test -race -count=5 -run 'TestSystem\|TestDNSProber\|TestLag' ./internal/query/` | PASS (stable) |
| `go mod tidy -diff` | clean (no new modules; go.mod/go.sum untouched) |
| golangci-lint `./...` without tags / with `--build-tags=integration` | 0 issues / 0 issues |
| gofmt on touched files | clean |
| `npm run gen:api && npx tsc --noEmit` | PASS (`"/api/v1/system"` at schema.d.ts:309; the file is gitignored and regenerated on pre-typecheck/test/build) |
| `go build ./... && go vet -tags=integration ./test/` | PASS |
| `go test -race ./internal/... ./cmd/...` | PASS |
| `uv run --locked pytest tests/test_bench_stack.py -q` | 48 passed (renders the bench overlay) |
| `python3 -B -m unittest discover -s scripts -p test_observability_config.py` | 24 OK (renders the observability overlay) |
| `bash scripts/check-pins.sh` | PASS |
| `bash scripts/check-resources.sh` | PASS: 13.07 GiB free repo/docker (floor 10), MemAvailable 5.86 GiB (floor 3) |
| `bash scripts/go-test-nonempty.sh './test/...' '^TestOpenAPI' -tags=integration -timeout=8m -p=1` | PASS, exit 0, 21 s wall, 17 TestOpenAPI* tests including the new SystemStatusContract / SystemStatusDrift / SystemRuntimeSecurity plus Routes / Security / ConsumerLag* |
| live check on `sentinel-chaoscheck` (base + observability overlay) | PASS, see below; torn down |
| `make test` | PASS: Go 40 packages ok, web 174 tests, generator 363 passed |
| `make lint` | PASS: golangci 0 issues, eslint + tsc, ruff, mypy (29 files) |
| `bash scripts/check-observability.sh` | PASS (promtool config/rules/tests and 24 Python checks) |
| `python3 -B -m unittest discover -s scripts` (full) | 117 run, 1 error: only the pre-existing `test_seed_scenarios` import failure on Python 3.10.12 (`datetime.UTC`), not fixed per instructions |

### Live check (05-15 lag endpoint and 05-21 System endpoint)

The resource gate passed (MemAvailable 6.04 GiB). The base compose file plus the observability overlay then ran on `COMPOSE_PROJECT_NAME=sentinel-chaoscheck`: infra, topic-init, `make migrate`, the five app services and Prometheus. Grafana and Jaeger were not started. The Grafana admin password was a random value set in the child environment only, because the root `.env` has no such key. Tokens came from `cmd/dev-jwt` through `scripts/dev_auth_env.py` into private scratch files, which were deleted afterwards.

| Call | Result |
|---|---|
| admin `/api/v1/system` | 200 |
| analyst `/api/v1/system`, viewer `/api/v1/system` | 403, 403 |
| no token `/api/v1/system` | 401 |
| admin `/api/v1/system?q=up` | 400 |
| admin / analyst / viewer `/api/v1/metrics/lag` | 200 / 200 / 403 |

- **Idle stack:** all 5 services were `ready` with 1/1 replicas through real DNS and `/readyz`. `replicas_up` was 1 and `restarts_15m` was 0 for each. Lag was `available` 0 for all three groups in both endpoints, which confirms 05-15's group-label mapping live. Ingest, error, DLQ and latency fields were `unavailable/no_data` (no series yet), never 0.
- **During `scripts/seed.py` traffic** (run directly, not `make seed`, so it did not trigger `dev-up`; 1000 accepted per run): `ingest.total` = `accepted` = 54.55 events/s and `clickhouse_write_p95_seconds` = 0.154. Shed, rate_limited, rejected, unavailable, all HTTP error statuses, the ratio, DLQ and detection-delay p95 stayed `no_data` because no such series existed.
- **After traffic stopped:** `accepted` showed a measured `available` 0 over the 1 m window (the series exists), while the p95 values became `no_data` (`histogram_quantile` of zero rates is NaN).
- **Prometheus stopped:** the response was 200 in 5 ms. `metrics_source` and every Prometheus field were `unavailable/prometheus_unavailable` with no `"value"` key anywhere in the body. All five readiness rows still showed `ready`. `/api/v1/metrics/lag` returned 200 and query-api's own `/readyz` stayed 200.

Teardown used the same files with `down -v --remove-orphans --rmi local`. It left 0 containers, 0 volumes, 0 networks and 0 images labelled `sentinel-chaoscheck`. Besides the 5 app images built by this run, `--rmi local` also removed a leftover `sentinel-chaoscheck-postgres-migrate:latest` image (1.53 GB) from an earlier run on the same disposable project; this run did not build it. No prune was run, and no other project was touched.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] Audit allowlist**
- The plan's key link calls `auth.Audit(c, "system_status", ...)`, but the allowlist dropped unknown actions silently. I added `system_status`. The tests capture the structured log and assert action, outcome, reason, role, actor and tenant for both analyst and viewer.
- **Commit:** 1cae457

**2. [Rule 2] Contract-level no-fake-zero for readiness and metrics source**
- `SystemReadiness` and `MetricsSource` are oneOf like `SystemMetric`. The drift test proves that the schema rejects an unavailable readiness that carries counts, an unavailable metric with `value: 0`, unknown states, a tenant key, an unknown result key, a missing service and a non-UTC `observed_at`.
- **Commit:** c0872ac

**3. [Rule 2] SSRF hardening of the prober beyond the plan text**
- The prober does not follow redirects, uses no environment proxy, drains a bounded body, rejects `/` or `@` in the host, and probes only IP literals returned by the resolver. Tests cover the redirect, path, cap and dedupe, timeout and concurrency.
- **Commits:** 1cae457, c0872ac

**4. Interface instead of a concrete resolver**
- The plan signature was `NewDNSReadinessProber(resolver *net.Resolver, client *http.Client)`. It now takes a `HostResolver` interface, which `*net.Resolver` satisfies (`net.DefaultResolver` in main). This made the 16-replica cap testable without running a DNS server.

**5. Shared helpers in lag.go**
- I extracted `pipelineGroups` and `seriesMetric`/`unavailableFor`, and added `ReasonNotReachable`. `NewLagHandler` behaviour is unchanged, and the 05-15 lag tests and contract tests pass unmodified.

**6. ConsumerLagRow schema extracted**
- `ConsumerLag.consumer_lag.items` is now `$ref: ConsumerLagRow`, which `SystemStatus.consumer_lag` also uses. The 05-15 contract and drift tests pass unchanged. In the generated TS, `components["schemas"]["ConsumerLagRow"]` is now available as well.

**7. Extra files beyond the plan's list**
- `backend/test/openapi_drift_test.go`: System contract and drift tests.
- `backend/internal/auth/audit.go`: the allowlist change (item 1).
- `backend/internal/query/lag.go`: the shared helpers (item 5).
- `backend/cmd/query-api/routes_test.go`: production wiring. It checks 401, 403 for viewer and analyst, 400 for `?q=up`, and 200 with `not_configured` metrics and readiness when nothing is configured.
- `scripts/test_observability_config.py`: asserts that only query-api gets the system env and Grafana URL, that the addresses use the ops ports, and that `SENTINEL_GRAFANA_PUBLIC_URL` overrides the Grafana URL.
- `generator/tests/test_bench_stack.py`: asserts the bench query-api env (PROMETHEUS_URL, Grafana 33000, addresses, EXPECTED 2/2/3/1/2).
- `web/src/api/schema.d.ts` is gitignored, so there is nothing to commit for it.

**8. Bench overlay: no new published port**
- The bench overlay publishes no new port, so `stack.BENCH_PORTS`, `bench/stack.py` and the expected ports in the render test are unchanged. I did not touch any file owned by 05-18.

## Known Stubs

None. An empty `prometheus_url`, Grafana URL or system address is an explicit `not_configured` state, not a stub.

## Deferred Issues

- Pre-existing, not fixed: `scripts/test_seed_scenarios.py` fails to import on the host's Python 3.10 (`from datetime import UTC` needs 3.11+). It is not part of `make test`.
- Pre-existing, unchanged: 22 phase4-tag lint findings in `backend/testphase4/tenant_matrix_test.go` (deferred-items.md).
- Observation, not a defect: `restarts_15m` uses `changes(process_start_time_seconds[15m])` per scrape target. If a replacement container comes up with a new IP, it is a new series and is not counted as a restart. In Compose, a restarted container keeps its IP, so that case is covered.

## Notes for 05-16 (System page and Overview)

- Endpoint: `client.GET("/api/v1/system", { signal })`. The type is `components["schemas"]["SystemStatus"]`. Every metric is a union discriminated on `state`, and readiness is a union on `state`. Render a number only when `state === "available"`. An `available` 0 is a measured zero and should be shown.
- Only send the request when `session.role === "admin"`. On a 403, show "Admin access required" / "System health shows pipeline activity across all tenants. Ask an admin to review it." Never add query parameters or cache busters, because any query string returns 400. The response carries `Cache-Control: no-store`.
- Map reasons to UI-SPEC copy:
  - `no_data` → "No data yet" badge plus the "No metrics recorded yet…" notice
  - `prometheus_unavailable` / `not_configured` → "Unavailable" badge
  - `metrics_source.state === "unavailable"` → the "Metrics source unavailable…" notice
  - readiness `not_reachable` / `not_configured` → "Unavailable" badge
- Status headline: count services whose `readiness.state` is not `ready`. If any readiness is `unavailable`, prefer "Service health unavailable", or follow UI-SPEC.
- Restart line: sum `restarts_15m` only if all five are `available`; otherwise show "Restart count unavailable".
- On a healthy stack, expect `http_errors.*`, `dlq_rate` and the non-accepted `by_result` fields to read "No data yet". A counter series appears only after its first increment, and the server does not invent zeros. This was observed live. `ratio` is (5xx + 429) / all requests and is `no_data` whenever an input is absent or traffic is 0.
- `grafana_url` is optional. Render "Open Grafana dashboards" only when it is present. It comes from server config only (http/https).
- Use the same `["system", session.generation]` key for admins on the Overview. Analysts use `/api/v1/metrics/lag`. The ConsumerLagRow shape is identical in both responses.

## Notes for 05-19 (docs)

- Document `GET /api/v1/system` as admin only and parameterless, with the allowlist table above, per-field availability, and readiness from `/readyz` independent of Prometheus. Also document the order: 401 before 403 before 400.
- Config keys (query-api):
  - `SENTINEL_OBSERVABILITY.PROMETHEUS_URL`
  - `SENTINEL_OBSERVABILITY.GRAFANA_URL` (http/https, no credentials)
  - `SENTINEL_OBSERVABILITY.SYSTEM.{INGEST,EVENT_WRITER,DETECTOR,ALERT_WRITER,QUERY_API}.ADDRESS` (host:port of the internal ops listener)
  - `...EXPECTED` (0..64; 0 = not declared)
  - Every key is empty by default, which means `not_configured`.
- Overlays:
  - observability: ops ports ingest 9101, detector 9102, event-writer 9103, alert-writer 9104, query-api 9105; EXPECTED 1; Grafana `${SENTINEL_GRAFANA_PUBLIC_URL:-http://127.0.0.1:13000}`.
  - bench: EXPECTED follows `BENCH_INGEST_REPLICAS` and `BENCH_DETECTOR_REPLICAS`, with fixed values 2/1/2 for event-writer, alert-writer and query-api. Grafana is `http://127.0.0.1:33000`.
  - `SENTINEL_GRAFANA_PUBLIC_URL` could be added to `.env.example` as an optional key. It is not required; the default applies.
- Live-check recipe used here: the base compose file plus the observability overlay on `COMPOSE_PROJECT_NAME=sentinel-chaoscheck`, with the Grafana password from the child environment. `make dev-up` covers only the base file, so the overlay needs explicit `-f` flags.

## Threat Flags

None beyond the plan's register.
- T-05-21-01 RequireAdmin plus audited 403: tested in unit, runtime-auth and routes tests, and live.
- T-05-21-02 const allowlist, 400 on any query and an exact query-set test.
- T-05-21-03 config-only targets, fixed path, no redirects or proxy, 1 s, 8 in flight, 16 cap.
- T-05-21-04 oneOf schemas and no-`"value"` assertions.
- T-05-21-05 2 s query timeout and readiness independent of Prometheus: tested, and live with Prometheus stopped.
- T-05-21-06 Grafana URL limited to http(s), no userinfo, server config only.

## TDD Gate Compliance

`test(05-21)` f425b6a (RED) comes before `feat(05-21)` 1cae457 (GREEN). Task 3 (`type="auto"`, not TDD) is c0872ac. There was no refactor commit.
## Self-Check: PASSED

- Files exist: backend/internal/query/system.go, backend/internal/query/system_test.go, this SUMMARY
- Commits present: f425b6a, 1cae457, c0872ac (each ends with the Co-Authored-By line; no file deletions; RED commit is tests only)
