# Plan 12 ownership amendment A1 — independent bounded check

**Result: ISSUES FOUND — 2 BLOCKERS, 0 warnings.**

Reviewed the proposed ten-path boundary at shared HEAD `50510e7`, with Plan11
accepted at `14815f3` and repaired source `ef19971`. This is an ownership and
constructability check before source editing, not implementation acceptance.
Only this report is owned by the checker.

## Necessary client addition

Adding `web/src/api/client.ts` is causally necessary. Its `openapi-fetch`
transport is private to `createAPIClient`; the returned API has no backfill
method. An Alerts fetch outside that factory would bypass the approved guarded
transport. A generated-path-derived GET method fits this file and must retain
generation checks before credential access, lifetime/caller cancellation,
post-body checks and current-session401 disposal. Cursor parameters and
responses remain decimal strings throughout, including values above2^53.

The proposed union of ten files is insufficient to preserve two existing
component fixtures once authenticated Alerts mounts and starts catch-up.

## BLOCKER 1 — App fixture ownership is missing

`web/src/App.test.tsx` establishes `/alerts` before each test. Its successful
sign-ins use sequential response queues for session then alert-list requests,
without a response for the new backfill request. For example, the explicit
list503/retry test at lines528–548 queues identity, list503 and recovered list,
then asserts exactly two requests before its explicit retry. Initial backfill
necessarily adds another request or consumes that recovered-list response;
the latter is the wrong delivery envelope. The session-switch tests also use
absolute request indices and counts. These fixtures cannot remain valid by
changing only `client.ts`, Alerts or the already owned Alerts test file.

Minimal additional path: **`web/src/App.test.tsx`**. Dispatch backfill separately
to a valid empty terminal page (`items: [], next_seq: "0", has_more: false`)
for these existing list/session regressions. Preserve the original list
responses, delays, strict list/session counts, filter assertions, cancellation,
cache-clearing and privacy assertions. Observe the same intended requests by
exact endpoint/method rather than letting a new request consume a positional
fixture; assert the added backfill requests separately. Do not replace exact
counts with loose lower bounds or remove the expiry/401/session-switch cases.

## BLOCKER 2 — AlertDetails fixture ownership is missing

`web/src/components/AlertDetails.test.tsx:start()` at lines86–105 handles only
session and the exact list path specially. Every other request is recorded in
the detail/triage `requests` array and passed to the test-specific `read`
callback. Initial `/api/v1/alerts/backfill` therefore reaches detail handlers
and receives a detail/PATCH fixture instead of a delivery page. The first
detail test at lines161–174 explicitly asserts that `requests[0]` is
`/api/v1/alerts/${id}` with no query. Backfill starts on Alerts mount before
that test opens the dialog, contradicting this assertion independently of
native implementation correctness. Other callbacks also maintain detail-read
state and must not be invoked by catch-up traffic.

Minimal additional path: **`web/src/components/AlertDetails.test.tsx`**. Route
the exact backfill path to a valid empty delivery page before recording or
calling the detail responder. Preserve every existing detail/triage request,
count, deep-link, focus, role, retention, cancellation and privacy assertion.
Do not route all `/alerts/*` traffic to the empty page or suppress real detail
requests. No production AlertDetails component change is required.

## Existing owned fixture and bounded exclusions

`web/src/pages/Alerts.test.tsx` is already owned. Its `start()` currently sends
every non-session request to the list responder and records it as a list read.
It needs the same exact-path backfill dispatch and independent observation so
existing list counts, normalized keys and cursor/filter deep links retain
their meaning. Its malformed-time direct-render case currently expects only
the session request; preserve the no-list-request assertion while accounting
explicitly for any independent catch-up request. New tests in this owned file
must exercise actual typed backfill, decimal precision, process-before-advance,
bounded invalidation, duplicate pages and session cancellation rather than
only furnishing empty responses.

The actual Events fixture was inspected because it mounts Alerts in the
Back/Forward cases. At `web/src/pages/Events.test.tsx:682`, its generic
`/alerts/*` branch returns404 for backfill. Its existing assertions, however,
select exact list/detail paths; the extra call does not itself contradict an
exact count or consume a sequential response. This is an existing mocked
backfill failure during navigation, not native delivery proof. No mandatory
Events ownership addition is established by this static check. If the chosen
catch-up error/retry UI makes that fixture fail, declare that causal amendment
before editing it; do not weaken navigation assertions or introduce a test-only
production exception. Ordinary Events routes and direct client/session tests
do not mount Alerts and do not acquire this fetch merely because the client
factory gains a method. Brief inspection of Rules/Overview navigation fixtures
likewise establishes no mandatory additional path. No blanket fixture, global
test setup, App production route or session-provider ownership is justified.

## Constructability of the retained implementation boundary

The backend remains constructable in the declared files. New `delivery.go`
can own PG projection, bounded tenant-log reads and a focused authenticated
REST handler importing the existing `query.WorkflowAlert`; main can inject
the PG pool/verifier and register the literal backfill route. Existing query
handler/store files need no new dependency. Main already has `app.Lifecycle`
with `Fail` and LIFO closers, so it can own projector cancellation/join before
PG closure and terminal overflow failure without editing app/httpx/opshttp.
No streaming timeout change is required for this REST-only increment.

Migration008 is automatically embedded by `postgres/*.sql`; no embed/migrator
source addition is needed. Its trigger installation and preexisting seed must
share the atomic migration transaction and a workflow-write-conflicting lock
that waits for earlier writers. A migration advisory lock alone does not
exclude ordinary workflow inserts. Native cutover tests in the owned delivery
test file must distinguish preexisting rows, an already-open writer and a
writer crossing trigger installation; commit-order projection counter locking
is a separate invariant. Preserve workflow seq/status/version/audit and the
Phase3 permanent reservations and insert-only replay behavior.

Keep the original bounded100 pending batch,5s transaction/1s lock timeout,
rollback/idempotent retry, uncertain ACK, two-projector and overflow assertions,
tenant filtering, finite50/max200 pages, future-cursor400, native reverse-commit
counterexample and CH-down backfill. Initial browser processing must complete
before cursor advancement and invalidate bounded queries without retaining an
unbounded alert-ID set/list. No workflow-seq substitute, identity-sequence
counter, cross-network call under the counter lock or fixed disk/GC claim.

The new delivery test file can own native cutover fixtures without modifying
older native test helpers or writer sources. Actual008 must later execute via
current host tern/current-source migrator in the approved NEW PG25432/schema7
context. Do not rerun the stale shared006 migration image. Current-source
ephemeral API/browser proof uses NEW CH28123/29000 and preserves the old held
stack. This review ran no runtime, application, build, tests or scanners and
asserts none of that proof has passed.

## Revision required before execution

Return to root for a separate amendment adding the two demonstrated paths.
The resulting minimal union is **12 paths:11 tracked plus ignored generated
schema**, still two bounded tasks for this one delivery increment. Synchronize
PLAN frontmatter, scope, task files/read-first/actions with client and fixture
ownership before source edits. This report does not itself authorize edits to
those undeclared paths, approve implementation or change acceptance budgets.
Only this report was written/staged/committed; user-owned `.planning/config.json`
and `.serena/`, all source/PLAN/tracking/runtime/scanner policy remain unchanged.
No root AGENTS.md or project skill directories were present.

```yaml
issues:
  - plan: "04-12"
    dimension: key_links_planned
    severity: blocker
    description: "Initial Alerts backfill invalidates App.test.tsx sequential fetch fixtures and exact request observations, but this path is not owned."
    fix_hint: "Add web/src/App.test.tsx ownership; dispatch exact backfill separately and retain all list/session/cache/privacy assertions."
  - plan: "04-12"
    dimension: key_links_planned
    severity: blocker
    description: "AlertDetails.test.tsx routes initial backfill to detail responders and asserts its first recorded request is the detail path, but this fixture path is not owned."
    fix_hint: "Add web/src/components/AlertDetails.test.tsx ownership; handle exact backfill before detail observation without changing existing detail/triage assertions."
additional_source_paths_needed:
  - web/src/App.test.tsx
  - web/src/components/AlertDetails.test.tsx
proposed_owned_path_count: 10
minimal_revised_owned_path_count: 12
implementation_acceptance: false
```
