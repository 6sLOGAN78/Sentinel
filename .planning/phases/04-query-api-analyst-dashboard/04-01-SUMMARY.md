---
phase: 04-query-api-analyst-dashboard
plan: "01"
subsystem: auth
tags: [jwt, postgres, echo, react, openapi]
requires:
  - phase: 03-detection-alert-pipeline
    provides: Authoritative PostgreSQL workflow rows and retained event lookup
provides:
  - Strict HS256 verified dashboard principals and GET /api/v1/session
  - Tenant-scoped bounded PostgreSQL workflow pages independent of ClickHouse
  - Memory-only browser sign-in and generated-type workflow rendering
affects: [04-02, 04-03, 04-04, dashboard-authentication]
tech-stack:
  added: [github.com/golang-jwt/jwt/v5 v5.3.1]
  patterns: [verified server principal, parameterized tenant reads, generated workflow DTO]
key-files:
  created:
    - backend/internal/auth/jwt.go
    - backend/internal/storage/postgres/query_alerts.go
    - backend/testphase4/auth_test.go
  modified:
    - backend/cmd/query-api/main.go
    - backend/go.mod
    - backend/go.sum
    - api/openapi.yaml
    - web/src/App.tsx
    - web/src/App.test.tsx
    - web/src/api/types.test.ts
    - backend/test/openapi_test.go
key-decisions:
  - "Verify the operator-supplied JWT before extracting tenant authority; keep credentials in browser memory."
  - "Expose only the 15 actual public PostgreSQL workflow fields; preserve canonical immutable Alert separately."
  - "Open ClickHouse lazily while retaining its actual readiness check, so PG reads survive a CH outage."
patterns-established:
  - "Phase4 native tests run the actual built command with the phase4 tag only."
  - "Ownership amendments A1/A2 explicitly cover affected retained contract fixtures."
requirements-completed: [AUTH-01, AUTH-02, AUTH-03, API-03, UI-02]
duration: 455min
completed: 2026-10-22
---

# Phase 4 Plan 01: Verified sign-in and tenant workflow reads Summary

HS256 dashboard sessions connect a memory-only React sign-in to real tenant-isolated PostgreSQL alerts, including when ClickHouse is unreachable.

## Performance

- Recorded wall span: approximately 455 minutes, 2026-10-21T16:53:22Z through 2026-10-22T00:28:07Z. Execution was interrupted and resumed; this is not active execution time or a performance benchmark.
- Tasks: 2/2, including the previously committed RED task.
- Source files changed across RED/GREEN: 11; the twelfth declared file, ignored `web/src/api/schema.d.ts`, is regenerated normally by npm lifecycle scripts.
- Tracking ownership: the orchestrator owns subsequent STATE/ROADMAP/REQUIREMENTS updates. This summary is committed before those updates.

## Accomplishments

- JWT verification restricts algorithms to HS256, issuer to sentinel-dev and audience to sentinel-dashboard. Subject, tenant, known role, exp/iat/nbf, positive lifetime no longer than 15 minutes, 30-second leeway, 8 KiB token limit and operator key of at least 32 bytes are enforced before creating a principal. Ambiguous Authorization headers are rejected. Authentication failures do not expose credentials or parser details.
- GET/session returns verified identity. GET/alerts uses the verified tenant in parameterized SQL, returns at most 50 rows by default/200 maximum, and freezes a 24-hour range across descending `(created_at, alert_id)` pages. Cursors are size bounded, version checked, tenant checked and range bounded. The response contains actual workflow fields, without invented detection/evidence fields.
- Existing API-key single-event reads remain available. Supplying a bearer header cannot downgrade to API-key authority. Lazy ClickHouse connection opening permits session/list service without changing dependency readiness.
- The browser verifies identity before loading workflows, masks the supplied token, retains it only in memory and clears data/aborts requests on sign-out or expiry. It uses generated OpenAPI response types and the approved sign-in/loading/error copy.

## Task Commits

1. Task 1 — Pin behavior and demonstrate causal RED: `684c521` (`test`).
2. Task 2 — Deliver sign-in and real tenant alert reads: `50ba19f` (`feat`).

Ownership/checkpoint documentation retained between them: `8d6a891` (A1), `0a111b5` (A2), `785f343` (interruption checkpoint). No history was rewritten.

## Verification and Evidence

Private raw logs remain in ignored `.local/phase4-acceptance/04-01/`, parent directory mode 0700 and acceptance logs mode 0600. Build/test commands run with normal umask 022. No credentials or request headers are logged.

RED logs identify base SHA `778d3db` plus the tests subsequently committed as `684c521`. These were separate commands, each with its own actual exit:

| Evidence | UTC start → end | Actual result |
|---|---|---|
| `native-red.log`: exact TestPhase4Auth command below | Oct21 16:53:22 → 16:53:44 | Exit 1; verified session/list expected 200 but received 404, required rejection expected 401 but received 404. The unreachable-CH process also failed to become live. |
| `web-red.log`: `cd web && npm test` | Oct21 16:53:27 → 16:53:30 | Exit 1; 4 new sign-in assertions failed, 5 retained tests passed. |

Fresh final GREEN logs identify precommit base SHA `785f343` plus the exact owned implementation committed as `50ba19f`. Source did not change between these checks and that commit. The retained earlier failures are not relabelled passes.

| Evidence / exact command | UTC start → end, Oct22 | Actual result |
|---|---|---|
| `native-green-final.log`: `bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Auth$' -tags=phase4 -race -timeout=6m -p=1` | 00:26:27 → 00:26:38 | Exit 0; all 17 subtests, package 9.438s. |
| `web-green-recovered.log`: `cd web && timeout 120s npm test && timeout 120s npm run typecheck && timeout 120s npm run lint && timeout 120s npm run build && timeout 120s npm run format:check` | 00:26:14 → 00:26:23 | Exit 0; 9 component/type-fixture tests, generated schema, typecheck, lint, build and formatting passed. |
| `retained-openapi-final.log`: `bash scripts/go-test-nonempty.sh './test/...' '^TestOpenAPI' -race -timeout=60s -p=1` | 00:26:27 → 00:26:31 | Exit 0; 11 top-level retained tests, package 1.880s; exact route map, canonical schema drift and security assertions retained. |
| `backend-static-final.log`: `cd backend && timeout 120s go test -race -timeout 60s ./... && timeout 120s go vet ./... && timeout 120s go build -o /dev/null ./cmd/query-api` | 00:26:40 → 00:27:35 | Exit 0 for the fail-closed command chain; default suite, vet and build passed. |
| `backend-lint-final.log`: `cd backend && timeout 120s go tool -modfile=tools.mod golangci-lint run ./...` | 00:26:27 → 00:26:37 | Exit 0; zero issues. |

Native proof uses the real PostgreSQL fixture, applies actual migrations, inserts two unique synthetic tenants, builds query-api with the race detector, and launches actual production processes on unique ports with bounded cleanup. It verifies the 50-row first page, 200 cap, tenant isolation, required-claim/signature/algorithm/expiry/oversize rejection, legacy read-key compatibility and no bearer fallback. A separate freshly launched process points ClickHouse at an unreachable address: session/list return 200 while actual `/readyz` returns 503. This proves the first auth/PG slice; it does not claim browser automation or whole-pipeline acceptance.

Read-only startup checks found the preserved dependency containers exited and about 11 GB free disk. Only the existing PostgreSQL/ClickHouse/Redis containers were started; all three became healthy. Kafka/application restart state was not used as evidence for this slice. No reset, recreation, pruning, volume deletion or fabricated cause of the earlier interruption occurred.

## Files Created/Modified

- `backend/internal/auth/jwt.go`: strict verifier, typed principal access and role helper.
- `backend/internal/storage/postgres/query_alerts.go`: public workflow DTO and bounded tenant-only list.
- `backend/cmd/query-api/main.go`: authenticated session/list routing, explicit legacy bearer rejection, lazy CH opening with unchanged readiness checks and small wiring helpers.
- `backend/go.mod`, `backend/go.sum`: pinned direct JWT dependency.
- `api/openapi.yaml`: verified Session and separate WorkflowAlert schemas; list response changes without changing canonical Event/Alert.
- `backend/testphase4/auth_test.go`: native actual-command family and corrected public-field count.
- `web/src/App.tsx`, `web/src/App.test.tsx`: thin real sign-in/read UI and component assertions.
- `web/src/api/types.test.ts`: independent workflow fixture while preserving canonical Alert coverage.
- `backend/test/openapi_test.go`: add only GET/session to the exact expected route list.
- `web/src/api/schema.d.ts`: generated ignored output, not staged or hand maintained.

## Deviations from Plan

1. **[Rule 3 — Blocking contract consumer] Approved ownership amendment A1.** Updating AlertList to the actual workflow DTO caused retained typecheck failure. Independent approval `8d6a891` added `web/src/api/types.test.ts`; the repair keeps the canonical Alert fixture and adds a separately typed workflow fixture. Verified by all web checks; included in `50ba19f`.
2. **[Rule 3 — Blocking contract consumer] Approved ownership amendment A2.** The retained exact-route test rejected the new documented session route. Independent approval `0a111b5` added `backend/test/openapi_test.go`; only the session route expectation was appended, preserving strict map equality. Verified by the nonempty retained OpenAPI family; included in `50ba19f`.
3. **[Rule 1 — Fixture bug] Corrected the public workflow field count from 16 to the actual 15** during Task 2. The immutable fingerprint and internal sequence remain excluded; no public field was fabricated to satisfy the mistaken count. Native GREEN proves the corrected assertion. Included in `50ba19f`.
4. **[Rule 3 — Blocking lint] Repaired owned complexity, shadow, line-length, numeric-constant and selector findings** by extracting small handler/query-validation functions, using named errors/constants and preserving behavior. Final full backend lint reports zero issues; included in `50ba19f`. No linter/config weakening or additional source file was introduced.

The full approved ownership declaration is 12 files including generated schema and A1/A2 fixtures. No thirteenth source file was edited. The Karpathy guidelines kept repairs within this slice and retained explicit verification outcomes.

## TDD Gate Compliance

The tests-only `684c521` precedes GREEN `50ba19f`. Original native RED contains actual missing-route assertion failures, separately from its unreachable-dependency startup failure. GREEN also contains the explicit 16→15 fixture correction; that correction is not claimed as an independently demonstrated RED behavior. The retained typecheck/OpenAPI/lint failures were resolved before GREEN. No passing empty selector or masked failure is counted.

## Known Stubs

None preventing this plan's first sign-in/read increment. Empty lists, nullable assignee/cursor and initially empty browser state are actual response/state semantics. The on-screen development CLI instruction refers to the next owned slice, 04-02; no CLI has been implemented or claimed here.

## Limits and Next Owned Refinements

The requirements frontmatter lists this plan's incremental requirement coverage; it does not certify the full AUTH/API/UI requirement matrix. Role mutation enforcement, richer alert filters/detail/pagination controls, React Query/session extraction, full browser/proxy acceptance, SSE/backfill and complete cross-tenant matrix remain owned by later approved plans. This slice accepts limit/cursor only; planned filters documented in the shared contract are not yet active.

04-02 owns central auth configuration and the development token CLI; 04-03 owns richer session/client/UI extraction; 04-04 owns the complete filtered alert paging behavior. The existing Compose query-api environment has not been changed by this plan: running this new binary requires the mandatory operator-provided `SENTINEL_AUTH.JWT_SECRET` environment variable. Full Phase3 integration families and hosted CI were not rerun or claimed here. No remote publication was attempted.

The source scan found no new security surface outside the plan's JWT/session/tenant-PG/browser threat model, no unsupported stubs, and no tracked binary. User-owned `.planning/config.json` and `.serena/` remain untouched and unstaged. No authentication gate occurred.

## Self-Check: PASSED

Verified the three created source/test files exist; both task commits `684c521` and `50ba19f` exist in git history. Checked the RED→GREEN ordering, all final recorded exit codes, generated-schema ignore status, scoped diff whitespace, zero unexpected deletions and no generated untracked files. Only the pre-existing user configuration/Serena changes remain outside the summary commit.
