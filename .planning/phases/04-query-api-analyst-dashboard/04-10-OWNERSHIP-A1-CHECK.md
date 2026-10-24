# Plan 10 ownership amendment A1 — independent bounded check

**Result: PASS — 0 blockers, 0 warnings.**

Checked against shared HEAD `59d433f31da9dd58e9d6da3b95c652983b5d6cca`
and accepted 09 source `68c10c1`. This is a static review of the proposed
ownership boundary, not implementation, runtime or whole-phase acceptance.
Only this review artifact is owned by the checker.

## Decision and causal need

Approve adding `web/src/api/client.ts` to the original eight declared paths:
nine paths total, including the ignored generated `web/src/api/schema.d.ts`.
`createAPIClient` keeps its `openapi-fetch` instance private and exposes only
specific methods; it currently exposes no Rules method. `useSession` supplies
that returned client. The real Rules page therefore needs a typed GET method
inside this factory. A direct page fetch or second credential/transport path
would bypass the existing session contract. The added method must retain
generation checks before credential access, session/caller cancellation,
post-body checks and current-session 401 disposal.

## Constructable delivery within the nine paths

| Concern | Owned implementation/test location and sufficient approach |
| --- | --- |
| Validated immutable startup definitions | `backend/cmd/query-api/main.go` can open `cfg.Detector.RulesPath`, call the existing exported `detection.LoadRules`, close the file and propagate open/parse/close failures before starting listeners. `config.Load` already populates Detector settings/defaults for query-api; adding the whole detector validation section is unnecessary. Copy the validated definitions into the Rules handler and never reread disk per request. |
| API registration without another source path | New `backend/internal/query/rules.go` can define its own Rules handler/constructor and `Register`, following the separate handler pattern in `query/events.go`. Query `main.go` can register it with the existing JWT verifier. This avoids modifying `query/alerts.go` merely to expand its existing `Dependencies` or `Handler.Register`. |
| All authenticated roles; no CRUD | Register only `GET /api/v1/rules`, using JWT middleware and verified principal without triage-role restriction. Native tests cover viewer/analyst/admin, unauthenticated 401 and rejected mutating methods. No detector, registry or mutable-rule write is required. |
| Exact version, threshold, window and enabled state | `rules.go` can map all four validated `detection.Rule` definitions, including disabled rules, to the OpenAPI DTO. Name/description can describe known rule semantics; operational values must come from the startup snapshot, with explicit duration units agreed in `api/openapi.yaml`. The generated schema and typed client remain inside ownership. |
| Current configuration versus retained historical definitions | Keep `postgres/query_alerts.go` and detector storage unchanged. See the historical distinction below. Owned native tests can create temporary valid manifests with distinguishable versions/values and use real query child processes. |
| Real Rules page and navigation | `web/src/pages/Rules.tsx`, `Rules.test.tsx` and `App.tsx` can use the new client method through React Query, expose actual `/rules` navigation/current-link state, render descriptions/parameters/version and explicit Enabled/Disabled text, and implement the exact loading/empty/error/retry copy from UI-SPEC. Existing styles are reusable. |
| Causal RED and failure proof | New `backend/testphase4/summary_rules_test.go` can build/run the actual query binary, reuse existing same-package token/port/process helpers for successful startup, and own a separate bounded child-launch helper for expected startup failures. No edits to `auth_test.go` or other fixtures are needed. `Rules.test.tsx` can exercise the actual App/client, including late body/session cancellation, without a separate client-test ownership addition. |

## Retained version distinction

The actual retained mechanism is not a generic current-rules registry.
`storage/postgres/query_alerts.go:RuleExplanation` reads a tenant/alert/rule/
version-bound detection decision joined to a matching fingerprint fence
manifest. It already checks the exact version and fails to an unavailable
explanation for missing, conflicting or incomplete historical data. Fence
manifests are mutable operational state, not a universal historical archive.
`alert_reservations` retains immutable canonical alerts, not current rule
definitions. Neither source should substitute for the query startup snapshot.

The approved distinction is constructable without additional source ownership:
the new native test file can insert an isolated decision/fence fixture using
the existing PG pool/schema, keep a retained version such as 77 distinct from
the temporary startup definitions, and assert `/rules` returns the exact
startup definitions. Existing historical-detail regression coverage remains
applicable and must continue to pass. No new historical API, registry migration
or detector configuration/loader behavior is needed or approved.

For startup immutability, the same native file can change/remove its temporary
rules file after the child is live and assert the GET remains unchanged;
another child can prove a valid restart adopts changed values. Invalid YAML,
invalid settings and an unreadable/missing rules path must cause a nonzero
process exit before public or ops availability. Those assertions fit the
owned native file; no fixture or config source expansion is necessary.

## Preconditions and limits

Root must synchronize the PLAN frontmatter, scope count, task files/read-first
and actions with this amendment before execution, including the separate
handler registration and historical/current-definition distinction above.
The currently unchanged eight-path PLAN is not authorization to edit a ninth
path until that synchronization. Any newly unavoidable path reopens ownership.

Read: A1 proposal, 10 PLAN, shared CONTRACT, accepted 09 SUMMARY, 07 runtime
recovery instructions, UI-SPEC, relevant requirements/research/patterns, actual
query main/handlers, detection loader, config, retained PG reads/schema, native
process fixtures, browser client/session/App and component analogs. No root
`AGENTS.md`, `.codex/skills/` or `.agents/skills/` exists. No application, tests,
builds, scanners, service operations or data changes were run by this checker.

Future executor proof must use current-source ephemeral API/browser processes
with the private NEW PG25432, CH28123/29000, Redis26379 and Kafka39092 context.
Shared 18081/18080 retain old 07 images and cannot establish current-source
acceptance. User-owned `.planning/config.json` and `.serena/` stay untouched.

```yaml
issues: []
additional_source_paths_needed: []
```
