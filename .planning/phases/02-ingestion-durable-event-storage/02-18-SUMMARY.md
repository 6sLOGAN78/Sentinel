---
phase: 02-ingestion-durable-event-storage
plan: "18"
subsystem: generator
tags: [python, filesystem, manifests, credentials, gap-closure]
requires:
  - phase: 02-13
    provides: Exact seeded run manifests and private ensured demo credentials
provides:
  - Atomic private manifests anchored to no-follow directory descriptors
  - Existing-parent permission preservation and unsafe-path rejection
  - Seed child environment isolated from ambient producer credentials
affects: [02-21, GEN-01, GEN-03]
tech-stack:
  added: []
  patterns: [descriptor-relative filesystem operations, explicit sanitized child environment]
key-files:
  created: []
  modified:
    - generator/src/sentinel_generator/cli.py
    - generator/tests/test_send.py
    - generator/README.md
    - scripts/seed.py
    - scripts/test_seed.py
key-decisions:
  - "Never chmod existing manifest directories or path-resolved destinations; create new directories at 0700 and validate them through no-follow descriptors."
  - "Anchor temporary creation, replacement, fsync and cleanup to the opened parent so path renames cannot redirect writes."
  - "Remove SENTINEL_API_KEY only from the seed generator child's environment; retain standalone environment-first precedence and the parent environment."
patterns-established:
  - "Output safety: atomically replace the requested regular manifest using a private temporary inode, without following output symlinks."
  - "Credential isolation: preserve ordinary child environment values while excluding the ambient key overridden by an ensured private key file."
requirements-completed: ["GEN-01","GEN-03"]
duration: 12 min
completed: 2026-10-16
---

# Phase 02 Plan 18: Safe Manifest Paths and Seed Credentials Summary

Manifest output preserves existing directory permissions and rejects symlink paths, while seeded sends reliably use their freshly ensured demo key despite a conflicting shell credential.

## Performance

- Started: 2026-10-16T09:10:25Z
- Completed: 2026-10-16T09:22:14Z
- Duration: 12 min
- Tasks: 2/2
- Files modified: 5, plus this summary

## Accomplishments

- Replaced path-based manifest writes/chmods with no-follow directory traversal and descriptor-relative temporary creation, atomic replacement, directory fsync and cleanup. Only new directories are created at mode 0700; existing directory modes remain unchanged. Successful manifest files are mode 0600.
- Added command-level regressions for absolute/relative existing 0755/0775 parents, private nested directories, atomic replacement, symlink parents/ancestors/output, non-directory parents, nonregular destinations, serialization/fsync/replace failures, parent swaps, directory-creation swaps and unavailable no-follow support.
- Passed an explicit copied environment to the seed generator, excluding only SENTINEL_API_KEY and retaining its ensured --key-file. The orchestration regression uses real private temporary key/manifest files across two runs and inspects the exact child environment, parent environment, key inode reuse, API identity and secret-free output/argv/manifest.
- Documented manifest-parent safety and the seed-specific environment choice. Standalone environment-first selection remains verified.

## Task Commits

1. Task 1 RED: `7a48749` — test(02-18): expose unsafe manifest parent mutation.
2. Task 1 GREEN: `6802101` — fix(02-18): anchor atomic manifests to safe directory descriptors.
3. Task 1 supplemental safety/standalone regressions: `9c94ce7` — test(02-18): cover directory creation swaps and key precedence.
4. Task 2 RED: `01afe9d` — test(02-18): expose ambient seed credential inheritance.
5. Task 2 GREEN/documentation: `a7054c8` — fix(02-18): isolate seed generator credentials from ambient shell.

The commits interleave only to add the supplemental Task 1 safety tests while Task 2 was in progress; each source repair follows its observed behavioral RED commit.

## Verification

| Command | Exit | Observed evidence |
|---------|------|-------------------|
| `cd generator && uv run --locked pytest tests/test_send.py -k test_manifest_preserves_existing_parent_mode` before repair | 1 | Four behavioral assertion failures: existing 0755/0775 absolute/relative parents became 0700; 0.24 s pytest duration. |
| `python3 -B -m unittest scripts.test_seed.CommandTests.test_generator_environment_ignores_ambient_key` before repair | 1 | Behavioral assertion failed because the generator's captured env was None; 0.003 s unittest duration. |
| `cd generator && uv run --locked pytest tests/test_send.py -k manifest` after all additions | 0 | 18 selected manifest/precedence cases passed; 0.17 s. |
| `python3 -B -m unittest discover -s scripts -p test_seed.py` final | 0 | All 11 seed tests passed; 0.004 s. |
| `cd generator && uv run --locked pytest` final | 0 | All 54 generator tests passed; 0.16 s. Deterministic generation, exact IDs, bounded retry behavior and identical request bytes remain covered. |
| `cd generator && uv run --locked ruff check .` final | 0 | All checks passed. |
| `cd generator && uv run --locked ruff format --check .` final | 0 | Eight files already formatted. |
| `cd generator && uv run --locked mypy src tests` final | 0 | No issues in seven source files. |
| `git diff --check --` the five owned files | 0 | No whitespace errors. |

The final combined seed/generator/lint/type command completed in 0.57 seconds of tool wall time. All checks were one-shot. No integration, Docker, or live seed run was repeated here; Plan 02-21 owns the final real two-run acceptance gate. No hosted CI success is claimed.

## Decisions Made

- Create new manifest directories with mkdir mode 0700 and validate the opened directory's exact mode instead of chmodding an inode that could have been swapped in after creation. Restrictive modes or unsupported safe filesystem primitives fail with sanitized errors.
- Validate the output as absent or regular without following symlinks both before temporary creation and before replacement. Atomic rename replaces the directory entry; it never opens/chmods the destination target.
- Keep standalone CLI precedence unchanged. Only seed's generator child receives the copied environment without SENTINEL_API_KEY; PATH and ordinary environment values remain intact.

## Deviations from Plan

None. Additional directory-creation-swap, fsync-failure and standalone-precedence cases strengthen the planned safety boundaries without expanding scope.

## Issues Encountered

The first post-repair check found one overlong test decorator; formatting corrected it before the successful lint/type gate. No unresolved finding remains within this plan's scope. Filesystem fault tests verify cleanup before replacement; a failure syncing the containing directory after an atomic replacement correctly reports failure without claiming rollback or durability.

## Known Stubs

None in the five changed files. A scan for TODO, FIXME, placeholder, coming-soon and unavailable placeholders returned no matches.

## User Setup Required

None.

## Next Phase Readiness

CR-02 / verification gap 5 and WR-01 / gap 7's ambient-key subgap have behavioral repair evidence. Independent phase assessment and final real API-to-storage seed verification remain owned by the orchestrator and Plan 02-21. Shared STATE, ROADMAP, REQUIREMENTS and user configuration were intentionally left to the orchestrator's single-writer tracking.

## Self-Check: PASSED

- All five modified files exist.
- All five task commits listed above exist in git history, and no task commit deletes tracked files.
- Both named regressions failed on behavior before their repairs; the same boundaries passed afterward.
- Final seed, generator, lint, format and type checks exited 0.
- SDK key-link verification passed 2/2 after summary commit; task commit objects and deletion checks were confirmed.
- Only assigned files were staged. Concurrent worker changes, user .planning/config.json and .serena contents were not staged or reverted. No volume mutation occurred.

---
*Phase: 02-ingestion-durable-event-storage*
*Completed: 2026-10-16*
