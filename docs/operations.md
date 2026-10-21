# Detection and alert operations

Phase 3 local/hosted acceptance and final 59/59 goal verification passed.
Phase 4 is not started. The scoped
[reservation R2 review](../.planning/phases/03-detection-alert-pipeline/03-REVIEW-PRODUCTION-R2.md)
is clean; it does not establish deployment or current-stack acceptance. Use this
as the selected operator protocol; the final acceptance record must accompany a
release. [Rules](rules.md) and [ADR 0007](decisions/0007-event-time-state-pin-recovery.md)
define the recovery contract. [Event storage](event-storage.md) covers the existing
event-writer and event TTL independently.

## Start and observe

From the repository root with private local `.env` settings:

For an existing pre-reservation deployment, first complete the
[reservation rollout gate](#reservation-protocol-rollout); the commands below
are not authorization to reconnect old writers or run a mixed-protocol upgrade.

```sh
make dev-up
scripts/check-stack.sh
```

Startup waits for Kafka/CH/PG/Redis, explicitly initializes topics, migrates both
databases, then starts all five applications. Repeating it preserves named
volumes, topic layout, group identities and existing data. It reapplies configured
retention; it does not authorize a rule change. The checked-in pinned shared image,
static loopback-only health probe and read-only rules mount remain in use.

All published host ports bind loopback. Ingest/query use 8080/8081. Operations
ports are ingest 9101, detector 9102, event-writer 9103, alert-writer 9104 and query
9105. Detector's `/metrics` currently exposes the fixed-label
`detection_late_events_total` counter; full observability is later work.

```sh
curl --fail --silent --max-time 5 http://127.0.0.1:9102/healthz
curl --fail --silent --max-time 5 http://127.0.0.1:9102/readyz
curl --fail --silent --max-time 5 http://127.0.0.1:9102/metrics
```

Liveness is process-local. Readiness checks actual dependency capability and
completed current-assignment restoration; a joined-empty replica is valid only
after its authoritative callback. PG is a real detector dependency. A healthy
empty source can have an acknowledged empty format-2 checkpoint with offset and
`processed_next` zero; that is different from missing/pending restoration.
Readiness retains sixteen-worker admission and independently bounded responses.

## Failure, replay and retention

The pipeline is at-least-once. A crash after publication can publish the same
canonical alert again. Immediate CH read dedupe and insert-only PG workflow
creation retain one visible identity and do not reopen resolved rows. CH success
before PG creation is repairable partial storage; replay must inspect immutable
orphan content before completing workflow creation. Infrastructure failures
remain bounded worker failures, not poison.

Detector windows are tumbling, event-time intervals. Wall-clock waiting does not
expire a window or release its pin. A valid nonmatching `auth.success` marker on
the same source key can advance that partition, but must satisfy ingest skew.
Never evict live windows, discard decision rows, reinterpret a state pin as
consumed-next, or reset a group to make readiness green.

Keep source retention beyond the oldest active pin plus downtime and catch-up.
Fired windows retain pins too. Local Kafka defaults are 24h events and seven days
alerts/DLQ; these are defaults, not recovery-duration guarantees. Event CH TTL is
independent (seven days by default). Idle state can outlive source retention;
trimmed pins or truncated history fail closed. Changing topic partition counts
also changes entity placement and is not a supported live recovery operation.

Format-2 metadata is strict and at most 1,024 bytes: fingerprint, all versions,
nullable pre-pin maximum and durable sealed floor, `processed_next`, four
`rule_effective_from` gates, drained status and decision frontier. The pin can be
behind processed-next while a window is active. Recovery must reproduce full
canonical evidence and first-threshold ordering, not only alert counts.

PG's immutable allow/suppress ledger, entity/config cursors and partition fences
are separate operational recovery history. They have no automatic GC, even when
cooldown has expired; monitor disk growth. There is no fixed retention period or
disk-capacity guarantee for these rows. Do not delete a replay-reachable decision
or pending allowed payload. Back up and restore Kafka offsets/history and PG
decisions/fences/proofs consistently. A checkpoint's decision frontier must match
an immutable PG row and a fence at least that far. PG ahead is expected after a
decision-before-publication crash; PG behind or mismatched fails closed.

Group generation rollback/recreation, changed incarnation and inconsistent store
restores need explicit stopped maintenance, not automatic namespace adoption.
Keep the failed evidence and restore a consistent recovery set before restarting.
No alert TTL or ledger-GC procedure is supplied by this phase.

## Change rules through a stopped-owner handover

Follow the complete [rules handover procedure](rules.md). In particular:

1. Pause other producers. Keep the old rules while draining contributors and all
   allowed publications, then send legal same-key nonmatching progress markers.
2. Confirm every source partition is caught up and explicitly drained: broker
   pin equals `processed_next` equals the stable source end. PG must contain that
   exact acknowledged drain checkpoint and saved semantic manifest. Missing
   metadata is not proof of an empty partition.
3. Stop every old detector member. Increase each changed rule's version and
   retain group/topic layout, operational history and the required migrations.
4. Start one new detector with `ALLOW_CONFIG_TRANSITION=true`. Pre-join admission
   requires zero old members; post-join admission requires sole membership.
   Unchanged-config replicas still support ordinary multi-member operation.
5. Verify the new fingerprint, versions and all persisted effective-from gates
   before resuming producers. Set the transition flag false for ordinary starts.

The gate is `ceil(max_verified_old_sealed_through / new_window) * new_window`.
Enlarging a 300s window at a 300s handover to 600s deliberately skips the partial
`[0,600)` window. Equality at 600s is eligible. This transition gap prevents
historical ID reuse because rule version is excluded from alert identity. Old
decisions remain immutable; new-config cursors start separately. This is a
stopped-owner transition, not hot reload or a mixed-config rolling restart.

## Capacity and bounded failures

Default admission limits include 10,000 active windows, 4,096 unique IDs/window,
100,000 prefix entries and 64 MiB accounted state shared across partitions.
Config additionally accounts fetch/concurrency/producer/state against 256 MiB.
Neither figure is a process-RSS guarantee. Capacity refusal terminates before
unsafe mutation/checkpoint advance; adding wall-clock time cannot repair it.

Workers share bounded owned-batch effects/retry allowances and preserve only the
contiguous durable prefix. Failed PG locks/commits, CH writes, DLQ publication or
authoritative checkpoint callbacks cannot authorize later unsafe progress.
Decision commit uncertainty is resolved through immutable replay. Logs use fixed
sanitized errors; do not export raw DLQ values as ordinary debug logs.

Remote CH commit after disconnect remains possible. A local canceled context is
not proof that the remote insert never committed. Permanent PG
reservations constrain conforming writers' effects to committed canonical bytes;
local lock/cancellation tests alone are not a remote-abort guarantee. See the
[proof boundaries](../.planning/phases/03-detection-alert-pipeline/03-ALERT-RESERVATION-EVIDENCE.md).

## Reservation protocol rollout

Migration `006_alert_reservations.sql` is additive and runs through the existing
PostgreSQL migration runner. Do not edit applied migrations, backfill/rewrite
workflow state, change the CH engine, reset groups or delete data for this rollout.
Reservations are created lazily from matching legacy workflow/orphan evidence;
equal replay retains the original sequence and analyst state.

1. Quiesce alert producers, drain and stop **all** old alert-writer instances,
   including restart policies and other hosts. No mixed old/new protocol is safe.
2. Reconcile any old uncertain native write before proceeding. A stopped process
   or closed socket does not prove that its remote write cannot still complete.
   Preserve the evidence; unresolved old outcomes block rollout rather than
   allowing a guessed winner or a new producer run.
3. Only after that gate, use the normal additive migration/startup path with the
   new binary. Verify schema 006 and that every active writer uses reservations
   before admitting traffic. Scoped R2 is clean, but final local acceptance and
   phase-wide verification must also pass before release.

The [preserved-stack rollout check](../.planning/phases/03-detection-alert-pipeline/03-RESERVATION-ROLLOUT-CHECK.md)
records confirmed old CH server termination, all five old applications stopped,
infrastructure-only restoration, full reconciliation of 91 physical alerts/IDs/
workflows, and additive migration 006 with complete snapshots unchanged. Root
then authorized new-protocol startup. The executor's subsequent `make dev-up`
returned zero, followed by `scripts/check-stack.sh` (exit 0, 13.32s), observing all
five applications and four dependencies healthy. The later preserved startup
and health checks passed in 48.69s and 14.32s, respectively. Local full integration,
operator/preserved-repeat and retained regression results are recorded in the
[acceptance evidence](../.planning/phases/03-detection-alert-pipeline/03-ALERT-RESERVATION-EVIDENCE.md);
these local results do not establish a hosted CI pass.

`alert_reservations` is separate operational authority, not the workflow/list
index or detector cooldown ledger. Its globally unique ID, tenant, fingerprint
and canonical bytes are immutable; `reserved_at` is neither expiry nor commit
order. Monitor permanent disk growth: no fixed retention, GC, lease or transfer
procedure is supplied. Ordinary UPDATE/DELETE are rejected; privileged schema
destruction is outside the writer boundary. Back up this authority consistently
with workflow, CH evidence and source history. Losing PG claims invalidates the
guarantee against an old remote effect. Do not delete a claim because its source
record, cooldown or current workflow is absent.

An uncertain reservation commit causes no CH write in that attempt. Replay reads
the durable outcome. CH failure leaves the claim and source unacknowledged;
workflow is created only after CH acknowledgement/matching retained evidence.
An equal existing workflow remains unchanged. Corrupt saved state is a bounded
operational refusal, not a poison record. There is no background reservation
sweeper; source replay supplies recovery, subject to retained source history.

## Regression commands

With a healthy preserved dev stack and other producers paused:

```sh
make test
make lint
make test-integ
bash scripts/check-phase3-tests.sh
```

The full suite has a finite 32m aggregate ceiling: a measured previous 10m run
exhausted its limit before scenarios. Each exact Phase 3 family has its own
nonempty runner invocation: ordinary 6m, cooldown 8m, pure families 60s,
scheduled scenario 16m, preserved repeat 32m. The relevant CI jobs use 45m.
The local acceptance record reports actual passing exits, including the repaired
full integration run and seven independently nonempty retained Phase 2 groups.
Hosted[CI39515550386](https://github.com/6sLOGAN78/Sentinel/actions/runs/39515550386)
passed all nine jobs on the repaired source; test existence or a broad regex
alone cannot establish a pass. Production builds exclude integration hook
files and read no fault-control environment settings. Fault tests alone build
integration binaries; there is no public failure-control endpoint.

For `seq`/commit-order and future backfill constraints, see
[architecture](architecture.md#alert-storage-and-phase-4-handoff). Phase 4 owns
that design; these operations do not provide an SSE implementation.
