---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-23T05:42:55Z
depth: standard
review_slice: 04-07
review_sha: 3738d7a95ebf5ea581a6f52b77a14dfe11120b7e
source_sha: 242b162abdb0f16708743bfbde016439954807fa
diff_base: ea2529e104170923806a5be826f36f03a5c11039
files_reviewed: 13
files_reviewed_list:
  - backend/internal/query/events.go
  - backend/internal/eventquery/handler.go
  - backend/testphase4/events_test.go
  - backend/test/openapi_runtime_auth_test.go
  - web/src/components/EventDetails.tsx
  - web/src/pages/Events.tsx
  - web/src/pages/Events.test.tsx
  - api/openapi.yaml
  - web/src/api/schema.d.ts
  - backend/cmd/query-api/main.go
  - web/src/api/client.ts
  - backend/test/openapi_test.go
  - .gitleaksignore
generated_schema_sha256: 7c5d0c380ad4cdf3b17d2fdc25fa0a435d89d89fbc1855c005eb433514e46194
findings:
  critical: 1
  warning: 0
  info: 0
  total: 1
status: issues_found
---

# Phase 4 Plan 07: Code Review Report

**Reviewed:** 2026-10-23T05:42:55Z

**Depth:** standard; explicit thirteen-path scope.

**Status:** issues_found — one BLOCKER; zero WARNING findings.

## Narrative Findings (AI reviewer)

### Summary and scope

One reproducible detail-link regression remains: a successful session reached after an earlier failed login cannot restore its selected event after a hard reload and same-identity sign-in. Passing existing tests do not cover this history-owner transition.

Read all thirteen declared paths fully, the cumulative source diff from accepted06base ea2529e, 04-07 PLAN/A1/CHECK/preserved R0, SCAN-A2/CHECK, SUMMARY and shared CONTRACT/UI-SPEC. Traced relevant actual JWT middleware, API-key verifier/store, native event Get/list adapter, error middleware, App/session lifecycle and retained dialog/test consumers. No root AGENTS.md, .codexignore or project .codex/skills/.agents/skills directory was present. Used the GSD code-review workflow and surgical evidence guidance; no structural/fallow pre-pass was supplied.

The generated declaration is ignored, but explicitly included by the assigned scope as a read-only contract artifact; its SHA-256 matches the declared hash above. The unchanged query/events.go was also explicitly reviewed. The cumulative tracked delta contains ten feature paths plus the root-owned scanner metadata path; no additional feature source path was found.

### Critical Issues

#### CR-01: [BLOCKER] Persisted event detail links depend on a session generation that resets on reload

**File:** `/home/logan78/Desktop/Sentinel/web/src/pages/Events.tsx:8-13`

**Related locations:** `Events.tsx:153`, `Events.tsx:164`, `Events.tsx:183-184`; actual dependency `web/src/auth/session.tsx:87,94-96,129-168`.

**Issue:** readSelected rejects a persisted history entry unless sentinelEventsOwner exactly equals the current owner, which embeds the in-memory session generation. Browser history state survives a hard reload, but SessionProvider's generation counter starts again. A failed login, retry or earlier sign-out advances the old counter, so the same subsequently verified subject/tenant/role receives a different generation after reload. readSelected returns an empty selection; the mount effect then rewrites the URL without event_id. The selected event silently disappears despite a valid link and unchanged verified identity. This violates the required refresh/deep-link behavior and is distinct from deliberate cross-identity cleanup.

**Reproduction:** In the actual App under StrictMode, submit a credential rejected with401, then sign in successfully. Open an event detail and reload the page. Sign in with the same successful identity. The dialog does not reopen and event_id is removed from the URL. A first-attempt successful login followed by reload does restore the detail, which explains why the existing happy-path check misses it.

A reviewer-run in-memory component harness loaded the actual App, Events, EventDetails, session and client source without editing files. It executed the failed-login/success/open sequence, retained the resulting URL/history state across a fresh App mount to emulate reload, and signed in with the same identity:

| Observation | Before fresh mount | After same-identity sign-in |
|---|---|---|
| Persisted/current generation in owner | 4 | 2 |
| event_id in URL | present | removed |
| Event dialog | open | absent |
| Detail requests after fresh mount | — | 0 |

The harness used the same StrictMode wrapper as main.tsx. This is an independently executed component reproduction, not a new native-browser run.

**Fix:** Separate persistent verified identity from the page-local session generation used for request/cache isolation. For example, persist a structured subject/tenant/role identity and a page-instance marker alongside the generation. On initial bootstrap in a new page instance, restore a syntactically valid event_id when the verified identity matches; within the same page, continue rejecting stale-generation entries and retain explicit sign-out/identity-change URL and cache cleanup. Do not simply remove the ownership check. Add a regression exercising failed login or retry, successful sign-in, opening a detail, preserving history state across a fresh StrictMode mount, and signing in as the same identity; also retain different-identity and same-page stale-session rejection.

### Other boundary analysis

No additional actionable defect was established in the bounded scope. Main registers one production detail route with NewAuthenticatedHandler and supplies the existing JWT verifier. Presence of Authorization selects strict JWT middleware; empty, repeated, malformed and invalid values do not reach the API-key branch. Genuine absence uses the retained read-key verifier/scope check. Verified tenant reaches parameterized native Get with a caller-derived five-second context and retained404/400/503 mapping. Other implemented dashboard routes retain bearer middleware; the legacy constructor is confined to retained direct consumers.

The new typed detail method uses the existing guarded private transport, pre-credential session checks, combined caller/session cancellation, post-parse checks and current401 revocation. Event JSON is React text inside pre/code, with no HTML insertion. Native-dialog focus/Escape behavior is delegated to showModal; no defect is inferred from jsdom's lack of native focus trapping or browser-chrome focus. Query keys retain verified session generation. Existing search/IPv6/cursor/UTC/debounce/paging behavior was traced through the unchanged native adapter and reviewed page code.

The OpenAPI event operation has exactly separate ApiKeyAuth and BearerAuth alternatives and describes strict presence-based dispatch. The runtime fixture uses the production dispatcher and a random-key, correctly claimed, independently verified signed JWT. Owned tests retain the authority/status matrix. Future triage/SSE/rules/overview and Phase5 work are outside this increment.

### Scanner disposition

Verified the exact .gitleaksignore delta against independently approved SCAN-A2/CHECK: only the short explanatory comment and the full commit/path/rule/line fingerprint for historical04-07PLAN line63 were appended; all existing exceptions remain unchanged. .gitleaks.toml is unchanged in the reviewed range. No broad rule/path/regex exclusion or scanner/history rewrite appears in the submitted delta.

Read only nonsensitive metadata from the private original report: one generic-api-key finding at commit5c0454cab0d0eca0ba6541e8a98082275a971571, the declared planning file, line63, and the exact approved fingerprint. A2/CHECK independently classifies it as test-policy prose. The original log retains553commits/EXIT1 at source242b162; the subsequent disposition log records557commits/EXIT0 at d2a3e43f9b5606387446b2d0bba2caa4d89b44fe and its report has zero entries. No Secret/Match/authentication value or raw report was printed. These are inspected execution records, not a fresh reviewer-run full-history scan at frozen3738d7a.

### Independent diagnostics and limits

| Reviewer-run check | Result |
|---|---|
| Direct `./node_modules/.bin/vitest run` from web | Exit0; all106tests across6files;3.07s. No npm pretest or generated-schema rewrite. |
| `go test -race ./internal/auth ./internal/eventquery ./internal/query -timeout=60s -count=1` from backend | Exit0; all three packages execute and pass. |
| Focused Go race run selecting TestOpenAPI/TestEvent families in eventquery/query/test | Exit0; relevant packages pass. Its auth selection matched no tests, so the separate unfiltered auth run above was required and completed. |
| Actual-source App/session/client StrictMode history reproduction described in CR-01 | Exit0; independently exposes the generation4→2 loss of event_id and dialog. |
| Source-range `git diff --check` | Exit0. |
| Generated schema SHA-256 | Exact declared hash. |

The root's reported native current-binary, browser, default/static and scanner results were read as context, not substituted for source analysis or independently recertified. No native write fixture, migration, server, new browser session, dependency installation or remote operation was started for this review. Standard review does not establish whole-phase acceptance, accessibility certification or performance guarantees.

### Preservation

Only this designated report was written; no source/test/generated declaration, Git index, commit, root tracking, user configuration or .serena content was changed. The previous06R1 report remains separately preserved as04-REVIEW-06-R1.md. Return CR-01 for repair and independent re-review before accepting07.

---

_Reviewer: gsd-code-reviewer_

_Depth: standard_
