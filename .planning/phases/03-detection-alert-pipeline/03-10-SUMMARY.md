---
phase: 03-detection-alert-pipeline
plan: "10"
subsystem: database
tags: [go, postgres, clickhouse, kafka, replay, dlq, tdd]
requires:
  - phase: 03-17
    provides: Extracted native stores and bounded locked two-store persistence
provides:
  - Rule-specific identity policy and immutable fingerprint regression coverage
  - Live bounded conflict quarantine before source acknowledgement
  - Real concurrent winner, resolved replay, partial-write and ambiguous-send proofs
affects: [03-11, 03-12, 03-14, phase-4]
tech-stack:
  added: []
  patterns: [global alert-ID winner serialization, retained orphan repair, native acknowledgement-loss testing]
key-files:
  created:
    - backend/internal/alertwriter/identity.go
    - backend/internal/alertwriter/identity_test.go
    - backend/test/alertwriter_conflict_test.go
    - backend/test/alertwriter_partial_test.go
  modified:
    - backend/internal/alertwriter/writer.go
    - backend/internal/alertwriter/writer_test.go
    - backend/cmd/alert-writer/main.go
key-decisions:
  - "03-10: Preserve proven global-ID lock/native-store guards and skip workflow INSERT entirely for an equal authoritative winner."
  - "03-10: Inject the existing bounded DLQ publisher for permanent identity/content conflicts; infrastructure failures remain uncommitted and never poison."
requirements-completed: [ALRT-01, ALRT-02]
duration: 13 min
completed: 2026-10-18
---

# Phase 3 Plan 10: Immutable Winner and Partial Replay Summary

Conflicting canonical alerts now reach the existing bounded DLQ before source acknowledgement; concurrent identical replay and native partial/ambiguous writes preserve one immutable identity and the entire workflow row.

## Performance

- Started: 2026-10-18T22:54:11Z; resumed after the quota interruption at 2026-10-18T22:57:51Z (October 19 locally).
- Implementation and verification completed: approximately 2026-10-18T23:07Z.
- Duration: approximately 13 minutes including context restoration and real-container verification.
- Tasks: 3/3; application/test files changed: 7. The two existing native store files required no change.

## Accomplishments

- Extracted the already-correct identity policy into `identity.go`. AUTH retains the whole first-pipe principal suffix, including Unicode and additional pipes; SCAN accepts exactly two IPs; FLOOD/DENY accept one. Strict schema, supported rule, tenant/key and unchanged canonical DedupeKey checks precede all store calls. Existing golden IDs remain unchanged.
- Tests vary every immutable field, ordered sample evidence and display dedupe text independently. Status and optional detection wall clock leave the shared immutable fingerprint unchanged. The storage adapter retains fingerprint ownership, avoiding a reverse dependency on the writer.
- The default binary constructs the existing `kafkax.NewDLQPublisher`, registers its bounded closer, and injects it before Run. Permanent key/hash/content conflicts use the existing `validation_error` classification and trusted source metadata; decode poison uses the existing `decode_error`. No new reason or raw error envelope was introduced. Publisher attempts share the finite owned effects budget and retain a source-commit reservation. A failed quarantine cannot acknowledge its record.
- Retained the existing parameterized global alert-ID PG advisory lock, authoritative tenant/fingerprint read and retained CH orphan check before any CH insertion. The lock remains held through CH acknowledgement and PG commit, with independently bounded rollback on every exit. Equal winners now skip workflow INSERT entirely, avoiding unnecessary defaults/sequence allocation. New workflows remain insert-only; no resolved status, assignee, version, seq, timestamps or list fields are overwritten.
- Real conflict cases cover changed payload, changed tenant rejected by canonical validation, an authoritative PG tenant mismatch, conflicting CH orphan, and two independent default binaries competing for the first winner. Exactly the loser is quarantined with its actual source topic/partition/offset, while the winner's CH/PG content remains consistent.
- Four independently owned real Kafka consumer groups replay one resolved alert concurrently and compare the complete PG row. Partial tests execute real CH Send and real PG INSERT before injecting failures only through private integration-test wrappers. CH-success/PG-failure rolls back PG; ambiguous CH success masks only the native acknowledgement. Both leave source offset zero, one immediate CH identity, no PG row and a broker DLQ end offset of zero. A new consumer in the same group repairs PG and commits offset one; another identical replay preserves the complete workflow at offset two. No OPTIMIZE or user-resource fault was used.

## Task Commits

1. **Task 1: Expose concurrency/conflict/partial boundaries** — `12c0d99` (`test`).
2. **Task 2: Extract identity policy and wire live quarantine** — `41de063` (`feat`).
3. **Task 3: Skip equal-winner workflow insertion and verify real recovery** — `945d733` (`fix`).

## Recorded Verification

Every required family ran separately with the nonempty runner. Package timings below are Go's reported times, including TestMain/container setup where applicable, not measured shell latency. Unit ceilings were 60 seconds; all real families used `-tags=integration -timeout 6m -p 1`.

| Command / stage | Actual result |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/alertwriter/...' '^TestAlertIdentity$' -timeout 60s`, before extraction and final | Both PASS, exit 0, package 0.008s. Existing identity behavior characterized honestly. |
| `scripts/go-test-nonempty.sh './internal/alertwriter/...' '^TestImmutableFingerprint$' -timeout 60s`, before extraction / final | PASS, exit 0, packages 0.006s / 0.005s. |
| `scripts/go-test-nonempty.sh './internal/alertwriter/...' '^TestAlertWriterOrdering$' -timeout 60s`, original five cases | PASS, exit 0, package 0.033s. |
| Same Ordering command after adding equal-winner INSERT avoidance, before refinement | Assertion RED, exit 1, package 0.058s; successful compilation, unwanted Ensure produces the expected replay failure. |
| `scripts/go-test-nonempty.sh './internal/alertwriter/... ./internal/contracts/...' '^TestAlertIdentity$' -timeout 60s` after extraction | PASS, exit 0, writer 0.007s; contracts has no matching case. |
| Same package list with separate `'^TestImmutableFingerprint$'` / `'^TestDedupeKey'` / `'^TestAlertWriterOrdering$'` selectors | All PASS, exit 0. Final writer fingerprint 0.005s, ordering 0.034s; canonical contracts DedupeKey 0.009s. Each selector has its own actual PASS. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterConflict$' -tags=integration -timeout 6m -p 1`, pre-quarantine | Assertion RED, exit 1, package 39.130s; no acknowledged quarantine. Initial earlier run 45.533s also failed but had a consumer-handoff fixture issue, corrected before relying on the proof. |
| Focused `'^TestAlertWriterConflict$/CH_orphan_changed_payload$'` with the same real flags, pre-quarantine | Definitive assertion RED, exit 1, test 6.79s/package 24.943s. Final captured default-binary logs show actual immutable conflict detection and exit without DLQ, distinguishing missing behavior from setup/compilation failure. |
| Full Conflict selector after live quarantine | GREEN, exit 0, initial test 2.64s/package 21.091s; expanded five-case test 3.78s/package 26.168s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterPartialStorage$' -tags=integration -timeout 6m -p 1`, baseline / final normal | Existing recovery characterization PASS, exit 0, tests 2.45s / 2.47s, packages 23.324s / 20.966s. Final run injects the real DLQ and proves its broker end offset remains zero. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterConcurrentReplay$' -tags=integration -timeout 6m -p 1`, baseline / final normal | PASS, exit 0, tests 1.62s / 1.86s, packages 21.146s / 22.246s. |
| Final independent Conflict selector, additionally `-race` | PASS, exit 0, five cases/test 4.23s/package 26.780s. Default subprocess binaries remain ordinary builds; the integration harness is race-instrumented. |
| Final independent ConcurrentReplay selector, additionally `-race` | PASS, exit 0, test 1.63s/package 30.624s; real in-process writers/native stores are race-instrumented. |
| Final independent PartialStorage selector, additionally `-race` | PASS, exit 0, test 2.51s/package 23.333s; real in-process writers/native fault wrappers are race-instrumented. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWorkflowReplay$' -tags=integration -timeout 6m -p 1` | PASS, exit 0, test 1.21s/package 21.985s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertWriterPipeline$' -tags=integration -timeout 6m -p 1` | PASS, exit 0, test 1.28s/package 22.655s; preserves real blocked-PG ordering. |
| `scripts/go-test-nonempty.sh './test/...' '^TestAlertsSchema$' -tags=integration -timeout 6m -p 1` | PASS, exit 0, test 0.23s/package 19.726s. |
| `timeout 300s make test` | PASS, exit 0; full backend Go race suite, web 6 tests and generator 99 tests. |
| `timeout 300s make lint` | Final PASS, exit 0: Go 0 issues, web lint/typecheck and generator checks. Initial exit 2 found one owned test line-length issue, subsequently repaired. |
| `cd backend && timeout 120s go build ./... && timeout 120s go vet -tags=integration ./...` | PASS, exit 0. |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | Final exit 1; sole existing out-of-scope `test/recovery_test.go:162` unparam warning. Earlier runs found owned fixture complexity/shadow/format/constant-parameter findings, repaired without changing lint policy. |
| `git diff --check` | PASS, exit 0. |

## Decisions Made

Reuse the proven native stores rather than rewriting them to manufacture a RED test. The new RED assertions target actual missing live quarantine and unnecessary equal-winner insertion; pre-existing identity, orphan repair and concurrency protection remain passing characterization guards. The Postgres skill and pinned pgx documentation reaffirm explicit independently bounded rollback because Begin cancellation alone does not roll back a transaction. The accepted protocol deliberately holds this bounded per-ID transaction across CH acknowledgement.

## Deviations from Plan

1. **[Rule 3 - Existing API name]** The plan names `cfg.Writer.DLQMaxValueBytes`; the actual pinned configuration field is `DLQMaxBytes`. Used that existing field and publisher constructor, matching event-writer, with no configuration/schema change (`41de063`).
2. **[Rule 1 - Fixture ownership]** The first conflict fixture left its one-shot consumer open until test cleanup, delaying the next same-group owner. Explicit bounded close now occurs immediately after Run; a focused orphan case captured the final worker error and supplied definitive assertion RED (`12c0d99`).
3. **[Rule 2 - Required conventions]** Wrapped owned long lines and split the conflict fixture into focused helpers; removed shadowed locals and a constant physical-count parameter. Final standard lint is clean, and integration lint has only the pre-existing deferred warning (`945d733`).

The extracted PG and CH stores already satisfy the global lock, parameterized winner/orphan reads, fresh Abort-safe batches, insert-only creation and bounded rollback requirements. They were read and exercised but remain byte-identical. Seven application/test files changed within the nine-file ownership allowance. There was no architectural expansion, installation, remote write or authentication gate.

## Issues Encountered

Two formatting invocations used repository-prefixed paths while already in backend; those paths were absent, no files changed, and formatting was rerun successfully from repository root. Initial RED/fixture and lint failures are retained above rather than reported as predeclared successes.

## Deferred Issues and Remaining Dependencies

- Existing integration lint `backend/test/recovery_test.go:162` unparam remains the recorded 03-14 obligation.
- 03-11 still owns the broader poison/DLQ outage, aggregate-budget and partition-prefix adversarial proofs. This plan wires the live publisher and proves conflict quarantine plus non-poison operational failures, not that later complete suite.
- Later plans retain detector boundaries/recovery, actual cross-window cooldown, live scheduling, preserved-state Compose acceptance and full Phase 3 security/CI/goal verification. The parent Wave 4 cross-plan gate remains unclaimed.
- ALRT-01/ALRT-02 frontmatter identifies this plan's slice; no global requirement checkbox or phase-complete operation is authorized or performed.

## User Setup Required

None. Only dedicated test topics/groups, private CH databases and integration-owned wrappers were faulted. User keys, environment, volumes, configuration edits and `.serena/` were preserved and excluded from commits.

## Stub and Threat Surface Scan

No TODO/FIXME/placeholder, unwired implementation or goal-blocking stub remains. Empty fingerprint status and nil detection time are intentional mutable-field exclusions. Test-only empty slices/pointers are fixtures, not UI data sources. No unplanned endpoint, authentication path, file-access pattern or schema boundary was introduced. Registered identity, winner and bounded partial-effect mitigations have the scoped evidence above; no phase-wide security verdict is claimed.

## Self-Check: PASSED

- All seven created/modified application/test files exist; the two unchanged native stores also exist and retain their original guards.
- `12c0d99`, `41de063` and `945d733` exist in order and delete no tracked files.
- Actual assertion RED precedes each new production behavior; final independent named unit and real families pass.
- Full tests, standard lint, build and integration vet pass; the sole integration-lint exception is recorded explicitly.
- SUMMARY is committed before serialized STATE/ROADMAP updates. Phase 3 remains in execution.
