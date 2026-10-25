# Plan 11 ownership amendment A2 — independent bounded recheck

**Result: PASS — 0 blockers, 0 warnings.**

Checked proposal `04-11-OWNERSHIP-A2.md` at shared HEAD `c9071db` against
the unchanged A1 proposal, original 11 PLAN/CONTRACT and the source inspected
in A1 check `51c16b1`. This is ownership sufficiency and blocker closure only;
it is not source implementation, runtime, test or whole-phase acceptance.

## Sole A1 blocker closed

A2 adds exactly `web/src/App.test.tsx`, making twelve owned paths total:
eleven tracked source/test/contract paths plus ignored generated schema
`web/src/api/schema.d.ts`. Its narrow responsibility is explicit `/alerts`
history setup before rendering the existing App Alert/session/privacy/retry
scenarios. All existing behavioral and negative/cache/token/disposal assertions
remain. This directly fixes the inspected implicit-root fixture conflict when
the approved Overview becomes `/`; it does not weaken those tests or alter
production routing according to test conditions.

New `Overview.test.tsx` must independently exercise actual root `/` with the
real App/session/client. Shared global test setup remains untouched. The route
adjustment belongs in the tests-only RED task; independent causal Overview
native/component RED still precedes production implementation. Compatibility
fixture changes alone are not behavioral RED evidence.

## Twelve paths sufficient

All other A1 obligations remain constructable within this boundary:

- Owned summary handler defines its own injected dependencies, DTOs and clock,
  authenticates with the verified principal and captures one UTC snapshot now.
  Main constructs the real CH/PG adapters and registers that separate handler;
  no edit to `query/alerts.go` or existing Dependencies is needed.
- CH summary can reuse the inspected package-local complete canonical-event
  identity selection before time predicates and aggregation. PG summary uses
  actual workflow rows for current open/severity counts and status-independent
  alert trend/top rules. Existing stores/schema supply needed connections and
  fields without a migration, detector or writer change.
- Owned native SummaryRules tests can inject a frozen clock into the exported
  focused production handler with real CH/PG/verifier/middleware, establish
  exact boundaries and duplicates/two tenants/status changes, and use isolated
  native dependency faults. Separate real-binary assertions prove production
  registration and authentication without an environment clock override or
  edits to shared fixture helpers.
- The same paths support rate `[now-60s,now)/60`, top 20 all-alert rule counts
  over `[now-24h,now)` ordered by count descending then rule ID ascending, and
  exactly 288 aligned five-minute buckets over `[end-24h,end)` where
  `end=floor(now/5min)*5min`. Bound queries/request to 5 seconds; only zero-fill
  after success, and return 503 for either native dependency failure.
- OpenAPI/generated schema and the guarded client method supply the typed
  browser path, preserving session/caller cancellation, generation checks,
  post-body checks and current-session 401 disposal. Overview/App/tests can
  deliver real SVG/table values, accessible states, root navigation and the
  explicit Phase 5 consumer-lag message using existing dependencies/styles.

No further causal source path is identified. The rejected A1 report remains
unchanged. Root must synchronize the full PLAN frontmatter, twelve-path scope,
task files/read-first/actions and route/test/wiring details before execution.
This PASS is not authorization to edit sources against the stale nine-path
PLAN. Any later unavoidable additional path requires another ownership check.

## Preservation and limits

Only this check report was written. No source, PLAN, shared tracking, runtime,
test, build, scanner or policy change was performed. User-owned planning config
and `.serena/` remain untouched and unstaged. Future proof retains the private
NEW PG25432/schema7, CH28123/29000, Redis26379/Kafka39092 context and current
ephemeral API/proxy; shared18081/18080 old07images and the stale006 migration
container do not prove current implementation or authorize migration reruns.

```yaml
issues: []
additional_source_paths_needed: []
owned_path_count: 12
closed_findings:
  - "A1 ownership_sufficiency: explicit App.test.tsx Alerts fixture route"
```
