---
phase: 05-observability-resilience-scale
plan: "26"
status: complete
subsystem: detector / postgres decision store / kafkax commit adapter
tags: [gap-closure, GAP-3, detector, fencing, startup-race]
requirements: [REL-01, REL-02]
dependency-graph:
  requires: [05-24]
  provides: [crash-free concurrent detector startup, classified decision-transaction errors]
  affects: [ADR 0007 (clarifying note only)]
tech-stack:
  added: []
  patterns: [errors.Is failure classes with SQLSTATE-only detail, manifest bound after first acknowledged checkpoint, broker re-read before a restore retry]
key-files:
  created:
    - backend/test/detector_startup_race_test.go
    - backend/internal/kafkax/detector_refresh_test.go
    - backend/internal/detector/restore_retry_test.go
  modified:
    - backend/internal/storage/postgres/detection_decisions.go
    - backend/internal/storage/postgres/detection_decisions_test.go
    - backend/internal/kafkax/detector_consumer.go
    - backend/internal/detector/worker.go
    - backend/internal/detector/readiness_test.go
    - docs/decisions/0007-event-time-state-pin-recovery.md
decisions:
  - "Task 2: user chose option-c plus the kafkax commit-response fix (verbatim: \"Option C + kafkax fix\")"
  - "The rules manifest is bound in CheckpointCommitted, after Kafka acknowledges a checkpoint; bindConfigManifest only validates"
  - "A failed restore of an unchanged assignment token re-reads the broker's committed checkpoint before retrying"
  - "checkCommitResponse resolves OffsetCommit v10+ topic IDs from the request"
  - "Legacy fences with a bound manifest and no Kafka checkpoint still fail closed (indistinguishable from real loss)"
metrics:
  completed: 2026-11-20
  tasks: 3
---

# Phase 5 Plan 26: Concurrent detector startup fatal (GAP-3) Summary

Three detectors starting at once on a fresh group no longer crash. Two changes fix it: the PostgreSQL rules manifest is bound only after Kafka acknowledges a partition's first checkpoint, and the detector re-reads the broker checkpoint before retrying a restore. The Kafka commit adapter now resolves OffsetCommit v10 topic IDs, so real per-partition codes replace a synthetic "mismatch". Decision-transaction errors carry typed, SQLSTATE-only classes.

## Diagnosis (Task 1)

The bench logs (scratchpad/diag) were gone, so the diagnosis rests only on reproduction.

TestDetectorConcurrentStartup starts 3 detector processes at once on a fresh group and a 6-partition topic. It uses real Kafka 4.3.1 (initial rebalance delay 0, the same as compose) and PostgreSQL 18.6. Before the fix:

| Run | Iterations with a fatal | `postgres decision invariant: existing detector group lost its Kafka checkpoint` | `detector commit response mismatch` | Healed after a Docker-style restart |
|-----|----|----|----|----|
| race2 | 2/20 (first at 1) | 1 | 3 | not probed |
| race3 | 4/20 (first at 1) | 4 | 3 | 2 of 4; iterations 5 and 17 stayed wedged |

**Class: ErrDecisionInvariant.** It is not superseded ownership and not a transient SQLSTATE; the plan's hypothesis was not observed in 40 iterations. The error comes from `bindConfigManifest`: `cp == nil && (len(manifest) > 0 || f.offset != -1)`.

**Interleaving (race2 iteration 6):**
1. At gen 1, two members split the partitions. Each restore runs Register and then bindConfigManifest, which **wrote** the manifest to PG. finishRestore then commits the first Kafka checkpoint.
2. The third member joins and triggers cooperative rebalances to gen 2 and gen 3. The first Kafka commit fails. `syncAssignments` skips the error because the generation moved.
3. At gen 3 the member keeps the partition with the same token and retries with its gen-1 snapshot (cp nil). It hits the manifest it wrote itself, the assignment is now current, and the process gets "background worker failed", then exit 1.
4. If the first commit never reached the broker, the partition is wedged for every later restorer: other members, higher generations and restarts (race3 iterations 5 and 17: `kafka_checkpoint=none manifest=true`). The bench healed only because its affected partitions already had Kafka checkpoints.

**Why only at startup:** every partition writes its first checkpoint during restore, and on a fresh group three near-simultaneous joins rebalance while those first commits are in flight.

**Second fatal: `detector commit response mismatch`.** franz-go v1.22.1 `updateCommitted` resolves v10 topic names only while the commit's generation and session are still current. Mid-rebalance, our `checkCommitResponse` keyed partitions by an empty topic name, which reported "mismatch" and hid the real code.

## Task 2 decision

User reply, verbatim: **"Option C + kafkax fix"**. Scope as relayed: (1) bind the manifest only after the first acknowledged checkpoint, with no schema change; (2) do not reuse the stale empty checkpoint after a restore failure; (3) checkCommitResponse matches by topic ID; (4) invert the deterministic test and make TestDetectorConcurrentStartup pass 20 iterations including the restart probe. ADR 0007 text is unchanged and gets a clarifying note. Also cover the upgrade path.

## Task 3 implementation

1. **Manifest binding.** `bindConfigManifest` validates only: the existing manifest must match, handover gates must hold, and a missing checkpoint is rejected when a manifest exists or the frontier is set. `CheckpointCommitted` validates any existing manifest and binds `rules_manifest=COALESCE(rules_manifest,$7)` after Kafka acknowledged the checkpoint. A saved manifest now proves a checkpoint was once acknowledged. No schema change.
2. **Restore retry.** `Worker.retry` records the token of each failed restore. `retryablePartition` calls the new `Consumer.RefreshCheckpoint` (kafkax: kadm FetchOffsets plus history bounds, then the same validation as the offset-fetch hook; the broker is authoritative, so no committed offset clears the snapshot) before retrying the same token. A refresh failure fails closed. A new token clears the mark, because it brings its own offset fetch.
3. **Commit responses.** `checkCommitResponse(request, response, checkpoints)` maps response TopicIDs to names from the request. Accepted partitions now pass; REBALANCE_IN_PROGRESS, ILLEGAL_GENERATION and UNKNOWN_MEMBER_ID surface as kerr errors. An unknown ID or a missing request is still a mismatch.
4. **Tests.** TestDetectorRestoreRetryWithoutAckedCheckpoint is inverted. The retry and a new owner both succeed without a checkpoint, and acknowledged-then-lost still fails closed with ErrDecisionInvariant. TestDetectorManifestUpgradePath checks two cases: a legacy-bound fence with an acknowledged checkpoint restores unchanged and keeps its exact manifest, and a legacy wedged fence (manifest, no checkpoint) still fails closed.

**Upgrade path:** existing groups with a bound manifest and Kafka checkpoints are unaffected. Their manifest is validated, never replaced. A partition already wedged by the old code still refuses to start, because it cannot be told apart from real loss. That case stays an operator escalation, as before.

## Commits

- `6fe1752` test(05-26): classify decision tx errors and reproduce concurrent detector startup fatal
- `5113baf` docs(05-26): draft GAP-3 diagnosis at Task 2 decision checkpoint
- `d9c84f9` test(05-26): add failing tests for restore retry without an acknowledged checkpoint (RED)
- `7022ebc` fix(05-26): bind detector manifest after first acked checkpoint and resolve commit topic IDs for concurrent startup (GREEN)

## Verification

- gofmt clean. golangci-lint `./...` and `--build-tags=integration ./...`: 0 issues each.
- `go test -race ./...`: all pass, including the new kafkax refresh/topic-ID, worker retry and postgres classification tests. `tools/detector-recover`: pass.
- RED: against HEAD production code, the kafkax and detector unit tests failed to compile and TestDetectorRestoreRetryWithoutAckedCheckpoint failed ("restore alone must not bind the manifest").
- TestDetectorConcurrentStartup: **no startup fatal in 20 iterations of 3 concurrent replicas**, in two separate runs (green1, and green2 within the full family).
- Integration (isolated testcontainers), all 40 pass: TestChaosGracefulShutdownDetector, TestDetectionDecisionsSchema, TestDetectorCapacity, TestDetectorCheckpoint, TestDetectorRetention, TestDetectorConfigRestart, TestConfigLedgerHandover, 8× TestDetectorCooldown*, TestDetectorPoison, TestDetectorHistoryRecovery (ADR 0012), TestDetectorIncorporatedOwnerSnapshot, TestDetectorPGDependency, TestDetectionPipelineAuth/Scan/Volume, TestDetectorThirdReplicaRebalance, TestDetectorCooperativeAcquisition, TestDetectorMidWindowRecovery, TestDetectorCrashBoundaries, TestDetectorConcurrentStartup, TestDetectorRestoreRetryWithoutAckedCheckpoint, TestDetectorManifestUpgradePath, TestHealthz, TestReadyz*, TestTracingPipelinePropagation, TestBinaryLifecycle*.
- `scripts/check-adrs.sh`: OK.
- Not counted: TestDetectionScenarios/preserved-live-edges and TestDetectionScenariosPreservedRepeat were matched by accident by the `-run TestDetection` pattern. They drive the live operator stack (`scripts/seed.py`, `make seed-scenarios`), which is not running, and they failed immediately with no effect: no sentinel containers exist and `.local/last-send.json` is unchanged since 2026-11-18. They are environmental and unrelated to this plan.

## Deviations from Plan

- **[Rule 2] Third superseded site.** CheckpointCommitted's "committed checkpoint ownership lost" is also classified as superseded, through the shared `lockedOwnedFence`. Lock errors at all three fence sites keep their SQLSTATE class.
- **No failpoint files.** postgres/failpoint.go and failpoint_integration.go were not created. They were conditional on the natural run failing to reproduce, and their Register-delay design targeted the ruled-out superseded hypothesis. The deterministic store-level test covers the PG half.
- **Plan file list.** The restore call site is `internal/detector/worker.go`, not `kafkax/detector_consumer.go` as the plan's file list and key_links assume. Option-c touched worker.go, readiness_test.go (fake RefreshCheckpoint), a new restore_retry_test.go and kafkax/detector_refresh_test.go.
- **Option-a artifacts not built.** The option-a superseded-revocation and transient-retry behaviour is not implemented, because the user chose option-c. ErrOwnershipSuperseded and ErrDecisionTransient remain classification-only (still fatal).
- **Iteration count.** TestDetectorConcurrentStartup defaults to 20 iterations (`SENTINEL_TEST_STARTUP_RACE_ITERATIONS` overrides). On failure it records class frequency, a per-partition Kafka/PG snapshot, and the result of a restart probe.

## Known Stubs

None.

## Follow-ups (not in scope)

- docs/operations.md has no runbook entry for a partition wedged by an earlier release (bound manifest, no Kafka checkpoint). It still needs manual escalation.

## Self-Check: PASSED

All files listed above exist, and commits 6fe1752, 5113baf, d9c84f9 and 7022ebc are present in `git log`.
