---
phase: 05-observability-resilience-scale
plan: "16"
subsystem: web
tags: [dashboard, system-page, consumer-lag, rbac, operator-tenants, accessibility, react-query]
requires:
  - phase: 04-query-api-analyst-dashboard
    provides: "App shell, session/client, Overview, Rules test pattern (04-16 closed)"
  - phase: 05-observability-resilience-scale
    provides: "GET /api/v1/metrics/lag (05-15), GET /api/v1/system (05-21), operator-tenant allowlist with operator_only rows and a separate 403 (05-21a, ADR 0014)"
provides:
  - "client.system() and client.consumerLag() with discriminated-union shape validation (APIError 503 on malformed bodies)"
  - "APIError.serverMessage on 403 (compared to fixed strings only, never rendered)"
  - "System page at /system with every UI-SPEC state, admin-only nav entry"
  - "Overview consumer-lag panel for analyst and admin, operator_only rendering, viewer notice"
affects: [05-19 docs, 05-20 Task 3 live visual check]
tech-stack:
  added: []
  patterns: [per-field availability badge instead of a number, client-side staleness from dataUpdatedAt, render-time state adjustment for transition-only live announcements, polling stopped on a final 403]
key-files:
  created:
    - web/src/pages/System.tsx
    - web/src/pages/System.test.tsx
  modified:
    - web/src/api/client.ts
    - web/src/App.tsx
    - web/src/App.test.tsx
    - web/src/pages/Overview.tsx
    - web/src/pages/Overview.test.tsx
    - web/src/styles.css
key-decisions:
  - "The two System 403s are told apart by the server message; the operator refusal shows 'Operator access required' with the ADR 0014 copy, while the role refusal and any other 403 show 'Admin access required'"
  - "The page-level 'No metrics recorded yet' empty state appears only when every Prometheus field is no_data; on a healthy stack, absent counters show only the per-field 'No data yet' badge"
  - "Polling stops after a 403, so refusals are not re-audited every 15 s"
  - "Lag rows with operator_only show an 'Operator only' badge with a title tooltip plus a visible explanatory note linked by aria-describedby, never an error, a zero or 'no data'"
  - "The 'Refresh overview' button also refetches lag when the role may read it; the queries stay independent"
metrics:
  duration: "~1h of active execution (split across a usage-limit pause)"
  completed: 2026-11-19
---

# Phase 5 Plan 16: System Page and Overview Consumer-Lag Panel Summary

This plan delivers the client half of UI-05. Admins get an admin-only System page at `/system` showing real readiness, replicas, restarts, throughput, errors, lag and latency, polled every 15 s. Overview gets a real consumer-lag panel for analysts and admins. ADR 0014 adds the operator-tenant allowlist: a non-operator admin gets a distinct "Operator access required" state, and non-operator lag rows read "Operator only". An unavailable field never renders as 0.

## Commits

| Commit | Type | Description |
| --- | --- | --- |
| e72c409 | test (RED) | Failing tests: System page (19 `it`/`it.each`), admin nav gating, Overview lag panel. Tests only. |
| 354e727 | feat (GREEN) | `client.system`/`consumerLag`, `System.tsx`, nav and route, Overview lag panel, CSS. Adds the harness fidelity fixes and a 403 polling test. |

RED check: every new test failed because the feature was missing. `/system` fell through to Overview, so tests got "Could not load overview" or had no System heading. There was no System link and no "Consumer lag" region. The viewer and analyst nav tests passed in RED, which is correct because they encode existing behaviour that must not regress. No test failed for an unrelated reason.

## Tests

- Web suite: 174 tests before, 215 after (+41). System.test.tsx has 27, Overview.test.tsx 17 (was 6) and App.test.tsx 33 (was 30).
- System covers:
  - Forbidden states:
    - viewer and analyst deep links send no request
    - an admin with the role 403 sees "Admin access required"
    - an admin with the operator 403 sees "Operator access required"
  - Full render: the copy and every number format:
    - `1,234.5`, `1,200.0 events/s`, `0.42%`, `1.3 requests/s`, `245 ms`, `1.25 s`, `12,345` and a measured `0` of lag
  - Headline and badge mapping, five cases (including degraded plus unavailable)
  - Prometheus down: notice, readiness still shown, no zeros (scans the rendered text)
  - Every field no_data: badges plus the empty state
  - Partial no_data: badges only, measured 0 shown
  - Loading: header kept, no digits in main
  - Error with no data, then Retry
  - Malformed bodies are rejected:
    - only 4 services
    - an unavailable metric that carries `value: 0`
  - Timing and the live region:
    - 15 s polling (fake timers)
    - background refetch failure: values kept, stale notice, "Live refresh failed", then "Live refresh restored"
    - stale after 60 s from the last client success even when `observed_at` is in the future
    - no polling after a 403
  - Refresh button: primary, disabled while fetching, announces "System status updated"
  - Grafana link: https and http render; absent or `javascript:` does not
  - Below 600px: card list
  - Sign out clears the cache and aborts the request
- Overview covers:
  - Viewer: notice, no lag request, no digits
  - Analyst and admin: `<dl>` values, with the admin-only "View system health" doing in-app navigation; no `/api/v1/system` request before that click
  - operator_only for analyst and admin: 3 "Operator only" badges, a tooltip plus a described note, no alert, no digits, no Retry
  - prometheus_unavailable and no_data rows: copy plus a secondary Retry that refetches
  - Lag 503: an alert, and the summary still renders
  - Summary 503: lag still renders
  - Lag 403: the analyst-role notice
  - Two-row lag body: rejected
- Mutation check: putting `refetchInterval: 15000` back makes the 403 polling test fail (4 requests instead of 1).

## Gates

| Gate | Result |
| --- | --- |
| `bash scripts/check-resources.sh` (before build) | PASS (13.32 GiB free on repo and Docker root, 6.53 GiB MemAvailable) |
| `cd web && npm test` (runs gen:api) | 215/215 passed |
| `cd web && npm run lint` | clean |
| `cd web && npm run typecheck` | clean |
| `cd web && npm run build` | built (324.79 kB JS, 5.88 kB CSS) |
| `make test` | exit 0 (Go packages ok, web 215, Python 363) |
| `make lint` | exit 0 |
| Browser or e2e | none in this plan; live visual check is 05-20 Task 3 |
| Compose runs | none needed (no real-container step in this plan) |

Acceptance greps:
- `grep -rn "available in Phase 5" web/src` and `grep -rn "requires the admin role" web/src` match nothing in production code. They match only negative test assertions (`not.toContain`) and the server 403 fixture in System.test.tsx that reproduces the backend's real message. The plan's literal "returns nothing" cannot hold for tests that assert absence.
- `session.role === "admin"` appears in App.tsx, System.tsx and Overview.tsx.
- `consumerLag` appears in Overview.tsx.
- CSS custom-property count: 15 before, 15 after. No new tokens, sizes or weights.

## Adopted checker recommendations

1. The result breakdown label reads "Dependency unavailable".
2. Headline and badge: "All services ready" pairs with the success "Ready" badge. "{n} of 5 services not ready" pairs with critical "Not ready" if any service is not_ready, otherwise warning "Degraded". "Service health unavailable" pairs with the muted "Unavailable" badge.
3. n counts every readiness state that is not ready (degraded, not_ready and unavailable), and m = 5.
4. Staleness is `isError && data` or `Date.now() - dataUpdatedAt > 60 s`, checked on a 5 s timer. "Updated" and the stale notice show the server `observed_at`.
5. The header (h2 and description) stays rendered during loading. Only "Loading system status…" shows, with no toolbar or numbers.

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 2] Operator-tenant 403 handling (ADR 0014, approved after the plan)**
- **Issue:** both System 403s use the code `FORBIDDEN`. The UI cannot know operator status before the request.
- **Fix:** the fetch wrapper reads at most a 4 KiB 403 body into `APIError.serverMessage`. It re-checks session currency after the read. The message is only compared with "System status is limited to operator administrators." and never rendered. Lag rows marked `operator_only` render "Operator only".
- **Commit:** 354e727

**2. [Rule 1/2] No polling of a final 403**
- **Issue:** a literal `refetchInterval: 15000` keeps polling after a 403, which writes an audited refusal every 15 s.
- **Fix:** `refetchInterval: (query) => (isRefusal(query.state.error) ? false : 15000)`. The plan grep `refetchInterval: 15000` therefore no longer matches literally; the 15000 value is on the same line. A test covers it.
- **Commit:** 354e727

**3. [Rule 3] Test harness fidelity (no assertion relaxed)**
- **Issue:** under StrictMode, React Query cancels and repeats the first fetch when the effect replays. The stub answered the cancelled request and used up sequenced responses.
- **Fix:**
  - The stub now rejects already-aborted requests, as real `fetch` does.
  - StrictMode is kept for the full-render and sign-out tests, which do not count requests.
  - Counting and sequencing tests render without StrictMode, like the existing Overview tests.
- **Commit:** 354e727

**4. CSS beyond the three listed rules (no new tokens)**
- **Issue:** the plan names `.surface`, but that class does not exist. The badge pairs, warning notice and visually hidden text also need classes.
- **Fix:** added these classes, all using existing tokens and the existing 14/16/20/28 sizes and 400/600 weights:
  - `.metric-card`, `.metric-unit`, `.metric-pairs`
  - `.badge-success`, `.badge-warning`, `.badge-critical`
  - `.notice`, `.notice-warning`, `.toolbar`, `.status-row`
  - `.system-section`, `.visually-hidden`

**5. Grafana link accessible name**
- **Issue:** dom-accessibility-api drops a leading space inside an inline span, giving the name "dashboards(opens…".
- **Fix:** the space now sits in the preceding text node, so the name is "Open Grafana dashboards (opens in a new tab)".

### Interpretation choices

- Readiness and replicas follow the real schema: `readiness.{ready_replicas, probed_replicas}`, `replicas_up` and `expected_replicas`. The plan's interface sketch used older field names. When `expected_replicas` is 0 (not declared), only the up count is shown.
- The System lag table shows bare grouped integers because the column header carries "(records)". Overview shows `12,345 records`.
- Rates use one decimal everywhere, so a measured zero rate reads `0.0 events/s` and a measured zero lag reads `0`.

## Known Stubs

None. Every value comes from the live endpoints, and no fixture or demo data ships.

## Threat Flags

None. The only new surface is reading a 403 error body. It is size-capped, checked for session currency and compared only with fixed strings (T-05-16-02 holds: React text nodes only, no `dangerouslySetInnerHTML`). T-05-16-03 holds: the Grafana link renders only for the `http:` and `https:` protocols, with `noopener noreferrer`.

## Notes for 05-19 (docs)

- The UI copy for the operator refusal is "Operator access required" / "System status is limited to operator administrators. Your tenant is not an operator tenant."
- The copy for non-operator lag is "Operator only" plus "Consumer lag is platform-wide and visible only to operator tenants."

## Manual visual checks for 05-20 Task 3

Run these on the isolated bench or observability stack with real data. Mint `--tenant demo --role admin` (operator), `--tenant demo2 --role admin` (non-operator), `--tenant demo --role analyst` and `--tenant demo --role viewer`.

1. **Operator admin, 1440px:**
   - The System link sits after Rules, and `aria-current` marks it on `/system`.
   - The status headline and badge colour match readiness.
   - The services table has 5 rows in fixed order.
   - The card grid is 3–4 columns.
   - "Updated HH:MM:SS UTC" advances about every 15 s while the tab is visible.
   - Counters with no increments read "No data yet" (http_errors.*, dlq_rate, non-accepted results), and none shows 0.
   - The Grafana link appears only if `grafana_url` is configured, opens a new tab, and the screen reader says "(opens in a new tab)".
2. **Responsive and zoom:**
   - At 768px the card grid is 2 columns.
   - At 375px the services render as cards with an 80px label column, the lag table stays 2 columns, and there is no horizontal page scroll.
   - At 200% zoom on 1280px there is no horizontal scroll, and long identifiers wrap.
3. **Keyboard only:**
   - Tab order is skip link → nav → Refresh → Grafana → (stale Retry).
   - The 2px focus ring is visible on each.
   - Refresh is disabled while fetching.
   - Focus does not move on a poll.
4. **Failure states:**
   - Stop Prometheus: the "Metrics source unavailable…" notice appears, readiness still shows and the values become "Unavailable".
   - Stop query-api briefly: the warning-pair stale notice "Showing data from … Live refresh failed." appears with Retry, values stay visible and a screen reader announces "Live refresh failed" once.
   - Restore query-api: "Live refresh restored".
5. **Non-operator admin (demo2):**
   - The System link is visible.
   - `/system` shows "Operator access required" with the ADR 0014 body, and there is no Refresh button.
   - Overview lag shows three "Operator only" badges, the tooltip on hover and the visible explanation, with no red error or zeros.
6. **Analyst (demo):** there is no System link, and Overview lag shows real per-group `N records` (a measured 0 reads `0 records`) with no "View system health". `/system` shows "Admin access required".
7. **Viewer:** Overview lag reads "Consumer lag requires the analyst role." The network tab shows no `/api/v1/metrics/lag` or `/api/v1/system` request.
8. **Colour and contrast:** the success, warning and critical badge pairs and the warning notice reach at least 4.5:1, and each badge carries its word.
9. **Reduced motion:** with `prefers-reduced-motion`, nothing animates.

## Self-Check: PASSED

- FOUND: web/src/pages/System.tsx, web/src/pages/System.test.tsx
- FOUND: commits e72c409, 354e727
