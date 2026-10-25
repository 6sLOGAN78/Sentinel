# Plan13 A1 — independent bounded ownership check

**Result: PASS — 0 blockers, 0 warnings.**

Checked at `2d767d8`, after accepted Plan12 `b4c54ed` and its repaired source
`1e9725e`. This is an ownership sufficiency check before implementation, not a
source acceptance, test pass, runtime recovery or whole-phase verification.
Read the proposed13 PLAN/A1, shared CONTRACT/RESEARCH/PATTERNS/UI-SPEC,
12 SUMMARY, current guarded client/session/Alerts and fixture dispatchers,
App/Events navigation, production main/lifecycle/httpx, durable delivery store,
native fixture helpers and existing OpenAPI assertions. No root AGENTS.md or
project `.codex/skills`/`.agents/skills` directory was found.

## Four additions are necessary and sufficient

Approve original ten paths plus exactly these four: **14 total artifacts,
13 tracked paths plus ignored `web/src/api/schema.d.ts`**. The package manifest
and derivative lockfile are already among the original ten; they are not two
new ownership additions. Preserve the bounded two-task feature increment and
context reserve. Root must synchronize the PLAN's file lists/read-first/scope
with this amendment before source edits; the current ten-file wording is not
authority to hide additional edits.

| Added path | Concrete causal need and permitted responsibility |
|---|---|
| `web/src/api/client.ts` | The verified session stores its credential privately and supplies only this client's credential callback, lifetime signal, generation/current check and401 callback. Existing public methods do not expose a stream transport. Add an authenticated stream capability/custom fetch adapter here so live-alerts can use the pinned fetch-event-source without exposing the credential. Retain guards before credential access, combined lifetime/caller cancellation, post-response checks and current-session401 disposal. |
| `web/src/pages/Alerts.test.tsx` | `start` dispatches backfill/session explicitly but routes every other request into the list responder and list request count. A stream would consume list responses/counts and invalidate existing pagination, filters, debounce, retry, cursor and cache assertions. Explicitly dispatch and observe streams separately while preserving all prior behavior, including Plan12 transient recovery, processing-before-cursor and newer-arrival-versus-old-refresh regressions. |
| `web/src/App.test.tsx` | `stubFetch` separates backfill only; its remaining responder includes ordered session/list responses and exact request expectations. Stream attach/reconnect needs explicit dispatch and observation without consuming those responses. Preserve verification-before-read, identity, generation, privacy,401, expiry, cancellation and cache assertions. |
| `web/src/components/AlertDetails.test.tsx` | `start` separates backfill/session/list, then records every other request as detail and delegates to its detail/PATCH responder. An attached stream would pollute exact detail/StrictMode counts or consume transition responses. Add explicit stream dispatch/observation, preserving drawer/history/deep-link/focus, version/conflict and cancellation assertions. |

These are fixture adaptations and the required private transport seam, not new
product features or permission to relax assertions. Do not introduce a public
token/context field, credential URL/storage/logging, or component credential
fetch. Test the new guarded transport through the owned live-alerts test file;
no separate client/session test owner is required merely to add those cases.

## Existing seams support the original feature within this union

`alertstream.Store.Read(ctx, tenant, after, limit)` already returns bounded,
ascending decimal-string delivery envelopes, captures the committed bound
before reading, rejects future cursors and filters by tenant. A separate owned
handler can consume that API independently for every stream/replica. It can
replay finite pages and poll every1s, including the REST-to-SSE handoff and
reconnect interval, without editing delivery storage or adding a competing
consumer. Do not reinterpret workflow seq or JS numbers as delivery cursors.
The existing projector remains managed in main; LISTEN stays Plan14.

The new handler and owned main can register the literal authenticated route
before alert-ID routes, enforce auth/query/cursor validity before flushing,
provide SSE/no-buffering headers,15s heartbeats,100-client admission, bounded
work/hints (at most32),5s writes and the earlier of10min/token expiry. They can
combine request/service cancellation and release admission/timers/work on every
exit. Existing `Lifecycle.Add` executes closers in reverse order: register stream
stop/join so handlers are cancelled before HTTP shutdown/pool closure; do not
leave them waiting out the session maximum during shutdown.

`httpx.New` sets ordinary global timeouts and exposes the standard server. The
handler has the underlying Echo response writer: route-local
`http.ResponseController.SetWriteDeadline` can replace the inherited deadline,
set a finite deadline for each write/flush, then clear it while idle so a5s
deadline does not defeat a15s heartbeat. Cancellation/token expiry remains
separately bounded. Treat controller/flush failure as a closed stream. This
requires no global timeout disable, httpx/config change or new framework.
Native actual-server assertions must prove the route survives the ordinary
write timeout and still disconnects a blocked writer within its write budget.

The new `stream_test.go` shares package `testphase4_test` and can reuse
`deliveryDatabase`, `deliveryInsert`, `startQueryAPI`, `freeAddress` and
`signToken` unchanged. It can add local HTTP stream readers/barriers and start
two real production binaries against one newly owned migrated PG database.
No shared helper modification is necessary. Keep every original two-replica,
each-replica-all-own-IDs, foreign-payload exclusion, invalid/expired-header401,
URL-credential rejection, commit-between-backfill/attach, during-pages and
reconnect expected-ID equality assertion. Local test-owned PG/proxy barriers
can observe/control reads; production store hooks are not required.

Client/live-alerts/Alerts cover exact decimal replay above2^53, runtime envelope
and tenant checks, processing before cursor advance, REST catch-up on every
connect/reconnect, fatal401, bounded jittered503 retry and abort on
unmount/logout/expiry/generation change. Reuse current bounded query
invalidation/new-arrival notice and explicit refresh semantics; retain current
filters/page and the repaired stale-refresh race handling. No session or App
source change is necessary: their current client authority and lifetime already
provide the needed ownership and disposal.

## No fifth addition justified

`web/src/pages/Events.test.tsx` does contain actual Alerts navigation. Its
general fixture presently routes backfill to an event-page response; its
Back/Forward fixture returns404 for `/api/v1/alerts/*`. Both already prevent a
successful catch-up. Attaching SSE only after successful catch-up preserves
that existing path; the navigation assertions select actual list/detail paths
and verify destination filters/history. There is no concrete new stream
request/count conflict requiring an Events owner. Do not add one speculatively
or weaken those existing assertions. The three approved fixture adapters must
exercise the successful attach path explicitly.

`backend/test/openapi_test.go` already lists `GET /api/v1/alerts/stream` in its
closed expected route set and requires bearer security by default; existing
stream assertions require text/event-stream, not the obsolete raw Alert data
description. The owned OpenAPI/schema can correct the delivery envelope and
decimal Last-Event-ID/400 semantics without another expected-route edit. Keep
the exact route/security/canonical-schema assertions unchanged.

No unavoidable session/App/Events source, httpx, delivery store, migration,
shared native helper or additional contract fixture edit was identified.
If implementation produces a concrete contrary interface/test failure, stop
and return its exact path/reason for checked ownership before editing it.

## Evidence and operating boundary

Only read-only static inspection was performed. No app, runtime, migration,
build, test or scanner was run; no RED/GREEN or resource proof is claimed.
Execution still owes actual causal RED, scoped source/tests and native/component
verification of the original requirements. This report does not weaken them.
NEW PG25432/schema8 and the separate detector-history-loss hold are unchanged;
PG-only ephemeral API/browser proof cannot claim full pipeline recovery.
No runtime reset, stale006 migrator, seed/recovery, config/policy/tracking or
remote action is authorized by this check. User config/.serena remain unstaged.

```yaml
issues: []
owned_path_count: 14
tracked_source_path_count: 13
approved_added_paths:
  - web/src/api/client.ts
  - web/src/pages/Alerts.test.tsx
  - web/src/App.test.tsx
  - web/src/components/AlertDetails.test.tsx
additional_source_paths_needed: []
implementation_acceptance: false
```
