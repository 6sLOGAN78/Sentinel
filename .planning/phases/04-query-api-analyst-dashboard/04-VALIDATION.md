---
phase: 4
slug: query-api-analyst-dashboard
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-10-21
---

# Phase 4 — Validation Strategy

Samplingcontract: original16plansapprovedR2;04-01nowhasactualnative17/web9/static/OpenAPIpassesbutindependentcodereviewreopenedcurrentintegrationacceptance. Inserted04-17repairbefore04-02awaitsplancheck;notallPhase4testsrun. ExistingPhase3acceptancestaysclosedbutnewregressionsmustfix,nopermissiontorelaxoldtests.

## Test Infrastructure

Go testing/race in `backend/`; Vitest/Testing Library in `web/`; native integration `backend/test/`; proposed pinned Playwright against a real preserved stack and same-origin proxy. Exact new version pins resolved before installation. No new global tools or external accounts.

Quick Go feedback once packages exist: `cd backend && go test -race -timeout 60s ./internal/auth/... ./internal/query/... ./internal/alertstream/...`. Component feedback: `cd web && npm test` (outer120s). Existing full unit gate `make test`; lint/build/schema gates remain.

## Sampling Rate

- Each behavioral task begins with a causal failing assertion, then minimal GREEN; retain real command/exit/SHA, not predicted results.
- After each task run its focused nonempty family plus affected unit/component checks.
- Every wave: run newly owned native family with prior wave regressions, web typecheck/lint/test/build and schema validation as affected.
- Before acceptance: full legacy gates, all new independent selectors, real proxy/two-replica/browser acceptance, security review and final independent verification.
- Quick loop budget60s Go/120s components; native ordinary family6m, delivery/stream10m, browser10m. These are hard planned caps, not measured performance claims. Do not increase old45minCI/32mfull caps to accommodate new work.

## Final Task / Threat Verification Map

| Plan/tasks (1 RED,2 GREEN) | Requirements | Threat / secure behavior | Planned test command / existence |
|---|---|---|---|
| 04-01-1/2,04-02-1/2,04-03-1/2 | AUTH01,02,03;API03;UI02,06 | T04-01..07 verifiedJWT/tenant,mintguard,cacheclear | `^TestPhase4Auth$`; new; componentApp/session |
| 04-04-1/2,04-05-1/2 | API03,04,08;AUTH03;UI02,06 | T04-08..12 status-beforelimit,cursor/evidence/DOMsafety | `^TestPhase4Alerts$`; new |
| 04-06-1/2,04-07-1/2 | API01,02,08;AUTH03;UI03,06 | T04-13..16 immediateCHdedupe,keycompatibility,JSONsafety | `^TestPhase4Events$`; new |
| 04-08-1/2,04-09-1/2 | API05,04;AUTH02,03,04;UI02,06 | T04-17..20 atomicCASaudit,role403,redactedproducerlogs | `^TestPhase4Triage$` plus `^TestPhase4Auth$`; new |
| 04-10-1/2,04-11-1/2 | API06,07,08;AUTH03;UI01,04,06 | T04-21..23 readonlyrules/realdedupedsummary/tenant | `^TestPhase4SummaryRules$`; new |
| 04-12-1/2 | LIVE02,03;AUTH03;UI02,06 | T04-24..26 reversedcommit/counterrollback/PGonlybackfill | `^TestPhase4Delivery$`; new |
| 04-13-1/2,04-14-1/2 | LIVE01,02,03;AUTH03;UI02,06 | T04-27..31 twoactualreplicas/slowreader/expiry/LISTENkill | `^TestPhase4Stream$`; new |
| 04-15-1/2 | All20Phase4requirements | T04-32..34 fulltenantmatrix andrealproxybrowser | `^TestPhase4TenantMatrix$` and `bash scripts/check-phase4-browser.sh`; new |
| 04-16-1/2; publication04-16-3 | All20Phase4requirements | T04-35..36 completefailclosedCIunion/scopedauthority | `bash scripts/check-phase4-tests.sh all`,browser+legacygates; new |
| 04-17-1/2/3 (wave2,blocks04-02) | AUTH01,03;API03;UI02,06 | CR01/02/WR01..03 privatekeyhandoff/actualViteAPIroute/503retry/validHS384/exactredaction | helperPythonnegativecontrols;literalquery-apilifecycle;retained`TestReadyz|TestHealthz|TestBinaryLifecycle`5m;nativeAuth6m;actualmake dev-up/check-stack/seed16m;allpendingrepair |

Native command for each row: `bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4FAMILY$' -tags=phase4 -race -timeout=6m -p=1`; use10m for Delivery/Stream. Replace FAMILY with the literal family shown, not a broad union. Each task's automated command is the fail-closed GREEN target. RED runs are separate invocations with each actual exit and assertion retained, not semicolon-masked aggregates. Eight complete families run again in04-16.

Runner: `scripts/check-phase4-tests.sh` must check each exactselector independently and fail empty/failure. Use `scripts/go-test-nonempty.sh` conventions. Before introducing tests verify their build-tag/package location doesn't silently overload existing `make test-integ`; explicitly isolate new acceptance if needed. Preserve all7Phase2groups and44core+2scenariofamilies with originalbudgets. CI source guards must prove exact selector union and negative empty/failing groups; a MODEL is not native success.

## Wave 0 Requirements

- [ ] 04-01-1 owns phase4-tagged native fixture embeddedin auth_test.go; tests use actualbuiltquery-api process overHTTP sincecmd/main notimportable. No empty stubs counted as coverage. Laterfamilies own focusedtestfiles.
- [ ] 04-03-1 owns browser session/componentfixtures; 04-15-1/2 own pinnedPlaywright+Chromium/privateJWTrealbrowserharness.
- [ ] 04-08-2 applies007auditmigration;04-12-2 applies008deliverymigration withpreexisting/concurrentcutoverproof;noSQL-onlyverification.
- [ ] 04-15-1/2 own pinnedproxyimage/config/composeoverlay andcausalbufferingnegativecontrol.
- [ ] 04-16-1/2 ownclosed8familyrunner,negativeMODELguards andpreservedlegacyCIunion/caps.

Wave0 is a dependency label, not a separate foundation-only plan: artifacts arrive with each real feature slice. References are all owned above, execution stillpending.

No reset,truncate,drop,deletevolumes,OPTIMIZE,goldenregeneration or committedcredentials to simplify acceptance. Test data uniquelyscoped to reserved synthetic identities. Files modified by user (`.planning/config.json`,`.serena/`) remain outofscope.

## Manual-Only Verifications

Primary behaviors automated. Final visual review should inspect fourpages at375/1440px,200%zoom,keyboard-only and reducedmotion; record screenshots and actual observed defects without claiming automated accessibility certification. If unavailable, leave explicitlypending rather than callPASS. No manual exception for native auth/replay/atomicity/tenant requirements.

## Validation Sign-Off

- [x] All32implementationtasks have fail-closed automated targets and concrete owner/dependency.
- [x] No three consecutive tasks without automated feedback.
- [x] Missing tests/harness owned by numbered slices, not waived.
- [ ] Every selected test family nonempty and assertionfailures propagate.
- [ ] Native / model / static / browser evidence distinguished.
- [ ] Planning contract linked by finalplans; no execution pass claimed.

Approval: original16plans/32implementationtasks independentR2PASS; inserted17adds3taskswithautomatedtargets/ownedfixtures,awaitingindependentrepairplancheck. `nyquist_compliant:true` retainsoriginalsamplingdesignapproval,notrepairacceptance orwholephaseexecution; wave_0_complete remainsfalse. Actual04-01proofinSUMMARYandreviewcounterevidencein04-REVIEW. No wholePhase4PASSclaimed.
