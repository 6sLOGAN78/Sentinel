---
phase: 02-ingestion-durable-event-storage
plan: "13"
subsystem: acceptance-and-ci
tags: [seed, clickhouse, tenant-isolation, ci, integration, docker]

requires:
  - phase: 02-10
    provides: deterministic generator send workflow and private run manifest
  - phase: 02-12
    provides: healthy local ingest, event-writer, query-api, and infrastructure stack
provides:
  - One-command seed workflow proving all 1000 current-run events reached tenant-scoped durable storage
  - CI guards for banned Go modules and independently nonempty Phase 2 integration groups
  - Complete local validation evidence for every Phase 2 task binding
affects: [phase-verification, ci, local-development, phase-03]

tech-stack:
  added: []
  patterns: [private idempotent local keys, explicit current-run ID verification, separately guarded integration groups, fail-closed acceptance]

key-files:
  created:
    - scripts/seed.py
    - scripts/test_seed.py
  modified:
    - Makefile
    - .github/workflows/ci.yml
    - README.md
    - generator/README.md
    - .planning/phases/02-ingestion-durable-event-storage/02-VALIDATION.md

key-decisions:
  - "Verify the exact manifest ID set under tenant and run-start bounds, so historical rows cannot satisfy a seed run."
  - "Provision demo credentials idempotently into private local files and pass secrets only through child environment, never argv or logs."
  - "Guard each integration behavior family independently in CI and preserve go-list failures before banned-module matching."

patterns-established:
  - "End-to-end acceptance emits only sanitized aggregate totals and fails closed on command, manifest, count, membership, API, or isolation mismatch."
  - "CI broad suites are supplemented by nonempty behavioral groups and exact causal-test source guards."

requirements-completed: [GEN-03, GEN-01, STOR-03, ING-01, ING-02, ING-03, ING-04, ING-05, ING-06, ING-07, STOR-01, STOR-02, STOR-04, OBS-01]

duration: 22 min
completed: 2026-10-16
---

# Phase 02 Plan 13: Exact Seed Acceptance and Phase Gate Summary

**A fail-closed seed command now proves every one of 1,000 current-run events traverses the real API into tenant-scoped durable storage, while CI protects the complete behavioral surface.**

## Performance

- **Duration:** 22 min
- **Started:** 2026-10-16T08:00:00Z
- **Completed:** 2026-10-16T08:22:29Z
- **Tasks:** 3
- **Files modified:** 7

## Accomplishments

- Added `make seed`, which idempotently ensures private demo credentials, sends a deterministic 1,000-event run, verifies the exact intended IDs in ClickHouse, spot-checks the query API, and proves cross-tenant reads return 404.
- Added 10 orchestration tests covering command failure, key reuse/repair, private modes, malformed or missing manifests, bounded shortfall failure, exact current-run membership, tenant isolation, and recursion prevention.
- Hardened CI with banned-module scanning that preserves `go list` failures, seed unit tests, seven separately guarded integration groups, and exact causal recovery-test presence checks.
- Ran the complete lint, race, web, generator, Docker-backed integration, production-build, and repeated live-seed gate without resetting existing volumes.

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement safe one-command seeded send and verification** - `a0c1e0f` (feat)
2. **Task 2: Enforce nonempty integration groups and banned-module checks in CI** - `fd6957e` (ci)
3. **Task 3: Run complete phase gate and record actual validation evidence** - `803b848` (docs)

## Files Created/Modified

- `scripts/seed.py` - Bounded, secret-safe key provisioning, generator invocation, exact ClickHouse membership polling, and tenant-scoped API acceptance.
- `scripts/test_seed.py` - Stdlib tests for orchestration failure and security boundaries.
- `Makefile` - Starts the healthy stack once before invoking the seed verifier.
- `.github/workflows/ci.yml` - Adds dependency policy, seed tests, and independently nonempty integration gates.
- `README.md` - Documents seed success/failure semantics, local credential boundaries, and key-cache revocation latency.
- `generator/README.md` - Documents deterministic seed inputs and private run-manifest behavior.
- `.planning/phases/02-ingestion-durable-event-storage/02-VALIDATION.md` - Records the observed local gate and leaves hosted CI explicitly pending.

## Decisions Made

- Bound the ClickHouse membership list as `Array(String)` because the established Phase 2 schema stores `event_id` as `String`; the generator still validates every manifest ID as a UUID before querying.
- Scoped storage verification by tenant, `received_at >= run_start`, and the complete explicit ID set, then required both `uniqExact(event_id) == 1000` and exact set equality.
- Kept all secrets out of command arguments and operator-facing errors. Local key files use directory mode 0700 and file mode 0600.
- Kept hosted GitHub Actions outside the local completion claim because this plan does not push to origin.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Matched the seed query parameter type to the established schema**
- **Found during:** Task 1 live `make seed` acceptance
- **Issue:** The first storage query bound intended IDs as `Array(UUID)`, while `events.event_id` is stored as `String`, causing the verification query to fail even though all 1,000 rows had arrived.
- **Fix:** Validate UUID syntax in Python, then bind the membership parameter as `Array(String)` to match ClickHouse storage.
- **Files modified:** `scripts/seed.py`
- **Verification:** The original run verified 1,000/1,000 rows after correction, and two final consecutive seed runs each reported exact 1,000-event success.
- **Committed in:** `a0c1e0f`

---

**Total deviations:** 1 auto-fixed correctness issue.
**Impact on plan:** The correction aligned the verifier with the existing schema without weakening exact-ID or tenant isolation guarantees.

## Verification

- `make lint test && make test-integ` — passed; the complete Docker-backed integration package reported 204.715 seconds.
- Independently nonempty groups — keyctl/API key, ingest, dedupe/schema/TTL, writer/DLQ, recovery, rate-limit, and readiness/lifecycle all passed.
- `python3 -B -m unittest scripts.test_seed` — 10/10 passed.
- Two consecutive `make seed` runs — each reported `sent=1000`, `accepted=1000`, `visible=1000`, `api_spotchecks=3`, `tenant_isolation=1`, and `retries=0`.
- Default `go build ./cmd/...` — passed with integration-only failure controls excluded.
- Placeholder check — no Phase 2 seed/application placeholder remains; `make bench` remains intentionally nonzero until Phase 5.

## Issues Encountered

- The first live verification exposed the ClickHouse parameter-type mismatch described above; no events were lost, and the corrected verifier proved the full run.
- Hosted GitHub Actions remains pending because no branch was pushed as part of this plan.

## User Setup Required

None beyond the documented local `.env` setup. `make seed` creates and reuses private local demo key files automatically.

## Next Phase Readiness

- Phase 2 is ready for independent goal verification with complete local evidence.
- The ingestion-to-storage path is suitable as the durable input foundation for Phase 3.
- A pushed branch is still required to observe the hosted GitHub Actions run.

## Self-Check: PASSED

- Confirmed all seven plan-owned files exist and commits `a0c1e0f`, `fd6957e`, and `803b848` are present.
- Confirmed the full local gate, all independently selected behavior groups, production build, and both live seed runs exited 0.
- Confirmed no user-owned `.planning/config.json` or `.serena/` change was staged.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-16*
