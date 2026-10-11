---
phase: 2
slug: ingestion-durable-event-storage
status: draft
nyquist_compliant: false
wave_0_complete: false
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
| **Estimated runtime** | ~45 s quick, ~5 min full (container-backed integration) |

---

## Sampling Rate

- **After every task commit:** Run `cd backend && GOTOOLCHAIN=go1.26.9 go test -race ./internal/<touched pkg>/...` plus `go vet` on touched packages
- **After every plan wave:** Run `make lint test`
- **Before `/gsd:verify-work`:** `make test-integ` green; `scripts/go-test-nonempty.sh ./test/... 'TestDedupe|TestWriter|TestIngest|TestRateLimit|TestReadyz' -tags=integration -timeout 10m -p 1`; `make dev-up && make seed` exit 0
- **Max feedback latency:** 60 seconds (quick loop)

---

## Per-Task Verification Map

Task IDs are bound by the planner. Rows are keyed by requirement and behavior.

| Requirement | Behavior | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|-------------|----------|------------|-----------------|-----------|-------------------|-------------|--------|
| ING-01 | Valid key + batch gives 202; tenant from key; key hashed at rest; cache TTL, cap, negatives uncached | — | Secret never stored or logged; constant-time compare | unit + integration | `go test ./internal/apikeys/... ./internal/ingest/... -run 'TestAPIKey\|TestIngestAuth'` | ❌ W0 | ⬜ pending |
| ING-01 | Escaped-colon route: `:batch` matches, `:xyz` and `%3A` give 404 | — | No accidental param route | unit | `go test ./internal/ingest/... -run TestRouteLiteralColon` | ❌ W0 | ⬜ pending |
| ING-02 | All-or-nothing 400 with `events[i].field` details (≤50), nothing published | — | Strict validation | integration | `go test -tags=integration ./test/... -run TestIngestInvalidBatch` | ❌ W0 | ⬜ pending |
| ING-03 | 202 only after ack; response fields; key `tenant\|ip`; headers | — | Durable ack semantics | integration | `... -run TestIngestAcceptedAfterAck` | ❌ W0 | ⬜ pending |
| ING-04 | Kafka timeout gives 503; identical retry gives exactly one visible copy | — | Idempotent retry | integration | `... -run 'TestIngestRetryNoDuplicates\|TestIngestKafkaTimeout503'` | ❌ W0 | ⬜ pending |
| ING-05 | 401 identical body for missing/malformed/unknown/revoked; 403 tenant + scope; 413 size and >500; 503 envelope; D-27 precedence | — | No key-state oracle; tenant isolation | integration | `... -run TestIngestStatusCodes` | ❌ W0 | ⬜ pending |
| ING-06 | 429 + Retry-After; Redis down gives 503 (closed) with zero published; open gives 202 + warn | — | Limits never silently bypassed | unit (Lua) + integration | `... -run TestRateLimit` | ❌ W0 | ⬜ pending |
| ING-07 | In-flight shedding returns 503 immediately; producer buffers bounded | — | Bounded memory | unit + integration | `go test ./internal/ingest/... -run TestInFlightShed`; `... -run TestIngestShedsUnderLoad` | ❌ W0 | ⬜ pending |
| STOR-01 | Commit only after insert; no commit on insert failure; exit non-zero after max attempts (D-26) | — | No lost acked events | integration | `... -run 'TestWriterCommitAfterInsert\|TestWriterInsertFailureNoCommit'` | ❌ W0 | ⬜ pending |
| STOR-01 | SIGKILL mid-batch + restart: no loss, no visible dupes | — | Replay safety | integration | `... -run TestWriterCrashRestart` | ❌ W0 | ⬜ pending |
| STOR-02 | Partition key, ORDER BY, TTL, bloom index present; TTL ALTER effective and idempotent | — | Bounded retention | integration + unit | `... -run 'TestEventsSchema\|TestEventsTTLConfigurable'`; `go test ./migrations/... -run TestMigrations` | ❌ W0 | ⬜ pending |
| STOR-03 | Double insert, no OPTIMIZE: raw 2N, `uniqExact` N, lookup one row; same id different `occurred_at` | — | Correct user-visible counts | integration | `... -run 'TestDedupeDoubleInsertNoOptimize\|TestDedupeDifferentOccurredAt'` | ❌ W0 (write first) | ⬜ pending |
| STOR-04 | Poison goes to DLQ with headers; commit only after DLQ ack; DLQ failure gives no commit | — | Redacted context only | integration | `... -run 'TestWriterPoisonToDLQ\|TestWriterDLQFailureNoCommit'` | ❌ W0 | ⬜ pending |
| GEN-01 | Seeded reproducibility, doc IP ranges, skew-safe anchoring (`base = run_start - count`), Retry-After honored | — | Key never on argv | unit (pytest) | `cd generator && uv run --locked pytest tests -k 'send or events'` | partial | ⬜ pending |
| GEN-03 | `make seed`: 1000 events visible (this run, `received_at >= run_start`), lookup spot-check, non-zero exit on shortfall | — | Keys stored in gitignored `.local/` | e2e (Compose) | `make dev-up && make seed` | ❌ W0 | ⬜ pending |
| OBS-01 | `/healthz` always 200; `/readyz` flips 503 when a dependency is paused and back to 200; draining on SIGTERM | — | Readiness reflects real capability | integration | `... -run 'TestReadyz\|TestHealthz'` | ❌ W0 | ⬜ pending |
| D-24 | Empty CORS gives no ACAO header; no query string or API key in logs | — | No log leakage; no open CORS | unit | `go test ./internal/middleware/... -run 'TestCORSEmptyDenies\|TestRequestLoggerNoQuery\|TestNoKeyInLogs'` | ❌ W0 | ⬜ pending |
| D-25 | Banned-module check in CI | — | Supply-chain hygiene | CI step | `! (cd backend && go list -deps -test ./... \| grep -E 'newrelic\|pkg/errors\|clerk\|asynq\|resend')` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `backend/internal/testing`: export `FreePort`; container helpers that expose IDs for pause/unpause on dedicated (not shared) containers; unique-topic helper via kadm
- [ ] `backend/test/lifecycle_test.go` `serviceEnv`: database for ingest, `NATIVE_ADDRESS` for ClickHouse users, unique ops/server ports
- [ ] Unit test files for `apikeys`, `ratelimit`, `opshttp`, `ingest`, `kafkax`, `storage/clickhouse`
- [ ] Integration files in `backend/test/`: `ingest_test.go`, `writer_test.go`, `dedupe_test.go`, `ratelimit_test.go`, `readyz_test.go`
- [ ] Generator `tests/test_send.py` with `httpx.MockTransport`; refresh `uv.lock` and `requirements.txt`
- [ ] CI: banned-module step; extended `go-test-nonempty.sh` filter lists; `.gitignore` `/.local/`; `.dockerignore`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| CI run on GitHub including the integration job | GEN-03, all | Needs a push to origin | Push a branch and confirm all CI jobs pass |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
