---
phase: 05-observability-resilience-scale
plan: "04"
subsystem: bench
tags: [benchmark, generator, workload, capacity, preflight, open-loop]
requirements: [BENCH-01, BENCH-02]
dependency-graph:
  requires: []
  provides: [sentinel_generator.bench.model, sentinel_generator.bench.schedule, sentinel_generator.bench.preflight]
  affects: [plan 05-08 (open-loop sender consumes Schedule/Batch), plan 05-03 (report reads expected hits and preflight predictions from raw JSON), plan 05-18 (bench scenarios)]
tech-stack:
  added: []
  patterns: [lazy per-batch generation from sha256(seed:k), smooth weighted round-robin exact quotas, round-robin entity assignment for exact per-key maxima, Go-constant drift guard test]
key-files:
  created: [generator/src/sentinel_generator/bench/__init__.py, generator/src/sentinel_generator/bench/model.py, generator/src/sentinel_generator/bench/schedule.py, generator/src/sentinel_generator/bench/preflight.py, generator/tests/test_bench_model.py, generator/tests/test_bench_schedule.py, generator/tests/test_bench_preflight.py]
  modified: [generator/pyproject.toml]
decisions:
  - "batch_size is capped at 500 (send.MAX_BATCH_SIZE, which matches ingest config max_batch_size), not the plan's 1000, because ingest rejects anything larger"
  - "The ledger horizon H also counts rules that have attacks: an open AUTH attack window holds its partition's pin for 330 s even when there is no benign auth.failure"
  - "The preflight takes either BenchConfig or a CapacityModel, so the round-1 and port_pool 4096 must-fail fixtures can be expressed without loosening BenchConfig's port_pool <= 19 guard"
  - "Schedule and AttackPlan.build take T0, because attack windows must be bucket-aligned to absolute epoch time"
metrics:
  duration: ~10 min
  completed: 2026-11-14
---

# Phase 5 Plan 04: Bench workload model, schedule and preflight Summary

`sentinel_generator.bench` is a live-time, bounded-entity, open-loop workload of 1M or more events. Batch k is rebuilt lazily from `sha256(f"{seed}:{k}")` and its `occurred_at` is `T0 + k*B/R`. The schedule contains an exact attack plan of 30 entities per rule (120 expected alerts), each followed by a window-advance marker. Two fail-closed preflights guard it. The capacity preflight mirrors the detector's real window key and caps (drift-guarded against `reducer.go`/`bounds.go`). The benign-threshold check computes exact per-key maxima over the generated stream.

## Commits

- ef9ab58 `test(05-04)` RED, tests only (3 files, 613 insertions). RED evidence: `uv run --locked pytest tests/test_bench_model.py tests/test_bench_schedule.py tests/test_bench_preflight.py -q` exited 2 at collection with `ModuleNotFoundError: No module named 'sentinel_generator.bench'` (2026-11-14T00:36:31Z).
- fde6b2c `feat(05-04)` GREEN (bench package plus a 3-line pyproject S311 per-file ignore).

## Default config (BenchConfig)

seed and tenant are required. The defaults are:

- total_events 1,000,000, rate_eps 400, batch_size 200 (5,000 batches, 0.5 s apart, about 2,500 s)
- detectors 3, partitions 6, lateness_seconds 30 (another value needs `lateness_override=True`)
- benign_auth_failure True
- source_pool 200, principal_pool 200, destination_pool 2, port_pool 16 (at most 19)
- attacks_per_rule 30 (`require_p99=True` enforces 4 x attacks >= 100)

Bounds:

- total 1..5,000,000
- rate 1..20,000
- batch 1..500
- detectors 2..64, partitions from detectors up to 1,024
- pools 1..100,000 (destinations up to 4,096)
- attacks 0..1,000
- tenant must match `[a-z0-9][a-z0-9_-]{0,62}`

Benign mix, exact per 100 benign events (smooth weighted round-robin):

| kind | share |
|---|---|
| net.connection | 40 |
| net.request | 30 |
| policy.decision allow | 12 |
| policy.decision deny | 3 |
| auth.failure | 1 |
| auth.success | 14 |

With `benign_auth_failure=False`, auth.failure is 0 and auth.success is 15. Sources are assigned round-robin per kind, and auth.failure always uses `pools.principal_for(source)`.

Address ranges: benign sources are 198.51.100/24, then 203.0.113/24, then 192.0.2/24, then 2001:db8:0:b::/64. Destinations are 2001:db8:0:d::/64. Attack sources are 2001:db8:5e:a77a::/64 and the attack SCAN destination is 2001:db8:5e:a77b::1. All are RFC 5737 / RFC 3849 documentation ranges.

## Preflight predictions for the default (computed, not estimated)

`capacity_preflight(BenchConfig(...))` gives f_proc 0.5, R_proc 200 ev/s and H 330 s.

| check | predicted | limit (80% of cap) | % of cap |
|---|---|---|---|
| windows | 1,240 | 8,000 | 12.4% |
| ledger | 66,000 | 80,000 | 66.0% |
| bytes | 8,100,160 | 53,687,091 | 12.07% |

- windows_by_rule: AUTH 200, SCAN 400, FLOOD 200, DENY 200, plus 240 attack windows
- events_by_rule: 660, 7,200, 5,400, 900
- bytes breakdown: 4,224,000 ledger + 2,322,240 event IDs + 1,349,120 windows + 204,800 SCAN ports

`benign_threshold_check` on the default 1M schedule found these maxima: AUTH 6, SCAN 16 distinct ports, FLOOD 36, DENY 8. The thresholds are 10, 20, 300 and 30.

Other measured values:

- Schedule iteration over 1M events peaked at 0.36 MB under tracemalloc (the limit is 200 MB).
- The attack plan adds 10,920 events (10,800 threat events and 120 markers) inside the 1M total.

Must-fail fixtures, all tested:

- **(a) round-1 defaults:** AUTH 6,600, SCAN 14,400, FLOOD 2,000, DENY 1,800, so 24,800 benign windows (25,040 with the attack windows). Only `windows` fails; the ledger is 66,000 and passes.
- **(b) 600 ev/s:** the ledger is 99,000, so only `ledger` fails.
- **(c) port_pool 4,096 with destination_pool 64:** `bytes` fails (windows fails too, at 12,800 SCAN windows).
- **Bytes on its own:** port_pool 4,096 with destination_pool 2 fails only `bytes`. This proves the bytes check is independent.
- **Partition skew:** share 0.2 gives a ledger of 79,200, which passes; share 0.21 refuses.

## API

- `model.BenchConfig` (frozen, validated)
  - `.weights`, `.batch_count`, `.offset_ns(k)`, `.batch_events(k)`
- `model.EntityPools.from_config(cfg)`
  - `.sources`, `.principals`, `.destinations`, `.ports`, `.principal_for(source)`
- `model.build_bench_event(rng, pools, kind, occurred_at, tenant, ordinal) -> dict`
  - `kind` is one of `BENIGN_KINDS`
  - produces the same keys and types as `events.build_event`, reusing `build_event_id` and `_format_time` by import
- `model.AttackPlan.build(cfg, t0)`
  - `.entities: tuple[AttackEntity]`, each with rule_id, source_ip, principal, destination_ip, window_start_ms/end_ms, event_count, placements, marker_batch and marker_at_ms
  - `.attack_sources`
  - `.expected() -> {rule_id: count}`
- `schedule.Schedule(cfg, pools, plan, t0)`
  - `len()` (batches), iteration, `.batch(k) -> Batch`, `.total_events`, `.plan`, `.t0`
  - refuses a plan or pools built for a different config or T0
- `schedule.Batch(seq, offset_ns, events, attack_rules)`
- `preflight.predict_capacity(cfg | CapacityModel, partition_share_max=None) -> CapacityPrediction` (TypedDict, JSON-ready, includes `failed`)
- `preflight.capacity_preflight(...)` returns the same dict or raises `PreflightError(failed, predictions)`
- `preflight.CapacityModel`, `.from_config(cfg)`
- `preflight.benign_threshold_check(schedule) -> {rule_id: max}` or raises `PreflightError`
- Mirrored constants: MAX_WINDOWS, MAX_LEDGER_ENTRIES, MAX_STATE_BYTES, MAX_WINDOW_EVENTS, LATENESS_SECONDS and the *_ENTRY_BYTES values. `test_mirrored_detector_constants_match_the_go_source` parses reducer.go and checks the windowBytes formula in bounds.go.

## Verification (run for real, generator/)

| command | start | end | exit |
|---|---|---|---|
| `uv run --locked pytest tests/test_bench_model.py tests/test_bench_schedule.py tests/test_bench_preflight.py -q` (58 passed) | 00:41:18Z | 00:42:05Z | 0 |
| `uv run --locked pytest -q` (222 passed, full suite incl. 05-03's test_bench_report.py, which was passing at that moment, so no deselect was needed) | 00:42:11Z | 00:42:59Z | 0 |
| `uv run --locked ruff check .` | 00:42:59Z | 00:42:59Z | 0 |
| `uv run --locked ruff format --check .` | 00:42:59Z | 00:42:59Z | 0 |
| `uv run --locked mypy src tests` | 00:42:59Z | 00:43:00Z | 0 |
| `uv lock --check` (no dependency change) | 00:43:00Z | 00:43:00Z | 0 |

Not run: `make test && make lint`. The Makefile is Phase 4-owned and its targets go beyond the generator (Go and web). All generator-scoped gates above were run.

## Deviations from Plan

1. **[Rule 1 - Bug] Default bytes figure.** The plan says "about 7.8 MB". Its own components (4.22 + 2.32 + 1.35 + 0.20) add up to 8.09 MB. The test asserts the exact computed 8,100,160 bytes (7.72 MiB), which is 12.07% of the cap.
2. **[Rule 1 - Bug] batch_size bound.** The plan allowed up to 1000. Ingest config `max_batch_size` is capped at 500, so larger batches would be rejected. BenchConfig reuses `send.MAX_BATCH_SIZE` (500).
3. **[Rule 2 - Correctness] Ledger horizon.** The plan defines H over rules with benign traffic only. Open attack windows also pin the ledger, so H also counts rules with attacks. With no benign auth.failure and no attacks, H is 150 s; with AUTH attacks it is 330 s (tested). The defaults are unaffected (330 s).
4. **API shape.**
   - `AttackPlan.build(cfg, t0)` replaces `(cfg, schedule_length_s)`, and `Schedule(cfg, pools, plan, t0)` gains `t0`, because attack windows are aligned to absolute epoch buckets.
   - `build_bench_event` takes an extra `ordinal` for exact round-robin assignment.
   - `CapacityModel` was added so the must-fail fixtures (round-1 auth 5% with independent principals, port_pool 4096) can be checked without weakening BenchConfig's port_pool <= 19 guard.
   - `predict_capacity` is the non-raising variant.
5. **Config fields.** `lateness_override` is the "explicitly set" switch for lateness, and `require_p99` gates the 4 x attacks >= 100 rule so benign-only test configs are possible.

## Known limits

- The bytes formula follows the plan. It counts benign retained IDs and SCAN ports, plus 1,088 bytes per window including attack windows. It does not count the event IDs of open attack windows. The worst case, if every attack were open at once on one process, is 10,800 x 164, about 1.8 MB. That still fits within the 80% margin (the default sits at 12%).
- benign_threshold_check uses a sliding (t-W, t] window. This dominates the detector's bucket-aligned windows, so it is conservative.
- The tracemalloc 1M test takes about 36 s, which makes it the slowest generator test.

## Self-Check: PASSED

- All 7 created files and the modified pyproject.toml exist.
- Commits ef9ab58 and fde6b2c are present in `git log`.
- events.py, send.py and cli.py are unchanged.
