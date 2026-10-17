---
phase: 03-detection-alert-pipeline
plan: "06"
subsystem: testing
tags: [python, generator, scenarios, go, contracts, deterministic-identity]
requires:
  - phase: 02-ingestion-durable-storage
    provides: Authenticated bounded sender, private manifests and event contracts
  - phase: 01-foundations-contracts
    provides: Canonical length-prefixed alert identity and strict wire contracts
provides:
  - Nine shared scenario cases with exact source events and canonical alert bytes
  - Pure seeded scenario builder and eight explicit CLI choices
  - Private dry-run manifests and ready-batch delivery from explicit caught-up schedules
affects: [03-03, 03-04, 03-05, 03-13, 03-19, 03-21]
tech-stack:
  added: []
  patterns: [static cross-language fixtures, immutable expected bytes, explicit live schedule admission]
key-files:
  created:
    - testdata/detection/scenarios.json
    - backend/internal/contracts/scenario_identity_test.go
    - generator/tests/test_scenarios.py
    - generator/src/sentinel_generator/scenarios.py
  modified:
    - generator/src/sentinel_generator/cli.py
key-decisions:
  - "Live scenarios require a private caught-up schedule; future batches refuse until plan 03-21 supplies legal clock waiting."
  - "All-mode drains share the latest relevant window end plus 30 seconds, preventing partition-watermark regression."
patterns-established:
  - "Expected alerts are frozen UTF-8 bytes in Go Alert field order, without detected_at."
  - "Each actual scenario batch invokes the existing sender with its unchanged five-attempt/120-second budget."
requirements-completed: [GEN-02, DET-09]
duration: "~10 min"
completed: 2026-10-17
---

# Phase 3 Plan 6: Deterministic Scenarios and Identity Summary

Nine shared cases fix exact event order, first-threshold alert bytes and identities; the CLI builds eight safe scenarios and admits live delivery only with an explicit caught-up schedule.

## Performance

- **Duration:** approximately 10 minutes through implementation completion; dispatch/start was not reliably captured.
- **First recorded execution timestamp:** 2026-10-17T06:24:56Z, after initial context and fixture work.
- **Implementation completed:** 2026-10-17T06:30:11Z.
- **Tasks:** 2.
- **Files:** 5 application/test/fixture files.
- Test durations below are actual tool output, independent of the approximate overall duration.

## Accomplishments

- Static JSON fixes 2,327 ordered source-event entries across the four individual rule scenarios, explicit benign traffic, boundary and lateness cases, combined all, and a second tenant.
- Isolated entities exercise 9/10/11 AUTH failures, 19/20/21 SCAN ports, 299/300/301 requests and 29/30/31 denies. Duplicate IDs, repeated SCAN ports, IPv6, a pipe/Unicode principal, half-open straddling and equality at the watermark have exact expectations.
- The pure builder reuses existing seeded event/UUID policy. Expected alerts are immutable bytes with exact windows, first-crossing counts and first-20 evidence. Each entity has one possible crossing, so these cases do not substitute candidate throttling for the selected cross-window cooldown.
- Explicit dry-runs require a historical run-start and write private exact expected manifests without credentials or network access.
- Live mode refuses arbitrary run-start/no schedule. Its private schema-1 schedule binds scenario/seed/tenant/base, six caught-up partition snapshots, an explicit empty/nonempty maximum, ordered batch IDs, send-not-before and a maximum 15-minute scheduling deadline. Ready batches use the unchanged authenticated sender. Future batches refuse before injection; plan 03-21 owns snapshot acquisition and legal clock waits.
- The default non-scenario generator, key-reader, private manifest writer and sender retry code remain intact.

## Task Commits

1. **Task 1: Shared exact scenario and identity expectations** — `466a353` (`test`).
2. **Task 2 RED: Loadable builder declaration and CLI/schedule assertions** — `2a0c384` (`test`).
3. **Task 2 GREEN: Pure builder, private manifests and safe scheduled sender wiring** — `a1e4d7d` (`feat`).

## Verification and TDD Evidence

All commands ran from the repository root unless a working directory is specified. Python unit commands had an outer 60-second timeout; CLI subprocesses have a 10-second timeout.

| Gate | Command | Actual result |
|------|---------|---------------|
| Task 1 RED | `cd generator && timeout 60s uv run --locked pytest tests/test_scenarios.py -q` | Exit 1: 1 failed, 1 passed in 0.11s; installed CLI exits 0, then missing `--scenario` assertion fails |
| Independent fixture identity, Task 1 | `scripts/go-test-nonempty.sh './internal/contracts/...' '^TestScenarioIdentityFixtures$' -timeout 60s` | Exit 0, 0.111s; all nine cases decode and canonical bytes/Go DedupeKey agree |
| Task 2 RED | `cd generator && timeout 60s uv run --locked pytest tests/test_scenarios.py -q` | Exit 1: 37 failed, 1 passed in 0.38s; declaration imports and returns empty values, so behavior assertions fail |
| Drain regression RED | `cd generator && timeout 60s uv run --locked pytest tests/test_scenarios.py -k 'seed_timebase_safety_and_markers' -q --tb=short` | Exit 1: 1 failed, 7 passed, 30 deselected in 0.15s; all-mode marker times regress |
| Final scenario GREEN | `cd generator && timeout 60s uv run --locked pytest tests/test_scenarios.py -q` | Exit 0: 45 passed in 0.49s |
| Final scenario/sender GREEN | `cd generator && timeout 60s uv run --locked pytest tests/test_scenarios.py tests/test_send.py -q` | Exit 0: 90 passed in 0.55s |
| Full generator unit suite | `cd generator && timeout 60s uv run --locked pytest -q` | Exit 0: 99 passed in 0.58s |
| Separate final fixture selector | `scripts/go-test-nonempty.sh './internal/contracts/...' '^TestScenarioIdentityFixtures$' -timeout 60s` | Exit 0: 0.079s, nine nonempty cases |
| Separate unchanged identity selectors | `scripts/go-test-nonempty.sh './internal/contracts/...' '^TestDedupeKey' -timeout 60s` | Exit 0: 0.008s, all six test families and original goldens pass |
| Full contracts unit suite | `cd backend && timeout 60s go test ./internal/contracts/... -count=1 -timeout 60s` | Exit 0: 0.098s |
| Go vet/format | `cd backend && timeout 60s go vet ./internal/contracts/... && gofmt -d internal/contracts/scenario_identity_test.go` | Exit 0; no format diff |
| Go lint | `cd backend && timeout 60s go tool -modfile=tools.mod golangci-lint run ./internal/contracts/...` | Exit 0: 0 issues |
| Python lint/format/type checks | `cd generator && uv run --locked ruff check src tests && uv run --locked ruff format --check src tests && timeout 60s uv run --locked mypy` | Exit 0: all checks pass, 9 files formatted, no type issues |
| Patch hygiene | `git diff --check` | Exit 0 |

Mocked authenticated delivery exercises every CLI scenario, identical retry bodies and a separate full 120-second retry allowance for each actual batch. It does not prove real ingest, detector, Kafka, ClickHouse, PostgreSQL or preserved-volume scheduling behavior.

## Files Created/Modified

- `testdata/detection/scenarios.json` — static complete inputs, rule definitions, canonical immutable alert bytes and source acceptance/drop/duplicate/marker expectations.
- `backend/internal/contracts/scenario_identity_test.go` — strict input/alert decoding and independent unchanged Go identity/byte verification; no detector import.
- `generator/tests/test_scenarios.py` — executable CLI RED, exact fixture comparison, safe deterministic inputs, all-choice dry/live mocked delivery and refusal tests.
- `generator/src/sentinel_generator/scenarios.py` — pure bounded scenario generation and immutable expected alerts.
- `generator/src/sentinel_generator/cli.py` — scenario flags, private schedule admission and existing sender/manifest integration.

## Decisions Made

- Keep fixed historical timebases confined to pure fixtures and explicit dry-runs. A supplied caught-up schedule is mandatory for live delivery.
- Use a common latest-window-end-plus-30-second drain time across all touched keys. All threat events precede all markers.
- The summary's requirements-completed list records this plan's GEN-02/DET-09 contribution. Overall requirement checkboxes remain pending real detector and preserved-state acceptance in later plans.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Prevent all-mode drain watermark regression**
- **Found during:** Task 2 ordering review and added regression.
- **Issue:** The initial independent fixture emitted AUTH drains at 330s before SCAN drains at 90s. Keys sharing a partition could classify the latter as late.
- **Fix:** All drains use the maximum relevant window end plus 30s. Corrected only new-fixture marker timestamps/UUIDs and corresponding source-ID expectations; expected alert IDs and payloads are unchanged.
- **Files modified:** `scenarios.py`, `test_scenarios.py`, `scenarios.json`.
- **Verification:** Targeted assertion RED above, then full final suites and independent Go fixture validation.
- **Commit:** `a1e4d7d`.

Total deviations: one scoped bug fix. No schema, alert encoding, dependency, architecture or phase-scope change.

## Issues Encountered

Routine new-code lint/import formatting and test type narrowing were corrected before final verification. No authentication gate or package installation occurred. User-owned `.planning/config.json` and `.serena/` remain unstaged and untouched.

After the summary commit, serialized SDK tracking counted 37/56 plans correctly but a later resync wrote 33% into frontmatter and the metric handler appended the plan row to the By Phase table. The closeout explicitly reconciled frontmatter/body to 66% and moved the row into Per Plan. Phase 3 remains 2/21, In Progress; requirements checkboxes were not advanced.

## Known Stubs

None preventing this plan's goal. Empty snapshots/batches, null deadline and zero sent/accepted counts in dry-run manifests explicitly mean no live run occurred. Future-batch refusal is the required boundary to plan 03-21, not an implemented clock-wait scheduler.

## Security Review

The existing no-follow private key/manifest routines remain unchanged and are reused for schedules/artifacts. Schedules require private regular files, bounded input, exact run identity, all six caught-up snapshots, base after processed maxima, matching batch IDs, legal event timestamps and a finite deadline. Counts and batch sizes retain existing ceilings; synthetic IPs/principals are reserved/pseudonymous. Logs contain aggregate counts, never credentials or payloads. No new endpoint, authentication scheme, database schema or unplanned trust boundary was introduced. This is slice evidence, not a phase-wide security certification.

## User Setup Required

None for pure fixtures/dry-runs. Live operator snapshot and clock-wait orchestration remains plan 03-21.

## Next Phase Readiness

Shared fixtures and the pure builder are ready for rule/evidence slices 03-03/04/05. Real rule-to-store reconciliation is 03-13; durable cross-window suppression is 03-19; source snapshots, legal wait scheduling and preserved-state live acceptance are 03-21. The Go contract tests validate fixed identity and bytes independently, not actual detector outcomes. Phase 3 and overall GEN-02/DET-09 remain incomplete.

## Self-Check: PASSED

All five owned application/test/fixture files and this summary exist. Commits 466a353, 2a0c384 and a1e4d7d exist. Final acceptance commands passed as recorded above; original DedupeKey implementation/goldens and sender are unchanged from 3fed31a. No tracked-file deletion occurred in the task commits.
