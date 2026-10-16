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

The table connects every structured verification gap and all nine review findings to the repair and observed evidence. RED evidence belongs to the committed repair summaries linked below; 02-21 is an acceptance/metadata plan and does not invent a new RED run or replay unsafe old production code. Fresh GREEN evidence is the Task 2 gate above, with additional detailed behavioral results in each repair summary.

| Original source / finding | Repair task and files | Named regression and observed outcome |
|---|---|---|
| Verification gap 1; premature completion metadata | 02-21 Task 1; canonical ROADMAP/STATE owned by root | `user-story.validate` returned **valid=true**, persisted exact goal/mvp/five criteria retained; root's registered state patch restored executing/pending status. Task 1 is metadata verification, with no claimed RED. |
| Verification gap 2 / CR-03: encoded DLQ bound | [02-14 Tasks 1–2](02-14-SUMMARY.md); `backend/internal/kafkax/dlq.go` | `TestBuildDLQRecordEncodedHeaderFlood` and destination-topic/boundary variants: owning-plan behavioral overflow RED then GREEN. Default race tests and integration/DLQ final gates passed. Encoded-size oracle and header cap address cardinality/varint/actual-topic bounds. |
| Verification gap 2 / CR-05: aggregate poison ownership | [02-15 Tasks 1–3](02-15-SUMMARY.md); writer, consumer, shared config | `TestWriterAggregatePoisonBudget`, `TestRebalanceTimeoutCoversWorstProcessingBudget`, `TestWriterBudget*`, `TestWriterConsumerCloseDoesNotStartShutdownTransfer`: owning-plan behavioral RED then GREEN. Real `TestWriterSlowPoisonRebalance` passed freshly at **32.403s**, plus all writer/recovery groups. Its exhaustion, failed-earliest-poison and cancellation cases recover every original poison origin, all four valid IDs, immediate unique count and final offsets. |
| Verification gap 3 / CR-04: advertised runtime auth | [02-16 Tasks 1–2](02-16-SUMMARY.md); OpenAPI, generated web types, runtime contract tests | `TestOpenAPIGetEventRuntimeSecurity/BearerAuth` reproduced advertised success versus actual 401 before repair; contract/handler regressions then passed. Full final suites and real `TestEventLookupTenantScope` group passed. `getEvent` is API-key-only; JWT remains Phase 4. |
| Verification gap 4 / CR-01: redirect escape | [02-17 Tasks 1–2](02-17-SUMMARY.md); `backend/tools/healthprobe/main.go` | `TestRunRejectsRedirects` observed inherited ten-case behavioral RED, then fresh GREEN with exactly one request for 301/302/303/307/308. Final default race, integration readiness and temporary production healthprobe build passed. |
| Verification gap 5 / CR-02: existing parent chmod | [02-18 Task 1](02-18-SUMMARY.md); generator manifest writer | `test_manifest_preserves_existing_parent_mode` observed four actual mode-change failures before repair; all manifest/precedence regressions then passed. Final **54 generator tests** passed, preserving existing parents, descriptor anchoring, symlink refusal and private atomic output. |
| Verification gap 6 / WR-03: credential build context | [02-19 Task 1](02-19-SUMMARY.md); `.dockerignore`, Docker checker/tests | `test_current_policy_excludes_private_material_in_real_export` failed on actual fixture leakage before repair. Final real Docker checker and four context tests, including exclusion-removal detection, passed. Only synthetic harmless material was exported. |
| Verification gap 7 / WR-01: ambient seed key | [02-18 Task 2](02-18-SUMMARY.md); `scripts/seed.py` | `test_generator_environment_ignores_ambient_key` failed because child env was inherited; repaired regression and all **11 seed unittests** passed in the final 25-script suite. Two real seed runs each verified exactly 1000 intended IDs and tenant isolation. |
| Verification gap 7 / WR-04: tag-only dependencies | [02-19 Task 2](02-19-SUMMARY.md); dual graph checker and CI | `test_integration_only_banned_import_fails` observed policy-bypass RED; all five checker regressions and both actual `go list` graph enumerations passed. Enumeration failures propagate before matching; pins were unchanged. |
| Review WR-02: creation-only retention/checker defaults | [02-20 Tasks 1–2](02-20-SUMMARY.md); Compose topic-init and `scripts/check-stack.sh` | `TestTopicRetentionOverridesReapply` observed old-retention RED then GREEN. Exact fresh real regression passed at **64.612s**; all five `test_stack_config.py` tests passed. Normal stack check accepted effective configured policies; partitions and named volumes survived. |

### D-28 supersession and scope

The original D-28 `getEvent` ApiKeyAuth-or-BearerAuth alternative is superseded for the live Phase 2 operation by 02-16's API-key-only contract, consistent with D-03/D-13 and the existing Phase 4 JWT/RBAC boundary. The general bearer scheme/future contracts, error/type work and literal-colon decision remain intact. This repairs the published alternative; it does not add a new authentication implementation or advance deferred dashboard work. The source decision/mapping is recorded in `02-GAP-COVERAGE.md` and `02-16-SUMMARY.md`.

### Historical report preservation

Before and after this acceptance gate, original report bytes were unchanged:

- `02-VERIFICATION.md`: SHA-256 `f8d0b5d37a52deaf8fd1b6041b8b8e6ca2c8120f3e6c24fe9791b88ffc496144`, historical status `gaps_found`.
- `02-REVIEW.md`: SHA-256 `bf47694073b07e184251e5c68af7ef90ef3c633f88f9af732b7f26b868931d49`, historical status `issues_found`.

Root will create byte-identical immutable copies at [02-VERIFICATION-2026-10-16-PRE-GAPS.md](02-VERIFICATION-2026-10-16-PRE-GAPS.md) and [02-REVIEW-2026-10-16-PRE-GAPS.md](02-REVIEW-2026-10-16-PRE-GAPS.md) after this executor finishes and **before** canonical reports are replaced by independent assessments. At this implementer handoff, those archival writes and independent verdicts are root-owned pending work. All original 02-00..13 plans/SUMMARYs and 41 historical passed validation rows remain preserved.

### Pending independent and external verdicts

Local repair acceptance: **passed**. Independent goal re-verification, security assessment and code review: **pending**, orchestrator-owned. Hosted GitHub Actions: **pending**, with no push authorized/performed. Overall phase status must remain pending independent assessment; neither this implementer evidence nor 22 SUMMARY files establishes an independent verdict. User-owned `.planning/config.json`, `.serena/`, root environment, local keys and persistent data were preserved.

## Independent review follow-up — 2026-10-17

The fresh reviewer found a residual directory-preparation defect in the seed wrapper and a stale README liveness path. The original generator manifest repair remains valid; the separate wrapper had still followed `.local` or `keys` symlinks before chmod.

- Behavioral RED commit `537e073`: both symlink-directory cases failed because preparation did not reject the path. Fixtures were isolated temporary directories; no real external directory was changed.
- The repair opens the root and each managed directory using `O_DIRECTORY|O_NOFOLLOW`, creates directories relative to opened descriptors, and validates the entire chain before descriptor-relative permission repair. External symlink targets are refused without mutation; repeated real-directory preparation retains the same inode and private modes.
- The complete script suite passed with **27 tests** after repair. The README now uses `/healthz`; a bounded request to the running listener returned `{"status":"live"}` and HTTP 200.
- No Go/service source changed in this follow-up, so the already-passing complete integration gate was not repeated. Two additional bounded live seed runs check the changed credential-preparation path; their outcomes are recorded in the follow-up closeout below.

Follow-up closeout: repair commit `428604a`; both consecutive `timeout 10m make seed` invocations exited 0 and each reported sent=accepted=visible=1000, api_spotchecks=3, tenant_isolation=1 and retries=0. No reset or volume removal occurred. Logs: `/tmp/sentinel-seed-followup-1.log` and `/tmp/sentinel-seed-followup-2.log`. The reviewer will reassess both findings before issuing its new canonical verdict. Hosted CI remains pending.
