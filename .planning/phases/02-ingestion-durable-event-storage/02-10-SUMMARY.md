---
phase: 02-ingestion-durable-event-storage
plan: "10"
subsystem: generator
tags: [python, httpx, retries, deterministic-events, manifests]
requires:
  - phase: 01-foundations-contracts
    provides: Seeded synthetic event builder, Python packaging, and locked generator toolchain
provides:
  - Deterministic past-anchored benign event runs through an installed send command
  - Bounded identical-byte HTTP retries with Retry-After and exact 202 validation
  - Private atomic run manifests containing every intended event ID
affects: [02-02, 02-13, phase-3-detection]
tech-stack:
  added: [httpx-0.28.1]
  patterns: [serialize-once retries, injected clocks and sleepers, atomic private manifests]
key-files:
  created:
    - generator/src/sentinel_generator/cli.py
    - generator/src/sentinel_generator/send.py
    - generator/tests/test_send.py
  modified:
    - generator/pyproject.toml
    - generator/uv.lock
    - generator/requirements.txt
    - generator/README.md
key-decisions:
  - "Anchor generated events at run_start minus count seconds so the newest event is one second before the injected UTC-millisecond run start."
  - "Limit each batch to five attempts, 30-second fallback delays, and a 120-second total wait budget while honoring a positive Retry-After in full."
  - "Accept a 202 response only when its counts and ordered event IDs exactly match the serialized batch."
patterns-established:
  - "Retry identity: serialize each compact JSON batch once and reuse the same bytes for every attempt."
  - "Credential boundary: load producer keys from SENTINEL_API_KEY or a private no-follow file and never echo remote bodies."
requirements-completed: [GEN-01]
duration: 15min
completed: 2026-10-13
---

# Phase 2 Plan 10: Deterministic Generator Send Workflow Summary

**Seeded benign-event delivery with identical-byte HTTP retries, exact acknowledgements, and private machine-readable run manifests.**

## Performance

- **Started:** 2026-10-13T13:41:43Z
- **Completed:** 2026-10-13T13:56:16Z
- **Duration:** 15 min
- **Tasks:** 3
- **Files modified:** 7

## Accomplishments

- Added the installed `sentinel-generator send` command with deterministic seed plus run-start generation, documentation-range IPs, skew-safe timestamps, and bounded argument validation.
- Added HTTPX batch delivery with connect and operation timeouts, serialize-once request bodies, five bounded attempts, integer and HTTP-date `Retry-After` support, capped jittered fallback, and a finite total wait budget.
- Enforced exact ordered `202` acknowledgement validation and sanitized failures for fatal HTTP responses, malformed success envelopes, transport errors, and exhausted retries.
- Loaded producer credentials only from the environment or a private no-follow file and wrote successful run manifests atomically at mode `0600` with every intended event ID.
- Documented locked setup, private key preparation, reproducible run starts, aggregate totals, manifest semantics, and the distinction between Kafka acknowledgement and ClickHouse persistence.

## Task Commits

1. **Task 1: Write deterministic generation and retry behavior tests** — `c7bd577` (test)
2. **Task 2: Implement bounded send command and structured run record** — `2aa1327` (feat)
3. **Task 3: Document CLI and validate installed entry point** — `e87466b` (docs)

## Verification

- `cd generator && uv lock --check` passed with 20 resolved packages and `httpx==0.28.1` pinned.
- `cd generator && uv run --locked pytest` passed: 37 tests.
- `cd generator && uv run --locked ruff check .` passed with zero issues.
- `cd generator && uv run --locked ruff format --check .` passed: 8 files already formatted.
- `cd generator && uv run --locked mypy src tests` passed with no issues in 7 source files.
- `cd generator && uv run --locked sentinel-generator send --help` passed and exposed `send`, `--key-file`, and `--run-start` without a secret-bearing argument.
- Retry tests observed identical request bytes across 429, 503, and transport-failure attempts; no live endpoint result was claimed.

## Decisions Made

- The default retry budget is 120 cumulative wait seconds across the run. A positive server delay that exceeds the remaining budget terminates the run instead of retrying early.
- A successful envelope must report zero rejections, the exact accepted count, a nonempty request ID, and event IDs in the same order as the batch.
- Default run starts are truncated to UTC milliseconds. An explicit run start plus seed reproduces bytes; an omitted run start creates fresh timestamp-derived IDs.
- Private key files must be regular, owner-only files and are opened with no-follow semantics where supported. Environment credentials take precedence.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Test correctness] Accounted for HTTP-date second precision**
- **Found during:** Task 2 GREEN verification
- **Issue:** The first HTTP-date retry test expected an exact eight-second delay from a clock with 123 milliseconds, although HTTP-date values have only whole-second precision.
- **Fix:** Anchored that injected test clock to a whole second so the expected full delay is exactly eight seconds.
- **Files modified:** `generator/tests/test_send.py`
- **Verification:** The retry-delay assertion and the complete 37-test suite pass.
- **Committed in:** `2aa1327`

---

**Total deviations:** 1 auto-fixed test-correctness issue. **Impact:** The fix aligns the test with RFC HTTP-date precision and does not change product scope.

## Issues Encountered

None. The initial RED suite failed on the intentionally absent HTTPX dependency and sender modules, then passed after implementation.

## Known Stubs

None. The client, retry policy, credential loading, response validation, manifest writer, installed entry point, and documentation are complete for this producer slice.

## User Setup Required

None. The README documents local private-key setup; Plan 02-13 will exercise the command against the assembled service stack.

## Next Phase Readiness

The generator can drive the authenticated ingest route once Plan 02-02 lands. Plan 02-13 can run the documented 1,000-event seed flow and compare the manifest IDs with durable storage without changing the producer contract.

## Self-Check: PASSED

All seven owned implementation artifacts, this summary, and the three task commits exist. Locked tests, lint, formatting, strict typing, entry-point help, and whitespace checks passed. Preexisting `.planning/config.json` and `.serena/` changes remain unstaged.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-13*
