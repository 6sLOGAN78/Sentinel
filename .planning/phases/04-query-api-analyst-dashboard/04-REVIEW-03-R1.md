---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-22T21:03:33Z
depth: standard
review_slice: 04-03
review_round: R1
review_sha: 0151e19ed6bd968d62406f253a518dcc9725e52d
source_sha: a793313d84630c0f9daf6d3b1f049382ad28e7ed
diff_base: 21ebf9840c57edd3125ba0b8be3d7508c525d5e5
prior_review_sha: 4ed9a93c496ff843c6b17140074d1799b4e8ffeb
prior_source_sha: 6bb0ac84ef7d0267cc5a4f54ec8d8b7e0a6e9bc4
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
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 4 Plan 03: Code Review Report

**Reviewed:** 2026-10-22T21:03:33Z

**Depth:** standard; focused repair re-review with explicit unchanged-file carry-forward from the preserved full nine-file R0.

**Files Reviewed:** 9, with the exact scope above.

**Status:** clean — original BLOCKER CR-01 closed; no remaining actionable findings established within this slice.

## Narrative Findings (AI reviewer)

### Summary

The credential draft now belongs to a dedicated sign-in component. Successful verified bootstrap removes that component; a nonsecret provider revision resets its controlled input on verification restart, retry and every sign-out/revocation path. The original post-submit-edit/sign-out sequence and retained-retry deadline gap are addressed without exposing a raw credential through context or changing client/cache authority guards.

This is independent source review of repair source a793313 at frozen HEAD 0151e19, not whole-Phase-4 acceptance. The root orchestrator owns post-wave verification and tracking; this report does not advance the recorded 3/17 accepted slices.

No structural/fallow pre-pass was supplied or run. No AGENTS.md, .codex/skills/, .agents/skills/ or .codexignore exists in the working directory. Repository ignores were respected; ignored evidence was consulted only as explicitly authorized verification material, not treated as submitted source. The GSD review instructions, React review guidance and selected render/effect rules, updated 04-03 PLAN/SUMMARY, shared CONTRACT and approved manual-CSS UI-SPEC were read.

### Method and unchanged-file proof

The full contents of all three files changed since original source 6bb0ac8 were read afresh: App.tsx (130 lines), App.test.tsx (787 lines) and auth/session.tsx (214 lines). Their complete changes were assessed against the preserved original full review, 04-REVIEW-03-R0.md. Current client.ts, session.test.tsx, Alerts.tsx, package.json and styles.css were also read afresh for context, together with actual main.tsx/test cleanup callers and installed QueryClient cancellation/clear implementations. The lockfile's prior full dependency review is carried forward by byte identity; this is not a claim that every lockfile record was newly reread.

Git object comparisons independently established the following identical blobs at prior source 6bb0ac8, frozen HEAD 0151e19 and the current working files:

| Unchanged path | Identical Git blob |
|---|---|
| web/package.json | b1d969e95cbfb25572be784c48700e944deb9664 |
| web/package-lock.json | 7ca21dc019c4a496219d2b9bd4a9bb789ba5c923 |
| web/src/api/client.ts | 2e05bbfff7a3e21c2892be088055bf47ae24ea77 |
| web/src/auth/session.test.tsx | 62e2024fe55b3e9067c08be60f3563701b96c774 |
| web/src/pages/Alerts.tsx | 8cdc9b33e12ff7e77d1c574a12bde4069b120783 |
| web/src/styles.css | 67bf8bd0d27206b628fe01e7c0a418cb8fac239d |

All nine scoped files were separately confirmed byte-identical between repair source a793313, frozen HEAD 0151e19 and the working tree. The prior full scope is therefore preserved transparently. The explicitly named lockfile remains included despite the generic workflow's normal lockfile exclusion. git diff --check reported no scoped whitespace errors.

Tests-only commit 56b1d4c adds 154 lines to App.test.tsx; it deletes or alters none of the original test body. An independent byte-prefix comparison verified the original body, excluding its closing describe delimiter, remains intact. Production GREEN a793313 changes only App.tsx and auth/session.tsx. No tenth source path or source deletion was found.

### Closed original BLOCKER CR-01: credential draft survives revocation

Original report: 04-REVIEW-03-R0.md, CR-01. Its original severity was BLOCKER, for credential text retained in the input after revocation; it did not establish an authentication bypass. The original report remains immutable. This closure is not a new finding counted in the frontmatter.

**Reviewed locations:** /home/logan78/Desktop/Sentinel/web/src/App.tsx:7; /home/logan78/Desktop/Sentinel/web/src/App.tsx:110; /home/logan78/Desktop/Sentinel/web/src/auth/session.tsx:106; /home/logan78/Desktop/Sentinel/web/src/auth/session.tsx:129; /home/logan78/Desktop/Sentinel/web/src/App.test.tsx:634.

The repair addresses the original causal paths:

| Trigger | Clearing and authority path |
|---|---|
| Submit or replace a credential | App.tsx:23 clears the current draft; verify:131–133 disposes the previous generation and advances signInRevision before awaiting the request. |
| Retry a retained attempt | session.tsx:197–201 evaluates the retained credential/deadline before verify disposes the old ref. The same verify revision clears a separately typed draft; the draft is not substituted for the retained credential. |
| Successful verified bootstrap | session.tsx:157–168 publishes authority only after existing identity/expiry checks. App.tsx:110 replaces SignInPanel with Alerts, removing the draft-owning component. A later signed-out panel starts empty. |
| Explicit sign-out or Cancel | Both UI handlers call signOut; session.tsx:108–114 disposes authority and advances the revision while clearing pending/retry/deadline/error state. |
| Retry deadline or authenticated expiry | The existing deadline effect calls that same signOut at session.tsx:123. A draft entered during retained retry resets while the sign-in component remains mounted. |
| Verification 401 or already-expired verification | session.tsx:161–173 routes these through signOut, clearing pending draft state and retained authority. |
| Authenticated request 401 | The client callback at session.tsx:152–154 routes only a current authenticated attempt through signOut. Stale clients retain their existing generation/lifetime guards. |
| Provider/App unmount | The sign-in subtree is removed; the provider's existing cleanup disposes active credentials, lifetime requests and cache. A remount begins without draft or authority. |

At App.tsx:10–16, the revision/value pair is local state. A changed provider revision causes one conditional update to that same component; the next render sees equality and terminates. There is no effect-delayed stale-input commit, ancestor state update, changing component definition or reset-key replacement of the live input. The stable module-level component and existing controlled input preserve DOM identity across revision resets.

React documents that a guarded update of the currently rendering component discards that render's JSX and retries before updating child DOM. That supports this particular conditional reset; the equality guard prevents a render loop. [Official React guidance](https://react.dev/learn/you-might-not-need-an-effect#adjusting-some-state-when-a-prop-changes). The reviewed implementation uses only a local state update in that branch.

All eight new cases run the actual App/SessionProvider/Alerts/QueryClient tree with fetch-only responses. They cover pending edit followed by bootstrap and sign-out/expiry/authenticated 401; retained-retry edit followed by deadline/Retry/restart/Cancel; and verification 401. Current assertions re-query the displayed field for the regression and retain cache/authority/abort checks. Earlier input-reference assertions are unchanged. The final input remains editable while verification is pending, so the original trigger was not removed from testing by disabling it.

No remaining cause for CR-01 was established in this review.

### Authority, cache and privacy regression assessment

The provider's production diff adds only the numeric signInRevision declaration, state and two functional increments, plus its context value. Existing generation revocation, active credential refs, abort-before-cache disposal order, verified identity gate, retry deadline, stale-response checks and error handling remain unchanged.

The actual App still supplies one lazily owned or caller-supplied QueryClient to both session and Alerts. Installed query-core cancelQueries handles its returned rejection, and clear synchronously clears both caches; no await was introduced that could hold revocation behind uncooperative network completion. The current query key still includes verified subject, tenant, role and generation. Client checks before credential access and after fetch/body parsing, plus individual-query abort checks, remain intact.

No raw token was added to context, query keys, browser persistence, URLs, rendered identity/error text or a public reset API. The new revision is an unrelated number. Actual verified identity still controls authority; decoded expiry can only shorten retained-retry lifetime. Availability failure intentionally retains its finite retry attempt until retry/cancel/deadline, and no new automatic credential retry was introduced.

### Retained verification evidence and actual failures

These are executor runs whose actual logs/selected result metadata were read by this reviewer. No new component suite, native suite, producer, browser, scanner or service startup was executed during R1. Source tracing and Git identity checks above are reviewer-performed checks.

Evidence is under the explicitly authorized ignored directory .local/phase4-acceptance/04-03/cr01/. Its directory mode is 0700 and listed evidence files are 0600. No real token, key, Authorization value, resolved Compose configuration or secret capture content was inspected or printed.

| Retained evidence, 2026-10-22 UTC | Observed result and limits |
|---|---|
| draft-red.log, 20:53:22–20:53:24 | Exit 1: six intended nonempty-input assertion failures, two already-passing restart/Cancel characterizations, 21 unrelated cases excluded by the focused selector. This is causal behavioral RED, not a setup failure. |
| web-first.log, 20:54:22–20:54:25 | Exit 1: five original input-reference assertions fail with the intermediate keyed-panel implementation; 31 tests pass. The subsequent commands in its chain did not run. These failures were preserved and the final implementation was corrected without changing the assertions. |
| web-green.log, 20:55:07–20:55:18 | Exit 0: all 36 tests, typecheck, lint, build and format checks, each bounded at 120 seconds. Metadata truthfully identifies tests-only HEAD 56b1d4c plus working production changes later committed as a793313. |
| preserved-startup.log, 20:54:45–20:55:23 | Exit 2: normal make dev-up stops at unhealthy detector; its chained check-stack did not execute. This remains a failed startup, not a passing command. |
| stack-recheck.log, 20:55:46–20:56:00 | Separate read-only check exits 0 with all dependencies/apps, readiness, topics and native dependency checks healthy. This does not establish the exact recovery cause or erase the earlier failure. No repair-source cause for detector startup was established. |
| native-green.log, 20:55:46–20:56:00 | Exact nonempty phase4-only TestPhase4Auth selector, -race, -timeout=6m, -p=1 exits 0; package 12.294s. It retains the 26 top-level/24 nested CLI cases. Backend/native source is unchanged by the repair. |
| browser-green.log / browser-draft-results.json, 20:57:06–20:57:08 | Exit 0 at committed a793313: pending field edit, real verified bootstrap with 50 PG workflows, then sign-out with empty displayed input and no tenant data, at 1440 and 375 pixels. |
| default-green.log, 20:57:14–20:57:25 | Exit 0 at a793313: default Go build and make test, including backend race, web 36 and generator 121 tests. Cached Go package results are explicitly retained, not called fresh executions. |
| final-history-scan.log, 20:59:47–20:59:48 | Full-history redacted gitleaks run at frozen 0151e19 exits 0; 513 commits. The final JSON report was independently parsed as exactly an empty array. This is retained scanner evidence, not a fresh reviewer scan or blanket security certification. |

The focused browser harness was read: it obtains a token privately through the actual helper/CLI, reaches Vite/API/PostgreSQL, holds the real HTTP 200 session response, permits the post-submit edit and releases that original response unchanged. It does not fabricate session/list payloads. Its report records no overflow, storage exposure or browser console/page errors at those two widths. Timer/401/retry variants rely on actual-component evidence with fetch mocked. This reviewer did not rerun the browser or newly visually inspect its screenshots.

The original four-viewport, keyboard, reduced-motion and contrast evidence remains historical evidence from the prior full review. CSS byte identity supports carrying that scoped assessment forward; it does not turn those observations into a fresh accessibility audit.

### Limits and explicitly declined future features

No full Phase 4, fresh Phase 3 aggregate, hosted CI, publication, production security or performance certification is asserted. The intermediate startup failure has no established exact cause; it remains separately disclosed evidence, not an accepted new behavior or waived gate.

Full alert filters and cursor/history handling, additional routes, evidence/details/triage, SSE/durable replay, full proxy/two-replica/tenant-matrix coverage and Phase 5 remain with their later numbered owners. The current empty filter key is valid for this increment. Their absence is not a defect in this repair, and none was started or represented as delivered here.

The current frontend is not a full runtime JSON Schema validator for arbitrary compromised-server payloads. The bounded native verified-session and workflow producers remain the current boundary. The prior review's dependency/DTO assessment is carried forward by byte identity, not expanded into fresh registry/audit certification. Screen-reader behavior, 200% zoom and complete cross-browser history are not certified.

### Preservation and handoff

Before replacement, 04-REVIEW.md matched immutable 04-REVIEW-03-R0.md, SHA-256 24ee8363164686b119c9d1c2642af37a274521b1210e14326b4c1d19de0847d1. The original 03 R0 and all 01/17/02 reports were preserved.

Only this designated REVIEW.md was written. No source, index, HEAD, shared tracking, user-owned .planning/config.json or .serena/ was changed; no commit, agent, worktree, later-slice or remote action occurred. CR-01 is closed for independent source review. Root post-wave checks and acceptance/tracking remain the orchestrator's responsibility.

---

_Reviewer: gsd-code-reviewer_

_Depth: standard; explicit nine-file 04-03 scope, focused R1 with byte-proven carry-forward_
