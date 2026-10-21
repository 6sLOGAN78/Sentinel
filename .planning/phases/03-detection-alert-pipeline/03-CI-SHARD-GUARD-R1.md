# Phase 3 CI shard guard WR-01 repair

Original finding: `03-REVIEW-CI-SHARDS.md`, WARNING WR-01. Actual workflow at
`84352ad` contains no skip/mask bypass; the flaw was its regression guard.
Only `scripts/test_phase3_guards.py` changes in this follow-up. The workflow,
shell runner, Go/Python runtime, fixtures, selector content and budgets do not.

Tests-only RED: `782c891`. The original positive assertions were moved unchanged
to `assert_phase3_workflow(workflow)`, used by both the real-file test and
in-memory workflow mutations. Thirteen fixtures each assert their source
replacement targets exactly one actual field. Two tests actually ran at
13:46:07Z on2026-10-21: the real workflow passed; seven mutations were falsely
accepted and produced intended `AssertionError not raised` failures (exit1).
Those seven are job skip, selector skip/mask, startup skip/mask and stack-check
skip/mask. Existing continued-error (job/selector), wrong-group, exclusion,
fail-fast and increased-budget controls were already rejected; no RED is
invented for those six controls.

GREEN: `f6618e3`. The checker requires exactly one Phase3 block and compares its
complete small approved job text, including pinned setup, exact two-group matrix,
prerequisites and full commands. This deliberately closed policy rejects extra
execution conditions or command suffixes; only the existing teardown has
`if: always()`. Future intentional formatting/pin/job changes must update this
explicit approval and negative fixtures. No YAML dependency or generic parser
framework was added.

Executed evidence (ignored private `.local/phase3-acceptance/`, directory0700,
logs0600; actual test subprocess umask022):

| Check | Actual result | Log |
| --- | --- | --- |
| Same two focused methods at GREEN | 2tests PASS,0.002s,exit0,13:46:30Z | `ci-shard-guard-r1-green.log` |
| Full locked script discovery | 50tests PASS,150.299s,exit0,13:46:47Z–13:49:17Z | `ci-shard-guard-r1-scripts.log` |
| Shell syntax, installed js-yaml semantic comparison, whitespace, immutable workflow/runner diff | PASS0,13:47:35Z | `ci-shard-guard-r1-static.log` |

RED log: `ci-shard-guard-r1-red.log`. The semantic comparison proves the current
workflow differs from pre-shard `e9f40dd` only by the two-value strategy and group
argument; all eight other job definitions are unchanged. There are nine job
definitions, ten expanded instances, with the same45mcap per Phase3 instance.
The full50-case suite retains exhaustive empty/failed-family propagation tests
inside both shards and the unchanged default all46 sequence.

These are guard/orchestration MODEL results, not new native46-family acceptance.
Actual local full integration at10ddd71 remains PASS0/1650.77s; latest hosted
39598061416 remains eight successes plus aggregate Phase3 cancellation at45m.
No heavy/native/live/remote operation ran during this follow-up. Independent
R1 review, aggregate reconciliation, final scan, authorized push/new actual
hosted success and released final verification remain required. Phase4 idle.
Worker hit a usage limit before editing this follow-up; root implemented it
inline. User config and `.serena/` remain untouched and unstaged.
