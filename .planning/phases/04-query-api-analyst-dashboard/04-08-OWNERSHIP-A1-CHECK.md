---
phase: 04-query-api-analyst-dashboard
plan: "08"
amendment: A1
review_sha: b445d1c3465e45b66b265c045f19bdcdfb601880
accepted_predecessor: 2d9a8b7
status: approved
approval_scope: planning-only
findings:
  blocker: 0
  warning: 0
  total: 0
issues: []
---

# 04-08 ownership A1 — independent bounded check

**APPROVE / PASS. Zero issues.** Root may synchronize 04-08-PLAN.md with A1,
then dispatch its executor. This approves the explicit eleven-path scope
exception for the already planned acknowledge increment. It is not source,
test, migration, browser, runtime or whole-phase acceptance.

## Causal additions

| Additional path | Inspected need and approved boundary |
|---|---|
| `backend/cmd/query-api/main.go` | The actual command constructs `AlertStore` and supplies only `Alerts`, `Workflows`, `Details`, auth and cursors to `query.NewHandler`. An explicitly injected triage adapter is necessary to make the new PATCH reachable through the real binary. Retain lazy CH initialization, existing lifecycle and route/auth behavior. No global, store import in query, or runtime assertion substitutes for injection. |
| `web/src/api/client.ts` | `createAPIClient` currently returns read methods only; its underlying generated transport is private. The component needs a typed PATCH method accepting the selected alert ID, current version and requested acknowledged status. Use that transport and preserve credential isolation, generation/lifetime and caller cancellation, post-body checks and current-session 401 revocation. No automatic mutation retry or direct component fetch. |

`APIClient` is inferred from the factory return type and the existing session
provider consumes that type. No separate session/type/helper edit is required
to expose the method. Existing component tests already use actual App/session/
client flow with an owned fetch fixture, and directly exercise delayed parsed
bodies and cancellation. They can cover the new method in the owned test file.

## Sufficient bounded envelope

The approved union is eleven paths, ten tracked source/test/contract paths plus
the existing ignored generated declaration:

1. `backend/migrations/postgres/007_alert_audit.sql`
2. `backend/internal/storage/postgres/triage.go`
3. `backend/internal/query/triage.go`
4. `backend/internal/query/alerts.go`
5. `backend/testphase4/triage_test.go`
6. `api/openapi.yaml`
7. `web/src/api/schema.d.ts` — regenerate, do not force-add
8. `web/src/components/AlertDetails.tsx`
9. `web/src/components/AlertDetails.test.tsx`
10. `backend/cmd/query-api/main.go`
11. `web/src/api/client.ts`

The existing two tests-first/implementation tasks remain. The additions are
narrow wiring on the same feature path; they do not add independent features
or dependencies. This explicit exception exceeds the normal small-file target
but stays below the fifteen-file blocker. It does not authorize another path
or increased implementation context/runtime budgets.

The owned query files can declare the transition/history interfaces, extend
`Dependencies`, register the authenticated PATCH and replace the currently
empty GET history. Existing `StatusChange` and `WorkflowAlert` already provide
the required DTOs, so `query/types.go` is not inherently needed. The new PG
file can implement transition/history on the existing `AlertStore` with its
pool, bounded timeout and independent rollback helper. No writer rewrite is
required: existing `Ensure` is insert-only on conflict. Replay preservation is
to be asserted in the owned native test, not inferred from this observation.

The existing migrator loads wildcard-embedded PostgreSQL SQL, so adding 007
does not require embed/migrator changes. Existing phase4 fixtures execute real
migrations, build the actual query-api and start it with inherited configured
database endpoints and independently generated test signing material. The new
native file shares their package and may reuse those existing helpers without
editing them. The helper can run a process with an unreachable CH address while
retaining real PG; outage proof requires no shared-service shutdown.

Current OpenAPI already declares PATCH and its request. The planned change
from its current AlertDetail response to PG-only WorkflowAlert is required by
the contract. Canonical Event/Alert schemas remain unchanged. Inspected strict
OpenAPI checks require the existing request status/version fields and status
enum; they do not require the old PATCH response shape. No additional fixture
path is demonstrably necessary. Preserve those assertions and document 08's
resolved-target 409 until the already assigned 09 increment.

## Required behavior and gates remain intact

Verified server identity supplies tenant/actor, with existing RequireTriage
enforcing analyst/admin authority. Conditional tenant/ID/version/status UPDATE
and audit INSERT must share one bounded PG transaction. Concurrent requests
must yield one 200, one 409, exactly one audit row and one version increment;
an audit-insert failure must roll back the workflow change. Foreign IDs remain
404, viewer mutation 403, invalid bodies 400 and oversized bodies 413. No skip,
reopen or no-op transition is accepted. History stays tenant-scoped, ordered by
version and bounded to two forward changes.

PATCH returns committed PG metadata without a CH read. A later independent GET
detail failure cannot turn that successful transition into a failed mutation
or trigger a repeated PATCH. Browser coverage retains pending-disable, viewer
controls hidden, status/version/history, explicit stale-conflict notice and
refetch with a new user action required. Preserve session revocation, delayed
body/caller cancellation, escaped content and prior list/detail/navigation
regressions. Acknowledgement is the whole 08 behavior; resolve remains 09.

Native and component causal RED must be separate actual invocations with real
exits and assertions before minimal GREEN. Missing migration/table, build or
fixture errors are setup failures, not behavioral RED. Native tests can first
assert the absent production PATCH behavior without depending on a nonexistent
audit table. Actual 007 migration, PG race/fault/rollback/replay assertions and
CH-independent positive PATCH remain required GREEN evidence. Retain the exact
nonempty TestPhase4Triage selector, phase4-only tag, race and six-minute cap;
all affected earlier native, web, OpenAPI/schema, quality and browser gates,
independent source review and root acceptance still apply.

## PLAN synchronization and runtime boundary

Before source execution, root must include both additions in frontmatter,
Task 1 read-first and Task 2 files/read-first/actions, update the nine-path
scope text to eleven, and describe the production injection and guarded client
link. Link A1 and the accepted 04-07 stack-recovery handoff. Preserve the
tests-only RED then minimal GREEN ordering and all original negative cases.

Native/migration acceptance must use only the private NEW context:
`.local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/context.py`.
It selects PG25432, CH28123/29000, Redis26379 and Kafka39092; the separate app
and Vite endpoints are 18081 and 15173. The launcher supports `--cwd backend`
and `--timeout N -- COMMAND`; from backend the existing test runner path is
`../scripts/go-test-nonempty.sh`. From repository root it is
`scripts/go-test-nonempty.sh`; that runner itself selects backend as Go cwd.
Retain the NEW environment in child native processes. Execute actual 007 via
the existing migrator explicitly against NEW PG; SQL presence/compilation is
insufficient. Do not use default old-stack Make/runtime/seed/migration flows.
Do not reset or stop shared services to create an outage test.

Any causally necessary twelfth path requires another ownership amendment
before editing. Root retains STATE/ROADMAP/REQUIREMENTS/VALIDATION/reviews;
executor retains only the eleven paths, ignored private 08 evidence and
04-08-SUMMARY. No scanner exception/configuration, new tool/setup, runtime
repair, remote operation or Phase5 work is included.

## Review provenance and limits

Read A1, original 08 PLAN, full CONTRACT, 07 SUMMARY and STACK-RECOVERY, relevant
PATTERNS/UI-SPEC/VALIDATION, and actual production main, query interfaces/routes,
PG workflow/writer/migrator/embedding, auth role guard, native fixtures,
OpenAPI strict assertions, generated-client types, session provider and detail
component/tests. Applied the supplied GSD plan-checker Revision Gate and its
scope/coverage/wiring/data-contract checks only to this bounded amendment.
There is no root AGENTS.md or project .codex/skills/.agents/skills directory.
Whole-phase dimensions and requirement replanning are outside this check.

Reviewed HEAD is the full SHA in frontmatter, with accepted 07 predecessor
2d9a8b7. Only this report was written and committed. No tests, builds, services,
migrations, producers, browser, SDK tracking or remote operations were run.
User `.planning/config.json` and `.serena/` remain untouched and unstaged.

```yaml
status: passed
decision: approve
blockers: 0
warnings: 0
issues: []
```
