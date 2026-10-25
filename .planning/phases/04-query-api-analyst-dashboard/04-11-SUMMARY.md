---
phase: 04-query-api-analyst-dashboard
plan: "11"
status: implemented-pending-re-review
subsystem: api-ui
tags: [clickhouse, postgres, overview, react-query, native]
requires:
  - phase: 04-10
    provides: authenticated startup Rules and guarded session client
provides:
  - causal committed summary/root Overview RED
  - committed native-backed tenant Overview implementation with passing focused gates
affects: [04-11-continuation, 04-15, 04-16]
tech-stack:
  added: []
  patterns: [complete canonical identity before aggregate filters, shared UTC snapshot, PG workflow authority]
key-files:
  created: [backend/internal/query/summary.go, backend/internal/storage/clickhouse/summary.go, backend/internal/storage/postgres/summary.go, web/src/pages/Overview.tsx, web/src/pages/Overview.test.tsx]
  modified: [backend/testphase4/summary_rules_test.go, api/openapi.yaml, web/src/App.tsx, backend/cmd/query-api/main.go, web/src/api/client.ts, web/src/App.test.tsx, web/src/pages/Alerts.test.tsx]
key-decisions:
  - Preserve the approved root Overview route; an additional existing Alerts fixture requires explicit ownership before repair.
  - No production clock override; focused handler accepts a clock and main uses the real clock.
requirements-completed: []
duration: 9min evidence window
completed: 2026-10-25
---

# Phase 4 Plan 11: Tenant Overview — implementation and preserved handoff

**Native canonical-event and authoritative-workflow aggregates power the actual Overview; root completed the explicitly approved fixture continuation after the preserved handoff.**

## CR-01 precision repair — supersedes the reviewed source below

Independent R0 `fe60b38` found a concrete rolling PG boundary defect at source
`2f0bc7a`: pgx truncates sub-microsecond timestamptz parameters, incorrectly
including lower-floor rows and excluding upper-floor rows when the single
captured now has a nanosecond remainder. The original review is unchanged.

Bounded repair owns only `backend/testphase4/summary_rules_test.go` and
`backend/internal/storage/postgres/summary.go`, this summary and private11
evidence. The overall approved source union remains thirteen paths (twelve
tracked plus ignored generated schema). No additional source path was edited.

Tests-only **RED `58525e2`** precedes minimal **GREEN `ef19971`**. The actual
focused production handler, verified principal and native CH/PG adapters read
valid millisecond workflows around both rolling boundaries. At 1ns and 999ns,
RED returned `[LOWER_FLOOR:2,MIDDLE:2,LOWER_NEXT:1]` instead of required
`[UPPER_FLOOR:3,MIDDLE:2,LOWER_NEXT:1]`. The aligned millisecond, exact1us and
next aligned millisecond controls passed. All five cases assert unchanged
all-time open9, current severity, exactly288 aligned trend buckets and native
tenant separation. The failure was behavioral, not compile/setup failure.

GREEN derives the microsecond ceiling of **both** PG rolling bounds, retaining
already aligned values, and binds those values only to top-rules predicates
`created_at >= lower` and `created_at < upper`. This preserves comparisons
against representable PG timestamps without changing the one captured clock,
CH nanosecond bounds, completed five-minute trend bounds or open counts. A
small private helper is confined to the existing PG summary file. No new
dependency, schema, auth/network surface or stubs were introduced.

Private evidence under `.local/phase4-acceptance/04-11/` records exact argv,
UTC times, actual exits, source SHA, working state, elapsed duration and disk
samples. The native command is:
`bash ../scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4SummaryRules$' -tags=phase4 -race -timeout=6m -p=1`,
cwd backend through the approved NEW context.

| Repair gate | UTC start–end, 2026-10-25 | Elapsed | Result |
|---|---|---:|---|
| Native causal RED, source fe60b38 with tests-only working change | 09:49:08.317683–09:49:20.265475 | 11.947805s | Exit1; only 1ns and999ns leaf cases fail; complete failure retained |
| Native GREEN working tree at tests commit58525e2 | 09:49:36.597338–09:49:57.255212 | 20.657898s | Exit0; package12.431s, all new and existing cases pass |
| Default/phase4/integration vet, build and pinned golangci-lint | 09:49:37.775677–09:50:17.460206 | 39.684544s | Exit0; all nine constituent gates pass, all lint modes0issues |
| Clean ordinary make test, outer300s, no runtime/SSL overrides | 09:49:37.837263–09:50:04.882098 | 27.044848s | Exit0; ordinary Go race/unit, web139 and generator121 |
| Nonempty OpenAPI family,60s cap | 09:49:37.798815–09:49:46.457268 | 8.658492s | Exit0; eleven tests |
| Committed source ef199718148df3cda545c6166ccae487f796a893 native repeat | 09:50:34.469718–09:50:46.052936 | 11.583235s | Exit0;22 RUN records including root and21named subtests, package9.503s |
| Same committed source, real API/Vite/browser regression | 09:50:34.433240–09:50:51.172565 | 16.739333s | Exit0; six actual role/width flows, all three roles at1440/375 |

The existing corrected private browser harness uses cached full Chromium1248,
the primary populated native fixture and a tenant-scoped PG open-count read.
No fetch interception/mocks or old07 service image supplies browser data.
`browser-1792921834542660277/results.json` records exact open2/rate0 and
eventTotal6/alertTotal26,288API/table/SVG values and top-rule rows, refresh,
keyboard/history, logout privacy, no page errors and no horizontal overflow
for each of the six flows. The375px screenshot was visually inspected.
Current-source API/Vite children were terminated and joined in the harness's
finally block. Every gate producer returned; none remains running.

Read-only preflight observed all nine NEW services healthy and9GiB available
disk, above the2GiB floor. All native/runtime proof uses only PG25432/schema7,
CH28123/29000, Redis26379/Kafka39092 through approved private07 context. No
Compose start/migration rerun, shared service stop, old-stack operation, seed
resend, reset/prune, remote action or Phase5 work occurred. Unit gates ran with
ordinary clean runtime environment. Evidence directories/files remain0700/
0600 and binaries0700; child umask022. User config/.serena remain untouched
and unstaged; shared tracking and all review reports remain root-owned.

Generated schema is unchanged after normal npm pretest regeneration:
`49cbb870db693360b4d424681a0b08b536078f52ecc1c766d1179055f432ff38`.
It remains ignored and was not force-added. Pinned8.30.1 staged RED and
staged GREEN/generated/private-harness/full-history scans all returned0 under
unchanged policy; no exception or allowlist was added. There were no failed
GREEN attempts in this repair, no deviations, and no gate/cap changes.

**Repair self-check: PASSED.** Both owned files and generated schema exist;
RED58525e2 and GREENef19971 exist in order; exact committed-source native and
browser evidence returned0. Independent R1 review of the full approved
thirteen-path union and root postwave acceptance remain pending. This repair
does not itself close review, mark requirements complete or claim whole-phase
acceptance. The earlier R0 and all prior failed/limited evidence remain below
and in their original reports.

## Root continuation — supersedes incomplete handoff below

Independent A3 PASS0 at29acc9e approved the thirteenth path Alerts.test.tsx.
Root synchronized PLAN in638b456 before adding its conditional route default:
only non-/alerts paths change; existing deep-link query strings and assertions
remain. No production workaround or global test setup changed. Source GREEN
2f0bc7a follows causal RED6e2ab68; source union12tracked plus ignored generated
schema=13 approved paths. Original A1rejection/A2approval/A3counterexample and
executor attempt-cap handoff13d2d50 remain preserved.

Clean full `root-default-continuation` EXIT0/10.498s ending2026-10-25T00:40:43Z
passes cachedGo/web139/generator121. `root-web-continuation` EXIT0/13.313s ending
00:40:46Z passes typecheck/lint/format/build. Earlier three-mode backend quality,
affected native families and OpenAPI passes remain unchanged; only fixture
route setup changed afterward. `root-precommit-scans` passes staged/generated/
harness/fullhistory with pinned unchanged policy. `root-source-native` executes
actual committed2f0bc7a SummaryRules, including frozen aggregate boundaries.
Independent source review and root postwave remain required; fullphase open.

First committed browser run failed before app interaction because the recorded
Chromium headless-shell executable was absent. Existing cached full Chromium
with the same1248 revision was located; only private browser.mjs executablePath
changed. No download, dependency, production source or browser assertion change.
This setup failure is retained separately from browser behavior evidence.

Committed2f0bc7a `root-source-native` EXIT0/8.143s END00:41:06Z passes all16
SummaryRules subtests including real stores/frozenclock/fault/cancellation.
Browserv2 failed its hard-coded open2 because the native fixture had changed
status; private harness now reads actual tenant-scoped PG open count. Browserv3
then exposed its selection of the foreign isolation tenant by latestseq; select
the primary populated SummaryRules tenant by workflow count/latestseq instead.
No API/UI or assertion was weakened. Both failures remain recorded.
Browserv4 EXIT0/11.498s END00:42:39Z, source2f0bc7a, runs6real role/width flows:
all three roles at1440/375, PG open count reconciliation, nonzero event/alert
trend, exact288API/table/SVG points and top-rule rows, refresh/history/keyboard/
logout/privacy/nooverflow. Results browser-1792888947660197833/results.json;
mobile screenshot visually inspected. Current-source API/Vite children joined.
No sharedruntime change or fixture traffic resend. Generatedschema SHA256:
`49cbb870db693360b4d424681a0b08b536078f52ecc1c766d1179055f432ff38`.

## Status and commits

Task 1 causal RED is committed: `6e2ab68` (`test(04-11): pin authenticated summary and root Overview behavior`). Task 2 remains incomplete and all implementation changes remain unstaged, per root direction. No GREEN implementation commit, browser proof, source review, final requirements claim or whole-phase acceptance is asserted.

The approved twelve-path union is unchanged: eleven tracked source/test/contract paths in key-files above, plus ignored generated `web/src/api/schema.d.ts`. This summary and ignored private `.local/phase4-acceptance/04-11/` are additional permitted execution artifacts. User `.planning/config.json` and `.serena/` are untouched and unstaged. Root owns STATE/ROADMAP/REQUIREMENTS/VALIDATION/reviews; no SDK tracking mutation ran.

## Blocking ownership discovery

`web/src/pages/Alerts.test.tsx:49` defines `start()` without establishing a path before rendering actual App at line 71. The first parameterized minute-edit case calls it at line 118, then requires `First filtered page` at line 119. Its only general route setup is **after** each case at line 87, so the first case starts at the default `/`. With the approved actual Overview at `/`, that case requests summary and receives an Alerts-shaped fixture response, then displays the safe Overview error. The remaining cases inherit `/alerts` and pass.

The full ordinary `make test` found this causal regression: 138 web tests passed, one failed. Do not change production routing or global test setup to accommodate it. A narrow thirteenth-path amendment/check is needed for Alerts.test.tsx, establishing `/alerts` before these existing scenarios while preserving every assertion and explicit history/deep-link fixture. That file was not edited. Parent explicitly instructed an incomplete summary and no further fixes after the three consolidated fix batches.

## Evidence and exact gates

Every private JSON retains exact argv, cwd, UTC start/end, elapsed duration, exit, source SHA, working changes and disk samples. Evidence resides only in `.local/phase4-acceptance/04-11/`, directories 0700 and files 0600. Children use umask 022. No credentials, auth headers, resolved Compose configuration or raw sensitive process logs were printed.

RED source baseline `6a33306584e0f68b8ff6a481e5c00180646bb765`:

| Gate | UTC start–end on 2026-10-25 | Elapsed | Exit / observed result |
|---|---|---:|---|
| Native `bash ../scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4SummaryRules$' -tags=phase4 -race -timeout=6m -p=1` | 00:31:24–00:31:32 | 7.818s | 1; real binary summary route returned 404 instead of required authenticated 401; Rules regressions passed |
| Component `npm test -- src/pages/Overview.test.tsx` | 00:31:24–00:31:30 | 5.849s | 1; actual App root rendered Alerts, no Overview heading; three causal failures, one existing-style 401 case passed |

The following gates ran at committed RED HEAD `6e2ab68f2416d4dd74e548db00822ee5db4c50b5` **with implementation changes in the working tree**, not at a committed GREEN candidate:

| Gate | UTC start–end | Elapsed | Exit / result |
|---|---|---:|---|
| Final native SummaryRules, exact command above | 00:35:34–00:35:50 | 15.404s | 0; nonempty family, 15 named subtests, package 10.311s |
| Affected Auth/Alerts/Events/Triage, same flags and selector `^TestPhase4(Auth\|Alerts\|Events\|Triage)$` | 00:36:21–00:36:59 | 37.923s | 0; native package 36.627s |
| Default + phase4 + integration `go vet ./...`, `go build ./...`, pinned `golangci-lint run ./...`, respective build tags | 00:35:32–00:36:02 | 30.330s | 0 for all nine constituent commands; all lint modes 0 issues |
| Web `npm run typecheck && npm run lint && npm run format:check && npm run build` | 00:36:21–00:36:34 | 13.770s | 0; generated schema rebuilt, all formatting accepted |
| OpenAPI nonempty `^TestOpenAPI`, 60s cap | 00:36:21–00:36:22 | 0.958s | 0; eleven tests |
| Clean ordinary `make test`, outer 300s, no runtime/SSL test overrides | 00:35:33–00:36:48 | 74.552s | 2; Go unit/race passed, web 138/139 passed, Alerts fixture failure above; generator stage not reached |
| Earlier isolated App + Overview component run | 00:34:36–00:34:42 | 5.971s | 0; 33 tests before two later caller/session body-cancellation cases; those cases also passed in the full web run |

The complete fail-closed plan target is **not passing** because ordinary web/full `make test` remains blocked. Current-source browser harnesses were prepared privately but never executed; no browser environment was started and no browser evidence is claimed. No committed-HEAD native repeat has occurred after GREEN because GREEN is not committed.

## Implemented working-tree behavior

The separate focused SummaryHandler uses verified principal tenant, captures UTC now once, shares a five-second caller-derived request bound and injects native adapters explicitly from main. CH reuses the existing complete earliest-received canonical tuple with deterministic ties before occurrence-time filtering, grouping at most 289 buckets. Nanosecond parameter bounds preserve precise half-open windows over millisecond events. PG computes all aggregates in one parameterized statement snapshot from workflows: all-time current-open severity, top 20 all-status workflows over `[now-24h,now)` ordered by count descending/rule ID ascending, and all-status five-minute trend. Only after both dependencies succeed does the handler create the 288 ordered known buckets over `[floorUTC(now,5min)-24h,floorUTC(now,5min))`. Dependency errors return generic 503 without fabricated zero success.

Frozen-clock native checks cover rate `[now-60s,now)/60`, exact lower/upper boundaries, a non-UTC clock input normalized to UTC, two tenant identities, actual immediately queried physical duplicates, cross-boundary changed representations and equal-received deterministic tie, exact first/last trend buckets, more than twenty rule groups/ties, PG status change, CH-only orphan detections, genuine empty tenant, independently closed owned native CH/PG handles and caller cancellation. Existing real binary registration/auth and Rules tests remain. No shared service was stopped to inject faults.

OpenAPI documents these semantics and fixed bucket count. The generated client method uses the existing guarded transport and post-body generation/lifetime/caller checks with normal current-session 401 revocation. Overview is the actual `/` route, with primary open-alert count, severity, actual rate, SVG series and equivalent 288-row accessible table, top rules, refresh, loading/empty/error/retry, root navigation/history and sign-out privacy. Consumer lag explicitly reads “Consumer lag — available in Phase 5”. Existing App test assertions are preserved with explicit `/alerts` setup.

## Preserved failed attempts and deviations

1. The first post-implementation native run (00:33:25–00:33:34, exit 1) inserted an invalid equal-start/end PG workflow window. This was fixture setup failure, **not causal RED**. Owned fixture now uses a one-second valid window; subsequent exact native runs passed.
2. Initial component GREEN (00:34:14–00:34:18, exit 1) found duplicate accessible `Open alerts` labels on the section and metric. Removing the redundant section label made the focused run pass; assertions remain.
3. Initial phase4 lint (00:34:36–00:34:41, exit 1) found scoped complexity, integer-conversion, constants, test line formatting/tag and float-assertion issues. A consolidated final batch split the trend assembly, used named resolution constants, scanned PG nonnegative counts directly as uint64 and conformed tests to existing conventions. All three lint/build/vet modes subsequently passed. No unrelated file was fixed.
4. **TDD limitation:** committed RED covers real binary absence and actual root UI absence. Precise frozen-clock native aggregate/fault tests were added alongside the exported focused constructor during Task 2, because that API did not exist at RED and compile failure would not be causal behavior evidence. They passed native execution but are not represented as independently committed preimplementation RED for every aggregate case.
5. Full `make test` failure above is preserved as the current blocker. No retry, gate bypass or undeclared edit was performed after discovering it. Attempt limit respected; root will arrange independently checked scope and a bounded continuation.

## Runtime and limits

Native commands used only the NEW private07 child context: PG25432/schema7, CH28123/29000, Redis26379/Kafka39092. NEW nine services were read-only observed healthy before tests; initial filesystem available space was 10GiB, above the 2GiB floor. No schema addition, default old-stack command, Compose app start, migration rerun, seed resend, reset, prune, OPTIMIZE, remote operation or Phase5 work occurred. Shared API18081 remains prior07 and was not used as current-source proof. Temporary native API binaries were owned by existing test fixtures and all producers have returned. Browser scripts were not run.

No TODO/FIXME/placeholder stubs were found in the owned source scan. The intentional Phase5 lag-unavailability message is specified product behavior. The summary endpoint is already covered by the plan's tenant/duplicate/unavailable aggregate threats; no additional trust-boundary surface outside the threat model was introduced.

Pinned gitleaks 8.30.1 unchanged-policy staged-summary/generated-schema/owned-private-harness/full-history scans all returned 0 at 00:38:49–00:38:52Z. The earlier tests-only RED staging scan also returned 0 before its commit. These scans do not replace the missing final GREEN acceptance gates.

## Continuation requirements

Root must approve/check the precise Alerts.test.tsx ownership amendment before it is edited. Then bounded continuation should anchor that existing fixture, run clean full `make test` and required focused gates, scan staged/generated/private harness/full history with pinned unchanged-policy gitleaks 8.30.1, commit minimal GREEN, run bounded native and real browser proof at that committed HEAD at 1440/375, obtain source review and replace this incomplete handoff with the final truthful summary. Do not mutate shared tracking from an executor; do not claim final requirements or advance Phase4/Phase5 acceptance.

## Self-Check: PASSED

All eleven tracked owned source/test/contract files exist, ignored generated schema exists, and causal RED commit `6e2ab68` exists. Passing working-tree evidence is explicitly distinguished from the outstanding full-gate failure and missing GREEN/browser/source-review proof. Self-check validates this incomplete report, not plan completion.
