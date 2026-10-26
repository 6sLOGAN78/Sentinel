---
phase: 04-query-api-analyst-dashboard
plan: "14"
subsystem: api
tags: [sse, postgres, listen-notify, recovery, readiness, resource-bounds]
requires:
  - phase: 04-13
    provides: Authenticated bounded SSE and independently reviewed UTC delivery timestamps
provides:
  - Dedicated reconnecting LISTEN connection and actual listener readiness per replica
  - Transactional constant-payload notification hints with unconditional durable polling
  - Bounded client hint queues, configurable hard resource caps and native shutdown proof
affects: [04-15, 04-16]
tech-stack:
  added: []
  patterns: [Dedicated pgx notification connection, bounded local hint fan-out, durable replay authority]
key-files:
  created:
    - backend/internal/alertstream/listener.go
    - backend/internal/alertstream/stream_test.go
  modified:
    - backend/internal/alertstream/handler.go
    - backend/internal/alertstream/delivery.go
    - backend/internal/httpx/httpx_test.go
    - backend/internal/config/config.go
    - backend/cmd/query-api/main.go
    - backend/testphase4/stream_test.go
key-decisions:
  - Notifications contain only a constant wakeup and never advance a delivery cursor.
  - Report listener connection state separately from database connectivity and keep polling unconditional.
  - Environment settings may lower the fixed resource ceilings; invalid or oversized values fail startup.
  - Hold each owned listener startup behind a native proxy barrier until both readiness failures and commit/delivery during absence are observed.
requirements-completed: []
requirements-targeted: [LIVE-01, LIVE-02, LIVE-03, UI-06]
duration: "Not inferred across the recorded UTC discontinuity; individual monotonic producer durations retained"
completed: 2026-10-26
status: complete
---

# Phase 4 Plan 14: LISTEN recovery and bounded stream resources Summary

**Each query replica now owns a reconnecting PostgreSQL listener, while bounded hints accelerate independent durable replay and one-second polling survives lost notifications.**

## WR-01 follow-up — native fixture repair, production unchanged

This section supersedes the original reconnect test's timing/commit-during-loss
claim below. Independent R0 at `0907adb` found no production defect and one
fixture reliability warning: a permitted100ms reconnect could finish before
the old single readiness sample. The warning is reproduced and repaired;
independent R1/root acceptance remain pending. The original R0 report, passing
schedules and clock-gap failure remain preserved.

Tests-only RED `62f09a0` deliberately lets both actual API children reconnect
with fresh PIDs and `ok` readiness before resuming the old insert/sample.
The exact full native family exits1 only at the old `require.True(degraded)`;
the other ten cases pass. This is a controlled counterexample to the test's
reliability, **not a production bug or an expected application outage**.

Fixture GREEN `66a3b35` changes only
`backend/testphase4/stream_test.go`. Two private loopback PostgreSQL proxies
serve the two owned API children. Each inspects only a bounded startup packet
for the owned database and fixed listener application name. Ordinary database
connections and subsequent authentication/query traffic pass unchanged. The
parent control pool and notification observer keep direct access to NEW PG.
No raw startup parameters, authentication bytes, passwords or DSNs are logged.
Startup length/key-value parsing follows the official
[PostgreSQL message format](https://www.postgresql.org/docs/current/protocol-message-formats.html).
The fixture explicitly requires its existing plaintext loopback test mode;
production SSL/security configuration is unchanged.

The repaired case first awaits both established listeners, closes both startup
gates, terminates only the two owned listener PIDs and observes a blocked
reconnect attempt from **each** child. While both gates stay closed, it asserts
actual PG listener PID absence and `unavailable` for **each** readiness
dependency. It then commits a real workflow using the direct control pool and
requires both actual streams to receive it through durable polling. It again
checks zero listener PIDs and both unavailable states **before releasing
either gate**. Only then does it release the gates and require new LISTEN PIDs,
successful re-LISTEN queries, `ok` for both and replay of another real commit.
The proof no longer depends on observing a brief reconnect window.

The proxies bind only127.0.0.1, target only the newly owned isolated database,
bound startup packets64KiB and startup reads/dials5s, and restore gates on
all failure paths. Cleanup cancels waiting work, closes every tracked accepted
and upstream connection, closes the listeners and joins proxy workers/copy
goroutines within5s. API child cleanup precedes proxy cleanup, which precedes
parent-pool/database cleanup. Every actual passing run completed those joins
and owned database drops without reported cleanup failure.

Follow-up evidence is retained in the same private14 directory, with exact
SHA/worktree/UTC/elapsed/exit fields and unchanged0700/0600/child022 handling:

| Follow-up check, Oct26 UTC | Start–end | Monotonic elapsed | Actual result |
|---|---|---:|---|
| Controlled old-sample schedule RED |06:09:30–06:10:07|37.850s|1; only WR-01 assertion fails; other10 pass |
| Held-barrier full native GREEN |06:12:25–06:13:06|40.799s|0; all11 cases, package38.873s |
| Initial phase4 lint |06:13:20–06:13:26|5.924s|0; no issues |
| Targeted held-barrier repeat |06:14:10–06:14:36|26.092s|0; three actual runs, package22.976s |
| Clean ordinary make test |06:14:11–06:15:21|70.006s|0; ordinary Go/web170/generator121; unchanged300s cap |
| Three-mode vet/build/lint |06:14:20–06:14:46|Individual0.558..6.249s|0 for all nine commands |
| Committed-HEAD full native Stream |06:15:44–06:16:26|41.746s|0; all11 cases, package40.739s at66a3b35 |

The full native command remains exact phase4-only/race/10m/p1/nonempty as
recorded below. The justified concurrency repeat uses the same flags and:

```sh
bash ../scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Stream$/^owned_listener_loss_readiness_relisten_and_both_replicas_replay$' -tags=phase4 -race -timeout=10m -p=1 -count=3
```

Each run mints fresh finite credentials; no auth, retry or test budget was
enlarged. All eleven original cases remain, including independent no-hint
polling,206-ID replay/auth/heartbeat/expiry, slow socket/queue stress/admission
and active-stream/listener shutdown. This repair introduces no test failure
beyond the deliberately demonstrated old-assertion RED, and no production fix.

`git diff 7ccf285..66a3b35 -- backend/internal backend/cmd web api` is empty.
Accordingly the actual browser1440/375 evidence at production `7ccf285` is
**carried by unchanged production identity**, not claimed as a browser rerun
at the fixture commit. No wider native families were rerun for this test-only
repair. Ordinary make's npm replacement ran without a concurrent separate
web/browser process. OpenAPI/generated hashes below remain unchanged.

Pinned gitleaks8.30.1 staged/generated/private-harness/full-history scans pass
with unchanged policy before RED and GREEN commits. The follow-up summary is
committed separately, followed by a final full-history scan reported to root.
Only this summary, the one owned native test source and private evidence changed
in the repair envelope. Root tracking/reviews and user dirt are untouched.
The NEW detector-history/eight-healthy and old-runtime holds remain unchanged;
no recovery/reset/role/hba/global clock/offset/fence/retention/remote action ran.

WR-01 self-check passes: RED `62f09a0` precedes fixture GREEN `66a3b35`, the
committed-HEAD native evidence names its full SHA and shows only preserved
user dirt, all owned proxy/child/database resources joined/cleaned up, and no
tracked deletion occurred. Independent R1 and root postwave acceptance are next.

## Original implementation record

Both implementation tasks are complete, with tests-only RED `fb8bba0` and
minimal GREEN `7ccf285`. Independent source review and root acceptance remain
pending. This is incremental stream implementation evidence, not whole-phase
or fresh-detection/full-pipeline acceptance. Root retains ownership of
STATE/ROADMAP/REQUIREMENTS/VALIDATION and review updates; no SDK tracking
mutation or remote operation ran.

The approved ownership envelope contains nine source paths. Eight changed;
`backend/internal/httpx/httpx.go` required no change. The ordinary HTTP deadline
regression lives in its owned test file. No tenth source path, migration,
dependency, API contract or generated schema change was needed. User dirty
`.planning/config.json` and `.serena/` remain untouched and unstaged.

## Behavior and interfaces

`NewListener` parses trusted connection configuration without exposing its
DSN in errors. `Run` owns one dedicated pgx connection with persistent
`application_name=sentinel-alert-listener`, executes the fixed LISTEN command,
and reconnects/re-LISTENs with cancellable100ms..5s backoff. It never checks out
a query-pool connection per client. `Ready` is unavailable before successful
LISTEN and throughout disconnected retry intervals. Main registers that actual
dependency alongside PG/Redis/CH and joins listener work before pool closure.
Unexpected background errors use the existing lifecycle failure seam.

The existing projector still locks its singleton counter before selecting
pending rows and keeps delivery/counter/pending operations in the same PG
transaction. The new commit helper emits one fixed
`pg_notify('sentinel_alert_delivery','changed')` only when new deliveries were
inserted, after insertion and before COMMIT. Notification errors roll back
the transaction. No foreign I/O, counter protocol change, trigger rewrite or
cursor derived from hints was introduced. All four UTC normalizations remain.

Each admitted handler owns a bounded hint subscription. Default capacity is32;
the33rd queued hint detaches and cancels that consumer without blocking fan-out
to another consumer. Admission and subscription registries are capped100.
Cancellation, overflow, expiry and shutdown release registration and admission.
The handler independently reads bounded durable pages, polls every1s even
without notifications, and retains15s heartbeat comments. Five-second route-local
write budgets and the earlier of10minutes/token expiry remain; ordinary HTTP
read/header/write/idle deadlines remain enabled.

`StreamConfig` adds `SENTINEL_STREAM.CLIENTS`, `.QUEUE`, `.WRITE_TIMEOUT` and
`.LIFETIME`. Defaults/ceilings are100/32/5s/10m. Query-api startup rejects zero,
negative, oversized and unrepresentable values. Lower valid settings reach the
real handler through main; direct construction also bounds every field.
Other services do not acquire a new required stream validation section.

## Actual verification

Evidence is private at `.local/phase4-acceptance/04-14/`. The approved NEW07
launcher records argv/cwd, exact SHA and working state, UTC start/end, monotonic
elapsed time, actual exit and disk observations. Directory0700, evidence and
harness files0600, owned binaries0700, child umask022. Secrets are redacted or
suppressed. All results below were inspected after their producer exited.

Initial evidence begins2026-10-25T23:25:06Z. A later producer records UTC
23:29:42→2026-10-26T05:46:31 with only32.390s monotonic elapsed. No overall
execution duration or cause of that discontinuity is inferred.

| Check | UTC interval | Monotonic elapsed | Actual result |
|---|---|---:|---|
| First native RED, Oct25 |23:25:06–23:25:45|39.620s|1; absent LISTEN/hint assertions plus fixture contamination |
| Config RED, Oct25 |23:26:30–23:26:33|3.109s|1; unsafe stream settings accepted; HTTP deadline regression passes |
| Registry/config RED, Oct25 |23:27:00–23:27:04|3.218s|1; absent bounded registry and unsafe settings |
| Corrected native RED, Oct25 |23:27:10–23:27:51|40.870s|1; exactly three new absent-LISTEN/notification/recovery branches fail; other seven pass |
| First owned unit GREEN, Oct25 |23:29:19–23:29:23|3.959s|0 |
| First native GREEN attempt, Oct25→26 |23:29:42–05:46:31|32.390s|1; last two branches401 after original token expired across recorded UTC gap |
| First phase4 lint, Oct26 |05:52:13–05:52:22|8.456s|1; ten scoped complexity/style findings |
| Final owned unit race checks |05:53:42–05:53:46|4.015s|0; alertstream/httpx/config,60s cap |
| Native GREEN-v2 |05:54:41–05:55:26|45.403s|0; all11 subtests |
| Second phase4 lint |05:54:42–05:54:49|7.048s|1; shadowed local plus121-character test declaration |
| Clean ordinary make test |05:54:57–05:56:08|70.718s|0; ordinary Go, web170, generator121;300s cap |
| Affected native Delivery |05:54:58–05:55:16|17.629s|0; all10 subtests including actual non-UTC child/ACK-loss/overflow |
| Affected native Auth/Alerts/Events/Triage/SummaryRules |05:55:10–05:55:51|41.463s|0; five top-level families/90 direct subtests |
| Complete OpenAPI |05:55:11–05:55:12|1.230s|0; all11 top-level assertions |
| Default/phase4/integration vet/build/lint |05:55:47–05:56:16|Individual0.648..6.809s|0 for all nine commands |
| Web test/type/lint/format/build |05:56:52–05:57:11|19.069s|0;170 tests, generated schema unchanged |
| Committed-HEAD native Stream |05:57:17–05:57:59|42.665s|0; all11 subtests, package39.239s at7ccf285 |
| Committed-HEAD actual browser harness |05:57:29–05:57:39|9.993s|0;1440/375, actual browser child5.517s at7ccf285 |

Native exact command, run from backend through the NEW context:

```sh
bash ../scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Stream$' -tags=phase4 -race -timeout=10m -p=1
```

Delivery uses the same command with `^TestPhase4Delivery$`. Other affected
families use `^TestPhase4(Auth|Alerts|Events|Triage|SummaryRules)$`, phase4/race/
6m/p1. Complete OpenAPI uses `^TestOpenAPI`,60s/p1, without integration tag.
Owned unit feedback uses `go test -race -count=1 -timeout=60s
./internal/alertstream ./internal/httpx ./internal/config` in a clean unit
environment. Ordinary make had no runtime configuration overrides. Make's
npm dependency replacement finished before separate web/browser execution.
No caps, nonempty selectors, existing Phase3 suites or scanner policy changed.

The final native family builds current race-enabled query-api children against
one newly created migrated schema8 database. It observes exactly two owned
LISTEN backends, terminates only those exact PIDs under database/application
predicates, commits during their absence, observes the listener readiness
dependency degrade/recover, and verifies new PIDs with the LISTEN query.
Both real replicas receive the expected delivery identity after loss and a
subsequent commit. A separate dedicated pgx observer receives the actual
committed constant hint. The controlled owned-DB durable-row fixture emits no
NOTIFY and still reaches the stream within3s through unconditional polling.

All earlier stream assertions remain:206 exact own identities on each replica,
foreign exclusion, REST-before-commit handoff, decimal reconnect, header auth,
query-token/cursor rejection before flush, heartbeat beyond ordinary2s deadline,
and real token expiry. The native slow reader uses an unread real TCP socket
with a small receive buffer and100 large newly owned workflows. An independent
replica reads all100 while that socket remains unread;40 committed hints then
stress wakeups. At8s from the committed workload, all100 admission slots can be
reacquired. The separate101st request returns429 and disconnect releases slots.
A third current API receives an interrupt with an active stream; process,
stream and owned listener join within5s while the other two listeners remain.

Internal component assertions prove exact32-hint capacity and synchronous
overflow cancellation on33, healthy-subscriber continuation, idempotent detach,
closed registry behavior,100 admissions and direct configuration caps. The
10minute/earlier-expiry clock advancement is explicitly **MODEL** through the
handler's actual deadline function. It is not ten minutes of native elapsed
observation. The native slow-socket result observes bounded admission release,
not exact internal queue occupancy or kernel-buffered TCP FIN drainage; it does
not establish a precisely measured5s blocked-write duration.

The real browser uses the independently reviewed graceful-EOF proxy variant
from13, preserving the original failed destroy/Vite harness and its evidence.
It builds the committed API, creates/migrates its own PG database, uses actual
CH/Redis, terminates only its own listener6619, and observes new PID6624 with
successful re-LISTEN before browser assertions. Desktop/mobile checks pass
live arrival, explicit filtered refresh, connection drop, REST recovery,
decimal cursor progress above2^53, signout/foreign-session isolation and
memory-only credentials. No route mocks or fresh detector run was used.
Both screenshots were inspected; visible layout is consistent with the
existing approved UI. No new full accessibility certification is claimed.

OpenAPI SHA256 remains
`c8485cefb0e91ac79b1d8cd5f7ac8242d8b00369a815927350adebef426821ef`;
generated schema remains
`18fa5c129537eb3678664b22f306a917fe20fef264570d65e75bdc44b215a28e`.
Pinned gitleaks8.30.1 staged/generated/private-harness/full-history scans passed
before both task commits with unchanged policy. Final post-summary scan is
reported separately to root after this metadata commit.

## Deviations and preserved failures

1. First RED fixture added own-tenant rows before the earlier snapshot branch,
   exposing its empty-start assumption. The corrected test captures the current
   committed bound before its real REST barrier; the intended three absent
   listener behaviors then fail while earlier behavior remains green. Both
   actual failing runs are retained; no compile/setup failure is called RED.
2. The first GREEN attempt's later401s coincide with the recorded UTC jump.
   Its listener/notification/recovery/poll/replay branches passed, but the run
   is still a failure. No token lifetime, auth policy or deadline was enlarged;
   the fresh finite-token run and committed-HEAD repeat pass.
3. Two bounded lint correction rounds extracted the transaction commit helper,
   grouped managed listener wiring, named config constants and fixed local
   shadow/line widths. Narrow internal-state/native-test lint explanations do
   not skip runtime tests. All three final build modes pass.

No architectural deviation, package installation, authentication gate, generated
golden replacement, service reset, shared data/counter mutation or remote
operation occurred. Stub and threat-surface scans found no goal-blocking stub
or security surface outside the approved listener/resource threat model.

## Limits and next ownership

The NEW detector's lost checkpoint history/eight-healthy hold and old-stack hold
remain unchanged. No fresh detection, nine-healthy runtime, whole pipeline,
full Phase4 acceptance, fixed disk usage, GC, RSS or throughput claim follows
from these results. New durable tables retain their existing no-GC policy.
All owned runtime children joined and native/browser fixture databases were
cleaned up. Available disk stayed above2GiB; final observed free space6.1GiB.

Next: independent review of these eight changed source paths and root postwave
acceptance, followed by the already planned15/16 work. No Phase5 transition.

## Self-Check: PASSED

All eight source paths and private browser/native artifacts exist. Git verifies
tests-only `fb8bba0` and GREEN `7ccf285`, in that order, with no accidental tracked
deletions. Committed-HEAD native/browser evidence names the exact full GREEN
SHA and shows only preserved user dirt. Root tracking remains untouched.
