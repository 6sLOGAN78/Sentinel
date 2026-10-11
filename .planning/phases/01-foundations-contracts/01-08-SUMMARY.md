---
phase: 01-foundations-contracts
plan: 08
subsystem: contracts
tags: [json-schema, event, validation, go]
requires: ["01-01"]
provides:
  - "contracts.Schema(name) embed accessor, event.v1.json (2020-12)"
  - "contracts.Event, Timestamp, Limits, DecodeEvent, Validate, FieldError"
  - "test-only CompileSchema helper (jsonschema/v6, AssertFormat)"
affects: [ingest, detector, alert contract plan 01-09]
tech-stack:
  added: ["github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 (tests only)"]
key-files:
  created:
    - backend/internal/contracts/schema.go
    - backend/internal/contracts/schema/event.v1.json
    - backend/internal/contracts/event.go
    - backend/internal/contracts/event_test.go
    - backend/internal/contracts/helpers_test.go
    - backend/internal/contracts/testdata/event/ (13 valid, 40 invalid_schema, 3 invalid_go_only)
  modified: [backend/go.mod, backend/go.sum]
decisions:
  - "DecodeEvent pre-checks root keys case-sensitively because encoding/json matches keys case-insensitively"
  - "DecodeEvent caps input at 1 MiB (ErrEventTooLarge) to keep decoding bounded"
  - "IPv4-mapped IPv6, zoned and dotted-mixed IPv6 are rejected by both schema (pattern) and Go"
metrics:
  tasks: 2
  completed: 2026-10-11
---

# Phase 1 Plan 08: Event contract Summary

Versioned event v1 JSON Schema plus strict Go Event type whose accept/reject decisions are proven identical on a 56-file fixture corpus, with Go-only skew (+5m/-7d) and attribute (32 keys, 4096 bytes) bounds tested at exact boundaries.

## Commits
- 8f9f951: schema, loader, fixtures, schema tests, jsonschema/v6 dependency
- a285083: event.go, agreement/skew/attribute/marshal tests

## Verification
- `go test -race ./internal/contracts/...` passes; `go-test-nonempty.sh` for TestSchema, TestFixture and TestSkew pass.
- gofmt and go vet clean on the package.
- Production code does not import jsonschema (only test files do).

## Deviations from Plan
- **[Rule 2 - Security]** Added case-sensitive root key check and 1 MiB read cap to DecodeEvent (T-08-01 parity; unbounded read otherwise).
- Go-behavior tests are named TestFixture*/TestSkew* so the mandated verify patterns match.
- `ports_boundary.json` holds 65535; extra `port_zero.json` covers 0.

## Deferred / Known
- golangci-lint cannot run: config fails with "goimports is a formatter" (fixed by plan 01-12; .golangci.yml untouched). The lint acceptance criterion is therefore not verified here.
- Tests ran with GOTOOLCHAIN=go1.26.9 (go.mod requires 1.26).
- go.mod only touched by `go get` of jsonschema/v6; go-redis retained.

## Known Stubs
None.

## Self-Check: PASSED
