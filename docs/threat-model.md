# Detection and alert pipeline threat model

This is the scoped Phase 3 trust model, not a security certification. Final
full local gates and the phase-wide security audit are pending. Scoped reservation
[R2 review](../.planning/phases/03-detection-alert-pipeline/03-REVIEW-PRODUCTION-R2.md)
is clean (zero findings), closing CR-02-R1 for conforming protocol writers without
certifying deployment or this complete threat model. The [architecture](architecture.md) describes the selected protocol;
per-plan execution summaries distinguish observed tests from pending evidence.

## Assets and boundaries

Assets are acknowledged event history, deterministic detection evidence,
immutable cooldown decisions, source recovery metadata, analyst workflow state,
tenant separation and private producer credentials. The relevant boundaries are:

| Boundary | Required control |
| --- | --- |
| Producer → ingest | Verified scoped key, atomic bounded validation, server-owned tenant/receipt time, bounded quotas and all-ISR acknowledgement |
| Kafka event → detector | Strict decode/key check; ordered event-time state; bounded windows, evidence and source-prefix accounting |
| Kafka ownership → PG decisions | Current member/generation/token/config fence and ordered frontier; completed authoritative restore before work |
| PG decision → Kafka alert | Immutable allow/suppress and canonical bytes committed before publication; checkpoint only after complete effects |
| Kafka alert → PG reservation → CH/PG workflow | Canonical ID/key, committed permanent global immutable winner before CH, revalidated serialized effects, insert-only workflow and tenant parameters |
| Checkpoint/PG history → restored worker | Strict format 2, retained pin, pre-pin maximum/sealed floor, immutable decision-frontier and at-least PG fence |
| Private operator files/processes → acceptance | Private no-follow bounded input/output, purpose-specific child environment and current-run exact verification |
| CI selection → correctness claim | Separate exact nonempty runner per named family; propagate command failure; finite test/job budgets |

## Threats and verification

| Threat | Mitigation and evidence family |
| --- | --- |
| Forged tenant/key or ambiguous identity | API-key-derived tenant, canonical event/alert keys and length-prefixed `DedupeKey`; real ingest authorization, alert conflict and tenant-scope tests |
| Replay changes evidence or reopens resolved workflow | First-threshold frozen bytes, immutable allow/suppress lookup before current cursor, global winner/preflight and insert-only workflow creation; CooldownReplay/Crash, AlertWorkflowReplay and concurrent/partial-store tests |
| Stale owner writes or advances recovery | Kafka generation/member/assignment-token/config fencing, pending offset restoration, per-partition source ordering; actual moved-target ThirdReplica/CooldownOwners and callback tests |
| Lost allowed alert across PG/Kafka/CH failure | PG decision before allowed publication, all-ISR acknowledgement before checkpoint, CH+PG before writer commit; observed decision/publication/checkpoint crash cuts and outage/partial-storage tests |
| Divergent or older restored PG state silently starts fresh | Exact checkpoint decision row/fingerprint and fence at least its marker; legitimate PG-ahead replay allowed, behind/mismatch refuses with unchanged Kafka metadata; RestoreBehindCheckpoint/frontier tests |
| Concurrent/late remote writer replaces the global winner | Commit immutable canonical reservation before any CH insert; revalidate under a second per-ID lock; use saved bytes only; ReservationProtocol/Reservation/Ambiguity/Restart/NativeAckLoss evidence, scoped R2 clean, final gates pending |
| Uncertain PG commit or corrupted claim permits unsafe progress | Any reservation commit error prohibits this attempt's CH insert; replay resolves durable outcome; strict saved-payload/identity/fingerprint validation fails operationally, not as poison |
| Unbounded state, retries or hostile poison | Shared admission limits, finite owned-batch/effects deadlines, bounded buffers and DLQ framing, independent final commit allowance; StateBounds/Capacity and both worker Poison/aggregate-budget tests |
| Late or sealed history becomes new evidence after replay | Pre-pin maximum reproduces lateness; durable sealed floor excludes closed windows; equality/+1ms, arbitrary restart-cut and retained-history tests |
| New configuration reinterprets an old alert ID | Saved semantic manifest, changed-rule version checks, exact acknowledged drain, zero old members/sole new member, aligned persisted effective-from gates; ConfigRestart/WindowEnlargement/LedgerHandover tests |
| Production fault switch or information disclosure | Default hooks unconditional no-op/no environment reads; integration-only private finite controls, default go-list/binary guards; no public debug endpoint |
| Credential/raw failure data leaks | Environment/private-file keys; purpose-isolated children, fixed sanitized errors/stages, private manifests and bounded origin headers; script/private-protocol guards |
| Empty or failed tests reported green | Each named Phase 3 family invokes `go-test-nonempty.sh` separately; fake-Go subprocess tests reject every empty family and failing Go command even with a PASS line; Phase 2 groups remain required |
| Historical rows satisfy a new scenario | Original `run_start` receipt bound, captured current Kafka range, exact canonical bytes/store fields and independent full prior snapshots; CheckAlertsCurrentEventRange and preserved-run tests |

The listed families identify obligations and recorded evidence; their presence is
not an assertion that the pending final suite or hosted CI passed. The last full
aggregate result and every scoped repair must be recorded in Plan 14's summary.
Reservations are the architectural repair for remote completion after a local
transaction ends, not a claim of server-side rollback. Native ACK-loss evidence
observes physical insertion before suppressing the response; a separate controlled
model exercises invisible deferred completion after PG transaction end and B's
attempt. Process-restart evidence observes a committed claim before any CH effect.
These proof classes are not interchangeable. Exact native pre-visibility delay is
unproved; independent whole-fix R2 closes CR-02-R1 through the durable admission
invariant rather than assumed remote abort. See the
[reservation evidence](../.planning/phases/03-detection-alert-pipeline/03-ALERT-RESERVATION-EVIDENCE.md).

## Residual limits and operator obligations

Source retention must cover active pins and downtime. Idle wall time does not
release pins; retention loss fails closed. State/resource limits are accounting
bounds rather than RSS/throughput guarantees. Operational decisions/cursors have
no automatic GC and can grow on disk. Permanent alert reservations also have no
expiry/lease/ownership transfer: ordinary UPDATE/DELETE is forbidden, and claim
retention is a correctness obligation. Intact PG durability, consistent recovery
and **all** active writers following this protocol are assumptions. Privileged
schema destruction/direct CH writes are outside that boundary. No alert
retention/compliance duration is invented.

The additive schema rollout requires drained/stopped old writers and explicit
reconciliation of old uncertain remote writes before new traffic. Process death
is not proof of remote completion. Mixed-protocol rolling deployment is unsafe;
unresolved outcomes must block rollout. The separately recorded
[rollout check](../.planning/phases/03-detection-alert-pipeline/03-RESERVATION-ROLLOUT-CHECK.md)
reconciles all 91 retained identities and proves migration preservation before
authorized new-protocol startup. Subsequent stack health is not a substitute for
the final full integration/operator gates.
See [operations](operations.md#reservation-protocol-rollout).

Changed-config rolling handover is unsupported. Live demos require quiescent
other producers, caught-up read-only snapshots and legal marker scheduling;
far-future or moving source state can cause bounded refusal. CH physical
duplicates and repeated canonical Kafka publication remain possible under
at-least-once delivery; immediate visible identity dedupe is the contract.

The local Docker stack has loopback-only publications but plaintext inside its
development network. Its local exceptions are not production TLS or secret
distribution. DLQ values can retain original event payloads and require the same
data handling as stored events. No public failure control, arbitrary SQL, user
executable rule DSL or untrusted-network deployment is introduced.

PG `seq` is allocation order, not commit order. Phase 4 must prove backfill across
late lower-sequence commits before claiming lossless SSE. JWT/session login,
complete query/triage APIs, UI, SSE, Kubernetes deployment and broad hardening are
later scope. This document does not certify those features, and contains no
unmeasured performance claim.
