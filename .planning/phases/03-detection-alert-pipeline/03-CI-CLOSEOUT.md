# Phase 3 hosted CI closeout

Verified run: [39515550386](https://github.com/6sLOGAN78/Sentinel/actions/runs/39515550386).
Tested candidate: `80d2d873762e4832abf4c87e2d36c22d359d8337`.
Root's authorized fast-forward push advanced `origin/main` from19db519 to80d2d87
after every required local gate passed. User configuration and `.serena` were not
staged or pushed. No forced push, rewritten history or skipped CI was used.

## Actual hosted result

GitHub reports **completed / success**. All nine jobs succeeded:

| Job | Result | Finished UTC |
| --- | --- | --- |
| hygiene | success | 2026-10-20T23:31:25Z |
| secrets | success | 2026-10-20T23:31:24Z |
| web | success | 2026-10-20T23:31:37Z |
| python | success | 2026-10-20T23:32:41Z |
| schema | success | 2026-10-20T23:31:43Z |
| go | success | 2026-10-20T23:36:09Z |
| integration | success | 2026-10-21T00:13:57Z |
| phase3 | success | 2026-10-21T00:20:45Z |
| compose-smoke | success | 2026-10-20T23:45:44Z |

Run created2026-10-20T23:31:15Z, completed2026-10-21T00:20:46Z. Root independently
queried the exactSHA/run/job conclusions and recovered `gh run watch --exit-status`
exit0. Private persistent watch output is in ignored
`.local/phase3-acceptance/hosted-ci-39515550386-watch.log`.

The reviewed workflow requires complete integration plus seven independently
nonempty retained Phase2groups, all46exact Phase3families, and actual Compose
startup/migration/legacy seed/two preserved scenario commands. No step masks
failure. The phase3job took44m34s, within its unchanged45mjob cap; no speculative
budget increase was used. Individual family limits remain unchanged.

## Historical failures and repairs

Earlier run39457945227 on19db519 failed. It is preserved, not reclassified green:

- Static scenario workflow-payload verification observed unrelated sharedPG rows.
  Native causalRED and permanentregressionRED preceded privatePGfixtureGREENdf0bfbb.
  Strict verifier/goldens/runtime unchanged; full native suite then passed locally.
- Python orchestration used systemPython without the lockedhttpx dependency.
  GuardRED59b3498 thenGREENdefe62e select the existing generator interpreter.
- Scanner flagged historical YAML-validation prose. Private classification and
  independent review established no credential. Only its exact historical
  commit/path/rule/line fingerprint was excepted; default rules/fullhistory remain.

See [local execution summary](03-14-SUMMARY.md),
[CI repair evidence](03-CI-REPAIR-EVIDENCE.md) and
[independent repair review](03-REVIEW-CI-REPAIR.md). Local failures and lost-log
interruptions remain separate from this actual hosted result. NativeACKloss,
modeled delayed visibility, missing separate Task3REDcommit and other recorded
proof limits are not erased by a green run.

## Scope boundary

This run certifies the tested candidate's checks, not unmeasured performance,
RSS, exactly-once physical effects or future Phase4API/UI/SSE behavior. Subsequent
STATE/VALIDATION tracking edits do not change the implementation tested above.
Final03-VERIFICATION.md passed59/59, all14requirements were marked complete,
and registeredphase.complete3 succeeded without warnings. Root reconciled its
supplemental-plan miscount to21/21numbered plans. Closure changes after the tested
candidate are documentation/tracking only; no implementation changed. Phase3 is
complete. Stop before Phase4.
