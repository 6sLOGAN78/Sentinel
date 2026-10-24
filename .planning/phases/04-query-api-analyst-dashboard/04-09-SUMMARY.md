---
phase: 04-query-api-analyst-dashboard
plan: "09"
status: complete
subsystem: triage
tags: [postgres, audit, zerolog, react, authorization]
requires:
  - phase: 04-08
    provides: Atomic acknowledgement, migration007, guarded transition client and native acceptance
provides:
  - Forward-only acknowledged-to-resolved transaction with atomic audit
  - Exact resolve confirmation and conflict review without automatic mutation retry
  - Secret-free structured producer and triage security events
affects: [04-15, 04-16]
tech-stack:
  added: []
  patterns: [same-transaction CAS audit, allowlisted security events, native modal confirmation]
key-files:
  created:
    - backend/internal/auth/audit.go
  modified:
    - backend/internal/storage/postgres/triage.go
    - backend/internal/query/triage.go
    - backend/internal/ingest/handler.go
    - backend/testphase4/triage_test.go
    - backend/testphase4/auth_test.go
    - web/src/components/AlertDetails.tsx
    - web/src/components/AlertDetails.test.tsx
    - docs/threat-model.md
key-decisions:
  - Reuse migration007 and the existing generic client without API or schema changes
  - Emit service security outcomes through the existing request logger; only committed triage has durable PG audit
  - Use current-source ephemeral query and ingest binaries with the approved NEW runtime
requirements-completed: [API-05, API-04, AUTH-02, AUTH-04, UI-02, UI-06]
duration: 37 min
completed: 2026-10-24
---

# Phase 4 Plan 9: Resolve and security audit Summary

**Acknowledged alerts resolve through one PostgreSQL CAS/audit transaction, with explicit browser confirmation and secret-free producer/triage security events.**

Two implementation tasks complete. Tests-only RED `08ee9fd`; GREEN
`68c10c1`. Final native Triage, Auth and browser proof ran against exact
committed source `68c10c1`. Behavioral evidence began
2026-10-24T16:59:33Z; committed-source proof ended17:36:19Z. Root independent
review, tracking updates and whole-phase acceptance remain pending. This
summary does not claim root acceptance or completion of Phase4.

Root accepted09 on2026-10-24 after clean independent STANDARD R0 atbcdfd18/
source68c10c1 across all10declared paths (9changed), zero findings. Reviewer
independently passed native Triage9, full Auth and component20. Preserved report:
04-REVIEW-09-R0. Root backend/web builds and normal make test passed EXIT0/
20.228s ending17:56:54.725780Z, cachedGo/web125/generator121, private
04-07/root-09-postwave.log. Source matches reviewed68c10c1. Phase4 is10/17
accepted; continue10 read-only startup Rules. Fullphase acceptance remains open.

## Task commits and ownership

| Task | Commit | Result |
| --- | --- | --- |
| 1. Pin behavior and causal RED | `08ee9fd` | Actual resolve409 versus expected200, missing process security events, absent Resolve button |
| 2. Deliver resolve and redacted audit | `68c10c1` | Forward CAS/audit, structured service events, confirmation UI and native/component regressions |

The source union is nine paths, all within the ten declared paths above.
`backend/internal/apikeys/verifier.go` needed no change: the existing verifier
already returns fixed unauthorized/unavailable errors and verified metadata;
the ingest boundary now logs its actual result. No eleventh source path,
generated-contract change, dependency, schema change or source deletion.
The summary is the only additional tracked executor artifact. User-owned
`.planning/config.json` and `.serena/` remain untouched and unstaged.
STATE/ROADMAP/REQUIREMENTS/VALIDATION and reviews remain root owned; no SDK
tracking mutation was performed.

## Delivered behavior

The PG store selects the required prior status from the two accepted targets:
open→acknowledged and acknowledged→resolved. Its conditional UPDATE retains
tenant/ID/version/prior-status predicates, increments version exactly once and
inserts the matching audit row before COMMIT. Skip, reopen, no-op and stale
requests cannot add another row. Actor comes from verified identity and time
from the server; five-second query and independent one-second rollback limits
remain unchanged. History remains tenant scoped, version ordered and at most
two entries. Positive PATCH returns committed PG metadata without CH enrichment.
Existing migration007 already permits both transitions and remains schema7.

Resolve requires the approved exact title/body/buttons. Return and Escape
cancel without PATCH and restore trigger focus. Back disposes the dialog and
aborts pending requests. Pending controls cannot duplicate submission.
Success updates workflow and refreshes history;409 closes confirmation,
refetches latest data and requires a fresh explicit action. Viewer controls
are hidden and server authorization is independently enforced. The existing
private generated transport retains generation/abort/body guards and401
session disposal. History, actor and canonical payloads render as text.

`auth.Audit` uses the existing request-scoped logger and inherited request ID,
with allowlisted action/outcome/reason fields. Query PATCH emits verified
actor/tenant/role for role denials and mutation outcomes. Ingest emits actual
invalid/unavailable producer credential outcomes plus scope/tenant denials;
only verified producer metadata supplies actor/tenant after authentication.
No credential, Authorization, body, unverified claim or arbitrary error is
passed into this API. Native tests capture actual query and ingest child
output and check the submitted JWT, key, secret fragment, Authorization and
body marker are absent while fixed structured fields are present. The wrong
key case keeps valid syntax and changes the actual secret.

Service events are best effort INFO logs, not durable PG receipts. Log output
loss or a higher configured level may suppress them; no delivery guarantee is
claimed. Failed producer requests are not persisted in PG. Only successful
triage transitions have durable same-transaction PG audit. Logging does not
change authorization or allow a positive commit to be rolled back afterward.
These boundaries and failure handling are documented in the threat model.

## Actual verification

Private evidence: `.local/phase4-acceptance/04-09/`, directories0700,
files0600, owned binaries0700. Child builds/tests use022. Command JSON records
exact argv/cwd, actual source SHA and working changes, UTC start/end,
monotonic duration and exit. Precommit checks truthfully record RED HEAD plus
uncommitted GREEN changes; committed-source checks record68c10c1.

The private launcher imports only the approved context at
`04-07/fresh-stack/run-c2ba399-01/context.py`. Each native family runs separately
from backend with the exact command below, substituting its family name:

```text
bash ../scripts/go-test-nonempty.sh ./testphase4/... '^TestPhase4Triage$' -tags=phase4 -race -timeout=6m -p=1
```

| Evidence label | UTC start–end, 2026-10-24 | Monotonic seconds | Exit / observed result |
| --- | --- | ---: | --- |
| native-triage-red | 16:59:33–16:59:49 | 15.734 | 1; resolve409 versus200, missing security event |
| native-triage-red-final | 17:00:06–17:00:19 | 12.336 | 1; additional real resolve race/fault/CH cases fail absent behavior |
| native-auth-red | 16:59:34–16:59:56 | 21.406 | 1; actual role403/producer401 lack security events |
| component-red | 16:59:35–16:59:44 | 8.448 | 1; four new failures, prior16 pass |
| native-triage-green | 17:14:24–17:15:57 | 93.193 | 0; all9subtests |
| component-green-v2 | 17:15:40–17:15:51 | 11.130 | 0;20tests |
| native-auth-green-v2 | 17:16:55–17:17:24 | 28.947 | 0; full Auth family including actual producer/log assertions |
| default-green | 17:16:58–17:18:33 | 95.453 | 0; normal make test/backend race+125web+121generator |
| web-quality-v2 | 17:23:36–17:23:55 | 18.742 | 0; typecheck/lint/format/build |
| browser-green-v2 | 17:23:37–17:23:55 | 18.212 | 0; current-source real API/UI1440+375 |
| affected-native | 17:33:59–17:34:26 | 26.282 | 0; separate nonempty Alerts/Events race selectors |
| focused-regressions | 17:34:07–17:34:13 | 5.853 | 0; apikeys/ingest race and OpenAPI/schema/fixture/dedupe tests |
| backend-quality-final | 17:34:47–17:35:12 | 24.767 | 0; default+phase4+integration vet, lint and build |
| native-auth-final-worktree | 17:35:23–17:35:41 | 17.372 | 0; strengthened real wrong-secret/body-marker capture |
| scans-precommit | 17:35:32–17:35:34 | 2.715 | 0; pinned staged/generated/harness/history |
| source-head-triage | 17:35:56–17:36:11 | 14.671 | 0 at68c10c1;9native subtests |
| source-head-auth | 17:35:57–17:36:19 | 21.831 | 0 at68c10c1; actual query/ingest process capture |
| source-head-browser | 17:35:58–17:36:15 | 17.218 | 0 at68c10c1; real current API/UI1440+375 |

Normal make test deliberately removes NEW runtime SENTINEL_/PG overrides;
local DB SSL settings must not contaminate production-config unit cases.
Quality uses `go vet ./...` and phase4/integration variants, matching three
`go tool -modfile=tools.mod golangci-lint run` configurations and three builds.
No full Phase3 aggregate, deadline increase, golden regeneration or scanner
policy/exception change. Pinned gitleaks8.30.1 scans staged, generated schema,
selected private harness code and full history with the unchanged config and
ignore file. Source-HEAD scan passed again; metadata scan follows this commit.

Native Triage proves both actual concurrent transition races, faulted audit
INSERT rollback, exactly two history rows, verified actor/server time, replay
preservation, stale/no-op/reopen/skip refusal, tenant/viewer constraints, strict
request parsing, lock/cancellation bounds and successful resolution with a
fixture-specific unreachable CH address. Shared services were never stopped.

## Browser and runtime evidence

Final results:
`.local/phase4-acceptance/04-09/browser-1792863358802899472/results.json`.
The harness builds actual current query source and owns an ephemeral API,
ops port and Vite proxy. Four random NEW PG/CH fixture alerts are used;07 seed
and detection manifests are not resent. Browser requests have no route mocks.
Browser plugin unavailable; existing cached Playwright/Chromium is used without
dependency installation. Real current ingest is independently exercised in Auth.

At1440/375, proof includes exact confirmation, Return/Escape/Back with no PATCH,
real pending state held by a short fixture-row PG lock, disabled pending submit,
resolve/version3/two history changes, conflict/refetch with one PATCH per user
action, viewer hidden plus actual403, foreign404, safe DOM, native nested-modal
keyboard containment/focus, Back and logout/storage privacy. Confirmation and
resolved screenshots were retained; confirmation1440/375 was inspected.
Page width equals viewport1440/375; detail widths768/375 without overflow.
Native Chromium browser-chrome Tab stops retain actual document.hasFocus and
BODY observations; the next Tab returns inside and no background control gets
focus. No production focus workaround was added for headless observations.

All owned API/Vite children joined and successful-browser ports were verified
released. Final read-only check proves NEW nine healthy services, schema7 and
audit table. No09producer remains. Shared query18081/ingest18080 remain prior07
images; passing09proof uses current ephemeral binaries. No image rebuild,
shared stop/reset, data/volume/offset operation, cache prune or remote operation.
The original old stack remains untouched. Runtime final evidence is
`final-runtime-v2.json`; disk stayed above the2GiB floor (12.46GB recorded at
the final precommit health check).

## Deviations and retained failures

1. **[Rule 1 - Test fixture] Future-issued credential used an aging setup clock.**
   First Auth GREEN exit1 at17:16:04: owned logging cases passed, but existing
   reject_future_iat reached the30s verifier leeway after real binary builds.
   Generate its future iat from submission time; verifier/leeway unchanged.
   Full corrected Auth and committed-source Auth pass. Included in68c10c1.
2. **[Rule 3 - Verification] New test helper/style corrections.** Initial web
   quality exit2 found unsupported Testing Library `exact` option; remove it
   (string names already exact). Backend quality exit1 first found one source
   line length; next tagged pass exposed new-test complexity/shadow/line/context
   conventions. Extract the producer helper and wrap lines; all three final
   quality configurations pass. Only owned paths changed, included in68c10c1.

Other failed attempts remain explicitly separate from behavioral RED:

- Initial component GREEN exit1/19of20: fireEvent click did not focus its
  trigger; model a focused user trigger as existing tests do. Component-v2
  passes20/20. No production focus change.
- Browser first attempt exit1 timed out after Back/Forward crossed the initial
  hard-navigation deep link. Memory-only identity correctly requires sign-in
  after returning to a fresh document; adapt the private harness. Browser-v2
  and committed-source browser both pass.
- First final-runtime harness exit1: multiple SQL statements in psql `-c`
  returned only the last result. One SELECT returning version and table proves
  both in v2 exit0; no database/source change.
- Initial preflight put launcher options after its remainder label, raising
  FileNotFoundError before a child started. Corrected options-first preflight
  returned0/nine healthy. Original launcher made no JSON/timestamps for the
  failed Popen; `setup-failures.json` records that evidence limit without
  inventing UTC values. A rejected apply_patch was atomic and left no edits.

No remaining known stubs or additional threat surface beyond the plan's
transition/security-log boundaries. No authentication gate, package install,
architecture expansion or deferred implementation issue. Root independent
review and final phase acceptance remain required; the next numbered slice
is read-only rules, not Phase5/System/consumer-lag work.

## Self-Check: PASSED

All nine source paths and this summary exist. Commits08ee9fd and68c10c1 exist,
with tests-only RED before GREEN. Final native and browser records identify
68c10c1 and actual exit0. No source deletions or uncommitted09source remain.
User dirt is preserved; root tracking/review artifacts were not edited.
