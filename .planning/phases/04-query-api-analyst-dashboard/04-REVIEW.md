---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-22T21:59:40Z
depth: standard
review_slice: 04-04
review_round: R0
review_sha: 31ab1ccc7a63e228b84dcbf0a40b0b9cde89acc6
source_sha: 0abb49d2855225faba0949ac86aa33255ce4224c
diff_base: 3dee0ab075c0c96b6bc8257722a9aa21b2ee358d
files_reviewed: 10
files_reviewed_list:
  - backend/internal/query/alerts.go
  - backend/internal/query/cursor.go
  - backend/internal/query/cursor_test.go
  - backend/internal/storage/postgres/query_alerts.go
  - backend/testphase4/alerts_test.go
  - api/openapi.yaml
  - web/src/pages/Alerts.tsx
  - web/src/pages/Alerts.test.tsx
  - backend/cmd/query-api/main.go
  - web/src/App.test.tsx
generated_boundary_context:
  - web/src/api/schema.d.ts
findings:
  critical: 1
  warning: 0
  info: 0
  total: 1
status: issues_found
---

# Phase 4 Plan 04: Code Review Report

**Reviewed:** 2026-10-22T21:59:40Z

**Depth:** standard; all ten tracked scoped files read in full, plus the ignored generated declaration as boundary context.

**Status:** issues_found — one BLOCKER; independent source acceptance must wait for repair and re-review.

## Narrative Findings (AI reviewer)

### Summary and scope

Normal user edits of the UTC time inputs can produce requests the API necessarily rejects. This breaks a delivered filter path despite the retained native, component and browser passes. No additional actionable security or correctness finding was established in the remaining reviewed code.

The explicit eleven-path envelope comprises the ten tracked source paths above and ignored generated web/src/api/schema.d.ts. The declaration was read in full but excluded from the source count under the generated-file rule; its SHA-256 is e6bbab8336ea22bfe93d1105c741b048414d083eb0114de31a0922fe65f26b7d. All ten tracked files were independently confirmed byte-identical between source candidate 0abb49d, frozen HEAD 31ab1cc and the working tree. Scoped git diff --check passed.

Read 04-04 PLAN/SUMMARY, shared CONTRACT, approved UI-SPEC, VALIDATION, RESEARCH, PROJECT/current STATE, full A1 amendment/check, ADR0006/0008, actual session/client/App callers, PG003 and retained store/event-handler boundaries. No AGENTS.md, project .codex/skills/, .agents/skills/ or .codexignore exists in the working directory. No structural/fallow pre-pass was supplied or run. GSD review, Karpathy and applicable UI/UX guidance informed the review. No unavailable React/Postgres skill was represented as read; actual SQL, migration and installed Go AEAD documentation were inspected directly.

### Critical Issues

#### CR-01: BLOCKER — native time edits send timestamps rejected by the API

**File:** /home/logan78/Desktop/Sentinel/web/src/pages/Alerts.tsx:294 and :307

**Related boundary:** /home/logan78/Desktop/Sentinel/web/src/pages/Alerts.tsx:94; /home/logan78/Desktop/Sentinel/backend/internal/query/alerts.go:202

**Issue:** Both datetime-local handlers append `Z` directly to the native input value. A datetime-local input normalizes a whole-minute value to `YYYY-MM-DDTHH:mm`, even when assigned zero seconds/milliseconds with step 0.001. Selecting From `2026-10-21T00:00` and To `2026-10-22T00:00` stores and sends `2026-10-21T00:00Z` and `2026-10-22T00:00Z`. Date.parse-based validFilters accepts this ordered 24-hour range. parseQueryTime requires explicit seconds and rejects both values, so Apply filters produces HTTP 400 and the generic load error. Retry repeats the invalid request. Editing just one bound to a whole minute is sufficient. This is ordinary supported user input.

**Reviewer reproduction:** A read-only Node diagnostic using installed jsdom assigned `2026-10-21T00:00:00.000` to the actual native input type; input.value returned `2026-10-21T00:00`. A second diagnostic evaluated the actual validFilters function extracted/transpiled from Alerts.tsx and the actual backend regex extracted from alerts.go. For the two onChange-produced values above it returned `actualValidFilters: true` and `actualBackendPatternAccepts: [false,false]`. toISOString converts the same valid UTC instants to accepted `2026-10-21T00:00:00.000Z` and `2026-10-22T00:00:00.000Z`. No source or fixture was written for these diagnostics. This is a DOM/parser boundary reproduction, not a newly executed full HTTP/browser run.

**Fix:** Canonicalize each nonempty valid datetime-local value as UTC before it enters normalized filters/request/key, for example `new Date(value + "Z").toISOString()` after checking the parsed instant is finite. Preserve empty-value and malformed-link recovery so the repair does not reintroduce RangeError. Use identical normalized values in request, cache key and URL. Add an owned Alerts component regression for valid minute-only From/To edits asserting exact canonical UTC millisecond request values; retain second/millisecond input behavior and reversed/malformed-range cases. Keep strict server validation.

**Why current evidence misses it:** Alerts.test.tsx covers valid non-time filters and invalid/reversed time ranges, but no successful minute-only user time edit. The real browser harness fills status, severity, rule and search, but never either time field. Native tests construct timestamps using Go's RFC3339 formatter. Their passing results do not cover this mismatch. The missing regression belongs to CR-01 and is not separately counted.

### Remaining boundary assessment

These observations bound the review; they do not waive CR-01 or establish whole-phase acceptance.

- PG query_alerts.go:29–37 applies verified tenant, half-open range, status, severity, rule and escaped literal search before limit+1. Selection, cursor predicate and descending order use the same millisecond tuple, including PG003's actual submillisecond rows. Caller cancellation reaches the bounded store context; scan/iteration errors propagate. No extra index or performance guarantee is demanded.
- query/alerts.go:115–151 obtains authenticated tenant authority before storage access, rejects query parsing errors and maps storage/cursor failures to bounded public errors. Raw UTF-8/control validation precedes trim. Page, range, query multiplicity and cursor byte bounds are enforced. WorkflowAlert contains exactly fifteen PG003 public fields; no canonical detection/evidence values are fabricated or fetched from ClickHouse for this list.
- cursor.go:32–98 derives a distinct AEAD key through HMAC/domain separation, uses installed Go random-nonce GCM correctly, authenticates payloads and binds endpoint, tenant, normalized filters, frozen range, limit and tuple. Omitted ranges come from authenticated payloads. Installed Go documentation confirms generated/prepended nonces and the zero-size nonce argument; its documented per-key message limit is not an unlimited key-lifetime claim.
- The query key includes verified subject/tenant/role/generation and the same query object passed to the typed client. Superseded requests use abort signals through existing lifetime/body guards. Session subtree cleanup clears current URL cursors; history owner markers prevent subsequent sessions from restoring old page authority. Cursor-only links preserve absent opaque bounds without decoding. Explicit previous history is capped at twenty and resets on filter/history/session changes.
- A1's App.test.tsx diff contains one strict HTTP-derived helper and exactly two approved seventh-element expectation replacements. The first six identity/generation elements and prior security cases remain unchanged. The helper validates the corresponding HTTP request's exact default keys, limit 50, finite canonical UTC millisecond values and exactly 24 hours; it neither derives expectations from cache values nor uses partial key matching.
- OpenAPI/generated list types retain the workflow DTO boundary and canonical Event/Alert schemas, adding search/cursor constraints. Command wiring registers the alert list once through authenticated dependencies and retains legacy event lookup. Manual CSS/mobile list and labelled filter/error/page controls are reused; no unsafe HTML rendering or new design framework was introduced.

### Retained evidence and limits

Selected actual logs/result metadata and the complete browser harness in authorized ignored .local/phase4-acceptance/04-04 were read. Directory mode is 0700 and sampled evidence files are 0600. No key, bearer value, Authorization capture or resolved Compose configuration was printed. These are executor runs, not fresh reviewer executions:

| Retained evidence, October 22 UTC | Actual result and scope |
|---|---|
| native-red.log, 21:14:14–21:14:20 | Exit 1: actual HTTP valid filters rejected, empty/malformed fields accepted. Setup is distinct from behavioral RED. |
| native-postlimit-negative.log, 21:21:01–21:21:06 | Exit 1 at intended full-page assertion, two items versus three. Correct SQL restored; causal negative control. |
| web-resume-diagnosis.log, 21:28:57–21:29:01 | Exit 1, 46/47: cursor-only request invents from/to. Current source preserves omitted bounds. |
| web-malformed-range-red.log / native-search-controls-red.log | Separate actual exit-1 RangeError and raw newline-search 200-versus-400 failures. Current corresponding paths are repaired; neither covers CR-01. |
| web-quality-green.log, 21:31:24–21:31:36 | Exit 0: web 48, typecheck/lint/build/format. Metadata identifies tests-only HEAD plus working GREEN. |
| native-clean.log, 21:38:05–21:38:30 | Exit 0: separate nonempty Alerts/Auth phase4 race selectors, six-minute caps; retained Auth 26 top-level/24 nested CLI cases. |
| native-filter-fixtures-final.log, 21:39:11–21:39:18 | Exit 0: strengthened actual Alerts HTTP/PG family, package 3.832s, phase4 vet/lint zero issues. |
| browser-final.log/results, 21:38:03–21:38:19 | Exit 0: fresh race-built API, actual helper/CLI/PG, private Vite proxy, cached Chromium at 1440/375. Distinct full three-row pages, Previous/Back/Forward, exact cursor-only page, search/Clear/signout, no route mocks. No time edits. |
| default-final.log, ending 21:53:12 | Exit 0: default aggregate, web 48 and generator 121. Cached package results remain identified as cached. |
| final-history-scan.log/report, 21:55:17–21:55:18 | Exit 0 at d47e124; parsed JSON is exactly an empty array. Retained scanner evidence is not blanket security certification. |

Browser metadata records all observed alert responses 200, 44px controls and no document overflow, browser storage exposure, console warnings/errors or page errors. The harness establishes its limited exercised paths; this reviewer did not rerun the browser or inspect screenshots anew. Historical web-first 41/46, later type/lint failures and cursor-unit setup limitations remain disclosed; no per-case causal RED is invented for already-correct or initially unbuildable cases.

### Assessment, declined future judgments and preservation

Fix CR-01 before accepting 04-04. The bounded repair belongs to existing owned page/component-test paths and requires no new subsystem. Re-review should verify canonical conversion, a causal regression and affected gates. Root owns subsequent acceptance/tracking; the prior four accepted slices remain four of seventeen.

No demand is made for future detail/evidence/triage/events/rules/Overview/SSE/durable replay/proxy/two-replica/full tenant-matrix features, status-edit snapshot isolation, an extra migration/index or Phase5. Their absence is not counted as a defect in 04-04. No fresh full Phase3 aggregate, complete accessibility/screen-reader/200%-zoom/cross-browser audit, performance guarantee, hosted CI or security certification is claimed.

Before replacement the old report matched preserved 04-REVIEW-03-R1.md (SHA-256 cdff77cab9e47d3d49a5fb5d4a022273e73c4ae4e97a905bfdbf82dbea720987). All earlier reports and A1 artifacts remain unchanged. Only this designated report was written. No source, index, HEAD, STATE, user-owned .planning/config.json or .serena/ was changed; no commit, agent, worktree, service startup, producer, later slice or remote action occurred.

---

_Reviewer: gsd-code-reviewer_

_Depth: standard; explicit 04-04 ten-source-file scope plus generated boundary context_
