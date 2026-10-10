---
phase: 01-foundations-contracts
plan: 03
subsystem: generator
tags: [python, uv, ruff, mypy, pytest, determinism]
requires: [01-01]
provides:
  - uv-managed Python 3.13 generator package with lockfile and requirements export
  - deterministic build_event / build_event_id builders
affects: [phase-2 generator sending, benchmarks]
tech-stack:
  added: [pytest 9.1.1, ruff 0.17.0, mypy 2.4.0]
  patterns: [seed-derived ids, documentation-range IPs]
key-files:
  created:
    - generator/pyproject.toml
    - generator/uv.lock
    - generator/requirements.txt
    - generator/.python-version
    - generator/README.md
    - generator/src/sentinel_generator/__init__.py
    - generator/src/sentinel_generator/py.typed
    - generator/src/sentinel_generator/events.py
    - generator/tests/__init__.py
    - generator/tests/test_events.py
key-decisions:
  - "S311 ignored for events.py and tests via per-file-ignores with justification (seeded RNG is intentionally non-cryptographic)"
requirements-completed: [FND-02, FND-06]
duration: ~10min
completed: 2026-10-11
---

# Phase 1 Plan 03: Generator scaffold Summary

uv-managed Python 3.13 generator with pinned pytest/ruff/mypy and a seeded event builder whose output (including UUIDv7-shaped event_ids) is byte-reproducible and uses documentation IP ranges only.

## Tasks
1. Scaffold uv project, lock, requirements export: bf042ad
2. TDD event builder: RED 6 failing tests (test commit), GREEN implementation (feat commit)

## Deviations from Plan
**[Rule 3] S311 ignore extended to tests/**: tests construct `random.Random(seed)` directly, which trips S311. Added to the `tests/**` per-file-ignores with a comment instead of per-line noqa. No other deviations.

## Verification
`uv lock --check`, `uv sync --locked`, ruff check/format, mypy src tests, pytest (6 passed) all green. requirements.txt has no `-e` line (it has no runtime deps).

## Known Stubs
None. No sending logic (Phase 2 by design).

## Self-Check: PASSED
