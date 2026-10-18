---
gsd_state_version: 1.0
milestone: v2.8.0
milestone_name: milestone
status: executing
stopped_at: Completed 03-15-PLAN.md; 4/21 Phase 3 plans complete, Wave 2 continues
last_updated: "2026-10-18T22:09:53.262Z"
last_activity: 2026-10-18
progress:
  total_phases: 6
  completed_phases: 2
  total_plans: 56
  completed_plans: 39
  percent: 70
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-10-17)

**Core value:** The end-to-end flow (generator → ingest → Kafka → detector → ClickHouse/PostgreSQL → API → live dashboard) works and is provably correct under failure, with deterministic, deduplicated alerts and no loss of acknowledged events.
**Current focus:** Phase 3 — Detection & Alert Pipeline

## Current Position

Phase: 3 (Detection & Alert Pipeline) — EXECUTING
Plan: 5 of 21
Status: Ready to execute
Last activity: 2026-10-18

Progress: [███████░░░] 70%

## Performance Metrics

**Velocity:**

- Total plans completed: 39 (13 Phase 1 + 22 Phase 2 + 4 Phase 3)
- Timing averages/totals: pending.

**Per Plan:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 02 P00 | 16 min | 3 tasks | 6 files |
| Phase 02 P01 | 16 min | 3 tasks | 11 files |
| Phase 02 P10 | 15 min | 3 tasks | 7 files |
| Phase 02 P02 | 5 h | 2 tasks | 14 files |
| Phase 02 P03 | 8 min | 3 tasks | 10 files |
| Phase 02 P04 | 9 min | 3 tasks | 13 files |
| Phase 02 P05 | 2 min | 3 tasks | 7 files |
| Phase 02 P06 | 7 min | 3 tasks | 12 files |
| Phase 02 P07 | 22 min | 3 tasks | 10 files |
| Phase 02 P08 | 14min | 3 tasks | 12 files |
| Phase 02 P09 | 26h 33m | 3 tasks | 15 files |
| Phase 02 P11 | 20 min | 3 tasks | 9 files |
| Phase 02 P12 | 8 min | 3 tasks | 10 files |
| Phase 02 P13 | 22 min | 3 tasks | 7 files |
| Phase 03 P01 | 9 min | 2 tasks | 8 files |
| Phase 03 P06 | ~10 min | 2 tasks | 5 files |
| Phase 03 P02 | 7 min | 2 tasks | 7 files |
| Phase 03 P15 | 3 min | 2 tasks | 5 files |

**By Phase:** Phase 1: 13 plans; Phase 2: 22 plans; Phase 3: 4 plans (timing aggregates pending).

## Accumulated Context

### Decisions

- Roadmap follows spec §14 order, 6 phases. Optional Phase 7 (Aegis, ML) deferred to v2.
- DEP-03 (hardened CI) assigned to Phase 6. Phase 1 CI skeleton covered by FND-02/FND-05.
- UI-05 (System page) placed in Phase 5 because it needs lag and health metrics from OBS-02.
- GEN-01/GEN-03 in Phase 2, GEN-02 in Phase 3, OBS-01 in Phase 2.
- SSE fan-out via Postgres LISTEN/NOTIFY with `seq` backfill. Postgres is the alert list index (ADRs in Phase 3/4).
- Epoch-aligned tumbling windows with state-pin replay recovery (ADR first in Phase 3).
- [Phase 02]: 02-00: Optional BindAddress accepts IPv4/IPv6 literals; unset retains all-interface container binding.
- [Phase 02]: 02-00: Request logs use paths only; error and panic logs exclude raw messages, headers and bodies.
- [Phase 02]: 02-01: Credential cache stores copied positive records only, verifies every presented secret and expires within 30 seconds.
- [Phase 02]: 02-01: Demo credentials are atomically written at mode 0600 beneath a mode 0700 directory and symlink files are refused.
- [Phase 02]: Retries preserve one serialized body per batch and stop after five attempts or 120 cumulative wait seconds. — This bounds remote backpressure while honoring positive Retry-After delays in full.
- [Phase 02]: A 202 is accepted only when counts and ordered event IDs exactly match; credentials come only from environment or private files. — This prevents false success accounting and keeps secrets out of process arguments and artifacts.
- [Phase 02]: 02-02: Reserve bounded request capacity before authentication and return 503 immediately when saturated.
- [Phase 02]: 02-02: Kafka 202 responses require synchronous all-ISR acknowledgements for every ordered record.
- [Phase 02]: Resolve every reliably parsed tenant before field validation so cross-tenant writes return 403 without publishing. — Preserves tenant authorization precedence while keeping invalid batches atomic.
- [Phase 02]: Strip client received_at through duplicate-safe normalization and apply one server UTC-millisecond timestamp. — Keeps received_at server-owned without weakening duplicate-key rejection.
- [Phase 02]: Error details are optional and capped at 50, preserving ordinary three-field envelopes. — Provides indexed diagnostics while bounding response work and maintaining wire compatibility.
- [Phase 02]: Store physical event duplicates but collapse every user-visible lookup and count immediately by event_id. — Keeps retries safe without depending on asynchronous ClickHouse merges.
- [Phase 02]: Bind authenticated tenant and event ID as native ClickHouse named parameters. — Enforces tenant isolation and prevents untrusted values from entering SQL text.
- [Phase 02]: Conflicting payloads sharing an event ID have one visible identity but no immutable content winner. — Matches ReplacingMergeTree behavior across merges and equal-millisecond ties.
- [Phase 02]: 02-05: Keep the seven-day migration default immutable and apply configured retention after every successful migration run.
- [Phase 02]: 02-05: Accept only validated 1..3650-day retention and a validated database identifier in fixed TTL DDL.
- [Phase 02]: 02-05: Do not automatically MATERIALIZE TTL; existing-part deletion remains asynchronous.
- [Phase 02]: 02-06: Disable Kafka auto-commit and commit only partition-contiguous offsets after ClickHouse insertion succeeds.
- [Phase 02]: 02-06: Validate replayed events relative to occurred_at while requiring nonzero occurred_at and received_at.
- [Phase 02]: 02-06: Preserve first worker failure through bounded lifecycle cleanup and cap consumer fetch/rebalance resources.
- [Phase 02]: 02-07: Insert valid events before publishing poison records so ClickHouse failures never create DLQ traffic.
- [Phase 02]: 02-07: Commit each Kafka partition only through its own contiguous durable effect prefix.
- [Phase 02]: 02-07: Bound DLQ records to the value cap plus an 8 KiB metadata allowance and reject source dlq.* overrides.
- [Phase 02]: 02-08: Keep fail-open handling inside each limiter so every admitted Redis bypass increments exactly one process-local atomic counter and emits one sanitized warning.
- [Phase 02]: 02-08: Apply one 250 ms limiter deadline on top of bounded Redis dial/read/write timeouts and retries while preserving shared AUTH/TLS client construction.
- [Phase 02]: 02-09: Expose dependency state only on a separately bound ops listener with loopback defaults and sanitized states.
- [Phase 02]: 02-09: Register ops shutdown first and draining last so LIFO cleanup advertises not-ready before stopping work and closes ops last.
- [Phase 02]: 02-09: Use real protocol checks for Kafka, PostgreSQL, Redis, and ClickHouse rather than TCP reachability.
- [Phase 02]: 02-11: Compile failure controls only with the integration tag; default builds contain unconditional no-ops and read no failure environment settings.
- [Phase 02]: 02-11: Use private once-only file barriers to prove exact durable boundaries before pausing or killing test-owned resources.
- [Phase 02]: 02-11: Treat Kafka topic-leader metadata, not broker-only Ping, as producer readiness after an outage.
- [Phase 02]: 02-12: Build all service binaries once into a digest-pinned distroless image and select each process with a Compose entrypoint.
- [Phase 02]: 02-12: Restrict the static health probe to literal loopback HTTP URLs so container probes cannot become an SSRF primitive.
- [Phase 02]: 02-12: Complete topics and migrations before starting the three Phase 2 application services.
- [Phase 02]: Verify the exact current-run manifest ID set under tenant and run-start bounds so historical rows cannot satisfy seed acceptance.
- [Phase 02]: Keep demo keys in private local files and pass secrets only through child environment, never argv or logs.
- [Phase 02]: Guard each Phase 2 integration behavior family independently in CI and preserve go-list failures before policy matching.
- [Phase 03]: 03-01: AUTH retains fired-window pins and commits format-2 metadata only after acknowledged alert publication.
- [Phase 03]: 03-01: Final cross-window cooldown depends on the later PostgreSQL decision ledger; the initial AUTH slice does not certify DET-08.
- [Phase 03]: 03-06: Live scenarios require a private caught-up schedule; plan 03-21 owns snapshot acquisition and clock waiting.
- [Phase 03]: 03-06: All-mode drains share the latest relevant window end plus 30 seconds to avoid partition-watermark regression.
- [Phase 03]: 03-02: Per-alert transaction locks cover PG winner and retained CH orphan preflight through CH acknowledgement and insert-only PG commit.
- [Phase 03]: 03-02: Preserve the complete first-pipe AUTH principal suffix and keep native store helpers cohesive until immediate 03-17 extraction.
- [Phase 03]: 03-15: Preserve existing AUTH and strict loader behavior with passing characterization guards before source-only extraction.

### Pending Todos

None yet.

### Planning Continuation

- Current Phase 3 work: user requested `$gsd-plan-phase 3 --auto`, explicitly approved specialist sub-agents and the operator story, then invoked `$gsd-resume-work`. The interrupted planner was resumed and its revision saved: 21 plans / 52 tasks / 14 waves. Independent 03-PLAN-CHECK-R2.md approves all 14 requirement IDs and five criteria with zero open planning blockers/warnings; registered structure/field/shell checks also pass. Wave 1 plans 03-01/03-06 and Wave 2 plans 03-02/03-15 are implemented with committed SUMMARYs. Automatic execution continues at Wave 2 plan 03-16, stopping before Phase 4.
- User chose option 1: actual alert-level cooldown across nearby windows, rejecting candidate-only throttling. See 03-COOLDOWN-DECISION.md and amended research: durable PG allow/suppress decisions/cursors/fences, checkpoint decision_frontier consistency, event-time expiry, unchanged canonical IDs and Kafka window replay. Initial and R1 checker failures remain preserved; their issues are resolved by R2. Planning approval is not production proof. Preserve unrelated configuration/.serena edits and existing user volumes.
- Phase 2 closeout evidence below is historical and remains valid; it is not the current Phase 3 authorization boundary.

- Final status: readiness RED 2afe6b8 / repair eb7f6e4 passed full make test, twenty ops race repetitions, lint and three real readiness/lifecycle race repetitions (138.221s). Hosted run 38974983287 passed all eight jobs and seven guarded groups at 5e6a82684019e92333c4af90eb3a46468ed9df8a; the previously failing separate readiness group passed. Phase 2 is verified complete.

- At the Phase 2 closeout, authorization covered Phase 2 only. The later Phase 3 --auto request now authorizes its workflow, subject to unresolved decision and verification gates above.
- All 22 plans (02-00..21) across 14 waves are implemented and summarized. Final verdict passed: 5/5 roadmap criteria, 14/14 requirements, 61/61 artifacts and 30/30 decisions accounted for.
- Hosted run 38974983287 passed all eight jobs at 5e6a82684019e92333c4af90eb3a46468ed9df8a, including full integration, seven nonempty groups and Compose. See 02-CI-CLOSEOUT.md for failed attempts, repairs and final proof.
- Independent local code review is clean and the registered threat audit has zero open threats; root explicitly reviewed the readiness delta and rechecked affected threats. Root completed final inline goal verification; the quota-limited handoff remains byte-identical in a dated snapshot.
- Registered phase.complete 02 was run. Its zero-padded lookup left unpadded roadmap rows unchanged and added completed-plan counts twice; root reconciled actual checkboxes/progress and the 35 summaries explicitly. Its two debt warnings refer only to intentionally preserved historical PRE-GAPS/PRE-CI snapshots, not the passed canonical verification or complete UAT.
- The old next-action suggestion to discuss Phase 3 was superseded by the user's explicit planning request; no new discuss-phase decision document was fabricated.

### Blockers/Concerns

- No current Phase 2 blocker or open Phase 3 planning blocker. Phase 3 has 4/21 plans implemented; remaining rule, native/failure persistence, poison, cooldown, recovery, live scheduling and phase-wide security/CI proofs remain pending. Existing integration lint unparam in test/recovery_test.go:162 remains for 03-14.
- Phase 3 selected research policies cover tumbling-window straddle, watermark recovery and drained rule-version handover; their production proofs remain execution requirements. Dashboard login flow belongs to Phase 4.
- The pre-Phase-1 codebase map is stale; refresh before relying on it for future planning.

## Session Continuity

Last session: 2026-10-18T22:09:53.250Z
Stopped at: Completed 03-15-PLAN.md; 4/21 Phase 3 plans complete, Wave 2 continues
Resume file: .planning/phases/03-detection-alert-pipeline/03-16-PLAN.md
