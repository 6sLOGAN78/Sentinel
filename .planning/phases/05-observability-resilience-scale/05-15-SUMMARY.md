---
phase: 05-observability-resilience-scale
plan: "15"
subsystem: query-api
tags: [consumer-lag, prometheus, rbac, audit, openapi, query-api]
requires:
  - phase: 04-query-api-analyst-dashboard
    provides: "auth.Verifier/RequireTriage/Audit, query handlers, OpenAPI contract tests (04-16 closed)"
  - phase: 05-observability-resilience-scale
    provides: "sentinel_kafka_consumer_lag{group,topic,partition} (05-05/05-09), observability overlay (05-13), query-api wiring helpers (05-14), resource gate (05-07)"
provides:
  - "GET /api/v1/metrics/lag (operationId getConsumerLag): analyst/admin only, audited 403 for others"
  - "query.SystemMetrics interface and NewPrometheusMetrics adapter (client_golang api/prometheus/v1, 2 s timeout) for reuse by 05-21"
  - "query.SystemMetric value type {state, value?, reason?} and reason constants"
  - "OpenAPI ConsumerLag and SystemMetric schemas (oneOf: available needs value, unavailable needs reason and no value)"
  - "observability.prometheus_url config; overlay sets it on query-api only"
  - "cmd/query-api registerQueryRoutes helper"
affects: [05-16 Overview lag panel, 05-21 System endpoint, 05-19 docs]
tech-stack:
  added:
    - "github.com/json-iterator/go v1.1.12 (indirect, via client_golang api/prometheus/v1)"
    - "github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd (indirect)"
    - "github.com/modern-go/reflect2 v1.0.2 (indirect)"
    - "github.com/prometheus/common v0.74.0 promoted from indirect to direct"
  patterns: [fixed PromQL package const, per-field availability instead of 503 for a soft dependency, discriminated-union metric schema, unconfigured adapter that reports not_configured]
key-files:
  created:
    - backend/internal/query/lag.go
    - backend/internal/query/prometheus.go
    - backend/internal/query/lag_test.go
    - backend/internal/query/prometheus_test.go
    - backend/cmd/query-api/routes_test.go
  modified:
    - backend/internal/config/observability.go
    - backend/internal/config/config_test.go
    - backend/internal/auth/audit.go
    - backend/cmd/query-api/main.go
    - backend/go.mod
    - backend/go.sum
    - api/openapi.yaml
    - backend/test/openapi_test.go
    - backend/test/openapi_drift_test.go
    - backend/test/openapi_runtime_auth_test.go
    - deployments/compose/docker-compose.observability.yml
    - scripts/test_observability_config.py
key-decisions:
  - "Lag readable by analyst and admin only (RequireTriage); viewer and any other role get 403 'Consumer lag requires the analyst role.' and a security_audit event action=consumer_lag reason=role_forbidden"
  - "Prometheus failure returns 200 with every row unavailable plus a reason (prometheus_unavailable, not_configured, no_data); an unavailable row never carries a value, and the OpenAPI schema rejects one"
  - "Added three indirect modules for client_golang api/prometheus/v1; the user approved this (Option B), relayed by the coordinator"
  - "Empty prometheus_url is valid and yields not_configured rather than a guessed default host"
  - "SystemMetric.value has no minimum so 05-21 can reuse it; lag is non-negative by construction (kafkax clamps max(0, end-next))"
requirements-completed: [UI-05, OBS-02]
duration: "spans a session restart (2026-11-17T21:22Z start, 2026-11-18T16:33Z finish); includes one checkpoint wait"
completed: 2026-11-18
---

# Phase 5 Plan 15: Consumer Lag Endpoint Summary

**`GET /api/v1/metrics/lag` serves one position-based lag value per pipeline consumer group from a fixed `max by (group) (sentinel_kafka_consumer_lag)` query over client_golang `api/prometheus/v1`. It is restricted to analyst/admin with an audited 403 for everyone else, and it reports every missing value as `unavailable` with a reason, never as 0.**

## Response schema

```json
{
  "observed_at": "2026-11-18T16:00:00Z",
  "consumer_lag": [
    {"group": "event-writer", "lag": {"state": "available", "value": 12345}},
    {"group": "detector",     "lag": {"state": "available", "value": 0}},
    {"group": "alert-writer", "lag": {"state": "unavailable", "reason": "no_data"}}
  ]
}
```

- There are always exactly 3 rows, in this fixed order: event-writer, detector, alert-writer. Each display name maps from the configured `kafka.event_writer_group` / `detector_group` / `alert_writer_group`; the series `group` label is the configured group name (`kafkax/lag.go`). Series for other groups are dropped.
- `lag` is a `SystemMetric` and takes one of two shapes:
  - `{state: available, value: number}`
  - `{state: unavailable, reason: prometheus_unavailable|no_data|not_configured|not_reachable}`. `not_reachable` is reserved for the 05-21 readiness probes.
- The response has no throughput, volumes, error counts, partitions, topics or tenant data. A key-set test enforces this (T-05-15-01).
- `observed_at` is the server's captured now in UTC. The query is evaluated at that instant.

| Condition | Status | Body |
|---|---|---|
| no or invalid token | 401 | Error envelope |
| viewer, or any non-analyst/admin principal | 403 "Consumer lag requires the analyst role." | audited `security_audit` action=consumer_lag outcome=denied reason=role_forbidden |
| any query string | 400 | no Prometheus call |
| Prometheus error or timeout (2 s) | 200 | all rows `unavailable/prometheus_unavailable` |
| `prometheus_url` empty | 200 | all rows `unavailable/not_configured` |
| series absent, NaN or ±Inf | 200 | that row `unavailable/no_data` |

query-api readiness checks still cover only postgres, redis, clickhouse and alert-listener. Prometheus is not a readiness dependency (T-05-15-04).

## Role rule and rationale (RESOLVED user decision, 2026-10-30, restated for this run)

The System page and `GET /api/v1/system` stay admin-only (05-21) because ingest volumes and error rates are global and would leak other tenants' activity. This endpoint carries only one lag number per consumer group, so analysts can see consumer lag (UI-01) without that exposure. Viewers are refused on the server with an audited 403; hiding the panel in the client is only UX. The order is: authentication, then role check, then query-string check. A viewer sending `?x` gets 403, not 400.

## Dependency approval (Option B)

The plan's interface note required a stop if `api/prometheus/v1` needed a separate module. It does: its `api_126.go` imports `json-iterator/go`. I stopped and returned a decision checkpoint. The coordinator then relayed: "The user approved Option B in chat ('B: approve modules')". I did not receive the approval directly from the user. Versions are pinned exactly as client_golang v1.25.0's go.mod resolves them:

| Module | Version | go.sum h1 (module) | go.sum h1 (go.mod) |
|---|---|---|---|
| github.com/json-iterator/go | v1.1.12 | `PV8peI4a0ysnczrg+LtxykD8LfKY9ML6u2jnxaEnrnM=` | `e30LSqwooZae/UwlEbR2852Gd8hjQvJoHmT4TnhNGBo=` |
| github.com/modern-go/concurrent | v0.0.0-20180306012644-bacd9c7ef1dd | `TRLaZ9cD/w8PVh93nsPXa1VrQ6jlwL5oN8l14QlcNfg=` | `6dJC0mAP4ikYIbvyc7fijjWJddQyLn8Ig3JB5CqoB9Q=` |
| github.com/modern-go/reflect2 | v1.0.2 | `xBagoLtFs94CBntxluKeaWgTMpvLxC4ur3nMaC9Gz0M=` | `yWuevngMOJpCy52FWWMvUC8ws7m/LJsjYzDa0/r8luk=` |
| github.com/prometheus/common (indirect → direct, unchanged version) | v0.74.0 | `6iYW8MsYvmEnXFD9zLGgF0qmReaRhs3OMONu1c/nrLI=` | `i/02C+FCst5B1etKgWesNrD49n+onplJCqUCtUKkOeA=` |

`go mod tidy` also added go.sum checksum lines, with no go.mod requirement, for these modules:
- `jpillora/backoff v1.0.0`
- `mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f`
- `golang.org/x/oauth2 v0.38.0`
- go.mod-only lines for `davecgh/go-spew v1.1.1`, `google/gofuzz v1.0.0` and `modern-go/concurrent v0.0.0-20180228061459-e0a39a4cb421`

`go mod why` shows these are reached only through `client_golang/api/prometheus/v1.test` → `prometheus/common/config`. `go list -deps ./...` and `go list -deps -test ./...` show that no Sentinel binary or test links them. The only newly linked modules are the three approved ones. `go mod tidy -diff` is clean.

## Commits

| Task | Gate | Commit | Files |
|---|---|---|---|
| 1 RED tests | test | 9e65ffe | lag_test.go, prometheus_test.go, config_test.go, openapi_test.go, openapi_drift_test.go, openapi_runtime_auth_test.go |
| 2 GREEN handler/adapter/config | feat | 935b727 | lag.go, prometheus.go, observability.go, auth/audit.go, go.mod, go.sum |
| 3 contract, wiring, overlay | feat | 3a4e94f | api/openapi.yaml, cmd/query-api/main.go, cmd/query-api/routes_test.go, docker-compose.observability.yml, scripts/test_observability_config.py |

RED failed for the right reason. The only failures were compile errors for the missing `NewLagHandler`, `NewPrometheusMetrics`, `ErrPrometheusNotConfigured` and `ObservabilityConfig.PrometheusURL`, and the RED commit contains only test files.

## Gate evidence

| Gate | Result |
|---|---|
| `go test -race ./internal/query/... ./internal/config/... ./internal/auth/...` | PASS |
| `go mod tidy -diff` | clean |
| golangci-lint `./...` without tags and with `--build-tags=integration` | 0 issues / 0 issues |
| gofmt on touched files | clean |
| `npm run gen:api && npm run typecheck` | PASS (`"/api/v1/metrics/lag"` at schema.d.ts:281) |
| `go build ./... && go vet -tags=integration ./test/...` | PASS |
| `go test -race ./internal/... ./cmd/...` | PASS |
| `python3 -B -m unittest discover -s scripts -p test_observability_config.py` | 23 tests OK (renders compose) |
| `bash scripts/check-pins.sh` | PASS |
| `bash scripts/check-resources.sh` | PASS: repo/docker 12.23 GiB (floor 10), MemAvailable 3.71 GiB (floor 3) |
| `bash scripts/go-test-nonempty.sh './test/...' '^TestOpenAPI' -tags=integration -timeout=8m -p=1` | PASS, 73.2 s, 14 TestOpenAPI* tests including ConsumerLagContract / ConsumerLagDrift / ConsumerLagRuntimeSecurity / Routes / Security |
| `make test` | PASS: Go 40 packages ok, web 174 tests, generator 363 passed |
| `make lint` | PASS: golangci 0 issues, eslint+tsc, ruff, mypy |
| `bash scripts/check-observability.sh` | PASS: promtool config/rules/tests and 23 Python checks |
| Acceptance greps | `getConsumerLag` present; "unavailable until Phase 5" absent; no `c.QueryParam`/`c.Param(` in lag.go; `qLag` const matches |

No compose stack was started. The plan's gates are testcontainers or static only, so no `sentinel-chaoscheck` project was created and none needed tearing down.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] auth.Audit dropped the consumer_lag refusal**
- **Found during:** Task 1, while designing the audit assertion
- **Issue:** `auth.Audit` has an action allowlist with only `producer_auth` and `alert_transition`. Calling it with `consumer_lag` returned silently, so the required audit would never be written.
- **Fix:** Added `consumer_lag` to the allowlist. The viewer and unknown-role tests capture the structured log line and assert action/outcome/reason/role/actor/tenant.
- **Files modified:** backend/internal/auth/audit.go
- **Commit:** 935b727

**2. [Rule 3 - Blocking, required approval] New indirect modules for api/prometheus/v1**
- Stopped at a decision checkpoint as the plan instructed, then resumed after Option B was approved. Details are in "Dependency approval" above.
- **Commit:** 935b727

**3. [Rule 2] Contract-level no-fake-zero**
- The plan specified SystemMetric as flat optional fields. I made it a `oneOf`: available requires `value`; unavailable requires `reason` and forbids `value` (additionalProperties false). The schema itself now rejects `{"state":"unavailable","value":0}`. openapi-typescript generates a discriminated union on `state`, which 05-16 can narrow.
- **Commit:** 3a4e94f

**4. Viewer 403 message**
- The plan said `http.StatusText(403)`. The user decision for this run requires "Consumer lag requires the analyst role.", so the 403 message uses that copy. The error code is still `FORBIDDEN`.

**5. Extra files beyond the plan's list**
- `backend/internal/query/prometheus_test.go` tests the adapter against an httptest Prometheus speaking the real `/api/v1/query` format. It covers vector by label, NaN/±Inf dropped, non-vector, 4xx/5xx, garbage, unreachable, 2 s timeout, unconfigured and invalid URL.
- `backend/cmd/query-api/routes_test.go` tests the production router wiring: 401, 403 for viewer, 200 not_configured, 200 prometheus_unavailable for an unreachable URL, and an invalid URL error.
- `scripts/test_observability_config.py` asserts the rendered overlay gives `PROMETHEUS_URL` only to query-api.
- `web/src/api/schema.d.ts` is gitignored and regenerated by `pre{typecheck,build,test}`, so there is nothing to commit for it.

**6. Line wrap in the OpenAPI description**
- The first draft split "analyst or / admin" across lines, which failed the `TestOpenAPISecurity` phrase check. I rewrapped the YAML; the test was not changed.

**7. Unknown-role test**
- The verifier rejects unknown roles at token parsing, so a token can't reach the handler with one. The test injects `auth.Principal{Role:"operator"}` under the literal context key `sentinel.verified-principal`. If that key changes, the test gets 401 and fails; it cannot pass silently.

## Known Stubs

None. An empty `prometheus_url` is an explicit `not_configured` state, not a stub.

## Deferred Issues

- Pre-existing, not fixed: `scripts/test_seed_scenarios.py` fails to import on the host's Python 3.10.12 (`from datetime import UTC` needs 3.11+). It is not part of `make test`.
- I did not run the endpoint live against a running Prometheus scraping real consumers. Label mapping is proven from the source (`kafkax/lag.go` publishes `t.group`, the configured group name). The adapter is proven against the real Prometheus HTTP response format. 05-16 or 05-21 should confirm it live on `sentinel-chaoscheck` with the observability overlay.

## Notes for 05-16 (Overview lag panel)

- Endpoint: `client.GET("/api/v1/metrics/lag", { signal })`. The type is `components["schemas"]["ConsumerLag"]`, and `lag` is a union discriminated on `state`. Render `value` only when `state === "available"` (`12,345 records`). Any `unavailable` row shows "Consumer lag unavailable. The metrics source is not responding." plus "Retry loading consumer lag". Never render 0 for a missing value. A real `{"state":"available","value":0}` is a measured zero and should be shown.
- Viewers: send no request and show "Consumer lag requires the analyst role.". The server returns the same text in its 403 message, so falling back to it on any 403 is consistent.
- The endpoint returns 200 even when Prometheus is down. Treat only non-200 responses as request errors (`role="alert"`). 503 means query-api itself cannot answer.
- Responses carry `Cache-Control: no-store`. Any query string returns 400, so don't add cache busters.
- Overview.tsx still has the "Consumer lag — available in Phase 5" placeholder, and `Overview.test.tsx:127` asserts the summary call. Both are 05-16's to change.

## Notes for 05-21 (System endpoint)

- Reuse `query.SystemMetrics` and `query.NewPrometheusMetrics(cfg.Observability.PrometheusURL)`. The adapter returns `map[label]float64`, accepts at most one grouping label per sample (`""` when unlabeled), drops NaN/±Inf, returns an error on non-vector results or duplicate keys, and applies its own 2 s timeout. `ErrPrometheusNotConfigured` maps to `ReasonNotConfigured`.
- Reuse `query.SystemMetric`, `MetricAvailable`/`MetricUnavailable` and the `Reason*` constants. Add `not_reachable` (already in the OpenAPI enum) for readiness probes. `SystemMetric.value` has no minimum, so it suits ratios too.
- Wire the System handler inside `registerQueryRoutes` in cmd/query-api/main.go. It already builds `prom` and receives `cfg`; reuse that instance instead of constructing another. `setupWithRules` is now about 44 statements.
- `auth.Audit` has an action allowlist (`producer_auth`, `alert_transition`, `consumer_lag`). Add a System action such as `system_health` there, or admin-only refusals will be dropped silently.
- `api/openapi.yaml` has a `metrics` tag. `TestOpenAPIRoutes` (want list) and `TestOpenAPISecurity` need the new route. `openapi_runtime_auth_test.go` has a `consumerLagFixture` pattern to copy.
- The overlay already sets `SENTINEL_OBSERVABILITY.PROMETHEUS_URL: http://prometheus:9090` on query-api (`<<: *app-observability` merge). The bench overlay (05-17) still needs the same key under `query-api.environment` in `docker-compose.bench.yml`.

## Threat Flags

None beyond the plan's register. T-05-15-01 to 05 are each mitigated and tested: key-set test, fixed const plus 400 plus a fake asserting the query, oneOf no-zero schema plus body assertions, 2 s timeout plus readiness unchanged, and RequireTriage plus an audited 403.

## TDD Gate Compliance

`test(05-15)` 9e65ffe (RED) comes before `feat(05-15)` 935b727 (GREEN). Task 3 (`type="auto"`, not TDD) is 3a4e94f. There was no refactor commit.

## Self-Check: PASSED
