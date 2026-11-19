---
phase: 05-observability-resilience-scale
plan: "22"
subsystem: storage-clickhouse
tags: [gap-closure, clickhouse, dedupe, pagination, scale, query-api, events]
requires:
  - phase: 05-observability-resilience-scale
    provides: "bench run 20261119a finding GAP-1 (GET /api/v1/events 503 at 1M events/tenant), 05-19/05-21 query stack"
provides:
  - "Bounded two-stage QueryEventStore.ListEvents (candidate keys in key order, winner resolution only for candidate event_ids, exact-prefix boundary, bounded widening)"
  - "clickhouse.ErrCandidateWindowExceeded (503 path) and unexported maxCandidates (default 20,000)"
  - "TestListEventsScale (1M rows, latency + query_log read_bytes), TestListEventsOracleEquivalence, TestListEventsTenantIsolation"
affects: [05-23 SummarizeEvents bounding, 05-20 bench re-run (Task 2 resumed), ADR 0011]
tech-stack:
  added: []
  patterns: [scalar WITH evaluated once per query so IN set, count and boundary share one read, kind-0 metadata row via UNION ALL so Go decides widening even with zero winners, oracle-equivalence page walk as contract proof]
key-files:
  created:
    - backend/test/events_scale_test.go
    - backend/internal/storage/clickhouse/query_events_internal_test.go
  modified:
    - backend/internal/storage/clickhouse/query_events.go
    - docs/decisions/0002-event-id-policy.md
decisions:
  - "Stage 1 caps raw key rows instead of DISTINCT keys: DISTINCT read 16.1 MB against 0.78 MB for raw rows on 1M rows, and exactness is unchanged because rows equal to the boundary key share its event_id"
  - "Stage 2 uses `event_id IN candidates.1` (an array constant from a scalar WITH): 13.9 MB read, against 33.5 MB for an IN subquery; ClickHouse uses PK generic exclusion on (tenant_id,event_id) plus the bloom index"
  - "Widening starts at Limit+1 and multiplies by 4 up to 20,000 keys; exhaustion returns ErrCandidateWindowExceeded (503), never a short page"
  - "events.go unchanged: read-in-order already applies by default (no setting needed), and clickhouse-go v2.49 Context() keeps the caller's query id across WithSettings, so attempts reuse it sequentially"
metrics:
  duration: "~75 min"
  completed: 2026-11-19
  tasks: 2
  files: 4
---

# Phase 5 Plan 22: Bounded ListEvents with exact-prefix dedupe Summary

`GET /api/v1/events` no longer dedupes the whole tenant. Stage 1 reads the newest raw keys that match range, cursor and filters in table order. Stage 2 resolves the unchanged argMin winner only for those event_ids. The result is proven equal to the old whole-tenant query page by page. At 1M events per tenant a page takes about 20-60 ms (the old query took 0.5-0.8 s idle and 503'd under bench load) and reads 9-23% of the old query's bytes.

## Commits

| Gate | Hash | Message |
|------|------|---------|
| RED | 5ea5d2d | test(05-22): add failing ListEvents scale regression and oracle equivalence |
| GREEN | 229693d | fix(05-22): bound ListEvents to candidate identities with exact-prefix dedupe |

No refactor commit was needed.

## RED evidence (5ea5d2d, unmodified ListEvents)

```
SCALE_LIST page=first duration=769.326207ms err=<nil>
SCALE_READ page=first list_read_bytes=135036201 oracle_read_bytes=135036201 ratio=1.0000
  Error: "540144804" is not less than "135036201"
SCALE_LIST page=middle_cursor duration=489.123887ms err=<nil>
SCALE_READ page=middle_cursor list_read_bytes=135036201 oracle_read_bytes=135036201 ratio=1.0000
--- FAIL: TestListEventsScale
--- PASS: TestListEventsOracleEquivalence (55.62s)   (8 subcases)
--- PASS: TestListEventsTenantIsolation (2.61s)
```

The test failed for the right reason: the old query is the oracle, so it read 100% of the oracle's bytes. On an idle single test container it finished in 0.5-0.8 s. Under bench concurrency it exceeded the 5 s queryTimeout (run 20261119a).

## GREEN measurements (1,000,000 rows + 1,000 exact duplicates for tenant A, 100,000 colliding rows for tenant B, merges stopped)

| Page | Old duration | New duration (3 runs) | Old read_bytes (oracle) | New read_bytes | Ratio |
|------|--------------|-----------------------|-------------------------|----------------|-------|
| first (limit 50) | 769 ms | 61.5 / 43.7 / 47.4 ms | 135,036,201 | 30,541,878 (2 attempts: 7,603,227 + 22,938,651) | 0.2262 |
| mid-data cursor | 489 ms | 19.4 / 23.4 / 21.1 ms | 135,036,201 | 12,307,399 (1 attempt) | 0.0911 |

read_bytes were identical across runs. The first page widens once (cap 51 -> 204) because the seeded duplicates are the newest rows, so 51 raw keys hold only 26 identities. The scale page also equals the oracle page (asserted).

## EXPLAIN evidence (ClickHouse 26.3.47.6)

Stage 1 with a mid-data cursor (`EXPLAIN indexes=1`; the redundant `occurred_at <= cursor` conjunct is present):

```
PrimaryKey  Keys: tenant_id, occurred_at
  Condition: and((occurred_at in (-Inf, '1795102220.828']), ... (tenant_id in ['scale-a','scale-a']))
  Parts: 1/2  Granules: 62/136  Search Algorithm: binary search
```

Stage 1 `EXPLAIN PIPELINE` (read in order, early stop):

```
Limit <- MergingSortedTransform 2 -> 1 <- ReverseTransform <- MergeTreeSelect(pool: ReadPoolInOrder, algorithm: InReverseOrder)
```

Stage 2 (the full attempt query; the scalar is evaluated at plan time):

```
PrimaryKey  Keys: tenant_id, event_id
  Condition: and((event_id in 51-element set), (tenant_id in ['scale-a','scale-a']))
  Parts: 2/3  Granules: 124/137  Search Algorithm: generic exclusion search
Skip  Name: idx_event_id  bloom_filter GRANULARITY 4  Granules: 36/124
```

## Cap constants

- Initial cap = Limit+1 (the page size).
- `candidateWidenFactor = 4`.
- `defaultMaxCandidates = 20,000` keys, about 100 full 200-row pages of slack. It is an unexported field on `QueryEventStore` so the unit test can inject a small value.
- Every attempt shares the single `queryTimeout` (5 s, unchanged) context and `max_execution_time`.

## Verification

- Integration (`-tags=integration ./test/`), all PASS: TestListEventsScale, TestListEventsOracleEquivalence (full_walk, source_ip_alternate, event_type, conflict_type, outcome, exact_boundaries, boundaries_filtered, single_row_pages), TestListEventsTenantIsolation, TestDedupeDoubleInsertNoOptimize, TestDedupeDifferentOccurredAt, TestEventLookupTenantScope, TestEventsSchema, and all 19 TestOpenAPI* tests (107.3 s).
- Unit: `go test -race ./internal/storage/clickhouse/... ./internal/query/...` PASS. The new tests are TestListEventsWideningExhaustedReturnsUnavailable (caps 2 -> 8 -> 10, ErrorIs ErrCandidateWindowExceeded, nil items), TestListEventsWideningStopsOnFullPage, TestListEventsQueryErrorIsWrapped, TestListEventsRejectsInvalidLimit and TestUniqueEventRowsSQLUnchanged.
- `golangci-lint run ./...`: 0 issues. `golangci-lint run --build-tags=integration ./...`: 0 issues. gofmt is clean.
- `queryTimeout   = 5 * time.Second` still matches. There is no ClickHouse schema change and no api/ or web/ change. uniqueEventRowsSQL is byte-identical to the pre-05-22 text, checked by script and by a unit test.
- `bash scripts/check-resources.sh` PASSED before every container run.
- **Not run:** TestPhase4Events / `scripts/check-phase4-tests.sh`. They need the live dev stack, which this executor must not touch. Task 1's oracle equivalence (conflicting later-received, moved, equal-received tie and identical-retry cases, page by page) is the contract proof.
- **Pending:** the 05-20 bench re-run (Task 2 resumed) must show /api/v1/events non-2xx = 0.

## Deviations from Plan

1. **[Rule 1 - Correctness/Perf] Stage 1 caps raw key rows, not DISTINCT keys.** The plan says "select distinct (occurred_at, event_id)". On 1M rows DISTINCT read 16.1 MB because preliminary DISTINCT defeats the early stop. Raw rows read 0.78 MB. Exactness is unchanged. Every raw row with key above the boundary is read, and rows equal to the boundary share its event_id, so every qualifying winner's identity is resolved. Duplicates only use slots, and widening handles that. Commit 229693d.
2. **events.go not modified.** No read-in-order setting was needed: the pipeline shows `ReadPoolInOrder / InReverseOrder` by default. Per-attempt query-id suffixes cannot be derived either, because clickhouse-go v2.49 has no public getter for a context's query id. `driverclickhouse.Context()` copies existing options, so the caller's query id already survives `boundedQueryContext`'s `WithSettings`. Sequential attempts reuse it, which ClickHouse allows; only concurrent duplicates are rejected. The test's matcher (`query_id = prefix OR LIKE prefix-%`) counts every attempt: the first page logged 2 QueryFinish rows.
3. **export_test.go not created.** testpackage skips `*_internal_test.go` by default, so the widening unit test lives in package clickhouse directly.
4. **uniqueEventRowsSQL composed from a shared prefix constant.** The text is byte-identical, so SummarizeEvents behavior is unchanged. The argMin winner tuple now has a single source for both summary and list.

## Concurrency note

Within one attempt, the candidate ids, count and boundary come from one scalar evaluation, so they are mutually consistent. Stage 2 may observe rows inserted after stage 1. A new identity inserted during the query is then treated like any insert above an already-served cursor: it can be absent from this page, but it never duplicates across pages.

## Known Stubs

None.

## Threat Flags

None. All values are bound with `driverclickhouse.Named`, and the SQL is built only from constant fragments (T-05-22-01). Both stages carry `tenant_id={tenant:String}`, and colliding-ID isolation is tested (T-05-22-02). Widening is capped and shares one timeout (T-05-22-03). Short pages are returned only when stage 1 is proven complete (T-05-22-04).

## Self-Check: PASSED

- FOUND: backend/test/events_scale_test.go, backend/internal/storage/clickhouse/query_events.go, backend/internal/storage/clickhouse/query_events_internal_test.go, docs/decisions/0002-event-id-policy.md
- FOUND commits: 5ea5d2d, 229693d
