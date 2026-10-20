---
phase: 03-detection-alert-pipeline
reviewed: 2026-10-20T05:02:14Z
depth: deep
review_kind: scoped_fix_rereview
diff_base: 2fdde70
review_head: cbc13f2
files_reviewed: 7
files_reviewed_list:
  - backend/internal/kafkax/detector_consumer.go
  - backend/internal/config/config.go
  - backend/internal/storage/postgres/alerts.go
  - backend/internal/kafkax/detector_consumer_test.go
  - backend/internal/config/config_test.go
  - backend/test/alertwriter_lock_lifetime_test.go
  - backend/test/detector_rebalance_test.go
findings:
  critical: 1
  warning: 0
  info: 0
  total: 1
status: issues_found
---

# Phase 3 Production Review — R1

## Narrative Findings (AI reviewer)

The original CR-01 and WR-01 are addressed. CR-02's reproduced live-caller five-second lease expiry is addressed, but its immutable-winner guarantee remains incomplete across uncertain remote ClickHouse completion. One BLOCKER remains. The original `03-REVIEW-PRODUCTION.md` is historical and has not been changed.

This review inspected the three production fix diffs and four focused test diffs in `2fdde70..cbc13f2`, the two repair evidence documents, and the unchanged writer/CH call chain needed to judge CR-02. It did not repeat the complete 41-file review, inspect active 03-14 changes, run tests, edit implementation, stage or commit. Focused test results below are recorded evidence from the fix authors, not executions by this reviewer. No structural pre-pass was supplied.

## Original finding disposition

| Original finding | Disposition | Reason |
| --- | --- | --- |
| CR-01 — cooperative additions-only offset response | Addressed | `validatePendingFetch` validates membership/completeness against captured pending assignments before native restoration. Retained nonpending entries need not appear and cannot be overwritten by that response. Completion retains token/member/generation checks and clears only captured pending entries. Full-assignment configuration authorization remains in place. |
| WR-01 — missing alert-writer resource validation | Addressed | `ServiceAlertWriter` now includes `sectionWriter`, routing startup through the existing field/cross-field fetch, concurrency, DLQ and timing validation. |
| CR-02 — premature winner-lock release | Partially addressed; residual BLOCKER below | The derived transaction/idle lifetime covers the live caller deadline plus a one-second independent rollback reserve; short acquisition/statement bounds remain. It closes the specific five-second-expiry race. Local synchronous return before rollback does not establish remote insertion completion or preserve a winner across ambiguous completion. |

The CR-01 evidence records compiled RED, pending/stale/foreign response controls, and a real two-member-to-one-member acquisition with both retained and acquired nine-event windows subsequently firing at event ten. The WR-01 evidence records compiled invalid-config RED and GREEN. The CR-02 evidence records native PG/CH differing-payload RED and GREEN with delayed but synchronous local effects, plus a cancellation characterization that holds local return **after an actual CH acknowledgement**. Those are appropriate proofs of their stated cases; the cancellation test deliberately does not model a server commit occurring after local return.

## Critical Issues

### CR-02-R1: BLOCKER — Uncertain CH insertion releases the only immutable winner before remote completion is known

**File:** `/home/logan78/Desktop/Sentinel/backend/internal/alertwriter/writer.go:186,207-208`

**Related fix:** `/home/logan78/Desktop/Sentinel/backend/internal/storage/postgres/alerts.go:35-79`

**Contract:** Concurrent/replayed alert IDs must have one immutable winning payload; a different payload is quarantined before it adds conflicting CH evidence. The selected policy explicitly handles overlapping writers, retained CH orphans and ambiguous acknowledgements. At-least-once delivery permits repeated identical evidence; it does not authorize two different immutable payloads for one winning identity.

**Remaining issue:** If `InsertBatch` returns a socket/context error after transmitting the insert but before a remote result is known, `persist` returns that error and immediately runs its deferred independent PG rollback. No committed winner claim survives that rollback. A different writer can acquire the same per-ID lock, observe neither a workflow nor a visible CH row, and submit a different payload before the first remote insertion becomes visible. A later successful remote completion of the first insertion then leaves conflicting evidence. Increasing the temporary lock lease does not close a lock released explicitly after local error, and the one-second cleanup reserve is not a remote-completion fence.

**Concrete fault interleaving:**

1. Two schema-valid records A/B have the same tenant, alert ID, entity/window and canonical Kafka key, but different immutable summaries/fingerprints. They reach overlapping writer attempts. For example, private/manual Kafka publication places those correctly keyed records in two consumed partitions; the writer does not reject this placement, and the selected concurrent-conflict contract already requires handling overlapping first writers. Normal single-partition ordered replay by itself is not asserted to produce this overlap.
2. A acquires PG's advisory transaction lock; PG workflow and CH preflight both find no winner. A transmits its full native CH insert, including the terminating data block. Server processing is delayed before the part becomes visible.
3. A's client connection fails or its query context expires. Its synchronous `Send` returns an error without receiving a definitive server success/error or an acknowledged remote abort. `persist` returns at 208 and deferred rollback at 186 releases the lock. A's source is uncommitted, but that does not revoke its remote insert.
4. B acquires the released lock. Its PG `Find` sees no committed workflow and its CH `Get` sees no visible A part. B inserts B and commits the B workflow.
5. A's already accepted remote work reaches its commit/visibility point after that preflight, without its client receiving the result. CH now contains A and B while PG names B's fingerprint. Immediate CH `Get` rejects the differing variants; eventual replacing merges can discard a variant without reconciling it to PG.

This is a failure schedule allowed by the current protocol, not an assertion that a lost acknowledgement always causes delayed visibility. If A is visible before B's preflight, the retained-orphan check correctly prevents the conflict. The defective interval is specifically local error/rollback **before remote finality or durable winner reservation**.

**Pinned implementation evidence:** `clickhouse-go/v2@v2.49.0/conn_batch.go:203-239` sends buffered data and installs a cancellation watchdog which closes TCP. `closeQuery` at 307-314 sends the terminal empty block and waits in `process`. `conn_process.go:95-128` can return on caller cancellation; `cancel` at 223-231 writes a cancel packet and closes the connection without draining a completion response. `context_watchdog.go:13-28` also supplies no remote completion acknowledgement. Thus neither returning a local context/network error nor sending a best-effort cancel establishes that the insert cannot commit later.

The production CH store returns `batch.Send()` directly (`storage/clickhouse/alerts.go:81`). Its five-second query context is a client bound. `storage/clickhouse/client.go:28-41` supplies connection/read/pool limits, not a cross-store completion/ownership protocol. The current native alerts insert sets no server completion fence. Even a server execution timeout by itself would not durably reserve the winning fingerprint before B's admission.

**Primary semantics:** ClickHouse distinguishes an explicit server rejection from not receiving the server's answer: in the latter case the client cannot know the insertion outcome. Its atomicity/isolation guarantees permit concurrent readers to observe the state before an insertion becomes visible. These documented semantics support treating the local error as uncertain; they do not provide a remote-finality deadline tied to the PG lease. [ClickHouse insert transaction guarantees](https://clickhouse.com/docs/concepts/features/operations/insert/transactions)

**Consequence:** Differing content can escape quarantine and become durable CH evidence despite the immutable winner contract. This remains a correctness/data-integrity blocker, not a request for distributed exactly-once delivery or a performance optimization.

**Minimal remediation choices requiring design authorization:**

- Persist an immutable per-alert fingerprint plus recoverable canonical payload/claim **before** allowing any CH insert. All writers must honor that committed authority across rollback, process loss and ambiguous CH completion; retries can then submit only the reserved identical payload. Create/retain the workflow only under its existing post-CH durability semantics, with claim recovery defined separately.
- Alternatively, use a sink-enforced ownership/idempotency protocol that atomically excludes differing fingerprints for the identity, with a demonstrated remote completion/abort barrier before any authority can be transferred.

A longer finite temporary lease, a post-insert PG check, an extra CH read immediately after cancellation, a generic timeout, or documentation declaring remote work ambiguous does not independently provide either property. A durable claim need not make CH and PG commits atomic, but it must prevent a second differing payload while the first insert's outcome is uncertain. No schema/protocol implementation is proposed as already authorized by this report.

**Required proof:** A bounded controlled fault must allow A's complete request to reach CH while withholding its commit/visibility or result, cause the local call to return and the original PG transaction to end, then attempt B. Verify that B cannot create a different CH variant or workflow winner and that A can recover after a process boundary. Also retain the original delayed-live-caller and identical-replay proofs. A model can establish protocol causality, but a real native client/server fault proof is still needed for the selected deployed protocol.

**Confidence and limits:** High confidence in the missing durable authority/remote-finality barrier, established by the production and pinned-driver paths. The exact delayed-remote-commit interleaving was not executed against pinned server 26.3.47.6 during this review; no claim is made about its frequency or a particular observed incident. The official semantic contract plus the absence of a sink fence permits this failure schedule; the evidence files explicitly leave it unproved and unsolved. Passing local lease/cancellation tests does not close it.

## Fix-diff review limits

No additional concrete defect was established in the scoped production changes. The lifetime arithmetic checks the PG millisecond ceiling before adding cleanup and rounds upward; caller rejection occurs before beginning a transaction; short lock/statement bounds remain; there is no new cancellation callback that rolls back while the local synchronous operation is still running. These observations close the original local five-second counterexample only.

This R1 does not certify complete Phase 3 behavior, active CI/docs/tests, the full long-running matrix, or preserved-volume acceptance. Original 41-file coverage belongs to the historical report; R1 lists its actual seven-file diff scope. Implementation of a durable ownership/claim protocol would be a material follow-up requiring a concrete authorized design and another scoped review.

---

_Reviewer: production review specialist (gsd-code-reviewer)_
_No implementation edits, test executions, staging or commits during R1._
