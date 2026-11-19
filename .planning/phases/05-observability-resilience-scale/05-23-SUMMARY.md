---
phase: 05-observability-resilience-scale
plan: "23"
subsystem: storage-clickhouse
tags: [gap-closure, clickhouse, dedupe, summary, overview, scale, query-api]
requires:
  - phase: 05-observability-resilience-scale
    provides: "05-22 bounded ListEvents, canonicalRowsPrefixSQL, events_scale_test.go helpers (seedScale, query_log matcher, equivalenceFixture)"
provides:
  - "Bounded QueryEventStore.SummarizeEvents: a three-column GROUP BY event_id main pass plus a canonical argMin correction run only for conflicting ids"
  - "clickhouse.ErrSummaryConflictsExceeded (503 path) and maxSummaryConflicts = 10,000"
  - "query_log tags: log_comment sentinel.summarize_events.main / .correction"
  - "TestSummarizeEventsScale, TestSummarizeEventsOracleEquivalence, TestSummarizeEventsNoConflictsSkipsCorrection, TestSummarizeEventsConflictCap"
affects: [05-20 bench re-run (Overview at 1M events), ADR 0011]
tech-stack:
  added: []
  patterns: ["lo/hi decomposition: an identity whose rows agree on occurred_at needs no tie-break", "NULL group key carries the conflicting-id array and total in the same aggregation", "log_comment tags round trips while the caller's query id is kept"]
key-files:
  created: []
  modified:
    - backend/internal/storage/clickhouse/summary.go
    - backend/internal/storage/clickhouse/query_events.go
    - backend/test/events_scale_test.go
    - docs/decisions/0002-event-id-policy.md
decisions:
  - "Round trips are distinguished by the log_comment setting rather than by -main/-correction query_id suffixes. clickhouse-go v2.49 cannot read the caller's query id (05-22), so both round trips reuse it sequentially"
  - "Conflicting ids are limited to those whose winner can reach the window (hi >= start AND lo < now). This holds for lo = hi as well, so the main-pass filter is that single predicate"
  - "Cap of 10,000 conflicting identities (groupArrayIf(10000) plus count()); more returns ErrSummaryConflictsExceeded, which the handler maps to 503"
metrics:
  duration: "~40 min"
  completed: 2026-11-19
  tasks: 2
  files: 4
---

# Phase 5 Plan 23: Bounded SummarizeEvents Summary

`SummarizeEvents` no longer runs the whole-tenant 12-column tuple argMin. A main pass runs `GROUP BY event_id` with `min/max(occurred_at)` and reads only `tenant_id`, `event_id` and `occurred_at`. Identities whose rows agree on `occurred_at` are bucketed directly. A second round trip runs the unchanged canonical argMin only for conflicting ids, and only when any exist. At 1M events per tenant the summary takes 70-106 ms and about 250 MB, against 440-945 ms and 1.8 GB before. The output equals the old query exactly.

## Commits

| Gate | Hash | Message |
|------|------|---------|
| RED | cb5bc90 | test(05-23): add failing SummarizeEvents scale regression and oracle equivalence |
| GREEN | 19e52bf | fix(05-23): bound SummarizeEvents to a three-column pass plus conflict correction |

No refactor commit was needed.

## RED evidence (cb5bc90, unmodified SummarizeEvents)

```
SCALE_SUMMARY tenant=scale-a duration=945.120465ms
SUMMARY_QUERY id=summary-... comment="" duration_ms=944 read_bytes=135036201 memory=1817284329
  columns=[action attributes_json destination_ip destination_port event_id event_type occurred_at outcome
           principal protocol received_at source source_ip tenant_id]
  Messages: main summary pass must read only tenant_id,event_id,occurred_at; read [...]
--- FAIL: TestSummarizeEventsScale
--- PASS: TestSummarizeEventsOracleEquivalence/equal
--- FAIL: TestSummarizeEventsOracleEquivalence/correction_logged
--- PASS: TestSummarizeEventsOracleEquivalence/tenant_isolation
--- FAIL: TestSummarizeEventsNoConflictsSkipsCorrection   (same column assertion)
```

The scale test failed for the right reason: the old query read all 14 columns.

## Measurements (1,000,000 rows + 1,000 exact duplicates, one tenant, merges stopped, idle test container)

| | Old (oracle / RED) | New main pass |
|---|---|---|
| Duration (wall) | 438-945 ms (4 runs) | 70 / 84 / 106 ms |
| read_bytes | 135,036,201 | 48,048,023 (0.356x) |
| memory_usage | ~1.81 GB | ~245-250 MB |
| Columns | all 14 | event_id, occurred_at, tenant_id |
| Correction round trip | n/a | not run (0 conflicts) |

Correction cost at the cap: 10,000 conflicting identities (20,000 rows) took 78-86 ms in total. The main pass was 14 ms / 0.96 MB, and the correction 55 ms / 2.64 MB / 50.6 MB of memory. On the 5,000-row equivalence fixture with about 800 conflicting identities, the main pass took 12 ms and the correction 30 ms.

## Verification

- Integration (`-tags=integration ./test/`), all PASS in 112.8 s:
  - The four TestSummarizeEvents* tests, including the equivalence subtests equal, correction_logged and tenant_isolation, and the cap subtests at_cap and over_cap.
  - The 05-22 tests: TestListEventsScale (first page 50 ms, 0.2262 ratio), TestListEventsOracleEquivalence and TestListEventsTenantIsolation.
  - TestDedupeDoubleInsertNoOptimize, TestDedupeDifferentOccurredAt, TestEventLookupTenantScope, TestEventsSchema and all 17 TestOpenAPI* tests.
- The equivalence fixture has 5,000 identities, each inserted 3 times as identical retries. It includes later-received and earlier-received conflicts, equal-received ties, and 50 conflicts whose second representation lies -12..+12 s around the 24 h start or the 1-minute rate boundary, alternating which side wins. A colliding-id foreign tenant whose rows would move every winner leaves the result unchanged.
- `go test -race ./...` PASS. `golangci-lint run ./...` and `--build-tags=integration ./...` report 0 issues. gofmt is clean.
- `queryTimeout = 5 * time.Second` is unchanged. There is no schema change, no new dependency and no API/web change. All SQL is constant fragments with named parameters; the groupArray cap comes from a Go constant.
- `bash scripts/check-resources.sh` PASSED before the container runs. Only isolated testcontainers were used.
- **Not run:** TestPhase4SummaryRules (`phase4` tag). It needs the live dev stack, which this executor must not touch. Its cancelled-context case exercises only the error path, and the summary semantics are proven by the oracle equivalence.

## Deviations from Plan

1. **[Driver limitation] log_comment tags instead of `-main`/`-correction` query_id suffixes.** 05-22 found that clickhouse-go v2.49 has no public getter for a context's query id, so the store cannot derive suffixed ids from the caller's prefix. Deriving them from a new exported prefix hook would add a test-only API. Instead, each round trip sets `log_comment` (`sentinel.summarize_events.main` / `.correction`) in its own settings. `driverclickhouse.Context` keeps the caller's query id, which both round trips reuse sequentially. The tests still match `query_id = prefix OR LIKE prefix-%` and classify rows by log_comment: correction rows are those tagged `.correction`, and every other row must read only the three allowed columns. In production, without a caller id, ClickHouse assigns ids and the tags still identify the round trips. Commit 19e52bf.
2. **[Rule 2 - Missing failure-case test] Added TestSummarizeEventsConflictCap in the GREEN commit.** The plan required the overflow 503 path but no test for it. With exactly 10,000 conflicting identities the result equals the oracle and one correction round trip is logged. With 10,001, `errors.Is(err, ErrSummaryConflictsExceeded)` holds and the summary is zero-valued. It lives in the planned test file.
3. **uniqueEventRowsSQL kept without a production caller.** `TestUniqueEventRowsSQLUnchanged` in `query_events_internal_test.go` still pins it, and that file is outside this plan's files_modified. Its comment now says it is the reference text. A later cleanup can delete the constant together with that test.
4. **Main-pass filter simplified** to `hi >= start AND lo < now`, which is equivalent to the planned `lo = hi AND lo in window` OR `lo != hi AND reachable`. It also keeps conflicting ids that cannot reach the window out of the correction.

## Known Stubs

None.

## Threat Flags

None. Both round trips carry `tenant_id={tenant:String}`, and colliding-id isolation is tested (T-05-23-01). The SQL is constant with named parameters; the ids are bound as `{ids:Array(String)}` (T-05-23-02). The correction is capped at 10,000 identities and shares the single queryTimeout and `max_execution_time` (T-05-23-03).

## Self-Check: PASSED

- FOUND: backend/internal/storage/clickhouse/summary.go, backend/internal/storage/clickhouse/query_events.go, backend/test/events_scale_test.go, docs/decisions/0002-event-id-policy.md
- FOUND commits: cb5bc90, 19e52bf
