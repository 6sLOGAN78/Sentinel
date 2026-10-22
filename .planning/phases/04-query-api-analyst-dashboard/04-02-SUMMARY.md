---
phase: 04-query-api-analyst-dashboard
plan: "02"
status: complete
subsystem: auth
tags: [jwt, config, cli, echo, integration]
requires:
  - phase: 04-17
    provides: Stable private signing-key bootstrap and supported Compose/host handoff
provides:
  - Local/test-only development JWT CLI with validated identity and bounded lifetime
  - Central mandatory query-api/dev-jwt auth configuration with fixed issuer/audience
  - Native actual-binary mint/refusal and retained JWT/tenant assertions
affects: [04-03, 04-15, 04-16, dashboard-authentication]
tech-stack:
  added: []
  patterns: [private helper environment reuse, fixed auth configuration, token-only CLI stdout]
key-files:
  created: [backend/cmd/dev-jwt/main.go, backend/internal/auth/jwt_test.go]
  modified:
    - backend/internal/auth/jwt.go
    - backend/internal/config/config.go
    - backend/internal/config/config_test.go
    - backend/cmd/query-api/main.go
    - backend/testphase4/auth_test.go
    - .env.example
    - docs/operations.md
key-decisions:
  - "Reuse the existing private-key helper and child environment; do not create a second signing identity."
  - "Validate fixed sentinel-dev/sentinel-dashboard settings and require the key only for query-api/dev-jwt."
patterns-established:
  - "CLI behavioral tests build the actual command and use minted tokens against real query-api session/list routes."
requirements-completed: [AUTH-01, AUTH-02, AUTH-03]
duration: 11min recorded verification span
completed: 2026-10-22
---

# Phase 4 Plan 02: Safe development tokens and central auth configuration Summary

The development CLI mints short-lived HS256 tokens from the existing private signing key, while centralized configuration rejects invalid keys and contract-mismatched issuer/audience settings.

**Root accepted this plan after clean independent source review and post-wave checks.** The nine-file review at `9a21749`, source `0b15694`, has zero blockers/warnings. Root's fresh build and `make test` chain passed at 16:11:04Z. The requirement IDs above describe incremental coverage, not completed whole-phase requirements. Root owns shared tracking; proceed to 04-03, not Phase 5 or remote publication.

## Performance

- Recorded verification span: 2026-10-22T09:28:02.139062Z through 09:38:39.699841Z, approximately 11 minutes. Initial instruction/source reading and final documentation are outside this measured span; this is not active execution time or a performance benchmark.
- Tasks: 2/2 implemented, locally verified and root-accepted after independent source review.
- Exact source ownership: nine files, including two newly created files. No tenth source file, dependency, Make/Compose change, scanner exception or remote operation was introduced.
- Source candidate: `0b15694e02b8f0dabb6d8dce587baba62e721c08`, based on accepted HEAD `987a4a4a6308ea9eef350af10e9c5a9a1f9dd713`.

## Accomplishments and interfaces

- `config.Config.Auth` contains `JWTSecret`, `Issuer`, `Audience`. The key has no default; query-api and the new `config.ServiceDevJWT` require 32..4096 bytes, nonblank content and no NUL/CR/LF. Other services retain their prior required sections and can load without dashboard credentials. Issuer/audience default to, and must equal, `sentinel-dev` / `sentinel-dashboard`.
- `auth.NewVerifier(secret, issuer, audience)` now receives central settings and independently refuses names outside that fixed contract. Existing HS256-only verification, required exp/iat/nbf/sub/tenant/known role, 15-minute maximum lifetime, 30-second leeway, 8 KiB token cap and invalid-bearer/no-API-key-fallback behavior remain intact.
- `dev-jwt` checks the original dotted `SENTINEL_PRIMARY.ENV` before dotenv loading, allowing only explicit local/test. Required tenant/subject/known-role flags and a 1-second..15-minute lifetime are validated; default lifetime is 15 minutes. Successful stdout contains exactly one JWT and newline, stderr is empty, and refusal has empty stdout with a fixed sanitized stderr message. No key-value flag, HTTP mint endpoint or production identity provider was added.
- The CLI consumes the key from environment. Documented invocation through existing `scripts/dev_auth_env.py` reuses ignored `.local/auth/jwt.key`, preserving the helper's mode/permission checks and operator overrides. There is no new key generator or independent file-loading policy. Operator examples use pseudonymous identity and capture/output cautions without embedding credentials.
- Native coverage retains all original 19 subtests and the independent HS384/historical-expiry and exact credential-leak controls. Six additional JWT negatives plus the CLI family yield **26 top-level PASS subtests and 24 nested CLI PASS subtests**. CLI positives cover local/test, all three roles, 1 second, exact 15 minutes and default 15 minutes; negatives cover every nonallowed mode, identity/lifetime/key validation and refusal of a secret argument flag.

## Task commits

1. Task 1, tests-only RED: `3c8aea5` — pin central auth validation and actual CLI/security boundaries.
2. Task 2, minimal GREEN: `0b15694` — implement local tokens, validated central auth settings and operator documentation.

The summary is committed separately before root performs acceptance/tracking updates.

## Verification and evidence

Private raw evidence is ignored under `.local/phase4-acceptance/04-02/`, directory 0700, logs/reports/scripts/token captures 0600 and the owned test binary 0700. The evidence harness records actual SHA, command, UTC start/end and exit. Builds/tests run with umask 022. Captured tokens and keys were never displayed or included in logs/argv/URLs/tracked files. Assertions avoid printing unexpected credential output.

Precommit logs identify `3c8aea5` plus the then-working implementation subsequently committed as `0b15694`; they do not claim that implementation already existed in the base SHA. No source changed after the final verified commands and source commit.

| Evidence | Actual UTC interval on Oct 22 | Result |
|---|---|---|
| `config-red.log`: `cd backend && go test -race -timeout 60s ./internal/auth/... ./internal/config/...` | 09:28:02.139–09:28:03.866 | Exit 1: six intended `config.Load(query-api)` rejection assertions received nil; auth verifier characterization passed. |
| `native-red.log`: exact Auth command below | 09:28:08.999–09:28:15.038 | Exit 1: all 25 JWT/PG/legacy cases passed; CLI build failed because the command did not yet exist. This setup failure is not behavioral RED. |
| `unit-green.log`: focused auth/config command above | 09:30:06.915–09:30:09.920 | Exit 0; actual focused race tests pass. |
| `native-green.log`: exact Auth command below | 09:30:31.265–09:30:42.883 | Exit 0, package 10.336s; initial actual CLI and retained native cases pass. |
| `task-green-verified.log`: exact Auth command AND focused auth/config command, joined with `&&` | 09:36:02.656–09:36:15.369 | Exit 0; final native package 11.479s, 26 top-level/24 nested CLI cases; focused units explicitly cached from the earlier passing run. |
| `default-regressions.log`: `timeout 300 bash -c 'cd backend && go test -race -timeout 60s ./... && cd .. && make test'` | 09:32:50.927–09:34:32.947 | Exit 0; complete default Go race suite, web 15 and generator 121 tests. Later changes were confined to phase4-tagged CLI assertions. |
| `retained-openapi.log`: `bash scripts/go-test-nonempty.sh './test/...' '^TestOpenAPI' -race -timeout=60s -p=1` | 09:35:19.047–09:35:21.638 | Exit 0; all 11 retained OpenAPI tests execute nonempty, package 1.700s. |
| `static-verified.log`: full command below | 09:36:03.826–09:36:10.676 | Exit 0; default vet/build/lint, phase4 vet/lint and integration vet/lint; all three lint passes report zero issues. |
| `operator-green.log`: build actual CLI, then private operator probe | 09:32:50.398–09:32:50.845 | Exit 0; existing helper + new CLI + running Compose API: verified session and own-tenant list HTTP 200, 26 demo rows; existing key byte-identical. |
| `preserved-startup.log`: `timeout 16m bash -c 'make dev-up && scripts/check-stack.sh'` at source `0b15694` | 09:37:10.120–09:38:23.600 | Exit 0; normal rebuild/startup preserves volumes, all four dependencies/five apps healthy, exact topics/retention/auto-create-off, readiness and native dependency queries pass. |
| `operator-post-startup.log`: private operator probe against rebuilt Compose API at `0b15694` | 09:38:39.617–09:38:39.699 | Exit 0; actual helper-minted token reaches rebuilt session/tenant list, existing key unchanged; deliberate private token stdout capture mode 0600. |
| `source-history-scan.log`: pinned gitleaks 8.30.1 full-history redacted scan at `0b15694` | 09:37:34.388–09:37:35.294 | Exit 0; 499 commits scanned, no leaks, report exactly `[]`. No allowlist changed. Final metadata is scanned separately after this summary commit. |

Exact native target, unchanged tag/selector/race/cap:

```sh
bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Auth$' -tags=phase4 -race -timeout=6m -p=1
```

Final static chain:

```sh
cd backend && timeout 120s go vet ./... && timeout 120s go build ./... && timeout 120s go tool -modfile=tools.mod golangci-lint run ./... && timeout 120s go vet -tags=phase4 ./testphase4/... && timeout 120s go tool -modfile=tools.mod golangci-lint run --build-tags=phase4 ./testphase4/... && timeout 120s go vet -tags=integration ./... && timeout 120s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...
```

Native proof applies real PG migrations, inserts uniquely scoped synthetic workflow tenants, builds real race-enabled query-api/dev-jwt binaries, exercises actual HTTP handlers and checks tenant isolation/legacy access/CH-unavailable behavior. The separate operator probe uses the stable existing private helper key with the actual Compose API, including after its source rebuild. These are native/component/operator proofs, not a MODEL or full browser acceptance. No canonical schema/OpenAPI/browser source changed; retained OpenAPI and web tests passed, but no new web build or full browser/SSE/Phase3 aggregate/Phase4 aggregate/hosted CI pass is claimed.

## Deviations and retained failures

1. **[Rule 3 — Owned quality blockers]** Initial static passes found one CLI line-length issue (`static-first.log`, exit 1), followed by phase4 test context-parameter/assertion-style findings (`static-second.log`, exit 1). Wrapped the line, placed context first and added a credential-safe no-output helper without suppressing linters or printing supplied credentials. Final static chain is zero; all repairs are in `0b15694` and the original failure logs remain retained.
2. **[Rule 1 — Minimum-lifetime fixture]** Adding the 1-second positive CLI boundary exposed the new independent parser's omitted contract leeway (`task-green-final.log`, 09:35:17.858–09:35:28.648, exit 1; only `mint_test_viewer_1s` failed after the race binary's observed approximately 1.03-second run). The independent mint parser now uses the same documented 30-second leeway while asserting the exact original lifetime. Existing historical-expiry and HS384 checks and production validation are unchanged. Final native target passes. This is fixture correction, not a claimed production defect.

No ownership expansion was required. The Karpathy/TDD/debugging/verification guidance kept the implementation within the approved design, required causal configuration assertions before production edits and preserved the distinction between setup failures, existing correct behavior and new runtime proof.

## TDD gate compliance

Tests-only `3c8aea5` precedes implementation `0b15694`. Causal RED is the six central configuration validation failures, not the absent CLI build or already-correct JWT negatives. The CLI's new implementation is verified by actual binary success/refusal tests; no per-input behavioral RED is invented for a command that did not exist. Additional positive lifetime/role assertions and the test-only timing correction are included in GREEN with their actual evidence. No refactor commit was needed, failure was masked, empty selector passed or old cap relaxed.

## Known stubs, threats and limits

No goal-blocking stub exists in the changed files. Empty CLI flag defaults are deliberate required-input validation, not unwired UI data. Private environment handoff, CLI mint guard and JWT validation remain within T04-04/T04-05; no unregistered HTTP endpoint, schema trust boundary or new private-key file access implementation was introduced. No authentication gate occurred.

Production minting is refused; matching production signature verification is supported by the same fixed verifier configuration, not claimed as a tested production identity-provider/deployment integration. Full role-sensitive mutations, browser/session refinements and complete tenant coverage remain later approved slices. Independent review is clean in `04-REVIEW-02-R0.md`; root accepted this slice, enabling approved 04-03. Phase 5 and remote publication remain unauthorized here.

## Root acceptance addendum

Independent standard review read all nine source files and traced their consumers; zero blockers, warnings or info findings. It inspected actual RED/GREEN/CLI/operator/startup/scanner evidence without rerunning those producers. Preserved report: `04-REVIEW-02-R0.md`, frozen HEAD `9a21749`, source `0b15694`.

Root separately ran `timeout 300 bash -c 'cd backend && go build ./... && cd ../web && npm run build && cd .. && make test'` at `9a21749`; private `root-post-review-gates.log` records exit 0 at 2026-10-22T16:11:04.022219Z, with backend race, web 15 tests and generator 121 tests. Final metadata is scanned again before dispatch. Phase 4 is 3/17 accepted; no full Phase 4, hosted CI or fresh Phase 3 aggregate pass is inferred.

## Self-Check: PASSED

Verified the two new source/test files and both task commits exist; confirmed tests-only RED precedes GREEN, exact nine-source-file union, zero tracked deletions, scoped diff whitespace and unchanged source after verified gates. Current uncommitted changes outside this summary are only the pre-existing user `.planning/config.json` and `.serena/`. Shared tracking and review reports were not staged. Native/CLI, static, operator and scanner claims match their retained exits; final independent review remains explicit.
