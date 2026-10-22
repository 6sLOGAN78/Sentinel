---
phase: 04-query-api-analyst-dashboard
plan: "04"
status: pending-independent-source-review
subsystem: api
tags: [postgres, keyset-pagination, authenticated-cursors, react-query, openapi]
requires:
  - phase: 04-03
    provides: Verified session generation, typed client and isolated workflow cache
provides:
  - Filtered PostgreSQL workflow pages with authenticated frozen tuple cursors
  - Debounced alert filters, bounded previous pages and session-safe URL history
affects: [04-05, 04-06, 04-08, 04-12, 04-15, 04-16]
tech-stack:
  added: []
  patterns: [filter-before-limit, millisecond-keyset, domain-separated-AEAD-cursors]
key-files:
  created:
    - backend/internal/query/alerts.go
    - backend/internal/query/cursor.go
    - backend/internal/query/cursor_test.go
    - backend/testphase4/alerts_test.go
    - web/src/pages/Alerts.test.tsx
  modified:
    - backend/internal/storage/postgres/query_alerts.go
    - backend/cmd/query-api/main.go
    - api/openapi.yaml
    - web/src/api/schema.d.ts
    - web/src/pages/Alerts.tsx
    - web/src/App.test.tsx
key-decisions:
  - "Filter PG workflows before limit+1; use date_trunc milliseconds consistently for selected created_at, tuple comparison and ordering."
  - "Encrypt/authenticate versioned cursors using AES-GCM with random nonces and an HMAC-derived domain-separated key from validated auth material."
  - "Cursor-only links omit absent ranges and rely on the authenticated server payload; changing filters creates a fresh explicit range."
  - "Apply independently approved A1 only to two obsolete cache filter expectations and one strict HTTP-derived local helper."
requirements-completed: [API-03, API-08, AUTH-03, UI-02, UI-06]
duration: 42min recorded elapsed span including ownership checkpoint and usage interruption
completed: 2026-10-22
---

# Phase 4 Plan 04: Filtered alert pages and authenticated cursors Summary

PostgreSQL now supplies full filtered workflow pages with stable millisecond tuples and authenticated frozen cursors; the Alerts UI supports filters, debounced search, finite paging and session-safe deep links.

**PENDING INDEPENDENT SOURCE REVIEW AND ROOT ACCEPTANCE.** Source candidate: `0abb49d2855225faba0949ac86aa33255ce4224c`; accepted starting base: `3dee0ab075c0c96b6bc8257722a9aa21b2ee358d`. Both planned tasks are implemented and locally verified. Requirement IDs above describe incremental coverage, not whole-phase completion. Root owns independent review and STATE/ROADMAP/REQUIREMENTS updates. No 04-05, Phase5, publication or hosted CI was started.

## Ownership and timing

The declared union is eleven paths, including ignored generated `web/src/api/schema.d.ts`; ten source paths are tracked. A1 independently approves only `App.test.tsx`'s two obsolete seventh key elements and one local strict helper. Its first six key elements and all original 36 tests/security assertions remain intact. Root committed A1 proposal `9b62afe` and approval/plan amendment `467157e`; these are planning approval, not source acceptance.

Recorded implementation/evidence span is approximately 21:12–21:54 UTC October22, including the ownership checkpoint and a usage interruption. It is elapsed session time, not measured active work or a benchmark. Every private wrapper retains its precise start/end/actual exit and HEAD plus working changes. Precommit runs identify tests-only HEAD plus working source; they do not claim GREEN was already committed. No task-owned source changed after final verification except test fixture strengthening, subsequently verified by the exact native family and phase4 lint/vet. The final default gate also ran the committed candidate's then-identical working source.

User-owned `.planning/config.json` and `.serena/` remain untouched and unstaged. No dependencies, migration, index, App/provider/client/CSS production source, shared fixtures, scanner exception or twelfth source path changed. No tracked file was deleted. No reset, prune, teardown, truncation or volume operation occurred. Existing dependencies were healthy; both read-only stack checks passed without startup mutation.

## Interfaces and behavior

- `query.NewHandler(Dependencies{Alerts, Auth, Cursors}).Register(e)` owns the one delivered list route; command wiring removes its previous route/parser without duplicate registration. The package owns `Page[T]`, `ListOptions`, `AlertFilters`, `Position`, `WorkflowAlert` and the injected `AlertLister`. WorkflowAlert is exactly the fifteen actual PG003 fields; there is no canonical Alert embedding, CH enrichment, invented evidence or exposed workflow sequence.
- The PG adapter applies verified tenant, raw half-open created_at range, status, severity, rule and literal summary/entity search before tuple ordering and `limit+1`. All values are bound parameters, with escaped backslash/percent/underscore. Caller context and the existing bounded store timeout reach the actual pool query. Native cancellation characterization passes.
- PG003 permits submillisecond timestamps. The selected created_at, ordering and cursor predicate all use `date_trunc('milliseconds',created_at)`. UTC millisecond range boundaries make raw range filtering consistent with these buckets. Native fixtures retain twenty-four actual submillisecond rows with deliberately reversed microsecond order inside buckets; four exact full sparse-status pages have no overlap and the hand-derived tuple order. No extra index or generic complexity/latency guarantee is claimed; truncation may require sorting within the bounded time range.
- Default range24h/max7d, default limit50/max200, cursor<=2048bytes, UTC Z timestamps with at most3fractional digits and search<=128raw UTF-8 bytes are enforced. Unknown/repeated/empty/malformed query fields fail400. Raw search controls/invalid UTF-8 are rejected before trimming; accepted search is a case-insensitive literal substring. A changed limit is also cursor-bound.
- Cursor payload v1 binds endpoint, verified tenant, normalized filters, frozen range/limit and last tuple. AES-GCM uses Go's random-nonce AEAD; HMAC-SHA256 derives a distinct cursor key from validated auth secret material with a fixed domain. Ciphertext authentication rejects mutation/wrong key/cross-tenant/endpoint/changed filters; public base64 alone grants no cursor authority. Key rotation invalidates old cursors. The standard random-nonce AEAD's documented per-key message limit applies; this slice claims no unbounded key lifetime or traffic guarantee. The narrow G407 lint suppression documents the API-required nil nonce, which generates/prepends a random nonce internally; no scanner allowlist changed.
- UI requests and exact query keys contain the same normalized current filters/range/cursor plus verified subject/tenant/role/generation. Explicit Apply controls status/severity/rule/time; search debounces300ms and aborts superseded requests through the existing typed client. Next/Previous retain at most20 prior filter/cursor objects and reset on filter changes. Browser history/deep links work without adding a router. Session-generation subtree removal clears current URL cursor; old entries retain nonsecret ownership markers so subsequent sessions discard their cursors.
- Cursor-only deep links deliberately omit absent from/to in request/key/URL and rely on the authenticated server range; no client cursor decoding or fresh time defaults are sent. Time inputs remain blank for those omitted opaque bounds. Changing filters establishes a fresh explicit range. Malformed time links remain recoverable without list requests. Existing loading/empty/error/retry, mobile/table data views, credential clearing and cache/body cancellation guards are retained.
- OpenAPI adds search and the2048cursor limit, documents filters/ranges/tuple normalization and leaves canonical schemas intact. Existing generator regenerates the ignored declaration; no force-add. Generated declaration SHA256: `e6bbab8336ea22bfe93d1105c741b048414d083eb0114de31a0922fe65f26b7d`.

## Task commits and causal evidence

1. `91b1ead` tests-only: actual HTTP filter acceptance and invalid page-query RED, plus absent UI controls RED; PG tuple/negative fixtures and unit cursor specifications precede implementation.
2. `835bb4b` tests-only: cursor-only link sends invented current from/to (observed assertion RED); correct new history/expiry/control-readiness test setup without removing intended behavior.
3. `8b48c10` tests-only: applying malformed time link raises actual Invalid time value error; owned test typing/format corrections.
4. `da3a5dc` tests-only: raw leading/trailing newline search returns200 instead of400; actual PG caller cancellation already passes and is characterized honestly.
5. `0abb49d` Task2 GREEN: bounded PG/query/cursor/API/UI implementation, approved strict App fixture adjustment and final regression refinements.

Cursor unit specifications could not execute before the new package existed; their initial passing run is not invented per-case RED. The actual HTTP/component RED above establishes causal missing feature behavior. Existing authentication hygiene and already-correct invalid cases are not relabelled as new defects.

The deliberate post-limit status-filter mutation was applied only temporarily to the owned PG file: exact native sparse-page assertion failed **2items versus3**, exit1. Correct status-before-limit SQL was restored immediately and later native tests pass. This is an effective negative control, not an accepted defect or an alternative implementation.

## Verification and preserved failures

All evidence is ignored under `.local/phase4-acceptance/04-04/`: directory0700, scripts/logs/reports/screenshots0600; build/test children use umask022. No actual key, token, Authorization header or resolved Compose configuration was printed or tracked. Native fixtures use unique synthetic tenants and independently random keys; retained data is not deleted.

| Log (October22 UTC) | Start–end | Exit and scope |
|---|---|---|
| native-red | 21:14:14–21:14:20 | 1; actual migrations/built API rejects valid filters400; empty/malformed page fields accepted200 |
| web-red | 21:15:28–21:15:31 | 1; ten absent filter/page control failures |
| native-first / cursor-first | 21:19:29–21:19:37 | 0; first native full-family / cursor unit GREEN |
| native-postlimit-negative | 21:21:01–21:21:06 | 1; exact2versus3 full-page assertion |
| web-first | 21:20:37–21:20:43 | 1;41/46, two obsolete retained key assertions plus three new fixture failures; subsequent chain commands not run |
| web-resume-diagnosis | 21:28:57–21:29:01 | 1;46/47, cursor-only fabricated-range assertion |
| web-range-green | 21:29:40–21:29:47 | 2;47tests pass then unsupported Testing Library exact option fails typecheck; later commands not run |
| web-malformed-range-red | 21:31:08–21:31:10 | 1; actual unhandled RangeError on Apply, despite body assertions passing |
| web-quality-green | 21:31:24–21:31:36 | 0;48/48 plus strict typecheck/lint/build/format, each120s |
| openapi-green | 21:32:29–21:32:33 | 0; exact nonempty11OpenAPI/race/60s, canonical drift/fixtures retained |
| native-search-controls-red | 21:35:49–21:35:55 | 1; two raw newline HTTP200versus400 failures; caller cancellation passes |
| static-clean | 21:38:02–21:38:30 | 0; cursor race60s, default race60s, vet/build/lint and phase4/integration vet/lint, each outer120s; some default packages explicitly cached |
| native-clean | 21:38:05–21:38:30 | 0; separate exact Alerts then Auth phase4/race/6m selectors, retained Auth26top-level/24CLI |
| native-filter-fixtures-final | 21:39:11–21:39:18 | 0; strengthened competing severity/rule, literal/case/entity/injection fixtures; exact Alerts3.832s plus phase4 vet/lint0issues |
| browser-final | 21:38:03–21:38:19 | 0; real current API/PG/Chromium flow below |
| stack-final | 21:39:12–21:39:27 | 0; all preserved services/dependencies/readiness/topics healthy |
| default-final | 21:53:03–21:53:12 | 0; bounded make test, default Go race, web48 and locked generator121 |
| staged-source-scan | 21:53:13 | 0; pinned8.30.1 staged source and ignored generated declaration, both reports exactly[] |

Exact native commands retain selector/tag/race/caps independently:

```sh
bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Alerts$' -tags=phase4 -race -timeout=6m -p=1
bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Auth$' -tags=phase4 -race -timeout=6m -p=1
```

The static scripts' complete commands and intermediate failures remain in logs. `go-quality-green` failed scoped new lint conventions; `go-quality-second` passed default but exposed phase4 test lint; `static-final` found the newly added validation branch exceeded complexity. The final parser validates raw search through its existing field-validation pass before trimming, and `static-clean` passes all three lint modes. No caps or lint configuration were relaxed. No failed chain's unexecuted commands are claimed as passing.

### Rendered QA

Browser plugin unavailable; existing cached Playwright1.64.0-alpha/Chromium1248 was used without downloads. Actual fresh race-built query-api runs on private loopback ports against preserved dependencies; a private Vite runtime proxy override reaches that binary. Checked-in Vite/Compose source and existing Compose application processes are unchanged. Helper/actual dev CLI supplies credentials privately. There are **no mocked session/list routes or payloads**.

At1440/375: correct Sentinel/Alerts identity and meaningful real PG results; full3row filtered first/next pages with distinct actual IDs; explicit Previous and native Back/Forward; cursor-only URL returns the exact same second-page IDs without invented bounds; debounced empty search, Clear filters and Sign out. All list responses observed200, controls44px, zero document overflow, overlay, console warnings/errors or page errors, no credential DOM/URL/storage exposure. Screenshots in `browser-final/` were visually inspected and show readable desktop table/mobile list. Time inputs are intentionally blank on cursor-only screenshots because ranges remain opaque server authority. API/Vite/Chromium processes were stopped and joined; only the owned temporary built binary/directory was removed after use.

`browser-first` also passed but had weaker cursor-only event timing; the final harness waits for that precise response before asserting exact second-page IDs. Its result is retained separately, not overwritten. Component tests mock only fetch; native and real-browser proof are distinct.

## Deviations and limits

- **[Rule4 — approved ownership amendment A1]** Two retained exact cache expectations conflicted with the newly required normalized range key. Execution stopped before editing the eleventh path. Independent A1 approval precedes the strict local HTTP helper and exactly two expectation replacements. Root owns approval artifacts; no additional production ownership was inferred.
- **[Rule1 — cursor-only range mismatch]** New causal component RED showed fresh defaults invalidating opaque cursor links. GREEN preserves omitted bounds while paging and initializes them only for new filters. Files: owned Alerts.tsx/test.
- **[Rule1 — malformed range and raw search controls]** Actual Apply RangeError and raw newline acceptance were separately reproduced before minimal fixes; final quality/native/component gates pass. Files: owned page/tests/query parser/native test.

No authentication gate or external configuration requirement occurred. Karpathy/TDD/debugging/verification guidance kept causal failures separate from setup/correct characterizations; React guidance kept complete request dependencies in keys and lifecycle cancellation; Postgres guidance led to keyset/filter-before-limit without speculative schema/index work. Approved UI-SPEC and existing manual CSS were reused. These scoped checks establish no benchmark, production security certification, complete accessibility, screen-reader/200%zoom/cross-browser, full Phase3 aggregate, wholePhase4/SSE/proxy/two-replica/tenant-matrix or hosted CI result. Those remain their numbered owners.

No goal-blocking stub/TODO or new unregistered endpoint/auth/file/schema trust surface was found in the owned source. T04-08/09 cover the new cursor/query boundary. Empty state/ref values are real state wired to the verified client, not fabricated data. Durable snapshot isolation across concurrent status edits is not promised; cursors preserve ordering/range/filter authority. No new source changes or deferred task repair remain pending locally; independent review/root acceptance are still required before04-05.

## Self-Check: PASSED

All eleven declared paths exist (including ignored generated schema); all five task commits exist in order, original RED precedes GREEN, scoped whitespace/deletion checks pass, and only pre-existing user dirt remains after source commit. Original36 tests remain with the bounded A1 amendment. Private logs record the named actual exits; reports for staged/generated source scanning are empty arrays. Source-history scan at0abb49d also exits0 with no leaks; the final committed-summary history scan follows separately. This is an executor claim check, not independent source acceptance. Root tracking files were not mutated by this executor.
