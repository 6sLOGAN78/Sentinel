---
phase: 5
slug: observability-resilience-scale
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-10-30
revised: 2026-10-30 (planner revision 1, aligned with 05-01..05-21 plans)
---

# Phase 5 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: 05-RESEARCH.md § Validation Architecture, reconciled with the plan files.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + testify + testcontainers-go; Vitest (web); pytest (generator/bench); unittest (scripts); promtool (pinned Prometheus image) |
| **Config file** | `backend/.golangci.yml`, `web/vite.config.*`, `generator/pyproject.toml` |
| **Quick run command** | `cd backend && go test -race ./internal/telemetry/... ./internal/retry/... ./internal/kafkax/... ./internal/opshttp/...` |
| **Resource gate (real containers)** | `bash scripts/check-resources.sh`: read-only. Requires ≥ 10 GiB free on the repo and Docker data filesystems and MemAvailable ≥ 3 GiB, else refuses. Never prunes. Created in 05-07 |
| **Full suite command** | `make test && make lint`, then (behind the resource gate) `make test-integ`, then `bash scripts/check-chaos.sh` (`make test-chaos` from 05-17) |
| **Estimated runtime** | Per-task verify < 120 s. `make test` ~15 s. `make test-integ` ~32 min. check-chaos ≤ sum of family ceilings (~78 min max). Real `make bench` ~45 min |

---

## Sampling Rate

- **After every task commit:** that task's `<automated>` verify. Each is a unit, static or list-level check that finishes in under 120 s.
- **After every plan:** the plan-level gate in its `<verification>` section. Real-container gates always run `scripts/check-resources.sh` first.
- **After every plan wave:** `make test && make lint`.
- **Before `/gsd:verify-work`:** full suite, `check-chaos.sh` with all 9 families, and one real (not simulated) `make bench` with a reconciled report (05-20).
- **Max feedback latency:** 120 seconds for per-task checks.

---

## Per-Task Verification Map

| Requirement | Behavior | Test Type | Automated Command | Plan | Status |
|-------------|----------|-----------|-------------------|------|--------|
| OBS-02 | ingest metrics; labels ⊆ ADR 0004 allowlist; public router has no /metrics (404) | unit | `go test ./internal/telemetry/... ./internal/ingest/... ./internal/opshttp/...` | 05-01 | ⬜ |
| OBS-02 | lag = end − processed_next; series deleted on revoke; CH write + DLQ metrics | unit | `go test ./internal/kafkax/... ./internal/telemetry/...` | 05-05 | ⬜ |
| OBS-02 | detector metrics, processed-position lag, capacity, pin margin, rename | unit + integration gate | `go test ./internal/detector/... ./internal/detection/...`; gate `^TestDetectorCapacity` | 05-09 | ⬜ |
| OBS-02 | sentinel_sse_clients on query-api | unit + phase4 gate | `go test ./internal/alertstream/... ./internal/telemetry/...` | 05-14 | ⬜ |
| OBS-02 | /metrics served per service | unit (each binary's ops handler test) + live scrape | per-plan unit tests; Prometheus `up` per job during 05-20 | 05-01/05/09/14, 05-20 | ⬜ |
| OBS-03 | JSON logs: service/timestamp/level/request_id/trace_id; no API key/JWT/body | unit | `go test ./internal/logger/... ./internal/middleware/...` | 05-06, 05-10 | ⬜ |
| OBS-04 | traceparent ingest → Kafka headers; ratio 0 → no export; public endpoint | unit + integration gate | `go test ./internal/telemetry/... ./internal/kafkax/... ./internal/middleware/...`; gate `^TestTracingEdgePropagation$` | 05-10 | ⬜ |
| OBS-04 | detect/store/store_alert spans end to end | unit + integration gate | `go test ./internal/detector/... ./internal/eventwriter/... ./internal/alertwriter/...`; gate `^TestTracingPipelinePropagation$` | 05-12 | ⬜ |
| OBS-05 | alert rules valid + unit-tested; dashboards reference only known metrics; doc drift | CI static | `bash scripts/check-observability.sh`; `python3 -B -m unittest discover -s scripts -p test_observability_config.py` | 05-13, 05-19 | ⬜ |
| REL-01 | jitter within [0, ceil]; bounded attempts; terminal error surfaces | unit | `go test ./internal/retry/... ./internal/eventwriter/... ./internal/alertwriter/... ./internal/detector/...` | 05-02, 05-14 | ⬜ |
| REL-02 | SIGTERM mid-batch → exit 0, commits ≤ completed offsets | chaos | family `chaos-shutdown`: `^TestChaosGracefulShutdown(EventWriter\|Detector)$` | 05-07 | ⬜ |
| REL-03 | broker restart, CH outages, writer restart (incl. TestWriterCrashRestart), DLQ, SSE reconnect, rate-limit | chaos | `bash scripts/check-chaos.sh` (7 families from 05-07) | 05-07 | ⬜ |
| REL-03 | slow-consumer lag rise/recovery, lag sum after rebalance | chaos | family `slow-consumer`: `^TestChaos(SlowConsumerLag\|LagAfterRebalance)$` | 05-11 | ⬜ |
| REL-04 | overload: only 202/429/503; buffers ≤ cap; heap bounded | chaos + bench | family `overload`: `^TestChaosOverloadBounded$`; `make bench SCENARIO=overload` | 05-11, 05-18, 05-20 | ⬜ |
| BENCH-01..04 | schedule/preflight; open-loop sender; reconciliation refuses on mismatch; stack safety; scenarios | unit | `cd generator && uv run --locked pytest tests/test_bench_*.py`; `uv run python ../bench/report.py --selftest` | 05-03, 05-04, 05-08, 05-17, 05-18 | ⬜ |
| BENCH-02/04 | real ≥1M run with detector kill and measured recovery | manual/long | `make bench SCENARIO=sustained-restart RUN=<id>` | 05-20 | ⬜ |
| UI-05 | lag-only endpoint (analyst/admin; viewer audited 403); System endpoint admin-only; no fake zeros | unit + OpenAPI gate | `go test ./internal/query/... ./internal/auth/...`; gate `^TestOpenAPI` | 05-15, 05-21 | ⬜ |
| UI-05 | System page states, admin gating, Overview lag numbers for analyst/admin, analyst-role notice for viewers | component | `cd web && npm test -- System App Overview` | 05-16 | ⬜ |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

Dropped from the research draft: a single `TestMetricsEndpoints` integration test. No plan builds all five binaries for one test. /metrics is proven per binary by unit tests in 05-01/05-05/05-09/05-14 and live by Prometheus `up` per job in 05-20.

---

## Wave 0 Requirements (test files created by the plans' RED tasks)

- [ ] `backend/internal/telemetry/metrics_test.go`, `consumer_test.go`, `detector_test.go`, `tracing_test.go`, `sse_test.go` (05-01, 05-05, 05-09, 05-10, 05-14)
- [ ] `backend/internal/retry/retry_test.go` (05-02)
- [ ] `backend/internal/kafkax/lag_test.go`, `headers_test.go` (05-05, 05-10)
- [ ] `scripts/check-resources.sh` + `scripts/test_check_resources.py` (05-07)
- [ ] `backend/test/chaos_broker_test.go`, `chaos_clickhouse_test.go`, `chaos_shutdown_test.go` + `scripts/check-chaos.sh` (05-07); `chaos_slow_consumer_test.go`, `chaos_overload_test.go` (05-11)
- [ ] `backend/test/tracing_edge_test.go` (05-10), `tracing_pipeline_test.go` (05-12)
- [ ] `internal/testing` FaultContainer Stop/Start (05-07)
- [ ] `generator/tests/test_bench_report.py` (05-03); `test_bench_model.py`, `test_bench_schedule.py`, `test_bench_preflight.py` (05-04); `test_bench_sender.py`, `test_bench_sse.py`, `test_bench_rawlog.py` (05-08); `test_bench_stack.py` (05-17); `test_bench_collect.py`, `test_bench_scenarios.py` (05-18)
- [ ] `scripts/test_observability_config.py` + promtool `alerts_test.yml` (05-13)
- [ ] `backend/internal/query/lag_test.go` (05-15), `system_test.go` (05-21)
- [ ] `web/src/pages/System.test.tsx` (05-16)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| ≥1M-event multi-replica run with detector kill | BENCH-02, BENCH-04 | ~45 min; needs ≥ 15 GiB free disk and an isolated `sentinel-bench-<run>` stack | 05-20 Task 2 |
| Grafana dashboards, Jaeger trace and System page with live data | OBS-05, OBS-04, UI-05 | visual and keyboard check | 05-20 Task 3 |

---

## Validation Sign-Off

- [ ] All tasks have an `<automated>` verify under 120 s; long suites are plan-level gates
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Real-container gates run `scripts/check-resources.sh` first
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
