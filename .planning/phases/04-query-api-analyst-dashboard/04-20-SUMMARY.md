---
phase: 04-query-api-analyst-dashboard
plan: "20"
subsystem: web
tags: [gap-closure, G4, live-alerts, sse, dashboard]
gap_closure: true
gaps_closed: [G4]
requires: ["04-19"]
provides: ["first-load baseline flag on receiveLiveAlerts process callback", "Alerts banner only for post-load arrivals"]
affects: [web/src/api/live-alerts.ts, web/src/pages/Alerts.tsx]
tech-stack:
  added: []
  patterns: ["per-generation baseline flag: first complete backfill walk is history, not arrivals"]
key-files:
  created: []
  modified:
    - web/src/api/live-alerts.ts
    - web/src/api/live-alerts.test.ts
    - web/src/pages/Alerts.tsx
    - web/src/pages/Alerts.test.tsx
decisions:
  - "Baseline is client-side and per receiveLiveAlerts call (mounted generation); a remount that resumes from a committed cursor is a new page load, so its first walk is also baseline"
  - "Transient REST retries inside the first walk (before the first has_more=false page is processed) remain baseline; only pages after the walk completes (SSE, reconnect backfill) are arrivals"
  - "Cache invalidation (refetchType none) is kept for baseline pages; only latestArrival/setArrivals are skipped"
requirements: [UI-02, LIVE-01, LIVE-02]
metrics:
  completed: 2026-11-15
  tasks: 1
  files: 4
---

# Phase 4 Plan 20: First-load banner baseline (G4) Summary

Fixes G4. Each mounted generation's first complete backfill walk now counts as the load baseline: its pages reach the process callback with `initial=true`, and `Alerts.tsx` skips `setArrivals(true)` for them. After that, SSE frames and reconnect backfill pages pass `initial=false`, so they show "New alerts available" / "Refresh alerts". The API, schema and cursor semantics are unchanged.

## Commits

| Gate | SHA | Message |
|------|-----|---------|
| RED | bbd8d35 | test(04-20): add failing first-load banner baseline tests |
| GREEN | a59f365 | feat(04-20): suppress first-load banner for initial catch-up history (G4) |

No refactor commit was needed.

## RED evidence

Command: `cd web && npx vitest run src/api/live-alerts.test.ts src/pages/Alerts.test.tsx` gave 7 failed and 46 passed out of 53.
- The 2 live-alerts baseline cases failed because `initial` was `undefined` where `true` was expected.
- 5 Alerts cases failed with "expected `<p role="status">` to be null", because the banner showed after catch-up. These were the 2 new first-load cases, the 2 parametrised 503-retry cases and the 2^53 catch-up case.

## GREEN gates (run in web/, 2026-11-15 UTC)

| Command | Start | End | Exit | Result |
|---------|-------|-----|------|--------|
| `npx vitest run src/api/live-alerts.test.ts src/pages/Alerts.test.tsx` | - | - | 0 | 53/53 passed |
| `npm test` (runs gen:api first) | 23:56:45 | 23:56:51 | 0 | 9 files, 174 passed (was 170; +4 new cases) |
| `npm run typecheck` | 23:56:51 | 23:56:54 | 0 | clean |
| `npm run lint` | 23:56:54 | 23:57:00 | 0 | clean |
| `npm run format:check` | 23:57:00 | 23:57:03 | 0 | all files formatted |
| `npm run build` | 23:57:03 | 23:57:08 | 0 | built |

`grep -n "setArrivals(true)" web/src/pages/Alerts.tsx` shows line 203, which is guarded by `if (initial) return;` at line 201.

## Behavior proven

- **Live client (real `createAPIClient`, stubbed fetch):**
  - The first walk returns two pages (has_more true, then false), and both get `initial=true`.
  - The next SSE frame (seq 3) gets `false`.
  - After the stream closes, the reconnect backfill page (seq 4) gets `false`.
  - No-gap/no-repeat evidence: backfill `after_seq` values are exactly `["0","1","3"]`, stream `Last-Event-ID` values are `["2","4"]`, and the processed seqs are `["1","2","3","4"]`.
- **Remount:** using the same client with a committed cursor "1", the second generation's first page (seq 2, from `after_seq=1`) gets `initial=true`.
- **Alerts page with existing history** (two backfill pages, then stream attached with Last-Event-ID "3"):
  - No "New alerts available" text and no "Refresh alerts" button.
  - Exactly one list request.
- **Alerts page after load:** a single SSE delivery shows the banner and the Refresh button.
- **Foreign-tenant rejection** (T-04-20-02): the case still passes. Validation is unchanged.

## Deviations from Plan

### Updated existing tests whose expectations contradicted G4

These are not relaxations. Each test keeps its original purpose, and the only expectation that changed is the first-load banner that G4 removes.

1. **"recovers transient backfill 503 ..." (both params):** this test used to wait for the banner after recovering within the first walk. Under the plan's rule that pages before the first has_more=false page of the first walk are baseline, those pages are history. The test now waits for the stream to attach and the "Live" state, then asserts there is no banner. The retry and after_seq cursor assertions are unchanged.
2. **"retains a later arrival and stale query when an older refresh snapshot finishes":** the first and later arrivals used to come from backfill pages during the initial walk. They now come as SSE frames (seq 2, then seq 3 with the later alert while the refresh is pending), after a one-page baseline walk. The race being tested (an old refresh snapshot finishes after a later delivery, the notice is kept and the query stays invalidated) is unchanged. The expected backfill count changed from 2 to 1 because the arrivals now come over SSE.
3. **"catches up exact decimal delivery pages above 2^53 ...":** the test now asserts the banner is absent after the >2^53 catch-up. An exact-decimal SSE frame (9007199254740995) then shows the banner, and Refresh clears it. The cursor-order, row-bound, invalidation and no-storage assertions are unchanged.

To support these cases, the `start()` test harness in Alerts.test.tsx now exposes the stream controllers (`bodies`).

No other deviations. No dependencies were added, and nothing was persisted to storage or the URL.

## Threat model

- T-04-20-01 (accept): no new buffering. Pages stay at 200 items and the queue cap stays at 32.
- T-04-20-02 (mitigate): `validDelivery` and the tenant match are untouched, and the foreign-tenant case still passes.
- T-04-20-03 (mitigate): the cursor stays only in the in-memory WeakMap. Tests assert `localStorage`/`sessionStorage` are empty.

## Limits

- The first walk still costs O(history/200) requests per new session. 04-VERIFICATION accepted this as a bounded cost, and fixing it would need a server head-seq contract that this plan does not add.
- Web-only. No running stack or Playwright run was used. The verification here is Vitest/jsdom plus typecheck, lint, format and build.
- `web/src/api/schema.d.ts` was regenerated by `npm run gen:api`. It is gitignored and was not staged.

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: web/src/api/live-alerts.ts, web/src/api/live-alerts.test.ts, web/src/pages/Alerts.tsx, web/src/pages/Alerts.test.tsx
- FOUND commits: bbd8d35, a59f365
