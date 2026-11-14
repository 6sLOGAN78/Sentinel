---
phase: 04-query-api-analyst-dashboard
plan: "26"
subsystem: backend/test (integration harness)
tags: [gap-closure, G8, G9, integration, testcontainers]
gap_closure: true
gaps_closed: [G8, G9]
requires: ["04-16"]
provides:
  - "query-api lifecycle env includes SENTINEL_DETECTOR.RULES_PATH"
  - "TestAlertReservationsSchema derives expected tern version from embedded migrations"
affects: [integration CI job, phase3 (core) CI job, 04-25 hosted proof]
tech-stack:
  added: []
  patterns: ["schema version assertions derived from migrations.Postgres() rather than literals"]
key-files:
  created: []
  modified:
    - backend/test/lifecycle_test.go
    - backend/test/alert_reservations_schema_test.go
decisions:
  - "Expected schema version is read from the embedded migrations FS (migrations.Postgres(), the same files tern applies from backend/migrations/postgres) instead of reading the directory by path, so it is cwd-independent."
  - "Helper also requires latest >= 6 so an empty/broken embed cannot pass vacuously."
requirements: [API-06, API-08]
metrics:
  started: 2026-11-14T23:50:53Z
  completed: 2026-11-14T23:55:00Z
  duration: ~5 min
  tasks: 2
  files: 2
---

# Phase 4 Plan 26: Integration harness gaps G8/G9 Summary

query-api in the integration lifecycle harness now gets the rules path it has required since 04-10. TestAlertReservationsSchema now expects the highest-numbered embedded Postgres migration (8 today) instead of the hard-coded 6. These are test-harness changes only. No production code, migrations, budgets or selectors were touched.

## Tasks

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Reproduce both failures (RED evidence) | none (evidence only) | none |
| 2 | Fix the harness and the stale assertion | cc72514 | backend/test/lifecycle_test.go, backend/test/alert_reservations_schema_test.go |

## Evidence

All test runs used testcontainers on random ports. `make dev-up` and the root `.env` ports were not used. `df -h /` showed 13G free before the runs.

**Task 1 RED** (existing tests, run from the repo root):
`bash scripts/go-test-nonempty.sh './test/...' '^(TestHealthz|TestAlertReservationsSchema)$' -tags=integration -timeout 15m -p 1`
- Ran 2026-11-14T23:50:58Z to 23:51:58Z. **EXIT=1**.
- `TestAlertReservationsSchema/fresh` and `/upgrade_from_5` failed with `expected: 6 / actual  : 8`.
- `TestHealthz/query-api` failed with `http://127.0.0.1:38399/healthz did not return 200`. The other four services passed.
- Both match the hosted logs: `.local/ci-mirror-43649750865/job-132617003504.log` lines 98-113 and 195-252, and `job-132617003548.log` lines 1802-1810.

**Task 2 GREEN:**
`bash scripts/go-test-nonempty.sh './test/...' '^(TestHealthz|TestReadyzDraining|TestReadyzDependencies|TestAlertReservationsSchema)$' -tags=integration -timeout 20m -p 1`
- First run: 23:52:12Z to 23:53:14Z, **EXIT=0**. All subtests passed, including TestHealthz/query-api, TestReadyzDraining/query-api, TestReadyzDependencies and both TestAlertReservationsSchema subtests.
- Re-run after the lint refactor (final code): 23:53:40Z to 23:54:43Z, **EXIT=0**. `ok github.com/6sLOGAN78/sentinel/test 62.164s`.
- `cd backend && go vet -tags=integration ./...`: **EXIT=0**. Run before and after the refactor.
- `cd backend && go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...`
  - First attempt: **EXIT=1**, one `lll` issue on a package-level regex with a nolint comment.
  - Fixed by moving the regex inside the helper, with no nolint needed.
  - Re-run: **EXIT=0**, `0 issues.`
- `grep -rn "require.Equal(t, 6, schemaVersion" backend/test/`: no matches (grep exit 1).

## Deviations from Plan

- The plan suggested reading `NNN_*.sql` file names from the `backend/migrations/postgres` directory. The helper instead reads `migrations.Postgres()`, the embedded FS of that same directory and the one tern migrates from. This keeps the test independent of the working directory, and the assertion stays the same: highest file number equals the applied version. It also adds a guard that the highest number must be at least 6.
- There were no other deviations. The relation, snapshot and upgrade-from-5 assertions are unchanged.

## Limits

- Proven locally only. Hosted proof of the integration and phase3 (core) jobs on the mirror belongs to 04-25.
- The hosted integration log also shows `TestDetectionScenarios/isolated-static-*` failures. Those are outside G8/G9 and were not touched or run here.
- When 04-23 adds migration 009, the schema test expects 9 automatically. That has not been exercised yet because 009 does not exist in this tree.

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: backend/test/lifecycle_test.go (contains `SENTINEL_DETECTOR.RULES_PATH` in the `case "query-api"` branch)
- FOUND: backend/test/alert_reservations_schema_test.go (`latestPostgresMigration`)
- FOUND: commit cc72514
