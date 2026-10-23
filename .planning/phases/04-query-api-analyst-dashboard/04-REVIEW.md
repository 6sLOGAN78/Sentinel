---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-23T12:47:14Z
depth: standard
review_slice: 04-07
review_round: R1
review_sha: 1dd2997dcbdcba4a31a13de7bfdc3600f0be3016
source_sha: ec14c34ffaf62eebdfa49059287bc3a46b1a3a17
original_source_sha: 242b162abdb0f16708743bfbde016439954807fa
diff_base: ea2529e104170923806a5be826f36f03a5c11039
repair_base: 3738d7a95ebf5ea581a6f52b77a14dfe11120b7e
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
resolved_findings: [CR-01]
findings:
  critical: 1
  warning: 0
  info: 0
  total: 1
status: issues_found
---

# Phase 4 Plan 07: Code Review Report — R1

**Reviewed:** 2026-10-23T12:47:14Z

**Depth:** standard; cumulative thirteen-path scope, with complete re-read of both repair files.

**Status:** issues_found — CR-01 resolved; one new BLOCKER, CR-02; zero warnings.

## Narrative Findings (AI reviewer)

### Summary and scope

The reload repair closes the original generation-reset defect, but its unconditional popstate URL rewrite introduces an interpage navigation regression. Returning from Events to a filtered Alerts history entry now removes the alert filters and issues an unfiltered request.

Read the preserved full R0 report, complete07REPAIR and updated SUMMARY, both current Events source/test files in full, actual App/session dependencies, relevant Alerts navigation code, and the actual repair diff. The original full thirteen-path review and CONTRACT/UI-SPEC context remain applicable to byte-identical files. No project AGENTS.md or project .codex/skills/.agents/skills directory was found. Continued the GSD code-review workflow; no structural/fallow pre-pass was supplied.

Verified that only Events.tsx and Events.test.tsx differ in backend/web/api/ignore scope between frozen R0 3738d7a and frozen R1 1dd2997. The ten other tracked declared paths, including .gitleaksignore, are byte-identical across that range. The eleventh unchanged path is the explicitly declared ignored generated schema; its hash still matches the value above. App/session and .gitleaks.toml are unchanged.

The original feature SHA242b162 predates the separately approved A2 ignore amendment. Consequently .gitleaksignore differs from242b162 by that already-reviewed exact fingerprint/comment, while remaining unchanged from frozen R0. This is not an undeclared repair edit. No additional feature source path was changed.

### R0 finding disposition

**CR-01 — resolved.** At Events.tsx:14-43 and185-225, a structured owner separates a JSON tuple of verified subject/tenant/role from the per-document UUID and transient session generation. A first Events session in a new document can adopt prior-document history for the same verified identity. Same-document entries still require the current generation, and the latched first Events generation prevents later sessions in that document from accepting prior-document entries. Ownership uses no ambiguous colon-string parsing. The effect initializes the latch after commit, the memoized owner keeps effect lifetime stable, and StrictMode replay retains the selected reference.

The new tests at Events.test.tsx:643-802 exercise the actual StrictMode App/session/client/component path, preserve the real URL/history before cleanup, reset modules to simulate a fresh document, and verify recovery after failed login followed by successful same-identity login. They also cover changed subject/tenant/role, same-page stale-generation entries and prior-document history after the restored session signs out. The independent direct suite passed all112cases, including these six new cases. Existing request/cache generations, session cancellation, current401 handling, escaped JSON and server tenant authority are unchanged.

### Critical Issues

#### CR-02: [BLOCKER] Events rewrites Alerts history entries while Back navigation leaves the Events page

**File:** `/home/logan78/Desktop/Sentinel/web/src/pages/Events.tsx:236`

**Affected block:** Events.tsx:226-236; actual caller/navigation dependency App.tsx:66-72,75-80,140-146; destination reader Alerts.tsx:40-115.

**Issue:** The Events popstate listener now always calls writeLocation after reading the current URL. It remains subscribed when the same popstate event changes App's route to Alerts. The destination Alerts entry has no sentinelEventsOwner, so ownsLocation accepts it. readFilters then interprets that entry as Events filters, and writeLocation removes alert-only severity/status/search/rule_id/alert_id parameters and replaces its ownership metadata before Alerts mounts. The user's saved alert investigation silently becomes an unfiltered or differently filtered request. The former conditional rewrite did not mutate an owned/unmarked destination this way.

**Reproduction:** Sign in at `/alerts?severity=critical&status=open&search=needle&limit=5`, navigate to Events using the main navigation, then press browser Back. The Alerts page returns with severity/status/search removed and requests alerts without those constraints. An alert_id in the destination URL is likewise omitted by the event serializer; cursor-bound investigations can also become inconsistent with their original filters.

The reviewer independently ran an in-memory harness loading actual App, Events, Alerts, session and client source under StrictMode. It used the real jsdom history.back() operation and guarded transport with response fixtures. The same harness was run against R0 Events and current R1 Events:

| Actual-source variant | Filters before navigation | Filters after Back | Subsequent alert request |
|---|---|---|---|
| R0 3738d7a | severity=critical, status=open, search=needle | all retained | all three constraints retained |
| R1 1dd2997 | same three constraints | all three absent | all three constraints absent |

This differential reproduction exited0 and establishes the new incorrect behavior independently of the passing112-test suite. It is component/history evidence, not a newly executed native-browser run. Existing tests do not exercise returning from Events to an Alerts history entry carrying alert-specific parameters.

**Fix:** Restrict the Events popstate handler to its own route before reading or rewriting state, for example return immediately when window.location.pathname is not "/events". Retain current adoption/rejection canonicalization for Events entries so CR-01 and same-page privacy guards continue to hold. Add a regression that starts with filtered Alerts, navigates to Events, uses Back and Forward, and asserts the Alerts URL, controls and API request retain their filters and selected alert where applicable. The causal fix belongs in the already-owned Events files; no Alerts/App change is necessary.

### Other boundary analysis

No additional actionable defect was established. The repair does not weaken the production dual-credential route, strict Authorization-presence dispatch, server tenant selection, native parameterization/error mapping, generated contract or guarded detail transport. Their R0 analysis remains applicable to the verified unchanged source.

Reviewed marker/type validation, initial restoration, accepted/rejected Events history adoption, cleanup, generation isolation and StrictMode effect replay. History metadata controls UI restoration only; it is not server authentication. Unmarked explicit URLs remain usable as deep links, while marked different-identity/stale-session entries are rejected. The per-document latch deliberately tracks the first committed Events session; the bounded review does not claim an application-wide authentication journal for sessions that never mounted Events.

The separately reported detector restart condition and preserved-stack readiness are outside this UI repair and were not investigated or converted into an07 source finding. Future triage/SSE/rules/overview, Phase5, whole-pipeline acceptance and performance certification remain outside this review.

### Independent diagnostics and limits

| Reviewer-run check | Result |
|---|---|
| Direct `./node_modules/.bin/vitest run` from web | Exit0;112tests across6files;3.74s. No npm pretest or generated-schema rewrite. |
| Actual-source StrictMode App/history differential reproduction described in CR-02 | Exit0; R0 preserves all three alert filters after real history.back(); R1 removes them and sends unfiltered requests. No file was created. |
| Explicit unchanged-path diff from frozen R0 to frozen R1 | Exit0; ten tracked declared paths unchanged, plus unchanged App/session/.gitleaks.toml dependencies. |
| Ignored generated schema SHA-256 | Exact unchanged declared hash. |
| Repair source `git diff --check` | Exit0. |

Read the root's causal RED/GREEN and later browser/native/default/static/scanner evidence as execution context. These records are not substituted for source analysis and were not independently recertified in this R1 run. The prior infrastructure setup failures remain setup failures; no detector/full-stack pass is inferred. No service, browser session, migration, native write fixture, scanner run, dependency installation or remote operation was started for this review.

### Preservation

Only this designated report was written. The original R0 remains preserved as04-REVIEW-07-R0.md. No source/test/generated declaration, Git index, commit, root tracking, user configuration or .serena content was changed. Return CR-02 for causal repair and independent re-review before accepting07.

---

_Reviewer: gsd-code-reviewer_

_Depth: standard_
