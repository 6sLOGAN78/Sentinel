---
phase: 04-query-api-analyst-dashboard
plan: "17"
status: complete
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
duration: 52min observed execution spans
completed: 2026-10-22
---

# Phase 4 Plan 17: Local authentication entrypoint repair Summary

Private local JWT provisioning now connects the supported Make/Compose/host launchers to real dashboard session and tenant workflow reads through Vite.

**Root accepted all three tasks after independent R1 review and post-wave checks.** Review at `a302b65`, source `11dddea`, is clean: zero blockers/warnings. All five original findings and WR-04 are closed. Root resolved the historical scanner gate through the independently approved three exact fingerprints and passing full-history scans. Root's fresh build and `make test` chain exited zero at 09:20:02Z. Plan 04-02 may proceed; these incremental coverage IDs do not mark whole Phase 4 requirements accepted.

## Performance

- Recorded evidence span: 2026-10-22T00:59:09Z through approximately 01:45Z, about 46 minutes; initial instruction reading is not measured. This includes the root-requested fixture-quality follow-up and is elapsed observation, not active execution or a performance benchmark.
- WR-04 follow-up evidence spans 09:04:54–09:10:16Z, approximately six additional minutes. The intervening review/wait interval is excluded from the combined approximately 52-minute observation spans; no active-time or unexplained-gap cause is inferred.
- The final stack/HTTP/helper gate ended at 01:20:11Z. A later evidence audit records 01:39:51Z. The reason for that clock interval is unestablished; no interruption or suspension cause is inferred.
- Executor source boundary: exactly the original 12 approved files. Root separately owned the independently approved bounded `.gitleaksignore` amendment under A1. Root owns STATE/ROADMAP/REQUIREMENTS updates. User-owned `.planning/config.json` and `.serena/` remain untouched and unstaged.

## Accomplishments

- CR-01: `scripts/dev_auth_env.py` provisions an independent random 48-byte-entropy signing key in ignored `.local/auth/jwt.key`. Checked directory descriptors reject symlinks, nonprivate modes and foreign ownership; key checks reject nonregular/multiply linked files and short/empty/oversized contents. Creation uses a private temporary file, fsync and a nonoverwriting atomic link under a directory lock. Reads are bounded. Existing credentials are never chmodded, replaced or silently repaired. An operator override comes only from environment variables. The helper copies the child environment, forwards normal and dotted JWT names, preserves child exit status, and rejects explicit nonlocal modes without printing credentials.
- Make routes Compose and host query-api through that helper; other host services have JWT variables removed. Compose explicitly maps the dotted application variable. Its empty interpolation value permits unwrapped read-only `config`/`ps`, while the unchanged binary refuses missing/short keys. Native lifecycle fixtures now use independently random test keys; missing/short-key process controls still fail closed. Existing reset guards, volume wiring, pins, legacy selectors and the exact approved Phase 3 CI block remain intact. CI hygiene explicitly runs the helper suite.
- CR-02: Vite binds `127.0.0.1` and proxies `/api` only to `http://127.0.0.1:8081`. Actual HTTP requests carrying an Authorization header through the Vite origin returned verified session JSON and a real own-tenant PG workflow, excluding an inserted foreign-tenant row. This is real HTTP proxy proof, not full browser/SSE acceptance.
- WR-01: session 401 retains invalid-token feedback; 5xx/network/HTML/invalid identity responses display availability feedback and a named retry action. Retry credentials stay in a ref, leave the input immediately and clear on success/cancel/sign-out/expiry/unmount. An unverified token expiry can only shorten a finite 15-minute retention ceiling; identity still comes from the server. Cancellation aborts work and prevents stale identity/data from replacing a subsequent session.
- WR-02/03: the HS384 fixture now actually signs with HMAC-SHA384 and is first accepted by an independent HS384 test parser. Production still rejects it with 401 under the unchanged HS256-only verifier. Exact nonempty JWT and API-key absence use a disclosure predicate with explicit verbatim-leak negative controls.
- WR-04: the expired fixture now has issued-at/not-before ten minutes ago and expiration one minute ago. The same signed raw credential passes an independent HS256 parser at five minutes ago with issuer/audience/subject and required date checks. Additional assertions establish a positive lifetime no longer than 15 minutes, not-before no later than expiration, expiry beyond current 30-second leeway, and expected tenant/role. The existing session/list requests then require current production 401s and exact credential absence. All 19 subtests remain; the production verifier is unchanged.

## Task Commits

1. Task 1 RED: `66ef55c` — private bootstrap/handoff regression tests and native missing/short-key controls.
2. Task 1 GREEN: `0ab0bc2` — private helper, supported launcher wiring, ephemeral lifecycle keys and CI step.
3. Task 2 RED: `d14b9f1` — outage/retry/cancel/expiry assertions and independently verified algorithm fixture assertion.
4. Task 2 GREEN: `a554ce5` — fixed proxy, session state handling, valid HS384 fixture and operator documentation.
5. Task 3 causal follow-up RED: `acd675c` — actual Make with controlled child exposes host settings hiding explicit nonlocal mode.
6. Task 3 follow-up GREEN: `a8b062e` — check original parent mode before applying local child settings.
7. Root-requested Task 3 quality repair: `a1fbd4f` — extract rejected-credential fixture construction, wrap long lines, rename shadowed outage base and use `http.StatusOK`; all 19 subtests and 44 assertion calls retained.
8. WR-04 tests-only RED: `5147aa8` — independent signed-credential/temporal checks fail on the original expired fixture's negative one-minute lifetime.
9. WR-04 minimal GREEN: `11dddea` — change only the expired fixture's issued-at and not-before timestamps to ten minutes ago; expiration remains one minute ago.

The metadata commit records this pending-review handoff separately. No history rewrite, branch change, remote push or hosted CI was performed.

## Verification and Evidence

Raw evidence is ignored under `.local/phase4-acceptance/04-17/`, directories 0700 and files 0600. Every command log records its actual base SHA, command, UTC start/end and exit. Precommit logs identify the then-current base plus the owned working changes subsequently committed; they do not claim those changes already existed in the base commit. Production backend/web implementation is unchanged after `a554ce5`; subsequent changes are Make's wrapper order/test and the scoped Auth test-fixture quality repair.

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
| `lint-quality-before.log` → `lint-quality-green.log` | Reconfirmed six phase4-tag quality findings at root's base `6e136cd`, exit 1; after the fixture-only repair, phase4 vet/lint exit 0 with zero issues, 01:44:54–01:44:56Z. Both commands retain their 120s ceilings. |
| `auth-quality-green.log` | Actual nonempty `^TestPhase4Auth$`, phase4/race/6m, exit 0 after the fixture repair. All 19 prior subtest names and their order are preserved; exact assertions remain unchanged. |
| `retained-quality-static.log` | Default Go race suite (60s Go/120s command caps), default vet/build/lint and integration-tag vet/lint all exit 0 after the fixture repair; each static command retains its 120s ceiling, 01:44:56–01:45:03Z. |
| `expiry-fixture-red.log` / `expiry-dependencies.log` | Initial exit 1 was PostgreSQL connection-refused setup, not behavioral RED. Existing PG/CH/Redis were observed exited, then restored normally with preserved data; dependency startup exit 0. No stopped-container cause was inferred. |
| `expiry-fixture-behavior-red.log` | Genuine exit 1, 09:07:18–09:07:24Z, package 5.188s. `reject_expired` failed because its original lifetime was -1m; all other 18 subtests passed. |
| `expiry-fixture-green.log` | Exact nonempty Auth/phase4/race/6m, exit 0, 09:08:03–09:08:10Z, package 6.571s. All 19 original subtest names/order retained. The same expired signed credential passes historical validity checks and both current production rejections. |
| `expiry-static-green.log` | Phase4 vet/lint, default Go race/vet/build/lint and integration vet/lint all exit 0, 09:08:04–09:08:16Z. Static commands keep 120s caps; default Go test retains its 60s Go timeout. No suppression/configuration change. |
| `wr04-fresh-stack.log` | At `11dddea`, one normal `timeout 16m make dev-up && bash scripts/check-stack.sh`, exit 0, 09:08:58–09:10:16Z. All nine services healthy, topic policies/absence, ports, real readiness and dependency queries passed after the observed external state change. No reset/data teardown or duplicate producer. |
| Root's `root-reviewed-history-scan.log` | At `852de68`, pinned 8.30.1 full-history scanner, 493 commits, zero findings/exit 0, 09:02:16.546769–09:02:17.923127Z. `root-reviewed-history-report.json` is `[]`, mode 0600. This supersedes, without erasing, the earlier historical scanner failures. |

Filesystem/concurrency tests use actual temporary directories and child processes. Make command capture is explicitly a MODEL with controlled Go/Docker children, separate from the actual host/stack/HTTP evidence above. Foreign-owner policy is tested by injecting the expected uid in an unprivileged child while retaining actual opens/fstats; it is not presented as a real privileged chown test.

## TDD Gate Compliance

Each focused behavior repair has a preceding tests-only commit and a separately observed causal failure. The original runtime startup and actual Vite HTML failures precede their fixes. `helper-red.log` also contains missing-helper setup errors; those errors are not claimed as behavioral RED for each individual filesystem safeguard. Security hardening checks that already passed correct production remain characterization/negative-control evidence. The HS384 RED identifies the broken fixture, not a production verifier defect. The later fixture-quality refactor uses actual lint exit 1 → 0 evidence; it is not presented as a new behavioral RED/GREEN cycle. WR-04 has a separate tests-only RED commit and minimal timestamp GREEN: the setup connection failure is excluded, and the demonstrated RED is the old fixture's negative lifetime. No production mutation or verifier failure is invented.

## Deviations from Plan

1. **[Rule 1 — Owned fixture lint]** Integration-tag lint caught one newly introduced shadowed `err` in lifecycle key generation. Reused the existing variable; integration vet/lint then passed. Included in `0ab0bc2`. No linter was weakened.
2. **[Rule 1 — Host mode handoff]** Final verification exposed the new host launcher applying local mode before the helper checked explicit parent mode. A causal Make regression (`acd675c`) failed before moving the wrapper ahead of local child settings (`a8b062e`). Helper/actual host and full normal preserved seed checks passed afterward. No additional source file was needed.
3. **[Root-requested quality closure]** After the initial checkpoint, root explicitly assigned the six inherited phase4-tag diagnostics to this already-owned fixture. A small rejected-credential helper reduced test-family complexity; formatting, the outage URL name and HTTP status constant were corrected without suppressions or configuration changes. Commit `a1fbd4f` retains all 19 subtests and 44 assertion calls; phase4/default/integration vet/lint and actual native Auth passed. This authorization supersedes the earlier deferral of those six findings.
4. **[Rule 1 — Reviewed expired fixture]** Independent review found that expiration rejection could be masked by invalid issued-at/not-before relationships. Tests-only `5147aa8` proved the negative lifetime; `11dddea` fixes only the two historical timestamps. Independent historical signature/required-claim acceptance and actual current session/list rejection now pass, with all prior assertions preserved. Root-requested normal full-stack restoration also passed after dependencies were observed stopped.

Karpathy guidance kept the changes within the approved launch/authentication repair. TDD/debugging and verification guidance supplied the observed failure → focused fix → actual runtime verification sequence. React guidance kept transient credentials in refs and cleanup tied to session lifetime; Compose guidance preserved existing pinned dependencies, loopback ports and volumes.

## Deferred Issues and Remaining Gates

- **Independent R1 review completed:** `04-REVIEW.md` is clean at `a302b65`, source `11dddea2fd58e38f841feb761edbcfee40800492`. It independently closes WR-04 and carries forward the original five closures with explicit unchanged-source proof. Original `04-REVIEW-01-R0.md` and `04-REVIEW-17-R0.md` retain their historical findings. All three tasks are accepted after root's additional post-wave check.
- **Historical scanner gate resolved by root:** earlier scans exited 1 on exactly three historical planning fingerprints (`778d3db` / `04-07-PLAN.md:58`; `5d11ca2` / `04-17-PLAN.md:70,101`). Independent review approved only those exact prose fingerprints under A1; root applied them in `852de68` and obtained the actual zero-finding full-history result recorded above. Original failures/reports remain preserved. This executor did not edit the allowlist, ownership proposal or review reports, and no broad exception/history rewrite was added.
- **Phase4-tag quality findings resolved:** historical `phase4-static.log` and reconfirming `lint-quality-before.log` retain the six findings; `lint-quality-green.log` now records zero issues after root's explicit fixture assignment. Earlier private baseline isolation/Go overlay attempts failed setup/typecheck and are not counted as baseline or acceptance proof. Default and integration gates also remain green.
- No full Phase 4, Phase 3 aggregate, full browser/SSE, hosted CI or performance/RSS guarantee is claimed. No 04-02 or Phase 5 work began. Overall tracking remains root-owned.

## Known Stubs and Threat Surface

No new goal-blocking stub was introduced. Initial empty React state is real pre-sign-in state; actual session/PG data sources are wired. The existing token CLI help refers to approved next slice 04-02; operator documentation explicitly says the CLI is not implemented yet. Existing future bench functionality is unchanged.

New private file handling, child secret environments, loopback proxy and retry credential lifetime are all within the plan's threat model. No new endpoint, schema boundary, authentication bypass or unregistered threat surface was introduced. No authentication gate occurred.

## Next Phase Readiness

Independent R1 review, scanner and root post-wave gates are complete. Root reconciled 04-01/04-17 as 2/17 accepted plans; proceed to approved 04-02. No Phase 5 transition or Phase 4 publication is authorized here.

## Root acceptance addendum

At reviewed HEAD `a302b65`, root independently ran `timeout 300 bash -c 'cd backend && go build ./... && cd ../web && npm run build && cd .. && make test'`. Private `root-post-review-gates.log` records actual command, SHA/start/end, exit 0 at 2026-10-22T09:20:02.477971Z; backend race, web 15 tests and generator 121 tests passed. This is bounded post-wave proof, not full integration/whole Phase 4 or hosted CI. The pinned full-history scanner separately passed 496 commits at `a302b65`; final metadata is scanned again before dispatch. User configuration and `.serena` remain untouched/unstaged.

## Self-Check: PASSED

Verified both new source/test files, this summary and all nine recorded task commits exist; checked scoped diff whitespace and zero unexpected deletions. Private evidence audit verified actual gate exits and the exact 12-source boundary before the initial metadata file was added. The quality follow-up preserved all 19 native subtests and 44 original assertion calls; WR-04 retains those assertions and adds independent temporal/signature checks. Its phase4/default/integration and fresh stack gate exits are zero. The production verifier, root's approved allowlist, ownership proposal and review reports remain unchanged by this follow-up. This self-check verifies the handoff's claims, not independent R1 acceptance.
