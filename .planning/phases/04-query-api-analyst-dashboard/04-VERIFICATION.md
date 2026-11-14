---
phase: 04-query-api-analyst-dashboard
verified: 2026-11-14T23:08:00Z
status: gaps_found
score: 5/6 must-haves verified (roadmap SC1-SC5 verified in code + local native evidence; SC6 "CI green / 04-16 fail-closed acceptance" FAILED)
overrides_applied: 0
gaps:
  - id: G1
    truth: "Phase 4 acceptance is fail-closed and green in hosted CI (04-16, spec §18 DoD: CI is green)"
    requirement: "Spec §18; 04-16 Task 3; proof for AUTH-03, API-05..07, LIVE-01..03, UI-01..06"
    severity: blocker
    status: failed
    reason: >
      Hosted run 43649750865 (chocopie247/Sentinel @ b764e24) fails phase4 (triage), phase4 (stream),
      phase4 (summary), phase4 (tenant) and phase4-browser. Root cause is one harness defect: the native
      families and the browser seeder rely on ambient env exported by the private local launcher.
      config.Load only reads .env when SENTINEL_PRIMARY.ENV=local is already in the process env
      (config.go:432-436); CI never sets it. Families that t.Setenv it (auth, alerts, events) pass or run;
      families that do not fail with "validate primary config: ... 'Primary.Env' ... required".
    evidence:
      - "backend/testphase4/triage_test.go:43-45 (no SENTINEL_PRIMARY.ENV/ENV_FILE before config.Load) -> CI log: triage_test.go:45 Primary.Env required"
      - "backend/testphase4/stream_test.go:89-94 -> CI log: stream_test.go:94 same error"
      - "backend/testphase4/tenant_matrix_test.go:97-99 -> CI log: tenant_matrix_test.go:99 same error"
      - "backend/testphase4/delivery_test.go:104-105 (same pattern; job still running at report time)"
      - "backend/testphase4/summary_rules_test.go:46-65 sets PRIMARY.ENV=local but never calls config.Load before startQueryAPI, so .env is never loaded into os env; child gets PRIMARY.ENV=test (auth_test.go:283-285) with no DB/CH config -> CI log: summary_rules_test.go:65 'production query-api did not become live within 15s'"
      - "scripts/check-phase4-browser.sh:134-136 runs `go run ./tools/keyctl ensure` with no PGHOST/PGPORT/PGUSER/PGPASSWORD; backend/tools/keyctl/main.go:233-248 reads only PG* env -> CI log: CalledProcessError, 'FAIL: seeding failed'"
    artifacts:
      - path: "backend/testphase4/{triage,stream,tenant_matrix,delivery,summary_rules}_test.go"
        issue: "depend on undeclared ambient env from the private launcher"
      - path: "scripts/check-phase4-browser.sh"
        issue: "seeder keyctl call depends on launcher-provided PG* env"
    missing:
      - "In every TestPhase4* family: t.Setenv(\"SENTINEL_PRIMARY.ENV\",\"local\") and t.Setenv(\"SENTINEL_ENV_FILE\", filepath.Join(root,\".env\")) and call config.Load once before building/starting the child query-api (summary_rules_test.go before line 65); or move this into a shared testphase4 harness helper"
      - "check-phase4-browser.sh: derive and export PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE from the repo .env (as the launcher does) before seeding, or let keyctl fall back to config.Load(database)"
      - "Add a guard in test_phase4_guards.py that runs each family's env bootstrap with a scrubbed environment (env -i + PATH/HOME) so launcher dependence is caught locally"
      - "Re-run hosted CI and record all 8 phase4 jobs + phase4-browser green"
  - id: G2
    truth: "Full CI (legacy + Phase 3 groups preserved by 04-16) is green"
    requirement: "Spec §18 DoD; 04-CONTRACT 'Preserve ... 44core+2scenarios'"
    severity: blocker
    status: failed
    reason: >
      Hosted phase3 (scenarios) failed: TestDetectionScenarios/isolated-static-{boundary,lateness,all}
      fail with 'private verifier verify failed at current_event_rows'. Not owned by Phase 4 code, but
      Phase 4 cannot claim CI green; root cause not diagnosed (possibly the same ambient-env class as G1,
      possibly a real regression). integration and phase3 (core) were still running at report time.
    evidence:
      - "backend/test/scenarios_pipeline_test.go:54,204,232 (CI job 132617003591 log lines 1454-1484)"
    missing:
      - "Diagnose the current_event_rows verifier failure on a clean hosted stack; fix the harness or the regression; re-run hosted CI"
  - id: G3
    truth: "Metrics summary and rules endpoints reject undeclared query fields consistently with the rest of the API"
    requirement: "API-08 (invalid/boundary requests covered); openapi.yaml summary declares 400"
    severity: minor
    status: partial
    reason: >
      GetSummary and ListRules never inspect the query string, so ?foo=1 or ?from=... return 200.
      Every other Phase 4 endpoint rejects unknown fields (alerts.go:123, triage.go:41, handler.go:132).
      04-CONTRACT does not explicitly mandate 400 for these two routes; summary's OpenAPI lists a 400 and
      says 'No caller ranges', rules lists no 400. Not a security issue (no parameter is used), so minor.
    evidence:
      - "backend/internal/query/summary.go:91-95"
      - "backend/internal/query/rules.go:53-56"
      - "api/openapi.yaml:380-399 (rules: no 400), 401-436 (summary: 400 declared, no parameters)"
    missing:
      - "Add `if c.Request().URL.RawQuery != \"\" { return errs.NewBadRequestError(...) }` after RequirePrincipal in both handlers; add 400 to /api/v1/rules in openapi.yaml; add unit cases for ?x=1 on both routes"
  - id: G4
    truth: "The Alerts page only announces 'New alerts available' for deliveries that arrived after the page loaded"
    requirement: "UI-02 / LIVE-01 (live feed UX)"
    severity: minor
    status: partial
    reason: >
      On mount the live client backfills from cursor '0' (no cursor exists for a new APIClient) and
      walks the tenant's whole durable delivery log in 200-item pages; every non-empty page calls the
      process callback, which sets arrivals=true. Any tenant with history therefore sees the banner on
      first load. The same walk costs O(history/200) requests per new session (bounded per page, not
      unbounded memory).
    evidence:
      - "web/src/api/live-alerts.ts:99-108 (backfill from cursors.get(client) ?? \"0\"; process(page.items))"
      - "web/src/pages/Alerts.tsx:191-201 (if items.length -> setArrivals(true))"
      - "web/src/pages/Alerts.tsx:315-317 (banner)"
    missing:
      - "Treat the initial catch-up walk as a baseline: pass an `initial` flag (true until the first has_more=false page) and skip setArrivals while initial; or add a server head-seq (e.g. backfill returning current tenant head, or after_seq=latest) so new sessions start at head; add a Vitest case 'no banner on first load with existing history'"
  - id: G5
    truth: "OpenAPI documents the delivered resolve transition"
    requirement: "API-05, UI-06 (typed client generated from OpenAPI)"
    severity: minor
    status: partial
    reason: "PATCH description still says resolve is undelivered; code delivers acknowledged->resolved (triage.go storage:36-41, handler decodeTransition accepts resolved). Stale text propagates into the generated client."
    evidence:
      - "api/openapi.yaml:343 'Only open to acknowledged is currently delivered; resolved returns 409.'"
      - "web/src/api/schema.d.ts:220 (generated copy)"
      - "backend/internal/storage/postgres/triage.go:36-41"
    missing:
      - "Replace with 'Allowed transitions: open->acknowledged and acknowledged->resolved; skip, reopen and no-op return 409.' then run `npm run gen:api` in web/"
  - id: G6
    truth: "Phase planning artifacts reflect the accepted state"
    requirement: "Evidence hygiene (04-CONTRACT 'Record ... truthfully')"
    severity: minor
    status: partial
    reason: "04-07-STACK-RECOVERY-PLAN.md header still says runtime gates pending; 04-07-STACK-RECOVERY.md (status PASS, 07 accepted 2026-10-24) and 04-07-STACK-RECOVERY-CHECK.md (APPROVE/PASS) supersede it."
    evidence:
      - ".planning/phases/04-query-api-analyst-dashboard/04-07-STACK-RECOVERY-PLAN.md:3-4"
      - ".planning/phases/04-query-api-analyst-dashboard/04-07-STACK-RECOVERY.md:3-6"
    missing:
      - "Add a 'Status: superseded by 04-07-STACK-RECOVERY.md (PASS, accepted 2026-10-24)' line at the top of the plan file"
  - id: G7
    truth: "The end-to-end live alert flow survives an idle stack (no permanent detector failure)"
    requirement: "Core value (generator->...->live dashboard); spec §3 recovery; affects SC4 in practice. Cross-phase (Phase 3 detector), not a Phase 4 requirement"
    severity: major
    status: partial
    reason: >
      If the stack is idle longer than security.events.v1 retention (24h), the detector refuses to start
      with 'detector checkpoint history lost' even when every partition is empty (earliest == end, nothing
      skipped). Recovery has twice required renaming the consumer group via a private overlay
      (sentinel-detector-p4r2, -p4r3). On any dev machine left off for a day, new alerts stop reaching the
      dashboard. No later roadmap phase covers it.
    evidence:
      - "backend/internal/kafkax/detector_consumer.go:468-470"
      - ".local/phase4-acceptance/04-15/detector-hold-recovery-NOTE.md (2026-10-30 and 2026-11-13 recurrences)"
      - ".planning/STATE.md:37 'Recurs if stack idles >24h'"
    missing:
      - "Decide and implement a deterministic policy: when committed < log start AND start == end (nothing retained), advance safely and record the gap; otherwise fail with an operator tool that performs an audited, fence-consistent reset. Add a native test for the idle-retention case. Track as a Phase 3 follow-up or gap plan"
deferred:
  - truth: "Overview shows consumer lag (UI-01)"
    addressed_in: "Phase 5"
    evidence: "ROADMAP Phase 4 note: 'The Overview consumer-lag panel (part of UI-01) is wired to real lag data when OBS-02 lands in Phase 5.' Overview.tsx:183 shows an honest 'available in Phase 5' placeholder, not invented data."
---

# Phase 4: Query API & Analyst Dashboard Verification Report

**Phase Goal:** Analysts can sign in, investigate events and alerts within their own tenant, triage alerts, and see new alerts appear live in the browser.
**Mode:** mvp. The user-story outcome is "an analyst triages their tenant's alerts and sees new ones live".
**Verified:** 2026-11-14T23:08Z, HEAD b764e24
**Status:** gaps_found
**Re-verification:** No. This is the initial verification.

## User Flow Coverage (MVP)

| Step | Expected | Evidence in codebase | Status |
|---|---|---|---|
| Sign in | Dev JWT, header bearer, verified `/session` | `backend/internal/auth/jwt.go:54-116` (HS256 only, fixed iss/aud, 8 KiB cap, single `Authorization` header); `cmd/query-api/main.go:135` `/api/v1/session`; `cmd/dev-jwt/main.go:31` refuses non-dev mode; `web/src/auth/session.tsx` keeps the token in memory | VERIFIED |
| Investigate events | Filter + cursor + safe JSON | `internal/query/events.go:130-243`, parameterized CH `storage/clickhouse/query_events.go:40-77`; `web/src/components/EventDetails.tsx:88-95` renders `JSON.stringify` in `<pre><code>`; no `dangerouslySetInnerHTML` anywhere in `web/src` | VERIFIED |
| Investigate alerts | Filters, full pages, details | `internal/query/alerts.go:109-270`, cursor codec `cursor.go`, `GetAlert` alerts.go:117-151 (PG ownership before CH, evidence, explanation, history) | VERIFIED |
| Triage | ack/resolve, CAS, audit in the same transaction | `storage/postgres/triage.go:15-69`: one tx does UPDATE ... `version=$3 AND status=$6`, then INSERT `alert_status_audit`, then COMMIT; handler `query/triage.go:29-72` 403 for viewers, 409 for conflicts, 404 for foreign IDs, and a security audit log line | VERIFIED |
| See new alerts live | SSE, header auth, Last-Event-ID, backfill | `alertstream/handler.go:94,131-231` (header bearer, Last-Event-ID, any URL query rejected, admission cap with 429, 15 s heartbeat, write deadlines); `delivery.go:237` backfill; `web/src/api/live-alerts.ts` (seq-string cursor, queue cap of 32, jittered reconnect) | VERIFIED in code and local native/browser runs. Hosted proof RED (G1) |

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|---|---|---|
| 1 | SC1: dev JWT sign-in, viewer mutation gets 403, full cross-tenant matrix | VERIFIED (local) | `RequireTriage` + 403 at triage.go:34-37; tenant only from `Principal` in every handler and SQL (`tenant_id=$1`). TestPhase4Auth and TestPhase4TenantMatrix PASS locally (04-15/04-16 evidence). Hosted: auth was still running; tenant FAILED on the harness env defect (G1), not on behavior |
| 2 | SC2: filter + cursor-paginate events/alerts with full pages under status filters, details, rules, summary | VERIFIED (local) | Handlers above; summary.go:91-131 is computed from CH + PG; rules.go is a startup snapshot. Hosted: alerts and events PASS; summary FAILED (G1) |
| 3 | SC3: ack/resolve, stale version returns 409, audit row in the same tx | VERIFIED (local) | triage.go storage:15-69. Hosted triage FAILED (G1, env) |
| 4 | SC4: live SSE without URL token, reconnect + seq backfill with no gaps, every replica delivers, real proxy | VERIFIED (local) | Listener plus 1 s poll, durable delivery log (migration 008), per-replica replay. Local Stream/Delivery/browser PASS (04-14/15/16). Hosted stream and browser FAILED (G1); delivery pending |
| 5 | SC5: 4 pages with loading/empty/error/reconnecting states, typed OpenAPI client, safe JSON, keyboard/labels, viewer controls hidden | VERIFIED | `web/src/api/client.ts:1-2` openapi-fetch + generated `schema.d.ts` (`gen:api` pre-hooks); labelled controls in Alerts.tsx:357-427 and Events.tsx:325-392; states in Alerts.tsx:303-317,468-482, Events.tsx:428-448, Overview.tsx:31-33, Rules.tsx:19-21; triage controls only for analyst/admin (AlertDetails.tsx:227-230); debounced search (Alerts.tsx:259, Events.tsx:255). 170/170 Vitest, tsc clean. Minor UX defect G4 |
| 6 | DoD: CI green / 04-16 fail-closed acceptance complete | FAILED | Hosted run 43649750865: 5 Phase 4 jobs and phase3 (scenarios) failed (G1, G2) |

**Score:** 5/6

### Required Artifacts

| Artifact | Status | Details |
|---|---|---|
| `backend/internal/auth/{jwt,audit}.go` | VERIFIED | Wired into all dashboard routes and the ingest producer-auth audit (`ingest/handler.go:107-136`) |
| `backend/internal/query/*` | VERIFIED | Registered in `cmd/query-api/main.go:137-141` |
| `backend/internal/storage/postgres/triage.go` | VERIFIED | Real tx and audit |
| `backend/internal/alertstream/*` | VERIFIED | main.go:106,132-133,153-222 |
| `backend/migrations/postgres` 007 audit, 008 delivery | VERIFIED | Used by triage/delivery |
| `api/openapi.yaml` | VERIFIED with a stale line | G5 |
| `web/src/{pages,components,api,auth}` | VERIFIED | Data is fetched through the typed client; no hardcoded empty data |
| `deployments/compose/docker-compose.yml` query-api rules mount | VERIFIED | Lines 299, 303 (commit e2b59e7). The hosted `make dev-up` + `check-stack.sh` passed with query-api Healthy (phase4 summary job log), so known issue 4 is confirmed fixed |
| `scripts/check-phase4-tests.sh`, `.github/workflows/ci.yml` phase4 jobs | EXISTS, NOT PASSING in hosted CI | G1 |

### Key Link Verification

| From | To | Via | Status |
|---|---|---|---|
| Alerts page | `/api/v1/alerts`, detail, PATCH | openapi-fetch client + React Query | WIRED |
| Live client | `/alerts/backfill` + `/alerts/stream` | fetch-event-source with header bearer (`client.stream`) | WIRED |
| PATCH handler | PG workflow + audit | `Triage.Transition` -> AlertStore | WIRED |
| PG workflow insert | delivery log | migration 008 trigger + projector (main.go:202) | WIRED |
| Overview | `/metrics/summary` | client + React Query | WIRED |
| Rules page | `/rules` | client | WIRED |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|---|---|---|---|---|
| Overview.tsx | summary | summary.go -> CH SummarizeEvents + PG SummarizeAlerts | yes | FLOWING (lag deferred to Phase 5) |
| Alerts.tsx | alert pages | PG list with a cursor | yes | FLOWING |
| Events.tsx | events | CH unique-event query | yes | FLOWING |
| Rules.tsx | rules | startup rules.yaml snapshot | yes | FLOWING |

### Behavioral Spot-Checks (run by verifier)

| Behavior | Command | Result | Status |
|---|---|---|---|
| Go vet whole module + phase4 tag | `go vet ./...`; `go vet -tags=phase4 ./testphase4/` | exit 0 / exit 0 | PASS |
| Phase 4 Go unit tests (race) | `go test -race ./internal/{query,auth,alertstream,eventquery,storage/...}` | all ok | PASS |
| Web unit tests | `npx vitest run src` | 9 files, 170 tests passed | PASS |
| Web typecheck | `npx tsc --noEmit` | exit 0 | PASS |
| Phase 4 guards | `python3 -B -m unittest ... test_phase4_guards.py` / `test_dev_auth_env.py` | 15 OK / 8 OK | PASS |
| Native families / browser locally | not run: the NEW stack needs the private launcher (per constraints) | relying on 04-16 SUMMARY gates (8/8 families, 13/13 Playwright at f76120c) | SKIP |

### Hosted CI: run 43649750865, chocopie247/Sentinel, head b764e24 (status at 23:07Z: in_progress)

| Job | Result |
|---|---|
| web, python, secrets, schema, hygiene, go, compose-smoke | success |
| phase4 (alerts), phase4 (events) | success |
| phase4 (triage) | failure: triage_test.go:45 Primary.Env required |
| phase4 (stream) | failure: stream_test.go:94 Primary.Env required |
| phase4 (tenant) | failure: tenant_matrix_test.go:99 Primary.Env required |
| phase4 (summary) | failure: summary_rules_test.go:65 child query-api not live within 15 s |
| phase4-browser | failure: seeder keyctl CalledProcessError (no PG* env) |
| phase3 (scenarios) | failure: verifier failed at current_event_rows |
| integration, phase3 (core), phase4 (auth), phase4 (delivery) | still running at report time (pending) |

Known issue 7: `make test-integ` (integration job) and `check-phase3-tests.sh` (phase3 jobs) were not run locally. Hosted, the scenarios group has already FAILED and core/integration are pending, so hosted CI does not yet cover them.

### Requirements Coverage

| Req | Description (short) | Status | Evidence |
|---|---|---|---|
| AUTH-01 | Dev-safe JWT sign-in, no committed creds | SATISFIED | jwt.go:54-116; dev-jwt refuses non-dev mode; `.env.example:15-26` holds no key; secrets job green |
| AUTH-02 | Server-side roles, viewer mutation returns 403 | SATISFIED | triage.go:34-37 `RequireTriage`; summary/rules allow every verified role |
| AUTH-03 | Tenant from verified identity, cross-tenant matrix | SATISFIED (local); hosted proof RED | `p.TenantID` in every handler; every PG/CH query has a tenant predicate; TenantMatrix PASS locally; hosted fail is env (G1) |
| AUTH-04 | Producer auth failures and role actions audit-logged without secrets | SATISFIED | auth/audit.go allowlist; ingest/handler.go:107-136; triage.go audit calls |
| API-01 | Event list filters + `(occurred_at,event_id)` cursor | SATISFIED | events.go, query_events.go:40-77 |
| API-02 | Single event by ID in tenant | SATISFIED | eventquery/handler.go:68-91 (bearer, no key fallback when Authorization is present) |
| API-03 | Alert list filters, full pages under status filters | SATISFIED | alerts.go:161-270; Alerts family hosted PASS |
| API-04 | Alert details with evidence, explanation, history | SATISFIED | alerts.go:117-151 |
| API-05 | PATCH with CAS, 409 when stale, audit in the same tx | SATISFIED (local); hosted proof RED | storage/postgres/triage.go:15-69; OpenAPI text stale (G5) |
| API-06 | GET /rules | SATISFIED (local); hosted proof RED | rules.go; accepts arbitrary query (G3) |
| API-07 | GET /metrics/summary from real data | SATISFIED (local); hosted proof RED | summary.go:91-131; accepts arbitrary query (G3) |
| API-08 | Parameterized SQL, limits/caps, invalid-input tests | SATISFIED with a minor gap | No string-built values; limit 1..200, range caps, cursor ≤2 KiB, search ≤128 B; G3 inconsistency |
| LIVE-01 | SSE without URL tokens | SATISFIED (local); hosted proof RED | handler.go:131-134 rejects any RawQuery; header-only middleware |
| LIVE-02 | Reconnect with Last-Event-ID + backfill, no gaps | SATISFIED (local); hosted proof RED | handler.go:136-147, delivery.go backfill, live-alerts.ts cursor advances only after processing |
| LIVE-03 | Keep-alives, bounded buffers, slow-client drop, every replica delivers | SATISFIED (local); hosted proof RED | heartbeat 15 s, admission cap/429, write deadlines, client queue 32; per-replica replay from the shared log |
| UI-01 | Overview | SATISFIED (consumer lag deferred to Phase 5) | Overview.tsx; deferred item |
| UI-02 | Alerts page | SATISFIED with a minor defect | Alerts.tsx, AlertDetails.tsx; G4 banner |
| UI-03 | Events page with safe JSON | SATISFIED | Events.tsx, EventDetails.tsx:88-95 |
| UI-04 | Rules page | SATISFIED | Rules.tsx:19-48 |
| UI-06 | Typed client, states, debounce, bounded pages, a11y | SATISFIED | client.ts, `gen:api`, labels, debounce, limit cap |

No orphaned requirements: all 20 IDs mapped to Phase 4 appear in plan frontmatter (04-16 lists all 20).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|
| backend/testphase4/*_test.go (5 files) | see G1 | Test harness depends on private launcher env | Blocker | Hosted CI red |
| api/openapi.yaml | 343 | Stale contract text | Minor | Misleads clients/generated docs |
| web/src/pages/Overview.tsx | 183 | "available in Phase 5" placeholder | Info | Documented deferral, no fake data |

No TBD/FIXME/XXX markers in the Phase 4 production code that was scanned.

### Known-issue classification

| # | Issue | Verdict |
|---|---|---|
| 1 | summary/rules ignore query strings | Minor real gap (G3). Not mandated by 04-CONTRACT, but inconsistent with the summary OpenAPI 400 and every other endpoint |
| 2 | "New alerts available" on first load | Confirmed, minor (G4). Initial backfill from 0 sets arrivals |
| 3 | Stale OpenAPI resolve text | Confirmed, minor (G5), openapi.yaml:343 |
| 4 | Base compose missing rules file for query-api | Confirmed FIXED: compose lines 299/303 (e2b59e7); hosted `make dev-up` + `check-stack.sh` PASS with query-api Healthy |
| 5 | STACK-RECOVERY-PLAN says gates pending | Superseded by 04-07-STACK-RECOVERY.md (PASS, accepted 2026-10-24). Doc-only, minor (G6) |
| 6 | Hosted CI 04-16 Task 3 | FAILED: 5 Phase 4 jobs + phase3 scenarios red, 4 jobs pending (G1, G2) |
| 7 | test-integ / phase3 runner not run locally | Hosted does not cover them yet: scenarios FAILED, core/integration pending (G2) |
| new | Detector dies after >24h idle | Major cross-phase gap (G7) |

### Human Verification Required

None beyond the gap closures. After G1/G2 are fixed, the hosted run must be re-checked (all phase4 matrix jobs, phase4-browser, integration, phase3 core and scenarios).

### Gaps Summary

The product code for Phase 4 is real and wired end to end. Auth, tenant scoping, filtered cursor lists, details, atomic triage with audit, rules, summary, durable delivery, SSE, and the typed React dashboard are all present, and local unit, web and guard checks pass. Local native and browser acceptance is recorded as PASS in 04-14..04-16. The phase still is not done under spec §18, because the hosted CI that 04-16 adds is red. Five Phase 4 jobs fail for one root cause (G1): the native families and the browser seeder silently depend on environment variables exported only by the private local launcher. Locally they passed; on a clean runner they cannot load config. Fixing G1 is mostly mechanical (set PRIMARY.ENV/ENV_FILE in each family, load config before spawning the child, export PG* in the browser script). Then re-run hosted CI together with the separate phase3 scenarios failure (G2). The remaining items (G3-G6) are minor contract/UX/doc fixes. G7 is a cross-phase detector robustness issue that should get its own follow-up plan.

---

_Verified: 2026-11-14T23:08Z_
_Verifier: Claude (gsd-verifier)_
