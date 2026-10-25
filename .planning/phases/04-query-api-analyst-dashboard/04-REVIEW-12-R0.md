---
phase: 04-query-api-analyst-dashboard
plan: "12"
reviewed: 2026-10-25T11:11:25Z
depth: standard
review_iteration: R0
reviewed_head: f579f6d548a3c26d04625396c5ad301f9e0ad34b
source_commit: 921dbb2cca621b342599b6ee8cab785e1486538f
strict_route_fixture_commit: bab2fbdbca65032700b2e023a4988c1be8288d46
tests_red_commit: e44f21731e5f8ed3c30dcb577559ebef598258d9
diff_base: c74f703d1683a90a1802a30b50ab8fcbe9800254
files_reviewed: 13
files_reviewed_list:
  - docs/decisions/0009-gap-safe-alert-delivery.md
  - backend/migrations/postgres/008_alert_delivery.sql
  - backend/internal/alertstream/delivery.go
  - backend/testphase4/delivery_test.go
  - backend/cmd/query-api/main.go
  - api/openapi.yaml
  - web/src/api/schema.d.ts
  - web/src/pages/Alerts.tsx
  - web/src/pages/Alerts.test.tsx
  - web/src/api/client.ts
  - web/src/App.test.tsx
  - web/src/components/AlertDetails.test.tsx
  - backend/test/openapi_test.go
findings:
  critical: 2
  warning: 0
  info: 0
  total: 2
status: issues_found
runtime_verification: focused_native_passed_browser_component_counterexamples_failed
---

# Phase 4 Plan 12: Code Review Report — R0

**Reviewed:** 2026-10-25T11:11:25Z
**Depth:** standard
**Files reviewed:** 13
**Status:** issues_found — 2 BLOCKERS, 0 warnings

## Summary

Two browser correctness defects remain in the approved delivery increment:
a transient backfill failure permanently stops the mounted catch-up, and a
refresh can erase a newer arrival notification after its delivery cursor has
already advanced. Both were independently reproduced against the actual
App, session provider, generated transport and Alerts component without
changing submitted source.

The independent native Delivery family passes all nine subtests. The strict
OpenAPI family now passes all eleven tests, including the exact fourteen-route
set. Those passing checks do not cover or negate the two component failures.

Scope is the complete thirteen-path union from base `c74f703`, not merely
the one-line A3 fixture continuation. Twelve paths are tracked; explicitly
authorized generated `web/src/api/schema.d.ts` is ignored and was read without
force-adding it. Its SHA256 is
`8a26dcc2cc5a8d29ac9cea36963a540d1718d4dcc339f100f000d2d2cce6043a`.
The only reviewed tracked source difference after GREEN `921dbb2` is the
authorized strict expected-route addition in `bab2fbd`.

Context read: reviewer instructions and GSD code-review skill; 12 PLAN,
A1/rejected CHECK, A2/CHECK, A3/CHECK; CONTRACT, RESEARCH, UI-SPEC; ADR0008/0009;
12 SUMMARY including its superseding root A3 continuation; 11 SUMMARY and
REVIEW-R1. Historical OpenAPI failure and premature passing-claim correction
remain historical evidence, not current blockers. No root AGENTS.md or project
skills directories were present. No structural pre-pass was supplied; fallow
remains disabled.

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: Transient backfill failure stops recovery for the mounted session

**Classification:** BLOCKER
**File:** `/home/logan78/Desktop/Sentinel/web/src/pages/Alerts.tsx:217-223`
**Related:** `web/src/pages/Alerts.tsx:318`

**Issue:** Any network error, dependency503, malformed response or failed
processing escapes the page loop. The catch block only sets `catchupError`,
and `catchUp()` then returns. There is no retry timer, retry action, online
listener or state dependency that restarts it. Its dependencies are the stable
query client, guarded client and verified session; ordinary rerenders, filter
changes and list retries do not restart this effect. The screen indefinitely
says “Reconnecting… Missed alerts will be recovered.” while issuing no further
backfill request. A failure on page two likewise strands all remaining pages.
A route remount or a new login happens to retry, but neither is offered as the
recovery action.

**Evidence:** Private `catchup.test.tsx:28-41` mounts actual App, returns one
backfill503 and configures subsequent requests to succeed with a delivery.
It observes the reconnect message, applies filters and waits six seconds.
The assertion fails with `expected 1 to be greater than 1`: the server would
recover, but the mounted client never asks again. This is independent of SSE
or later notification work; the initial REST catch-up itself is stranded.
UI-SPEC requires independent error/retry states and dependency503 recovery.

**Fix:** Keep catch-up retryable from its last processed private cursor.
Add an abort-aware bounded backoff or an explicit retry control wired to this
same catch-up operation; retain the cursor on failed processing, reset the
error on successful recovery, and cancel retry work on navigation/logout/
expiry. Do not retry current401 after session disposal or turn malformed-page
rejection into an immediate unbounded loop. Add a transient503-then-success
component regression, including failure after a processed first page.

### CR-02: Refresh completion clears notifications for later deliveries

**Classification:** BLOCKER
**File:** `/home/logan78/Desktop/Sentinel/web/src/pages/Alerts.tsx:326-327`
**Related:** `web/src/pages/Alerts.tsx:204-215`

**Issue:** A successful list refresh unconditionally sets the shared
`arrivals` boolean false. Catch-up can process another delivery while that
refresh is in flight, invalidate the query without refetching, set arrivals
true and advance its durable session cursor. If the older list request then
finishes successfully, its callback clears the newer notification as well.
That response can reflect a PostgreSQL snapshot from before the later
workflow committed, so the row is absent. The arrival notice and its only
Refresh button disappear, and the processed cursor prevents replaying that
delivery in the current catch-up. Refetch completion also clears React Query's
invalidation flag; invalidating before advancing does not prevent this race.

**Evidence:** Private `catchup.test.tsx:43-70` processes delivery1 and starts
Refresh, keeping the list response pending. It then completes delivery2 with a
different alert ID and finishes the earlier list response containing only the
old row. The actual component has neither “Later commit” nor “New alerts
available”; the final notification assertion fails with
`expected null not to be null`. This is a legal READ COMMITTED/network
ordering: list selection, later workflow/projection commit, then delayed list
body completion.

**Fix:** Track an arrival revision or last processed delivery cursor rather
than clearing an undifferentiated boolean. Capture that revision before
starting a refresh and acknowledge only arrivals already covered at that
point when it succeeds. A delivery processed while the refresh is pending
must retain the notice/button and stale state for another explicit refresh.
Preserve filters, pagination and bounded rows. Add the interleaved
refresh/new-delivery regression; a simple refresh-after-all-pages test cannot
exercise it.

## Scope analysis and supporting traces

All thirteen files were read, with changed hunks compared to the declared base.
Additional unchanged code was read only to trace the existing transaction,
tenant, session and lifecycle boundaries.

- Migration008 takes a workflow-write-conflicting table lock before creating
  the AFTER INSERT trigger and seeding preexisting workflows. The current
  tern migrator executes migration statements transactionally. Existing
  insert-only workflow Ensure uses ON CONFLICT DO NOTHING, preserving seq,
  status/version/audit and the earlier permanent-reservation writer boundary.
- Project locks the singleton row before selecting at most100 pending rows.
  New delivery inserts, counter update and selected pending deletion share
  one READ COMMITTED transaction and its commit. Five-second operation/server
  bounds, one-second lock acquisition and independent one-second rollback
  are explicit. It makes no CH/external-network call under that lock.
  Managed main cancels and joins the projector before closing PG; overflow
  begins draining and fails the lifecycle.
- Backfill captures the committed bound first and restricts the tenant page
  to it. Full pages advance to their last row; exhausted sparse tenant
  intervals advance to that captured global bound. Limits, strict decimal
  inputs, future cursors, JWT middleware and verified tenant predicate were
  traced. Returned alert data contains the fifteen real workflow fields.
- Client backfill retains generation/lifetime/caller-abort and post-body
  checks, exact decimal BigInt comparisons, ascending-progress validation and
  tenant validation in Alerts. Cursor storage is private per guarded client.
  Query invalidation precedes advancement, but the two findings above still
  break recovery/visibility.
- Existing App/Alerts/detail fixtures dispatch only the exact new endpoint
  separately, preserving list/detail/session/history/focus/privacy assertions.
  OpenAPI's only fixture change adds the backfill GET to exact equality; it
  does not weaken route or bearer-security checks.

No additional concrete backend BLOCKER or WARNING was established. This is
not a claim that passing tests prove the entire implementation correct, nor
a performance audit. The ADR explicitly admits persistent ledger growth and
does not promise a fixed disk bound, GC or original-workflow commit ordering.

## Independent verification and counterexamples

All checks ran at reviewed HEAD `f579f6d`, with only preexisting user config
and .serena dirt. Native execution used only the approved NEW07 context and
new isolated databases owned by its native fixtures. Ordinary component and
OpenAPI checks used the launcher clean environment without NEW PG overrides.

| Check | UTC interval on 2026-10-25 | Elapsed | Result |
| --- | --- | ---: | --- |
| Exact nonempty native Delivery, phase4/race/10m/p1 | 11:08:02.702667–11:08:17.447289 | 14.744662s | Exit0; root plus nine named subtests |
| Private mounted catch-up and refresh-interleaving counterexamples | 11:09:54.208452–11:10:01.877827 | 7.669390s | Exit1; two behavioral assertion failures |
| Existing affected Alerts/App/AlertDetails components | 11:10:43.002443–11:10:47.556016 | 4.553589s | Exit0;73/73 |
| Nonempty OpenAPI family | 11:10:43.024163–11:10:43.833848 | 0.809689s | Exit0;11/11 |
| `git diff --check c74f703..HEAD` | Review interval | — | Exit0 |

Native invocation:

```sh
python .local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/context.py \
  --cwd backend --timeout 630 -- \
  bash ../scripts/go-test-nonempty.sh './testphase4/...' \
  '^TestPhase4Delivery$' -tags=phase4 -race -timeout=10m -p=1
```

Native output covers the real workflow-max counterexample, reversed commits,
CH-unreachable backfill, strict inputs/tenant/pages, rollback/two replicas,
large decimals, concurrent migration cutover, dropped successful COMMIT
acknowledgement and terminal managed overflow. These tests do not mutate the
shared delivery counter: high-counter/cutover/fault cases use owned databases.

The private regression command is:

```sh
python .local/phase4-acceptance/04-12/run.py --clean --cwd web --timeout 30 \
  independent-review-counterexamples-final -- npx vitest run \
  --config ../.local/phase4-acceptance/04-12-review/private.config.ts
```

Its configuration imports the existing web configuration and tests actual App,
not a model of the cursor algorithm. Only the HTTP dependency is controlled.
An initial private run also failed both tests, but its first test hit Vitest's
default5s timeout before its6s wait assertion. That harness timeout was
corrected to8s; the final run above fails the actual request-count assertion.
The second run's refresh test fails its direct notice assertion. The initial
timeout is retained as harness setup evidence, not causal behavioral RED.

Private evidence index/harness are under
`.local/phase4-acceptance/04-12-review/` (directory0700/files0600).
Launcher records retain exact argv, cwd, SHA, UTC, duration, exit and disk:
native `20261025T110802702646Z-operator-6b2617d9.json` in the NEW07 context;
component/contract/counterexample records in the existing private04-12 directory.
Child processes use umask022. Lowest recorded completion disk was
8,146,018,304 bytes, above the2GiB floor. No credentials, authorization headers,
resolved Compose configuration or raw sensitive service logs were printed.

## Limits and preservation

The NEW detector checkpoint-history-loss hold remains separate and unchanged.
The parent authorized this PG-only review while eight services were healthy;
this report asserts neither nine healthy services nor a fresh detection
pipeline. Shared18081/18080 old07 images were not current-source evidence.
No runtime recovery, shared migration/seed, reset/offset/group/fence/retention
change, service stop/start/rebuild, prune, scanner/policy or remote action ran.

The source RED commit precedes GREEN as recorded in the summary. Earlier
native/browser/migration/scanner/full ordinary-suite evidence remains earlier
executor/root evidence, not an independent rerun here. No real-browser layout,
full Phase3 aggregate, full pipeline, whole-phase acceptance or new security
certification is claimed. Later SSE/LISTEN work is outside this increment and
does not resolve the current REST catch-up defects by assumption.

Only the designated two review reports and ignored private review artifacts
were written. There is no source repair or commit. User .planning/config.json,
.serena, shared tracking and historical reports were preserved. Root owns the
review commit and the decision to route these findings through a scoped fix.

---

_Reviewed: 2026-10-25T11:11:25Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
