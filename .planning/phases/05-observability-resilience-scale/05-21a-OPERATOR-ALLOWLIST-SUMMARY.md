---
phase: 05-observability-resilience-scale
plan: "21a"
subsystem: query-api
tags: [security, gap-closure, rbac, tenant-isolation, audit, openapi, compose, adr]
requires:
  - phase: 05-observability-resilience-scale
    provides: "GET /api/v1/system + auth.RequireAdmin (05-21), GET /api/v1/metrics/lag + SystemMetric reasons (05-15), observability and bench overlays (05-13, 05-17/05-18)"
provides:
  - "SENTINEL_AUTH.OPERATOR_TENANTS (auth.operator_tenants): comma list, max 16, tenant-slug format, no duplicates, fail closed"
  - "auth.OperatorTenants / auth.NewOperatorTenants / Includes(Principal); audit reason operator_forbidden"
  - "System: operator-tenant admins only; distinct audited 403 for non-operator admins"
  - "Lag: non-operator analyst/admin get every row unavailable/operator_only with no value"
  - "OpenAPI SystemMetric reason operator_only; ADR 0014"
affects: [05-16 System page and Overview copy, 05-19 docs]
tech-stack:
  added: []
  patterns: [fail-closed static allowlist read only from the verified principal, redacted 200 instead of 403 where a panel must stay honest]
key-files:
  created:
    - docs/decisions/0014-operator-tenant-allowlist.md
  modified:
    - backend/internal/auth/jwt.go
    - backend/internal/auth/audit.go
    - backend/internal/config/config.go
    - backend/internal/query/system.go
    - backend/internal/query/lag.go
    - backend/cmd/query-api/main.go
    - api/openapi.yaml
    - deployments/compose/docker-compose.yml
    - deployments/compose/docker-compose.observability.yml
    - deployments/compose/docker-compose.bench.yml
    - .env.example
    - docs/decisions/README.md
    - docs/threat-model.md
    - docs/operations.md
    - "tests: auth/jwt_test.go, config/config_test.go, query/system_test.go, query/lag_test.go, cmd/query-api/routes_test.go, test/openapi_{runtime_auth,drift,}_test.go, testphase4/tenant_matrix_test.go, scripts/test_observability_config.py"
decisions:
  - "Operator status comes from a static, fail-closed tenant allowlist (SENTINEL_AUTH.OPERATOR_TENANTS); it narrows 05-21 'System admin-only' and 05-15 'analyst lag' (ADR 0014)"
  - "System check order: 401, role 403 (unchanged), 400 on any query string, operator 403"
  - "Lag for non-operator tenants is a redacted 200 (operator_only, no value), not a 403, and never queries Prometheus"
  - "operator_only joins the shared SystemMetric reason enum; System never emits it"
  - "demo is the only operator tenant in dev, observability and bench stacks; demo2 is not"
metrics:
  duration: 17min
  completed: 2026-11-18
---

# Phase 5 Plan 21a: Operator Tenant Allowlist Summary

This is a security gap closure that falls between 05-21 and 05-16. Platform-wide System data and consumer-lag values are now limited to operator tenants. Operator tenants come from the fail-closed `SENTINEL_AUTH.OPERATOR_TENANTS` allowlist, and membership is read only from the verified token's tenant. A non-operator admin gets a 403 with its own message and its own audit reason. A non-operator analyst or admin gets lag rows marked `operator_only` with no value.

## Commits

| Commit | Type | Description |
| --- | --- | --- |
| 84127fd | test (RED) | Failing operator allowlist tests: auth, config, System, lag, OpenAPI runtime-auth/drift/contract, phase4 matrix |
| e785a2e | feat (GREEN) | `OperatorTenants`, config key and validation, System/lag gating, `operator_forbidden` audit, OpenAPI enum and descriptions, wiring |
| a42a795 | chore | `demo` set as the operator tenant in the base compose and the observability and bench overlays; `.env.example`; compose guard test |
| cfc3144 | docs | ADR 0014 and its index row, threat-model section, operations section |

RED check: before GREEN, all four test packages failed only because the operator API was missing: undefined `auth.OperatorTenants` and `auth.NewOperatorTenants`, missing `cfg.Auth.OperatorTenants`, and too many arguments to `NewLagHandler`/`NewSystemHandler`. There were no other compile or behaviour failures.

## Behaviour

- **Config:** `SENTINEL_AUTH.OPERATOR_TENANTS` is a list key, split on commas the same way as `server.cors_allowed_origins`. It allows at most 16 entries. Each entry must match `^[a-z0-9][a-z0-9_-]{0,62}$`, and entries must not repeat. Validation runs wherever the auth section is required (query-api and dev-jwt). An unset or empty list means no tenant is an operator.
- **System** (`GET /api/v1/system`):
  - The checks run in this order: 401, then the role 403 ("System health requires the admin role.", `role_forbidden`), then 400 for any query string, then the operator 403 ("System status is limited to operator administrators.", `operator_forbidden`).
  - A denied caller never reaches Prometheus or the readiness probes.
- **Lag** (`GET /api/v1/metrics/lag`):
  - Viewers keep the audited 403.
  - Analysts and admins in a non-operator tenant get 200 with three rows of `{"state":"unavailable","reason":"operator_only"}`. The rows carry no value, and Prometheus is never queried for them.
  - This redacted 200 is not audited, the same as other allowed reads.

## Gates

| Gate | Result |
| --- | --- |
| `gofmt -l` (touched Go) | clean |
| `golangci-lint run ./...` | 0 issues |
| `golangci-lint run --build-tags=integration ./...` | 0 issues |
| `go mod tidy -diff` | clean, no new modules |
| `go test -tags integration ./test -run '^TestOpenAPI'` | PASS (System/ConsumerLag runtime security, contract, drift included) |
| `go vet -tags phase4 ./testphase4` | clean (matrix compiles) |
| `make test` (Go race, web vitest 174, generator 363) | PASS |
| `make lint` (backend, web lint + typecheck, generator) | PASS |
| `scripts/check-observability.sh` | PASS |
| `scripts/check-pins.sh` | PASS |
| `scripts/check-adrs.sh` | PASS (0014 OK) |
| `python3 -m pytest scripts --ignore=scripts/test_seed_scenarios.py` | 117 passed (includes the new compose guard and the phase4 guards) |
| `npm run gen:api` | regenerated; `schema.d.ts` is gitignored, and the union now includes `"operator_only"` |
| `scripts/check-resources.sh` | PASS, but disk free was only 10.04 GiB against a 10 GiB floor |

**Known failure, recorded and not fixed:** under Python 3.10.12, `scripts/test_seed_scenarios.py` fails at collection with `ImportError: cannot import name 'UTC' from 'datetime'`. It is pre-existing and unrelated to this plan.

**Not run:**
- **Live two-token check on `sentinel-chaoscheck`:** this was not cheap. Building the app image would pull `golang:1.26.9` (about 0.9 GB, not cached) with disk at 10.04 GiB, which breaks the 10 GiB floor. query-api also needs Postgres at startup, so the binary cannot be exercised without containers. No compose project was started, so no teardown was needed. The same two-tenant behaviour is covered in-process by the OpenAPI runtime-auth test (`demo` vs `demo2`) and by the `cmd/query-api` wiring test.
- **Phase4 tenant-matrix run:** it needs the phase4 PG/CH harness, so it was not run. The new matrix case sets `SENTINEL_AUTH.OPERATOR_TENANTS` to tenant 0 and checks System and lag for every role in both tenants.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Existing query-api wiring test missed in RED**
- **Found during:** GREEN
- **Issue:** `cmd/query-api/routes_test.go` builds a `config.Config` with no allowlist, so its tenant-a analyst/admin started getting `operator_only` and the operator 403.
- **Fix:** the fixture now sets `Auth.OperatorTenants: ["tenant-a"]`. Added `TestRegisterQueryRoutesWiresOperatorTenants`, which proves `cfg.Auth.OperatorTenants` reaches both handlers.
- **Commit:** e785a2e

**2. [Rule 3 - Lint] Test-only lint fixes in GREEN**
- Fixed two `lll` overlong lines introduced by RED tests. In the phase4 matrix, the new checks were moved into a `matrixGlobalViews` helper so the main test's complexity does not grow, and the existing `//nolint:gocognit,funlen` directive stays on the test function. The phase4 file's other lint findings (lll, musttag, perfsprint, unusedwrite) are pre-existing and outside the required lint gates.
- **Commit:** e785a2e

### Interpretation notes

- **Check order.** The brief says "Keep the order: 401, then 400 on a query string, then 403". The existing System order (05-21) is 401, role 403, 400. I kept the role 403 where it was and placed the new operator 403 after the 400, so for admins the order is literally 401, 400, 403. The lag endpoint has the same shape, since operator status there changes the body after the 400. This is pinned by `TestSystemCheckOrder` and the existing `TestSystemNonAdminForbiddenAndAudited`.
- **Enum placement.** `operator_only` was added to the shared `SystemMetric` reason enum, not to a new lag-only schema. As a result the SystemStatus schema would also accept the reason, but the System handler never emits it. The OpenAPI description says the reason appears only on lag.
- **Phase4-acceptance overlay:** left unchanged. It uses fresh random tenants and does not check System or lag.

## Notes for 05-16 (dashboard)

- **Lag panel (Overview):** a row with `reason: "operator_only"` and no `value` needs copy. Suggested label: "Operator only". Suggested tooltip: "Consumer lag is platform-wide and visible only to operator tenants." Do not render it as an error, a zero or a dash meaning "no data". All three rows have this reason together, so the panel can show a single notice.
- **System page:** a 403 with the message "System status is limited to operator administrators." differs from the role 403 ("System health requires the admin role."). Suggested copy: "System status is limited to operator administrators. Your tenant is not an operator tenant." The navigation may keep System visible to admins. The UI cannot know operator status before the request, because no session field exposes it; adding one would be a separate decision.
- The regenerated `schema.d.ts` reason union includes `"operator_only"`, so any exhaustive switch over reasons must handle it.
- **Demo:** mint `--tenant demo --role admin` for the System page and `--tenant demo2 --role admin` to show the refusal.

## Notes for 05-19 (docs)

- Docs that describe System as "admin-only" or lag as "analyst/admin" should say operator tenants (ADR 0014): README, architecture, demo walkthrough and the UI-SPEC mention.
- `docs/operations.md` has a new "Operator tenants" section. `docs/threat-model.md` has a new "Phase 5 platform-wide views" section.
- **Upgrade note:** a local `.env` created before this change has no `SENTINEL_AUTH.OPERATOR_TENANTS`, so host `make run SVC=query-api` fails closed until `SENTINEL_AUTH.OPERATOR_TENANTS=demo` is added. Compose stacks are unaffected because they set the key.

## Threat Flags

None beyond the plan: this change only narrows existing surface. No new endpoint, schema change or trust boundary was added.

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: docs/decisions/0014-operator-tenant-allowlist.md
- FOUND commits: 84127fd, e785a2e, a42a795, cfc3144
