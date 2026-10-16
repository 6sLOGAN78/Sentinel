---
phase: 02-ingestion-durable-event-storage
plan: "17"
subsystem: operations
tags: [healthprobe, http, redirects, ssrf, readiness]
requires:
  - phase: 02-13
    provides: packaged local stack and final Phase 2 gates
provides:
  - Health probes reject every redirect before a follow-up request
  - Ten safe redirect regressions preserving caller-client configuration
affects: [02-21, local-deployment, phase-verification]
tech-stack:
  added: []
  patterns: [copy injected HTTP client before enforcing redirect policy]
key-files:
  created: []
  modified:
    - backend/tools/healthprobe/main.go
    - backend/tools/healthprobe/main_test.go
key-decisions:
  - "Enforce ErrUseLastResponse in run on a shallow client copy so main and injected clients share the same policy without mutating their caller."
requirements-completed: [OBS-01, GEN-03]
duration: 6 min resumed closeout
completed: 2026-10-16
---

# Phase 02 Plan 17: Redirect-Safe Health Probe Summary

**The container health probe rejects HTTP 301, 302, 303, 307 and 308 responses without sending any request to the redirect destination.**

## Performance

- **Resumed closeout:** approximately 6 min, completed 2026-10-16T17:16:25Z.
- **Tasks:** 2/2, including the inherited committed RED regression.
- **Source files modified:** 2.
- The earlier execution was interrupted. Its unknown integration outcome is not counted as evidence; the continuation reran the required gates.

## Accomplishments

- Added ten cases covering all five redirect statuses, each with both an external literal-IP destination and another loopback destination. The first hop uses a real loopback HTTP server; the recording transport synthesizes any attempted second response, so external targets are never dialed.
- Enforced redirect rejection inside the shared `run` path used by `main`, using a shallow copy of the supplied `http.Client`. The original timeout and transport remain effective, and the caller's redirect callback remains unchanged.
- Preserved literal-loopback/plain-HTTP URL validation, the runtime three-second timeout, 4 KiB response drain, response closure, generic request errors and HTTP-200-only success semantics.

## Task Commits

1. **Task 1: Prove redirects cannot issue a second probe request** — `0b5f866` (`test`), committed by the original executor.
2. **Task 2: Apply a non-redirecting client policy for every probe call** — `3d98864` (`fix`), verified and committed by the continuation.

## RED/GREEN and Verification Evidence

- **Inherited behavioral RED:** the original executor reported running `cd backend && go test ./tools/healthprobe -run '^TestRunRejectsRedirects$' -count=1 -timeout 15s` before the implementation change. All ten cases failed behaviorally: the old probe returned success (`0`) and sent two requests. The continuation verified that commit `0b5f866` contains those regression cases and is an ancestor of the GREEN commit. This is inherited evidence, not a newly executed RED run.
- **Fresh named GREEN:** `cd backend && go test ./tools/healthprobe -run '^TestRunRejectsRedirects$' -count=1 -timeout 15s -v` exited `0`; all ten subtests passed, package duration `0.006s`. Each asserts failure and exactly one request, plus unchanged caller-client redirect policy.
- **Fresh race suite:** `cd backend && go test -race ./tools/healthprobe -count=1 -timeout 60s` exited `0`, package duration `1.120s`. Existing 200, 503, timeout, invalid-URL and argument-count tests passed.
- **Fresh vet:** `cd backend && go vet ./tools/healthprobe` exited `0`.
- **Fresh build:** `go build -o "$build_dir/healthprobe" ./tools/healthprobe` from `backend`, with `build_dir` created using `mktemp -d /tmp/sentinel-healthprobe-build.XXXXXX`, exited `0`.
- **Fresh pinned lint:** `cd backend && go tool -modfile=tools.mod golangci-lint run ./tools/healthprobe/...` exited `0` with `0 issues.`
- **Fresh health/readiness integration:** `cd backend && go test -tags=integration ./test -run 'TestReadyz|TestHealthz' -count=1 -timeout 5m -p 1` exited `0`, package duration `68.464s`. The selected source includes `TestHealthz`, `TestReadyzDependencies` and `TestReadyzDraining`; the selection is nonempty.
- `git diff --check` passed before the GREEN commit. That commit deleted no tracked files. No placeholder/stub markers were found in either owned source file.

## Decisions Made

Reject all redirects, including loopback-to-loopback redirects: readiness has no redirect use case. Preserve injected client configuration by copying the client before replacing `CheckRedirect`; rely on existing non-200 failure handling for the original 3xx response.

## Deviations from Plan

- Built to a temporary path instead of creating `backend/healthprobe` in the checkout. The interrupted root run had already created that untracked output; the continuation verified it was an ELF Go binary for `github.com/6sLOGAN78/sentinel/tools/healthprobe` and removed only that generated file. No tracked file or user data was removed.
- A bare `golangci-lint` command was absent from `PATH` (exit `127`), so lint was rerun successfully with the existing repository-pinned `go tool -modfile=tools.mod` invocation. No package installation or dependency change was needed.
- Shared state, roadmap, requirements and validation updates remain owned by the phase orchestrator to prevent concurrent tracking edits.

## Unresolved Findings / Next Readiness

No unresolved finding remains within this plan's healthprobe scope. Plan 02-21 must rebuild the application image and execute the final live-stack gate before asserting deployed-image repair. Hosted CI success is not claimed. The original review and verification reports remain intact for additive independent re-verification.

## Threat Review

T-02-17-1 is mitigated by the shared redirect policy and safe zero-follow regressions. T-02-17-2 retains the existing timeout and response-drain bounds, with timeout coverage passing. T-02-17-SC introduces no dependencies; the dual-build dependency guard remains a final-gate responsibility. No new network endpoint, schema or authentication surface was introduced.

## User Setup Required

None.

## Self-Check: PASSED

Both owned source files and this summary exist. Commits `0b5f866` and `3d98864` exist, and the RED commit precedes the GREEN commit. All fresh verification commands listed as passing completed successfully; the interrupted integration run is excluded.
