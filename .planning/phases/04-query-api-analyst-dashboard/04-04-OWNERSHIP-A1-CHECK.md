# 04-04 ownership amendment A1 — independent check

Decision: **APPROVE / PASS** for the proposed planning amendment only.

Issues: **0 BLOCKER, 0 WARNING**.

```yaml
status: approved
scope: 04-04-OWNERSHIP-A1.md only
review_head: 9b62afe
accepted_base: 3dee0ab
tests_only_head: 91b1ead
issues: []
```

## Evidence and causal assessment

Read the complete proposed amendment, approved 04-04 PLAN and shared CONTRACT;
the relevant 04-03 SUMMARY and complete clean 04-REVIEW-03-R1; actual App test
helpers, cleanup, both cache-key assertions and retained security cases;
Alerts filter normalization/request construction; existing pattern/validation
contracts; and bounded private test-log metadata/assertions. No AGENTS.md or
project .codex/skills/ or .agents/skills/ exists in the working directory.

The accepted 04-03 SUMMARY explicitly describes `{}` as that increment's empty
filter set and assigns future filter/cursor variables to 04-04. The clean R1
review approves that earlier behavior without claiming filter delivery. Plan
04-04 now requires frozen default 24-hour ranges, default limit 50 and
filter-bound pagination, retaining verified identity/generation isolation.

In the working Alerts.tsx, readFilters (lines 28–53) freezes from/to and supplies
limit 50; queryFilters (56–67) emits the exact default shape `{from,to,limit}`
and conditionally includes nonempty additional filters. The actual useQuery
key (182–191) contains the six existing identity/generation/discriminator
elements followed by that query object. queryFn (192–195) passes the same
object to client.alerts; client.ts (58–62) supplies it as HTTP query parameters.
Thus keeping `{}` in the two retained expectations would contradict the
newly required cache partition, rather than protect a valid old interface.

App.test.tsx:395 and :476 still use exact seven-element toEqual assertions
ending in `{}`. Git comparison against accepted base 3dee0ab confirms the
whole App.test.tsx is unchanged. The existing first six expectations include
the literal session/subject/tenant/role/alerts values and the retained numeric
generation matcher; these must remain as written.

Private 04-04/web-first.log records tests-only SHA 91b1ead plus enumerated
working production changes, not committed GREEN at that SHA. Its command is
the chained web test/typecheck/lint/build/format invocation, bounded 120 seconds
per command, started 2026-10-22T21:20:37.103056+00:00 and ended
21:20:43.367571+00:00 with exit 1. Both App failures differ solely in the last
key element: received finite millisecond UTC from/to values 24 hours apart
and limit 50 instead of `{}`. The log reports 41/46 passing, with the other
three failures in the new owned Alerts component tests: frozen next/previous
and browser history, 20-page previous history, and expiry/old-session history.
The later chained web gates did not execute after npm test failed.

Retained native-first.log and cursor-first.log record exit 0; they are executor
evidence, not fresh reviewer executions or slice acceptance. Native/web RED
logs record genuine HTTP/component behavior failures on 3dee0ab. The cursor
package's first passing run does not establish per-case causal RED. The
separate post-limit mutant log records exit 1 at the intended full-page
assertion (expected 3 items, received 2); it is negative-control evidence,
not an accepted defect or permission to leave it in source.

## Approved boundary and strict implementation handoff

The extra source ownership is **web/src/App.test.tsx only**, added to both
files_modified and Task 2's files by root before execution resumes. The full
eleven-path union is explicit:

1. backend/internal/query/alerts.go
2. backend/internal/query/cursor.go
3. backend/internal/query/cursor_test.go
4. backend/internal/storage/postgres/query_alerts.go
5. backend/testphase4/alerts_test.go
6. api/openapi.yaml
7. web/src/api/schema.d.ts (ignored generated derivative; still counted)
8. web/src/pages/Alerts.tsx
9. web/src/pages/Alerts.test.tsx
10. backend/cmd/query-api/main.go
11. web/src/App.test.tsx

This is an explicit fixture-only exception to the already approved ten-file
envelope. Two existing expectations and at most one small local assertion
helper add no subsystem or implementation feature; the two-task structure and
<=45% implementation-context target remain. Requirements API-03, API-08,
AUTH-03, UI-02, UI-06, wave 5 and dependency 04-03 remain unchanged. There is
no new dependency, framework, production path, cap increase or requirement
reduction. A whole-phase replan is not warranted for this bounded correction.

The correction must retain exact seven-element toEqual assertions. Derive
only the seventh expected object from the actual captured `/api/v1/alerts`
Request for the corresponding session: the first session's alert request in
the first case, and the new session's alert request in the delayed-old-tenant
case. Existing fetch mocks expose those Requests without modifying production
or shared setup. A helper can access the stubbed fetch calls and validate the
selected Request before returning exactly `{from,to,limit:50}`.

Validate the captured alert endpoint and exact default query fields; require
limit's HTTP value to be `50`, from/to to be finite UTC millisecond timestamps
(canonical ISO millisecond form with `Z`, with parse/ISO round-trip checks),
and `Date.parse(to) - Date.parse(from) === 86400000`. Compare those exact
captured values to the cache key. Do not derive expected values from the cache
key itself, accept arbitrary timestamps/limits, or use objectContaining,
arrayContaining, any-object matchers, or a key-prefix-only comparison. The
same strict helper may serve both cases; no additional shared fixture or
setup change is needed.

All existing 36 web cases remain, including the 29 App cases and other retained
files. Cache clearing, cached response equality, stale old-tenant suppression,
generation renewal, aborts, credential privacy and all eight CR-01 draft
clearing cases remain intact. No test removal, skip or weakened matcher is
authorized. Existing App/provider/client/CSS production files remain outside
this additional grant.

## Remaining gates and limits

Approval authorizes root's ownership metadata update and the bounded fixture
correction; it does not accept the working implementation. The executor still
must fix all three new component failures, verify cursor-only deep links keep
their frozen range, restore/retain correct status-before-limit SQL, and pass
the complete web, native Alerts/Auth, cursor, OpenAPI and affected static/default
gates with truthful SHA/working-state/command/start/end/exit evidence. Existing
independent source review and root acceptance remain required. No test budget,
security boundary or prior acceptance gate is waived, and 04-05 is not started.

This checker ran no tests, application, producer, agent, remote action or SDK
mutation. Only this report was written; source, PLAN, STATE, index, HEAD,
user-owned .planning/config.json, .serena and prior reviews were preserved.
