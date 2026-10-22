---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-22T08:56:00Z
depth: standard
review_sha: c3b060bbcfdcd364f353e2d95f10208e318c925e
source_sha: a1fbd4f7b8393616bb199853734156f2b9dd91cb
review_slice: 04-01 and 04-17
diff_base: 358114c
repair_diff_base: f28ef6eba7172bf66c01a75b8e2ee7f1f3d7d787
files_reviewed: 21
files_reviewed_list:
  - .env.example
  - .github/workflows/ci.yml
  - Makefile
  - api/openapi.yaml
  - backend/cmd/query-api/main.go
  - backend/go.mod
  - backend/go.sum
  - backend/internal/auth/jwt.go
  - backend/internal/storage/postgres/query_alerts.go
  - backend/test/lifecycle_test.go
  - backend/test/openapi_test.go
  - backend/testphase4/auth_test.go
  - deployments/compose/docker-compose.yml
  - docs/operations.md
  - scripts/dev_auth_env.py
  - scripts/test_dev_auth_env.py
  - web/src/App.test.tsx
  - web/src/App.tsx
  - web/src/api/types.test.ts
  - web/vite.config.ts
  - .gitleaksignore
findings:
  critical: 0
  warning: 1
  info: 0
  total: 1
status: issues_found
historical_scanner_amendment: approved_bounded
scanner_gate: pending_full_history_rerun_after_amendment
---

# Phase 4 Plans 01 and 17: Code Review Report

**Depth:** standard; all 21 explicitly scoped tracked files read in full.

**Frozen HEAD:** c3b060bbcfdcd364f353e2d95f10208e318c925e

**Source candidate:** a1fbd4f7b8393616bb199853734156f2b9dd91cb
**Status:** issues_found — one test-reliability warning remains.

## Narrative Findings (AI reviewer)

The two original blockers and three original warnings are closed in the reviewed source. One additional negative-fixture defect remains: the expired-token case cannot isolate expiration enforcement. Passing native tests do not close that warning. This is the sign-in/read slice and its repair, not whole-Phase-4 acceptance.

The original five findings remain preserved verbatim in 04-REVIEW-01-R0.md (review SHA f28ef6eba7172bf66c01a75b8e2ee7f1f3d7d787, Git blob 58fef39594e4c6fac6d1e01b555e79d1f6f3e9e6). The current counts include only unresolved findings; the historical report retains its original two blockers/three warnings.

### Warnings

#### WR-04: WARNING — Expired-token fixture also violates independent lifetime checks

**File:** /home/logan78/Desktop/Sentinel/backend/testphase4/auth_test.go:175-176
**Related:** claims initialization at lines 81-85; production lifetime checks at /home/logan78/Desktop/Sentinel/backend/internal/auth/jwt.go:76-80; assertion at backend/testphase4/auth_test.go:141-146.

**Issue:** The expired case changes only exp to now minus one minute. The supplied claims still have iat = now and nbf = now. Consequently this correctly signed token has both exp <= iat and nbf > exp. The production verifier explicitly rejects both conditions after parsing. Even if parser claim validation were accidentally disabled, this fixture would still receive 401 from those independent checks, so reject_expired would remain green while otherwise valid expired tokens could be accepted. This is a defect in the existing expiration regression assertion, analogous to the original wrong-algorithm fixture defect; it is not a claim that the current production verifier accepts expired tokens.

**Evidence:** Direct source trace: the case at lines 175-176 mutates only exp; verifier lines 78-79 require ExpiresAt.After(IssuedAt) and NotBefore <= ExpiresAt. The pinned JWT library's validator.go:109-125 performs expiration validation separately from those application checks. No production mutation or new runtime test was performed by this reviewer.

**Fix:** Construct a temporally consistent expired credential, preserving the correct HS256 signature and all other accepted claims, for example:

    case "expired":
        claims["iat"] = now.Add(-10 * time.Minute).Unix()
        claims["nbf"] = now.Add(-10 * time.Minute).Unix()
        claims["exp"] = now.Add(-time.Minute).Unix()

Assert the fixture's positive lifetime is at most 15 minutes, nbf <= exp, and expiry is beyond the 30-second leeway in the past. An independent parser with its clock inside that valid interval can establish that the same signed fixture is otherwise acceptable; the production process at the current clock must return 401. Retain both session/list assertions, exact disclosure checks and all 19 native subtests. Rerun the exact nonempty Auth family and affected phase4 vet/lint after correction.

### Disposition of the original five findings

| Historical finding | Current disposition and causal source/evidence |
|---|---|
| CR-01 — BLOCKER, missing launch key | **Closed.** Makefile:4,78 invokes the private wrapper for Compose and host query-api before local child settings; other host services have both JWT names removed. scripts/dev_auth_env.py:92-101 rejects explicit nonlocal modes/invalid or conflicting overrides and sets both child variable names. deployments/compose/docker-compose.yml:297-299 explicitly forwards the normal variable into the dotted application key. Empty interpolation permits read-only config; main.go:48-54 still refuses missing/short keys. lifecycle_test.go:95-104 supplies fresh random test keys; lines 304-322 retain actual missing/short-key startup negatives. Retained readiness helpers call this same serviceEnv, including dependency replacements. Private lifecycle/readiness, actual host, normal dev-up and two 1000-ID seed logs corroborate the wiring. |
| CR-02 — BLOCKER, browser/API routing | **Closed.** web/vite.config.ts:6-8 binds 127.0.0.1 and proxies /api to literal http://127.0.0.1:8081; neither request input nor credentials choose the target. Relative fetches retain Authorization headers. docs/operations.md:46-69 describes the actual launchers. Reviewed vite_probe.py:22-52 reads the key privately, signs a valid JWT, inserts own/foreign PG fixtures and checks session plus own-tenant list through the real Vite origin. vite-json-red.log records HTML/exit 1; vite-json-green.log and final-handoff-stack.log record verified JSON/exit 0. This is HTTP proxy evidence, not a full browser/SSE run. |
| WR-01 — WARNING, misleading availability feedback | **Closed.** App.tsx:136-180 handles session 401 separately from network/5xx/invalid identity failures, offers named retry, and clears the input before verification. Retry uses an in-memory ref and finite deadline; an unverified expiry can only shorten that deadline (14-36). Success clears the retry ref; cancel/sign-out/expiry/unmount clear credentials and abort work (71-102,169-173). Aborted checks after both fetch and JSON parsing suppress stale identity/page updates. Component cases at App.test.tsx:116-264,266-305 exercise availability retry, cancellation, expiry and stale responses. |
| WR-02 — WARNING, invalid HS384 fixture | **Closed.** auth_test.go:269-274 selects sha512.New384 for HS384. Lines 88-93 independently verify the signed HS384 fixture before production rejection at lines 141-146. jwt.go:69-72 retains HS256-only parsing. Removing that restriction would allow the otherwise valid fixture and fail the production assertion. |
| WR-03 — WARNING, punctuation-biased disclosure assertion | **Closed.** auth_test.go:190-197 checks each exact nonempty credential, with no appended punctuation. Lines 95-98 require that verbatim JWT disclosure and verbatim API-key disclosure each fail the predicate; lines 144-146 use it on both endpoint responses. Reviewed request logging/error mapping emits sanitized metadata and does not serialize Authorization/API-key headers. |

### Additional review coverage

Private-file handling was traced through scripts/dev_auth_env.py:30-89: descriptor-relative directory traversal, no-follow opens, actual uid/type/mode checks on both private directories and the key, hardlink rejection, bounded read, cryptographic randomness, private exclusive temporary creation and nonoverwriting atomic publication under a directory lock. Descriptors close before child exec. Existing unsafe credentials are rejected without chmod, overwrite or deletion. The helper does not put the key in argv or output. Tests use real temporary files/processes; foreign-owner coverage deliberately substitutes the expected uid rather than claiming privileged chown. Make child capture is a MODEL; actual host/Compose evidence is separate.

JWT signature/claim/error handling, authenticated route wiring, legacy API-key dispatch, parameterized tenant-only bounded PG reads, DTO columns against migration 003, generated-type consumers, and retained canonical schema/API-key contracts were reviewed in context. The approved later CLI/central config, richer client, complete filter/paging UI, triage, SSE and full browser acceptance remain owned by later slices and are not reported as missing current features.

The complete CI diff against 358114c adds only the two-line hygiene helper test step (.github/workflows/ci.yml:136-137). Existing job definitions, full-integration 32-minute Go cap, 45-minute job caps, seven retained groups, exact Phase 3 block and 44-core/two-scenario selectors are unchanged. The retained CI guard and selector scripts were traced; no failure masking or selector weakening was added.

No AGENTS.md, .codexignore or project skill directories were present. PROJECT, CONTRACT, UI-SPEC, both PLAN/SUMMARY pairs, repair plan check, original review, ownership proposal and relevant ADRs 0001/0006/0008 were read. No structural pre-pass was provided. Ignored generated web/src/api/schema.d.ts is excluded from the source count. .gitleaksignore is the 21st explicitly scoped existing candidate; its proposed new entries were not yet applied. Scoped source files are identical between source candidate and frozen HEAD. Source review was read-only; no services/tests/scanner were rerun, no source was edited and no commit was made. Only this report and the independent ownership-check artifact were written.

### Evidence inspected and its limits

Private evidence under .local/phase4-acceptance/04-17/ was inspected for actual commands, SHAs, timestamps, named results and exits. Reviewed probes establish what their assertions measure; tests/logs are corroboration, not a substitute for source analysis. Precommit logs identify their base plus subsequently committed working changes, as the SUMMARY explains; they are not represented as executions at final HEAD.

| Evidence | Observed result |
|---|---|
| lifecycle-green.log | Integration/race/5m; query-api graceful/SIGINT and missing/short-key negatives named PASS; exit 0. |
| readiness-lifecycle-final.log | Actual Healthz, ReadyzDependencies, ReadyzDraining and lifecycle families named PASS; integration/race/5m; exit 0. |
| make-mode-red.log → make-mode-green.log | Actual Make/controlled-child mode regression exit 1 → 0; actual host readyz proof also present. |
| dev-up-green.log, make-final-preserved.log, final-handoff-stack.log | Normal preserved-volume startup; final seed reports sent/accepted/visible 1000, three API checks and tenant isolation; final stack/proxy/helper chain exit 0. |
| python-all.log, web-task2-final.log, retained-openapi.log | 59 script tests, 15 web/type-fixture tests and 11 OpenAPI tests passed; recorded chains exit 0. |
| lint-quality-before.log → lint-quality-green.log, auth-quality-green.log, retained-quality-static.log | Retained quality failure exit 1; fixture follow-up records zero phase4 issues, all 19 Auth subtests and default/integration checks exit 0. These do not exercise an expiration-specific negative with valid temporal relationships; WR-04 remains open. |
| final-history-scan.log / final-secret-report.json | Pinned full-history scan exit 1; exactly the three historical planning fingerprints described below. No passing replacement is claimed. |

The original review blob still matches HEAD. User-owned .planning/config.json and .serena/ were preserved. No whole-Phase-4, full Phase-3 aggregate, hosted CI or performance guarantee is inferred from these scoped records.

### Proposed historical scanner amendment

**Decision: APPROVE only the bounded .gitleaksignore amendment in 04-17-OWNERSHIP-A1.md:17-19.** The independent check is in 04-17-OWNERSHIP-A1-CHECK.md.

The three redacted report fingerprints exactly match the proposal. Historical 778d3db.../04-07-PLAN.md:58 describes API-key/security test cases; historical 5d11ca2.../04-17-PLAN.md:70,101 duplicates session outage/retry policy prose. Direct historical-line inspection finds no credential value. These are scanner false positives in prose. Approval covers only those three complete commit/path/rule/line fingerprints following the existing policy; no path/rule/content-wide exception, implementation exception or history rewrite is authorized.

This approval does not make the scanner gate pass: .gitleaksignore is unchanged at reviewed HEAD and the retained scan exited 1. Root must apply only the approved entries, rerun pinned gitleaks 8.30.1 over full history, preserve actual SHA/start/end/exit privately and require zero findings/exit 0. Any other fingerprint remains a failure. Repair acceptance/04-02 advancement also remains blocked by WR-04 until corrected and independently re-reviewed.

---

_Reviewed: 2026-10-22T08:56:00Z_

_Reviewer: gsd-code-reviewer_
_Depth: standard; frozen 04-01/04-17 explicit scope only_
