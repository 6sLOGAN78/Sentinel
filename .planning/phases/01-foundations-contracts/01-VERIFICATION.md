---
phase: 01-foundations-contracts
verified: 2026-10-11T21:10:00Z
status: human_needed
score: 5/5 roadmap success criteria verified locally (2 evidence items need human confirmation)
overrides_applied: 0
human_verification:
  - test: "Push a branch to the GitHub remote and confirm every job in .github/workflows/ci.yml (go, web, python, schema) passes"
    expected: "All CI jobs green on push"
    why_human: "CI has never run on GitHub. Roadmap SC4 says 'CI validates it' and 'runs lint, test and schema validation on push'. Only local equivalents were run."
  - test: "On a disposable machine or after accepting data loss, run 'make reset CONFIRM=1 && make dev-up && scripts/check-stack.sh && make migrate'"
    expected: "Stack comes up healthy from zero volumes, topics are created and migrations apply"
    why_human: "The destructive 'fresh from reset' path was not exercised. Only the refusal without CONFIRM=1 and idempotent re-runs on an existing stack were verified."
---

# Phase 1: Foundations & Contracts Verification Report

**Phase Goal:** A developer can clone the repo, bring up the full dependency stack with one command, and rely on versioned contracts and decisions that later phases build on.
**Status:** human_needed. No code gaps were found. Two evidence items cannot be produced in this environment.
**Re-verification:** No, initial verification.

## Roadmap Success Criteria

| # | Criterion | Status | Evidence (run by verifier) |
|---|-----------|--------|----------------------------|
| 1 | `make dev-up` gives healthy Kafka/ClickHouse/PG/Redis, topics explicit (auto-create off); migrate/test/lint/dev-down/reset idempotent | VERIFIED (reset-from-zero is human) | `make dev-up` re-run on the running stack succeeded and topic-init was idempotent. `scripts/check-stack.sh` passed: 4 services healthy, topics events 6 partitions / alerts 3 / dlq 1 with the expected retention, auto-create off confirmed, 5 host ports dialed, CH/PG/Redis ping. `make migrate` ran twice and the second run was a no-op ("clickhouse schema up to date"). `make test` exit 0. `make lint` exit 0 (golangci-lint 0 issues, eslint, tsc, ruff, mypy). `make reset` refuses without CONFIRM=1. `make seed` and `make bench` exit non-zero, so they are honest placeholders. `dev-down` and `reset CONFIRM=1` were not run, per instruction. |
| 2 | One Go 1.26.x module, five binaries in `backend/cmd/*`; boilerplate gone; defects fixed and tested | VERIFIED | `go.mod` has module `github.com/6sLOGAN78/sentinel`, `go 1.26.0`, toolchain go1.26.9. `go build ./cmd/...` succeeded. `cmd/` holds exactly alert-writer, detector, event-writer, ingest and query-api. `go list -deps` has no newrelic/clerk/asynq/resend/pkg/errors. `middleware/chain.go` puts Recover first and a test pins the order. `redisx.New` pings and closes the client on failure. `database.ConnString` uses `url.UserPassword` for DSN escaping. `godotenv` is gated in `config.go`. `app.go` has Shutdown/closer handling with a lifecycle test. `go test -race ./...` is all ok. `make test-integ` is ok (25.8s, includes TestBinaryLifecycle and migration/DSN/Redis tests against containers). |
| 3 | Valid and invalid fixtures (IPv4/IPv6, skew) pass or fail as expected; one canonical SHA-256 `alert_id` function, fuzzed with `\|` | VERIFIED | `contracts/` has event and alert schemas, Go types, and large valid / invalid_schema / invalid_go_only fixture sets (including skew and IPv6). `dedupe.go` with golden tests. `go test ./internal/contracts -run DedupeKey -fuzz=FuzzDedupeKey -fuzztime=10s` gave 782k execs and PASS. |
| 4 | `api/openapi.yaml` (3.1) covers every v1 endpoint, error envelope and pagination; CI validates it; CI skeleton runs lint/test/schema on push | VERIFIED locally; CI run is human | `openapi: 3.1.0`. All spec section 8 routes are present: `events:batch`, events, events/{id}, alerts, alerts/{id} (GET and PATCH), alerts/stream, rules, metrics/summary, healthz, readyz and metrics. `backend/test/openapi_test.go` and `openapi_drift_test.go` validate the doc with kin-openapi and check drift against the JSON Schemas, and they pass in `test-integ`. `web` generates TS types and typechecks. `.github/workflows/ci.yml` has go/web/python/schema jobs triggered on push and pull_request, with SHA-pinned actions and no `\|\| true`. The workflow has never executed on GitHub. |
| 5 | Pinned versions, placeholder-only `.env.example`, five ADRs in `docs/decisions/` | VERIFIED | `scripts/check-pins.sh` passed (images digest-pinned, ports bound to 127.0.0.1, env placeholders only, `.env` untracked and ignored, lockfiles present, CI clean). `scripts/check-adrs.sh` passed for ADRs 0001..0006. That is the five required topics plus the alert-storage split. Lockfiles: go.sum, web/package-lock.json, generator/uv.lock. |

## Requirements Coverage

| Requirement | Status | Evidence |
|-------------|--------|----------|
| FND-01 | SATISFIED | SC1: compose stack, explicit topics, check-stack.sh passes |
| FND-02 | SATISFIED (CI half human) | SC1 and SC4: Make targets verified, CI skeleton present but not run on GitHub |
| FND-03 | SATISFIED | SC2 |
| FND-04 | SATISFIED | SC3 |
| FND-05 | SATISFIED (CI run human) | SC4 |
| FND-06 | SATISFIED | SC5: check-pins.sh |
| FND-07 | SATISFIED | SC5: check-adrs.sh |

All seven IDs map to Phase 1 in REQUIREMENTS.md, and there are no orphaned IDs.

Bookkeeping note: the REQUIREMENTS.md checkboxes and traceability table still show FND-01..07 as `Pending`. Update them to complete when the phase closes. This is not a code gap.

## Anti-Patterns

No TBD/FIXME/XXX debt markers were found in the code. `seed` and `bench` fail loudly instead of faking success. The advisory findings in 01-REVIEW.md and the threat verification in 01-SECURITY.md were not re-audited, per instructions.

## Human Verification Required

1. **GitHub Actions run.** Push a branch and confirm the go, web, python and schema jobs pass. This is the manual-only item already listed in 01-VALIDATION.md. The remote `origin` is configured but nothing has been pushed.
2. **Destructive fresh-start.** Run `make reset CONFIRM=1`, then `make dev-up`, `scripts/check-stack.sh` and `make migrate` from empty volumes. This was not run because it would destroy the running stack.

## Gaps Summary

There are no code or contract gaps. Every locally testable truth passed on a real run by the verifier. The `human_needed` status reflects only the two evidence items above.

_Verified: 2026-10-11_
_Verifier: Claude (gsd-verifier)_
