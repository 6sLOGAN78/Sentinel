# Durable Alert Reservation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent conflicting immutable alert content across uncertain ClickHouse completion without moving workflow creation before evidence durability.

**Architecture:** Commit an immutable PG reservation under the existing per-ID lock before any CH insert, then reacquire the lock to persist only its saved payload and create a missing workflow. Keep existing bounded retries, source-prefix/DLQ rules and immediate retained-evidence validation. Pair native ACK-loss recovery with a separately labeled controlled delayed-visibility fault model.

**Tech Stack:** Existing Go module, pgx/tern, native clickhouse-go, franz-go, PostgreSQL and pinned plain ReplacingMergeTree fixtures; no new dependencies.

**Spec:** [03-ALERT-RESERVATION-DESIGN.md](03-ALERT-RESERVATION-DESIGN.md), approved by user “approve” on 2026-10-20 after explanation of the selected approach.

**Status:** User approved with “okk” on 2026-10-20; execution starting. This is a supplemental CR-02-R1 repair plan, not an additional SDK-counted phase plan. Phase 3 remains 20/21 complete.

## Global Constraints

- “Preserve the at-least-once contract, canonical alert identity/fingerprint, tenant isolation, source-prefix/DLQ ordering, existing bounded retry budgets and mutable workflow semantics.”
- “Stop before Phase 4. Do not reset user volumes, regenerate canonical goldens, push or claim hosted CI success.”
- “Add a new forward tern migration, `backend/migrations/postgres/006_alert_reservations.sql`. Do not edit already-applied migrations.”
- `canonical_payload`: required `bytea`, 1..1048576 bytes; `alert_id`/fingerprint: lowercase 64-hex; tenant syntax unchanged.
- “A trigger rejects UPDATE and DELETE”; no sequence, FK to workflows/decisions, automatic TTL/GC, lease expiry or ownership transfer.
- “Do not add a second serial CH preflight to the same attempt”; “The two transactions release their pool connection before acquiring the next.”
- “The additional operations consume existing budgets; do not increase retry counts, operation timeouts, source batch limits or rebalance deadlines to hide failures.”
- Existing GSD configuration uses shared branch/no worktrees. Serialize runtime writers and explicit ownership transfers; preserve dirty `.planning/config.json`, `.serena/`, and paused 03-14 CI/docs/guard drafts. Do not stash/reset them.
- Use `apply_patch` for edits; formatting commands may operate only on owned files. Record expected assertion RED before behavior implementation, then GREEN and review per task. Never treat compile errors or no matched tests as RED proof.

## Review Focus

1. Reservation COMMIT succeeded but its answer was lost: no CH call in that failed attempt; independent replay must recover the same reservation (Task 2/3).
2. Legacy workflow/CH orphan predates the new table: lazy reservation must preserve its authoritative fingerprint and full mutable workflow/sequence (Task 2).
3. Matching immutable fingerprint but differing `status`/`detected_at`: recover the saved canonical payload, not the new presentation (Task 2/3).
4. Corrupt stored bytes, mismatched identity/fingerprint or serialized-payload boundary: fail closed without replacing authority or poisoning valid input (Task 1/2).
5. Reservation committed but process dies before CH, or native ACK is lost: no premature workflow/source commit, no differing CH variant, and no dependence on process memory (Task 3).

## File Ownership and Execution Order

Tasks 1–3 are sequential. A runtime implementer owns migration/store/writer/tests for its assigned task only; the previous runtime task must be reviewed before proceeding. Root owns planning state, evidence coordination and serialized tracking. Task 4 hands CI/docs/guard files back to the paused 03-14 executor after runtime review. Do not edit a file owned by another active agent.

Fresh reviewer gates check spec conformance and code quality after each task; whole-fix review checks the combined invariant. Preserve the existing specialist-agent execution choice rather than asking the user to choose it again. Every handoff must say the worker is not alone and must preserve others' changes.

Commands below run from the repository root unless explicitly prefixed with `cd backend`. Long commands yield output periodically; run no competing live producer suites against the preserved stack.

---

### Task 1: Add the immutable reservation schema without changing runtime behavior

Completed and independently reviewed at `ca7a4cd`; RED `f08a060`. Detailed execution tracking is in the plan-scoped SDD ledger. Runtime protection remains Task 2 onward.

**Files:**

- Create: `backend/migrations/postgres/006_alert_reservations.sql`.
- Create: `backend/test/alert_reservations_schema_test.go` (`integration` build tag).
- Modify: `backend/migrations/migrations_test.go` only to check new embedded migration/forward contract.

**Interfaces:**

- Consumes: `migrations.Postgres()`, `database.Migrate(ctx, cfg, log)`, native pgx and existing test-owned PG fixture.
- Produces: `alert_reservations(alert_id,tenant_id,immutable_fingerprint,canonical_payload,reserved_at)` with the exact spec constraints and an UPDATE/DELETE rejection trigger. No runtime APIs change yet.

- [ ] **Step 1 — Write `TestAlertReservationsSchema` and an embedded migration assertion.** Use an isolated test database, not user schemas. Cover fresh migration, upgrade from tern version 5 with an existing resolved workflow, second migration no-op, original workflow row/sequence unchanged, zero workflow/sequence allocation from reservation insertion, and one-row PK uniqueness. Assert payload lengths 1 and 1048576 are accepted by the SQL storage bound; 0 and 1048577, malformed IDs/fingerprints/tenants are rejected (`23514`), and UPDATE/DELETE raise the immutable-ledger error. For the upgrade use the pinned tern `MigrateTo(ctx, 5)` on a new test database, never downgrade a populated/shared database.
- [ ] **Step 2 — Observe expected RED.** Run `bash scripts/go-test-nonempty.sh './test/...' '^TestAlertReservationsSchema$' -tags=integration -timeout 6m -p 1`. Baseline must compile and fail because the reservation relation is absent; record the actual assertion/log and commit tests before adding migration logic.
- [ ] **Step 3 — Add forward migration.** Use global text PK, existing tenant/fingerprint checks, bounded bytea, `reserved_at timestamptz NOT NULL DEFAULT clock_timestamp()`, and a dedicated rejection function/trigger for UPDATE OR DELETE. Follow tern's existing up/down separator; never execute down migration on user data.
- [ ] **Step 4 — Verify GREEN.** Repeat the exact family; run `bash scripts/go-test-nonempty.sh './migrations/...' '^TestMigrations' -timeout 60s` and `cd backend && go vet ./migrations/...`. Require real success, unchanged preexisting workflow JSON/seq and no extra migration or CH changes.
- [ ] **Step 5 — Commit and review.** Commit only the named schema/tests, retain RED/GREEN evidence, then review migration safety and spec coverage before Task 2. A schema-only commit does not claim that runtime insertion is protected.

### Task 2: Enforce committed reservations in the existing writer path

**Files:**

- Create: `backend/internal/storage/postgres/alert_reservations.go`.
- Create: `backend/internal/alertwriter/reservation.go`.
- Modify: `backend/internal/alertwriter/writer.go` (port plus persistence ordering only).
- Modify: `backend/internal/alertwriter/writer_test.go` (transaction-aware fake and ordering expectations, without weakening effects/prefix assertions).
- Create: `backend/internal/alertwriter/reservation_test.go`.
- Create: `backend/test/alertwriter_reservation_test.go` (`integration`).
- Modify: `backend/test/alertwriter_lock_lifetime_test.go` only to distinguish deferred cleanup of a successfully committed reservation transaction from rollback of the still-open native-effect transaction (execution ruling below).

**Interfaces:**

- Preserve `BeginLocked(context.Context,string)(pgx.Tx,error)`, `Find(context.Context,pgx.Tx,string)(string,string,error)` and `Ensure(context.Context,pgx.Tx,*contracts.Alert,string) error`.
- Add to the existing `WorkflowStore` port and implement on native `*postgres.AlertStore`:
  - `FindReservation(context.Context, pgx.Tx, string) (tenant string, fingerprint string, payload []byte, err error)`; return `pgx.ErrNoRows` for absence.
  - `InsertReservation(context.Context, pgx.Tx, string, string, string, []byte) error`, with arguments ID, tenant, fingerprint, payload; parameterized insert-only `ON CONFLICT (alert_id) DO NOTHING`, no implicit commit.
- Private writer type `reservationAttempt` contains `alert *contracts.Alert`, `preflightDone bool`, `retained bool`.
- Private helpers: `loadReservation(context.Context,pgx.Tx,string)(*contracts.Alert,string,error)`, `prepareReservation(context.Context,*contracts.Alert,string)(reservationAttempt,error)`, `persistReserved(context.Context,reservationAttempt,string) error`.
- Keep the existing `persist(context.Context,*contracts.Alert,string) error` as the single orchestration entry; the PG adapter imports contracts, never alertwriter. Stored-state errors use a sanitized operational error distinguishable from `ErrConflict`.

- [ ] **Step 1 — Write behavior RED through the current public Writer API.** `TestAlertWriterReservation` wraps the real native CH store: immediately before delegating `InsertBatch`, query PG through an independent connection and assert a committed matching reservation/canonical payload already exists. The current writer compiles but fails because its only PG authority remains uncommitted/no reservation. Also write `TestAlertWriterReservationProtocol` with distinct transaction objects/commit outcomes and test-local extra reservation methods; adding extra methods to the fake must not require changing the production interface to compile RED.
- [ ] **Step 2 — Observe and commit RED.** Run the exact native family with the nonempty runner, `-tags=integration -timeout 6m -p 1`, and the pure family `bash scripts/go-test-nonempty.sh './internal/alertwriter/...' '^TestAlertWriterReservationProtocol$' -race -timeout 60s`. Capture the intended order/independent-visibility failure, not compilation failure or mock-call counts alone.
- [ ] **Step 3 — Implement the port/native helpers and two-phase protocol.** `prepareReservation` acquires the current lock, validates global workflow/reservation authority, performs CH orphan preflight only if reservation is absent, serializes/selects saved bytes and inserts/re-reads a missing reservation. Validate strict schema/identity, stored tenant/ID, recomputed fingerprint and 1..1048576 serialization bound. Commit before return; any commit error returns without CH insertion. A fresh statement must read the selected row after insert-or-ignore. `persistReserved` reacquires under the same deadline, validates reservation/workflow again, uses the earlier preflight result or performs exactly one now, inserts only the selected saved alert, and creates only a missing workflow after CH evidence succeeds. Each transaction gets independent deferred rollback and releases its connection before the next begins.
- [ ] **Step 4 — Complete focused acceptance cases.** Assert: reservation alone never creates workflow/source commit; conflicting immutable input is quarantined without CH insertion; equal incoming mutable-only changes use the saved payload; legacy resolved workflow and matching retained orphan are preserved; differing orphan creates no claim; CH failure retains claim but not workflow/source acknowledgement; reservation commit failure has zero CH calls in that attempt; workflow commit failure preserves claim; corrupt reservation is operational/uncommitted, not DLQ poison. Cover missing/invalid payload, mismatched ID/tenant/fingerprint and same-ID conflict. Include a single-connection PG pool case proving phase 1 releases its connection before phase 2. Test SQL helpers directly under native transactions now that the API exists; use independent reads to distinguish uncommitted from committed rows.
- [ ] **Step 5 — Verify, commit and review.** Run both exact new families; then `cd backend && go test -race -timeout 120s ./internal/alertwriter/... ./internal/storage/postgres/...`. Independently run native `TestAlertWriterConflict`, `TestAlertWriterConcurrentReplay`, `TestAlertWorkflowReplay`, `TestAlertWriterPartialStorage`, `TestAlertWriterPoison`, `TestAlertWriterLockLifetime`, `TestAlertWriterDifferingPayloadLockLifetime`, `TestAlertWriterCancellationLockLifetime`, each through the nonempty runner with its existing 6m bound. Run bounded backend build/vet and default/integration lint. Do not change the old live-delay tests to hide an extra CH read or loosen deadlines. Review the runtime diff before Task 3; CR-02-R1 is not closed by these tests alone.

Execution ruling: the existing cancellation fixture observes every deferred `Rollback`, including no-op cleanup after a successful reservation commit. Its one-slot channel otherwise reports an irrelevant pre-CH cleanup and can block the actual effect-tx rollback. Make that test-only observer phase-aware by excluding only cleanup after a positively acknowledged successful commit; preserve observation/assertions on the still-open CH-effect transaction, exact physical evidence, source status and all deadlines. This is not permission to suppress actual rollback or uncertainty evidence.

### Task 3: Prove reservation survival across process and remote-result boundaries

**Files:**

- Create: `backend/test/alertwriter_reservation_fault_test.go`, `backend/test/alertwriter_reservation_restart_test.go`, `backend/test/alertwriter_ackloss_proxy_test.go` (all `integration`).
- Create: `backend/internal/alertwriter/failpoint.go` (`!integration`), `backend/internal/alertwriter/failpoint_integration.go` (`integration`), `backend/internal/alertwriter/failpoint_test.go` (`!integration`).
- Modify: `backend/internal/alertwriter/writer.go` only for the integration boundary call after reservation commit and before the second lock/CH effect.

**Interfaces:**

- Consumes: Task 2 port/native store, existing `newAlertReplayFixture`, `f.run`, `f.binary`, real Kafka group offsets, existing process lifecycle helpers and `driver.Conn`/`driver.Batch` wrappers.
- Private hook: `afterReservationBeforeInsert(context.Context,string) error`; default production implementation unconditionally returns nil and reads no environment/configuration.
- Integration hook uses once-only `SENTINEL_TEST_ALERT_WRITER_AFTER_RESERVATION_BARRIER` and `SENTINEL_TEST_ALERT_WRITER_ALERT_ID`, private 0700 parent, exclusive 0600 `.reached`, validated regular `.release`, 10ms polling and minimum of caller deadline/30s. No raw payload, keys or DSNs in markers/logs. The ID selector prevents unrelated source rows from hitting the barrier.
- Proxy/helper code stays in test files. It uses unchanged native connections and existing exported `PrepareBatch(...)(driver.Batch,error)`/`Batch.Send() error`; no replacement production sink.

- [ ] **Step 1 — Add the process-boundary failing assertion before hook behavior.** `TestAlertWriterReservationRestart` runs an actual integration-built alert-writer binary, waits at the specified post-reservation/pre-CH barrier, checks committed saved bytes with no CH row/workflow/source advancement, kills only that test-owned process, then restarts without controls and verifies exact saved payload, one workflow and durable source prefix. Against Task 2 without the boundary call it must fail the missing boundary assertion rather than silently skip. Use a fresh alert ID/topic/group and private files for each subcase.
- [ ] **Step 2 — Add the causal and commit-ambiguity schedules.** `TestAlertWriterReservationAmbiguity` uses real PG with a controlled detection store. A records its accepted payload then returns uncertain while still invisible; observe A's original transaction end by independent per-ID lock acquisition; attempt B with differing fingerprint through independently consumed, correctly keyed test input before A is replayed; only then release A's deferred effect and replay A. This explicitly models overlapping writers, not normal serial delivery of one partition. Require no B CH write, one immutable PG claim, A-only physical/native persisted fingerprint after release, no premature workflow and correct DLQ/source prefixes. Separate subcases use test-owned tx wrappers for committed-but-answer-lost and rolled-back reservation commits, plus workflow commit ambiguity; never use a generic fake commit that cannot represent the distinction. Record which tests characterize already-correct Task 2 behavior rather than calling a passing test RED.
- [ ] **Step 3 — Implement only bounded integration controls and native fault harness.** Add the no-op/guarded hooks after recording Step 1 RED. `TestAlertWriterReservationNativeAckLoss` places A's native connection behind a test-owned TCP proxy. Complete native PrepareBatch normally; a test-only Batch.Send wrapper arms downstream response suppression immediately before delegating the real Send. Forward client-to-server bytes unchanged, independently prove physical A is visible, then force a real frontend socket error. This avoids inventing a native packet parser or blocking the sample/header handshake. Assert native Send returned error, the workflow tx ended, claim survived and original source is uncommitted. Publish correctly keyed B to a separate test-owned alert topic/group sharing the same PG/CH stores; run a fresh real writer process to reject B before replaying A in its original group. This prevents A's recovered workflow from masking the reservation-only boundary. B must never add a physical CH variant; A's original offset remains uncommitted until its recovery. Bound/join proxy goroutines and connections; cleanup only exact test-owned resources. Injecting an error after a successful real Send is not a substitute for this native ACK-loss case.
- [ ] **Step 4 — Verify controls are absent from default behavior.** `TestAlertWriterFailpointBuildBoundary` must call the default helper with fault environment set and assert nil/no files; build a default binary and verify its source list excludes the integration hook. Strengthen with default-binary healthy replay in the real integration restart acceptance, so invalid configuration startup alone cannot certify no runtime fault effects.
- [ ] **Step 5 — Verify, commit and review evidence honestly.** Run `TestAlertWriterReservationAmbiguity`, `TestAlertWriterReservationRestart`, `TestAlertWriterReservationNativeAckLoss` independently through the integration nonempty runner at 6m each; repeat the restart/native families twice with fresh fixtures. Run the default failpoint family with `-race -timeout 60s`, backend build/vet and lint. Record exact commands/exits and actual transaction/process/socket boundaries. Native ACK loss proves lost result after visible insertion; controlled delayed visibility proves the selected protocol schedule, not a stock-server pre-visibility pause. No new CH engine/custom server instrumentation or timeout widening. Scoped production re-review must explicitly assess both proof classes and the durable-authority invariant before closing CR-02-R1.

### Task 4: Register acceptance and return to the incomplete 03-14 closeout

**Files:**

- Modify paused executor drafts only after explicit handback: `scripts/check-phase3-tests.sh`, `scripts/test_phase3_guards.py`, `docs/operations.md`, `docs/architecture.md`, `docs/threat-model.md`.
- Create: `.planning/phases/03-detection-alert-pipeline/03-ALERT-RESERVATION-EVIDENCE.md`.
- Reviewer produces a new scoped `03-REVIEW-PRODUCTION-R2.md`; root alone updates STATE/PROJECT/validation after evidence review. Preserve historical production/R1/repair reports.

**Interfaces:**

- Register five new native families: `TestAlertReservationsSchema`, `TestAlertWriterReservation`, `TestAlertWriterReservationAmbiguity`, `TestAlertWriterReservationRestart`, `TestAlertWriterReservationNativeAckLoss`, each independently at 6m with integration tag.
- Register two pure alertwriter families: `TestAlertWriterReservationProtocol`, `TestAlertWriterFailpointBuildBoundary`, each independently at 60s with race enabled.
- Preserve all original 39 package/name families; registry becomes 46, not a broad combined regex. Preserve Phase 2 CI groups and the existing 8m cooldown/16m scheduled/32m repeat limits.

- [ ] **Step 1 — Guard RED then GREEN.** Extend the fake-Go characterization's package/name expectation and inject empty/failure independently for all seven new families before updating the shell registry. Run `python3 scripts/test_phase3_guards.py`, observe intended registry failure, then register the exact families and require all guard tests to pass. Fake-Go success is selector coverage, not native acceptance.
- [ ] **Step 2 — Document operational compatibility.** Record permanent claim retention, additive migration, stopped/drained old writers, no mixed old/new writer protocol and fail-closed reconciliation of old uncertain writes. Keep workflow creation post-CH, mutable replay preservation and no exactly-once claim. Do not migrate/restart the user's preserved stack until the old-writer drain/outcome precondition is checked; unresolved uncertainty is reported, never cleared by deleting volumes.
- [ ] **Step 3 — Run scoped whole-fix gates and review.** `timeout 300s make test`; `timeout 300s make lint`; `cd backend && go vet ./...` and integration-tag vet; bounded build and integration-tag lint. Run the seven new exact native/pure families on final code and retain all Task 2 regressions. Write evidence distinguishing RED/GREEN, protocol model, real native ACK loss, process restart and unproved exact native delayed visibility. Independent R2 must show disposition of CR-02-R1; no automatic downgrade or accepted risk on the user's behalf.
- [ ] **Step 4 — Resume existing 03-14 only after R2 allows it.** Reconcile the previous isolated-static-all and preserved-repeat aggregate failures using existing fixed diagnostics. Run actual `make test-phase3` and bounded `make test-integ` on the final code, serially; record real commands/outcomes, not reuse old passes as final proof. Finish the existing plan's operator/Compose checks, SUMMARY-before-tracking and root gate. Then complete required phase-wide review/security/goal verification; hosted CI remains unclaimed without an authorized run.
- [ ] **Step 5 — Final handoff.** Phase 3 completion requires the existing workflow's actual gates, not this plan document or focused passes. Preserve 20/21 count until 03-14 really completes. Report any remaining failures/blockers honestly and stop before Phase 4.

## Plan Self-Review

- Spec coverage: schema/retention/upgrade → Task 1; immutable authority/canonical replay/deadlines/workflows/prefixes → Task 2; commit/process/native-result failures and default-build boundary → Task 3; registration/rollout/evidence/re-review/03-14 return → Task 4.
- Interfaces: native helpers stay on existing AlertStore/WorkflowStore; worker-private alertwriter helpers do not introduce storage-to-writer imports; test wrappers embedding AlertStore inherit the added methods. The standalone order fake is updated explicitly.
- Review focus: all five listed failure classes have named test ownership above; no test alone certifies remote finality or complete Phase 3.
- Proportion: implementation bodies are left to the implementer; no placeholders or unrelated refactors. Main self-review performed inline; no implementation started or plan approval inferred from written-design approval.

## User Review Gate

The written design is approved. Please review this implementation plan and confirm it captures the intended change. The already selected specialist-agent execution method is preserved. After this plan is approved, proceed task-by-task with test/review gates; do not ask again for the same design or execution-method choice unless a material new decision arises.
