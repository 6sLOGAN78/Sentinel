---
phase: 04-query-api-analyst-dashboard
reviewed: 2026-10-23T04:06:43Z
depth: standard
review_slice: 04-05
review_round: R0
review_sha: 1b033661baab712f38c0ff6e517733c50a84f04a
source_sha: 01ca62a52523869d1abd8fdd0e2609a671f911a8
diff_base: fdd3938
files_reviewed: 12
files_reviewed_list:
  - api/openapi.yaml
  - backend/cmd/query-api/main.go
  - backend/internal/query/alerts.go
  - backend/internal/query/types.go
  - backend/internal/storage/clickhouse/query_alerts.go
  - backend/internal/storage/postgres/query_alerts.go
  - backend/testphase4/alerts_test.go
  - web/src/api/client.ts
  - web/src/api/schema.d.ts
  - web/src/components/AlertDetails.test.tsx
  - web/src/components/AlertDetails.tsx
  - web/src/pages/Alerts.tsx
generated_schema_sha256: 8b49a4adf11528b42ddce1b0eb40bd52f5667132652ae9be0b3ad24efabe21aa
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 4 Plan 05: Code Review Report

**Reviewed:** 2026-10-23T04:06:43Z

**Depth:** standard, full explicit twelve-path scope.

**Status:** clean — no actionable BLOCKER or WARNING established in this slice.

## Narrative Findings (AI reviewer)

### Summary and scope

Read all twelve submitted files in full, including the explicitly authorized ignored generated declaration, and inspected their diff from accepted 04-04 baseline fdd3938. This is independent adversarial source review at frozen HEAD 1b033661baab712f38c0ff6e517733c50a84f04a of implementation source 01ca62a52523869d1abd8fdd0e2609a671f911a8. No reportable bug, security vulnerability or robustness defect was established. This conclusion applies to the delivered detail slice, not whole-Phase-4 acceptance or a security certification.

Read the complete 04-05 PLAN, approved OWNERSHIP-A1 and A1-CHECK, shared CONTRACT, approved UI-SPEC and SUMMARY. The SUMMARY supplied intended behavior, not evidence of source correctness. No root AGENTS.md, .codexignore, project .codex/skills or .agents/skills directory was present. Applied the available GSD code-review and Karpathy instructions. No structural/fallow pre-pass was supplied or performed.

Relevant actual dependencies inspected include CH canonical AlertStore.Get and EventStore.Get, CH query settings, PG workflow storage and migrations 003/004/005, detector manifest binding/adoption, detection Rules/Fingerprint, canonical Alert/Event decoding and validation, writer validation, existing event handler, SessionProvider and App mounting. Earlier mapping/stack descriptions were not treated as current facts.

### Boundary analysis

- **Ownership and SQL:** query/alerts.go:115–144 obtains the verified principal and validates the path before storage. PG query_alerts.go:67–85 directly binds both tenant and alert ID under the inherited timeout. CH follows successful ownership; unknown/foreign/orphan workflows gain no authority from CH. Queries use bound parameters. Dependency errors map to public 503; missing ownership/detection maps to 404. The existing PG list remains independent of CH and preserves its fifteen-field DTO, filtering, millisecond ordering and cursor handling.
- **Canonical detection and evidence:** CH query_alerts.go:20–33 composes the tenant-scoped immutable read, fingerprint/conflict checks and canonical validation. Evidence at :39–65 caps recorded IDs at twenty, deduplicates before reading and shares one deadline. Each composed EventStore.Get has its own tenant-and-ID predicate and LIMIT 1; duplicate physical rows cannot consume another ID's result slot. Missing IDs remain explicit and unique in recorded order. Storage failures propagate instead of producing successful empty evidence. Existing event writer validation remains the write boundary.
- **Recorded rules:** PG query_alerts.go:90–137 binds tenant, alert, rule and exact recorded version to the immutable decision's group/configuration fingerprint. It never substitutes startup parameters. The query returns at most thirty-three rows, using the last as an overflow sentinel; decoded manifests are bounded to 8192 bytes with schema/count/fingerprint checks and exact candidate agreement. Missing/null/invalid/overflowing/conflicting data yields explicit unavailability. Mutable fence binding/adoption and fingerprint semantics were checked: a retained matching definition supports the explanation without implying an immutable historical archive. SQL/transport errors still fail detail.
- **DTO and generated types:** OpenAPI detail and Go JSON fields agree: workflow, canonical detection, evidence, missing_evidence_ids, rule_explanation and history. Canonical Event/Alert remain separate. Evidence/missing IDs are capped at twenty and future forward history at two. Empty current history is the approved 08/09 refinement, not an invented audit record or a defect in 05. Future PATCH/rules/events/stream contracts are not treated as delivered 05 behavior.
- **Client lifetime:** client.ts:72–87 uses the existing generated authenticated client. It checks caller cancellation before dispatch and after parsing and checks generation/lifetime after parsing. Shared fetch combines caller/session signals, checks authority before credentials and after headers, and retains current-session 401 revocation. SessionProvider invalidates generation before abort/cache clearing; App keys the subtree by generation. Detail cache keys include verified subject, tenant, role, generation and selected ID. No credential enters public context, URLs, persistent storage or logs.
- **Dialog, history and privacy:** AlertDetails uses native showModal, explicit initial close focus, cancellation, cleanup and connected-trigger restoration/fallback. JSON and identifiers render as escaped React text with wrapping. Alerts adds selection to its filter URL without forwarding alert_id to the list API. Selection state/ref, popstate handling and owner markers preserve StrictMode replay, Back/Forward and session isolation; cleanup removes the current cursor/selection. Existing UTC minute conversion, filter keys, previous-page bounds and draft/session behavior remain intact. The jsdom open/close shim does not prove native browser focus containment; no native-dialog defect was inferred from that shim.
- **Test reliability:** native additions exercise actual binary wiring and PG/CH fixtures for evidence, tenant/orphan, outages and retained rules. Component assertions cover actual App/provider/client behavior through fetch-only responses, malicious text escaping, retention, loading/error/retry, deep links, StrictMode, focus restoration, cancellation and stale parsed bodies. No new reliability defect was established. Passing tests did not substitute for tracing the source.

### Independent diagnostics and limits

Reviewer-run checks on the frozen source:

| Command | Actual result |
|---|---|
| `go test -race -timeout=60s ./internal/query/...` from backend | Exit 0, cached; no claim of fresh uncached execution. |
| `npm test -- --run src/components/AlertDetails.test.tsx` from web | Exit 0, 9/9 focused component cases. Existing pretest regenerated schema.d.ts; before/after SHA-256 is exactly the declared 8b49a4ad…21aa. This was an idempotent generated-file rewrite, not a source-content change. |
| `git diff --check` | Exit 0 before writing this report. |

Executor native Auth/Alerts, full web/default/static/OpenAPI/browser and scanner results are recorded in SUMMARY and private evidence. They were not rerun or independently re-certified here. Earlier failed runs remain historical failures; no passing result is assigned retroactively. No native fixture, server, browser, migration, scanner, external service or remote operation was started by this reviewer. No new screenshot inspection or whole-suite acceptance is claimed.

Standard-depth source analysis and these focused checks do not establish cross-browser/accessibility certification, screen-reader or 200%-zoom behavior, future triage/audit/SSE integration, whole-Phase-4 tenant matrix, complete Phase-3 aggregate acceptance, production deployment readiness or performance. Root owns remaining acceptance and tracking.

### Preservation

The prior report remains byte-identical as 04-REVIEW-04-R1.md, SHA-256 16c8b5b5565c4b9adcb1b90a213ffe104dd0766e8b058c49ca37ca0ad3364099. Only this designated report has new content. No production/test content, Git index, HEAD, commit, STATE, SDK tracking, user .planning/config.json or .serena change was made. The explicitly included generated declaration remains ignored and byte-identical; it was not force-added. No later slice or Phase 5 advancement is authorized by this report.

---

_Reviewer: gsd-code-reviewer_

_Depth: standard; all twelve explicit source paths read in full_
