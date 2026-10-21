# Phase 3 CI group partition repair

Base: `e9f40dd5e761f74d746b3bd1d89576127c1d9d15`.
Assertion RED: `570b868d3a28b591169a2810f0922d7ab6ac12b1`.
Orchestration GREEN: `84352ad9aab1a9da582cd2dab9d586f578f43f8f`.
Source scope is exactly `scripts/check-phase3-tests.sh`,
`scripts/test_phase3_guards.py` and `.github/workflows/ci.yml`.

## Actual hosted boundary

Root retained completed run39598061416 at a69d158: eight jobs succeeded,
including full integration, all seven retained Phase 2 groups and Compose.
The single Phase 3 job was cancelled. Its GitHub check-run119216776646 annotation
states that the job exceeded its maximum execution time of45m0s; this positive
timeout evidence establishes the budget cause. Absence of an assertion failure
is not by itself the cause proof.

The job started12:18:15Z and ended13:03:35Z on2026-10-21. Stack setup took about
three minutes plus a17s stack check. The exact-family step began12:21:40Z and
recorded cancellation at13:03:29.677Z. The privately retained actual log is
`.local/phase3-acceptance/hosted-ci-39598061416-phase3-cancelled.log`.

This worker extracted only safe family/result records. There are45top-level
PASS records: all44core package/name families, followed by
`TestDetectionScenarios` (325.14s, PASS at12:55:33.703Z). The final
`TestDetectionScenariosPreservedRepeat` started12:55:56.814Z and has no terminal
PASS before cancellation. Two `TestConfigLedgerHandover` PASS records correspond
to the intentional integration and pure detection package families. No46/46
completion is claimed. No top-level assertion failure appears in the retained
matrix log; an interrupted final family is not classified as passed or failed.

Root separately recorded the latest complete local integration pass: exit0,
package1648.087s, elapsed1650.77s. Historical failed/cancelled runs remain distinct
from that result. This repair introduces no new native or hosted pass claim.

## Minimal partition and retained guarantees

The shell accepts zero arguments or explicit `all` for the unchanged46-family
sequence. `core` selects its first44families; `scenarios` selects exactly
`TestDetectionScenarios` and `TestDetectionScenariosPreservedRepeat`. Their union
is the complete default sequence and their package/name sets are disjoint.
Selector lists remain written once. Unknown/empty group values and extra
arguments return usage/exit2 before any Go command.

Existing core blocks and scenario calls are wrapped in group conditions. The
nonempty runner, every exact selector, per-family timeout, integration tag,
serial `-p 1`, race option and failure propagation remain unchanged. The default
Make target and zero-argument local command still execute all46families.

The existing Phase 3 workflow job gains only `strategy.matrix.group: [core,
scenarios]`, `fail-fast: false`, and forwarding of that group to the shell.
Both instances keep the existing45m cap, pinned actions, Go prerequisite,
real stack startup/check, and always-run preserved-volume teardown. No matrix
exclusion, continued error, skipped selector, deadline increase or wider
parallel producer sharing was introduced. The runners are separate jobs.
The workflow has nine job definitions and ten instances after expansion;
the other eight jobs are unchanged.

## Causal regression and executed checks

Permanent guards execute the real shell/nonempty runner against a synthetic Go
process, independently record every package/selector/argument, and reject
empty/failed families even when another PASS line precedes failure. The new
tests check every family inside its group, exact group union/order and budgets,
zero-argument versus explicit `all`, closed argument validation before Go, and
the exact two-group workflow strategy with retained failure/prerequisite policy.
These are orchestration simulations, not native integration executions.

Before implementation, three new guard methods reached nine intended assertion
failures: the old shell ignored group/invalid arguments, and the old workflow
lacked a matrix. Test collection and prerequisite setup succeeded. This
assertion RED was committed before changing either implementation file.

All commands ran from the repository root with umask022. Logs below live under
ignored `.local/phase3-acceptance/` (0700 parent,0600 logs), with actual
START/END/EXIT markers.

| Check | Actual result | Private log |
| --- | --- | --- |
| Locked unittest selection of new group-union, invalid-argument and workflow-strategy guards at RED | Exit1;3tests,9assertion failures;13.943s;13:13:46Z–13:14:00Z | `ci-shard-red.log` |
| Same three guards at GREEN | Exit0;3tests;4.775s;13:14:46Z–13:14:50Z | `ci-shard-focused-green.log` |
| `bash -n scripts/check-phase3-tests.sh` | Exit0;13:16:12Z | `ci-shard-static.log` |
| Existing installed js-yaml parser plus semantic comparison against base workflow | Exit0; parsed exact matrix and45m cap, then full workflow equality after only the two intended changes;13:16:12Z | `ci-shard-static.log` |
| Owned source whitespace check | Exit0;13:16:12Z | `ci-shard-static.log` |
| `generator/.venv/bin/python -B -m unittest discover -s scripts -p 'test_*.py' -v` | Exit0;49tests passed in148.266s;13:15:11Z–13:17:40Z | `ci-shard-scripts-green.log` |

The complete49-case script suite includes all45existing cases plus four new
partition/argument/workflow guards, including exhaustive per-group empty/failure
checks. Independent source review remains pending at this draft.
No new dependency was installed. No production, Go, golden, fixture,
test budget, native/live command, stack operation or remote operation was
changed or executed by this worker. User `.planning/config.json` and `.serena/`
remain preserved. Root owns remaining acceptance, review, scanner, tracking and
authorized push/hosted rerun. Stop before Phase 4.
