---
phase: 02-ingestion-durable-event-storage
plan: "02"
subsystem: ingestion
tags: [go, kafka, franz-go, echo, postgres]
requires:
  - phase: 02-ingestion-durable-event-storage
    provides: Public HTTP hardening, configurable binds and scoped producer-key verification
provides:
  - Authenticated literal-colon batch intake with strict request and concurrency bounds
  - All-ISR idempotent Kafka publishing with finite buffers, retries and delivery deadlines
  - Real PostgreSQL-key and Kafka acknowledgement acceptance coverage
affects: [02-03, 02-06, 02-08, 02-09, 02-13]
tech-stack:
  added: [franz-go-1.22.1, kadm-1.19.0]
  patterns: [before-auth nonblocking semaphore, synchronous batch acknowledgements, tenant-derived Kafka keys]
key-files:
  created:
    - backend/internal/ingest/handler.go
    - backend/internal/kafkax/producer.go
    - backend/test/ingest_test.go
  modified:
    - backend/cmd/ingest/main.go
    - backend/internal/config/config.go
    - backend/internal/testing/containers.go
    - backend/go.mod
    - .env.example
key-decisions:
  - "Reserve an in-flight slot before authentication and shed immediately with 503 when all 64 default slots are occupied."
  - "Return 202 only after ProduceSync confirms every all-ISR record acknowledgement; Kafka acknowledgement does not claim ClickHouse persistence."
requirements-addressed: [ING-01, ING-03, ING-04, ING-05, ING-07]
requirements-completed: [ING-01, ING-03, ING-05, ING-07]
duration: 5h
completed: 2026-10-14
---

# Phase 2 Plan 02: Authenticated Kafka Intake Summary

**Scoped bounded batch intake that returns 202 only after every event receives a real all-ISR Kafka acknowledgement.**

## Performance

- **Started:** 2026-10-13T19:42:58+05:30
- **Completed:** 2026-10-14T00:43:47+05:30
- **Duration:** 5 h including session interruption and resumed verification
- **Tasks:** 2
- **Files modified:** 14

## Accomplishments

- Added `/api/v1/events:batch` with before-auth load shedding, uniform credential handling, early write-scope enforcement, strict 1 MiB and 500-event limits, whole-batch validation, and tenant comparison before publishing.
- Added a long-lived franz-go producer using all-ISR acknowledgements, idempotence, cancellation-safe production, five retries, a ten-second delivery timeout, and finite record/byte buffers.
- Preserved event order and client event IDs, assigned one UTC-millisecond `received_at` per batch, keyed records by `tenant_id|source_ip`, and propagated schema/request headers.
- Wired PostgreSQL, Redis, Kafka, hardened middleware, the HTTP listener, and ordered lifecycle closers into the ingest binary.
- Added real PostgreSQL credentials and real Kafka acceptance for acknowledgements, auth/scope/tenant failures, null source IP keys, overload shedding, and binary lifecycle behavior.

## Task Commits

1. **Task 1: Specify real Kafka acknowledgements and isolate the integration harness** — `75af508` (test)
2. **Task 2 RED: Specify bounded intake configuration** — `93c1b48` (test)
3. **Task 2 GREEN: Publish scoped batches after durable Kafka acknowledgement** — `34d53d6` (feat)

Supporting full-gate cleanups: `a58444c` fixed 02-00 test lint and `f957f26` fixed 02-01 integration-test lint without changing behavior.

## Verification

- `cd backend && go test -race ./internal/ingest/... ./internal/kafkax/... ./internal/config/... -timeout 60s` passed.
- `cd backend && go test -count=1 -tags=integration ./test/... -run 'TestIngestAcceptedAfterAck|TestIngestAuth|TestIngestShedsUnderLoad|TestBinaryLifecycle' -timeout 5m -p 1` passed against real PostgreSQL and Kafka in 20.289s.
- `cd backend && go vet -tags=integration ./...` passed.
- `cd backend && go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` passed with zero issues.
- `git diff --check` passed. Normal commit hooks ran without bypass.

## Interfaces for Downstream Plans

- `kafkax.Publisher.PublishBatch(ctx context.Context, events []*contracts.Event, requestID string) error`
- `kafkax.NewProducer(ctx context.Context, cfg config.KafkaConfig) (*Producer, error)` with `Ping` and bounded `Close`
- `ingest.NewHandler(verifier, publisher, cfg, logger) *Handler`, followed by `Register(*echo.Echo)`
- Kafka event records carry keys `tenant_id|source_ip` and headers `schema_version` plus `request_id`.

## Decisions Made

- Authentication and the write-scope check precede JSON parsing and publication; tenant identity always comes from the verified principal.
- Every record in a batch is encoded before synchronous publication, and any record-level error produces a sanitized 503 with `Retry-After: 1`.
- Ingest defaults to port 8080 and query API to 8081 when no explicit port is supplied; producer timeout must stay below HTTP write timeout.

## Deviations from Plan

- The plan described three tasks but encoded only two task blocks. Execution followed the two concrete task blocks and retained the planned RED/GREEN split through a separate configuration-test commit.
- Integration-tag lint exposed preexisting findings in plan 02-01 tests. They were corrected separately and revalidated against PostgreSQL before the full 02-02 lint gate ran.
- The delegated executor reached its usage limit after achieving green integration acceptance. The implementation was reviewed, re-tested, and committed inline after resumption.

## Issues Encountered

- No product blocker remained after resumption. The real Kafka and binary lifecycle acceptance suite passed unchanged.

## Known Limitations

- Error details are batch-level in this slice; indexed atomic validation details and exact OpenAPI/web contracts are delivered by 02-03.
- Preserved client IDs enable retries, but duplicate-free visible storage remains pending until the ClickHouse query/store plans.
- A local single-broker acknowledgement is durability evidence for the development stack, not a high-availability claim.

## User Setup Required

None. Environment examples now document all ingest, Kafka, server, and cache defaults.

## Next Phase Readiness

Plan 02-03 can refine exact indexed rejection envelopes without changing the acknowledgement boundary. Plans 02-06 and 02-08 can consume the shared Kafka producer and insert quota enforcement ahead of publication.

## Self-Check: PASSED

All owned implementation artifacts and three task commits exist. Race, real integration, lifecycle, vet, lint, and whitespace gates passed. Preexisting `.planning/config.json` and `.serena/` changes remain unstaged.
