---
phase: 04-query-api-analyst-dashboard
plan: "07"
status: complete
subsystem: api
tags: [events, jwt, api-key, clickhouse, react-query, dialog, openapi]
requires:
  - phase: 04-06
    provides: Accepted unique event search, bounded cursors and guarded browser transport
provides:
  - Single production event lookup with strict bearer-or-absent-header read-key dispatch
  - Tenant-scoped canonical event JSON in a keyboard-operable native dialog
  - Typed guarded detail requests, history links and signed runtime contract coverage
affects: [04-08, 04-15, 04-16]
tech-stack:
  added: []
  patterns: [explicit-credential-dispatch, native-dialog, escaped-json-text]
key-files:
  created: [web/src/components/EventDetails.tsx]
  modified:
    - backend/internal/eventquery/handler.go
    - backend/cmd/query-api/main.go
    - backend/testphase4/events_test.go
    - backend/test/openapi_runtime_auth_test.go
    - backend/test/openapi_test.go
    - web/src/api/client.ts
    - web/src/pages/Events.tsx
    - web/src/pages/Events.test.tsx
    - api/openapi.yaml
    - web/src/api/schema.d.ts
key-decisions:
  - "Retain the isolated legacy key constructor; inject both verifiers into one strict production dispatcher shared by main and the runtime contract fixture."
  - "Use existing session transport, generated Event types, identity/generation query keys and native dialog lifecycle without exposing credentials."
requirements-completed: [API-02, AUTH-03, UI-03, UI-06]
duration: Approximately 15min from first timed setup to source and summary closeout; excludes earlier mandatory reads
completed: 2026-10-23
---

# Phase 4 Plan 07: Safe event details Summary

**Verified-tenant event JSON with strict dual credential dispatch and an escaped native detail dialog.**

Implementation tasks and independent R2/source/root gates are complete. Root
accepted this slice on2026-10-24 after the explicitly authorized separate demo
recovery and fresh independent checks recorded in04-07-STACK-RECOVERY. The old
Kafka history remains missing; its hold and all prior failures are preserved.
R2 at2059335/sourcecc22566 closes CR-01/CR-02 with zero findings;
preserved04-REVIEW-07-R2. Root independently ran backend/web builds and normal
make test: EXIT0/13.857s,END2026-10-23T13:08:56.404072Z,cachedGo/web114/gen121,
private root-postwave.log. Full-history scanning
found one historical planning-prose match described below; root subsequently
resolved it through independentSCAN-A2classification and one exact fingerprint
exception. Actualfullhistoryatd2a3e43EXIT0/zero findings/557commits,0.819s,
END2026-10-23T05:35:55Z,private root-disposition-history-scan.log. Original
failed scan remains unchanged. Phase4 is8/17 accepted. Requirement IDs above describe incremental
coverage, not whole-phase acceptance or permission to advance.

Independent R0 subsequently found CR-01: a persisted detail owner embedded a
generation that resets on hard reload. Its failed-login/retry reproduction was
missing from the original passing gates. Repair RED`3260ab6`/GREEN`ec14c34`
addresses that finding in the two already-owned Events files only. Independent
R1 closed CR-01 but found the interpage popstate regression CR-02. Its repair
RED`843ba72`/GREEN`cc22566` subsequently passed clean R2 and root postwave.
Read-only Kafka bounds now establish detector checkpoints below earliest
retained offsets in all six event partitions; each earliest equals end. The
unchanged detector correctly refuses that missing history. See04-07-STACK-HOLD;
operator chose a separate fresh synthetic stack preserving old state. That
stack passed real health, seed, detection and browser checks; root independently
rechecked native Events, exact seed/detection, health, preservation and builds.
No old offsets/groups/data/volumes were reset. Neither the original passes nor these scoped repair passes
constitute whole-stack or whole-phase acceptance.

## Task commits and ownership

1. Task1, causal tests-only RED: `c40b1e5`.
2. Task2, minimal implementation and owned fixture/style corrections: `242b162`.

Base `d4fbaa2945bb9d075039731c90f299bdb7aeaea1`. A1R1 approves twelve declared
paths; the actual changed union is ten tracked paths plus the ignored generated
schema. `backend/internal/query/events.go` needed no change and remains in the
declared review envelope. No thirteenth source path was edited. No dependency,
framework, migration, index, canonical schema, shared session/style/test helper or
scanner allowlist changed. User configuration and `.serena/` remain untouched and
unstaged. Root owns shared STATE/ROADMAP/REQUIREMENTS/VALIDATION updates; no SDK
tracking mutation was run by this executor.

## Delivered boundary

- Main injects its existing strict JWT verifier into `NewAuthenticatedHandler`
  and registers one GET single-event route. Every present Authorization header,
  including empty, repeated, malformed and invalid values, selects existing JWT
  middleware with no key fallback. Genuine absence selects the original read-key
  branch. The old key constructor remains isolated for retained direct consumers.
  Native actual-binary tests and the signed runtime fixture exercise this same
  production dispatch. Other dashboard routes remain bearer-only.
- The selected verified tenant reaches existing native `EventStore.Get`, with a
  caller-derived five-second context. Foreign/unknown IDs return404, invalid
  UUID400, missing/invalid credentials401, write-only keys403, dependency503.
  Native success responses are compared with the real canonical store result;
  no producer seed lookup representation or stored event bytes were changed.
- Guarded `eventDetail(id,signal)` uses generated types and the existing private
  fetch transport, authority generation, session/caller cancellation, post-body
  checks and current401 revocation. No token appears in public context, URLs,
  persistent browser storage or logs.
- EventDetails uses native `showModal`, close/Escape, exact trigger restoration,
  per-session detail queries, loading, honest404/error and retry. `pre/code`
  contains only React text from `JSON.stringify`, with wrapping for long JSON
  and stored expanded IPv6. Events owns detail buttons, initial deep links,
  Back/Forward and session-owned URL cleanup, including StrictMode replay.
- OpenAPI advertises exactly two separate nonempty alternatives on this route.
  The narrowly affected security test retains all other scheme, no-query-token,
  events:read and dashboard bearer-only assertions. The runtime fixture replaces
  arbitrary bearer text with an independently verified random-key signed JWT,
  while retaining real read/write/missing/invalid/no-fallback checks.
- Accepted06 dedupe-before-filter/order/limit, mixed stored IPv6 comparison,
  exact fifteen-ID/five-page checks, strict AEAD cursor binding, invalid initial
  and history URL validation, UTC minute edits, debounce, bounded history and
  session privacy remain covered and unchanged.

Generated ignored schema SHA256:
`7c5d0c380ad4cdf3b17d2fdc25fa0a435d89d89fbc1855c005eb433514e46194`.

## Actual verification

Private evidence: `.local/phase4-acceptance/04-07/`, directories0700 and files0600.
The harness records command, HEAD, working changes, UTC start/end, monotonic elapsed
and actual exit independently. Children use umask022. GREEN runs record RED HEAD
plus working implementation; the exact reviewed source is committed as242b162.

All dates below are2026-10-23 UTC. Separate RED commands ran before implementation;
setup/build/signature verification succeeded and actual behavior assertions failed.

| Log | Start–end UTC | Exit / elapsed | Actual evidence |
|---|---|---|---|
| native-red | 05:18:50–05:18:57 | 1 /6.946s | Actual binary rejects valid bearer details401 instead of200/404/400/503; prior search cases pass |
| contract-red | 05:18:50–05:18:51 | 1 /0.728s | Exact TestOpenAPISecurity sees sole ApiKeyAuth instead of two separate alternatives |
| runtime-red | 05:18:50–05:18:51 | 1 /0.780s | Valid signed bearer401; legacy fixture incorrectly permits malformed present bearer plus read key |
| web-red | 05:18:50–05:18:54 | 1 /3.725s | Sixteen new detail/client cases fail; all26 prior Events cases pass |
| contract-final | 05:24:12–05:24:13 | 0 /1.020s | Exact nonempty TestOpenAPISecurity,60s |
| runtime-final | 05:24:12–05:24:13 | 0 /1.234s | Exact nonempty TestOpenAPIGetEventRuntimeSecurity,60s, shared strict production dispatcher |
| openapi-final | 05:24:12–05:24:15 | 0 /3.393s | All11 OpenAPI/race/60s cases, strict fixtures and canonical drift |
| alerts | 05:21:52–05:22:01 | 0 /8.724s | Prior exact native Alerts/race/6m family |
| auth | 05:21:52–05:22:06 | 0 /13.716s | Prior exact native Auth/race/6m family and24 CLI cases |
| quality-final | 05:27:12–05:27:25 | 0 /13.768s | Auth/eventquery/query race60s; default/phase4/integration vet/build/lint, zero issues |
| native-complete | 05:27:34–05:27:43 | 0 /8.471s | Final exact native Events/race/6m, both credentials/tenants/status matrix and preserved search |
| web-complete | 05:27:34–05:27:48 | 0 /13.452s | All106 cases (90 retained+16 new), typecheck/lint/build/format |
| default-complete | 05:27:34–05:27:43 | 0 /8.385s | Normal make test300s outer: Go race, web106 and locked generator121 |
| browser-complete | 05:28:50–05:28:58 | 0 /8.001s | Actual current race-built API+CLI, helper, native PG/CH/Redis, Chromium1440/375, no mocked routes |
| source-scan | 05:29:02–05:29:03 | 0 /0.722s | Pinned gitleaks8.30.1 staged source and generated schema reports both[] |
| history-candidate | 05:29:58–05:29:59 | 1 /0.843s |553 commits, one historical root-owned planning-prose match; not source leakage or a passed gate |

Native commands independently use `bash ../scripts/go-test-nonempty.sh './testphase4/...'`
with the exact `^TestPhase4Events$`, `^TestPhase4Alerts$`, `^TestPhase4Auth$` selector,
`-tags=phase4 -race -timeout=6m -p=1`, from backend. Contract/runtime commands use
the exact selectors above with `-timeout=60s`; all OpenAPI uses `^TestOpenAPI`
and `-race -timeout=60s -p=1`. Default make test runs `go test -race ./...` internally,
without an invented explicit60s cap. Original CI/native caps were not changed.
Full exact static/web/browser/scanner commands are retained in each private log.

Native fixtures retain physical duplicates before reads (129rows/63identities in
the first GREEN) without merge waits or OPTIMIZE. Browser compares displayed JSON
with the actual native response, checks foreign404, malicious source/attributes,
2500-character JSON, retained expanded IPv6, focus, Escape/exact trigger, history,
deep reload/sign-in, missing-event state, no page/dialog overflow, no overlay,
no unexpected console errors and no persistent credentials. Actual phone dialog
is375px wide/top0/bottom1000; desktop768px wide, also fits its1000px viewport.
Viewport phone screenshots were visually inspected. Screenshots/results remain
private under `complete/`; focused slice evidence is not final proxy/two-replica,
SSE, full tenant-matrix, accessibility certification or performance proof.

## Issues and deviations

No architectural or ownership deviation. Three bounded owned lint correction
passes fixed canonical header spelling/named timeout/line length, extracted the
same native detail cases to meet complexity limits, then used context-first
helper arguments. Final three-mode quality passed; no assertion was weakened.
Initial web GREEN exposed JSON quote-escaping and mock-cleanup-order expectations;
the owned tests now compare parsed payload and clean up before restoring dialog
methods. All failed runs remain preserved.

Initial browser assertions incorrectly required Tab always to focus the sole
button. Actual diagnostics proved BODY only when `document.hasFocus()==false`
(browser chrome), native modal still active, next Tab returns inside, and focus
of background controls is blocked. The exploratory focus-red component assertion
required custom event cancellation rather than required native behavior; it was
removed before source closeout. It is **not product RED or a fixed product bug**.
No custom focus trap was added. The original failed browser/focus expectations
and subsequent native-focus proof remain distinct historical evidence.

Full-history scanner finding: `generic-api-key` in historical root-owned
`04-07-PLAN.md:63`, commit `5c0454cab0d0eca0ba6541e8a98082275a971571`.
Source/generated scans were clear but the executor correctly stopped before
changing scanner ownership. Root then proposed SCAN-A2; independent classification
confirmed historicaltest-policyprose,notcredential. Rootdeclared.gitleaksignore
asthirteenthmetadata/reviewpathbeforeaddingonlytheexactfingerprint/commentin
d2a3e43. Existingexceptions/config/pin/commands/historyremainunchanged; no broad
exclusion or credential waiver. Fullhistory nowEXIT0/zero as recorded above.
No Secret/Match was printed. Independent source review and root postwave still
must clear before acceptance; original failure is not retroactively green.

Karpathy guidance kept edits causal; React documentation confirmed native-dialog
effect cleanup under StrictMode. Existing UI-SPEC supplied the design without a
new design cycle. Browser plugin unavailable; cached Playwright/Chromium was
reused without installing dependencies. No authentication gate, new unregistered
security surface, stub, external setup, remote/push/hosted CI, data deletion,
reset/prune, golden regeneration or Phase5 operation occurred.

## Self-Check: PASSED

All declared paths exist including the new component and ignored generated schema.
Both task commits exist and RED precedes GREEN. The cumulative changed source union
is inside the approved twelve-path envelope. Source whitespace checks pass; no
tracked file deletion or unexpected source/untracked artifact remains. Only user
configuration and `.serena/` are dirty. This checks artifact claims, not acceptance:
the independent/root gates remain pending. Scanner closure afterward is recorded
above, with explicit independentlyapprovedrootmetadataexception.

## CR-01 repair evidence — pending R1

Tests-only`3260ab6a8d0fb91559deb9afe73e50428f6a4a53` precedes minimal
GREEN`ec14c34ffaf62eebdfa49059287bc3a46b1a3a17`. Repair baseline is root's
documentation-only`738f8f5`; feature242b162 was not restarted. Only
`web/src/pages/Events.tsx` and `Events.test.tsx` changed. Existing R0 report and
all historical failures remain preserved; tracking/reviews/ignore configuration
were not edited by this executor.

History now stores an unambiguous JSON tuple of verified subject/tenant/role,
a per-document random marker and the transient session generation. A fresh
document may restore its first verified identity despite a reset generation;
an effect remembers that document's first verified Events generation. Later
sessions in the same document cannot resurrect old-document history, while
same-document entries continue requiring the exact current generation. Accepted
popstate entries are adopted into the current owner. Different subject, tenant
or role is rejected. Explicit logout cleanup compares structured owner fields.
Existing client/request/cache generations and lifetime/caller cancellation are
unchanged. No colon-string identity parsing, auth fallback, persistent token,
App/session source edit, dependency or focus workaround was introduced.

Regression tests use actual StrictMode App/session/client/components. They first
fail login with401, successfully retry, open a real component detail, save the
actual URL/history before unmount, restore it after cleanup, reset modules and
bootstrap a fresh provider. This models hard reload without falsely applying
React unmount URL cleanup. The causal RED loses the literal event UUID. Six
new cases cover recovery, each changed identity field, same-page stale generation,
and old-document history after restored-session sign-out; the last guard also
checks cache clearing. All original106 tests remain, final total112.

Actual2026-10-23UTC repair logs in the same private07directory:

| Log | Start–end UTC | Exit / elapsed | Evidence |
|---|---|---|---|
| review-reload-red | 12:19:22–12:19:24 | 1 /1.971s | Same-identity hard-reload UUID disappears; four negative guards pass |
| review-reload-green | 12:20:35–12:20:42 | 0 /7.115s | Focused five cases plus typecheck/lint after minimal owner repair |
| review-web-final | 12:24:51–12:25:05 | 0 /14.067s | All112 tests, typecheck/lint/build/format |
| review-default-final | 12:24:51–12:25:00 | 0 /8.812s | Normal make test300s outer, Go race/web112/generator121 |
| review-quality | 12:21:29–12:21:46 | 0 /16.129s | Quick auth/eventquery/query race60s; default/phase4/integration vet/build/lint |
| review-contract | 12:21:30–12:21:32 | 0 /2.333s | Exact nonempty TestOpenAPISecurity,60s |
| review-runtime | 12:21:29–12:21:32 | 0 /2.418s | Exact nonempty signed production-dispatch runtime selector,60s |
| review-openapi | 12:21:30–12:21:35 | 0 /5.506s | All11 OpenAPI/race/60s cases |
| review-native-ready | 12:24:02–12:24:10 | 0 /8.177s | Exact Events/race/6m; physical duplicates, mixedIPv6, search/cursor/detail authority retained |
| review-auth-ready | 12:24:02–12:24:15 | 0 /13.334s | Exact Auth/race/6m and CLI cases |
| review-alerts-ready | 12:24:02–12:24:10 | 0 /8.271s | Exact Alerts/race/6m |
| review-browser-ready | 12:24:41–12:24:50 | 0 /9.156s | Actual current race-built API+CLI/helper/native PG+CH, Chromium1440/375, real failed-login/retry/hard-reload recovery |
| review-source-scan | 12:27:09–12:27:10 | 0 /0.855s | Pinned8.30.1 staged repair/generated reports[] |
| review-history | 12:27:26–12:27:27 | 0 /0.871s | At ec14c34,562commits, report[], existing exact root-approved exception unchanged |

Each log retains its full command, HEAD/working state and precise timestamps.
Browser uses actual `page.reload()` on the open selected URL/history, authenticates
the same identity after prior failed401/success, observes generation4→2 and a new
page marker, and verifies restored canonical JSON and event_id. Both widths also
retain native-modal/browser-chrome focus proof, exact Escape trigger, Back/Forward,
expanded stored IPv6, long safe JSON, missing404 and logout/privacy checks. No route
or network mock is used; expected401/404 console network messages are distinguished
from unexpected browser errors. Private results/screenshots are in
`review-reload-ready/`. Generated schema hash is unchanged.

Initial repair native Events/Auth/Alerts attempts exited1 during setup because
PostgreSQL15432 was refused after the host/server restart; the browser then
correctly exited1 before execution because it had no passing native fixture.
These are **setup failures**, not causal RED or behavioral results. The executor
started only the four existing Kafka/CH/PG/Redis containers with preserved volumes
using normal Compose `start --wait`; all four became healthy, exit0/7.417s ending
12:23:30Z. No recreate/pull/teardown/reset/config/data edit occurred. The separate
exact native reruns above then passed.

At bounded read-only closeout, eight stack services were healthy but detector was
still restarting (exit1, OOMfalse,33restarts). Private
`review-detector-readonly.log` captures the last25 log lines; no raw error, secret,
header, credential or resolved Compose configuration was printed. No unowned
detector investigation or repair followed. Root will assess preserved-stack
readiness separately; **no all-nine-healthy or full-pipeline PASS is claimed**.

Repair self-check: both owned files, all declared artifacts and generated schema
exist; RED3260ab6 and GREENec14c34 exist in order; source whitespace passes and no
tracked file deletion occurred. No stub or new security surface was introduced.
Only user config/.serena remain dirty outside the intentionally updated summary
and repair evidence. Independent R1/root postwave and stack readiness remain open.

## CR-02 repair evidence — pending R2

Independent R1 closes CR-01 and establishes a new counterexample missed by the
passing112-test suite: Events processed a popstate to `/alerts` and overwrote
the destination's filters/selection before Alerts mounted. The prior passes
remain valid scoped observations, not proof against this newly exercised path.

Tests-only`843ba721e68df1ff82ed369e72f774d6875425c5` precedes minimal
GREEN`cc22566de61b2c5a6d19812e960fd46e01bbf349`, on root baselinebe7ef9b.
The production delta is exactly one pathname guard at the start of the Events
popstate callback, before any state read/update or URL write. Events history
adoption, CR-01 stable identity/document/generation policy and privacy guards
are unchanged. No App, Alerts, session, backend, generated contract, scanner
exception, dependency or other source file changed.

Two actual StrictMode App cases start with critical/open/needle/rule/limit Alerts
filters, navigate through the real Events navigation link, and invoke real
`history.back()`/`history.forward()` rather than replacing the destination or
dispatching a synthetic popstate. They assert destination URL, visible controls,
all subsequent typed alert-list request filters and the selected alert/detail
request in the selected-ID variant. They also assert Forward restores the Events
URL/results. Existing112 cases remain; final total114. Both new cases fail
causally before the guard because severity is removed (null vs critical).

All following logs are private04-07 evidence, date2026-10-23UTC, with full
commands/SHA/working state/precise start/end/actual exits:

| Log | Start–end UTC | Exit / elapsed | Evidence |
|---|---|---|---|
| cr02-red | 12:56:33–12:56:35 | 1 /1.364s | Both real Back cases lose destination severity |
| cr02-green | 12:56:49–12:56:50 | 0 /1.404s | Same two cases pass after only the pathname guard |
| cr02-web-final | 12:58:58–12:59:14 | 0 /15.480s | All114 cases, type/lint/build/format |
| cr02-default | 12:58:08–12:58:26 | 0 /18.122s | Normal make test300s outer: Go race/web114/gen121 |
| cr02-quality | 12:58:08–12:58:28 | 0 /20.601s | Quick race60s and default/phase4/integration vet/build/lint |
| cr02-native | 12:58:08–12:58:23 | 0 /15.103s | Exact native Events/race/6m, all retained search/detail cases |
| cr02-auth | 12:58:08–12:58:29 | 0 /20.972s | Exact native Auth/race/6m and CLI cases |
| cr02-alerts | 12:58:08–12:58:23 | 0 /15.260s | Exact native Alerts/race/6m |
| cr02-openapi | 12:58:08–12:58:14 | 0 /6.152s | All11 OpenAPI/race/60s cases |
| cr02-contract | 12:58:08–12:58:09 | 0 /1.330s | Exact nonempty TestOpenAPISecurity60s |
| cr02-runtime | 12:58:08–12:58:09 | 0 /1.244s | Exact nonempty signed runtime dispatcher selector60s |
| cr02-browser | 12:58:35–12:58:45 | 0 /9.854s | Actual current API/CLI/helper/native PG+CH, Chromium1440/375 |
| cr02-source-scan | 12:59:44–12:59:45 | 0 /0.788s | Pinned8.30.1 staged repair and generated reports[] |
| cr02-history | 12:59:58–12:59:59 | 0 /0.942s | Atcc22566,567commits,report[]; exact exception unchanged |

Actual browser starts with filtered Alerts, verifies real failed401 followed by
successful login, navigates to Events, presses Back and checks the exact saved
Alerts URL, visible severity/status/search and real native API query, then presses
Forward. It continues through the retained detail flow, real hard reload with
same identity/generation4→2, canonical safe JSON, expanded IPv6, native focus/
Escape, missing404 and logout/privacy. No mocked browser routes. Results and
screenshots are in `cr02/`; generated schema hash remains unchanged.

The first combined web gate passed114 runtime cases but exited2 at typecheck
because the newly written component tests supplied four unsupported `exact`
options to Testing Library ByRoleOptions. Removing those options preserves
literal-name matching and every assertion; final web/type/lint/build/format
then passed. This owned fixture correction is recorded, not relabelled green.

Repair self-check: RED/GREEN commits and owned artifacts exist, whitespace is
clean, no tracked deletion or unexpected source/untracked artifact occurred.
Only the two declared source/test files changed; user dirt remains unstaged.
No stub, new security surface or authority change. Detector/readiness remains
a separate root-owned hold; no additional detector investigation, stack change,
whole-stack pass or acceptance claim occurred in this CR-02 repair. R2 and root
postwave subsequently passed as recorded above. Stack recovery still requires
operator direction; do not advance08 or Phase5. Historical failed scans,
infrastructure setup failures and review counterexamples remain preserved.
