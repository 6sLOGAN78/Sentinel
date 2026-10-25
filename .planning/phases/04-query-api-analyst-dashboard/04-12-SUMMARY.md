---
phase: 04-query-api-analyst-dashboard
plan: "12"
subsystem: database
tags: [postgres, delivery, pgx, react, backfill]
requires:
  - phase: 04-11
    provides: Authenticated dashboard, authoritative workflows and managed query API
provides:
  - Transactional pending-to-delivery projection and bearer-only PostgreSQL backfill
  - Bounded browser catch-up using exact decimal cursors and explicit refresh
affects: [04-13, 04-14, 04-15, 04-16]
tech-stack:
  added: []
  patterns: [Counter-row serialized projection, committed-bound tenant replay, in-memory session cursors]
key-files:
  created:
    - docs/decisions/0009-gap-safe-alert-delivery.md
    - backend/migrations/postgres/008_alert_delivery.sql
    - backend/internal/alertstream/delivery.go
    - backend/testphase4/delivery_test.go
  modified:
    - backend/cmd/query-api/main.go
    - api/openapi.yaml
    - web/src/api/schema.d.ts
    - web/src/pages/Alerts.tsx
    - web/src/pages/Alerts.test.tsx
    - web/src/api/client.ts
    - web/src/App.test.tsx
    - web/src/components/AlertDetails.test.tsx
key-decisions:
  - Delivery IDs order projection commits rather than workflow sequence allocation.
  - Read the committed counter before the page and restrict rows to that captured bound.
  - Catch-up invalidates finite queries before advancing its private session cursor.
requirements-completed: []
requirements-targeted: [LIVE-02, LIVE-03, AUTH-03, UI-02, UI-06]
duration: 20min
completed: 2026-10-25
status: implemented-pending-review
---

# Phase 4 Plan 12: Gap-safe alert delivery Summary

**Gap-safe PostgreSQL replay now retries transient REST failures and preserves arrivals newer than a refresh snapshot; independent R1 review and root acceptance remain pending.**

## CR-01 / CR-02 repair — supersedes reviewed browser defects below

Preserved independent `04-REVIEW-12-R0.md` found two concrete blockers at
`f579f6d`: failed initial catch-up never restarted, and an older successful
refresh erased a newer processed arrival notice. Both have scoped fixes and
actual causal RED/GREEN evidence. This section records implementation proof;
only root/reviewer may close R0, accept the plan or update shared tracking.
Original review, A1 rejection, A2/A3 checks, failures and premature OpenAPI
claim correction remain unchanged below and in their reports.

Repair commits:

- `3de6a95` — tests-only `test(04-12): reproduce stranded catch-up and stale refresh notice races`
- `1e9725e` — `fix(04-12): retry catch-up and preserve arrivals newer than refresh snapshots`

The repair owns only `web/src/pages/Alerts.tsx`, its test file, this summary
and ignored private12 evidence. The complete accepted ownership boundary
remains13 source artifacts after root's A3, including the ignored generated
schema. No backend/contract/other fixture/tracking/review file was edited.
User config/.serena remain unstaged. Per root handoff, the user-authorized
GitHub push already published `6cef65d` with R0 documented; this repair made
no remote operation and does not claim to have published its new commits.

CR-01 now uses a single sequential catch-up driver. Failed network/5xx/429
requests or processing retry from the last successfully processed private
cursor after250ms, doubling to a5s cap. At most one request or wait is active
per mounted driver. Abort clears the wait timer and listener and prevents
later requests after navigation, logout or expiry. Current401 disposal is
left to the guarded transport and is never retried. Permanent other4xx
failures stop with honest reload/retry guidance. A successfully processed
page clears the reconnect state; invalidation still finishes before cursor
advancement. Successful terminal pages stop this initial REST catch-up;
future live/SSE behavior remains in its separate plan.

CR-02 tracks the latest processed arrival's exact decimal cursor in one ref.
Refresh captures that watermark before its list request. A successful response
clears only a still-matching watermark. If another delivery was processed
while that response was pending, the notice remains and the affected finite
query is re-invalidated without an automatic fetch. Thus an older snapshot
cannot consume the only refresh affordance for a later commit. No alert-ID
set, unbounded list, forced rewind, implicit filter change or direct UI fetch
was added. Existing filters, pagination, selection/history/focus and verified
generation/privacy behavior are retained.

### Repair evidence,2026-10-25 UTC

Commands use the existing private12 launcher and unchanged caps. Component,
ordinary and web/OpenAPI checks use its clean environment. Native/browser
checks use only NEWPG25432/schema8, CH28123/29000 and Redis26379 through the
approved07 context; shared18081 is not current-source proof.

| Check | Start–end | Elapsed | Actual result |
|---|---|---:|---|
| Three new actual App/session/client RED assertions |11:17:20–11:17:24|4.159s|1; initial503 stalls at1of2 calls, second-page503 stalls at2of3, stale refresh erases notice |
| Corrected focused Alerts/App/AlertDetails |11:21:57–11:22:02|5.904s|0;80/80 |
| Web typecheck/lint/format/build |11:21:57–11:22:09|11.517s|0 |
| OpenAPI exact nonempty family |11:19:31–11:19:32|1.276s|0;11/11 |
| Clean ordinary `make test` |11:23:20–11:23:30|9.995s|0; default Go, web151, generator121 |
| **Committed1e9725e focused components** |11:25:03–11:25:09|6.030s|0;80/80 |
| **Committed1e9725e real fault-proxy Chromium1440/375** |11:25:04–11:25:15|10.570s|0 |
| **Committed1e9725e native Delivery** |11:25:06–11:25:19|13.930s|0; root plus9subtests, package12.756s |

The unchanged original private R0 counterexamples were also run against
committed1e9725e with `./node_modules/.bin/vitest run --config
../.local/phase4-acceptance/04-12-review/private.config.ts`: both pass. This
is an executor rerun, not independent R1 review. Its existing private config
produces a future native-config-loader warning; the config was not edited or
the warning suppressed. Native command remains the exact nonempty Delivery
selector with phase4/race/10m/p1 documented below. Backend production files
are unchanged by this repair; earlier three-mode quality evidence is carried
by source identity, not represented as a fresh broad rerun.

The permanent component regressions retain the original three causal cases
and add guards for exact250/500/1000/2000/4000/5000/5000ms retry intervals,
cursor preservation, cancellation on sign-out and route unmount, permanent
400 reporting, and current401 session/cache disposal. Existing processing-
before-advance, duplicate page, decimal precision, session and list/detail
assertions remain. All seven new tests pass in the151-test ordinary web suite.

Committed browser results are under
`.local/phase4-acceptance/04-12/repair-browser-1792927504906997542/`.
The private loopback Node proxy returns controlled503 responses and then
forwards the real API. It holds the real tail-page request and captures a real
list response before a later workflow is inserted into a new owned PG database.
That new row is projected by the current API; the real tail response is then
processed before releasing the older list body. Both viewport runs retain the
notice after the older snapshot, and the next explicit keyboard refresh shows
the actual late row and clears the notice. Cursors stay exact above2^53;
visible rows stay50, navigation resumes, separate sessions start at0, and
storage/URL privacy/no overflow/pageerror checks pass. The mobile screenshot
was inspected. There are no browser route mocks and no shared dependency
outage. Synthetic workflow fixtures are native PG data, not a new detection
pipeline claim. All API/Vite/proxy/Chromium/native producers joined and the
newly owned databases were dropped after their children closed.

### Retained repair failures and limits

The first focused GREEN passed75/76: the strengthened final refresh assertion
clicked its still-disabled button before React Query's completion notification.
The test now waits for the actual enabled state before the next click, retaining
all notice/staleness and later-row assertions. Core CR-01/CR-02 assertions had
already passed. Additional timer guards initially froze `start()`'s wait with
non-advancing fake timers: four default5s test timeouts and the ordinary-suite
failure are retained as fixture setup failures. Advancing fake time preserves
the exact retry-delay assertions and original timeout. Typecheck separately
caught an invalid test-only `exact` role-query option; it was removed without
weakening the role/name query. No production workaround or timeout increase
was used. Final focused/ordinary/type gates pass after those scoped fixture
corrections; all failed records remain.

The first private browser fault fixture counted raw list requests, so a genuine
cancelled StrictMode initial read was mistaken for the refresh to hold. It timed
out waiting for initial data. The proxy now explicitly arms only the upcoming
user refresh after the initial list succeeds. The failed fixture and subsequent
worktree/committed passes remain distinct. StrictMode cancellation is preserved;
no production change was made for the harness.

Pinned8.30.1 staged RED and staged GREEN/generated/private-harness/full-history
scans pass with unchanged policy and existing exceptions. Final summary staging
and post-summary history scans run during close-out; actual private records
retain their exits. No new exception or copied whole-source scan was added.
Generated schema remains ignored and unchanged at SHA256
`8a26dcc2cc5a8d29ac9cea36963a540d1718d4dcc339f100f000d2d2cce6043a`.
No new stub or production trust boundary was introduced.

The separate NEW detector `checkpoint history lost` hold remains untouched;
there was no stack recovery, service stop/start, shared migration, seed resend,
checkpoint/offset/group/fence/retention reset or prune. No nine-healthy, fresh
pipeline or whole-phase acceptance claim is made. Final disk observation7.4GiB
remains above the2GiB floor. No active owned producer remains. Independent R1
review of the full13-path union and root postwave acceptance are next.

**Repair self-check: PASSED.** Both owned source files exist, RED3de6a95 precedes
GREEN1e9725e, and committed-source component/native/browser evidence returned0.
Source working changes are clean after GREEN, excluding the user's preexisting
config/.serena dirt. Root tracking/review/remote authority is preserved.

## Root A3 continuation — supersedes fixture blocker below

Independent A3 PASS0atab66e7a approved backend/test/openapi_test.go as thirteenth
path. Root synchronized PLAN inaaf64d3 before adding only GET backfill to its
strict expected route set. Exact equality and all other route/security/schema
assertions remain. Root isolatedOpenAPI nowEXIT0/all11; private
root-openapi-continuation records actualSHA/UTC/elapsed. Pinnedstaged/generated/
harness/history scansPASS. The original10/11failure and prematureclaimcorrection
below remain preserved; no retroactivepassingclaim. Fullsourceenvelope now12
tracked plus ignored generated schema=13approvedpaths. Source review/rootgate
remainpending. ActualPG-onlyfeaturepasses and separateNEWdetectorhistorylost
hold remain distinct; no fullpipeline/ninehealthy/Phase4completionclaim.

## Status and commits

Task 1 is complete. Task 2 production implementation is committed and its
native/browser feature checks pass, but Task 2 acceptance is **incomplete**.
The isolated OpenAPI gate returns 1: ten tests pass and `TestOpenAPIRoutes`
fails because its thirteen-route expected set lacks the new backfill route.
`backend/test/openapi_test.go` is outside the twelve approved paths and was
not edited. Root explicitly requires a separately checked A3 ownership
amendment before that strict fixture is extended. No whole-phase acceptance,
requirement completion, SDK tracking update, remote operation or Phase 5
transition is claimed.

- `e44f217` — `test(04-12): pin native delivery gaps and bounded browser catch-up`
- `921dbb2` — `feat(04-12): project gap-safe alert deliveries and recover bounded browser pages`

**Premature claim correction:** the executor told root that OpenAPI11 passed
before inspecting its completed evidence. That claim was incorrect. The
actual OpenAPI invocation at 10:54:34Z returned 1; its complete assertion and
exit remain in the private record. The error was explicitly corrected to root
before this summary. Passing ordinary `make test` does not replace this
separately invoked OpenAPI gate.

## Scope and implementation

The exact union is eleven tracked files plus ignored generated
`web/src/api/schema.d.ts`, as approved by A1/A2 and synchronized PLAN at
`c74f703`. This summary and private ignored `.local/phase4-acceptance/04-12/`
are permitted execution artifacts. User `.planning/config.json` and `.serena/`
remain untouched and unstaged. Root owns STATE/ROADMAP/REQUIREMENTS/VALIDATION
and all review reports; no tracking handler ran.

Migration008 locks workflow writes before installing an AFTER INSERT pending
trigger and seeding preexisting identities in one migration transaction.
The ordinary singleton bigint counter is locked before selecting at most100
pending rows. Unique delivery inserts, counter advancement and pending removal
commit atomically under a five-second transaction deadline, one-second lock
timeout and independent one-second rollback context. Multi-replica projectors
serialize through this row. Idle polling is1s; failures back off100ms–5s.
Overflow drains readiness and fails the managed process. Main owns projector
cancellation/join before pool closure. The focused delivery module contains
projection, bounded reads and literal bearer route registration; no hidden
source helper/path was added.

`GET /api/v1/alerts/backfill` precedes the alert-ID route, accepts only canonical
decimal `after_seq` in0..MaxInt64 and finite limit50/default,200/max, rejects
future/unknown/duplicate/malformed fields, and derives tenant from verified
bearer identity. Reads capture the committed global counter first and bound
the subsequent page by it. Full pages advance to their last item; exhausted
tenant intervals can advance to that captured global bound. Only the fifteen
real PostgreSQL workflow fields are returned. CH is not involved.

The typed generated client retains the existing generation/lifetime/caller
abort/post-body/current401 guards. It validates decimal progress with BigInt,
never Number. Alerts holds cursors in a private WeakMap keyed by the guarded
session client, validates tenant envelopes, awaits bounded query invalidation
before advancing, and offers explicit refresh without unbounded append or an
identity set. Navigation resumes that session cursor; separate sessions start
at zero. App/Alerts/detail fixtures dispatch only the exact backfill endpoint
separately while retaining original list/detail/session/cache/focus/history/
version/privacy assertions. StrictMode has an explicitly observed cancelled
initial replay; production behavior was not special-cased for tests.

ADR009 records global projection serialization, latency/backlog and durable
ledger growth. It promises neither original workflow commit ordering, physical
exactly-once delivery, browser disk persistence nor a fixed disk/GC bound.

## Actual evidence

Private records retain exact argv/cwd, SHA, working changes, UTC start/end,
monotonic duration, exit and disk observations. Directories0700/files0600;
children umask022 and owned binaries0700. No token/header, resolved Compose
configuration or credential was printed. Evidence window starts10:44:34Z;
source work began after the authorized10:42Z resume.

All native commands below run from backend using only the approved NEW07
private child context. The clean ordinary/static commands remove inherited
Sentinel/PG runtime overrides. Browser proof uses an owned current-source API
and Vite proxy, never the stale shared18081 image.

| Check,2026-10-25 UTC | Start–end | Elapsed | Actual result |
|---|---|---:|---|
| Native causal RED, exact Delivery selector |10:50:10–10:50:16|5.605s|1; actual API required200/got400 on backfill, with successful build/PG setup |
| Component causal RED, four new Alerts cases |10:49:11–10:49:17|6.005s|1; actual App issued no catch-up requests; four failures |
| Actual host tern008 on NEWPG25432 |10:53:20–10:53:20|0.334s|0; schema7→8 |
| Final worktree native Delivery |10:57:43–10:58:02|18.791s|0; nine subtests, package16.411s |
| Affected native Auth/Alerts/Events/Triage/SummaryRules |10:54:33–10:55:13|39.971s|0 |
| Isolated OpenAPI11 |10:54:34–10:54:35|1.130s|**1;10pass, exact-route fixture fails** |
| Default/phase4/integration vet, build, lint |10:57:42–10:57:59|17.535s|0; all nine constituent commands |
| Web typecheck/lint/format/build |10:56:18–10:56:33|15.255s|0 |
| Clean ordinary final-source `make test` |10:59:28–10:59:38|9.672s|0; default Go, web144, generator121 |
| Worktree current-source browser1440/375 |11:00:12–11:00:19|7.377s|0 |
| **Committed921dbb2 native Delivery** |11:01:19–11:01:36|16.350s|0; nine subtests, package15.167s |
| **Committed921dbb2 current-source browser1440/375** |11:01:21–11:01:29|8.646s|0 |

Native command: `bash ../scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Delivery$' -tags=phase4 -race -timeout=10m -p=1`.
Affected command uses selector `^TestPhase4(Auth|Alerts|Events|Triage|SummaryRules)$`
with phase4/race/6m/p1. OpenAPI uses `bash ../scripts/go-test-nonempty.sh
'./test/...' '^TestOpenAPI' -timeout=60s -p=1` without integration tag.
No empty selection, changed deadline, full Phase3 aggregate or hosted CI claim.

Native proof includes a real held earlier workflow transaction, later committed
workflow and actual max-workflow-seq query that misses the earlier alert.
Delivery then recovers that alert after the later delivery cursor. Tests also
cover insert-only writer replay preserving status/version/seq/audit, native
tenant/bounds/strict inputs, default50 and max200 pagination, CH-unreachable
success, constraint-induced projector rollback/retry, two actual projectors,
and native preexisting/open-writer/cross-trigger migration cutover. A transparent
PG wire proxy drops the first successful server COMMIT response: pgx sees EOF,
retry recovers and exactly two unique deliveries remain after a second insert.
Counter overflow causes the actual managed query process to exit nonzero.
Overflow/large-counter/cutover/fault fixtures use newly created owned databases,
joined processes and cleanup; shared NEW delivery history/counter is never
rewritten to simulate failures.

Browser proof at committed921dbb2 is in
`browser-1792926081136145223/results.json`. A new owned native PG database has203
workflow fixtures and synthetic counter9007199254740992; actual API responses
contain200+3items and exact decimal cursors9007199254741192/9007199254741195.
Two actual repeated HTTP pages are byte-equivalent JSON with stable identities.
Visible rows stay50; explicit keyboard refresh, navigation resume, separate
session zero cursor, foreign empty list, storage/URL privacy, no horizontal
overflow/overlay/pageerror pass. The mobile populated screenshot was visually
inspected. These are synthetic native workflow fixtures, not a new detection
pipeline or proof that CH contains those detections. Component tests separately
prove awaiting processing before advancement and cancellation/duplicate-page
rejection; browser observers do not claim to instrument React Query internals.

## Failures, deviations and limits

1. Initial component attempt had a fixture syntax error and executed zero tests;
   it is retained as setup failure, never causal RED. Corrected four-case RED
   and tests-only commit precede all production implementation.
2. First focused GREEN passed72/73; the new detail fixture wrongly expected one
   backfill during existing StrictMode replay. It now asserts exactly two under
   StrictMode and that the first was aborted, exactly one otherwise. Existing
   detail/history/focus assertions remain.
3. Initial scoped lint/format checks failed. Three consolidated source fix
   batches corrected the fixture count, formatting/context/error naming and
   focused main/proxy helper extraction. Final three-mode and web gates pass.
   No further source fix was attempted after the cap or ownership discovery.
4. First private browser assertion expected only one initial cursor0 request.
   Actual development StrictMode emits a cancelled0 then successful0, then
   the exact next cursor. The private observer records this exact cancellation
   separately. Subsequent real runs pass; the failed record remains.
5. **Blocking ownership deviation:** the approved new route causes the strict
   existing OpenAPI route set to fail. No forbidden thirteenth path was edited.
   Root must arrange checked ownership, preserve every prior route assertion
   and rerun the complete OpenAPI gate. Implementation acceptance remains open.
6. TDD scope: all native cases existed before implementation, but the RED
   family intentionally stops before schema-dependent assertions when the
   actual route is absent. Their individual fault branches were subsequently
   exercised GREEN, not independently claimed as observed preimplementation
   failures. Native counterexample and missing-route RED and four component
   REDs are actual observations. RED/GREEN commits exist in that order.

No unresolved implementation stub was found in the owned source scan. The new
network endpoint and migration are within the plan threat model; no additional
undeclared trust boundary was introduced. Authentication gates did not occur.

## Separate NEW runtime hold

Initial read-only ps showed NEW infra stopped and apps restarting. Root
authorized normal preserved recovery only: validated existing NEW IDs, explicit
infra start, current host tern at schema7 and direct exact owned app-ID starts,
avoiding the stale006 migration container. The recovery10:45:31–10:47:49
returned1 after its120s readiness poll (138.466s total including startup).
Eight services became healthy; NEW detector repeatedly fails closed with
`detector checkpoint history lost`. Its private diagnostic is retained.

Root explicitly authorized PG-only delivery/currentAPI/browser continuation
while retaining this separate runtime hold. No reset, checkpoint/group/offset
change, prune, seed resend, original-stack command or detector guard change
occurred. Existing IDs/data and stale migrator status were preserved. No nine
healthy or full fresh pipeline claim is made. Root must address runtime
authority before the final whole-phase gate. Final free disk7.8GiB is above the
2GiB floor. No owned ephemeral API, Vite, browser or native test producer remains;
the preserved shared detector restart loop is not a joined acceptance producer.

## Scans and continuation

Pinned gitleaks8.30.1 with unchanged policy/exact existing exceptions passed
tests-only staged, GREEN staged, generated schema, owned private harness and
full Git history scans. No new exception, broad source-directory copied scan,
credential disclosure or scanner version change occurred. Generated schema
SHA256 is `8a26dcc2cc5a8d29ac9cea36963a540d1718d4dcc339f100f000d2d2cce6043a`;
it remains ignored and was not force-added. Final summary staging and
post-summary full-history scans are run during this close-out; exact private
records retain their actual outcomes.

Root continuation: checked A3 for the one strict route fixture, narrow fix and
complete OpenAPI11 rerun; then independent whole-union source review and root
postwave acceptance. Preserve this incomplete report and all failures as
provenance. Shared tracking remains root-owned. SSE/notifications are subsequent
owned plans; no Phase5/publication authority is inferred.

## Self-Check: PASSED

All twelve approved artifacts exist, including ignored generated schema. Both
RED e44f217 and GREEN921dbb2 commits exist in order. The source union is exactly
the approved eleven tracked paths; task-related tracked source is clean after
GREEN. Committed-HEAD native/browser evidence and private failures exist.
Self-check validates this truthful incomplete handoff; it does not waive the
OpenAPI route fixture blocker or certify plan/phase completion.
