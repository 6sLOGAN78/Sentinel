---
phase: 04-query-api-analyst-dashboard
plan: "22"
subsystem: detection
tags: [gap-closure, G7, detector, checkpoint, recovery, tdd]
gap_closure: true
gaps_closed: [G7]
requirements: [ALRT-03]
dependency_graph:
  requires: ["04-16"]
  provides: ["PlanHistoryRecovery", "RepinExpiredCheckpoint", "RecoveryPlan.Digest", "G7 design decision option-a + offsets retention yes"]
  affects: ["04-23", "04-24"]
tech-stack:
  added: []
  patterns: ["pure checkpoint rewrite (TransitionCheckpoint precedent)", "domain-separated length-prefixed sha256 digest"]
key-files:
  created:
    - backend/internal/detection/repin.go
    - backend/internal/detection/repin_test.go
  modified: []
decisions:
  - "G7 recovery design: option-a, offline audited operator tool on the same consumer group (user choice)"
  - "Set KAFKA_OFFSETS_RETENTION_MINUTES=525600 in compose (user choice: yes); implemented by a later plan, not this one"
  - "Re-pin target is max(log_start, processed_next): already-evaluated records are never replayed with empty window state"
  - "Re-pinned checkpoints are always drained; clocks, gates, frontier, rule versions and fingerprint are copied unchanged"
metrics:
  duration: "~15m"
  completed: 2026-11-15
---

# Phase 4 Plan 22: G7 decision + pure expired-history recovery core Summary

The user chose option-a (offline audited same-group tool) and one-year offset retention. `backend/internal/detection/repin.go` implements the pure core under TDD: `PlanHistoryRecovery`, `RepinExpiredCheckpoint` and `RecoveryPlan.Digest`. Plans 04-23/04-24 build the tool on it.

## Task 1: Decision (checkpoint:decision, answered by the user)

Both answers are recorded verbatim.

**Question 1:** "G7: when the stack is idle >24h, events expire and the detector refuses to start ("checkpoint history lost"). How should operators recover, without silently losing events?"

**Answer:** "Offline audited tool (Recommended)" = **option-a**. This is an offline operator tool on the same consumer group. It produces a dry-run plan and applies only when given the plan digest, the operator and a reason. It writes a PG audit row and re-pins checkpoints via the Kafka admin API. It never writes fence rows, and the detector restore path is unchanged.

**Question 2:** "Also keep Kafka committed offsets for a year (KAFKA_OFFSETS_RETENTION_MINUTES=525600) so an idle detector group isn't forgotten after 7 days?"

**Answer:** "Yes (Recommended)", i.e. **retention: yes**.

Consequences:
- Task 2 runs because option-a needs the core.
- 04-23/04-24 implement option-a exactly: the tool, migration 009 `detection_history_recoveries`, the runbook/ADR 0012, and the compose retention setting with its `test_stack_config` assertion.
- This plan does not modify compose or any running stack.

## Task 2: Pure recovery plan and checkpoint re-pin (TDD)

| Gate | Commit | Message |
|------|--------|---------|
| RED | `7951ede` | test(04-22): add failing expired-history recovery plan tests |
| GREEN | `7c65554` | feat(04-22): implement pure expired-history recovery plan and checkpoint re-pin |

The RED run (`go test ./internal/detection/ -run '...'`) failed with a build failure: `undefined: RepinExpiredCheckpoint / PartitionBounds / RecoveryPlan`. No refactor commit was needed.

**Tests** in `backend/internal/detection/repin_test.go`. Fixtures are real acknowledged checkpoints built with `checkpointApply`: undrained pin=1/processed_next=4 with a fired window, and drained pin=5 with a non-None frontier.

- `TestRepinExpiredCheckpoint`
  - Covers 5 cases: drained below start, drained with start==end, undrained N<S, undrained S<=N (pin N, empty never-evaluated range), and undrained S==N.
  - Each case compares the decoded old and new metadata field by field: format, fingerprint, rule versions, sealed_through, prefix max, rule_effective_from and decision frontier.
  - Each case also checks Drained=true, ProcessedNext=pin, size <= MaxMetadataBytes, and `NewPartitionState(rules).Restore` succeeds.
  - Rejects: history not expired, end<start, negative start, processed beyond end, fingerprint mismatch, invalid metadata, oversized metadata, nil rules.
- `TestPlanHistoryRecovery`
  - Only expired partitions produce entries. Uncommitted and in-range partitions are skipped.
  - Entries are sorted by topic, then partition.
  - Nothing-lost and nil input both give zero entries, no error and equal digests.
  - Each of these returns an error and no plan: committed>end, end<start, invalid/missing/oversized metadata, empty topic, negative partition, duplicate partition, committed<0, end<start on an uncommitted partition, fingerprint mismatch, empty group.
- `TestRecoveryPlanDigest`
  - The digest is 64-character lowercase hex. The same inputs give the same digest, and input order does not change it.
  - The digest changes when any one of these changes: committed, start, end, partition, topic, group, fingerprint, never_from, never_to, open_windows_discarded, repinned pin, repinned metadata, entry committed, or a swap of start and end.
  - Computing the digest does not mutate the plan.

## Verification (run for real, 2026-11-15 local / 2026-11-14 UTC)

| Command (from backend/) | Start (UTC) | End (UTC) | Exit |
|---|---|---|---|
| `bash ../scripts/go-test-nonempty.sh './internal/detection/...' 'TestPlanHistoryRecovery\|TestRepinExpiredCheckpoint\|TestRecoveryPlanDigest' -race -timeout 3m` | 23:53:00 | 23:53:02 | 0 (3 PASS lines) |
| `go test -race -count=1 ./internal/detection/...` | 23:53:08 | ~23:53:56 | 0 |
| `go tool -modfile=tools.mod golangci-lint run ./internal/detection/...` | ~23:53:56 | ~23:53:59 | 0 (0 issues) |
| `go vet ./internal/detection/...` | ~23:53:59 | 23:53:59 | 0 |

No running stack was used, and the root `.env` ports were not touched.

## Deviations from Plan

**1. [Rule 1 - Bug] Test fixture: the digest "field shift" mutation was a no-op**
- **Found during:** Task 2 GREEN.
- **Issue:** The start/end swap mutated entry 0 (`a.events:7`), whose log_start equals log_end (8). Swapping them changed nothing, so the assertion could never fail.
- **Fix:** All per-field digest mutations now target entry 1, which has log_start 9 and log_end 20, so the swap is observable. No assertion was removed or weakened.
- **Commit:** `7c65554`.

**2. [Rule 2 - Input validation] Extra rejections beyond the behavior block**
- `PlanHistoryRecovery` also rejects an empty group, an empty topic, a negative partition and duplicate topic/partition inputs.
- `RepinExpiredCheckpoint` also rejects nil rules, a negative start, and processed_next > log end.
- All of these are covered by tests.

## Design notes for 04-23/04-24

- Kafka reports "no committed offset" as -1. Callers must map that to `HasCommitted=false`. A negative `Committed` with `HasCommitted=true` is rejected.
- Partitions whose checkpoint is retained are skipped without decoding their metadata. Corrupt metadata there remains the detector's normal fail-closed restore error, outside this tool's scope.
- For an undrained checkpoint, the preserved `PrefixMaxEventTime` is the pre-pin maximum. As the plan requires, it is kept unchanged. The lateness invariant `sealed_through >= prefix - Lateness` therefore still holds, and `Restore` accepts the result.
- `Digest()` uses a hand-rolled length-prefixed encoding with no error path. JSON field order or map iteration cannot change it. Its domain prefix is `sentinel.detector.history-recovery.v1\0`.

## Threat model

- T-04-22-01 is mitigated: strict `CheckpointMetadata` decode, size bound, fingerprint equality, and a `Restore` self-check before return.
- T-04-22-03 is mitigated: the core has no I/O and the detector path is untouched.
- T-04-22-04 is mitigated: the core has no storage access.
- T-04-22-02 (persisting the digest) is deferred to 04-23 by design.

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: backend/internal/detection/repin.go
- FOUND: backend/internal/detection/repin_test.go
- FOUND: 7951ede (RED)
- FOUND: 7c65554 (GREEN)
