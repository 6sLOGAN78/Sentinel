---
phase: 04-query-api-analyst-dashboard
plan: "03"
status: pending_independent_source_review
subsystem: auth
tags: [react, typescript, jwt, react-query, openapi-fetch, accessibility]
requires:
  - phase: 04-02
    provides: Actual local development token CLI and verified server sessions
provides:
  - Generation-bound typed session client and tenant alert query cache
  - Memory-only credentials with session-wide cancellation and verified expiry
  - Responsive native analyst shell and real PostgreSQL alert views
affects: [04-04, 04-05, 04-15, 04-16, dashboard-authentication]
tech-stack:
  added: ["@tanstack/react-query@5.104.5", "openapi-fetch@0.17.0"]
  patterns: [verified-identity query keys, generation revocation, post-parse cancellation]
key-files:
  created:
    - web/src/api/client.ts
    - web/src/auth/session.tsx
    - web/src/auth/session.test.tsx
    - web/src/pages/Alerts.tsx
    - web/src/styles.css
  modified: [web/package.json, web/package-lock.json, web/src/App.tsx, web/src/App.test.tsx]
key-decisions:
  - "Use one QueryClient throughout the actual Alerts/session tree; allow a caller-supplied client without a separate test cache."
  - "Revoke generation before aborting requests and clearing cache; check cancellation again after body parsing."
  - "Display only delivered Alerts navigation; later pages, filters, cursor history and SSE keep their numbered owners."
patterns-established:
  - "A session client can obtain a credential only after its generation/lifetime check, and cannot return stale parsed results."
requirements-completed: [AUTH-01, AUTH-03, UI-02, UI-06]
duration: 19min recorded setup and verification span
completed: 2026-10-22
---

# Phase 4 Plan 03: Typed session isolation and analyst shell Summary

The Alerts page now reads real tenant workflows through generated OpenAPI requests and a verified-identity React Query cache, with cancellation and cache removal on expiry, sign-out, identity switch and unmount.

**PENDING INDEPENDENT SOURCE REVIEW.** Two implementation tasks are locally verified at source `6bb0ac84ef7d0267cc5a4f54ec8d8b7e0a6e9bc4`, based on accepted `21ebf9840c57edd3125ba0b8be3d7508c525d5e5`. This is a review candidate, not root acceptance or whole-Phase-4 completion. Requirement IDs describe incremental coverage only. Root owns STATE/ROADMAP/REQUIREMENTS, review reports and post-wave acceptance; none were updated by this executor.

## Performance and ownership

- Recorded setup/verification span: 2026-10-22T16:15:38.341399Z–16:34:17.543367Z, approximately 19 minutes. Initial instruction reading and final metadata work are outside that span; it is not measured active time or a benchmark.
- Tasks: 2/2 implemented and locally verified, awaiting independent source review.
- Exact source union: nine approved paths, five newly created. No backend, generated schema/type fixture, main entrypoint, Vite configuration, Make/Compose, scanner exception or tenth source path changed. No tracked file was deleted.
- User-owned `.planning/config.json` and `.serena/` remain untouched and unstaged. No agent/worktree, later plan, remote push or hosted CI was started.

## Delivered behavior and interfaces

- Exact dependency pins resolve and remain exact in package/lock files. Registry metadata reports React Query's React peer as `^18 || ^19`; the existing React 19.3.0 stack is retained. Lockfile changes add the two libraries and their two dependencies, plus npm's optional-peer metadata; existing package versions are unchanged. Actual `npm ci`, strict TypeScript compilation and audit pass with zero reported vulnerabilities. APIs were checked against installed source, Context7 TanStack documentation, [official Query cancellation guidance](https://tanstack.com/query/latest/docs/framework/react/guides/query-cancellation), and [openapi-fetch's official API](https://openapi-ts.dev/openapi-fetch/api).
- `api/client.ts` derives session, workflow-page and query parameter types from generated `paths`. Its two real methods are `session()` and `alerts(filters?, signal?)`. Requests use the current same origin, header Authorization, `cache: no-store`, omitted cookies and rejected redirects. Non-success statuses become a fixed `APIError`; raw service bodies are not rendered. Generation/lifetime checks run before and after fetch and after parsing; individual query cancellation is also checked before and after its typed request.
- `SessionProvider` supplies verified `session` (including a local monotonic `generation`), typed `client`, pending/error/retry state, and sign-in/retry/sign-out actions. No raw credential is exposed in context. One active ref owns the credential during verification or its authenticated lifetime; successful verification removes retry availability rather than retaining a second retry copy. Unverified expiry can only shorten the fixed 15-minute retry-retention ceiling. The separate authenticated deadline comes from `/session`; decoded subject/tenant/role never establish UI identity.
- Revocation increments generation first, empties the active credential, aborts the shared lifetime signal, cancels all queries and clears QueryClient (including its mutation cache). Every client request combines session and per-query signals. Old clients cannot acquire the replacement credential. A timer removes authenticated authority/data at verified expiry without needing a subsequent HTTP 401. A current authenticated 401 also signs out without credential retry. Uncooperative old fetch/body completions cannot return current data or repopulate the removed cache.
- App supplies one QueryClient to the actual SessionProvider and Alerts tree. Its optional caller-supplied QueryClient is the very same instance observed by component cache assertions; there is no nested probe cache. Current alert keys are `[session, verified subject, tenant, role, generation, alerts, {}]`; `{}` is the normalized current empty-filter set. Future filter/cursor variables must join that key when 04-04 delivers them. No cursor history exists yet; no unused placeholder history store was added. The Alerts subtree is keyed by session generation.
- All current data remains the actual bounded PG WorkflowAlert page, with its existing 15-field DTO; canonical detection fields are not fabricated. Native table and responsive labelled mobile list render the same query result, loading/empty/error states and explicit read retry. Text is escaped React content. Viewer copy is shown from verified role; no future mutation controls are invented.
- Approved manual CSS supplies system fonts, semantic colors, four font sizes/two weights, focus rings, skip/main/navigation landmarks, 44px controls and reduced-motion handling. Desktop uses the 224px sidebar and 64px header; the mobile header grows to wrap the real tenant identity. Only Alerts navigation is delivered; ordinary current-page navigation uses native history without reloading away the in-memory session. `/alerts` refresh starts signed out as required. Overview/Events/Rules, filtering/pagination/detail/SSE and their complete history interactions remain later slices, not fake pages or claimed delivery.

## Task commits and TDD provenance

1. Setup `53f16b6` — pin verified query/client dependencies. This is dependency setup, not behavioral RED.
2. Task 1 tests-only `c9a0986` — existing App renders an actual alert response, but three focused assertions fail because it does not populate the requested identity-scoped query cache. These are genuine cache behavior failures, not import/compiler errors. Existing visible expiry/abort behavior was already correct and is not relabelled as broken.
3. Task 1 tests-only `aa52e1d` — provider verification, concurrent cancellation, post-body stale identity and reload/expiry cases. Their first absent-module run is explicitly SETUP failure, not causal per-case RED.
4. Focused tests-only `14808d8` — while implementing the new client, a canceled individual query with uncooperative body parsing incorrectly resolves. Observed promise-resolution RED precedes the two pre/post-query signal checks.
5. Task 2 GREEN `6bb0ac8` — typed client, provider, real Alerts query, shell/CSS and retained/new component assertions.

The three original typed fixtures and every original App behavior remain covered. The final suite is 28 tests, including actual-cache identity/generation/expiry/signout/unmount assertions, delayed old-tenant responses after new login, stale parsed identities, concurrent request abortion, invalid and unavailable sign-in cases, retained retry ceiling/cancel cleanup, authenticated 401 clearing, explicit read retry, server-versus-unverified lifetime and mobile rendering. Existing fetch assertions were adapted to openapi-fetch's native Request boundary while retaining path/header/signal expectations and adding URL/no-store/omitted-cookie checks. No passing security characterization was presented as a new RED. No refactor commit or per-case RED is invented for modules that did not exist.

## Verification and private evidence

Raw evidence is ignored under `.local/phase4-acceptance/04-03/`, directory 0700 and every script/log/report/screenshot 0600. The wrapper records actual base SHA, command, UTC start/end and exit; test/build child umask is 022. Precommit logs identify their base plus the then-working changes subsequently committed as `6bb0ac8`; they do not claim that source already existed in the base SHA. No source changed after the final UI/component verification. Keys, real bearer headers/tokens and resolved Compose configuration were not printed or tracked.

| Evidence (UTC on Oct 22) | Actual command/result |
|---|---|
| `pins-setup.log`, 16:15:38–16:15:42 | Exact pinned npm install, 120s cap; exit 0, four added packages/audit zero. |
| `preserved-startup.log`, 16:15:54–16:18:01 | `timeout 16m make dev-up && timeout 120s bash scripts/check-stack.sh`; exit 0, all dependencies/five apps healthy, actual readiness/topic/dependency checks. PG/CH/Redis had been observed stopped; no cause, reset or teardown is inferred. |
| `app-cache-red.log`, 16:17:21–16:17:28 | Focused existing-App cache tests, 120s; exit 1 at three intended missing-cache assertions after visible real-component results. |
| `native-before.log`, 16:18:01–16:18:23 | Exact Auth target below; exit 0, package 19.403s, 26 top-level/24 nested CLI PASS cases. |
| `session-setup-red.log`, 16:19:02–16:19:07 | Provider test import fails because module is absent; exit 1, no executed tests, SETUP only. |
| `web-green-first.log` → `web-green-second.log`, 16:22:12–16:23:21 | Six old fetch-shape assertion failures → 21/21 tests plus typecheck/lint/build/format, exit 0. Original failures retained; no assertions removed. |
| `web-expanded.log`, 16:24:19–16:24:47 | 26/26 plus typecheck/lint/build/format, each command 120s; exit 0. |
| `backend-regressions.log`, 16:26:31–16:26:56 | Bounded full default Go race (`-timeout 60s`), vet, all-package build and lint; exit 0, lint zero issues, some packages explicitly cached. Backend source is unchanged. |
| `query-abort-red.log`, 16:27:54–16:28:00 | Focused canceled-query-body assertion, 120s; exit 1 because the promise resolves instead of rejecting. |
| `native-after.log`, 16:28:38–16:29:03 | Exact Auth target; exit 0, package 21.924s, retained 26 top-level/24 nested CLI cases. No backend/native source changed. |
| `web-final.log`, 16:28:37–16:29:37 | Clean `npm ci`, 27/27 including canceled-query fix, typecheck/lint/build/format, each 120s; exit 0, audit zero. |
| `web-mobile-final.log`, 16:30:18–16:30:49 | Final 28/28 plus typecheck/lint/build/format, each command 120s; exit 0. |
| `browser-mobile-final.log`, 16:30:20–16:30:26 | Actual Chromium/helper/CLI/Vite/API/PG flow, 120s cap; exit 0, 50 actual native-fixture workflows, four viewport and session checks below. |
| `contrast-final.log`, 16:31:14 | Actual semantic CSS colors, computed sRGB luminance: 11 required text/control/focus pairs pass; exit 0. Static color computation, not accessibility certification. |
| `openapi-final.log`, 16:31:42–16:31:50 | Nonempty `^TestOpenAPI`, race/60s/-p1; all 11 retained tests, package 3.957s, exit 0. Canonical/API schema unchanged. |
| `make-default-final.log`, 16:31:40–16:32:02 | `timeout 300 make test`; default backend race, web 28 and generator 121 tests pass, exit 0. |
| `source-history-scan.log`, 16:32:59–16:33:00 | Pinned gitleaks 8.30.1 full-history redacted scan at `6bb0ac8`; 507 commits, exit 0, report exactly `[]`, no exception change. |
| `evidence-audit.log`, 16:34:17 | Exit 0: nine source paths/five commits exist, no deletions, all private modes/key absence, ten required actual zero-exit logs and empty scanner report. |

Exact native invocation, unchanged tag/selector/race/deadline:

```sh
bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Auth$' -tags=phase4 -race -timeout=6m -p=1
```

Native Auth applies real PG migrations, builds actual race-enabled query-api/dev-jwt commands, and checks actual HTTP authentication/tenant/legacy-key/CH-unavailable behavior. Component requests alone are mocked at fetch; their actual React/query cache/provider remain real. No mock-only native acceptance is claimed.

## Rendered QA and retained failures

Browser plugin is absent. The private probe uses an already-installed cached Playwright `1.64.0-alpha-1790635538000` with explicit existing Chromium revision1248; no browser/package download or new project Playwright dependency occurred. Two initial launches expected missing revision1247 and failed SETUP (`browser-real.log`, `browser-matched-cache.log`). The explicit-browser attempt then reached the real page but failed an overly exact URL assertion after the keyboard skip link legitimately added a hash (`browser-explicit-chromium.log`); the fixture now checks the `/alerts` pathname. Neither launch failure nor hash assertion is a production auth defect. These exits remain recorded.

The actual flow is `/alerts` → private helper/local CLI token → masked sign-in → verified viewer → real Vite-origin session/list HTTP200 → 50 native-test PG workflows → current Alerts link → sign-out → reload signed out. No request routes are mocked. Page title/content, absence of Vite overlay, zero browser console warnings/errors/page errors, no credential in DOM/URL/storage, and owned browser/server shutdown all pass.

An initial successful screenshot run revealed compressed mobile table text despite passing overflow metrics. The final source implements the specified labelled mobile list from the same result. Final screenshots `alerts-mobile-final-375.png` and `alerts-mobile-final-1440.png` were visually inspected: mobile metadata is readable; desktop retains table/sidebar. Actual measured document widths equal 375/768/1024/1440 viewports, controls are at least 44px, and sidebar/header are 224px/64px at desktop widths. Keyboard skip reaches main; reduced-motion disables the transition. Final result data and measurements are in `browser-mobile-results.json`. No 200% browser zoom, screen-reader certification, future-page history, full proxy/SSE or full tenant-matrix acceptance is claimed; those remain 04-15/whole-phase work.

## Deviations and decisions

1. **[Rule 1 — Newly introduced query cancellation gap]** Found during Task 2: session-wide guards alone allowed an individually aborted query body to resolve. Tests-only `14808d8` demonstrates that exact promise-resolution failure; `6bb0ac8` checks its supplied signal before and after the typed request. Final 28-test and actual-browser gates pass. Files: owned `client.ts` and `session.test.tsx`.
2. **[Rule 3 — Existing stopped verification dependencies]** PG/CH/Redis were observed unavailable before native execution. Normal preserved `make dev-up` restored the existing stack and actual checks passed; no data removal, reset, prune, cap relaxation or source expansion.

Dependency setup is committed separately to keep the RED commits tests-only. Provider import failure, mechanical Request assertion adaptation, one corrected test-file parenthesis caught by formatter before execution, cached-browser setup and hash fixture correction are retained as setup/implementation facts rather than behavioral security REDs. The approved mobile-list refinement is within the planned UI-SPEC work. No architectural deviation, authentication gate, new threat acceptance or scope expansion occurred.

Karpathy/TDD/debugging guidance kept production within the nine paths and distinguished observed failures from already-correct behavior. React guidance kept credentials in refs and tied requests to session lifecycle. UI/UX guidance and the approved manual-CSS contract led to the responsive list and explicit color/focus/target checks. Verification guidance required actual outputs before the review handoff.

## Known stubs, threat surface and readiness

No goal-blocking stub or TODO remains in the owned implementation. Empty state/ref values are the real signed-out or empty-result state, with an actual verified HTTP data source. No fake later-page metrics/data, canonical fields, cursor history or stream status was introduced. T04-06 cache isolation and T04-07 memory-only credentials cover the new browser boundaries; no new backend endpoint, database schema or unregistered file-access surface was introduced.

Independent source review and root post-wave acceptance remain required before 04-04. Root must keep shared tracking at 3/17 accepted until review clears; this summary does not advance it. Full Phase 3 aggregate, complete Phase 4, hosted CI/publication and Phase 5 are outside this handoff. The normal preserved stack remains running; the probe's owned Vite/Chromium processes were stopped and joined.

## Self-Check: PASSED

Verified all nine owned paths and five task/setup commits, tests-only commits before GREEN, zero tracked deletions and scoped diff whitespace. Actual evidence audit confirms private directory/file modes, exact key absence, named zero exits and empty source-history report. Final working-tree changes outside this summary are only the pre-existing user configuration and `.serena/`. Summary commit and final metadata scan follow separately; this self-check validates local claims, not independent source acceptance.
