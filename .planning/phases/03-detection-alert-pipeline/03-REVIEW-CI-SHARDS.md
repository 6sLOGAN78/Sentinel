---
phase: 03-detection-alert-pipeline
reviewed: 2026-10-21T13:22:12Z
depth: deep
files_reviewed: 3
files_reviewed_list:
  - .github/workflows/ci.yml
  - scripts/check-phase3-tests.sh
  - scripts/test_phase3_guards.py
diff_base: e9f40dd5e761f74d746b3bd1d89576127c1d9d15
red_commit: 570b868d3a28b591169a2810f0922d7ab6ac12b1
reviewed_head: 84352ad9aab1a9da582cd2dab9d586f578f43f8f
findings:
  critical: 0
  warning: 1
  info: 0
  total: 1
status: issues_found
---

# Phase 3 CI shards: scoped code review

## Narrative Findings (AI reviewer)

### WR-01 — WARNING: mandatory-job guard accepts skipped or failure-masked execution

**File:** `/home/logan78/Desktop/Sentinel/scripts/test_phase3_guards.py:155`

**Issue:** The new workflow guard validates the exact strategy string but checks
the selector command only with `assertIn`, and never checks execution conditions.
A valid YAML `if: false` on the Phase 3 job before `strategy`, or on the selector
step, leaves every assertion in this method satisfied while skipping required
coverage. Appending ` || true` to the selector's `run:` also retains the expected
substring and passes the guard while masking a failed shard. Existing workflow
guards later in this file do not reject these changes either: they check the
same job/command substrings and prerequisite strings. The shell's exhaustive
fake-Go failure checks cannot catch bypasses in the enclosing GitHub step.

These are direct counterexamples traced through the assertions, not executed
mutation-test results. The current workflow itself has **none** of these
bypasses; the defect is in the regression test's mandatory/failure policy
coverage, which was an explicit requirement of this repair. This is a test
reliability warning, not a claimed present native-test or workflow execution
failure.

**Fix:** Validate the actual job and selector-step fields, rather than command
presence alone. Require the selector's complete `run` value to equal the approved
command; reject conditional skipping on the Phase 3 job and its required
startup/check/selector steps; retain only the approved unconditional cleanup
condition. Keep the exact closed matrix and reject exclusions and continued
errors. Extract the validation into a callable check over workflow text/data
and add negative fixtures for job-level skip, selector-step skip, `|| true`,
wrong group, exclusion and `continue-on-error`. Use an existing available parser
or strict block validation without introducing an unnecessary dependency.

## Other reviewed behavior

All three source files were read completely at the immutable reviewed head.
Read-only cross-file tracing covered the nonempty runner, Make targets and both
native scenario families. No project AGENTS.md, project-local skill directory,
source-ignore exclusion or structural pre-pass was found/supplied.

The shell partition itself is exact: 31 ordinary integration families, seven
cooldown families and six pure/internal families form `core` (44); the two
scenario families form `scenarios`. Their package/name identities are disjoint;
the intentional same test name in two packages remains distinct. Default zero
arguments and explicit `all` retain the original ordered 46 calls. Invalid,
empty or extra arguments fail with usage/status 2 before Go is invoked.

The exact selectors, integration tags, `-p 1`, required race options and
6m/8m/60s/16m/32m family caps remain unchanged. The real nonempty runner retains
`pipefail`, propagates Go failure despite an earlier PASS, disables result
caching and refuses output without a passing test. `set -e` propagates each
selected family failure through the shard script.

The current workflow declares exactly the two matrix values with fail-fast
disabled, a 45-minute cap on each instance and no exclusions, conditions or
continued errors. Both runners retain their own stack startup/check and
always-run preserved-volume teardown, avoiding shared producer state between
jobs. The other eight jobs, pinned actions, integration aggregate, seven
retained groups and Compose smoke obligation are unchanged. There are nine job
definitions and ten expanded job instances. Sharding does not guarantee every
possible execution finishes within the cap; it removes the observed serial
core-plus-scenarios accumulation without increasing that cap.

## Evidence inspected and limits

Actual logs, rather than only the repair summary, were read:

- `ci-shard-red.log`: three new guard methods reach nine intended assertion
  failures; 13.943s, exit 1, 13:13:46Z–13:14:00Z.
- `ci-shard-focused-green.log`: the same three methods pass; 4.775s, exit 0,
  13:14:46Z–13:14:50Z.
- `ci-shard-scripts-green.log`: all 49 script tests pass; 148.266s, exit 0,
  13:15:11Z–13:17:40Z. This includes per-family empty/failure checks inside each
  shard and the pre-existing default-runner guards.
- `ci-shard-static.log`: shell syntax, existing js-yaml workflow parse plus
  complete semantic delta comparison, and whitespace checks return 0 at
  13:16:12Z. This verifies the present candidate's small workflow delta; it is
  not a permanent negative guard for WR-01.
- Retained hosted run `39598061416` metadata shows eight successful jobs and
  the Phase 3 job cancelled at head `a69d158`. Its actual log contains the core
  family passes, `TestDetectionScenarios` PASS (325.14s), then the final repeat
  RUN at 12:55:56.814Z without a terminal PASS before cancellation. The root's
  separately recorded check annotation identifies the 45-minute job cap; this
  reviewer made no remote API call to retrieve that annotation.

The fake-Go checks are an orchestration MODEL, not fresh native 46-family
acceptance. The final interrupted hosted family is neither passed nor an
assertion failure. The root's earlier complete local integration success is
historical proof at its recorded source, not a new native run at this CI-only
GREEN. Fresh hosted execution remains pending.

The reviewer ran no tests, builds, native/live services or remote operations,
modified no source and created no commit. Only this report was written; prior
reports and user changes remain untouched.

_Reviewer: independent gsd-code-reviewer; deep review with Karpathy scope and
evidence discipline._
