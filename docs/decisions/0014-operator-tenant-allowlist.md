# 0014. Operator tenant allowlist for platform-wide views

## Status

Accepted, 2026-11-18. The user chose this in chat ("Operator allowlist") after a security review of plan 05-21. It closes a Phase 5 gap between 05-21 and 05-16. It narrows two earlier user decisions:

- 05-21 "System admin-only": `GET /api/v1/system` was open to the admin of any tenant.
- 05-15 "analyst lag": `GET /api/v1/metrics/lag` showed lag values to analysts and admins of any tenant.

It is implemented in `backend/internal/auth/jwt.go` (`OperatorTenants`), `backend/internal/config/config.go` (`SENTINEL_AUTH.OPERATOR_TENANTS`), `backend/internal/query/system.go` and `lag.go`. It is guarded by the System, lag, config, OpenAPI runtime-auth/drift tests and the phase4 tenant matrix.

## Context

Sentinel is multi-tenant: tenant comes from the verified identity and every alert, event and summary read is tenant scoped (spec §10). Two Phase 5 endpoints break that pattern by design, because their data is platform-wide:

- System returns pipeline throughput, ingest outcomes, HTTP error ratios, DLQ rate, consumer lag, latency and replica readiness.
- Lag returns the maximum position-based lag per pipeline consumer group.

Neither has a tenant label, so neither can be scoped. Gating them only by role meant that an admin (System) or analyst (lag) of any tenant could watch the shared pipeline: other tenants' traffic volume, error bursts and backlog. In a hosted deployment, any customer's admin would be a platform operator.

## Decision

A server-side allowlist decides which tenants are operator tenants.

- Config: `SENTINEL_AUTH.OPERATOR_TENANTS`, a comma-separated list of tenant ids. It is validated at startup by query-api and dev-jwt: at most 16 entries, each must match the tenant slug `^[a-z0-9][a-z0-9_-]{0,62}$`, and entries must not repeat. It fails closed: empty or unset means no tenant is an operator.
- Membership is read only from the verified principal's tenant, never from a header, query parameter or extra claim. `auth.OperatorTenants` also drops malformed entries, so they can never match.
- System: data is returned only to an admin whose tenant is on the list. Check order: 401, role 403 (`role_forbidden`, unchanged), 400 for any query string, then the operator 403 with the message "System status is limited to operator administrators." and the audit reason `operator_forbidden`. A denied caller never reaches Prometheus or the readiness probes.
- Lag: viewers keep their audited 403. Analysts and admins of a non-operator tenant get 200 with all three rows `{"state":"unavailable","reason":"operator_only"}` and no value, and Prometheus is not queried. Returning rows keeps the Overview panel honest: it shows "operator only", not an error and never a zero.
- OpenAPI adds `operator_only` to the `SystemMetric` reason enum. The rule that an unavailable metric never carries a value still applies.
- The dev, observability and bench Compose stacks set `demo` (the primary demo tenant used by `make seed`, keyctl and the docs) as the only operator tenant. `demo2` is deliberately not an operator, so the cross-tenant refusal can be shown.

Rejected alternatives:

- A new `operator` role: it would widen the JWT role contract, dev-jwt and every role check for one platform view.
- Per-tenant System metrics: the pipeline metrics have no tenant label, and adding one would break the metrics label policy (ADR 0004).
- Keeping the endpoints open to any admin or analyst: this is the cross-tenant exposure the review flagged.

## Consequences

- Existing deployments, and local `.env` files created before this change, have no operator tenant until the key is set: System returns 403 to every admin and lag shows `operator_only`. This is intentional, fail-closed behaviour; the fix is to set the key.
- An admin outside the allowlist has a distinct refusal and audit reason, so operators can tell "wrong tenant" from "wrong role".
- The dashboard needs copy for `operator_only` and for the operator 403 (plan 05-16). Docs that described System as "admin-only" and lag as "analyst" now describe operator tenants (plan 05-19).
- The allowlist is static config. Changing it means restarting query-api; there is no runtime management API.
