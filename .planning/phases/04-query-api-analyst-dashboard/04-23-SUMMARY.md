---
phase: 04-query-api-analyst-dashboard
plan: "23"
subsystem: detection
tags: [gap-closure, G7, detector, checkpoint, recovery, audit, operator-tool, tdd]
gap_closure: true
gaps_closed: [G7]
requirements: [ALRT-03]
dependency_graph:
  requires: ["04-22", "04-21"]
  provides: ["detector-recover CLI", "migration 009 detection_history_recoveries", "DetectionRecoveryStore", "TestDetectorHistoryRecovery"]
  affects: ["04-24"]
tech-stack:
  added: []
  patterns: ["admin-only kadm client (never a group member)", "audit pending before the side effect, applied after verification", "append-only table via update/delete/truncate triggers"]
key-files:
  created:
    - backend/test/detector_history_recovery_test.go
    - backend/migrations/postgres/009_detection_history_recoveries.sql
    - backend/internal/storage/postgres/detection_recovery.go
    - backend/tools/detector-recover/main.go
    - backend/tools/detector-recover/main_test.go
  modified: []
decisions:
  - "The read-only fence count lives in DetectionRecoveryStore.CountPartitionFences, so main.go has no SQL. main.go mentions detection_partition_fences only in its doc comment, which says the tool never writes it."
  - "Missing group metadata means the group is absent or Dead, or no partition has a committed offset (this covers offsets-retention expiry). It is refused when fences exist. With no fences it is a never-started group: nothing to recover, exit 0."
  - "Acknowledgment flags without --apply are a usage error (exit 2), so an operator cannot think a dry-run applied."
  - "After RecordPending, the remaining bounded steps run on context.WithoutCancel. A signal cannot split commit and verify. Any failure is finished by rerunning the same acknowledged apply (ON CONFLICT DO NOTHING on the unique audit key)."
  - "Store errors are fixed (ErrRecoveryAudit) and never wrap pg errors, because CHECK-violation details echo row values (operator, reason)."
metrics:
  duration: "~10h wall clock, including a usage-limit interruption; about 15m of active work"
  completed: 2026-11-15
---

# Phase 4 Plan 23: G7 offline audited detector history recovery Summary

This plan adds `detector-recover`, an offline operator tool. It re-pins expired detector checkpoints on the same consumer group through the Kafka admin API. Dry-run is the default. Apply needs the plan digest, an operator and a reason. Each recovered partition gets an append-only PostgreSQL audit row (migration 009), and the tool never writes fence rows. A native integration test proves the full workflow and every refusal against a real broker, PostgreSQL, ClickHouse and detector. This implements option-a exactly as recorded in 04-22.

## Commits

| Gate | Commit | Message |
|------|--------|---------|
| RED (task 1) | `df80202` | test(04-23): add failing native expired-history recovery test |
| task 2 | `eab9930` | feat(04-23): add append-only detection_history_recoveries audit and store |
| RED (task 3) | `c1202bb` | test(04-23): add failing detector-recover unit tests |
| GREEN (task 3) | `82ed791` | feat(04-23): implement detector-recover offline audited re-pin tool |

No refactor commit was needed.

**RED evidence:**
- Native test: run 00:50:19 to 00:50:46 UTC, exit 1. It failed at `detector_history_recovery_test.go:52` (`buildRecoverTool`) with `stat .../backend/tools/detector-recover: directory not found`. The tool was missing; setup was not the cause.
- Unit tests: `go test ./tools/detector-recover/` failed to build (`undefined: groupState` ...), exit 1.

## What was built

- **Migration 009** creates `detection_history_recoveries`.
  - Columns and CHECKs follow the plan. Added invariants: `committed_offset < log_start`, `log_start <= repinned_offset <= log_end`, and `never_evaluated_to = repinned_offset`.
  - `UNIQUE (group_id, topic, partition_id, plan_digest)`.
  - A BEFORE UPDATE trigger allows only pending to applied, with applied_at set and every other column unchanged. BEFORE DELETE and BEFORE TRUNCATE triggers raise.
  - There is deliberately no FK to `detection_partition_fences`. The only mention is a comment: `grep -v '^--' ... | grep -c detection_partition_fences` returns 0.
- **`DetectionRecoveryStore`** provides:
  - `RecordPending`: one transaction that inserts every entry with ON CONFLICT DO NOTHING, then requires the row count to equal the entry count.
  - `MarkApplied`: one transaction, then requires every row of the digest to be applied.
  - `CountPartitionFences`: read-only.
  - All SQL is `$n`-parameterized.
- **`tools/detector-recover`**:
  - Uses `config.Load(config.ServiceDetector)` and the strict rules loader. The kgo client has no ConsumerGroup option, so it never joins the group.
  - Order of operations: DescribeGroups, then FetchOffsetsForTopics with ListStart/EndOffsets, then `PlanHistoryRecovery` and `Digest`, then dry-run JSON. Apply continues: digest check, zero members, RecordPending, zero members again, `CommitOffsets` to the same group (leader epoch -1, re-pinned metadata, per-partition error check), `FetchOffsetsForTopics` exact verify, MarkApplied.
  - On success it writes one zerolog line, "detector history recovery applied". It contains group, partitions, never_evaluated_total, open_windows_discarded_count and plan_digest, and no operator, reason or credentials.
  - Exit codes: 0 OK or nothing to recover, 1 refused or failed (fixed messages), 2 usage. No DeleteGroups or DeleteRecords (the grep is empty).
  - The detector restore path is unchanged, and `repin.go` was not modified.

## Tests

- `TestDetectorHistoryRecovery` (integration tag, testcontainers):
  - `idle_retention_recovery` runs steps 1-11 of the plan. The real dry-run plan was committed=3, log_start=log_end=7, never-evaluated [3,7), repinned 7 (drained).
    - The detector failed with "detector checkpoint history lost".
    - Wrong digest, a joined plain kgo member, and an empty reason each refused with zero writes.
    - Apply produced one audit row with status applied and exact fields. FetchOffsets equals the re-pin, and the fence JSON snapshot is byte-identical. stderr does not contain the operator or reason.
    - After restart, /readyz returned 200 and exactly one AUTH_BRUTE_FORCE alert appeared on Kafka, in PG `alert_workflows` and `detection_decisions`, and in ClickHouse.
    - The second apply printed "nothing to recover" and added no rows.
  - `audit_table_is_append-only`: every non-permitted UPDATE fails, as do DELETE and TRUNCATE. Only pending to applied succeeds.
  - `missing_group_metadata_with_fences`: a real detector creates fences, then the test-owned group is deleted. Dry-run and apply both refuse with "group metadata missing while PG fences exist". Nothing is written: no audit rows, unchanged fences, no committed offsets.
- `tools/detector-recover` unit tests (fakes behind the `kafkaPort`/`auditPort` interfaces):
  - `TestParseFlags`, `TestDryRun`, `TestApplyOrdering`.
  - `TestApplyStopsAtFirstFailure` covers each step, including a verify mismatch that keeps rows pending.
  - `TestCommitFailureRerunCompletes`, `TestDigestMismatchRefusesBeforeWrites`, `TestNothingToRecover` (both modes), `TestMissingGroupMetadata` (absent group and no-committed-offsets, both modes), `TestExitCodes`.
- `TestDetectorRetention` and `TestDetectorCheckpoint` still pass. `detector_checkpoint_test.go` has no diff.

## Verification (final run after the resume, 2026-11-15 UTC, from backend/)

| Command | Start | End | Exit |
|---|---|---|---|
| `go test -race -count=1 ./tools/detector-recover/... ./internal/storage/postgres/... ./migrations/` | 10:43:21 | 10:43:24 | 0 |
| `go build ./...` | 10:43:24 | 10:43:27 | 0 |
| `go vet ./...` | 10:43:27 | 10:43:27 | 0 |
| `go vet -tags=integration ./test/` | 10:43:27 | 10:43:27 | 0 |
| `go tool -modfile=tools.mod golangci-lint run ./...` | 10:43:27 | 10:43:29 | 0 (0 issues) |
| `go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./test/...` | 10:43:29 | 10:43:33 | 0 (0 issues) |
| `go tool -modfile=tools.mod golangci-lint run ./tools/detector-recover/... ./internal/storage/postgres/...` | 10:43:33 | 10:43:34 | 0 (0 issues) |
| `COMPOSE_PROJECT_NAME=sentinel-g1check bash ../scripts/go-test-nonempty.sh './test/...' '^TestDetectorHistoryRecovery$\|^TestDetectorRetention$\|^TestDetectorCheckpoint$' -tags=integration -race -timeout 12m -p 1` | 10:43:45 | 10:44:23 | 0 (all PASS) |
| `COMPOSE_PROJECT_NAME=sentinel-g1check bash ../scripts/go-test-nonempty.sh './test/...' '^TestMigrate\|^TestAlertReservationsSchema$' -tags=integration -timeout 8m -p 1` | 10:44:23 | 10:45:01 | 0 (TestAlertReservationsSchema derives 9 from the embedded 009; 3 TestMigrate* PASS) |

The same gates also passed before the interruption: native 00:57:15 to 00:57:54 and migrate 00:52:20 to 00:52:51, both exit 0.

These native tests use testcontainers only. No compose stack was started, the root `.env` ports were not used, and no shared topics, groups or volumes were touched. DeleteRecords and DeleteGroups were applied only to test-created unique names.

## Deviations from Plan

**1. [Rule 2 - Verification coverage] Append-only trigger subtest added to the native test file**
- Task 2 acceptance requires the update trigger to be proven. I added the subtest `audit_table_is_append-only` inside `TestDetectorHistoryRecovery`, committed in `eab9930`.

**2. [Placement] Fence count SQL lives in the store, not main.go**
- The acceptance grep on main.go is satisfied more strictly: its only mention of the table is the doc comment saying it never writes it.

`main_test.go` in `backend/test` was not changed, because the tool is built in-test with `go build -race`. `repin.go` was not changed.

## Known limitations

- Suppose the Kafka commit and verify succeed but `MarkApplied` fails. The audit rows then stay `pending` and the tool reports `errApplied`. A rerun finds nothing to recover, so it does not finalize those rows. The pending rows still record operator, reason, digest and ranges, but their status is stale. This needs a PG failure in a sub-second window. 04-24's runbook should mention it.
- The tool handles only the configured events topic. That matches the detector's single-topic consumer.

## Threat model

- T-04-23-01: zero members is checked before RecordPending and again before CommitOffsets, and the admin client never joins the group.
- T-04-23-02: the plan is recomputed at apply and the digest must match exactly.
- T-04-23-03: audit rows are append-only (triggers) and record operator, reason, digest and ranges.
- T-04-23-04: there is no fence write path, the native test proves byte-identical fences, and there is no FK.
- T-04-23-05: flags are length-bounded and control characters rejected, and all SQL is parameterized.
- T-04-23-06: messages are fixed, logs exclude operator and reason, store errors do not wrap pg details, and no metrics were added.
- T-04-23-07: the detector still fails closed, as asserted in step 4.

All seven are mitigated.

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: backend/test/detector_history_recovery_test.go
- FOUND: backend/migrations/postgres/009_detection_history_recoveries.sql
- FOUND: backend/internal/storage/postgres/detection_recovery.go
- FOUND: backend/tools/detector-recover/main.go
- FOUND: backend/tools/detector-recover/main_test.go
- FOUND: df80202, eab9930, c1202bb, 82ed791
