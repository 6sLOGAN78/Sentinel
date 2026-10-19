# Detector rule configuration and handover

The detector loads `config/rules.yaml` once at startup. Set
`SENTINEL_DETECTOR.RULES_PATH` to a path relative to the detector's working
directory, or use an absolute path. `make run SVC=detector` starts in `backend`,
so its default is `../config/rules.yaml`. There is no hot reload or public rule
editing API. Invalid configuration terminates startup before source consumption
or alert publication.

The file must contain exactly one YAML document, at most 64 KiB, with
`schema_version: 1` and exactly four rule definitions. Every definition requires
`id`, `version`, `enabled`, `threshold`, `window`, `severity`, and `cooldown`.
Unknown or differently cased keys, duplicates, aliases, anchors, merge keys,
nulls, missing fields, extra documents, unsupported IDs and enums fail closed.
Versions are integers from 1 through 2147483647. Thresholds are 1..4096; all
rules retain exact unique event IDs up to this capacity, and SCAN counts exact
distinct destination ports within those events. Window durations are 1s..1h
at whole-millisecond precision; cooldowns are 1ms..1h at that same precision.
Severity is `low`, `medium`, `high`, or `critical`. Disabled rules still require
complete valid settings and participate in configuration identity.

| Rule | Matching event and entity (always scoped by tenant) | Default |
|---|---|---|
| `AUTH_BRUTE_FORCE` | `auth.failure`, source IP and nonempty principal | 10 unique events / 300s; high; 60s cooldown |
| `PORT_SCAN` | `net.connection`, source IP, destination IP and destination port | 20 distinct ports / 60s; medium; 30s cooldown |
| `REQUEST_FLOOD` | `net.request`, source IP | 300 unique events / 60s; high; 30s cooldown |
| `DENY_SPIKE` | `policy.decision` with `action=deny`, source IP | 30 unique events / 120s; medium; 60s cooldown |

All four defaults are enabled at version 1. Missing grouping fields produce no
match. Windows are epoch-aligned, half-open intervals `[start,end)`. Their
partition watermark is the greatest processed event time minus 30s; equality is
accepted. Event time advances only through valid source records. Wall-clock
idleness neither seals a window nor releases its pin. The first threshold
crossing freezes evidence and at most the first 20 accepted unique event IDs.
Rule evaluation order is AUTH, SCAN, FLOOD, DENY regardless of YAML ordering.

The semantic SHA-256 fingerprint includes all parsed settings, the four versions,
lateness, evidence/state bounds and fixed predicate, window, evidence, identity
and cooldown policy versions. Reordering YAML cannot change it. Editing any
rule's threshold, window, severity, cooldown or enablement requires increasing
that particular rule's version. Increasing another rule's version cannot mask
an equal-version edit. Unchanged rules may retain their versions; no version may
decrease. Changing a binary's fixed semantics also changes the fingerprint and
must be treated as a compatibility migration; a prior fingerprint that cannot
be verified against its saved semantic manifest is rejected.

## Durable cooldown and recovery

Cooldown suppresses repeat alerts across nearby windows for the same tenant,
rule and entity. AUTH crossings at 299s and 301s fall in separate windows but are
only 2s apart: with a 60s cooldown only the first is visible. Candidate time is
the crossing event's `occurred_at`. The clock is the maximum of this time and the
prior candidate clock. A clock equal to the next eligible time is allowed. A
suppression leaves eligibility unchanged, and its window cannot fire again
later merely because cooldown elapsed.

PostgreSQL commits an immutable allow/suppress decision with canonical bytes
before the allowed bytes are published to Kafka. Suppressions create no Kafka
alert, ClickHouse detection or workflow. A replay returns the old decision and
bytes without reevaluating today's cursor. An allowed publication can repeat
physically after a crash; the existing alert identity deduplicates visible
detections, and workflow insertion never reopens a resolved row. This is an
at-least-once pipeline with independently acknowledged effects.

Keep Kafka source retention long enough to retain the earliest active state
pin. Fired windows also retain pins until event-time expiration. Memory limits
are 10,000 windows, 4,096 unique events per window, 100,000 source-prefix entries
and a conservative 64 MiB state allowance; admission failure stops processing
without discarding live evidence. Kafka checkpoints remain bounded to 1,024
bytes. A trimmed pin, missing metadata or inconsistent recovery field is a
terminal incompatibility, not permission to reset the group. The lifecycle
reports failure and withdraws readiness.

Operational decisions and old cooldown cursors have no automatic garbage
collection. They can outlive cooldown and alert evidence. Monitor database disk
growth and preserve these rows across configuration changes. Kafka offsets and
PostgreSQL decisions/fences/proofs must be backed up and restored consistently;
a checkpoint referencing lost PostgreSQL history fails closed. Arbitrary group
renaming, changing incarnation, resetting offsets or deleting ledger rows does
not constitute a safe upgrade. Planned namespace resets require stopped,
explicit maintenance outside this procedure.

## Restart with changed rules

This implementation conservatively permits changed rules only when the new
detector is the sole member of its existing Kafka group. Unchanged-config
replicas continue to support normal multi-replica operation. A rolling restart
mixing old and new rule settings is unsupported.

1. Pause other producers, retain the old rules and let the old detector finish
   its source backlog. Use valid, authenticated `auth.success` progress records
   with each touched source key `tenant_id|source_ip`. Send threat contributors
   before their markers. Choose a shared marker time at or after the latest
   active rule window end plus 30s. A marker on each touched partition advances
   event time without opening any of the four rule windows. A marker must also
   satisfy ingest's timestamp-skew limits; wait until its time is permitted.
   Sending random newer matching traffic can open more windows.
2. Observe real OffsetFetch metadata on every source partition: `drained=true`,
   pin equal to `processed_next`, and `processed_next` caught up to the paused
   source-end snapshot. Check the actual PostgreSQL partition fences: `drained`
   is true and `drain_checkpoint` identifies that exact offset and metadata.
   Its semantic manifest is stored in `rules_manifest`. A broker offset alone
   cannot prove completion. An unused partition must first acquire an explicit
   acknowledged empty checkpoint; do not infer drain from a missing offset.
3. Stop **all** old detector replicas and keep the producers paused. Edit the
   rules and increase each changed rule's version. Retain group, source topics,
   partition layout, database and decision history. Apply the additive database
   migrations, including `005_detection_config.sql`, using the normal runner.
   An older deployment must first register/checkpoint with unchanged compatible
   semantics to populate the manifest; missing manifest/proof cannot authorize
   a configuration change.
4. Start exactly one new detector with
   `SENTINEL_DETECTOR.ALLOW_CONFIG_TRANSITION=true`. A bounded admin-only check
   requires zero existing members before constructing a consuming client; this
   check cannot itself evict or rebalance an old replica. After joining, the
   detector also verifies actual sole membership, including when it is assigned
   zero partitions. PostgreSQL locks the stable group partition fences in
   topic/partition order, verifies all old drains and the old semantic manifest,
   checks each version, and authorizes the new fingerprint. An old owner cannot
   create further decisions. Old decisions and cursors remain intact; new
   fingerprint-scoped cursors start empty. No caller-supplied fingerprint or
   `drained` flag can substitute for the saved acknowledged proof.
5. Verify all partitions' new fingerprints, four versions and four
   `rule_effective_from` gates in their broker checkpoints and their matching
   PostgreSQL authorization. The gate for each rule is
   `ceil(max_verified_old_sealed_through / new_window_ms) * new_window_ms`.
   New windows starting below that gate are deliberately skipped. For example,
   enlarging 300s to 600s with a handover at 300s skips the partial `[0,600)`
   window and starts at 600s. This intentional detection gap prevents reuse of
   historical alert IDs: rule version is excluded from `DedupeKey`. Equality at
   the gate is accepted; shrinking windows and threshold-only changes use the
   same alignment rule. Gates persist across subsequent restarts.
6. Resume producers after verification. Additional replicas may start with the
   same new rules once every partition has adopted its checkpoint. Set
   `ALLOW_CONFIG_TRANSITION=false` for ordinary restarts. If PostgreSQL committed
   authorization but the new Kafka checkpoint failed, retry with the same new
   rules and the sole-member transition flag: the saved two-sided
   `handover_checkpoint` reproduces the exact gates. Do not delete proofs or
   rerun old rules to force progress.

A drain marker is recorded in PostgreSQL only after all decision/publication
effects and the successful synchronous Kafka checkpoint callback. Pending
allowed bytes or a decision frontier ahead of that checkpoint prevent drain
authorization. Failure after Kafka acknowledgement but before recording its PG
proof requires an unchanged-config restart to establish the proof safely.
