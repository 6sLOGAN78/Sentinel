# 04-07 ownership amendment A1 — actual event detail wiring

Status: APPROVED by independentR1PASS/zeroissues after R0identified the required
contract-test path. Root synchronizes PLAN/both tasks before source/test edits.
R0preserved. This is planning approval, not runtime/source acceptance.

Read-only inspection at accepted06baselineea2529e establishes two unavoidable
runtime/client omissions from the original nine-path event-detail plan; the
independent checker additionally established the contract-test omission:

| Additional path | Exact bounded responsibility |
|---|---|
| backend/cmd/query-api/main.go | Inject the existing JWT verifier into the actual single-event auth dispatcher, and replace/remove the blanket middleware rejection that currently returns401 for every Authorization header on this path. Preserve all unrelated startup/routes and ensure one single-event route registration. |
| web/src/api/client.ts | Add generated typed eventDetail(id,signal) through the existing authenticated transport and its generation/lifetime/caller/session cancellation, parsed-body checks and401 handling. |
| backend/test/openapi_test.go | Update only the GetEvent-specific assertions in TestOpenAPISecurity that currently pin exactly one ApiKeyAuth alternative and API-key-only text, to require exactly separate ApiKeyAuth and BearerAuth alternatives. Retain scheme definitions, nonempty auth and all other dashboard bearer-only assertions. |

main.go currently installs preventBearerDowngrade before the legacy lookup;
that middleware rejects even a valid JWT. Editing only the planned lookup
handler cannot make actual bearer requests reach it. The key verifier has no
JWT verifier dependency. Actual command injection is necessary; no global
config read, second route, init-time side effect or auth bypass is acceptable.
The typed APIClient exposes session/alerts/alertDetail/events but no single
event method or public generic transport. The detail UI must not expose token
context or invent a standalone fetch path to avoid declaring this addition.

The exact envelope becomes twelve paths: originalnine plus these three (eleven
tracked plus the existing ignored generated schema). Retain two original
feature tasks and <=45% implementation-context target. No dependency/framework,
migration/index, new feature or additional source path. No source edits, tests
or RED/GREEN were performed for07 before this proposal.

R0found backend/test/openapi_test.go:146-151 necessarily fails the new required
dual-auth contract regardless of dispatcher implementation. This narrow owned
fixture correction preserves the gate, rather than skipping or relaxing it.
Existing eventquery/handler_test.go does not inherently require changes: its
legacy key-handler constructor can remain the key branch while a distinct
production dispatcher selects that branch only when Authorization is absent.
Native and owned runtime tests must use that exact production dispatcher.
No alternate weak handler may stand in for the real binary's authority boundary.

## Binding boundaries and unchanged gates

- One explicit GET event auth dispatch. Presence of Authorization (including
  malformed/empty/repeated/invalid bearer) never falls back to an otherwise
  valid read API key. A verified bearer supplies its own tenant; without the
  header, only the existing API-key read branch is allowed. Write-only key403,
  missing/invalid401, foreign/unknown404, malformedUUID400, dependency503.
  Other dashboard routes remain bearer-only. Preserve producer seed lookup
  response/canonical bytes, existing strict verifier and scoped error mapping.
- Shared dispatcher belongs cohesively in the already-owned eventquery handler
  and/or query/events.go; actual main supplies dependencies. No duplicated Echo
  registration or weakening of main's protection without the replacement
  production dispatch fully wired and tested. Existing consumers/testhelpers
  must remain compatible or trigger a new causal ownership checkpoint before
  any extra file is edited.
- The typed detail method obtains no credential outside the existing guarded
  transport. Retain checks before credentials/fetch and after parsed body,
  individual/session cancellation and current401 revocation. No credentials in
  context/keys/URLs/storage/logs. Native dialog uses escaped React pre/code
  text, honest404/503/retry, keyboard/Escape/restore and safe longJSON/IPv6.
- Keep accepted06 mixedIPv6-after-dedupe search, strict cursor/page/UTC/filter
  behavior and invalid initial/history validation. Retain all90web and earlier
  Alerts/Auth/detail/session privacy regressions. Exact nativeEvents6m and
  TestOpenAPIGetEventRuntimeSecurity60s must execute nonempty; replace arbitrary
  bearerfixture with valid signedJWT and productiondispatch, retaining legacy
  API-key/missing/invalid/invalid-bearer+key assertions. Real current-binary
  tenant/dual-auth/browser evidence, OpenAPI/drift, local/static/review gates
  remain required. No test, selector or deadline relaxation.
- Any thirteenth source path requires a causal ownership checkpoint before edit;
  no hidden shared changes. Root owns shared tracking; userconfig/.serena
  remain untouched/unstaged. No remote/push/hostedCI before04-16human-action
  authority, no destructive/datareset/OPTIMIZE/goldenregen or Phase5 action.

Independent checker must assess the exact twelve-path exception and causal
necessity. Approval is planning only; RED/GREEN, native/browser/local gates,
independent source review and root acceptance remain required.
