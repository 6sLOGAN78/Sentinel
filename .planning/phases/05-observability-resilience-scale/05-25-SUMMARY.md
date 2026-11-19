---
phase: 05-observability-resilience-scale
plan: "25"
subsystem: query-api
tags: [consumer-lag, coverage, prometheus, openapi, web, gap-closure, GAP-2, OBS-02, UI-05]
requires:
  - phase: 05-observability-resilience-scale
    provides: "05-24 sentinel_kafka_topic_partitions{group,topic} coverage denominator; 05-15 lag endpoint; 05-21 System endpoint"
provides:
  - "qLagReported / qLagPartitions fixed PromQL consts and lagMetric coverage helper shared by GET /api/v1/metrics/lag and GET /api/v1/system"
  - "ReasonPartialCoverage = \"partial_coverage\" (OpenAPI SystemMetric reason enum, additive)"
affects: [05-20 bench re-run, Overview lag panel, System page]
tech-stack:
  added: []
  patterns: [coverage-gated max (value only when reported distinct partitions >= broker-listed partitions)]
key-files:
  created: []
  modified:
    - backend/internal/query/lag.go
    - backend/internal/query/system.go
    - backend/internal/query/lag_test.go
    - backend/internal/query/system_test.go
    - backend/test/openapi_drift_test.go
    - backend/test/openapi_runtime_auth_test.go
    - api/openapi.yaml
    - docs/operations.md
    - web/src/pages/Overview.test.tsx
    - web/src/pages/System.test.tsx
decisions:
  - "Contract addition (additive): SystemMetric reason enum gains partial_coverage. No field, status code, role rule or operator-allowlist rule changed"
  - "Per group: any failed lag/coverage query -> that failure's reason; denominator absent or < 1 -> no_data; reported absent or reported < expected -> partial_coverage; else the unchanged max"
  - "Lag still with zero reporters but a known denominator is partial_coverage (zero coverage), not no_data"
metrics:
  duration: "~40 min"
  completed: 2026-11-19
  tasks: 3
  files: 10
---

# Phase 5 Plan 25: Coverage-aware consumer lag Summary

The lag endpoint and System now return a group's lag as `unavailable`/`partial_coverage` whenever any partition of its topic has no live position reporter. Before this, an operator saw `max by (group)` over only the reported partitions, which reads 0 exactly when a detector has died. With full coverage the value is unchanged.

## Tasks

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | RED: coverage cases for lag and system handlers | 1910671 | query/lag_test.go, query/system_test.go, test/openapi_drift_test.go, test/openapi_runtime_auth_test.go |
| 2 | GREEN: coverage-aware lag, OpenAPI enum, docs | 6f5a7e7 | query/lag.go, query/system.go, query/lag_test.go (one count), api/openapi.yaml, docs/operations.md |
| 3 | Web rendering tests for partial_coverage | b9c6b51 | web/src/pages/Overview.test.tsx, web/src/pages/System.test.tsx |

## What changed

- **lag.go**: two new fixed consts.
  - `qLagReported = count by (group) (count by (group, topic, partition) (sentinel_kafka_consumer_lag))` counts distinct partitions.
  - `qLagPartitions = sum by (group) (max by (group, topic) (sentinel_kafka_topic_partitions))`.
  - Both aggregate to `by (group)` before leaving Prometheus.
  - `ReasonPartialCoverage`, plus `lagQueries()` and `lagMetric(results, group)`.
  - `GetLag` runs the three queries one after another under the existing 2 s context. The order (auth, role, query string, operator short-circuit) is unchanged, and the operator_only short-circuit still runs before any query.
- **system.go**: both consts are added to the allowlist (now 13 queries, still concurrent under one timeout). `assemble` uses `lagMetric`.
- **openapi.yaml**: `partial_coverage` is added to the SystemMetric reason enum. The required sentence is in the lag operation description, and a matching sentence is in the SystemMetric and System descriptions (4 occurrences in total).
- **docs/operations.md**:
  - The reason list gains `partial_coverage`.
  - The coverage PromQL now uses the distinct-partition form.
  - The 05-24 sentence "lag endpoint and System do not check coverage yet" is replaced by the actual per-group rule.
  - It now says Overview/System render "Unavailable", never 0.
- **web**: no page code change was needed. Both pages already render any non-`no_data`, non-`operator_only` reason as "Unavailable", and `client.ts` accepts any reason string.

## RED evidence (1910671)

- `go test -race ./internal/query/...` failed with assertion failures in these tests:
  - TestLagPartialCoverageIsUnavailableNeverZero (detector `available 0` instead of `partial_coverage`)
  - TestLagNoReporterIsPartialCoverage
  - TestLagUnknownCoverageIsNoData
  - TestLagAnyFailedQueryIsUnavailableForEveryRow
  - TestLagSendsOnlyTheFixedQueries
  - TestLagAnalystAndAdminAllowed (1 call instead of 3)
  - TestSystemLagCoverage
  - TestSystemSendsOnlyTheFixedQueries
  - TestSystemOperatorAdminAllowed (11 queries instead of 13)
- `./test` failed to compile: `undefined: query.ReasonPartialCoverage`.
- TestLagFullCoverageKeepsTheMaximum passed in RED by design: it pins the unchanged full-coverage behaviour.
- The test files spell out the PromQL instead of referencing unexported consts. This follows the existing `wantLagQuery` convention ("so a changed constant fails"), and the tests live in the external `query_test` package.

## Verification

- gofmt clean. golangci-lint `./...` 0 issues; `--build-tags=integration ./...` 0 issues.
- `go test -race ./...` (all default packages): pass.
- `go test -run TestOpenAPI ./test/`: pass. This includes a real `partial_coverage` handler body validated against the schema, and the schema rejecting `partial_coverage` with a value or with `state: available`.
- Integration on isolated testcontainers (resource gate PASS: 15.02 GiB disk, 7.00 GiB RAM): `-tags=integration -run 'TestOpenAPI|TestChaosSlowConsumerLag|TestChaosLagAfterRebalance' ./test/` passed in 40.9 s. No integration test drives the lag/System HTTP endpoints against a real Prometheus; they are covered by the handler unit tests.
- OpenAPI generation: `npm run gen:api` regenerated `web/src/api/schema.d.ts` (4 `partial_coverage` occurrences, prettier clean).
- web: typecheck clean, lint clean, format:check clean, `npm test` 10 files / 217 tests pass (Overview + System 46).
- Docs drift: `test_observability_config.py` 32 OK. generator `test_bench_collect.py` + `test_bench_scenarios.py` 62 passed.
- `test_phase4_guards.py`: 1 pre-existing failure, unrelated (see Deferred).

## Deviations from Plan

1. **[Rule 1 - Correctness] A denominator below 1 is no_data.** The plan maps only an absent denominator to no_data. With a present 0 (or NaN), `reported >= expected` would be trivially true and expose an uncovered max. `lagMetric` therefore treats `!(expected >= 1)` as unknown coverage, and a test pins it. In practice 05-24 never publishes 0.
2. **[Rule 3 - Blocking] `backend/test/openapi_runtime_auth_test.go` modified (not in files_modified).** The contract fakes live there. The lag fake now answers by PromQL text and gains a real `partial_coverage` case. The System table gains the coverage series, so the drift test validates real bodies.
3. **Test-only update in GREEN.** `TestLagOperatorTenantAnalystGetsValues` asserted exactly one Prometheus call. It now asserts three, the new contract. This was missed in RED because it passed there.
4. **schema.d.ts not committed.** `web/src/api/schema.d.ts` is gitignored (`.gitignore:74`) and is regenerated by `pretypecheck`/`pretest`/`prebuild`, so there was nothing to stage. It was regenerated and verified locally.
5. **Web tests did not go RED.** The pages already fall through to "Unavailable", as the plan anticipated, so no page file changed. The tests pin the behaviour.
6. **Lag queries run sequentially** in GetLag (the plan allows sequential). No errgroup exists in the package and none was added.

## Observations (not changed)

- The Overview lag panel shows its existing paragraph "Consumer lag unavailable. The metrics source is not responding." for any non-operator unavailable row, now including `partial_coverage` (and, as before, `no_data`). The plan forbids new copy, so the paragraph is unchanged. Partial-coverage-specific copy would need a UI-SPEC decision.

## Deferred Issues

- `test_phase4_guards.py::test_docs_describe_the_gate_without_claiming_phase_completion` fails on "Phase 4 is complete" in docs/operations.md. That line came from dec4a7e and the failure exists at 95d41e2, before 05-25. It is logged in deferred-items.md.

## Threat model

- T-05-25-01: `Query` is called only with package consts (`lagQueries()`, `systemQueries()`). Any query string is still a 400 before Prometheus (existing tests).
- T-05-25-02: both new queries aggregate to `by (group)`. The response key-set test still forbids partition/topic/tenant.
- T-05-25-03: the check order is unchanged. Non-operator tests still assert zero Prometheus calls.
- T-05-25-04: partial_coverage replaces the misleading 0.

## Known Stubs

None.

## TDD Gate Compliance

RED `test(05-25)` 1910671, then GREEN `feat(05-25)` 6f5a7e7. Task 3 was `test(05-25)` b9c6b51 (coverage tests for already-correct rendering). No refactor commit was needed.

## Self-Check: PASSED

- FOUND: backend/internal/query/lag.go, backend/internal/query/system.go, api/openapi.yaml, docs/operations.md, web/src/pages/Overview.test.tsx, web/src/pages/System.test.tsx
- FOUND: 1910671, 6f5a7e7, b9c6b51
