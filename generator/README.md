# sentinel-generator

Seeded synthetic security event generator for Sentinel.

## Seed contract

Every value, including `event_id`, derives from the seed, event index, and UTC run start. The
same seed plus `--run-start` produces byte-identical events. Without `--run-start`, each run
uses its current UTC start, so its timestamp-derived IDs are fresh. Retries always reuse the
original serialized batch bytes.

Generated IP addresses use reserved documentation ranges (RFC 5737 and RFC 3849), and
principals are pseudonymous (`user-NNNN`). The newest generated event predates the run start,
which keeps a normal send inside the ingestion time policy.

## Setup

Install the locked application and development dependencies:

```bash
uv sync --locked
```

The command reads its scoped producer key from `SENTINEL_API_KEY` first. It otherwise reads
`.local/keys/<tenant>.key`, or the file selected by `--key-file`. Keep key files private:

```bash
install -d -m 700 .local/keys
install -m 600 /path/to/demo.key .local/keys/demo.key
```

Credentials are never accepted as command-line values and are not written to output, errors,
or manifests.

## Send benign events

Send 1,000 deterministic benign events in batches of 100:

```bash
uv run --locked sentinel-generator send \
  --seed 42 \
  --count 1000 \
  --batch-size 100 \
  --url http://127.0.0.1:8080 \
  --tenant demo
```

Use an explicit key file and run start when reproducing the exact same event bytes:

```bash
uv run --locked sentinel-generator send \
  --seed 42 \
  --count 1000 \
  --batch-size 100 \
  --url http://127.0.0.1:8080 \
  --tenant demo \
  --key-file .local/keys/demo.key \
  --run-start 2026-10-13T12:00:00.000Z \
  --output .local/repro-send.json
```

A successful command prints aggregate `sent`, `accepted`, `rejected`, and `retries` totals.
It atomically writes a mode `0600` JSON manifest to `.local/last-send.json` by default. The
manifest also records the UTC-millisecond run start and every intended event ID for later
verification. HTTP `202` means Kafka acknowledged the batch; it does not claim ClickHouse
persistence. Terminal HTTP, response-validation, and exhausted-retry failures return a
nonzero exit status without writing a success manifest.

## Verified local seed

From the repository root, the supported end-to-end path is:

```bash
make seed
```

It starts the Phase 2 stack once, idempotently ensures private `demo` and
`demo2` credentials, sends 1,000 events with seed 42, and then independently
checks the exact manifest IDs in ClickHouse and through the tenant-scoped query
API. Historical tenant rows cannot satisfy the gate. Storage polling is capped
at 60 seconds, and any rejected batch, missing ID, API mismatch, or subprocess
failure makes the command exit nonzero.

Keys and manifests stay under the ignored `.local/` directory at modes `0600`
and `0700`; credentials are passed by file or child environment, never as
argument values or log fields. Ingest caches successful key lookups for at
most 30 seconds, so a revoked local key may remain accepted for that bounded
interval. The default Redis rate-limit policy is fail-closed.

## Commands

```bash
uv run --locked sentinel-generator send --help
uv run --locked ruff check .
uv run --locked mypy src tests
uv run --locked pytest
```
