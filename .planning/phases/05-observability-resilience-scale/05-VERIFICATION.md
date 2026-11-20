---
phase: 05-observability-resilience-scale
verified: 2026-11-20T12:58:00Z
status: human_needed
score: 5/6 must-haves verified (roadmap SC1-SC5 VERIFIED; SC6 "Definition of Done: CI green" UNCERTAIN, hosted run 44738994392 at 210330a in progress, static jobs green, container jobs pending)
overrides_applied: 0
verified_at_head: 210330a535d363aa24977eacdf22131503375202
requirements_score: 13/13 SATISFIED in code and recorded evidence (hosted CI confirmation pending)
gaps: []
warnings:
  - id: W1
    item: "ADR 0011 capacity-preflight sentence misstates its denominator"
    detail: "docs/decisions/0011-benchmark-method.md:17 says 'a predicted ledger of 72302 entries, 72.3% of the 80000 admission limit'. The JSON preflight.recheck has ledger_pct 72.3 = 72302 / ledger_cap 100000. 72302 of the 80000 limit is 90.4%. The number traces to the results file but the sentence is wrong. Fix: '72.3% of the 100000 cap (admission limit 80000 = 80%)'."
  - id: W2
    item: "ADR 0011 cites per-alert diagnosis values that are not in any committed results file"
    detail: "218 s, 261 s, 38.424 s and the kill/takeover/rejoin timestamps (ADR 0011 lines 45-48) come from the raw files, which were kept private and then purged. They are labelled as diagnosis, not as benchmark results, and benchmarks/README.md does not quote them. They cannot be reproduced from committed artifacts. Closing 05-20 follow-up 1 (a unique-alert first-detection delay and an SSE max in report.py) would put them in the report."
  - id: W3
    item: "Goal is not in MVP user-story form"
    detail: "ROADMAP marks Phase 5 'Mode: mvp', but the goal fails user-story.validate. As in 04-VERIFICATION, a user-flow table was derived from the operator/tester personas and the standard goal-backward method was applied."
human_verification:
  - test: "Confirm hosted CI run chocopie247/Sentinel 44738994392 (headSha 210330a) finishes with every job green"
    expected: "All 19 jobs succeed, including integration (make test-integ runs the chaos, tracing-pipeline and DLQ tests under ./test/... with -tags=integration), phase3, phase4 x8, phase4-browser and compose-smoke. Watch TestDetectorCooldownOwners (known hosted flake watch item)."
    why_human: "CLAUDE.md DoD requires CI green. At verification time hygiene, secrets, go, python, web and schema were green and the 13 container jobs were still running. The last fully green hosted run (43714363178) is at 7b15a68, 109 commits before HEAD, so none of 05-12..05-28 has hosted proof yet."
---

# Phase 5: Observability, Resilience & Scale Verification Report

**Phase Goal:** Operators can see pipeline health, and failure and scale behavior is proven by tests and a reproducible benchmark report rather than claimed
**Verified:** 2026-11-20T12:58:00Z at HEAD 210330a
**Status:** human_needed (no failed truth; hosted CI for the Phase 5 changes not yet complete)
**Re-verification:** No. This is the initial verification. No earlier 05-VERIFICATION.md existed.

Method: every claim below was checked against code, tests or committed result files. SUMMARY text was used only to find evidence. No docker stack was started and no chaos, integration or benchmark run was repeated. Evidence for those comes from the recorded runs, as stated per row. The `sentinel` stack was not touched.

## User Flow Coverage (MVP, derived)

| Step | Expected | Evidence in codebase | Status |
|---|---|---|---|
| Operator watches pipeline health in Grafana | Dashboards as code for ingest rate, error rate, lag, detection delay, write latency, DLQ | `observability/grafana/dashboards/sentinel-pipeline.json` has 17 panels, including ingest events/s, HTTP error ratio, consumer lag by group, detection delay p50/p95/p99, ClickHouse write p95 and errors, and DLQ by reason. Provisioned by `observability/grafana/provisioning/*`. `scripts/check-observability.sh` PASS | VERIFIED |
| Operator (admin) opens /system | Health, throughput, lag and errors from the server | `backend/internal/query/system.go` uses a fixed PromQL allowlist, readiness probes, per-field availability and the admin + operator allowlist (ADR 0014). `web/src/pages/System.tsx` uses `useQuery` with a 15 s refetch and a Grafana link. `App.tsx:109` shows the route to admins only. The user approved it live in 05-20 Task 3 ("Approved") | VERIFIED |
| Operator follows a trace | ingest → publish → detect → store / store_alert through Kafka headers | `telemetry/tracing.go:84` uses ParentBased(TraceIDRatioBased(SampleRatio)). Workers are wired in `cmd/{ingest,detector,event-writer,alert-writer}/main.go`. `backend/test/tracing_pipeline_test.go` `TestTracingPipelinePropagation` asserts the parent chain | VERIFIED |
| Tester runs `make bench` | ≥1M events, multi-replica, reconciled report or refusal | `Makefile:119` bench target, `bench/report.py` `reconcile()` (`SUSTAINED_FLOOR_EVENTS = 1_000_000`), committed `benchmarks/results/2026-11-20-*.{md,json}` with provenance `measured` | VERIFIED |

## Goal Achievement

### Observable Truths

| # | Truth (roadmap SC) | Status | Evidence |
|---|---|---|---|
| 1 | Grafana dashboards as code (ingest rate, error rate, position-based lag, detection delay, write latency, DLQ). System page shows pipeline health, throughput, lag and errors. No high-cardinality labels. JSON logs carry request/trace IDs and no secrets | VERIFIED | Dashboard panels are listed above. Lag is position-based: `internal/kafkax/lag.go:186-206` sets `max(0, end.Offset - next)` from `ListEndOffsets`. A `sentinel_kafka_topic_partitions{group,topic}` coverage denominator (`telemetry/consumer.go:44`) feeds `partial_coverage` in the lag and System APIs. Every metric label set in `internal/telemetry/*.go` is bounded: result, status, group/topic/partition, rule_id/severity, reason. There is no tenant, IP, user or event ID label. Logs default to JSON (`config/observability.go:16,181`), console is opt-in, and tests cover this: `TestRequestLogJSONContract`, `TestRequestLogTraceID`, `TestNoKeyInLogs`, `TestRequestLoggerNoQuery`, `TestRedactPgxTraceArgs` and `TestBuildDLQRecordRedactsOversizeValueAndBoundsHeaderFlood` |
| 2 | One trace spans ingest, publish, detect and store through Kafka headers, with configurable sampling | VERIFIED | `SENTINEL_TRACING.SAMPLE_RATIO` is validated to [0,1] (`config/observability.go:125-132`). W3C TraceContext is injected on every record (`TestPublishBatchSpanCarriesSameTraceparentOnEveryRecord`). An edge client cannot force sampling (`TestEdgeClientTraceparentCannotForceSampling`). The end-to-end chain on real Kafka/CH/PG is checked in `test/tracing_pipeline_test.go:201-210` (publish→detect→store_alert parents, store links). The live bench Jaeger showed ingest, publish, detect and event-writer store. store_alert was absent there, which fits sampling at ratio 0.01 with 120 alerts |
| 3 | Failure suites pass for writer restart mid-batch, unreachable CH, broker restart, slow consumers, malformed→DLQ, SSE reconnect and rate-limit overload. Retry with backoff and jitter, graceful shutdown, commit only completed offsets | VERIFIED (recorded evidence) | `scripts/check-chaos.sh:25-33` maps all 9 families to real tests: chaos-broker, chaos-clickhouse, chaos-shutdown, writer-restart, dlq, rate-limit, sse-reconnect (TestPhase4Stream/Delivery), slow-consumer and overload. 05-11 recorded the full selector at EXIT 0, 9/9 PASS. Affected subsets were re-run after the gap plans (05-24, 05-25, 05-26: 40/40 integration). `internal/retry` provides a bounded, capped exponential policy with full jitter and validation, used by the writers and the detector. Graceful SIGTERM has its own tests (`TestChaosGracefulShutdown{EventWriter,Detector}`). Not re-run here (no containers). CI `make test-integ` runs them all, and hosted confirmation is pending (SC6) |
| 4 | Under excess load, memory and queues stay bounded and clients get 429/503 | VERIFIED | Bench overload: status {202: 1281, 429: 2319}, producer buffer 0/50000, OOM none, restart delta 0 for every service, loss 0 (`2026-11-20-overload.json` `overload.*`). 503 paths are covered deterministically by `TestChaosOverloadBounded` (05-11 run: 503 x 24145, buffer 2000/2000, HeapInuse 63.1 MiB, 5/5 PASS) and `TestChaosKafkaBrokerRestart`. The bench report states that 503 was not exercised in that run (`shed_paths.503 = false`) |
| 5 | `make bench` runs a multi-replica run with ≥1,000,000 events and writes `benchmarks/results/<date>-<scenario>.md` with every required section. Counts reconcile, refusal on mismatch, detector restart under load with a measured recovery time | VERIFIED | Run 20261120a at git 8424348 used replicas ingest 2, detector 3, event-writer 2, alert-writer 1, query-api 2. Generated = accepted = Kafka = CH unique = CH physical = 1,000,000; loss 0, duplicates 0, digest match. Rules 30/30/30 x4, benign 0. Events/alerts API non-2xx 0. Recovery is coverage-gated: coverage_checked and coverage_dropped true, 6 expected, min 4 reported, coverage lost 45.107 s, recovered, 53.759 s. The report has hardware, configuration, throughput, p50/p95/p99, API p95, resources, lag, loss/duplicates and limitations. `reconcile()` (`bench/report.py:1090-1110`) checks counts, rules, histogram, recovery coverage, API non-2xx, replicas, state and overload. Selftest ok; 57 tests in test_bench_report.py, 15 of them named refusal tests |
| 6 | DoD: tests pass incl. invalid and failure cases, CI green, no stubs or invented numbers | UNCERTAIN (human) | Local: `make test` exit 0 (Go -race 40 packages ok, web 217/217, generator 414 passed). Go lint: `./...` 0 issues, `--build-tags=integration` 0 issues. check-observability PASS (32 tests + promtool), check-adrs OK. No TBD/FIXME/XXX/TODO/HACK in the 219 non-planning files changed since 64711fb. Hosted: run 44738994392 at HEAD is in progress; 6 static jobs are green and 13 container jobs are pending. See Human Verification |

**Score:** 5/6 truths verified, 1 uncertain (hosted CI pending). 0 failed.

### Benchmark Number Traceability

| Document | Numeric tokens | Untraced to `benchmarks/results/2026-11-20-*` | Verdict |
|---|---|---|---|
| `benchmarks/README.md` | 70 | `0011` (ADR id) and `20261119` (run id) only | Every number traces |
| `observability/prometheus/alerts.yml`, `docs/operations.md` thresholds | 133098, 73.943224, 1800, 134793, 1822, 1% | none: ceil(73.943224 x 1800) = 133098 (recomputed); 134793 / 73.943224 = 1822.9; R and 1% are labelled user policy | Every number traces |
| `docs/decisions/0011-benchmark-method.md` | 75 | 218, 261, 38.424, kill/rejoin timestamps (raw-only diagnosis); `72.3% of the 80000` wording | W1, W2 |

The superseded 2026-11-19 results are unedited and are listed with no numbers quoted.

### Required Artifacts

| Artifact | Status | Details |
|---|---|---|
| `backend/internal/telemetry/{ingest,consumer,detector,sse,tracing}.go` | VERIFIED | Registered on an injected registry; wired in cmd/*/main.go |
| `backend/internal/kafkax/lag.go` | VERIFIED | Broker end offsets minus next durable offset; per-partition series deleted on revoke; partitions gauge |
| `backend/internal/retry` | VERIFIED | Full-jitter bounded policy with Validate; injected source for tests |
| `backend/internal/query/{system,lag}.go`, `web/src/pages/{System,Overview}.tsx` | VERIFIED | Role and operator gating (viewer 403, analyst lag, admin System), partial_coverage, Grafana link |
| `observability/**` (dashboard, alerts.yml + alerts_test.yml, prometheus.yml, jaeger) | VERIFIED | promtool rules and tests pass in check-observability.sh |
| `backend/test/chaos_*_test.go`, `scripts/check-chaos.sh`, `make test-chaos` | VERIFIED | 9 families mapped to real tests |
| `bench/{report,run,stack,collect,scenarios}.py`, `make bench/bench-up/bench-down/bench-purge` | VERIFIED | Report refuses on any mismatch; results reproduced byte-identically from the raw files (05-20 record; raws not committed) |
| `benchmarks/results/2026-11-20-*`, `benchmarks/README.md`, ADRs 0010-0014, `docs/operations.md`, `docs/architecture.md` | VERIFIED (W1/W2 on ADR 0011) | check-adrs OK; doc-drift tests (test_observability_config, test_phase4_guards 26/26) pass |

### Key Link Verification

| From | To | Via | Status |
|---|---|---|---|
| LagTracker | Prometheus | `SetLag`/`SetTopicPartitions` → `sentinel_kafka_consumer_lag` / `_topic_partitions` | WIRED |
| query-api System/lag | Prometheus | fixed allowlist `systemQueries()` → `h.metrics.Query` | WIRED |
| System.tsx | /api/v1/system | `useQuery` + render of consumer_lag, services, ingest, errors | WIRED |
| ingest producer | detector / event-writer / alert-writer | `traceparent` header → `WithTracing(tr.Provider(), tr.Propagator())` in all 4 binaries | WIRED |
| bench run | report.py | raw.json → `reconcile()` → .md/.json or FAILED.json | WIRED |
| alerts.yml thresholds | results JSON | annotations cite `threshold_inputs.*` | WIRED |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|---|---|---|---|---|
| System page | services, ingest, lag, errors | Prometheus queries plus /readyz probes | Yes (live 05-20: ingest 2/2, detector 3/3, lag `available` 0) | FLOWING |
| Overview lag panel | consumer_lag | /api/v1/metrics/lag | Yes; viewers get a notice, operator-only otherwise | FLOWING |
| Benchmark report | all sections | raw measured file (provenance `measured`) | Yes; fixture provenance refused for results | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| Unit suites | `make test` | exit 0; Go 40 ok (cached, same inputs), web 217/217, generator 414 | PASS |
| Observability config, rules and drift | `bash scripts/check-observability.sh` | PASS, 32 tests, promtool SUCCESS | PASS |
| ADR format | `bash scripts/check-adrs.sh` | OK 0010-0014 | PASS |
| Script guards | `python3 -m unittest` for each scripts/test_*.py | all OK except test_seed_scenarios under system Python 3.10 (ImportError `datetime.UTC`); OK under `generator/.venv` (3.13), which is how CI runs it (ci.yml:101) | PASS (see follow-ups) |
| report.py | `python3 bench/report.py --selftest` | selftest ok | PASS |
| Go lint gates | golangci-lint `./...` and `--build-tags=integration ./...` | 0 issues each | PASS |
| phase4-tag lint | `--build-tags=phase4 ./testphase4/...` | 22 issues, all in tenant_matrix_test.go (lll 17, perfsprint 2, musttag 1, nolintlint 1, govet 1). None on the lines 05-21a added (88-115, 195-198, 348-350). Not a CI gate | INFO |
| Results consistency | python cross-check of the JSON vs the .md | reconciliation, rules, api, recovery and overload fields match the published tables | PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` exist and no phase plan declares a probe. Step 7c: no probes to run.

### Requirements Coverage

| Req | Description (short) | Status | Evidence |
|---|---|---|---|
| OBS-02 | Spec metrics on /metrics, bounded labels, position-based lag | SATISFIED | telemetry/*.go; lag.go; ADR 0004 label policy; sentinel_sse_clients in telemetry/sse.go |
| OBS-03 | JSON logs with service/timestamp/level/request_id/trace_id, scrubbed | SATISFIED | logger tests; middleware log contract tests; TestNoKeyInLogs |
| OBS-04 | Spans across ingest/publish/detect/store via Kafka headers; configurable sampling | SATISFIED | tracing.go; TestTracingPipelinePropagation |
| OBS-05 | Dashboards as code plus alert examples (lag, ingest error, write errors, restart loops) | SATISFIED | dashboard JSON; alerts.yml has 6 rules with promtool tests; thresholds trace to measured inputs or labelled policy |
| REL-01 | Backoff + jitter + bounded retries, terminal handling | SATISFIED | internal/retry; writer/detector use; TestChaosClickHouse{Short,Long}Outage* |
| REL-02 | Graceful shutdown, commit only completed offsets | SATISFIED | TestChaosGracefulShutdown*; TestWriterCommitAfterInsert / InsertFailureNoCommit (writer-restart family) |
| REL-03 | Failure suites (7 named) | SATISFIED (recorded) | check-chaos.sh 9 families, 05-11 9/9 PASS; hosted confirmation pending |
| REL-04 | Bounded memory/queues, 429/503 | SATISFIED | overload bench + TestChaosOverloadBounded |
| BENCH-01 | `make bench` multi-replica open-loop benign + malicious | SATISFIED | Makefile:119-132; bench/run.py; ADR 0011 |
| BENCH-02 | ≥1M run reconciles sent/accepted/rejected/persisted and rule hits | SATISFIED | 2026-11-20-sustained-restart.json reconciliation + rules |
| BENCH-03 | report.py writes the full report and refuses on mismatch | SATISFIED | report.py reconcile/FAILED.json; selftest; refusal tests |
| BENCH-04 | Detector restart under load recovers without loss, time measured | SATISFIED | recovery block: coverage-gated 53.759 s, loss 0, restarted detector rejoined (restarted_ns set) |
| UI-05 | /system page with pipeline health, throughput, lag, errors, Grafana link | SATISFIED | System.tsx + system.go; live approval 05-20 Task 3 |

No orphaned requirements: REQUIREMENTS.md maps exactly these 13 IDs to Phase 5, and every one is claimed by at least one plan.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|
| docs/decisions/0011-benchmark-method.md | 17 | "72.3% of the 80000 admission limit": wrong denominator (it is 72.3% of the 100000 cap) | WARNING (W1) | Misleading method text; number itself traces |
| docs/decisions/0011-benchmark-method.md | 45-48 | Values from uncommitted raw (218 s, 261 s, 38.424 s, timestamps) | WARNING (W2) | Not reproducible from committed artifacts; labelled diagnosis, not results |
| (none) | - | TBD/FIXME/XXX/TODO/HACK/placeholder | - | 219 phase-changed files clean |

### Known Follow-ups: Classification

| Item | Classification | Reasoning |
|---|---|---|
| report.py detection-delay row should say "per publication, incl. crash replays" and add a unique-alert first-detection delay | Acceptable follow-up (WARNING) | The table reports n=125 against 120 alerts and labels it "histogram". README and ADR 0011 explain that the p99 of 281.6 s comes from replays. Nothing is misreported, but the label alone can mislead. Fixing it also closes W2 |
| DetectionDecision "already existed" replay flag | Acceptable follow-up | Optional observability refinement; correctness (dedupe, loss 0, 30/30/30) is proven without it |
| Kafka log-start-offset metric | Acceptable follow-up | The consequence is that `SentinelPinRetentionLow` fires for about R s on a fresh deployment. This is documented in operations.md (Pin retention hazard) and the alert annotation. It errs toward a false positive, not a missed alert |
| Sustained steady-window pin margin includes ~30 s of fresh-topic zeros | Acceptable follow-up | The threshold was validated against the genuine overload steady minimum (134793). The sustained 0 is labelled a fresh-topic artifact in the report's input table and the README. No published claim depends on it |
| Runbook for partitions wedged by pre-05-26 detector code | Acceptable follow-up (Phase 6 docs) | Affects only stores that ran the old code. ADR 0012 recovery tooling exists. Phase 6 SC4 requires operations.md to match the implementation, so carry it there |
| 05-14 SSE admission returns 429 rather than the plan's 503 | Acceptable deviation | The code already returned 429 `NewTooManyRequestsError` (Phase 4 behavior, tested in alertstream). REL-04 accepts "429/503". A per-endpoint concurrency cap is reasonably a 429. The plan text was wrong, not the code |
| 05-21a live two-token check not run | Closed by later evidence | The 05-20 Task 3 live check exercised the same rule on the bench stack with two tenants. Admin of operator tenant `bench-20261120a` got System 200 and lag available. Admin of non-operator `bench-20261120a-ovl` got System 403 "limited to operator administrators" and lag `operator_only`. In-process two-tenant tests also exist |
| Python 3.10 `datetime.UTC` in scripts/test_seed_scenarios.py | Acceptable (not a defect) | The project pins Python 3.13 (`generator/pyproject.toml` requires-python >=3.13). CI runs this test with `generator/.venv/bin/python` (ci.yml:101). Verified OK with the venv here; it fails only under the host's system 3.10 |
| 22 phase4-tag lint findings in backend/testphase4/tenant_matrix_test.go | Acceptable follow-up | Pre-existing since Phase 4 (recorded 05-07, count unchanged). None are on lines 05-21a added. The CI lint gates (`./...`, integration tag) report 0. Worth a cleanup quick task |
| TestDetectorCooldownOwners hosted flake watch | Watch item, folded into the human CI check | No failure observed locally. The in-progress hosted run is the place to see it. If it flakes, treat it as a gap, because DoD requires green CI |
| Hosted CI for Phase 5 not yet complete | Verification limitation → human_needed | DoD requires CI green. Run 44738994392 at HEAD exists (static jobs green, container jobs running). It has not failed, so this is not a gap. It is unconfirmed |

### Human Verification Required

#### 1. Hosted CI green at HEAD

**Test:** `gh run watch 44738994392 -R chocopie247/Sentinel` (or view it once it has finished).
**Expected:** All 19 jobs succeed. In particular, `integration` (make test-integ, which includes the TestChaos*, TestTracingPipelinePropagation, DLQ and writer-restart tests), all phase3/phase4 groups, phase4-browser and compose-smoke.
**Why human:** CLAUDE.md DoD requires CI green. The run was still in progress at verification time, and the last fully green hosted run (7b15a68) predates 109 Phase 5 commits.

The live Grafana, Jaeger and System check is already recorded as human-approved (05-20 Task 3, user reply "Approved"), so it is not repeated here.

### Gaps Summary

No must-have truth failed. Every roadmap success criterion has code, tests and committed measured evidence:

- The 1M-event run reconciles exactly.
- Recovery is coverage-gated and measured.
- Overload stays bounded.
- Every README and threshold number traces to the 2026-11-20 results files.

Two documentation warnings in ADR 0011 (W1 wrong denominator, W2 raw-only diagnosis values) should be fixed in a quick task but do not invalidate any published result. The remaining condition for `passed` is the in-progress hosted CI run at HEAD. If it finishes green, Phase 5 meets its Definition of Done. If any job fails, re-run verification with that failure as a gap.

---

_Verified: 2026-11-20T12:58:00Z_
_Verifier: Claude (gsd-verifier)_
