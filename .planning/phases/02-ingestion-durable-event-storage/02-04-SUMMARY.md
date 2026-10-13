---
phase: 02-ingestion-durable-event-storage
plan: "04"
subsystem: clickhouse-storage
tags: [clickhouse, native-protocol, dedupe, echo, api-key, tenant-isolation]

requires:
  - phase: 02-ingestion-durable-event-storage
    provides: scoped API keys, strict event contracts, hardened HTTP envelopes
provides:
  - native ClickHouse event batch insertion with explicit columns and bounded connections
  - immediate tenant-scoped deduplicated lookup and uniqExact counting
  - read-scoped query-api event endpoint with cross-tenant 404 behavior
  - monthly events schema with bloom index and seven-day default TTL
affects: [02-05-retention, 02-06-event-writer, 02-13-seed-ci, phase-04-query-api]

tech-stack:
  added: [github.com/ClickHouse/clickhouse-go/v2@v2.49.0]
  patterns: [native explicit-column batches, named tenant parameters, read-time identity dedupe]

key-files:
  created:
    - backend/migrations/clickhouse/002_events.sql
    - backend/internal/storage/clickhouse/client.go
    - backend/internal/storage/clickhouse/events.go
    - backend/internal/eventquery/handler.go
    - backend/test/dedupe_test.go
  modified:
    - backend/cmd/query-api/main.go
    - backend/internal/config/config.go
    - docs/decisions/0002-event-id-policy.md

key-decisions:
  - "Store physical duplicates but collapse every user-visible lookup and count immediately by event_id."
  - "Bind both authenticated tenant and event ID as native ClickHouse named parameters."
  - "Conflicting payloads sharing an ID have one visible identity but no immutable content winner."

patterns-established:
  - "Storage retry pattern: create a fresh PrepareBatch, append explicit validated columns, Send, and Abort unfinished batches."
  - "Lookup isolation: authenticate events:read, derive tenant from the key, validate canonical UUID, and return 404 for absent or other-tenant IDs."

requirements-completed: [STOR-02, STOR-03, ING-04]

duration: 9 min
completed: 2026-10-14
---

# Phase 2 Plan 04: Native Event Storage and Duplicate-Safe Lookup Summary

**Native ClickHouse persistence now keeps retry duplicates physically observable while exposing exactly one tenant-scoped event identity through immediate parameterized reads and counts.**

## Performance

- **Duration:** 9 min
- **Started:** 2026-10-13T19:25:09Z
- **Completed:** 2026-10-13T19:34:19Z
- **Tasks:** 3
- **Files modified:** 13

## Accomplishments

- Added the exact `ReplacingMergeTree` events schema with monthly `occurred_at` partitions, `(tenant_id, occurred_at, event_id)` ordering, UTC millisecond timestamps, bloom index, nullable contract columns, and seven-day default TTL.
- Added a bounded native ClickHouse pool plus explicit-column batch insertion, parameterized tenant/ID lookup, and tenant-scoped `uniqExact(event_id)` counting with an optional received-time lower bound.
- Added the query-api `GET /api/v1/events/:event_id` route with API-key authentication, `events:read` scope, canonical UUID validation, tenant-hidden 404s, and sanitized ClickHouse failures.
- Proved immediate dedupe against real ClickHouse with background merges stopped: raw storage remains `2N`, while lookup and counts expose `N` identities without `OPTIMIZE` or insert-deduplication settings.
- Documented that conflicting content has no immutable winner even though user-visible identity is deduplicated.

## Task Commits

1. **Task 1: Write immediate duplicate-safe lookup acceptance first** - `0a0f251` (RED tests)
2. **Task 2: Deliver native persistence and tenant-scoped lookup** - `f3f14eb` (GREEN implementation)
3. **Task 3: Document exact identity and conflicting-content guarantees** - `4ccad1a` (documentation)

## Files Created/Modified

- `backend/migrations/clickhouse/002_events.sql` - Idempotent events table with required engine, partition, order, index, nullable columns, and TTL.
- `backend/internal/storage/clickhouse/client.go` - Bounded native pool creation, authentication, compression, and startup ping.
- `backend/internal/storage/clickhouse/events.go` - Native insertion, tenant-bound lookup, unique counting, null handling, and attribute JSON reconstruction.
- `backend/internal/storage/clickhouse/events_test.go` - Static migration contract and forbidden-setting guard.
- `backend/internal/eventquery/handler.go` - Read-key authentication and tenant-scoped single-event HTTP route.
- `backend/internal/eventquery/handler_test.go` - Auth, scope, canonical ID, tenant binding, 404, and sanitized failure tests.
- `backend/test/dedupe_test.go` - Real ClickHouse and PostgreSQL acceptance for schema, physical duplicates, cross-partition conflicts, round-trip values, and tenant scope.
- `backend/cmd/query-api/main.go` - Native client, key verifier, route registration, public port listener, and ordered shutdown.
- `backend/internal/config/config.go` - Native ClickHouse address requirement for services that consume the ClickHouse section.
- `backend/internal/config/config_test.go` - Relevant-service native-address validation and unrelated-service compatibility.
- `backend/go.mod`, `backend/go.sum` - Pinned clickhouse-go v2.49.0 and resolved transitive checksums.
- `docs/decisions/0002-event-id-policy.md` - Honest visible-identity and conflicting-content guarantees.

## Decisions Made

- Physical duplicates remain allowed and test-observable; correctness comes from tenant-scoped `uniqExact` and bounded lookup SQL rather than background merges.
- `Get` orders currently retained rows by `received_at ASC`, while equal-millisecond ties and later ReplacingMergeTree merges leave the conflicting payload winner unspecified.
- Query values use clickhouse-go named parameters with fixed SQL; neither tenant nor event ID enters query text.
- Native ClickHouse addresses are required only for services whose required configuration includes ClickHouse; ingest and detector configuration remain valid without them.
- Query API continues to initialize its existing PostgreSQL and Redis dependencies and registers HTTP shutdown after dependencies so lifecycle LIFO closes HTTP first.

## Verification Evidence

- `cd backend && go test -race ./internal/storage/clickhouse/... ./internal/eventquery/... ./internal/config/... -timeout 60s` — passed.
- `cd backend && go test -tags=integration ./test/... -run 'TestDedupe|TestEventLookupTenantScope|TestEventsSchema' -timeout 3m -p 1` — passed against real ClickHouse and PostgreSQL in 22.795s.
- `cd backend && go test ./migrations/... ./internal/storage/clickhouse/... -timeout 60s` — passed.
- `cd backend && go test -tags=integration ./test/... -run TestDedupe -timeout 3m -p 1` — passed in 18.152s.
- Container-backed dedupe, schema, configured/idempotent migration, and binary lifecycle suite — passed in 28.967s.
- Targeted regular and integration-tag `go vet` — passed.
- Targeted regular and integration-tag golangci-lint — 0 issues.
- OpenAPI tests plus web generation, typecheck, 6 Vitest tests, and ESLint — passed.
- `make lint test` — passed across full backend race tests, web checks, and generator Ruff/format/mypy/37 pytest cases.

## Security Evidence

- The handler always obtains tenant identity from a verified key and passes it as a bound ClickHouse parameter.
- Other-tenant and absent IDs share the same 404 envelope; invalid/missing credentials and missing read scope return 401/403 before querying.
- ClickHouse failures return a sanitized 503 with no query, credential, or backend error details.
- The native pool, reads, and batches are bounded by connection counts, dial/read timeouts, query execution time, and one-row lookup limits.

## Interface Signatures for Later Plans

- `clickhouse.Open(ctx, config.ClickHouseConfig) (clickhouse.Conn, error)` opens and pings the native pool.
- `clickhouse.NewEventStore(conn)` returns an `EventStore` with `InsertBatch`, `Get`, and `CountUnique`.
- `EventStore.InsertBatch(ctx, []*contracts.Event) error` requires server-owned `received_at` and writes one explicit native block.
- `EventStore.Get(ctx, tenantID, eventID) (*contracts.Event, error)` returns `clickhouse.ErrNotFound` for tenant-hidden absence.
- `EventStore.CountUnique(ctx, tenantID, since) (uint64, error)` uses `uniqExact(event_id)` and applies `received_at >= since` when nonzero.
- Plan 02-06 can reuse `InsertBatch`; plan 02-05 can modify TTL without changing the immutable migration default.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

- Strict lint initially flagged optional-port conversion and test formatting. The implementation was adjusted before the GREEN commit to validate UInt16 range explicitly and satisfy all configured checks.

## User Setup Required

None - no external service configuration required.

## Known Stubs

None.

## Next Phase Readiness

- Retention configuration can now alter the default seven-day TTL through the existing migration path.
- The event writer can persist Kafka batches through the verified native `InsertBatch` interface and rely on immediate read dedupe after ambiguous retries.
- No blockers remain for plan 02-05.

## Self-Check: PASSED

- All thirteen modified production, migration, contract, and test files exist.
- Commits `0a0f251`, `f3f14eb`, and `4ccad1a` exist in history.
- No known stubs, forbidden dedup-window settings, `OPTIMIZE` calls, or unplanned threat surfaces remain.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-14*
