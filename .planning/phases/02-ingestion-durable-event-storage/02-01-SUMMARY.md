---
phase: 02-ingestion-durable-event-storage
plan: "01"
subsystem: auth
tags: [go, postgres, api-keys, sha256, cache]
requires:
  - phase: 01-foundations-contracts
    provides: PostgreSQL lifecycle, embedded migrations, tenant validation patterns and integration containers
provides:
  - Hash-only tenant-scoped producer credential persistence and operator CRUD
  - Uniform request authentication with a bounded 30-second positive cache
  - Idempotent private demo-key provisioning through keyctl ensure
affects: [02-02, 02-04, 02-10, 02-13]
tech-stack:
  added: []
  patterns: [constant-time credential comparison, positive-only bounded cache, atomic private credential files]
key-files:
  created:
    - backend/internal/apikeys/keys.go
    - backend/internal/apikeys/store.go
    - backend/internal/apikeys/verifier.go
    - backend/migrations/postgres/002_producer_keys.sql
  modified:
    - backend/tools/keyctl/main.go
    - backend/test/keyctl_test.go
    - backend/test/migrate_test.go
    - .gitignore
key-decisions:
  - "Cache verified credential records for at most 30 seconds, but compare the presented secret on every request and never cache misses."
  - "Open reusable credential files with no-follow semantics, validate the opened descriptor, and atomically replace files at mode 0600 under a 0700 directory."
requirements-addressed: [ING-01, ING-05, GEN-03]
requirements-completed: []
duration: 16min
completed: 2026-10-13
---

# Phase 2 Plan 01: Scoped Producer Credentials Summary

**Hash-only scoped producer keys with constant-time verification, bounded positive caching, and private idempotent CLI provisioning.**

## Performance

- **Started:** 2026-10-13T18:54:06+05:30
- **Completed:** 2026-10-13T19:10:04+05:30
- **Duration:** 16 min
- **Tasks:** 3
- **Files modified:** 11

## Accomplishments

- Added a PostgreSQL `producer_keys` migration and parameterized store that persists only SHA-256 secret hashes while returning full credentials only at creation.
- Added `keyctl create`, `list`, `revoke`, and `ensure` with validated tenants/scopes, secret-free metadata output, revocation, and private reusable demo-key files.
- Added a shared verifier with uniform unauthorized errors, sanitized dependency errors, constant-time comparisons, a capped positive-only cache, copied mutable data, and a hard 30-second TTL.
- Proved create/list/revoke/ensure, hash-only storage, cross-tenant and scope rotation, malformed-file repair, symlink refusal, and revocation expiry against real migrated PostgreSQL.

## Task Commits

1. **Task 1: Specify scoped key provisioning against real PostgreSQL** — `6a88f9e` (test)
2. **Task 2: Create real scoped credentials and implement keyctl CRUD** — `996d2d6` (feat)
3. **Task 3 RED: Specify bounded credential verification cache** — `d8d64c1` (test)
4. **Task 3 GREEN: Bound verification and private key provisioning** — `06e8d1a` (feat)

## Verification

- `cd backend && go test -race ./internal/apikeys/... ./tools/keyctl/... -timeout 60s` passed.
- `cd backend && go test -count=1 -tags=integration ./test/... -run 'TestKeyctl|TestMigratePostgresIdempotent' -timeout 3m -p 1` passed against real PostgreSQL in 32.661s.
- `cd backend && go vet ./internal/apikeys/... ./tools/keyctl/...` passed.
- `cd backend && go tool -modfile=tools.mod golangci-lint run ./internal/apikeys/... ./tools/keyctl/...` passed with zero issues.
- `git diff --check` passed, and normal commit hooks ran without bypass.

## Interfaces for Downstream Plans

- `apikeys.NewStore(pool *pgxpool.Pool) *Store`
- `Store.Create(ctx, tenant, scopes, name) (string, error)`, `Lookup`, `List`, and `Revoke`
- `apikeys.NewVerifier(store LookupStore, capacity int, ttl time.Duration, clock func() time.Time) *Verifier`
- `Verifier.Authenticate(ctx, rawCredential) (Principal, error)` and `Principal.HasScope(scope) bool`
- Key format: `snt_<12 lowercase base32 chars>_<43 raw-base64url chars>` with 32 random secret bytes.

## Decisions Made

- Unknown, malformed, revoked, and wrong-secret credentials return the same `ErrUnauthorized`; storage failures return the fixed `ErrUnavailable` classification.
- Cache entries contain copied hash/scope data and never extend their absolute expiry on hits or concurrent misses.
- `ensure` refuses symlink credential files and non-private directories, secures reusable files to 0600, and revokes a newly created key if its file cannot be delivered.

## Deviations from Plan

- Verifier RED tests moved from Task 1 to immediately before Task 3 because Task 1 otherwise referenced the not-yet-created verifier and made Task 2's build boundary uncompilable. The same planned cases were retained.
- Lint exposed a path race in the first private-file implementation. The final code opens with `O_NOFOLLOW`, validates the opened descriptor, and reads through that descriptor before reuse.
- ING-01, ING-05, and GEN-03 remain pending because the HTTP intake route and `make seed` are delivered by later plans.

## Issues Encountered

- The delegated executor reached its usage limit after creating the Task 3 implementation. Execution resumed inline from the clean commit boundary; no work was discarded.

## Known Stubs

None. The CLI, verifier, migration, and PostgreSQL acceptance path are complete for this slice.

## User Setup Required

None. Plan 02-13 will wire the local demo-key paths into the seed workflow.

## Next Phase Readiness

Producer identity and scopes are ready for the ingest route in 02-02. The deterministic Python generator in 02-10 can consume keys from the private files without putting secrets in arguments or logs.

## Self-Check: PASSED

All owned artifacts and four task commits exist. Targeted race, vet, lint, whitespace, and non-cached real-PostgreSQL acceptance checks passed. Preexisting `.planning/config.json` and `.serena/` changes remain unstaged.
