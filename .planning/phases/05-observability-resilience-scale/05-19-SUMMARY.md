---
phase: 05-observability-resilience-scale
plan: "19"
subsystem: docs
tags: [docs, adr, observability, ci, drift-guard]
requirements: [OBS-02, OBS-03, OBS-04, OBS-05, REL-01, REL-02, REL-03]
requires:
  - phase: 05-01..05-15, 05-17, 05-18, 05-21, 05-21a
    provides: the implemented metrics, lag, logs, tracing, overlay, System/lag APIs, chaos runner, bench tooling, operator allowlist
provides:
  - docs/decisions/0010-observability-and-tracing.md
  - docs/operations.md sections System page and consumer lag, Observability, Metrics, Consumer lag (position-based), Logs, Tracing, Alert examples, Pin retention hazard, Graceful shutdown, Failure suites, Benchmark tooling
  - docs/architecture.md Telemetry flow
  - DocsDriftTests in scripts/test_observability_config.py (run in CI through check-observability.sh)
affects: [05-20 (fill the two placeholder thresholds and bench docs), 05-16 (UI copy can link operations.md#system-page-and-consumer-lag)]
tech-stack:
  added: []
  patterns: [doc metric table checked for set equality against names parsed from Go source, overlay env keys and Makefile targets used as doc oracles]
key-files:
  created:
    - docs/decisions/0010-observability-and-tracing.md
  modified:
    - docs/decisions/README.md
    - docs/operations.md
    - docs/architecture.md
    - scripts/test_observability_config.py
decisions:
  - "ci.yml unchanged: 05-13 already runs bash scripts/check-observability.sh in the python job, and that script runs promtool check/test plus test_observability_config.py. A separate unittest step would run the same 32 tests twice. The guard asserts the transitive chain (ci.yml -> script -> unittest + promtool) instead"
  - "The metric table is checked for set equality, not inclusion, so a doc row for a metric that does not exist also fails"
  - "System/lag access is documented per ADR 0014 (operator tenants), replacing the plan's 'admin-only' and 'analyst/admin' wording"
  - "ADR 0010 dated 2026-11-19 (written date), not the plan's 2026-10-30"
metrics:
  duration: "about 50 min of work across a usage-limit restart (2026-11-18 to 2026-11-19)"
  completed: 2026-11-19
  tasks: 2
  files: 5
---

# Phase 5 Plan 19: Observability documentation and drift guards Summary

ADR 0010 records the Phase 5 observability decisions. operations.md now explains how to run the overlay on an isolated project, read each registered metric, lag, logs, traces and alerts, and run the chaos and bench tooling. A CI-run unittest fails if the docs drift from the registry, the overlay config keys, the alert names, the ops ports or the Makefile targets. Every name, key, port and target in the docs was taken from source, not from plan text. The docs contain no performance numbers.

## Commits

| Task | Gate | Commit | Files |
|---|---|---|---|
| 1 RED | test | 783c3b3 | scripts/test_observability_config.py |
| 2 GREEN | docs | 11ae863 | ADR 0010, decisions/README.md, operations.md, architecture.md |

RED evidence: 6 of 7 new tests failed for the intended reasons:
- no `## Metrics` section
- two old-name hits (architecture.md:61, operations.md:115)
- no ADR 0010
- 18 overlay keys and 6 alert names missing, and query-api not paired with 9105
- no Telemetry flow section

The seventh, `test_ci_runs_check_observability_which_runs_these_tests`, passed in RED. That is expected because 05-13 already wired the step (see Deviations). One test bug was fixed before the RED commit: the make-target regex matched the prose "to make readiness green", so it now matches only inline code or lines that start with `make`.

## Drift guards added (DocsDriftTests)

| Test | Oracle |
|---|---|
| metric table == registered names | `sentinel_*` literals in backend/internal/telemetry/*.go (non-test) |
| old unprefixed late metric gone | text files under docs, backend, scripts, observability (excluding scripts/test_*.py) |
| ADR 0010 with 4 MADR headings and index entry | docs/decisions |
| CI runs check-observability.sh (no `|| true`, no continue-on-error), which runs promtool test and this unittest | ci.yml, check-observability.sh |
| every `SENTINEL_OBSERVABILITY.*`/`SENTINEL_AUTH.*` key in the overlay, every alert name and each service/ops-port pair in operations.md | docker-compose.observability.yml, ALERT_NAMES, APP_PORTS |
| every `make <target>` in operations/architecture exists | Makefile |
| architecture has a Telemetry flow section and the prefixed name | architecture.md |

## Doc sections added

- **ADR 0010:** covers the injected registry (labels within ADR 0004, no amendment), position-based lag (detector uses `processed_next`, not the pin) with deletion on revoke, publication counters, and JSON logs (`timestamp`, `trace_id`/`span_id`, `ip` once). Tracing uses ParentBased(TraceIDRatioBased) with a public-endpoint edge, a franz-go header carrier instead of kotel, at most 32 store links, a bounded BSP (2048/512) and no OTel globals. The overlay is loopback-only with bounded storage. System and lag use fixed queries, with access per ADR 0014. It also records the full-jitter retry and the 40 s grace / 30 s lifecycle. Each item has its consequences.
- **operations.md:**
  - **System page and consumer lag:** the 401/403/400/operator-403 order, reasons including `operator_only`, the readiness prober bounds, and a config key table (PROMETHEUS_URL, GRAFANA_URL, SYSTEM.<SVC>.ADDRESS/EXPECTED, OPERATOR_TENANTS).
  - **Observability:** the overlay recipe on an isolated `COMPOSE_PROJECT_NAME`, with an explicit warning against `sentinel` and `sentinel-p4-demo-20261024`. `make dev-up` does not include the overlay, so the recipe adds explicit `-f` flags. It notes the `SENTINEL_GRAFANA_ADMIN_PASSWORD` upgrade issue for an older `.env`, the base host-port conflict, teardown, and that `make reset` leaves the overlay volumes behind.
  - **Metrics:** 19 rows with type, labels, owner and meaning.
  - **Consumer lag (position-based)**, **Logs** (with the console opt-in) and **Tracing** (keys, defaults, semantics).
  - **Alert examples:** the six rules. Two are flagged as placeholders: ingest 5xx at 5% and pin margin 100000.
  - **Pin retention hazard**, **Graceful shutdown**, and **Failure suites** (9 families with ceilings, plus the resource gate at 10 GiB / 3 GiB, raise-only and never prunes).
  - **Benchmark tooling:** commands, refusals, floors (15 GiB / 6 GiB) and ports. No results.
  - A retry and producer paragraph, including ADR 0013.
  - The stale "query 9105 ... detection_late_events_total ... later work" paragraph is replaced.
- **architecture.md:** the late metric is renamed, and a Telemetry flow mermaid diagram and text are added (services -> /metrics -> Prometheus -> Grafana / query-api System and lag; traceparent through Kafka headers -> Jaeger).

The existing "Operator tenants" section (05-21a), with the `SENTINEL_AUTH.OPERATOR_TENANTS` format and the fail-closed upgrade note for an older `.env`, was kept as is and is linked from the new config table.

## Gate table (2026-11-19)

| Gate | Result |
|---|---|
| RED: `! python3 -B -m unittest discover -s scripts -p test_observability_config.py` | exit 0 (suite failed as intended) |
| `python3 -B -m unittest discover -s scripts -p test_observability_config.py` | OK, 32 tests (25 existing + 7 new; includes real overlay renders) |
| `bash scripts/check-adrs.sh` | PASS, 0010 OK and indexed |
| `bash scripts/check-observability.sh` | PASS (promtool check config, check rules, test rules; 32 tests) |
| `bash scripts/check-pins.sh` | PASS |
| `! grep -rnE --exclude="test_*.py" "(^\|[^_])detection_late_events_total" docs backend scripts observability` | no matches |
| `grep -c "\|\| true" .github/workflows/ci.yml` | 0 before, 0 after (ci.yml not modified) |
| `grep -n check-observability.sh .github/workflows/ci.yml` | line 113 (python job, 05-13) |
| ADR 0010 MADR heading count | 4 |
| `grep -n sentinel-p4-demo-20261024 docs/operations.md` | matches (protected-stack warning) |
| actionlint | not installed locally; ci.yml unchanged and parses with PyYAML (11 jobs) |
| `make lint` | exit 0 (golangci, eslint + tsc, ruff, mypy) |
| `make test` | exit 2. Go: 40 packages ok. Web: 214/215; the one failure is `Overview.test.tsx` "never fabricates zeros", from 05-16's RED commit e72c409 plus its uncommitted web changes. This plan touches no web file. Generator not reached by make, so run separately: `uv run --locked pytest` 363 passed. Rerun after 05-16 GREEN 354e727 landed: web `npm test` 215/215 passed, so all three suites are green |

## Deviations from Plan

**1. [Plan text superseded] CI step not added**
- **Issue:** The plan asks for a python-job unittest step plus a check-observability step. 05-13 already added `bash scripts/check-observability.sh` to the python job, and that script runs promtool and `test_observability_config.py`.
- **Fix:** ci.yml was left unchanged, so the tests don't run twice. The new test pins the chain end to end: CI step, no masking, the script running promtool test and this unittest.
- **Files:** none.

**2. [Rule 2] Stronger drift guards than the plan's four**
- **What was added:** set equality for the metric table, so invented metrics also fail. Also checks for overlay config keys, alert names, ops port pairing, make targets and the architecture section. These cover the brief's demand that names, keys, ports and targets come from source.
- **Commit:** 783c3b3.

**3. Access wording per ADR 0014**
- The plan text says "System admin-only" and "lag analyst/admin". The docs say operator tenants: System needs admin of an operator tenant, and lag values go to analysts and admins of operator tenants. Others get `operator_only`, and viewers get 403.

**4. Test bug before RED**
- The make-target regex matched English prose. It was fixed before the RED commit.

## Doc items to fill in after 05-20

- `SentinelIngestErrorRate` threshold (5%) and `SentinelPinRetentionLow` threshold (100000 offsets). Both are marked as placeholders in alerts.yml, operations.md "Alert examples" and the ADR 0010 Consequences. Replace them with values derived from the 05-20 `threshold_inputs` (ingest_5xx_ratio for normal load and overload, pin_retention_margin_min plus the partition rate), and update the alerts_test.yml cases.
- operations.md "Benchmark tooling" says the benchmark has not run and makes no claim. 05-20 should link ADR 0011 and benchmarks/README and the results files there, and check that the floors, ports and refusals still match.
- The ADR 0010 overlay bounds (Jaeger 1000 traces, 512m each) are configured caps. If 05-20 measures Jaeger RSS or Prometheus disk, record the measurements there instead of editing the caps silently.

## Deferred / out of scope

- docs/architecture.md lines 3-4 ("Phase 4 is not started") and the operations.md intro ("Phase 4 ... in progress") are stale status lines from 04-16-owned text. They were not changed because they are outside this plan's scope and need a phase-status decision.
- STATE.md, ROADMAP.md and REQUIREMENTS.md were not touched, as instructed.
- Pre-existing: `scripts/test_seed_scenarios.py` does not import on host Python 3.10 (not run here).

## Known Stubs

None. The two placeholder thresholds are labelled as placeholders in the docs and the rules; they are not presented as measured values.

## Threat Flags

None. These are docs and tests only:
- T-05-19-01: isolated-project recipe and protected-name warning.
- T-05-19-02: only placeholder secret names appear.
- T-05-19-03: check-observability.sh uses tag@sha256.
- T-05-19-04: DocsDriftTests runs in CI.

## TDD Gate Compliance

`test(05-19)` 783c3b3 (RED) comes before the GREEN commit 11ae863. GREEN is a `docs(...)` commit because the implementation is documentation, not a `feat`. There was no refactor commit.

## Self-Check: PASSED

- Files exist: docs/decisions/0010-observability-and-tracing.md, docs/operations.md, docs/architecture.md, scripts/test_observability_config.py, this SUMMARY
- Commits present: 783c3b3, 11ae863 (each ends with the Co-Authored-By line; no deletions)
