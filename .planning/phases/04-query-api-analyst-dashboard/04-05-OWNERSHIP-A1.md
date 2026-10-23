# 04-05 ownership amendment A1 — actual detail dependency wiring

Status: PROPOSED; independent planning approval required before implementation.

Read-only inspection at accepted `fdd3938` found three necessary consumers or
adapters absent from the original nine-file ownership. No source/test changes
or test execution preceded this proposal.

| Additional path | Bounded responsibility |
|---|---|
| `backend/cmd/query-api/main.go` | Inject the actual ClickHouse detail adapter and existing PG workflow adapter into query dependencies; preserve list/session/legacy routes and lazy CH startup. |
| `backend/internal/storage/postgres/query_alerts.go` | Parameterized, bounded-time tenant-and-alert-ID workflow lookup before any CH access. Any retained exact rule-version lookup must also be bounded and truthfully return unavailable when absent or ambiguous. |
| `web/src/api/client.ts` | Generated typed `alertDetail(id, signal)` method using the existing guarded client, with current generation/lifetime checks before fetch and after parsing, plus individual/session cancellation. |

The existing query dependencies expose only the PG list interface; command
wiring supplies that adapter, auth and cursors. The CH adapter cannot be reached
by the real binary without a new explicit injection. Existing ListAlerts
requires a time range and is not a substitute for a tenant-and-ID lookup.
The writer-only Find method is not tenant-scoped and returns only tenant and
fingerprint, not the public workflow. The current typed client has only
session()/alerts(), so a standalone token fetch would bypass accepted lifecycle
guards rather than deliver the intended typed detail boundary.

The exact proposed envelope is twelve paths: the original nine (including
ignored generated schema) plus these three. This is a bounded repair of the
plan's omitted wiring, not a new feature, subsystem, migration, registry,
framework, dependency or additional endpoint. Keep two tasks and <=45% context
target; if necessary, return a checkpoint rather than silently expand again.

## Constraints and strict acceptance

- Preserve the fifteen-field PG WorkflowAlert and current list filtering,
  authenticated cursors, precision, bounded queries and canonical schemas.
  Never sweep list pages to find one ID or access concrete store internals.
- Get the verified tenant first; direct workflow lookup must filter both tenant
  and ID in SQL and complete before CH. Unknown/foreign IDs remain 404 without
  an orphan-detection fallback. CH canonical detection and every evidence query
  remain tenant-filtered, bounded, cancellable and parameterized.
- `detection_partition_fences.rules_manifest` is a retained mutable manifest,
  not a guaranteed historical registry. Exact recorded-version definitions may
  be used only when truly available and unambiguous; otherwise explain their
  unavailability. Do not label current thresholds as historical facts or add a
  migration/registry. Limit returned rows and decoded payload sizes; no bulk
  unbounded manifest loading. This lookup is optional explanation enrichment,
  not a substitute for real canonical detection/evidence.
- Reuse the existing typed client's credential/ref and pre/post-body guards.
  Keep credentials out of public context, keys, URLs, storage and logs. Invalid
  current 401s must retain existing revocation behavior; old detail requests
  must not return/repopulate another session's data.
- No other source paths are authorized. Existing App/session/styles, retained
  fixtures and CI remain outside the extra grant. If an actual strict consumer
  fixture fails because of the newly delivered route, stop with its causal
  evidence rather than weaken or silently edit it.
- Preserve all existing native Alerts/Auth/cursor, OpenAPI and web regressions.
  Actual native PG+CH detail/evidence/tenant/outage/rule-version negatives,
  component dialog/focus/XSS/history cases, applicable quality gates and scoped
  real browser evidence remain required. No mock-only acceptance or waived cap.

Independent checker must approve this explicit twelve-path exception and its
causality before root updates PLAN ownership/Task2. Approval is planning only;
executor must still demonstrate causal RED, implement, verify and obtain clean
independent source review/root acceptance. Root owns shared tracking; user
configuration and `.serena` remain untouched/unstaged. No 04-06, Phase5, remote
push or hosted CI is authorized by this amendment.
