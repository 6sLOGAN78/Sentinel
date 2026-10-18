---
phase: 03-detection-alert-pipeline
plan: "03"
subsystem: detection
tags: [go, kafka, port-scan, event-time, exact-cardinality, tdd]
requires:
  - phase: 03-15
    provides: Strict finite startup rules loader and AUTH characterization guards
  - phase: 03-16
    provides: Bounded Kafka publication and metadata-aware detector checkpoints
provides:
  - Exact bounded PORT_SCAN detection at twenty distinct ports per tenant/source/destination/minute
  - Frozen unique-event evidence and rule-isolated shared window state
  - Default-binary real Kafka SCAN acceptance with canonical payload/key and fired-state pin
affects: [03-04, 03-05, 03-07, 03-09, 03-19]
tech-stack:
  added: []
  patterns: [exact port set bounded by unique-event admission, rule-qualified window keys, shared frozen evidence]
key-files:
  created:
    - backend/internal/detection/scan.go
    - backend/internal/detection/scan_test.go
    - backend/test/detector_scan_test.go
  modified:
    - backend/internal/detection/reducer.go
    - backend/internal/detection/config.go
    - backend/internal/detection/loader_test.go
    - config/rules.yaml
key-decisions:
  - "03-03 uses a rule-qualified shared window key and exact port set; distinct-port threshold and accepted unique-event evidence count remain separate."
  - "03-03 enables SCAN startup configuration only alongside implemented AUTH; durable cross-window cooldown remains the later 03-19 obligation."
patterns-established:
  - "A duplicate event ID cannot introduce another port; repeated ports with fresh IDs still contribute to frozen event_count and evidence."
requirements-completed: [DET-02, DET-07, DET-09]
duration: 6 min
completed: 2026-10-19
---

# Phase 3 Plan 3: Exact Port Scan Detection Summary

Twenty distinct destination ports in one event-time minute now produce one canonical PORT_SCAN alert, with frozen source-order evidence and bounded shared replay state.

## Performance

- Started: 2026-10-18T22:18:32Z (2026-10-19 local).
- Implementation and verification completed: approximately 2026-10-18T22:24:15Z.
- Duration: approximately 6 min, excluding tracking bookkeeping.
- Tasks: 2/2.
- Application/test/config files changed: 7.

## Accomplishments

- SCAN requires `net.connection`, two valid IP addresses and a present destination port in 0..65535. It groups by tenant/source/destination and the epoch-aligned half-open 60-second window. Null or malformed grouping and other event types create no scan window.
- An exact port set shares the existing bounded event-ID admission path: at most 4096 accepted unique IDs and therefore at most 4096 distinct ports per window. Conservative tracked usage includes the added port entries. No unexpired evidence is evicted.
- Threshold cardinality is separate from event_count. The first positive repeated-port fixture emits at 20 ports after 24 accepted unique events, retaining the first 20 unique IDs in source order. Duplicate IDs cannot contribute a new port. Later input leaves emitted canonical bytes unchanged.
- Shared window keys now include rule ID so an AUTH principal equal to the scan destination IP cannot merge rule state. AUTH canonical evidence and checkpoint semantics remain covered by passing regressions. Fired SCAN windows retain their first source pin until event-time expiry, and replay reconstructs identical first-hit bytes.
- Checked-in SCAN is enabled at version 1, medium severity, threshold 20, window 60s and configured cooldown 30s. Configured cooldown is not a claim of implemented cross-window suppression; plan 03-19 owns the durable decision path.
- Real Kafka acceptance uses dedicated topics/group, an absolute private copied rules path, shared real dependency fixtures and the default production detector binary. It observes processed_next before accepting the 19-port negative, then checks the full IPv6 alert, canonical key and fired state pin.

## Task Commits

1. Task 1: Expose scan threshold, predicate and real publication gaps — `c263f3b` (`test`).
2. Task 2: Run exact port-scan detection from versioned rules — `4419bbe` (`feat`). Includes surgical test helper/line-wrap cleanup required by static checks.

## Verification Evidence

All commands ran from the repository root unless a directory is shown. Every required named family was run independently with the nonempty runner. Durations below are actual Go package/test timings where emitted; finite command ceilings are not performance benchmarks.

| Command | Actual result |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestRulesLoaderScan$' -timeout 60s` before minimal loader allowance | Assertion RED, exit 1, package 0.005s: enabled SCAN was rejected. Unknown IDs and unimplemented FLOOD/DENY remained rejected. This startup failure was not counted as detector acceptance RED. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestScanThresholds$' -timeout 60s` before SCAN implementation | Assertion RED, exit 1, initial package 0.006s; final expanded RED 0.014s. Missing alerts at 20/21 ports, fresh twentieth-port ID and repeated-port evidence crossing; missing shared event-bound enforcement also failed. Nineteen-port negatives passed. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestScanPredicates$' -timeout 60s` before SCAN implementation | Assertion RED, exit 1, final expanded package 0.007s. Missing positive alerts for rule/tenant/source/destination isolation, IPv6 and next-minute crossing. Null/wrong-type negative cases passed. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineScan$' -tags=integration -timeout 6m -p 1` before SCAN implementation, after loader allowance | Genuine absent-output RED, exit 1, test 22.88s / package 40.645s. Default binary started, Kafka source publication and processed_next=23 succeeded, then the twentieth-port alert assertion at detector_scan_test.go:97 failed. No missing import/type or setup failure counted as RED. |
| Same independent threshold selector after implementation/helper cleanup | GREEN, exit 0, final non-race package 0.019s. Includes 19/20/21 ports, >=20 IDs on 19 ports, duplicate IDs, 4096-ID admission refusal without eviction, first crossing/evidence bytes, fired pin, replay and expiry. |
| Same independent predicate selector after implementation/helper cleanup | GREEN, exit 0, final non-race package 0.007s. Includes malformed/null fields, disabled SCAN, rule and tenant/source/destination separation, IPv6, ports 0/65535 and half-open minute boundary. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestAuth' -timeout 60s` | GREEN, exit 0, final package 0.006s. Both AUTH threshold and canonical evidence families passed. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestRulesLoaderScan$' -timeout 60s` after allowance | GREEN, exit 0, final package 0.006s. Only implemented AUTH/SCAN can be enabled; unknown and unimplemented IDs still rejected. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestRulesLoaderShape$' -timeout 60s` | GREEN, exit 0, final package 0.009s. Strict fields, single document, duplicates, shape and byte limits preserved. |
| Same independent real SCAN command after implementation | GREEN, exit 0, test 5.11s / package 23.875s. Exact alert/key, event_count=24 on 20 distinct ports, first 20 IDs, created_at=end, fired pin=0 and no second alert at port 21. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` | GREEN, exit 0, test 4.53s / package 27.603s. Real default-binary AUTH publication/checkpoint/evidence preserved. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestScanThresholds$' -race -count=10 -timeout 60s` | GREEN, exit 0, package 1.398s; ten repetitions after helper cleanup. |
| `scripts/go-test-nonempty.sh './internal/detection/...' '^TestScanPredicates$' -race -count=10 -timeout 60s` | GREEN, exit 0, package 1.175s; ten repetitions after helper cleanup. |
| `timeout 300s make test` | GREEN, exit 0: full Go race suite (detection package 1.233s), web 6/6 and generator 99/99 (0.72s). Test-only helper/line-wrap cleanup afterward was covered by the independent selectors/race repetitions above. |
| `cd backend && timeout 300s go build ./...` | GREEN, exit 0 within ceiling; all packages/binaries compile. |
| `timeout 300s make lint` | Initial exit 2: new test complexity and line lengths; final exit 0 after focused helper/line-wrap cleanup. Default Go lint reported 0 issues; web lint/typecheck and Python ruff/format/mypy passed. |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags integration ./...` | Initial exit 1: three new fixture line lengths plus known recovery warning. Final exit 1 after wrapping the new lines: only pre-existing `test/recovery_test.go:162` unparam remains. No clean integration-lint claim. |
| `cd backend && timeout 120s go vet -tags=integration ./test/...` | GREEN, exit 0 (command wall time 0.297s). |
| `git diff --check`, per-task tracked deletion checks and scoped stub scan | Passed; no tracked deletions or unfinished stubs in the changed slice. Existing contracts/goldens/dependency pins unchanged. |

## Decisions Made

- Add rule identity to the existing window key and share admission/evidence/expiry rather than creating parallel dedupe/checkpoint state. This avoids AUTH/SCAN key collisions and preserves replay behavior.
- Count exact distinct ports for triggering while preserving all accepted unique matching events in event_count. Port-set growth cannot exceed the existing unique-ID limit.
- Keep configured cooldown data intact while leaving durable cross-window suppression to the explicitly scheduled 03-19 implementation and proof.

## Deviations from Plan

### Auto-fixed Issues

1. **[Rule 3 - Blocking] Permit the newly implemented SCAN rule in the extracted loader.**
   - Found during Task 1 startup preparation: config.go rejected all enabled non-AUTH rules, preventing the required real SCAN test from reaching its assertion.
   - Parent approved ownership extension to config.go/loader_test.go. A focused loader RED preceded the one-condition allowance; genuine missing-SCAN-output RED followed successful startup, before reducer implementation.
   - Fix: permit only AUTH and SCAN, preserving strict known IDs/shape/versions/caps and rejection of unimplemented FLOOD/DENY.
   - Files: backend/internal/detection/config.go and loader_test.go.
   - Verification: independent loader RED/GREEN, loader shape and real SCAN RED/GREEN above.
   - Commits: `c263f3b` tests; `4419bbe` allowance/feature.

Total deviations: one necessary blocking prerequisite, no architectural expansion.

## Issues Encountered

- Default/integration static checks required splitting the new nested test scaffolds into focused helpers and wrapping long lines. The assertions and named selectors remained intact and passed again.
- One formatting/lint invocation used a root-relative path while explicitly running from backend and exited 2 (`stat backend/test/detector_scan_test.go`); the immediately corrected backend-relative invocation ran the full integration lint and isolated the known warning.
- No authentication gates or package installations occurred. No user configuration, .serena files, volumes or remote resources were changed.

## Deferred Issues and Remaining Obligations

- The existing integration lint warning at backend/test/recovery_test.go:162 (`requireOutputEventually` text parameter always receives `service started`) remains out of scope and assigned to 03-14.
- Native store extraction 03-17 and PostgreSQL detector prerequisite 03-18 remain Wave 3 work after this plan. Remaining predicates, all-restart-cut/crash/rebalance proofs, configuration handover/state-cap refinements, poison paths, durable cross-window cooldown, scheduled preserved-state scenarios and phase-wide security/full integration/CI verification are pending their plans.
- `requirements-completed` copies this plan's DET-02/DET-07/DET-09 IDs for traceability of this contribution. It does not certify all-rule evidence/fixture requirements or close global requirement checkboxes; no phase.complete operation is authorized here.
- The existing at-least-once transport/replay model is retained. This plan proves the scoped SCAN slice, not complete Phase 3 recovery or cooldown correctness. Stop before Phase 4 remains binding.

## User Setup Required

None. Existing real container fixtures provision test-owned dependencies automatically.

## Next Phase Readiness

The exact SCAN predicate/cardinality feature and its real Kafka acceptance are ready for subsequent rule, evidence, recovery and cooldown plans. Phase 3 remains in execution; this plan adds its sixth committed summary out of 21.

## Self-Check: PASSED

All three created artifacts exist; task commits c263f3b and 4419bbe exist in history. Both tasks meet their scoped acceptance criteria, independently nonempty assertion RED/GREEN is recorded, scoped static issues are repaired, and the remaining pre-existing integration-lint failure is explicitly deferred above. No incomplete stub prevents the SCAN objective.
