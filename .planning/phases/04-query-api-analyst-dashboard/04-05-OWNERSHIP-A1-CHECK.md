---
phase: 04-query-api-analyst-dashboard
plan: "05"
amendment: A1
reviewed_at: "2026-10-23T02:10:21Z"
review_sha: 055f47bbf646ec5845e241c24ff71881cad7f218
accepted_baseline: fdd3938
status: approved
approval_scope: planning-only
findings:
  blocker: 0
  warning: 0
  total: 0
issues: []
---

# 04-05 ownership A1 — independent planning check

APPROVE / PASS. Zero issues. The three additional paths repair concrete missing
connections required for the already planned alert-detail feature. This approves
the explicit twelve-path exception before root amends ownership and Task 2; it
does not approve implementation or waive any acceptance gate.

## Causality and integration

| Additional path | Independently inspected need and approved limit |
|---|---|
| `backend/cmd/query-api/main.go` | Lines 117–120 currently construct the PG adapter and inject only `Alerts`, auth and cursors. The binary cannot reach a new CH detail adapter without explicit wiring. Inject real PG workflow and CH detail dependencies; preserve existing routes, lifecycle and lazy CH startup. |
| `backend/internal/storage/postgres/query_alerts.go` | Its current `ListAlerts` requires a bounded time range and returns pages. Writer `alerts.go:Find` is ID-only and returns tenant/fingerprint, not the public workflow. Add a parameterized tenant-and-ID workflow lookup with the existing bounded timeout. Neither list sweeps nor the writer lookup satisfies this boundary. |
| `web/src/api/client.ts` | The actual client exposes only `session()` and `alerts()`. Add generated-type-backed `alertDetail(id, signal)` through the same credential callback, lifetime/generation checks, authenticated fetch and post-body guard. Preserve individual cancellation and current-session 401 revocation. |

The original owned query handler/types, CH adapter, OpenAPI/generated declarations,
Alerts page and detail component can implement the remaining links. The actual
`AlertLister`/`Dependencies` boundary is list-only; extending it inside the owned
query files keeps store implementations out of handlers. The existing CH alert
store already demonstrates tenant-bound canonical decoding/conflict checks, and
the event store demonstrates native named parameters. No new architecture,
dependency, framework or canonical Event/Alert schema is required.

## Exact scope exception

The approved union is twelve paths, eleven expected tracked paths plus the
existing ignored generated declaration:

1. `backend/internal/query/alerts.go`
2. `backend/internal/query/types.go`
3. `backend/internal/storage/clickhouse/query_alerts.go`
4. `backend/testphase4/alerts_test.go`
5. `api/openapi.yaml`
6. `web/src/api/schema.d.ts` — regenerate; do not force-add
7. `web/src/components/AlertDetails.tsx`
8. `web/src/components/AlertDetails.test.tsx`
9. `web/src/pages/Alerts.tsx`
10. `backend/cmd/query-api/main.go`
11. `backend/internal/storage/postgres/query_alerts.go`
12. `web/src/api/client.ts`

This exceeds the normal small-plan file target but remains below the fifteen-file
blocker threshold. The three additions are narrow adapters/consumers on one
vertical path, not independent feature work. Two existing test/implementation
tasks and the <=45% context target remain binding. This explicit exception does
not permit another path or a larger context/runtime budget; stop at a checkpoint
if the bounded implementation cannot fit. Root must incorporate A1 into the plan,
including replacing Task 2's misleading historical-registry shorthand with the
actual retained-manifest/unavailable contract.

## Security, history and data contracts

Verified tenant -> bounded tenant-and-ID PG workflow lookup -> tenant-scoped CH
canonical detection -> at most twenty deduplicated actual evidence events is the
required order. Unknown/foreign workflow IDs return 404 before CH, including when
an orphan detection exists. Every CH query keeps its own tenant predicate,
parameter binding, cancellation and bounds. Missing retained event IDs are
reported explicitly; dependency failures never become invented evidence.

The fifteen-field PG workflow and immutable CH detection remain distinct. List,
auth, cursor/filter authority, timestamp precision and CH-outage independence
must retain accepted 04-04 behavior. The already declared final history DTO may
remain empty until its assigned 04-08/09 audit work, as the shared contract says;
this amendment does not add triage or fabricate history.

Migration 005 bounds each `rules_manifest` to 8192 bytes, while manifest binding
and configuration adoption update that column. It is not an immutable archive.
Only an available, unambiguous definition for the actual recorded rule version
may inform historical explanation. Any lookup must bound rows, time and decoded
payloads; truncating candidates cannot justify a claim of uniqueness. Missing or
ambiguous history uses the original explicit unavailable explanation, preserving
recorded ID/version. No migration, registry, index, unbounded manifest scan or
substitution of current thresholds is approved.

The inspected SessionProvider revokes generation before cancellation, clears
credentials/cache and owns the client lifetime. The proposed client extension
can preserve those guarantees without provider/auth/UI refactoring. Detail query
keys and late-body handling must retain verified-session isolation, with no
credential in URLs, public context, persistent storage or logs.

## Test ownership and remaining gates

The owned native Alerts test file already builds and starts the actual query-api
binary against PG. Extending that family can exercise the new production wiring
and actual CH evidence without changing shared fixtures. The owned detail test
file can exercise the real provider/client through component interactions. These
are feasible ownership assignments, not evidence that those cases now pass.

Separate causal native/component RED, tests-only commits, actual PG+CH detail,
evidence deduplication/retention, tenant/unknown/orphan negatives, dependency
outages and rule-version mismatch/unavailability remain required. Preserve all
55 accepted web regressions, native Alerts/Auth/cursors and strict OpenAPI/schema
checks; retain dialog focus/Escape/restore, escaped malicious payloads, deep-link
back/forward, cancellation and stale-session/body negatives. Applicable quality,
scoped real-browser evidence, clean independent source review and root acceptance
remain required at their existing caps.

If delivering the actual route causally breaks a retained strict OpenAPI/consumer
fixture outside this union, preserve the failure and checkpoint for explicit
ownership review. Do not weaken an assertion, silently edit another path or
preemptively grant fixture ownership. No such runtime failure is claimed here.

## Review provenance and limits

Read the complete A1 proposal, original 04-05 plan, CONTRACT, UI-SPEC, relevant
04-04 summary and clean R1, pattern/validation context, and actual query dependency,
main wiring, PG list/writer lookup, CH store, typed client and SessionProvider
code. No root AGENTS.md or project `.codex/skills`/`.agents/skills` directory was
present. Reviewed HEAD and user-dirty status with read-only commands.

This is static, bounded planning review, not a whole-phase replan or source
acceptance. No tests, server, browser, SDK, producer, agent, service or remote
operation was run. Only this report was written; no source, test, PLAN, tracking,
index or commit was changed. User `.planning/config.json` and `.serena/` remain
untouched. No 04-06, Phase 5, publication or hosted CI authority is granted.
