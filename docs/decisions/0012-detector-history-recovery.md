# 0012. Detector expired-history recovery

## Status

Accepted, 2026-11-15. The user chose the design in plan 04-22 (gap G7). It is implemented by `backend/internal/detection/repin.go` (04-22), `backend/tools/detector-recover` with migration `009_detection_history_recoveries.sql` (04-23), and `make detector-recover-history`, the [runbook](../operations.md#detector-refuses-to-start-expired-checkpoint-history) and the compose offsets retention setting (04-24). ADR numbers 0010 and 0011 are reserved by Phase 5 plans.

## Context

Local Kafka keeps `security.events.v1` for 24 hours. The detector commits a resume pin per partition (ADR 0007), and an idle stack does not advance it. If the stack stays off longer than retention, the broker deletes the records at and after the pin. On the next start the committed offset is below the retained log start, and `restorePartition` fails closed with "detector checkpoint history lost". This happens even when every partition is now empty. Nothing is skipped silently, but new alerts stop reaching the dashboard until someone intervenes.

During Phase 4 acceptance this happened twice (2026-10-30 and 2026-11-13). Both times the only way forward was a private overlay that renamed the consumer group (`sentinel-detector-p4r2`, then `-p4r3`). Renaming throws away cooldown and decision lineage, accumulates dead groups and fence rows, and drifts from the documented `sentinel-detector` group.

Deleting the group and letting the detector start fresh does not work either. The PostgreSQL `detection_partition_fences` rows record the Kafka generation and member of each partition owner. A recreated group starts at a lower generation, so the fences reject it ("stale detector assignment"). Fences must never be rewritten to get around this, because they are what make replay deterministic.

The pipeline is at-least-once (spec §3). Records in `[processed_next, log_start)` expired before the detector evaluated them. They cannot be recovered from Kafka. The event-writer may have stored them in ClickHouse, but detection on them is lost. Any recovery therefore records an acknowledged loss. The user ruled out automatic, silent recovery.

A related failure has a longer fuse. Kafka's default `offsets.retention.minutes` is 7 days. If a detector group has no members for longer than that, the broker drops its committed offsets and group metadata. The generation then resets and the fences reject the group.

## Decision

The user's 04-22 answer, verbatim: "Offline audited tool (Recommended)" = **option-a**. This is an offline operator tool on the same consumer group. It produces a dry-run plan and applies only when given the plan digest, the operator and a reason. It writes a PG audit row and re-pins checkpoints via the Kafka admin API. It never writes fence rows, and the detector restore path is unchanged.

In detail:

- `make detector-recover-history` runs `detector-recover` in dry-run mode. The tool never joins the group. It prints a JSON plan with each expired partition's committed offset, log start and end, the never-evaluated range, whether open windows are discarded, the re-pinned checkpoint, and a `plan_digest`.
- Apply needs `ACK=<plan_digest> OPERATOR=<name> REASON=<text>`. The tool recomputes the plan and refuses if the digest changed. It requires zero group members before writing anything and again before the commit. It inserts `pending` rows into the append-only `detection_history_recoveries` table, commits the re-pinned checkpoints to the same group, verifies them with an offset fetch, and marks the rows `applied`.
- The re-pin target is `max(log_start, processed_next)`, and the new checkpoint is drained. The fingerprint, rule versions, gates, sealed floor, prefix maximum and decision frontier are copied unchanged. Already-evaluated records are never replayed with empty window state.
- The detector itself still fails closed. Its error now names the runbook. Nothing else in the restore path changed.

The user's second answer, verbatim: "Yes (Recommended)", i.e. **retention: yes**. Compose sets `KAFKA_OFFSETS_RETENTION_MINUTES=525600` (one year), and `scripts/test_stack_config.py` asserts it.

## Consequences

- Every recovery is an acknowledged, audited loss. Each re-pinned partition gets a row with the operator, reason, digest, offsets and never-evaluated range. Update and delete triggers keep the rows append-only; the only allowed change is `pending` to `applied`.
- An undrained checkpoint's open windows are discarded, and the plan and audit row say so. A threshold that was partly reached before the outage starts counting again.
- Records in the never-evaluated range are not re-detected, by this tool or by anything else. ClickHouse may still hold them for search.
- The same group keeps its generation, fences, cooldown cursors and decision lineage. The detector then restores through its normal, unchanged path.
- Operators must stop every detector replica first. The tool refuses while the group has members.
- If the offset commit succeeds but marking the audit rows `applied` fails, the rows stay `pending`. A rerun reports "nothing to recover" and does not finalize them. The runbook explains how to recognize and record this case.
- The tool covers only the configured events topic, which matches the detector's single-topic consumer.

### The group-offset expiry sub-case

When the group's metadata is gone but PG fences exist, the tool refuses with "group metadata missing while PG fences exist" and writes nothing. A same-group re-pin is impossible there: the generation the fences recorded no longer exists. This case is a manual escalation that this ADR does not automate. The one-year offsets retention keeps it from happening on an idle local stack. Neither the retention setting nor this ADR claims any duration beyond what the broker configuration states.

### Rejected options

- **Option B, a detector startup flag** (`SENTINEL_DETECTOR.ACKNOWLEDGE_HISTORY_LOSS=<plan_digest>`) that re-pins in-process during restore. It changes the delicate restore and fence path and needs single-member admission. A dry-run tool is needed anyway to produce the digest, and a flag left set is a foot-gun.
- **Option C, formalized group rotation** (an audited new group name). New fences and cooldown cursors would restart, which can re-alert entities whose cooldown should still suppress them. It needs a config change per incident, leaves dead groups and fence rows behind, and hides lineage. This is what the two manual recoveries did.
- **Automatic advance when nothing is retained** (`start == end`). The user ruled out silent recovery. An automatic advance would also hide the loss of records that expired before evaluation.
- **Deleting the group or editing fences.** The fences reject a recreated group, and rewriting fences destroys the replay guarantees of ADR 0007.
