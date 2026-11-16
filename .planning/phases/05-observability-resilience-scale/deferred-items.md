# Phase 5 deferred items (out of scope for the plan that found them)

## Found during 05-07 (2026-11-16)

- `cd backend && go tool -modfile=tools.mod golangci-lint run --build-tags=phase4 ./testphase4/...` exits 1 with 22 issues, all in `backend/testphase4/tenant_matrix_test.go`: lll x17, govet unusedwrite x1, musttag x1, nolintlint x1, perfsprint x2. 05-07 does not touch that file. The project's lint gates (`./...` and `--build-tags=integration ./...`) report 0 issues.
