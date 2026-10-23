---
phase: 04-query-api-analyst-dashboard
plan: "06"
status: complete
subsystem: api
tags: [clickhouse, events, cursor, react-query, openapi]
requires:
  - phase: 04-05
    provides: Guarded authenticated client and accepted alert evidence details
provides:
  - Tenant-scoped unique event search with native dedupe before filters and paging
  - Authenticated event-specific cursor and bounded filter validation
  - Guarded typed Events page with session-safe navigation and cancellation
affects: [04-07, 04-15, 04-16]
tech-stack:
  added: []
  patterns: [dedupe-before-filter, distinct-event-cursor-AAD, guarded-typed-search]
key-files:
  created:
    - backend/internal/query/events.go
    - backend/internal/storage/clickhouse/query_events.go
    - backend/testphase4/events_test.go
    - web/src/pages/Events.tsx
    - web/src/pages/Events.test.tsx
  modified:
    - backend/internal/query/cursor_test.go
    - backend/cmd/query-api/main.go
    - api/openapi.yaml
    - web/src/api/schema.d.ts
    - web/src/api/client.ts
    - web/src/App.tsx
key-decisions:
  - "Select the earliest received whole event representation before any event filter or page bound; resolve equal received timestamps deterministically by canonical tuple."
  - "Reuse CursorCodec AEAD with distinct event AAD and event-specific typed payload; never overload alert IDs or filters."
  - "Add only the independently approved guarded client path; event registration is cohesive in owned events.go and main.go."
requirements-completed: [API-01, API-08, AUTH-03, UI-03, UI-06]
duration: Approximately 32min from first RED run to candidate verification, including interruptions; not active-work or performance evidence
completed: 2026-10-23
---

# Phase 4 Plan 06: Unique event search

**ACCEPTED after clean independentR1 and root postwave.** Original source candidate
`2436464` implements both tasks; reviewed counterexamples then required repair
RED4619571/GREEN2c6720e. R0 found CR01IPv6 string false negatives and WR01invalid
URL loading, preserved in04-REVIEW-06-R0. Both are now repaired and locally
verified. IndependentR1atc1079a2/source2c6720e closesbothfindingswithzeroissues,
preserved04-REVIEW-06-R1. Rootpostwavebackend/webbuild+normalmake testEXIT0/
12.834s,END2026-10-23T05:06:58.827292Z,cachedGo/web90/generator121,private
root-postwave.log. See04-06-REPAIR for exact gates. Phase4 now7/17 accepted. Requirement IDs
above represent incremental coverage, not whole-phase acceptance. No07,
Phase5, remote push or hosted CI was performed.

## Commits and ownership

- `6ffd056`: tests-only causal RED for actual native Events and actual-App UI.
- `aea966f`: tests-only cursor/negative/native strengthening and causal inactive
  navigation-state regression. Other tests in this refinement already passed;
  no per-case RED or clean pre-implementation history is invented.
- `2436464`: bounded backend/native adapter, guarded client, generated contract,
  Events UI and App navigation GREEN.

The original ten paths could not deliver the session-guarded Events request:
APIClient exposed no event search. Execution stopped before edits; A1 proposed
only client.ts, independently approved PASS/zeroissues before PLAN amendment
`d36e183`. The final union is eleven declared paths (ten tracked plus ignored
generated schema), two original feature tasks. Shared alerts.go/cursor.go,
session/styles/retained fixtures, dependencies, migrations, indexes and CI were
not edited. Userconfig/.serena remain untouched and unstaged.

Executor stopped at a provider usage limit after substantive gates passed and
before source commit/SUMMARY. User requested resume; root inspected the saved
working changes, found no producer/source commit/SUMMARY, and completed the
small owned navigation repair, affected reruns, scans and closeout inline.
No rollback or duplicate implementation occurred.

## Delivered boundary

- Actual main registers one bearer-only GET /api/v1/events through a focused
  EventHandler. Existing API-key single-event route and invalid-bearer
  non-downgrade middleware remain unchanged. Verified principal supplies tenant
  before parsing/storage; dependency errors are503, not fabricated empty data.
- EventFilters/UUID EventPosition and event-specific codec methods live in
  owned events.go. The codec uses existing AEAD with distinct authenticated
  event domain; binds endpoint/tenant/normalized filters/frozen half-open range,
  limit and millisecond position. Alert/event token reuse is rejected. Tokens
  are<=2KiB; strict unknown/repeated/empty/malformed fields fail400. UTC Z,
  default24h/max7d, limit50/max200, canonical IPv4/IPv6 (no zones/mappedIPv6),
  whitelisted event type/outcome and a five-second caller-derived query cap.
- Native Named SQL binds tenant and all values. Whole canonical tuples are
  selected by earliest received_at, deterministic canonical-tuple tie breaker,
  grouping tenant+event_id BEFORE time/IP/type/outcome/cursor predicates,
  descending ordering and Limit+1. No OPTIMIZE/background merge dependency.
  Nullable fields stay in the selected whole row. Existing Event representation
  and Get remain unchanged; no throughput or unbounded-history cost guarantee.
- Guarded typed events(filters,signal) retains pre/post-authority and parsed-body
  generation checks, caller/session abort, current401 revocation, no-store and
  finite page validation. No credential appears in URLs/public context/storage.
- Events has labelled UTC/IP/type/outcome controls, canonical minute edits,
  300ms IP debounce, obsolete request cancellation, loading/empty/error/retry,
  retained real data on refresh failure, bounded20-page previous history and
  safe text. Identity/generation keys and URL owner handling prevent old-session
  cursor authority. App routes/navigates Alerts/Events with Back/Forward;
  inactive link background/marker is transparent while current link retains
  approved semantic colors and aria-current. Event detail remains07.
- Generated schema is ignored, SHA256
  `24c84112b5e3cd4abbd7bc7648b1fcd40ac5ad2a0e27999282800f350d83632e`.

## Actual verification (2026-10-23 UTC)

Private evidence `.local/phase4-acceptance/04-06/`, dirs0700/files0600;
children build/test umask022. Logs preserve commands/HEAD/working state/start/
end/monotonic elapsed/exit. No secrets or resolved Compose configuration printed.

| Log | Actual result |
|---|---|
| native-red | EXIT1/6.991s,04:20:47–04:20:54; actual API/setup succeeds, valid search404 instead of200 |
| web-red | EXIT1/10.979s,04:21:06–04:21:17; nine compiled cases lack Events heading |
| native-complete | EXIT0/6.620s,04:38:16–04:38:22; exact nonempty phase4/race/6m Events family |
| alerts-final/auth-final | Separate EXIT0/6.875s and11.864s,04:34:14–04:34:32; prior exact families retained |
| quality-complete | EXIT0/12.956s,04:37:43–04:37:56; query race60s and default/phase4/integration vet/build/lint0issues |
| openapi-final | EXIT0/2.807s,04:36:56–04:36:59; exact11OpenAPI/race/60s, canonical drift/fixtures |
| default-final | EXIT0/55.338s,04:37:44–04:38:39; timeout300s make test, Go race +web84+locked generator121 |
| root-nav-red | EXIT1/1.300s,04:51:37–04:51:39; inactive Alerts inline background expectedtransparent, actualempty before repair |
| root-final-web | EXIT0/14.333s,04:51:57–04:52:12;84/84 (all64prior+20new), typecheck/lint/build/format after App repair |
| root-final-browser | EXIT0/6.941s,04:52:12–04:52:19; current race-built API/helper/CLI/native PG+CH and Chromium1440/375, no mocked routes |
| root-source-scan | EXIT0; pinned gitleaks8.30.1 staged-source and ignored generated declaration reports[] |

Native commands: `bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Events$' -tags=phase4 -race -timeout=6m -p=1`;
Auth/Alerts use their own exact selector independently. Native fixtures prove
physical rows>unique identities immediately before reads, full unique pages,
timestamp ties, earliest received representation, filter placement, half-open
bounds, IPv4/IPv6, tenant exclusion, legacy auth and dependency failure. Cursor
unit tests cover all filter/range/tenant binding, precision, replica/key/tamper
and disjoint AAD including alert payload labelled events. Default make test's
inner command is go test -race ./..., NOT an asserted explicit60s inner cap;
the separate quick query race command has60s. Original CI/native caps unchanged.

Real browser checks confirm three-row unique timestamp-tied pages, IPv6 debounce
and cursor reset, escaped malicious native source, active-only nav marker,
Alerts/Events Back navigation, focus,44px controls, no horizontal overflow,
logout/draft clearing and no persistent browser credentials/errors at1440/375.
The executor visually inspected the earlier phone screenshot; root repeated
computed/navigation/browser assertions after the final marker fix. Existing
cached Playwright/Chromium/private harness reused, no new dependency. This is
focused slice evidence, not final04-15proxy/two-replica/SSE/fullaccessibility proof.

## Historical failures and limits

First valid native search returned503. Isolated actual CH probes established a
parameter NAME collision: `{limit:UInt64}`/Named limit201 throws exception26;
`{page_size:UInt64}`/Named page_size201 succeeds. Timestamp roundtrip and tuple
aggregation passed separately. An earlier timestamp/type hypothesis and failed
attempts remain recorded; UInt32 vsUInt64 is not claimed as established cause.
Renamed page_size makes the exact native family pass. Web initial72/73 failure
used an inconsistent one-row/default50 has_more fixture; later test clicked Next
while refetch disabled it. Owned harness corrections are not product defects.
Owned lint duplication/complexity/constants/line/overflow fixes passed all three
modes; no lint configuration/shared-file edit or gate reduction.

Karpathy kept changes causal; existing approved UI-SPEC plus targeted UI/UX
active-state guidance supplied the navigation check, not a new design cycle.
Available Playwright guidance supported reuse of the private cached harness.
No destructive git, reset/stash/prune, data/volume deletion, OPTIMIZE, new
dependency, benchmark, security certification, full Phase3 aggregate or hosted
CI claim. Fixture rows remain synthetic and uniquely scoped. Pending independent
source review and root post-wave gate must clear before acceptance/07.

## Self-Check: PASSED

All eleven declared paths exist, source union is exactly ten tracked paths plus
ignored schema; RED commits precede corresponding source GREEN. Source whitespace
checks passed, staged source/generated scan reports are[], user dirt unstaged.
Full-history/metadata scan follows this committed SUMMARY. This self-check does
not substitute for independent review/root acceptance.

## Review repair addendum

Original scoped passing evidence did not cover stored expanded/uppercase IPv6
or initial/history invalid links. Causal nativeRED returned5vs15; sixcomponent
REDcases lacked validation.2c6720e compares normalized nativeIP representations
after dedupe, retaining all stored bytes and tenant/boundparameter semantics;
allthreequeryspellings nowreturntheexact15orderedIDsacrossfivepages. Restored
appliedfilters derive visible validation and disabled queries no longer show
loading. Clearfilters recovers.90webtests andquality/nativeEvents+Alerts+Auth/
OpenAPI11/actualChromium1440+375/defaultmake test nowPASS. Full details, failed
static-loopstyle attempt and actualtimestamps/durations are in04-06-REPAIR.
Only four originallyowned source/test files changed from2436464; generated
schema and otherreviewedpaths unchanged. IndependentR1 and rootpostwave then
passed as recorded above; historical failures remain unchanged.
