---
phase: 04-query-api-analyst-dashboard
plan: "25"
subsystem: ci (hosted proof on the chocopie247/Sentinel mirror)
tags: [gap-closure, G1, G2, hosted-ci]
gap_closure: true
gaps_closed: [G1, G2]
requires: ["04-18", "04-19", "04-20", "04-21", "04-23", "04-24", "04-26"]
requirements: [AUTH-01, AUTH-02, AUTH-03, AUTH-04, API-01, API-02, API-03, API-04, API-05, API-06, API-07, API-08, LIVE-01, LIVE-02, LIVE-03, UI-01, UI-02, UI-03, UI-04, UI-06]
metrics:
  completed: 2026-11-16T08:24:50Z
  hosted_runs: 3
---

# 04-25 Summary: hosted CI proof

**Result: hosted run [43714363178](https://github.com/chocopie247/Sentinel/actions/runs/43714363178) at `7b15a68` finished with all 19 jobs green on attempt 2.**

That HEAD contains every Phase 4 gap-closure commit (04-18 to 04-24 and 04-26), plus the ADR fix `aa1d60e` and the port-allocation fix `7b15a68`. Nothing was pushed to origin; only `ci` (github.com/chocopie247/Sentinel) was pushed. The evidence is saved in `.local/ci-mirror-43714363178/run.json` (untracked).

## Iterations (recorded truthfully)

| Run | Head | Result | Diagnosis | Fix |
|-----|------|--------|-----------|-----|
| 43649750865 | b764e24 | failure | G1 (launcher env), G8 (query-api rules path in lifecycle harness), G9 (hard-coded schema version) | 04-18, 04-26 |
| 43708110897 | aa1d60e | failure, 18/19 | phase3 (core) `TestDetectorConfigRestart`: "public and ops ports must differ". Two free-port probes returned the same port | `7b15a68`: `FreePorts(n)` keeps the listeners open until all ports are chosen; `distinctOpsPort` added in detector tests; `TestFreePortsDistinct` |
| 43714363178 attempt 1 | 7b15a68 | failure, 18/19 | integration `TestDetectorCooldownOwners`: "missing actual per-process incorporated-nine checkpoint observations" (broker pins 0 within 20s). It passed 5/5 locally (~9.5s each) | none: the failed job was re-run once |
| 43714363178 attempt 2 | 7b15a68 | **success, 19/19** | | |

## Per-job table (run 43714363178, attempt 2 for integration)

| Job | Conclusion | Duration |
|-----|-----------|----------|
| go | success | 255 s |
| web | success | 47 s |
| python | success | 229 s |
| schema | success | 38 s |
| secrets | success | 6 s |
| hygiene | success | 19 s |
| compose-smoke | success | 541 s |
| integration | success | 2243 s |
| phase3 (core) | success | 1658 s |
| phase3 (scenarios) | success | 1144 s |
| phase4 (auth) | success | 358 s |
| phase4 (alerts) | success | 365 s |
| phase4 (events) | success | 263 s |
| phase4 (triage) | success | 281 s |
| phase4 (summary) | success | 277 s |
| phase4 (delivery) | success | 331 s |
| phase4 (stream) | success | 302 s |
| phase4 (tenant) | success | 270 s |
| phase4-browser | success | 364 s |

## Open items

- **`TestDetectorCooldownOwners` hosted flake (not root-caused).** It failed once on a hosted runner and passed on the re-run and 5/5 times locally. The 20s observation window was not relaxed. If it recurs, diagnose the pin-observation timing on slow runners at the root instead of raising the timeout. This is tracked as a watch item, not as a Phase 4 gap.
- The integration job took 37 min on the hosted runner. Its cap has headroom, but it is the longest job.

## Self-Check: PASSED

`run.json` was saved and its `headSha` equals `7b15a68`. origin was not pushed.
