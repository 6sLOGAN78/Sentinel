# Phase 3 — Multi-Source Coverage Audit

Planning coverage only, 2026-10-17. All rows marked COVERED mean executable tasks are assigned; none claim implementation or tests already passed. Sources: ROADMAP Phase3, all14 assigned REQUIREMENTS IDs, complete 03-RESEARCH.md including amended A6/A7/A9 and resolved Open Questions, explicit USER option1 in03-COOLDOWN-DECISION.md, and inherited STATE/Phase1 context/ADR constraints. No Phase3 CONTEXT.md exists; no D-XX locks are invented.

| SOURCE | ID | Feature/requirement | Plan/task | Status | Notes |
|--------|----|---------------------|-----------|--------|-------|
| GOAL | G-00 | Approved operator story: deterministic evidence-backed detections survive crashes/restarts/replay | 03-01..21 | COVERED | All live slices and failure proofs. |
| GOAL | G-01 | Four exact scenarios, benign zero, boundary/lateness equality and tolerance+1ms | 03-03,04,05,06,13 | COVERED | Roadmap criterion1. |
| GOAL | G-02 | Complete immutable evidence and actual cross-window cooldown; eligible windows retain separate IDs | 03-05,03-18,03-19,03-20 | COVERED | Roadmap criterion2; USER option1, visible suppression and immutable replay. |
| GOAL | G-03 | 9/10 restart and actual third-replica ownership recovery | 03-07,12 | COVERED | Roadmap criterion3. |
| GOAL | G-04 | Three exact crash boundaries and resolved replay preservation | 03-02,10,12 | COVERED | Roadmap criterion4. |
| GOAL | G-05 | Kafka alerts→CH/PG unique/list/seq plus startup rules | 03-01,02,08,13 | COVERED | Roadmap criterion5. |
| REQ | DET-01 | AUTH 10 unique failures/300s tenant/source/principal | 03-01, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | DET-02 | SCAN 20 exact distinct ports/60s tenant/source/destination | 03-03, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | DET-03 | FLOOD 300 unique net.request/60s tenant/source | 03-04, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | DET-04 | DENY 30 policy.decision action=deny/120s tenant/source | 03-04, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | DET-05 | Strict versioned startup YAML and restart/drained handover | 03-01, 03-08, 03-13, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | DET-06 | ADR half-open event-time/watermark/lateness metric/bounded state | 03-01, 03-05, 03-07, 03-08, 03-09, 03-11, 03-12, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | DET-07 | Frozen severity/summary/count/windows/<=20 IDs/rule/version/SHA identity | 03-01, 03-03, 03-04, 03-05, 03-07, 03-12, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | DET-08 | Actual per-rule/tenant/entity cross-window alert suppression with separately eligible window IDs | 03-18,03-19,03-20,03-14 | COVERED | Nearby299s/301s visible suppression; equality/new window, immutable old allow/suppress, durable PG recovery. |
| REQ | DET-09 | Exact malicious/benign/edge/late fixture outcomes | 03-03, 03-04, 03-06, 03-13, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | ALRT-01 | Bounded acknowledged alerts publication and immutable CH persistence | 03-01, 03-02, 03-10, 03-11, 03-13, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | ALRT-02 | PG unique alert_id/seq/list workflow; replay preserves resolved state | 03-02, 03-10, 03-11, 03-13, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | ALRT-03 | Metadata-aware state-pin restart/actual rebalance reconstruction | 03-01, 03-07, 03-08, 03-09, 03-11, 03-12, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | ALRT-04 | Three exact crash boundaries and immediately unique visible effects | 03-11, 03-12, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| REQ | GEN-02 | Scenario flags with exact reproducible expected rule hits | 03-06, 03-13, 03-14 | COVERED | Nonempty requirement frontmatter plus task acceptance. |
| RESEARCH | R-01 | ADRs before ANY rule code; inherited storage/window/identity decisions | 03-01-T01 | COVERED | Both accepted engineering ADRs precede live implementation. |
| RESEARCH | R-02 | Pure reducer, dedicated metadata consumer/all-ISR publisher and lifecycle | 03-01,03-15,03-16 | COVERED | First real AUTH slice followed immediately by focused loader/client proofs. |
| RESEARCH | R-03 | Exact predicates/null required groups no match; tenant/source canonical Kafka key | 03-01,03-03,03-04,03-11 | COVERED | Four features plus poison key validation. |
| RESEARCH | R-04 | Epoch tumbling UTC ms [start,end), T==watermark accepts; older than30s drops | 03-05,03-07,03-13 | COVERED | Boundary and tolerance+1ms proof; no sliding semantics. |
| RESEARCH | R-05 | Valid nonmatching events update max; poison/idle do not | 03-05,03-07,03-09,03-11 | COVERED | Exact reducer and worker tests. |
| RESEARCH | R-06 | Frozen first threshold, unique matching event_count even SCAN, first20 ordered IDs, event-time created_at, omit detected_at | 03-01,03-03,03-05,03-07 | COVERED | Immutable payload bytes compared across replay. |
| RESEARCH | R-07 | DedupeKey rule_version excluded, goldens unchanged; component identity parsing | 03-01,03-05,03-06,03-10 | COVERED | AUTH split first pipe preserving entire principal; SCAN two IPs; volumes one. |
| RESEARCH | R-08 | Live pin includes fired/unfired first offsets, empty-state processed-next | 03-01,03-07 | COVERED | Dedicated metadata commit, never CommitRecords for detector. |
| RESEARCH | R-09 | Prefix max immediately BEFORE pin and bounded metadata-only offset ledger | 03-01,03-07,03-09 | COVERED | Prune only after acknowledged checkpoint pin. |
| RESEARCH | R-10 | Sealed floor only after ALL prefix alert/poison acknowledgements; replay suppression separate from lateness seed | 03-01,03-07,03-11 | COVERED | Advancing-pin/expired suffix adversary tests. |
| RESEARCH | R-11 | Format2 metadata/fingerprint/four versions/drained/processed_next/effective_from/decision_frontier<=1024bytes | 03-01,03-07,03-08,03-20 | COVERED | Pin remains earliest live including fired; decision frontier distinguishes PGbehind from legal PGahead. |
| RESEARCH | R-12 | Pinned kgo PreCommitFnContext; void CommitOffsetsSync callback transport/partition/completeness checks | 03-01,03-07 | COVERED | No invented return-error API. |
| RESEARCH | R-13 | OnOffsetsFetched terminal error also propagates lc.Fail rather than ready infinite retry | 03-01,03-07,03-08 | COVERED | Terminal worker failure path verified. |
| RESEARCH | R-14 | AtStart new group boundary; NoResetOffset existing pins; retention/truncation/missing metadata fail closed | 03-01,03-07,03-09 | COVERED | Only test-owned retention mutation; no silent seek/reset. |
| RESEARCH | R-15 | Blocked owned poll; revoke completed effects then clear revoked; loss no new effects/commit | 03-01,03-07,03-12 | COVERED | Actual target handoff across third replica. |
| RESEARCH | R-16 | Actual cross-window monotone trigger-event-time cooldown and immutable allow/suppress | 03-05,03-18,03-19,03-20 | COVERED | Old candidate-only policy SUPERSEDED; first candidate frozen, expiry equality allows fresh window, suppressed window never retries. |
| RESEARCH | R-17 | Strict capped one-document YAML, KnownFields/EOF, aliases/merges/duplicates/IDs/enums/bounds/overflow | 03-01,03-08 | COVERED | Existing yaml/v3 pin; strict parser tests. |
| RESEARCH | R-18 | Fingerprint/drained version handover, PGconfig generation/cursor reset, aligned effective_from | 03-08 | COVERED | Same-version drift/active transition rejected;300→600s cannot reuse historicalID; nohotreload. |
| RESEARCH | R-19 | Rule severity/cooldown engineering defaults and restart-only definitions | 03-01,03-03,03-04,03-08 | COVERED | AUTH high/60s, SCAN medium/30s, FLOOD high/30s, DENY medium/60s, version1. |
| RESEARCH | R-20 | 64KiB rules;1s..1h window;4096 IDs;10k windows;100k ledger;64MiB state; checked aggregate admission | 03-01,03-08,03-09 | COVERED | 256MiB accounted fetch+producer+state cap; no eviction or probabilistic port set. |
| RESEARCH | R-21 | Idle pin/retention tradeoff and same-key valid progress/drain markers | 03-01,03-06,03-08,03-13,03-14 | COVERED | No wall-clock expiry or deletion-recovery guarantee. |
| RESEARCH | R-22 | Minimal fixed numeric late-drop internal ops counter; repaired readiness16/admission/independent deadline | 03-09,03-14 | COVERED | No high-cardinality labels; full metrics stay Phase5. |
| RESEARCH | R-23 | Fresh native CH batches/Abort; CH then PG then contiguous source commit | 03-02,03-10,03-11 | COVERED | Real partial/ambiguous effects tests. |
| RESEARCH | R-24 | CH immediate duplicate-safe tenant reads/counts across partitions; no OPTIMIZE or invented TTL | 03-02,03-10,03-13 | COVERED | Physical identical duplicates permitted. |
| RESEARCH | R-25 | PG unique alert_id/seq, list fields/tenant indexes; insert-only workflow replay | 03-02,03-10 | COVERED | Resolved status/assignee/version/updated_at/seq preservation. |
| RESEARCH | R-26 | Per-alert PG transaction advisory lock; winner preflight and CH orphan consistency before CH insert | 03-02,03-10 | COVERED | Wrong tenant/immutable content quarantined under concurrency. |
| RESEARCH | R-27 | Immutable fingerprint excludes workflow/status/optional detected_at | 03-02,03-10 | COVERED | Independent canonical field-change tests. |
| RESEARCH | R-28 | seq allocation is not transaction commit order; Phase4 backfill handoff | 03-01,03-08,03-14 | COVERED | No speculative SSE implementation or gap-free seq promise. |
| RESEARCH | R-29 | Existing bounded redacted DLQ for both workers, trusted origin, one aggregate retry/deadline/commit reservation | 03-10,03-11 | COVERED | Transient sink/history/capacity failures remain non-poison; other partitions can safely progress. |
| RESEARCH | R-30 | Synthetic scenarios reuse body/key/retry/private manifest; exact shared Go/Python expected bytes | 03-06,03-13 | COVERED | Documentation IPs/principals only; no offensive traffic. |
| RESEARCH | R-31 | Arbitrary restart cuts compare immutable bytes, advancing pins/out-of-order/overlapping/expired windows | 03-07 | COVERED | Uninterrupted vs restart live-state and output union proof. |
| RESEARCH | R-32 | 9/10 SIGKILL, then tenth; two replicas + third actually moves target | 03-12 | COVERED | Causal incorporation/assignment/checkpoint evidence required. |
| RESEARCH | R-33 | Exact before-publish/after-ack-before-commit/after-successful-commit barriers and broker/Kafka/CH/PG checks | 03-12 | COVERED | Default hooks unconditional no-op; integration private finite files. |
| RESEARCH | R-34 | CH success PG failure, ambiguous insert, conflict/tenant/concurrency, poison outage and aggregate/prefix proofs | 03-10,03-11 | COVERED | Real stores/fault resources; no mock-only acceptance. |
| RESEARCH | R-35 | Per-family nonempty CI; TestMain default builds, dedicated buildIntegrationBinary; retained Phase2 suite | 03-12,03-14 | COVERED | Finite setup/gate bounds, no empty regex pass. |
| RESEARCH | R-36 | Compose distinct topics/groups, rules read-only mount and healthy startup; exact current-run scenario seed | 03-13,03-14 | COVERED | Preserved volume/demo secrets; Kafka current-offset and tenant/window manifest proof. |
| RESEARCH | R-37 | Installed pinned dependencies only; YAML indirect→direct same pin | 03-15,all SC | COVERED | No package install; registry legitimacy gate applies if authorized scope changes. |
| RESEARCH | R-38 | ASVS5 applicable validation/authorization/config/protection/logging + STRIDE | all21 plans | COVERED | Every plan has actionable threat_model and task verification; high issues block completion. |
| CONTEXT | absent | Phase3 CONTEXT.md/D-XX decision set | N/A | COVERED | Absence explicitly recorded; engineering defaults are research selections, not new user locks. |
| CONTEXT | inherited D-15 | Existing length-prefixed component SHA256 DedupeKey, version excluded, pipe safety | 03-01,05,06,10 | COVERED | Schema and golden vectors unchanged. |
| CONTEXT | inherited D-16 | Event-time created_at and at most20 sample IDs | 03-01,05,07 | COVERED | Window-end timestamp; detected_at omitted from canonical payload. |
| CONTEXT | inherited D-24 / ADR0006 | Immutable CH, mutable PG workflow/list with unique alert_id | 03-01-T01,03-02,03-10 | COVERED | Concrete list-source ADR before rule code. |

| USER | option1 | Actual alert-level cooldown across nearby windows; preserve eligible window IDs/resolved workflows | 03-18,03-19,03-20,03-14 | COVERED | 03-COOLDOWN-DECISION.md is actual session decision record; no invented Phase3 D-XX. |
| RESEARCH | A6-ledger | Operational PGfences/cursors/immutable decisions with frozen bytes/fingerprint/source/config before Publish | 03-18,03-19 | COVERED | Real additive004 migration/store/readiness/lifecycle/config/Compose/fixtures precede activation; operational state is not CHalert authority. |
| RESEARCH | A6-replay | Lookup matching old allow/suppress before cursor access; divergent bytes fail closed | 03-19-T01/T02,03-20 | COVERED | No replay cursor mutation; monotone triggerclock/equality; suppressed-window no retry and tenant/entity/rule separation. |
| RESEARCH | A6-owner | Kafka member/generation/token/group incarnation + source offset/rule ordinal frontier fences | 03-19,03-20 | COVERED | Reject stale/delayed/equal-generation wrong member; actual third-owner transfer and concurrent transaction proofs. |
| RESEARCH | A6-failure | Bounded lock/statement/transaction/explicit rollback; no PGtransaction across Kafka Publish | 03-18,03-19,03-20 | COVERED | PGoutage terminal/not-ready/noDLQ/noadvance; uncertain commit lookup/replay; all four decision/Kafka/checkpoint crashcuts. |
| RESEARCH | A6-retention | No automatic operational decision GC; disk growth and inconsistent backup restore fail closed | 03-19,03-20,03-14 | COVERED | No assumed lookback/infinite source pin/ephemeral cooldown seed. |
| RESEARCH | A7-handover | Persist four aligned effective_from gates/cursor generation; pending allowed decisions drain | 03-08 | COVERED | Explicit300→600collision, shrink/threshold/equality/old-decision tests. |
| RESEARCH | A9-snapshot | Private end+OffsetFetch processed_next caughtup snapshot; derive processedmax=sealed+lateness | 03-07,03-21 | COVERED | Pin canlag; prefixmax is wrong anchor; explicit empty, idle/noactive windows, missing metadata requires capped retained scan. |
| RESEARCH | A9-schedule | Livebase>=max(now,all caughtupmax+1ms), same-ms contributors, threats before per-key windowend+30s markers | 03-06,03-21,03-13 | COVERED | Configured5minfuture+2s margin; clock-injected waits/preflight/refusal; quiescent producers, private manifest, no public debugHTTP. |
| RESEARCH | A9-budget | Finite15m scenario orchestration with unchanged5attempt/120s delivery per actual batch | 03-21,03-13,03-14 | COVERED | Single16m verification/repeat32m, CIjob45m; existing6m grouping is not a scheduled-run budget. |
| RESEARCH | A9-preserved | Real legacyseed→scenario→scenario preserves group/volumes/priorrows and exact new outputs | 03-21-T01,03-13-T03,03-14 | COVERED | Fakeclock excessive prior future-progress/concurrent-producer refusal; no groupreset. |
| GOAL | recovery-restore | Checkpoint last durable decision marker detects PG restored behind newer Kafka | 03-01,03-07,03-19,03-20 | COVERED | decision_frontier explicitnone or offset/ordinal/ID/fingerprint; exact immutable row + PGfence>=marker; PGahead legal. Additional root research clarification pending; concrete plan/test already assigned. |

## Revision mapping (original14 drafts → current21 plans)

Original03-PLAN-CHECK.md remains historical and untouched. No original draft has executed. Plan IDs01..14 are retained; no wholesale renumbering occurred. Other original tasks retain their IDs unless listed below.

| Original task(s) | Revised owner | Change |
|------------------|---------------|--------|
|03-01-T01/T02|03-01-T01/T02|Accepted selected cooldown ADR and focused real one-partition AUTH acceptance; bounded context accounting.|
|03-01-T03 loader portion|03-15-T01/T02|Exact loader source reducer.go is owned/read; tests first, YAML pin promotion moved here.|
|03-01-T03 Kafka portion|03-16-T01/T02|Separate live adapter extraction and early publisher/metadata proof.|
|03-02-T03|03-17-T01/T02|Immediate native store/order proofs and extraction; two-store happy path retained02.|
|03-05 candidate-only throttle|03-05 immutable Candidate +03-18/19/20 durable cooldown|Candidate-only suppression claims SUPERSEDED; actual alert suppression/recovery added.|
|03-08 module promotion|03-15-T02|Existing pinned YAML dependency only;08 owns PGgeneration/effective_from transition.|
|03-12 crash instrumentation|03-12 retained +03-20 extensions|Original three barriers/9of10/actualthirdowner retained; add four PGdecision/Kafka cuts and inconsistent restore.|
|03-06 live past anchoring|03-06 schedule contract +03-21 snapshot/scheduler|Past anchoring SUPERSEDED; fixed historical purefixtures remain isolated.|
|03-13-T03 verifier tests|03-13-T01|Loadable verifier/test scaffold before production; T03 owns implementation and real repeat execution.|
|03-14 fixture work|03-18 early PG compatibility +03-14 final guards|Real PG dependencies before cooldown activation; final CI/readiness regressions retained.|

## Dependency and ownership audit

| Plan | Wave | Needs | Creates / live capability | Checkpoint |
|------|------|-------|---------------------------|------------|
|03-01|1|ExistingPhase2|Let an operator consume a real AUTH_BRUTE_FORCE alert from Kafka after ten authenticated failure events.|none|
|03-02|2|03-01|Persist the live AUTH alert into immutable ClickHouse evidence and one PostgreSQL workflow row.|none|
|03-03|3|03-15, 03-16|Detect a port scan as twenty exact distinct destination ports in one event-time minute.|none|
|03-04|4|03-03|Add request-flood and deny-spike alerts to the live detector.|none|
|03-05|5|03-04|Freeze evidence and first-threshold candidate source/clock for durable cross-window decisions.|none|
|03-06|1|ExistingPhase2|Generate exact seeded malicious, benign, edge and late scenarios with a current-run expected-alert manifest.|none|
|03-07|7|03-17, 03-19, 03-06|Prove checkpoint reconstruction across arbitrary restart cuts and advancing state pins.|none|
|03-08|8|03-07|Allow rule changes only through a strict startup config and drained version handover.|none|
|03-09|9|03-08|Bound detector state and expose deterministic late drops without weakening readiness.|none|
|03-10|4|03-17|Repair partial alert writes and quarantine conflicting immutable identities under concurrency.|none|
|03-11|10|03-09, 03-10|Quarantine detector and writer poison within one owned-batch budget without skipping durable prefixes.|none|
|03-12|11|03-11|Prove mid-window restart, actual third-replica ownership transfer and all three crash boundaries.|none|
|03-13|13|03-12, 03-20, 03-21|Run a healthy preserved-volume Compose detector/writer stack and verify exact current-run scenario alerts.|none|
|03-14|14|03-13|Guard every Phase 3 acceptance family in CI and document verified operator/replay behavior.|none|
|03-15|2|03-01|Extract and test the strict AUTH rules loader without changing the real Kafka slice.|none|
|03-16|2|03-01|Extract the live Kafka detector adapters and test acknowledgements/metadata before adding other rules.|none|
|03-17|3|03-02|Extract and prove native alert stores immediately after the first two-store path.|none|
|03-18|3|03-16, 03-02|Make real PostgreSQL capability part of detector startup, shutdown and local worker readiness before cooldown activation.|none|
|03-19|6|03-05, 03-18, 03-17|Deliver actual cross-window alert cooldown through durable PG decisions on the existing events→alerts path.|none|
|03-20|12|03-12|Prove cooldown decisions across PG failure, stale ownership and all four PG/Kafka checkpoint crash cuts.|none|
|03-21|9|03-08, 03-06|Schedule live scenarios after a private caught-up partition snapshot within legal ingest skew.|none|

All21 plans have2–3 tasks (52 total). Every task declares exact modified targets and read_first includes current targets or predecessor-created targets; newfiles identify real source analogs and loadable scaffolds. Every task owns<=5files; same-wave file sets are disjoint; shared-file work has transitive dependencies. Scope estimates itemize reused versus new work perplan; largest decision refinement19 targets40–47% context, not a blanket estimate. If execution discovers contextcost>50%, split without omitting source behavior.

## Test creation and dependency bindings

- 03-01: first test task creates all new behavioral scaffolds before production; Phase2 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-02: first test task creates all new behavioral scaffolds before production; 03-01 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-03: first test task creates all new behavioral scaffolds before production; 03-15, 03-16 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-04: first test task creates all new behavioral scaffolds before production; 03-03 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-05: first test task creates all new behavioral scaffolds before production; 03-04 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-06: first test task creates all new behavioral scaffolds before production; Phase2 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-07: first test task creates all new behavioral scaffolds before production; 03-17, 03-19, 03-06 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-08: first test task creates all new behavioral scaffolds before production; 03-07 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-09: first test task creates all new behavioral scaffolds before production; 03-08 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-10: first test task creates all new behavioral scaffolds before production; 03-17 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-11: first test task creates all new behavioral scaffolds before production; 03-09, 03-10 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-12: first test task creates all new behavioral scaffolds before production; 03-11 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-13: first test task creates all new behavioral scaffolds before production; 03-12, 03-20, 03-21 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-14: first test task creates all new behavioral scaffolds before production; 03-13 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-15: first test task creates all new behavioral scaffolds before production; 03-01 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-16: first test task creates all new behavioral scaffolds before production; 03-01 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-17: first test task creates all new behavioral scaffolds before production; 03-02 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-18: first test task creates all new behavioral scaffolds before production; 03-16, 03-02 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-19: first test task creates all new behavioral scaffolds before production; 03-05, 03-18, 03-17 provides existing contracts. 2 automated task bindings in03-VALIDATION.
- 03-20: first test task creates all new behavioral scaffolds before production; 03-12 provides existing contracts. 3 automated task bindings in03-VALIDATION.
- 03-21: first test task creates all new behavioral scaffolds before production; 03-08, 03-06 provides existing contracts. 2 automated task bindings in03-VALIDATION.

## Exclusions (not gaps)

Phase4 JWT/query/triage/SSE/browser UI; Phase5 full metrics/tracing/benchmarks; Phase6Kubernetes; deferred ML/Aegis. No Phase3 CONTEXT.md exists and no D-XX are invented. Inherited D-15/D-16/D-24 remain binding. No new alertTTL/compliance period, offensive traffic, reset, OPTIMIZE or scope waiver.

Coverage result: GOALstory+fivecriteria, all14REQIDs, researchR01..38 plus A6/A7/A9/restore additions, explicitUSERoption1 and inherited constraints have assigned tasks. COVERED means planned, not proven. Candidate-only and past-anchor claims are SUPERSEDED. Independent semantic checker approval and all production tests remain pending.
