---
phase: 2
slug: ingestion-durable-event-storage
status: pending
local_repair_acceptance: passed
independent_reverification: local_audits_passed_root_closeout_complete
hosted_ci: readiness_repeat_failed_repair_pending
hosted_ci_run: 38970203760
hosted_ci_revision: 36bba7cfefbe8955879d6b402c39495eeef999ad
nyquist_compliant: true
wave_0_complete: true
created: 2026-10-12
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `02-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `go test` + testify (unit next to code; integration in `backend/test/` with `//go:build integration` and shared TestMain containers); pytest + ruff + mypy for `generator/` |
| **Config file** | `backend/.golangci.yml`, `generator/pyproject.toml`, `scripts/go-test-nonempty.sh` (all exist) |
| **Quick run command** | `cd backend && GOTOOLCHAIN=go1.26.9 go test -race ./internal/...` and `cd generator && uv run --locked pytest` |
| **Full suite command** | `make lint test && make test-integ` |
| **Observed runtime** | Full Docker-backed integration suite: 204.715 s on 2026-10-16; separately guarded groups also passed |

---

## Sampling Rate

- **After every task commit:** Run `cd backend && GOTOOLCHAIN=go1.26.9 go test -race ./internal/<touched pkg>/...` plus `go vet` on touched packages
- **After every plan wave:** Run `make lint test`
- **Before `/gsd:verify-work`:** `make test-integ` green; `scripts/go-test-nonempty.sh ./test/... 'TestDedupe|TestWriter|TestIngest|TestRateLimit|TestReadyz' -tags=integration -timeout 10m -p 1`; `make dev-up && make seed` exit 0
- **Max feedback latency:** target 60 seconds for quick unit/static loop; real-container acceptance has explicit longer task ceilings

---

## Per-Task Verification Map

Task bindings below now record execution results from the completed local Phase 2 gate on 2026-10-16. Each plan contains concrete tasks and a threat model; hosted GitHub Actions remains a separate manual verification.

| Task ID | Wave | Requirements | Threat Ref | Test Type | Automated Command | Test Availability | Status |
|---------|------|--------------|------------|-----------|-------------------|-------------------|--------|
| 02-00-01 | 1 | ING-05, ING-07 | T-02-00 | unit / static | `cd backend && go test -race ./internal/middleware/... ./internal/httpx/... ./internal/config/... -timeout 60s` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-00-02 | 1 | ING-05, ING-07 | T-02-00 | unit / static | `cd backend && go test -race ./internal/config/... ./internal/httpx/... -timeout 60s` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-00-03 | 1 | ING-05, ING-07 | T-02-00 | unit / static | `cd backend && go test -race ./internal/middleware/... ./internal/httpx/... ./internal/config/... -timeout 60s && go vet ./internal/middleware/... ./internal/httpx/... ./internal/config/...` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-01-01 | 1 | ING-01, ING-05, GEN-03 | T-02-01 | unit + integration | `cd backend &amp;&amp; go test -tags=integration ./test/... -run 'TestKeyctl&#124;TestMigratePostgresIdempotent' -timeout 3m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-01-02 | 1 | ING-01, ING-05, GEN-03 | T-02-01 | unit / static | `cd backend &amp;&amp; go test -race ./internal/apikeys/... ./tools/keyctl/... -timeout 60s` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-01-03 | 1 | ING-01, ING-05, GEN-03 | T-02-01 | unit / static | `cd backend &amp;&amp; go test -race ./internal/apikeys/... ./tools/keyctl/... -timeout 60s` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-02-01 | 2 | ING-01, ING-03, ING-04, ING-05, ING-07 | T-02-02 | unit + integration | `cd backend && go test -tags=integration ./test/... -run 'TestIngestAcceptedAfterAck&#124;TestIngestAuth' -timeout 3m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-02-02 | 2 | ING-01, ING-03, ING-04, ING-05, ING-07 | T-02-02 | unit + integration | `cd backend && go test -race ./internal/ingest/... ./internal/kafkax/... ./internal/config/... -timeout 60s && go test -tags=integration ./test/... -run 'TestIngestAcceptedAfterAck&#124;TestIngestAuth&#124;TestIngestShedsUnderLoad&#124;TestBinaryLifecycle' -timeout 5m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-03-01 | 3 | ING-02, ING-05 | T-02-03 | unit + integration | `cd backend && go test -tags=integration ./test/... -run 'TestIngestInvalidBatch&#124;TestIngestStatusCodes' -timeout 3m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-03-02 | 3 | ING-02, ING-05 | T-02-03 | unit / static | `cd backend && go test -race ./internal/ingest/... ./internal/errs/... -timeout 60s` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-03-03 | 3 | ING-02, ING-05 | T-02-03 | unit / static | `cd backend && go test ./test/... -run TestOpenAPI -timeout 60s && cd ../web && npm run gen:api && npm run typecheck` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-04-01 | 4 | STOR-02, STOR-03, ING-04 | T-02-04 | unit + integration | `cd backend && go test -tags=integration ./test/... -run 'TestDedupeDoubleInsertNoOptimize&#124;TestDedupeDifferentOccurredAt&#124;TestEventLookupTenantScope' -timeout 3m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-04-02 | 4 | STOR-02, STOR-03, ING-04 | T-02-04 | unit + integration | `cd backend && go test -race ./internal/storage/clickhouse/... ./internal/eventquery/... ./internal/config/... -timeout 60s && go test -tags=integration ./test/... -run 'TestDedupe&#124;TestEventLookupTenantScope&#124;TestEventsSchema' -timeout 3m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-04-03 | 4 | STOR-02, STOR-03, ING-04 | T-02-04 | unit + integration | `cd backend && go test ./migrations/... ./internal/storage/clickhouse/... -timeout 60s && go test -tags=integration ./test/... -run TestDedupe -timeout 3m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-05-01 | 5 | STOR-02 | T-02-05 | unit + integration | `cd backend && go test ./internal/chmigrate/... ./tools/chmigrate/... -timeout 60s && go test -tags=integration ./test/... -run 'TestEventsTTLConfigurable&#124;TestClickHouseMigrations' -timeout 3m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-05-02 | 5 | STOR-02 | T-02-05 | unit + integration | `cd backend && go test -race ./internal/chmigrate/... ./tools/chmigrate/... -timeout 60s && go test -tags=integration ./test/... -run 'TestEventsTTLConfigurable&#124;TestMigrations' -timeout 3m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-05-03 | 5 | STOR-02 | T-02-05 | unit / static | `cd backend && go test ./internal/chmigrate/... ./tools/chmigrate/... -timeout 60s` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-06-01 | 5 | STOR-01, ING-04 | T-02-06 | unit + integration | `cd backend && go test -race ./internal/app/... ./internal/eventwriter/... ./internal/kafkax/... -timeout 60s && go test -tags=integration ./test/... -run 'TestWriterCommitAfterInsert&#124;TestWriterInsertFailureNoCommit' -timeout 4m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-06-02 | 5 | STOR-01, ING-04 | T-02-06 | unit + integration | `cd backend && go test -race ./internal/eventwriter/... ./internal/kafkax/... ./internal/config/... -timeout 60s && go test -tags=integration ./test/... -run 'TestWriterCommitAfterInsert&#124;TestWriterInsertFailureNoCommit' -timeout 4m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-06-03 | 5 | STOR-01, ING-04 | T-02-06 | unit + integration | `cd backend && go test -race ./internal/app/... ./internal/eventwriter/... -timeout 60s && go test -tags=integration ./test/... -run 'TestWriter&#124;TestLifecycle' -timeout 4m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-07-01 | 6 | STOR-04, STOR-01 | T-02-07 | unit + integration | `cd backend && go test -race ./internal/kafkax/... ./internal/eventwriter/... -timeout 60s && go test -tags=integration ./test/... -run 'TestWriterPoisonToDLQ&#124;TestWriterDLQFailureNoCommit' -timeout 4m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-07-02 | 6 | STOR-04, STOR-01 | T-02-07 | unit + integration | `cd backend && go test -race ./internal/eventwriter/... ./internal/kafkax/... -timeout 60s && go test -tags=integration ./test/... -run 'TestWriterPoisonToDLQ&#124;TestWriterDLQFailureNoCommit&#124;TestWriterInsertFailureNoCommit' -timeout 4m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-07-03 | 6 | STOR-04, STOR-01 | T-02-07 | unit / static | `cd backend && go test -race ./internal/eventwriter/... ./internal/kafkax/... -timeout 60s` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-08-01 | 6 | ING-06, ING-07, ING-05 | T-02-08 | unit + integration | `cd backend && go test -race ./internal/ratelimit/... ./internal/ingest/... -timeout 60s && go test -tags=integration ./test/... -run TestRateLimit -timeout 4m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-08-02 | 6 | ING-06, ING-07, ING-05 | T-02-08 | unit + integration | `cd backend && go test -race ./internal/ratelimit/... ./internal/redisx/... ./internal/config/... ./internal/ingest/... -timeout 60s && go test -tags=integration ./test/... -run TestRateLimit -timeout 4m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-08-03 | 6 | ING-06, ING-07, ING-05 | T-02-08 | unit + integration | `cd backend && go test -race ./internal/ingest/... ./internal/ratelimit/... -timeout 60s && go test -tags=integration ./test/... -run 'TestRateLimit&#124;TestIngestStatusCodes&#124;TestIngestShedsUnderLoad' -timeout 4m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-09-01 | 7 | OBS-01 | T-02-09 | unit + integration | `cd backend && go test -race ./internal/opshttp/... -timeout 60s && go test -tags=integration ./test/... -run 'TestReadyz&#124;TestHealthz&#124;TestLifecycle' -timeout 5m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-09-02 | 7 | OBS-01 | T-02-09 | unit + integration | `cd backend && go test -race ./internal/opshttp/... ./internal/config/... -timeout 60s && go test -tags=integration ./test/... -run 'TestReadyz&#124;TestHealthz&#124;TestLifecycle' -timeout 5m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-09-03 | 7 | OBS-01 | T-02-09 | unit + integration | `cd backend && go test -race ./internal/app/... ./internal/opshttp/... -timeout 60s && go test -tags=integration ./test/... -run 'TestReadyz&#124;TestHealthz&#124;TestLifecycle' -timeout 5m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-10-01 | 1 | GEN-01 | T-02-10 | unit / static | `cd generator && uv run --locked pytest tests/test_events.py tests/test_send.py` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-10-02 | 1 | GEN-01 | T-02-10 | unit / static | `cd generator && uv lock --check && uv run --locked pytest tests/test_events.py tests/test_send.py && uv run --locked ruff check . && uv run --locked mypy src tests` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-10-03 | 1 | GEN-01 | T-02-10 | unit / static | `cd generator && uv run --locked sentinel-generator send --help && uv run --locked pytest && uv run --locked ruff format --check .` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-11-01 | 8 | ING-04, STOR-01, STOR-03, ING-07 | T-02-11 | unit / static | `cd backend && go test -race ./internal/eventwriter/... ./internal/kafkax/... -timeout 60s && go list -f '{{.GoFiles}}' ./internal/eventwriter ./internal/kafkax && go build ./cmd/...` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-11-02 | 8 | ING-04, STOR-01, STOR-03, ING-07 | T-02-11 | unit + integration | `cd backend && go test -tags=integration ./test/... -run 'TestWriterCrashRestart&#124;TestWriterInsertFailureNoCommit' -timeout 5m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-11-03 | 8 | ING-04, STOR-01, STOR-03, ING-07 | T-02-11 | unit + integration | `cd backend && go test -tags=integration ./test/... -run 'TestIngestKafkaTimeout503&#124;TestIngestRetryNoDuplicates&#124;TestIngestShedsUnderLoad&#124;TestWriterCrashRestart' -timeout 6m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-12-01 | 9 | GEN-03, OBS-01, ING-07 | T-02-12 | unit / static | `cd backend && go test -race ./tools/healthprobe/... -timeout 60s && go build ./cmd/... ./tools/healthprobe` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-12-02 | 9 | GEN-03, OBS-01, ING-07 | T-02-12 | e2e | `docker compose -f deployments/compose/docker-compose.yml --env-file .env config --quiet && scripts/check-pins.sh && scripts/check-makefile-dotenv.sh && make dev-up && scripts/check-stack.sh` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-12-03 | 9 | GEN-03, OBS-01, ING-07 | T-02-12 | e2e | `make dev-up && scripts/check-stack.sh && scripts/check-pins.sh && make lint test` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-13-01 | 10 | GEN-03, GEN-01, STOR-03, ING-01, ING-02, ING-03, ING-04, ING-05, ING-06, ING-07, STOR-01, STOR-02, STOR-04, OBS-01 | T-02-13 | e2e | `python3 -B -m unittest discover -s scripts -p test_seed.py && scripts/check-makefile-dotenv.sh && make seed` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-13-02 | 10 | GEN-03, GEN-01, STOR-03, ING-01, ING-02, ING-03, ING-04, ING-05, ING-06, ING-07, STOR-01, STOR-02, STOR-04, OBS-01 | T-02-13 | unit + integration | `python3 -B -m unittest discover -s scripts -p test_seed.py && scripts/check-pins.sh && scripts/go-test-nonempty.sh './test/...' 'TestIngest' -tags=integration -timeout 4m -p 1 && scripts/go-test-nonempty.sh './test/...' 'TestWriter' -tags=integration -timeout 5m -p 1` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |
| 02-13-03 | 10 | GEN-03, GEN-01, STOR-03, ING-01, ING-02, ING-03, ING-04, ING-05, ING-06, ING-07, STOR-01, STOR-02, STOR-04, OBS-01 | T-02-13 | e2e | `make lint test && make test-integ && python3 -B -m unittest discover -s scripts -p test_seed.py && make seed && make seed` | Implemented; full local phase gate passed 2026-10-16 | ✅ passed |

Quick feedback is the unit/static part of the task command (target <=60s). Real-container acceptance has explicit 3–6 minute ceilings and runs at the specified task/feature gate; full suite and live seed are phase gates. No runtime is claimed as measured during planning. RED test-authoring tasks record the named expected failure, then their implementation task must make the same tests pass. A failed or empty test selection is not GREEN.

---

## Wave 0 Requirements

- [x] 02-02 Task 1 — `backend/internal/testing`: export `FreePort`; container helpers that expose IDs for pause/unpause on dedicated (not shared) containers; unique-topic helper via kadm
- [x] 02-02 Task 1 / 02-09 Task 1 — `backend/test/lifecycle_test.go` `serviceEnv`: database for ingest, `NATIVE_ADDRESS` for ClickHouse users, unique ops/server ports
- [x] 02-01 / 02-02 / 02-04 / 02-06 / 02-08 / 02-09 first tasks — Unit test files for `apikeys`, `ratelimit`, `opshttp`, `ingest`, `kafkax`, `storage/clickhouse`
- [x] Respective feature first tasks (02-02 / 02-04 / 02-06 / 02-08 / 02-09 / 02-11) — Integration files in `backend/test/`: `ingest_test.go`, `writer_test.go`, `dedupe_test.go`, `ratelimit_test.go`, `readyz_test.go`
- [x] 02-10 Task 1 / Task 2 — Generator `tests/test_send.py` with `httpx.MockTransport`; refresh `uv.lock` and `requirements.txt`
- [x] 02-13 Task 2 — CI: banned-module step; extended `go-test-nonempty.sh` filter lists; `.gitignore` `/.local/`; `.dockerignore`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| CI run on GitHub including the integration job | GEN-03, all | Needs a push to origin | Push a branch and confirm all CI jobs pass |

---

## Execution Evidence — 2026-10-16

- `make lint test && make test-integ` exited 0. Lint, web type/tests, Go race tests, 37 generator tests, and the complete Docker-backed integration suite passed; the integration suite reported `ok github.com/6sLOGAN78/sentinel/test 204.715s`.
- Each CI-owned nonempty integration group passed independently: keyctl/API keys, ingest, dedupe/schema/TTL, writer/DLQ, recovery, rate limiting, and readiness/lifecycle.
- `python3 -B -m unittest scripts.test_seed` passed all 10 orchestration tests.
- Two consecutive `make seed` runs exited 0 without resetting volumes. Each reported `sent=1000`, `accepted=1000`, `visible=1000`, `api_spotchecks=3`, `tenant_isolation=1`, and `retries=0` for its own intended ID set.
- The default production build passed. `go list` selected the `!integration` no-op shims while excluding the integration failure-control files.
- No Phase 2 seed or application placeholder remains. `make bench` remains deliberately unavailable and nonzero until Phase 5.
- Hosted GitHub Actions is not claimed here because no origin push was performed.

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Quick loop targets <=60s; real-container feature gates have explicit longer ceilings
- [x] `nyquist_compliant: true` set in frontmatter

**Historical approval:** local Phase 2 validation passed on 2026-10-16 with real Docker-backed integration and two exact seed runs. Hosted GitHub Actions remains pending until a branch is pushed. The later verification/review findings require the pending repairs and fresh acceptance below.

**Final approval (2026-10-17 Asia/Kolkata):** the repaired code at `36bba7c` passed hosted run [38970203760](https://github.com/6sLOGAN78/Sentinel/actions/runs/38970203760): all eight jobs, full real integration, all seven independently nonempty groups and Compose startup/check/repeat migration/teardown. This supersedes pending-status statements in the historical observations below. Independent local review is clean, the threat audit has zero open items, and root completed inline goal verification after observing hosted success. See `02-VERIFICATION.md` and `02-CI-CLOSEOUT.md` for provenance and exact evidence.

## Gap-Closure Validation — Local Acceptance Passed; Independent Assessment Pending

The original local gate and 41 passed task rows above remain valid historical observations at the pre-review revision. They do not establish the repaired boundaries. The seven code-repair plans now have committed behavioral RED/GREEN evidence, and 02-21's fresh local acceptance passed on 2026-10-17 Asia/Kolkata. The complete gate exited 0 in 809 seconds, including all seven independently nonempty CI integration groups and two complete 1000-ID seeds. Overall status remains pending until independent assessment evaluates the original gaps; hosted CI is a separate pending check. See [02-GAP-ACCEPTANCE.md](02-GAP-ACCEPTANCE.md) for actual commands, exits, timings, source-to-repair mappings and preserved-volume evidence.

| Task ID | Wave | Requirements | Threat Ref | Test Type | Automated Command | Test Availability | Status |
|---------|------|--------------|------------|-----------|-------------------|-------------------|--------|
| 02-14-01 | 11 | STOR-04, ING-07 | T-02-14 | Go unit | `cd backend && go test ./internal/kafkax -run TestBuildDLQRecordEncodedHeaderFlood -count=1 -timeout 30s` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-14-02 | 11 | STOR-04, ING-07 | T-02-14 | Go race | `cd backend && go test -race ./internal/kafkax -count=1 -timeout 60s` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-15-01 | 12 | STOR-01, STOR-04, ING-04, ING-07 | T-02-15 | unit + real Kafka | `cd backend && go test -tags=integration ./test -run TestWriterSlowPoisonRebalance -count=1 -timeout 3m -p 1` | Observed RED/GREEN; see 02-15-SUMMARY | ✅ passed |
| 02-15-02 | 12 | STOR-01, STOR-04, ING-04, ING-07 | T-02-15 | Go race | `cd backend && go test -race ./internal/config ./internal/kafkax ./internal/eventwriter -count=1 -timeout 60s` | Observed RED/GREEN; see 02-15-SUMMARY | ✅ passed |
| 02-15-03 | 12 | STOR-01, STOR-04, ING-04, ING-07 | T-02-15 | real rebalance + replay | `cd backend && go test -tags=integration ./test -run TestWriterSlowPoisonRebalance -count=1 -timeout 3m -p 1` | Observed RED/GREEN; see 02-15-SUMMARY | ✅ passed |
| 02-16-01 | 11 | ING-01, ING-05, STOR-03 | T-02-16 | runtime contract | `cd backend && go test ./test ./internal/eventquery -run 'TestOpenAPIGetEventRuntimeSecurity&#124;TestEventLookup' -count=1 -timeout 30s` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-16-02 | 11 | ING-01, ING-05, STOR-03 | T-02-16 | contract + real lookup | `cd backend && go test -tags=integration ./test -run TestEventLookupTenantScope -count=1 -timeout 3m -p 1` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-17-01 | 11 | OBS-01, GEN-03 | T-02-17 | Go unit | `cd backend && go test ./tools/healthprobe -run TestRunRejectsRedirects -count=1 -timeout 15s` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-17-02 | 11 | OBS-01, GEN-03 | T-02-17 | Go race/readiness | `cd backend && go test -race ./tools/healthprobe -count=1 -timeout 60s` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-18-01 | 11 | GEN-01, GEN-03 | T-02-18 | pytest | `cd generator && uv run --locked pytest tests/test_send.py -k manifest` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-18-02 | 11 | GEN-01, GEN-03 | T-02-18 | stdlib + pytest | `python3 -B -m unittest discover -s scripts -p test_seed.py` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-19-01 | 13 | GEN-03, ING-07 | T-02-19 | Docker context fixture | `python3 -B -m unittest discover -s scripts -p test_docker_context.py` | Observed RED/GREEN; see 02-19-SUMMARY | ✅ passed |
| 02-19-02 | 13 | GEN-03, ING-07 | T-02-19 | dual Go graph | `python3 -B -m unittest discover -s scripts -p test_go_dependencies.py && scripts/check-go-dependencies.sh` | Observed RED/GREEN; see 02-19-SUMMARY | ✅ passed |
| 02-20-01 | 11 | GEN-03, STOR-04 | T-02-20 | real Kafka topic config | `cd backend && go test -tags=integration ./test -run TestTopicRetentionOverridesReapply -count=1 -timeout 3m -p 1` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-20-02 | 11 | GEN-03, STOR-04 | T-02-20 | config + real Kafka | `python3 -B -m unittest discover -s scripts -p test_stack_config.py` | Observed RED/GREEN; see plan SUMMARY and Wave 11 gate | ✅ passed |
| 02-21-01 | 14 | all Phase 2 IDs | T-02-21 | canonical/status | `gsd-sdk query user-story.validate --story '<canonical story>' && gsd-sdk query roadmap.get-phase 2 && gsd-sdk query state.load` | Observed valid=true, exact persisted goal and executing/pending status; metadata task has no fabricated RED | ✅ passed |
| 02-21-02 | 14 | all Phase 2 IDs | T-02-21 | full gate + exact e2e | `make lint test && make test-integ && timeout 10m make seed && timeout 10m make seed` | Observed outer 45m gate exit 0 in 809s; all additional static, script, exact and nonempty groups in GAP-ACCEPTANCE passed | ✅ passed |
| 02-21-03 | 14 | all Phase 2 IDs | T-02-21 | evidence/tracking | `test -s .planning/phases/02-ingestion-durable-event-storage/02-GAP-ACCEPTANCE.md && git diff --check` | Observed additive mapping/evidence; original report hashes unchanged; independent/hosted verdict pending | ✅ passed |

Full command details, extra static gates and exact nonempty groups live in the corresponding PLAN tasks. Each defect must first fail a behavioral regression, then the same test must pass; missing executables, compile errors and empty selections are not RED/GREEN evidence.

New test scaffolds created and exercised by the owning tasks and final acceptance:

- [x] 02-14: independent encoded-header bound cases in kafkax/dlq_test.go.
- [x] 02-15: aggregate unit cases and integration writer_rebalance_test.go.
- [x] 02-16: YAML-driven openapi_runtime_auth_test.go plus handler negatives.
- [x] 02-17: redirect no-second-request regressions.
- [x] 02-18: parent-mode/symlink and ambient-seed-key regressions.
- [x] 02-19: real Docker context fixture/mutation and dual-tag/fail-closed checker tests.
- [x] 02-20: topic_retention_test.go and test_stack_config.py.
- [x] 02-21: additive GAP-ACCEPTANCE with canonical validator, full local gate, two exact seed runs, preserved volumes and true status.

**Observed local repair sign-off:** the new 18 task bindings passed their observed owning-plan/final gates. This records local execution only. Original 2026-10-16 verification/review findings remain historical evidence, and the orchestrator must archive them before independent reassessment. Hosted GitHub Actions is pending because no push occurred. No requirement or overall phase completion is inferred from SUMMARY counts.

Quick loops target <=60s; real rebalance/retention tests have <=3m command ceilings, related replay gate <=6m, full integration <=10m, each seed <=10m and complete final local gate <=45m. Hosted GitHub Actions remains pending; no push is included. Preserve dated original VERIFICATION/REVIEW snapshots before updating canonical assessment reports. Coverage mapping: 02-GAP-COVERAGE.md.
