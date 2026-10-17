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
| internal/storage/postgres/alerts.go | backend/internal/database/database.go; backend/internal/auth/postgres.go | Existing pgx pool/parameterized statements; explicit bounded transaction and advisory-lock ownership; insert-only replay path |
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
