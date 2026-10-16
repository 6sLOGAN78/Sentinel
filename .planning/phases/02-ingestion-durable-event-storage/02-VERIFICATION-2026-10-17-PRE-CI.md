---
phase: 02-ingestion-durable-event-storage
verified: 2026-10-17
status: human_needed
score: 14/14 phase requirements locally verified; hosted CI pending
implementation_gaps: 0
overrides_applied: 0
report_completion: root_recovered_from_verifier_audit_messages
gaps: []
human_verification:
  - test: "Push an authorized branch and observe all required GitHub Actions jobs."
    expected: "All jobs, including Docker-backed integration and every nonempty Phase 2 group, pass at the pushed revision."
    why_human: "No push was performed or authorized by the repair plans; hosted runner evidence is still absent."
---

# Phase 2 — Re-verification handoff

**Verdict: human_needed.** The local producer → ingest → Kafka → writer → ClickHouse → tenant lookup slice is implemented and tested. No outstanding implementation gap was identified in the final audit. Full phase sign-off still requires a real hosted CI run.

## Audit provenance

The independent goal verifier inspected current implementation, plans, summaries, raw acceptance logs, artifact/link declarations and targeted behaviors. It reported all seven original gap groups and all nine original review findings closed, with hosted CI as the sole pending check. The verifier then hit its provider usage limit before writing a report. The root agent recovered this handoff from its recorded audit messages and committed evidence. This document does not falsely claim that the verifier completed and signed a final report or assign an invented must-have score.

The separate independent code review completed as `clean` in `02-REVIEW.md` (commit `c16a784`). The independent security audit completed as `verified`, with all 90 registered threats closed and zero open in `02-SECURITY.md` (commit `6c09a58`). Original failing assessments remain byte-identical in the dated `PRE-GAPS` snapshots.

## Goal and user flow

The persisted MVP goal now passes the canonical story validator:

> As a producer with a scoped API key, I want to submit event batches acknowledged only after Kafka accepts them, so that my events become queryable and duplicate-safe in ClickHouse.

| Flow step | Evidence | Local result |
|---|---|---|
| Authenticate scoped producer and validate batch | Key/tenant/scope/error-envelope unit and real ingest tests | Pass |
| Return 202 after Kafka acknowledges every event | Synchronous all-ISR producer; timeout and partial-publish retry tests | Pass |
| Persist valid events before source commit | Native ClickHouse writer; failure, crash, replay and prefix-offset tests | Pass |
| Quarantine poison before source commit within bounded work | Encoded/header caps; shared processing budgets; real slow-poison rebalance and full-origin recovery | Pass |
| Retrieve tenant-scoped, immediately duplicate-safe events | Parameterized lookup, uniqExact and immediate no-OPTIMIZE duplicate tests | Pass |
| Prove the complete run and readiness | Exact-ID seed manifest verification, tenant-hidden 404 and dependency-readiness tests | Pass |

## Requirement coverage

All 14 Phase 2 IDs occur in plan frontmatter and have implementation/test evidence. No orphan requirement was identified.

| Requirement | Local evidence |
|---|---|
| ING-01 | Hash-only scoped API keys, tenant binding, 500-event/1 MiB intake caps |
| ING-02 | Strict all-or-nothing field validation and zero-publication invalid-batch tests |
| ING-03 | Ordered 202 response only after synchronous all-ISR acknowledgements |
| ING-04 | Stable IDs, identical retry bytes and immediate read-time deduplication |
| ING-05 | Authentication, scope, tenant, size and availability error-envelope cases |
| ING-06 | Redis TIME quota, Retry-After and explicit outage behavior |
| ING-07 | Bounded admission, producer/fetch buffers and aggregate writer deadlines |
| STOR-01 | Insert-before-offset commits and real crash/failure recovery |
| STOR-02 | Monthly event schema and configurable TTL migration behavior |
| STOR-03 | Immediate duplicate-safe lookup/counts without background merge reliance |
| STOR-04 | Trusted bounded DLQ origins, publish-before-prefix commit, replay after rebalance |
| GEN-01 | Deterministic reserved-range events and private safe manifests |
| GEN-03 | Repeated real 1,000-ID seed acceptance and controlled child credentials |
| OBS-01 | Five binaries wired to liveness/readiness and bounded draining |

## Original gap closure

| Original gap | Repair evidence |
|---|---|
| Noncanonical MVP goal | Persisted exact story validates; no goal meaning changed |
| DLQ wire bound and poison-heavy rebalance | 02-14/15 RED→GREEN summaries, encoded-size oracle, real two-partition rebalance/origin/offset recovery |
| Advertised bearer alternative absent at runtime | 02-16 YAML-driven runtime test and accurate API-key-only live getEvent contract; JWT remains Phase 4 |
| Redirect escape from loopback probe | 02-17 ten-case zero-follow regression and safe copied client |
| Manifest parent permission mutation | 02-18 descriptor-anchored private writes, existing-parent/symlink/swap/failure cases |
| Credentials entering Docker context | 02-19 actual fixture export and exclusion-removal regression |
| Ambient seed key and integration-only dependency policy | Explicit generator environment and fail-closed dual-graph guard |

The additional retention warning is closed by 02-20's real default/change/repeat topic test and effective exact-value checker. The follow-up review found and then closed the separate seed-directory symlink chmod defect (`537e073` RED, `428604a` fix) and stale README liveness route. Focused review recheck passed before the canonical review was marked clean.

## Observed acceptance

See `02-GAP-ACCEPTANCE.md` for complete commands, timings, commits and provenance.

- Full sequential local acceptance exited 0 in **809 seconds**: default/integration lint and vet, Go race/web/generator checks, scripts, Docker context and dependency guards, pins, full real integration and all seven independently nonempty CI groups.
- Full Docker-backed integration package passed in **253.377 seconds**. Exact new rebalance and retention tests passed in **32.403** and **64.612 seconds**.
- Two consecutive rebuilt-stack seeds each verified sent=accepted=visible=1000, API spotchecks=3, tenant isolation=1, retries=0. After the small seed-directory follow-up, all **27 script tests** and two more complete 1,000-ID seed runs passed.
- Three named Sentinel volumes retained their original identities and creation timestamps. No reset or OPTIMIZE was used.
- Default builds exclude integration failure controls. Schema drift is false; all 30 recorded decisions remain covered.

## Independent verifier spot-checks and limits

The verifier separately confirmed raw gate logs and persistent-volume identities, all **61 artifact declarations**, and **50/51 automated key links**. It manually traced the remaining API-key link through `VerifyHash` to `subtle.ConstantTimeCompare`.

Fresh focused checks reported by the verifier:

- All **13 seed tests** passed, including both symlink-directory cases, environment isolation and exact-ID/shortfall oracles.
- The stdlib healthprobe package passed uncached in **0.108 seconds**.
- **19 selected generator manifest/retry/precedence cases** passed in **0.33 seconds** using the existing virtual environment.
- A separate wider Go spot-check could not finish uncached dependency downloads under its 10-second diagnostic ceiling; it was recorded as an environment skip, not a code failure or a pass.
- At reassessment time no Sentinel containers were running; live requests were connection-refused. This temporal observation does not erase the independently inspected earlier real integration/seed evidence. The verifier did not restart services or touch unrelated running containers.
- Hosted GitHub Actions was not observed. Local equivalence does not substitute for that external check.

## Remaining human check

Authorize and push the intended branch, then confirm every required GitHub Actions job succeeds at that revision. Save the run URL and result in `02-HUMAN-UAT.md`, then rerun phase verification to obtain a completed final verdict. Phase 2 stays pending human verification; this handoff does not advance Phase 3.

## Historical reports

- `02-VERIFICATION-2026-10-16-PRE-GAPS.md`: original 37/43, gaps_found assessment.
- `02-REVIEW-2026-10-16-PRE-GAPS.md`: original five critical/four warning findings.
- Current `02-REVIEW.md`: independent clean re-review with follow-up audit trail.
- Current `02-SECURITY.md`: independent verification of all 90 planned threat dispositions.
