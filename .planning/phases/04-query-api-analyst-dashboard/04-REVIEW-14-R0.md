---
phase: 04-query-api-analyst-dashboard
plan: "14"
reviewed: 2026-10-26T06:04:48Z
depth: standard
review_iteration: R0
reviewed_head: db0fa224cb1015ae50bd0a99f02e6d496ae8cf86
source_commit: 7ccf285c05a7a4123ead5f00ef8bfa2ac8eb8af4
tests_red_commit: fb8bba032f2aa9cd5e8b5427f739588179baef8b
diff_base: b5e842a37b47199eef01f5b6ed47082135fe23c7
files_reviewed: 9
files_reviewed_list:
  - backend/internal/alertstream/listener.go
  - backend/internal/alertstream/handler.go
  - backend/internal/alertstream/stream_test.go
  - backend/internal/httpx/httpx.go
  - backend/internal/httpx/httpx_test.go
  - backend/internal/config/config.go
  - backend/cmd/query-api/main.go
  - backend/testphase4/stream_test.go
  - backend/internal/alertstream/delivery.go
findings:
  critical: 0
  warning: 1
  info: 0
  total: 1
status: issues_found
structural_prepass: disabled
runtime_verification: independent_native_stream_and_clean_unit_race_passed
whole_phase_acceptance: false
---

# Phase 4 Plan 14: Code Review Report — R0

**Reviewed:** 2026-10-26T06:04:48Z
**Depth:** standard
**Files reviewed:** nine declared source paths; eight changed
**Result:** 0 BLOCKERS, 1 WARNING.

## Narrative Findings (AI reviewer)

### Warnings

### WR-01: WARNING — Listener-loss test races the automatic reconnect

**File:** `/home/logan78/Desktop/Sentinel/backend/testphase4/stream_test.go:151-163`
**Related:** `/home/logan78/Desktop/Sentinel/backend/internal/alertstream/listener.go:48-62`
**Issue:** After terminating the two owned listener backends, the test inserts a
workflow and only then samples each replica's readiness once. Nothing prevents
both listeners from reconnecting during their normal 100ms retry interval.
A valid schedule pauses the test goroutine after termination for more than
100ms, lets both API children reconnect/re-LISTEN, and resumes the insert and
readiness requests. Both readiness results are then correctly `ok`, but
`require.True(degraded)` fails against a healthy implementation. Ordinary
scheduler delay, a slow fixture insert or readiness dependency checks can all
consume this observation window. The same missing barrier means the claimed
commit-during-loss is not guaranteed. Checking only whether either replica was
unavailable also leaves degradation of the other replica unverified.

**Evidence:** The retry is autonomous and explicitly resets to 100ms after a
previously established connection. There is no connection gate, handshake or
held absence interval between termination and the readiness requests. This is
a source-proven permitted interleaving, not a reproduced failure in the
independent run: the unmodified native suite passed on this run. That passing
schedule does not remove the race.

**Fix:** In this owned database fixture, hold a test-scoped connection gate
that prevents both terminated listener connections from reconnecting. Keep an
already acquired control connection for the workflow commit. While the gate
is held, await `unavailable` for **each** replica and commit the new workflow;
then release the gate and await fresh LISTEN PIDs and `ok` for both. Restore
the gate in cleanup on every failure path. Keep the fault restricted to the
new fixture database/connections. Increasing sleeps or wrapping the current
post-reconnect sample in Eventually does not establish the missing barrier.

## Review scope and source analysis

Read the full nine files, approved 14 PLAN/A1/CHECK and SUMMARY,
CONTRACT/RESEARCH/UI-SPEC, 13 SUMMARY/feature review, separate UTC prerequisite
review, and ADR0009. Traced unchanged authentication, database connection
construction, native fixture ownership, ops readiness and LIFO lifecycle.
No root AGENTS.md, project .codex/skills/.agents/skills directory or
.codexignore was present. Scoped source paths are not ignored. Fallow was
disabled; no structural findings are inferred. Planning, generated and lock
files are not hidden additions to the nine-file source scope.

No additional concrete production correctness or security defect was
established in this review. In particular:

- Project keeps counter locking before pending discovery, bounded100 selection,
  delivery/counter/pending atomicity, independent rollback and ACK-loss retry.
  The new constant channel/payload pg_notify runs only after nonempty insertion
  and before COMMIT; errors propagate. Hints never enter cursor calculations.
- Store.Read retains captured-bound reads, tenant predicates, decimal IDs,
  bounded pages and all four UTC conversions. Streams still poll every1s
  regardless of notification state or lost hints.
- Listener owns a dedicated pgx connection per replica, outside the client
  pool. Connection/LISTEN failure clears the atomic readiness state; reconnect
  and re-LISTEN use cancellable100ms..5s retries. Main registers the actual
  readiness function and joins the managed listener before pool closure.
- Handler bounds admission100 and hint queues32, detaches/cancels overflow
  without blocking another subscriber, and releases registrations/admission by
  deferred cleanup. Close prevents new admissions and joins active handlers.
  Route-local writes retain the5s ceiling and earlier lifetime/token deadline;
  ordinary httpx header/read/write/idle limits remain configured.
- Config.Load validates runtime stream caps and main passes them to the real
  handler; direct constructors also cap values. Other services do not acquire
  the query-api-only required stream validation section.
- Authentication/actual token expiry precede admission, durable reads and SSE
  headers. No credential-bearing URL, dynamic notification SQL, per-client
  listener pool checkout or foreign-network operation under the counter lock
  was introduced.

These observations explain the reviewed causal paths; passing compilation or
tests was not treated as proof that every interleaving is correct.

## Independent executed checks

Both checks ran at HEAD `db0fa224cb1015ae50bd0a99f02e6d496ae8cf86`, containing
source GREEN `7ccf285`, with no source edits and only preserved user dirt.
Actual producer output was inspected after exit.

| Check, 2026-10-26 UTC | Start–end | Monotonic elapsed | Result |
|---|---|---:|---|
| Clean owned unit race checks |06:03:38.604883–06:03:41.938772|3.334s|exit0; alertstream/httpx/config |
| Exact native Stream |06:03:38.605210–06:04:16.928293|38.323s|exit0; all11 subtests, package37.116s |

Commands, from backend:

```sh
go test -race -count=1 -timeout=60s ./internal/alertstream ./internal/httpx ./internal/config
bash ../scripts/go-test-nonempty.sh ./testphase4/... '^TestPhase4Stream$' -tags=phase4 -race -timeout=10m -p=1
```

Unit execution removed SENTINEL_/PG runtime overrides. Native execution used
only the approved NEW07 PG25432/schema8, CH28123/29000, Redis26379 and
Kafka39092 context, newly owned migrated fixture database and current API
children. Only the fixture's listener PIDs were faulted. Child joins/database
cleanup completed without reported error.

Private evidence:
`.local/phase4-acceptance/04-14-review/20261026T060338604851Z-independent-review-unit.json`
and
`.local/phase4-acceptance/04-14-review/20261026T060338605194Z-independent-review-native-stream.json`.
Directory0700, files0600, child umask022; no secrets, headers or raw sensitive
configuration were exposed.

## Recorded evidence and proof limits

Reviewed the corrected native RED record at base `b5e842a`: exit1 with exactly
the three new missing-listener/notification/recovery branches failing. Config
and bounded-registry RED records independently exit1 for absent behavior.
Tests-only commit is `fb8bba0`, followed by GREEN `7ccf285`. The earlier
fixture-contaminated RED is preserved and is not substituted for causal RED.

The first GREEN attempt remains a failure: its recorded UTC interval spans
23:29:42 on Oct25 to05:46:31 on Oct26 while its monotonic elapsed time is32.390s.
Its late slow-socket/admission branches received401. No cause of that clock
discontinuity, overall duration or passing result is inferred. Fresh finite
tokens subsequently pass, including this independent run.

Executor committed-HEAD Stream evidence at `7ccf285` reports exit0/all11
subtests in42.665s. Its browser child at the same source reports exit0 in5.517s
for both1440/375 widths, using the separately reviewed13 graceful-EOF proxy
through Vite and an actual owned listener kill/re-LISTEN. Original failed
destroy/Vite harness evidence remains preserved. Those browser observations
are carried evidence, not a browser run repeated by this reviewer. Delivery10,
ordinary make/web170/generator121, OpenAPI11, three-mode quality and unchanged
scanner-policy results are also executor evidence with their recorded source
and worktree identities, not independent checks rerun here.

The independent Stream suite exercises actual two-replica replay, real fixed
notifications, the unmodified readiness/reconnect case, separately controlled
durable publication without NOTIFY, own/foreign identity, REST handoff,
decimal reconnect, pre-header rejection, heartbeat beyond the ordinary2s HTTP
deadline, actual token expiry, slow-socket admission release,100/101 admission
and active-stream/listener shutdown. WR-01 qualifies the reliability and
commit-during-loss claim of the reconnect test despite this run passing.

Exact32/33 queue occupancy is component evidence. The native unread socket
proves admission can be reacquired after the8s workload interval while the
other replica progresses; it does not measure an exact5s blocked-write
interval, prove native internal queue occupancy, or time kernel-buffered FIN
drainage. The10minute/earlier-expiry check is explicitly MODEL through the
actual deadline function, not ten minutes of native elapsed observation.
The lost-hint case does independently prove unconditional polling without
relying on the reconnect timing claim.

No full Phase3 aggregate, cap increase, new dependency installation, fresh
detector run, shared reset/checkpoint/fence/retention change, stack recovery,
whole Phase4/full-pipeline acceptance, fixed-disk/RSS claim, Phase5 transition,
remote action or commit occurred. The NEW detector-history-loss/eight-healthy
hold and old runtime hold remain separate. User .planning/config.json and
.serena/ were preserved. Root owns report commit, fixes and postwave acceptance.

_Reviewer: independent gsd-code-reviewer_
_Depth: standard_
