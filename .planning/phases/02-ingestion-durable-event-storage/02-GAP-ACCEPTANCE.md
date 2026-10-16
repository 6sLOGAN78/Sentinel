# Phase 02 repair acceptance — 2026-10-17 local session

This is additive implementer evidence for plan 02-21, not an independent phase verdict. Original `02-VERIFICATION.md` (`gaps_found`) and `02-REVIEW.md` (`issues_found`) remain unchanged. Hosted GitHub Actions is pending; no push is part of this gate.

## Task 1 — canonical story and tracking

Observed at 2026-10-16T19:48:26Z (2026-10-17 Asia/Kolkata):

- `gsd-sdk query init.execute-phase 2`: exit 0; 22 plans, 21 committed summaries, only 02-21 incomplete.
- `gsd-sdk query user-story.validate --story 'As a producer with a scoped API key, I want to submit event batches acknowledged only after Kafka accepts them, so that my events become queryable and duplicate-safe in ClickHouse.'`: exit 0 and **valid=true**.
- `gsd-sdk query roadmap.get-phase 2`: exit 0; exact canonical goal persisted, mode `mvp`, all five success criteria and 22 ordered historical/repair plans retained.
- `gsd-sdk query state.load`: exit 0. Initial frontmatter incorrectly said `completed` despite the pending body. Root corrected this through registered `state.patch`, commit `b2eb387`; current status is executing final local validation with independent verification pending. The acceptance executor does not mutate shared STATE/ROADMAP/REQUIREMENTS.
- Root previously applied the explicitly authorized focused ROADMAP fallback because this SDK has no registered goal/list mutation. Existing canonical text was verified without rewriting it.

## Task 2 — bounded local acceptance

**Local repair acceptance passed.** The one-shot sequential gate ran from `2026-10-16T19:49:19Z` to `2026-10-16T20:02:48Z`: exit **0**, **809 seconds (13m29s)**, within the explicit outer `timeout 45m`. Source included root prerequisite fix `27d3390`; metadata HEAD at gate start was `4e1c693`. No source changed during the gate and no failing stage was retried. Timings below are observed wall seconds at one-second resolution; `0` means it completed within that resolution.

| Command (repository root unless stated) | Exit | Wall seconds | Observed result |
|---|---:|---:|---|
| `timeout 10m make lint test` | 0 | 16 | Default Go lint/race suites, web lint/typecheck and 6 tests, generator ruff/format/mypy and 54 pytest tests passed. Some unchanged default Go packages legitimately reported cached results. |
| `cd backend && timeout 3m go vet ./...` | 0 | 0 | Default vet passed. |
| `cd backend && timeout 3m go vet -tags=integration ./...` | 0 | 0 | Integration-tag vet passed. |
| `cd backend && timeout 5m go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | 0 | 1 | Zero issues. |
| `timeout 3m scripts/check-go-dependencies.sh` | 0 | 1 | Both actual default and integration dependency graphs satisfy policy; each enumeration also has an internal 60-second bound. |
| `timeout 3m python3 -B -m unittest discover -s scripts -p 'test_*.py'` | 0 | 4 | All 25 script regressions passed: seed 11, Docker context 4, dependency policy 5, stack configuration 5. |
| `timeout 90s python3 -B scripts/check-docker-context.py` | 0 | 0 | Actual Docker export excluded harmless private fixtures and retained required source; no real credentials entered the synthetic context. |
| `timeout 1m scripts/check-pins.sh` | 0 | 0 | Image digests, loopback publications, environment hygiene, lockfiles and workflows passed. |
| `timeout 1m scripts/check-makefile-dotenv.sh` | 0 | 0 | Literal dotenv handling passed. |
| `timeout 11m make test-integ` | 0 | 256 | Full Docker-backed package passed in **253.377s**; Make retains `go test -timeout 10m`. |
| `scripts/go-test-nonempty.sh './test/...' 'TestKeyctl\|TestIngestAuth' -tags=integration -timeout 4m -p 1` | 0 | 36 | 3 top-level tests passed; package **34.852s**. |
| `scripts/go-test-nonempty.sh './test/...' 'TestIngest' -tags=integration -timeout 6m -p 1` | 0 | 39 | 7 top-level tests passed; package **37.708s**. |
| `scripts/go-test-nonempty.sh './test/...' 'TestDedupe\|TestEventLookupTenantScope\|TestEventsSchema\|TestEventsTTLConfigurable' -tags=integration -timeout 4m -p 1` | 0 | 28 | 5 top-level tests passed; package **26.583s**. |
| `scripts/go-test-nonempty.sh './test/...' 'TestWriterCommitAfterInsert\|TestWriterPoisonToDLQ\|TestWriterDLQFailureNoCommit\|TestWriterFailureExitsNonzero\|TestWriterSlowPoisonRebalance' -tags=integration -timeout 5m -p 1` | 0 | 55 | 5 top-level tests passed, including slow-poison rebalance; package **53.507s**. |
| `scripts/go-test-nonempty.sh './test/...' 'TestWriterCrashRestart\|TestIngestRetryNoDuplicates\|TestIngestKafkaTimeout503' -tags=integration -timeout 6m -p 1` | 0 | 76 | 3 causal recovery tests passed; package **74.625s**. |
| `scripts/go-test-nonempty.sh './test/...' 'TestRateLimit' -tags=integration -timeout 4m -p 1` | 0 | 20 | 6 top-level tests passed; package **18.690s**. |
| `scripts/go-test-nonempty.sh './test/...' 'TestReadyz\|TestHealthz\|TestBinaryLifecycle' -tags=integration -timeout 5m -p 1` | 0 | 69 | 9 top-level tests passed; package **67.567s**. |
| `cd backend && timeout 3m go test -tags=integration ./test -run '^TestWriterSlowPoisonRebalance$' -count=1 -timeout 3m -p 1` | 0 | 33 | Exact fresh regression passed; package **32.403s**. |
| `cd backend && timeout 3m go test -tags=integration ./test -run '^TestTopicRetentionOverridesReapply$' -count=1 -timeout 3m -p 1` | 0 | 66 | Exact fresh retention regression passed; package **64.612s**. |
| Default `go list` file inspection, `go build ./cmd/...`, temporary-path `go build -o ... ./tools/healthprobe` (outer 3m) | 0 | 2 | Default writer files `[failpoint.go writer.go]`; Kafka files `[consumer.go dlq.go failpoint.go producer.go]`. Both `failpoint.go` files are `!integration` no-op shims; neither integration control file was selected. Healthprobe binary stayed under `/tmp`, not the repository. |
| `timeout 10m make dev-up` | 0 | 45 | Existing infrastructure, migrations and newly rebuilt ingest/event-writer/query-api became healthy. |
| `timeout 3m scripts/check-stack.sh` | 0 | 14 | Healthy dependencies/apps, all required ports/readiness, disabled topic auto-create, native dependency checks and effective topic policies passed. |
| `timeout 10m make seed` — first | 0 | 25 | Complete current-run exact-ID oracle passed; totals below. |
| `timeout 10m make seed` — second, consecutive | 0 | 23 | Complete independent current-run exact-ID oracle passed; totals below. |

Each nonempty CI group used the exact filter/finite flags from `.github/workflows/ci.yml`; the wrapper uses `-count=1` and requires passing-test output. Exact regression invocations also used `-count=1`. The complete integration gate ran only once after dependent repairs.

### Exact real seed outcomes

| Run | sent | accepted | visible | API spotchecks | tenant_isolation | actual retries |
|---|---:|---:|---:|---:|---:|---:|
| First, started `20:02:00Z` | 1000 | 1000 | 1000 | 3 | 1 | 0 |
| Second, started `20:02:25Z` | 1000 | 1000 | 1000 | 3 | 1 | 0 |

`scripts/seed.py` loaded each newly generated private manifest, required exactly 1000 unique canonical IDs and accepted events, then required tenant/run-start-bound `uniqExact` **and full set equality** against those IDs. It checked first/middle/last IDs through the real query API and required the other tenant to receive 404. Historical rows cannot satisfy this oracle. No reset, `OPTIMIZE`, topic deletion or volume removal was used.

Stack acceptance observed effective topic values of events **6 partitions / 86400000ms**, alerts **3 / 604800000ms**, and DLQ **1 / 604800000ms**, each RF=1. Startup reapplied those existing effective settings without editing `.env`. Local credential files and the final manifest remained mode **0600**; credential contents were never printed.

### Preserved-volume evidence

The gate's generic `compose` project-label rows were not valid Sentinel evidence because Compose explicitly names project `sentinel`. Before any normal-stack startup, a corrected read-only snapshot at `20:00:56Z` inspected the actual `sentinel`-labeled volumes. After both seeds, `20:02:54Z` inspection returned the identical three names **and creation timestamps**:

| Named volume | CreatedAt, unchanged before/after |
|---|---|
| `sentinel_clickhouse-data` | `2026-10-11T21:08:33+05:30` |
| `sentinel_kafka-data` | `2026-10-11T21:08:33+05:30` |
| `sentinel_postgres-data` | `2026-10-11T21:08:33+05:30` |

The corrected comparison passed and excluded unrelated Compose-project rows. Private bounded-command logs are retained locally in `/tmp/sentinel-02-21-gate.ZZYUAD/`; only sanitized aggregates are recorded here.

### Final-gate prerequisite deviation

Before this gate, root fixed four pre-existing integration-test style findings and added narrowly scoped `#nosec G304 G703` comments only at trusted integration-runner barrier-file access (`27d3390`). The fresh full integration-tag lint/vet commands above then passed. This was a required static-gate repair, not a production security exemption; default builds exclude both integration control files. The acceptance executor changed no application source.

## Task 3 — evidence handoff

Pending observed gate results and repair-to-regression mapping. Independent goal verification, security assessment and code review are orchestrator-owned and pending. Hosted CI is separately pending.
