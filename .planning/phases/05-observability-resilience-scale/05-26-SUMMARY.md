---
phase: 05-observability-resilience-scale
plan: "26"
status: draft-at-checkpoint
subsystem: detector / postgres decision store
tags: [gap-closure, GAP-3, detector, fencing, startup-race]
requirements: [REL-01, REL-02]
key-files:
  created:
    - backend/test/detector_startup_race_test.go
  modified:
    - backend/internal/storage/postgres/detection_decisions.go
    - backend/internal/storage/postgres/detection_decisions_test.go
---

# Phase 5 Plan 26: Concurrent detector startup fatal (GAP-3) - DRAFT at Task 2 checkpoint

Task 1 is done; Task 2 (decision) is pending; Task 3 has not started. This draft holds the diagnosis for the continuation agent.

## Task 1 result: diagnosis

The bench logs (scratchpad/diag) were gone, so this diagnosis rests only on the reproduction.

**Reproduced.** TestDetectorConcurrentStartup starts 3 detector processes (integration binary) at once on a fresh group and a 6-partition topic, using real Kafka 4.3.1 (initial rebalance delay 0, the same as compose) and PostgreSQL 18.6. Two 20-iteration runs gave these fatals:

| Run | Iterations with a fatal | `postgres decision invariant: existing detector group lost its Kafka checkpoint` | `detector commit response mismatch` | Healed after restarting the failed replica |
|-----|-------------------------|----------------------------------------------------|-------------------------------------|---------------------------------|
| race2 (no restart probe) | 2/20 (first at 1) | 1 | 3 | n/a |
| race3 | 4/20 (first at 1) | 4 | 3 | 2 of 4 iterations (1, 18); NOT healed in iterations 5 and 17 |

**Class: ErrDecisionInvariant.** It is not ErrOwnershipSuperseded and not ErrDecisionTransient. The plan's hypothesis (a stale generation or lock_timeout in Register) was not observed in 40 iterations. The error comes from `bindConfigManifest`: `cp == nil && (len(manifest) > 0 || f.offset != -1)`.

**Interleaving, from race2 iteration 6 observations:**
1. At gen 1, two members split the 6 partitions (A: p1, p2, p5; B: p0, p3, p4). Each worker calls `restorePartition` → `PrepareRestore` → `Register` and then `bindConfigManifest`. That **commits rules_manifest to PostgreSQL**. Next, `finishRestore` → `CommitCheckpoints` (the first Kafka checkpoint).
2. The third member joins, which triggers cooperative rebalances to gen 2 and gen 3. The in-flight first Kafka commit fails. Either the broker rejects it, or the response comes back after the local kgo session ended (see the second finding). Because the generation has moved, `syncAssignments` skips the error (`err != nil && assignmentCurrent` is false). PG now has a bound manifest and Kafka has no checkpoint.
3. At gen 3, A keeps p1/p2 with the same token, so the worker restores again. The local `Assignment.Checkpoint` is still nil from the gen-1 offset fetch. `bindConfigManifest` sees manifest-present plus cp-nil, returns "existing detector group lost its Kafka checkpoint", and `assignmentCurrent` is true. The worker returns the error, the process gets "background worker failed", then exit 1.
4. If the first commit never reached the broker, the state is **permanent**. Every later restorer of that partition, including a different member at a higher generation and a Docker restart, hits the same error (race3 iterations 5 and 17: `kafka_checkpoint=none manifest=true`). The bench healed after one restart because, in that run, every manifest-bound partition already had a Kafka checkpoint (as in race3 iterations 1 and 18).

Deterministic PG half: TestDetectorRestoreRetryWithoutAckedCheckpoint runs PrepareRestore(gen 1, cp nil), skips the commit, then runs PrepareRestore(gen 3, same member and token, cp nil). Result: ErrDecisionInvariant "existing detector group lost its Kafka checkpoint". A different member at gen 4 gets the same error.

**Why at startup:** the first Kafka checkpoint for every partition is written during the restore. On a fresh group, three members joining within ~10 ms produce gen 1 → 2 → 3 cooperative rebalances while those first commits are in flight. Later rebalances do not hit this, because partitions then already have Kafka checkpoints (cp != nil).

**Second startup fatal found (kafkax, outside Task 1 files): `detector commit response mismatch`.** In franz-go v1.22.1, `groupConsumer.commit` sends OffsetCommit v10+ with topic IDs, and the broker response has only TopicID with an empty Topic name. `updateCommitted` fills in names from the request, but it returns early when the commit's generation is no longer current or the session ended (`g.uncommitted == nil`). That happens during a rebalance. `kafkax.checkCommitResponse` then keys partitions by an empty topic, which reports "mismatch" and hides the real per-partition code. It fired in 3 of 40 iterations. It is also the commit failure that feeds step 2 above.

## Task 1 code (classification only; no control-flow change)

- `ErrOwnershipSuperseded`, `ErrDecisionTransient`, `ErrDecisionInvariant` (errors.Is-able). transaction() classifies via `classifyDecisionError`: PgError → SQLSTATE only (55P03/40001/40P01/57014 are transient, everything else is invariant); package constant faults (`decisionFaultError`) → invariant plus constant text; ErrNoRows/context errors → invariant wrapping the constant; anything else → `invariant: unclassified <type>`. Detail, Message, Hint and row values are never included. "begin failed" and "commit uncertain" are unchanged. The opaque string is gone.
- Register's combined stale check is split into two branches. Incarnation/config mismatch → ErrDecisionInvariant. Generation/member/token supersession → ErrOwnershipSuperseded.
- The "detector ownership fence lost", "configuration ownership lost" and "committed checkpoint ownership lost" sites go through `lockedOwnedFence`. A fence mismatch → ErrOwnershipSuperseded. A lock/read error keeps its own class instead of being reported as an ownership loss.

## Deviations

- [Rule 2] CheckpointCommitted's "committed checkpoint ownership lost" is also classified as superseded. It is the same fence-mismatch check, and finishRestore reaches it at startup. A lock error at all three sites now keeps its SQLSTATE class instead of being folded into an ownership-loss string. That is still classification only.
- The failpoint files (postgres/failpoint.go, failpoint_integration.go) were not created. The plan made them conditional ("If 10 natural iterations do not reproduce"), and the natural run reproduced. A store-level deterministic test replaces the hook for the PG half. The plan's Register-delay hook targets the superseded hypothesis, which was ruled out.
- TestDetectorConcurrentStartup defaults to 20 iterations (the plan says at least 10). On a fatal it does not stop: it records class frequency, a per-partition Kafka/PG snapshot, and whether a Docker-style restart heals.
- The restore/register call site is `internal/detector/worker.go` (restorePartition/partitionState/syncAssignments), not `kafkax/detector_consumer.go` as the plan's key_links assume.

## Task 2 decision

Pending (blocking checkpoint). The user's verbatim reply goes here.
