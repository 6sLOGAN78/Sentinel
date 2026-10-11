---
phase: 01-foundations-contracts
plan: 09
subsystem: contracts
tags: [dedupe, sha256, alert, json-schema, fuzz, go]
requires: ["01-08"]
provides:
  - "contracts.DedupeKey(tenant, ruleID, entity []string, windowStart) - sole alert_id producer"
  - "contracts.ValidWindow (years 2000..2100)"
  - "alert.v1.json (2020-12), contracts.Alert, DecodeAlert, Validate"
affects: [detector phase 3, alert store phase 4]
tech-stack:
  added: []
key-files:
  created:
    - backend/internal/contracts/dedupe.go
    - backend/internal/contracts/dedupe_test.go
    - backend/internal/contracts/alert.go
    - backend/internal/contracts/alert_test.go
    - backend/internal/contracts/schema/alert.v1.json
    - backend/internal/contracts/testdata/alert/ (20 valid, 36 invalid_schema, 4 invalid_go_only)
decisions:
  - "Golden vectors computed with an independent Python implementation of the encoding, then frozen"
  - "entity_key and dedupe_key require minLength 1 so Go can detect missing fields without pointers"
  - "sample_event_ids is required (nil rejected); empty array is valid"
metrics:
  tasks: 2
  completed: 2026-10-11
---

# Phase 1 Plan 09: DedupeKey and alert contract Summary

Frozen length-prefixed SHA-256 DedupeKey (domain `sentinel.alert.v1`, uint32 length prefixes, component count, uint64 UTC ms window) with five golden vectors and an injectivity fuzz, plus an alert v1 schema and strict Go type that agree on the whole fixture corpus.

## Commits
- 586bb46: DedupeKey, ValidWindow, golden/collision/normalization tests, FuzzDedupeKey
- 277e436: alert.v1.json, alert.go, alert_test.go, fixtures

## Verification
- `go test -race ./internal/contracts/...` and `go test ./...` pass.
- `go-test-nonempty.sh` with TestAlert passes; TestDedupeKey tests pass.
- `go test -fuzz=FuzzDedupeKey -fuzztime=10s`: about 940k execs, no violation.
- Fixtures: 20 valid, 36 invalid_schema (includes spec label, 63-char id, 21 samples), 4 invalid_go_only. gofmt and go vet clean.
- Only dedupe.go uses sha256.Sum256 in non-test code.

## Deviations from Plan
- **Summary max length**: the plan lists it as Go-only, but JSON Schema can express it, so the schema has maxLength 512 and the fixture is in invalid_schema. Go-only fixtures are the window ordering and range rules.
- **Fuzz corpus**: Go only writes corpus files for failing inputs, so no files were generated under testdata/fuzz; the seeds live in `f.Add`.
- Alert code was written before alert_test.go for Task 2 (tests were then run against fixtures); Task 1 followed red then green.
- Added case-sensitive root key check and 1 MiB decode cap to DecodeAlert (parity with DecodeEvent, reuses ErrEventTooLarge).

## Deferred / Known
- golangci-lint could not be run: the config fails to load ("goimports is a formatter"), fixed by plan 01-12. The lint acceptance criterion is unverified here. `//nolint:gosec` directives were added for uint32/uint64 conversions in dedupe.go.
- Tests ran with GOTOOLCHAIN=go1.26.9. go.mod/go.sum untouched.

## Known Stubs
None.

## Self-Check: PASSED
