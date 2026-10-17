# Phase 3 — Revision Gate, Iteration 1

## ISSUES FOUND

**Phase:** Detection & Alert Pipeline. **Plans reviewed:** 21. **Tasks:** 52. **Waves:** 14. **Issues:** 1 BLOCKER, 1 WARNING. **Execution approval:** withheld pending the small corrections below.

Static review on 2026-10-17. This report does not claim implementation or test success. The original `03-PLAN-CHECK.md` remains unchanged and historical. Only this R1 report was written; no application, test suite, container, remote operation, commit or configuration mutation was performed.

Sources reviewed: revised plans01–21, complete amended RESEARCH, COOLDOWN-DECISION, VALIDATION, PATTERNS, SOURCE-AUDIT, current roadmap/requirements and relevant PROJECT/STATE scope. Current ops test names and the existing nonempty-runner behavior were inspected read-only. No root AGENTS.md or project skill directories were found in the original discovery; no new project rules were supplied. The stale pre-Phase1 map was not used.

The canonical operator story and all five roadmap criteria are unchanged. The user's explicit option1 selects actual cross-window alert suppression; it is correctly treated as a binding choice, while the PG schema/fencing/scheduling mechanisms remain engineering decisions and execution proof obligations.

## Remaining blocker

### R1-B1 — Sixteen new tasks omit mandatory acceptance_criteria

**Severity: BLOCKER. Dimension: task completeness / invoked workflow compliance.**

Every task in plans15–21 has `<done>` but no `<acceptance_criteria>`. Affected tasks: 15-T01/T02, 16-T01/T02, 17-T01/T02, 18-T01/T02/T03, 19-T01/T02, 20-T01/T02/T03, 21-T01/T02.

The invoked plan-phase workflow explicitly requires both task fields. Its downstream-consumer contract states that `read_first` and `acceptance_criteria` are “MANDATORY on every task”; the Anti-Shallow Execution Rules and quality gate repeat that requirement. The existing `<done>` text and automated commands contain usable acceptance material, so this is a required plan-interface repair, not evidence that the underlying feature behavior is missing. Registered `verify.plan-structure` checks validate Files/Action/Verify/Done but do not detect this missing mandatory field.

**Minimal fix:** add an explicit, checkable `<acceptance_criteria>` block to each of these16 tasks, derived from its existing behavior, named checks and `<done>`. Distinguish assertion-level RED completion from production GREEN completion. No architecture change, new user decision or phase split is needed. Extend the static gate to check this field across all52 tasks rather than relying solely on the registered structure checker.

## Remaining warning

### R1-W1 — Removed task references remain in threat dispositions

**Severity: WARNING. Dimension: security traceability / verification derivation.**

Plan01 now contains only two tasks, but T-03-01-02 still cites “Tasks2–3” and T-03-01-03 cites “Task3.” Plan02 likewise contains two tasks while T-03-02-01 cites “Tasks2–3.” These references point to tasks removed by the otherwise appropriate extraction split.

The mitigation work is concretely assigned elsewhere: loader/client extraction and callback proofs are in15/16, checkpoint refinement in07, native-store extraction/order proofs in17, and later writer conflict tests in10. Thus this is stale traceability, not an uncovered security capability or an open high-severity implementation finding.

**Minimal fix:** replace those references with current plan/task IDs and named proof families. When doing that cleanup, reconcile the stale prose file counts in15's objective (four stated, six declared) and20's objective (six stated, seven declared); declared ownership was used for this review.

## Previous findings: resolution tracking

| Original issue | R1 assessment | Concrete revised coverage |
|---|---|---|
| B1 candidate-only cooldown | RESOLVED in planning |05 freezes candidates;18 establishes real PG dependencies;19 commits allow/suppress before publication;20 proves crash/ownership/restore behavior;08 handles drained generations;14 documents the selected behavior. No final in-memory/no-op flag is permitted.|
| B2 reading lists omit actual targets | RESOLVED |All52 task lists include their declared targets. New targets are identified as new with real analogs/contracts; predecessor-created targets are explicitly read. The separate acceptance-field omission is R1-B1.|
| B3 loader source ownership | RESOLVED |15-T02 owns and reads both reducer.go and config.go;16 separately owns worker/client extraction.17 owns the corresponding writer/store extraction sources.|
| B4 live past anchor versus retained watermark | RESOLVED in planning |21 adds private caught-up snapshot plus legal scheduling;06 requires explicit schedule context;13/14 execute legacy seed→scenario→repeat on preserved topics/groups/volumes.|
| W1 validation semicolon drift | RESOLVED |All52 VALIDATION commands exactly match their task automated command, after Markdown pipe unescaping; each shell command passes `bash -n`.|
| W2 unsupported context estimates | RESOLVED as a planning warning |Initial01/02 have two tasks and8/7 files;15/16/17 separate immediate extraction/proofs;18/19/20/21 isolate added cooldown and scheduling work. Estimates distinguish reused/new work and tests. Max3 tasks/12 files; no automatic scope blocker.|

Resolved means the plan now commits to full behavior and a falsifiable proof, not that the mechanism has already passed those proofs.

## Goal-backward coverage

All14 roadmap requirement IDs appear in plan frontmatter and have concrete implementing actions. Relevant PROJECT scope is retained; no Phase4 UI/JWT/query/triage/SSE, Phase5 full metrics/benchmarks, Phase6 Kubernetes or deferred ML/Aegis work is silently included or substituted.

| Requirement | Primary implementation / proof owners | Planned delivery |
|---|---|---|
|DET-01|01,15,16|AUTH10 unique failures/300s, tenant/source/principal, live canonical Kafka output.|
|DET-02|03|Exact20 distinct ports/60s, duplicate-port/event negatives, tenant/destination isolation.|
|DET-03|04|REQUEST_FLOOD299/300/301 unique net.request cases and live output.|
|DET-04|04|DENY_SPIKE29/30/31 policy.decision/action=deny cases and live output.|
|DET-05|01,15,08,18|Strict startup YAML, independent topics/groups/config, fingerprint/version/drained transition.|
|DET-06|01-T01,05,07,09,11|ADRs before any rule code; half-open UTC windows,30s watermark/equality, deterministic late counter, bounded state/ledger.|
|DET-07|01,03,04,05,07,19|Existing component DedupeKey, frozen first crossing/count/first20 ordered IDs, created_at=window_end, no canonical detected_at.|
|DET-08|18,19,20,08|Actual visible cross-window suppression, equality allow, immutable first decision, durable cursor/ledger/fences and config handover.|
|DET-09|03–07,21,13|Exact malicious/benign/boundary/lateness fixtures and preserved-state repeated live scenarios.|
|ALRT-01|01,02,16,17,19,10,11,13|All-ISR tenant|alert_id publish, stored canonical allowed bytes, native CH immutable persistence and immediate dedupe.|
|ALRT-02|02,17,10,12,20|Unique insert-only PG workflow/list/seq, concurrent winner/orphan checks, resolved replay preservation.|
|ALRT-03|01,07–09,18–20,12|Pin/prefix/sealed recovery plus durable cooldown decisions;9/10 kill/restart and actual third-owner target handoff.|
|ALRT-04|12,20|Three Kafka boundaries plus PGdecision cuts, causal private barriers, immutable bytes and immediate visible store uniqueness.|
|GEN-02|06,21,13|Scenario flags, independent shared Go/Python expectations, private manifests, bounded safe delivery and legal live scheduling.|

All five roadmap criteria have covering actions and named proofs: (1) exact scenarios/benign/edges in03–06/13/21; (2) evidence plus actual cooldown in05/19/20; (3) incorporated-nine restart/target ownership transfer in12; (4) three durable barriers/resolved replay in12 and the added PGcuts in20; (5) alerts-topic/CH/PG/list/seq/config in01/02/08/13/17. The remaining blocker concerns the required executable-plan interface, not omitted goal behavior.

## Critical semantic contracts checked

**Actual cooldown:** Plan19 defines `clock=max(max_candidate_time, trigger_occurred_at)`, suppresses strictly before next eligibility, allows equality, advances eligibility only on allow, and freezes a suppressed window permanently. It uses the first trigger event time, not window-end created_at. The299s/301s adjacent-window example and a fresh short-window exact-expiry fixture prove an actual alert-count difference. Rule/tenant/entity isolation and monotone out-of-order behavior are required. Allowed/suppressed historical decisions return before current cursor access, after original source/config/canonical-byte validation; a new owner is not required to impersonate a historical owner.

**Effects, PG failure and fencing:**18 wires an existing real pool, lifecycle/readiness/config/fixtures/Compose capability before19 activation.19 assigns migration004 plus bounded lock/statement/transaction/finalization, immutable decisions/cursors/partition fences, captured GroupMetadata member/generation/assignment-token/incarnation and source offset/rule ordinal order. A missing older decision fails closed. PGcommit precedes stored-byte publication, all-ISR acknowledgement and checkpoint; suppress commits without publishing or creating CH/workflow state. No PGtransaction spans publication, and explicit bounded Rollback handles pgx cancellation. PG failures are terminal/not-ready, no DLQ and no unsafe pin advancement.20 has actual target transfer, delayed stale assignment, equal-generation wrong-member/source-order races and four decision/Kafka crash cuts.

**Checkpoint and datastore consistency:**01/07/19/20 explicitly couple format2 offset metadata with earliest unexpired fired/unfired pin, immediately pre-pin maximum, durable sealed floor, processed_next, fixed config/version manifest, drained marker, four effective_from gates and decision_frontier. All fit the1024-byte cap. Restore validates the frontier's exact immutable PG decision and fence at least as far; legitimate PG-ahead state is allowed.20's299s allow→300s sealed/pin-advanced→PGrestored-behind→340s candidate counterexample must fail before a new alert, even though old source contributors are already sealed. This corrects the incomplete idea that checking processed_next alone proves ledger consistency.

**Kafka/window reconstruction:**07 retains the arbitrary restart-cut/advancing-pin immutable-byte proof, including prior maxima, late/duplicate/out-of-order records and expired historical suffixes. The adapter uses actual PreCommitFnContext/void CommitOffsetsSync callback transport/partition/completeness checks; terminal OnOffsetsFetched errors reach lifecycle rather than infinite healthy retry. Ownership, BlockRebalanceOnPoll, revoke/loss and no-reset retention/truncation behavior remain explicit.12 proves actual partition transfer and broker-restored state before event10, not merely a third member joining.

**Version transition:**08 requires verified old-config drain, pending allowed publications completed, coordinated old-replica stop and bumped affected versions. It persists authorized PG config generations and resets only new-generation cursors while retaining decisions. Four aligned effective_from gates prevent300→600s identity reuse; shrink/threshold/equality/old-ledger cases and deliberate transition gaps are documented. DedupeKey and its rule-version exclusion remain unchanged.

**Live scenarios:**21 uses quiescent producers, source-end snapshots and format2 processed_next catch-up independently of the earlier resume pin. It derives caught-up maxima from the fully effected sealed floor plus lateness; explicit empty/idle/history states and capped scan fallback prevent treating missing metadata as zero. The base is at/after max(now,all caught-up maxima+1ms), threshold events share one millisecond, and all threats precede per-key progress markers. Future-skew margin, interruptible waits, capped scans, preflight refusal and15-minute orchestration retain the unchanged120s sender budget per actual delivery.13/14 require exact current-run Kafka/CH/PG reconciliation and a second scenario after legacy seed on the same retained state.16m/32m dedicated test ceilings are carried into Make/script/CI acceptance. Arbitrary future pollution/concurrent producers cause bounded refusal, not reset or weakened lateness.

**Storage and bounds:**02/17/10 preserve fresh Abort-safe native CH batches, CH→PG→sourcecommit, tenant parameters, per-global-alert-ID serialization, winner/orphan fingerprint consistency, no conflicting CH insertion and insert-only replay preserving status/assignee/version/updated_at/seq.09/11 retain64MiB tracked state,10k windows,100k offset ledger,4096 IDs/window, conservative aggregate admission and a single poison/effects budget with final commit reservation. Internal metrics stay fixed and low-cardinality, and repaired readiness admission/independent deadlines remain regression gates. The new operational decision history has explicit noautomaticGC/disk-growth/restore limits, rather than pretending durable history is an in-memory cache.

These are sufficiently concrete plan obligations; execution may expose design defects and must stop/repair rather than weaken the proofs. No extra unrequested research or code was undertaken to declare them already correct.

## Dependencies and scope

|Plan|Tasks|Files|Wave|Dependencies|
|---|---:|---:|---:|---|
|01|2|8|1|—|
|02|2|7|2|01|
|03|2|5|3|15,16|
|04|2|5|4|03|
|05|2|6|5|04|
|06|2|5|1|—|
|07|3|8|7|17,19,06|
|08|3|12|8|07|
|09|3|11|9|08|
|10|3|9|4|17|
|11|3|10|10|09,10|
|12|3|8|11|11|
|13|3|9|13|12,20,21|
|14|3|12|14|13|
|15|2|6|2|01|
|16|2|5|2|01|
|17|2|6|3|02|
|18|3|9|3|16,02|
|19|2|8|6|05,18,17|
|20|3|7|12|12|
|21|2|7|9|08,06|

Read-only parsing independently confirmed all dependency targets, acyclicity and exact max-dependency-wave+1 assignments. No shared file pair lacks a transitive dependency; same-wave declared ownership is disjoint. Every task file is covered by its plan file list and reading list. No task exceeds five declared files; no plan exceeds three tasks or12 files. Numeric ordering is not execution order:03 waits for15/16,07 waits for19 and13 waits for20/21. The non-numeric dependencies are explicit and valid.

MVP ordering remains sound:01's first non-test task is a real AUTH→Kafka path;02's first non-test task is real alert→CH/PG→commit. The required ADRs are in01's first test task before any rule code. New plans refine these running capabilities, rather than a separate schema/API/wiring layer cake. Walking Skeleton is not applicable to Phase3. Context estimates are planning bounds, not measured completion claims;19 is the largest new domain refinement and20 separately owns its failure/fence proofs.

## Dimension 8: Nyquist Compliance

Nyquist is enabled and RESEARCH has Validation Architecture. VALIDATION exists with exactly52 matching automated-command bindings. All52 shell commands pass syntax-only `bash -n`; no task command was executed. None is MISSING/watch/empty, and all selected Go families use finite nonempty runners. Important distinct tests now have separate runner invocations, avoiding one PASS concealing a missing regex branch. Python checks are one-shot fixture/fake-clock suites; real dependency setup is explicitly a slow verification gate.

|Wave|Automated tasks|Sampling|
|---:|---:|---|
|1|4/4|PASS|
|2|6/6|PASS|
|3|7/7|PASS|
|4|5/5|PASS|
|5|2/2|PASS|
|6|2/2|PASS|
|7|3/3|PASS|
|8|3/3|PASS|
|9|5/5|PASS|
|10|3/3|PASS|
|11|3/3|PASS|
|12|3/3|PASS|
|13|3/3|PASS|
|14|3/3|PASS|

Every three-task implementation window therefore satisfies sampling continuity. Named-test creation is assigned before production behavior, including loadable declarations for new APIs.19 owns initial decision/schema/replay tests,20 owns restore-behind/ownership/outage/crash additions,21 owns snapshot/fake-clock/repeated-live scaffolds, and13 executes the latter after operator orchestration exists. The three original crash barriers/default-noop controls remain in12. No broken MISSING→Wave0 link was found.

Unit ceilings are60s; ordinary real groups6m; cooldown8m; scheduled scenarios16m; repeated scenarios32m. The full integration ceiling must be explicitly adjusted if scheduled proofs are included, with evidence and finite Make/CI budgets, as the revised validation contract states. Do not claim the legacy10m default covers a32m proof. These are planned budgets, not measured runtimes. `nyquist_compliant=false` remains pending the revision gate and `wave_0_complete=false` remains correct until actual scaffold creation/execution; this report changes neither flag.

## Dimension summary

|Dimension|Assessment|
|---|---|
|Requirement coverage|PASS —14/14 IDs and concrete full behaviors, including selected cooldown.|
|Task completeness|FAIL — R1-B1; base Files/Action/Verify/Done and reading lists otherwise present.|
|Dependencies|PASS — exact waves, valid acyclic graph, all shared edits ordered.|
|Key links|PASS — decision→publish→checkpoint, CH→PG→commit, config restore and live helper wiring explicitly assigned.|
|Scope sanity|PASS with execution estimates —2–3 tasks and max12 files; no unsupported automatic split requirement.|
|Verification derivation|PASS with R1-W1 traceability cleanup — observable truths and named negative/failure proofs.|
|Context compliance|PASS — explicit user option1 and inherited identity/evidence/storage decisions honored; no invented Phase3 D-XX.|
|Scope reduction|PASS — rejected candidate-only/past-anchor policies are marked superseded, not final behavior.|
|Architectural tier compliance|PASS — detector decision logic, operational PG state, CH evidence, PG workflow/list and producer scheduling have correct owners.|
|Nyquist|PASS in planning structure/command coverage; overall approval withheld for R1-B1.|
|Cross-plan data contracts|PASS — timestamp snapshot/schedule, Candidate/decision bytes, format2 and config transitions have compatible explicit contracts.|
|AGENTS.md|SKIPPED — no root AGENTS.md found.|
|Research resolution|PASS — Open Questions explicitly RESOLVED; implementation uncertainty is identified as blocking execution proofs.|
|Pattern compliance|PASS — current analogs/contracts are named in per-task readings; new domain behavior uses amended research.|

All21 plans contain scoped ASVS5.0.0 controls and actionable STRIDE dispositions, with high findings blocking completion; the removed task references are R1-W1. No new package install, secret-bearing output, destructive data action, public fault/debug route or speculative Phase4 feature is required. Default hooks remain no-op, integration hooks private/finite, tenant SQL parameterized, and existing Phase2 regression families remain mandatory. A threat-model plan is not a passed implementation security audit.

## Structured issues

```yaml
issues:
  - id: R1-B1
    plan: null
    plans: ["03-15", "03-16", "03-17", "03-18", "03-19", "03-20", "03-21"]
    dimension: task_completeness
    severity: BLOCKER
    description: "All16 tasks in plans15–21 omit mandatory acceptance_criteria despite having done/actions/automated checks; registered structure checks do not validate this field."
    fix_hint: "Add explicit checkable acceptance_criteria to each task, derived from existing RED/GREEN behavior and done criteria; statically require it across all52 tasks."
  - id: R1-W1
    plan: "03-01"
    plans: ["03-01", "03-02"]
    dimension: verification_derivation
    severity: WARNING
    description: "Threat dispositions still cite removed Task3 after those plans were reduced to two tasks; mitigation implementation/proofs now live in15/16/17/07/10."
    fix_hint: "Replace stale task references with current plan/task IDs and proof families; reconcile objective file-count prose during the same cleanup."
```

## Recommendation

Repair the16 missing acceptance blocks and the stale threat references, then run a focused follow-up gate on those exact changes plus structural/ownership/command-parity regression checks. No renewed user decision or broad architecture research is necessary. The earlier substantive blockers are addressed in the revised plan contracts; execution approval is withheld only until the required plan fields are restored. All implementation and behavioral test results remain pending.
