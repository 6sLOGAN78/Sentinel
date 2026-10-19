---
phase: 03-detection-alert-pipeline
plan: "08"
subsystem: detection
tags: [go, yaml, kafka, postgres, configuration, handover, tdd]
requires:
  - phase: 03-07
    provides: Strict bounded format-2 checkpoints and verified replay pins
  - phase: 03-19
    provides: Immutable decisions, current-membership fences and durable cooldown cursors
provides:
  - Complete finite YAML validation and order-independent semantic fingerprint
  - PG-saved rule manifest, acknowledged drain proof and recoverable authorized handover
  - Stopped-old-member startup admission, persisted aligned gates and documented operator procedure
affects: [03-09, 03-12, 03-20, 03-21, 03-13, 03-14]
tech-stack:
  added: []
  patterns: [Saved semantic manifest, post-Kafka acknowledged drain, pre-join stopped-member admission]
key-files:
  created:
    - backend/internal/detection/config_test.go
    - backend/internal/detection/transition_test.go
    - backend/internal/detection/transition.go
    - backend/internal/config/detection_test.go
    - backend/test/detector_config_test.go
    - backend/migrations/postgres/005_detection_config.sql
    - docs/rules.md
  modified:
    - backend/internal/detection/config.go
    - backend/internal/detection/checkpoint.go
    - backend/internal/config/config.go
    - backend/internal/storage/postgres/detection_decisions.go
    - backend/internal/kafkax/detector_consumer.go
    - backend/internal/detector/worker.go
    - backend/cmd/detector/main.go
    - .env.example
key-decisions:
  - "03-08: Save actual prior rule definitions and exact acknowledged drain in existing PG partition fences; never infer changed-rule versions from a fingerprint/version vector alone."
  - "03-08: Changed-config startup explicitly requires zero old members before joining, then sole membership after joining; unchanged-config replicas retain normal multi-member operation."
  - "03-08: Persist both handover checkpoints so PG-before-Kafka failure resumes exactly the authorized four gates without resetting the stable group/fence namespace."
  - "03-08: Idle checkpoint restoration defers only demonstrably changed assignment snapshots; nonempty source processing keeps strict current-owner fencing."
requirements-completed: [DET-05, DET-06, ALRT-03]
duration: 31 min
completed: 2026-10-19
---

# Phase 3 Plan 8: Strict Rules and Drained Configuration Handover Summary

Strict startup rules now bind to a saved semantic manifest and an acknowledged PostgreSQL/Kafka handover, with aligned window gates that prevent historical alert-ID reuse.

## Performance

- Started: 2026-10-19T04:58:30Z
- Verification completed: 2026-10-19T05:29:13Z
- Duration: approximately 31 minutes, including fixture correction, runtime rebalance diagnosis and verification.
- Tasks: 3/3. Files: 15 implementation/test/operator files, plus this summary and serialized tracking.

## Accomplishments

- Strict one-document YAML accepts only the exact complete four-rule schema, with a 64 KiB cap, explicit scalar types, no aliases/anchors/merges/duplicate keys or IDs, finite durations/counts/version ranges, supported IDs/severities and whole-millisecond precision. All configuration errors omit file bodies.
- Fingerprints sort copied definitions and include all parsed rule settings plus explicit fixed predicate/window/evidence/cooldown/identity policy versions, lateness and admission/evidence bounds. YAML ordering cannot change identity. Every changed rule requires its own strictly increasing version; all versions are monotone.
- Additive migration 005 stores the prior canonical rule manifest and exact acknowledged drain/handover proofs on existing partition fences. No new table, group incarnation, ID encoding or arbitrary fingerprint reset was introduced. Stable group/topic/partition locks serialize handover; old decisions and old cursor namespaces remain intact.
- PG authorization requires the matching saved drain, no active pin and no uneffected PG-ahead decision. PG-ahead unchanged recovery remains allowed and does not falsely certify a drain. New gates use the maximum verified old sealed floor and retain prior gates conservatively; each is rounded up to its new rule duration. All four persist through checkpoint/restart. Intentional straddling-window gaps preserve DedupeKey's exclusion of rule version.
- An explicit transition flag first checks zero existing members with a bounded admin-only client before creating the consuming client. A post-join sole-member check remains, including zero-assignment members. Normal unflagged replicas coexist and rebalance. Empty assignments acquire real acknowledged checkpoints from verified retained starts; idle handovers no longer require an extra source event.
- Operators have actual rules/defaults, limits, cross-window cooldown, immutable allow/suppress replay, retention/backup consistency, no automatic ledger GC, marker scheduling, transition gaps, stopped-replica and failure-recovery instructions in docs/rules.md. Environment examples contain safe placeholder values only.

## Task Commits

1. Task 1 — compileable behavior scaffolds and assertion RED: `e53d4b5`.
2. Task 2 — strict canonical rules, saved manifest and durable generation authorization: `29289eb`.
3. Task 3 — real restore/drain lifecycle, stopped-member admission, idle rebalance repair and operator docs: `e9d124e`.

## Verification and TDD Evidence

Each command below used scripts/go-test-nonempty.sh independently for its named family. Unit families used `./internal/detection/... ./internal/config/...` and `-timeout 60s`; the window/ledger selections also ran independently against their owning detection package. No compile/setup failure is counted as RED.

| Exact family / command | RED exit | Final exit | Evidence |
|---|---:|---:|---|
| `^TestRulesConfig$` | 1 | 0 | Aliases/merges, missing/null enabled and sub-ms cooldown initially accepted; complete strict loader now rejects them and all malformed/oversized/overflow cases |
| `^TestConfigFingerprint$` | 1 | 0 | Reversed semantic rule order initially produced a different hash; ordering and every decision setting are now covered |
| `^TestConfigDrain$` | 1 | 0 | Unchanged/drained handover success, same-version edits, active metadata, mixed manifest and rollback |
| `^TestDetectorSettings$` | 1 | 0 | Blank rules path initially accepted; independent topics/groups/path, explicit transition flag and unrelated service independence pass |
| `^TestConfigWindowEnlargement$` | 1 | 0 | 300→600 and shrinking windows, shared sealed floor, four gates, skip below gate and admit exact equality |
| `^TestConfigLedgerHandover$` (unit) | 1 | 0 | Unacknowledged effects cannot yield a checkpoint; new frontier/gates prevent old identity reuse |
| `./test/... '^TestDetectorConfigRestart$' -tags=integration -timeout 6m -p 1` | 1 | 0 | Corrected baseline RED 43.827s; final current-source GREEN 41.180s (22.00s actual test) |
| `./test/... '^TestConfigLedgerHandover$' -tags=integration -timeout 6m -p 1` | — | 0 | Final PG proof 22.853s; prior post-tag roundtrip rerun 19.978s |
| `./test/... '^TestDetectionPipelineAuth$' -tags=integration -timeout 6m -p 1` | — | 0 | Existing actual AUTH publication/evidence regression |
| `./test/... '^TestDetectionPipelineScan$' -tags=integration -timeout 6m -p 1` | — | 0 | Existing actual SCAN regression; this does not resolve the independent shared-summary fixture debt |
| `./test/... '^TestDetectionPipelineVolume$' -tags=integration -timeout 6m -p 1` | — | 0 | Existing FLOOD/DENY pipeline regression |
| `./test/... '^TestDetectorCooldownReplay$' -tags=integration -timeout 8m -p 1` | — | 0 | Immutable replay, current-owner rejection, matching frontier/PG-ahead and real PG lock failure retaining exact broker checkpoint; 37.741s |
| `./test/... '^TestDetectorCheckpoint$' -tags=integration -timeout 6m -p 1` | — | 0 | Actual pre-pin stored-byte replay and terminal malformed/missing metadata; 33.379s |
| `./test/... '^TestDetectorRetention$' -tags=integration -timeout 6m -p 1` | — | 0 | Trimmed pin, explicit new-group retained start and checkpoint beyond log end; 22.808s |
| `timeout 300s make test` | — | 0 | Full Go race suite, web 6 tests and generator 99 tests; repeated after runtime repair |
| `timeout 300s make lint` | — | 0 | Standard Go 0 issues, web lint/typecheck and generator Ruff/format/mypy |
| `cd backend && timeout 180s go build ./... && timeout 180s go vet -tags=integration ./...` | — | 0 | Final source build/integration vet; both succeeded before the following integration-lint command |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | — | 1 | Sole remaining finding is pre-existing recovery_test.go:162 unparam, owned by 03-14 |
| `git diff --check`, artifact/commit/deletion/stub checks | — | 0 | All claimed artifacts and ordered commits exist; no tracked deletion or unfinished runtime stub |

The real restart acceptance observes old stored-byte replay, rejects valid increased-version active metadata and equal-version drained edits, and obtains a genuine marker drain at 300s. It proves two unchanged idle replicas coexist and one leaves normally. A changed-config startup then fails before joining and preserves the exact old member ID. After that member stops, one new detector adopts all four gates at 600s, produces no alert for accepted-time traffic at 400s, produces one version-2 alert at exact gate equality, and republishes identical version-2 stored bytes after an unchanged restart. No historical alert is recreated by the transition.

The real PG proof applies the normal migration runner twice, verifies saved semantic settings rather than version-vector inference, rejects fake/active drains and a same-version edit to another rule, retains old allow/suppress bytes, fences the old owner, and verifies old cursors remain while new generation cursors begin empty. Simulating PG authorization before Kafka checkpoint adoption returns exactly the saved new checkpoint on retry. Dropping a persisted gate while retaining the new fingerprint is rejected.

## Deviations from Plan

**1. [Rule 2 — Required semantic and acknowledgement plumbing] Extend existing fence columns, worker and checkpoint serialization.** The parent approved worker.go, additive 005_detection_config.sql and explicit Checkpoint JSON tags before edits. A fingerprint/version vector cannot establish which old rule changed, and only the worker observes fully effected outputs plus the successful broker callback. Full manifests/proofs therefore live in PG; the 1,024-byte broker envelope is unchanged. Reducer.go ownership was approved but no edit was needed: the focused transition module accesses the reducer's existing state directly.

**2. [Rule 1 — Idle rebalance race] Preserve normal multi-member operation while restoring idle state.** Actual old-member logs showed `detector partition ownership lost` during the initial mixed probe: empty polls do not hold Kafka's rebalance gate, so an assignment snapshot can change during idle restore. Idle sync now defers only when current membership/token demonstrably differs from its captured snapshot. Stable incompatibilities still fail, and record-bearing processing retains strict fences. The real two-member coexistence/leave test passes. Parent cancellation during bounded idle restore is normal lifecycle shutdown.

**3. [Rule 2 — Stopped-replica admission] Add bounded non-consuming pre-join admission.** A new member's join could otherwise make an old process leave before a later sole-member snapshot. The parent approved checking zero old members before constructing the consuming client, plus retained post-join/concurrent-arrival checks. The real rejection preserves the original member ID. This is deliberately a conservative stopped-deployment procedure, not a rolling/multi-replica config handover claim.

## Issues Encountered

- The initial real RED fixture replaced the first substring `version: 1`, unintentionally changing schema_version. Corrected replacements target the indented rule key. That initial run is not treated as valid handover RED. A temporary git-archive of e53d4b5 with only that correction compiled and reached the actual drained-adoption assertion failure (exit 1, 43.827s); no branch/worktree/stash/reset occurred.
- Expanded runtime assertions initially observed an unconsumed old-config replay in the alert observer. The fixture now consumes and compares that old replay before asserting that handover itself creates no historical output.
- A second real process needs its own ops port; the first mixed probe was rejected for port collision and is not counted as membership proof. The corrected probe uses a dedicated port and exact rejection text/member identity checks.
- The final unchanged restart now observes actual stored-byte replay before termination instead of treating the service-start log as completed restoration. The runtime also treats parent cancellation during idle work as normal shutdown.
- Owned complexity/format/tag findings were resolved through focused helpers and explicit JSON tags. Startup rules-file access is intentionally an operator-owned path; a narrowly documented gosec annotation covers that existing capability after helper extraction. One early formatting command used a root-relative path while already inside backend; corrected immediately and not counted as verification.
- Context7 transport failed and ctx7 was absent. Installed pinned `go doc`/source verified yaml.Node, kadm.DescribeGroups/GroupMetadata and explicit pgx transaction cleanup; no package installation occurred.
- No authentication gate, remote write, destructive cleanup, existing-stack reset or credential disclosure occurred. Existing .planning/config.json and .serena changes remain untouched.

## Known Stubs and Threat Review

No unfinished production scaffold remains. Empty fresh state, a none frontier before the first new-generation decision, nullable initial clocks and empty skipped-window outcomes are intentional protocol states. No public endpoint or hot reload was added.

T-03-08-01 is covered by the capped exact node/field/type parser, canonical semantic identity and bounds. T-03-08-02 is covered by the actual prior PG manifest, exact acknowledged drain, stable partition locks and current ownership, pre-/post-join deployment admission, immutable history and saved four-rule gates. T-03-08-03 preserves bounded reads/deadlines, sanitized errors and safe examples. The additive existing-fence schema and admin-only Kafka membership request are within the approved configuration/recovery trust boundary. No new dependency or open high finding was identified in this slice.

## Next Plan Readiness

- 03-08 is implemented; the parent owns the fresh Wave 8 gate. Serialized tracking advances from 13/21 to 14/21 summaries only after this summary is committed.
- Frontmatter requirements identify this plan's requirement slices. Global REQUIREMENTS checkboxes and Phase 3 completion remain unchanged; broader late/state-bound, poison, crash/ownership/outage, live scheduling, preserved-stack, security and CI proofs belong to later plans.
- Existing integration lint recovery_test.go:162 unparam remains 03-14 debt. Independent shared SCAN summary wording remains the known 03-13/14 acceptance reconciliation; these passing runtime regressions do not claim a full independent canonical scenario fixture pass.
- Operational history has no automatic GC, and changed configuration requires the documented stopped-member procedure and intentional aligned gap. Fixed binary semantic changes that cannot validate a saved prior manifest fail closed and need a compatible maintenance migration.

## Self-Check: PASSED

- All seven new and eight modified implementation/test/operator artifacts exist.
- e53d4b5, 29289eb and e9d124e exist in assertion-RED-before-production-GREEN order; no tracked files were deleted.
- Six independent unit families, corrected real baseline RED, final real restart/PG handover and all six affected integration families have actual recorded results.
- Full unit/race and standard lint pass; the sole pre-existing integration-lint debt is explicitly retained.
- The summary is committed before serialized STATE/ROADMAP mutation. No global requirement/phase-complete operation is performed.
