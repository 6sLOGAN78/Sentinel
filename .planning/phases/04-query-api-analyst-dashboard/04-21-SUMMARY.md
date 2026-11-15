---
phase: 04-query-api-analyst-dashboard
plan: "21"
subsystem: detection-scenarios-harness
tags: [gap-closure, G2, clickhouse, ttl, scenarios, verifier]
gaps_closed: [G2]
requires: ["04-18"]
provides:
  - "Static scenario ClickHouse database keeps the fixed 2026-10-17 fixtures (events TTL 3650 days, private DB only)"
  - "static-history-retained regression subtest inside the existing ^TestDetectionScenarios$ selector"
affects: [phase3 scenarios CI job, integration CI job (TestDetectionScenarios)]
tech-stack:
  added: []
  patterns: ["Historical-fixture overrides live only in the private per-run store (like PAST_SKEW), never in production schema or the verifier"]
key-files:
  created: []
  modified:
    - backend/test/scenarios_pipeline_test.go
key-decisions:
  - "Root cause: the events retention TTL (002_events.sql:20, occurred_at + 7 days) expired the fixed 2026-10-17 static fixtures from 2026-10-24, and a ClickHouse TTL merge deletes the acknowledged rows soon after insert"
  - "Fix in the harness, not the verifier: newStaticScenarioCH sets the private DB's events TTL to the max operator-supported 3650 days through chmigrate.SetEventsTTL"
  - "Regression added as a subtest of TestDetectionScenarios so the frozen Phase 3 selectors still cover it; it uses OPTIMIZE TABLE events FINAL to run the TTL merge deterministically"
metrics:
  completed: 2026-11-15
  tasks: 2
  files: 1
---

# Phase 4 Plan 21: G2 static scenarios current_event_rows Summary

The static scenarios failed for a date reason, not a code regression. The fixed fixtures are dated 2026-10-17, and the
`events` retention TTL is 7 days, so ClickHouse has deleted every acknowledged row since 2026-10-24. The fix sets the
private static-scenario database to the longest supported retention. A regression subtest forces the TTL merge to
pin this behaviour.

## Root cause

`backend/migrations/clickhouse/002_events.sql:20` has `TTL toDateTime(occurred_at) + INTERVAL 7 DAY`. Every event in
`testdata/detection/scenarios.json` has occurred_at on 2026-10-17 (a1e4d7d). `runStaticScenario` already lets ingest
accept these old fixtures with `PAST_SKEW=876000h`, but its ClickHouse database kept the default retention. ClickHouse's
TTL merge therefore removed the rows shortly after the event-writer inserted them. `eventSnapshots`
(`backend/tools/check-alerts/main.go:143`) then returned "missing acknowledged event rows" on every retry until the
1m `verifyTimeout` ran out. That matches the 60.7-63.2 s subtest times in both hosted jobs. The last local pass (03-14,
2026-10-20) was inside the 7-day window.

## Hypotheses

| ID | Hypothesis | Verdict | Evidence |
|----|-----------|---------|----------|
| h1 | DateTime64 param bound in a non-UTC tz | refuted | clickhouse-go v2.49.0 sends a time.Time parameter as epoch; the debug run also failed without the since predicate |
| h2 | run_start vs received_at clock/truncation | refuted | ingest is a host process using time.Now().UTC(); the table had 0 rows, so the predicate was irrelevant |
| h3 | writer lag beyond budget | refuted | the rows were deleted, not late; after the fix verify passes in about 2 s on the unchanged budget |
| h4 | ambient env (root .env LOGGING.FORMAT etc.) | refuted | hosted (no .env) and local fail identically; the cause is wall clock vs fixture date |
| h5 | Phase 4 regression | refuted | the fixtures and TTL are unchanged since Phase 2/3; expiry started on 2026-10-24 |
| h6 | retention TTL expires the fixed fixtures | confirmed | debug run: `total_rows=0` on all 57 attempts; a CH 26.3.47.6 probe drops a 2026-10-17 row (202610 parts at level 2, rows=0) and keeps a now-1d row; the RED test fails with "event not found" |

The private diagnosis record is `.local/phase4-acceptance/04-21/diagnosis.md`. It is untracked and mode 0600. The
temporary debug print wrote only to a scratchpad file, was reverted with `git checkout`, and was never committed.

## Commits

- RED `52cb5c2` test(04-21): reproduce current_event_rows root cause
- GREEN `cb2d6fd` fix(04-21): keep fixed static scenario events past the retention TTL

## Commands and exits

All runs are from the repo root and UTC. Logs are in `.local/phase4-acceptance/04-21/`.

| Step | Command | Start - End | Exit |
|------|---------|-------------|------|
| Hosted logs | `gh run view 43649750865 --repo chocopie247/Sentinel --log-failed > .local/ci-mirror-43649750865/failed.log` (459,939 bytes, 0 secret-pattern hits) | 00:2x | 0 |
| Repro (before) | `bash scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenarios$/^isolated-static-boundary$' -tags=integration -timeout 16m -p 1` | 00:14:20 - 00:15:45 | 1 (current_event_rows, 62.14 s) |
| RED | same runner, `'^TestDetectionScenarios$/^static-history-retained$'` at 52cb5c2 | 00:19:22 - 00:19:43 | 1 ("event not found") |
| GREEN unit | same selector at the fix | 00:20:07 - 00:20:29 | 0 |
| Stack | `COMPOSE_PROJECT_NAME=sentinel-g2check make dev-up` (root .env ports, checked free) | 00:21:35 - 00:23:37 | 0 |
| Scenarios | `COMPOSE_PROJECT_NAME=sentinel-g2check bash scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenarios$' -tags=integration -timeout 16m -p 1` | 00:23:42 - 00:25:38 | 0 (all 5 subtests PASS; static 2.07-2.35 s) |
| Phase 3 scenarios group | `COMPOSE_PROJECT_NAME=sentinel-g2check bash scripts/check-phase3-tests.sh scenarios` | 00:28:41 - 00:45:38 | 0 (TestDetectionScenarios 390.91 s; TestDetectionScenariosPreservedRepeat 580.82 s) |
| Unit | `cd backend && go test -race ./tools/check-alerts/...` | 00:26 | 0 |
| Vet | `go vet -tags=integration ./test/` | 00:26 | 0 |
| Lint | `go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./tools/check-alerts/... ./test/...` | 00:26 | 0 (0 issues) |
| Guards | `python3 -B -m unittest scripts/test_phase3_guards.py` | 00:26 - 00:29 | 0 (12 tests OK) |
| Reset | `COMPOSE_PROJECT_NAME=sentinel-g2check make reset CONFIRM=1` | 00:45:48 - 00:45:52 | 0 (0 containers, 0 volumes left) |

Before the fix, the scenarios selector without a stack had already shown the three static subtests passing in about
2 s and preserved-live-edges failing at scenario_orchestration, because no compose stack was up. That is expected:
CI runs `make dev-up` first.

## Deviations from Plan

1. **[Rule 3 - Blocking] Own isolated stack.** The run used `sentinel-g2check` on the root .env ports instead of
   `sentinel-g1check`, which 04-18 had already reset, as the orchestrator instructed. The ports were checked free first.
   The integration tests themselves use testcontainers, so the stack was only needed for `preserved-live-edges` (seed_scenarios.py).
2. **RED initially passed (fail-fast followed).** The first RED version read the row back immediately and passed,
   because ClickHouse runs the TTL merge shortly after the insert, not during it. The test was changed to run
   `OPTIMIZE TABLE events FINAL` before reading, which makes it deterministic. Only the version that fails for
   the diagnosed reason was committed.
3. **Regression location.** The regression lives in the harness (`scenarios_pipeline_test.go`) rather than
   `tools/check-alerts/main_test.go`, because the cause is in the harness's store setup and the verifier is correct.
   It is a subtest of `TestDetectionScenarios`, so the frozen Phase 3 selectors still run it. `main.go` and `main_test.go` are unchanged.

No verifier stage, selector, budget, timeout or production schema was changed. `git diff` on the plan's files
touches only `scenarios_pipeline_test.go`, and `fixedVerifierStage` is unchanged.

## Known limits

- The 3650-day retention is the most `chmigrate.SetEventsTTL` accepts, so the fixed fixtures work until about 2036-10-17.
  That date is documented in the helper comment.
- In the second TestDetectionScenarios run against the same stack, preserved-live-edges took 383.67 s instead of 81.62 s. It was still well inside the 16m budget.
- Hosted confirmation of the phase3 (scenarios) and integration jobs belongs to 04-24. The integration job's separate
  32m timeout was not investigated here. The static subtests used to burn about 3 min of it and now take about 7 s.

## Self-Check: PASSED

- FOUND: backend/test/scenarios_pipeline_test.go
- FOUND: 52cb5c2, cb2d6fd
- No file deletions in 52cb5c2~1..cb2d6fd
