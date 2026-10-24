---
phase: 04-query-api-analyst-dashboard
plan: "08"
subsystem: triage
tags: [postgres, optimistic-concurrency, audit, react, openapi]
requires:
  - phase: 04-07
    provides: Tenant-scoped detail UI, guarded client and authorized NEW runtime context
provides:
  - Atomic open-to-acknowledged workflow and audit transaction
  - Explicit production PATCH injection and generated guarded browser method
  - Bounded tenant history and real native/browser race and privacy proof
affects: [04-09, 04-15, 04-16]
tech-stack:
  added: []
  patterns: [tenant-scoped CAS, same-transaction audit, PG-only mutation response]
key-files:
  created:
    - backend/migrations/postgres/007_alert_audit.sql
    - backend/internal/storage/postgres/triage.go
    - backend/internal/query/triage.go
    - backend/testphase4/triage_test.go
  modified:
    - backend/internal/query/alerts.go
    - backend/cmd/query-api/main.go
    - api/openapi.yaml
    - web/src/api/client.ts
    - web/src/api/schema.d.ts
    - web/src/components/AlertDetails.tsx
    - web/src/components/AlertDetails.test.tsx
key-decisions:
  - Return committed PG WorkflowAlert without ClickHouse enrichment
  - Keep resolve unavailable with 409 until plan09
  - Use current-source ephemeral API/proxy for browser acceptance without rebuilding shared services
requirements-completed: [API-05, API-04, AUTH-02, AUTH-03, UI-02, UI-06]
duration: 14 min
completed: 2026-10-24
---

# Phase 4 Plan 8: Atomic acknowledgement Summary

**Tenant/version/status guarded PostgreSQL acknowledgement commits one audit row, with guarded browser actions and real concurrent HTTP acceptance.**

Two implementation tasks complete. Source base `4380e39`; tests-only RED
`49d10c4`; GREEN `360100e`. Final native and browser tests executed at exact
source HEAD `360100e`. Earlier GREEN gates record `49d10c4` plus their actual
uncommitted source changes. Evidence begins 2026-10-24 16:28:13Z; final native
and browser proof ended 16:41:10Z / 16:41:12Z. This summary records the bounded
increment; root independent review, shared tracking updates and whole-phase
acceptance remain pending.

## Task commits

| Task | Commit | Result |
| --- | --- | --- |
| 1. Pin behavior and causal RED | `49d10c4` | Actual native PATCH405 versus expected200/409, and absent browser Acknowledge button |
| 2. Deliver acknowledgement and audit | `360100e` | Atomic PG transition, explicit route/client wiring, migration, UI and expanded native/component checks |

The source union is exactly eleven approved paths: ten tracked paths and the
regenerated ignored `web/src/api/schema.d.ts`. No force-add. The summary is the
only additional tracked executor artifact. No source deletion, dependency,
scanner-policy or framework change. User `.planning/config.json` and `.serena/`
remain untouched and unstaged. STATE/ROADMAP/REQUIREMENTS/VALIDATION and review
artifacts remain root owned; no SDK mutation was performed.

## Delivered behavior

Migration007 creates `alert_status_audit` with alert/version uniqueness, tenant
index, actor/version checks and forward-transition checks. The actual tern
migrator ran against NEW PG25432 in the native fixture; final metadata proof
confirms schema version7 and the audit table.

`Transition(ctx, tenant, id, actor, expectedVersion, target)` uses the existing
five-second store timeout and independent one-second rollback. The conditional
UPDATE requires tenant, ID, expected version and open status. Its audit INSERT
and COMMIT share the transaction; actor comes from verified identity and time
from the server. Unknown/foreign IDs return404, viewer mutation403, invalid
bodies400, bodies above4096bytes413, stale/illegal transition409. Duplicate and
unknown fields are rejected. Only open→acknowledged is delivered; resolve
returns409 until09.

Main explicitly injects the PG triage adapter. PATCH returns committed
`WorkflowAlert` without a CH call. Full detail GET independently reads up to two
tenant-scoped audit rows ordered by version. Native proof covers one race
winner/one conflict, one audit/version increment, injected audit failure with
complete rollback, replay preserving status/version/seq/audit, five-second row
lock bounds, cancellation, history order/bounds and a successful mutation while
the fixture-specific CH address is unreachable. No shared service was stopped
to simulate an outage.

AlertDetails uses the existing private generated transport, preserving lifetime,
generation, caller-abort, post-body and401 checks. It disables pending controls,
hides mutation for viewers, retains committed workflow state during detail
refresh, and refetches409 without automatic PATCH retry. A new action requires
an explicit user click. Tests exercise actual App/session/client flow,
cancellation after headers/body parsing, close/sign-out privacy and PATCH401.

## Actual verification

Private evidence is `.local/phase4-acceptance/04-08/` (directories0700,
files0600; owned executable binaries0700). Every command JSON retains exact
argv/cwd, source SHA, working changes, UTC start/end, monotonic duration and
actual exit. No credential/token/header or resolved Compose configuration is
printed. Child build/test umask is022.

Commands run through private `04-08/run.py`, which imports only the approved
NEW context at `04-07/fresh-stack/run-c2ba399-01/context.py`. Native cwd is
backend; exact selector command is:

```text
bash ../scripts/go-test-nonempty.sh ./testphase4/... '^TestPhase4Triage$' -tags=phase4 -race -timeout=6m -p=1
```

| Evidence label | UTC start–end, 2026-10-24 | Monotonic seconds | Exit / observed proof |
| --- | --- | ---: | --- |
| native-red | 16:30:13–16:30:22 | 9.492 | 1; race actual405/405 versus200/409; CH-outage PATCH405 versus200 |
| component-red-final | 16:30:46–16:30:50 | 4.748 | 1; two missing-button failures, ten existing passes |
| native-green | 16:32:18–16:32:27 | 9.047 | 0; first actual migration007/transaction acceptance |
| affected-native | 16:33:46–16:34:12 | 25.183 | 0; separate nonempty Auth, Alerts, Events selectors, phase4/race/6m each |
| default-clean | 16:34:31–16:34:41 | 9.677 | 0; normal make test, backend race +121web +121generator |
| openapi-schema | 16:34:22–16:34:23 | 1.113 | 0; schema/fixture/dedupe and eleven top-level OpenAPI tests |
| quality-final | 16:37:20–16:37:32 | 11.595 | 0; go vet and golangci default, phase4 and integration configurations |
| web-quality | 16:37:27–16:37:34 | 6.719 | 0; ESLint and Prettier |
| web-final | 16:38:50–16:39:04 | 14.341 | 0;121tests, generated types/typecheck, build and lint |
| scans-precommit | 16:39:57–16:39:59 | 2.486 | 0; pinned staged/generated/harness/full-history scans |
| source-head-native | 16:40:57–16:41:10 | 13.222 | 0 at360100e; six subtests, package11.583s |
| source-head-browser | 16:40:58–16:41:12 | 13.752 | 0 at360100e; real current API/UI1440+375 |
| migration-and-health-v2 | 16:41:20–16:41:21 | 0.339 | 0; NEW schema7, audit exists, nine services healthy |

Default `make test` used an environment with runtime SENTINEL_/PG overrides
removed: injecting local DB SSL settings into production-config unit cases is
not a meaningful unit environment. This cleanup changed no configuration file.
Quality used `go vet ./...`, `go vet -tags=phase4 ./...`,
`go vet -tags=integration ./...`, and the corresponding three
`go tool -modfile=tools.mod golangci-lint run` invocations. No expensive full
Phase3 integration aggregate, golden regeneration or budget increase.

Final generated schema SHA256:
`a491e335f0925ba93cbba2972a7904bcf3596bdf826606e4c525a2e9be5d812f`.
Pinned gitleaks8.30.1 scanned staged changes, generated schema, selected private
harness source and full Git history with the existing `.gitleaks.toml` and
`.gitleaksignore`. Final source scan repeated successfully at360100e. No copied
whole-source directory scan, scanner exception or finding suppression was added.

## Browser and runtime context

Final browser proof:
`.local/phase4-acceptance/04-08/browser-1792860058652616653/results.json`.
The harness builds the actual current binary, starts owned ephemeral API/ops
ports with NEW PG/CH/Redis and an independent random signing key, and starts an
owned Vite proxy. Browser requests use the real API; no route mocks. Four
unique fixture alerts are created directly in NEW PG/CH; no07 seed/detection
manifest is resent.

Both1440/375 runs verify real pending state using a short fixture-row PG lock,
acknowledgement/history, stale409/refetch with exactly one mutation per click,
viewer UI hiding plus actual403, foreign404, escaped malicious text, Back,
Escape, exact trigger focus restoration and logout/storage privacy. Geometry
records page widths1440/375 and dialog widths768/375 without page overflow.
Screenshots were inspected at mobile and desktop widths. Focus records include
document.hasFocus, activeElement, native modal state and next-Tab return inside;
Chromium's browser-chrome stop never focuses background page controls.

All owned API/Vite children joined and their ports were verified released.
Shared NEW images were not rebuilt; service18081 still uses its prior image.
The passing08 browser flow used the documented current-source ephemeral API,
not a claim that running shared containers hot-reload source. No producer
remains from08. NEW stack nine-health is green. Old missing Kafka history stays
an independent historical hold; no old stack mutation/reset/stop occurred.

## Deviations and preserved failed attempts

**[Rule3 — setup] Existing NEW infrastructure was stopped after the session
gap.** Root authorized normal `compose start --wait --wait-timeout120` of the
existing four NEW infrastructure services only (16:29:56–16:30:04Z, exit0).
The following app start exited1 for unhealthy ingest during startup
(16:30:12–16:30:25Z). Before/after service IDs were identical. Read-only logs
showed prior dependency DNS failures; a later read-only check at16:32:30Z and
the final check confirmed all nine healthy without a further runtime mutation.
No recreate, pull, build, data reset, retention/group change or prune.

The initial preflight attempts at16:28:13Z/16:28:30Z exited1 and remain setup
failures. RED's missing audit table was likewise a separately labelled setup
failure; causal RED is the real405 assertion and missing UI action above.

All additional failures are retained with their exact argv/times/exits:

- `default` (16:33:37–16:33:44Z, exit2): injected local runtime SSL setting
  invalidated production-config unit cases; `default-clean` passed.
- `quality` (16:34:20–16:34:26Z, exit1): seven new line-length/magic-number
  findings. `quality-fixed` (16:36:28–16:36:38Z, exit1): default clean, eleven
  remaining test line lengths. Formatting/constants fixed within owned files;
  `quality-final` passed all configurations without policy changes.
- `browser` (16:36:27–16:36:32Z, exit1): private CH fixture HTTP400.
  `browser-v2` (16:37:22–16:37:57Z, exit1): fixture lacked canonical payload/
  fingerprint and detail timed out. Fixture construction corrected privately;
  neither is a behavior RED or production success claim.
- `browser-v3` (16:38:10–16:38:15Z, exit1): harness treated native browser-chrome
  focus as background focus. Corrected to require return inside and reject
  background controls; v4 and exact-source-head runs passed.
- `migration-and-health` (16:40:59Z, exit1): psql multi-statement output was
  interpreted incorrectly. A single bounded query in v2 verified version7 and
  actual audit-table access; no migration or runtime repair followed.

No authentication gates or unresolved source failures. No known stubs prevent
this plan's goal. Resolve remains the explicitly assigned09 refinement. No
unplanned threat surface beyond the declared PATCH/audit T04-17/T04-18 scope.
No Phase5 or remote operation was performed.

## Next plan readiness

Root independent review and post-wave gates must assess360100e before accepting
08. Plan09 may add acknowledged→resolved and its logging/history refinements.
Continue with the authorized NEW context; default runtime Make/seed/migrate
flows still point to the old held stack. Shared planning acceptance remains
root owned.

## Self-Check: PASSED

All four created source/test files and this summary exist. RED49d10c4 and
GREEN360100e resolve to commits. Final native/browser proof names360100e;
source union and generated schema hash match the eleven-path ownership.
