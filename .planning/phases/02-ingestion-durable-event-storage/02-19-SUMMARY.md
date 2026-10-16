---
phase: 02-ingestion-durable-event-storage
plan: "19"
subsystem: packaging-and-ci
tags: [docker, buildkit, credentials, go, dependencies, ci]
requires:
  - phase: 02-13
    provides: Phase 2 seed and seven independent CI integration groups
  - phase: 02-15
    provides: TestWriterSlowPoisonRebalance causal regression
provides:
  - Real BuildKit fixture export proving nested private material is excluded before COPY
  - PEM exclusion-removal regression using Docker's actual context matcher
  - Shared bounded dependency guard for default and integration Go graphs
  - CI wiring preserving behavioral groups and requiring the rebalance causal test
affects: [02-21, phase-verification, local-development, ci]
tech-stack:
  added: []
  patterns: [synthetic private build-context fixtures, dual-build dependency enumeration, sanitized fail-closed checks]
key-files:
  created:
    - scripts/check-docker-context.py
    - scripts/test_docker_context.py
    - scripts/check-go-dependencies.sh
    - scripts/test_go_dependencies.py
  modified:
    - .dockerignore
    - .github/workflows/ci.yml
key-decisions:
  - "Use a private synthetic FROM scratch BuildKit export with no external frontend or base image to verify Docker's actual ignore semantics."
  - "Capture default and integration go-list outputs independently with a 60-second ceiling per enumeration before matching prohibited imports."
requirements-completed: [GEN-03, ING-07]
duration: 3 min
completed: 2026-10-16
---

# Phase 02 Plan 19: Secret-Free Docker Context and Dual-Graph Dependency Policy Summary

**BuildKit now proves nested credentials cannot enter the Docker context, and CI rejects prohibited dependencies or enumeration failures under either Go build graph.**

## Performance

- Started: 2026-10-16T19:32:26Z
- Completed: 2026-10-16T19:35:00Z
- Tasks: 2/2
- Files modified: 6 implementation/test/configuration files, plus this summary

## Accomplishments

- Excluded root and nested PEM/key/P12/PFX files, certificate directories, dotenv variants, and local key directories; retained ordinary backend source and root/nested `.env.example`.
- Added a stdlib-only checker that sends only harmless fixtures in a private temporary directory to BuildKit, exports `FROM scratch` plus `COPY . /context/`, and inspects actual exported paths. The child command has a 60-second ceiling and fails if Docker is absent, fails, or times out.
- Added a real PEM-rule mutation regression: removing the exclusion causes `backend/fixture.pem` to leak and the check to fail.
- Added a shared shell guard that resolves the repository from any working directory and separately enumerates default/integration dependency graphs before applying the existing prohibited-import policy. Each enumeration has a 60-second ceiling; failures emit fixed diagnostics without raw Go output.
- Wired both script test suites and the actual Docker check into CI, preserved all seven integration groups and existing pins, and added `TestWriterSlowPoisonRebalance` to both the writer selection and exact causal-name guard.

## Task Commits

1. Task 1 RED: `dd5bee9` — `test(02-19): expose credential leaks through real Docker context`
2. Task 1 GREEN: `1a1257f` — `fix(02-19): exclude nested credentials before Docker context transfer`
3. Task 2 RED: `0ceacab` — `test(02-19): expose integration-only dependency policy bypass`
4. Task 2 GREEN: `d288523` — `fix(02-19): enforce dependency policy across both Go build graphs`

## Observed RED/GREEN Evidence

| Gate | Command / observation | Exit | Duration |
|------|-----------------------|------|----------|
| Docker RED | `python3 -B -m unittest discover -s scripts -p test_docker_context.py -k test_current_policy_excludes_private_material_in_real_export` against the original policy | 1 | 0.393 s test runtime |
| Docker RED detail | Actual export exposed `fixture.pem`, nested PEM/key/P12/PFX files, certificate-directory fixtures, and dotenv variants. The failure was the leaked-fixture assertion after successful Docker export, not missing infrastructure. | 1 | included above |
| Docker GREEN | `python3 -B -m unittest discover -s scripts -p test_docker_context.py && python3 -B scripts/check-docker-context.py` | 0 | 0.434 s command wall time; 4 tests in 0.309 s |
| Mutation GREEN | `test_removing_pem_exclusion_leaks_in_real_export` removes `**/*.pem` in a temporary policy and requires the real export checker to detect `backend/fixture.pem`. | 0 | included above |
| Dependency RED | `python3 -B -m unittest discover -s scripts -p test_go_dependencies.py -k test_integration_only_banned_import_fails` ran the original inline CI policy with a temporary stub Go executable; integration-only `github.com/pkg/errors` returned success unexpectedly. | 1 | 0.028 s test runtime |
| Dependency GREEN | `python3 -B -m unittest discover -s scripts -p test_go_dependencies.py && scripts/check-go-dependencies.sh && scripts/check-pins.sh && scripts/check-makefile-dotenv.sh` | 0 | 1.435 s command wall time; 5 tests in 1.101 s |
| CI acceptance | Extracted and executed the actual exact causal-name shell guard from CI; checked all seven independent integration groups remain. | 0 | under 1 s |
| Syntax / whitespace | `bash -n scripts/check-go-dependencies.sh` and `git diff --check` | 0 | under 1 s |

Dependency tests independently exercise failed default and failed integration enumeration, all prohibited import families in both graphs, both exact argument vectors, sanitized diagnostics, and direct invocation from an unrelated temporary working directory. Stub-driven subprocesses are bounded to 10 seconds. The standalone dependency check also passed against the real repository Go graphs.

## Threat Mitigations

- T-02-19-1: Nested private fixtures are absent from actual exported COPY contents; removing the PEM rule demonstrably fails.
- T-02-19-2: Both complete dependency graphs are checked and independent enumeration failures remain nonzero.
- T-02-19-3: The checked-in CI job runs the same guards and requires four exact causal test names, including the implemented rebalance regression.
- T-02-19-SC: No packages, actions, images, versions, or lockfiles were added or changed.

## Decisions Made

The fixture deliberately omits a remote syntax directive and uses `FROM scratch`, so context verification requires neither a base-image pull nor a frontend download. Only synthetic files are transferred. Real repository secrets and user volume contents are neither opened nor printed.

## Deviations from Plan

None. Dotenv variant exclusions strengthen the explicitly requested dotenv boundary while preserving `.env.example`. Shared STATE/ROADMAP/REQUIREMENTS/VALIDATION updates remain assigned to the parent orchestrator under its single-writer instructions.

## Issues Encountered

None. Both targeted regressions reproduced behavioral defects before their repairs. No dependency installs or credential/authentication gates occurred.

## Next Phase Readiness

WR-03 / gap 6 and the WR-04 / gap 7 dependency-policy subgap have targeted local evidence. Plan 02-21 owns the complete post-repair phase gate, including broad Docker-backed integration and the real rebalance selection. Hosted GitHub Actions remains pending; this plan did not push or claim hosted success. Original reports and user-owned configuration remain unchanged.

## Self-Check: PASSED

All six owned implementation/test/configuration paths exist. All four RED/GREEN task commits were verified with `git cat-file -e`. No tracked files were deleted. No blocking stubs or additional security surfaces outside the plan's filesystem/build-policy boundaries were introduced.
