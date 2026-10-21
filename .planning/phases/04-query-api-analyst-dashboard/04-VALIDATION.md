---
phase: 4
slug: query-api-analyst-dashboard
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-10-21
---

# Phase 4 — Validation Strategy

Contract only: no Phase4 tests have run. Planner must replace plan/task IDs below with final IDs and preserve the capability coverage. Existing Phase3 acceptance stays closed; this is not permission to relax its tests.

## Test Infrastructure

Go testing/race in `backend/`; Vitest/Testing Library in `web/`; native integration `backend/test/`; proposed pinned Playwright against a real preserved stack and same-origin proxy. Exact new version pins resolved before installation. No new global tools or external accounts.

Quick Go feedback once packages exist: `cd backend && go test -race -timeout 60s ./internal/auth/... ./internal/query/... ./internal/alertstream/...`. Component feedback: `cd web && npm test` (outer120s). Existing full unit gate `make test`; lint/build/schema gates remain.

## Sampling Rate

- Each behavioral task begins with a causal failing assertion, then minimal GREEN; retain real command/exit/SHA, not predicted results.
- After each task run its focused nonempty family plus affected unit/component checks.
- Every wave: run newly owned native family with prior wave regressions, web typecheck/lint/test/build and schema validation as affected.
- Before acceptance: full legacy gates, all new independent selectors, real proxy/two-replica/browser acceptance, security review and final independent verification.
- Quick loop budget60s Go/120s components; native ordinary family6m, delivery/stream10m, browser10m. These are hard planned caps, not measured performance claims. Do not increase old45minCI/32mfull caps to accommodate new work.

## Capability / Threat Verification Map

| Slice (final task IDs pending) | Requirements | Threat / secure behavior | Planned test command / existence |
|---|---|---|---|
| Sign-in / tenant list | AUTH01,02,03,04;API03;UI02,06 | forged/expiredJWT401,viewer403,verifiedtenantonly,redactedlogs | nonempty `^TestPhase4Auth$`; new files required |
| Events | API01,02,08;AUTH03;UI03,06 | tuple ties,immediate dedupe,SQL injection rejected,boundedranges,cross-ID404 | `^TestPhase4Events$`; new |
| Alert filtering/detail | API03,04,08;AUTH03;UI02 | status filter beforelimit,tenant evidence scope,expired evidence honest | `^TestPhase4Alerts$`; new |
| Triage | API05;AUTH02,03,04;UI02 | concurrentversiononewinner,auditfailurefullrollback,foreignIDs404 | `^TestPhase4Triage$`; new |
| Summary/rules | API06,07,08;UI01,04 | realdedupedcounts,tenant scope,noinventedzeros | `^TestPhase4SummaryRules$`; new |
| Durable delivery | LIVE01,02,03 | delayed lowerworkflowseqstilldelivered,counterrollback,restart/preexistingrows | `^TestPhase4Delivery$`; new |
| Stream browser | LIVE01,02,03;UI02,06 | two replicas eachdeliver;lostLISTEN/NOTIFY recover;boundedslowclient;auth expiry | `^TestPhase4Stream$`; new |
| Full isolation | AUTH03;allAPI/LIVE | no foreign IDs/cursors/evidence/cache/stream content on all surfaces | `^TestPhase4TenantMatrix$`; new |
| Browser/proxy | UI01,02,03,04,06;LIVE01,02,03 | keyboard,escapedJSON,reconnect fromsamecursor,noURLtoken,logoutcacheclear | `cd web && npm run test:e2e`; new script/spec required |

Runner: `scripts/check-phase4-tests.sh` must check each exactselector independently and fail empty/failure. Use `scripts/go-test-nonempty.sh` conventions. Before introducing tests verify their build-tag/package location doesn't silently overload existing `make test-integ`; explicitly isolate new acceptance if needed. Preserve all7Phase2groups and44core+2scenariofamilies with originalbudgets. CI source guards must prove exact selector union and negative empty/failing groups; a MODEL is not native success.

## Wave 0 Requirements

- [ ] Add owned test packages/fixtures as part of their vertical slice; no empty stubs counted as coverage.
- [ ] Resolve pinned Playwright plus Chromium through lockfile-owned tools; real browser test harness with private runtime JWT fixtures.
- [ ] Apply additive migrations through actual existing migrator before native tests; verify preexisting rows preserved and migration repeat safe.
- [ ] Native proxy image/config pinned, bufferingoff, forwarded headers trusted only from configuredproxy, no publicsecret exposure.
- [ ] Tests-first references and finite runners mapped by finalplan task IDs before marking ready.

No reset,truncate,drop,deletevolumes,OPTIMIZE,goldenregeneration or committedcredentials to simplify acceptance. Test data uniquelyscoped to reserved synthetic identities. Files modified by user (`.planning/config.json`,`.serena/`) remain outofscope.

## Manual-Only Verifications

Primary behaviors automated. Final visual review should inspect fourpages at375/1440px,200%zoom,keyboard-only and reducedmotion; record screenshots and actual observed defects without claiming automated accessibility certification. If unavailable, leave explicitlypending rather than callPASS. No manual exception for native auth/replay/atomicity/tenant requirements.

## Validation Sign-Off

- [ ] All tasks have exact automated verify and known path/dependency.
- [ ] No three consecutive tasks without automated feedback.
- [ ] Missing tests/harness owned by initialslice, not waived.
- [ ] Every selected test family nonempty and assertionfailures propagate.
- [ ] Native / model / static / browser evidence distinguished.
- [ ] Planning contract linked by finalplans; no execution pass claimed.

Approval: pending plan checker. `nyquist_compliant` may reflect approved sampling design, not pretend testexecution; actual wave_0 completion remains false until artifacts exist and are verified.
