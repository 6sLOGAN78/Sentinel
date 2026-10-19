---
gsd_state_version: 1.0
milestone: v2.8.0
milestone_name: milestone
status: executing
stopped_at: Completed 03-21-PLAN.md; 16/21 Phase 3 plans complete, Wave 9 parent gate pending; 03-11 next
last_updated: "2026-10-19T07:20:42.959Z"
last_activity: 2026-10-19
progress:
  total_phases: 6
  completed_phases: 2
  total_plans: 56
  completed_plans: 51
  percent: 91
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-10-17)

**Core value:** The end-to-end flow (generator → ingest → Kafka → detector → ClickHouse/PostgreSQL → API → live dashboard) works and is provably correct under failure, with deterministic, deduplicated alerts and no loss of acknowledged events.
**Current focus:** Phase 3 — Detection & Alert Pipeline

## Current Position

Phase: 3 (Detection & Alert Pipeline) — EXECUTING
Plan: 17 of 21
Status: Executing — 03-21 complete; Wave 9 parent gate pending, 03-11 next
Last activity: 2026-10-19

Progress: [█████████░] 91%

## Performance Metrics

**Velocity:**

- Total plans completed: 51 (13 Phase 1 + 22 Phase 2 + 16 Phase 3)
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
| Phase 03 P16 | 5 min | 2 tasks | 5 files |
| Phase 03 P03 | 6 min | 2 tasks | 7 files |
| Phase 03 P17 | ~6 min | 2 tasks | 6 files |
| Phase 03 P18 | ~9 min | 3 tasks | 12 files |
| Phase 03 P04 | 5 min | 2 tasks | 7 files |
| Phase 03 P10 | 13 min | 3 tasks | 7 files |
| Phase 03 P05 | 6 min | 2 tasks | 7 files |
| Phase 03 P19 | ~13 min timed | 2 tasks | 11 files |
| Phase 03 P07 | 5h 15m elapsed incl quota pause | 3 tasks | 7 files |
| Phase 03 P08 | 31 min | 3 tasks | 15 files |
| Phase 03 P09 | 85 min elapsed incl quota pause | 3 tasks | 14 files |
| Phase 03 P21 | approximately 18 min | 2 tasks | 7 files |

**By Phase:** Phase 1: 13 plans; Phase 2: 22 plans; Phase 3: 16 plans (timing aggregates pending).

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
- [Phase 03]: 03-16: Extract existing Kafka adapters through explicit transport interfaces and detector compatibility wrappers; detection retains checkpoint/topic type ownership.
- [Phase 03]: 03-03: Rule-qualified shared windows count exact distinct ports separately from accepted unique-event evidence; fired SCAN state retains replay pins until event-time expiry.
- [Phase 03]: 03-03: Enable startup SCAN only alongside implemented AUTH; configured 30s cooldown is retained while durable cross-window suppression remains the 03-19 obligation.
- [Phase 03]: 03-17: Extract native stores without changing global alert-ID preflight lock, immutable winner, CH acknowledgement, insert-only PG commit or durable source-prefix order.
- [Phase 03]: 03-17: Share immutable fingerprint and conflict identity through the immutable storage adapter; storage accepts contracts.Alert and never imports the writer.
- [Phase 03]: 03-18: Require real PG at startup and before nonempty source processing; inject bounded pool/budgets explicitly, with durable cooldown transactions reserved for 19.
- [Phase 03]: 03-18: Profile Compose workers and gate detector on existing pinned tern migrations; retain dev-up activation for 13.
- [Phase 03]: 03-04: Enable exactly all four implemented built-ins; FLOOD/DENY reuse shared windows with source-only DedupeKey components.
- [Phase 03]: 03-04: Cross-window alert suppression remains the durable 03-19 obligation; configured durations and one-per-window firing are not that proof.
- [Phase 03]: 03-10: Preserve proven global-ID lock/native-store guards and skip workflow INSERT entirely for an equal authoritative winner.
- [Phase 03]: 03-10: Inject the existing bounded DLQ publisher for permanent identity/content conflicts; infrastructure failures remain uncommitted and never poison.
- [Phase 03]: 03-05: ApplySource receives actual Kafka topic/partition/offset; legacy Apply is alert-only without fabricated candidate source.
- [Phase 03]: 03-05: Fixed rule ordinals AUTH=0, SCAN=1, FLOOD=2, DENY=3 govern evaluation and future decision frontiers independently of YAML sorting.
- [Phase 03]: 03-05: Candidate fingerprints clear workflow status and detected_at using immutable storage policy; full canonical open payload is retained separately.
- [Phase 03]: 03-19: Every runtime candidate commits immutable PG allow/suppress before allowed stored-byte publication; suppressions have no Kafka/CH/workflow effect.
- [Phase 03]: 03-19: Lock current partition fence, read globally unique alert_id before any cursor access, then atomically persist a missing ordered decision/cursor/frontier.
- [Phase 03]: 03-19: Derive incarnation from configured group, exclude it from fence primary key and reject generation/member/token rollback.
- [Phase 03]: 03-19: Checkpoint last fully effected decision and verify immutable PG row plus a fence at least that far on restore; legitimate PG ahead is allowed.
- [Phase 03-07]: Require complete format-2 recovery fields, explicit nullable clocks, unique JSON members and non-null gates/frontiers before restoring source state.
- [Phase 03-07]: Adopt checkpoints only after complete successful synchronous callback and unchanged assignment tokens; record verified retained history start.
- [Phase 03-07]: Replay uses uninterrupted canonical bytes and exact independent shared fixture IDs; pre-existing SCAN summary mismatch remains unresolved phase acceptance debt.
- [Phase 03-08]: Persist actual prior rule definitions and exact acknowledged drain in existing PG partition fences; changed-rule versions are checked against saved semantics.
- [Phase 03-08]: Changed-config startup requires zero old members before joining and sole membership afterward; unchanged-config replicas retain normal multi-member operation.
- [Phase 03-08]: Persist both handover checkpoints and all four aligned effective gates so PG-before-Kafka failures resume the same authorization without resetting group/fence identity.
- [Phase 03-08]: Idle checkpoint restoration defers only demonstrably changed assignment snapshots; nonempty source processing retains strict current-owner fencing.
- [Phase 03-09]: Share one process budget across partitions and reserve complete prospective changes before recoverable mutation; 64 MiB state and 256 MiB resources are documented accounting limits, not RSS guarantees.
- [Phase 03-09]: Readiness compares current callback-observed assignment against completed restoration; genuine joined-empty replicas are valid only after their assignment callback.
- [Phase 03-09]: Retained cooperative assignments keep lifetime tokens and reducer state while current-generation PG registration verifies the fully effected frontier before changing owner; revoked or newly acquired assignments restore fully.
- [Phase 3]: 03-21: Retained-history scans reconstruct maxima only; nonempty live admission still requires compatible format-2 processed_next equal to stable source end.
- [Phase 3]: 03-21: Use sealed_through plus lateness, explicit empty partitions, actual keyed partition mapping and private schema-2 live schedules; fixed fixtures remain unchanged.
- [Phase 3]: 03-21: Reserve unchanged per-batch sender budgets and enforce one original 15-minute deadline with interruptible waits and a POSIX main-thread timer.

### Pending Todos

None yet.

### Planning Continuation

- Plan 03-21 completed: compiling declaration/assertion RED ae48ca8, private snapshot/live scheduler GREEN f404916 and verified SUMMARY abdd693 committed before serialized tracking. Stable source ends plus strict current format-2 processed_next, explicit empty/idle state, sealed maximum, bounded retained-history reconstruction and actual key/partition mapping pass. Missing metadata never fabricates detector effect completion. All 22 live fake-clock/protocol/sender/deadline tests and 112 focused Python tests pass; full make test/race (121 Python, six web), standard lint, build, integration compile/vet pass. Integration-tag lint has only the existing recovery_test.go:162 debt. TestDetectionScenariosPreservedRepeat is created but unexecuted: Plan 13 supplies the private --output/--preserve-manifest operator path and actual Kafka/CH/PG/prior-row/late/drain proof; the summary records its exact contract. Sixteen of twenty-one summaries exist; parent Wave 9 gate is next, then 03-11. SDK wrote 33% into frontmatter despite counting 51/56 correctly and appended the metric after the table; closeout reconciled both to actual artifact counts. Global requirement checkboxes remain pending; stop before Phase 4.
- Plan 03-09 completed after the quota interruption: admission RED 91fa195, shared bounds a8c9bb8, scalar refinement 6cb4f89, runtime metric 5401b1a, inherited readiness RED 5dcaed2 and current-assignment/retained-state repair 433fb4b; verified SUMMARY 5076101 was committed before tracking. Actual aggregate 10k-window/100k same-time ledger/64 MiB/4096-ID proofs, exactly one internal late count, real terminal capacity with unchanged broker metadata, restored-assignment readiness, and nine-event checkpoint before join/leave then exact ten-event evidence pass. Untouched 945c972 archive independently reproduced the inherited early-ready PG restore race. Full make test/race, standard lint, build and integration vet pass; only known recovery_test.go:162 integration lint debt remains. Fifteen of twenty-one summaries exist; 03-21 is next, and the parent Wave 9 gate follows it. No global requirements or Phase 3 completion claim.
- Wave 8 plan 03-08 is complete: e53d4b5 assertion RED, 29289eb strict rules/PG manifest authorization, e9d124e runtime/docs, and verified SUMMARY 90f4bb3 were committed before tracking. Corrected real baseline RED and final real restart (41.180s) prove active/equal-version rejection, acknowledged drain, pre-join stopped-member admission preserving old member identity, normal unchanged two-member coexistence, the intentional 300→600 gap and exact version-2 replay. Real PG handover proves saved semantic version checks, immutable allow/suppress retention and PG-before-Kafka gate recovery. Six named unit families, six affected real integration families, full make test/race, standard lint and bounded build/integration vet pass. The parent recorded the passing Wave 8 cross-plan gate at 945c972 before dispatching 03-09. Global requirements and Phase 3 completion remain unclaimed.
- Wave 7 plan 03-07 completed after quota interruption; proof/model/adapter commits d255ef0, 48dac29, b8351c6 and 0e93039 plus SUMMARY 47daeb5 preserve exact canonical bytes/live state across 3,410 restart cuts and 156 successive restarts. Real metadata/corruption/retention and stored-byte restart proofs passed; root recorded the passing Wave 7 cross-plan gate at 6f38f80. Shared SCAN wording and integration-lint debts remain explicit.
- Current Phase 3 work: user requested `$gsd-plan-phase 3 --auto`, explicitly approved specialist sub-agents and the operator story, then invoked `$gsd-resume-work`. The interrupted planner was resumed and its revision saved: 21 plans / 52 tasks / 14 waves. Independent 03-PLAN-CHECK-R2.md approves all 14 requirement IDs and five criteria with zero open planning blockers/warnings; registered structure/field/shell checks also pass. Wave 1 plans 03-01/03-06 and Wave 2 plans 03-02/03-15/03-16 are implemented with committed SUMMARYs; both cross-plan gates passed. Wave 3 plans 03-03 (SCAN), 03-17 (native stores) and 03-18 (real PG dependency/fixtures) have committed proof/SUMMARY evidence. Automatic execution continues after the parent Wave 3 gate, stopping before Phase 4.
- Waves 1–6 passed their cross-plan gates, with Wave 6 recorded at a09cc92. Plan 03-19 has committed assertion RED/GREEN (99e19cb/b385283) and verified SUMMARY (b8c7d99): real PG immutable allow/suppress decisions, monotone entity cursors and current Kafka membership fences precede stored-byte publication; populated fully effected checkpoint frontiers are verified against PG on restore. All five independent cooldown/transaction/schema families pass, including actual 299s/301s visibility in Kafka/CH/workflow, equality, immutable old replay, ownership rejection, legitimate PG ahead/restore-behind checks and real PG fence-lock failure preserving exact previous broker checkpoint metadata. Actual restart republishes identical stored bytes. Full make test/race, standard lint, build/integration vet and independent AUTH/SCAN/Volume/PG dependency/writer/workflow/readiness regressions pass. Full crash/rebalance/outage/restore-behind worker matrix remains 03-20, drained config handover remains 03-08, and existing integration lint recovery_test.go:162 unparam remains 03-14.
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

- No current Phase 2 blocker or open Phase 3 planning blocker. Phase 3 has 16/21 plans implemented, including exact checkpoint reconstruction, all four predicates, native stores, adversarial alert persistence, frozen candidates, durable cross-window cooldown, stopped-replica configuration handover, aggregate fail-closed state admission, internal late metrics, restored-assignment readiness and private caught-up live scheduling. Broader poison proofs, complete detector crash/rebalance/outage recovery, actual preserved-stack scenario acceptance and phase-wide security/CI remain pending. Existing integration lint unparam in test/recovery_test.go:162 remains for 03-14. Operational decision history has no automatic GC; deliberate group-generation reset requires stopped maintenance. Changed-config rolling handover is intentionally unsupported; use zero old members before startup and sole new membership.
- Unresolved phase acceptance debt discovered by 03-07: shared scenarios.json expects SCAN summary `Distinct destination ports probed`, while runtime evidence.go emits `Distinct destination ports on one host`; complete static expected canonical fixture equality fails. Root retained reconciliation for 03-13/03-14. Checkpoint proofs compare all runtime bytes against uninterrupted output and exact independent fixture IDs; this is not a static canonical fixture pass. See phase deferred-items.md and 03-07-SUMMARY.md.
- Phase 3 selected research policies cover tumbling-window straddle, watermark recovery and drained rule-version handover; their production proofs remain execution requirements. Dashboard login flow belongs to Phase 4.
- The pre-Phase-1 codebase map is stale; refresh before relying on it for future planning.

## Session Continuity

Last session: 2026-10-19T07:20:42.948Z
Stopped at: Completed 03-21-PLAN.md; 16/21 Phase 3 plans complete, Wave 9 parent gate pending; 03-11 next
Resume file: None
