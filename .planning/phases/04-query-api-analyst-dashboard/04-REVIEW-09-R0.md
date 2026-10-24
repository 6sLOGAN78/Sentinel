---
phase: 04-query-api-analyst-dashboard
plan: "09"
reviewed: 2026-10-24T17:53:15Z
depth: standard
reviewed_head: bcdfd18974e6e1ccd4f5f3eff98bd13e5f8290eb
source_commit: 68c10c1d9e318b9e6675bcf757bab586cdc811d9
tests_red_commit: 08ee9fd0190dac02618c30ba491c7cb32285cd39
diff_base: ed3d83e6bb4b5cd6bba8f689c252dfdfeae79135
files_reviewed: 10
files_reviewed_list:
  - backend/internal/storage/postgres/triage.go
  - backend/internal/query/triage.go
  - backend/internal/auth/audit.go
  - backend/internal/apikeys/verifier.go
  - backend/internal/ingest/handler.go
  - backend/testphase4/triage_test.go
  - backend/testphase4/auth_test.go
  - web/src/components/AlertDetails.tsx
  - web/src/components/AlertDetails.test.tsx
  - docs/threat-model.md
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 4 Plan 09: Code Review Report

**Reviewed:** 2026-10-24T17:53:15Z
**Depth:** standard
**Files reviewed:** 10
**Status:** clean

## Summary

Independent adversarial review found no concrete BLOCKER or WARNING in the
ten-file resolve/security-event increment. Every scoped file was read, including
the unchanged declared API-key verifier; nine paths actually changed between
the supplied base and GREEN source. Relevant unchanged auth, middleware,
client/session, route, store/replay and migration dependencies were traced.
Reviewed HEAD contains source68c10c1 without further source changes.
Fallow was disabled; no structural pre-pass was supplied.

Context includes the GSD reviewer/workflow instructions, approved09 PLAN,
shared CONTRACT/UI-SPEC,09 SUMMARY,08 SUMMARY/REVIEW-R0 and07 STACK-RECOVERY.
No root AGENTS.md or project .codex/skills/.agents/skills directory exists.
No scoped path is ignored. The explicit ten-path envelope was preserved.

## Narrative Findings (AI reviewer)

No findings.

Direct inspection covered these failure boundaries:

- `backend/internal/storage/postgres/triage.go:19-68`: tenant/ID existence
  check, target-derived required prior status, version/status CAS, increment,
  audit INSERT and COMMIT in one transaction. Both accepted transitions are
  forward only; skip/no-op/reopen/stale requests cannot create another audit.
  SQL values are parameters. The existing store caps queries at five seconds;
  deferred rollback uses an independent one-second context.
- `backend/internal/storage/postgres/triage.go:72-91`: tenant-scoped history,
  ascending version and LIMIT2. Migration007 permits the two legal transition
  pairs; the unchanged replay writer remains insert only and preserves
  workflow version/status/seq and audit history.
- `backend/internal/query/triage.go:29-114`: verified principal supplies actor
  and tenant, analyst/admin authorization precedes parsing, strict bounded
  JSON rejects unknown/duplicate/missing fields and invalid versions/statuses,
  and errors map to sanitized400/403/404/409/413/503 responses. Successful PATCH
  returns committed PG metadata without requiring a CH lookup.
- `backend/internal/auth/audit.go:13-38` and
  `backend/internal/ingest/handler.go:82-114`: fixed action/outcome/reason
  allowlists, verified identity only, zero principal before authentication,
  actual producer credential/scope/tenant outcomes and triage role/outcome
  events. No request body, credential, Authorization, unverified claims or
  arbitrary error is passed to Audit. The inherited request logger adds a
  bounded validated request ID, fixed route/method and direct peer IP.
- `backend/internal/apikeys/verifier.go:65-137`: secret comparison occurs on
  cache hits as well as misses; inactive/unknown/wrong-secret outcomes remain
  fixed unauthorized errors, infrastructure errors remain unavailable, positive
  metadata is copied and cache expiry does not slide on cache hits.
- `web/src/components/AlertDetails.tsx:6-63` and `:113-173`: exact resolve
  title/body/buttons, Return/Escape cancellation, native nested-dialog handling,
  trigger restoration, synchronous mutation-ref duplicate guard and pending
  disabling. Successful workflow state is retained before independent history
  refresh.409 closes confirmation and refetches without automatic PATCH retry;
  failed refresh keeps mutation controls disabled until a successful retry.
- `web/src/components/AlertDetails.tsx:81-110` and `:227-250`: cleanup abort,
  generation-scoped query key, verified role hiding, fresh confirmation after
  conflict, and unchanged client lifetime/caller-signal/post-body guards.
  Session disposal revokes generation before cancellation/cache clearing.
  Workflow, detection, evidence and history render through escaped React text.
- `docs/threat-model.md:104-132`: durable same-transaction successful triage
  history is distinguished from best-effort INFO service events. Output loss
  or a higher configured log level may suppress service events; failed producer
  requests are not claimed to be persisted in PG.

## Independent verification

All checks ran at HEADbcdfd18974e6e1ccd4f5f3eff98bd13e5f8290eb.
The private launcher imports only the approved NEW context at
`.local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/context.py`.
Children use umask022; review evidence is in
`.local/phase4-acceptance/04-09-review/` with directory0700/files0600.

| Exact child command / scope | UTC interval, 2026-10-24 | Actual result |
| --- | --- | --- |
| Explicit NEW `docker compose ... ps -a`, cwd repository | 17:52:19.891368–17:52:20.086903 | Exit0; nine healthy services;11,680,956,416 available bytes |
| `bash ../scripts/go-test-nonempty.sh ./testphase4/... '^TestPhase4Triage$' -tags=phase4 -race -timeout=6m -p=1`, cwd backend | 17:52:29.035714–17:52:43.310172 | Exit0;14.274465s; all nine subtests, package12.157s |
| `bash ../scripts/go-test-nonempty.sh ./testphase4/... '^TestPhase4Auth$' -tags=phase4 -race -timeout=6m -p=1`, cwd backend | 17:52:29.061989–17:52:48.106260 | Exit0;19.044274s; full nonempty Auth family, package16.965s |
| `./node_modules/.bin/vitest run src/components/AlertDetails.test.tsx`, cwd web, runtime SENTINEL_/PG overrides removed | 17:52:29.054631–17:52:32.991622 | Exit0;3.936988s; one file,20/20 tests |
| `git diff --check ed3d83e..68c10c1 --` followed by the ten exact scoped paths | Review interval | Exit0; no whitespace errors |
| Explicit NEW `docker compose ... ps -a`, cwd repository | 17:53:15.657720–17:53:15.810476 | Exit0;0.152751s; nine healthy services;11,143,147,520 available bytes |

Exact argv/cwd/sourceSHA/working changes/UTC start/end/monotonic duration/exits
and redacted outputs are retained in timestamped private JSON. Compose argv
uses project `sentinel-p4-demo-20261024`, absolute base Compose first, approved
private overlay second and absolute root .env via --env-file. No resolved
Compose configuration or credential material was printed.

Native checks build and run actual current query-api/ingest children against
NEW PG25432/CH28123+29000/Redis26379/Kafka39092. They independently exercise both
transition races, audit-insert-fault rollback, two-row history/replay preservation,
tenant/viewer/body guards, bounded locks/cancellation, and positive resolution
with fixture-specific unreachable CH. Auth captures actual process security
events for producer401/scope403 and verified-role403, checking submitted
credentials/body material are absent. No shared service was stopped to model
a dependency outage.

Passing checks support the inspected paths; they are not proof against every
possible defect. Browser acceptance was not repeated here: the09 SUMMARY's
current-source ephemeral API/browser1440/375 proof remains its own evidence.
Shared query18081/ingest18080 images still predate09. No full Phase3 aggregate,
full web/scanner/quality gate or whole-phase acceptance is claimed by this
source review.

The09 SUMMARY's historical launcher setup, future-iat clock-aging, component
focus-fixture, quality, browser navigation and psql-harness failures remain
recorded failures; this clean review does not relabel them. The old stack's
missing Kafka history and original resource hold remain separate preserved
history. No old-stack operation was performed.

Only assigned review reports and ignored private review harness/evidence were
written. No source, shared tracking, configuration, scanner-policy, runtime
configuration or user-dirty .planning/config.json/.serena changes were made.
No shared service stop/rebuild/reset, seed resend, prune, branch switch,
remote operation or commit occurred. Reports are uncommitted for root review.
This verdict covers09 source only and makes no implementation or phase-completion
claim.

---

_Reviewer: gsd-code-reviewer_
_Depth: standard_
