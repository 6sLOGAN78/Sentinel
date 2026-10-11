---
status: partial
phase: 01-foundations-contracts
source: [01-VERIFICATION.md]
started: 2026-10-11T15:37:09Z
updated: 2026-10-11T15:39:30Z
---

## Current Test

Test 1 (GitHub Actions CI run) awaiting a push to origin

## Tests

### 1. GitHub Actions CI run
expected: After pushing a branch, the go, web, python, schema, hygiene and secrets jobs in .github/workflows/ci.yml pass on GitHub (compose-smoke is optional until Phase 6).
result: [pending]

### 2. Destructive fresh start
expected: `make reset CONFIRM=1` then `make dev-up`, `scripts/check-stack.sh` and `make migrate` (twice) all succeed from empty volumes.
result: passed (2026-10-11T15:39:30Z): make reset CONFIRM=1 removed all sentinel containers/volumes/network; from empty volumes make dev-up=0, scripts/check-stack.sh=0 (all 7 PASS), make migrate run 1=0 (tern to version 1, clickhouse applied 1), run 2=0 (both up to date). Run by Claude at the user's request.

## Summary

total: 2
passed: 1
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
