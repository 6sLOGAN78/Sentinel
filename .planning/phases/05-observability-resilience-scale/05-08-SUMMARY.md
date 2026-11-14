---
phase: 05-observability-resilience-scale
plan: "08"
subsystem: bench
tags: [benchmark, open-loop, coordinated-omission, sse, raw-format, asyncio]
requirements: [BENCH-01, BENCH-02]
dependency-graph:
  requires: [05-03 (RAW_FORMAT v1, report.id_digest), 05-04 (Schedule/Batch, AttackPlan)]
  provides: [sentinel_generator.bench.sender, sentinel_generator.bench.sse, sentinel_generator.bench.rawlog]
  affects: [05-18 (bench run.py assembles raw JSON from client_section/sse_section)]
tech-stack:
  added: []
  patterns: [open-loop asyncio task per batch with semaphore cap, identical-byte bounded retries, deterministic VirtualClock test driver, header-only bearer SSE with Last-Event-ID]
key-files:
  created:
    - generator/src/sentinel_generator/bench/sender.py
    - generator/src/sentinel_generator/bench/sse.py
    - generator/src/sentinel_generator/bench/rawlog.py
    - generator/tests/test_bench_sender.py
    - generator/tests/test_bench_sse.py
    - generator/tests/test_bench_rawlog.py
    - generator/tests/virtual_clock.py
  modified: []
decisions:
  - "Retry budget is enforced two ways: the summed waits AND the next attempt's start relative to intended_ns (slip included) must stay within retry_budget_s. request_timeout_s is capped at 10 s, so the last attempt ends before intended + 30 s (detector lateness)"
  - "Retry delay = max(Retry-After, jittered fallback); fallback reuses send._fallback_delay capped at 4 s"
  - "Only 429/503/transport errors are retried. Other 4xx count as rejected_4xx. Other 5xx and invalid 202 envelopes count as unaccepted_final. A transport-only failure is recorded with status 0"
  - "The sender clock is Unix-epoch ns, so intended_ns = schedule.t0 + offset_ns equals each batch's occurred_at. epoch_clock() anchors once and then advances monotonically"
  - "SSE arrivals map to the trigger time through (rule_id, normalized source IP) plus an exact window_start match. Unmatched alerts are returned by unmatched_alert_ids for limitations, never silently dropped"
metrics:
  duration: ~10 min
  completed: 2026-11-14
  tasks: 2
  files: 7
---

# Phase 5 Plan 08: Open-loop sender, SSE subscriber and raw client section Summary

The harness can now turn a 05-04 `Schedule` into real open-loop HTTP traffic across up to 8 ingest replicas (round robin, no load balancer) and capture live alert arrivals from the SSE stream. Both are recorded in the exact RAW_FORMAT v1 `client` and `sse` sections, and all of it is tested offline using httpx.MockTransport and a deterministic virtual clock.

## Commits

| Task | Commit | Content |
|---|---|---|
| 1 RED | d0bb0e9 `test(05-08)` | Tests only: 3 test files plus the `tests/virtual_clock.py` helper. 30 `def test_` functions, 40 collected cases. |
| 2 GREEN | dd3f9df `feat(05-08)` | sender.py, sse.py, rawlog.py |

RED evidence: `uv run --locked pytest tests/test_bench_sender.py tests/test_bench_sse.py tests/test_bench_rawlog.py -q` ran from 2026-11-14T00:54:27Z to 00:54:28Z and exited 2 at collection with `ModuleNotFoundError: No module named 'sentinel_generator.bench.rawlog'` (3 collection errors). The failure was causal and the commit contains only generator/tests.

## Public API

**sender.py**
- `SenderConfig(urls, max_in_flight=64, retry_budget_s=15.0, request_timeout_s=10.0, max_attempts=5)`, frozen and validated:
  - 1 to 8 http(s) base URLs, with no query or fragment
  - max_in_flight between 1 and 256
  - 0 < budget < 20
  - 0 < timeout ≤ 10
  - attempts between 1 and 10
- `async run_open_loop(schedule, cfg, api_key, *, clock, sleeper=asyncio.sleep, client_factory=None, jitter=None, cpu_clock=time.process_time) -> ClientLog`
- `RequestRecord`: a NamedTuple whose fields are exactly the RAW `requests[]` keys.
- `ClientLog` has these fields: `requests`, `accepted_ids`, `event_sizes` (a Counter histogram), `harness_cpu_percent_max` and `invalid_envelopes`. Its repr hides the bulky fields, and it holds no credentials.
- `BatchSource` is a Protocol with `t0`, `total_events` and `__iter__`, which the real `Schedule` satisfies.
- Helpers: `serialized_event_size(event)` and `epoch_clock()`.
- It reuses `send._serialize_batch`, `_validate_accepted`, `_retry_after_seconds`, `_fallback_delay`, `RetryPolicy` and `INGEST_PATH` by import.

**sse.py**
- `async subscribe_alerts(base_url, token_provider, stop, *, clock, sleeper, client_factory, jitter, stats=None) -> list[Arrival]`
- `TokenProvider = Callable[[], Awaitable[tuple[str, int]]]`, returning `(token, expires_at_ns)`
- `Arrival(alert_id, rule_id, entity_key, window_start_ms, received_ns, seq)`
- `SubscriberStats(malformed, reconnects, fatal_status)`

**rawlog.py**
- `id_digest(ids)` produces 16 hex characters and equals `report.id_digest` (this is tested).
- `size_summary(counter)` gives exact nearest-rank min, p50, p95 and max.
- `client_section(log, schedule)` returns exactly the RAW client keys. `generated` comes from `schedule.total_events`, not from what was sent.
- `sse_section(arrivals, plan)` returns `{"arrivals": [...]}`. `trigger_intended_ns` is `t0_ms·1e6 + offset_ns(last placement batch)`.
- `unmatched_alert_ids(arrivals, plan)` lists alerts that matched no planned attack.

## Default limits

- **In-flight cap:** 64 requests. The backlog of scheduled-but-unfinished tasks is capped at 4 × max_in_flight (256 by default). If that cap is hit, the scheduler waits; intended_ns keeps the schedule time, so the wait shows up as slip and latency instead of being hidden.
- **Retry budget:** 15 s per batch (validated below 20 s), plus a request timeout of at most 10 s. The worst-case final response therefore arrives before intended + 25 s, inside the 30 s lateness. There are at most 5 attempts and the fallback backoff is capped at 4 s.
- **SSE:** tokens are re-minted 60 s before expiry, both by a timer during an open stream and before a reconnect. Backoff is 0.5 to 5 s with jitter and resets after a connection that delivered frames. The default client uses a 10 s connect timeout and a 45 s read timeout (heartbeats arrive every 15 s). A 400, 403 or 404 ends the subscription and sets `fatal_status`, instead of looping on a rejected cursor. A 401 forces a token re-mint.
- **CPU:** harness CPU% is sampled from `time.process_time()` deltas over windows of at least 10 s, plus a final window.

## Verification (run for real, cwd generator/)

| Command | Start | End | Exit |
|---|---|---|---|
| `uv run --locked pytest tests/test_bench_sender.py tests/test_bench_sse.py tests/test_bench_rawlog.py -q` (40 passed) | 00:57Z | 00:57Z | 0 |
| `uv run --locked pytest -q` (262 passed) | 00:58:08Z | 00:59:00Z | 0 |
| `uv run --locked ruff check .` | 00:59:00Z | 00:59:00Z | 0 |
| `uv run --locked ruff format --check .` | 00:59:00Z | 00:59:00Z | 0 |
| `uv run --locked mypy src tests` (strict, 26 files) | 00:59:00Z | 00:59:00Z | 0 |
| `uv lock --check` | 00:59:00Z | 00:59:00Z | 0 |

Acceptance checks:

- The sender imports its helpers from `sentinel_generator.send` (line 27).
- No `params=` or `?token` appears in sse.py (grep exit 1).
- `git diff --stat d0bb0e9~1 HEAD -- generator/pyproject.toml generator/uv.lock` is empty, so no dependency was added. Other plans' commits landed in between, so the check runs from the RED commit's parent rather than `HEAD~2`.
- There is no pytest-asyncio usage.

Not run: `make test && make lint`. The Makefile is owned by Phase 4 and also covers Go and web code that concurrent plans are editing. The generator-scoped equivalents above were run.

## Deviations from Plan

1. **[Rule 3 - Blocking] Test helper file.** `generator/tests/virtual_clock.py` was added. It is not in `files_modified`, but both the sender and SSE tests need the same deterministic virtual-time driver, which lets them assert exact `*_ns` values without pytest-asyncio or wall-clock sleeps.
2. **[Rule 2 - Correctness] Budget measured against intended time.** The plan bounds the summed retry waits. The implementation also refuses a retry whose start would fall beyond intended + budget, and it caps `request_timeout_s` at 10 s. Together these keep retried attack events inside the 30 s lateness even when the batch slipped.
3. **[Rule 2 - Correctness] Fatal SSE statuses and token guard.** A 400, 403 or 404 stops the subscriber, because a rejected Last-Event-ID would otherwise loop forever. A provider that returns two tokens in a row already inside the 60 s margin raises `ValueError`, because it would otherwise spin.
4. **API shape.**
   - `subscribe_alerts` takes injectable `sleeper`, `client_factory`, `jitter` and an optional `stats` out-object, so malformed-frame counts reach the limitations while the return type stays `list[Arrival]`.
   - `Arrival` carries `entity_key`, `window_start_ms` and `seq`, which the trigger mapping needs.
   - `run_open_loop` takes `jitter` and `cpu_clock` for testability.
   - `unmatched_alert_ids` was added so that unmatched arrivals are visible.
5. **Invalid 202 envelope.** It is not retried. It is counted in `invalid_envelopes` and the batch's events count as `unaccepted_final`, so the accounting still closes.

## Known limits

- `accepted_ids` is an exact `set[str]`, which takes about 100 MB at 1M events. That is the price of an exact `accepted_unique`; the size histogram is a bounded Counter.
- Status 0 means no HTTP response (a transport error or timeout). The report treats it as unaccepted.
- The double-short-token refusal path in sse.py has no dedicated test, because tests were fixed at the RED commit. Every other branch named in the plan is tested.
- The SSE trigger mapping assumes the detector's `entity_key` starts with the source IP (true for all four rules, per `detection/evidence.go`). 05-18 should confirm this on a live stack.
- No real stack was exercised, by design. 05-18 runs it live.

## Threat Flags

None. The surface matches the plan's threat model:
- T-05-08-01: header-only auth, with tests asserting no key or token in URLs, the client section or reprs.
- T-05-08-02: semaphore, task backlog cap and lazy schedule.
- T-05-08-03: identical bytes on retry and set-deduped accepted IDs.
- T-05-08-04: malformed frames are counted and never fatal.

## Self-Check: PASSED

- All 7 created files exist.
- Commits d0bb0e9 and dd3f9df are in `git log`.
- backend/, the Makefile, docs and the Phase 4 files are untouched. STATE.md and ROADMAP.md were not updated (orchestrator instruction).
