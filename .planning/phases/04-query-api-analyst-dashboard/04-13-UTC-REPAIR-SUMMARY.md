---
phase: 04-query-api-analyst-dashboard
plan: 13-UTC-REPAIR
subsystem: api
tags: [postgres, pgx, utc, delivery, regression]
requires:
  - phase: 04-12
    provides: Durable delivery store and owned database fixtures
  - phase: 04-13
    provides: Strict live delivery validator and polling SSE at 2c88d18
provides:
  - UTC Z serialization of all four delivery workflow timestamps
  - Actual non-UTC-child wire regression preserving distinct inserted instants
affects: [04-13-browser-acceptance]
tech-stack:
  added: []
  patterns: [Normalize scanned workflow times to UTC before serialization]
key-files:
  created: [.planning/phases/04-query-api-analyst-dashboard/04-13-UTC-REPAIR-SUMMARY.md]
  modified: [backend/testphase4/delivery_test.go, backend/internal/alertstream/delivery.go]
key-decisions:
  - Keep UTCTimestamp and browser delivery validation strict; normalize only the delivery read DTO.
  - Keep the separate browser drop/reconnect failure outside this prerequisite's two source paths.
requirements-completed: []
duration: 4min
completed: 2026-10-25
---

# Phase 4 Plan 13 UTC Prerequisite: Delivery Timestamp Summary

**The actual delivery API now emits all four workflow timestamps with UTC Z suffixes while preserving their instants. The browser reaches Live and receives an online arrival; a separate stream-drop/reconnect failure still blocks Plan13 browser acceptance.**

## Scope and commits

Executed the independently approved two-path prerequisite at `ce39dfb`,
separately from Plan13's fourteen-artifact feature ownership. Root owns shared
tracking, independent repair review, subsequent feature review and acceptance.
No STATE/ROADMAP/requirements/review file was changed by this executor.

1. `ac7882d` — `test(04-13-utc): prove delivery timestamps violate UTC wire contract`
2. `1a3529e` — `fix(04-13-utc): normalize delivery timestamps before serialization`

`delivery_test.go` adds one sequential subtest with a newly owned migrated PG
database, random tenant credentials, the actual current race-built query API
and explicit inherited child `TZ=Asia/Kolkata`. Four distinct fixed instants
use PostgreSQL microsecond precision. Raw JSON strings must end in Z and parse
to their exact respective inserted instants. The test also checks actual200,
own/foreign tenant isolation, exact durable sequence, bound2, status open,
version1 and stable replay. All earlier delivery scenarios, including their
status/version/audit assertions, remain intact. The child joins before its
owned database cleanup. Global Go time.Local is never replaced.

`Store.Read` adds exactly four `.UTC()` assignments after successful Scan and
before appending: CreatedAt, UpdatedAt, WindowStart and WindowEnd. SQL,
projection, transaction, counters, tenant filtering, pagination and decimal
cursor behavior are unchanged. REST backfill and SSE share this read path.
The browser validator, OpenAPI and generated schema were not weakened.

## Causal RED and GREEN evidence

Private evidence stays under ignored `.local/phase4-acceptance/04-13/`.
Records retain exact argv, source SHA, working changes, UTC start/end, elapsed
time, actual exit, bounded resources and sanitized command output. All times
below are UTC on 2026-10-25.

| Gate | Start–end | Elapsed | Exit |
|---|---|---:|---:|
| Causal RED, focused actual delivery subtest at ce39dfb + test diff |22:44:46.291337–22:44:53.580936|7.290s|1|
| Full delivery GREEN before lint cleanup |22:45:10.926602–22:45:27.245553|16.319s|0|
| Final-source full delivery GREEN before commit |22:45:55.177667–22:46:10.738651|15.561s|0|
| Clean ordinary make test, final source before commit |22:46:00.551032–22:46:10.497518|9.946s|0|
| Committed1a3529e delivery, all10 subtests |22:46:18.297239–22:46:34.728446|16.431s|0|
| Committed1a3529e stream, all6 subtests |22:46:22.089915–22:46:56.856049|34.766s|0|
| Committed1a3529e default/phase4/integration vet/build/lint, all9 commands |22:46:23.272358–22:46:37.345005|14.073s|0|
| Web typecheck, including schema generation |22:46:30.554208–22:46:35.708470|5.154s|0|
| OpenAPI all11 top-level tests |22:46:37.361332–22:46:40.160011|2.799s|0|
| Web lint, format and build, sequential |22:46:43.645100–22:46:50.938993|7.294s|0|
| Actual browser at committed1a3529e, partial1440 proof |22:46:47.479227–22:47:20.223382|32.744s|1|

Native commands use `bash ../scripts/go-test-nonempty.sh ./testphase4/...`
with `'^TestPhase4Delivery$'` or `'^TestPhase4Stream$'`,
`-tags=phase4 -race -timeout=10m -p=1`. RED selects
`'^TestPhase4Delivery$/^utc_wire_preserves_instants_in_non_utc_child$'`.
The actual200 old wire had, in created/updated/window-start/window-end order:

```text
2026-10-25T06:32:03.123456+05:30
2026-10-25T07:33:04.234567+05:30
2026-10-25T05:31:02.345678+05:30
2026-10-25T06:31:02.456789+05:30
```

All four suffix assertions failed; parsing and exact instant comparisons
passed. This was a behavioral wire failure, not a setup/build/connection
failure. Full causal output is retained in
`20261025T224446291318Z-utc-repair-native-red.json`.

Clean ordinary `make test` passed default Go/race, web170 and generator121.
It completed before any separate web gate; no npm-ci/web dependency race.
OpenAPI uses clean environment and nonempty `'^TestOpenAPI'` with race/2m/p1.
Three-mode quality uses existing `quality.py`; each constituent actual result
is retained. No dependency installation or policy change occurred.

## Browser finding handed to root

The unchanged existing `browser.py` used actual current API, real isolated
migrated PG, real stores and loopback HTTP proxy, without route mocks.
`browser-1792968404824598561/delivery-shape.json` now shows the exact15 workflow
keys, decimal cursor9007199254740993 and all four timestamps ending in Z.
At1440 the strict client reached Live, attached real SSE200, received the
online arrival without refresh, kept filters, and displayed it on keyboard
explicit refresh. This proves the identified UTC blocker is repaired.

The next assertion timed out at `browser.mjs:36`: after the proxy drop the
expected `Reconnecting… Missed alerts will be recovered.` notice never
appeared within30s. Failure-state still shows Live and the online1440 row;
the recorded response sequence contains no subsequent stream/backfill.
Root was notified with exact evidence. No harness assertion or product source
outside the two paths was edited. Drop/backfill recovery, foreign-session
browser assertions and375 remain unproven. The actual failure remains in
`20261025T224647479201Z-actual-browser-1792968404824598561.json`
and the browser folder's failure-state. Owned API/proxy/Vite/browser children
were stopped and joined, then their owned database was dropped.

## Deviations and retained failures

One directly caused lint finding was fixed within the approved test path:
gocognit22 exceeded20 after adding the regression. The first phase4 lint exit1
is preserved at22:45:31.922371–22:45:34.564975. Replacing a manual
`if !HasSuffix / t.Errorf` with the existing strict `require.True` assertion
removed control-flow complexity without relaxing the condition. The focused
lint then passed and the full final-source and committed delivery gates passed.
This cleanup is included in GREEN `1a3529e`; production remains exactly4lines.

No new endpoint, auth path, SQL, schema, external file access or security
boundary was introduced. No new stubs or placeholders were added. Existing
failed browser attempts and all RED/lint failure evidence remain untouched.

## Runtime and limits

Only the authorized NEW context PG25432/schema8 and newly owned fixture
databases were used. No shared workflow/counter/history/data mutation, runtime
recovery, timezone configuration change, stack start/reset, offset/group fence,
prune, remote action or full-pipeline claim occurred. The separate detector
checkpoint-history hold remains unresolved. Disk stayed above2GiB (last check
6.1GiB). Private directories/files retain0700/0600; children use022 where the
existing harness requires it. User `.planning/config.json` and `.serena/`
remain untouched and unstaged.

OpenAPI SHA256 remains
`c8485cefb0e91ac79b1d8cd5f7ac8242d8b00369a815927350adebef426821ef`;
generated schema remains
`18fa5c129537eb3678664b22f306a917fe20fef264570d65e75bdc44b215a28e`.
No requirement or whole Plan13 acceptance is marked complete here.

## Self-Check: PASSED

Both modified source files and this repair report exist. Commits `ac7882d`
and `1a3529e` exist, contain only the two approved source paths, and introduce
no tracked file deletions. Committed native records and the truthful partial
browser/failure records exist. Independent repair source review remains with
root. This check validates this bounded report, not Plan13 acceptance.
