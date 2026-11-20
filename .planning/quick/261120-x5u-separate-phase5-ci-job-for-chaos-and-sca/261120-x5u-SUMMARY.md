---
quick_id: 261120-x5u
description: Separate phase5 CI job for chaos and scale integration families
date: 2026-11-20
status: complete
decision: "Separate Phase 5 job" (user, verbatim)
---

# Quick Task 261120-x5u: Separate Phase 5 CI job

## Problem

Hosted CI run 44738994392 (mirror chocopie247/Sentinel) failed only in `integration`:
`make test-integ` hit `panic: test timed out after 32m0s` (package FAIL at 1947 s) while
TestTopicRetentionOverridesReapply was running. No test failed. Phase 5 added about 3,700
lines of integration tests to backend/test after 7b15a68.

## Change

- `scripts/check-phase5-tests.sh` is the single list of moved tests. It has groups `chaos`
  and `scale` plus `--groups`, `--names <group>` and `--skip-regex`. Each group checks that
  every named test exists (`go test -list ^Name$`) before running and that each one prints
  its own `--- PASS:` line afterwards.
  - chaos: `CHAOS_FAMILIES=chaos-broker,chaos-clickhouse,chaos-shutdown,slow-consumer,overload`
    through `scripts/check-chaos.sh`. This uses only testcontainers families, so the
    compose project guard does not run. It keeps the resource gate, `-race` and the family
    ceilings 8m/10m/10m/10m/6m.
  - scale: the 7 events_scale tests and the 3 detector_startup_race tests, as one
    anchored `go-test-nonempty.sh` run with `-tags=integration -timeout=20m -p=1`.
- Makefile: `PHASE5_SKIP` comes from `--skip-regex` and fails closed through
  `$(error)`. `test-integ` adds `-skip '$(PHASE5_SKIP)'`. The 32m ceiling is unchanged.
- CI: new `phase5` job (ubuntu-24.04, needs go, fail-fast false, matrix include
  chaos/30m and scale/30m, checkout and setup-go pinned as in integration). It has no
  dev stack because both groups use testcontainers only. Guard step added to the python job.
- Guards: new `scripts/test_phase5_guards.py` (15 tests). The pinned Makefile line in
  `test_phase4_guards.py` was updated on purpose. The phase3 closed policy and all
  existing budgets are unchanged.

## Measured durations (local, 16 cores; hosted 4 vCPU is slower)

| Group | Tests | Wall clock |
| --- | --- | --- |
| chaos (check-chaos.sh, -race, 5 TestMain starts) | 8 | 208 s (families 30-49 s each) |
| scale (runner end to end) | 10 | 223 s (DetectorConcurrentStartup 91 s, ListEventsOracleEquivalence 86 s) |

Not moved, because they would not relieve the aggregate: tracing (1.6 s total), the
OpenAPI drift/runtime tests (under 1 s), and TestDetectorCapacity (7 s, which also runs
in phase3 `core`).

## Verification

- All `scripts/test_*guards*.py` pass. `make -n test-integ` renders the skip.
- `go test -tags=integration -list` with the skip regex lists 18 of the 122 top-level
  tests. That set is identical to the union of the group selectors, with no duplicates.
- A real `-run '^(TestChaosOverloadBounded|TestDetectorCapacity)$' -skip <regex>` ran
  only TestDetectorCapacity.
- The full integration suite was not run locally. Hosted confirmation is pending a push
  to the mirror.
