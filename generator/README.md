# sentinel-generator

Seeded synthetic security event generator for Sentinel.

Sending events to the ingest API arrives in Phase 2. Phase 1 provides only the deterministic
event builder in `sentinel_generator.events`.

## Seed contract

Every value, including `event_id`, is derived from the supplied `random.Random` and the event
index. The same seed always yields byte-identical events; a different seed yields different
events. IP addresses come only from reserved documentation ranges (RFC 5737, RFC 3849) and
principals are pseudonymous (`user-NNNN`).

## Commands

```bash
uv sync --locked
uv run ruff check
uv run mypy src tests
uv run pytest
```
