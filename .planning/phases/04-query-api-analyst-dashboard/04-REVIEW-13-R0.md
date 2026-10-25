---
phase: 04-query-api-analyst-dashboard
plan: "13"
reviewed: 2026-10-25T23:15:51Z
depth: standard
review_iteration: R0
reviewed_head: 6bd7390ae5dadcd2e1e459d918b68914ffacd2c7
source_commit: 2c88d18deb4b8a233d2d685efb4892d0776f9df7
tests_red_commit: bc5acbe3d1fe8eacfb615acd270ab9c2ac81e0a8
diff_base: 338bcc7221a08d07e18bca59fa690ea2553c51ff
prerequisite_source_commit: 1a3529ec218b7ab2365560e1a4fbdc1f809c12e4
prerequisite_review: 04-13-UTC-REPAIR-REVIEW.md
files_reviewed: 14
files_reviewed_list:
  - backend/internal/alertstream/handler.go
  - backend/cmd/query-api/main.go
  - backend/testphase4/stream_test.go
  - api/openapi.yaml
  - web/src/api/schema.d.ts
  - web/package.json
  - web/package-lock.json
  - web/src/api/live-alerts.ts
  - web/src/api/live-alerts.test.ts
  - web/src/pages/Alerts.tsx
  - web/src/api/client.ts
  - web/src/pages/Alerts.test.tsx
  - web/src/App.test.tsx
  - web/src/components/AlertDetails.test.tsx
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
structural_prepass: disabled
runtime_verification: independent_native_components_and_browser_passed
whole_phase_acceptance: false
---

# Phase 4 Plan 13: Code Review Report — R0

**Depth:** standard. **Result:** PASS — 0 BLOCKERS, 0 WARNINGS.

## Narrative Findings (AI reviewer)

No concrete remaining correctness, security or robustness defect was established
in the fourteen-artifact feature scope. No source fix is requested. This is an
independent scoped review, not whole-plan/root acceptance or a full-pipeline
verification.

The original feature has thirteen tracked paths and the explicitly owned,
ignored generated schema. The separate UTC prerequisite has two other source
paths, reviewed in `04-13-UTC-REPAIR-REVIEW.md`. Thus sixteen related source
paths were read across **two separately recorded execution envelopes**, not an
undisclosed sixteen-file feature amendment. Package/lock and generated schema
were inspected as the explicitly requested derivatives; the schema stays
ignored and was not force-added.

## Source analysis

Read the approved 13 PLAN/A1 check, the root continuation that supersedes its
historical incomplete handoff, CONTRACT/RESEARCH/UI-SPEC, 12 SUMMARY/R1 review,
and the separate UTC plan/check/summary. No root AGENTS.md or project
.codex/skills/.agents/skills directory exists. Fallow was disabled; no structural
results are inferred.

- `handler.go:80-126`: verified principal and actual expiry precede admission,
  durable read and header flush. Noncanonical/duplicate/future cursors and URL
  parameters fail closed. Store.Read uses the verified tenant and bounded pages;
  there is no ClickHouse dependency or competing fan-out consumer.
- `handler.go:129-220`: independent per-client durable replay, unconditional
  one-second polls, fifteen-second comments, finite per-write deadlines,
  ten-minute/token-expiry cap, and cancellation. Poll-only transport holds zero
  hints within the 32 bound. Admission is capped at 100 and released by defer.
  Main registers literal routes and cancels/joins streams before HTTP/pool
  shutdown; ordinary HTTP timeouts remain configured.
- `client.ts:47-102`: fixed same-origin stream capability privately injects the
  credential; pre-credential caller/lifetime/generation checks, combined abort,
  post-response checks and current-session 401 disposal remain intact.
  Credentials are absent from URLs, public session/context and persistence.
- `live-alerts.ts:26-207`: complete fifteen-field workflow validation, tenant
  check, decimal MaxInt64 comparisons, one WeakMap cursor per session client,
  REST catch-up before attachment, processing before advancement, per-frame
  lifetime checks, at most 32 queued callbacks, serialized processing, and
  bounded retry/cancellation. Permanent 4xx stop; 401 disposes authority.
- `Alerts.tsx:175-206,315-335`: invalidates existing queries without appending
  rows or changing filters/page. The earlier newer-arrival-versus-old-refresh
  watermark remains. Fixture adapters dispatch streams separately and retain
  earlier identity, cache, cursor, detail/history, version, focus and retry
  assertions.
- OpenAPI and regenerated schema agree on durable delivery envelopes, decimal
  Last-Event-ID, header auth, pre-flush errors and replay/resource limits.
  The new dependency is exactly fetch-event-source 2.0.1 with its lock integrity;
  implementation was checked against the installed package's actual retry,
  abort, visibility and parser behavior.

The four delivery timestamp conversions are a prerequisite, not a relaxed
client/schema check. The reviewed schema SHA256 remains
`18fa5c129537eb3678664b22f306a917fe20fef264570d65e75bdc44b215a28e`;
OpenAPI SHA256 remains
`c8485cefb0e91ac79b1d8cd5f7ac8242d8b00369a815927350adebef426821ef`.

## Independent disconnect diagnosis

The preserved browser failure
`browser-1792968404824598561/failure-state.json` is genuine evidence of that
attempt failing: Live remained displayed after the old proxy's drop, with only
the initial SSE response. It is not counted as a passing disconnect test.

A bounded actual HTTP probe removed the product entirely: a tiny SSE upstream
was connected through the exact private proxy and Vite transport. After
`/__stream/drop`, the direct proxy reader terminated with TypeError within
one second, while the reader through Vite remained pending with zero further
bytes. Both had actual 200 text/event-stream, chunked responses; Vite's response
had Connection: close. Thus destroying the proxy response did not deliver EOF
to the browser side of Vite. This establishes a private fault-harness transport
failure without assuming a client defect or a stale Set.

Original proxy/browser files and failed attempts are preserved. A separate
`review-proxy.mjs` changes drop from downstream `res.destroy()` to
`res.end()`, while still closing the upstream request and returning 503 for
offline recovery requests. The same probe now observes actual EOF both directly
and through Vite within one second, and active streams return to zero.
Evidence: `review-drop-probe.json` and
`review-drop-probe-graceful.json`, under private 04-13 evidence.

`review-browser.py` selects this separate proxy and executes the original
`browser.mjs` assertions unchanged. No assertion, timeout or product file was
modified. Current API and a newly owned migrated PG database then pass both
1440px and 375px: online arrival without append, keyboard filtered refresh,
actual stream EOF, visible recovery, disconnected commit recovered through
REST before reattach, exact decimal cursors above 2^53, sign-out/foreign-session
isolation, no browser storage and no page errors. No browser route mocks.
This proves recovery from the exercised EOF/503 fault, not every proxy/network
failure mode or full-stack detection.

## Independent executed checks

All checks below ran at reviewed HEAD `6bd7390`, whose feature source is
`2c88d18` plus separately reviewed prerequisite `1a3529e`. Times are UTC
on 2026-10-25; each actual producer exited and its output was inspected.

| Check | Start–end | Elapsed | Exit/result |
|---|---|---:|---|
| Current API browser environment and original assertions with EOF-corrected private proxy |23:13:56.612329–23:14:08.574223|11.962s|0; both widths |
| Actual Chromium assertion child |23:14:00.283666–23:14:07.464360|7.181s|0; results.json contains both widths |
| Focused affected web files |23:14:47.279832–23:14:53.093071|5.813s|0; 99 tests in 4 files |
| Exact native Delivery |23:14:47.296521–23:15:03.600678|16.304s|0; 10 subtests, including actual non-UTC child |
| Exact native Stream |23:14:47.292418–23:15:22.911153|35.619s|0; 6 subtests |

Native commands, from backend, use
`bash ../scripts/go-test-nonempty.sh ./testphase4/... '^TestPhase4Stream$' -tags=phase4 -race -timeout=10m -p=1`
and the identical Delivery command with selector `'^TestPhase4Delivery$'`.
Each selector executes nonempty. Web uses clean runtime environment:
`npm test -- src/api/live-alerts.test.ts src/pages/Alerts.test.tsx src/App.test.tsx src/components/AlertDetails.test.tsx`.
Its pretest regenerated the unchanged ignored schema. No npm-ci/make ran
concurrently with browser or web dependency readers.

Evidence is retained under `.local/phase4-acceptance/04-13/`:
`20261025T231356612309Z-independent-review-browser.json`,
`20261025T231400283640Z-review-actual-browser-1792970036669595972.json`,
`browser-1792970036669595972/results.json` and screenshots,
`20261025T231447279812Z-independent-review-components.json`,
`20261025T231447292399Z-independent-review-native-stream.json`, and
`20261025T231447296500Z-independent-review-native-delivery.json`.

## Proof boundaries and disposition

The native Stream family exercises two actual production replicas and exact
206 own alert IDs, tenant exclusion, REST-to-attach commit barrier, subsequent
205 commits across pages/polls, reconnect, pre-header negative responses,
heartbeat beyond ordinary two-second timeout, actual token expiry, real
slow-socket handler release and 100/101 admission/release. It does not hold a
deterministic internal Store.Read page-query boundary; the ten-minute maximum
was inspected in source rather than observed for ten wall-clock minutes.
Slow-client proof observes released admission, not when kernel-buffered FIN
bytes are finally drained.

LISTEN/re-LISTEN and broader recovery/resource hardening remain owned by 14;
they are not fabricated missing-feature findings against this approved
poll-only increment. Full browser matrix/full-phase/full-pipeline, hosted CI,
production edge behavior, benchmarks and root acceptance are not claimed.
Previously recorded three-mode quality/default web170/generator121/OpenAPI11
results are inherited evidence, not checks rerun by this reviewer.

Only authorized NEW07 context and newly owned fixture databases/processes were
used. Browser cleanup joined API/proxy/Vite children and dropped its owned DB;
native fixtures cleaned up their children/databases. Shared schema8 and the
separate detector checkpoint-history-loss hold were unchanged. Private files
are 0600, evidence directories 0700; children retain prescribed umask022.
No source, configuration, scanner policy or shared tracking was changed, no
runtime recovery or remote action ran, and no commit was made. User dirty
.planning/config.json and .serena/ were preserved. Root owns remaining scans,
report staging and acceptance.

_Reviewed: 2026-10-25T23:15:51Z_
_Reviewer: independent gsd-code-reviewer_
_Depth: standard_
