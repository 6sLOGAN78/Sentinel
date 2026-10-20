---
phase: 03
slug: detection-alert-pipeline
status: verified
register_authored_at_plan_time: true
threats_total: 78
threats_closed: 78
threats_open: 0
mitigations_verified: 57
risks_accepted: 21
threats_transferred: 0
unregistered_flags: 0
asvs_version: 5.0.0
asvs_level: 1
security_enforcement: true
block_on: open_high_or_unverified_declared_mitigation
implementation_head: 62099b20c2a98a1966a0d6f90e5b1d653fc1e855
created: 2026-10-20
verification_scope: registered_mitigation_presence
phase_acceptance: pending
summary_reconciliation_pending: 03-14
---

# Phase 03 — Security

All 78 unique plan-time threats have a verified disposition: 57 declared mitigations are present in the frozen implementation and 21 explicitly accepted dependency risks are recorded below. No transfer dispositions exist. No missing declared mitigation or unmapped attack-surface flag was established. This is a static, registered-threat audit, not completion of Phase 3, a fresh security-test execution, an ASVS certification, or a claim that final local/hosted gates passed.

## Scope and method

Read the `<threat_model>` blocks in all 21 numbered `03-01-PLAN.md` through `03-21-PLAN.md`, and the threat/security/stub portions of all 20 existing numbered summaries. `03-14-SUMMARY.md` did not exist at audit time. The supplemental `03-ALERT-RESERVATION-IMPLEMENTATION-PLAN.md` is not a 22nd numbered plan and contributes no invented plan-time threat IDs. Read the approved reservation design, original production review, R1, R2, reservation evidence, rollout check and `docs/threat-model.md`. Existing findings and summaries supplied context, not proof that implementation controls exist.

Every `mitigate` row started unverified. Used `git show`/`git grep` at the full implementation head above, then read the actual guard and its runtime caller, effect ordering, error path and relevant proof assertions. File:line evidence below refers to that frozen head. Test-file evidence establishes the presence and content of an assertion; this auditor ran no tests, compilation, live service/container commands, installs, staging or commits. Implementation files remained read-only. Only this report was created.

No applicable ancestor/root `AGENTS.md` or project `.codex/skills/` / `.agents/skills/` directory was present. The `gsd-secure-phase` auditor workflow/template supplied disposition checks and report structure. `.planning/config.json` has no `workflow.security_enforcement` key; the workflow default is true. ASVS 5.0.0 level 1 is scoped guidance for V1/V2/V8/V13/V14/V16. Open high findings or an unverified declared mitigation block advancement. The supplied policy does not turn pending full acceptance into a fabricated code-presence failure or an accepted risk.

## Trust boundaries

| Boundary | Required implemented control | Data crossing |
| --- | --- | --- |
| Kafka events → detector | Strict bounded decode, schema validation, canonical tenant/source key, exact rule predicates | Tenant events and event time |
| Event cardinality → reducer memory | Prospective shared admission, exact evidence/port sets, no live-state eviction | IDs, windows, prefix maxima |
| Kafka ownership/recovery → PG decisions | Actual generation/member/token/incarnation/config checks and authoritative restore | Assignment and checkpoint/frontier |
| PG decision → Kafka alerts/checkpoint | Immutable decision commit before publication; all-ISR output before successful source checkpoint | Frozen canonical alert bytes |
| Kafka alert → PG reservation → CH/workflow | Canonical identity, committed immutable global winner before CH; revalidation; post-CH insert-only workflow | Immutable payload and mutable analyst state |
| Poison → DLQ → source prefix | Trusted origin metadata, bounded/redacted framing, acknowledgement before prefix | Bounded source context |
| Local rules/env → startup | Strict capped one-document parsing, finite checked budgets and semantic/version handover | Operator configuration |
| Private files/processes → scenario acceptance | No-follow private artifacts, purpose-specific environment, bounded schedules, current-run exact sets | Keys and private expected/observed evidence |
| Integration fault controls → production binaries | Build-tag exclusion and unconditional production no-op | Test-only private finite barriers |
| CI selectors → evidence claims | Independent exact nonempty families, failure propagation and finite budgets | Test selection and process status |

## Threat register — mitigate

Each row is CLOSED on implementation evidence. Mitigation descriptions below summarize the declared plan obligation and the actual enforcement; the corresponding numbered plan retains the original text.

| Threat ID | Category | Component | Disposition | Verified mitigation and evidence | Status |
| --- | --- | --- | --- | --- | --- |
| T-03-01-01 | Spoofing/Tampering | event grouping and alert identity | mitigate | `detector/poison.go:19` calls DecodeEvent and Validate and checks exact tenant/source key before `worker.go:418` ApplySource; `detection/bounds.go:135` includes tenant/source in grouping; `detection/evidence.go:54` calls component-based DedupeKey. Paths in this table are under `backend/internal/` unless stated otherwise. | CLOSED |
| T-03-01-02 | Denial of service | poll/state/publisher | mitigate | `detector/worker.go:147,157` caps poll count and owned-batch time; `detection/reducer.go:116` and `detection/bounds.go:152` perform prospective admission; `detection/config.go:46` caps rules; `kafkax/detector_consumer.go:149` caps fetches; `kafkax/alerts.go:18,28,61` bounds canonical size, delivery, buffers and retries. Startup resource check: `config/config.go:676`. | CLOSED |
| T-03-01-03 | Tampering/Repudiation | metadata/effect order | mitigate | `detector/worker.go:428,436,440,444` orders PG decision, allowed publication, frontier, then acknowledgement; `detection/reducer.go:151,188,252` separates durable sealing/pin/prefix pruning; `kafkax/detector_consumer.go:551,562,577` attaches metadata, checks callback and adopts only successful state. | CLOSED |
| T-03-02-01 | Tampering | immutable detection and mutable workflow | mitigate | Real service wires native stores at `backend/cmd/alert-writer/main.go:89`; `alertwriter/writer.go:183` requires prepareReservation before persistReserved; `alertwriter/reservation.go:52,87,91,100,125,131` locks, commits/reloads authority, performs CH effect, skips equal workflow creation. `storage/postgres/alerts.go:91` is insert-only. Full resolved workflow assertion: `backend/test/detector_recovery_test.go:249`. | CLOSED |
| T-03-02-02 | Spoofing/Information disclosure | storage SQL and tenant reads | mitigate | `alertwriter/poison.go:63,67,70,74` validates schema/key/recomputed identity before persistence; `alertwriter/identity.go:44,48` recomputes component DedupeKey and key; `storage/clickhouse/alerts.go:91,119` binds tenant/ID; `storage/postgres/alerts.go:85,92` and `alert_reservations.go:13,23` bind values. Global PG-ID reads are intentional winner checks followed by tenant/fingerprint comparison, not tenant query APIs. Retry/error logs are fixed at `alertwriter/poison.go:34,81`. | CLOSED |
| T-03-02-03 | Repudiation/Denial of service | source effect prefix | mitigate | `storage/clickhouse/alerts.go:45,49,51` creates a fresh finite native batch each attempt; `storage/postgres/alerts.go:36,48,69` bounds transactions/locks; `alertwriter/writer.go:146,163,173` commits only contiguous completed per-partition effects; `reservation.go:125,132,136` requires CH then workflow commit. Partial failure returns before source eligibility. | CLOSED |
| T-03-03-01 | Tampering/Spoofing | scan grouping | mitigate | Runtime event validation at `detector/poison.go:27`; `detection/scan.go:14` requires net.connection, nonnull source/destination/port and parses both IPs; `reducer.go:141` selects destination component and `bounds.go:135` isolates tenant/source/destination/window. | CLOSED |
| T-03-03-02 | Denial of service | distinct-port state | mitigate | `detection/bounds.go:149,152,159,183` deduplicates IDs before exact bounded port admission; `reducer.go:116` reserves aggregate state before mutation; `reducer.go:162` retires only acknowledged sealed windows. | CLOSED |
| T-03-04-01 | Tampering | volume counts/predicates | mitigate | `detection/volume.go:16,25,27` requires valid source and exact net.request / policy.decision+deny; `bounds.go:135,149,155` isolates tenant/source and deduplicates before increment. | CLOSED |
| T-03-04-02 | Denial of service | counter evidence state | mitigate | Shared limits at `detection/reducer.go:15` and `bounds.go:59,152`; pin includes every retained window at `reducer.go:195`; release/retirement requires ownership loss or acknowledged sealed event time (`bounds.go:220`, `reducer.go:151`). No wall-clock eviction path in these methods. | CLOSED |
| T-03-05-01 | Tampering | immutable first-threshold payload | mitigate | `detection/bounds.go:172,194,209` freezes only once and supplies private crossing evidence; `evidence.go:15,57` serializes exact sampled order/count; `candidate.go:33` exposes a copy. Canonical byte/state assertions: `detection/replay_test.go:44,133,152`. | CLOSED |
| T-03-05-02 | Spoofing | entity display/identity encoding | mitigate | `detection/evidence.go:40,54` hashes component slices, not display delimiters; `alertwriter/identity.go:24,31,44` preserves the first-pipe AUTH suffix, validates SCAN/volume shape and recomputes ID. Shared independent Python encoding: `generator/src/sentinel_generator/scenarios.py:98`. | CLOSED |
| T-03-05-03 | Repudiation | cooldown claims | mitigate | `detection/bounds.go:172` emits one frozen candidate/window; runtime `detector/worker.go:428,435` publishes only PG-allowed decisions; `storage/postgres/detection_decisions.go:604,608,614` returns saved disposition before new cursor access, and `:698` applies monotone cooldown only for a new decision. | CLOSED |
| T-03-06-01 | Information disclosure | keys and manifests | mitigate | Both sender paths load keys through `_load_api_key` at `generator/src/sentinel_generator/cli.py:445,569`, using no-follow private inode-checked read `:95` or environment. Manifest output uses anchored private exclusive creation `:131,179,191`; terminal scenario output at `:515` contains aggregate counts. No credential argv option is introduced. | CLOSED |
| T-03-06-02 | Denial of service | scenario count and sender | mitigate | CLI count/batch ceilings `generator/src/sentinel_generator/cli.py:231,239,246`; sender limits and retry loop `send.py:17,168,185,190,221`; live timer `live_schedule.py:221`. Scenario builder validates known names/tenant/time at `scenarios.py:125`; safe reserved-address/marker assertions at `generator/tests/test_scenarios.py:84`. | CLOSED |
| T-03-06-03 | Tampering | expected hit manifest | mitigate | Independent fixture comparison asserts exact events/canonical payloads/IDs at `generator/tests/test_scenarios.py:62`; `cli.py:487` records actual tenant/seed/timebase/evidence/batches; `scenarios.py:98,125` derives deterministic identity and windows. No historical-only match is accepted by `backend/tools/check-alerts/main.go:341,765`. | CLOSED |
| T-03-07-01 | Tampering/Repudiation | checkpoint restoration | mitigate | `detection/reducer.go:195,205,242` retains the pre-pin maximum separately from durable sealed floor and restores both; `detection/replay_test.go:42,70,140` compares full canonical union/live state at every restart cut and repeated advancing pins, not counts alone. | CLOSED |
| T-03-07-02 | Denial of service | metadata/ledger/log validation | mitigate | `detection/reducer.go:219,226` enforces 1024-byte format/config context; `checkpoint.go:48` rejects ambiguous/trailing metadata; `kafkax/detector_consumer.go:270,280,284,459,469` uses bounded history requests and retained-start/end checks; `:167` makes restore failures terminal. | CLOSED |
| T-03-07-03 | Tampering | commit callback | mitigate | `kafkax/detector_consumer.go:554` starts with missing-callback error; `:558` checks transport; `:611` rejects nil, foreign/duplicate/missing partitions and every ErrorCode; only `:577` adopts acknowledged checkpoints. | CLOSED |
| T-03-08-01 | Tampering | YAML parser/fingerprint | mitigate | Runtime rules loader is called by `backend/cmd/detector/main.go:149`; `detection/config.go:45,84,110` checks size, exact mappings/types/duplicate keys/anchors/aliases/merges, KnownFields and EOF; `:69` checks rule/version/threshold/duration/severity bounds; `:128` sorts semantic fingerprint input. | CLOSED |
| T-03-08-02 | Tampering/Repudiation | version transition | mitigate | `backend/cmd/detector/main.go:51,134` verifies stopped group before joining; `kafkax/detector_consumer.go:366` checks sole current member. `storage/postgres/detection_decisions.go:155,261,281,370` requires locked saved manifest/exact acknowledged all-partition drain and rejects mixed configurations; `detection/transition.go:51,94` requires drain and increased changed-rule versions, persisting effective gates. | CLOSED |
| T-03-08-03 | Denial of service/Information disclosure | file/env startup | mitigate | `detection/config.go:46,69` caps 64 KiB input and checked counts/durations; parse errors are fixed strings. `config/config.go:588,670,676,680` invokes field/cross-field/resource/PG checks; `backend/cmd/detector/main.go:143` opens the operator path and calls that loader without printing its body. | CLOSED |
| T-03-09-01 | Denial of service | state/ledger accounting | mitigate | `detection/reducer.go:15` declares 64 MiB/100k/10k/4096 limits; `bounds.go:29,59,152` permits only lower fixture limits and rejects aggregate admission before mutation (`reducer.go:116`); startup `config/config.go:676` invokes overflow-safe `ComputeDetectorResources` at `:129`. | CLOSED |
| T-03-09-02 | Information disclosure | metrics/logs | mitigate | `detector/worker.go:422,458` updates one process counter; `opshttp/metrics.go:24,30` renders fixed numeric detection_late_events_total without labels. Worker infrastructure/quarantine messages are sanitized at `detector/worker.go:163` and `detector/poison.go:94,99`; ops errors never reach probe bodies (`opshttp/server.go:128`). | CLOSED |
| T-03-09-03 | Denial of service | readiness handler | mitigate | `opshttp/server.go:20,84,110` admits sixteen workers; independent request deadline at `:118,134` responds even if a check ignores cancellation; `:157` retains admission until checks actually finish. Real detector wires restored-assignment readiness at `backend/cmd/detector/main.go:91`; `detector/worker.go:36` checks authoritative member/generation/tokens. | CLOSED |
| T-03-10-01 | Spoofing/Tampering | key/entity/hash/fingerprint | mitigate | `alertwriter/poison.go:63,67,70` requires strict decode/ValidateIdentity before fingerprinting; `identity.go:24,27,32,37,44,48` reconstructs exact rule components and key/ID; shared immutable policy clears status/detected_at at `storage/clickhouse/alerts.go:23,28`. | CLOSED |
| T-03-10-02 | Tampering | concurrent conflicting writes | mitigate | Every writer effect goes through `alertwriter/writer.go:183`. `reservation.go:52,57,69,77,82,87,91` locks before global winner/orphan read and commits selected immutable authority before CH eligibility; `:95,100,114,125` re-locks/revalidates before insertion. `backend/migrations/postgres/006_alert_reservations.sql:4,16` enforces global uniqueness/immutability. Differing content returns ErrConflict before insert, then bounded DLQ (`alertwriter/poison.go:75`). | CLOSED |
| T-03-10-03 | Repudiation/Denial of service | partial effects/retry budget | mitigate | `alertwriter/poison.go:22,74` uses original shared attempts/deadline; `storage/clickhouse/alerts.go:45` constructs fresh batches. `reservation.go:91,125,132,136` propagates reservation/workflow uncertainty and CH failure; `writer.go:163,173` permits source commit only after complete effects. Native/model/process proof limits are recorded below. | CLOSED |
| T-03-11-01 | Information disclosure/Spoofing | DLQ origin/context | mitigate | Both real services bind DLQ (`backend/cmd/detector/main.go:66,71`; `backend/cmd/alert-writer/main.go:43,90`). `kafkax/dlq.go:104,158,167,174,189,235` derives trusted origin from actual record, caps/redacts value/key/framing/header inspection and rejects spoofed dlq.* headers; raw decoder error strings are never supplied as reasons. | CLOSED |
| T-03-11-02 | Tampering/Repudiation | partition durability | mitigate | `detector/poison.go:107,115,121` quarantines before poison acknowledgement; `detection/reducer.go:175` changes source bookkeeping without clocks. Valid late input follows `detector/worker.go:418,422,444`, with no poison classification. Both workers stop a failed partition at its first incomplete effect before committing the prefix (`detector/worker.go:204,219`; `alertwriter/writer.go:160,173`). | CLOSED |
| T-03-11-03 | Denial of service | poison aggregate deadline | mitigate | `detector/worker.go:187,198,201,219` creates one owned/effects budget, one shared retry object and independent commit reserve; `detector/poison.go:79,86,97` consumes shared attempts/backoff. Equivalent writer bounds at `alertwriter/writer.go:151,157,174`; all Run exits release rebalance ownership at `detector/worker.go:170` / `alertwriter/writer.go:103`. | CLOSED |
| T-03-12-01 | Elevation of privilege/Denial of service | fault controls | mitigate | Default detector hooks are unconditional no-ops in `detector/failpoint.go:1,13`; integration file requires build tag and once/private exclusive marker, <=30s wait at `detector/failpoint_integration.go:1,22,56,65,114`. Default writer reservation hook likewise no-op (`alertwriter/failpoint.go:1,8`), integration hook bounded/private (`failpoint_integration.go:15,22,29,34,41`). | CLOSED |
| T-03-12-02 | Tampering/Repudiation | recovery proof | mitigate | Actual reached boundary and exact predecessor checkpoint assertions at `backend/test/detector_recovery_test.go:189,214,219`; full canonical Kafka bytes, CH detail and PG decision comparisons at `:128,146,161,165`; resolved workflow complete row preservation at `:249`. These assertions exist independently of a duplicate-count-only check. | CLOSED |
| T-03-12-03 | Tampering | ownership callbacks | mitigate | `kafkax/detector_consumer.go:216` deletes only supplied revoked/lost partitions and manufactures no commit; `:302,334` completes only captured pending restorations, preserving retained entries. `detector/worker.go:252,281,404` releases lost state and checks current ownership before commits. Actual different-member target transfer/replay assertions at `backend/test/detector_rebalance_test.go:45,50,51,54,57`. | CLOSED |
| T-03-13-01 | Information disclosure | seed credentials/config | mitigate | `scripts/seed_scenarios.py:33` builds purpose-specific child environments, `:163` captures resolved Compose privately, `:430` reads private key with existing no-follow helper; subprocess argv contains helper paths/manifest paths, while credentials stay in `env` (`:424,450,482,508`). Private manifest read/write at `:70,112`; no resolved config print. | CLOSED |
| T-03-13-02 | Tampering/Repudiation | acceptance evidence | mitigate | `scripts/seed_scenarios.py:482` captures current offsets before `:490` sends; `backend/tools/check-alerts/main.go:341,503,739` verifies exact expected canonical sets over current Kafka range and both stores; `:765` uses original run_start receipt bound; `:758,762` compares complete prior snapshots. `seed_scenarios.py:514` requires all final proof booleans. | CLOSED |
| T-03-13-03 | Denial of service | startup/seed polling | mitigate | `scripts/seed_scenarios.py:121,157,419,454,512,529` uses finite subprocess caps clipped to original 15m deadline; native verifier `backend/tools/check-alerts/main.go:190,722` and snapshot `backend/tools/detection-state/catchup.go:51` are bounded. Active execution path preserves prior snapshots and contains no reset/topic deletion/OPTIMIZE/volume deletion. | CLOSED |
| T-03-14-01 | Repudiation/Tampering | CI empty selection/results | mitigate | `scripts/check-phase3-tests.sh:3,7,19,22,26,29,31,36` invokes every one of 46 exact package/name families separately under set -e; `scripts/go-test-nonempty.sh:4,22,27` preserves failed Go/pipe status and rejects no PASS. `.github/workflows/ci.yml:179,181,224` retains full integration, all Phase 2 groups and Phase 3 guard invocation. Subprocess assertions cover every empty/failing family at `scripts/test_phase3_guards.py:112,120`. Hosted execution is pending. | CLOSED |
| T-03-14-02 | Denial of service | test subprocess/runtime | mitigate | All family invocations have finite unchanged class budgets in `scripts/check-phase3-tests.sh:19,23,27,32,36,37`; `Makefile:57,61` records the measured 10m exhaustion and finite 32m full gate. CI jobs are bounded at `.github/workflows/ci.yml:168,213,233`. Runtime-change evidence is explicitly preserved in `03-ALERT-RESERVATION-EVIDENCE.md`; no final pass is inferred. | CLOSED |
| T-03-14-03 | Information disclosure/Elevation of privilege | default binaries/docs/fault hooks | mitigate | `.github/workflows/ci.yml:36` checks actual default Go file lists for detector/eventwriter/alertwriter and fails if integration hook is present; unconditional runtime no-ops cited in T-03-12-01. `docs/operations.md:195` states default exclusion and no public failure endpoint; private credential flow cited in T-03-13-01. Runtime ops mux exposes only fixed health/readiness/optional metric routes (`opshttp/server.go:89`). | CLOSED |
| T-03-15-01 | Tampering | Rule extraction | mitigate | Actual service calls strict loader at `backend/cmd/detector/main.go:47,149`; loader retains canonical fingerprint/predicate-bound settings at `detection/config.go:45,69,128`; runtime uses those rules in `detector/worker.go:145,418`. Native AUTH proof implementation at `backend/test/detector_pipeline_test.go:29`; no facade/no-op loader bypass found. | CLOSED |
| T-03-15-02 | Denial of service | YAML input | mitigate | Installed yaml parser is used directly by `detection/config.go:15,46,51,54`; read is capped before node construction and second decode must return EOF at `:61`. Exact-key node check `:110` rejects aliases/merges/duplicates. | CLOSED |
| T-03-16-01 | Tampering/Repudiation | Commit callback | mitigate | `kafkax/detector_consumer.go:551,558,562,611` validates exact request/complete partition response and transport/ErrorCode failures; `:167,516,639` propagates terminal restore failure through fatal callback/poll/readiness. Actual service passes lifecycle fail at `backend/cmd/detector/main.go:113`. | CLOSED |
| T-03-16-02 | Denial of service | Producer/consumer | mitigate | `kafkax/detector_consumer.go:137,149,270,506,549` uses validated processing/rebalance/fetch/restore/poll/commit bounds; `kafkax/alerts.go:28,60` sets finite output deadline/retries/buffers with all-ISR acknowledgement. Worker owned-batch bound at `detector/worker.go:157`. | CLOSED |
| T-03-17-01 | Tampering | Workflow replay | mitigate | `storage/postgres/alerts.go:36,58,91` locks global ID and inserts only; `alertwriter/reservation.go:57,87,110,131` preserves existing winner and skips equal workflow INSERT entirely. Existing resolved replay assertion at `backend/test/alertwriter_pipeline_test.go:170` and complete recovery row assertion `backend/test/detector_recovery_test.go:249`. | CLOSED |
| T-03-17-02 | Information disclosure | SQL/logging | mitigate | Native SQL values are parameterized (`storage/postgres/alerts.go:85,92`; `alert_reservations.go:13,23`; `storage/clickhouse/alerts.go:91`); PG lock/statement/whole-attempt lifetimes and independent cleanup at `storage/postgres/alerts.go:41,48,69,100`. Writer logs fixed retry/persistence errors (`alertwriter/poison.go:34,81`) without payload or DSN. | CLOSED |
| T-03-18-01 | Denial of service | PG outage/locks | mitigate | Startup validates PG durations/order and derives within existing batch budget (`config/config.go:151`); actual detector startup acquisition/cleanup is finite (`backend/cmd/detector/main.go:157`); all operational decision transactions apply server/client/cleanup limits (`storage/postgres/detection_decisions.go:85,108,117,122`) within worker ownership/effects bounds. | CLOSED |
| T-03-18-02 | Information disclosure | PG configuration | mitigate | Detector opens the existing pool with debug tracing disabled and sanitized dependency error at `backend/cmd/detector/main.go:164`; pool parsing drops DSN-bearing cause and wraps tracing in RedactingLogger (`database/database.go:36,75,90`). Operator child credentials use purpose-specific env (`scripts/seed_scenarios.py:33,187`) and no DSN argv. | CLOSED |
| T-03-19-01 | Tampering/Repudiation | Decision/cursor replay | mitigate | `storage/postgres/detection_decisions.go:604,608` validates immutable saved decision before any new cursor; `:662` checks exact bytes/fingerprint/source/config/trigger. New decision/cursor/frontier writes at `:671,699,709,715` share `:108` transaction and commit. Worker publishes after Decide returns (`detector/worker.go:428,436`). | CLOSED |
| T-03-19-02 | Spoofing/Tampering | Owner fence | mitigate | Runtime `detector/worker.go:404` validates actual Kafka group member/generation/assignment token before effects; PG `storage/postgres/detection_decisions.go:71,453,480,601` validates stable configured-group incarnation, exact fence and source position (`:611` rejects absent older decisions). Cross-source/ordinal/canonical identity validated at `:525`. | CLOSED |
| T-03-19-03 | Denial of service | PG failure | mitigate | `storage/postgres/detection_decisions.go:108,117,122,131` bounds lock/statement/transaction/independent rollback; connection release completes before Decide returns at `:626`. No transaction spans worker Kafka publication (`detector/worker.go:428,436`); failure returns terminal from Run (`:175`) and never enters poison DLQ (`detector/poison.go:107`). | CLOSED |
| T-03-20-01 | Tampering/Repudiation | Cross-store restore | mitigate | `detector/worker.go:363` calls PrepareRestore before processing; PG Register verifies marker/fence (`storage/postgres/detection_decisions.go:484,494`), requiring exact immutable decision row and at-least source frontier. Real rollback-state fixture asserts nonzero exit, unchanged Kafka metadata/PG rows, no output and zero DLQ at `backend/test/detector_cooldown_test.go:727,771,782,790`. | CLOSED |
| T-03-20-02 | Spoofing | Stale owner | mitigate | Actual membership/token/incarnation/source fencing is called at `detector/worker.go:350,404,425` and PG `storage/postgres/detection_decisions.go:71,480,601,611`. Native moved-target proof checks actual different member and stale rejection with unchanged rows (`backend/test/detector_cooldown_test.go:668,680,688,723`); cooperative addition restore uses captured pending set (`kafkax/detector_consumer.go:302`). | CLOSED |
| T-03-20-03 | Elevation of privilege/Denial of service | Fault hooks | mitigate | Decision boundary callback uses real PG transaction boundaries (`storage/postgres/detection_decisions.go:621,629`), injected by real worker `detector/worker.go:145`; default `detector/failpoint.go:13` is unconditional no-op. Integration barrier/observation requires private exclusive files and finite once-only bounds (`detector/failpoint_integration.go:22,56,65,114`). Reservation hook follows same isolation (`alertwriter/failpoint.go:8`). | CLOSED |
| T-03-21-01 | Tampering | Snapshot/schedule manifest | mitigate | Private protocol caps/exact fields at `backend/tools/detection-state/main.go:68,105`; `catchup.go:147` validates format2/pin/current fingerprint/processed-next and derives closed-prefix max; missing checkpoint with trimmed history refuses (`:183`). Generator reconstructs exact supplied schedule at `generator/src/sentinel_generator/cli.py:283,350,382` with no-follow bounded JSON `:265`. | CLOSED |
| T-03-21-02 | Denial of service | Future watermark/waits | mitigate | `backend/tools/detection-state/catchup.go:15,47,51,210,245` bounds aggregate scan to 100k/32MiB/60s and catchup/run time; `generator/src/sentinel_generator/live_schedule.py:209,221,254` reserves complete delivery/wait budget, enforces hard original <=900s timer and interruptible waits. Caller uses original 15m deadline (`scripts/seed_scenarios.py:419,454`). | CLOSED |
| T-03-21-03 | Information disclosure | Operator helper | mitigate | `backend/tools/detection-state/main.go:54,68,98,160` uses sanitized bounded stdin/stdout and environment configuration, without listener; no-follow private files at `scripts/seed_scenarios.py:70,112` and `generator/src/sentinel_generator/cli.py:179`. Purpose-specific env at `seed_scenarios.py:33`; sanitized child stages at `:138`. | CLOSED |

## Threat register — accepted dispositions

These 21 rows preserve the approved numbered plans' `accept` dispositions. They are not newly accepted missing runtime mitigations. Each has its own accepted-risk log entry below; no other threat was downgraded or accepted during this audit.

| Threat ID | Category | Component | Disposition | Evidence | Status |
| --- | --- | --- | --- | --- | --- |
| T-03-01-SC | Tampering | Dependency graph | accept | `03-01-PLAN.md:170`; AR-03-01-SC below | CLOSED — accepted risk |
| T-03-02-SC | Tampering | Dependency graph | accept | `03-02-PLAN.md:160`; AR-03-02-SC below | CLOSED — accepted risk |
| T-03-03-SC | Tampering | Dependency graph | accept | `03-03-PLAN.md:148`; AR-03-03-SC below | CLOSED — accepted risk |
| T-03-04-SC | Tampering | Dependency graph | accept | `03-04-PLAN.md:148`; AR-03-04-SC below | CLOSED — accepted risk |
| T-03-05-SC | Tampering | Dependency graph | accept | `03-05-PLAN.md:149`; AR-03-05-SC below | CLOSED — accepted risk |
| T-03-06-SC | Tampering | Dependency graph | accept | `03-06-PLAN.md:154`; AR-03-06-SC below | CLOSED — accepted risk |
| T-03-07-SC | Tampering | Dependency graph | accept | `03-07-PLAN.md:194`; AR-03-07-SC below | CLOSED — accepted risk |
| T-03-08-SC | Tampering | Dependency graph | accept | `03-08-PLAN.md:205`; AR-03-08-SC below | CLOSED — accepted risk |
| T-03-09-SC | Tampering | Dependency graph | accept | `03-09-PLAN.md:196`; AR-03-09-SC below | CLOSED — accepted risk |
| T-03-10-SC | Tampering | Dependency graph | accept | `03-10-PLAN.md:191`; AR-03-10-SC below | CLOSED — accepted risk |
| T-03-11-SC | Tampering | Dependency graph | accept | `03-11-PLAN.md:192`; AR-03-11-SC below | CLOSED — accepted risk |
| T-03-12-SC | Tampering | Dependency graph | accept | `03-12-PLAN.md:190`; AR-03-12-SC below | CLOSED — accepted risk |
| T-03-13-SC | Tampering | Dependency graph | accept | `03-13-PLAN.md:196`; AR-03-13-SC below | CLOSED — accepted risk |
| T-03-14-SC | Tampering | Dependency graph | accept | `03-14-PLAN.md:183`; AR-03-14-SC below | CLOSED — accepted risk |
| T-03-15-SC | Tampering | Dependencies | accept | `03-15-PLAN.md:125`; AR-03-15-SC below | CLOSED — accepted risk |
| T-03-16-SC | Tampering | Dependencies | accept | `03-16-PLAN.md:129`; AR-03-16-SC below | CLOSED — accepted risk |
| T-03-17-SC | Tampering | Dependencies | accept | `03-17-PLAN.md:134`; AR-03-17-SC below | CLOSED — accepted risk |
| T-03-18-SC | Tampering | Dependencies | accept | `03-18-PLAN.md:150`; AR-03-18-SC below | CLOSED — accepted risk |
| T-03-19-SC | Tampering | Dependencies | accept | `03-19-PLAN.md:145`; AR-03-19-SC below | CLOSED — accepted risk |
| T-03-20-SC | Tampering | Dependencies | accept | `03-20-PLAN.md:168`; AR-03-20-SC below | CLOSED — accepted risk |
| T-03-21-SC | Tampering | Dependencies | accept | `03-21-PLAN.md:145`; AR-03-21-SC below | CLOSED — accepted risk |

## Accepted risks log

SC policy carried forward verbatim in substance: reuse pinned Go/Python dependencies; no new npm/pip/cargo installation in the declared slice. Any new installation requires a research legitimacy audit and blocking registry verification for ASSUMED/SUS packages before installation; SLOP is forbidden. This accepts the existing dependency-graph exposure under that policy, not arbitrary future packages. The date below is this log's transcription date; the approving authority is the existing approved plan disposition, not a new auditor approval.

| Risk ID | Threat ref | Rationale | Accepted by | Logged |
| --- | --- | --- | --- | --- |
| AR-03-01-SC | T-03-01-SC | Pinned reuse / conditional installation policy above, `03-01-PLAN.md:170` | Approved plan disposition | 2026-10-20 |
| AR-03-02-SC | T-03-02-SC | Pinned reuse / conditional installation policy above, `03-02-PLAN.md:160` | Approved plan disposition | 2026-10-20 |
| AR-03-03-SC | T-03-03-SC | Pinned reuse / conditional installation policy above, `03-03-PLAN.md:148` | Approved plan disposition | 2026-10-20 |
| AR-03-04-SC | T-03-04-SC | Pinned reuse / conditional installation policy above, `03-04-PLAN.md:148` | Approved plan disposition | 2026-10-20 |
| AR-03-05-SC | T-03-05-SC | Pinned reuse / conditional installation policy above, `03-05-PLAN.md:149` | Approved plan disposition | 2026-10-20 |
| AR-03-06-SC | T-03-06-SC | Pinned reuse / conditional installation policy above, `03-06-PLAN.md:154` | Approved plan disposition | 2026-10-20 |
| AR-03-07-SC | T-03-07-SC | Pinned reuse / conditional installation policy above, `03-07-PLAN.md:194` | Approved plan disposition | 2026-10-20 |
| AR-03-08-SC | T-03-08-SC | Pinned reuse / conditional installation policy above, `03-08-PLAN.md:205` | Approved plan disposition | 2026-10-20 |
| AR-03-09-SC | T-03-09-SC | Pinned reuse / conditional installation policy above, `03-09-PLAN.md:196` | Approved plan disposition | 2026-10-20 |
| AR-03-10-SC | T-03-10-SC | Pinned reuse / conditional installation policy above, `03-10-PLAN.md:191` | Approved plan disposition | 2026-10-20 |
| AR-03-11-SC | T-03-11-SC | Pinned reuse / conditional installation policy above, `03-11-PLAN.md:192` | Approved plan disposition | 2026-10-20 |
| AR-03-12-SC | T-03-12-SC | Pinned reuse / conditional installation policy above, `03-12-PLAN.md:190` | Approved plan disposition | 2026-10-20 |
| AR-03-13-SC | T-03-13-SC | Pinned reuse / conditional installation policy above, `03-13-PLAN.md:196` | Approved plan disposition | 2026-10-20 |
| AR-03-14-SC | T-03-14-SC | Pinned reuse / conditional installation policy above, `03-14-PLAN.md:183` | Approved plan disposition | 2026-10-20 |
| AR-03-15-SC | T-03-15-SC | Pinned reuse / conditional installation policy above, `03-15-PLAN.md:125` | Approved plan disposition | 2026-10-20 |
| AR-03-16-SC | T-03-16-SC | Pinned reuse / conditional installation policy above, `03-16-PLAN.md:129` | Approved plan disposition | 2026-10-20 |
| AR-03-17-SC | T-03-17-SC | Pinned reuse / conditional installation policy above, `03-17-PLAN.md:134` | Approved plan disposition | 2026-10-20 |
| AR-03-18-SC | T-03-18-SC | Pinned reuse / conditional installation policy above, `03-18-PLAN.md:150` | Approved plan disposition | 2026-10-20 |
| AR-03-19-SC | T-03-19-SC | Pinned reuse / conditional installation policy above, `03-19-PLAN.md:145` | Approved plan disposition | 2026-10-20 |
| AR-03-20-SC | T-03-20-SC | Pinned reuse / conditional installation policy above, `03-20-PLAN.md:168` | Approved plan disposition | 2026-10-20 |
| AR-03-21-SC | T-03-21-SC | Pinned reuse / conditional installation policy above, `03-21-PLAN.md:145` | Approved plan disposition | 2026-10-20 |

No runtime integrity, ownership, fault-isolation, reservation, rollout, or unfinished acceptance defect was newly accepted. No transferred-risk documentation was needed because the register contains zero `transfer` rows.

## Summary flags and implementation-surface reconciliation

None of the 20 existing numbered summaries has a literal `## Threat Flags` section. Their adjacent security/threat/stub reviews were inspected rather than treating the missing heading as an empty complete inventory. Historical deferred work was checked against the final implementation, not interpreted as a remaining stub solely because an earlier summary deferred it.

| Summary inputs | Surface or statement checked | Threat mapping / disposition |
| --- | --- | --- |
| 03-01, 03-15, 03-16 | Bounded local rules and extracted Kafka adapters; no unplanned endpoint | T-03-01-01..03, T-03-08-01..03, T-03-15-01..02, T-03-16-01..02; informational |
| 03-02, 03-10, 03-17 | Native schema/storage, immutable identity/winner, workflow replay | T-03-02-01..03, T-03-10-01..03, T-03-17-01..02; informational |
| 03-03, 03-04, 03-05 | Exact scan/volume counters, unique evidence and frozen candidate; no new public surface | T-03-03-01..02, T-03-04-01..02, T-03-05-01..03; informational |
| 03-06 | New private schedule/manifests and bounded live admission | T-03-06-01..03, T-03-21-01..03; informational |
| 03-07, 03-08 | Bounded recovery metadata, saved semantic manifest/additive fence schema, admin-only Kafka membership | T-03-07-01..03, T-03-08-01..03, T-03-19-02, T-03-20-01; informational |
| 03-09 | Shared accounting, fixed metric, restored-assignment readiness | T-03-09-01..03; informational |
| 03-11 | Bounded DLQ and aggregate poison ownership budget | T-03-11-01..03; informational |
| 03-12 | Private integration-only fault files and moved-target recovery observations | T-03-12-01..03, T-03-14-03; informational |
| 03-13 | Private orchestration manifests and native current-run verification queries | T-03-13-01..03, T-03-21-03; informational |
| 03-18, 03-19, 03-20 | Real PG dependency, durable decision/cursor/frontier, fence and fault boundaries | T-03-18-01..02, T-03-19-01..03, T-03-20-01..03; informational |
| 03-21 | Private read-only snapshot helper, format2 schedule, bounded scan/waits | T-03-21-01..03; informational |
| Approved reservation addition after numbered summaries | Permanent global PG reservation, two-phase effect admission, migration006, private pre-effect hook and native proxy fixtures | Existing winner/conflict/failure obligations T-03-02-01/03, T-03-10-02/03, T-03-17-01/02; fault isolation T-03-12-01, T-03-14-03, T-03-20-03. Authority/constraint/call evidence is in register, not design intent. Informational; no new risk acceptance. |
| Missing 03-14 summary | Final CI/docs/operator execution evidence has not yet been returned | Audit input limitation; parent must reconcile the eventual summary and any new flags before claiming complete phase acceptance. Existing 03-14 code obligations were independently checked. |

`unregistered_flag` WARNING count: **0** in the inspected inputs/surface mappings. A missing future summary is not silently assumed to contain no flags. No blind new-vulnerability scan was performed.

## Reservation invariant and evidence boundaries

The original production review's winner defect and R1's uncertain-remote-completion defect are historical findings. R2 closes CR-02-R1 for conforming protocol writers. This audit independently found the current authority path: `alertwriter/poison.go:74` → `writer.go:183` → `reservation.go:48` → acknowledged reservation commit at `:91` → re-lock/reload at `:95,100` → CH insertion using that saved alert at `:125`. A preparation commit error returns at `writer.go:185` before phase two. Every direct writer insert call is behind that path. The global primary key and immutable UPDATE/DELETE trigger at migration006 lines4/16 preserve authority when a transaction or process ends. Incoming conflicting bytes cannot replace it; existing matching workflow creation is skipped at `reservation.go:131`.

Native CH `Send` still may return uncertainty (`storage/clickhouse/alerts.go:81`). No local timeout, rollback, cancel packet or stopped writer is treated as a remote-abort guarantee. Reservation safety does not depend on CH visibility or finality: once A may insert, only A's saved winner is eligible, including while A's remote result is unknown.

Proof classes remain distinct: native ACK-loss fixture `backend/test/alertwriter_ackloss_proxy_test.go:162` observes physical A before losing the real result; controlled delayed-visibility fixture `backend/test/alertwriter_reservation_fault_test.go:132` covers local uncertainty/transaction end/B admission before A visibility; process fixture `backend/test/alertwriter_reservation_restart_test.go:132` kills after committed authority but before any CH effect. The exact native pre-visibility pause remains unproved and is not claimed. Recorded focused executions belong to the executor evidence/R2, not to this static audit.

The separately recorded `03-RESERVATION-ROLLOUT-CHECK.md` establishes the local rollout sequence: old CH server terminated and all old applications stopped; infrastructure restored with writers disabled; all 91 physical alerts/global identities/workflows reconciled with all payload hashes checked and zero conflicts/orphans/missing evidence; additive migration006 applied, zero reservations initially, complete PG/physical CH snapshot hashes unchanged; only then upgraded startup released. This is attributed rollout evidence, not a live check by this auditor. All active writers conforming, intact PG durability/retained authority and consistent recovery remain explicit protocol assumptions. Permanent claims have no automatic expiry, transfer or GC; privileged schema destruction/direct CH writes and unresolved external legacy writers are not newly accepted risks.

## Open threats and limitations

**OPEN/BLOCKER: none.** `threats_open: 0` means every declared mitigation was found in its actual path, or the original plan's explicit acceptance is logged. It does not waive the following remaining phase obligations:

- At audit cutoff, final unit/static runs, all 46 independently guarded families, full integration and operator/preserved-state runs were still in progress. Their actual outcomes must be recorded by the executor and reconciled with this report. The historical aggregate scenario failures remain historical evidence until those results explain/reconcile them; this auditor claims no fresh green gate.
- `03-14-SUMMARY.md` was absent. Parent must inspect its eventual threat/stub flags and final changed surface, and update this audit if new evidence or implementation changes invalidate any closure. Source changes after the frozen head require scoped revalidation.
- Hosted CI is pending and is not certified by workflow-file presence. No deployment, full-phase completion, throughput/RSS guarantee, future JWT/API/UI/SSE behavior or broader production hardening is certified.

## Security audit trail

Root post-audit delta check: `26f3f8a` changes only two generator test files,
adding fail-if-key-load sentinels and stage-specific refusal diagnostics. No
runtime, schema, dependency, credential flow or control boundary changed.
The counterexample mutations now fail, normal67tests/static checks pass, and
independent `03-REVIEW-FINAL-DELTA-R1.md` closes the test-only warning. Existing
mitigation-presence closures remain applicable; the missing03-14 summary and
final gate reconciliation remain pending, not silently checked off.

| Audit date | Threats total | Mitigate closed | Accepted closed | Transfer closed | Open | Unregistered warnings | Run by |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-10-20 | 78 | 57 | 21 | 0 | 0 | 0 | gsd-security-auditor; frozen-source read-only verification |

Initial artifact audit, register authored at plan time. Original categories/dispositions preserved; no threat skipped and no missing mitigation repaired. Exactly 21 numbered plan blocks were included, all 20 available summary threat/stub inputs reconciled, and the approved reservation addition mapped to existing threats. No implementation mutation, test execution or commit by this auditor.

## Sign-off

- [x] All 78 plan-time threats classified by original disposition.
- [x] All 57 declared mitigations found in actual runtime/CI/proof paths.
- [x] All 21 original accepted dependency risks recorded individually.
- [x] No transfer dispositions omitted; no new accepted risk.
- [x] `threats_open: 0`; status verified for frozen registered-mitigation presence.
- [ ] Eventual 03-14 summary/final acceptance results reconciled by parent.
- [ ] Full Phase 3 goal verification and completion authorized by the parent workflow.

**Approval:** registered-threat mitigation-presence audit verified 2026-10-20 at `62099b20c2a98a1966a0d6f90e5b1d653fc1e855`; complete phase/release acceptance remains pending.
