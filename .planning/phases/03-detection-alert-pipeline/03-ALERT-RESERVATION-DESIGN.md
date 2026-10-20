# Durable Alert Reservation — Design for CR-02-R1

Date: 2026-10-20

Status: Written design awaiting user review; not implemented

Baseline: `3cd0474`

Related review: [03-REVIEW-PRODUCTION-R1.md](03-REVIEW-PRODUCTION-R1.md)

## Intent and authority

The operator needs one immutable winning alert payload despite crashes, restarts, concurrent conflicting input and uncertain ClickHouse acknowledgements. The user approved adding a durable PostgreSQL reservation, migration and recovery/failure tests with “okk do it,” following the explicit scope-expansion question. That approval permits this concrete design; it is not evidence that the change exists or passes.

This is an architectural addition to the existing alert-writer path, not a new delivery service. Preserve the at-least-once contract, canonical alert identity/fingerprint, tenant isolation, source-prefix/DLQ ordering, existing bounded retry budgets and mutable workflow semantics. Stop before Phase 4. Do not reset user volumes, regenerate canonical goldens, push or claim hosted CI success.

## Alternatives and selection

1. **Selected: committed PG reservation before any CH insert.** Store the immutable fingerprint and recoverable canonical alert payload independently of workflow creation. It adds one operational table and a commit boundary, but prevents conflicting admission even when a previous CH request outlives its client.
2. **Sink-enforced ownership/finality protocol.** This could also exclude differing payloads, but the present CH table and native driver expose no demonstrated identity-level conditional insert or acknowledged abort barrier. Introducing a different sink protocol is a larger change.
3. **Longer temporary lock or more reads.** Keep existing finite lock safety, but reject this as the remedy: releasing a transaction after local failure still loses authority before remote finality. An immediate extra read cannot prove the absence of a later remote effect.

The reservation is permanent winner authority, not a lease. It is not a claim that PG and CH commit atomically or that physical CH duplicates disappear.

## Persistent state

Add a new forward tern migration, `backend/migrations/postgres/006_alert_reservations.sql`. Do not edit already-applied migrations.

`alert_reservations` contains only:

| Column | Contract |
| --- | --- |
| `alert_id` | Global text primary key; existing lowercase 64-hex constraint |
| `tenant_id` | Required existing tenant syntax constraint |
| `immutable_fingerprint` | Required lowercase 64-hex fingerprint using the unchanged shared policy |
| `canonical_payload` | Required `bytea`, 1..1048576 bytes; strict validated alert JSON serialized with the existing Go contract |
| `reserved_at` | Required server timestamp for operator diagnosis; not an expiry or commit-order cursor |

The primary key provides the needed point lookup and uniqueness; no speculative secondary indexes, sequence, status, ownership token or background queue. Parameterize all runtime values. A trigger rejects UPDATE and DELETE, following the existing immutable operational-ledger pattern but also preventing ordinary deletion. Privileged schema removal/admin corruption is outside the writer threat boundary.

There is no workflow foreign key: a reservation must exist while no workflow exists. There is no dependency on `detection_decisions`, since schema-valid manually published alerts also enter the writer. No automatic TTL/GC is safe while old source data or in-flight CH work could return. The migration down section is destructive maintenance only, never an automatic failure-recovery path.

The 1 MiB persisted-payload bound matches the existing alert decode ceiling. Validate the serialized length before storage; a valid producer record must not create an oversized internal representation unnoticed. Do not silently introduce a smaller admission limit.

## Writer protocol

Retain the existing per-alert advisory transaction lock and finite caller-derived transaction lifetime, including independent rollback cleanup. It coordinates local admission and workflow creation; the committed reservation, not the lifetime of that lock, protects against remote uncertainty.

### 1. Establish and commit authority

Under the existing per-ID lock:

- Read any reservation and existing workflow by global ID. Validate a stored reservation with strict decode, schema/identity checks, tenant/ID equality and recomputed fingerprint before using it. Corrupt operational state fails closed as an operational error, not permission to replace it or quarantine a valid incoming alert as poison.
- An existing workflow remains authoritative. Any incoming or reserved tenant/fingerprint mismatch is a conflict; never replace or mutate that workflow.
- An existing reservation rejects a differing incoming tenant/fingerprint before any CH insert. Matching replay selects the saved payload, including the first selected `status`/`detected_at` representation; those fields remain excluded from the immutable fingerprint as today.
- Preserve retained CH orphan preflight. With no reservation, read CH while holding the lock: a visible differing retained payload rejects the incoming record without creating a reservation. A matching retained payload supplies the canonical payload; otherwise the validated incoming alert supplies it. Carry this attempt's completed preflight result into the next phase. With an existing reservation, defer this one CH preflight to phase 2. Existing PG workflow fingerprint checks still apply if CH retention has removed the evidence.
- Insert a missing reservation using insert-only conflict handling. Never `DO UPDATE` a winning reservation. Re-read and validate the selected persisted row after a no-op conflict, using a fresh statement snapshot; do not infer success from a zero-row `RETURNING` result.
- Commit this PG transaction **before** any CH insert. On any commit error, do not insert into CH during that attempt, even if the PG outcome may have succeeded. Existing bounded retries/replay re-read committed authority and resolve the uncertainty.

### 2. Persist the reserved payload and workflow

After a positively acknowledged reservation commit, reacquire the per-ID transaction lock under the same original operation deadline. Re-read/validate the reservation and workflow; never carry a stale mutable workflow assumption across the commit boundary.

- Use only the saved canonical alert for CH insertion and workflow creation, never substitute matching incoming bytes.
- Keep immediate retained CH validation, using this attempt's phase-1 result when a reservation was newly established, or performing the preflight now when a reservation already existed. Do not add a second serial CH preflight to the same attempt: the existing single-read/single-insert operation bounds must remain viable. A matching visible row can satisfy the existing immutable evidence effect; a missing row is inserted from the reservation. Another conforming writer in the inter-transaction interval can only add the same reserved payload, so a duplicate insert is safe. A conflicting visible variant is never overwritten or repaired automatically.
- If CH returns any error, roll back the current workflow transaction, leave the reservation intact and leave the source record uncommitted. A later remote completion is now constrained to the reserved payload.
- Only after CH success/matching retained evidence, create a missing workflow using the existing insert-only `Ensure`. An equal existing workflow skips INSERT entirely, preserving `seq`, triage status/version/actor/times and audit state.
- Commit the workflow transaction. Ambiguous PG completion leaves source uncommitted and resolves through replay without allocating a replacement workflow or reservation.

The two transactions release their pool connection before acquiring the next; do not hold two pool slots or add an out-of-band persistence path. The additional operations consume existing budgets; do not increase retry counts, operation timeouts, source batch limits or rebalance deadlines to hide failures.

### 3. Source effects

Existing permanent identity/content conflicts use the bounded sanitized DLQ path. A conflicting record becomes source-committable only after its DLQ acknowledgement. Infrastructure/claim-corruption/uncertain-commit failures remain uncommitted and are not poison. Partition-contiguous durable prefixes and independent final source commit time remain unchanged.

Reservation alone is not a completed source effect: it creates no workflow, alert-list entry or source acknowledgement. No sweeper is added. The original uncommitted source replays into recovery; stored canonical bytes allow that replay to resume without trusting a changed payload. Loss of both the source retention and the databases is not repaired by this change.

## Safety argument and recovery

Every conforming CH insertion has a previously committed reservation for the global alert ID. The reservation cannot change or expire. Every insertion for that identity must use its validated saved payload. Therefore a local timeout, transaction rollback or process loss cannot admit a second differing payload, even if a first native request is still running remotely. Identical physical duplicates remain possible and are handled by existing immediate read-time identity deduplication.

| Failure boundary | Required recovery |
| --- | --- |
| Before reservation commit | No CH insert is permitted; replay repeats admission |
| Reservation commit answer lost | No CH insert in that failed attempt; replay reads the committed row or safely creates it if absent |
| After reservation commit, before CH | No workflow/source commit; new process uses saved bytes |
| CH outcome unknown, workflow transaction ended | Reservation remains; differing B is quarantined without a CH insert; identical A can resume |
| CH durable, before workflow commit | Replay finds matching evidence and inserts one workflow |
| Workflow commit answer lost | Replay preserves the existing workflow and its sequence/triage state |
| DLQ outcome unknown for differing B | B source remains uncommitted; it never becomes eligible for CH insertion |

Safety assumes all active alert writers honor the reservation protocol and the configured PG durability contract remains intact. This does not retroactively cure conflicting CH variants or defend against privileged direct sink writes.

## Migration and existing data

Use additive forward migration and the existing embedded tern runner; repeat migration execution must be a no-op. No CH migration, broad backfill or rewrite of workflows/decision ledgers is needed. Existing workflows and retained CH orphans are lazily reconciled on first replay under the original lock, preserving the old winner rather than selecting a new one.

Do not run old and new writer binaries together: old writers bypass the reservation. Drain and stop all old alert-writer processes before deploying the protocol, then migrate and start only new writers. A prior uncertain old-protocol insertion must be explicitly reconciled before rollout; a stopped process alone is not proof that remote work ended. If graceful drain or remote-outcome reconciliation is unresolved, do not silently roll forward. Existing user databases, topics and volumes remain intact.

## Verification required for closure

Write assertions before implementation and record real expected RED failures, not missing-symbol/compile failures. Keep deterministic protocol causality tests distinct from native fault evidence.

- Real PG migration/constraints: fresh and existing-workflow databases, rerun no-op, immutable update/delete refusal, payload bounds, invalid fingerprints/tenants, no workflow/sequence allocation from reservation alone.
- Competing first writers: one global reservation, equal replay returns the saved canonical payload, differing fingerprints/tenants cannot insert CH content. Preserve existing orphan and resolved-workflow regressions.
- Reservation commit ambiguity: both committed-but-answer-lost and not-committed cases allow no premature CH call; independent replay resolves the state.
- Process boundary: commit reservation, stop the test-owned writer before CH, then start another process with changed mutable-only incoming fields and prove saved payload, one workflow and unchanged durable source-prefix rules.
- Delayed-remote causality: A's local insert returns uncertain, its workflow transaction ends, B attempts before A's deferred effect completes, then A completes/replays. Assert no B CH effect, only A's fingerprint in all physical CH variants, correct quarantine/source prefixes and one A workflow. A controlled model establishes this protocol property but is not labeled native-server proof.
- Actual native client/server fault: through a bounded test-owned TCP proxy, forward A's unchanged native insert while suppressing its completion response. Independently establish A's physical payload/fingerprint is visible, then force the client socket error. Observe the original PG transaction end and the uncommitted source, restart the writer, attempt differing B and replay A. Require durable reservation survival, B rejection before any CH insertion, only A's physical fingerprint/payload and unchanged workflow recovery. Do not use a sleep as transaction-end evidence.
- Evidence limits: the native ACK-loss case proves recovery after a real completed server insert with lost result, not a native pre-visibility delay. The separate deterministic fault model covers that stricter schedule. Scoped re-review must judge the reservation invariant and both proof classes before closing CR-02-R1; neither test alone automatically closes it. An exact delayed-visibility native run remains unproven unless a real barrier is demonstrated on the pinned plain ReplacingMergeTree binary. Do not change engines, buffer the final request block, or count proxy socket writes as evidence that CH parsed a complete request.
- Retain real live-caller lock-lifetime and local cancellation tests, identical replay, native PG/CH conflict/concurrency/workflow/partial/DLQ/crash cases, race tests and bounded build/vet/lint.
- After scoped re-review clears the actual residual blocker, finish 03-14: explain/reconcile both aggregate scenario failures, pass the actual full guarded matrix and full integration, complete operator docs and phase-wide review/security/goal verification. Hosted CI remains unclaimed without an authorized run. Do not fabricate a 03-14 SUMMARY or close Phase 3 from focused passes.

## Implementation surface and non-goals

Expected surface: new migration; cohesive native PG reservation helpers; alert-writer workflow-store boundary and persistence ordering; corresponding unit/native integration fixtures and migration assertions; later the owned CI guard registry/operator docs. Exact signatures and test commands belong to the subsequent implementation plan.

No new dependencies, broker topics, public API fields, alert schema/ID changes, detection algorithm/cooldown changes, replacement CH engine, distributed transaction coordinator, background dispatcher, automatic claim transfer or automatic claim deletion.

## Evidence and primary semantics

Repository basis: `alertwriter/writer.go` currently rolls back its only PG authority after a CH error; `storage/postgres/alerts.go` supplies bounded transaction locks and insert-only workflow creation; `storage/clickhouse/alerts.go` returns native `Send` and validates retained variants; migrations 003/004 establish workflow and immutable-ledger conventions. R1 records the pinned-driver cancellation paths and the limits of existing tests.

PostgreSQL read-committed statements can observe newly committed rows in successive statements, and insert-or-ignore may produce no inserted row after a concurrent conflict; use explicit validated re-read rather than a same-statement visibility assumption. [PostgreSQL transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html), [INSERT](https://www.postgresql.org/docs/current/sql-insert.html).

ClickHouse distinguishes a definite answer from an unknown result after loss of the server response; local uncertainty is not remote abort proof. The reservation protocol is our proposed cross-store safety argument, not a claim that ClickHouse supplies PG-style conditional ownership. [ClickHouse insert transaction guarantees](https://clickhouse.com/docs/concepts/features/operations/insert/transactions).

The read-only fault-harness specialist found no deterministic pre-visibility pause hook established for this pinned plain MergeTree path: part publication occurs within its commit path, whereas the identified pre-commit failpoint is replicated-path-specific. This is a limit of the proposed stock-server fixture, not a proof that the old failure schedule is impossible. Use the native ACK-loss plus explicitly labeled protocol-model gates above. [Pinned MergeTree insert sink](https://raw.githubusercontent.com/ClickHouse/ClickHouse/v26.3.47.6-lts/src/Storages/MergeTree/MergeTreeSink.cpp), [pinned failpoints](https://raw.githubusercontent.com/ClickHouse/ClickHouse/v26.3.47.6-lts/src/Common/FailPoint.cpp), [pinned native TCP handler](https://raw.githubusercontent.com/ClickHouse/ClickHouse/v26.3.47.6-lts/src/Server/TCPHandler.cpp).

## Review checklist

- Intent, data-retention and at-least-once boundaries are explicit.
- Reservation precedes every new CH insert; workflow remains post-CH.
- Existing winners/orphans and equal replay do not change canonical identity or mutable workflow state.
- Commit ambiguity, corruption, migration rollout and pool/deadline bounds have explicit behavior.
- Native fault evidence and controlled-model evidence are not conflated.
- Implementation and final phase completion remain unclaimed pending their actual gates.
