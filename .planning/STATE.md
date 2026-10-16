---
gsd_state_version: 1.0
milestone: v2.8.0
milestone_name: milestone
status: Wave 11 repaired; validating combined changes before 02-15
stopped_at: Wave 11 complete; next 02-15 aggregate writer/rebalance repair
last_updated: "2026-10-16T17:28:02.689Z"
last_activity: 2026-10-16 -- Phase 02 planning complete
progress:
  total_phases: 6
  completed_phases: 1
  total_plans: 35
  completed_plans: 32
  percent: 17
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-10-10)

**Core value:** The end-to-end flow (generator → ingest → Kafka → detector → ClickHouse/PostgreSQL → API → live dashboard) works and is provably correct under failure, with deterministic, deduplicated alerts and no loss of acknowledged events.
**Current focus:** Phase 02

## Current Position

Phase: 02 — EXECUTING GAP REPAIRS
Plan: 20 of 22
Status: Wave 11 repaired; validating combined changes before 02-15
Last activity: 2026-10-16 -- Phase 02 planning complete

Progress: [█████████░] 91%

## Performance Metrics

**Velocity:**

- Total plans completed: 25
- Average duration: -
- Total execution time: -

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

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1 | 13 | - | - |
| 2 | 12 | 33 h 42 min elapsed | - |
| Phase 02 P08 | 14min | 3 tasks | 12 files |
| Phase 02 P09 | 26h 33m | 3 tasks | 15 files |
| Phase 02 P11 | 20 min | 3 tasks | 9 files |
| Phase 02 P12 | 8 min | 3 tasks | 10 files |
| Phase 02 P13 | 22 min | 3 tasks | 7 files |

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

### Pending Todos

None yet.

### Planning Continuation

- Latest user authorization supersedes the earlier stop-after-planning instruction: after verifying plans, run `$gsd-execute-phase 2 --auto`. Scope remains Phase 2.
- All 14 plans (02-00 through 02-13) are saved across 10 dependency waves, with 41 concrete tasks. Plans 02-00 through 02-07 and 02-10 are implemented and committed; 5 Phase 2 plans remain.
- Initial checker found two blockers and one warning: missing fail-open counter, oversized intake plan, and bind-field compile ordering. Revisions add prerequisite 02-00 and the atomic bypass counter, reducing intake scope and resolving declaration order. Independent recheck passed with no remaining issues.
- Structural gates pass; all 14 requirements and 30 decisions covered; no same-wave modified-file conflicts. Validation task bindings and roadmap updated.
- Next: execute 02-12 Compose full-path wiring, then 02-13 end-to-end seed/CI guards. Preserve unrelated configuration/.serena edits and existing user volumes.

### Blockers/Concerns

- Phase 1: confirm layout (`backend/cmd/*`) and boilerplate removal in discuss. Go 1.26 bump required by franz-go.
- Open gaps: tumbling-window straddle acceptability, rule-version semantics under replay, event_id policy, dashboard login flow.

## Session Continuity

Last session: 2026-10-16T17:28:02.679Z
Stopped at: Wave 11 complete; next 02-15 aggregate writer/rebalance repair
Resume file: .planning/phases/02-ingestion-durable-event-storage/02-15-PLAN.md
