---
phase: 05-observability-resilience-scale
plan: "13"
subsystem: observability
tags: [prometheus, grafana, jaeger, promtool, compose, alerting, dashboards-as-code]
requirements: [OBS-05, OBS-02, OBS-04]
status: complete (static and promtool gates passed; no stack started)
dependency-graph:
  requires: [05-01 approved image digests, 05-05 writer lag/CH/DLQ metrics, 05-09 detector metrics, 05-10/05-12 tracing env keys, 05-11 producer buffered_records semantics]
  provides: [deployments/compose/docker-compose.observability.yml, observability/prometheus (scrape config, 6 alerts, promtool tests), observability/grafana (datasource uid sentinel-prom, Sentinel pipeline dashboard), observability/jaeger/config.yaml, scripts/check-observability.sh, check-pins over every compose file with port ranges]
  affects: [05-14 (must delete sentinel_sse_clients from PENDING_METRICS; query-api /metrics makes its scrape job go up), 05-16 System page (may link Grafana 127.0.0.1:13000), 05-17 bench overlay (reuses this overlay; host ports, !reset, ranges), 05-18/05-20 bench (Jaeger RSS and span-size measurement, threshold tuning), 05-19 docs]
tech-stack:
  added: [prom/prometheus v3.15.1 (image), grafana/grafana 13.2.5 (image), jaegertracing/jaeger 2.22.0 (image)]
  patterns: [metric names parsed from Go telemetry source as the drift oracle, PromQL label extraction for the forbidden-label guard, promtool in the pinned image with --network none, mutation-checked promtool fire/no-fire cases, compose render test with a temp env file]
key-files:
  created: [deployments/compose/docker-compose.observability.yml, observability/prometheus/prometheus.yml, observability/prometheus/alerts.yml, observability/prometheus/alerts_test.yml, observability/grafana/provisioning/datasources/prometheus.yml, observability/grafana/provisioning/dashboards/sentinel.yml, observability/grafana/dashboards/sentinel-pipeline.json, observability/jaeger/config.yaml, scripts/check-observability.sh, scripts/test_observability_config.py]
  modified: [scripts/check-pins.sh, .env.example, .github/workflows/ci.yml]
decisions:
  - "Prometheus v3.15.1 rejects --web.enable-lifecycle=false (\"unexpected false\"); the flag is omitted because lifecycle and admin APIs are disabled by default, and the test asserts neither enabling flag is passed"
  - "Jaeger max_traces is 1000, not the planned 20000: a worst-case trace is about 504 spans (MAX_BATCH_SIZE 500 detect spans + edge + store), so 20000 traces could need about 10 GiB against a 512m limit. Uses an estimated ~1 KiB/span, flagged for measurement in 05-18. The key name was verified with `jaeger validate`, which rejects a misspelled key"
  - "Grafana mem_limit is 512m, not 256m: measured 222 MiB right after boot under 256m and 335 MiB under 512m with this image and provisioning. GF_PLUGINS_PREINSTALL_DISABLED=true stops startup plugin downloads"
  - "check-pins default glob is deployments/compose/*.yml, not docker-compose*.yml, so phase4-acceptance.yml is also enforced (the must-have says every compose file). The >= 4 image minimum applies only to docker-compose.yml"
  - "Registered names are parsed from every \"sentinel_*\" string literal in non-test telemetry files, not only Name: fields, because the six detector state gauges are built via newGauge(name, help). 19 names found"
  - "Alert expressions keep the plan's shape. SentinelDetectorCapacityHigh also covers the bytes ceiling (all three Bounds limits), since capacity refusal terminates the detector"
  - "Alert thresholds cite their source in the description. Lag 10000 = 2 x default writer batch; CH errors 5m > retry budget; restart > 2 changes; capacity 80% before terminating refusal. Error ratio 5% and pin margin 100000 are explicitly marked placeholders (the spec gives no number)"
metrics:
  duration: ~17 min (19:03:59Z to 19:21Z)
  completed: 2026-11-17
---

# Phase 5 Plan 13: Observability as code Summary

Observability is now code. `docker-compose.observability.yml` adds pinned Prometheus, Grafana and Jaeger, all bound to 127.0.0.1 with bounded storage, log rotation and memory limits. All five app services get JSON logs and an identical tracing env. Prometheus scrapes every replica through Compose DNS (`dns_sd_configs` type A). Six example alerts each have promtool fire and no-fire unit tests, and the tests are mutation-checked. Grafana provisions the `sentinel-prom` datasource and the "Sentinel pipeline" dashboard (17 panels). A booted Grafana 13.2.5 probe confirmed both load. A static test ties every dashboard and alert expression to metric names parsed from `backend/internal/telemetry` and rejects forbidden labels. `check-pins.sh` now enforces digests and loopback binds, including port ranges, on every compose file.

## Preflight

| Item | Value |
|---|---|
| `bash scripts/check-resources.sh` (19:03:59Z) | PASS: repo and docker-root 18.83 GiB free, MemAvailable 7.16 GiB |
| `df --output=avail -B1 /` before the prom pull | 20213841920 B (18.83 GiB, >= 1 GiB required) |
| after prom + jaeger pull | 19654557696 B |
| resource gate before the Grafana pull | PASS: 18.30 GiB free, 6.58 GiB MemAvailable |
| after Grafana pull | 17699409920 B |

## Digests actually inspected (`docker buildx imagetools inspect`, 2026-11-17)

| Image | Index digest | Matches 05-01 |
|---|---|---|
| prom/prometheus:v3.15.1 | sha256:fa3302279710be2f21a8301ec17a8b4f2301fd829bd113839b3b6b02b6f191a2 | yes |
| grafana/grafana:13.2.5 | sha256:9f7aa1e1e3246aade5792d35a738049f7f32f89cd51d1956cf426a4e86268a84 | yes |
| jaegertracing/jaeger:2.22.0 | sha256:836b967b5abcf1f7758066aa6b348619267a2f1459f72d472840570f9bd88a17 | yes |

All three were pulled. Grafana is about 485 MB compressed (amd64 manifest sha256:ffeeebf6...). Later bench plans need it, and pulling it let this plan verify schemaVersion and provisioning.

## Ports (all 127.0.0.1)

| Service | Host port | Container | Notes |
|---|---|---|---|
| Prometheus | 19090 | 9090 | lifecycle/admin APIs off (defaults) |
| Grafana | 13000 | 3000 | user admin, password `${SENTINEL_GRAFANA_ADMIN_PASSWORD:?}`, anonymous off, sign-up off |
| Jaeger UI | 16686 | 16686 | OTLP 4318 is network-internal only, never published |
| Scrape targets | (none) | ingest 9101, detector 9102, event-writer 9103, alert-writer 9104, query-api 9105 | via `dns_sd_configs` |

## Alerts (group `sentinel-examples`)

| Alert | Expression (summary) | for | Severity | Threshold source |
|---|---|---|---|---|
| SentinelConsumerLagSustained | `max by (group) (sentinel_kafka_consumer_lag) > 10000` | 5m | warning | 2 x default `SENTINEL_WRITER.BATCH_SIZE` (5000) |
| SentinelIngestErrorRate | 5xx / all ingest requests > 0.05 | 5m | critical | **placeholder**: spec defines no SLO; 503 is deliberate shedding, so only a sustained ratio alerts |
| SentinelClickHouseWriteErrors | `sum by (job) (rate(..._count{result="error"}[5m])) > 0` | 5m | warning | exceeds the per-batch retry budget (5 attempts, 100ms-5s backoff, 10s op timeout, about 1 min) |
| SentinelRestartLoop | `changes(process_start_time_seconds[15m]) > 2` | none | critical | 1 change = deploy/restart; 3+ = crash loop under `restart: unless-stopped` |
| SentinelDetectorCapacityHigh | max usage/limit of windows, ledger entries or bytes > 0.8 | 2m | warning | spec 11.1 "alert on capacity limits"; 100% refusal terminates the detector (docs/operations.md) |
| SentinelPinRetentionLow | `min by (topic, partition) (pin_margin) < 100000` | 10m | critical | **placeholder**: 0 = checkpoint history lost; 10m = 20 polls at the 30 s `pinMarginInterval` |

Every description ends with "example threshold, tune per deployment", and the static test asserts the marker.

## promtool output (`bash scripts/check-observability.sh`)

```
Checking /etc/prometheus/prometheus.yml
  SUCCESS: 1 rule files found
 SUCCESS: /etc/prometheus/prometheus.yml is valid prometheus config file syntax
Checking /etc/prometheus/alerts.yml
  SUCCESS: 6 rules found
Checking /etc/prometheus/alerts.yml
  SUCCESS: 6 rules found
  SUCCESS
Ran 21 tests in 0.703s
OK
PASS: observability config, alert rules and dashboard checks
```

Mutation check of the promtool tests, run against scratch copies; each mutated alerts.yml made `test rules` exit 1:
- lag threshold 10000 → 30000
- error ratio 0.05 → 0.5
- `status=~"5.."` → `"503"` and → `"500"`
- CH `result="error"` → `=~".+"`
- CH window/threshold changed
- restart `> 2` → `> 0`
- capacity 0.8 → 0.95
- pin 100000 → 10000
- pin `for: 10m` → 2m
- lag `for: 5m` → 1m
- capacity `for: 2m` → 1s

Three of these mutations at first survived the initial tests: pin `for`, capacity `for` and the 503-only matcher. Cases were added inside the pending window, and a 500+503 fire case was added, before committing.

## Dashboard "Sentinel pipeline" (uid `sentinel-pipeline`, schemaVersion 42)

`schemaVersion` 42 is `SCHEMA_VERSION = 42` in the Grafana 13.2.5 frontend bundle, read from the pinned image.

Panels:
1. Ingest events/s by result
2. HTTP error ratio (5xx and 4xx)
3. Ingest request p95
4. Producer buffer saturation (`max(buffered / limit)`; 1.0 means the next batch sheds)
5. Consumer lag by group (`max by (group, partition)`)
6. Detection delay p50/p95/p99
7. Alert publications/s by rule (from `sentinel_detection_alerts_total`, never logs)
8. Late events/s
9. ClickHouse write p95 by job
10. ClickHouse write errors/s
11. DLQ events/s by reason
12. SSE clients (pending 05-14)
13. Detector capacity % (windows/ledger/bytes)
14. Pin retention margin
15. Restarts (15m)
16. Scrape targets up
17. Resident memory by service

Every panel and target uses datasource uid `sentinel-prom`. Labels used: group, partition, topic, reason, result, rule_id, severity, status, plus the job and le target/bucket labels. No forbidden label appears.

Grafana probe: one isolated container `sentinel-chaoscheck-grafana-probe`, `--network none`, removed afterwards. It ran the pinned image with the repo provisioning mounted read-only:
- `GET /api/dashboards/uid/sentinel-pipeline` (admin) returned the dashboard with `"schemaVersion":42`.
- `GET /api/datasources/uid/sentinel-prom` returned the provisioned datasource.
- An anonymous request returned `401 Unauthorized`.

## Static test (`scripts/test_observability_config.py`, 21 tests)

- Inventory: 19 registered `sentinel_*` names. `PENDING_METRICS = {"sentinel_sse_clients"}`. A test fails as soon as a pending name becomes registered, which forces 05-14 to delete it.
- Alerts: exactly the six names; `for` ≥ 2m except RestartLoop; severity, summary and the example marker present; expressions reference only registered/standard names and no forbidden labels; promtool tests contain ≥ 2 cases per alert.
- Dashboard: uid, datasource uid on every panel and target, the 10 required titles, registered names only, no forbidden labels in expressions or legends.
- Prometheus config: a dns_sd job per service with the right port; self-scrape; the rule file is loaded.
- Overlay render: renders `docker compose -p sentinel-chaoscheck --env-file <temp> -f base -f overlay config --format json` and checks:
  - every port is bound to 127.0.0.1, and Jaeger publishes only 16686
  - Prometheus has retention size and time set and no lifecycle/admin flag
  - json-file `max-size` logging and `mem_limit` on all three backends
  - Grafana has anonymous access and sign-up off, its password comes from the variable, and rendering fails without it (no secret echoed)
  - all five app services have JSON logs and identical tracing (default 0.01, override 0.25 propagates); there is no top-level `name:`
  - the `.env.example` placeholders are present
- Pin checker (`COMPOSE_GLOB` seam):
  - a loopback range passes
  - `0.0.0.0:` and IP-less ranges fail
  - an overlay image without a digest fails
  - an overlay with a single image passes
  - the default run names every repository compose file

## Gate table

| Gate | Result |
|---|---|
| RED: `python3 -B -m unittest discover -s scripts -p test_observability_config.py` at 907a150 | FAILED (failures=15, errors=2) for missing artifacts and unenforced ranges/overlays, as intended |
| `bash scripts/check-observability.sh` | PASS (promtool SUCCESS x3, 21 tests OK) |
| `bash scripts/check-pins.sh` (all sections) / `images` | PASS (3 compose files) |
| `python3 -B -m unittest discover -s scripts -p test_stack_config.py` | OK (8 tests) |
| `jaeger validate --config=/etc/jaeger/config.yaml` (pinned image) | exit 0 |
| `make test` | exit 0 (Go 39 packages ok, web 174 tests, generator 262 tests) |
| `make lint` | exit 0 (golangci-lint, eslint + tsc, ruff + mypy) |
| other scripts unittests (host Python 3.10.12) | all OK except the pre-existing `test_seed_scenarios.py` |
| acceptance greps | 6 `alert: Sentinel`; `retention.size` present; no `^name:`; 39 `sentinel-prom` occurrences; 21 `def test_`; no `.env` literal |

## Commits

| Hash | Message |
|---|---|
| 907a150 | test(05-13): add failing observability-as-code and pin-checker tests (tests + COMPOSE_GLOB seam only) |
| a6c06eb | feat(05-13): observability overlay, alert rules with promtool tests, provisioned dashboard |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `--web.enable-lifecycle=false` crashes Prometheus v3.15.1**
- **Found during:** Task 2, flag check in the pinned image
- **Issue:** `prometheus --web.enable-lifecycle=false` fails with `error: unexpected false`, so the container would never start.
- **Fix:** Omitted the flag, since lifecycle and admin APIs are off by default. A comment in the overlay explains why, and the test asserts neither enabling flag is present.
- **Files:** deployments/compose/docker-compose.observability.yml
- **Commit:** a6c06eb

**2. [Rule 1 - Bug] Jaeger `max_traces: 20000` cannot fit its 512m limit**
- **Found during:** Task 2
- **Issue:** One sampled request produces one trace with a detect span per event (05-12), up to about 504 spans at MAX_BATCH_SIZE 500. 20000 such traces could need about 10 GiB, so Jaeger would be OOM-killed.
- **Fix:** Set `max_traces: 1000`. The derivation is in config.yaml, and the ~1 KiB/span figure is marked as an unmeasured estimate for 05-18.
- **Files:** observability/jaeger/config.yaml
- **Commit:** a6c06eb

**3. [Rule 1 - Bug] Grafana 256m limit is too tight**
- **Found during:** Task 2, Grafana probe
- **Issue:** Measured 222.4 MiB of 256 MiB right after boot, and 335 MiB under 512m. The startup plugin preinstaller also contacted grafana.com.
- **Fix:** Set `mem_limit: 512m` (measurement cited in a comment) and `GF_PLUGINS_PREINSTALL_DISABLED=true`.
- **Files:** deployments/compose/docker-compose.observability.yml
- **Commit:** a6c06eb

**4. [Rule 2 - Missing critical] Pin checker skipped a compose file; guards not run in CI**
- **Issue:** The planned glob `docker-compose*.yml` misses `phase4-acceptance.yml`, but the must-have says every compose file. The new guards were also not wired into CI.
- **Fix:** The default glob is `deployments/compose/*.yml`, and phase4-acceptance.yml passes. Added a CI step "observability rules, dashboard and overlay guards" that runs `bash scripts/check-observability.sh` (ci.yml was not in files_modified).
- **Files:** scripts/check-pins.sh, .github/workflows/ci.yml
- **Commit:** a6c06eb

**5. [Rule 3 - Blocking] Registered-name parser**
- **Issue:** The plan parses `Name: "sentinel_..."` literals, but the detector state gauges use `newGauge("sentinel_...", ...)`, so six names would be missed.
- **Fix:** The parser takes every `"sentinel_*"` string literal in non-test telemetry files.
- **Commit:** 907a150

Additions within plan intent: the bytes ceiling in SentinelDetectorCapacityHigh, and seven extra panels (ingest p95, producer buffer saturation, alert publications, late events, CH errors/s, targets up, RSS). Restart policy and json-file logging are set on all three backends.

## Deferred / Known Issues

- Pre-existing, not fixed: `scripts/test_seed_scenarios.py` fails on host Python 3.10.12 (`ImportError: cannot import name 'UTC' from 'datetime'`). CI runs it with `generator/.venv/bin/python`.
- Grafana logs harmless errors for the absent `provisioning/plugins` and `provisioning/alerting` directories, plus the dynamic angular-detector fetch when it has no network. This is noise, not a failure.
- check-pins only matches numeric quoted port mappings. A variable-based mapping without `127.0.0.1:` (e.g. `"${PORT}:8080"`) would not be caught. Current files always write the literal `127.0.0.1:` prefix.
- shellcheck is not installed on this host, so the scripts were not shellchecked locally.

## Known Stubs

- Dashboard panel "SSE clients" (`sum(sentinel_sse_clients)`) shows no data until 05-14 registers the metric. This is intentional and tracked through `PENDING_METRICS`.
- Prometheus job `query-api` reports `up == 0` until 05-14 mounts `/metrics` on the query-api ops listener. No alert depends on it.

## Threat Flags

None beyond the plan's threat model:
- T-05-13-01: loopback binds enforced; OTLP not published; anonymous Grafana returns 401 (probe).
- T-05-13-02: `${SENTINEL_GRAFANA_ADMIN_PASSWORD:?}`, change-me placeholder, check-pins env rule passes.
- T-05-13-03: TSDB retention 2d/1GB, max_traces, log rotation and mem_limit on all backends.
- T-05-13-04: forbidden-label test.
- T-05-13-05: digests re-inspected.
- T-05-13-06: lifecycle/admin APIs disabled.

## Notes for later plans

- **05-14:** after registering `sentinel_sse_clients`, delete it from `PENDING_METRICS` in scripts/test_observability_config.py; `test_pending_metrics_are_not_yet_registered` fails until you do. Mounting `/metrics` on query-api (port 9105) makes its scrape job go up, and the RestartLoop/RSS panels then include query-api. query-api already receives the tracing env from the overlay but starts no spans yet.
- **05-16:** Grafana runs at `http://127.0.0.1:13000/d/sentinel-pipeline` when the overlay is active.
- **05-17 bench overlay:**
  - Layer this overlay. Do not set `name:`.
  - Remap Prometheus/Grafana/Jaeger to distinct 3xxxx ports with `ports: !reset [...]`.
  - Ranges like `"127.0.0.1:38080-38082:8080"` are now accepted by check-pins, and any non-loopback range fails.
  - The env file must define `SENTINEL_GRAFANA_ADMIN_PASSWORD`, otherwise compose refuses to render. `SENTINEL_TRACING_SAMPLE_RATIO` sets one ratio for all services.
  - The backends add up to 1.5 GiB of mem_limit; include that in the preflight.
- **05-18 / 05-20:** measure Jaeger RSS and spans per trace at the bench ratio, and adjust `max_traces` from the measurement. Replace the two placeholder thresholds (ingest 5xx ratio 5%, pin margin 100000) with values derived from bench data and the events retention.
- Scrape interval 5s matches the lag poller (5s). The pin margin poll is 30s, so its panel moves in 30s steps.
- `sentinel_detection_alerts_total` counts publications, including replays. Unique alert counts must come from PG/CH.

## Self-Check: PASSED

- FOUND: all 10 created files and 3 modified files listed in key-files (`git show --stat a6c06eb 907a150`).
- FOUND commits: 907a150 (RED, test file + seam only), a6c06eb (GREEN; test file byte-identical to RED).
- No compose stack was started. Only `docker compose config` renders and `docker run --rm` containers (promtool, jaeger validate/help, one Grafana probe, all removed) ran. No prune, no `sentinel`/`sentinel-p4-demo-*` project touched. `make reset CONFIRM=1` was not needed because no `sentinel-chaoscheck` project resources were created.
- STATE.md and ROADMAP.md not touched. .planning/config.json and .serena/ not staged. backend/.env not read.
