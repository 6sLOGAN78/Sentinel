---
phase: 04-query-api-analyst-dashboard
plan: "17"
status: pending_independent_review
subsystem: auth
tags: [jwt, compose, python, vite, react, integration]
requires:
  - phase: 04-01
    provides: Strict JWT sessions and tenant-scoped PostgreSQL workflow reads
provides:
  - Private stable local JWT bootstrap with explicit Compose and host handoff
  - Fixed-loopback Vite API routing and bounded memory-only sign-in retry
  - Valid HS384 rejection fixture and exact credential disclosure controls
affects: [04-02, 04-03, 04-15, 04-16]
tech-stack:
  added: []
  patterns: [descriptor-based private credential handling, real Vite HTTP path proof]
key-files:
  created: [scripts/dev_auth_env.py, scripts/test_dev_auth_env.py]
  modified:
    - Makefile
    - deployments/compose/docker-compose.yml
    - backend/test/lifecycle_test.go
    - backend/testphase4/auth_test.go
    - web/vite.config.ts
    - web/src/App.tsx
    - web/src/App.test.tsx
    - docs/operations.md
    - .env.example
    - .github/workflows/ci.yml
key-decisions:
  - "Provision a private stable local signing key without changing production verification or committing credentials."
  - "Proxy browser API requests only to fixed loopback 8081; retain retry credentials only in memory with expiry and cancellation."
  - "Check explicit nonlocal parent mode before applying host child local settings."
requirements-completed: [AUTH-01, AUTH-03, API-03, UI-02, UI-06]
duration: 41min
completed: 2026-10-22
---

# Phase 4 Plan 17: Local authentication entrypoint repair Summary

Private local JWT provisioning now connects the supported Make/Compose/host launchers to real dashboard session and tenant workflow reads through Vite.

**Acceptance remains pending.** Tasks 1 and 2 are implemented and locally verified. Task 3's requested runtime gates passed, but independent source review and root-owned historical scanner findings remain open. This summary does not authorize 04-02, mark requirements accepted, or update overall tracking. The requirements field above copies the plan's incremental coverage IDs.

## Performance

- Recorded evidence span: 2026-10-22T00:59:09Z through approximately 01:40Z, about 41 minutes; initial instruction reading is not measured. This is elapsed observation, not active execution or a performance benchmark.
- The final stack/HTTP/helper gate ended at 01:20:11Z. A later evidence audit records 01:39:51Z. The reason for that clock interval is unestablished; no interruption or suspension cause is inferred.
- Source boundary: exactly the 12 approved files. Root owns STATE/ROADMAP/REQUIREMENTS updates. User-owned `.planning/config.json` and `.serena/` remain untouched and unstaged.

## Accomplishments

- CR-01: `scripts/dev_auth_env.py` provisions an independent random 48-byte-entropy signing key in ignored `.local/auth/jwt.key`. Checked directory descriptors reject symlinks, nonprivate modes and foreign ownership; key checks reject nonregular/multiply linked files and short/empty/oversized contents. Creation uses a private temporary file, fsync and a nonoverwriting atomic link under a directory lock. Reads are bounded. Existing credentials are never chmodded, replaced or silently repaired. An operator override comes only from environment variables. The helper copies the child environment, forwards normal and dotted JWT names, preserves child exit status, and rejects explicit nonlocal modes without printing credentials.
- Make routes Compose and host query-api through that helper; other host services have JWT variables removed. Compose explicitly maps the dotted application variable. Its empty interpolation value permits unwrapped read-only `config`/`ps`, while the unchanged binary refuses missing/short keys. Native lifecycle fixtures now use independently random test keys; missing/short-key process controls still fail closed. Existing reset guards, volume wiring, pins, legacy selectors and the exact approved Phase 3 CI block remain intact. CI hygiene explicitly runs the helper suite.
- CR-02: Vite binds `127.0.0.1` and proxies `/api` only to `http://127.0.0.1:8081`. Actual HTTP requests carrying an Authorization header through the Vite origin returned verified session JSON and a real own-tenant PG workflow, excluding an inserted foreign-tenant row. This is real HTTP proxy proof, not full browser/SSE acceptance.
- WR-01: session 401 retains invalid-token feedback; 5xx/network/HTML/invalid identity responses display availability feedback and a named retry action. Retry credentials stay in a ref, leave the input immediately and clear on success/cancel/sign-out/expiry/unmount. An unverified token expiry can only shorten a finite 15-minute retention ceiling; identity still comes from the server. Cancellation aborts work and prevents stale identity/data from replacing a subsequent session.
- WR-02/03: the HS384 fixture now actually signs with HMAC-SHA384 and is first accepted by an independent HS384 test parser. Production still rejects it with 401 under the unchanged HS256-only verifier. Exact nonempty JWT and API-key absence use a disclosure predicate with explicit verbatim-leak negative controls.

## Task Commits

1. Task 1 RED: `66ef55c` — private bootstrap/handoff regression tests and native missing/short-key controls.
2. Task 1 GREEN: `0ab0bc2` — private helper, supported launcher wiring, ephemeral lifecycle keys and CI step.
3. Task 2 RED: `d14b9f1` — outage/retry/cancel/expiry assertions and independently verified algorithm fixture assertion.
4. Task 2 GREEN: `a554ce5` — fixed proxy, session state handling, valid HS384 fixture and operator documentation.
5. Task 3 causal follow-up RED: `acd675c` — actual Make with controlled child exposes host settings hiding explicit nonlocal mode.
6. Task 3 follow-up GREEN: `a8b062e` — check original parent mode before applying local child settings.

The metadata commit records this pending-review handoff separately. No history rewrite, branch change, remote push or hosted CI was performed.

## Verification and Evidence

Raw evidence is ignored under `.local/phase4-acceptance/04-17/`, directories 0700 and files 0600. Every command log records its actual base SHA, command, UTC start/end and exit. Precommit logs identify the then-current base plus the owned working changes subsequently committed; they do not claim those changes already existed in the base commit. Backend/web implementation is unchanged after `a554ce5`; the later source change is only Make's wrapper order and its test.

| Evidence | Actual result |
|---|---|
| `lifecycle-red.log`, retained `^TestBinaryLifecycle$/^query-api$`, integration/race/5m | Exit 1; graceful and SIGINT processes exited early because the mandatory JWT key was absent. Package 25.075s. |
| `vite-json-red.log`, actual Vite-origin session request | Exit 1; actual 200 `text/html` SPA fallback violated expected API JSON. The earlier discovery probe intentionally recognized the HTML and returned 0; it is not counted as the failing JSON assertion. |
| `web-retry-red.log` | Exit 1; six new component cases failed, nine retained cases passed. The invalid-identity case also exposed an actual unchecked-body error. |
| `auth-fixture-red.log` | Exit 1 at independent HS384 verification of the old incorrectly signed fixture. Existing production rejection/disclosure checks passed; no production algorithm or redaction RED is invented. |
| `make-mode-red.log` → `make-mode-green.log` | Exit 1 → 0; actual Make/controlled-child policy regression, followed by helper tests and actual healthy host query-api launch. |
| `lifecycle-green.log` | Exit 0; query-api graceful/SIGINT plus real missing/short-key startup controls. Package 41.378s. |
| `dev-up-green.log` | `timeout 16m make dev-up`, exit 0, 01:01:56–01:07:51Z; normal preserved-volume startup, all nine services healthy. |
| `host-handoff.log` / `make-mode-green.log` | Actual `make run SVC=query-api` reused the private key and returned readyz 200 on unique host ports; owned process group stopped and joined. |
| `stack-config-green.log` | Six retained tests passed, including actual read-only Compose parsing without a JWT key. |
| `python-all.log` | Locked generator Python, 59 script tests passed in 202.012s, exit 0. Includes 8 new helper tests and the retained exact CI guard/selector negative controls. |
| `web-task2-final.log` | All 15 component/type-fixture tests, typecheck, lint, build and format check passed, each command capped at 120s. |
| `auth-final.log` | Nonempty actual `^TestPhase4Auth$`, phase4/race/6m, exit 0 at `a554ce5`; 19 subtests, including independently valid HS384 rejected by production, exact leak controls and real unreachable-CH/readiness 503 proof. |
| `readiness-lifecycle-final.log` | Nonempty retained `TestReadyz\|TestHealthz\|TestBinaryLifecycle`, integration/race/5m, exit 0, package 68.358s. Actual TestHealthz, TestReadyzDependencies, TestReadyzDraining and all lifecycle families ran. |
| `default-gates.log` | `make test && make lint`, default Go vet and all-package build, exit 0. Backend race suite, web 15 tests and generator 121 tests passed; default backend lint reports zero issues. |
| `integration-static-green.log` | Integration-tag vet and full backend lint passed, exit 0, original 120s per-command ceilings. |
| `retained-openapi.log` | Nonempty `^TestOpenAPI`, race/60s, exit 0; 11 retained tests. Canonical DTO/schema and production JWT verifier are unchanged. |
| `seed1000.log` | `timeout 16m make seed`, exit 0, 01:13:39–01:15:06Z. Sent/accepted/exactly visible 1000, zero retries, three API spot checks, foreign-tenant isolation passed. |
| `make-final-preserved.log` | After wrapper-order repair, normal `timeout 16m make seed` again passed, 01:18:54–01:19:28Z; all nine healthy and another exact 1000-ID run. No data reset or teardown. |
| `final-handoff-stack.log` | At `a8b062e`, actual check-stack, actual Vite session/own PG list and helper 8 tests all passed; 01:19:55–01:20:11Z, exit 0. |
| `hygiene.log` / `post-seed-final.log` | Pins, ADR, literal-dotenv and private Docker-context guards passed; no secret-bearing Compose configuration was printed. |
| `evidence-audit-final.log` | Exit 0; verified required actual exits, private modes, exact local-key absence from logs/tracked changes, all 12 source paths, and unchanged production verifier/canonical schema/PG DTO. |

Filesystem/concurrency tests use actual temporary directories and child processes. Make command capture is explicitly a MODEL with controlled Go/Docker children, separate from the actual host/stack/HTTP evidence above. Foreign-owner policy is tested by injecting the expected uid in an unprivileged child while retaining actual opens/fstats; it is not presented as a real privileged chown test.

## TDD Gate Compliance

Each focused source repair has a preceding tests-only commit and a separately observed causal failure. The original runtime startup and actual Vite HTML failures precede their fixes. `helper-red.log` also contains missing-helper setup errors; those errors are not claimed as behavioral RED for each individual filesystem safeguard. Security hardening checks that already passed correct production remain characterization/negative-control evidence. The HS384 RED identifies the broken fixture, not a production verifier defect.

## Deviations from Plan

1. **[Rule 1 — Owned fixture lint]** Integration-tag lint caught one newly introduced shadowed `err` in lifecycle key generation. Reused the existing variable; integration vet/lint then passed. Included in `0ab0bc2`. No linter was weakened.
2. **[Rule 1 — Host mode handoff]** Final verification exposed the new host launcher applying local mode before the helper checked explicit parent mode. A causal Make regression (`acd675c`) failed before moving the wrapper ahead of local child settings (`a8b062e`). Helper/actual host and full normal preserved seed checks passed afterward. No additional source file was needed.

Karpathy guidance kept the changes within the approved launch/authentication repair. TDD/debugging and verification guidance supplied the observed failure → focused fix → actual runtime verification sequence. React guidance kept transient credentials in refs and cleanup tied to session lifetime; Compose guidance preserved existing pinned dependencies, loopback ports and volumes.

## Deferred Issues and Remaining Gates

- **Independent review pending:** root must review the frozen source `a8b062e891115e6c80d264cbeaa1ad50b53c6de1` against all five original findings and new helper/wiring safety. Original `04-REVIEW-01-R0.md` is unchanged. Task 3 is not accepted until that review is clean.
- **Full-history scanner is NOT green:** pinned gitleaks 8.30.1 exited 1 with exactly three historical planning matches, all predating this repair's source commits: `778d3db` / `04-07-PLAN.md:58`; `5d11ca2` / `04-17-PLAN.md:70` and `:101`. Final redacted report: `.local/phase4-acceptance/04-17/final-secret-report.json`. Root owns classification and any precise follow-up; this executor did not edit planning history, change the allowlist or waive the gate. No implementation commit was reported by this scan.
- **Additional phase4-tag lint is NOT green:** `phase4-static.log` reports six diagnostics in inherited Auth fixture code: cognitive complexity 25, shadowed `base`, three overlong lines and literal HTTP 200. These source constructs/control flow predate this repair. Required default and integration lint are green. Attempted private baseline isolation/Go overlay did not produce a valid baseline lint run (internal-import/typecheck failures), so no executed baseline equivalence is claimed. These unrelated fixture findings were left unchanged and reported for root disposition.
- No full Phase 4, Phase 3 aggregate, full browser/SSE, hosted CI or performance/RSS guarantee is claimed. No 04-02 or Phase 5 work began. Overall tracking remains root-owned.

## Known Stubs and Threat Surface

No new goal-blocking stub was introduced. Initial empty React state is real pre-sign-in state; actual session/PG data sources are wired. The existing token CLI help refers to approved next slice 04-02; operator documentation explicitly says the CLI is not implemented yet. Existing future bench functionality is unchanged.

New private file handling, child secret environments, loopback proxy and retry credential lifetime are all within the plan's threat model. No new endpoint, schema boundary, authentication bypass or unregistered threat surface was introduced. No authentication gate occurred.

## Next Phase Readiness

Return to the orchestrator for independent review, historical scanner classification and tracking reconciliation. Do not treat summary presence as accepted completion or start 04-02 until the repair review and remaining gates are resolved.

## Self-Check: PASSED

Verified both new source/test files, this summary and all six recorded task commits exist; checked scoped diff whitespace and zero unexpected deletions. Private evidence audit verified actual gate exits and the exact 12-source boundary before this metadata file was added. This self-check verifies the handoff's claims, not independent acceptance; the scanner and extra tag-lint failures remain explicitly recorded above.
