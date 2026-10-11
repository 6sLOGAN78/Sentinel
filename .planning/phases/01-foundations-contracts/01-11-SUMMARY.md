---
phase: 01-foundations-contracts
plan: 11
subsystem: api-contract
tags: [openapi, kin-openapi, openapi-typescript, drift-test]
requires: [01-04, 01-08, 01-09, 01-10]
provides:
  - api/openapi.yaml (OpenAPI 3.1, all 12 spec section 8 routes)
  - Go validity, route-table and drift tests
  - generated TS types (web/src/api/schema.d.ts, gitignored)
affects: [phase-2 ingest, phase-4 query API, web dashboard]
tech-stack:
  added: [github.com/getkin/kin-openapi v0.149.0]
key-files:
  created:
    - api/openapi.yaml
    - backend/test/openapi_test.go
    - backend/test/openapi_drift_test.go
    - web/src/api/types.ts
    - web/src/api/types.test.ts
  modified:
    - backend/go.mod
    - backend/go.sum
    - web/package.json
    - web/eslint.config.js
    - web/.prettierignore
    - .gitignore
requirements-completed: [FND-05]
duration: ~35 min
completed: 2026-10-11
---

# Phase 1 Plan 11: OpenAPI contract Summary

Hand-authored OpenAPI 3.1 contract for every v1 endpoint, validated with kin-openapi, tied to the canonical JSON Schemas by fixture and structural drift tests, with openapi-typescript types compiling under TypeScript 6.

## Tasks

1. Route/envelope tests then api/openapi.yaml (commit 77c4fba). 7 TestOpenAPI* tests: validity (3.1, no dangling refs, external refs disallowed), exact 12-route table, batch statuses 202/400/401/403/413/429/503 with Retry-After, maxItems 500, security per route (ApiKey on batch, Bearer elsewhere, none on health, no query-token scheme), Error envelope on all non-2xx, list shape, PATCH body, limit max 200, SSE content type. Verified a broken $ref fails the validity test, then reverted.
2. Drift tests (commit 24ec8c6). TestOpenAPIDriftFixtures validates all valid fixtures and rejects all invalid_schema fixtures (event and alert) against the components. TestOpenAPIDriftStructure compares property names, required, additionalProperties and per-property constraints (type, enum, const, pattern, lengths, bounds, oneOf branches) against the embedded schemas via contracts.Schema. Verified that removing `critical` from Alert.severity fails the tests, then reverted.
3. TS types (commit 299d478). gen:api plus pretypecheck/prebuild/pretest, ignore entries, types.ts aliases, compile-only types.test.ts. typecheck, lint, format:check, test (6 passed) and build all pass.

## Deviations from Plan

- [Rule 3 - Blocking] kin-openapi has no `EnableJSONSchema2020Validation`; `doc.Validate(ctx)` is used (3.1 documents are handled by default).
- [Rule 1 - Bug in approach] kin's 2020-12 validator silently falls back to its legacy validator if the schema contains unresolved $ref, and the legacy one rejects `enum` containing null. The drift test therefore inlines component refs into a standalone schema before validating. kin also does not register ipv4/ipv6 format validators by default, so the test passes them per call via `WithStringFormatValidator(NewIPValidator)` (no globals).
- Event/Alert reuse shared component schemas (UUID, UTCTimestamp, IPAddress) via $ref instead of fully flat copies; the structural test resolves them so equivalence with the JSON Schemas is still checked.
- golangci-lint could not run (known "goimports is a formatter" config error, fixed in plan 01-12). gofmt and go vet are clean for ./test/.
- `go mod tidy -e` run as allowed; kin-openapi is currently marked direct.

## Known Stubs

None.

## Notes

- The structural comparison ignores `description`/`title` annotations only. Go-only invalid fixtures (skew, window bounds, 4 KiB attributes) are intentionally not checked against OpenAPI.
- Response schemas for AlertDetail, MetricsSummary, Rule and HealthStatus are first definitions (no canonical JSON Schema exists yet); Phases 2 and 4 may refine them.

## Self-Check: PASSED

Files exist, commits 77c4fba, 24ec8c6 and 299d478 present on worktree-agent-ac4987f37bdba3d57; `go test -race ./test/` and web typecheck/lint/format/test/build pass.
