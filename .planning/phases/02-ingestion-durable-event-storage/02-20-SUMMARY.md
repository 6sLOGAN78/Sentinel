---
phase: 02-ingestion-durable-event-storage
plan: "20"
subsystem: infra
tags: [kafka, compose, retention, regression]
requires:
  - phase: 02-13
    provides: Ordered local startup and exact-run seeding
provides:
  - Existing Kafka topics receive configured retention on every normal startup
  - Stack checker resolves the same effective Compose topic settings
  - Real existing-topic and subprocess configuration regressions
affects: [02-21, local-operations]
tech-stack:
  added: []
  patterns: [Idempotent creation followed by explicit policy alteration, In-memory secret-bearing configuration parsing]
key-files:
  created: [backend/test/topic_retention_test.go, scripts/test_stack_config.py]
  modified: [deployments/compose/docker-compose.yml, scripts/check-stack.sh, README.md]
key-decisions:
  - "Reapply retention explicitly without changing existing partitions or deleting topics."
  - "Resolve startup expectations through Compose, propagate its exit status, and emit only five nonsecret settings."
requirements-completed: ["GEN-03", "STOR-04"]
duration: 17 min
completed: 2026-10-16
---

# Phase 2 Plan 20: Repeatable Topic Retention Summary

Configured Kafka retention now reapplies to existing topics, and acceptance checks use the same effective environment as startup.

## Performance

- Started: 2026-10-16T17:10:09Z
- Completed: 2026-10-16T17:27:00Z
- Duration: approximately 17 minutes
- Tasks: 2/2
- Files created/modified: 5 implementation/test/documentation files

## Accomplishments

- Added `kafka-configs.sh --alter --add-config retention.ms=...` after each idempotent topic creation. Variable arguments are quoted, and `set -euo pipefail` propagates any alteration failure.
- Resolved the five known topic settings from `docker compose config --format json` in memory. Shell environment precedence agrees with startup; configuration failures preserve their exit status; full resolved configuration is never logged.
- Added exact numeric-token checks so longer partition, replication, or retention values cannot pass by matching the expected value's prefix.
- Documented operational behavior: retention reapplies on every `make dev-up`; partition mismatches remain visible and require a separately authorized reset; reducing retention can permanently delete eligible data.

## Task Commits

1. Task 1, behavioral regressions: `b9dd037` — `test(02-20): expose retention reapply and effective config failures`.
2. Task 2, repair and test style cleanup: `1865781` — `fix(02-20): reapply retention and check effective topic settings`.

## Verification Evidence

| Check | Observed result |
| --- | --- |
| `python3 -B -m unittest discover -s scripts -p test_stack_config.py` before repair | Exit 1; 4 of 5 tests failed behaviorally in 1.683s. Dotenv/shell overrides were compared to hardcoded partitions, Compose failure was ignored, and a longer retention value with the expected prefix was accepted. |
| `cd backend && go test -tags=integration ./test -run '^TestTopicRetentionOverridesReapply$' -count=1 -timeout 3m -p 1` before repair | Exit 1, 37.237s. Existing events topic still reported `retention.ms=86400000` after initialization requested `172800000`; the named changed-retention assertion failed. |
| Same Python command after repair | Exit 0; all 5 tests passed in 2.173s. Real Compose resolution supplies defaults, dotenv overrides, and shell-over-dotenv precedence; stub Kafka reports actual values; deliberate mismatches fail. |
| Same real Kafka command after repair | Exit 0, 63.373s. Default policies, changed policies for all three topics, and an unchanged repeated run passed; partition counts stayed 6/3/1 throughout. Test context is bounded to 90s and command timeout to 3m. |
| `bash -n scripts/check-stack.sh` | Exit 0. |
| `docker compose -f deployments/compose/docker-compose.yml --env-file .env config --quiet` | Exit 0; no resolved secrets printed. |
| `cd backend && go tool -modfile=tools.mod golangci-lint run --build-tags integration --timeout 60s ./test/topic_retention_test.go` | Exit 0; 0 issues after test formatting/testify style cleanup. |
| `cd generator && uv run --locked ruff format ../scripts/test_stack_config.py && uv run --locked ruff check ../scripts/test_stack_config.py` | Exit 0; formatted and all checks passed. |
| `git diff --check` scoped to plan files | Exit 0. |

The real regression executes the Compose-resolved topic-init shell program in a dedicated throwaway Kafka fault container. Compose's serialized `$$` transport escaping is decoded as it is for container creation, then only `kafka:9092` is adapted to fixture `localhost:9092`. Synthetic passwords come from a private temporary env file. Altered policies apply only to empty fixture topics; cleanup is registered immediately. No user topic, partition, volume, `.env`, or environment override was changed.

The checker unit fixture deliberately reports an undeclared topic after the three topic checks to stop before host TCP/HTTP checks. Its success assertions prove all three topic checks accepted the effective settings, then the existing auto-create check rejected the fixture topic. This is targeted topic/configuration evidence, not a claim that the full local stack passed.

## Deviations from Plan

**[Rule 1 - Bug] Reject prefix-only configuration matches.** During Task 1, a real checker subprocess accepted `864000000` when configured for `86400000`. Task 2 now matches complete numeric tokens for retention, partitions, and replication factor. The named mismatch regression failed before the repair and passed after it. Committed in `1865781`.

## Issues Encountered

The real test initially encountered two fixture setup errors: its repository-relative path was one level short (exit 1, 24.787s), and Compose JSON's `$$` serialization required transport decoding before direct execution (exit 1, 20.542s). Both were corrected before the behavioral RED run; neither is counted as defect reproduction. Targeted lint then identified test line lengths and an unnecessary regexp compilation; the style corrections passed lint and preserve the same assertions.

## Security and Stub Review

All changes stay within the plan's topic-policy/configuration trust boundaries. No new dependencies, endpoints, or uncovered threat surfaces were introduced. No blocking stubs were found in plan-owned files. Secret-bearing Compose JSON stays in memory; tests assert synthetic secrets do not appear in checker output.

## Next Phase Readiness

02-20's targeted gates pass. The normal preserved-stack `make dev-up` and `scripts/check-stack.sh`, full phase gate, two seed runs, and independent re-verification remain assigned to 02-21/orchestrator. Shared STATE/ROADMAP/REQUIREMENTS/VALIDATION/configuration files were left to the orchestrator. Hosted CI remains pending; no push was performed.

## Self-Check: PASSED

All five declared implementation/test/documentation paths exist. Task commits `b9dd037` and `1865781` exist and contain no tracked-file deletions. Targeted behavioral gates passed; original verification/review reports and user-owned changes were preserved.
