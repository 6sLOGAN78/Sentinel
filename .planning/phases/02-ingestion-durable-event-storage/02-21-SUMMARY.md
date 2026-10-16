---
phase: 02-ingestion-durable-event-storage
plan: "21"
subsystem: testing
tags: [acceptance, kafka, clickhouse, seed, verification]
requires:
  - phase: 02-14..20
    provides: Seven committed safety/correctness repair plans and behavioral regressions
provides:
  - One-shot bounded full local acceptance with seven independently nonempty CI groups
  - Two exact current-run 1000-event seeds and preserved persistent-volume evidence
  - Additive repair-to-finding evidence and pending independent/hosted verdict tracking
affects: [phase-02-verification, phase-02-security, phase-02-code-review]
tech-stack:
  added: []
  patterns: [separate local and independent verdicts, immutable historical report handoff]
key-files:
  created:
    - .planning/phases/02-ingestion-durable-event-storage/02-GAP-ACCEPTANCE.md
    - .planning/phases/02-ingestion-durable-event-storage/02-21-SUMMARY.md
  modified:
    - .planning/phases/02-ingestion-durable-event-storage/02-VALIDATION.md
key-decisions:
  - Local repair acceptance can pass while independent phase assessments and hosted CI remain pending.
  - Preserve historical failed report bytes and passed validation rows; append new observed repair evidence.
requirements-completed: [ING-01, ING-02, ING-03, ING-04, ING-05, ING-06, ING-07, STOR-01, STOR-02, STOR-03, STOR-04, GEN-01, GEN-03, OBS-01]
duration: 16min26s
completed: 2026-10-17
---

# Phase 02 Plan 21: Bounded Repair Acceptance Summary

One complete local gate passed in 809 seconds, including all seven CI integration groups and two exact 1000-event tenant-isolated seeds, while independent phase and hosted CI verdicts remain pending.

## Performance

- Recorded canonical-check start: `2026-10-16T19:48:26Z`.
- Final evidence verification: `2026-10-16T20:04:52Z` (2026-10-17 Asia/Kolkata); recorded execution window **16m26s**.
- Full gate: `19:49:19Z`–`20:02:48Z`, **809s / 13m29s**, exit **0**, outer limit **45m**.
- Tasks: **3/3**. Owned documentation files created/modified: **3**, including this summary. No application source changes by this executor.

## Task Commits

1. Task 1 — verify canonical story and pending verdict: `4e1c693` (`docs`). Root's preceding registered status correction is `b2eb387`.
2. Task 2 — record bounded full gate and exact seed results: `4acddb2` (`docs`).
3. Task 3 — map repaired gaps and preserve pending independent verdicts: `2b628a8` (`docs`).

Shared STATE/ROADMAP/REQUIREMENTS mutations belong to the root orchestrator by the explicit plan/task assignment. This executor did not invoke summary-count completion, phase completion or requirements mutations. The `requirements-completed` frontmatter copies plan provenance as required; it does not replace independent assessment.

## Verification and Accomplishments

All command exits and measured stage timings are recorded in [02-GAP-ACCEPTANCE.md](02-GAP-ACCEPTANCE.md).

- SDK canonical story validation returned **valid=true**; persisted exact goal, `mvp`, five success criteria and all 22 plans were verified. The initially incorrect completed state was corrected by root using the registered state handler; final observed state was executing with independent assessment pending.
- `make lint test` passed: default Go lint/race suites, web lint/typecheck and 6 tests, generator ruff/format/mypy and **54 tests**. Some unchanged Go unit packages used legitimate cached results, explicitly distinguished from fresh `-count=1` integration runs.
- Default/integration `go vet`, integration-tag golangci-lint (**0 issues**), dual actual dependency graphs, **25 script tests**, actual harmless Docker context export, pin and literal-dotenv checks all exited 0.
- `make test-integ` passed, package **253.377s**, wall **256s**. It ran once after repairs with its existing 10-minute test timeout.
- Seven exact CI nonempty selections passed independently: keyctl/API **34.852s** (3 top-level tests), ingest **37.708s** (7), dedupe/schema/TTL **26.583s** (5), writer/DLQ **53.507s** (5), recovery **74.625s** (3), rate limit **18.690s** (6), readiness/lifecycle **67.567s** (9). Each wrapper requires passing tests and runs with `-count=1`.
- Exact `TestWriterSlowPoisonRebalance` passed in **32.403s**; exact `TestTopicRetentionOverridesReapply` passed in **64.612s**, each under a 3-minute ceiling.
- Default command build and a temporary-path healthprobe build passed. `go list` selected only `[failpoint.go writer.go]` and `[consumer.go dlq.go failpoint.go producer.go]`; inspected `failpoint.go` shims use `!integration` and no failure environment settings. Neither integration control file entered the default build.
- Normal `make dev-up` rebuilt healthy apps; `scripts/check-stack.sh` passed with effective configured policies. Consecutive `timeout 10m make seed` runs took **25s** and **23s**. Both reported **sent=accepted=visible=1000**, **api_spotchecks=3**, **tenant_isolation=1**, **retries=0** for their complete newly loaded manifest ID sets.
- Correctly labeled Sentinel volume snapshots before startup/after both seeds matched all three names and original `CreatedAt` values from 2026-10-11. Keys and final manifest remained mode **0600**. No reset, topic deletion, volume removal or `OPTIMIZE` was used.
- `gsd-sdk query verify.key-links .../02-21-PLAN.md`: **3/3** verified, exit 0. Documentation whitespace/nonempty checks passed.

## Behavioral RED/GREEN Provenance

This acceptance/metadata plan adds no behavior or new RED test. Original behavioral RED observations and subsequent GREEN evidence are inherited from the committed repair summaries; they were not fabricated or rerun against reverted production code. Fresh final GREEN coverage is listed above.

| Repair | Named behavioral failure before repair → observed GREEN |
|---|---|
| [02-14](02-14-SUMMARY.md) | `TestBuildDLQRecordEncodedHeaderFlood`: four encoded-size assertion failures (exit 1), then same regression exit 0 and destination-topic/header-boundary cases green. |
| [02-15](02-15-SUMMARY.md) | `TestWriterAggregatePoisonBudget` / rebalance/config budget assertions failed behaviorally; real `TestWriterSlowPoisonRebalance` failed the six-second ownership assertion; explicit-consumer-close regression also failed. Repairs and same tests passed; exact real regression passed freshly in this gate. |
| [02-16](02-16-SUMMARY.md) | `TestOpenAPIGetEventRuntimeSecurity/BearerAuth`: advertised success expected 200, actual 401 (exit 1); API-key-only repaired contract/runtime tests and real tenant lookup passed. |
| [02-17](02-17-SUMMARY.md) | Inherited `TestRunRejectsRedirects`: ten cases returned success/two requests before repair; fresh owning-plan GREEN asserted failure/exactly one request. Final probe/default/readiness gates passed. |
| [02-18](02-18-SUMMARY.md) | `test_manifest_preserves_existing_parent_mode`: four actual chmod failures; `test_generator_environment_ignores_ambient_key`: captured child env incorrectly None. Same tests passed after repair; final 54 generator and 11 seed tests passed. |
| [02-19](02-19-SUMMARY.md) | Actual Docker export leaked private fixtures; `test_integration_only_banned_import_fails` observed policy bypass. Same oracles, exclusion-removal mutation, both real graphs and final script suite passed. |
| [02-20](02-20-SUMMARY.md) | `TestTopicRetentionOverridesReapply`: existing retention unchanged after requested update; four effective-config Python assertions failed. Same oracles passed after repair; exact real regression and five configuration tests passed freshly. |

The additive acceptance map covers all seven structured verification gap groups and **CR-01..05 / WR-01..04**, including repeatable retention and D-28's getEvent-auth supersession. API-key-only getEvent follows the established Phase 2 boundary; JWT/RBAC remains Phase 4.

## Deviations from Plan

1. **[Rule 3 — Blocking static gate] Root prerequisite lint repair.** Root fixed four legacy integration-test style findings and scoped `#nosec G304 G703` comments to trusted integration-runner barrier file access (`27d3390`) before this gate. Fresh integration lint/vet then passed; default production files exclude those controls. No broad security exemption or application-source edit was made by this executor.
2. **Evidence harness project-label correction.** Generic gate volume rows accidentally selected the unrelated `compose` label. Before normal stack startup, the executor corrected the read-only evidence to project `sentinel` and compared names **and creation timestamps** before/after. Incorrect generic rows are expressly excluded from acceptance evidence. No user resource was modified by the correction.
3. **Root-owned shared-state serialization.** Task 1 verified existing authorized ROADMAP changes and requested root's registered status correction instead of writing shared files. Root owns subsequent state/session/metrics updates and independent report archival. The focused ROADMAP fallback was previously authorized because the installed SDK exposes no goal/list mutation; no invented command was used.

## Historical Evidence and Pending Work

Original report bytes remained unchanged through executor handoff:

- Verification SHA-256 `f8d0b5d37a52deaf8fd1b6041b8b8e6ca2c8120f3e6c24fe9791b88ffc496144`, historical `gaps_found`.
- Review SHA-256 `bf47694073b07e184251e5c68af7ef90ef3c633f88f9af732b7f26b868931d49`, historical `issues_found`.

Root will archive byte-identical copies as `02-VERIFICATION-2026-10-16-PRE-GAPS.md` and `02-REVIEW-2026-10-16-PRE-GAPS.md` before independently updating canonical reports. Independent goal verification, security assessment and code review remain **pending** at this implementer handoff. Hosted GitHub Actions remains **pending**; no push was performed. The 41 historical passed validation rows and all old plans/SUMMARYs remain preserved.

No failing acceptance command or unresolved local implementation failure was observed in this gate. `.planning/config.json` and `.serena/` remained unrelated user changes and were never staged. No dependency install, environment edit, new endpoint or new security surface was introduced.

## Known Stubs

None introduced. Pending archival/independent/hosted verdicts are explicit handoff obligations, not implemented features or passing claims. The historical Phase 5 benchmark placeholder remains intentionally out of this plan's scope.

## Self-Check: PASSED

All three owned documentation paths exist; task commits `4e1c693`, `4acddb2` and `2b628a8` exist and delete no tracked files. Full gate exit 0, both exact seed totals, corrected persistent-volume equality and original report hashes were directly observed. `git diff --check` and all three declared key links passed. Shared-state updates and independent assessments remain assigned to root rather than silently inferred from SUMMARY counts.
