# Plan 12 ownership amendment A2 — independent bounded recheck

**Result: PASS — 0 blockers, 0 warnings.**

Checked A2 at shared HEAD `df0e86b`, the preserved A1 rejection `ba0d114`,
A1's client amendment, and the actual App/Alerts/AlertDetails fixture dispatch
and request assertions. Since A1, only its report and the A2 proposal changed;
the previously inspected source and constructability evidence remain current.
This approves the revised ownership boundary only, not implementation, passing
tests, runtime proof or phase acceptance.

## Both A1 blockers are closed

A2 adds exactly `web/src/App.test.tsx` and
`web/src/components/AlertDetails.test.tsx` to the proposed ten-path union.
The resulting **12 paths comprise11 tracked files plus ignored generated
`web/src/api/schema.d.ts`**. No additional source path is required by this check.

The first addition permits endpoint-aware backfill handling before App's
existing sequential session/list response queues and exact observations.
Existing list503/retry, delayed responses, identity changes, token expiry,
current401 disposal, abort, post-body checks, cache keys/clearing and privacy
assertions must retain their original meaning. Count the intended requests
strictly by endpoint/method and verify added backfill separately; do not relax
exact assertions into loose bounds. No production App route change is needed.

The second addition permits the exact backfill path to return a valid delivery
page before the AlertDetails fixture records detail/triage requests or invokes
its test-specific detail responder. Its existing assertion that the first
recorded detail request is `/api/v1/alerts/${id}` remains valid under this
dispatch. Preserve all role, version, history, retention, focus, deep-link,
Escape, cancellation and privacy assertions. Never intercept all `/alerts/*`
paths or substitute for actual detail/PATCH calls.

The already owned `web/src/pages/Alerts.test.tsx` can make the corresponding
fixture change while retaining explicit query/cursor deep links and exact
list observations, including malformed-time no-list-request coverage. Its
new behavioral tests must separately exercise real typed catch-up, decimal
cursors above2^53, duplicate pages, processing before advancement, bounded
query invalidation and session cancellation. Empty compatibility pages alone
are not evidence for those behaviors. Keeping the actual App/session/client
in these tests preserves the relevant integration boundary.

A2 explicitly excludes speculative Events edits and requires another checked
amendment for any further demonstrated path. This matches A1's finding: the
Events navigation fixture may return404 for backfill, but its existing exact
list/detail assertions are not demonstrably broken by that additional call.
No blanket fixture, global setup, production session or navigation expansion
is approved.

## Retained constraints and next step

The twelve-file boundary remains sufficient for the focused delivery module,
main injection/managed projector lifecycle, additive atomic008 trigger/seed
cutover, ADR009, generated API contract and bounded Alerts catch-up described
in A1. No backend store, writer, lifecycle, migrator or httpx source addition
is necessary. Preserve the original two-task bound and native reverse-commit,
rollback/uncertain ACK/two-projector/overflow, tenant, CH-down and concurrent
cutover proof. Workflow seq remains distinct from the durable delivery cursor;
canonical alerts and Phase3 replay/reservations stay intact. Cursor IDs remain
decimal strings, and no unbounded list/deduplication set or disk-GC claim is
introduced.

Root must synchronize PLAN frontmatter, scope, task files/read-first/actions
with A1+A2 before source execution. The tests-only step owns compatibility
fixture adjustments before the new causal delivery tests; compilation or
fixture failures do not become behavioral RED. Actual008 migration via the
current host tern/current-source migrator on NEW PG25432/schema7 and later
current-source browser/API evidence remain required. The stale shared006
migration image and old held stack are outside this execution path. Original
runtime isolation, budgets, scanner policy and review requirements remain.

Only this report was written and committed. Prior A1 report, source, PLAN,
tracking, runtime and user-owned `.planning/config.json`/`.serena/` were
unchanged and unstaged. No application, build, tests or scanner ran.

```yaml
issues: []
additional_source_paths_needed: []
owned_path_count: 12
approved_added_paths:
  - web/src/App.test.tsx
  - web/src/components/AlertDetails.test.tsx
closed_findings:
  - "A1 BLOCKER 1: missing App fixture ownership"
  - "A1 BLOCKER 2: missing AlertDetails fixture ownership"
implementation_acceptance: false
```
