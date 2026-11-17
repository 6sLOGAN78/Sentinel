---
phase: 05-observability-resilience-scale
plan: "07"
subsystem: resilience
tags: [chaos, kafka, clickhouse, sigterm, resource-gate, compose, testcontainers]
requirements: [REL-02, REL-03]
status: complete
dependency-graph:
  requires: [05-02 WithRetryWaiter hook, 05-05 kafkax/writer state, 05-06 logger/middleware]
  provides: [scripts/check-resources.sh, scripts/check-chaos.sh (7 named families, compose-project guard), FaultContainer.Stop/Start, stop_grace_period 40s on the 5 app services, ClickHouse dial/handshake bounded by the caller deadline]
  affects: [05-11 (adds slow-consumer and overload families to check-chaos.sh), 05-17 (make test-chaos target), every Phase 5 real-container verify (resource gate)]
tech-stack:
  added: []
  patterns: [read-only resource gate before any container, per-name nonempty -list guard, compose-backed families pinned to an explicit unprotected COMPOSE_PROJECT_NAME with port ownership proven through docker ps labels, net.Conn deadline clamp during the driver handshake]
key-files:
  created: [scripts/check-resources.sh, scripts/test_check_resources.py, scripts/check-chaos.sh, scripts/test_check_chaos.py, backend/test/chaos_broker_test.go, backend/test/chaos_clickhouse_test.go, backend/test/chaos_shutdown_test.go, backend/internal/storage/clickhouse/client_test.go]
  modified: [backend/internal/testing/containers.go, backend/internal/testing/containers_test.go, backend/internal/testing/export_test.go, backend/test/main_test.go, backend/internal/storage/clickhouse/client.go, deployments/compose/docker-compose.yml, scripts/test_stack_config.py]
decisions:
  - "sse-reconnect (phase4 tag) reaches the dev stack through the root .env host ports. The compose file is named sentinel, so check-chaos.sh now requires an explicit COMPOSE_PROJECT_NAME, refuses sentinel and sentinel-p4-demo-* (exit 6), and checks that the PG, Redis, Kafka and ClickHouse ports are published only by that project's running containers. It never invokes docker compose"
  - "The ClickHouse adapter dials through DialContext and clamps conn deadlines to the acquire-context deadline until the driver clears them at the end of the handshake. clickhouse-go v2.49 otherwise ignores the caller deadline during dial and handshake (net.DialTimeout plus a DialTimeout-only handshake deadline)"
  - "The broker-restart harness closes an exited writer's consumer before starting the replacement, matching the production lifecycle (lc.Add kafka-consumer)"
  - "For sse-reconnect only the infra part of make dev-up was run (kafka, clickhouse, postgres, redis, topic-init, migrate) on sentinel-chaoscheck. Aegis holds 0.0.0.0:8080 (ingest's host port), and both phase4 families build and run their own query-api, so the five app containers are not used by them"
metrics:
  duration: "~2 h across two sessions (session 1 2026-11-16 19:10Z to 19:50Z; session 2 2026-11-17 17:15Z to 17:45Z)"
  completed: 2026-11-17
---

# Phase 5 Plan 07: First failure-suite slice Summary

Spec §15 failures now run from one selector, `scripts/check-chaos.sh`, behind the read-only resource gate. The families are real Kafka broker stop/start, ClickHouse outage within and beyond the retry budget, graceful SIGTERM mid-batch for event-writer and detector, writer restart, DLQ, rate limit/overload and SSE reconnect. All 7 families pass on real containers at d9c89fc. Running the gate found two defects, both fixed tests-first: a test-harness group-membership leak, and a real production bound violation in the ClickHouse adapter.

## Resource gate (Task 3 checkpoint)

Authorization: the orchestrator prompt for this run stated "The run was previously blocked only by host RAM. The gate passes now: 18 GiB of disk and 4.9 GiB of memory are free." After the session restart, the coordinator prompt said: "Host resources now: MemAvailable about 6 GiB, 26 GiB free disk. Continue from where you stopped." No human message was received directly.

The gate output before the final chaos-broker run (each family logged its own gate):

```
resource gate: ok repo /home/logan78/Desktop/Sentinel free 25.65 GiB (floor 10 GiB)
resource gate: ok docker-root /var/lib/docker free 25.65 GiB (floor 10 GiB)
resource gate: ok MemAvailable 5.00 GiB (floor 3 GiB)
resource gate: PASS
```

Across all final runs the gate passed every time. The minimum seen was 21.01 GiB free and 5.00 GiB MemAvailable. Nothing was pruned.

## Final per-family results (code at d9c89fc, sequential, gate before each)

Each family was run as `bash scripts/check-resources.sh` and then `CHAOS_FAMILIES=<name> bash scripts/check-chaos.sh`. sse-reconnect also had `COMPOSE_PROJECT_NAME=sentinel-chaoscheck`.

| Family | Tests | Result | Exit | Wall (s) | Ceiling | Start / end (UTC, 2026-11-17) |
|---|---|---|---|---|---|---|
| chaos-broker | TestChaosKafkaBrokerRestart (22.84s) | PASS | 0 | 238 | 8m | 17:16:26 / 17:20:24 |
| chaos-clickhouse | ShortOutageAbsorbed (20.59s), LongOutageRestart (11.97s) | PASS | 0 | 56 | 10m | 17:20:24 / 17:21:20 |
| chaos-shutdown | GracefulShutdownEventWriter (3.17s), GracefulShutdownDetector (5.20s) | PASS | 0 | 42 | 10m | 17:21:20 / 17:22:02 |
| writer-restart | DetectorCrashBoundaries (148.95s), WriterCrashRestart (46.81s), WriterCommitAfterInsert (0.35s), WriterInsertFailureNoCommit (11.33s) | PASS | 0 | 231 | 12m | 17:22:02 / 17:25:53 |
| dlq | AlertWriterPoison, DetectorPoison, WriterPoisonToDLQ, WriterDLQFailureNoCommit | PASS | 0 | 47 | 10m | 17:25:53 / 17:26:40 |
| rate-limit | TestRateLimit{TokenBucket,Concurrent,ValidationConsumesQuota,QuotaPrecedesTenantAndFields,RedisDownClosed,RedisDownOpen}, TestIngestShedsUnderLoad | PASS | 0 | 27 | 8m | 17:26:40 / 17:27:07 |
| sse-reconnect | TestPhase4Delivery (15.00s), TestPhase4Stream (35.46s) | PASS | 0 | 55 | 10m | 17:28:05 / 17:29:00 |

Wall time includes compiling the `-race` test binary. chaos-broker's 238 s is mostly a cold build after the session restart; the test itself ran in 22.84 s.

Evidence lines:
- broker: `broker restart: total records=40 unique=40 max buffered=20`. The failed publish during the outage left no records, committed stayed 20 until recovery, and the buffer stayed under the 5000 cap.
- ClickHouse: `short outage absorbed after 1 retry waits`. The long outage spent all 3 attempts, returned a terminal error with nothing committed, and a new writer then completed with unique == 6.
- shutdown: event-writer `SIGTERM to clean exit in 97.88ms` / `7.59ms`; detector `SIGTERM to clean exit in 8.38ms`, `pin after SIGTERM processed_next=9 decisions=1`, `8.56ms`. Lifecycle message asserted: `shutdown complete` (app.go `log.Info().Msg("shutdown complete")`, 30 s budget, test limit 35 s).
- sse-reconnect: `check-chaos: sse-reconnect targets compose project 'sentinel-chaoscheck' (ports 15432 16379 29092 19000)`.

### First-session results (history)

Session 1 (2026-11-16) ran each family and found the two failures below. After the fixes, every family passed in session 1 too. The session restart lost those logs, so the final table above is the authoritative full re-run. Recorded session-1 outcomes:
- chaos-broker FAIL, exit 1, 252 s. After d2ab678: PASS, 55 s.
- chaos-clickhouse FAIL, exit 1, 54 s. After d9c89fc: PASS, 60 s.
- chaos-shutdown PASS 37 s; writer-restart PASS 232 s; dlq PASS 41 s; rate-limit PASS 33 s; sse-reconnect PASS 54 s.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Safety] Compose-project guard for the sse-reconnect family**
- **Found during:** pre-run review of check-chaos.sh
- **Issue:** sse-reconnect reaches whatever stack publishes the root `.env` ports. `make dev-up` defaults to compose project `sentinel` (the top-level `name:`), which is protected.
- **Fix:** sse-reconnect now requires an explicit `COMPOSE_PROJECT_NAME`. It refuses `sentinel` and `sentinel-p4-demo-*`, and it fails if any of the four `.env` dependency ports is not published by that project's running container (all exit 6). The check uses `docker ps` labels only; there is no compose call. Testcontainers families are unaffected. Verified live: with the project unset, exit 6; with `COMPOSE_PROJECT_NAME=sentinel`, exit 6.
- **Files:** scripts/check-chaos.sh, scripts/test_check_chaos.py (6 tests)
- **Commits:** 7a62162 (RED), 6fce4a6 (GREEN; also corrected one RED assertion that matched the word "compose" inside the `com.docker.compose.project` label text)

**2. [Rule 1 - Bug, test harness] Orphaned consumer stole the partition after the broker restart**
- **Found during:** chaos-broker real run
- **Issue:** committed stayed at 20 of 40 for the whole 60 s. A diagnostic showed `members=2`. The writer that exited during the outage had its kgo client closed only in t.Cleanup, so the client rejoined, took partition 0 under BlockRebalanceOnPoll and never polled it. Production closes the consumer when Run fails.
- **Fix:** close the exited writer's consumer before starting the replacement, and assert that the replacement is the sole group member. No timeout or assertion was relaxed.
- **Files:** backend/test/chaos_broker_test.go
- **Commit:** d2ab678. The failing real run is the RED evidence. It is a test-only fix, so it is a single commit.

**3. [Rule 1 - Bug, production] ClickHouse handshake ignored the caller deadline**
- **Found during:** chaos-clickhouse real run (LongOutageRestart: `expected 2 retry waits, actual 1`)
- **Issue:** with OperationTimeout 500ms, attempt 2 took 5.003 s (`handshake: ... i/o timeout`) against the paused server, so the retry budget expired after 2 of 3 attempts. clickhouse-go v2.49 dials with `net.DialTimeout` and bounds the handshake by DialTimeout (5 s) alone. Any configured `OPERATION_TIMEOUT` under about 10 s could therefore be overrun on a new connection.
- **Fix:** `DialContext` dials with the acquire context. `handshakeConn` clamps deadlines to that context's deadline until the driver clears them at the end of the handshake, or the limit passes. After that, deadlines pass through, so pooled reuse follows each operation's own context.
- **Files:** backend/internal/storage/clickhouse/client.go, client_test.go (silent-TCP-server unit test: 5.01 s before the fix, about 0.3 s after)
- **Commits:** 0b4db18 (RED), d9c89fc (GREEN)

### Environment notes
- Aegis (`compose` project) holds `0.0.0.0:8080`, so only the infra services of `make dev-up` were started on `sentinel-chaoscheck` (see decisions). Teardown: `COMPOSE_PROJECT_NAME=sentinel-chaoscheck make reset CONFIRM=1` removed its 4 containers, 3 volumes and its network. No project images were built, so there were no images to remove; shared infra images were left alone.
- Observation: `sentinel-p4-demo-20261024` was listed as `exited(5)` (containers only; no `sentinel*` volumes were present) at the start of session 1. After the session restart it was no longer listed. Every compose command in this plan carried `COMPOSE_PROJECT_NAME=sentinel-chaoscheck`, and testcontainers only remove their own containers. The Aegis `compose` project also changed config file and state in the same window, so the change was external to this run. The `sentinel` project did not exist at any point.

## Verification

- `make test`: exit 0 (Go `-race ./...`, web 174 tests, generator 262 tests)
- `make lint`: exit 0. `golangci-lint run --build-tags=integration ./...`: 0 issues.
- `python3 -B -m unittest discover -s scripts -p test_check_chaos.py`: 6 OK. Full scripts discovery: 90 OK and 1 pre-existing import error. `test_seed_scenarios.py` uses `datetime.UTC` (Python 3.11+), while the host's system python3 is 3.10. That file was not touched by this plan.

## Deferred Issues

- Pre-existing, unchanged: 22 phase4-tag lint findings in `backend/testphase4/tenant_matrix_test.go` (deferred-items.md, b31bd3c).
- Pre-existing: `scripts/test_seed_scenarios.py` cannot import under system Python 3.10 (see above).

## Known Stubs

None.

## Threat Flags

None. The new surface is local only: the guard reads `docker ps` labels, and the ClickHouse dialer is a narrower bound on an existing connection.

## Self-Check: PASSED

- Files exist: scripts/check-chaos.sh, scripts/test_check_chaos.py, scripts/check-resources.sh, backend/internal/storage/clickhouse/client_test.go, backend/test/chaos_{broker,clickhouse,shutdown}_test.go
- Commits present: 6c842d5, 76f4429, 34b876b, b31bd3c, 7a62162, 6fce4a6, d2ab678, 0b4db18, d9c89fc
