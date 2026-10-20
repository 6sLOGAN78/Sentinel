---
phase: 03-detection-alert-pipeline
reviewed: 2026-10-20T11:43:45Z
depth: deep
review_kind: scoped_reservation_whole_fix_review
diff_base: fa33ed0eee76e7b3ed11c5aeda3683125a7675be
review_head: 3a26feb6b5d095dafff47711a293513e13f77c0c
files_reviewed: 16
files_reviewed_list:
  - backend/internal/alertwriter/failpoint.go
  - backend/internal/alertwriter/failpoint_integration.go
  - backend/internal/alertwriter/failpoint_test.go
  - backend/internal/alertwriter/reservation.go
  - backend/internal/alertwriter/reservation_test.go
  - backend/internal/alertwriter/writer.go
  - backend/internal/alertwriter/writer_test.go
  - backend/internal/storage/postgres/alert_reservations.go
  - backend/migrations/migrations_test.go
  - backend/migrations/postgres/006_alert_reservations.sql
  - backend/test/alert_reservations_schema_test.go
  - backend/test/alertwriter_ackloss_proxy_test.go
  - backend/test/alertwriter_lock_lifetime_test.go
  - backend/test/alertwriter_reservation_fault_test.go
  - backend/test/alertwriter_reservation_restart_test.go
  - backend/test/alertwriter_reservation_test.go
focused_unchanged_repository_contract_files_inspected: 17
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 3 Production Review — R2

## Narrative Findings (AI reviewer)

CR-02-R1 is addressed by the complete durable-reservation repair at the frozen backend head. No remaining BLOCKER or WARNING was established in the scoped change. This verdict covers the reservation invariant and its direct contracts; it does not certify completed Phase 3, active CI/docs/guard changes, the final full integration matrix, or deployment of the preserved stack.

The review began with `03-RESERVATION-REVIEW-SCOPE.md`, then read the approved design, historical R1, all three task reports and review snapshots, and the complete 2,304-line frozen whole-fix package through EOF. All 16 listed source files were reviewed, including full contents of the modified existing writer, ordering tests, migration tests and lock-lifetime tests. Frozen source reads used `git show` at the stated head. The frontmatter list is the exact changed-file review scope; the additional bounded contract inspections are itemized below and are not a second baseline review. No structural pre-pass was supplied. No root `AGENTS.md`, `.codexignore`, or project `.codex/skills/` / `.agents/skills/` directory was present; none of the 16 scoped files was excluded by the checked ignore rules.

No tests, compilation, live writes, staging, commits or nested agents were run by this reviewer. Only this new report was written. Execution results cited below are attributed author evidence, not fresh reviewer executions. The code-review and Karpathy guidelines kept this review focused on provable failure paths and the approved scope.

## CR-02-R1 disposition and invariant

**Disposition: addressed; original residual BLOCKER closed for conforming reservation-protocol writers.** The argument depends on immutable committed authority, rather than a timeout or an assumption that a local native error aborts remote work.

The only writer insertion path now runs `persist` → `prepareReservation` → acknowledged reservation COMMIT → `persistReserved` → `DetectionStore.InsertBatch`. At `backend/internal/alertwriter/reservation.go:91`, any reservation commit error returns to `writer.go:185` before an insertion is eligible. At `reservation.go:100`, phase two reloads and validates the committed reservation; `reservation.go:125` inserts that reloaded alert. Incoming replay bytes and the retained preflight object's mutable representation cannot replace it in phase two.

The global primary key and immutable UPDATE/DELETE trigger in `backend/migrations/postgres/006_alert_reservations.sql:4,16` preserve the authority after transaction rollback or process loss. `backend/internal/storage/postgres/alert_reservations.go:23` uses parameterized insert-or-ignore and never updates a winner; `reservation.go:82` performs a separate validated read instead of inferring the selected winner from attempted insertion. There is no expiration, workflow foreign key, sequence, claim transfer, or recovery deletion.

Consequently, once A can submit a native insertion, A's tenant, immutable fingerprint and recoverable saved representation have already committed. If A's insertion returns uncertainty and its effect transaction ends, differing B still encounters A's reservation at `reservation.go:87` and returns conflict before B can reach any insertion. A remote completion after that point can add only A's saved representation. Identical physical duplicates remain possible and permitted; the repair does not claim atomic PG/CH commits or exactly-once effects.

### Native ACK loss and delayed visibility

These are different proof classes and both matter:

| Boundary | Evidence inspected | What it establishes |
| --- | --- | --- |
| Real completed native insertion with lost result | `backend/test/alertwriter_ackloss_proxy_test.go:150,162` | The wrapper arms only after real PrepareBatch completes, calls the real native Send, and suppresses only downstream replies. Independent native physical queries establish A's insertion and exact payload while Send is still pending. Actual socket closure must produce EOF/non-timeout socket failure, rather than a synthetic post-success error or caller timeout. |
| Transaction end and B admission before A recovery | `backend/test/alertwriter_ackloss_proxy_test.go:236`; `backend/test/alertwriter_reservation_fault_test.go:50,63` | After the uncertain call returns, independent same-ID PG lock acquisition proves transaction release. Reservation/no workflow/uncommitted A source are asserted. A fresh writer process consumes conflicting B from another real keyed source, acknowledges its DLQ/source effect, and leaves A's source untouched before A replay. |
| A locally fails while its effect remains invisible; B runs before A's effect is released | `backend/test/alertwriter_reservation_fault_test.go:22,132` | The explicitly controlled detection model saves A's request and returns uncertainty without inserting. Real PG authority remains after the transaction ends; independent B is rejected while CH remains empty. Only then is A released into real native CH and recovered by another writer process. |
| Process loss after reservation and before any effect | `backend/test/alertwriter_reservation_restart_test.go:132` | An integration-built child reaches the committed-reservation boundary with no physical CH row, workflow, or source acknowledgement, is killed and joined, then a new process consumes mutable-only changed input and persists the original saved representation. The original group subsequently replays without replacing workflow sequence/state. |

The task-3 report records repeated native ACK-loss results with actual `read: EOF` and 2,784 suppressed response bytes, repeated process restart results, and the five ambiguity subcases. Its native test is a real client/server fault after physical visibility. It is **not** an executed native pre-visibility pause. The stricter A-error/transaction-end/B-attempt/A-visibility schedule is established by the controlled model plus inspection of the production admission path. Stopping merges only on the UUID-owned fixture table preserves physical conflicting variants for these assertions; it does not pause insertion or change the engine.

An exact pre-visibility barrier on the stock pinned plain ReplacingMergeTree server remains unproved. That limit is not silently treated as proof of remote abort. It also does not leave the former integrity defect open: the new committed reservation excludes B independently of whether A is visible, still running, later commits, or never commits. This code-level invariant, together with real native uncertainty, independent transaction-release evidence and process recovery, supplies the required closure. No fabricated stock-server proof is required or claimed.

## Failure-path assessment

`loadReservation` at `backend/internal/alertwriter/reservation.go:26` checks stored byte bounds, strict JSON decoding, schema/canonical identity, saved tenant/ID equality and recomputed fingerprint. Malformed saved authority remains a sanitized operational failure, not a source-poison classification or permission to elect another winner. A missing row in phase two also fails operationally. Ordinary storage errors propagate without creating a workflow/source effect.

Preparation checks an existing global workflow before establishing a claim and rejects a differing tenant/fingerprint. A missing reservation performs retained-orphan preflight while locked: a differing retained payload creates no claim, while an equal orphan supplies the saved representation. Phase two rechecks workflow state after the inter-transaction gap. Equal workflows bypass Ensure entirely (`reservation.go:131`), preserving sequence allocation and mutable triage data. An existing reservation performs its one preflight in phase two; newly established authority carries the already completed preflight flag. There is no added second serial CH read per attempt.

Reservation commit uncertainty never permits that attempt's CH insertion. Workflow commit uncertainty leaves the claim intact and source unacknowledged, so replay can observe the workflow if it committed or recreate it after matching CH evidence if it did not. The unit model distinguishes staged versus committed reservation/workflow state, and the native fault wrappers actually COMMIT or ROLLBACK before returning the injected unknown answer. These are outcome-characterization fixtures, not claims of native PG wire-level COMMIT response loss.

Both phases use the same caller operation context. Preparation returns and finishes its deferred cleanup before phase two acquires a pool connection. The unchanged pgxpool implementation releases the associated connection after either Commit or Rollback; native pgx closes a failed rollback connection and reports a COMMIT command that actually rolled back. Thus success does not retain a first pool slot while acquiring a second. A successful preparation commit's deferred rollback is already closed; it does not add another full cleanup wait. The single-slot native fixture checks this dependency explicitly. Original finite operation, retry, shutdown and server transaction bounds remain in use.

The direct `persistRecord` caller still sends only `ErrConflict` through the bounded DLQ path after persistence has returned/released its transaction. Infrastructure and corrupt-claim errors remain uncommitted. `Writer.process` advances only each partition's completed contiguous prefix, with a separate final source-commit allowance. Reservation creation alone is not treated as completion, workflow creation, or a DLQ acknowledgement. There is no new source-commit path.

The default build's hook is an unconditional no-op. The integration-only hook is after preparation returns and before the second transaction, has an exact identity/once-only gate, finite caller-derived wait, exclusive private marker creation, and no payload-bearing marker content. Proxy sockets, listener, goroutines and task-owned child processes have explicit finite cleanup/join paths. No additional security defect was established in these changes. The new imports do not create a storage-to-writer dependency or package cycle.

## Deferred minor and evidence rulings

The Task-2 minor at **`/home/logan78/Desktop/Sentinel/backend/internal/alertwriter/writer_test.go:90`** is explicitly triaged as **a non-finding test-helper limitation**, not an accepted runtime defect or a counted WARNING. Its odd/even BeginLocked count can label the next preparation as an effect transaction if an earlier preparation aborts. In the current callers, the preparation-error ordering case has `MaxAttempts: 1` and ends that run; the conflict case ends in quarantine; successful multi-record use completes both transactions per persistence. No current retry-after-preparation-error assertion uses this parity helper. The separate `protocolWorkflows` fixture models transaction closure/commit state and covers preparation ambiguity followed by replay. Therefore the limitation does not presently invalidate a reported test outcome. If this helper is extended to preparation retries or additional mixed failure records, replace parity with explicit transaction phase/commit state or reuse the transaction-aware fixture, and add a preparation-abort-then-retry case before relying on its labels. This is a concrete conditional maintenance recommendation, not a style warning.

Both approved fixture rulings are preserved:

- `backend/test/alertwriter_lock_lifetime_test.go:215,224` suppresses observer notification only for deferred Rollback after a positively acknowledged successful Commit. It still delegates that rollback and observes open/uncertain effect transactions. The original cancellation/lock, actual CH effect and uncommitted-source assertions remain. It does not label local cancellation as remote abort.
- `backend/test/alertwriter_reservation_restart_test.go:189` uses the existing finite crash-recovery offset observer only for the original SIGKILL group. The configured 45-second Kafka session timeout explains why the ordinary 12-second observer was unsuitable. Production operation/retry/session settings and ordinary replay observers were not increased.

Task 3 records a genuine compiled behavior RED for the missing pre-effect boundary, but **there is no separate tests-only RED commit**. The report preserves that provenance deviation and does not reconstruct history. This limits independent tests-first provenance; it is not a demonstrated production defect. The native ambiguity/ACK-loss characterizations are not mislabeled as RED failures. Task 1's later test-only lint repair is present in the frozen complete package; the older Task-2 report's upstream lint concern is historical rather than a live unchanged defect.

## Bounded unchanged-contract coverage

These reads addressed named repair risks only. They did not repeat the unchanged 41-file Phase 3 review or inspect active CI/docs/guard work. There were 17 additional repository contract files inspected, with the following actual coverage:

| Contract risk | Frozen file and inspected area |
| --- | --- |
| Saved-state identity and mutable-field fingerprint policy | `backend/internal/alertwriter/identity.go:1-62`; `backend/internal/contracts/alert.go:1-213`; strict decoder/tenant policy in `backend/internal/contracts/event.go:165-270` |
| Source poison classification, retry/deadline and DLQ effects | `backend/internal/alertwriter/poison.go:1-84` |
| Native PG advisory lock lifetime, workflow Find/Ensure and cleanup | `backend/internal/storage/postgres/alerts.go:1-104` |
| Native CH Send, retained conflict/read and shared fingerprint contract | `backend/internal/storage/clickhouse/alerts.go:1-128`; connection/pool configuration in `backend/internal/storage/clickhouse/client.go:1-52` |
| Real service wiring and failure/shutdown propagation | `backend/cmd/alert-writer/main.go:45-110` |
| Additive migration execution and existing schema conventions | `backend/internal/database/migrator.go:1-52`; `backend/migrations/postgres/003_alert_workflows.sql:1-26`; `backend/migrations/postgres/004_detection_decisions.sql:1-65` |
| Fixture source separation, canonical Kafka keys, committed offsets and actual writer settings | `backend/test/alertwriter_conflict_test.go:1-135,312-348` |
| Integration build, UUID CH database provenance and crash offset observer | `backend/test/recovery_test.go:75-115,135-184` |
| Child process environment, join/exit and termination contracts | `backend/test/lifecycle_test.go:20-95,175-235` |
| Bounded consumer cleanup | `backend/test/writer_test.go:115-155` |
| Independent DLQ observation, origin indexing and cleanup | `backend/test/writer_rebalance_test.go:85-165` |
| Existing Kafka session/fetch/rebalance settings | `backend/internal/kafkax/consumer.go:1-80` |

Pinned external implementation reads were limited to the relevant native driver boundaries: `clickhouse-go/v2@v2.49.0/conn_batch.go:195-250,295-320`, `conn_process.go:90-135,215-240`, and `context_watchdog.go:1-28`; `pgx/v5@v5.11.0/tx.go:185-245` and `pgxpool/tx.go:1-70`. They confirm that a local CH cancellation/socket error supplies no remote finality acknowledgement, while PG transaction cleanup releases the pool resource. The reservation protocol is safe despite that continuing native uncertainty.

## Existing-data rollout and remaining limits

The code does not retroactively repair conflicting historical CH variants, old-protocol uncertain writes, privileged ledger mutation/schema removal, or loss of the configured PG durability guarantee. All active writers must honor the reservation protocol. No mixed old/new writer rollout is safe.

Root separately reports that preserved PG/CH/Redis were already stopped and that it stopped the exact five restart-looping applications, without removing data or volumes; dependencies remain stopped. This is attributed operational context, not a live check by this reviewer and not evidence that retained data is consistent. Confirmed termination of the actual old CH server removes still-running requests on that instance; stopping only an old writer would not supply that fact.

Before starting any new writer, keep old writers disabled, apply the additive migration and reconcile retained legacy outcomes against global workflow winners/physical CH fingerprints for any known uncertain old identities. Restore dependencies with writers still disabled for those checks. A conflict or unresolved legacy outcome remains a rollout stop; do not clear it by deleting data or inferring absence from one read while old remote work could still execute. Given confirmed server termination plus completed retained-outcome reconciliation, no additional native pause fixture, claim backfill, protocol change or volume reset is required by this review. This report does not claim that the preserved-stack reconciliation has happened.

Author reports record focused native families, race tests and build/vet/lint results; those are supporting evidence only. R2 does not replace final exact-family execution on final code, the complete guarded/full integration runs, CI/docs/operator review, security/goal verification, or 03-14 completion. Historical original/R1 reports remain untouched. Proceeding with the authorized remaining Phase 3 gates is supported; declaring Phase 3 complete is not.

---

_Reviewer: production review specialist (gsd-code-reviewer). No implementation edits, test executions, staging or commits during R2._
