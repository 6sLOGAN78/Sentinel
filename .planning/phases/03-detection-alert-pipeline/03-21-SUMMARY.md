---
phase: 03-detection-alert-pipeline
plan: "21"
subsystem: generator
tags: [go, kafka, python, scheduling, private-protocol, tdd]
requires:
  - phase: 03-06
    provides: Fixed deterministic scenario construction, exact expectations and unchanged sender
  - phase: 03-08
    provides: Strict bounded format-2 checkpoint validation and current rules fingerprint
provides:
  - Private read-only six-partition snapshot with stable source ends and caught-up detector checkpoints
  - Live scenario schedules, exact derived windows, actual source-key partition mapping and bounded waits
  - Preserved seed/scenario/repeat acceptance scaffold for execution by 03-13/14
affects: [03-13, 03-14]
tech-stack:
  added: []
  patterns: [Read-only catch-up admission, separate live schedule protocol, one orchestration deadline]
key-files:
  created:
    - backend/tools/detection-state/main_test.go
    - generator/tests/test_live_schedule.py
    - backend/test/scenarios_preserved_test.go
    - backend/tools/detection-state/main.go
    - backend/tools/detection-state/catchup.go
    - generator/src/sentinel_generator/live_schedule.py
  modified:
    - generator/src/sentinel_generator/cli.py
key-decisions:
  - "03-21: Retained-history scans reconstruct maxima only; nonempty live admission still requires compatible format-2 detector processed_next equal to stable source end."
  - "03-21: Use sealed_through plus lateness, explicit empty partitions, actual keyed partition mapping and schema-2 private live schedules; retain fixed fixtures unchanged."
  - "03-21: Reserve each unchanged 120-second/five-attempt delivery plus request allowance, use interruptible clock waits and enforce the shared 15-minute deadline through a POSIX main-thread timer."
requirements-completed: [GEN-02, DET-09, ALRT-03]
duration: approximately 18 min
completed: 2026-10-19
---

# Phase 3 Plan 21: Caught-up Live Scenario Scheduling Summary

Private Kafka snapshots now anchor live threats beyond processed partition maxima, with legal marker delivery, exact window expectations and a finite shared run deadline.

## Performance

- Approximately 18 minutes elapsed from the preceding serialized tracking commit at 2026-10-19T07:00:35Z through the implementation commit/final verification at 07:18:39Z. A distinct dispatch timestamp was not captured; this is an elapsed estimate, not measured active time.
- First RED commit: 2026-10-19T07:07:30Z. Tasks: 2/2. Application/test files: seven.

## Accomplishments

- `detection-state` uses bounded franz-go admin reads and direct partition readers. It never joins the detector group, commits offsets, creates a replacement group, publishes records or resets existing state. The current strict rules file supplies the expected fingerprint; existing `detection.CheckpointMetadata` validates the complete format-2 envelope.
- All six source start/end bounds must be complete. Ends and retention starts must stay unchanged during catch-up and the final read. Admission uses `processed_next == snapshot_end`, independent of replay pin. Initialized idle/drained checkpoints work without active windows. The caught-up nonempty maximum is `sealed_through + 30s`, explicitly distinct from the pre-pin maximum. Only a truly empty zero-offset source can have `empty=true, max_event_time=null`; a nonempty prefix without a safe clock refuses.
- Missing checkpoints with complete retained history trigger capped reconstruction and remain pending until a valid detector checkpoint arrives. A scan never fabricates detector effect completion. Missing checkpoints after trimming, corrupt/incompatible metadata, pin outside retained bounds, gaps/truncation, moving producers and exhausted bounds refuse. Reconstruction supports retained-earliest or pin-to-end scans seeded by the pre-pin maximum, applies the detector's event/key/received-at validation and skips poison clocks. The aggregate scan is capped at 100,000 records/32 MiB; scan and catch-up share the caller's deadline and at most 60 seconds. Read buffers and output are separately bounded.
- The private request has an exact five-field object, rejects duplicates/unknown/null/missing fields, validates canonical tenant/IP source keys and caps stdin/stdout at 64 KiB. Kafka configuration and rules path come from the existing service environment names. Terminal diagnostics omit credentials, broker errors, source payloads and source keys. The caller owns private pipe/file handling; Python reuses no-follow mode-0600 manifest writing and creates new output directories at mode 0700.
- The live base is `max(now UTC milliseconds, all processed maxima + 1ms)`. Existing scenario construction preserves same-millisecond contributors, actual epoch-aligned windows and immutable expected bytes. Lateness's local anchor shifts one millisecond so its earliest input still meets the admitted base. All threats precede nonmatching `auth.success` markers on every touched source key at the shared latest relevant window end plus 30 seconds. Key-to-partition mappings use the same keyed murmur2 policy as the existing producer's pinned franz-go default.
- An explicit AUTH adjacent-cooldown mode constructs crossings two milliseconds apart across one boundary, records both candidate windows and expects only the first allowed alert. Ordinary live modes and historical fixture construction remain separate; neither `scenarios.py` nor the shared JSON goldens changed.
- Schema-2 schedules bind identity, snapshots, actual touched partitions, exact ordered batch IDs, window decisions and derived send-not-before times. Future skew defaults to 300 seconds with a two-second margin, configurable through the schedule command. Before any injection, preflight reserves every remaining delivery's unchanged 120-second/five-attempt sender budget plus five 10-second request allowances. Waits and retry sleeps use steps no longer than one second and propagate interruption. One POSIX main-thread timer enforces the original run deadline even during a trickling/stalled HTTP response; unsupported timer contexts refuse before injection. Ready-only private schema-1 input remains compatible with the existing isolated fixture tests.

## Task Commits

1. Task 1 — compileable declarations, snapshot/fake-clock assertion RED and future preserved-repeat scaffold: `ae48ca8` (`test`).
2. Task 2 — private snapshot, scheduler, CLI, strictness guards and focused GREEN: `f404916` (`feat`).

No tracked file was deleted. User-owned `.planning/config.json` and `.serena/` remain unchanged and unstaged.

## Verification and TDD Evidence

Commands ran from the repository root unless noted. Go family selection always used its own `scripts/go-test-nonempty.sh` invocation. Import/type/setup failures do not count as RED.

| Command | Actual exit / result |
|---|---|
| `scripts/go-test-nonempty.sh './tools/detection-state/...' '^TestDetectionStateSnapshot$' -timeout 60s` before behavior | 1; compiled, 0.006s: explicit empty, idle, pin-versus-processed, catch-up and reconstruction assertions fail on typed error returns; private-protocol rejection assertion fails on nil return |
| `cd generator && timeout 60s uv run --locked pytest tests/test_live_schedule.py -q --tb=short` before behavior | 1; imports succeed, 19 assertion failures in 0.37s: no live scenario/schedule and missing refusal behavior |
| `cd generator && uv run --locked pytest tests/test_scenarios.py tests/test_send.py -q` during RED | 0; all 90 pre-existing scenario/sender guards pass in 0.56s |
| `cd generator && timeout 60s uv run --locked pytest tests/test_live_schedule.py -k private_schedule_cli -q --tb=short` before CLI wiring | 1; 1 failed/20 deselected, 0.30s: actual CLI lacks schedule command, checked via returned exit assertion |
| Separate snapshot family during strictness refinement | 1; 0.039s: real protocol roundtrip succeeds, then duplicate quiescent field is incorrectly accepted; rejection fixed before final GREEN |
| Final `scripts/go-test-nonempty.sh './tools/detection-state/...' '^TestDetectionStateSnapshot$' -timeout 60s` | 0; 0.035s: empty/idle/pinned progress, bounded wait, missing-metadata reconstruction followed by detector catch-up, corrupt/trimmed/moving/unsafe state, gap/record/byte/cancellation limits and strict private protocol |
| `cd generator && timeout 60s uv run --locked pytest tests/test_live_schedule.py -q` | 0; 22 passed in 0.75s |
| `cd generator && timeout 60s uv run --locked pytest tests/test_live_schedule.py tests/test_scenarios.py tests/test_send.py -q` | 0; 112 passed in 1.54s |
| `timeout 300s make test` | 0; full Go race suite, web six tests and generator 121 tests pass; helper race test 1.184s, Python 1.32s |
| `timeout 300s make lint` | 0; Go zero issues, web lint/typecheck and Python Ruff/format/mypy pass |
| `cd backend && timeout 180s go build ./... && timeout 180s go vet -tags=integration ./...` | 0; production build and integration vet pass |
| `cd backend && timeout 180s go test -c -tags=integration -o /tmp/sentinel-plan21-integration.test ./test` | 0; future preserved-run test compiles; compilation is not execution |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./test/...` | 1; final sole issue is pre-existing `test/recovery_test.go:162` unparam, retained for 03-14; owned formatting/context issues corrected |
| `git diff --check`, artifact/commit/deletion/stub checks | 0; seven owned artifacts and both task commits exist, no unexpected deletion or unfinished runtime declaration |

The fake-clock sender test uses the real unchanged delivery function, identical serialized retry bytes, a full 120-second Retry-After and legal event-clock assertions. A separate real 30ms timer interrupts a blocking 500ms wait. These are not real Kafka/ingest/CH/PG or preserved-volume acceptance claims. The Kafka adapter is compiled/vetted; snapshot behavior is exercised through a read-only fake observer.

## Private Operator Contract for 03-13

Run the Go helper from `backend/` with no command-line credentials or flags: `go run ./tools/detection-state`. Supply private stdin JSON with exactly:

```json
{"schema_version":1,"producers_quiescent":true,"source_keys":["demo|192.0.2.11"],"started_at":"2026-10-19T07:00:00Z","deadline":"2026-10-19T07:15:00Z"}
```

Times above illustrate shape only. Plan 13 must use current time and one original deadline, map every selected scenario key, keep other producers quiescent through marker acknowledgement and persist the bounded stdout snapshot privately. The quiescent assertion does not replace independently checked stable source ends. Source output includes six `source_snapshots`, actual `source_partitions`, original `snapshot_at`/`deadline`, schema version 1 and the quiescent flag. Helper environment names are `SENTINEL_KAFKA.BROKERS`, `SENTINEL_KAFKA.EVENTS_TOPIC`, `SENTINEL_KAFKA.DETECTOR_GROUP` and `SENTINEL_DETECTOR.RULES_PATH`, matching current services.

From `generator/`, invoke `uv run --locked sentinel-generator schedule --snapshot <private-snapshot> --scenario <name> --seed <seed> --tenant <tenant> --batch-size 500 --future-skew-seconds <actual-ingest-skew-seconds> --output <private-schedule>`, optionally `--adjacent-cooldown` for AUTH. Then use `sentinel-generator send` with matching scenario/seed/tenant/batch-size, `--schedule` and `--output`. Keys enter through the existing environment/private-file path. The scheduler refuses an impossible total budget before key loading/injection. Default live batch size 500 avoids exhausting the total budget through many small deliveries.

`TestDetectionScenariosPreservedRepeat` is created now and intentionally **not executed**. Plans 13/14 must supply and run its operator path:

1. Run ordinary `python3 scripts/seed.py`; preserve `.local/last-send.json` privately.
2. Run `python3 scripts/seed_scenarios.py --output <first-manifest> --preserve-manifest <legacy-manifest>`.
3. Run the same operator command again with a second output and the first manifest as the preservation input, using the same topics/group/volumes.

The future script/verifier must produce private manifests with nonempty fresh `event_ids`, `verified=true` from actual current-run Kafka/CH/PG reconciliation, `prior_rows_preserved=true`, explicit `new_late_drops=0` and `drained=true`. The test checks these fields, permissions and disjoint successive scenario event identities. Plans 13/14 own the actual independent store/frontier/metric proof behind those fields, existing legacy seed behavior, noncolliding current-run entities, the 32-minute repeat ceiling and final verifier. They may refine invocation while retaining these obligations; an absent script is never an acceptable RED or skipped success.

## Deviations from Plan

- [Rule 2 — Safe catch-up clarification] The parent explicitly confirmed that a capped scan can reconstruct a maximum but cannot prove detector effects. Nonempty admission therefore still waits for compatible format-2 metadata at the stable end; permanently missing metadata refuses within the deadline. A dedicated missing-metadata-to-checkpoint test demonstrates this distinction.
- [Rule 3 — Loadable Python RED] The Python declaration-only module was created with Task 1 so independent Python tests could import and reach assertions before behavior. This remains within the plan's seven owned files.
- [Rule 2 — Enforce aggregate delivery deadline] HTTP read timeouts are inactivity bounds. Added a standard-library POSIX main-thread timer to enforce the same original deadline during blocking delivery; unsupported contexts fail before injection. Sender retry semantics and dependencies remain unchanged.

No architecture, schema, public API, dependency, runtime group/fence, alert identity or fixture-golden change was introduced. Karpathy guidance kept changes within the private helper/scheduler/scaffold scope. No authentication gate or package installation occurred.

## Known Stubs and Threat Review

No runtime stub remains. The preserved-repeat test's future script invocation is the intentional 03-13/14 dependency specified by this plan, not an implemented or executed acceptance result. Empty source clocks, missing pending checkpoints and dry-run empty fields are explicit states.

- T-03-21-01: strict existing metadata/current fingerprint, independently stable end/start offsets, caught-up processed-next, explicit empty representation, conservative reconstruction and exact private schedule reconstruction.
- T-03-21-02: 100k-record/32-MiB/60-second scan, 60-second catch-up, bounded input/output, precomputed sender/wait allowances, one-second interruptible sleeps and hard shared run timer; excessive future state refuses.
- T-03-21-03: existing environment-only service configuration, private stdin/stdout protocol and manifest handling, sanitized terminal diagnostics, no new public listener or dependency.

No open high finding was identified for this slice; no phase-wide security certification is claimed.

## Remaining Dependencies

- Real ordinary-seed → scheduled-scenario → repeated-scenario on preserved topics/group/volumes, exact current-run Kafka/CH/PG reconciliation, no new late drops and prior rows preserved remain unexecuted obligations of Plans 13/14.
- The independent shared SCAN summary still expects `Distinct destination ports probed`; runtime emits `Distinct destination ports on one host`. Neither goldens nor runtime wording changed here. Passing Python fixed-fixture tests do not settle the whole cross-language canonical payload acceptance debt.
- Existing integration lint `recovery_test.go:162` remains for 03-14. Full detector crash/recovery/outage/ownership/security/CI matrices and global requirement completion remain pending. Frontmatter requirement IDs record this plan's contribution only; no global requirement checkbox is advanced.
- Sixteen of twenty-one Phase 3 summaries will exist after this summary. The parent owns the fresh Wave 9 gate and subsequent authorized Phase 3 execution; stop before Phase 4.

## Self-Check: PASSED

All seven owned source/test files exist. Commits `ae48ca8` and `f404916` exist in RED-before-GREEN order. Final named unit/Python gates, full make test/lint, build, integration compile and vet passed with the sole existing integration-lint debt explicitly recorded. No tracked file was deleted. This verified summary is committed before serialized STATE/ROADMAP updates.
