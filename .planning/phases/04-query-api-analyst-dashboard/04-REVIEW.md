---
phase: 04-query-api-analyst-dashboard
plan: "12"
reviewed: 2026-10-25T11:29:37Z
depth: standard
review_iteration: R1
reviewed_head: 7aa711e020478ec59dc8757050d055cba4614b63
source_commit: 1e9725e2c526fbc12f841ba73eb73bdf51a26c51
tests_red_commit: 3de6a95704f16d825e0eabd8f97f2e0f065d3529
original_source_commit: 921dbb2cca621b342599b6ee8cab785e1486538f
original_tests_red_commit: e44f21731e5f8ed3c30dcb577559ebef598258d9
strict_route_fixture_commit: bab2fbdbca65032700b2e023a4988c1be8288d46
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
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
closed_findings: [CR-01, CR-02]
runtime_verification: focused_native_and_components_passed
---

# Phase 4 Plan 12: Code Review Report — R1

**Reviewed:** 2026-10-25T11:29:37Z

**Depth:** standard

**Files reviewed:** 13

**Status:** clean — 0 BLOCKERS, 0 warnings

## Summary

The scoped Alerts repair closes CR-01 and CR-02. Independent re-review found
no remaining concrete BLOCKER or WARNING in the full approved thirteen-path
increment. All reviewed files meet quality standards. No issues found.

R1 directly inspected both repaired files and verified source identity for
the other eleven paths against the complete R0 review. The original two
private counterexamples now pass unchanged. Independent affected component
and native Delivery checks also pass at reviewed HEAD `7aa711e`, whose source
matches repair GREEN `1e9725e`.

The original report remains byte-identical to committed R0 in `6cef65d`:
SHA256 `70bfe8e483be736ceedbaa606dd927bfc72a5482a55e530d576f005fc868f43f`.
It retains both original BLOCKERS, their failing evidence and historical
limits. This report does not rewrite that provenance.

## Narrative Findings (AI reviewer)

No remaining findings.

### Closed CR-01 — prior BLOCKER: mounted catch-up stopped after transient failure

At `web/src/pages/Alerts.tsx:208-260`, one sequential driver now retries the
same last processed private cursor after transient failures. Page processing,
tenant checks, awaited invalidation and cancellation checks still precede
cursor advancement. Failures cannot advance it. A processed page clears the
reconnect state and resets backoff; terminal successful pages end this initial
REST catch-up normally.

The driver waits250ms initially and doubles to a5000ms cap. The helper at
lines14-24 clears its timer and removes its abort listener both when the wait
finishes and when the driver is aborted. There is one request or wait per
mounted driver; no recursive concurrent retries or persistent alert-ID set is
introduced. The loop checks abort before its next request, and cleanup aborts
both current I/O and a pending wait.

Current401/session/caller cancellation exits instead of retrying. Other
non4294xx errors stop with explicit failure/reload guidance at lines359-362,
rather than a false reconnect promise. Network/5xx/429 and processing failures
retain progress and retry with the bound above. The existing guarded transport
continues to revoke current401 and enforce generation/lifetime/post-body
checks; no auth bypass or direct UI fetch was added.

The permanent regressions at `web/src/pages/Alerts.test.tsx:100-148`
independently cover failure before the first page and failure after a processed
page above2^53. The observed requests are respectively [0,0] and
[0,9007199254740993,9007199254740993]. Recovery stays in the mounted session,
clears the reconnect text, preserves visible list bounds and does not store a
cursor on disk. Lines218-293 cover exact backoff intervals, unchanged failed
cursor, route/signout cancellation, permanent400 and current401 cache/session
disposal. Existing processing-before-advance and stale-body/session tests are
retained and passed.

### Closed CR-02 — prior BLOCKER: an older refresh erased a newer arrival notice

At `web/src/pages/Alerts.tsx:228`, each processed nonempty delivery page
updates one exact decimal arrival watermark. At lines370-383, each refresh
captures that watermark before starting its list request. Success clears the
notice only if the latest processed watermark still matches. A delivery
processed while that response is pending leaves the notice visible and
re-invalidates only the captured finite query without an automatic fetch.

This handles the original ordering: old list snapshot, later workflow/delivery,
then old body completion. The older response no longer consumes the refresh
affordance for the later delivery, and re-invalidation restores the stale flag
that React Query clears on successful fetch completion. Each completion uses
its own captured watermark/key. A later watermark cannot be acknowledged by
an earlier completion, while another explicit refresh after processing that
watermark can acknowledge it. Cursors are compared as canonical strings;
no unsafe Number conversion or unbounded revision history is added.

The strengthened regression at `web/src/pages/Alerts.test.tsx:150-215`
holds the older list response, processes delivery2, releases that response and
asserts that its absent later row still has a notice and stale query. It then
uses the enabled explicit Refresh control, observes the later row and verifies
that the notice clears. It also retains identical query filters, finite row
count and exact request counts. The unchanged original private R0 race test
now passes as well.

## Full scope identity and retained analysis

The review scope remains thirteen paths from base `c74f703`, not only the
two repair files. These ten tracked paths have an empty diff from reviewed
R0 HEAD `f579f6d`:

- `docs/decisions/0009-gap-safe-alert-delivery.md`
- `backend/migrations/postgres/008_alert_delivery.sql`
- `backend/internal/alertstream/delivery.go`
- `backend/testphase4/delivery_test.go`
- `backend/cmd/query-api/main.go`
- `api/openapi.yaml`
- `web/src/api/client.ts`
- `web/src/App.test.tsx`
- `web/src/components/AlertDetails.test.tsx`
- `backend/test/openapi_test.go`

The eleventh unchanged path, ignored generated `web/src/api/schema.d.ts`,
retains SHA256
`8a26dcc2cc5a8d29ac9cea36963a540d1718d4dcc339f100f000d2d2cce6043a`.
It was not force-added or regenerated for this review.

Their R0 analysis carries forward: additive atomic trigger/seed cutover;
counter row locked before pending selection through commit; transactional
unique delivery/counter/pending changes with bounded rollback; committed-bound
tenant replay independent of CH; strict decimal/future-cursor/finite-page
validation; JWT route ordering; terminal overflow and managed projector
shutdown; actual workflow DTO and preservation of Phase3 workflow/audit/
reservation semantics. Strict OpenAPI exact equality contains the authorized
fourteenth route and has no new changes.

The repaired query key extracts the same prior session/generation/filter key
without changing its contents. Filter, pagination, selected-detail/history,
focus, session disposal and old fixture assertions remain intact. The80-test
affected suite includes those existing checks. No original test was deleted
or replaced with a weaker mock of App/session/client.

Context from R0 was retained, and current12 SUMMARY's superseding repair
section and actual repair RED record were read. Its historical failure/setup
records remain distinct. Fallow is disabled; no structural pre-pass is claimed.
No project AGENTS.md or project skill directory was present in the R0 context.

## Independent verification

All independent checks below ran at `7aa711e`. Source identity confirms both
repair files are unchanged from GREEN `1e9725e`. Repair RED `3de6a95`
precedes GREEN and changes only the test file; production Alerts in RED is
unchanged from R0. The actual11:17:20 RED record returned1 with three causal
assertion failures: initial503, second-page503 and lost arrival notice.

| Independent check | UTC interval on 2026-10-25 | Elapsed | Actual result |
| --- | --- | ---: | --- |
| Original unchanged private R0 counterexamples |11:29:17.313551–11:29:19.297204|1.983680s|Exit0;2/2 |
| Affected Alerts/App/AlertDetails components |11:29:17.321725–11:29:22.314774|4.993060s|Exit0;80/80 |
| Nonempty native Delivery, phase4/race/10m/p1 |11:29:17.326116–11:29:33.286406|15.960302s|Exit0;root plus9subtests |
| Unchanged ten tracked paths / repair-to-HEAD source identity |Review interval|—|Empty diffs |
| R0 committed/file hash and ignored schema hash |Review interval|—|Match values above |
| `git diff --check c74f703..HEAD` |Review interval|—|Exit0 |

Exact native invocation:

```sh
python .local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/context.py \
  --cwd backend --timeout 630 -- \
  bash ../scripts/go-test-nonempty.sh './testphase4/...' \
  '^TestPhase4Delivery$' -tags=phase4 -race -timeout=10m -p=1
```

Ordinary component checks used the existing private12 launcher with
`--clean --cwd web --timeout 120` and `npx vitest run` selecting the three
affected files. The unchanged R0 private config used the same clean launcher
with a30s outer cap. Its existing future-config-loader warning was neither
suppressed nor altered.

Private R1 index:
`.local/phase4-acceptance/04-12-review/r1-index.json`.
The retained launcher records are
`20261025T112917313532Z-independent-r1-r0-counterexamples.json` and
`20261025T112917321706Z-independent-r1-components.json` in private04-12;
native record `20261025T112917326098Z-operator-7f1c279b.json` is in the NEW07
context. Exact argv/cwd/SHA/UTC/duration/exit/resource data are retained.
The review directory is0700 and its files0600; children use umask022.
Lowest completion disk observation was7,892,373,504 bytes, above the2GiB floor.
No credentials/headers, resolved Compose configuration or raw sensitive
service logs were printed.

## Limits and preservation

Native execution used only the authorized NEW context and the Delivery family's
new isolated databases. High-counter, overflow and cutover fixtures did not
rewrite the shared counter. The separate detector checkpoint-history-loss
runtime hold remains unchanged; this review claims neither nine healthy
services nor a new detection pipeline. Shared18081/18080 old07 images were
not current-source proof.

The current summary records earlier committed-source real Chromium1440/375
fault-proxy/held-PG-snapshot checks, full web151/generator121, ordinary tests,
quality/OpenAPI and scans. These are executor/root evidence, not independent
R1 reruns. R1 does not claim a new real-browser/layout, full Phase3 aggregate,
whole-phase, remote-publication or scanner acceptance. Original and repair
fixture failures remain historical rather than being rewritten as passes.

Only R1/designated review reports and the ignored private R1 index were written.
R0 and source/config/tracking/runtime/user dirt were preserved. No commit,
remote operation, reset/stash, branch switch, service recovery, shared migration,
seed resend, checkpoint/group/offset/fence/retention change or prune occurred.
Root owns the review commit, authorized publication and postwave acceptance.

---

_Reviewed: 2026-10-25T11:29:37Z_

_Reviewer: the agent (gsd-code-reviewer)_

_Depth: standard_
