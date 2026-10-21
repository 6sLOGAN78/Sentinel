---
phase: 03-detection-alert-pipeline
verified: 2026-10-21T04:24:57Z
status: gaps_found
score: 58/59 must-haves verified; latest repeat acceptance gate failed
historical_score: 59/59 at 80d2d873762e4832abf4c87e2d36c22d359d8337
roadmap_score: 5/5
plan_truth_score: 53/54 current; 54/54 historical
artifact_declarations: 72/72
unique_artifact_paths: 68
key_links: 46/46
requirements_satisfied: 14/14
overrides_applied: 0
tested_commit: 80d2d873762e4832abf4c87e2d36c22d359d8337
hosted_ci_run: 39515550386
hosted_ci_conclusion: success
re_verification_pending: true
latest_hosted_ci_run: 39544892831
latest_hosted_ci_head: eee632f
latest_hosted_ci_conclusion: failure
scoped_repair_head: e3423373a80d1bbcb8737be99fcb08d5cfd22a11
scoped_repair_review: clean; two test files only; acceptance still pending
re_verification:
  previous_status: passed
  previous_score: 59/59
  gaps_closed: []
  gaps_remaining:
    - "Latest repeat CI integration acceptance must pass after diagnosed repair and independent review."
  regressions:
    - "Required full integration repeat failed at TestDetectorCooperativeAcquisition retained-owner fixture assertion."
gaps:
  - truth: "Full unit/static/integration gates and preserved-volume current-run scenarios have recorded results with no invented pass/performance claims."
    status: failed
    reason: "Latest repeat CI 39544892831 at eee632f failed its required integration job: TestDetectorCooperativeAcquisition had an empty retained map. Earlier successful execution remains valid historical evidence; this does not establish a new runtime defect."
    artifacts:
      - path: "backend/test/detector_rebalance_test.go"
        issue: "Line 94 retained-owner precondition failed in the latest full integration repeat. Its causal readiness collector is repaired and independently reviewed at e342337; full replacement acceptance remains pending."
    missing:
      - "Required replacement acceptance gates and actual new hosted CI success, followed by root release for re-verification."
---

# Phase 3: Detection & Alert Pipeline Verification Report

**Phase Goal:** As a system operator, I want to receive deterministic, evidence-backed alerts from malicious traffic, so that crashes, restarts and replay cannot lose or duplicate detections.

**Status:** gaps_found — re-verification pending. **BLOCKER: latest repeat CI acceptance failed; Phase 3 closure is held.** No override was requested or applied.

> **Parent follow-up hold:** Root reported new counterevidence from [CI 39544892831](https://github.com/6sLOGAN78/Sentinel/actions/runs/39544892831) at `eee632f`. Eight jobs succeeded, including all 46 Phase 3 families and Compose; the required full integration job failed (package 1684.173s, make exit 2). `TestDetectorCooperativeAcquisition` failed after 2.85s at `backend/test/detector_rebalance_test.go:94`: the retained map was empty, failing “survivor must retain incorporated state while acquiring additions.” This was an assertion failure, not a timeout. Root retained the failed log at `.local/phase3-acceptance/ci-repeat-39544892831-failed.log`. The assigned specialist's two-file test repair is now independently reviewed, including the scoped verifier assessment below; required replacement full/hosted gates remain pending. Do not restore `passed` until those gates actually pass and root explicitly releases re-verification.

The prior 59/59 implementation/source findings and successful nine-job run at `80d2d87` remain historical validated evidence below; they are not erased or presented as the latest repeat result. Current accounting holds **03-14.T2** failed at its acceptance gate (**58/59**, **53/54** plan truths). No new runtime implementation failure is inferred from the empty fixture snapshot. The original passed report is preserved in commit `800b084`. All remaining VERIFIED rows and execution tables below describe that earlier verified baseline unless explicitly updated by the hold.

## User Flow Coverage

The roadmap sets `mode: mvp`; the registered `user-story.validate` query returned `true`. The outcome is an operator-observable backend flow, without a Phase 4 dashboard dependency.

| Step | Expected | Evidence in the codebase and executed flow | Status |
| --- | --- | --- | --- |
| Start the preserved stack | Detector and alert-writer become ready with real dependencies and startup rules | Compose orders infrastructure/topics/migrations/apps; detector main constructs the real pool, rules, consumer, publisher, DLQ and worker; readiness checks restored current assignments. Hosted Compose startup/checker/migration passed. | VERIFIED |
| Run legacy seed and malicious scenarios | Current acknowledged traffic produces the exact expected alerts; benign traffic produces none | `seed_scenarios.py:415` acquires caught-up context, sends scheduled traffic and invokes the native verifier; `check-alerts/main.go:739` reconciles Kafka/CH/PG, current event receipt time and prior rows. Hosted Compose executed legacy seed and both preserved scenario commands successfully. | VERIFIED |
| Inspect alert evidence | Stable identity, event-time windows/count and bounded ordered evidence | `detection/evidence.go:15` freezes canonical payload; `candidate.go:38` binds source/trigger; real/static fixture assertions compare complete payloads, keys and both stores. | VERIFIED |
| Crash/restart and transfer ownership | Nine incorporated failures survive replay and detect event ten; crash cuts retain one visible identity | Actual causal barriers, broker metadata, old/new member observations and native store assertions in `detector_recovery_test.go` and `detector_rebalance_test.go`; all exact families passed in hosted Phase 3. | VERIFIED |
| Replay after analyst resolution | Workflow state and sequence remain unchanged; immutable conflicts cannot replace a winner | Permanent reservation is committed before any CH effect; workflow creation is post-CH and insert-only. Tests resolve/assign/version rows and compare their entire JSON before/after replay. | VERIFIED |
| Outcome | Crashes, restarts and replay preserve deterministic detections and visible deduplication | The complete path below is substantive and connected; full integration, all 46 exact Phase 3 families and preserved operator acceptance passed on the tested candidate. | VERIFIED |

## Method and evidence boundary

This verifier independently read all 21 numbered PLANs and their 54 frontmatter truths, all 21 eventual SUMMARYs, all five roadmap success criteria, the 14 requirement descriptions, approved cooldown/research/ADR scope, source-review dispositions, reservation evidence/rollout record and registered security audit. SUMMARY claims were discovery/context, not implementation proof. The supplemental reservation implementation plan is an approved repair, not a 22nd SDK plan.

Direct source tracing covered reducer predicates/admission/evidence/checkpoints; Kafka acknowledgement, metadata and ownership callbacks; PG decisions/cursors/fences/handover; writer reservation/native stores/workflow/source commits; private snapshot/scheduling/sending/reconciliation; concrete command/Compose wiring and relevant positive/negative test assertions. Fresh registered queries checked every numbered plan's artifacts and links. Test artifacts were inspected for actual value/error/offset/state assertions and traced to runnable packages/required selectors, not accepted solely because files exist.

Per the serialized closeout assignment, this verifier ran no tests, builds, compilation, live services, scanner, staging or commits. Execution belongs to root/executors/hosted CI. The verifier independently inspected persistent raw local logs and root-captured GitHub API metadata plus the completed watch log. A fresh verifier `gh` lookup encountered transient API connectivity failure; it is not represented as a successful query. Root's saved API response proves the exact SHA, completed/success status and nine successful jobs, independently corroborated by `WATCH_RETURNED_EXIT=0` and completed job/step output. This is stronger evidence than SUMMARY narration.

At initial verification, tested source was `80d2d873762e4832abf4c87e2d36c22d359d8337`. Subsequent commits then inspected through `ff7fbc3` changed only STATE/VALIDATION/CI-closeout documentation; no implementation delta separated that reviewed baseline from the candidate. The new follow-up diagnosis/repair is outside this historical source statement and awaits re-verification. Existing user changes in `.planning/config.json` and `.serena/` were preserved. Only this report is written by the verifier.

### Scoped repair assessment — acceptance held

The verifier read the actual complete new `backend/test/detector_owner_snapshot_test.go`, the full `eee632f..e342337` change to `backend/test/detector_rebalance_test.go`, affected callers/assertions, `groupOwns`/`observationAtNine`, the private observation reader and real Kafka successful-commit observation contract. It also read the independent deep two-file review `03-REVIEW-COOPERATIVE-REPEAT.md` at `64fd7c8`, the repair evidence and persistent RED/GREEN/native/static/script/scanner logs. This is source/evidence verification, with no verifier test/build/live execution or duplicate implementation work.

**Assessment: the scoped test-readiness defect is addressed; no new runtime defect or weakened acceptance assertion was established.** The implementation diff from the original production baseline contains only the two named test files. Changes after the reviewed GREEN are tracking/evidence documentation. The prior 59 implementation/source truths, 72 artifact declarations, 68 paths, 46 links and 14 requirement mappings therefore remain substantive and connected. Only 03-14.T2's replacement whole-acceptance gate remains open; current status/score stay `gaps_found`, 58/59.

The old collector could accumulate all twelve owners across different Eventually polls or accept a transition with only one process incorporated. The repaired collector clears the output map each poll, requires a Stable two-member group and two process observation streams, accepts only configured-topic partitions 0–11 with committed `processed_next=9` and actual current broker ownership, and requires nonempty contribution from both streams, twelve unique partitions and two current member IDs. Kafka commit success precedes private observation writing; the reader deliberately skips incomplete JSON. Broker offsets or member count alone cannot replace this readiness evidence.

The downstream retained-map assertion is still present. Actual acquired restore, different old/new member, checkpoint equality, readiness, retained token/generation and PG fence, canonical Kafka/CH/decision bytes and the two distinct retained/acquired identities remain asserted. The 20-second/25ms readiness polling budget is unchanged. The new fixture obtains public decoded assignments from a real two-member group; its controlled observation schedules are a model of the readiness gap, not a reconstruction of the failed coordinator schedule. It preserves positive complete coverage plus incomplete/wrong-owner controls. Sequential decoded-assignment mutation is restored, and polling cleanup cancels/closes/joins.

| Scoped evidence | Actual inspected result | Meaning and limit |
| --- | --- | --- |
| Tests-first `c8b4310`, exact snapshot test | `cooperative-snapshot-red.log`: three intended assertion failures, package 24.804s, EXIT=1 | One incorporated owner, stale prior-poll coverage and in-progress rebalance expose the old collector. The missing START marker is not invented. |
| GREEN `e342337`, same snapshot test | `cooperative-snapshot-green.log`: all six cases pass, package 25.098s, EXIT=0 | Direct collector regression is repaired; no compilation/setup failure is used as RED. |
| Original native acquisition and third-replica tests, each `-count=3` | `cooperative-native-repeat.log`: six actual top-level PASS results, package 70.937s, EXIT=0 | Fresh fixture/topic/process per call; shared invocation dependencies. This does not establish whole-suite/hosted success. |
| Independent deep review | `03-REVIEW-COOPERATIVE-REPEAT.md`: two source files, zero findings | Production, hooks, goldens, payload assertions and budgets unchanged. |
| Root post-repair static/scripts/scanner | `cooperative-final-static.log`: STATIC_RETURNED_EXIT=0; scripts: 44 tests, 75.114s, returned 0; scanner: 436 commits, no leaks, returned 0 | Actual bounded local checks; no new scanner exclusion. Cached unit package results are not described as uncached native repetitions. |
| Replacement full aggregate / new hosted run | Pending at this scoped assessment | Root reports the full process still active with a retained log despite an unexplained wall-clock gap. No returned full exit, elapsed compliance, timeout, interruption or success is inferred from timestamps or child progress. New hosted success and root release are still required. |

The failed hosted log lacks an atomic group description at the assertion; the exact historical coordinator timing is not claimed as reconstructed. Historical observation files checked against one broker description are also not an atomic cross-system generation snapshot; unchanged downstream token/generation/fence assertions remain necessary. These proof limits are retained without inventing a production failure or waiving the acceptance gate.

## Goal Achievement

### Roadmap observable truths

| # | Roadmap contract | Status | Evidence |
| --- | --- | --- | --- |
| SC1 | Generator scenarios (brute force, port scan, request flood, deny spike) each trigger their rule exactly as expected, benign traffic triggers none, and boundary and lateness fixtures (exactly at the watermark limit and at +1ms) behave as the window ADR specifies | VERIFIED | Exact predicates in `detection/{reducer,scan,volume}.go`, unique-ID/distinct-port admission in `bounds.go`, strict `< maximum-30s` late classification and half-open epoch windows. Shared fixture has nine cases, 2,327 inputs and 20 exact expected payloads; Go/Python compare independent static bytes. Real static all/boundary/lateness and preserved live edges pass. |
| SC2 | Each alert carries severity, summary, event_count, window start/end, ≤20 sample event IDs, rule_id/version and a deterministic SHA-256 alert_id. Cooldown suppresses repeats for the same entity while separate windows remain investigable | VERIFIED | `freezeEvidence/windowAlert` sets all fields, first-20 order, count at first crossing and `created_at=window_end`; unchanged component-based `contracts.DedupeKey` supplies ID. PG `Decide` returns immutable old decisions before reading a cursor; new monotone event-time cooldown suppresses nearby windows and allows equality. Native Visible/Expiry/Replay families prove 299s/301s suppression and later distinct identity. |
| SC3 | Killing the detector after 9 of 10 failures and then sending the 10th event still produces the alert once, including across a rebalance when a third replica joins | VERIFIED | `TestDetectorMidWindowRecovery` observes incorporated ninth record, SIGKILLs/joins, restarts and asserts tenth-event canonical Kafka/CH/PG result. `TestDetectorThirdReplicaRebalance` seeds twelve partitions, proves actual target oldOwner != newOwner and restoration before event ten. Cooperative acquisition additionally proves retained and acquired live windows. |
| SC4 | Failpoint tests at each crash boundary (before publish, between publish and offset commit, after commit) show exactly one visible alert per dedupe key. A replayed detection never reopens or duplicates a resolved alert | VERIFIED | Worker hooks surround actual PublishCanonical/all-ISR acknowledgement/successful checkpoint, with PG-before/after-commit cuts also exercised. Native immediate unique count/detail, complete canonical bytes and entire resolved workflow row are asserted. Insert-only workflow and committed immutable reservations preserve authority through uncertain CH effects. |
| SC5 | Alerts reach security.alerts.v1, immutable detections land in ClickHouse, and a workflow row with unique alert_id, seq and denormalized list fields exists in PostgreSQL. Rule thresholds load from config/rules.yaml at startup | VERIFIED | Main constructors connect real clients/stores. Publisher uses tenant\|alert_id and all-ISR ProduceSync; writer loads committed saved bytes, acknowledges native CH, ensures PG then commits contiguous source prefix. Migrations 003/004/005/006 and ClickHouse 003 are embedded/applied. Strict startup `LoadRules` and read-only Compose mount use the four checked-in defaults. |

**Historical roadmap score: 5/5.** Plan-specific truths below add recovery/configuration/operational detail; none reduces the roadmap contract. The original combined accounting was **59/59**. The latest repeat acceptance failure now holds 03-14.T2 failed, giving **58/59 current**, without asserting that the earlier implementation findings were disproved.

### Plan-specific observable truths

The identifiers preserve each plan's frontmatter order. Wording is compacted here without removing its behavioral obligation.

| Truth | Required behavior | Status | Concrete evidence |
| --- | --- | --- | --- |
| 03-01.T1 | Ten unique tenant/source/principal failures in five minutes publish one valid AUTH alert | VERIFIED | `reducer.go:130`, `bounds.go:135`, `evidence.go:54`; real AUTH test asserts 9/10/11 and exact payload. |
| 03-01.T2 | Canonical Kafka key and acknowledged output before checkpoint | VERIFIED | `kafkax/alerts.go:28`, `detector/worker.go:418,449,282`; callback-authoritative checkpoint. |
| 03-01.T3 | Both prerequisite ADRs precede rule code; startup config controls AUTH | VERIFIED | Git history has ADR/test commit `71b05a8` before rule implementation `aff6c34`; main calls `LoadRules`, checked-in version-1 settings. |
| 03-02.T1 | Real Kafka alert becomes immediately visible once in CH and one PG workflow | VERIFIED | Native aggregate Get/CountUnique, unique PG identity and real blocked-PG pipeline assertions. |
| 03-02.T2 | Replay preserves every mutable workflow field and source waits for both stores | VERIFIED | `reservation.go:125,131`, `writer.go:173`; full-row `TestAlertWorkflowReplay`. |
| 03-03.T1 | Twenty exact ports trigger SCAN; repeated ports alone do not | VERIFIED | Exact ID/port sets in `bounds.go:149,159,183`, `scan.go:14`; 19/20/21 and repeated-port assertions. |
| 03-03.T2 | Null/wrong predicates and separate tenants/destinations stay isolated | VERIFIED | `scanMatches`, rule-qualified tenant/source/destination window key; ScanPredicates cases. |
| 03-04.T1 | 300 unique requests/minute produce one source-scoped FLOOD | VERIFIED | `volume.go:25`, threshold/window YAML; FloodThresholds and real Volume. |
| 03-04.T2 | 30 policy deny events/two minutes produce DENY; unrelated events do not | VERIFIED | `volume.go:27` checks exact type/action, shared unique-ID counting; DenyThresholds/Volume. |
| 03-05.T1 | First crossing freezes all immutable bytes, first 20 ordered IDs and event-time creation | VERIFIED | `bounds.go:172,194`, `evidence.go:15,57`; complete-byte evidence/replay assertions. |
| 03-05.T2 | Candidate freezes trigger/source/bytes; final eligibility is durable | VERIFIED | Private Candidate fields/copy accessor, fixed ordinals; runtime always calls PG `Decide` before publication. |
| 03-06.T1 | Same inputs produce identical source order/IDs and expected payloads | VERIFIED | Pure seeded `scenarios.py:125`, independent encoding at `:98`; fixture equality and unchanged Go goldens. |
| 03-06.T2 | Benign zero and exact boundary/late accepted/dropped inputs | VERIFIED | Explicit negative/boundary/lateness construction and static expectation sets, actual default-binary scenario reconciliation. |
| 03-06.T3 | Scenario flags reuse authenticated bounded sender/private artifacts and safe synthetic identities | VERIFIED | CLI `_run_scenario` validates before key loading, invokes existing deliverer; no-follow private writers and documentation IP fixtures. |
| 03-07.T1 | Every tested restart cut preserves canonical union/live state, including advancing pins/closed history | VERIFIED | `replay_test.go:70,115,140` compares complete independent fixture bytes, state and checkpoints at all cuts; successive restarts advance pins. |
| 03-07.T2 | Pre-pin maximum/sealed floor restore; invalid metadata or unavailable history terminates without reset | VERIFIED | Strict nine-field decoder and Restore; Kafka start/end bounds, NoResetOffset, fatal callback; real corruption/retention/shorter-history cases. |
| 03-08.T1 | Invalid/oversized/unknown/duplicate/unsupported YAML fails before consumption | VERIFIED | 64KiB capped node validation, KnownFields and one-document EOF in `config.go:45`; startup load before consumer. |
| 03-08.T2 | Equal-version edits/active or mixed handovers fail; verified drain plus increased versions succeeds | VERIFIED | Saved semantic manifest/exact drain, stable ordered PG fence locks, version comparison and sole-member checks. |
| 03-08.T3 | Restart and old-config drain prevent historical reinterpretation | VERIFIED | `TransitionCheckpoint` persists four aligned effective gates; real restart and 300→600 enlargement/equality/ledger handover assertions. |
| 03-09.T1 | Aggregate windows/IDs/ports/prefix state caps reject before irreversible admission | VERIFIED | Shared Bounds prospective reserve before mutation; overflow-safe resource admission; 10k/100k/64MiB/4096 cases and unchanged checkpoint assertions. |
| 03-09.T2 | Late counter is fixed/internal; poison and idle do not advance time | VERIFIED | `recordLateness`, optional numeric ops metric, event-free AcknowledgePoison and snapshot-only idle paths. |
| 03-09.T3 | Fatal errors withdraw readiness/nonzero; sixteen-worker and response deadline remain intact | VERIFIED | Lifecycle fail begins draining; Ready compares restored/current assignment; unchanged bounded readiness handler and native failure tests. |
| 03-10.T1 | Partial PG failure/uncertain CH send recovers one visible identity and unchanged workflow | VERIFIED | Source retained on sink error; reservation survives uncertainty, identical native replay repairs PG. Native PartialStorage/ACK-loss/ambiguity coverage. |
| 03-10.T2 | Concurrent replay preserves workflow; competing tenant/content quarantines before conflicting CH | VERIFIED | Permanent global-ID reservation, phase-two reload, fingerprint/tenant preflight and ErrConflict DLQ before prefix eligibility. |
| 03-10.T3 | Pipe/Unicode AUTH suffix survives identity reconstruction | VERIFIED | `identity.go:24` uses first `strings.Cut`, then full suffix in component-based DedupeKey; identity/fixture tests. |
| 03-11.T1 | Permanent event/alert poison reaches acknowledged bounded DLQ before source eligibility | VERIFIED | Both classifiers use installed strict contracts and real DLQ; detector AcknowledgePoison follows success; writer persistRecord quarantines before return. |
| 03-11.T2 | DLQ outage blocks only unsafe partition prefixes; durable other partitions may advance | VERIFIED | Sorted per-partition processing stops failed partition and records only completed checkpoints/last records; real/unit prefix assertions. |
| 03-11.T3 | One aggregate effects/retry budget reserves final commit; transient failures are not poison | VERIFIED | Shared retry state/context per batch, parent-bound final commit; sink/PG/capacity/history errors return operational failure. |
| 03-12.T1 | Incorporated nine, SIGKILL, restart and tenth give one visible alert | VERIFIED | Actual after-record barrier and broker predecessor metadata, default-binary restart, exact native effects. |
| 03-12.T2 | Third replica actually acquires target, restores then detects tenth | VERIFIED | Broker ownership plus private assigned/revoked/restored/committed observations in real twelve-partition test. |
| 03-12.T3 | All three exact publish/checkpoint cuts yield one immediate CH identity and PG row | VERIFIED | Hooks are placed at real effect boundaries; crash tests inspect pre-cut absence/presence and post-replay bytes/whole workflow. |
| 03-13.T1 | Healthy workers start after topics/migrations with read-only rules/distinct groups | VERIFIED | Compose services/dependencies/mount and Make dev-up order; actual local and hosted stack checks. |
| 03-13.T2 | seed-scenarios checks exact acknowledged current run across Kafka/CH/PG and benign zero | VERIFIED | Offset capture before send; strict expected-set/native workflow reconciliation, original receipt-time lower bound and proof booleans. |
| 03-13.T3 | Legacy 1000-event seed/private artifacts/preserved volumes remain usable | VERIFIED | Actual legacy and repeated Make flow, full prior event/CH/workflow snapshots and disjoint current IDs; hosted Compose passes. |
| 03-14.T1 | CI rejects empty/failed exact families and retains all Phase 2 groups | VERIFIED | Separate exact 46-family calls, real nonempty runner with pipe failure propagation, independent guard registry and seven retained selectors; hosted steps succeed. |
| 03-14.T2 | Full/static/integration/operator gates have actual recorded results | FAILED — BLOCKER; re-verification pending | Previously VERIFIED at 80d2d87. Repeat 39544892831 failed at the empty retained-owner snapshot assertion. Test-only repair e342337 and independent review are complete; actual replacement full/hosted green gates and root release remain required. No runtime defect is inferred. |
| 03-14.T3 | Docs state cooldown/replay/retention/config and later seq-order limitation | VERIFIED | `docs/rules.md`, operations/demo and architecture explicitly document those constraints; temporal pending statements predate the separate CI closeout. |
| 03-15.T1 | Strict extracted AUTH loader preserves threshold evidence | VERIFIED | Single LoadRules implementation in config.go, parser and AUTH threshold/canonical assertion families. |
| 03-15.T2 | No duplicate loader definitions; actual AUTH acceptance retained | VERIFIED | Definitions removed from reducer; main loads extracted package; default real AUTH executed. |
| 03-16.T1 | All-ISR exact publication has finite buffers/delivery | VERIFIED | Concrete NewAlertProducer options, PublishCanonical copies stored bytes and awaits ProduceSync; publisher option/failure assertions. |
| 03-16.T2 | Void commit callback/fetched metadata failures cannot report success | VERIFIED | Missing callback default error, full response validation, unchanged-token adoption, `fail`→onFatal/lifecycle/Poll/Ping. |
| 03-17.T1 | Each CH attempt uses fresh native batch and bound tenant queries | VERIFIED | `alerts.go:45` PrepareBatch with unsent Abort; named parameters and immediate aggregate reads; fresh-batch/query tests. |
| 03-17.T2 | Extracted stores retain immediate uniqueness and resolved replay | VERIFIED | Main injects native stores into public worker; real pipeline/replay/schema and exact source ordering assertions. |
| 03-18.T1 | Detector requires bounded real PG and fails/not-ready on outage | VERIFIED | Main database creation/readiness, batch Ping and real operational store; paused-PG process proof preserves source. |
| 03-18.T2 | Compose/test fixtures provide real PG and migration order | VERIFIED | postgres-migrate prerequisite and service environment, private migrated integration configurations and actual ready/startup gates. |
| 03-19.T1 | AUTH 299s/301s yields allow plus suppression; later window has distinct ID | VERIFIED | Native Visible test checks Kafka/CH/workflow/ledger, event-time cooldown policy and canonical identities. |
| 03-19.T2 | Old decisions immutable; equality allows new window; suppressed window never retries | VERIFIED | Find-before-cursor, SQL UPDATE rejection, frozen reducer identity and Cooldown equality; Expiry/Replay native assertions. |
| 03-19.T3 | PG error retains Kafka checkpoint and fails without DLQ | VERIFIED | Decide error stops effect before Acknowledge/checkpoint; real row-lock/outage/restore assertions inspect entire metadata and zero DLQ. |
| 03-20.T1 | Four PG-decision/publish/checkpoint cuts preserve disposition/bytes | VERIFIED | Real precommit locked-uncommitted and released postcommit observations, SIGKILL recovery, old allow/suppress replay. |
| 03-20.T2 | Actual owner transfer and stale concurrent requests cannot create wrong decisions | VERIFIED | Current Kafka membership/token plus locked PG fence/source order; native moved-target stale requests leave complete rows unchanged. |
| 03-20.T3 | PG restore behind newer Kafka checkpoint fails before new allow/output | VERIFIED | Exact immutable frontier row/fence verification before reducer restore; real lost-299s-seed/340s attempt refuses with unchanged pin/no output. |
| 03-21.T1 | Private helper uses ends/processed_next and caught-up maximum, not replay pin | VERIFIED | `catchup.go:45,147` checks stable six-partition history and complete format2; maximum=sealed+lateness; missing scan cannot fabricate effect completion. |
| 03-21.T2 | Live base exceeds maxima, threats precede markers, waits fit 15m | VERIFIED | `_snapshot_base`, `live_scenario`, exact derived schedules, full delivery reservations and POSIX hard deadline; operator reserves final minute. |
| 03-21.T3 | Fake clocks prove refusal/waits and preserved legacy/scenario/repeat executes | VERIFIED | Scheduling assertions, real sender reuse and actual same-stack repeat test; hosted Compose independently repeats documented commands. |

### Required artifacts: existence, substance and wiring

Registered `verify.artifacts` returned **72/72 passing declarations**, **68 unique paths**. Every declaration is listed below; repeated paths remain repeated plan declarations. Each row is **VERIFIED** at all applicable levels: actual implementation/assertions, runtime/test/document consumer and execution evidence above. Paths sharing a prefix use brace notation to name the exact files, not a wildcard scope.

| Plan | Declared artifacts | Substantive implementation and consumer |
| --- | --- | --- |
| 01 (5) | `docs/decisions/{0007-event-time-state-pin-recovery,0008-alert-list-source-and-conflicts}.md`; `backend/internal/detection/reducer.go`; `backend/internal/detector/worker.go`; `backend/test/detector_pipeline_test.go` | Accepted policy, actual event state/effect loop and exact default-binary Kafka test; mains construct worker. |
| 02 (4) | `backend/migrations/clickhouse/003_alerts.sql`; `backend/migrations/postgres/003_alert_workflows.sql`; `backend/internal/alertwriter/writer.go`; `backend/test/alertwriter_pipeline_test.go` | Real schemas, bounded source/effect processing, blocked-PG and resolved-replay assertions; migration runners/main wire them. |
| 03 (3) | `backend/internal/detection/{scan.go,scan_test.go}`; `backend/test/detector_scan_test.go` | Exact complete scan predicate and cardinality/isolation/evidence assertions; reducer dispatch and native family. |
| 04 (3) | `backend/internal/detection/{volume.go,volume_test.go}`; `backend/test/detector_volume_test.go` | Both exact type/action predicates and thresholds/duplicates/isolation; live Volume family. |
| 05 (4) | `backend/internal/detection/{evidence.go,candidate.go,evidence_test.go,candidate_test.go}` | Canonical builder, private frozen candidate and byte/source/copy assertions; bounds and worker use them. |
| 06 (4) | `testdata/detection/scenarios.json`; `generator/src/sentinel_generator/scenarios.py`; `generator/tests/test_scenarios.py`; `backend/internal/contracts/scenario_identity_test.go` | Complete nine-case fixture, deterministic builder, independent Python/Go bytes/identity checks; CLI and replay/native scenarios consume them. |
| 07 (4) | `backend/internal/detection/{checkpoint.go,replay_test.go}`; `backend/internal/kafkax/detector_consumer_test.go`; `backend/test/detector_checkpoint_test.go` | Strict complete metadata decoder, all-cut/live-state proof, callback error tests and native broker metadata/history tests. |
| 08 (4) | `backend/internal/detection/{config_test.go,transition.go}`; `backend/test/detector_config_test.go`; `docs/rules.md` | Complete parser/fingerprint tests, aligned persisted handover and actual startup/restart procedure. |
| 09 (4) | `backend/internal/detection/{bounds.go,bounds_test.go}`; `backend/internal/opshttp/metrics.go`; `backend/test/detector_capacity_test.go` | Prospective process accounting, cap/rejection invariants, actual numeric ops route and real failure/metric/retained-owner test. |
| 10 (4) | `backend/internal/alertwriter/{identity.go,identity_test.go}`; `backend/test/{alertwriter_conflict_test.go,alertwriter_partial_test.go}` | Canonical identity/fingerprint, concurrent winner/quarantine, native partial/ambiguous recovery; real writer path. |
| 11 (4) | `backend/internal/detector/poison.go`; `backend/internal/alertwriter/poison.go`; `backend/test/{detector_dlq_test.go,alertwriter_dlq_test.go}` | Real classifiers/shared retry bounds, acknowledged prefixes, trusted envelope/outage/400-record assertions. |
| 12 (4) | `backend/internal/detector/{failpoint.go,failpoint_integration.go}`; `backend/test/{detector_recovery_test.go,detector_rebalance_test.go}` | Build-separated no-op/private hooks plus causal kills/actual target transfer and complete stores/metadata assertions. |
| 13 (4) | `deployments/compose/docker-compose.yml`; `scripts/seed_scenarios.py`; `backend/tools/check-alerts/main.go`; `backend/test/scenarios_pipeline_test.go` | Actual startup/mount, authenticated finite operator flow, native exact current/prior reconciliation and default-binary fixture/private-PG proof. |
| 14 (4) | `scripts/{check-phase3-tests.sh,test_phase3_guards.py}`; `.github/workflows/ci.yml`; `docs/demo.md` | 46 exact required selectors, independent empty/failure/config guards, actual nine-job workflow and executable documented operator commands. |
| 15 (2) | `backend/internal/detection/{config.go,auth_test.go}` | Single strict loader used at startup; substantive 9/10/duplicates/null and canonical evidence assertions. |
| 16 (2) | `backend/internal/kafkax/{alerts.go,detector_consumer.go}` | Bounded all-ISR canonical publisher and strict metadata/membership consumer; compatibility wrappers connect worker/main. |
| 17 (2) | `backend/internal/storage/{clickhouse/alerts.go,postgres/alerts.go}` | Fresh native CH blocks/aggregate reads, bounded PG locks/insert-only workflow; concrete writer injection. |
| 18 (2) | `backend/cmd/detector/main.go`; `deployments/compose/docker-compose.yml` | Real PG/lifecycle/readiness and migrated deployment; no final no-op/in-memory operational store. |
| 19 (3) | `backend/internal/storage/postgres/detection_decisions.go`; `backend/migrations/postgres/004_detection_decisions.sql`; `backend/internal/detector/worker.go` | Locked immutable decisions/cursors/fences with bounded finalization, durable schema and mandatory decision-before-publish path. |
| 20 (3) | `backend/test/detector_cooldown_test.go`; `backend/internal/storage/postgres/detection_decisions.go`; `backend/internal/detector/failpoint_integration.go` | Four causal cuts, real outage/owner/restore counterexamples, exact frontier enforcement and bounded private observations. |
| 21 (3) | `backend/tools/detection-state/main.go`; `generator/src/sentinel_generator/live_schedule.py`; `backend/test/scenarios_preserved_test.go` | Read-only Kafka observer, legal clock/deadline scheduler and actual legacy/two-Make same-state proof. |

Additional approved repair artifacts were also traced: `alertwriter/reservation.go`, `storage/postgres/alert_reservations.go`, migration `006_alert_reservations.sql`, build-separated writer hook and reservation/native/proxy/schema tests. They implement the permanent authority invariant, not a stub reservation facade. The CI repair's `.gitleaksignore` is a single exact historical prose fingerprint; the reviewed artifact union is 130, not 129 after that addition.

### Key Link Verification

Registered text matching verified **41/46**; the five false negatives below were resolved manually against actual source and callers. Broad extraction regex matches were not treated as proof of behavior. Every declared link is accounted for by its plan/order here; **46/46 WIRED**.

| Links | Connection and checked behavior | Status |
| --- | --- | --- |
| 01.K1 | Detector main `detector.New`→`worker.Run`; terminal Run error→draining/lc.Fail. SDK escaped-pattern false negative; actual calls at main:70,75. | WIRED |
| 01.K2 | Worker→alert topic through PublishCanonical/all-ISR success before CommitCheckpoints. | WIRED |
| 01.K3 | Worker constructor wrapper→extracted Kafka consumer→PreCommitFnContext/OnOffsetsFetched. SDK searches old owner; adapter contains both real hooks. | WIRED |
| 02.K1 | Alert-writer main→native stores and `alertwriter.New/Run` at main:89,94. SDK escaped-pattern false negative. | WIRED |
| 02.K2 | Writer→committed reservation→native CH acknowledgement→insert-only PG commit→source commit. | WIRED |
| 03.K1–K2 | Reducer dispatch→scanMatches/exact port set; enabled YAML PORT_SCAN definition loaded by main and passed into state. | WIRED |
| 04.K1–K2 | Reducer dispatch→volumeMatches; actual FLOOD/DENY YAML definitions reach loaded thresholds/windows. | WIRED |
| 05.K1–K2 | Admission crossing→freezeEvidence once; ApplySource→source-aware frozen Candidate→PG decision. | WIRED |
| 06.K1 | CLI scenario branch imports/calls builder while retaining existing authenticated sender. | WIRED |
| 06.K2 | Python tests' FIXTURES constant points to `testdata/detection/scenarios.json`, loaded into CASES/full equality tests. SDK escaped-pattern false negative. | WIRED |
| 06.K3 | Go fixture test reads same JSON and recomputes unchanged DedupeKey independently. | WIRED |
| 07.K1 | Strict checkpoint metadata→Restore pre-pin maximum/sealed floor→bounds skips already sealed windows. | WIRED |
| 07.K2 | Kafka OffsetCommit/Fetch→atomic metadata request attachment and strict complete response/history restore, NoResetOffset. | WIRED |
| 07.K3 | Terminal adapter error→onFatal/lifecycle and Poll/Ping; cannot hide in endlessly retrying callback. | WIRED |
| 08.K1 | Canonical parsed fingerprint/four versions→transition semantic comparison. | WIRED |
| 08.K2 | Consumer restore→drained/config authorization→locked saved PG manifest and aligned transition checkpoint. | WIRED |
| 08.K3 | Main rules path→bounded LoadRules before consumer creation; startup-only. | WIRED |
| 09.K1 | Reducer→shared Bounds reserve before state mutation; safe acknowledged retirement/committed prefix pruning. | WIRED |
| 09.K2 | Worker valid-late classification→IncLate on supplied metrics instance. | WIRED |
| 09.K3 | Ops optional metric instance→internal fixed GET /metrics; health/readiness preserved. | WIRED |
| 10.K1 | persistRecord→ValidateIdentity and shared ImmutableFingerprint before sinks. | WIRED |
| 10.K2 | PG global-ID lock/winner→writer retained-CH preflight and committed authority→native stores. | WIRED |
| 10.K3 | ErrConflict after transaction return→real bounded DLQ; source waits for acknowledgement. | WIRED |
| 11.K1 | Detector classifier→existing DLQ.Publish→event-free AcknowledgePoison→checkpoint eligibility. | WIRED |
| 11.K2 | Writer partition effects→last contiguous completed record→CommitRecords. | WIRED |
| 11.K3 | Detector main NewDLQPublisher→bounded lifecycle closer→WithQuarantine. | WIRED |
| 12.K1 | Worker→exact reachBoundary hooks around real record/publish/ack/commit effects. | WIRED |
| 12.K2 | Kafka assigned/revoked/lost callbacks→optional actual assignment observer→private moved-target test assertions. | WIRED |
| 12.K3 | Recovery test→broker OffsetFetch plus immediate CountUnique/detail and complete workflow assertions. | WIRED |
| 13.K1 | Make seed-scenarios→dev-up prerequisite→operator Python script. | WIRED |
| 13.K2 | Compose actual `../../config/rules.yaml:/etc/sentinel/rules.yaml:ro`→matching configured absolute path. SDK escaped-pattern false negative. | WIRED |
| 13.K3 | Operator captures/private writes acknowledged manifest→check-alerts capture/verify subprocess. | WIRED |
| 13.K4 | check-alerts→actual current Kafka offsets/range, parameterized CH and PG rows→complete expected-set reconciliation. | WIRED |
| 14.K1 | Required hosted Phase 3 job→exact-family shell; integration retains all seven Phase 2 groups/full suite. | WIRED |
| 14.K2 | Each exact package/name→nonempty runner; set -e/pipefail retains errors even with misleading PASS output. | WIRED |
| 14.K3 | Operations→rules restart/drain/retention procedure and architecture seq caveat. | WIRED |
| 15.K1 | Extracted LoadRules→AUTH tests and real startup path; no duplicate declaration remains. | WIRED |
| 16.K1 | Worker constructor aliases connect dedicated producer/consumer, preserving shared neutral checkpoint types. | WIRED |
| 17.K1 | Writer orchestration injects extracted CH/PG stores and preserves CH→workflow→source order. | WIRED |
| 18.K1 | Detector PG/lifecycle constructor→Compose real credentials/migration prerequisite and readiness. | WIRED |
| 19.K1 | Locked immutable decision commit→worker stored-byte publish→fully effected frontier/checkpoint; old lookup precedes cursor. | WIRED |
| 20.K1 | Native cooldown tests→actual optional store pre/post-commit callbacks and integration-only worker barriers. | WIRED |
| 21.K1 | Private snapshot/processed-next/helper→schedule/operator flow→preserved-state repeat test. | WIRED |

### Data-flow trace

There is no Phase 3 dynamic UI to inspect for hollow render props. Equivalent backend data flow was traced through real external sources and returned effects:

| Consumer | Data variable/source | Real producer and downstream use | Status |
| --- | --- | --- | --- |
| Detector state | Kafka record→strict decoded Event | Actual owned PollRecords; tenant/source key and server timestamp validation before ApplySource | FLOWING |
| Canonical candidate | First accepted threshold crossing | Exact unique IDs/ports plus rule YAML→frozen evidence/source/trigger→PG transaction | FLOWING |
| Allowed alert | PG immutable canonical_payload/allowed | Existing decision fetched before cursor or transactionally created; stored bytes are returned and published only on allow | FLOWING |
| Immutable evidence/workflow | Real alert-topic record→permanent PG reservation | Reloaded validated saved bytes→native PrepareBatch/Send→PG insert/commit→Kafka source commit | FLOWING |
| Operator proof | Independent static/builder expectations and captured current offsets | Actual Kafka values/CH native rows/PG workflow fields, current received_at and complete prior snapshots→strict proof booleans | FLOWING |

No empty/static fallback supplies runtime detections or success. Empty maps/frontier.none/null clocks represent explicit initial/negative states; successful paths populate them through the traced consumers.

## Behavioral checks and execution evidence

No fresh short runtime spot-check was run by this read-only verifier; the serialized executor owned all runnable gates. Directly inspected commands/results and hosted execution supply behavioral evidence, not mere test presence.

| Behavior/gate | Command or executed boundary | Actual result |
| --- | --- | --- |
| Whole local exact-family matrix | `make test-phase3` | 46 independent top-level named PASS results; exit 0, 2545.66s. Predates fixture-only isolation; not presented as a second final local matrix. |
| Affected scenario after fixture delta | Exact `^TestDetectionScenarios$`, integration, 16m, serial | Exit 0, 282.50s elapsed/package 279.476s; static boundary/lateness/all and preserved live edges. |
| Full repaired integration | `go test -tags=integration -timeout 32m -p 1 ./test/...` | Persistent raw log: package 1845.917s, elapsed 1849.21, EXIT=0/RETURNED_EXIT=0; 17:16:37Z–17:47:26Z on Oct 20. |
| Retained Phase 2 compatibility | Seven exact original 4m/6m/4m/5m/6m/4m/5m selectors | Each raw group log returns 0; combined elapsed 321.83s, returned 0, 23:19:13Z–23:24:35Z. |
| Legacy/operator flow | `make seed`, actual two `timeout 16m make seed-scenarios` within preserved-repeat | Legacy 1000 sent/accepted/visible, zero retries; repeat proves prior full rows/seq, fresh IDs, drained state and zero normal new late drops. Hosted independently executes this flow. |
| Final unit/static | Persistent final-static bundle at 855cb60 | Returned 0/20.59s: Go race, web 6, Python 121, lint, build, default/integration vet/lint, default hook exclusion for three workers, web format/build and whitespace. |
| Script/CI/scanner repairs | All-script regression and pinned full-history scanner | Raw script log: 44 tests/77.382s, OK. gitleaks 8.30.1 raw final scan: 421 commits, no findings; historical prose exception is exact and independently reviewed. |
| Fresh hosted final candidate | [CI 39515550386](https://github.com/6sLOGAN78/Sentinel/actions/runs/39515550386) | completed/success at exact 80d2d87; all nine jobs success. Full integration, seven groups, 46 families and preserved Compose acceptance all required and executed. |

### Hosted CI provenance

Root-captured raw metadata is retained privately at `.local/phase3-acceptance/hosted-ci-39515550386-result.json`; independently read watch output is `hosted-ci-39515550386-watch.log` with `WATCH_RETURNED_EXIT=0`. Run creation is `2026-10-20T23:31:15Z`, completion `2026-10-21T00:20:46Z`.

| Job | Conclusion | Completed UTC |
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

The Phase 3 job took 44m34s within its unchanged 45m ceiling. Earlier failed run `39457945227` at `19db519` remains failed. Its interpreter/scanner/fixture repairs are supported by source diffs, regression evidence and clean scoped review, not a retrospective reassignment of its conclusion. The final hosted run supplies a complete exact-family execution on the repaired candidate.

### Probe execution

No numbered plan or summary declares a `probe-*.sh`; no conventional `scripts/*/tests/probe-*.sh` was found. **Not applicable.** The actual named integration/selector scripts were executed by their owning executor and hosted jobs; they are not substituted for an unexecuted declared probe.

## Requirements Coverage

All 14 plan IDs match REQUIREMENTS.md's Phase 3 mapping. No orphaned ID exists. Global checkboxes are left to root after this verdict.

| Requirement | Source plans | Implementation and behavioral evidence | Status |
| --- | --- | --- | --- |
| DET-01 | 01,14,15,16 | Exact AUTH type/group, ten unique failures/300s, canonical evidence; real AUTH and 9/10 recovery | SATISFIED |
| DET-02 | 03,14 | Exact SCAN ports/group/60s; repeated ports, 19/20/21, isolation and real scan | SATISFIED |
| DET-03 | 04,14 | Exact net.request/300/60s source count; unit and real Volume | SATISFIED |
| DET-04 | 04,14 | Exact policy.decision/action=deny/30/120s; outcome negatives and native Volume | SATISFIED |
| DET-05 | 01,08,13,14,15,18 | Strict versioned startup YAML, stopped/drained semantic handover and mounted rules; native config restart | SATISFIED |
| DET-06 | 01,05,07,08,09,11,12,14 | ADR, half-open windows, pre-pin maximum/sealed floor, 30s equality/drop, fixed late metric and shared bounded state | SATISFIED |
| DET-07 | 01,03,04,05,07,12,14,15,19 | Sole canonical DedupeKey, frozen bounded ordered evidence and all immutable fields; independent fixtures/replay/native payloads | SATISFIED |
| DET-08 | 05,14,18,19,20 | Actual PG durable cross-window allow/suppress, monotone clock/equality, later distinct windows and old decision replay | SATISFIED |
| DET-09 | 03,04,06,13,14,21 | Seeded four-rule/benign/boundary/late fixtures, legal current-run scheduler and exact native/hosted reconciliation | SATISFIED |
| ALRT-01 | 01,02,10,11,13,14,16,17,19 | Real keyed all-ISR alerts topic→permanent canonical authority→native immutable CH; both mains wired | SATISFIED |
| ALRT-02 | 02,10,11,13,14,17 | Unique alert/seq/list PG schema, insert-only workflow, resolved concurrent replay and conflicting identity quarantine | SATISFIED |
| ALRT-03 | 01,07,08,09,11,12,14,16,18,19,20,21 | Coupled resume pin/strict format2, actual restarts/third-owner/cooperative acquisition, PG frontier and history failure | SATISFIED |
| ALRT-04 | 11,12,14,20 | Causal three source publish/checkpoint and four PG-decision cuts, native immediate uniqueness and whole resolved row preservation | SATISFIED |
| GEN-02 | 06,13,14,21 | Eight scenario flags, deterministic expected hit manifests, private authenticated bounded operator scheduling/verification | SATISFIED |

## Anti-patterns, source/security dispositions and limits

The initial changed-source scan covered 129 non-planning artifacts; the final reviewed CI exception adds `.gitleaksignore`, giving 130. No unreferenced TBD/FIXME/XXX debt marker, unfinished handler, placeholder output, orphan runtime adapter or disconnected source was established. The `.env.example` “Placeholders only” comment describes safe template values. Intentional no-op production fault hooks are required isolation, not absent functionality. Empty outcomes for nonmatches/suppression and initial clocks/maps are populated or deliberately empty according to the traced protocol.

| Item | Classification | Assessment |
| --- | --- | --- |
| `Makefile:90` bench target prints Phase 5 availability and exits nonzero | INFO — explicitly later scope | Phase 5 owns benchmarks; this target cannot masquerade as measured success and is not a Phase 3 gap. |
| Source review's original cooperative fetch/resource admission/uncertain CH winner findings | Closed findings | Pending-only fetch completeness, actual alert-writer config validation and permanent immutable reservations are present and wired. R1's residual integrity issue closes in complete R2; original reports remain historical. |
| Generator negative-test false pass | Closed finding | Both refusal families now fail immediately if credential loading is reached and assert validation-specific diagnostics; mutation counterexamples and independent R1 recorded. |
| Strict static scenario shared-PG contamination | Closed finding | Private migrated UUID PG fixture preserves unrelated full workflow; runtime, goldens and strict verifier remain unchanged. Causal regression precedes repair and subsequent aggregate/hosted pass. |
| CI interpreter/historical scanner prose | Closed findings | Existing locked generator interpreter selected; one exact commit/path/rule/line prose fingerprint, full default rules/history retained. Independent repair review and actual hosted python/secrets success. |

`03-REVIEW.md` aggregates clean scoped source reviews over the 130-artifact union; it does not pretend uniform deep review. `03-SECURITY.md` records 78/78 registered dispositions closed: 57 present mitigations and 21 original accepted dependency risks, zero open/unregistered flags. Its `summary_reconciliation_pending: false` records completed reconciliation with 03-14; no new risk acceptance or security certification is inferred. The approved reservation addition is mapped to existing integrity/failure boundaries. No override is used to close any requirement.

Disconfirmation/evidence limits retained:

- Native CH ACK-loss proves a real send/socket failure after independent physical visibility. Deferred pre-visibility completion is a controlled model, not an executed stock-server pause. Permanent acknowledged authority excludes a differing B payload independently of visibility; no local cancellation is called remote abort. This limit does not leave a missing implementation of immutable authority.
- Reservation Task 3 lacks a separate tests-only RED commit. Actual observed RED and current native/model/process evidence exist; history was not reconstructed. This limits provenance, not current implemented behavior.
- The earlier unexplained third-owner timeout, discarded-diagnostic aggregate failures and interrupted session46545 remain historical failures/UNKNOWN. The recovered full log and final hosted success are separate observations, not invented causes or recovered exits.
- The static fixture cleanup comment overstates an unchanged process helper's join proof. Cleanup requests Kill; it does not independently observe every join. R2 investigated the bounded PG cleanup behavior without establishing a concrete current failure. No claim of completed joins is made for that helper.
- Accounted state/resource ceilings are not measured RSS, latency or throughput guarantees. Physical identical duplicates remain allowed under at-least-once effects. Source retention, intact PG durable authority, supported fixed partition placement and all conforming reservation writers remain documented recovery assumptions. Confirmed old-server termination/complete retained-outcome reconciliation and additive migration preceded the local new-protocol rollout.
- Phase 4 auth/query/triage/UI/SSE and lossless handling of lower seq committing later are explicitly future work. Current unique seq allocation is not represented as commit order. No Phase 4 behavior was required to pass this phase.

## Human Verification Required

No new subjective human test is identified by this follow-up. The previous baseline's real integration and operator evidence remains historical. The latest repeat introduces a failed automated acceptance gate, owned by the assigned diagnosis/repair specialist and root; it cannot be replaced by human approval or the earlier green run. No deferred `<human-check>` block was found in the original numbered-plan review.

## Gaps Summary

**One acceptance gap is open:** the latest completed required repeat full integration failed. The assigned specialist's test-only causal readiness repair, independent deep review and focused/native/static/script/scanner checks are now complete. Root's replacement full aggregate has not returned at this scoped assessment; actual replacement full/hosted success and explicit release are still required. The failure is the retained-owner fixture precondition described in the parent follow-up banner, not an established runtime implementation failure. No later-phase deferral or override removes this gate.

The original five roadmap criteria, 54 plan truths, 72 artifact declarations, 46 links and 14 requirement mappings remain the preserved baseline verification. The original 80d2d87 run remains successful; the later eee632f repeat remains failed. Current status is **gaps_found / re-verification pending**, not approval to proceed to Phase 4.

Root owns requirement checkboxes, `phase.complete`, final tracking and any report commit. This verifier neither commits nor begins Phase 4.

---

_Verified: 2026-10-21T04:24:57Z_
_Verifier: independent goal-backward verification agent (gsd-verifier)_
