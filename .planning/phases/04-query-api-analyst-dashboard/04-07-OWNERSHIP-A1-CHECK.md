# 04-07 ownership amendment A1 — independent R1 check

Decision: **APPROVE / PASS**. Zero blockers; zero warnings.

Planning-only review of revised proposal `9c12712` against accepted06baseline
`ea2529e`. R0 remains preserved in04-07-OWNERSHIP-A1-CHECK-R0.md. This approval
does not assert source correctness, causal RED, runtime success or whole-phase
acceptance.

## R0 resolution

The revised amendment declares the exact missing path,
`backend/test/openapi_test.go`, and confines its change to the event-detail case
in TestOpenAPISecurity. Its current lines146–151 require exactly one
ApiKeyAuth alternative, which necessarily conflicts with the required dual-auth
document. R1 now requires exactly separate ApiKeyAuth and BearerAuth
alternatives, preserving scheme definitions, nonempty authentication and the
bearer-only assertions for other dashboard operations. This closes the sole
R0 BLOCKER without skipping or weakening the gate.

The root's R1 instruction explicitly commits to putting this contract test in
both original tasks and recording actual TestOpenAPISecurity RED/GREEN. The
original PLAN remains unamended during independent proposal approval, as
specified by the root. Before source/test work, synchronize its ownership,
task files/read-first/actions and verification with this approved amendment,
including the exact nonempty TestOpenAPISecurity selector alongside the existing
runtime security selector. This is the agreed plan synchronization step, not
permission to execute the stale nine-path PLAN or omit the new test.

## Bounded scope and architecture

The approved envelope is twelve declared paths, eleven tracked plus the
existing ignored generated declaration:

- backend/internal/query/events.go
- backend/internal/eventquery/handler.go
- backend/testphase4/events_test.go
- backend/test/openapi_runtime_auth_test.go
- web/src/components/EventDetails.tsx
- web/src/pages/Events.tsx
- web/src/pages/Events.test.tsx
- api/openapi.yaml
- web/src/api/schema.d.ts (ignored generated artifact)
- backend/cmd/query-api/main.go
- web/src/api/client.ts
- backend/test/openapi_test.go

This explicit exception retains the original two feature tasks and <=45%
implementation-context target. The three additions are causal: actual command
JWT injection and removal/replacement of blanket rejection; the existing
guarded client's absent eventDetail method; and the directly contradictory
contract assertion. No new feature, dependency, framework, migration, index or
canonical schema change is included. No further path is demonstrably
unavoidable; a thirteenth path requires the stated checkpoint before editing.

The revised compatibility arrangement is feasible: the existing API-key-only
handler constructor can remain the legacy branch for direct consumers, while
one production dispatcher selects it only when Authorization is genuinely
absent. Every present header, including empty/repeated/malformed/invalid,
selects strict JWT handling with no key fallback. Runtime tests must use that
same dispatcher and valid signed JWTs; actual-binary native tests prove main
wiring. Production must expose exactly one GET detail route. The legacy branch
cannot become an alternate production route or lenient bypass.

Existing strict JWT middleware supplies a verified tenant, and existing native
Get can serve the selected tenant with retained error mapping. Other dashboard
routes remain bearer-only. Missing/invalid credentials401, write-only keys403,
foreign/unknown404, malformedUUID400 and dependency503 remain required. The
guarded client can add its generated method without changing session context:
APIClient is inferred from the factory return type. Authority/cancellation and
post-parse checks remain mandatory.

## Gates and evidence limits

Re-read the revised A1 and preserved R0 fully, the original07PLAN and the pinned
OpenAPI/main sections. The prior full CONTRACT/UI-SPEC/06SUMMARY and interface
assessment remains applicable; the revision adds planning ownership only.
The git range contains only planning artifacts and preserves R0. No07source or
test change was inferred or credited.

All original native Events6m and runtime OpenAPI60s gates, exact new
TestOpenAPISecurity RED/GREEN, all90existing web cases, accepted06IPv6/cursor/
invalid-link behavior and earlier Auth/Alerts/session/privacy regressions remain
required. Browser/native current-binary evidence, schema/drift, static/default/
local gates, independent source review and root acceptance remain unchanged.
No selector, assertion or timeout reduction, mock-only native acceptance,
publication permission or Phase5 advancement is approved.

Only this R1 check file was written. No source/test/tracking/userconfig/.serena
edits, tests, builds, migrations, services, commits or remote actions occurred.

```yaml
status: passed
decision: approve
revision: R1
blockers: 0
warnings: 0
issues: []
```
