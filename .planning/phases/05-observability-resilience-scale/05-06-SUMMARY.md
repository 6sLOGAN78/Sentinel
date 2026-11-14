---
phase: 05-observability-resilience-scale
plan: "06"
subsystem: observability
tags: [logging, zerolog, json, trace_id, otel, scrubbing, middleware]
requirements: [OBS-03]
dependency-graph:
  requires: [05-01 package approvals (go.opentelemetry.io/otel/trace@v1.47.0)]
  provides: [JSON-by-default logger with "timestamp" field, config.LogFormatJSON/LogFormatConsole, json|console Validate, trace_id/span_id on the request logger from a valid span context, single-occurrence request log keys]
  affects: [05-10 tracer provider + otelhttp (its tests assert trace_id on every request line), 05-19 docs/operations.md (ip as personal data), Compose logs (local env is now JSON)]
tech-stack:
  added: [go.opentelemetry.io/otel/trace v1.47.0 (direct; was v1.46.0 indirect), go.opentelemetry.io/otel v1.47.0 and otel/metric v1.47.0 (indirect bumps), go.opentelemetry.io/otel/log v1.47.0 (indirect, required by otel v1.47.0)]
  patterns: [zerolog Hook for the timestamp field with no global mutation, echo-context flag to avoid re-adding fields the context logger already carries, token-stream duplicate-key check in tests]
key-files:
  created: []
  modified: [backend/go.mod, backend/go.sum, backend/internal/logger/logger.go, backend/internal/logger/logger_test.go, backend/internal/config/observability.go, backend/internal/config/config_test.go, backend/internal/middleware/context.go, backend/internal/middleware/global.go, backend/internal/middleware/middleware_test.go]
decisions:
  - "Logging.Format, not Environment, selects the writer. Only \"console\" yields ConsoleWriter. Validate rejects anything other than json|console, including \"\" (Load always defaults it to json)"
  - "The timestamp is a zerolog Hook that writes \"timestamp\" (UTC RFC3339Nano). zerolog.TimestampFieldName and other globals are untouched"
  - "user_id rule: EnhanceContext adds user_id when identity is already set and marks the echo context (logger_has_user_id). RequestLogger adds user_id only when GetUserID(c) != \"\" and that mark is absent, which is the case for route-level auth after EnhanceContext"
  - "ip stays in request logs exactly once (from the context logger). It is personal data kept for security investigation (T-05-06-02 accept) and is never a metric label"
  - "Removing the duplicate method key went beyond the plan's request_id/ip de-duplication. The contract test asserts there are no duplicate keys on any line"
  - "NewPgxLogger keeps its signature and emits JSON. The console-only formatFieldValue (200-char truncation, pretty JSON) and its consts were deleted because nothing else referenced them. SQL args are still redacted upstream by database.RedactingLogger"
metrics:
  duration: ~8 min (00:59Z to 01:07Z)
  completed: 2026-11-14
---

# Phase 5 Plan 06: Spec §11.3 JSON log contract Summary

Every Sentinel binary now writes one JSON object per line in every environment, including Compose's `local`. Each line has `service`, `environment`, `level` and `timestamp` (UTC RFC3339Nano, no `time` key). HTTP request lines also carry `request_id`, `method`, `path` and `ip` exactly once, plus `trace_id`/`span_id` when the request has a valid span context. Console output appears only when `SENTINEL_OBSERVABILITY.LOGGING.FORMAT=console` is set explicitly (`.env.example` already sets it for host-run dev).

## Commits

| Hash | Message |
|---|---|
| 69331e1 | test(05-06): add failing tests for JSON log contract, trace_id and scrubbing (only `*_test.go` plus go.mod/go.sum) |
| 55771a6 | feat(05-06): JSON-by-default logs with timestamp hook, trace_id and no duplicate keys |

## Rewritten and added tests

- `logger_test.go`: removed `TestLoggerLocalUsesConsole`. Added `TestLoggerLocalEmitsJSON` (local + json parses, has service/level/timestamp in UTC RFC3339Nano, no `time`), `TestLoggerConsoleOptIn` and `TestPgxLoggerJSON` (stdout captured through `os.Pipe`; `component=database` and timestamp).
- `config_test.go`: `TestObservabilityLoggingFormat`. json and console pass. xml, "" and JSON fail with a message naming json and console.
- `middleware_test.go`:
  - `TestRequestLogJSONContract`, with success, 4xx and 5xx subtests. Uses the real `logger.NewWithWriter` with `local` env. The request carries `X-Api-Key: sk_test_secret_value`, `Authorization: Bearer eyJhbGciOi.secret.sig` and body `{"password":"hunter2"}`. Every line must parse as JSON, contain `"request_id":` exactly once and `"ip":` at most once, have no duplicate keys (checked on the token stream), contain none of the secrets, and have no `time` key.
  - `TestRequestLogUserIDOnce`, covering identity set before EnhanceContext and identity set by route-level auth.
  - `TestRequestLogTraceID`, which checks the fixed 32-hex trace_id and span_id with a span context, and no trace keys without one.

## Verification (run for real)

| Command | Start-End (UTC) | Exit |
|---|---|---|
| RED: `go test ./internal/logger/... ./internal/middleware/... ./internal/config/...` | 01:03:02-01:03:03 | 1 (only the 7 new tests failed) |
| GREEN: `go test -race ./internal/logger/... ./internal/middleware/... ./internal/config/... ./internal/database/... && go build ./... && go mod tidy -diff && golangci-lint run ./internal/logger/... ./internal/middleware/... ./internal/config/...` | 01:04:44-01:04:51 | 0 |
| `go tool -modfile=tools.mod golangci-lint run ./...` | 01:04:30-01:04:35 | 0 (0 issues) |
| `... golangci-lint run --build-tags=integration ./...` | 01:04:35-01:04:40 | 0 (0 issues) |
| `make test` (go test -race ./..., web vitest 170 passed, generator pytest 262 passed) | 01:04:57-01:06:44 | 0 |

RED causes were confirmed by a probe run (production env, not committed):
- the API line had `request_id`, `ip`, `method` and `user_id` twice and a `time` key, and no `trace_id`
- in the local env, every line was ConsoleWriter output and failed to parse

Acceptance greps: `ConsoleWriter` appears only in the console branch (logger.go:48). No `localEnv`, `Timestamp()`, `TimestampFieldName` (under backend/internal) or `Str("request_id"` in global.go.

`make lint` was not run as a whole. Its Go part (golangci-lint, both tag sets) was run. Web and generator lint were not run because this plan touched no files there.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Correctness] Removed duplicate `method` key**
- **Found during:** Task 1 RED probe
- **Issue:** RequestLogger re-added `method`, which the context logger already carries. That duplicate key breaks forensics (T-05-06-03), the same as the duplicate request_id/ip.
- **Fix:** Dropped `Str("method", ...)` and `LogMethod` from RequestLogger. Added a generic duplicate-key assertion to the contract test.
- **Files:** backend/internal/middleware/global.go, middleware_test.go
- **Commits:** 69331e1, 55771a6

**2. [Rule 3 - Blocking] goconst on the "json" literal**
- **Issue:** The new Validate case made "json" appear 3 times. One of them is in the Phase 4-owned config.go:518, which was not edited.
- **Fix:** Added exported `config.LogFormatJSON` and `config.LogFormatConsole`, used in observability.go and logger.go.
- **Commit:** 55771a6

**3. [Dependency note] go.opentelemetry.io/otel/log v1.47.0 (indirect) added by `go mod tidy`**
- It is a `require` of the approved go.opentelemetry.io/otel@v1.47.0. It is not imported by Sentinel code and is not compiled into binaries; the module graph needs it.
- `go list -m -json` Origin: https://github.com/open-telemetry/opentelemetry-go, Subdir log, commit 66cfc9520e205b7d450183532772401bc2b6674c, tag log/v1.47.0. This is the same repository and commit as the approved otel modules.
- It requires go 1.26.0, which equals backend/go.mod `go 1.26.0`.
- `otlptrace` was NOT installed by this plan, so its Origin is still unverified.

## Threat Flags

None. No new endpoints or trust boundaries were added. The logger never reads headers, query or body (T-05-06-01 is enforced by the test).

## Known Stubs

None. trace_id is absent, not faked, until 05-10 installs the tracer provider and otelhttp.

## Self-Check: PASSED

- 69331e1 and 55771a6 are present in `git log`.
- All modified files listed above exist.
- STATE.md and ROADMAP.md were not touched, as instructed.
