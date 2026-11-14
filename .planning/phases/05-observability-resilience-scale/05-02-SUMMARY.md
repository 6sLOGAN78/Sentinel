---
phase: 05-observability-resilience-scale
plan: "02"
subsystem: resilience
tags: [retry, backoff, jitter, eventwriter, alertwriter, detector]
requirements: [REL-01]
dependency-graph:
  requires: []
  provides: [internal/retry shared full-jitter policy, WithRetryWaiter seams]
  affects: [plan 05-14 (alertstream delivery/listener conversion), plan 05-07 (chaos suite)]
tech-stack:
  added: []
  patterns: [capped exponential backoff with full jitter via crypto/rand, injected waiter builder]
key-files:
  created: [backend/internal/retry/retry.go, backend/internal/retry/retry_test.go]
  modified: [backend/internal/eventwriter/writer.go, backend/internal/eventwriter/writer_test.go, backend/internal/alertwriter/poison.go, backend/internal/alertwriter/writer.go, backend/internal/alertwriter/writer_test.go, backend/internal/detector/poison.go, backend/internal/detector/worker.go, backend/internal/detector/worker_test.go]
decisions:
  - "Callers keep their own retry loops and call the jittered wait with the policy/shared-budget ceiling rather than switching to retry.Do, so error wrapping, per-attempt timeouts and shared per-batch budgets stay byte-for-byte the same"
  - "Default wait is retry.Wait when no waiter is injected (nil field), so WithRetryWaiter(nil) restores production behavior"
metrics:
  duration: ~6 min
  completed: 2026-11-14
---

# Phase 5 Plan 02: Shared jittered retry Summary

Event-writer inserts and DLQ publishes, alert-writer effects and detector quarantine all wait through a single full-jitter policy now (`internal/retry`). Delays are crypto/rand-uniform in [0, ceiling], and the ceiling is a capped doubling that cannot overflow. That closes the REL-01 fixed-delay gap in the alert-writer and detector, and leaves attempt bounds, shared budgets and commit ordering unchanged.

## Commits

- cde37d9 `test(05-02)` RED, tests-only (4 `*_test.go` files, 385 insertions, 0 deletions)
- 070f538 `feat(05-02)` GREEN

## Retry API (`backend/internal/retry`)

- `type Policy struct { Min, Max time.Duration; Attempts int }`
- `(Policy).Validate() error` rejects Min <= 0, Max < Min and Attempts < 1
- `(Policy).Ceiling(attempt) time.Duration` returns min(Min*2^(attempt-1), Max). It is iterative and stops at Max, so attempt 64 or 1000 gives Max. attempt <= 1 gives Min.
- `(Policy).Delay(attempt, ...Option) time.Duration` draws full jitter in [0, Ceiling]
- `Wait(ctx, ceiling, ...Option) error` is a full-jitter timer that returns `ctx.Err()` on cancellation
- `Do(ctx, p, op, ...Option) error` validates first, runs at most `Attempts` times and stops on success or ctx. It returns `retry exhausted after N attempts: %w` (last op error), or `retry stopped ...: errors.Join(lastErr, ctxOrWaitErr)`
- `type Source func(maximum int64) (int64, error)` (inclusive) and `WithSource(Source) Option`. An RNG error or an out-of-range value falls back to the ceiling.
- `type Waiter func(ctx, ceiling) error`

## Caller map

| Caller | Before | After |
|---|---|---|
| eventwriter `insertWithRetry`, `publishWithRetry` | local `waitForRetry`/`jitterDelay`, hand-doubled backoff | `retry.Policy{RetryMinBackoff, RetryMaxBackoff, MaxAttempts}.Ceiling(attempt)` passed to `w.waitRetry` (default `retry.Wait`). The local helpers were deleted. |
| alertwriter `(*Writer).retry` | `time.NewTimer(retries.backoff)` with no jitter | `w.waitRetry(ctx, retries.backoff)`, full jitter. `effectRetries{remaining, backoff}` shared budget and doubling/cap are unchanged. |
| detector `(*Worker).quarantine` | `time.NewTimer(retries.backoff)` with no jitter | `w.waitRetry(ctx, retries.backoff)`, full jitter. `poisonRetries` shared sequence is unchanged, and the same "budget exhausted" error is returned on wait failure. |
| all three | none | `WithRetryWaiter(retry.Waiter)` builder, in the same style as `WithQuarantine` |

## Verification (all run for real, from backend/)

- RED at 2026-11-14T00:26:26Z: `go test ./internal/retry/... ./internal/eventwriter/... ./internal/alertwriter/... ./internal/detector/...` exited 1. The build failed because retry had no non-test files and `WithRetryWaiter` was undefined on all three workers. This proves only that the identifiers were absent.
- Behavioral mutation check after GREEN: with `if w.wait != nil` forced false in all three callers, TestWriterInsertRetryWaitsThroughJitteredCeilings, TestWriterInsertRetryWaitFailureIsTerminalWithoutCommit, TestAlertWriterRetryUsesJitteredWaiter and TestDetectorQuarantineUsesJitteredWaiter all FAILED. The mutation was reverted (grep confirmed 0 remaining).
- GREEN `go test -race -count=1` on the 4 packages, 00:28:03 to 00:28:07: exit 0
- `go tool -modfile=tools.mod golangci-lint run` on the 4 packages: 0 issues, exit 0. With `--build-tags=integration ./internal/... ./test/...`: 0 issues, exit 0.
- Full module, 00:28:34 to 00:29:19: `go vet ./...` exit 0, `go test -race -count=1 ./...` exit 0, `golangci-lint run ./...` 0 issues exit 0, `go build -tags=integration ./...` exit 0
- Acceptance greps: no `time.NewTimer` in either poison.go. No `func jitterDelay|func waitForRetry` in eventwriter. 14 non-test `retry.(Wait|Do|Policy)` references, and `retry.Wait(` appears in all three callers. The pre-existing test files changed by additions only (numstat shows 0 deletions).
- Not run: `make test`/`make lint` (Makefile is Phase 4 owned, and the equivalent go commands above were run), and no Docker or integration-tag test execution (the integration build and lint were run).

## Deviations from Plan

1. [Rule 3 - Blocking] `WithRetryWaiter` needs a struct field. Added a `wait retry.Waiter` field and import to `backend/internal/alertwriter/writer.go` and `backend/internal/detector/worker.go`, which are not in the plan's files_modified list. The change is 2 lines each and neither file is Phase 4 owned.
2. The detector test stays in internal package `detector`, not `detector_test`. The existing worker_test.go is internal (`//nolint:testpackage`) and its `poisonWorker`/`process` harness is unexported. The new tests reuse it.
3. Added `WithRetryWaiter` to the eventwriter as well, so its jittered ceilings and wait-failure terminal path are tested without sleeping. The plan required it only on the alert-writer and detector.
4. eventwriter keeps its loop (the plan allowed this) rather than `retry.Do`, because the loop preserves exact error strings and the ctx-break-before-wait behavior. The `retry.Do` path is covered by retry_test.go.

## Known Stubs

None.

## Threat Flags

None. No new network, auth or schema surface.

## Self-Check: PASSED

- FOUND: backend/internal/retry/retry.go, backend/internal/retry/retry_test.go
- FOUND commits: cde37d9, 070f538
