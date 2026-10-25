# Plan 11 ownership amendment A1 — independent bounded check

**Result: ISSUES FOUND — 1 BLOCKER, 0 warnings.**

Checked shared HEAD `2a412c3`, accepted 10 metadata `9a47814` and source
`dbc7ae2`. This is a static ownership review, not implementation, runtime,
test, or whole-phase acceptance. Only this report is owned by the checker.

## BLOCKER — existing App regression fixture needs explicit route ownership

The proposed eleven paths omit `web/src/App.test.tsx`, which must be adapted
when the approved UI-SPEC Overview route `/` becomes real.

`App.test.tsx` imports and renders the actual App without setting an initial
history location. Its default document location is `/`; numerous successful
sign-in cases then require an Alerts response and rendered workflow. For
example, the identity-before-read/sign-out case requires “Real workflow alert”
and the second request pathname `/api/v1/alerts`; `defaultAlertFilters` also
requires that endpoint. Retry, cache, cancellation and expiry cases retain the
same implicit Alerts landing route. The shared test setup only cleans up React;
it does not establish `/alerts` for this file.

UI-SPEC explicitly requires Overview `/` and Alerts `/alerts`. Implementing
Overview at `/` makes these existing tests exercise the wrong page; keeping
Alerts at `/` or changing routing according to response shape would violate
the delivery contract. Changing shared global test setup from the new Overview
test file would introduce cross-file coupling and would not repair this file
when run independently.

**Required fix before source:** add `web/src/App.test.tsx` to ownership, making
twelve paths total (eleven tracked plus ignored generated schema). Restrict its
changes to explicitly establishing `/alerts` for the existing Alerts/session
scenarios and restoring navigation state as needed; preserve their behavioral
assertions, security coverage and request checks. Owned `Overview.test.tsx`
must independently establish and verify the actual `/` Overview route, current
navigation link and Back/Forward behavior. Root must synchronize the proposal
and PLAN frontmatter, scope count, task files/read-first/actions before execution.

## Causal need and remaining sufficiency

The two proposed additions are necessary:

- `backend/cmd/query-api/main.go` owns the actual CH connection, PG pool and JWT
  verifier. It must construct the summary adapters and register the real route.
  A separate focused summary constructor/Register in owned `query/summary.go`,
  following Rules, avoids edits to `query/alerts.go` or its Dependencies.
- `web/src/api/client.ts` keeps the generated `openapi-fetch` client private and
  exposes named methods to the session. A typed summary method must live there,
  retaining credential-generation checks, caller/session cancellation, checks
  after body parsing and current-session 401 handling.

No additional backend or clock ownership is necessary on the inspected source:

| Required behavior | Constructable within the proposed paths |
| --- | --- |
| Verified tenant and complete identity dedupe | Focused handler obtains the verified principal; CH summary can reuse package-local `uniqueEventRowsSQL` from `query_events.go`, which chooses the complete earliest-received canonical tuple with deterministic ties per tenant/event ID. Apply occurrence-time predicates and aggregates after that selection, never to physical duplicate rows first. No edit to the existing helper is needed. |
| PG workflow authority | New `postgres/summary.go` can add bounded aggregate methods using the existing AlertStore pool, or its own explicitly injected store. Current open/severity counts use PG status; top rules and alert trend count PG workflows independently of mutable status. CH-only orphan detections do not become authoritative alerts. Existing schema 7 provides all needed columns; no migration is required. |
| Shared time and exact boundaries | Own summary DTOs/interfaces and injectable clock in `query/summary.go`; capture UTC now once and pass explicit boundaries to both real adapters. Use rate `[now-60s,now)/60`, top rules `[now-24h,now)`, and trend `[end-24h,end)` with `end=floor(now/5min)*5min`. Return exactly 288 ordered buckets; stable top 20 orders count descending, then rule ID ascending. |
| Native deterministic and fault proof | `summary_rules_test.go` may construct the exported focused production handler with a frozen clock, existing verifier/middleware and real isolated native CH/PG adapters. It can own same-file fixture helpers, exact edge rows, changed representations across range boundaries, two tenants, more than 20 rule groups and status transitions. A separate real query-binary assertion must verify main registration/default production clock and authentication. Existing same-package token/process helpers are reusable read-only; no `auth_test.go`, other fixture file or production environment clock override is required. |
| Failure semantics | The owned test file can create independently owned failing/closed native connections or pools for CH and PG fault cases while retaining the other healthy dependency. Assert 503 for each failure, not an empty successful summary. Bound the request and queries to 5 seconds; zero-fill only after all required queries succeed. Do not stop shared services to inject failure. |
| Typed real browser behavior | OpenAPI/generated schema, guarded client, Overview page/tests and App cover the summary GET, session-scoped React Query, real accessible SVG/table, loading/error/retry/empty states and navigation. Existing CSS can be reused with local native markup/styles; no styling/framework dependency or extra source file is forced by these requirements. Preserve the explicit Phase 5 consumer-lag message, without System or fabricated metrics. |

The added App regression-fixture ownership is the sole further causal path
identified. This finding does not authorize editing it before root amends the
ownership proposal and PLAN. Any other unavoidable path reopens ownership.

## Review limits and preserved context

Read A1, 11 PLAN, CONTRACT, accepted 10 SUMMARY, 07 STACK-RECOVERY, applicable
UI-SPEC/research/patterns, actual main/query handlers, native stores/schema,
SummaryRules and native process/event/alert fixtures, guarded client, App and
existing component/session test routing. No root `AGENTS.md`, `.codex/skills/`
or `.agents/skills/` exists. This bounded review does not replan the phase.

Future proof uses only private NEW PG25432/schema7, CH28123/29000,
Redis26379/Kafka39092 and current-source ephemeral API/proxy. Shared18081/18080
are old07images. The stale006 migration container EXIT1 does not justify a
default Compose migration rerun; current-host tern and directly owned NEW app
recovery are the accepted context. No runtime, application, build, test, scanner,
policy, source, PLAN or shared-tracking operation ran during this check.
User-owned `.planning/config.json` and `.serena/` remain untouched and unstaged.

```yaml
issues:
  - plan: "04-11"
    dimension: "ownership_sufficiency"
    severity: "blocker"
    file: "web/src/App.test.tsx"
    description: "Root Overview routing changes the page exercised by existing App tests, which implicitly start at / and require Alerts; this necessary regression-fixture adjustment is outside the proposed eleven paths."
    fix_hint: "Add App.test.tsx as the twelfth owned path; explicitly start existing Alerts/session cases at /alerts without weakening assertions, and verify root Overview independently in Overview.test.tsx. Synchronize proposal and PLAN before source edits."
additional_source_paths_needed:
  - "web/src/App.test.tsx"
```
