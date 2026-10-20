---
phase: 03-detection-alert-pipeline
reviewed: 2026-10-20T16:41:13Z
depth: standard
review_kind: scoped_static_scenario_pg_fixture_repair
diff_base: ec44aa80cc31c620d659e1de745d389c72968d4c
review_head: df0bfbb28a84cea3c3036a26f49841f35bdd27e0
files_reviewed: 1
files_reviewed_list:
  - backend/test/scenarios_pipeline_test.go
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 3: Static Scenario Fixture Repair Review R2

**Reviewed:** 2026-10-20T16:41:13Z
**Depth:** standard
**Files Reviewed:** 1
**Status:** clean

## Narrative Findings (AI reviewer)

### Summary

The private PostgreSQL fixture addresses the established shared-database contamination of static scenario verification. No concrete BLOCKER or WARNING was established in this single-file repair at frozen head `df0bfbb28a84cea3c3036a26f49841f35bdd27e0`. This review does not replace the earlier 73-artifact review or its two-test-file WR-01 repair review. Historical reports remain unchanged.

The complete `ec44aa8..df0bfbb` diff and the full frozen `backend/test/scenarios_pipeline_test.go` were read through EOF. Bounded unchanged-contract reads checked database connection/migration behavior, service environment and process cleanup, the real alert-replay fixture, ClickHouse fixture setup and the strict verifier's PostgreSQL connection/query path. No optional structural pre-pass was supplied.

### Isolation and preservation assessment

`newStaticScenarioPG` at lines 107-126 copies the existing test-container connection configuration and changes only the database name to `static_scenario_` plus a UUID without dashes. `pgx.Identifier.Sanitize` quotes that exact identifier for both CREATE and DROP; no scenario input selects a database or SQL fragment. The admin connection stays on the existing fixture database. The ordinary migration runner applies all embedded PG migrations to the new database, including detector configuration/decision and alert-reservation tables.

At line 155, the private name enters the common settings used for detector, event-writer, alert-writer and ingest environments. `scenarioEnvironment` removes the prior exact variable before appending the override. Detector, alert-writer and ingest therefore share the new PG database; event-writer receives the same setting without introducing a new PG dependency. Other connection fields are inherited unchanged from `pgCfg`. The API key is created through a pool opened with the same copied private configuration at line 174. Capture and verify both receive `pg`, and `staticVerifier` forwards its host, port, user, password, database and SSL mode through environment variables. The unchanged verifier uses those PG variables through `pgxpool.ParseConfig("")`.

This isolates fixed scenario source identities from unrelated workflow rows without narrowing the strict verifier. The verifier still queries all same-tenant/source workflow rows plus expected IDs and rejects unexpected identities or payload/state mismatches. It is not changed to filter away the original counterexample. Canonical fixtures, production runtime, verifier implementation, source/alert topic layouts, expected payload assertions and existing test deadlines are unchanged.

`seedStaticWorkflowCollision` at lines 85-97 creates an unrelated alert with source `192.0.2.10` and a distinct UUID-bearing principal/identity through actual Kafka publication and the real alert writer against shared PG/native CH. Its first full workflow read requires an existing row after that run; nil completion without persistence cannot satisfy it. The returned deferred check reads the complete `row_to_json` row again and requires JSON equality. It runs while the subtest context and shared fixture pool are still live, before their cleanup. A missing/deleted or modified unrelated workflow fails that check. No shared row is deleted to obtain the static pass.

### Cleanup assessment and explicit non-finding

The private database cleanup is registered before its four service cleanups. Its ten-second context is independent of test cancellation, targets only the generated quoted UUID database, and closes the admin connection even when DROP fails. The helper pool closes on return from `runStaticScenario`; verifier children synchronously finish before that return. Existing shared workflow data and the preserved Compose database are not DROP targets.

The new comment at lines 116-117 overstates the existing process helper: `startProc` cleanup sends `Process.Kill()` if its done channel is not ready, but does not wait for `cmd.Wait` before returning. LIFO therefore orders kill requests rather than proving completed process joins. This was investigated, not promoted automatically to a defect. PostgreSQL 18's `CountOtherDBBackends` waits up to five seconds for remaining sessions; DROP refuses if sessions remain rather than removing a database still in use. That server wait mitigates the ordinary asynchronous-disconnect interval. [PostgreSQL 18 backend check](https://raw.githubusercontent.com/postgres/postgres/REL_18_STABLE/src/backend/storage/ipc/procarray.c), [DROP implementation](https://raw.githubusercontent.com/postgres/postgres/REL_18_STABLE/src/backend/commands/dbcommands.c).

No concrete failing interleaving within this fixture's current limits was established without assuming an arbitrary extended scheduling/server stall or adding a new fault. Thus this is an explicit cleanup-proof limitation/non-finding, not a counted WARNING or a requirement to refactor the unchanged shared process helper. This report does not claim that process join was observed. No FORCE or broad backend termination is needed or proposed by this review. An actual busy-database cleanup failure would remain a nonzero test failure and justify focused follow-up.

### Attributed evidence and remaining limits

Root supplied these native execution results; this reviewer did not run them:

- The original combined ConcurrentReplay → static-all case failed at the strict `workflow_payload` stage: static 60.89s, package 81.930s, exit 1, elapsed 84.37s.
- Tests-first regression commit `b336634` seeded and snapshotted the unrelated shared workflow and failed at the intended `workflow_payload` assertion: static 64.55s, package 103.061s, exit 1, elapsed 112.80s. An earlier `COORDINATOR_NOT_AVAILABLE` setup attempt was not counted as behavioral RED.
- The private-PG repair at `df0bfbb` returned exit 0: static 4.76s, package 44.876s, elapsed 51.89s, including the retained full-row preservation assertion.

Those results support the stated causal repair, not a complete final matrix result. The original combined causal GREEN, complete scenario family and full aggregate were still pending when assigned for review. Hosted CI and Phase 3 completion remain unclaimed. Earlier unexplained failures are not retroactively assigned this cause merely because one contamination counterexample has now been isolated.

Only the new report was written. No source edits, tests, compilation, live operations, staging, commits or nested agents were performed by this reviewer. Extra contract reads were limited to `backend/internal/database/database.go`, `backend/internal/database/migrator.go`, `backend/test/lifecycle_test.go`, `backend/test/alertwriter_conflict_test.go`, `backend/test/recovery_test.go`, `backend/tools/check-alerts/main.go`, and the pinned PG image declaration. They do not add changed files to the one-file scope.

---

_Reviewer: remaining-source review specialist (gsd-code-reviewer)_
_Depth: standard; narrow frozen fixture repair review only_
