# Plan 12 ownership amendment A3 — independent bounded check

**Result: PASS — 0 blockers, 0 warnings.**

Checked A3 at shared HEAD `d9d3f62`, committed delivery source `921dbb2`,
corrected incomplete summary `12e8a0b`, the actual OpenAPI fixture/document,
production registration and preserved isolated-gate record. This approves
one fixture ownership addition only; it is not implementation review, a new
test pass, runtime recovery or Plan12/Phase4 acceptance.

## Necessary and sufficient thirteenth path

Approve adding only **`backend/test/openapi_test.go`** to the twelve approved
paths: **13 total,12 tracked plus ignored generated schema**. Its
`TestOpenAPIRoutes` builds a closed13-operation expected list and compares
sorted lists with `assert.Equal`. The document contains those same13
operations plus `GET /api/v1/alerts/backfill`. Fresh read-only static extraction
confirmed that adding exactly this one entry makes the two14-operation sets
equal; there are no other expected/documented route differences.

This route is already authorized by Plan12's bearer backfill action and the
shared delivery contract. Current `api/openapi.yaml` documents the exact GET
path with `BearerAuth`, bounded decimal cursor/limit semantics and200/400/401/
503 responses. `delivery.go:Register` registers that literal path through the
existing JWT verifier middleware, its handler obtains `RequirePrincipal` and
uses the verified tenant, and query main registers it before generic alert-ID
routes. The fixture addition aligns an existing approved route; it introduces
no new endpoint or runtime/security behavior.

The permitted source repair is **one expected-route string** inside
`TestOpenAPIRoutes`. Preserve sorting, exact equality, every earlier expected
operation and all existing schema/security/runtime assertions. In particular,
`TestOpenAPISecurity` already visits every documented operation and its default
branch requires exactly `BearerAuth` for backfill; no security special case or
exemption is needed. No wildcard, subset comparison, route removal, scanner
change or second source path is justified.

## Evidence and acceptance boundary

Read the existing private `20261025T105434279871Z-openapi.json` record: its
actual nonempty `^TestOpenAPI` invocation returned1 and contains10 top-level
passes plus only `TestOpenAPIRoutes` failing, with the exact13-versus14 diff
above. This is preserved prior evidence, not a newly executed test. The
corrected summary explicitly retracts the premature eleven-pass claim and
keeps Task2/Plan12 incomplete. Neither ordinary make-test nor this static
ownership PASS converts that failed gate into a pass.

Root must synchronize PLAN's ownership/task/read-first/scope before editing
the fixture, then execute the recorded nonempty OpenAPI family and retain its
real exit/output. Implementation source review and remaining root gates still
apply to the full approved union. All original008/native reverse-commit,
counter/cutover, CH-down, decimal precision, browser and quality requirements
remain. The separate NEW detector checkpoint-history hold is unaffected; no
full-pipeline or nine-services-healthy assertion is made.

Only this report was written and committed. Prior A1/A2 reports, source, PLAN,
tracking, runtime, policy and user-owned `.planning/config.json`/`.serena/`
remain unchanged and unstaged. No application, build, tests, scanner or runtime
tool was run; verification consisted of file reads and a static route-set/
record comparison.

```yaml
issues: []
additional_source_paths_needed: []
owned_path_count: 13
approved_added_path: backend/test/openapi_test.go
repair_constraint: "Add only GET /api/v1/alerts/backfill to TestOpenAPIRoutes expected list; preserve exact equality and every existing assertion."
implementation_acceptance: false
```
