---
phase: 02-ingestion-durable-event-storage
plan: "15"
subsystem: storage
tags: [kafka, clickhouse, rebalance, deadlines, replay, dlq]
requires:
  - phase: 02-14
    provides: Encoded DLQ safety and durable origin preservation
provides:
  - Shared checked writer processing and rebalance budgets
  - Aggregate poison deadline with reserved durable-prefix commits
  - Real multi-poison rebalance, replay and cancellation evidence
affects: [02-19, 02-21, event-writer, future-consumers]
tech-stack:
  added: []
  patterns: [aggregate effects deadline, partition-contiguous durable prefixes, parent-only shutdown transfer]
key-files:
  created: [backend/test/writer_rebalance_test.go]
  modified:
    - backend/internal/config/config.go
    - backend/internal/config/config_test.go
    - backend/internal/kafkax/consumer.go
    - backend/internal/kafkax/consumer_test.go
    - backend/internal/eventwriter/writer.go
    - backend/internal/eventwriter/writer_test.go
    - docs/event-storage.md
key-decisions:
  - "One batch shares one poison retry-sequence budget; its bound does not scale with poison record count."
  - "Effects expiry preserves an independent commit reservation under the original parent and processing deadline."
  - "Only original parent cancellation starts one fresh shutdown attempt; its final ownership release also runs on expiry."
requirements-completed: [STOR-01, STOR-04, ING-04, ING-07]
duration: 22 min active execution; closeout resumed later
completed: 2026-10-17
---

# Phase 2 Plan 15: Aggregate Writer Budgets and Rebalance Recovery Summary

Shared checked deadlines bound poison-heavy batches while preserving durable partition prefixes and complete Kafka/ClickHouse recovery after real rebalances.

## Performance

- Started implementation: 2026-10-16T17:29:11Z.
- Task execution and verification completed: approximately 2026-10-16T17:51:00Z.
- Summary closeout resumed: 2026-10-16T19:30:26Z (2026-10-17 in the workspace timezone).
- Tasks: 3/3; implementation/test/documentation files: 8.

## Accomplishments

- `ComputeWriterBudgets` validates direct writer construction and loaded production configuration. Retry `R=A*O+(A-1)*B`, processing `P=2*R+O`, and rebalance `max(15s, flush+P+shutdown+5s)` use checked duration addition and validated multiplication inputs. Ownership budgets over five minutes are rejected. Defaults remain 5,000 records/one second and yield 70s retry, 150s processing and 176s rebalance.
- The processing clock starts before classification. Inserts occur before DLQ effects; all poison work shares one retry-sequence deadline, followed by a live reserved commit context. Successful independent prefixes can commit, while incomplete effects remain replayable. Internal expiry exits with an error and releases ownership; original parent cancellation alone permits one bounded shutdown transfer.
- A real two-partition fixture uses 24 poison records and four valid events, a 750ms context-aware delay around the production DLQ publisher, a unique Kafka group/topics and dedicated ClickHouse database. It observes `PreparingRebalance` and a subsequent real assignment, consumes DLQ origins independently, checks source offsets against durable prefixes on both partitions, then recovers every poison origin and valid event without `OPTIMIZE`.

## Task Commits

1. Task 1 behavioral RED regressions: `0b011c0` — aggregate poison ownership failures.
2. Task 2 repair: `56d8a02` — shared budgets, aggregate effects and reserved commits.
3. Additional parent-cancellation regression and repair: `0d89499` (RED), `2c16598` (GREEN).
4. Task 3 real recovery acceptance: `c5f28c8` — complete replay, mixed partitions, failed earliest poison and cancellation.

Each task-related commit stages only explicit plan-owned files. No tracked files were deleted.

## Behavioral RED Evidence

- `cd backend && go test ./internal/eventwriter ./internal/kafkax ./internal/config -run 'TestWriterAggregatePoisonBudget|TestRebalanceTimeout|TestWriterBudget' -count=1 -timeout 30s` — exit 1 against the original code. The aggregate fixture completed all eight poisons and returned nil instead of deadline exhaustion; rebalance was 15s rather than the required 27s; both oversized ownership configurations were accepted.
- `cd backend && go test -tags=integration ./test -run '^TestWriterSlowPoisonRebalance$' -count=1 -timeout 3m -p 1` — exit 1, package 77.350s. Real resources started, the second slow publication entered and Kafka reported `PreparingRebalance`; the behavioral assertion failed because ownership was not released within six seconds. This was not a setup or compilation failure.
- `cd backend && go test ./internal/eventwriter -run '^TestWriterConsumerCloseDoesNotStartShutdownTransfer$' -count=1 -timeout 10s` — exit 1 before the follow-up repair: an explicit consumer close triggered fresh shutdown processing and returned nil, despite no parent cancellation.

## GREEN Verification Evidence

- The same initial named unit selection passed after the repair.
- `cd backend && go test -race ./internal/config ./internal/kafkax ./internal/eventwriter -count=1 -timeout 60s` — exit 0; final package times 1.461s, 1.068s and 1.142s.
- `cd backend && go vet ./internal/config ./internal/kafkax ./internal/eventwriter` — exit 0.
- `cd backend && go tool -modfile=tools.mod golangci-lint run ./internal/config/... ./internal/kafkax/... ./internal/eventwriter/...` — exit 0, zero issues.
- `cd backend && go test -tags=integration ./test -run '^TestWriterSlowPoisonRebalance$' -count=1 -timeout 3m -p 1 -v` — exit 0, package 31.943s, named test 13.21s. Exhaustion/mixed-partition, failed-earliest-poison and cancellation subcases passed; ownership released approximately 226ms, 253ms and 82ms after the second publication entered. Each also asserts under six seconds from the first publication; cancellation asserts the shutdown timeout plus 250ms scheduling allowance. Each recovery consumed every original poison origin before final offset claims, observed next offset 14 on both partitions, retrieved all four valid IDs and counted four immediately unique events.
- The named real test also passed with `-race`: package 36.153s, test 13.58s; no race report.
- `cd backend && go test -tags=integration ./test -run 'TestWriterCommitAfterInsert|TestWriterPoisonToDLQ|TestWriterDLQFailureNoCommit|TestWriterInsertFailureNoCommit|TestWriterCrashRestart|TestIngestRetryNoDuplicates' -count=1 -timeout 6m -p 1 -v` — exit 0, package 106.157s. All six selected tests ran and passed, preserving insert-before-commit, no DLQ on storage failure, poison failure boundaries, crash recovery and retry deduplication.
- `cd backend && go vet -tags=integration ./test/... && go list -f '{{.GoFiles}}' ./internal/eventwriter ./internal/kafkax && go build ./cmd/...` — exit 0. Default files were `[failpoint.go writer.go]` and `[consumer.go dlq.go failpoint.go producer.go]`; integration failure controls remain excluded.
- `gsd-sdk query verify.key-links .planning/phases/02-ingestion-durable-event-storage/02-15-PLAN.md` — exit 0, all three links verified.
- `git diff --check` and the plan-owned stub scan found no whitespace defects or blocking placeholders.

## Deviations from Plan

1. [Rule 3 — Blocking] Existing unit fixtures used an hour-long flush interval as a size-only-test convenience. The new required five-minute ownership cap correctly rejected them. Changed those fixture intervals to one second while preserving their behavioral assertions; unit race tests passed.
2. [Rule 1 — Bug] Final lifecycle inspection found explicit consumer close started fresh shutdown processing without original parent cancellation. Added and observed the named RED regression, then made close propagate its error and release ownership without starting new work. Original parent cancellation retains its bounded transfer.

No new dependency, public API, infrastructure service or schema was added.

## Issues Encountered and Deferred Findings

The standalone `golangci-lint` command was absent (exit 127); the repository-pinned `go tool -modfile=tools.mod golangci-lint` was used successfully without installing anything.

Integration-tag lint of `./test/...` exits 1 for four pre-existing findings outside this plan's ownership. All findings in the new rebalance test were repaired; the orchestrator was given these exact remaining items for the final phase gate:

- `backend/test/ingest_test.go:271`: `lll`, 126-character line.
- `backend/test/ratelimit_test.go:43`: `lll`, 151-character function signature.
- `backend/test/writer_test.go:37`: `perfsprint`, replace fixed-message `fmt.Errorf` with `errors.New`.
- `backend/test/writer_test.go:242`: `testifylint`, unnecessary `fmt.Sprintf` argument to `require.NoError`.

These findings were not changed by this executor. Full phase acceptance, dual-build dependency policy and independent re-verification remain orchestrator/02-19/02-21 work. No hosted CI result is claimed.

## Known Stubs and Threat Flags

None. New file-access/network/auth/schema surfaces were not introduced beyond the planned Kafka ownership and durable-effect boundaries. Diagnostic errors remain fixed/location-only; resource buffers and publication concurrency remain bounded. Real tests use isolated resources and finite loop/consumer cleanup; existing user volumes were preserved.

## Next Phase Readiness

CR-05 and the aggregate/rebalance portion of verification gap 2 now have behavioral RED/GREEN and real durable-effect evidence. The source test exists for the downstream nonempty CI guard. This summary does not mark Phase 2 complete. Shared STATE, ROADMAP, REQUIREMENTS and VALIDATION updates belong to the orchestrator; user-owned `.planning/config.json` and `.serena/` were untouched.

## Self-Check: PASSED

- All eight plan-owned implementation/test/documentation files exist.
- Task commits `0b011c0`, `56d8a02`, `0d89499`, `2c16598` and `c5f28c8` exist in git history.
- Named real tests, unit race checks, vet, default targeted lint, build isolation and all three key links passed as recorded above.
- Remaining integration lint findings are explicitly deferred rather than reported as passed.
