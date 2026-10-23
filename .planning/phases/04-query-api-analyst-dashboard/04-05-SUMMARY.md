---
phase: 04-query-api-analyst-dashboard
plan: "05"
status: complete
subsystem: api
tags: [postgres, clickhouse, evidence, react-query, dialog, openapi]
requires:
  - phase: 04-04
    provides: Filtered tenant workflow pages, authenticated cursors and canonical UTC inputs
provides:
  - PG ownership followed by canonical CH detection and bounded unique retained evidence
  - Explicit missing sample IDs and exact-version retained-rule explanation or unavailable state
  - Guarded typed detail requests and native dialog with session-safe deep-link history
affects: [04-06, 04-08, 04-09, 04-15, 04-16]
tech-stack:
  added: []
  patterns: [PG-first-detail-authorization, bounded-retained-manifests, native-modal-dialog]
key-files:
  created:
    - backend/internal/query/types.go
    - backend/internal/storage/clickhouse/query_alerts.go
    - web/src/components/AlertDetails.tsx
    - web/src/components/AlertDetails.test.tsx
  modified:
    - backend/internal/query/alerts.go
    - backend/internal/storage/postgres/query_alerts.go
    - backend/cmd/query-api/main.go
    - backend/testphase4/alerts_test.go
    - api/openapi.yaml
    - web/src/api/schema.d.ts
    - web/src/api/client.ts
    - web/src/pages/Alerts.tsx
key-decisions:
  - "Establish verified-tenant PG workflow ownership before every detail's CH access; keep PG list independent of CH."
  - "Use at most twenty distinct sample-ID reads under one five-second detail deadline; report missing IDs explicitly."
  - "Explain only an exact recorded-version definition from complete bounded decision-bound retained manifests; absent, conflicting or overflowing candidates are unavailable."
  - "Preserve selected detail state in a ref during StrictMode effect replay, matching the existing filter-state restoration."
requirements-completed: [API-04, API-08, AUTH-03, UI-02, UI-06]
duration: approximately 33min implementation/evidence span, excluding the earlier ownership approval pause
completed: 2026-10-23
---

# Phase 4 Plan 05: Tenant alert evidence details Summary

Verified-tenant PG ownership now precedes immutable CH detection and bounded evidence reads; the analyst dialog shows missing retained IDs and honest recorded-rule explanations through the existing guarded typed client.

**ACCEPTED BY ROOT.** Source is `01ca62a52523869d1abd8fdd0e2609a671f911a8`. Independent standard review at frozen HEAD `1b033661` covered all twelve paths with zero findings; preserved report is `04-REVIEW-05-R0.md`. Root separately ran backend/web builds and normal `make test` under the unchanged300s post-wave outer budget: actual EXIT0/12.571s, END2026-10-23T04:09:03.893731Z, private `root-postwave.log` (cached Go results, web64 and generator121). Phase4 is now6/17 accepted. Requirement IDs above are incremental plan coverage, not whole-requirement or whole-phase acceptance. At this acceptance, no04-06, Phase5, push or hosted CI was performed. Earlier failures remain recorded below, not retrospectively relabelled.

## Ownership and provenance

Execution initially stopped before edits because production dependency injection, direct PG ownership lookup and the guarded typed client were outside the original nine paths. Independently approved A1 expands ownership to exactly twelve paths: eleven tracked source paths and the existing ignored generated declaration. No thirteenth source path, dependency, framework, migration, index, rule registry or CI/configuration change was introduced.

The implementation/evidence span starts around 03:28 UTC October23 and ends with source verification/commit around 04:00; private wrappers preserve precise command timestamps, elapsed durations, actual exits, HEAD and working changes. This is elapsed session provenance, not active-work or performance evidence. Runs before the source commit identify a tests-only/documentation HEAD with the working implementation, never an invented already-committed GREEN. Root's intermediate checkpoint commit `09d772d` and root-owned STATE edits are separate from executor changes.

User-owned `.planning/config.json` and `.serena/`, and root-owned `.planning/STATE.md`, remain untouched and unstaged by this executor. No tracked deletion occurred. Native tests leave uniquely scoped synthetic fixture rows intact. No reset, prune, teardown, truncate, OPTIMIZE, data deletion or volume operation occurred. The real browser harness removed only its own temporary built binary/directory and joined its API/Vite/browser processes.

## Task commits and causal RED

1. `942256b` — tests-only Task1: extend the existing actual HTTP/PG Alerts family with canonical CH fixtures, evidence/tenant/outage/version cases; add actual-App component specifications without importing a nonexistent detail component.
2. `ab98360` — tests-only refinement: causal StrictMode selected-URL regression plus duplicate sample/missing canonical and direct post-body cancellation cases; local test-harness corrections remain explicit below.
3. `01ca62a` — Task2 source: PG-first detail handler, bounded PG/CH adapters, actual main wiring, OpenAPI/generated boundary, guarded client method and native dialog/deep-link UI.

The first native RED (`native-red`) executed real migrations, inserted real PG/CH data and built/started query-api successfully. Own detail expected200 but returned404; CH-down own detail expected503 but returned404; invalid ID expected400 but returned404. Earlier filter/cursor/precision tests passed. `native-final-red` repeats the extended expected failure separately, exit1. Foreign/unknown404 cases already correct by unmatched routing are not falsely called per-case RED.

`web-behavior-red` exits1 on absent detail buttons and absent deep-link dialog through the actual App with fetch-only responses. The first `web-red` instead failed because jsdom lacks showModal; that is setup failure, not causal RED. The local shim models only open/close, not native top-layer focus containment or Escape defaults.

The actual browser then found a distinct implementation defect: StrictMode cleanup removed alert_id from a fresh detail deep link while state still displayed the dialog. `web-strict-red` expected the64-character selected ID but received null, exit1, before the minimal owned page repair. The selected ref now restores that state during effect replay, analogously to the existing filters ref. Other working Task2 source already existed during this targeted RED; no clean-history or per-case original RED is manufactured.

## Delivered interfaces and behavior

- `query.Dependencies` adds injected `Workflows WorkflowReader` and `Details AlertDetailReader`; actual main supplies the existing PG adapter and new CH query adapter. `GET /api/v1/alerts/:alert_id` uses the existing verified-auth middleware and obtains its tenant before parsing/storage. Lowercase64hex IDs are required, and any query fields are rejected. The direct PG query binds both tenant and ID before any CH read; unknown/foreign workflows, including CH-only orphan detections, return404. An owned workflow with no canonical CH detection also returns404. Storage failures return bounded public503 errors.
- `AlertDetail` contains `workflow`, `detection`, `evidence`, `missing_evidence_ids`, `rule_explanation`, `history`. Workflow remains exactly the actual fifteen PG003 public fields; canonical `contracts.Alert` is fetched only from tenant-scoped CH and preserves its dedupe key and sample IDs. Existing canonical conflict/fingerprint decoding and full Alert validation remain authoritative. The PG list never fills canonical fields or calls CH.
- A five-second caller-derived deadline covers the entire detail flow, including PG and CH work. Evidence deduplicates the canonical sample IDs before reading, rejects more than20 IDs, and composes at most20 existing parameterized tenant-and-ID event reads under one shared deadline/settings context. Each returns one retained representation, independent of physical duplicates/background merges. Missing IDs are unique, in recorded order; empty arrays are actual wired results, not failure fallbacks. Dependency failures cannot become successful empty evidence.
- Rule explanation joins the verified tenant's actual detection decision to retained fence manifests for that decision's group/config fingerprint. Those mutable manifests are not a historical archive. Read count is bounded to32 plus one overflow sentinel, each payload to8192bytes, rule count to4 and query time to the existing timeout. Every candidate must match its recorded fingerprint and exact rule ID/version and agree; absent/null/invalid/conflicting/incomplete data returns the explicit unavailable explanation retaining recorded ID/version. More than32 candidates is unavailable, never a uniqueness claim based on truncation. Current startup thresholds are never substituted. SQL/transport failures remain errors rather than invented rule facts.
- OpenAPI replaces the obsolete detail envelope with the contract above, caps evidence/missing IDs at20 and history at2, and leaves canonical Event/Alert definitions unchanged. Existing route/security/drift/strict fixture tests pass. Future PATCH implementation remains owned by08/09. Generated declaration is ignored and not force-added; SHA256 is `8b49a4adf11528b42ddce1b0eb40bd52f5667132652ae9be0b3ad24efabe21aa`.
- `client.alertDetail(id, signal)` uses generated path/response types through the existing authenticated client. It preserves credential callbacks, generation/lifetime checks before network and after body parsing, session/caller cancellation and current-session401 revocation. No standalone token fetch, URL credential, public credential context or persistent storage was introduced.
- Alerts supplies details buttons in both desktop table and mobile list. Only the current Alerts page changes; filters/query keys/cursor history,300ms search, strict canonical UTC minute edits and prior session/draft behavior remain covered. Detail cache keys include verified subject/tenant/role/generation and selected ID. The native dialog uses showModal, labelled heading/close, initial close focus, native cancellation and trigger restoration. alert_id deep links, Back/Forward, reload followed by fresh sign-in and effect replay work; logout removes the current selected URL state and old ownership markers cannot grant another session authority.
- The dialog implements loading, dependency error/retry, last-real-data retention during refresh failure, empty evidence/history and populated states. Rule unavailability and missing retention are explicit. Canonical/event JSON uses escaped React text/pre rendering with long-value wrapping; malicious HTML never creates executable elements. No triage controls or fabricated audit records were added.

## Verification evidence

All evidence is ignored under `.local/phase4-acceptance/04-05/`, directory0700/files0600; child builds/tests use umask022. Logs do not print keys, bearer values, Authorization captures or resolved Compose configuration. Each failed chain's unexecuted later commands remain unclaimed.

| Evidence (October23 UTC) | Start–end | Actual result |
|---|---|---|
| `native-final-red` | 03:32:48–03:32:57 | Exit1; actual missing detail HTTP behavior; prior filters/cursors pass |
| `web-behavior-red` | 03:31:49–03:31:52 | Exit1; actual-App missing controls/deep-link behavior |
| `web-strict-red` | 03:51:14–03:51:16 | Exit1; selected deep-link ID expected64hex, actualnull during effect replay |
| `native-final` | 03:49:45–03:50:07 | Exit0,21.065s; independent exact Alerts and Auth phase4/race/6m selectors, retaining Auth26 top-level and24 CLI cases |
| `static-final` | 03:49:59–03:50:11 | Exit0,12.399s; query race60s (cached), default/phase4/integration vet, build and all three lint modes0issues, each outer120s |
| `web-final-green` | 03:51:35–03:51:49 | Exit0,13.417s;64/64 including all retained55, plus typecheck/lint/build/format, each120s |
| `openapi-final` | 03:52:24–03:52:26 | Exit0,2.707s; exact nonempty11OpenAPI/race/60s, canonical drift and strict fixtures |
| `browser-final` | 03:51:48–03:51:56 | Exit0,7.539s; current race-built API, actual helper/CLI, PG+CH, cached Chromium and private Vite runtime proxy at1440/375; no mocked routes |
| `default-final` | 03:52:30–03:53:21 | Exit0,50.589s; standard make test with authorized300s outer bound: default Go race (detection uncached42.501s), web64 and locked generator121 |
| `stack-final` | 03:53:47–03:54:02 | Exit0,14.768s; preserved dependency/application readiness and native probes, no startup mutation needed |
| `staged-source-scan` | 03:54:06–03:54:07 | Exit0; pinned gitleaks8.30.1 staged production source and ignored generated declaration, both reports exactly[] |
| `source-history-scan` | 03:59:58 | Exit0 at01ca62a; pinned full-history scan/report exactly[]; no scanner exception changed |

Exact native commands are preserved independently and fail closed:

```sh
bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Alerts$' -tags=phase4 -race -timeout=6m -p=1
bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Auth$' -tags=phase4 -race -timeout=6m -p=1
```

Native data proves18 retained unique events from20 canonical sample IDs, with a real duplicate event and foreign-only sample excluded. Additional cases prove duplicate sample IDs yield one event/one missing ID, missing canonical records are not synthesized, own/foreign/orphan/unknown/invalid/auth behavior, PG-first404 during CH outage, own503 during that outage and successful independent PG list without canonical fields. Rule tests prove recorded77 unavailable without retained definition, exact retained threshold13, conflicting manifest unavailable and33-plus candidate overflow unavailable. These are real store/binary tests, not mock-only acceptance. Missing evidence is represented by absent retained IDs rather than a destructive TTL/data-deletion operation.

### Browser scope and diagnostic correction

The final real browser run renders actual18-event evidence,2missing IDs, canonical dedupe data and recorded77 unavailability; a separately minted foreign identity gets404. At1440/375 it verifies initial close focus, keyboard containment, Escape exact trigger restoration, Back/Forward, reload/fresh-sign-in deep link, malicious CH attributes remaining text, no dialog/document overflow, no console/page errors or browser credential persistence, and cleared draft on sign-out. Screenshots and result/tab-stop JSON remain private; the phone screenshot was visually inspected for readable wrapped evidence and a visible focused control.

`browser-first` failed an overly strict assertion requiring every Tab stop to be a dialog descendant. The diagnostic retains the actual sequence: dialog buttons, then BODY with document.hasFocus=false (browser chrome), then DIALOG/buttons with document focus. The final assertion excludes background controls while allowing that native browser-chrome transition. No custom production focus workaround or false initial PASS was introduced. `browser-diagnostic-2` subsequently exposed the real StrictMode defect described above. `browser-diagnostic` failed only because its exclusive log already existed; evidence was preserved and later runs use distinct private directories.

This uses the existing cached Playwright1.64.0-alpha/Chromium1248, no installation/download/project browser dependency. It is focused evidence for this detail slice, not04-15 full proxy/SSE/two-replica/tenant-matrix acceptance, accessibility certification, screen-reader,200%zoom or cross-browser proof.

## Historical failures and recovery

- `static-first` and the no-competing-producer `default-solo-repeat` both actually failed the unchanged default Go race suite's60s timeout while `TestReplayEveryRestartCut/shared_frozen_fixtures/all` was running. These remain FAIL, not waived. The earlier shorthand TestReducerReplay was inaccurate; the logs preserve the actual name. Detection source never changed in this plan.
- Separate `default-correct-bound` was `timeout120s make test`, whose existing inner command is go test -race ./...; it exited124 at120.024s before backend completion, so web/generator did not run. It does not prove the timed-out60s selector. No inner/CI cap was increased.
- Execution checkpointed; root's identical quiet default60s repeat then passed, exit0,43.464s (detection uncached42.562s), ending03:45:32. Source/caps/machine configuration were unchanged. Cause of the earlier slowdown is unestablished; concurrency is not asserted as its explanation. The later standard make test has its separate actual PASS above under root's authorized existing300s post-wave outer budget.
- `native-complete-red` had an import/name-shadowing compile setup failure, corrected before `native-final-red`; it is not behavioral RED. `web-first-green` found JSON-escaping expectations, local shim cleanup order and a delayed-body mock targeting json rather than the installed client's text parsing. Those were test-harness corrections, not invented product defects. `web-final` was62/63 because Node/jsdom DOMException constructors differ; the exact AbortError shape now verifies cancellation across realms. Prior logs remain unchanged.
- `quality-first/second` found owned line-length/constants/builtin-name/context-argument lint issues. Narrow fixes pass the final three lint modes; no lint configuration or unrelated source was changed.

## Deviations from Plan

- **[Rule4 — independently approved A1]** Actual command dependency wiring, direct PG adapter lookup and guarded typed client required three extra paths. Execution stopped before editing them; A1/CHECK approval preceded the amended twelve-path implementation. No further ownership exception was needed.
- **[Rule1 — StrictMode selected URL loss]** Found by actual browser, causally reproduced in owned component tests before the minimal selected-ref repair. Files: owned Alerts page/component tests. REDab98360 precedes source01ca62a.
- **Verification checkpoint:** Repeated unchanged default timeout was reported and preserved; root cleared it with the identical selector. No out-of-scope detector repair or gate relaxation occurred.

Karpathy guidance kept changes narrow and causal; UI/UX guidance supplied the visible-focus/native-control checks and the existing approved UI-SPEC supplied design. Available Playwright instructions and explicit session constraints led to reuse of the cached browser/private harness. Installed openapi-fetch source and official API/HTML dialog references were checked for request/body/modal semantics. No unrelated design cycle, framework or unavailable skill was represented as applied.

## Known Stubs and limits

`backend/internal/query/alerts.go:144` intentionally returns empty history per the approved CONTRACT until04-08/09 implement audit reads. The final DTO/OpenAPI already declares the maximum two forward transitions; no status history is invented. This is the expressly assigned later refinement, not a claim that triage/audit is delivered by05. No other goal-blocking placeholder/TODO/stub was found in the owned source. T04-10/11/12 cover the new detail/evidence/rule trust surfaces; no additional unregistered endpoint/auth/file/schema boundary was introduced.

No benchmark, security certification, full Phase3 aggregate, wholePhase4/native matrix, SSE or hosted CI result is claimed. Independent source review and root post-wave acceptance subsequently passed as recorded above. No shared tracking/SDK writes were performed by this executor; root owns the acceptance updates.

## Self-Check: PASSED

All twelve owned paths exist, including the ignored generated declaration. Commits942256b/ab98360/01ca62a exist in order; causal RED precedes each corresponding source behavior. Scoped whitespace/deletion checks pass; no source changed after the final verified candidate. The source union is exactly eleven tracked paths plus generated schema. Source/generated/full-history scan reports are empty arrays; private modes are0700/0600. Only user/root-owned STATE/config/.serena changes remain outside the metadata commit. This verifies executor claims, not independent source acceptance. The committed-summary full-history scan is recorded separately after this commit.
