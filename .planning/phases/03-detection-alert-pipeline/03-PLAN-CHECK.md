# Phase 3 — Pre-execution Plan Check

## ISSUES FOUND

Phase: Detection & Alert Pipeline. Reviewed 2026-10-17. Plans checked: 14, with 38 tasks in 11 waves. Verdict: **4 BLOCKERS, 2 WARNINGS**. Return to the planner; execution is not approved.

This is a static review of planned delivery, not a claim that Phase 3 code exists or tests passed. No application, containers, test suites or remote operations were run. All 14 registered `verify.plan-structure` queries returned `valid=true`; their structural checks do not establish semantic coverage or compliance with the mandatory per-task reading rule.

Review sources: current PROJECT, REQUIREMENTS, ROADMAP, STATE and config; all 14 PLAN files; complete phase RESEARCH, PATTERNS, VALIDATION and SOURCE-AUDIT; the Revision Gate reference and planning calibration references. Current integration fixture and generator source was inspected only to establish the relevant plan assumptions. No root AGENTS.md or project skill directories were found. The old pre-Phase-1 codebase map was not used as implementation truth.

The approved story is: “As a system operator, I want to receive deterministic, evidence-backed alerts from malicious traffic, so that crashes, restarts and replay cannot lose or duplicate detections.” During this review the parent relayed the user's explicit choice of **actual alert-level cooldown across nearby windows**. The candidate-only interpretation in the reviewed files is therefore superseded, rather than an available discretionary implementation choice.

## Blockers

### B1 — Cooldown does not suppress alerts and contradicts the user's selected behavior

Dimension: requirement coverage / scope reduction / context compliance. Affects 03-05-T01/T02 and policy propagated through 03-01, 03-08, 03-14, RESEARCH, VALIDATION and SOURCE-AUDIT.

DET-08 requires cooldown to suppress repeat alerts for the same entity. Plan 05 instead says a distinct window remains eligible even within the previous cooldown, and the action explicitly says not to claim cross-window suppression. Research Pattern 5 acknowledges that this changes candidate work/counters and does not reduce unique visible alert counts beyond permanent per-window dedupe. Thus changing cooldown has no alert-level effect in the proposed implementation. A D-XX reference or a candidate-throttle test cannot prove the missing behavior.

Before the user's clarification, the wording required an explicit interpretation decision; it did not justify assuming a particular greedy cross-window algorithm. That ambiguity is now resolved by the user's selection of actual alert suppression. The reviewed candidate-only policy cannot satisfy it.

Fix: select and document precise event-time alert-level eligibility across windows, including before/at/after the deadline, out-of-order inputs, separate-window investigation and restart behavior. Plan bounded durable recovery of any per-entity cooldown seed needed immediately before the replay pin; alternatively select an explicitly durable state mechanism. Add uninterrupted/restart/advancing-pin tests proving identical suppression and immutable output bytes. Revise every affected policy, acceptance criterion and coverage claim together. Do not add processing-time suppression, an infinite pin or an assumed fixed lookback as shortcuts.

### B2 — Mandatory per-task reading lists omit the files and source patterns being changed

Dimension: task completeness / workflow compliance. Affects all 38 tasks.

Every `<read_first>` contains only RESEARCH.md and PATTERNS.md. For example, 03-01-T02 edits `backend/cmd/detector/main.go`, `backend/internal/config/config.go` and `config/rules.yaml` but does not list any of them, the contract sources, or the actual worker/client analogs. Later tasks similarly omit their existing target files, including files created by predecessor tasks.

The invoked plan-phase workflow's mandatory Anti-Shallow Execution Rules state that `<read_first>` must always include “The file being modified” and “Any file whose patterns, signatures, types, or conventions must be replicated or respected”; its quality gate requires every task to include at least its modified file. A general `<context>` reference and the pattern-map summary do not satisfy that explicit per-task contract.

Fix: give each task its actual existing targets and required source files/analogs. For genuinely new files, name the real analog or source contract and identify the file as new rather than requiring a nonexistent file to be read. Include predecessor-created targets when a later task changes them. This can be repaired without changing feature scope or requiring user approval.

### B3 — AUTH loader extraction requires a source-file edit outside the task's declared ownership

Dimension: task completeness / artifact wiring. Affects 03-01-T03.

Task 02 places Rules and strict YAML loading in `backend/internal/detection/reducer.go`. Task 03 then extracts the working loader into `backend/internal/detection/config.go`, but Task 03's `<files>` omits `reducer.go`. Moving the implementation requires deleting/replacing its definitions and imports in that source file; creating config.go alone either leaves the implementation unextracted or risks duplicate definitions. The plan-level file list includes reducer.go, but does not resolve task-level ownership or the advertised five-file boundary.

Fix: include reducer.go in Task 03's ownership and reading list, and reconcile the task sizing statement. If retaining five files per task, split the config extraction from the Kafka adapter extraction while keeping a live passing AUTH acceptance after each refinement. Plan 02's analogous extraction already lists its source writer.go and main, so it does not have this specific omission.

### B4 — Live scenario timing does not account for an existing partition watermark

Dimension: cross-plan data contracts / requirement coverage. Affects 03-06-T02, 03-13-T03 and 03-14-T03; relevant to DET-09 and GEN-02 and roadmap criterion 1.

Plan 06 instructs live scenarios to anchor threat windows “far enough in the past” so progress markers expire the longest windows without violating ingest future skew. Plans 13 and 14 then require `make seed` followed by `make seed-scenarios` on preserved volumes and the running detector groups. Current `send.generate_events` makes the newest legacy seed event occur at run_start minus one second. This valid traffic, or prior successful scenario drain markers, can advance a partition's maximum near the current time.

The selected watermark is **partition-wide**, not keyed by source/entity. Fresh source addresses and current-run IDs prevent identity collisions but do not make past events eligible. For example, with prior maximum 12:03:59 the watermark is 12:03:29; AUTH events in a deliberately past 11:55–12:00 window are late regardless of their new principal/IP. An isolated historical fixture can pass while the required preserved-volume demo fails or omits expected hits. No task specifies how live threat timestamps stay at/above the established watermark, or tests the second-run case after prior drain traffic.

Fix: keep fixed historical fixtures for isolated pure tests, and specify an explicit live scheduling/timebase policy compatible with both existing partition progress and ingest skew. Use bounded scheduling/waiting or another concrete non-destructive policy; do not reset detector groups, erase volumes or weaken lateness to make acceptance pass. Add deterministic fixed-clock tests and a repeated preserved-volume scenario run after legacy seed and prior progress markers. Derive the expected manifest from the actual scheduled timebase and retain the exact current-run checks.

## Warnings

### W1 — Validation-map commands drift from the executable plan commands

Dimension: verification derivation / Nyquist command parity. Affects VALIDATION rows 03-03-T02 and 03-04-T02.

Both PLAN `<automated>` commands correctly join unit and integration checks with `&&`. The corresponding VALIDATION rows use `;`, which can return the second command's successful exit after the first command failed when copied into a normal shell. The primary task commands are safe, so this is not a blocker to the implementation itself.

Fix: copy the exact PLAN commands into these validation rows with `&&`; check all 38 row bindings for exact command parity after revision.

### W2 — Initial complete slices and larger plans need more credible context sizing

Dimension: scope sanity. Affects principally 03-01-T02 and 03-02-T02; plans 01/02/08/09/11/14 have 10–13 files.

All plans meet the 2–3 task target and none reaches 15 files. The largest plan has 13 files, and no declared task has more than five. These counts are not proof of either success or failure. In particular, 03-01-T02 implements a new reducer, strict config, consumer ownership/metadata restore, publisher, worker and lifecycle wiring in five files; extracting them later does not make that initial implementation smaller. Plan 02 similarly places complete native storage/transaction logic into writer.go before extracting it. The repeated 35–50% context estimates have no accompanying basis for these complex initial slices.

Fix recommended: retain the first real end-to-end AUTH and two-store paths, but bound their initial acceptance scope explicitly and attach focused recovery/client/storage proofs as early refinements. Avoid forcing unrelated implementations into fewer physical files merely to satisfy a count. Reassess scope after adding actual cooldown state and the missing extraction source edit; split plans if the resulting tasks exceed context limits. No phase split is demanded solely by the existing file counts.

## Requirement and roadmap coverage

Every one of the 14 roadmap IDs appears in plan frontmatter. No relevant PROJECT requirement was silently assigned away; JWT/query/triage/SSE/UI, full observability/benchmarks and Kubernetes remain in their mapped later phases. Coverage below means concrete planned work, not verified implementation.

| Requirement | Primary implementing tasks | Assessment |
|---|---|---|
| DET-01 | 01-T02/T03 | AUTH predicate, tenant/source/principal, unique failures and real Kafka path planned |
| DET-02 | 03-T01/T02 | Exact distinct ports, grouping, duplicates and real SCAN path planned |
| DET-03 | 04-T01/T02 | 299/300/301 REQUEST_FLOOD cases and real output planned |
| DET-04 | 04-T01/T02 | 29/30/31 DENY_SPIKE/action predicate and real output planned |
| DET-05 | 01-T02/T03, 08-T01..T03 | Startup YAML and fingerprint/drained-version transition planned; B3 affects extraction ownership |
| DET-06 | 01-T01/T02, 05, 07, 09, 11 | ADR first; half-open/watermark/equality/late counter and bounded resources planned |
| DET-07 | 01, 03, 04, 05, 07 | Stable component IDs and frozen count/first20/window-end bytes planned |
| DET-08 | 05, 08, 14 | **BLOCKED — B1: candidate-only policy does not deliver selected alert cooldown** |
| DET-09 | 03..07, 13, 14 | Exact isolated fixtures planned; **live repeated-run coverage blocked by B4** |
| ALRT-01 | 01, 02, 10, 11, 13 | Keyed all-ISR alert publication and native CH persistence planned |
| ALRT-02 | 02, 10, 11, 12 | Insert-only unique workflow/list/seq; resolved replay and conflict handling planned |
| ALRT-03 | 01, 07..09, 11, 12 | Pin/prefix/sealed metadata, terminal restore errors, 9/10 and actual third-owner handoff planned; cooldown recovery must be extended under B1 |
| ALRT-04 | 11, 12 | Causal before-publish, after-ack-before-checkpoint and callback-confirmed after-commit tests planned |
| GEN-02 | 06, 13 | Flags, shared Go/Python expectations and private safe sender planned; **B4 live timing gap** |

Roadmap criterion 1 is blocked by B4; criterion 2 is blocked by B1. Criteria 3–5 have concrete proposed proofs, including immutable bytes and immediate CH/PG visibility, but are not credited as executed or passing.

## Plan and dependency summary

| Plan | Tasks | Files | Wave | Depends on |
|---|---:|---:|---:|---|
| 03-01 | 3 | 12 | 1 | — |
| 03-02 | 3 | 10 | 2 | 03-01 |
| 03-03 | 2 | 5 | 2 | 03-01 |
| 03-04 | 2 | 5 | 3 | 03-03 |
| 03-05 | 2 | 6 | 4 | 03-04 |
| 03-06 | 2 | 5 | 1 | — |
| 03-07 | 3 | 8 | 5 | 03-02, 03-05, 03-06 |
| 03-08 | 3 | 13 | 6 | 03-07 |
| 03-09 | 3 | 11 | 7 | 03-08 |
| 03-10 | 3 | 9 | 3 | 03-02 |
| 03-11 | 3 | 10 | 8 | 03-09, 03-10 |
| 03-12 | 3 | 8 | 9 | 03-11 |
| 03-13 | 3 | 9 | 10 | 03-06, 03-12 |
| 03-14 | 3 | 12 | 11 | 03-13 |

All dependency targets exist, the graph is acyclic, and waves equal max(dependency waves)+1. Same-wave declared file sets are disjoint. Shared reducer/config/writer files have transitive ordering. The dependency graph itself passes. The later lifecycle/readiness fixture updates are explicit in 14-T01: current TestMain migrates/builds but does not launch all workers, and early dedicated selections can supply their own topics/groups/rules. I found no certain selected-test setup blocker solely from placing those compatibility updates in plan 14.

## Dimension assessment

| Dimension | Result | Evidence / limit |
|---|---|---|
| 1 Requirement coverage | FAIL | All IDs present, but B1 and B4 prevent full behavior coverage |
| 2 Task completeness | FAIL | Required fields structurally present; B2 mandatory reading and B3 source ownership fail |
| 3 Dependencies | PASS | Valid targets, correct waves, no cycles or same-wave declared overlaps |
| 4 Key links | PASS with B3 correction required | Main→worker→Kafka/checkpoint and writer→CH→PG→source commit are explicitly wired |
| 5 Scope sanity | WARNING | W2; 2–3 tasks each, max13 files; no count-based automatic blocker |
| 6 Verification derivation | FAIL pending revision | User-visible truths exist, but selected cooldown truth is missing and W1 has command drift |
| 7 Context compliance | FAIL for new explicit choice | No Phase3 CONTEXT file; inherited D-15/D-16/storage decisions preserved; B1 contradicts subsequently selected cooldown |
| 7b Scope reduction | FAIL | B1 replaces alert suppression with candidate-only work suppression |
| 7c Architectural tiers | PASS | Detector owns rule/recovery logic; CH immutable evidence; PG workflow/list; producer scenarios; Phase4 browser/query work excluded |
| 8 Nyquist | Structural PASS; approval withheld | All38 tasks have automated commands; creation bindings and sampling below; semantic blockers remain |
| 9 Cross-plan data contracts | FAIL | B4 live scenario timestamps versus existing partition watermark |
| 10 AGENTS.md | SKIPPED | No root AGENTS.md found |
| 11 Research resolution | PASS structurally; revise superseded policy | Open Questions explicitly RESOLVED; cooldown resolution is now outdated under the user's choice |
| 12 Pattern compliance | FAIL alongside B2 | Relevant analogs exist in PATTERNS/context, but tasks universally omit actual source/analog reading lists; add exact references when repairing B2 |

## Dimension 8: Nyquist Compliance

`workflow.nyquist_validation=true`; phase RESEARCH includes Validation Architecture and VALIDATION exists. Every task has a finite, nonempty `<automated>` command. No `MISSING` placeholder or watch command occurs. New tests are assigned to predecessor test tasks or explicitly before implementation in the same task. This is planned creation coverage; `wave_0_complete=false` is accurate until those files exist. `nyquist_compliant=false` should remain unchanged while the revision gate is unresolved.

The commands are in each PLAN's task verify blocks and in VALIDATION's 38-row map. Compact per-task assessment:

| Plan | Wave | Task → automated command family | Status |
|---|---:|---|---|
| 03-01 | 1 | T01 AUTH integration RED; T02 AUTH integration GREEN; T03 AUTH/identity unit + AUTH integration | Present, finite |
| 03-02 | 2 | T01 writer/workflow/schema RED; T02 same GREEN; T03 native store unit + writer/workflow/schema integration | Present, finite |
| 03-03 | 2 | T01 SCAN unit; T02 SCAN/AUTH unit + SCAN integration | Present; validation-map W1 |
| 03-04 | 3 | T01 FLOOD/DENY unit; T02 all-rule/identity unit + volume integration | Present; validation-map W1 |
| 03-05 | 4 | T01 evidence/identity/window/watermark/cooldown unit; T02 same + identity goldens | Present; behavior must change under B1 |
| 03-06 | 1 | T01 scenario pytest RED; T02 scenario/sender pytest + Go fixture identity | Present; live timing must change under B4 |
| 03-07 | 5 | T01 checkpoint/replay/client unit; T02 restart-byte/prefix unit; T03 client unit + checkpoint/retention integration | Present, finite |
| 03-08 | 6 | T01 config/fingerprint/drain/settings unit; T02 same; T03 config restart integration | Present, finite |
| 03-09 | 7 | T01 bounds/metrics unit; T02 bounds/config/checkpoint unit; T03 ops race + capacity integration | Present, finite |
| 03-10 | 3 | T01 identity/fingerprint/ordering unit; T02 same + goldens; T03 conflict/concurrency/partial/workflow integration | Present, finite |
| 03-11 | 8 | T01 both-worker poison/budget unit; T02 detector unit + poison integration; T03 writer unit + both-worker/Phase2 poison integration | Present, finite |
| 03-12 | 9 | T01 causal recovery/crash/rebalance integration; T02 default hook unit + recovery/crash integration; T03 client unit + all causal recovery integration | Present, finite |
| 03-13 | 10 | T01 seed orchestration unittest; T02 stack unittest + Compose config; T03 verifier unit + orchestration + scenario integration | Present, finite |
| 03-14 | 11 | T01 guard unittest; T02 guards/full unit/lint/integration/family gates; T03 preserved-volume stack/legacy seed/scenario/diff | Present, finite |

Sampling: wave1 5/5; wave2 5/5; wave3 5/5; wave4 2/2; waves5–11 each3/3 automated. Every consecutive three-task window therefore has at least two automated checks. No broken `MISSING`→Wave0 link exists. TestDetectorFailpointBuildBoundary is assigned to `failpoint_test.go` in 12-T01 before 12-T02 runs it; the tool verifier unit is explicitly written first in 13-T03. These are not unowned missing tests.

Pure commands use60s ceilings; real dependency selections use6m and full integration retains10m unless measured need justifies a documented finite change. Real-container setup is an acknowledged slow feedback path, not a fast-unit substitute. Plans use pure checks for most refinement tasks; keep full-stack acceptance at the documented gates. No application/test execution was performed to measure these durations.

## Security and substantive recovery assessment

All14 plans contain actionable STRIDE dispositions, trust boundaries, ASVS5.0.0 scoped controls and an explicit high-finding completion block. Plans do not make a compliance certification claim. Installed pinned dependencies are reused; no new external installation is proposed. If that changes, the recorded legitimacy gate must apply before installation.

The reviewed actions explicitly address the difficult contracts: earliest unexpired fired/unfired pin; immediately pre-pin maximum; durable prefix sealed floor; <=1024-byte format/fingerprint/four-version/drained metadata; actual void CommitOffsetsSync callback transport/partition/completeness handling; terminal OnOffsetsFetched failure to lifecycle; ownership/revoke/loss; no reset on retention/truncation; frozen unique evidence and stable component IDs; strict capped installed YAML API; native fresh batches; CH→PG→source commit; per-ID winner/orphan consistency and insert-only resolved replay; bounded state/ledger accounting; low-cardinality internal late counter; and default-noop/private integration hooks.

The required proofs are likewise concrete: repeated advancing pins and arbitrary restart cuts compare immutable bytes; nine incorporated events precede SIGKILL; target ownership actually moves to a different replica; all three durable barriers inspect broker metadata and both sinks; conflict/partial/ambiguity/poison/budget/prefix cases exist; independently nonempty CI groups retain Phase2 regressions. These are appropriate plan commitments, not evidence that the design is already correct. Actual cooldown adds another recovery obligation that must be incorporated into this set under B1.

## Structured Issues

```yaml
issues:
  - id: B1
    plan: "03-05"
    plans: ["03-01", "03-05", "03-08", "03-14"]
    task: 2
    dimension: scope_reduction
    severity: BLOCKER
    description: "Candidate-only window cooldown cannot suppress alerts, and is superseded by the user's explicit selection of actual alert-level cooldown across nearby windows."
    requirement: DET-08
    fix_hint: "Plan precise event-time alert suppression, bounded durable cooldown recovery, and suppression-equivalence restart tests; revise all policy and coverage artifacts."
  - id: B2
    plan: null
    dimension: task_completeness
    severity: BLOCKER
    description: "All38 read_first lists omit actual modified files and required concrete source patterns, violating the invoked workflow's mandatory per-task reading contract."
    fix_hint: "Add each existing target and exact analog/contracts to its task; identify new files and use their real source analogs."
  - id: B3
    plan: "03-01"
    task: 3
    dimension: task_completeness
    severity: BLOCKER
    description: "YAML loader extraction from reducer.go omits reducer.go from task ownership, so the move cannot be completed within the declared file set."
    fix_hint: "Own/read reducer.go in the extraction task and reconcile sizing, or split config and Kafka extractions into live passing refinements."
  - id: B4
    plan: "03-06"
    plans: ["03-06", "03-13", "03-14"]
    task: 2
    dimension: cross_plan_data_contracts
    severity: BLOCKER
    description: "Past-anchored live scenarios can be dropped by partition watermarks advanced by legacy seed or prior runs; fresh entities do not reset partition event time."
    requirements: [DET-09, GEN-02]
    fix_hint: "Specify bounded live scheduling compatible with retained watermark and ingest skew; test legacy seed plus repeated preserved-volume scenario runs without resets."
  - id: W1
    plan: null
    file: "03-VALIDATION.md"
    dimension: verification_derivation
    severity: WARNING
    description: "Validation rows03-03-T02 and03-04-T02 use semicolon command chaining while their PLAN automated commands correctly use &&, permitting a copied validation command to hide unit failure."
    fix_hint: "Synchronize the validation rows with exact PLAN commands and check all38 bindings."
  - id: W2
    plan: "03-01"
    plans: ["03-01", "03-02", "03-08", "03-09", "03-11", "03-14"]
    dimension: scope_sanity
    severity: WARNING
    description: "Complex initial live slices and10–13-file plans have unsupported35–50% context estimates; packing adapters into fewer files does not reduce implementation scope."
    fix_hint: "Retain real first slices, attach focused proofs early, and reassess/split context scope after cooldown and extraction corrections."
```

## Recommendation

Revise the four blockers and synchronize the validation map. The user's cooldown choice is already authorized; no additional approval is needed to make that decision concrete in the research and plans. Re-run the independent Revision Gate on the changed policy, task ownership, timing and complete dependency/coverage set. If the bounded revision loop reaches its cap or stalls, escalate with the remaining concrete findings. Do not execute Phase3 or mark validation complete from this report.
