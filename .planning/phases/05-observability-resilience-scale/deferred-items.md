# Phase 5 deferred items (out of scope for the plan that found them)

## Found during 05-07 (2026-11-16)

- `cd backend && go tool -modfile=tools.mod golangci-lint run --build-tags=phase4 ./testphase4/...` exits 1 with 22 issues, all in `backend/testphase4/tenant_matrix_test.go`: lll x17, govet unusedwrite x1, musttag x1, nolintlint x1, perfsprint x2. 05-07 does not touch that file. The project's lint gates (`./...` and `--build-tags=integration ./...`) report 0 issues.

## Found during 05-25 (2026-11-19)

- `python3 -B -m unittest discover -s scripts -p test_phase4_guards.py` fails 1 of 26: `test_docs_describe_the_gate_without_claiming_phase_completion` rejects the line "Phase 4 is complete (re-verified 2026-11-16 ...)" in `docs/operations.md`, introduced by dec4a7e (stale status refresh). Pre-existing at 95d41e2, before any 05-25 change; 05-25 does not touch that line. Either the guard or the status sentence needs a decision now that Phase 4 is closed.
