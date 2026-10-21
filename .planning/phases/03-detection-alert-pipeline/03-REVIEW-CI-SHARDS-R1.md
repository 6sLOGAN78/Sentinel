---
phase: 03-detection-alert-pipeline
reviewed: 2026-10-21T13:50:41Z
depth: deep
files_reviewed: 1
files_reviewed_list:
  - scripts/test_phase3_guards.py
diff_base: 6fb327b2b98df8f281eb555128cf7829a1f0b2f2
red_commit: 782c891d580e77f49ab545392808da024794c2a6
reviewed_head: f6618e313bb55c18c4e54a20a5f9cb26038d6dc3
evidence_head: 44f50fd08c92d96c73fedd32479d0d3cbafe9dfe
findings:
  critical: 0
  warning: 1
  info: 0
  total: 1
status: issues_found
---

# Phase 3 CI shard guard: R1 re-review

## Narrative Findings (AI reviewer)

### WR-01-R1 — WARNING: comment delimiter permits job conditions outside the checked block

**File:** `/home/logan78/Desktop/Sentinel/scripts/test_phase3_guards.py:161`

**Issue:** The strict comparison checks only text before the first `\n  #`.
YAML comments do not terminate a job mapping. Consequently this valid workflow
mutation disables Phase 3 while leaving the entire compared string unchanged:

```yaml
      - name: tear down
        if: always()
        run: make dev-down

  # Preserved-volume legacy seed followed by two exact scheduled scenario runs.
    if: false
  compose-smoke:
```

The four-space `if` still belongs to `phase3`; the two-space comment is ignored
by YAML. `workflow.count("\n  phase3:\n")` remains one, and the extractor stops
before that comment, so the approved-text equality accepts it. The existing
other workflow assertions use the same comment delimiter and do not reject
the appended job field. The 13 mutation fixtures all modify the pre-comment
slice, so none exercises this residual bypass. The current actual workflow
contains no such condition; this remains a test-reliability defect in the
promised mandatory-job policy, not a present workflow execution failure.

This counterexample is established by source/control-flow and YAML mapping
indentation analysis; this reviewer did not run a mutation test or YAML parser.

**Fix:** Extract through the next actual two-space job key, not a comment. For
this deliberately small closed policy, locating the known `\n  compose-smoke:\n`
boundary and comparing all preceding non-comment job text is sufficient if
properly fail-closed; alternatively use an existing parser or indentation-aware
block extraction. Preserve intervening four-space job fields for validation.
Add a permanent negative fixture inserting job-level `if: false` and preferably
`continue-on-error: true` after the separator comment. Ensure the mutation is
applied to the entire workflow rather than only the already extracted prefix.
Keep the approved command/matrix comparisons and the existing 13 controls.

## Reviewed changes and evidence

The entire 276-line source file was read, along with the complete original
warning report and R1 evidence document. Read-only references were the current
workflow and shard runner. Their diff from `84352ad` is empty, retaining the
44+2 partition, all-family default, budgets, flags and failure propagation.
No project AGENTS.md or source-ignore exclusion was found; no structural
pre-pass was supplied.

The complete approved-text comparison fixes the original command-substring
acceptance gap *inside its extracted slice*. Its positive test reads the actual
workflow, so an always-failing checker cannot satisfy both positive and negative
tests. Each mutation verifies one actual replacement target before requiring
an AssertionError. No parser dependency, source runtime, test obligation or
native deadline was added or relaxed. The exhaustive per-shard empty/failed
Go checks and default 46-family checks remain unchanged.

Actual private logs were read directly:

- `ci-shard-guard-r1-red.log`: the real workflow passes while seven skip/mask
  mutations are falsely accepted, producing seven intended `AssertionError not
  raised` failures; two methods, 0.002s, returned exit 1 at 13:46:07Z. Six other
  negative controls already reject their mutation; no RED is attributed to them.
- `ci-shard-guard-r1-green.log`: both focused methods report success, 0.002s,
  returned exit 0 at 13:46:30Z, covering the 13 specified mutation fixtures.
- `ci-shard-guard-r1-scripts.log`: recorded full discovery reports 50 tests,
  150.299s, returned exit 0, 13:46:47Z–13:49:17Z.
- `ci-shard-guard-r1-static.log`: recorded semantic/static bundle returns 0
  at 13:47:35Z and reports the approved two-group delta with eight other job
  definitions unchanged.

Those recorded executions are guard/orchestration MODEL evidence; the residual
post-comment case above is absent from them. They do not establish fresh native
46-family acceptance or a new successful ten-instance hosted run. The previous
hosted cancellation and historical local full-suite result retain their own
provenance. Phase completion is not asserted.

Only this report was created. The reviewer ran no tests, builds, native/live
services, scanner, remote operations, staging or commits, and made no source
edits. Original reports and user changes remain untouched.

_Reviewer: independent gsd-code-reviewer; deep read-only re-review using GSD
classification and Karpathy evidence/scope discipline._
