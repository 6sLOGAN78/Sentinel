---
phase: 03-detection-alert-pipeline
reviewed: 2026-10-20T04:23:49Z
depth: deep
files_reviewed: 41
files_reviewed_list:
  - backend/cmd/alert-writer/main.go
  - backend/cmd/detector/main.go
  - backend/internal/alertwriter/identity.go
  - backend/internal/alertwriter/poison.go
  - backend/internal/alertwriter/writer.go
  - backend/internal/config/config.go
  - backend/internal/detection/bounds.go
  - backend/internal/detection/candidate.go
  - backend/internal/detection/checkpoint.go
  - backend/internal/detection/config.go
  - backend/internal/detection/cooldown.go
  - backend/internal/detection/evidence.go
  - backend/internal/detection/reducer.go
  - backend/internal/detection/scan.go
  - backend/internal/detection/transition.go
  - backend/internal/detection/volume.go
  - backend/internal/detector/failpoint.go
  - backend/internal/detector/failpoint_integration.go
  - backend/internal/detector/poison.go
  - backend/internal/detector/worker.go
  - backend/internal/kafkax/alert_consumer.go
  - backend/internal/kafkax/alerts.go
  - backend/internal/kafkax/detector_consumer.go
  - backend/internal/opshttp/metrics.go
  - backend/internal/opshttp/server.go
  - backend/internal/storage/clickhouse/alerts.go
  - backend/internal/storage/postgres/alerts.go
  - backend/internal/storage/postgres/detection_decisions.go
  - backend/tools/check-alerts/main.go
  - backend/tools/detection-state/catchup.go
  - backend/tools/detection-state/main.go
  - backend/migrations/postgres/003_alert_workflows.sql
  - backend/migrations/postgres/004_detection_decisions.sql
  - backend/migrations/postgres/005_detection_config.sql
  - backend/migrations/clickhouse/003_alerts.sql
  - config/rules.yaml
  - deployments/compose/docker-compose.yml
  - generator/src/sentinel_generator/cli.py
  - generator/src/sentinel_generator/live_schedule.py
  - generator/src/sentinel_generator/scenarios.py
  - scripts/seed_scenarios.py
findings:
  critical: 2
  warning: 1
  info: 0
  total: 3
status: issues_found
---

# Phase 3: Production Code Review Report

**Reviewed:** 2026-10-20T04:23:49Z
**Depth:** deep
**Files Reviewed:** 41
**Status:** issues_found

## Narrative Findings (AI reviewer)

### Summary

Two BLOCKER defects affect normal cooperative group recovery and the alert writer's immutable-winner serialization. One WARNING affects configuration admission for the newly active alert writer. Findings below follow production call chains and the pinned dependency source; they do not infer correctness from passing tests or completed plans.

Scope is exactly `03-REVIEW-PRODUCTION-SCOPE.md`, baseline `7b77246e52412b3ca331f726d1163875daa5a609`, with production frozen after 03-13. This is a production-only review, not the complete Phase 3 review. Active 03-14 tests, fixtures, CI and documentation were not inspected or changed. No tests, stack commands, package installation, source edits, staging or commits were performed by this reviewer. No fallow structural pre-pass was supplied.

## Critical Issues

### CR-01: BLOCKER — Cooperative acquisition treats retained partitions as missing offset metadata

**File:** `/home/logan78/Desktop/Sentinel/backend/internal/kafkax/detector_consumer.go:301-304`

**Affected call chain:** `NewDetectorConsumer` → franz-go cooperative assignment → `assigned` (189-202) → `fetched` (261-306) → `fail` (167-183) → detector lifecycle failure.

**Issue:** `fetched` requires every entry in `c.Assignments()` to appear in this OffsetFetch response. During a cooperative rebalance that map includes retained partitions, whose tokens and already restored reducer state remain valid, plus new partitions that actually require offset fetching. The Kafka client fetches offsets only for additions. A valid response containing every requested new partition therefore fails the all-assignment completeness check.

**Reproducible counterexample:**

1. Two detector members own partitions `{0,1,2}` and `{3,4,5}` of the six-partition source.
2. The second member leaves. The first member retains `{0,1,2}` and acquires `{3,4,5}`.
3. `assigned` updates retained entries' member/generation at 189-194 without changing their false `RestorePending`; it creates additions with `RestorePending=true` at 196-202. Consequently `expected := c.Assignments()` at 266 contains all six.
4. The pinned client requests and returns offset metadata for `{3,4,5}`. The loop at 289-299 restores those additions correctly.
5. Lines 301-304 also demand `{0,1,2}` in `seen`, report `detector restore response incomplete`, and call `fail`. `fail` cancels the consumer lifetime and invokes the service failure callback. A normal replica departure terminates the surviving worker instead of continuing recovery.

**Pinned source evidence:** `/home/logan78/go/pkg/mod/github.com/twmb/franz-go@v1.22.1/pkg/kgo/config.go:712-714` selects `CooperativeStickyBalancer()` by default; production adds no different balancer. In `consumer_group.go`, `diffAssigned` supplies additions at 1092, `s.assign(g, added)` dispatches those additions at 1124, cooperative pending-fetch adjustment occurs at 1130, and `fetchOffsets(ctx, added)` runs at 1143. The request is built from `added` at 1982 onward. The comments preceding `adjustCooperativeFetchOffsets` explicitly state retained assignments are not fetched again. With no additions, this client skips the offset-fetch hook; an empty normal response is not needed for this counterexample.

**Consequence:** Routine rebalance recovery fails, creating detector outages and potentially repeated process/rebalance churn under replica changes. Source pins remain recoverable; this finding does not claim immediate source loss or exactly-once publication.

**Fix:** Snapshot the partitions actually pending restoration for the captured assignment and require completeness against that set. Keep strict rejection of duplicate, failed, unowned or unexpected response partitions and stale token/member/generation checks. Clear `RestorePending` only on successfully restored pending tokens; preserve retained tokens and state. Configuration-transition authorization must still inspect the full assignment. Add a regression using retained nonpending partitions plus newly acquired pending partitions and an OffsetFetch response containing only the additions, followed by a real cooperative acquisition check.

**Confidence:** High — direct mismatch between the production completeness condition and the pinned client's request construction. No live reproduction was run during this review.

### CR-02: BLOCKER — PostgreSQL releases the winner lock while a ClickHouse write can still succeed

**File:** `/home/logan78/Desktop/Sentinel/backend/internal/storage/postgres/alerts.go:31-45`

**Related files:** `/home/logan78/Desktop/Sentinel/backend/internal/alertwriter/writer.go:181-217`; `/home/logan78/Desktop/Sentinel/backend/internal/alertwriter/poison.go:24-26`; `/home/logan78/Desktop/Sentinel/backend/internal/storage/clickhouse/alerts.go:45-81,86-112`.

**Issue:** The writer deliberately holds a PostgreSQL transaction advisory lock across ClickHouse preflight and insertion, but `NewAlertStore` caps its timeout at five seconds. `BeginLocked` applies that same value to both `transaction_timeout` and `idle_in_transaction_session_timeout`. The complete `persist` attempt has the default ten-second operation timeout, and its two sequential ClickHouse operations each allow five seconds. PostgreSQL can therefore terminate the transaction and release the lock before the in-flight ClickHouse insertion has completed or been canceled. A competing writer can then pass the same preflight and insert different immutable content.

**Reproducible counterexample with default settings:**

1. Writer A begins the per-ID PG transaction at time 0. It finds no workflow. Its CH `Get` returns not-found around 4.9 seconds, within that query's five-second bound.
2. A begins CH insertion of payload A at 4.9 seconds. This insertion is delayed before becoming visible and completes at 9 seconds, still within its own five-second query bound and the ten-second overall attempt.
3. At approximately 5 seconds, PG's `transaction_timeout` terminates A's transaction and releases its advisory lock. The idle timeout is independently unsafe whenever the SQL-idle period spanning CH operations exceeds the same five-second bound. Neither timeout cancels the independent CH context.
4. Writer B, processing a different payload for the same valid alert ID, acquires the now-free PG lock at 5.1 seconds. Before A's CH insertion is visible, B sees no PG winner and no retained CH row. B inserts payload B and commits its workflow before A's delayed insertion completes.
5. A's insertion subsequently succeeds. Its following PG `Ensure`/commit fails because PG terminated the transaction, but rolling back or retrying cannot undo the already durable CH row. The stores now contain a PG winner B and conflicting CH content A/B. `AlertStore.Get` rejects these variants before merges; a replacing merge can subsequently retain content inconsistent with the PG winner.

All timings fit the configured deadlines. Both writers obey the intended lock protocol; no direct SQL mutation, malformed record or unauthorized dependency is needed. A controlled delayed CH get/insert together with two concurrent writers can reproduce the race.

**Consequence:** The immutable conflict quarantine guarantee fails: rejected content can become durable evidence, and the canonical alert can become unavailable or inconsistent. This is a correctness/data-integrity issue, not a throughput finding.

**Fix:** Keep lock and statement acquisition/query bounds separate from the transaction's whole critical-section lifetime. Both PG transaction and idle-in-transaction deadlines must outlive the complete bounded CH preflight + insert + PG ensure/commit attempt, including independent rollback/cleanup reservation. Derive that lifetime from the caller's bounded operation deadline (or pass an explicit validated critical-section budget); retain shorter lock/statement limits if desired. Do not merely check PG after the CH acknowledgment: the conflicting orphan is already durable then. Verify with a lock-lifetime regression that keeps a competing writer blocked throughout delayed CH work, and with concurrent differing payloads so only the winning immutable payload can be stored.

**Confidence:** High — the five-second PG transaction/idle limits and ten-second caller/five-second-per-CH-operation limits are explicit in the production call chain. The delayed two-writer race was not executed during this review.

## Warnings

### WR-01: WARNING — Alert-writer startup bypasses the writer resource-admission checks

**File:** `/home/logan78/Desktop/Sentinel/backend/internal/config/config.go:368`

**Issue:** `ServiceAlertWriter` omits `sectionWriter`, although its new runtime consumes `cfg.Writer` for Kafka fetching, quarantine bounds and persistence. Consequently `Load(ServiceAlertWriter)` never runs either the `WriterConfig` field validation or `validateWriterConfig` for this service. `NewAlertConsumer` delegates to `NewConsumer`, which calls only `ComputeWriterBudgets`; that function checks timing/batch/retry bounds but not fetch/concurrency/DLQ limits. `NewDLQPublisher` checks positivity/overflow, not the one-MiB configured DLQ ceiling. There is no downstream equivalent of the omitted admission checks.

**Concrete counterexample:** With otherwise valid alert-writer settings, `SENTINEL_WRITER.FETCH_MAX_BYTES=67108864` and `SENTINEL_WRITER.MAX_CONCURRENT_FETCHES=8` pass timing validation and are passed directly to franz-go. The nominal fetch allowance is 512 MiB, despite `validateWriterConfig` explicitly limiting aggregate fetch allowance to 128 MiB and the Compose alert-writer container having a 256-MiB memory limit. Similarly `SENTINEL_WRITER.DLQ_MAX_BYTES=2097152` bypasses the configured one-MiB admission ceiling and reaches the DLQ publisher. The analogous event-writer configuration is rejected.

**Consequence:** Operator settings that should fail at admission instead start an alert writer outside the defined resource/quarantine bounds. This warning concerns missing enforcement of explicit robustness limits; it does not assert measured RSS, an observed OOM, or an algorithmic performance issue.

**Fix:** Include `sectionWriter` in the `ServiceAlertWriter` required sections so the existing struct and cross-field checks run before construction. Add service-specific invalid-configuration coverage for aggregate fetch/concurrency and DLQ ceilings.

**Confidence:** High — `Load` → required sections → consumer/DLQ constructors was traced; the missing checks are not restored downstream. No runtime resource experiment was performed.

## Coverage and limitations

All 41 listed files were read. Cross-file review followed reducer admission/accounting, first-threshold evidence, event-time sealing/prefix maxima, source pins, immutable decision lookup/create, cursor clocks, ownership/config fences, acknowledged handover and restore-behind checks; alert publication and quarantine ordering; native CH writes/reads and insert-only PG workflow transactions; readiness assignment snapshots; private read-only Kafka context, schedule derivation, bounded delivery, current-run reconciliation and preservation hashes; migration schemas and Compose wiring. Unchanged contracts, Kafka consumer/DLQ helpers, database setup and the installed pinned Kafka client were consulted only to establish interfaces and behavior.

Requirements, research and the explicit cross-window cooldown choice supplied context. Selected completed summaries supplied recorded contracts/evidence; their passing results were not treated as a substitute for direct review. The earlier unexplained timeout is not attributed to either finding. This review neither ran nor certified the active integration matrix. Tests/CI/docs still require the separately scoped final review and an aggregate Phase 3 report after 03-14.

No additional concrete blocker was established in the other inspected paths. This is not a guarantee that none exists. Deliberate at-least-once delivery, bounded state accounting rather than an RSS claim, retained operational ledger history without automatic GC, stopped-owner configuration handover and the Phase 4 sequence-versus-commit-order limitation are not reported as defects.

---

_Reviewer: production review specialist (gsd-code-reviewer)_
_Depth: deep; production-only; no implementation changes or commits_
