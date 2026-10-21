# Phase 3 CI shard guard boundary repair

Finding: `03-REVIEW-CI-SHARDS-R1.md`, WR-01-R1. YAML comments do not terminate
job mappings. The old checker excluded a valid four-space job condition placed
after the separator comment and before the actual next job. Current hosted
workflow has no such condition. Scope remains only `scripts/test_phase3_guards.py`.

Tests-only RED `8b26d8d`: whole-workflow fixtures insert `if: false` and
`continue-on-error: true` after the existing separator comment. Actual execution
at13:53:53Z returned1 with two intended `AssertionError not raised` failures.
The previous13negative controls and real-workflow positive test remain intact.

Initial implementation `3204a00` extracts through the known actual
`compose-smoke` job key, requiring exactly one of each boundary. It preserves
intervening comments and job fields in the complete approved-text comparison.
Its first focused run returned1 because the expected literal contained one
extra terminal newline: the actual-file positive test failed, while both
negative methods passed. This was an implementation formatting failure,
not an additional behavioral RED. Corrective `a31af33` matches the exact boundary
newline. All three focused methods actually pass at13:54:32Z (0.002s,exit0).
No workflow, runner, parser dependency, runtime, test content or budget changes.

A full-suite run started prematurely at3204a00 was deliberately stopped after
that known positive-test failure; the exact owned unittest PID was identified
and sent SIGTERM. Its log ends13:54:55Z/RETURNED_EXIT143. It is not a completed
suite or PASS. The corrected replacement is distinct:

| Executed check | Actual result | Private log |
| --- | --- | --- |
| Boundary mutation RED | 1method,2assertion failures,exit1,13:53:53Z | `ci-shard-guard-r2-red.log` |
| Initial newline-mismatch attempt | 3methods,1failure,exit1,13:54:10Z | `ci-shard-guard-r2-green.log` |
| Corrected focused methods | 3methods PASS,0.002s,exit0,13:54:32Z | `ci-shard-guard-r2-focused-final.log` |
| Superseded full attempt | Deliberately terminated,exit143,not PASS | `ci-shard-guard-r2-scripts.log` |
| Corrected full locked script discovery | 51tests PASS,150.750s,exit0,13:54:55Z–13:57:26Z | `ci-shard-guard-r2-scripts-final.log` |
| Shell syntax/YAML semantic/whitespace/immutable workflow-runner checks | PASS0,13:55:12Z | `ci-shard-guard-r2-static.log` |

The installed js-yaml parser positively confirms both post-comment fixtures
parse as fields on `phase3`, not `compose-smoke`. It also rechecks the exact
two-group strategy/45m caps and full workflow equality with pre-shard e9f40dd
after reversing only the intended matrix/group argument changes. Eight other
job definitions remain unchanged. The51-case suite includes all prior controls
and exhaustive fake-Go failure/empty-family checks in both shards.

Private ignored logs are under `.local/phase3-acceptance/` (0700/0600); test
subprocesses use umask022. These remain orchestration MODEL checks, not a new
native46-family result. Backend/generator/deployments/scenario source has an
empty diff from actual local fullPASS10ddd71 (1650.77s). Latest hosted
39598061416 remains eight successes plus Phase3 cancellation at45m. Independent
R2 review, final scanner, authorizedpush/newhosted10job success and released
finalverification remain required. No Phase4 work or user-dirt changes.
