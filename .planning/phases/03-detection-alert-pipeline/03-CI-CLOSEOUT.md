# Phase 3 hosted CI closeout

## Reviewed two-job candidate — replacement running

Finalscanner at7a72028 passed0/467commits/no leaks14:01:29Z. Root's authorized
normalpush advanceda69d158→7a72028cd7af506cc27097317d61fec0ea918d89.
[Replacement39613182464](https://github.com/6sLOGAN78/Sentinel/actions/runs/39613182464)
was created14:01:44Z atthatexacthead and is inprogress, not yet aPASS.
Privatepersistentwatch: hosted-ci-39613182464-watch.log. All10instances must
actually succeed before releasedverification and closure; Phase4notstarted.

CI-only84352ad preserves defaultall46 and partitions44core/2scenarios into
isolated matrix instances, both mandatory, fail-fastfalse and unchanged45mcaps.
Allselectors/tags/race/per-familycaps and the othereight jobs are unchanged.
Independent deep R2review ata31af33 is clean; both prior guard warnings are
closed. Actual51script tests PASS0/150.750s and shell/YAML/semanticdelta checks
PASS0. Runtime/scenario/deployment source remains identical to local fullPASS
10ddd71/1650.77s. Scanner/push now passed; new actualall10job successes remain
required. This candidate is not a hostedPASS or Phase3closure yet.

## Latest repaired candidate: matrix job timed out

[Run39598061416](https://github.com/6sLOGAN78/Sentinel/actions/runs/39598061416)
at a69d1583c27843612d5ca9f45df19d41b348f1dd completed/cancelled. Eight jobs
succeeded, including fullintegration/allsevenretainedgroups andCompose acceptance.
Onlyphase3 wascancelled. Its check119216776646 explicitly reports the45m maximum
execution time was exceeded. Job12:18:15Z–13:03:35Z; make dev-up used3m and
checkstack17s; matrixstep12:21:40Z–13:03:29Z wascancelled. This is an aggregate
job-cap failure, not a reported assertion failure or a completed46-family pass.
Private full APIresult and cancelledphase3log are retained in
`.local/phase3-acceptance/hosted-ci-39598061416-*`.

Root is preparing two isolated phase3 matrix groups:44core and2scheduledscenario
selectors, with unchanged defaultall46 and unchanged per-test/45mjob caps. All
individual selectors remain required and independently nonempty; no mask/skip
or production change is authorized. Review/localguardproof/newhosted acceptance
are required before Phase3 closure. Phase4 remainsnotstarted.

## Latest repeat gate: reopened

Documentation-only closure commit `eee632febdf601286d2ba8ec3de1e3f75febe2cd`
triggered [repeat run39544892831](https://github.com/6sLOGAN78/Sentinel/actions/runs/39544892831).
It completed with eight successful jobs and one failed integration job119042234176.
`TestDetectorCooperativeAcquisition` failed at detector_rebalance_test.go:94 because
the retained-owner map was empty. Full package elapsed1684.173s, Makeexit2; this
was an assertion failure, not the 32-minute aggregate timeout. The seven subsequent
retained selectors were skipped. The independent all46-family phase3job succeeded.
There was no runtime, test, workflow or configuration change between80d2d87 and
eee632f. This is a newly exposed reliability gap, not erased by the prior green.
Private failed output is retained in
`.local/phase3-acceptance/ci-repeat-39544892831-failed.log`; watch returned1.
Causal fixture diagnosis/repair, independent review and fresh local/hosted proof
are required before closing Phase3 again. Phase4 is not started.

## Prior all-green acceptance (historical, unchanged)

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
