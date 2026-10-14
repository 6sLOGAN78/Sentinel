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
