---
phase: 04-query-api-analyst-dashboard
plan: "11"
reviewed: 2026-10-25T09:53:39Z
depth: standard
review_iteration: R1
reviewed_head: a885410f9b67de2c5bd79165084bfed274ba7e4e
source_commit: ef199718148df3cda545c6166ccae487f796a893
tests_red_commit: 58525e24f7c812632e39806d8f8b418f31789b4e
original_source_commit: 2f0bc7aaede2fbf8c7a76a0d68327f3c3cd4c036
original_tests_red_commit: 6e2ab68f2416d4dd74e548db00822ee5db4c50b5
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
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
closed_findings: [CR-01]
runtime_verification: focused_native_passed
---

# Phase 4 Plan 11: Code Review Report — R1

**Reviewed:** 2026-10-25T09:53:39Z
**Depth:** standard
**Files reviewed:** 13
**Status:** clean — 0 blockers, 0 warnings

## Summary

CR-01 is closed by the bounded PostgreSQL precision repair. Independent
adversarial re-review found no remaining concrete BLOCKER or WARNING in the
approved thirteen-path increment. All reviewed files meet quality standards.
No issues found.

R1 retains the original scope from base `6a33306`, rather than narrowing its
file list to the two repair paths. The preserved R0 supplied the full original
per-file review. R1 directly inspected the changed PostgreSQL summary and
native test code and verified the identity of the other eleven paths. The
current source matches GREEN `ef19971` at reviewed HEAD `a885410`.

Context includes the previously loaded reviewer instructions/code-review skill,
11 PLAN and A1/A2/A3 ownership decisions, CONTRACT/UI-SPEC, preserved R0,
10 SUMMARY/REVIEW and 07 STACK-RECOVERY. The current 11 SUMMARY's superseding
CR-01 section was read; historical incomplete handoffs remain historical.
No project AGENTS.md or project skills directory exists. Fallow remains
disabled; no structural pre-pass was supplied.

## Narrative Findings (AI reviewer)

No remaining findings.

### Closed CR-01 — prior BLOCKER: PostgreSQL rolling boundary truncation

At `backend/internal/storage/postgres/summary.go:32-33`, both rolling
top-rules parameters now use `ceilSummaryMicrosecond`. The helper at lines
64-69 floors to a representable microsecond, adds one microsecond only when
the original instant is later, and returns an already aligned instant unchanged.

For any PostgreSQL-representable timestamp `x` and captured bound `b`,
`x >= b` is equivalent to `x >= ceilMicrosecond(b)`, and `x < b` is
equivalent to `x < ceilMicrosecond(b)`. The repair therefore preserves the
required half-open interval; it does not admit a representable timestamp
outside the original bounds. The encoded values have no remainder for pgx to
discard. Both ends are repaired, avoiding a one-sided lower-bound leak.

The helper uses time.Time truncation/comparison, not signed Unix division.
The same ceiling logic applies before the Unix epoch and across a second
boundary; no negative-remainder arithmetic is introduced. This is static
reasoning, not a claim of a historical-epoch native fixture. Production captures
the real clock; it accepts no caller-controlled clock override.

Only rolling PG parameters $3/$4 changed. The shared captured now, CH nano
parameters, aligned trend parameters $2/$5, current all-time open/severity
counts, status-independent rule count/order/limit, verified tenant and error
propagation are unchanged. No schema, constructor, network or auth surface
changed.

The new production-handler/native-store cases in
`backend/testphase4/summary_rules_test.go:380-445` use valid millisecond
workflows and random isolated tenants. The clock update is sequential, restored
after the subtest and still checked for one capture per request. Cases assert
the entire ordered rule-count result at 0ns, 1ns, 999ns, exactly1us and the
next aligned millisecond. They also retain all-time open9/severity, complete
288-bucket trend equality, expected first/middle buckets and foreign isolation.
The original tests and assertions were not removed or weakened.

The actual preserved RED record was read: the 1ns and999ns leaves failed with
LOWER_FLOOR:2 replacing required UPPER_FLOOR:3; aligned controls passed.
Tests-only commit `58525e2` precedes GREEN `ef19971`, and the PG adapter was
unchanged from original GREEN in that tests-only commit. This closes the
specific causal gap identified in R0.

## Full scope identity and retained analysis

The two changed paths are the PG summary and SummaryRules test. These eleven
paths are identical to the source independently reviewed in R0:

- `backend/internal/query/summary.go`
- `backend/internal/storage/clickhouse/summary.go`
- `api/openapi.yaml`
- `web/src/api/schema.d.ts`
- `web/src/pages/Overview.tsx`
- `web/src/pages/Overview.test.tsx`
- `web/src/App.tsx`
- `backend/cmd/query-api/main.go`
- `web/src/api/client.ts`
- `web/src/App.test.tsx`
- `web/src/pages/Alerts.test.tsx`

Ten tracked paths have an empty diff from `2f0bc7a`; the ignored schema's
SHA256 remains
`49cbb870db693360b4d424681a0b08b536078f52ecc1c766d1179055f432ff38`.
Their R0 analysis carries forward: canonical identity selection precedes event
filters, verified tenant reaches every aggregate, PG owns workflows, request
cancellation/deadlines and 503 behavior remain intact, and the guarded session
client/root Overview/accessible series/table/fixture routes remain unchanged.
The explicit Phase5 consumer-lag unavailability also remains unchanged.

R0 is preserved byte-for-byte with SHA256
`afade3dc26f3559d7b8e32bdeda23b068a1fed8fb82b51d972ba4d61ad73b6f1`.
It remains the historical report that discovered the blocker; its findings
were not edited into a clean result.

## Independent verification

First read-only NEW health admission at09:52:52Z showed all nine authorized
services healthy, with PG25432, CH28123/29000, Redis26379 and Kafka39092.
Available disk was8,939,139,072 bytes initially and8,912,793,600 at native
completion, above the2GiB floor. No runtime recovery was performed.

| Check | UTC interval / duration | Actual result |
| --- | --- | --- |
| NEW-context nonempty SummaryRules with phase4/race/6m | 2026-10-25T09:53:02.286848Z–09:53:20.326855Z;18.040093s | Exit0;22 RUN records: root plus21 named subtests; all precision controls and prior cases pass |
| `git diff --check 6a33306..ef19971` | Review interval | Exit0 |
| Ten unchanged tracked source paths, `2f0bc7a..HEAD` | Review interval | Empty diff |
| Source paths `ef19971..HEAD` | Review interval | Empty diff |
| Generated schema and preserved R0 hashes | Review interval | Match the values above |
| RED ancestor/source separation | Review interval | Tests-only58525e2 precedes GREENef19971; no PG repair in RED |

Exact independent native invocation, from repository root:

```sh
python .local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/context.py \
  --cwd backend --timeout 390 -- \
  bash ../scripts/go-test-nonempty.sh './testphase4/...' \
  '^TestPhase4SummaryRules$' -tags=phase4 -race -timeout=6m -p=1
```

The approved launcher captures exact argv/cwd/head/time/exit/resource data in
`20261025T095302286822Z-operator-770bf5a3.json` under its private07 context.
A private R1 index is retained under
`.local/phase4-acceptance/04-11-review-r1/` (directory0700/files0600).
Children use umask022 and credentials are redacted. Native tests run the actual
current-source binary and focused handler against real isolated stores.
Shared API18081/ingest18080 earlier07 images were not current-source proof.

## Limits and preservation

No component rerun was needed for the backend-only repair: unchanged components
had independently passed54/54 across the three affected files in R0. The current
SUMMARY records earlier repair-native22, six current-source browser role/width
flows, ordinary web139/generator121, three-mode backend quality, OpenAPI and
scans. These remain earlier executor/root evidence, not independent R1 reruns.
R1 does not claim new browser/layout, full-suite, scanner, migration/topic or
whole-phase acceptance.

The original increment's aggregate tests were added alongside its constructor,
as preserved in R0/SUMMARY. This repair supplies its own committed causal RED;
it does not rewrite the older TDD limitation or failed attempts.

Only R1/designated review reports and ignored private evidence were written.
R0, source, config, scanner policy, shared tracking, runtime and user dirt were
preserved. No commit, remote operation, branch switch, reset/stash, seed resend,
migration rerun, service stop/start/rebuild or prune occurred. User-owned
.planning/config.json and .serena/ remain untouched and unstaged.
Root owns the review commit, postwave checks and acceptance.

---

_Reviewed: 2026-10-25T09:53:39Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
