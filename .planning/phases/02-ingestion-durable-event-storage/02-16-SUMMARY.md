---
phase: 02-ingestion-durable-event-storage
plan: "16"
subsystem: api
tags: [openapi, api-key, tenant-isolation, runtime-contract]
requires:
  - phase: 02-ingestion-durable-event-storage
    provides: scoped API-key verification and tenant-scoped ClickHouse event lookup
provides:
  - truthful API-key-only getEvent authentication contract
  - YAML-driven advertised-security regression against the production HTTP route
  - credential/scope rejection tests with storage-side-effect assertions
affects: [phase-02-verification, phase-04-authentication, api-consumers]
tech-stack:
  added: []
  patterns: [published security alternatives exercised through registered runtime handlers]
key-files:
  created: [backend/test/openapi_runtime_auth_test.go]
  modified: [api/openapi.yaml, backend/test/openapi_test.go, backend/internal/eventquery/handler_test.go]
key-decisions:
  - "Resolve D-28's premature getEvent bearer alternative in favor of D-03/D-13 and the established Phase 2 boundary: publish API-key-only lookup; JWT remains Phase 4."
  - "Preserve BearerAuth and future-route contracts without adding a bearer verifier to Phase 2."
patterns-established:
  - "Every advertised lookup authentication alternative must succeed through middleware.Apply and eventquery.Register, with storage tenant/id assertions."
requirements-completed: [ING-01, ING-05, STOR-03]
duration: 8 min
completed: 2026-10-16
---

# Phase 2 Plan 16: Accurate Event Lookup Authentication Summary

**getEvent now advertises the read-scoped API-key authentication implemented by its HTTP handler, backed by runtime contract and real tenant-isolation tests.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-10-16T09:11:29Z
- **Completed:** 2026-10-16T09:19:35Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments

- Added a default-build test that loads getEvent security from OpenAPI, derives credential headers from declared schemes, and sends requests through the production registered route and middleware. Unknown/unimplemented scheme forms fail explicitly. Successful requests must return the fixture and use its authenticated tenant and requested ID.
- Added missing/no-key, bearer-only, invalid-key-plus-bearer, read-key, write-only-key, and cross-tenant cases. Authentication/scope failures must never query storage; errors retain their code/message/request_id envelope.
- Removed only getEvent's unsupported bearer alternative. Its description now specifies `events:read`, tenant-from-key, and tenant-hidden 404 behavior. The document distinguishes implemented Phase 2 routes from planned Phase 4 bearer routes.
- Regenerated ignored TypeScript output, typechecked the web consumer, and retained the real PostgreSQL/ClickHouse lookup acceptance.

## Task Commits

1. **Task 1: Exercise every advertised getEvent alternative through the production handler** — `bb50e84` (test; observed runtime RED).
2. **Task 2: Publish the API-key-only live operation and retain future route contracts** — `879a28c` (test; observed contract RED), `7825e64` (fix; GREEN).

## Verification Evidence

| Command | Exit | Observed result |
|---------|------|-----------------|
| `cd backend && go test ./test ./internal/eventquery -run 'TestOpenAPIGetEventRuntimeSecurity\|TestEventLookup' -count=1 -timeout 30s` before document repair | 1 | Behavioral RED: `TestOpenAPIGetEventRuntimeSecurity/BearerAuth` expected advertised success 200 but received the handler's uniform 401; package 0.016s. API-key alternative and handler tests passed. |
| `cd backend && go test ./test -run '^TestOpenAPISecurity$' -count=1 -timeout 30s` before document repair | 1 | RED: actual getEvent security contained ApiKeyAuth and BearerAuth instead of exactly one ApiKeyAuth requirement; package 0.017s. |
| `cd backend && go test ./test ./internal/eventquery -run 'TestOpenAPI\|TestEventLookup' -count=1 -timeout 60s` after repair | 0 | GREEN: all selected contract/runtime/handler cases passed, packages 0.172s and 0.005s. |
| `cd backend && go test -tags=integration ./test -run '^TestEventLookupTenantScope$' -count=1 -timeout 3m -p 1` | 0 | Real PostgreSQL-backed keys and ClickHouse event lookup passed in 19.598s: owner 200, other tenant 404, invalid/missing key 401, write-only key 403. |
| `cd web && npm run gen:api && npm run typecheck` | 0 | OpenAPI TypeScript generation and `tsc --noEmit` passed; generated output remains ignored. |
| `cd backend && go tool -modfile=tools.mod golangci-lint run ./test/... ./internal/eventquery/...` | 0 | Zero issues. |
| `cd backend && go test -race ./test ./internal/eventquery -run 'TestOpenAPI\|TestEventLookup' -count=1 -timeout 60s` | 0 | Passed in 1.755s and 1.019s. |
| `gsd-sdk query verify.key-links .planning/phases/02-ingestion-durable-event-storage/02-16-PLAN.md` | 0 | 2/2 links verified. |

The checks above are local evidence; hosted CI has not been inferred. Phase-wide validation and independent re-verification remain the orchestrator's final gate.

## Decisions Made

D-28 originally promised API-key-or-bearer getEvent. D-03/D-13 and the explicit Phase 4 JWT boundary establish that the delivered Phase 2 lookup uses read-scoped keys. This bounded repair supersedes only that premature bearer alternative; it leaves D-28's other contract work, the BearerAuth component, and future dashboard/list route assertions intact. No JWT implementation or dependency was introduced.

## Deviations from Plan

None — plan executed as written. Shared STATE/ROADMAP/REQUIREMENTS updates are delegated to the orchestrator to avoid parallel-write conflicts.

## Issues Encountered

The standalone `golangci-lint` executable was unavailable (exit 127). The repository's pinned `go tool -modfile=tools.mod golangci-lint` invocation completed with zero issues; no package install or version change was needed.

## Security Evidence

- T-02-16-1: bearer-only, missing key, and invalid key plus bearer return 401; write-only key returns 403 before storage access.
- T-02-16-2: authenticated tenant/path binding is asserted in the registered route tests; real other-tenant lookup returns 404.
- T-02-16-3: every current YAML security alternative is exercised against the actual handler, and the exact API-key/header contract is checked separately.
- T-02-16-SC: no dependencies or lockfiles changed. The dual-build dependency policy check remains assigned to the final phase gate.

## Known Stubs

None introduced or remaining in this plan's delivered lookup. Existing future route contracts remain assigned to Phase 4.

## User Setup Required

None.

## Next Phase Readiness

CR-04 and verification gap 3 have local repair evidence. No unresolved 02-16 findings remain; other repair plans and independent phase re-verification still determine overall Phase 2 acceptance.

## Self-Check: PASSED

- All four plan-owned source/test/contract files exist.
- Commits `bb50e84`, `879a28c`, and `7825e64` exist in history.
- Both contract-to-runtime links verify. No tracked files were deleted and no unplanned threat surfaces were introduced.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-16*
