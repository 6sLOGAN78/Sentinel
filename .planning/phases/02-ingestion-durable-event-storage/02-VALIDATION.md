---
phase: 2
slug: ingestion-durable-event-storage
status: passed
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

**Approval:** local Phase 2 validation passed on 2026-10-16 with real Docker-backed integration and two exact seed runs. Hosted GitHub Actions remains pending until a branch is pushed.
