---
phase: 03-detection-alert-pipeline
plan: "13"
subsystem: detection
tags: [go, python, kafka, clickhouse, postgres, compose, tdd]
requires:
  - phase: 03-12
    provides: Restored-assignment readiness and strict authoritative recovery
  - phase: 03-20
    provides: Durable decision crash/outage/ownership recovery proof
  - phase: 03-21
    provides: Private caught-up snapshot and bounded live scheduling protocol
provides:
  - Healthy five-application preserved-volume Compose startup
  - Private current-run scenario command and exact Kafka/CH/PG reconciliation
  - Full canonical shared-fixture and real preserved legacy/scenario/repeat proof
affects: [03-14, phase-3-verification]
tech-stack:
  added: []
  patterns: [Current-offset alert evidence, original-run received-at bound, bounded typed full-row batches]
key-files:
  created:
    - scripts/seed_scenarios.py
    - scripts/test_seed_scenarios.py
    - backend/tools/check-alerts/main.go
    - backend/tools/check-alerts/main_test.go
    - backend/test/scenarios_pipeline_test.go
  modified:
    - deployments/compose/docker-compose.yml
    - Makefile
    - scripts/check-stack.sh
    - scripts/test_stack_config.py
    - backend/test/scenarios_preserved_test.go
    - backend/internal/detection/replay_test.go
    - backend/internal/detection/evidence.go
    - backend/internal/detection/evidence_test.go
    - backend/internal/detection/scan_test.go
    - backend/tools/detection-state/main.go
    - backend/tools/detection-state/main_test.go
key-decisions:
  - "03-13: Preserve independent fixture wording and align only the SCAN runtime summary; retain all canonical fixture bytes and identity/schema/version policies."
  - "03-13: Map fresh documentation IPv6 entities before snapshot key mapping and schedule construction; reuse pinned generator functions and the canonical identity helper."
  - "03-13: Exact current alerts require the captured Kafka range plus immediate-dedupe CH and PG workflow payloads; current event rows additionally require original UTC-millisecond run_start."
  - "03-13: Preserve historical full-row hashes with bounded typed batches and existing received_at ASC earliest-row policy; do not spend more time or weaken assertions."
requirements-completed: [GEN-02, DET-05, DET-09, ALRT-01, ALRT-02]
duration: approximately 10h elapsed including quota interruptions and stack recovery
completed: 2026-10-19
---

# Phase 3 Plan 13: Preserved-Stack Exact Scenario Verification Summary

Five healthy Compose applications now support a private bounded scenario command that proves exact current-run Kafka, ClickHouse and PostgreSQL alerts while retaining prior events and workflows.

## Performance

- First assertion RED commit: 2026-10-19T12:37:07Z; final acceptance/commit: 2026-10-19T22:30:47Z. Approximately ten hours wall time includes two quota interruptions, legal clock waits and resumed-stack recovery, not measured active implementation time.
- Tasks: 3/3. Application/test/config files: 16 (nine declared plus seven explicitly approved).
- Times and completion date in this record are UTC; final local date was October 20.

## Accomplishments

- Existing pinned image, Compose filename, named volumes, health probe and loopback bindings remain. Startup is infrastructure → explicit topics → migrations → all five healthy apps. Detector rules remain read-only; detector/alert-writer ops use 9102/9104 and bounded resources.
- `make seed-scenarios` invokes the new script after normal startup, with optional private output/prior-manifest paths. Legacy `make seed` retains its exact 1,000-event contract.
- The script fixes noncolliding documentation IPv6 mappings before acquiring the actual six-partition key map, reuses the pinned generator's independent scenario expectations, identity function, private writers, preflight/waits and unchanged 120-second/five-attempt sender, and verifies ordered acknowledgment IDs. It never derives expected alerts from observed results.
- One original start/deadline covers snapshot/catch-up, construction, legal future-skew waiting, delivery and final reconciliation. Delivery reserves the final 60 seconds inside the original 15 minutes; one scenario has a 16-minute outer cap and the separate preserved repeat a 32-minute ceiling. No watermark reset, group substitution or producer-quiescence bypass is used.
- Acknowledged output is written privately with `verified=false` before reconciliation. Success requires three explicit true verifier flags, exact expected alert bytes/IDs/tenant/window in the captured current Kafka range, immediate-dedupe CH and PG workflow/list fields, drained six-partition checkpoints, and unchanged full historical event/CH/PG snapshots including workflow seq.
- Current acknowledged event rows require `received_at >= run_start`, with canonical UTC-millisecond start validation. Historical event rows cannot satisfy malicious or benign-zero current-run manifests. Prior preservation intentionally remains historical and uses the existing earliest received-row policy.
- File access refuses symlink ancestors, public/nonregular/oversized/ambiguous manifests; new directories/files use 0700/0600. Child credentials are purpose-isolated, API keys stay out of argv, and diagnostics expose fixed sanitized stages only. Queries/processes/polls/rows/bytes are bounded.
- Late observations record actual container identity and counter baselines after legacy seed/startup, reject restart/reset during each run, and require normal scenario delta zero or explicit lateness-edge delta exactly one. This is not a claim of zero late drops across legacy seed itself.

## Task Commits and TDD Gates

1. Task 1 — compilable declaration/behavioral RED, private subprocess expectations, absent worker regressions and complete canonical fixture comparison: `97797ef`.
2. Task 2 — healthy workers, ordered startup/checker, read-only rules and approved canonical SCAN alignment: `a8ff12e`.
3. Task 3 supporting RED/GREEN:
   - `2e17dd3` — actual constructor-bounds RED and two approved old SCAN expectations.
   - `184084a` — explicit observer fetch bound GREEN.
   - `3ad49a6` — compilable bounded batch/full-row assertion RED.
   - `e0f3777` — exact typed batch GREEN.
   - `765ecaa` — historical-current-row assertion RED for malicious and benign-zero.
   - `f686b61` — final script/verifier/run-start/static/preserved acceptance GREEN.

The first REDs failed behavior after loading/compilation, not missing imports or setup. Initial setup failures are separately recorded below. No tracked file was deleted; no fixture/golden or generator source was regenerated.

## Actual Verification

Commands ran from repository root unless explicitly prefixed `cd backend`/`cd generator`. Every Go family below was selected independently through `scripts/go-test-nonempty.sh`, never a possibly empty combined selector.

| Command / independently selected family | Actual exit and result |
|---|---|
| `python3 -B -m unittest discover -s scripts -p test_seed_scenarios.py` | 0; ten tests, including real orchestration order/arguments, original deadline, key isolation, private paths, finite failures, clock/partition mapping, restart/reset and exit-zero/false verifier rejection |
| `python3 -B -m unittest discover -s scripts -p test_stack_config.py` and quiet Compose config | 0; six tests, `docker compose -f deployments/compose/docker-compose.yml --env-file .env config --quiet` also 0; no resolved secrets printed |
| `timeout 60s scripts/go-test-nonempty.sh './tools/check-alerts/...' '^TestCheckAlertsManifest$' -race -timeout 60s` | 0; exact three-store bytes, missing/extra/wrong tenant/window/payload, historical-only, benign-zero and strict private-input guards |
| Same wrapper/package, separate `^TestCheckAlertsEventBatch$` selector | 0; explicit typed tenant/ID-array query, full row hashes, missing/extra/duplicate/wrong-tenant and corrupt-prior-hash rejection |
| Same wrapper/package, separate `^TestCheckAlertsCurrentEventRange$` selector | 0; malicious/benign historical rows rejected; equality accepted; prior historical hashing retained |
| Separate `./tools/detection-state/...` selectors `^TestDetectionStateClientBounds$` (race) and `^TestDetectionStateSnapshot$` | 0 each; actual no-network client construction plus unchanged strict snapshot/catch-up/retention/ambiguity/scan guards |
| Separate `./internal/detection/...` selector `^TestReplayEveryRestartCut$` | 0; 7.875s, full independent canonical fixtures (all rules, benign, boundary, lateness, combined and other tenant) plus all restart cuts |
| Separate `./internal/contracts/...` selector `^TestScenarioIdentityFixtures$` | 0; all nine cases, unchanged canonical IDs |
| Separate detection selectors `^TestFrozenEvidence$`, `^TestScanThresholds$` with race | 0 each; canonical evidence and original SCAN thresholds/order/bounds |
| `cd generator && timeout 60s uv run --locked pytest tests/test_scenarios.py -q` | 0; 45 passed, unchanged independent full payload fixtures |
| `timeout 300s make dev-up` | 0 initially; after interruption one network-failed attempt exit2, then one bounded normal retry exit0; all nine infrastructure/application containers healthy |
| `timeout 150s scripts/check-stack.sh` | 0 after healthy startup, including final resumed stack: all apps/ops, 6/3/1 topics, retention/RF, no-auto-create and actual protocols |
| `timeout 900s make seed` | 0; 1,000 sent/accepted/visible, zero retries, API spot checks and tenant isolation; also ordinary `python3 scripts/seed.py` succeeded inside final repeat |
| `python3 -B scripts/seed_scenarios.py --output /tmp/sentinel-plan13-first.json` | 0 earlier direct operator run; eight exact alerts, drained, zero new late drops; final hardened Make commands are exercised below |
| `timeout 32m scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenariosPreservedRepeat$' -tags=integration -timeout 32m -p 1` | 0 final frozen code; test315.63s/package333.091s. Actual legacy seed followed by both exact `timeout 16m make seed-scenarios SCENARIO_OUTPUT=... SCENARIO_PRESERVE=...` commands exited0 on the same topics/groups/volumes, retaining legacy and first-run rows, with disjoint IDs, both normal deltas zero and drained checkpoints |
| `timeout 16m scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenarios$' -tags=integration -timeout 16m -p 1` | 0 final frozen code; test559.28s/package582.668s. Isolated static boundary2.11s/lateness1.79s/all1.91s compare complete fixed canonical payloads through default binaries and actual stores. Preserved live edges verify ten exact alerts, drained checkpoints and exactly one explicitly intended late drop |
| `timeout 300s make test` | 0 on final functional code; full Go race suite, web six tests, generator121 tests (2.02s) |
| `timeout 300s make lint` | 0; Go zero issues, web eslint/typecheck, generator Ruff/format/mypy |
| `cd backend && timeout 120s go build ./... && timeout 120s go vet -tags=integration ./...` | 0; bounded production build and integration vet |
| Owned integration lint `go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./tools/check-alerts/... ./tools/detection-state/...` | 0, zero issues |
| Same integration lint including `./test/...` | 1; sole pre-existing `test/recovery_test.go:162` unparam finding remains for03-14; owned additions are clean |
| `git diff --check`, fixture/source/deletion/stub checks | 0; no unexpected source scope, deletion, runtime scaffold or generated golden |

Final preserved commands used `/tmp/TestDetectionScenariosPreservedRepeat3264482567/001/{legacy,first,second}.json` private test-owned paths. First proof recorded original start22:17:09.067Z, 1,117 unique acknowledged events and eight exact alerts; second original start22:18:23.232Z. Their normal scenario baselines are explicitly after legacy seed. Both complete command exits are proved by the final passing repeat, not inferred from historical row counts.

## Approved Deviations and Diagnosis

1. **[Rule1 — canonical payload mismatch]** The initial full canonical assertion in `backend/internal/detection/replay_test.go` failed solely on SCAN summary wording. Root approved preserving independent `Distinct destination ports probed` fixtures and changing that one runtime evidence string. Full `make test` then exposed precisely two old expectations in `evidence_test.go`/`scan_test.go`; root approved only those strings. Full canonical Go/Python and actual stores now pass; no ID, evidence, window, version, schema or golden change.
2. **[Rule3 — real observer constructor]** A read-only sanitized diagnostic established that existing admin-client default fetch size exceeded its broker-read cap before any network request. Permanent actual-constructor RED preceded one approved `FetchMaxBytes(scanFetchBytes)` option in `tools/detection-state/main.go`; all existing snapshot/retention/admission checks remain. Temporary diagnostic code was removed.
3. **[Rule1 — bounded exact reconciliation]** The first preserved repeat failed final reconciliation, not delivery. N+1 event reads consumed the 10-second attempt budget; a read-only 1,000-ID prior capture measured9.173s. Root approved explicit full-column typed batches, capped at5,000 IDs/rows and32MiB, keeping missing/extra/duplicate/tenant/hash guards. The equivalent capture measured0.872s. Existing `received_at ASC LIMIT1 BY event_id` is preserved; an initial proposed DESC description was corrected before implementation, without changing winner semantics. Budgets remained10s/60s/original15m.
4. **[Rule2 — current-run evidence]** Root approved the original private-core `run_start` plus parameterized received-at lower bound for current IDs only. Focused assertion RED demonstrated the historical-row acceptance gap, including benign-zero; strict UTC-ms/bounds and equality/history GREEN followed. Final repeat and pipeline acceptance use that field.
5. **Approved integration ownership:** `scenarios_preserved_test.go` now executes the actual Make commands with prior manifests. Historical fixtures use separate test-owned topics/groups/stores and explicit test-only past-skew with default binaries; no production/live event-time policy changed.
6. **Approved script-local reuse:** Reused the existing few pinned generator functions rather than adding a remapping public CLI/framework. Mapping occurs before snapshot keys/partition assignment/schedule; fixed shared fixtures and independent expectations remain binding.

These are scoped correctness/setup corrections with explicit parent approvals, not authorization for broader worker, Dockerfile, dependency, recovery or public-API changes. Karpathy guidelines kept changes surgical; Compose guidance informed health/dependency/resource ordering subject to the user's existing filename/image/volume requirements.

## Interruptions and Non-Passing Attempts

- Initial real setup attempts exited1 and exposed the observer constructor and an acknowledgment-field mismatch; they are not counted as behavioral RED. A premature stack checker failed before app startup, then passed after startup.
- Initial full `make test` failed the two old SCAN wording expectations described above, then passed after the narrow approved correction.
- Initial preserved repeat exited1 (test321.34s/package337.203s) during final reconciliation; first Make exit2, second never started. Measured N+1 diagnosis and the final passing replacement are recorded, not hidden.
- A diagnostic post-batch repeat passed (test219.70s/package238.722s) before the final original-start guard. It is not the final hardening proof.
- A later final-code repeat captured first command exit0 (~3m58s) and was interrupted during second startup; surviving private core had zero acknowledged current IDs. No overall/second success is inferred. The resumed attempt then exited1 before source send because the interrupted Compose infrastructure was stopped. Existing volumes were preserved.
- Normal resumed `make dev-up` initially exited2 when the existing pinned tern migration helper's proxy.golang.org downloads were reset. One bounded normal retry succeeded without dependency/version/Dockerfile changes. Final whole repeat was freshly rerun with new original start/deadlines; no expired schedule was reused.
- Existing 03-12 unexplained third-target timeout remains historical debt; later passing repeats here do not establish its cause.

## Threat Surface Check

The new private manifest/file access and existing store queries are within the plan's registered trust boundaries. No new network endpoint, authentication path, public schema or dependency was introduced. This scoped check is not the phase-wide security audit.

## Known Stubs and Remaining Scope

- Unchanged `Makefile:83` bench placeholder is intentionally Phase5-owned and outside this scenario path; no blocking runtime stub remains in this plan.
- Existing integration-test unparam cleanup and full CI/operator documentation remain03-14. Phase-wide threat/security review, hosted CI, all-phase requirement completion and the parent Wave13 gate are not claimed here.
- Requirements in frontmatter record this plan's contributions; global requirement checkboxes remain pending phase verification.
- User-owned `.planning/config.json`, `.serena/`, credentials and data remain unstaged/untouched by source commits. No reset/down-v/OPTIMIZE, broad cleanup, branch/worktree, new dependency installation, push or remote write was used.

## User Setup Required

None. Existing local Docker/pinned dependencies and private demo key handling were reused.

## Next Plan Readiness

03-14 can guard these now-executed acceptance selectors and document actual operator behavior. Stop after03-13; no Phase4 work was started.

## Self-Check: PASSED

All16 owned artifacts and this summary exist; all eight implementation/RED/GREEN commits resolve. Actual final16m and32m selectors exited0 on frozen source, with the commands/results above. No unexpected tracked deletion, altered golden/generator/worker recovery source, unresolved evidence placeholder or blocking scenario stub was found. User config and Serena remain unstaged.

