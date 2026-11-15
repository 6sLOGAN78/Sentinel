---
phase: 04-query-api-analyst-dashboard
plan: "24"
subsystem: detection-operations
tags: [gap-closure, G7, detector, recovery, runbook, adr, ci, compose]
gap_closure: true
gaps_closed: [G7]
requirements: [ALRT-03]
dependency_graph:
  requires: ["04-22", "04-23"]
  provides: ["make detector-recover-history", "runbook hint in detector expired-history error", "ADR 0012", "operations.md expired-history runbook", "CI nonempty TestDetectorHistoryRecovery step", "KAFKA_OFFSETS_RETENTION_MINUTES=525600"]
  affects: ["04-25"]
tech-stack:
  added: []
  patterns: ["Make apply flags pass through the recipe environment, quoted, never interpolated or echoed", "make conditional keeps dry-run recipe free of --apply"]
key-files:
  created:
    - docs/decisions/0012-detector-history-recovery.md
  modified:
    - Makefile
    - backend/internal/kafkax/detector_consumer.go
    - docs/decisions/README.md
    - docs/operations.md
    - .github/workflows/ci.yml
    - deployments/compose/docker-compose.yml
    - scripts/test_stack_config.py
decisions:
  - "ACK/OPERATOR/REASON reach the recipe as environment variables quoted by the shell, so REASON is never interpolated into recipe text or echoed, even by make -n"
  - "The detector error stays one literal (with a justified nolint:lll), so a grep for the original phrase also finds the runbook hint"
  - "Retention: yes (04-22). Compose sets KAFKA_OFFSETS_RETENTION_MINUTES=525600, and test_stack_config asserts it in the rendered config"
  - "Pending-after-commit case: the runbook tells operators to verify, record it in incident notes, then optionally run the guarded pending-to-applied UPDATE (the only change the trigger allows)"
metrics:
  duration: "~25m"
  completed: 2026-11-15
---

# Phase 4 Plan 24: G7 operations (Make target, runbook, ADR 0012, CI, offsets retention) Summary

The G7 recovery from 04-22/04-23 can now be found and run by an operator:

- `make detector-recover-history` plans by default. It applies only with `ACK`, `OPERATOR` and `REASON` all set.
- The detector's expired-history error now names the runbook.
- `docs/operations.md` has the full runbook, and ADR 0012 records the decision.
- Hosted CI runs `TestDetectorHistoryRecovery` as its own nonempty step.
- Compose keeps group offsets for one year.

I walked through it on the isolated `sentinel-g1check` stack, then reset that stack and removed its images.

## Commits

| Task | Commit | Message |
|------|--------|---------|
| 1 | `83d94f6` | feat(04-24): add detector-recover-history target, runbook hint, ADR 0012 and runbook |
| 2 | `8d6d24a` | ci(04-24): run detector history recovery group nonempty and keep group offsets a year |

## What changed

- **Makefile `detector-recover-history`**
  - Listed in `.PHONY`. `make help` shows "Plan (default) or apply an acknowledged expired-history detector recovery".
  - The recipe runs, from `backend/`: `env -u SENTINEL_AUTH_JWT_SECRET -u SENTINEL_AUTH.JWT_SECRET env 'SENTINEL_PRIMARY.ENV=local' 'SENTINEL_ENV_FILE=$(CURDIR)/.env' go run ./tools/detector-recover "$@"`.
  - With no apply variables set, a make conditional keeps every acknowledgement text out of the recipe, so `make -n` shows no `--apply` at all.
  - If any of the three is set, a shell check requires all three. If one is missing, it prints `refusing: apply needs ACK=<plan_digest> OPERATOR=<name> REASON=<text> together` and exits before any go command or `.env` check.
  - Values are read as `"$ACK"`, `"$OPERATOR"` and `"$REASON"` from the recipe environment (GNU Make exports command-line variables). They are never interpolated into recipe text, and the recipe is `@`-silent. Even `make -n` shows only `$REASON`.
  - There is no default operator.
- **Detector error** (`detector_consumer.go:471`): "detector checkpoint history lost; stop detectors and see docs/operations.md (make detector-recover-history)".
  - The original phrase is kept as a prefix.
  - The only assertion of the text is `backend/test/detector_history_recovery_test.go:299`, which uses `require.Contains`, so no test changed.
  - `requireCheckpointFailure` checks exit/readiness/offsets, not the text.
  - The line carries `//nolint:lll` with an explanation, which keeps the literal greppable in one piece.
  - Nothing else in `restorePartition` changed.
- **ADR 0012** covers:
  - Context: 24h retention vs idle stacks, the p4r2/p4r3 incidents, why deleting the group trips the fences, and at-least-once delivery.
  - Decision: both 04-22 answers, verbatim.
  - Consequences, the group-offset expiry sub-case, and the rejected options: B (startup flag), C (group rotation), automatic advance, and deleting the group or editing fences.
  - It is indexed in `docs/decisions/README.md`.
- **Runbook "Detector refuses to start: expired checkpoint history"** covers:
  - The symptom string and the cause.
  - The six steps, with the exact stop command and a table explaining every dry-run JSON field and the exit codes.
  - The audit query, both as `$1` and as a psql `:'g'` variable form.
  - The **Do not delete** forbidden actions: deleting or renaming the group, editing `detection_partition_fences`, deleting topics.
  - The 04-23 known limit, "commit succeeded, audit still pending". The runbook explains how to recognize it: rows of that `plan_digest` are still `pending`, a dry-run reports `nothing_to_recover: true`, and the committed offsets equal `repinned_offset`. It then says how to record it: in incident notes, then optionally the trigger-permitted `pending`-to-`applied` UPDATE scoped by group and digest. The rows already hold operator, reason, digest and ranges.
  - The other pending-row errors.
  - The "group metadata missing while PG fences exist" refusal and its escalation.
  - Keeping the stack up avoids the condition.
  - It makes no performance claims.
  - The existing retention paragraph now links to the runbook.
- **CI**: one new step, "detector history recovery group is nonempty", directly after "API key integration group is nonempty". It runs `scripts/go-test-nonempty.sh './test/...' '^TestDetectorHistoryRecovery$' -tags=integration -race -timeout 8m -p 1`. No other step, cap or job changed, and the 04-18 go-job step is intact. The guards did not pin the integration job text, so no guard edit was needed.
- **Retention (04-22: "Yes (Recommended)")**: kafka env `KAFKA_OFFSETS_RETENTION_MINUTES: "525600"`, plus `test_kafka_keeps_group_offsets_for_a_year` in `scripts/test_stack_config.py`, which reads real `docker compose config --format json`. Rendering the pre-change compose (`git show 83d94f6:deployments/compose/docker-compose.yml`) gives `None` for the key, so the assertion fails without the line.

## Verification (run for real, 2026-11-15 UTC)

| Command | Start | End | Exit |
|---|---|---|---|
| `make -n detector-recover-history` (no `--apply` in output; count 0) | ~10:50 | ~10:50 | 0 |
| `make detector-recover-history ACK=00` / `OPERATOR=x REASON=...` (partial) | ~10:50 | ~10:50 | 2 (refused before go) |
| `make detector-recover-history ACK=notadigest OPERATOR=me REASON='it "was" idle'` (args reach the tool's flag parser) | ~10:51 | ~10:51 | 2 (tool usage error) |
| `bash scripts/check-adrs.sh` | ~10:53 | ~10:53 | 0 |
| `go test -race ./internal/kafkax/...` + `golangci-lint run ./internal/kafkax/...` | ~10:52 | ~10:52 | 0, 0 issues |
| plan grep: `grep -rn "checkpoint history lost" internal test \| grep -v _test.go \| grep -q detector-recover-history` | ~10:52 | ~10:52 | 0 |
| `bash scripts/check-pins.sh` (after compose and CI edits) | ~10:56 | ~10:56 | 0 |
| `unittest test_stack_config.py` / `test_phase3_guards.py` / `test_phase4_guards.py` / `test_go_dependencies.py` (after all edits) | 10:56:19 | 10:59:01 | 0 / 0 (12 tests, 151s) / 0 (26) / 0 (5) |
| `go vet ./...` + `golangci-lint run ./...` + `--build-tags=integration ./test/...` | 11:05:51 | 11:05:59 | 0, 0 issues, 0 issues |

### Walkthrough on isolated `sentinel-g1check` (scrubbed env as in 04-18)

Every command ran under `env -i PATH HOME COMPOSE_PROJECT_NAME=sentinel-g1check GOCACHE GOMODCACHE GOPATH`. Root `.env` ports were confirmed free first, and disk was 11 GB free at the start. Evidence is in `.local/phase4-acceptance/04-24/`.

| Step | Command | Time (UTC) | Exit / result |
|---|---|---|---|
| 1 | `make dev-up` (fresh project; the kafka service is created with the new setting) | ~10:56-11:04:03 | 0 (`dev-up.log`) |
| 2 | `kafka-configs --describe --entity-type brokers --all \| grep offsets.retention.minutes` | ~11:01 | `offsets.retention.minutes=525600` (STATIC_BROKER_CONFIG; default 10080) |
| 3 | `make detector-recover-history` (detector running) | 11:04:19-11:04:20 | 0; `nothing_to_recover:true`, stderr "nothing to recover" (`dry-run-detector-running.json`) |
| 3b | Runbook steps 1/2/4/5: compose `stop detector`, dry-run, partial ACK refusal, `up -d --wait detector`, `/readyz`, dry-run | 11:04:29-11:04:46 | 0 / 0 nothing to recover / 2 refused / 0 / HTTP 200 / 0 (`runbook-walkthrough.log`) |
| 3c | Runbook audit query via psql `:'g'` | ~11:05 | 0 rows (table present); group describe shows committed 0 on all 6 partitions, so the real plan path ran (not the missing-metadata path) |
| 4 | `go-test-nonempty.sh './test/...' '^TestDetectorHistoryRecovery$' -tags=integration -race -timeout 8m -p 1` | to 11:05:45 | 0; 3 subtests PASS (`native-history-recovery.log`) |
| 5 | guard suites | 10:56-10:59 | all 0 (table above) |
| end | `make reset CONFIRM=1` (same project name), then `docker rmi` of the 6 `sentinel-g1check-*` images | 11:06:08 | 0 / 0 |

**Final state of the isolated stack:**
- `sentinel-g1check` has no containers, no volumes with its project label, and no `sentinel-g1check-*` images.
- All root `.env` ports are free.
- Disk is 7.7 GB free. The image build cache grew, and caches were not cleared.

`docker compose ls -a` after the run (`compose-ls-after.txt`) shows `sentinel` (exited 5) and `sentinel-p4-demo-20261024` (exited 4, restarting 5) unchanged from before. I touched neither project.

The orchestrator override was to reset and remove images instead of only stopping the stack. The plan text said "stop, and remove volumes only under disk pressure". I followed the orchestrator's instruction. Before the removal I verified that every removed volume carried the `sentinel-g1check` project.

## Deviations from Plan

**1. [Rule 2 - Security] Apply flags passed via the environment, not interpolated.** The plan's sketch was `--reason="$(REASON)"`. Interpolating would place REASON in the recipe text, where `make -n` would show it and a `"` or `` ` `` in it would break or inject into the shell. The recipe reads `"$REASON"` from the exported environment instead. One side effect remains: Make still expands `$` inside command-line values, and the runbook says to avoid `$` in REASON. Commit `83d94f6`.

**2. [Rule 3 - Lint] `//nolint:lll` on the detector error.** The required text is 131 columns. Splitting it would break the plan's own grep, which needs the original phrase and the hint on one line. The directive names the linter and gives a reason, as nolintlint requires. Commit `83d94f6`.

**3. Teardown per orchestrator.** `make reset CONFIRM=1` plus image removal, instead of `docker compose stop` (see above).

## Known limitations

- **Pending rows are a documented limit, not fixed here.** The pending-after-commit case from 04-23 is documented and recorded by hand, as described above. The tool itself still does not finalize those rows.
- **The full stale-pin scenario was not reproduced on compose.** The walkthrough on a healthy stack shows "nothing to recover". The native test, run here with -race, proves the full expired-history flow with a real broker.
- **Hosted CI is not proven yet.** No hosted run was triggered (nothing was pushed). The hosted integration job proof belongs to 04-25.
- Out-of-scope finding: the ADR index omits 0007-0009, and `check-adrs.sh` only gates 0001-0006. It is logged as D-04-24-01 in `deferred-items.md`.

## Threat model

- T-04-24-01 (mitigated): dry-run is the default, and partial input fails before any command.
- T-04-24-02 (mitigated): REASON is never echoed or interpolated, and the JWT variables are unset for the tool.
- T-04-24-03 (mitigated): one-year offsets retention, verified on the live broker.
- T-04-24-04 (mitigated): the runbook directs operators to the audit query and forbids untracked recovery. The one manual UPDATE it allows is the trigger-guarded `pending` to `applied` change, recorded in incident notes.

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: docs/decisions/0012-detector-history-recovery.md
- FOUND: Makefile target `detector-recover-history:`
- FOUND: docs/operations.md contains detector-recover-history, detection_history_recoveries, "Do not delete"
- FOUND: .github/workflows/ci.yml contains `^TestDetectorHistoryRecovery$` (one step)
- FOUND: 83d94f6, 8d6d24a
