---
phase: 01-foundations-contracts
plan: 10
subsystem: infra
tags: [go, lifecycle, testcontainers, graceful-shutdown, integration-test]
requires:
  - phase: 01-01
    provides: config.Load, logger, database, redisx
  - phase: 01-07
    provides: ClickHouse address conventions
provides:
  - internal/app lifecycle (Run, RunContext, Lifecycle, DialTCP, DialBrokers, CloseRedis)
  - five service binaries under backend/cmd
  - generic-container test helpers (postgres, redis, clickhouse, apache/kafka)
  - TestBinaryLifecycle* integration suite
affects: [phase-2, 01-12, 01-13]
tech-stack:
  added: [testcontainers-go v0.44.0 (moby/moby/api types)]
  patterns: [LIFO closers, os.Exit(app.Run(...)) mains, isolated-env subprocess tests]
key-files:
  created:
    - backend/internal/app/app.go
    - backend/internal/app/app_test.go
    - backend/internal/testing/containers.go
    - backend/test/main_test.go
    - backend/test/lifecycle_test.go
    - backend/cmd/ingest/main.go
    - backend/cmd/detector/main.go
    - backend/cmd/event-writer/main.go
    - backend/cmd/alert-writer/main.go
    - backend/cmd/query-api/main.go
  modified:
    - backend/go.mod
    - backend/go.sum
    - backend/internal/testing/assertions.go
key-decisions:
  - "testcontainers-go v0.44.0 uses github.com/moby/moby/api types, so HostConfigModifier port bindings use network.PortMap/MustParsePort/netip.Addr rather than docker/go-connections nat"
  - "RunContext(ctx, ...) exposed so unit tests inject cancellation instead of sending real signals"
requirements-completed: [FND-03]
duration: 40min
completed: 2026-10-11
---

# Phase 1 Plan 10: App lifecycle and five binaries Summary

Five listener-free service binaries (ingest, detector, event-writer, alert-writer, query-api) built on a shared `app.Run` lifecycle with LIFO closers, SIGINT/SIGTERM handling and a 30s shutdown bound, proven end-to-end against real Postgres, Redis, ClickHouse and apache/kafka containers.

## Tasks

1. Task 1 (RED): container helpers and failing lifecycle tests, commit a2a309b.
2. Task 2 (GREEN): internal/app, five mains, commit 6c1bb0a.

## Verification

- `go build ./cmd/...`, `go test -race ./internal/app/...` pass; `go test ./...` passes.
- `scripts/go-test-nonempty.sh ./test/... TestBinaryLifecycle -tags=integration -timeout 10m -p 1` passes (all five binaries graceful on SIGTERM and SIGINT, Redis-down, Kafka-down, bad Postgres password, missing config, env-file cases).
- No httpx/echo/listen in cmd; no newrelic/clerk/pkg-errors/asynq/resend in cmd deps.

## Deviations from Plan

- **[Rule 3] moby API types**: v0.44.0 requires moby types, not docker/docker + go-connections; adapted helper imports (no new direct deps beyond moby/moby/api).
- Extra tests beyond plan: SIGINT case, Kafka-down case, DialBrokers, closer-failure exit code, invalid-config test.
- `go-test-nonempty.sh` must receive extra flags as separate args (the plan's single quoted string would be passed as one arg).
- assertions.go gofmt-formatted (pre-existing drift).
- golangci-lint could not run: config fails to load ("goimports is a formatter"); fixed in plan 01-12, not edited here.

## Known Stubs

None. Kafka and ClickHouse "closers" are no-ops because Phase 1 uses plain TCP reachability checks (no client held); later phases replace them with real clients.

## Self-Check: PASSED
