---
phase: 02-ingestion-durable-event-storage
plan: "00"
subsystem: api
tags: [go, echo, cors, logging, http]
requires:
  - phase: 01-foundations-contracts
    provides: HTTP lifecycle helper, configuration loader, error envelope and middleware chain
provides:
  - Validated optional IPv4/IPv6 HTTP bind address
  - Deny-empty CORS and sanitized request, error and panic logging
  - Race-tested listener and public HTTP safety regressions
affects: [02-02, 02-09, 02-12]
tech-stack:
  added: []
  patterns: [path-only request logs, status-only error logs, explicit IP bind validation]
key-files:
  created: []
  modified:
    - backend/internal/config/config.go
    - backend/internal/config/config_test.go
    - backend/internal/httpx/httpx.go
    - backend/internal/httpx/httpx_test.go
    - backend/internal/middleware/global.go
    - backend/internal/middleware/middleware_test.go
key-decisions:
  - "BindAddress accepts an empty value or an IPv4/IPv6 literal; unset retains all-interface container binding."
  - "Log fixed HTTP status metadata rather than raw errors or panic values, which may contain request secrets."
requirements-addressed: [ING-05, ING-07]
requirements-completed: []
duration: 16min
completed: 2026-10-13
---

# Phase 2 Plan 00: Public HTTP Prerequisites Summary

**Validated IPv4/IPv6 listener binds, deny-empty CORS and path-only request logs with sanitized error and panic output.**

## Performance

- **Started:** 2026-10-13T12:54:24Z
- **Completed:** 2026-10-13T13:10:36Z
- **Duration:** 16 min
- **Tasks:** 3
- **Files modified:** 6

## Accomplishments

- `SENTINEL_SERVER.BIND_ADDRESS` loads and validates an optional IP literal before `httpx` constructs its address with `net.JoinHostPort`. Empty binding preserves container compatibility.
- Empty origins install no CORS permissions; explicitly allowed origins still work. Direct TCP-peer extraction and caller-configured trusted extraction remain intact, with Recover registered first.
- Request logs omit queries and request headers/body. Error logs record status metadata without raw error messages or stacks; expected client errors use warning level. Echo panic output is suppressed in favor of the sanitized error handler. Server error responses sanitize custom internal messages while preserving status and Retry-After.
- Real IPv4 and IPv6 HTTP requests, wildcard binding, occupied-port failures, configured timeouts, and nil/zero/never-started shutdowns are covered.

## Task Commits

1. **Task 1: Write bind and public HTTP safety regressions** — `95f30ab` (test)
2. **Task 2: Implement bind config before HTTP listener changes** — `e2837ad` (feat)
3. **Task 3: Harden CORS, logs and peer identity defaults** — `b425fbd` (fix)

## Verification

- Baseline: `cd backend && go test -race ./internal/middleware/... ./internal/httpx/... ./internal/config/... -timeout 60s` passed all three packages before new tests.
- RED: the same command failed only the new `TestCORSEmptyDenies`, `TestRequestLoggerNoQuery`, `TestNoKeyInLogs`, `TestHTTPXBindAddress`, `TestLoadServerBindAddress`, and `TestLoadRejectsMalformedBindAddress` cases. Existing regressions stayed green. A test-constructor arity error was corrected before recording this valid RED run.
- Bind GREEN: `cd backend && go test -race ./internal/config/... ./internal/httpx/... -timeout 60s` passed, including real IPv4/IPv6 listener requests.
- Final GREEN: the full three-package race command above passed, including sentinel success/plain-error/typed-error/Echo-error/client-error/panic cases, CORS GET/preflight cases, and existing chain/IP/shutdown tests.
- `cd backend && go vet ./internal/middleware/... ./internal/httpx/... ./internal/config/...` passed.
- `git diff --check` passed. Normal commit hooks ran; no bypass was used. No tracked files were deleted.

## Decisions Made

- Bind addresses are IP literals, without brackets, schemes or ports; this rejects malformed configuration before listener startup.
- Logs use fixed HTTP status text, excluding even typed error messages because callers can attach request data. Raw panic output is also unsafe.

## Deviations from Plan

- **Tracking adjustment:** ING-05 and ING-07 remain pending in REQUIREMENTS.md. This prerequisite contributes HTTP safety and finite timeouts; authentication, cross-tenant rejection, intake shedding and Kafka buffer caps require downstream plans. Marking either complete here would overstate delivered behavior.
- No production scope deviation; panic-output sanitization implements the planned no-secret-in-error-log requirement.

## Issues Encountered

- The sandbox denied writes to the Go build cache and `.git/index.lock`. Commands were rerun with standard approval escalation and succeeded. No authentication gate occurred.
- One test invocation used the repository root instead of `backend/`; it was corrected to the module directory before validation.
- SDK tracking required repairing the existing nonnumeric plan position and using the installed handlers' named arguments instead of the workflow's stale positional examples. Plan advancement, metrics, decisions, session and roadmap updates then succeeded; the metric row was moved into its matching table after the handler selected the older phase table.

## Known Stubs

None. Stub-pattern inspection of the six owned files found no TODO/FIXME/placeholder behavior. Empty origin/bind values are intentional security and container defaults.

## User Setup Required

None for this plan. Operators may set the optional dotted bind environment variable. Loopback wiring for `make run` remains assigned to 02-12.

## Next Phase Readiness

Public HTTP prerequisites are ready for 02-02 intake wiring. Phase 2 remains in progress; no ingestion route, Kafka producer or storage pipeline is claimed by this plan. No new network route, auth mechanism or schema trust boundary was introduced outside the threat model.

## Self-Check: PASSED

All six owned files exist. All three task commits exist in git history. Targeted race tests, vet and whitespace checks passed. Preexisting `.planning/config.json` and `.serena/` changes remained unstaged.
