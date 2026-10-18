---
phase: 03-detection-alert-pipeline
plan: "05"
subsystem: detection
tags: [go, kafka, event-time, evidence, candidates, tdd]
requires:
  - phase: 03-04
    provides: Four exact enabled predicates over shared event-time windows
  - phase: 03-17
    provides: Existing immutable storage fingerprint policy
provides:
  - Frozen canonical alert bytes and immutable source-aware first-threshold candidates
  - Independent evidence, identity, window, watermark and candidate regression families
  - Fixed AUTH/SCAN/FLOOD/DENY source ordinals independent of YAML sorting
affects: [03-19, 03-07, 03-08, 03-09, 03-20]
tech-stack:
  added: []
  patterns: [Private immutable candidate values, copy-returning canonical bytes, fixed rule evaluation order]
key-files:
  created:
    - backend/internal/detection/candidate.go
    - backend/internal/detection/evidence.go
    - backend/internal/detection/evidence_test.go
    - backend/internal/detection/candidate_test.go
    - backend/internal/detection/window_test.go
  modified:
    - backend/internal/detection/reducer.go
    - backend/internal/detector/worker.go
key-decisions:
  - "03-05: ApplySource receives actual topic/partition/offset; legacy Apply remains alert-only without fabricated candidate source."
  - "03-05: Canonical rule ordinals are AUTH=0, SCAN=1, FLOOD=2, DENY=3; evaluation order and later decision frontiers must use these indices."
  - "03-05: Candidate fingerprint matches immutable storage by clearing workflow status and detected_at before hashing; full canonical open payload is retained separately."
requirements-completed: [DET-06, DET-07, DET-08]
duration: 6 min
completed: 2026-10-18
---

# Phase 3 Plan 5: Frozen Evidence and First-Threshold Candidates Summary

Each first crossing freezes canonical evidence bytes and an immutable candidate with the triggering event time, real Kafka source position and fixed rule ordinal, ready for durable PostgreSQL decisions in 03-19.

## Performance

- Started: 2026-10-18T23:11:09Z.
- Implementation verification completed: 2026-10-18T23:16:45Z (2026-10-19 local date).
- Duration: approximately 6 minutes.
- Tasks: 2/2.
- Implementation/test files: 7.

## Accomplishments

- Added a canonical evidence builder preserving the existing alert schema, summaries, severity/version, DedupeKey component slices, first 20 unique source-ordered evidence IDs, first-crossing unique-event count, window-end created_at and omission of detected_at. SCAN's predicate counts distinct ports while its evidence counts all accepted unique matching events, including repeated ports.
- Replaced the fired boolean with retained canonical payload bytes, keeping the first source pin and dedupe state until event-time expiry. Usage accounting includes the frozen bytes. Later matching events cannot rewrite emitted payloads or recreate a window candidate.
- Added immutable Candidate values with private fields and value/copy accessors for source, offset, zero-based rule ordinal, trigger occurred_at, canonical bytes and immutable fingerprint. Mutation of returned bytes or the compatibility Alert cannot alter the candidate. Fingerprints are checked against the existing immutable storage policy.
- Added ApplySource and supplied the actual record topic/partition in the worker. The compatibility Apply method continues returning alerts without inventing a source identity. Rule evaluation itself follows AUTH/SCAN/FLOOD/DENY order and aggregates matches, independently of YAML order.
- Retained live Publish behavior pending 03-19. No in-memory cross-window eligibility or final cooldown claim was added. Separate windows still produce distinct candidates and IDs.

## Task Commits

1. Task 1 — expose evidence, boundaries and immutable candidate behavior: `400efc0` (test).
2. Task 2 — freeze canonical evidence and source-aware candidates: `f32ec36` (feat).

The RED commit precedes GREEN. Declaration-only scaffolds and Outcome.Candidates allow assertion failures after successful compilation. No tracked files were deleted. Existing dirty config and untracked .serena files were excluded from staging.

## RED Evidence

Each command was run independently before runtime implementation, with `-timeout 60s`.

| Command | Exit | Observed result |
|---|---:|---|
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestFrozenEvidence$' -timeout 60s` | 1 | AUTH and SCAN canonical candidates absent at first crossing; assertion failures, package 0.005s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestEntityIdentity$' -timeout 60s` | 0 | Existing pipe/Unicode principal and IPv6 component identity guards pass; 0.008s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestWindowBoundary$' -timeout 60s` | 0 | Existing UTC-millisecond half-open boundaries pass; 0.005s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestWatermarkLimit$' -timeout 60s` | 0 | Existing watermark equality/drop, nonmatching advancement and poison/idle guards pass; 0.005s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestCandidateFreeze$' -timeout 60s` | 1 | Missing source/trigger/ordinal/canonical candidate outputs and invalid-source rejection; assertion failures, 0.006s |

Already passing guards were retained honestly; no missing import, symbol or setup failure was counted as RED.

## GREEN Verification

Every command below was actually executed. Each named family used its own nonempty invocation; the final detection-only and combined-package selections were both rerun after the implementation/lint fixes.

| Command | Exit | Result / package duration |
|---|---:|---|
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestFrozenEvidence$' -timeout 60s` | 0 | AUTH/SCAN complete frozen payloads, >20 unique matches/repeated ports and out-of-order/repeat stability; 0.006s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestEntityIdentity$' -timeout 60s` | 0 | Pipe/Unicode AUTH and IPv6 SCAN component identity; 0.005s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestWindowBoundary$' -timeout 60s` | 0 | Exact start/end-1ms/end, UTC timestamps and distinct IDs; 0.004s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestWatermarkLimit$' -timeout 60s` | 0 | T=watermark accepted, T-1ms dropped, valid nonmatch advances, poison/idle do not; 0.006s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestCandidateFreeze$' -timeout 60s` | 0 | Source/trigger clock, immutable copies/fingerprint, ordinals, repeats/expiry and separate windows; 0.009s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestFrozenEvidence$' -timeout 60s` | 0 | Detection 0.005s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestEntityIdentity$' -timeout 60s` | 0 | Detection 0.006s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestWindowBoundary$' -timeout 60s` | 0 | Detection 0.004s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestWatermarkLimit$' -timeout 60s` | 0 | Detection 0.006s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestCandidateFreeze$' -timeout 60s` | 0 | Detection 0.004s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestDedupeKey' -timeout 60s` | 0 | Unchanged contract goldens/collisions/normalization/field sensitivity; contracts 0.010s |
| `cd backend && go test -race ./internal/detection/... ./internal/contracts/... ./internal/detector/... -timeout 60s` | 0 | Detection/contracts race pass; worker compiles, no standalone worker test files |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` | 0 | Real Kafka worker acceptance; test 4.50s, package/setup 25.326s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineScan$' -tags=integration -timeout 6m -p 1` | 0 | Real SCAN regression; test 5.08s, package/setup 31.916s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineVolume$' -tags=integration -timeout 6m -p 1` | 0 | Real FLOOD/DENY regression; test 3.16s, package/setup 29.243s |
| `timeout 300s make test` | 0 | Full Go race suite, web 6/6, generator 99/99 |
| `timeout 300s make lint` | 0 | Go 0 issues, web ESLint/typecheck, Ruff/check-format and mypy pass |
| `cd backend && timeout 300s go build ./... && go vet -tags=integration ./...` | 0 | Production build and integration-tag vet pass |
| `cd backend && timeout 300s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | 1 | Only existing test/recovery_test.go:162 unparam; deferred to 03-14 |
| `git diff --check` | 0 | No whitespace errors |

## Decisions Made

- ApplySource carries actual Kafka identity; unsourced compatibility calls have no candidate. The worker still publishes its legacy Alert list until 03-19 routes every candidate through durable PG allow/suppress decisions.
- Fixed source rule ordinals are AUTH=0, SCAN=1, FLOOD=2, DENY=3, derived from the existing canonical ruleIDs order. Future decision frontiers must preserve this numbering and evaluation order.
- The fingerprint excludes mutable workflow status and detected_at exactly as immutable storage does. Full canonical bytes separately retain status=open and the complete evidence.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Explicit runtime source plumbing**
- Found during: Task 1 interface discovery / Task 2 implementation.
- Issue: Existing Apply(offset,event) has no topic/partition, so it cannot create a truthful source-aware candidate.
- Fix: Added a compatibility-preserving ApplySource entry point and changed only the worker's reducer call to pass the actual Kafka record source. Added Outcome.Candidates as a loadable Task 1 declaration. The parent approved this narrow worker.go ownership extension before implementation.
- Files modified: detection/candidate.go, detection/reducer.go, detector/worker.go.
- Verification: Candidate source assertions, full race suite and independent real AUTH/SCAN/Volume families pass.
- Commit: `f32ec36` (declaration in `400efc0`).

No architectural/package/schema changes or installations were needed. Standard lint required scoped helper extraction, comments and formatting; the final gate passes.

## Issues Encountered

- One initial worker compile qualification error was corrected before the passing race gate. First standard lint reported introduced complexity/comments/line lengths; the next run exposed one introduced test-variable shadow. Both were fixed and final make lint exited 0.
- Existing integration lint debt at test/recovery_test.go:162 remains unchanged for 03-14. No unrelated files were repaired.
- Stub scan found no unfinished runtime scaffolds. Empty outcomes for nonmatches/repeats/late inputs and alert-only legacy Apply are intentional contracts. No new endpoints, authentication paths, schema changes or file-access surfaces were introduced beyond the plan's input/evidence boundaries.

## User Setup Required

None.

## Next Phase Readiness

- Ready for 03-19 durable decision ledger/cursor/fence implementation with immutable Candidate accessors and fixed ordinals.
- DET-08 remains globally incomplete: real cross-window suppression, allowed/suppressed replay, decision-frontier consistency and crash/ownership behavior are later 03-19/03-20 proofs. The requirements-completed frontmatter copies this plan's bindings; it does not certify full phase requirements.
- Broader checkpoint/replay/config handover, state bounds/late metric, poison prefixes, live scheduling, preserved-volume acceptance and phase security/CI remain pending. Root Wave 5 cross-plan gate remains pending.

## Self-Check: PASSED

- All five newly created files and the modified reducer/worker exist.
- Commits `400efc0` and `f32ec36` exist, in RED-before-GREEN order.
- Both task acceptance families and DedupeKey guards passed after implementation; actual verification outcomes and remaining debt are recorded above.
