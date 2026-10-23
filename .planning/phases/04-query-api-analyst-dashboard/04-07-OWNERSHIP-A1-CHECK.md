# 04-07 ownership amendment A1 — independent check

Decision: **ISSUES FOUND / REVISION REQUIRED**.

One BLOCKER; zero warnings. This is a planning-only assessment of proposal
`c598a3e` against accepted06baseline `ea2529e`, not runtime acceptance or an
implementation source review. No tests or application were run.

## Required revision

**BLOCKER — undeclared contract-test change:** add
`backend/test/openapi_test.go` to ownership and both tasks where applicable.
`TestOpenAPISecurity` currently pins GET /api/v1/events/{event_id} to exactly
ApiKeyAuth, requires exactly one security alternative, and asserts that sole
alternative is ApiKeyAuth. These assertions are in the event-specific case at
lines146–151. Required dual authentication in the already-owned OpenAPI document
necessarily contradicts this retained test. Updating only
TestOpenAPIGetEventRuntimeSecurity cannot resolve it; the tests independently
read the same document.

Revise that case to require exactly two separate nonempty alternatives,
ApiKeyAuth and BearerAuth (OR, not an AND requirement), retaining events:read
documentation, bearer-only assertions for all other dashboard operations and
the no-query-credential guard. Include this exact test in the causal RED and
retained OpenAPI/default acceptance. Do not omit its selector, weaken the global
security check, leave the document API-key-only or modify an undeclared file.

This makes the concrete envelope twelve declared paths: the proposed eleven
plus `backend/test/openapi_test.go`, or eleven tracked paths plus the existing
ignored generated schema. Retain the original two tasks and bounded context
target; the additional work is the directly affected existing contract test,
not another feature or permission for unrelated test changes.

## Architecture and necessity assessment

Both proposed additions are necessary. Actual main installs the blanket
preventBearerDowngrade middleware that rejects every Authorization header on
the detail path, including valid JWTs. It must supply the existing JWT verifier
to the replacement dispatcher and stop blanket rejection only when the strict
replacement is wired. The current guarded APIClient exposes event search but
no eventDetail method or public transport, so client.ts is required for the
approved detail component without exposing credentials.

The shared dispatch can live cleanly in the already-owned eventquery handler
and/or query/events.go, with main registering one single-event GET route.
Present Authorization must select the existing strict JWT middleware and its
verified principal/error mapping, including empty, repeated, malformed and
invalid headers. Only genuine absence selects the existing read-key branch;
write-only keys remain403. The selected verified identity supplies the tenant
to existing native Get. No new auth abstraction, global config lookup,
alternate token fetch, duplicate route, canonical schema change, migration or
dependency is necessary.

There is one compatibility detail to resolve deliberately: the retained
internal/eventquery/handler_test.go helper unconditionally sets Authorization
to an empty string, including passing read-key cases. A strict production
dispatcher must reject such requests. This does not establish that another
path is unavoidable: the existing API-key-only constructor/handler can remain
the isolated legacy branch, and a production dispatcher can select it only
when Authorization is absent. The owned runtime test must construct that same
production dispatcher with a real signed JWT, while actual-binary native tests
prove main wiring. Retaining the old branch for existing direct consumers must
not provide a production route that bypasses dispatch or accepts empty present
headers. If implementation instead makes that legacy constructor itself the
strict dual dispatcher and must adjust its fixture, declare the additional
test path before editing; do not mask the fixture mismatch with lenient auth.

No other source path is demonstrably unavoidable from the inspected
interfaces. APIClient is inferred from the factory return type and already
flows through session context; adding a guarded typed method does not require
a session.tsx edit. The new component can follow existing native-dialog and
escaped JSON patterns, with styling in its owned file and selected-state/history
logic in owned Events.tsx.

## Preserved gates and review limits

Read the complete07PLAN and A1, CONTRACT, approved UI-SPEC and accepted06SUMMARY;
inspected current main, legacy event handler and direct consumers/fixtures,
query event handler, JWT middleware, client/session boundary, Events page,
existing dialog, OpenAPI security tests and event operation. No root AGENTS.md
or project .codex/skills or .agents/skills directory was present.

All planned native Events6m, runtime OpenAPI60s, all90existing web cases,
Auth/Alerts/cursor/session/privacy and06IPv6/deep-link regressions remain
required. Real current-binary tenant/dual-auth and browser checks, schema/drift,
static/default/local gates, causal RED before minimal GREEN, independent source
review and root acceptance remain unchanged. A planning amendment cannot claim
any of these passed. No remote/publication or Phase5 authority is added.

Only this check file was written. No source, tests, tracking, user configuration
or .serena edits, builds, migrations, services, commits or remote actions were
performed. Return to the producer for the concrete ownership revision before
implementation.

```yaml
status: issues_found
decision: revision_required
blockers: 1
warnings: 0
issues:
  - plan: "04-07"
    dimension: "task_completeness"
    severity: "BLOCKER"
    task: "1 and 2"
    description: "The required dual-auth OpenAPI change necessarily contradicts retained TestOpenAPISecurity, but backend/test/openapi_test.go is outside the proposed eleven-path ownership."
    fix_hint: "Declare backend/test/openapi_test.go as the twelfth path; update only the event-detail security case to assert exactly separate ApiKeyAuth/BearerAuth alternatives while retaining all other security guards, and cover it in RED/GREEN and retained OpenAPI/default acceptance."
```
