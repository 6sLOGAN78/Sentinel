---
phase: 04-query-api-analyst-dashboard
plan: "10"
reviewed: 2026-10-25T00:18:10Z
depth: standard
reviewed_head: 8fdcdaedac631b2239dbd122683e53700a663566
source_commit: dbc7ae226fa9045055edfccd49e21092dad4ac70
tests_red_commit: a55d82f20a0b3667f798a540f12e04a761fd9c51
diff_base: 73dddfbc2b87d06b04bade01d2545f59d3a3dd88
files_reviewed: 9
files_reviewed_list:
  - backend/internal/query/rules.go
  - backend/cmd/query-api/main.go
  - backend/testphase4/summary_rules_test.go
  - api/openapi.yaml
  - web/src/api/schema.d.ts
  - web/src/pages/Rules.tsx
  - web/src/pages/Rules.test.tsx
  - web/src/App.tsx
  - web/src/api/client.ts
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
runtime_verification: withheld_unhealthy_new_stack
---

# Phase 4 Plan 10: Code Review Report

**Reviewed:** 2026-10-25T00:18:10Z
**Depth:** standard
**Files reviewed:** 9
**Source status:** clean
**Independent native rerun:** withheld; NEW runtime failed health admission

## Summary

Independent adversarial source review found no concrete BLOCKER or WARNING in
the nine-file startup Rules increment. All scoped files were read, and the
changed code was traced through the existing loader, configuration, auth,
lifecycle, retained PG explanation, browser transport and session dependencies.
The eight tracked source paths at reviewed HEAD match GREEN source dbc7ae2.
The explicitly assigned ignored generated schema was also read; its SHA256 is
`a590e36b43fcf289ee50a15af9a4a831610c85c65f8b82a2830abf5f78a2280a`.
It remains ignored and was not staged or force-added.

Context included the GSD reviewer instructions and code-review skill,
10 PLAN, ownership A1/CHECK, shared CONTRACT/UI-SPEC, 10 SUMMARY including its
superseding root continuation, 09 SUMMARY/REVIEW-R0, and 07 STACK-RECOVERY.
No root AGENTS.md or project .codex/skills/.agents/skills directory exists.
Fallow was disabled; no structural pre-pass was supplied.

The clean verdict is a source-review result. Independent native and current
browser runtime acceptance remain open because the required NEW infrastructure
was stopped at admission. That environmental condition is not classified as a
source defect or relabelled as a passing native check.

## Narrative Findings (AI reviewer)

No findings.

The review examined these concrete failure boundaries:

- `backend/cmd/query-api/main.go:44-54` and `:155-168`: actual
  `detection.LoadRules` runs before dependency/listener setup. Open, parse and
  close errors propagate to failed startup. The existing loader bounds input
  to 64 KiB, rejects aliases/merges/extra documents/unknown or absent settings,
  requires all four unique known definitions, and validates version, enabled,
  threshold, severity and millisecond durations. Invalid input cannot reach
  public or ops listener creation. The path is trusted operator startup
  configuration, never request input.
- `backend/internal/query/rules.go:35-59`: the handler copies only scalar
  values into its own finite snapshot, including disabled rules. Duration
  conversion preserves the loader's validated millisecond precision. The
  request path performs no disk read or mutation. GET uses existing verified
  JWT middleware and RequirePrincipal for every accepted role; no management
  or executable-input route is added.
- `backend/cmd/query-api/main.go:63-150`: loader integration retains the
  existing managed dependency/listener lifecycle and reverse-order cleanup.
  Auth verification, existing event compatibility route and query routes
  remain wired. No detector configuration or retained-store write was added.
- `backend/internal/query/rules.go:62-74`: fixed explanations match the
  current detector predicates and grouping: failed authentication by source
  and principal, distinct destination ports by source and destination,
  net.request volume by source, and denied policy.decision volume by source.
- `backend/testphase4/summary_rules_test.go:29-204`: actual query binary
  fixture, independent random signing material, exact four definitions for
  all three roles, unauthenticated401, rejected writes, startup file mutation
  and removal, restart adoption, and malformed/invalid/missing startup cases.
  The failure helper requires an actual nonzero exit before its deadline;
  timeout is explicitly rejected as fail-closed proof. Process cleanup uses
  the unchanged bounded startQueryAPI helper.
- `backend/testphase4/summary_rules_test.go:127-162` and unchanged
  `backend/internal/storage/postgres/query_alerts.go:90-137`: the retained
  version77 fixture is decision/fingerprint-bound and remains distinct from
  startup version12. Historical lookup requires tenant, alert, rule, exact
  version and matching manifest fingerprint, with unavailable fallback for
  missing/conflicting/incomplete candidates. Current Rules does not query
  this mechanism as a registry.
- `api/openapi.yaml:327-348` and `:960-1006`, plus generated schema:
  JWT read contract, explicit integer threshold/window_ms/cooldown_ms and a
  maximum of four rules match the handler and typed UI. Canonical Event/Alert
  schemas remain unchanged by this increment.
- `web/src/api/client.ts:29-74`: Rules uses the existing private generated
  transport. Generation/lifetime checks precede credential access, fetch
  combines session and caller signals, and Rules repeats current-generation
  and caller-abort checks after body parsing. A current401 disposes the
  session; a revoked client cannot obtain a newer credential. The response
  envelope is checked for an array and finite maximum.
- `web/src/pages/Rules.tsx:4-55` and `web/src/App.tsx:67-162`:
  generation-scoped React Query reads, exact loading/error/retry/empty copy,
  enabled/disabled text, exact operational values, real Rules link and
  aria-current state, back/forward subscription and normal anchor keyboard
  activation. Descriptions and values render through escaped React text.
  Existing wrapping card styles apply. There are no CRUD controls or extra
  credential/storage paths.
- `web/src/pages/Rules.test.tsx:65-193`: the eight component cases exercise
  the actual App/session/client, all roles, empty/error/retry, history return,
  escaped text, caller/session post-body cancellation, stale-client
  credential denial, logout cancellation/cache removal and current401.
  Existing session disposal revokes generation before abort and cache clear.

## Independent verification

Checks ran at HEAD8fdcdaedac631b2239dbd122683e53700a663566, with source unchanged
from dbc7ae2. Private review evidence is in
`.local/phase4-acceptance/04-10-review/`: directory0700, harness/evidence0600,
child umask022. Exact argv/cwd/sourceSHA/working changes, UTC timestamps,
monotonic elapsed time, actual exit and redacted output are retained privately.

| Check | UTC interval, 2026-10-25 | Actual result |
| --- | --- | --- |
| Explicit NEW Compose ps -a --format json | 00:17:29.301381–00:17:29.434282 | Compose exit0; enclosing health admission exit1; 11,054,395,392 available disk bytes |
| ./node_modules/.bin/vitest run src/pages/Rules.test.tsx, cwd web | 00:17:44.207763–00:17:46.661036 | Exit0; 2.453287s; one file, eight tests passed |
| git diff --check 73dddfb..dbc7ae2, exact tracked scope | Review interval | Exit0 |
| git diff dbc7ae2..HEAD, exact tracked scope | Review interval | Empty; no subsequent source changes |
| Generated schema SHA256 | Review interval | Matches the root continuation's recorded hash |

The Compose invocation used only project `sentinel-p4-demo-20261024`,
absolute base Compose followed by the approved private overlay, and absolute
root .env via --env-file, using the approved07 context environment. No resolved
Compose configuration, headers, token or credential value was printed.

Admission observed Kafka exited143 and ClickHouse/PostgreSQL/Redis exited0,
approximately six hours earlier. Ingest, detector, event-writer, alert-writer
and query-api were restarting. Disk passed the2GiB floor, but health did not.
The reviewer did not start, restart or repair services. Therefore the intended
independent command
`bash ../scripts/go-test-nonempty.sh ./testphase4/... '^TestPhase4SummaryRules$' -tags=phase4 -race -timeout=6m -p=1`
was **not run**. No native PASS, current runtime health, schema7 observation,
real API/browser observation or recovery is claimed by this review.

The component command removes runtime SENTINEL_/PG overrides. Its request
responses are mocked; it proves browser component/transport behavior, not live
PG/CH/API integration or physical keyboard/layout behavior. No full web suite,
typecheck/build/lint/scanner gate, expensive Phase3 aggregate or whole-phase
acceptance was repeated here.

## Earlier evidence and remaining limits

The 10 SUMMARY's superseding root continuation records actual committed-source
native SummaryRules EXIT0/all nine cases and current-source browser EXIT0/all
three roles at1440/375 at dbc7ae2. Those remain prior executor/root evidence,
not independent reviewer reruns. Shared18081/18080 retain earlier07images and
cannot establish current10source behavior.

Tests-only REDa55d82f precedes GREENdbc7ae2. Historical component harness,
quality and root helper-ordering failures remain honestly recorded in the
summary/private evidence; the clean source verdict does not relabel them.
The summary's superseded incomplete handoff is preserved history. Root still
owns runtime admission/recovery, independent native/postwave checks, acceptance
and tracking. Old original missing-history runtime remains held and untouched.

Only the two assigned review reports and ignored private review harness/evidence
were written. No source/configuration/scanner policy/shared tracking/user dirt
was edited; no branch change, reset, stash, seed resend, migration, service
stop/start/rebuild, prune, remote operation or commit occurred. Reports are
uncommitted for root review. No whole-plan or phase-completion claim is made.

---

_Reviewer: gsd-code-reviewer_
_Depth: standard_
