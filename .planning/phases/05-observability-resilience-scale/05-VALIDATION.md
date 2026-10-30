---
phase: 5
slug: observability-resilience-scale
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-10-30
---

# Phase 5 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: 05-RESEARCH.md § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + testify + testcontainers-go; Vitest (web); pytest (generator/bench); unittest (scripts); promtool (pinned Prometheus image) |
| **Config file** | `backend/.golangci.yml`, `web/vite.config.*`, `generator/pyproject.toml` |
| **Quick run command** | `cd backend && go test -race ./internal/telemetry/... ./internal/retry/... ./internal/kafkax/... ./internal/opshttp/...` |
| **Full suite command** | `make test && make lint`, then `make test-integ`, then `make test-chaos` |
| **Estimated runtime** | quick ~60 s; `make test` ~15 s; `make test-integ` ~32 min; `make test-chaos` finite ceiling set by its plan; real `make bench` ~40 min |

---

## Sampling Rate

- **After every task commit:** quick run command + affected package tests
- **After every plan wave:** `make test && make lint`
- **Before `/gsd:verify-work`:** full suite, `make test-chaos`, and one real (not simulated) `make bench` with a reconciled report
- **Max feedback latency:** 120 seconds for per-task checks

---

## Per-Task Verification Map

Filled in by the planner and executor; one row per task. Requirement → test map from research:

| Requirement | Behavior | Test Type | Automated Command | File Exists | Status |
|-------------|----------|-----------|-------------------|-------------|--------|
| OBS-02 | spec metrics registered; labels ⊆ ADR 0004 allowlist; lag = end − processed_next; series deleted on revoke | unit | `go test ./internal/telemetry/... ./internal/kafkax/ -run 'TestMetric\|TestLag'` | ❌ W0 | ⬜ pending |
| OBS-02 | `/metrics` served on every service ops port | integration | `go test -tags=integration ./test/ -run TestMetricsEndpoints` | ❌ | ⬜ pending |
| OBS-03 | JSON logs: service/timestamp/level/request_id/trace_id; no API key/JWT/body | unit | `go test ./internal/logger/ ./internal/middleware/ -run TestLog` | partial | ⬜ pending |
| OBS-04 | traceparent ingest → Kafka headers → detector/writers; sampling 0 → no export; ratio honored | unit (in-memory exporter) + integration | `go test ./internal/telemetry/... -run TestTrace` | ❌ | ⬜ pending |
| OBS-05 | alert rules valid + unit-tested; dashboards reference only known metrics | CI static | promtool `test rules`; `python3 -m unittest scripts/test_observability_config.py` | ❌ | ⬜ pending |
| REL-01 | jitter within [0, ceil]; bounded attempts; terminal error surfaces | unit | `go test ./internal/retry/ ./internal/alertwriter/ ./internal/detector/ -run TestRetry` | ❌ | ⬜ pending |
| REL-02 | SIGTERM mid-batch → exit 0, commits ≤ completed offsets | integration | `go test -tags=integration ./test/ -run TestGracefulShutdown` | partial | ⬜ pending |
| REL-03 | broker restart, CH outage, slow-consumer lag rise/fall, DLQ, SSE reconnect, rate-limit overload | chaos | `make test-chaos` | partial | ⬜ pending |
| REL-04 | overload returns only 202/429/503; buffers ≤ cap; RSS bounded | integration + bench scenario | `go test -tags=integration ./test/ -run TestOverloadBounded`; `make bench SCENARIO=overload` | ❌ | ⬜ pending |
| BENCH-01..04 | open-loop schedule; reconciliation refuses on mismatch; report fields present | unit | `cd generator && uv run --locked pytest tests/test_bench_*.py`; `bench/report.py --selftest` | ❌ | ⬜ pending |
| BENCH-02/04 | real ≥1M-event run with detector kill and measured recovery | manual/long | `make bench SCENARIO=sustained-restart` | ❌ | ⬜ pending |
| UI-05 | System page loading/unavailable/forbidden/data states; no fake zeros; role gating | component + handler | `cd web && npm test -- System`; `go test ./internal/query/ -run TestSystem` | ❌ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `backend/internal/telemetry/*_test.go` — registry / label allowlist guard
- [ ] `backend/internal/retry/retry_test.go`
- [ ] `backend/test/chaos_*_test.go` + `make test-chaos` + nonempty selection guard
- [ ] `internal/testing` container helpers: allowlisted stop/start for a dedicated broker
- [ ] `generator/tests/test_bench_schedule.py`, `test_bench_pools.py`, `bench/tests/test_report.py` (mismatch → refuse)
- [ ] `scripts/test_observability_config.py` + promtool rule tests
- [ ] `web/src/pages/System.test.tsx`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| ≥1M-event multi-replica run with detector kill | BENCH-02, BENCH-04 | ~40 min, needs ≥15 GB free disk and an isolated `sentinel-bench-<run>` stack | `make bench SCENARIO=sustained-restart`; confirm report written and reconciliation passes |
| Grafana dashboards render with live data | OBS-05 | visual check | open Grafana on loopback, confirm each panel has data during a bench run |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
