# 0005. Redis failure policy

## Status

Accepted, 2026-10-10. Decisions: D-27, D-05.

## Context

Redis backs rate limiting for ingest. When Redis is unavailable, the service must choose between protecting itself and staying available.

## Decision

- Default is fail-closed: ingest returns 503 when the rate limiter cannot reach Redis.
- `SENTINEL_RATELIMIT.FAIL_MODE=open` opts in to fail-open. In that mode a metric is incremented and a warning is logged on each bypass.
- Redis unreachable at startup is fatal; the process exits instead of logging and continuing.

## Consequences

- A Redis outage degrades ingest to 503 by default, which clients already handle by retrying (ADR 0003).
- Operators who prefer availability must opt in knowingly, and the bypass is observable.
- Misconfiguration is caught at startup rather than at first request.
