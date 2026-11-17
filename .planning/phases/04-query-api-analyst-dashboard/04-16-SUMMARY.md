---
phase: 04-query-api-analyst-dashboard
plan: "16"
subsystem: acceptance
tags: [ci, guards, phase4-runner, playwright]
requires:
  - phase: 04-15
    provides: TestPhase4TenantMatrix, real-proxy browser acceptance
provides:
  - scripts/check-phase4-tests.sh (closed eight-family runner, fail fast, per-family durations)
  - scripts/test_phase4_guards.py (15 guard tests, orchestration MODEL)
  - CI jobs phase4 (8-way matrix), phase4-browser, and a required guard step in the python job
  - make test-phase4
affects: []
key-files:
  created: [scripts/check-phase4-tests.sh, scripts/test_phase4_guards.py]
  modified: [.github/workflows/ci.yml, Makefile, docs/operations.md, docs/architecture.md, docs/threat-model.md, .planning/phases/04-query-api-analyst-dashboard/04-VALIDATION.md]
requirements-targeted: [AUTH-01, AUTH-02, AUTH-03, AUTH-04, API-01, API-02, API-03, API-04, API-05, API-06, API-07, API-08, LIVE-01, LIVE-02, LIVE-03, UI-01, UI-02, UI-03, UI-04, UI-06]
status: tasks 1-2 done locally; task 3 (publication and hosted CI) pending user approval
completed: 2026-11-13
---

# Phase 4 Plan 16: Closed Phase 4 runner and CI coverage Summary

**Update 2026-11-16: all three tasks are done. Task 3 passed on the mirror (hosted run 43714363178, 19/19 green; see the Task 3 section at the end and 04-25-SUMMARY). Phase 4 was re-verified as passed (04-VERIFICATION, cdae189).** The original note follows: Tasks 1-2 were done locally, and Task 3 was pending approval.

## Commits

| Role | SHA |
|---|---|
| RED (tests only) | `08515a9` |
| GREEN | `f76120c` |
| Validation note | `38a76c5` |

## Honest RED record

`python3 -B -m unittest discover -s scripts -p test_phase4_guards.py` exited 1 at UTC 2026-11-13 18:27:22 (15 failures, 60 errors). The causes are absence: the runner, the CI jobs, the Makefile alias and the doc text did not exist. This is not a behavioural RED of production code. The native families already passed in 04-13..15, so there is no native RED. The orchestration tests use a fake `go` (MODEL) and prove selection and exit propagation only. Evidence: `.local/phase4-acceptance/04-16/red/`.

## What was delivered

- Runner groups: auth, alerts, events, triage, summary, delivery, stream, tenant, all. Each runs the exact `^TestPhase4X$` through `go-test-nonempty.sh` with `-tags=phase4 -race -p=1`. Ordinary families get 6m, delivery and stream 10m. Invalid or extra arguments exit 2 before any Go command. `all` stops at the first failure.
- Guards also mutate the runner and the CI text. Mutants that drop a family, widen a selector, remove race or the tag, relax a budget, mask a failure, skip a job or step, add `continue-on-error`, add `|| true`, or place a field after separator comments are all rejected.
- CI: 8 independent `phase4` matrix jobs, a `phase4-browser` job (Node, Playwright Chromium, generator env), and the guard step in the `python` job. Each job is capped at 45 minutes. The legacy integration job, Phase 3 matrix (44 core + 2 scenario selectors), compose-smoke and their caps are unchanged. The phase4 tag keeps `make test-integ` unpolluted (asserted by a guard).
- `make test-phase4`, plus the operations, architecture and threat-model docs (T04-35/36). 04-VALIDATION.md has a short local-status note.

## Gates (real commands, UTC 2026-11-13, HEAD f76120c; evidence in `.local/phase4-acceptance/04-16/gates2/`)

The first pass at 18:31-18:35 was interrupted by a machine restart before `make test` completed. Every gate below was re-run after the restart on the restarted stack.

| Command | Start-End | Exit |
|---|---|---|
| `python3 -B -m unittest discover -s scripts -p test_phase4_guards.py` | 21:21:11-21:21:17 | 0 (15 tests) |
| `bash scripts/check-phase4-tests.sh all` (launcher, NEW project, detector group p4r3) | 21:21:17-21:23:01 | 0 (eight families, each PASS, 103s) |
| `bash scripts/check-phase4-browser.sh` (launcher, NEW project) | 21:23:01-21:24:35 | 0 (13/13 Playwright) |
| `make test` | 21:24:35-21:25:26 | 0 (web, generator 121) |
| `make lint` (pinned `go tool -modfile=tools.mod golangci-lint`, web lint+typecheck, ruff, mypy) | 21:25:26-21:26:16 | 0 |
| `test_phase3_guards.py`, `test_stack_config.py`, `test_docker_context.py`, `test_go_dependencies.py`, `test_dev_auth_env.py`, `check-pins.sh`, `check-makefile-dotenv.sh` | earlier in the session | all 0 |

Family durations (s): Auth 15, Alerts 6, Events 7, Triage 10, SummaryRules 6, Delivery 15, Stream 37, TenantMatrix 7. These are local measurements on a warm Go cache, not performance claims.

## Not run (with exact reason)

- **`make test-integ` and `bash scripts/check-phase3-tests.sh`: NOT RUN.** Both select `TestDetectionScenariosPreservedRepeat` (and the scenario group). That test runs `scripts/seed.py` and `make seed-scenarios`, which call `make dev-up` against the old `sentinel` compose project with root `.env` ports (15432, 8080 and so on). The run constraints forbid acting on that stack, and narrowing the selector would weaken the gate. Verdict for these gates: not run, not PASS. They need a decision on the old stack, or a hosted run.
- Hosted CI and push: not done (Task 3).
- Full-history secrets scan (gitleaks) was not run locally.

## Deviations / findings

1. **[Rule 1] Guard fixture fixes during GREEN.** The CI mutation anchors were not unique against the Phase 3 job (two matches), and a missing-step lookup raised `IndexError` instead of `AssertionError`. I scoped the anchors to the phase4 job and made the missing-step case an assertion. The fix is in `f76120c`.
2. **Hosted `make dev-up` blocker (pre-existing, not fixed).** The base `deployments/compose/docker-compose.yml` does not wire the rules file that query-api needs (`SENTINEL_DETECTOR.RULES_PATH`, required since 04-10). On a hosted runner `make dev-up` will fail at query-api health, which affects the existing integration/phase3/smoke jobs too and all new phase4 jobs. The base file is not owned by this plan; it is a two-line fix that should be approved before Task 3. Documented in operations.md and threat-model.md.
   **Update 2026-11-14:** fixed in the base compose file (query-api now mounts `config/rules.yaml` and sets `SENTINEL_DETECTOR.RULES_PATH`, same as the detector); base and acceptance-overlay configs render, `check-pins.sh images`, `test_stack_config.py` and `test_phase4_guards.py` pass. Not yet proven by a real `make dev-up` run (that targets the old `sentinel` stack) or by hosted CI.
3. The phase4 jobs and the browser job each start the full stack, so the jobs are heavy. The 45-minute caps have not been measured on hosted runners.

## Limits

No hosted evidence, no whole-phase verification, no independent final review in this run. No exactly-once or performance claim. Consumer lag and System remain Phase 5. STATE.md and ROADMAP.md were deliberately not touched (orchestrator-owned).

## Task 3

Superseded. See the "Task 3 (hosted CI), 2026-11-16" section below.

## Self-Check: PASSED

Files exist and commits `08515a9`, `f76120c` exist. Only `.planning/config.json` and `.serena/` remain dirty and were never staged.

## Task 3 (hosted CI), 2026-11-16

Done on the mirror rather than origin (origin is billing-locked). Run [43714363178](https://github.com/chocopie247/Sentinel/actions/runs/43714363178) at `7b15a68` is green for all 19 jobs: all 8 phase4 groups, phase4-browser, phase3 core and scenarios, integration, compose-smoke and the lightweight jobs. The integration job needed one failed-job re-run. The full table and iterations are in 04-25-SUMMARY.md. Deviation 2 above (the base compose rules path) is proven on hosted runners by compose-smoke and the phase4 jobs.
