---
phase: 04-query-api-analyst-dashboard
plan: "11"
reviewed: 2026-10-25T09:46:14Z
depth: standard
reviewed_head: ce4f575a404ceef3a79e23a31118c837771603c8
source_commit: 2f0bc7aaede2fbf8c7a76a0d68327f3c3cd4c036
tests_red_commit: 6e2ab68f2416d4dd74e548db00822ee5db4c50b5
diff_base: 6a33306584e0f68b8ff6a481e5c00180646bb765
files_reviewed: 13
files_reviewed_list:
  - backend/internal/query/summary.go
  - backend/internal/storage/clickhouse/summary.go
  - backend/internal/storage/postgres/summary.go
  - backend/testphase4/summary_rules_test.go
  - api/openapi.yaml
  - web/src/api/schema.d.ts
  - web/src/pages/Overview.tsx
  - web/src/pages/Overview.test.tsx
  - web/src/App.tsx
  - backend/cmd/query-api/main.go
  - web/src/api/client.ts
  - web/src/App.test.tsx
  - web/src/pages/Alerts.test.tsx
findings:
  critical: 1
  warning: 0
  info: 0
  total: 1
status: issues_found
runtime_verification: focused_native_and_components_passed
---

# Phase 4 Plan 11: Code Review Report

**Reviewed:** 2026-10-25T09:46:14Z
**Depth:** standard
**Files reviewed:** 13
**Status:** issues_found — 1 BLOCKER, 0 warnings

## Summary

The independent review found one reproducible range-correctness defect:
PostgreSQL silently truncates the shared nanosecond snapshot when binding the
rolling top-rules bounds. Valid boundary workflows can consequently be included
or excluded incorrectly. Passing existing native and component tests do not
cover this clock precision.

All thirteen explicitly owned paths were read: twelve tracked source/test/API
files plus the expressly assigned ignored generated schema. The tracked scope
is unchanged between source GREEN `2f0bc7a` and reviewed HEAD `ce4f575`.
Generated schema SHA256 is
`49cbb870db693360b4d424681a0b08b536078f52ecc1c766d1179055f432ff38`.
It remains ignored and was not force-added.

Context included the GSD reviewer instructions and code-review skill, 11 PLAN,
A1/rejected CHECK, A2/CHECK, A3/CHECK at approved `29acc9e`, CONTRACT/UI-SPEC,
11 SUMMARY's superseding root continuation, 10 SUMMARY/REVIEW-10-R0 and
07 STACK-RECOVERY. No root AGENTS.md, project .codex/skills/.agents/skills or
.codexignore exists. Fallow was disabled; no structural pre-pass was supplied.

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-01: PostgreSQL truncation changes the rolling half-open window

**Severity:** BLOCKER
**File:** `/home/logan78/Desktop/Sentinel/backend/internal/storage/postgres/summary.go:26-32`
**Related:** `backend/internal/query/summary.go:96`,
`backend/internal/storage/clickhouse/summary.go:29-31`,
`backend/testphase4/summary_rules_test.go:217`

**Issue:** The handler captures an unrestricted nanosecond-precision UTC
`time.Time`. ClickHouse preserves that precision through UnixNano parameters,
but PostgreSQL receives `now.Add(-24*time.Hour)` and `now` as timestamptz
parameters. The pinned pgx v5.11.0 binary encoder computes
`ts.Time.Unix()*1000000 + int64(ts.Time.Nanosecond())/1000`, discarding the
sub-microsecond remainder. Thus PostgreSQL uses different rolling boundaries
from the captured snapshot promised by the API.

For captured `now = 2026-10-25T12:00:00.000000001Z`:

- A workflow created at `2026-10-25T12:00:00.000000000Z` is strictly before
  now and should count. The encoded upper bound becomes that same whole-second
  instant, so `created_at < $4` wrongly excludes it.
- A workflow created at `2026-10-24T12:00:00.000000000Z` is one nanosecond
  before the required lower boundary and should not count. The encoded lower
  bound becomes that whole-second instant, so `created_at >= $3` wrongly
  includes it.

Both example workflows have valid canonical millisecond timestamps; this does
not require malformed or extra-precision stored data. A result can lose the
newest rule or retain a rule older than the requested rolling window, changing
the count, ordering or top-20 membership. Completed five-minute trend bounds
are already exactly representable and are not the affected predicates.

**Evidence:** Inspected the actual pinned encoder at
`/home/logan78/go/pkg/mod/github.com/jackc/pgx/v5@v5.11.0/pgtype/timestamptz.go:173`.
A read-only NEW PostgreSQL query compared the implemented predicates using
those encoded values with exact numeric epoch-nanosecond predicates. It returned
`upper|f|t` and `lower|t|f` (actual versus required). The evidence record is
`20261025T094400789274Z-operator-706fa64c.json` in the approved 07 context.
This is native SQL plus verified encoder evidence, not a newly executed
production-handler regression case. The existing frozen test deliberately
constructs a whole-second now at line 217, so its passing boundaries do not
exercise this defect.

**Fix:** Keep the single captured now unchanged. For PostgreSQL comparisons,
round each rolling bound **up** to the next representable microsecond when
there is a remainder, then bind those derived values. For timestamps stored at
microsecond resolution, both `created_at >= exactLower` and
`created_at < exactUpper` are equivalent to the same operators against their
respective microsecond ceilings. Leave an already aligned bound unchanged.

```go
func ceilMicrosecond(t time.Time) time.Time {
    floor := t.Truncate(time.Microsecond)
    if floor.Before(t) {
        return floor.Add(time.Microsecond)
    }
    return floor
}
// Bind $3 and $4 respectively:
lower := ceilMicrosecond(now.Add(-24 * time.Hour))
upper := ceilMicrosecond(now)
```

Add a frozen native handler/store regression with now one nanosecond after a
millisecond boundary, asserting inclusion of the upper-floor workflow and
exclusion of the lower-floor workflow, plus an exactly aligned exclusive-upper
case. Preserve shared-clock capture and ClickHouse's existing nanosecond bounds.

## Scope and boundary analysis

Beyond CR-01, no additional concrete BLOCKER or WARNING was established in this
increment. Review included these causal paths and their unchanged dependencies:

- Verified JWT principal supplies the tenant to both parameterized native
  aggregates. Main explicitly injects the real connections and verifier into
  the focused summary handler; viewer access introduces no write capability.
- ClickHouse reuses the existing complete earliest-received canonical tuple,
  including deterministic complete-tuple ties, before occurrence-time filters.
  The aggregate covers rate data in the current partial bucket while exposing
  only completed UTC trend buckets.
- PostgreSQL uses workflow rows exclusively, counts current open/severity over
  all time, and derives status-independent historical counts and top rules from
  one statement snapshot. CH-only orphan detections do not become workflows.
- The handler shares one captured now and one caller-derived five-second
  deadline. Native query/scan/iteration errors propagate to generic 503; known
  buckets are zero-filled only after both adapters succeed.
- The typed client retains the existing guarded credential transport, caller
  and session abort composition, post-body generation/caller checks and
  current-session 401 revocation. React Query scope includes session generation;
  unchanged session disposal revokes authority, aborts reads and clears caches.
- Actual root navigation renders Overview and preserves explicit Alerts,
  Events and Rules paths. The page renders returned metrics, two real SVG
  series, an equivalent 288-row table, top rules, loading/error/retry/empty
  states and refresh. It explicitly leaves consumer lag unavailable in Phase 5.
- App regression tests retain all assertions with explicit /alerts setup.
  Alerts start() conditionally defaults only a different pathname; existing
  /alerts query strings and cursor/filter/history fixtures survive. The new
  Overview tests independently exercise App/session/client at the actual root.

## Independent verification

First read-only health observation after the resumed-session gap showed all
nine authorized NEW services healthy with their approved loopback bindings.
Initial available disk was 9,941,827,584 bytes, later 9,577,644,032 bytes, both
above the 2 GiB floor. No service recovery or runtime configuration change was
needed or performed. Original held services remained untouched.

Private evidence index:
`.local/phase4-acceptance/04-11-review/checks.json` (directory0700/file0600).
The approved context launcher itself retains the exact native records under
`.local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/`; children use
umask022 and logs redact credentials.

| Check | Time / duration | Actual result |
| --- | --- | --- |
| NEW-context `bash ../scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4SummaryRules$' -tags=phase4 -race -timeout=6m -p=1`, cwd backend | 2026-10-25T09:42:54.585589Z–09:43:05.132112Z; 10.546542s | Exit0; nonempty root plus 15 named subtests, including four frozen-summary child cases |
| `npm test -- src/pages/Overview.test.tsx src/App.test.tsx src/pages/Alerts.test.tsx`, cwd web | Runner reports 15:12:56 local / 09:42:56Z, duration4.60s | Exit0; 3 files, 54 tests passed |
| Read-only NEW PostgreSQL precision predicate probe | 2026-10-25T09:44:00.789323Z–09:44:00.863735Z; 0.074418s | Exit0; both predicates disagree with required exact bounds |
| `git diff --check 6a33306..2f0bc7a` | Review interval | Exit0 |
| Exact tracked scope diff `2f0bc7a..HEAD` | Review interval | Empty |
| Generated-schema hash after npm pretest regeneration | Review interval | Identical to submitted/root-recorded hash |

The component command ran with no ambient SENTINEL_/PG overrides. Its direct
tool transcript records the test runner start/duration, not a separately
captured exact encompassing process start/end. npm pretest regenerated the
ignored schema without changing its content. Components use mocked HTTP
responses; they do not prove live layout, browser keyboard or database behavior.

Native verification uses only NEW PG25432, CH28123/29000, Redis26379 and
Kafka39092 via the approved private context. The native suite constructs its
current-source binary and focused frozen handler. Shared API18081/ingest18080
are earlier07 images and were not used as current11 source proof. Native
fixtures create their isolated random identities; no seed traffic was resent.

## Evidence limits and preservation

The 11 SUMMARY's superseding root continuation records committed-source native
proof, six real role/width browser flows at1440/375, full web139, clean ordinary
make test and quality gates. Those are earlier root evidence, not reviewer
reruns. Earlier missing-browser, fixture-count/foreign-tenant harness failures,
ownership rejections and the original incomplete handoff remain preserved.

Tests-only RED `6e2ab68` precedes GREEN `2f0bc7a`. Its causal proof covers the
missing authenticated route and root Overview. As the summary explicitly
states, precise frozen-clock aggregate/fault tests were added alongside the
focused constructor; they were not independently committed preimplementation
RED for every aggregate case. This review does not upgrade that limitation.

No new browser, complete web suite, typecheck/build/lint/scanner, topic/migration,
full readiness or preservation audit was run here. No expensive Phase3
aggregate or whole-phase acceptance is claimed. CR-01 remains a source blocker
even though the focused existing tests pass.

Only the two assigned reports and ignored private review evidence were created.
No tracked source, configuration, scanner policy, shared tracking or user dirt
was edited; no branch switch, reset, stash, migration, seed resend, shared
service stop/start/rebuild, prune, remote operation or commit occurred.
The user-owned modified .planning/config.json and untracked .serena/ remain
untouched and unstaged. Root owns repair, review commit and postwave acceptance.

---

_Reviewed: 2026-10-25T09:46:14Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
