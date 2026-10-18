---
phase: 03-detection-alert-pipeline
plan: "15"
subsystem: detection
tags: [go, yaml, auth, event-time, characterization, refactor]
requires:
  - phase: 03-01
    provides: Live AUTH reducer, strict startup loader and real Kafka acceptance
provides:
  - Independent AUTH threshold and canonical frozen-evidence characterization tests
  - Strict YAML shape, single-document and bounded-read tests
  - Same-package configuration extraction with unchanged loader and fingerprint behavior
affects: [03-03, 03-08, 03-16]
tech-stack:
  added: []
  patterns: [passing characterization before source extraction, strict capped YAML startup input]
key-files:
  created:
    - backend/internal/detection/auth_test.go
    - backend/internal/detection/loader_test.go
    - backend/internal/detection/config.go
  modified:
    - backend/internal/detection/reducer.go
    - backend/go.mod
key-decisions:
  - "03-15 preserves existing AUTH and strict loader behavior; passing characterization guards precede the source-only extraction."
patterns-established:
  - "Every named characterization family executes through its own nonempty selector before and after extraction."
requirements-completed: [DET-01, DET-05, DET-07]
duration: 3 min
completed: 2026-10-19
---

# Phase 3 Plan 15: Strict AUTH Loader Extraction Summary

Strict bounded YAML loading now lives in `backend/internal/detection/config.go`, protected by independently executed AUTH threshold, frozen-evidence and loader-shape tests with unchanged real Kafka acceptance.

## Performance

- Resumed execution started: 2026-10-18T22:05:33Z (2026-10-19 local).
- Task implementation completed: 2026-10-18T22:08:40Z.
- Active resumed implementation duration: 3 min; the earlier usage-limit interruption is excluded.
- Tasks: 2/2.
- Application/test/module files changed: 5; `backend/go.sum` remains unchanged.

## Accomplishments

- `TestAuthThreshold` proves nine unique failures do not alert, a duplicate does not cross the threshold, ten unique failures do, and the eleventh does not repeat. Null source, null/empty principal and wrong event type create no group.
- `TestAuthCanonicalEvidence` checks the complete first-threshold Alert for thresholds 10 and 25, event-derived canonical UTC timestamps, count, pipe-containing principal identity, source-order samples capped at 20, omitted detected_at, and unchanged emitted bytes after later matching input.
- `TestRulesLoaderShape` checks valid duration/version fields, unknown root/rule fields, malformed/wrong-shape/duplicate-key/empty YAML, nonempty/empty/null second documents, trailing comments, acceptance at exactly 64 KiB and rejection at 64 KiB + 1. An oversized reader independently proves consumption stops at the cap plus one byte.
- Rule declarations, `LoadRules`, `Fingerprint`, `Versions` and the finite rule-ID helper moved unchanged into `backend/internal/detection/config.go`; `backend/internal/detection/reducer.go` retains reducer and format-2 recovery logic. The extracted block is byte-identical to its pre-extraction source, with no duplicate Rules/LoadRules declarations.
- Offline module bookkeeping promotes the already resolved `go.yaml.in/yaml/v3 v3.0.5` from indirect to direct. No dependency version or checksum changed and no package was installed.

## Task Commits

1. **Task 1: Prove current AUTH and loader contracts** — `8dcd0be` (`test`).
2. **Task 2: Move loader from its owned source and preserve the live path** — `9dcb347` (`refactor`).

## Verification Evidence

Commands run from the repository root unless an explicit directory is shown. Every named family below had a separate nonempty runner invocation; none is inferred from a broad selector.

| Command | Actual result |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestAuthThreshold$' -timeout 60s` before extraction | PASS, exit 0; package 0.005s; all five subtests executed. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestAuthCanonicalEvidence$' -timeout 60s` before extraction | PASS, exit 0; package 0.006s; both threshold subtests executed. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestRulesLoaderShape$' -timeout 60s` before extraction | PASS, exit 0; package 0.007s; all thirteen shape cases and bounded-read assertion executed. |
| `cd backend && go tool -modfile=tools.mod golangci-lint run ./internal/detection/...` | Final PASS, exit 0, 0 issues after wrapping one new test-helper signature. |
| `cd backend && GOPROXY=off go mod tidy` | PASS, exit 0; only pinned YAML indirect-to-direct promotion in `backend/go.mod`; `backend/go.sum` unchanged. |
| Same independent `TestAuthThreshold` command after extraction | PASS, exit 0; package 0.009s. |
| Same independent `TestRulesLoaderShape` command after extraction | PASS, exit 0; package 0.007s. |
| Same independent `TestAuthCanonicalEvidence` command after extraction | PASS, exit 0; package 0.005s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` | PASS, exit 0; real Kafka test 4.55s, package including setup/cleanup 27.837s. Existing canonical payload, key, threshold, no-repeat and format-2 pin/metadata acceptance remained passing. |
| `make test` | PASS, exit 0: full backend `go test -race ./...`, web 6 tests, generator 99 tests. Existing contracts/DedupeKey tests passed. |
| `make lint` | PASS, exit 0: Go 0 issues; web ESLint/typecheck; generator ruff/format/mypy. This is default-build lint, not integration-tag lint. |
| Source-block comparison against `8dcd0be:backend/internal/detection/reducer.go` | PASS, exit 0; extracted declarations, loader, fingerprint and versions are byte-identical. |
| `rg -n '^(type Rules struct\|func LoadRules)' backend/internal/detection` | Exactly one Rules and one LoadRules declaration, both in `backend/internal/detection/config.go`. |
| `git diff --exit-code -- backend/go.sum` and `git diff --check` | PASS, exit 0. |

## TDD / Refactor Gate

The approved plan explicitly requires proving existing exports before extraction and preserving already passing behavior. All three new families passed against the original implementation; no behavior gap appeared, so no assertion RED or new feature commit was fabricated. The sequence is passing characterization commit followed by a behavior-preserving refactor commit. The original live AUTH feature's historical assertion RED/GREEN evidence remains in `.planning/phases/03-detection-alert-pipeline/03-01-SUMMARY.md`; it is not a new 03-15 RED run.

## Files Created/Modified

- `backend/internal/detection/auth_test.go`: AUTH threshold, grouping, canonical evidence and frozen bytes.
- `backend/internal/detection/loader_test.go`: strict YAML and capped-reader characterization.
- `backend/internal/detection/config.go`: extracted existing rule definitions, loader, fingerprints and versions.
- `backend/internal/detection/reducer.go`: removed moved declarations/imports; reducer behavior unchanged.
- `backend/go.mod`: same-version direct YAML requirement.
- `backend/go.sum`: read/checked, unchanged.

## Decisions Made

Followed the approved source-extraction scope and existing behavior. Kafka adapter extraction remains 03-16. Rule activation, operational cooldown, exhaustive config handover and recovery/failure proofs remain owned by later plans.

## Deviations from Plan

None in application behavior or scope. The `tdd="true"` extraction task used the plan's explicit existing-behavior characterization/refactor sequence. `backend/go.sum` needed no change after tidy, as permitted. No package installation, branch/worktree operation, remote write or user-data cleanup occurred.

## Issues Encountered

- The first tidy command also set `GOSUMDB=off`; Go rejected cached toolchain verification with exit 1 before changing modules. Retrying with only `GOPROXY=off` succeeded. This command-environment error is not a behavioral RED.
- Scoped lint initially reported one 124-character helper signature; wrapping it and rerunning yielded 0 issues before the final Task 1 commit.
- The pre-existing integration-tag `unparam` finding at `backend/test/recovery_test.go:162` was already recorded in `.planning/phases/03-detection-alert-pipeline/03-02-SUMMARY.md` for 03-14. It was not modified or rerun here, and no clean integration-lint claim is made.

## Known Stubs

None introduced or preventing this plan's loader extraction and AUTH proof goal. Modified files were scanned for placeholder/TODO/FIXME patterns. The existing disabled SCAN/FLOOD/DENY definitions and later durable cooldown/decision-frontier behavior remain intentional subsequent-plan work, not completed capabilities.

## Requirement Scope and Next Plan Readiness

Frontmatter requirement IDs provide this plan's traceability only. DET-01/DET-05/DET-07 checkboxes remain pending until the full Phase 3 evidence is complete. This focused acceptance does not claim phase-wide recovery, cooldown, full integration or hosted CI completion.

Ready for 03-16. The bounded local configuration surface is unchanged; no new endpoint, authentication path, file-access pattern or schema trust boundary was introduced. T-03-15-01 is covered by exact source equivalence plus real AUTH acceptance; T-03-15-02 is covered by the single-document and capped-read assertions. No open high finding was identified in this change.

## User Setup Required

None.

## Self-Check: PASSED

- All three created files and both modified files exist at the full artifact paths above.
- `8dcd0be` and `9dcb347` exist in history; neither deletes tracked files.
- Independent pre/post characterization, real Kafka acceptance, complete unit/race checks and default lint have actual passing exits recorded above.
- The original `.planning/config.json` and `.serena/` user changes remain excluded from the plan commits.
