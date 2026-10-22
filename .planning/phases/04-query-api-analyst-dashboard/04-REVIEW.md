---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-22T16:47:30Z
depth: standard
review_slice: 04-03
review_sha: 4ed9a93c496ff843c6b17140074d1799b4e8ffeb
source_sha: 6bb0ac84ef7d0267cc5a4f54ec8d8b7e0a6e9bc4
diff_base: 21ebf9840c57edd3125ba0b8be3d7508c525d5e5
files_reviewed: 9
files_reviewed_list:
  - web/package.json
  - web/package-lock.json
  - web/src/api/client.ts
  - web/src/auth/session.tsx
  - web/src/auth/session.test.tsx
  - web/src/pages/Alerts.tsx
  - web/src/App.tsx
  - web/src/App.test.tsx
  - web/src/styles.css
findings:
  critical: 1
  warning: 0
  info: 0
  total: 1
status: issues_found
---

# Phase 4 Plan 03: Code Review Report

**Reviewed:** 2026-10-22T16:47:30Z

**Depth:** standard; full nine-file review, dependency-lock comparison and contextual tracing of the entrypoint, verified session endpoint, PG list contract and installed client/cache implementations.

**Status:** issues_found — one BLOCKER credential-clearing regression.

## Narrative Findings (AI reviewer)

### Summary

The provider and actual Alerts query use one QueryClient, verified identity, session generations and cancellation guards. A separate token draft in the shell escapes that lifecycle: editing the token field during verification leaves credential text available after subsequent sign-out. This is a demonstrated regression from the accepted base, whose signOut explicitly cleared that state. The slice requires repair before acceptance.

The scope is exactly the nine paths listed above, matching the diff from the reliable base to source 6bb0ac8. Their bytes remain unchanged at frozen HEAD 4ed9a93. The lockfile was included explicitly despite the generic review workflow's lockfile exclusion. No structural/fallow pre-pass was supplied or run.

No AGENTS.md, .codex/skills/, .agents/skills/ or .codexignore was found in the working directory. Repository ignore rules were respected; generated schema declarations were consulted only as an API boundary, not counted as submitted source. The GSD review instructions, React guidance on component identity/effect dependencies, UI accessibility guidance and approved manual-CSS UI-SPEC informed the review. No redesign or implementation was performed.

### Critical Issues

#### CR-01: BLOCKER — Shell retains a token draft after session revocation

**File:** /home/logan78/Desktop/Sentinel/web/src/App.tsx:23

**Related lines:** App.tsx:10, 68–77, 89; /home/logan78/Desktop/Sentinel/web/src/auth/session.tsx:104–124.

**Issue:** suppliedToken belongs to the long-lived AnalystShell and the input remains editable while verification is pending. The submit handler clears its previous value, but a user can immediately paste/type another token while awaiting /session. Successful verification hides the input without destroying AnalystShell or clearing this new value. The header's Sign out calls only the provider's signOut; that provider cannot reset the shell's local state. The sign-in screen consequently reappears with the token draft still in its password field. For example: submit A, enter B while verification is pending, let A verify, then sign out; the field contains B. B may also be a second paste of A.

The same disconnected state has no clearing path when the provider's expiry/401 handlers revoke a session or retry attempt. The Cancel sign in handler clears it explicitly, but that does not cover header sign-out and automatic revocation. The accepted base's App.signOut included setSuppliedToken(""); extraction into the provider removed that coverage.

This violates the current credential-clearing contract and leaves a credential accessible in the DOM after sign-out. Its scope is retained input text: the diagnostic confirmed authenticated authority was absent, and this finding does not claim backend authentication bypass or restored tenant query data.

**Evidence:** A read-only in-memory transpilation of the actual App/session/client/Alerts modules used installed React, Testing Library, JSDOM, React Query and openapi-fetch. Only fetch was replaced with synthetic session/list responses; no component/provider/cache was replaced. At 2026-10-22T16:46:06.606Z–16:46:06.764Z, the sequence above produced two requests and:

```json
{"draftRetainedAfterSignOut":true,"authenticatedAuthorityPresent":false}
```

The bounded diagnostic exited 0 because it asserted the regression was reproduced; this is not a passing product acceptance test. No real token/key/header or capture file was read or printed. Two preliminary attempts failed in the diagnostic's CJS/ESM setup and are not product failures. An earlier successful observation was followed by intentional termination/join of that diagnostic's Node process because its library GC timer kept it alive; the final bounded run exited normally.

**Fix:** Connect the token draft to the same lifecycle reset as the provider, so every sign-out, expiry, 401, verification restart and successful bootstrap clears the controlled input state. A reset/generation signal for a dedicated sign-in component is one option; keep raw credentials out of public context. Clear the draft before Retry sign in as well. Disabling edits during pending verification can prevent this particular sequence, but by itself does not clear a draft typed during retained-retry state when that attempt expires. Do not limit the repair to the header click handler.

Add component regressions using the actual App tree: edit the field during pending verification, verify, sign out and assert its value is empty; edit it during retained-retry state, expire/revoke the attempt and assert it is empty. Retain current cache/authority assertions to prove the repair preserves cancellation and tenant isolation.

### Other boundaries assessed

- App.tsx:120–127 supplies one lazily created per-mount QueryClient to the provider and Alerts tree. The optional injected instance is that same client; existing actual-response cache assertions are not against a separate artificial cache.
- session.tsx:92–102 revokes the generation before emptying the active credential and aborting its lifetime. It initiates cancellation without awaiting uncooperative network work, then clears both query and mutation caches. Installed query-core confirms clear is synchronous and cancelQueries handles its returned rejections.
- client.ts:26–68 checks session lifetime before credential access, after fetch and after parsing. Alerts additionally checks the individual query signal before its request and after parsing; its signal is combined with the session lifetime. An old client fails its generation guard before obtaining a replacement credential. Status bodies are not exposed through APIError or UI text. Requests omit cookies, reject redirects and request no-store.
- session.tsx:126–174 publishes authority only after the server response passes its identity guard and has future expiry. Unknown roles, missing identity properties, blank subjects and invalid/past expiry do not establish current authority. Retry credentials have a fixed finite deadline; decoded unverified expiry can only shorten retention. Authenticated expiry comes from /session. Current authenticated 401 revokes authority; availability failures retain explicit retry/cancel. These provider protections do not clear the separate shell draft described in CR-01.
- Alerts.tsx:20–37 uses verified subject, tenant, role, session generation and the current normalized empty filter set in its actual query key. The generated AlertPage/AlertFilters contract is used directly. The current server emits the bounded 15-field PostgreSQL workflow DTO; this increment fabricates no canonical detection/evidence fields.
- Alerts.tsx:41–135 retains populated data on a read error, uses fixed permission/unavailability copy and offers explicit retry. React text escapes summaries and identifiers. Native table and labelled mobile list use the same result. CSS provides semantic colors, focus indicators, a skip link, 44px controls, responsive wrapping and reduced-motion handling. Other pages, filters, pagination, dialogs and SSE are later owned increments.
- package/lock inspection found the exact new pins @tanstack/react-query 5.104.5 and openapi-fetch 0.17.0, with query-core 5.104.5 and openapi-typescript-helpers 0.1.0. Parsing all 278 package records against the base found no existing version changes; other semantic changes are root dependency additions and the two optional-peer dev metadata adjustments. Installed React Query's React/types peers accept the existing React 19 stack. This is compatibility review of these pinned packages, not a fresh dependency-security certification.

### Retained evidence and limits

Selected private command metadata/results and the browser measurement report were inspected. They support what their commands exercise; they do not override the source finding above.

| Evidence | Observation |
|---|---|
| app-cache-red.log / query-abort-red.log | Genuine missing-cache assertions and canceled-query-body resolution failure are recorded separately. The absent provider import is setup-only; already-correct expiry/abort behavior is not invented RED. |
| web-mobile-final.log | 28 tests, typecheck, lint, build and format checks exited 0 at 16:30:49Z. Component fetch responses are mocked, while the actual provider and query cache execute. The suite does not exercise CR-01's post-submit field edit. |
| native-after.log | Exact nonempty phase4 Auth/race/6m family exited 0 at 16:29:03Z; package 21.924s. Backend/native source is unchanged in this slice. |
| make-default-final.log / openapi-final.log | Default aggregate and OpenAPI command exited 0. Default Go output explicitly includes cached results; the retained source suite reports 11 OpenAPI cases. These are executor runs, not new reviewer executions. |
| browser-mobile-final.log / browser-mobile-results.json | Retained real Chromium/helper/CLI/Vite/API/PG flow reports 50 workflows, verified viewer, session/list HTTP 200, keyboard skip, sign-out/reload and widths 375/768/1024/1440 without document overflow. Controls measure at least 44px. This reviewer did not rerun the browser or independently certify that harness. |
| contrast-final.log | Eleven static computed color pairs passed their declared thresholds. This is not rendered accessibility certification. |
| final-history-scan.log / final-history-report.json | Retained scan at 798e5ca exited 0; the report was independently parsed as an empty array. It predates this report and is not a fresh reviewer scan. |

Precommit verification logs identify the tests-only base plus the then-working source subsequently committed as 6bb0ac8; they were not relabelled as commands against an already committed GREEN tree. Prior browser launch/setup failures and the corrected skip-link hash assertion remain historical fixture facts, not production regressions.

### Explicitly declined judgments

No full Phase 4, hosted CI, publication, performance or fresh Phase 3 aggregate acceptance is asserted. No expensive native suite, browser, producer, restart, agent, worktree or remote action was run by this reviewer.

Full filter/cursor history, additional routes, detail/evidence/triage, SSE replay and proxy/two-replica/full tenant-matrix behavior remain with their later numbered owners. The current empty filter key is valid for this increment; absent future scaffolding is not a finding.

The client is not an independent full JSON Schema validator for every workflow field or arbitrary corrupt upstream payload. The current native verified-session producer enforces tenant grammar and known roles, and the real PG handler emits the declared DTO. This review does not certify behavior against every possible compromised/malformed server response; ordinary invalid verification bodies and existing guarded cases were reviewed. Browser 200% zoom, screen-reader behavior and complete cross-browser history are also not certified by the retained viewport/component evidence.

The individual-query body-cancellation regression is covered by the current test and guards. This review does not infer future filter/mutation/SSE race coverage from that one test or claim that clearing a mutation cache cancels future mutation effects. No such production operations exist in this increment.

### Preservation and handoff

Before replacement, 04-REVIEW.md was byte-identical to preserved 04-REVIEW-02-R0.md (SHA-256 e8bc9b4f24b22a8567e7b8b8d343437c8a18ea87e68407325d5767e8f2b66886). That report and original 01/17 R0 and accepted 17 R1 reports were left intact.

Only this REVIEW.md was written. No source, generated API declaration, index, HEAD, shared tracking, user-owned .planning/config.json or .serena/ was changed. No commit was made. CR-01 remains open for the orchestrator's authorized repair and re-review.

---

_Reviewer: gsd-code-reviewer_

_Depth: standard; nine-file 04-03 scope_
