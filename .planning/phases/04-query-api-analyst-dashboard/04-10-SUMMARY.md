---
phase: 04-query-api-analyst-dashboard
plan: "10"
status: complete
subsystem: rules
tags: [startup-config, jwt, react-query, native-browser]
requires:
  - phase: 04-09
    provides: Verified session, guarded transport, preserved NEW runtime
provides:
  - Committed causal native and component RED tests
  - Committed startup Rules endpoint and browser implementation with passing behavioral proof
affects: [04-10-continuation, 04-11, 04-15, 04-16]
tech-stack:
  added: []
  patterns: [validated startup snapshot, generated guarded read-only client]
key-files:
  created: [backend/testphase4/summary_rules_test.go, web/src/pages/Rules.test.tsx]
  modified: [backend/internal/query/rules.go, backend/cmd/query-api/main.go, api/openapi.yaml, web/src/api/schema.d.ts, web/src/pages/Rules.tsx, web/src/App.tsx, web/src/api/client.ts]
key-decisions:
  - Use existing bounded detection.LoadRules before query listeners and copy its validated definitions
  - Keep retained decision/fingerprint historical explanations separate from current definitions
  - Preserve executor attempt-cap handoff; root completes bounded native-test cleanup
requirements-completed: [API-06, UI-04, UI-06, AUTH-03]
duration: 8 min to handoff
completed: null
---

# Phase 4 Plan 10: Startup Rules — implementation and preserved handoff

**Validated read-only startup Rules and guarded browser page; root completed the bounded quality continuation after the executor handoff. Independent review remains pending.**

## Root continuation — supersedes pending-quality handoff below

Tests-only REDa55d82f precedes source GREENdbc7ae2. Root extracted the existing
fail-closed startup assertions into an owned test helper and wrapped long lines;
no assertion, source ownership, runtime or lint policy changed. First root
quality continuation failed solely because the new helper placed Context after
testing.T; parameter ordering was corrected. That failed attempt remains private.
Final `root-quality-final` EXIT0/20.802s END2026-10-24T18:11:21.173584Z executes
all three vet/lint/build configurations, each lint reporting0issues. Native
`root-native-continuation` EXIT0/8.320s passes all9cases after extraction.
Pinned unchanged-policy `root-scans-precommit` passes staged/generated/harness/
full-history. Generated schema SHA256:
`a590e36b43fcf289ee50a15af9a4a831610c85c65f8b82a2830abf5f78a2280a`.
Eight tracked source paths plus ignored generated schema remain the approved9.
Original RED tests and incomplete handoff5cf9df5 stay preserved in history.
At committeddbc7ae2, `root-source-head-native` EXIT0/8.290s ending18:11:36Z
passes all9SummaryRules cases; `root-source-head-browser` EXIT0/12.961s ending
18:11:41Z passes all3roles at1440/375 with actual current API and NEW data.
No producer remains after the joined browser children; no07traffic was resent.
The historical pending-quality statements below describe that handoff, not the
current source. Root acceptance below closes independent review and postwave.

Root accepted10 on2026-10-25 after clean9-path R0at8fdcdae/sourcedbc7ae2 and
independentcomponent8PASS. Reviewer withheld native when NEWinfra was stopped.
Root normalinfra start0 preservedIDs; Composeappstart1 because stale006migration
image refused schema7 (current version7 outside0..6). Current host tern against
NEWPG25432 exited0; direct start of recorded owned NEWappIDs yielded9healthy,
EXIT0/0.606s END00:25:26Z. No downgrade/recreation/reset/oldstackoperation.
StalecontainerEXIT1 remains historical, not a passing migration-container claim.
FreshrootSummaryRules9 EXIT0/11.401s END00:25:04Z. Rootbackend/webbuilds+normal
make test EXIT0/23.246s END00:25:17Z (cachedGo/web133/gen121), private
07/root-10-postwave.log. Source/generatedSHA matchrevieweddbc7ae2.
Private10 root-runtime-recovery/v2 and root-native-after-recovery retain actual
commands/UTC/exits. Phase4 now11/17accepted; continue11. Later runtime must use
current host migrations or separately reviewed NEWimage refresh, not stale006.

## Status and commits

Plan remains **pending-quality**, with **1/2 tasks complete**. No whole-plan,
phase, requirement or acceptance completion is claimed. Root owns tracking.

1. Task1 tests-only RED: `a55d82f20a0b3667f798a540f12e04a761fd9c51`
   — `test(04-10): pin startup Rules API and browser behavior`.
2. Task2 source remains uncommitted and unstaged for root continuation.
   The generated schema remains ignored and was not force-added.

Source union stays inside all nine approved paths: rules handler, query main,
native SummaryRules tests, OpenAPI, ignored generated schema, Rules page/tests,
App and client. No ownership expansion occurred. User config and `.serena/`
remain untouched and unstaged. No tracking SDK mutation was performed.

## Implemented behavior awaiting quality completion

The real query process uses `detection.LoadRules` on
`cfg.Detector.RulesPath` before opening listeners, closes the file and rejects
open/parse/close failures. Separate `NewRulesHandler/Register` copies a finite
startup snapshot, registers JWT-protected GET for all three roles and performs
no request-time file read. Detector configuration/behavior and retained PG
historical explanations are unchanged. Integer `window_ms` and `cooldown_ms`
preserve the loader's millisecond precision; threshold/version/severity/enabled
come from actual startup definitions, including disabled definitions.

Rules uses the existing generated transport and React Query/session lifetime,
shows exact loading/empty/error/retry copy, explanations/parameters/version and
Enabled/Disabled text, and adds actual `/rules` navigation/current-link state.
There is no CRUD or credential path. Tests cover real-binary startup mutation,
restart, malformed/invalid/missing configuration, unsupported writes, all roles,
unauthenticated401 and retained historical version77 versus current version12.
The component suite covers real App/session/client loading/retry/navigation,
escaped text, privacy, current401 and caller/session post-body cancellation.

## Retained evidence and actual results

All evidence is under ignored `.local/phase4-acceptance/04-10/`, directories0700,
files0600 and browser binaries0700. Launcher records exact argv/cwd, UTC times,
elapsed seconds, actual exit, source SHA and working changes. Child test/build
umask022. All listed commands used source HEAD `a55d82f` plus the recorded
working changes, except both RED runs at `73dddfb`. They are worktree proof,
**not committed GREEN source proof**.

| Evidence suffix (20261024 UTC) | Start–end | Elapsed s | Exit |
| --- | --- | ---: | ---: |
| 180255275505Z-native-rules-red | 18:02:55–18:03:25 | 30.443 | 1 |
| 180258830569Z-component-rules-red | 18:02:58–18:03:07 | 8.592 | 1 |
| 180423161603Z-native-rules-green | 18:04:23–18:04:31 | 8.785 | 0 |
| 180424335366Z-component-rules-green | 18:04:24–18:04:28 | 3.796 | 1 |
| 180459658565Z-component-rules-green-v2 | 18:04:59–18:05:03 | 3.885 | 0 |
| 180500807486Z-default-green | 18:05:00–18:05:16 | 15.200 | 0 |
| 180502087995Z-affected-native | 18:05:02–18:05:39 | 37.578 | 0 |
| 180619220441Z-web-quality | 18:06:19–18:06:40 | 21.140 | 0 |
| 180620419080Z-backend-quality | 18:06:20–18:06:28 | 8.407 | 1 |
| 180621591403Z-browser-green | 18:06:21–18:06:34 | 12.542 | 0 |
| 180622816807Z-openapi-green | 18:06:22–18:06:24 | 2.032 | 0 |
| 180702222425Z-backend-quality-v2 | 18:07:02–18:07:06 | 4.693 | 1 |
| 180737635688Z-backend-quality-v3 | 18:07:37–18:07:44 | 6.929 | 1 |
| 180753759923Z-native-rules-final-worktree | 18:07:53–18:08:04 | 10.442 | 0 |
| 180754950942Z-default-final | 18:07:54–18:08:06 | 11.866 | 0 |

Native exact command: `bash scripts/go-test-nonempty.sh './testphase4/...'
'^TestPhase4SummaryRules$' -tags=phase4 -race -timeout=6m -p=1`.
Final native run passes all nine subtests. Affected native command uses the same
flags with `^TestPhase4(Auth|Alerts|Events|Triage)$`; all four families pass.
Every native command uses the approved NEW context via private `run.py`, which
imports the exact07 `context.py` environment. No generic old-stack command ran.

RED native failure is causal: valid binary started; authenticated GET404 versus
expected200, unauthenticated404 versus401, invalid/missing rule configuration
failed the assertion that startup exits before an8second timeout. Compilation
and fixture setup passed; timeout is explicitly not called failclosed proof.
RED components compiled and rendered existing Alerts at `/rules`, missing the
Rules heading/loading/request behavior. Each RED ran separately with exit1.

The first component GREEN retained exit1 for two harness mistakes: openapi-fetch
parses via `text()` rather than an overridden `json()`; StrictMode can launch
two requests including its cancelled first mount. Tests were corrected and v2
passes6 tests. Two further direct post-body cancellation tests are included in
the later full web-quality/default passes. Web-quality passes all133 tests,
typecheck/generated types, ESLint, Prettier and production build.

Clean normal `make test` runs with all NEW Sentinel/PG overrides removed and a
300second outer cap; both actual runs pass. OpenAPI nonempty runner
`'./test/...' '^TestOpenAPI' -timeout=60s` passes. No expensive Phase3 aggregate,
cap increase, golden regeneration or default runtime command was run.

## Browser and runtime

Current-source ephemeral race-built query API and Vite proxy used the exact
NEW PG25432/CH28123+29000/Redis26379/Kafka39092 context. Shared18081/18080
remain old07images; no current-source claim is made for them. Initial all9health
and ~10.76GB disk were observed; latest disk remains ~10.45GB, above2GiB.
No container recreation/start/reset/prune, traffic resend or schema change ran.

Browser uses existing recovery tenant `recovery-rule-c2ba399-01` with zero new
browser fixture writes. All six combinations of role viewer/analyst/admin and
width1440/375 pass exact four API/UI rules, actual PG alert rows and CH event
rows, keyboard Enter navigation, browser Back/Forward, current-link state,
logout/input/storage privacy, no page horizontal overflow and no page errors.
Evidence `browser-1792865181689339495/{results.json,rules-1440.png,rules-375.png}`
is retained; the mobile screenshot was visually inspected. No network mocks.
The browser proof predates the final setup-helper lint refactor; native and
normal tests were repeated after that refactor. Committed-source browser proof
remains a continuation obligation. Browser/API/Vite children were stopped and
joined. No producer remains after final native/default exits.

## Deviations and failed attempts

Scope remains approved. Routine fixture/lint fixes exhausted the executor's
three-attempt limit; execution stops before further source editing. Earlier
quality failures remain preserved: initial setup57statements and a long rule
description; second setup51statements and gosec variable-path warning. The
bounded loader was extracted and setup split within owned main. G304 has a
narrow documented annotation for the trusted operator startup path, never
request input. Default lint now passes; no lint policy was changed.

## Deferred Issues — required bounded continuation

Latest `backend-quality-v3` passed all three vet variants and default lint,
then phase4 lint failed only in new `backend/testphase4/summary_rules_test.go`:

- Line29: `TestPhase4SummaryRules` cognitive complexity28 exceeds20; extract
  a test helper within this owned file, preserving assertions.
- Lines41,58,104,145,146,148,156 exceed120characters; split expressions,
  SQL lines and fixture construction without weakening behavior.

The command is fail-closed, so integration lint and all three explicit build
variants after phase4 lint **have not run**. Complete them after the owned test
cleanup. Then stage only approved source, run pinned8.30.1 unchanged-policy
staged/generated/private-harness/full-history scans, commit GREEN, run bounded
native/browser at that committed HEAD, update this summary and commit metadata.
The tests-only RED staged scan passed before a55d82f. Final implementation and
private harness scans remain pending; `scans.py` is ready but was not executed.
No new scanner finding or waiver is asserted. Root independent review and
postwave acceptance remain mandatory. Stop beforePhase5/remote gate04-16.

No known goal-blocking stubs, new dependencies or authentication gates exist.
Security surface is the already planned read-only JWT endpoint and trusted
startup file read; no unplanned threat surface was introduced.

## Self-Check: PASSED (incomplete handoff only)

Verified the RED commit exists, all nine owned source/generated paths exist,
the private evidence and mobile screenshot exist, both final producers exited0,
and only this incomplete summary is staged for the handoff metadata commit.
This self-check does not assert task2 or plan completion.
