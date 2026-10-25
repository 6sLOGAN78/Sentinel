# Plan14 A1 — independent bounded ownership check

**Result: PASS — 0 blockers, 0 warnings.**

Checked at `5265ac8`, after accepted Plan13 `d933a34`, feature source
`2c88d18` and separately reviewed UTC source `1a3529e`. This is a static
ownership sufficiency check before implementation, not implementation
acceptance, RED/GREEN evidence or whole-phase verification.

Read Plan14 and proposed A1, shared CONTRACT/RESEARCH/PATTERNS/UI-SPEC,
Plan13 SUMMARY and feature/UTC reviews, current delivery projector/read path,
stream handler, query-api wiring, config loader/validation and historical
config assertions, httpx implementation/tests, lifecycle/readiness/database
seams and native Stream/Delivery/Auth fixture helpers. No root AGENTS.md or
project `.codex/skills`/`.agents/skills` directory was found.

## One addition is necessary; nine paths are sufficient

Approve the original eight paths plus exactly
`backend/internal/alertstream/delivery.go`: **nine source paths total**.
`Store.Project` currently commits durable deliveries but never publishes
NOTIFY. Adding only a listener would leave actual projection without its
notification producer. The proposed existing-transaction producer closes that
specific wiring gap; no migration or trigger rewrite is needed.

Root must synchronize the PLAN frontmatter, scope, action and affected
files/read-first lists with this amendment before source edits. This check
does not itself edit PLAN or shared tracking. Preserve the two bounded tasks
and stop for a checked amendment if a concrete additional dependency emerges.

| Owned path or group | Necessary responsibility and available seam |
|---|---|
| `backend/internal/alertstream/delivery.go` | Emit one fixed-channel, constant-payload `pg_notify` for a nonempty newly inserted delivery batch inside the existing transaction, after insertion and before COMMIT; propagate failure so the entire transaction rolls back. |
| `backend/internal/alertstream/listener.go` | Own one dedicated pgx connection per replica, LISTEN/re-LISTEN, cancellable bounded reconnect, connection state and bounded local wakeup fan-out. |
| `backend/internal/alertstream/handler.go` | Subscribe/unsubscribe each admitted stream with at most32 pending hints, retain independent bounded durable replay and unconditional1s polling, enforce slow-write/admission/lifetime/cancellation limits. |
| `backend/cmd/query-api/main.go` | Construct and register actual listener/projector lifecycle and readiness, wire hints to the existing handler and order shutdown/join before pool closure. |
| `backend/internal/alertstream/stream_test.go` | New internal component tests can observe exact queue capacity, overflow/cancellation and accelerated lifetime behavior without a public diagnostic endpoint. |
| `backend/testphase4/stream_test.go` | Extend the exact native family and define local helpers for owned listener PID faults, lost hints, replica recovery, slow sockets, config and shutdown proof. |
| `backend/internal/httpx/httpx.go`, `backend/internal/httpx/httpx_test.go` | Existing ownership covers any necessary ordinary HTTP deadline regression proof; source changes are optional. |
| `backend/internal/config/config.go` | Existing ownership covers safe stream settings/defaults/caps if configuration is introduced; no unrelated global timeout change is justified. |

## Preserve the transactional delivery contract

Keep counter-row lock acquisition before selecting pending rows, transactional
counter allocation, bounded batch100, delivery/pending/counter atomicity,
rollback and uncertain-COMMIT retry unchanged. Guard notification by newly
inserted deliveries, not merely selected stale pending identities. A single
transactional notification near the existing COMMIT is sufficient. Its
constant payload is only a wakeup; never derive or advance a delivery cursor
from it. Preserve captured-bound reads, tenant predicates, decimal IDs,
pagination and all four UTC normalizations.

The SQL command uses the current transaction and its deadlines. Do not add
HTTP/CH/Redis or other non-PG work while holding the counter lock. A lost
notification or an uncertain commit must still recover through the durable
log and polling. No migration008 modification, global DB trigger or separate
unregistered producer is required.

## Existing wiring supports readiness and resource ownership

`opshttp.Check{Name, Fn}` already accepts an application-owned readiness check;
owned main can construct the listener before registering checks. No opshttp
edit is needed. Report actual disconnected/reconnecting state as unavailable,
restore it after successful LISTEN, and preserve existing dependency checks.
`Draining.Begin` is irreversible: reserve it for shutdown/terminal failure,
not transient reconnect. Terminal listener/projector errors must reach the
existing `Lifecycle.Fail` seam; routine cancellation must not become failure.

`database.ConnString` and pgx already support a separately managed connection;
no database package edit, new dependency or pool connection per client is
necessary. The single listener must not block while broadcasting to a slow
subscriber. Bound subscriber queues and registrations, close/detach them on
overflow or cancellation, and join listener/retry work before DB shutdown.
Reconnect bounds remain100ms..5s and durable polling stays1s regardless of
connection or notification state. Notifications carry no identity or cursor.

The handler already admits100 streams, writes with a5s route-local
ResponseController deadline, clears that deadline while idle and caps lifetime
at the earlier of10min/token expiry. Retain those rules when adding wakeups;
queue overflow and blocked writes must release admission and subscriptions.
Keep ordinary httpx read/header/write/idle limits enabled and prove an ordinary
handler still has its deadline. Source edits in config/httpx are optional;
their owned test seams are sufficient without changing application routes.

## No hidden config or fixture owner established

Historical config tests assert named fields and service requirements; they do
not enumerate a closed Config field set or require whole-Config equality.
There is no demonstrated need to edit `backend/internal/config/config_test.go`
or detector fixtures. The owned alertstream/native/httpx tests can call public
`config.Load`, use scoped environment overrides, and assert defaults plus
zero/negative/oversized/cross-field or overflow-safe boundaries for any new
settings. Do not merely test a clamp helper while leaving runtime loading or
main/handler consumption untested. Keep other services' configuration behavior.

The existing native package exposes reusable `deliveryDatabase`,
`deliveryInsert`, `startQueryAPI`, `freeAddress` and `signToken` helpers. Its
Stream owner can add local helpers instead of editing Auth/Delivery fixtures.
`startQueryAPI` returns both public and ops addresses and inherits scoped
environment settings; the new checks can inspect actual readiness and start
two actual replicas against one newly owned migrated schema8 database.
CH-unreachable fixtures must inspect the listener dependency state or use a
real CH address for an overall200 readiness assertion: an already503 response
alone does not prove listener degradation/recovery.

Identify and terminate only LISTEN PIDs belonging to the newly owned fixture
database/connections. Commit during the controlled loss and verify both
replicas receive exact own IDs after re-LISTEN with new backend PIDs. Prove
lost-hint recovery while the notification consumer is controlled/suppressed,
then observe unconditional durable polling; merely killing a link and allowing
instant reconnect does not isolate polling. Such controls can live in the
owned test files/private harness, not shared migrations or production bypasses.

Use exact-capacity component assertions for queue32 and native sustained
wakeups plus an actually unread TCP socket for the real write/resource bound.
Retain an independent progressing client,101st admission failure and release,
client disconnect, token expiry and service shutdown. Measure write-budget
release relative to the controlled blocked-write interval with explicit
scheduling allowance; kernel-buffered FIN drainage and arbitrary sleeps alone
are not precise5s release evidence. Do not claim native observation of an
internal queue's exact occupancy from the socket test alone. Keep accelerated
10min/expiry clock proof explicitly MODEL/component rather than elapsed native
ten-minute proof. No new public endpoint, INFO hint payload or credentials in
logs/URLs are needed for these assertions.

For browser regression proof reuse the reviewed private proxy's graceful
`res.end` EOF path through Vite and preserve the original failing
`res.destroy` harness/evidence. No web source owner is required by this backend
increment. Preserve all earlier tenant/auth/replay/UTC and browser assertions.

## Evidence and operating boundary

Only static reads and this report write were performed. No application,
runtime tool, test, build, migration or scanner ran; no implementation or
resource proof is claimed. Execution still owes causal behavioral RED,
tests-only/minimal-GREEN scoped commits, affected regressions and truthful
native/component/browser evidence. Root owns PLAN/tracking/review acceptance;
the executor owns declared sources, private14 evidence and14 SUMMARY.

Only the authorized NEW PG25432/schema8, CH28123/29000, Redis26379 and
Kafka39092 context applies to later execution. The detector checkpoint-history
loss remains a separate eight-healthy hold; PG-only proof cannot establish
pipeline recovery. No shared reset/migration/seed, stale006 migrator, runtime
recovery, remote action, branch switch or policy change is authorized here.
User dirty `.planning/config.json` and `.serena/` remain untouched/unstaged.

```yaml
issues: []
owned_source_path_count: 9
approved_added_paths:
  - backend/internal/alertstream/delivery.go
additional_source_paths_needed: []
implementation_acceptance: false
runtime_verification: not_run
```
