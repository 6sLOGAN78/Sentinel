---
phase: 03-detection-alert-pipeline
plan: "04"
subsystem: detection
tags: [go, kafka, event-time, flood, deny, tdd]
requires:
  - phase: 03-03
    provides: Rule-qualified exact windows and live port-scan detection
  - phase: 03-18
    provides: Real bounded detector PostgreSQL dependency and integration fixtures
provides:
  - Exact REQUEST_FLOOD and DENY_SPIKE predicates over shared unique-event state
  - Source-only canonical volume identities and frozen first-crossing evidence
  - Four enabled built-ins with strict startup loading and real Kafka acceptance
affects: [03-05, 03-07, 03-08, 03-09, 03-19, 03-13, 03-14]
tech-stack:
  added: []
  patterns: [Shared exact counters and replay pins, source-only volume identity]
key-files:
  created:
    - backend/internal/detection/volume_test.go
    - backend/test/detector_volume_test.go
    - backend/internal/detection/volume.go
  modified:
    - backend/internal/detection/reducer.go
    - backend/internal/detection/config.go
    - backend/internal/detection/loader_test.go
    - config/rules.yaml
key-decisions:
  - "03-04: Enable exactly all four implemented built-ins; FLOOD/DENY reuse shared windows with source-only DedupeKey components."
  - "03-04: Cross-window alert suppression remains the durable 03-19 obligation; configured durations and one-per-window firing are not that proof."
requirements-completed: [DET-03, DET-04, DET-07, DET-09]
duration: 5 min
completed: 2026-10-18
---

# Phase 3 Plan 4: Exact Flood and Deny Detection Summary

REQUEST_FLOOD emits at 300 unique net.request events per minute and DENY_SPIKE at 30 unique policy.decision/action=deny events per two minutes, preserving canonical identities, frozen evidence and replay pins.

## Performance

- Started: 2026-10-18T22:45:46Z
- Completed implementation verification: 2026-10-18T22:51:10Z (2026-10-19 local date)
- Duration: approximately 5 minutes
- Tasks: 2/2
- Implementation/test/config files: 7

## Accomplishments

- Added exact count predicates with valid, non-null source grouping. DENY requires the exact action `deny`; unrelated outcome text never changes that decision. AUTH source/principal and SCAN source/destination/distinct-port behavior remain intact.
- Reused shared unique-ID counting, half-open epoch-aligned windows, source-order evidence capped at 20, event-time window-end created_at, freeze at first crossing, fired-state pins and replay. Volume IDs pass a one-component source slice to the unchanged contracts.DedupeKey.
- Enabled version-1 FLOOD high/300/60s/cooldown30s and DENY medium/30/120s/cooldown60s. All four definitions load at startup, while unknown IDs, shapes, fields, documents, bounds, severities and version checks remain enforced.
- Real Kafka acceptance proves no alert at 299/29 including duplicate IDs, exactly the two expected volume IDs at 300/30, exact samples/counts/windows/keys, and no repeat at 301/31. Isolated AUTH=9 and SCAN=19 controls cannot create unexpected cross-rule hits. Real AUTH and SCAN positive regressions also passed with all four configured rules enabled.

## Task Commits

1. Task 1 — expose exact flood/deny and cross-rule behavior: `8f3e11f` (test).
2. Task 2 — emit enabled flood/deny alerts: `ecae098` (feat).

The RED commit precedes the feature commit. No tracked files were deleted. The original dirty `.planning/config.json` and untracked `.serena/` were preserved and excluded from staging.

## RED Evidence

Each command was run independently before production changes; failures were assertions after successful compilation, rather than missing symbols or setup failures.

| Command | Exit | Observed result |
|---|---:|---|
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestFloodThresholds$' -timeout 60s` | 1 | 299 passes; expected one alert at 300/301, received zero; package 0.015s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestDenyThresholds$' -timeout 60s` | 1 | 29 passes; expected one alert at 30/31 and with deny/outcome variants, received zero; package 0.010s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestRulesLoaderVolume$' -timeout 60s` | 1 | Existing temporary implementation-not-enabled restriction rejects known enabled volume rules; package 0.003s |

The real acceptance file existed in Task 1 and compiled. `cd backend && go test ./test/... -tags=integration -run '^$' -timeout 6m -p 1` exited 0 (25.803s), proving only compilation and real TestMain setup, not a behavior-family pass. Integration behavior was verified in GREEN below.

## GREEN Verification

All commands below were actually executed. Named families use separate nonempty runner invocations; no broad selector substitutes for an individual family.

| Command | Exit | Result / duration |
|---|---:|---|
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestFloodThresholds$' -timeout 60s` | 0 | Threshold/duplicates/predicates/isolation/boundary; 0.013s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestDenyThresholds$' -timeout 60s` | 0 | Same plus action/outcome combinations; 0.010s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestFloodThresholds$' -timeout 60s` | 0 | Canonical source-only identity, frozen evidence and replay; detection 0.016s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestDenyThresholds$' -timeout 60s` | 0 | Canonical evidence/replay and exact deny predicate; detection 0.009s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestScan' -timeout 60s` | 0 | Both SCAN families; detection 0.021s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestAuth' -timeout 60s` | 0 | AUTH threshold and canonical evidence; detection 0.004s |
| `scripts/go-test-nonempty.sh './internal/detection/... ./internal/contracts/...' '^TestDedupeKey' -timeout 60s` | 0 | Unchanged golden identity/normalization/collision families; contracts 0.007s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestRulesLoaderVolume$' -timeout 60s` | 0 | All enabled known IDs load; unknown enabled ID rejected; 0.003s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestRulesLoaderScan$' -timeout 60s` | 0 | Updated implemented-rule compatibility, unknown-ID rejection preserved; 0.006s |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestRulesLoaderShape$' -timeout 60s` | 0 | Strict fields/documents/shapes/duplicate mappings and byte cap; 0.008s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineVolume$' -tags=integration -timeout 6m -p 1` | 0 | Test 3.03s; full real-container package 21.647s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineVolume$' -tags=integration -race -timeout 6m -p 1` | 0 | Strengthened final test 3.02s; package 22.521s; TestMain still builds default service binaries |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` | 0 | Test 4.52s; package 26.134s |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineScan$' -tags=integration -timeout 6m -p 1` | 0 | Test 5.08s; package 26.521s |
| `timeout 300s make test` | 0 | Final run 9.26s: backend race suite, web 6 tests, generator 99 tests |
| `timeout 300s make lint` | 0 | Final serial run 4.41s: backend zero issues, web lint/typecheck, generator ruff/format/mypy |
| `cd backend && timeout 300s go build ./...` | 0 | Final build 2.99s |
| `cd backend && timeout 300s go vet ./...` | 0 | Default vet passed |
| `cd backend && timeout 300s go vet -tags=integration ./...` | 0 | Final integration vet 0.85s |
| `cd backend && timeout 300s go tool -modfile=tools.mod golangci-lint run --build-tags integration ./...` | 1 | Only pre-existing recovery_test.go:162 unparam remains; deferred to 03-14 |
| `git diff --check` | 0 | No whitespace errors |

Combined package-list commands naturally print no-tests warnings for the package that does not own the selected family; each selected family itself ran and passed. All integration resources are dedicated test fixtures; no user volumes, keys, topics or running services were reset.

## Decisions Made

Enable exactly the finite known built-ins now that all predicates exist. Volume alerts share the existing reducer state and use source-only canonical components. Keep the configured cooldown durations, with actual durable cross-window allow/suppress behavior and its recovery proof reserved for 03-19 and 03-20.

## Deviations from Plan

### 1. [Rule 3 - Blocking] Remove the temporary two-rule loader restriction

- Found during: Task 1 prerequisite inspection and separate loader RED.
- Issue: config.go still rejected enabled REQUEST_FLOOD/DENY_SPIKE, so enabling the planned startup definitions could never reach the reducer.
- Fix: Removed only that obsolete temporary restriction after RED. Existing four-known-ID membership and all shape/size/version/value checks remain. Updated TestRulesLoaderScan's obsolete disabled-only expectations and added TestRulesLoaderVolume with unknown-enabled-ID rejection.
- Files: backend/internal/detection/config.go and loader_test.go (narrow extension preapproved by parent).
- Verification: Independent loader assertion RED then GREEN; strict shape suite and full race suite pass.
- Commits: `8f3e11f`, `ecae098`.

One scope deviation. No new package, storage, public API, hash function, eviction policy or transport was added.

## Issues Encountered

Initial lint identified gocognit in the added dispatch path and larger test helper. Split time admission from rule dispatch and divided tests into behavioral helpers; default lint now passes. Integration lint additionally caught one introduced long line, which was wrapped; its only remaining warning is the pre-existing recovery helper issue. One simultaneous lint invocation hit golangci-lint's shared execution lock (make exit 2, 5.71s); the final serial run passed. These intermediate failures are retained here rather than reported as clean runs.

## Deferred Issues and Remaining Phase Obligations

- Existing test/recovery_test.go:162 unparam remains for 03-14; no unrelated code was changed.
- DET-08 cross-window suppression is not implemented or certified by this plan. Window firing and configured cooldown durations cannot substitute for the user's durable alert-level semantics; 03-19/03-20 remain required.
- Boundary/lateness refinement, adversarial reconstruction, configuration handover, aggregate capacity, storage/poison/crash proofs, preserved-state scenario scheduling, full integration/Compose/security/CI gates remain with their later plans. The requirement IDs above identify this plan's slice; no overall requirement checkbox or Phase 3 completion was changed.
- Stub scan found no placeholder implementation preventing this plan's stated goal. Threat scan found no new endpoint, authentication path, file access boundary or schema surface outside the plan's registered predicate/counter and immutable-evidence boundaries. No new high threat was discovered in this slice; this is not phase-wide security sign-off.

## User Setup Required

None.

## Next Phase Readiness

03-04 supplies all four exact predicates for 03-05 and subsequent cooldown/recovery work. Phase 3 advances from 8 to 9 committed plan summaries out of 21, after this summary is committed. Phase 4 remains out of scope.

## Self-Check: PASSED

All seven implementation/test/config files exist, both task commits resolve as commits, named RED/GREEN evidence and actual verification exits are recorded, and whitespace validation passed before committing this summary. No missing artifact or blocking stub remains for 03-04.
