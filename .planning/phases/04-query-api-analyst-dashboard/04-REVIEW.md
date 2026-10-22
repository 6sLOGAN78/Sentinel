---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-22T00:39:23Z
depth: standard
review_sha: f28ef6eba7172bf66c01a75b8e2ee7f1f3d7d787
review_slice: 04-01
diff_base: 778d3db
files_reviewed: 11
files_reviewed_list:
  - api/openapi.yaml
  - backend/cmd/query-api/main.go
  - backend/go.mod
  - backend/go.sum
  - backend/internal/auth/jwt.go
  - backend/internal/storage/postgres/query_alerts.go
  - backend/test/openapi_test.go
  - backend/testphase4/auth_test.go
  - web/src/App.tsx
  - web/src/App.test.tsx
  - web/src/api/types.test.ts
findings:
  critical: 2
  warning: 3
  info: 0
  total: 5
status: issues_found
---

# Phase 4 Plan 01: Code Review Report

**Reviewed:** 2026-10-22T00:39:23Z  
**Depth:** standard  
**Frozen source:** `f28ef6eba7172bf66c01a75b8e2ee7f1f3d7d787`  
**Scope:** the 04-01 slice only; 11 tracked files, read in full.  
**Status:** issues_found

## Narrative Findings (AI reviewer)

The current increment has two integration blockers and three robustness/test-reliability warnings. This is not a whole-Phase-4 review. The approved later CLI, central configuration, richer client/UI, filters, triage, SSE and full browser/proxy acceptance are not treated as missing features. The findings below concern behavior already introduced or compatibility already required by 04-01.

`web/src/api/schema.d.ts` was explicitly supplied but is ignored generated output (`.gitignore:74`); it is excluded from the reviewed-source list/count under the review's ignore/generated-file rule. Relevant launchers, middleware, storage adapters, migration 003, local dependency code, approved PLAN/SUMMARY, amendments A1/A2, CONTRACT, UI-SPEC, PROJECT and ADRs 0006/0008 were consulted as context. No AGENTS.md or project skill directories were present. No structural pre-pass was supplied.

The existing reported GREEN checks do not exercise the missing Compose secret handoff or a real browser using the default Vite origin. Findings are source-traced reproductions; this reviewer did not rerun tests, start services, install dependencies, change source, or commit.

### Critical Issues

#### CR-01: BLOCKER — Mandatory JWT key breaks existing stack and lifecycle launches

**File:** `/home/logan78/Desktop/Sentinel/backend/cmd/query-api/main.go:48-50`

**Issue:** Every query-api process now exits before opening either listener unless `SENTINEL_AUTH.JWT_SECRET` exists. The existing Compose service explicitly supplies its environment at `deployments/compose/docker-compose.yml:297-314` but supplies neither this key nor an `env_file`; putting a key in the host shell or Compose interpolation file does not automatically forward it into that container. Consequently the existing `make dev-up` application startup (`Makefile:29`) cannot make query-api healthy, and the `seed`/`seed-scenarios` prerequisites also fail. This also breaks retained native lifecycle/readiness acceptance: `backend/test/lifecycle_test.go:93-97` constructs the query-api environment without a key, `startProc` assigns exactly that environment, and the graceful/sigint cases at lines 233/248 require the process to stay up. Those cases necessarily encounter the new startup failure instead. Recording the limitation in the SUMMARY does not preserve these previously working entry points.

**Reproduction:** Build the current query-api using the existing Compose service and invoke the documented `make dev-up`, or select the retained query-api lifecycle cases against their normal fixtures. The new setup branch returns `SENTINEL_AUTH.JWT_SECRET is required`; no query/ops listener is opened. This is a deterministic source trace, not a newly executed runtime result.

**Fix:** Wire private, operator-supplied or privately generated random key provisioning through the existing supported local launch paths and forward it explicitly into the Compose query-api container; supply independent ephemeral keys to retained query-api process fixtures. Update the local setup instructions/template without committing a usable credential. Keep startup failure for missing/short keys and keep all dashboard routes behind verification. Recheck retained lifecycle/readiness families and the documented Compose/seed startup after the handoff is wired.

#### CR-02: BLOCKER — Browser fetches have no route to the query API in the current local launch

**File:** `/home/logan78/Desktop/Sentinel/web/src/App.tsx:78-81` (also alert fetch at lines 46-50)

**Issue:** Both newly introduced fetches use relative `/api/v1/...` URLs, but the available web launcher (`web/package.json`, `dev: vite`) serves a different origin from query-api. `web/vite.config.ts:5-12` defines only the plugin/test configuration and no API proxy. Compose publishes query-api on 8081 and supplies no web server or shared-origin proxy; query-api itself serves no web assets. Thus launching the available web development server and the actual query-api does not connect this slice: the session request goes to Vite (normally port 5173), whose SPA fallback returns the HTML application for a browser fetch with `Accept: */*`. `response.json()` then fails, and even a valid supplied JWT produces the invalid-token message. Mocked component fetches cannot expose this failure.

**Reproduction:** With a valid JWT and a healthy query-api on 8081, run `cd web && npm run dev`, open its URL, and sign in. The browser requests `/api/v1/session` from the Vite origin rather than 8081, so no verified identity or real alert list is obtained. The routing/fallback behavior was traced in the installed Vite implementation; no real-browser run is claimed here.

**Fix:** Provide the minimal supported local connection for this current slice, for example a Vite `/api` proxy to the local query-api, and document the two launch commands/key prerequisite. Alternatively configure an API origin together with the appropriate explicit CORS policy. Keep the supplied credential in the Authorization header. The later multi-replica/proxy/SSE browser acceptance can remain in 04-15; this fix only makes the current sign-in/read path reachable.

### Warnings

#### WR-01: WARNING — Sign-in reports service/network failures as invalid credentials

**File:** `/home/logan78/Desktop/Sentinel/web/src/App.tsx:83-93`

**Issue:** Every non-2xx `/session` response, a failed network request, and an invalid response body take the same catch path as a rejected JWT. A 503 (explicitly part of the new session contract) or temporary proxy/network outage therefore tells the operator their valid credential could not be verified and should be regenerated. The input has already been cleared and there is no corresponding retry action, so transient availability failures are misleading and unnecessarily discard the attempted sign-in. The existing alert-read path correctly distinguishes 401 from a retriable read failure, but session bootstrap does not.

**Fix:** Handle session 401 as the invalid-token case and show a distinct availability/read error with a usable retry path for network/5xx failures. Keep any retained retry credential only in memory and clear it on cancellation/sign-out. Add component assertions that a 503 and a network failure do not display the invalid-token copy and that retry can succeed.

#### WR-02: WARNING — Wrong-algorithm negative test uses an invalid signature for that algorithm

**File:** `/home/logan78/Desktop/Sentinel/backend/testphase4/auth_test.go:234` (case at lines 126-133)

**Issue:** The `wrong_algorithm` case changes the JWT header to `HS384`, but `signToken` always computes an HMAC with SHA256. Its signature therefore fails HS384 verification independently of the verifier's HS256-only method whitelist. Removing `jwt.WithValidMethods([]string{"HS256"})` would still leave this assertion green, despite permitting correctly signed HS384 dashboard tokens. This test cannot support the claimed algorithm-restriction regression coverage.

**Fix:** Sign the HS384 negative fixture using HMAC-SHA384 (for example `sha512.New384`) or the pinned JWT library's actual HS384 signing method, while preserving the HS256 positive fixture. Establish that the fixture is valid under HS384 with the same key; then require production verification to reject it solely because the allowed algorithm is HS256.

#### WR-03: WARNING — Credential-leak assertion misses disclosure of the exact JWT

**File:** `/home/logan78/Desktop/Sentinel/backend/testphase4/auth_test.go:143`

**Issue:** The test checks that the response does not contain `raw + "."`, rather than checking the supplied credential itself. An error response containing the complete JWT verbatim, without an extra trailing period, passes this assertion. This leaves the negative credential-disclosure check ineffective for the most direct leak it is intended to catch.

**Fix:** When `raw` is nonempty, assert that the response does not contain `raw` itself; separately assert that it does not contain the supplied API key. Skip the empty-token substring assertion rather than adding punctuation. Include a deliberately credential-bearing response in a small negative-control check if needed to demonstrate that the assertion fails on disclosure.

---

_Reviewed: 2026-10-22T00:39:23Z_  
_Reviewer: gsd-code-reviewer_  
_Depth: standard; frozen 04-01 scope only_
