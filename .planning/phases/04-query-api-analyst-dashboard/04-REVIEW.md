---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-22T09:16:55Z
depth: standard
review_revision: R1
review_sha: a302b65cd6b511f23502592cc0712842e4fcde55
source_sha: 11dddea2fd58e38f841feb761edbcfee40800492
prior_full_review_sha: c3b060bbcfdcd364f353e2d95f10208e318c925e
review_slice: 04-01 and 04-17
review_method: focused_delta_with_unchanged_scope_carry_forward
diff_base: 358114c
repair_diff_base: f28ef6eba7172bf66c01a75b8e2ee7f1f3d7d787
delta_base: c3b060bbcfdcd364f353e2d95f10208e318c925e
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
  warning: 0
  info: 0
  total: 0
status: clean
historical_scanner_amendment: approved_and_applied_exactly
scanner_gate: passed_at_reviewed_head
---

# Phase 4 Plans 01 and 17: Code Review Report — R1

**Status:** clean — no unresolved BLOCKER or WARNING in the scoped review.

**Frozen HEAD:** a302b65cd6b511f23502592cc0712842e4fcde55

**Source candidate:** 11dddea2fd58e38f841feb761edbcfee40800492

**Depth and method:** standard; focused repair re-review plus the unchanged scope carried forward from the full 21-file review at c3b060b. This revision does not claim that all files or runtime gates were freshly rerun.

## Narrative Findings (AI reviewer)

WR-04 is closed. The repaired expired credential has valid original temporal relationships, verifies independently during its historical validity interval, and is then rejected by both actual production endpoints using the same signed bytes. No new defect was found in the reviewed delta. The original five findings remain closed. The previously approved scanner amendment was applied exactly, and the retained pinned full-history scan at the frozen HEAD has zero findings and exit 0.

### Scope continuity and preserved history

The prior full review is preserved in 04-REVIEW-17-R0.md, Git blob 0652add1743e1ae7badc58167d192d6b157a11af. It records all 21 source files read in full, the five original closures, and WR-04 as the sole remaining WARNING. The original 04-01 review is preserved verbatim in 04-REVIEW-01-R0.md, blob 58fef39594e4c6fac6d1e01b555e79d1f6f3e9e6. The bounded ownership check remains unchanged, blob 8330646e59c7ec367f895f8857e469018aa69bd7. These historical reports keep their original outcomes.

The exact 21-file list in that prior report was parsed and compared with Git changes from c3b060b through a302b65. Only backend/testphase4/auth_test.go and .gitleaksignore changed; the other 19 files are byte-identical. All 21 are also unchanged from source commit 11dddea through frozen HEAD a302b65. The complete repository diff adds only those two source changes plus the named review/ownership/SUMMARY artifacts. Production auth, query storage/routes, browser code, launch helper, Make/Compose, CI, dependencies and scanner configuration are unchanged. R1 read the full current Auth fixture, full verifier, full ignore file, the fixture/ignore diffs, relevant route/caller code, updated summary delta, and new private evidence. Unchanged source conclusions are carried from R0, not represented as a second full read.

No AGENTS.md or project skill directories were present in the original full review. No structural pre-pass was provided. Ignored generated web/src/api/schema.d.ts remains excluded from the source count. Later approved CLI/configuration, richer client, filtering, triage, SSE and full browser acceptance remain outside this completed increment's review.

### Closure of WR-04 — WARNING, expired-token fixture

**Current file:** /home/logan78/Desktop/Sentinel/backend/testphase4/auth_test.go:137-150,170-208

The original fixture set exp to one minute ago while leaving iat/nbf at now; it was independently invalid under production jwt.go:78-79. Tests-only commit 5147aa8 added the independent fixture checks and demonstrated the old negative one-minute lifetime. GREEN commit 11dddea changes only the expired case's iat and nbf to ten minutes ago (lines 205-208), retaining exp one minute ago and all existing native subtests/assertions.

Before either endpoint call, requireOtherwiseValidExpiredCredential receives the same raw string used at lines 147-149. It independently parses that raw credential with HS256, the correct key, issuer, audience, subject, required expiration/not-before, issued-at checking, strict decoding and 30-second leeway, using a clock five minutes ago (170-182). It asserts required dates, a positive lifetime no longer than 15 minutes, nbf <= exp, current expiry beyond the 30-second leeway, successful historical verification, and the expected tenant/role (183-194).

The repaired interval is nine minutes long and valid at the independent clock. At the real current clock, both /api/v1/session and /api/v1/alerts must return 401, and exact JWT/API-key absence checks remain required. The production route middleware and verifier are unchanged. Thus the old independent lifetime/not-before defects cannot mask the current expiration assertion. No permissive parser or historical clock was added to production.

The retained behavioral RED names reject_expired and fails at the positive-lifetime assertion with -1m; its other 18 subtests pass. The GREEN repeats the exact nonempty phase4/race/6m Auth family and passes all 19 subtests, package 6.571s. This supports fixture repair; it is not represented as a production verifier defect or mutation test.

### Carried-forward disposition of the original five findings

| Finding | Disposition and causal proof retained from the full review |
|---|---|
| CR-01 — BLOCKER, missing launch key | **Closed.** Makefile:4,78 routes Compose and host query-api through the private wrapper before child local-mode settings; other host services have both JWT names removed. dev_auth_env.py:92-101 checks modes/overrides and forwards both names; Compose:297-299 explicitly maps the dotted application key. main.go:48-54 still fails closed on missing/short keys. lifecycle_test.go:95-104 supplies independent random keys, and 304-322 tests actual missing/short-key startup rejection. Retained readiness helpers reuse serviceEnv. Actual host, lifecycle/readiness, dev-up and exact 1000-ID seed evidence remains preserved. The new normal dev-up/check-stack log independently observes all nine healthy at source 11dddea. |
| CR-02 — BLOCKER, browser/API routing | **Closed.** vite.config.ts:6-8 binds loopback and fixes /api proxy target to 127.0.0.1:8081. Header-only relative fetches and docs/operations.md:46-69 connect the supported launchers. The reviewed real Vite probe verifies session JSON and an own-tenant PG list with a foreign fixture excluded. Original HTML RED and JSON GREEN are retained. No full browser/SSE execution is inferred. |
| WR-01 — WARNING, availability feedback | **Closed.** App.tsx:136-180 distinguishes session 401 from network/5xx/invalid identity failures and supplies a retry action. Input clears immediately; retry credentials remain only in a ref with a finite deadline. Success/cancel/sign-out/expiry/unmount clear credentials and abort work; aborted checks after fetch and JSON parsing suppress stale identity/data. Existing component tests cover availability retry, cancellation, expiry and stale responses. Browser source/tests are unchanged. |
| WR-02 — WARNING, HS384 fixture | **Closed.** Current auth_test.go:301-304 signs HS384 with SHA384. Its independent parser at 88-93 accepts the fixture; the unchanged production whitelist accepts HS256 only and the actual endpoint loop rejects HS384. Fresh Auth GREEN retains both independent-validity and production-rejection cases. |
| WR-03 — WARNING, exact credential disclosure | **Closed.** Current auth_test.go:222-229 checks exact nonempty credentials without added punctuation. Lines 95-98 test verbatim JWT and API-key disclosure independently; 147-149 retain the real response checks. Sanitized logger/error mapping is unchanged. Fresh Auth GREEN retains these controls. |

The descriptor-relative private filesystem/owner/permission/symlink/hardlink/concurrency analysis, child-only environment/argv/output checks, parameterized tenant SQL, actual DTO columns, canonical contracts and complete CI selector/cap analysis in 04-REVIEW-17-R0.md remain applicable by the explicit unchanged-source proof. In particular, the only prior CI change remains the helper hygiene test; no retained Phase 3 block, selector, matrix, failure handling or time cap changed.

### R1 evidence inspected

Private logs are under .local/phase4-acceptance/04-17/ and the inspected files retain mode 0600. Commands, actual base SHAs, UTC times, named outcomes and exits were read. Precommit logs identify their base plus the then-working repair subsequently committed; they are not relabelled as execution at final HEAD.

| Evidence | Actual observation |
|---|---|
| expiry-fixture-behavior-red.log | Base 852de68 plus tests subsequently committed as 5147aa8; 09:07:18.285815–09:07:24.248891Z; exact Auth/phase4/race/6m; exit 1 at the old fixture's -1m lifetime, other 18 subtests pass. Initial dependency-connection setup failure is separately retained and is not counted as behavioral RED. |
| expiry-fixture-green.log | Base 5147aa8 plus the two timestamps subsequently committed as 11dddea; 09:08:03.336868–09:08:10.901155Z; exact Auth/phase4/race/6m; all 19 subtests pass, package 6.571s, exit 0. |
| expiry-static-green.log | Same precommit base/repair; 09:08:04.530756–09:08:16.648271Z; phase4 vet/lint, default race/vet/build/lint, integration vet/lint; each command retains its 120s cap, default Go test 60s; chain exits 0 with zero lint issues. Some default packages are explicitly cached. |
| wr04-fresh-stack.log | Source 11dddea; 09:08:58.918601–09:10:16.778483Z; normal timeout 16m make dev-up followed by check-stack; exit 0. All four dependencies/five applications healthy, topic partitions/retention/auto-create-off, listener ports, real readiness and dependency queries pass. No reset or new seed producer is claimed. |
| wr04-final-history-scan.log / wr04-final-history-report.json | Frozen HEAD a302b65; 09:13:25.928009–09:13:26.779257Z; full-history redacted gitleaks scan, 496 commits, no leaks found, exit 0; JSON report is exactly an empty array. Reviewer separately checked the executable version is 8.30.1. |

The earlier 59 script tests, 15 web/type-fixture tests, 11 OpenAPI tests, full readiness/lifecycle selection, host/Vite HTTP probes and exact 1000-ID seed records remain earlier evidence as documented in R0/SUMMARY. They were not all freshly rerun for this test-only delta, and this report makes no contrary claim. This reviewer did not rerun native producers/tests, restart services or execute a replacement scan; the new observations above are retained executor/root evidence inspected independently.

### Applied scanner amendment

**Decision: the applied amendment exactly conforms to prior bounded APPROVE. Scanner gate passes at the reviewed HEAD.**

Commit 852de68 adds two explanatory comment lines and only the three previously approved complete commit/path/rule/line fingerprints to .gitleaksignore: historical 778d3db.../04-07-PLAN.md:58 and 5d11ca2.../04-17-PLAN.md:70,101. A read-only set/order comparison verified those exact three lines against 04-17-OWNERSHIP-A1-CHECK.md and verified every earlier exception is unchanged. The prior historical-line inspection classified these as test-policy/session-outage prose with no usable credential.

No scanner rule/path/content-wide allowlist, scanner configuration or scanner CI command changed. Any other fingerprint remains subject to the normal failure gate. Earlier exit-1 logs/reports remain preserved, and the exact empty report/full-history exit-0 evidence above supersedes their pending gate without erasing them. The frozen scan predates this review-artifact update; root should retain normal final metadata hygiene/scanning before any later acceptance or publication.

### Review limits and handoff

All six recorded findings are closed without a waiver. This is a clean source review for 04-01/04-17 and the named source candidate. It does not certify whole-Phase-4 completion, full Phase-3 aggregate, full browser/SSE, hosted CI or a performance/security guarantee, and it performs no later-slice work.

Only 04-REVIEW.md was updated during R1. The immutable R0 reports, ownership check, source and user-owned .planning/config.json/.serena changes were preserved. No commit, remote action or publication was performed. The orchestrator owns acceptance/tracking and subsequent workflow decisions.

---

_Reviewed: 2026-10-22T09:16:55Z_

_Reviewer: gsd-code-reviewer_

_Depth: standard; focused R1 delta with explicit unchanged 21-file scope carry-forward_
