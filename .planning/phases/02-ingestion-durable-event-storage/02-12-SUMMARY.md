---
phase: 02-ingestion-durable-event-storage
plan: "12"
subsystem: local-deployment
tags: [docker, compose, distroless, healthcheck, make, readiness]

requires:
  - phase: 02-09
    provides: dependency-aware operations readiness and graceful lifecycle
  - phase: 02-11
    provides: verified ingest and writer recovery behavior
provides:
  - Pinned multi-stage nonroot image containing five service binaries and a static loopback health probe
  - Ordered Compose startup for ingest, event-writer, and query-api after topics and migrations
  - Repeatable local operations, readiness acceptance, and credential provisioning documentation
affects: [02-13, local-development, deployment, operations]

tech-stack:
  added: []
  patterns: [digest-pinned multi-stage builds, distroless nonroot runtime, migration-first Compose startup, bounded exec probes]

key-files:
  created:
    - .dockerignore
    - deployments/docker/Dockerfile
    - backend/tools/healthprobe/main.go
    - backend/tools/healthprobe/main_test.go
    - README.md
  modified:
    - deployments/compose/docker-compose.yml
    - Makefile
    - .env.example
    - scripts/check-stack.sh

key-decisions:
  - "Build all service binaries once into a digest-pinned distroless image and select the process with per-service Compose entrypoints."
  - "Restrict the static health probe to literal loopback HTTP URLs so container probes cannot become an SSRF primitive."
  - "Run infrastructure, topic creation, and migrations in strict order before starting the three Phase 2 application services."

patterns-established:
  - "Application containers use service DNS internally while every host publication remains loopback-only."
  - "Normal dev-up/dev-down paths preserve named volumes; destructive reset remains separately confirmation-gated."

requirements-completed: [GEN-03, OBS-01, ING-07]

duration: 8 min
completed: 2026-10-16
---

# Phase 02 Plan 12: Repeatable Local Application Stack Summary

**A pinned nonroot image and migration-first Compose workflow now launch the complete Phase 2 ingestion/storage path with real dependency readiness.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-10-16T02:27:05Z
- **Completed:** 2026-10-16T02:35:11Z
- **Tasks:** 3
- **Files modified:** 10

## Accomplishments

- Added a digest-pinned, cache-aware multi-stage Docker build that copies only five statically linked service binaries and a health probe into a UID 65532 distroless runtime.
- Added Compose services for ingest, event-writer, and query-api with relevant healthy dependencies, internal service DNS, required password substitution, loopback publications, and honest readiness probes.
- Made `make dev-up` perform infrastructure readiness, idempotent topic creation, migrations, and application startup in order, then proved a second run succeeds against preserved volumes.
- Replaced the stale Phase 1 root readme with current environment, key provisioning, ports, readiness, host-run, preserved-volume, and local TLS-boundary guidance.

## Task Commits

Each task was committed atomically:

1. **Task 1: Test healthprobe and prepare a secret-free runtime image** - `e50e85a` (feat), `3a7da09` (fix)
2. **Task 2: Wire ordered Compose services and make startup** - `285ddc4` (feat)
3. **Task 3: Document and verify repeatable local operation** - `d7f91af` (docs)

## Files Created/Modified

- `.dockerignore` - Excludes repository metadata, planning state, credentials, local keys, dependencies, and artifacts from the build context.
- `deployments/docker/Dockerfile` - Builds five commands plus the probe from pinned Go and copies only `/out` into pinned distroless/nonroot.
- `backend/tools/healthprobe/main.go` - Performs a three-second local HTTP probe and exits successfully only on HTTP 200.
- `backend/tools/healthprobe/main_test.go` - Covers 200, 503, timeout, invalid URL, and argument errors.
- `deployments/compose/docker-compose.yml` - Runs the three Phase 2 app services with internal DNS and static readiness probes.
- `Makefile` - Sequences topics and migrations before app startup and preserves loopback binds for host-run services.
- `.env.example` - Documents application, operations, infrastructure, retention, and placeholder-only local settings.
- `scripts/check-stack.sh` - Verifies infrastructure/app health, declared topics, ports, readiness JSON, and live dependency operations.
- `README.md` - Documents the repeatable Phase 2 local workflow and its security boundary.
- `readme.md` - Removed in favor of the canonical uppercase root README path.

## Decisions Made

- Published operations ports 9101, 9103, and 9105 on loopback so developers can inspect the same endpoints used by container health checks.
- Kept app services build-only in Compose; no app `image:` line bypasses the repository rule that every declared image is digest-pinned.
- Restricted healthprobe targets to literal loopback HTTP addresses. Compose probes use `127.0.0.1`, and the binary cannot be redirected to arbitrary network destinations.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Matched stack acceptance to structured readiness responses**
- **Found during:** Task 2 live stack verification
- **Issue:** The first check expected the bare body `ready`, while the implemented operations endpoint returns dependency-aware JSON.
- **Fix:** Assert the bounded response contains a JSON `status` of `ready` without printing dependency details on failure.
- **Files modified:** `scripts/check-stack.sh`
- **Verification:** `scripts/check-stack.sh` passed for all three application readiness endpoints.
- **Committed in:** `285ddc4`

**2. [Rule 2 - Security] Prevented arbitrary healthprobe network targets**
- **Found during:** Task 3 full lint gate
- **Issue:** Static analysis correctly flagged a command-line URL reaching `net/http` as an SSRF surface.
- **Fix:** Accept only plain HTTP URLs whose host is a literal loopback IP, retain sanitized errors, and document the validated call sites.
- **Files modified:** `backend/tools/healthprobe/main.go`, `backend/tools/healthprobe/main_test.go`
- **Verification:** Probe race tests and repository lint passed with zero findings; Compose health checks remained healthy.
- **Committed in:** `3a7da09`

---

**Total deviations:** 2 auto-fixed issues (1 correctness, 1 security).
**Impact on plan:** Both fixes strengthened the specified readiness contract without adding deployment scope.

## Verification

- `cd backend && go test -race ./tools/healthprobe/... -timeout 60s && go build ./cmd/... ./tools/healthprobe` — passed.
- `docker compose ... config --quiet`, `scripts/check-pins.sh`, and `scripts/check-makefile-dotenv.sh` — passed.
- Runtime inspection — `user=65532:65532`, entrypoint `/app/ingest`, exactly five service binaries plus `/app/healthprobe`, and no credential-like paths.
- First `make dev-up` — created topics, applied PostgreSQL/ClickHouse migrations, built all app images, and reached healthy state.
- Second `make dev-up` — succeeded against existing volumes; PostgreSQL had no pending migration and ClickHouse reported the schema current.
- `scripts/check-stack.sh` — all infrastructure/apps healthy; three declared topics correct; auto-create off; all ports reachable; readiness and dependency operations passed.
- `gsd-sdk query verify.key-links .../02-12-PLAN.md` — 2/2 key links verified.
- `make lint test` — passed with zero Go lint issues, all Go race tests, web lint/type/tests, and 37 generator tests.

## Issues Encountered

- The initial readiness body assertion did not match the existing structured endpoint; it was corrected to test the semantic status field.
- The first full lint run exposed unchecked probe diagnostics and unrestricted URL flow; both were resolved before the final clean gate.

## User Setup Required

Copy `.env.example` to `.env` and replace the local placeholder passwords before use. No external service or dashboard configuration is required.

## Next Phase Readiness

- Plan 02-13 can run end-to-end seed and query acceptance against a healthy, repeatable application stack.
- No unresolved packaging, migration-ordering, readiness, or local-volume blocker remains.

## Self-Check: PASSED

- Confirmed all created packaging/probe/documentation files exist and the lowercase root readme was replaced.
- Confirmed commits `e50e85a`, `285ddc4`, `3a7da09`, and `d7f91af` exist in history.
- Confirmed the runtime file list, repeated startup, 2/2 key links, and fresh full repository gate pass.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-16*
