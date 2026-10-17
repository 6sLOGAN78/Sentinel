# Phase 3 — Current Code Pattern Map

Mapped inline by the orchestrator from current Phase 1/2 files, 2026-10-17. This replaces reliance on the stale pre-Phase-1 map. Paths below are existing analogs, not new implementations or passed Phase 3 tests. Planner should expand target paths into its exact file ownership list.

## File Classification

| Target role / proposed path | Existing analog | Required reuse / deliberate difference |
|---|---|---|
| internal/detector/worker.go | backend/internal/eventwriter/writer.go | Explicit consumer/publisher interfaces, bounded processing budget, lifecycle failure; detector commits state pins rather than consumed-next offsets |
| internal/kafkax/detector_consumer.go | backend/internal/kafkax/consumer.go | Disable auto commit; BlockRebalanceOnPoll; bounded fetches; add metadata-aware checkpoints/ownership hooks without changing event-writer semantics |
| internal/kafkax/alerts.go | backend/internal/kafkax/producer.go | Same bounded synchronous all-ISR producer; alert key tenant|alert_id and strict validated payload |
| internal/alertwriter/writer.go | backend/internal/eventwriter/writer.go | Per-partition contiguous durable effects and shared poison/retry deadline; two storage effects required before commit |
| internal/storage/clickhouse/alerts.go | backend/internal/storage/clickhouse/events.go | Fresh native PrepareBatch/Append/Send; abort unsent batch; bound tenant parameters; immediate read dedupe |
| internal/storage/postgres/alerts.go | backend/internal/database/database.go | Existing pgx pool; explicit bounded parameterized transaction and advisory-lock ownership; insert-only replay path (new store policy has no existing alert-store analog) |
| cmd/detector/main.go; cmd/alert-writer/main.go | backend/cmd/event-writer/main.go | Start ops lifecycle first, connect dependencies, register closers, worker goroutine failure via lc.Fail, draining last |
| internal/detector/failpoint*.go | backend/internal/eventwriter/failpoint.go; failpoint_integration.go | Default no-op; integration-only private once-only barrier and finite wait |
| backend/test/detector*_test.go; alertwriter*_test.go | backend/test/recovery_test.go; writer_rebalance_test.go; main_test.go | Shared real containers; dedicated test-owned fault resources and integration binary when hooks needed; exact observed offsets/IDs, no arbitrary sleeps |
| generator/src/sentinel_generator/scenarios.py; cli.py | generator/src/sentinel_generator/events.py; send.py; cli.py | Seeded random source and exact serialized retry body; private manifests; documentation IPs; reuse sender and key handling |
| scripts/scenario acceptance | scripts/seed.py | Exact current-run tenant/time/ID manifest, finite polling, no historical rows satisfying success |
| migrations/*/003_* | backend/migrations/postgres/002_producer_keys.sql; clickhouse/002_events.sql | Additive embedded migrations; tern separator; repeatable ClickHouse DDL; no hardcoded database name |

## Concrete Interface Excerpts

Existing event-writer consumer interface:

```go
PollRecords(context.Context, int) ([]*kgo.Record, error)
CommitRecords(context.Context, ...*kgo.Record) error
AllowRebalance()
```

The detector needs a separate checkpoint commit method; do not retrofit CommitRecords to silently mean a replay pin.

Existing native event store pattern:

```go
batch, err := s.conn.PrepareBatch(ctx, insertEventsSQL)
// defer Abort when !batch.IsSent(); Append explicit columns; batch.Send()
```

Existing lifecycle pattern in command mains:

```go
lc.Add("ops-http", func(closeCtx context.Context) error { /* bounded shutdown */ })
// register dependency closers; fail worker through lc.Fail(error)
lc.Add("draining", opshttp.DrainCloser(draining, cfg.Ops.DrainHold))
```

Existing identity API (backend/internal/contracts/dedupe.go):

```go
func DedupeKey(tenant, ruleID string, entity []string, windowStart time.Time) string
```

It is the only alert-ID producer. Preserve the domain separator and golden vectors. Parse display entity_key rule-specifically without discarding a pipe-containing principal suffix.

## No Analog Found

- Pure event-time reducer/window/first-threshold/cooldown logic: implement the selected research invariants, not an event-writer counter clone.
- Prefix watermark/sealed-floor recovery checkpoint: use the pinned official kgo metadata hooks in RESEARCH; a consumed-next commit wrapper is not equivalent.
- Rule config fingerprint/drained version handover: explicit finite state machine and behavioral tests; current environment loader does not supply replay safety.
- Late metric: existing ops routes are health/readiness only; extend with a bounded fixed numeric exposition without weakening the repaired readiness admission/deadline behavior.

## Shared Patterns

- Bounds at admission, one aggregate deadline, finite retries, sanitized terminal failures. Never erase unexpired state to look healthy.
- Kafka/CH duplicate physical effects are permitted; visible identity dedupe is immediate. PostgreSQL conflict handling never mutates workflow state on replay.
- Tenant and opaque identifiers are SQL parameters; no tenant/IP/principal/event-ID metrics labels or raw payload/error/credential logs.
- Existing user data, local keys, .planning/config.json edits and .serena/ survive. No resets, OPTIMIZE, volume cleanup, branch switching or unrelated staging.
- All new acceptance families use scripts/go-test-nonempty.sh; Phase 2 tests remain regression gates.

## Planner accuracy expansions

- Initial AUTH slice owns live worker/Kafka helpers together; 03-15/16 extract focused loader/Kafka clients only after the real happy path passes. Initial two-store slice02 is refined immediately by03-17. This keeps the first non-test task end to end while preserving bounded file ownership.
- Current DLQReason whitelist is decode_error, validation_error and missing_received_at. Key/hash/immutable conflicts can reuse validation_error; do not pass unregistered reason strings or invent an envelope. Constructor is NewDLQPublisher(ctx, cfg.Kafka, topic, maxValueBytes).
- Existing integration TestMain builds five default binaries and exposes kafkaCfg/chCfg/pgCfg/binDir. New failure tests call buildIntegrationBinary(t, service); global integration builds are forbidden. Lifecycle/readiness fixture topic/env helpers need the real alerts topic, independent groups and absolute rules path (PGcompatibility owned early by03-18-T03, final regressions03-14-T01).
- Actual existing ops regressions include TestReadyzTimeoutAndConcurrencyBound and TestReadyzUsesOneBoundedRequestContext. Preserve sixteen-worker admission and independently bounded responses; the numeric late metric route must not replace these semantics.
- Existing make dev-up launches ingest/event-writer/query-api;03-13 adds detector/alert-writer after topics/migrations. Existing ops ports follow9101..9105, so detector9102 and alert-writer9104 are the expected additions, checked against current config.
- Shared scenario contract is testdata/detection/scenarios.json. Python expectations and Go DedupeKey checks are independent; current-run CLI acceptance also snapshots Kafka offsets and scopes stores to manifest tenant/entities/windows.
- Existing yaml/v3 pin is already resolved.03-15 promotes it indirect→direct with go mod tidy while preserving versions; no package installation or framework upgrade is authorized.

## Revision contract additions (planning, not implemented)

- USERoption1 in03-COOLDOWN-DECISION.md supersedes candidate-only throttle.05 freezes Candidate(bytes/fingerprint, trigger occurred_at, sourceoffset/ruleordinal);18 supplies real PG lifecycle/readiness/config/Compose fixtures before19 activates durable decision transactions.
- New operational migration004_detection_decisions.sql and postgres/detection_decisions.go have no current domain analog. Reuse database.Database.Pool/parameterized pgx and002migration style, with explicit bounded Commit/Rollback. Row-lock fence→immutable decision lookup→cursor; matching replay returns before cursoraccess. No PGtransaction spans Kafka Publish.
- Kafka GroupMetadata(memberID,generation), assignmenttoken/groupincarnation/configgeneration/sourcefrontier fence stale owners; generation rollback/equalgeneration wrongmember/delayed assignment failclosed.20 proves actual targethandoff/races/PGoutage.
- Format2 contains processed_next independent ofpin, pre-pinmax, sealedfloor, fingerprint/fourversions/drained, fourrule_effective_from and decision_frontier (explicitnone or offset/ordinal/ID/fingerprint), total<=1024bytes. Restore matches immutable row and PGfence>=marker; legitimate PGahead survives decision-before-publish crash, PGbehind failsclosed. NoautomaticdecisionGC; document disk growth/restore consistency.
-08 PGgeneration drain retains decisions, resets only new-generation cursors and aligns effective_from=ceil(maxverifiedhandoversealed/newwindow)*newwindow to prevent300→600historicalID collision. No IDencoding change.
-21 private Kafka operator helper snapshots sourceend+OffsetFetch; catchup uses processed_next, processedmaximum=sealed+lateness (not prefixmax), explicit empty/idle states and capped retainedscan fallback. Base>=max(now,allmax+1ms), same-ms contributors, threats before per-key auth.success windowend+30smarkers, future-skew scheduling within15m. Sender5attempt/120s budget per actualbatch is unchanged. Concurrent/far-future progress produces bounded refusal; no freshgroup/reset.
-13/14 real legacyseed→scenario→repeat preserves volumes/group/priorrows. Single scheduled proof16m, repeat32m and CIjob45m are explicit; ordinary tests6m/cooldown8m. Quiescent otherproducers is an operator precondition. No public debugHTTP or secret-bearing output.
- Every task now reads each current/predecessor target and sourceanalog. New declarations/tests are scaffolded in first testtask to support assertion-level RED. Each named family uses its own nonempty runner call; broad regex only proves onePASS and is insufficient.
