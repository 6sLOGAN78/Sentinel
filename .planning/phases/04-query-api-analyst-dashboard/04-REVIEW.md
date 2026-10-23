---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-23T04:58:12Z
depth: standard
review_slice: 04-06
review_round: R0
review_sha: bc27d83abcbeba290a2e9d08f044c606b07d60d9
source_sha: 2436464
diff_base: 74578e9
files_reviewed: 11
files_reviewed_list:
  - backend/internal/query/events.go
  - backend/internal/storage/clickhouse/query_events.go
  - backend/internal/query/cursor_test.go
  - backend/testphase4/events_test.go
  - backend/cmd/query-api/main.go
  - api/openapi.yaml
  - web/src/api/schema.d.ts
  - web/src/pages/Events.tsx
  - web/src/pages/Events.test.tsx
  - web/src/App.tsx
  - web/src/api/client.ts
generated_schema_sha256: 24c84112b5e3cd4abbd7bc7648b1fcd40ac5ad2a0e27999282800f350d83632e
findings:
  critical: 1
  warning: 1
  info: 0
  total: 2
status: issues_found
---

# Phase 4 Plan 06: Code Review Report

**Reviewed:** 2026-10-23T04:58:12Z

**Depth:** standard; all eleven explicitly declared paths read in full.

**Status:** issues_found — one BLOCKER and one WARNING.

## Narrative Findings (AI reviewer)

### Summary and scope

The event-search increment has a reproducible IPv6 false-negative bug and an invalid-deep-link state defect. Review covers source candidate 2436464 at frozen HEAD bc27d83abcbeba290a2e9d08f044c606b07d60d9 against accepted baseline 74578e9. Read the complete 04-06 PLAN, OWNERSHIP-A1, OWNERSHIP-A1-CHECK, CONTRACT, UI-SPEC and SUMMARY; the SUMMARY was context rather than correctness authority. Read each declared source/test file and its tracked diff. The expressly declared ignored generated schema was inspected and its hash verified without regeneration or staging.

No root AGENTS.md, .codexignore, project .codex/skills or .agents/skills directory was present. Applied the GSD code-review skill. No structural/fallow findings were supplied. Dependencies traced include event validation and ingestion normalization, Kafka serialization, native event insertion/Get and table schema, shared cursor/time/page primitives, bearer verification and legacy lookup, SessionProvider, existing Alerts navigation behavior, and React Query disabled-query state semantics.

### Critical Issues

#### CR-01: Canonicalizing only the query makes valid stored IPv6 events unsearchable

**Classification:** BLOCKER

**File:** `/home/logan78/Desktop/Sentinel/backend/internal/storage/clickhouse/query_events.go:55-59`

**Related:** `/home/logan78/Desktop/Sentinel/backend/internal/query/events.go:216-221`

**Issue:** The handler converts every IPv6 filter to `netip.Addr.String()`, but the adapter compares it directly to the retained `source_ip` String. The accepted Event contract permits expanded, zero-padded and uppercase IPv6 spellings: `contracts/event.go:328-343` validates without rewriting them, ingestion and Kafka preserve their original bytes, and `storage/clickhouse/events.go:70-74` inserts those bytes into Nullable(String). An event ingested with `2001:0db8:0:0:0:0:0:44` or `2001:DB8::44` appears in unfiltered results but disappears when searched by its own IP. Supplying the exact displayed spelling does not help because the handler always rewrites it to `2001:db8::44`. This violates the delivered IPv6 search behavior and can hide relevant retained events from investigations.

**Reproduction/evidence:** A reviewer-run read-only SELECT on the existing native ClickHouse server used `values('source_ip Nullable(String)', ('2001:0db8:0:0:0:0:0:44'), ('2001:DB8::44'), ('2001:db8::44'))`. Evaluating the adapter's comparison `source_ip = '2001:db8::44'` returned 0, 0 and 1 respectively, while `IPv6NumToString(IPv6StringToNum(source_ip))` returned `2001:db8::44` for all three rows. The native fixture stores only the already-compressed spelling (`events_test.go:291`); expanding the query at :158 does not exercise the stored-spelling case.

**Fix:** After selecting the whole retained identity, compare IP addresses in a normalized/native address representation on both sides, preserving tenant selection, dedupe-before-filter semantics, nullable handling and bound parameters. Handle existing stored rows; normalizing only future writes is insufficient. Add native regression rows with expanded and uppercase IPv6 values and prove canonical and alternate query spellings return the same identities without duplicates or changed page order.

### Warnings

#### WR-01: Invalid Events deep links display a permanent loading state

**Classification:** WARNING

**File:** `/home/logan78/Desktop/Sentinel/web/src/pages/Events.tsx:211`

**Related:** `/home/logan78/Desktop/Sentinel/web/src/pages/Events.tsx:133-142`, `/home/logan78/Desktop/Sentinel/web/src/pages/Events.tsx:350`

**Issue:** Initial URL filters and popstate filters enter component state without setting validation. When an IP, limit or time range is invalid, `enabled` disables the query; a disabled query without data still has `isPending === true`, so the page displays “Loading events…” indefinitely although no request is running. IP debounce does not repair initial IP errors because draft and applied IP already match. Users following an invalid or stale filter link receive neither a field error nor an explanation until they manually submit or clear the form.

**Reproduction/evidence:** Start at `/events?source_ip=bad`, `/events?limit=201`, or `/events?from=2026-10-23T00:00:00Z&to=2026-10-22T00:00:00Z`, then authenticate. A read-only diagnostic evaluated the actual extracted/transpiled filter helpers and the installed React Query QueryObserver. All three yielded `enabled:false`, `requests:0`, `isPending:true`, `isFetching:false`, `status:"pending"`. Source initializes validation to an empty string and renders loading from `isPending` alone. Existing invalid-filter tests submit the form after a valid initial load, missing URL initialization and Back/Forward.

**Fix:** Validate restored filters at initialization and on popstate, show the appropriate existing validation message, and render loading only for an enabled fetch state (for example `isLoading`, with explicit invalid-filter handling). Add component cases for invalid initial URLs and history navigation, asserting a visible actionable error and no permanent loading indicator or request.

### Other boundaries examined

Traced verified tenant selection before storage, Named SQL parameters, whole nullable tuple selection before event filters/order/limit, earliest received_at and deterministic tie selection, millisecond UUID tuple pagination, half-open range/limit bounds, five-second caller-derived query deadlines, strict query fields, and disjoint event/alert AEAD AAD and payload binding. Examined single list registration, unchanged legacy lookup/non-downgrade, guarded client cancellation and post-body authority checks, current-session 401 handling, finite page envelopes, escaped event rendering, debounce, bounded previous history, owner markers, and App navigation/focus. No additional actionable finding was established at this depth. Future event details, triage, SSE, metrics and Phase 5 are outside this slice.

### Independent diagnostics and limits

- `go test -race ./internal/query -timeout=60s` from backend: exit 0, cached.
- Direct `./node_modules/.bin/vitest run src/pages/Events.test.tsx` from web: exit 0, 20/20 tests, 1.83 seconds. No npm pretest or generated-file rewrite.
- Read-only native ClickHouse SELECT over inline values: exit 0; IPv6 mismatch demonstrated above. No data/schema changes.
- Actual filter-helper/installed QueryObserver diagnostic: exit 0; disabled permanent-pending states demonstrated above.
- `git diff --check 74578e9..2436464` over the ten tracked declared paths: exit 0.

Prior native, full web, OpenAPI, browser and other gate results were not independently rerun or recertified here. Focused passing tests do not negate the counterexamples. This is standard source review, not whole-phase acceptance, accessibility certification or a production security guarantee. No service, migration, remote action or native write fixture was started by this reviewer.

### Preservation

Only this designated report was written. The parent preserved the previous report as 04-REVIEW-05-R0.md. No source/test file, generated declaration, Git index, commit, root tracking, user configuration or .serena content was changed. No dependency was installed.

---

_Reviewer: gsd-code-reviewer_

_Depth: standard_
