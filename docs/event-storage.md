# Event storage operations

Sentinel stores events in the ClickHouse database selected by the migration
tool's `--database` option. The migration history keeps the original seven-day
TTL in `002_events.sql`; operators change retention through the migration tool
without editing that immutable history.

## Configure event retention

The retention value is resolved in this order:

1. `--events-ttl-days` on `backend/tools/chmigrate`
2. `SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS`
3. the seven-day local default

The value must be a base-10 integer from 1 through 3650. Invalid, negative,
zero, non-integer, and overflowing values are rejected before an `ALTER` is
sent to ClickHouse. The configured database name is validated as an identifier;
the tool does not accept arbitrary SQL.

For the normal local workflow, add the desired value to the untracked `.env`
file and rerun migrations:

```dotenv
SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS=14
```

```console
make migrate
```

`make migrate` forwards the plain environment value without evaluating it.
The tool first applies any pending migrations, then executes the fixed
`ALTER TABLE <configured-database>.events MODIFY TTL ...` operation on every
successful run. This means a retention change still takes effect when the
schema is already up to date. Repeating the same value is safe and does not add
or modify migration-history rows.

When invoking the tool directly, an explicit flag overrides the environment:

```console
cd backend
go run ./tools/chmigrate --database sentinel --events-ttl-days 14
```

`SENTINEL_CLICKHOUSE_PASSWORD` must still be present in the environment. Do not
put passwords in command-line arguments.

`MODIFY TTL` changes the table rule immediately, but deletion is asynchronous.
Existing parts are evaluated as ClickHouse processes and merges them; they are
not synchronously rewritten or deleted by the command. Sentinel deliberately
does not run `MATERIALIZE TTL` automatically because it can rewrite existing
parts and create substantial load.

## Duplicate visibility

Delivery is at-least-once, so ClickHouse may contain multiple physical rows for
the same `event_id`. Sentinel does not claim exactly-once writes and does not
wait for `ReplacingMergeTree` background merges to make retries safe.

User-visible point lookups collapse rows by `event_id` at read time, and
user-visible counts use `uniqExact(event_id)`. Identical retries therefore have
one visible identity even while duplicate physical rows remain.

Clients must not reuse an `event_id` for different logical content. If rows
with conflicting content share an ID, read-time deduplication guarantees only
one visible identity, not a permanent content winner. Background merges and
equal-millisecond `received_at` values can change which conflicting row is
retained. See [ADR 0002](decisions/0002-event-id-policy.md) for the complete
identity contract.

## Poison records and the event DLQ

The event-writer quarantines only records that cannot become valid stored
events. It publishes them to `security.events.dlq.v1` with one of these stable
reasons:

- `decode_error`: invalid JSON, unknown fields, type mismatches, trailing JSON,
  or another failure to decode the versioned event contract
- `validation_error`: decoded JSON that violates the event schema, including an
  unsupported schema version or a missing/invalid field other than
  `received_at`
- `missing_received_at`: the server-owned ingestion timestamp is absent or zero

The reason is always an enum. Decoder text, validation text, stack traces, and
exception messages are not copied to Kafka headers.

Each DLQ record keeps the source key and, by default, source value. It includes
`dlq.reason`, `dlq.origin.topic`, `dlq.origin.partition`,
`dlq.origin.offset`, and a UTC RFC 3339 `dlq.at`. Non-`dlq.*` source headers are
copied only while the record remains within its bounded metadata allowance.
Source headers cannot override trusted `dlq.*` fields.

The default source-value cap is 64 KiB (`SENTINEL_WRITER.DLQ_MAX_BYTES`). A
larger value is replaced by an empty value with
`dlq.value.redacted=true` and `dlq.value.original_bytes=<size>`. An abnormally
large key is similarly removed and described by `dlq.key.redacted` and
`dlq.key.original_bytes`. The complete encoded record is bounded by the value
cap plus an 8 KiB metadata allowance, so a source-header flood cannot create an
unbounded quarantine message.

DLQ values can contain the original event payload. Inspecting or exporting
them requires the same data access and handling controls as inspecting stored
events; the DLQ is not a lower-trust debugging channel.

## Offset ordering and recovery

Every source partition advances only through its contiguous durable prefix. A
valid record is durable after ClickHouse acknowledges its insert; a poison
record is durable after the all-ISR DLQ producer acknowledges it. A later
record never causes an earlier failed insert or failed DLQ publish in the same
partition to be committed. Other partitions in the same poll may independently
commit their own durable prefixes.

ClickHouse/network failures are infrastructure failures, not poison. They do
not create DLQ records: the writer exhausts its bounded retry budget, exits,
and leaves the source offsets replayable. Likewise, if the DLQ topic is missing
or Kafka cannot acknowledge the quarantine record, the writer exits without
committing past that poison record. Restore Kafka access or create the expected
DLQ topic, then restart the writer with the same consumer group; it resumes at
the earliest uncommitted offset.

Delivery is at-least-once on both paths. A shutdown or ambiguous acknowledgement
can cause a valid ClickHouse row or a DLQ record to be written again before its
source offset is committed. Event reads collapse duplicate `event_id` values;
operators inspecting the DLQ must tolerate duplicate quarantine records and
use the origin topic, partition, and offset as their source identity. Sentinel
does not expose an administrative replay endpoint in this phase.

## Writer work and rebalance bounds

The writer keeps one batch of at most 5,000 source records and flushes at the
configured size or interval (defaults: 5,000 records / one second). All poison
records in an owned batch share one deadline, rather than each receiving a new
complete retry sequence. With attempts `A`, operation timeout `O`, and maximum
retry backoff `B`, the shared bounds are:

- Retry sequence `R = A*O + (A-1)*B`.
- Processing `P = 2*R + O`: one insert sequence, one aggregate poison stage,
  and one reserved commit operation.
- Group rebalance `max(15s, flush interval + P + shutdown timeout + 5s)`.

Defaults yield `R=70s`, `P=150s`, and a 176-second rebalance timeout. The shared
configuration helper rejects invalid, overflowing, or greater-than-five-minute
ownership budgets; it is also applied to directly constructed writers. The
bound does not multiply by the number of poison records.

The processing clock starts before classification. Valid rows are inserted
before any DLQ work. On aggregate poison expiry, no further poison publish
starts, and a separate live commit context uses the reserved operation budget
to acknowledge only already durable, contiguous partition prefixes. A blocked
partition never prevents an independently durable prefix from committing.
The worker then exits with an error and releases rebalance ownership. Restart
with the same group to process the remaining source offsets with a fresh batch
budget; DLQ origin tuples can appear again after ambiguous acknowledgements.

Only cancellation of the original run context transfers the current batch to
one fresh shutdown attempt. That attempt is capped by `shutdown_timeout` and
always releases ownership, including on expiry. Internal processing/poison
expiry does not start another shutdown attempt or reset the effects budget.
