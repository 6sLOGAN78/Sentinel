---
phase: 04-query-api-analyst-dashboard
plan: "13"
subsystem: api
tags: [sse, postgres, polling, react, bearer, delivery]
requires:
  - phase: 04-12
    provides: Gap-safe delivery projection, bounded REST backfill and session client
provides:
  - Committed causal native and actual App/client stream RED assertions
  - Uncommitted polling SSE implementation with passing native and component evidence
affects: [04-13-continuation, 04-14, 04-15, 04-16]
tech-stack:
  added: ["@microsoft/fetch-event-source@2.0.1"]
  patterns: [Independent per-client durable replay, private guarded stream capability, one processed cursor owner]
key-files:
  created:
    - backend/internal/alertstream/handler.go
    - backend/testphase4/stream_test.go
    - web/src/api/live-alerts.ts
    - web/src/api/live-alerts.test.ts
  modified:
    - backend/cmd/query-api/main.go
    - api/openapi.yaml
    - web/src/api/schema.d.ts
    - web/package.json
    - web/package-lock.json
    - web/src/api/client.ts
    - web/src/pages/Alerts.tsx
    - web/src/pages/Alerts.test.tsx
    - web/src/App.test.tsx
    - web/src/components/AlertDetails.test.tsx
key-decisions:
  - Poll each stream's authoritative delivery log independently every second; LISTEN remains plan14.
  - Keep credential injection private and sequence REST/SSE progress through one session-client cursor owner.
  - Observe slow-handler release through real HTTP admission rather than delayed TCP FIN draining.
requirements-completed: []
requirements-targeted: [LIVE-01, LIVE-02, LIVE-03, AUTH-03, UI-02, UI-06]
duration: 12min
completed: 2026-10-25
status: implemented-pending-browser-review
---

# Phase 4 Plan 13: Polling SSE implementation and incomplete verification Summary

## Root continuation — supersedes uncommitted handoff below

Root wrapped the two owned native test lines, then corrected two unsupported
RTL ByRoleOptions.exact properties (literal names remain exact). Initial root
web typecheckEXIT2 preserved; finalwebtype/lint/format/buildEXIT0/9.882s,
END22:37:40Z. Rootthree-modequalityEXIT0/17.841s END22:37:15Z/all9commands.
PinnedunchangedpolicyscansPASS before sourceGREEN2c88d18. CommittednativeStream
6subtests EXIT0/35.567s END22:38:36Z. Originalnative/worktree passes remain.

Firstcurrentbrowser failedwaitingLive. Diagnostic actualbackfill200 repeatedly
emitted+05:30WorkflowAlert fields violating OpenAPIUTCTimestampZ; strictclient
correctlyrejected. Separatetwo-sourceprerequisiterepair independentlyapproved
ce39dfb: REDac7882d/GREEN1a3529e/report21e357c. SeeUTC-REPAIR-SUMMARY for exact
nativeNonUTCchildRED/instants andfour.UTC()normalizations, noSQL/counterchange.
AfterrepairnativeDelivery10/Stream6/OpenAPI11/quality/defaultweb170+gen121/
webgates/scansPASS. BrowsernowLive/SSE200andonlinearrival/filteredrefresh1440
passes, but drop/reconnect step timesoutstillLive; mobile andfullbrowserproof
remainunproved. Privatebrowser-1792968404824598561/failure-state.json retains
responses/DOM; no route-mocks or timeoutincrease. This may need proxy-vs-client
diagnosis; do notcallitpassingorweakenassertions. Full14feature+separate2source
repair review and rootpostwave remainpending; no13acceptance/pipelineclaim.

**Bearer-authenticated polling SSE and a guarded browser replay driver pass native and component checks; GREEN source remains uncommitted pending two test lint findings and browser/static completion.**

## Execution status

Task1 is complete, committed as `bc5acbe` —
`test(04-13): pin authenticated live replay and guarded session lifecycle`.
Task2 is **incomplete**. There is no implementation/GREEN commit and no
committed-HEAD native/browser proof. Production changes, subsequent test
strengthening and package/lock derivatives remain intentionally unstaged for
root's bounded continuation. This summary is committed separately without
staging that implementation. No plan/phase acceptance, requirement completion,
independent source review, hosted CI or remote publication is claimed.

The approved union remains exactly14 artifacts:13 tracked source paths plus
ignored generated schema. No fifteenth source path was changed. Existing
OpenAPI already includes the stream route; no strict-route fixture amendment
or edit was needed. User `.planning/config.json` and `.serena/` were untouched
and remain unstaged. Root owns STATE/ROADMAP/REQUIREMENTS/VALIDATION/reviews;
no SDK tracking mutation ran.

Observed post-recovery evidence spans2026-10-25T22:22:23Z through22:34:31Z
(about12minutes); earlier read-only stopped-store evidence is also retained.
Task1 changed5 test/fixture files; the full planned increment touches14 owned
artifacts including ignored generated schema. Current source HEAD is
`bc5acbe`; evidence records include exact working changes as well as HEAD.

## Implementation currently in the working tree

The new handler authenticates before reading/flushing, rejects URL query
parameters and malformed/duplicate/future decimal Last-Event-ID, and reuses
the exported delivery Store.Read API. Each admitted client/replica independently
replays ascending tenant deliveries in pages<=200 and polls unconditionally
every1s. The poll-only path holds zero notification hints, within the32-hint
bound; LISTEN is deliberately reserved for14. It uses text/event-stream,
Cache-Control:no-cache,no-transform and X-Accel-Buffering:no,15s comment
heartbeats,100 admissions,5s per-write deadlines and the earlier of10minutes
or verified token expiry. Route-local ResponseController deadlines replace
the inherited ordinary deadline while retaining finite writes. Main registers
the literal route before alert-ID routes and cancels/joins streams before
HTTP/pool shutdown. No CH call or PG lock protocol was added.

The client exposes a fixed stream capability and an active-session assertion,
never a bearer value or arbitrary credential-bearing URL. It checks caller,
lifetime and generation before credential access, after response and per frame;
only a current401 disposes its session. The new module validates full runtime
Delivery/WorkflowAlert envelopes and tenant identity, compares decimal IDs with
BigInt, and sequences REST catch-up before every attach/reconnect through one
private WeakMap cursor owner. Frame processing is serialized with at most32
queued callbacks. Progress advances only after awaited processing and another
cancellation/current check. Library retries are disabled; the owner uses
bounded250ms..5s backoff with jitter for stream failures. openWhenHidden keeps
the guarded stream across tab visibility changes; a broken connection still
replays through REST. No disk cursor, token storage or unbounded alert append.

Alerts reuses bounded cache invalidation and the explicit refresh banner while
retaining current filters/page and Plan12's newer-arrival watermark repair.
The connection copy is Live/Connecting/Reconnecting. All old list/detail/
history/focus/session assertions remain; the three authorized fixture files
dispatch successful streams explicitly. Two additional component assertions
cover aborted awaited SSE processing and hidden-tab visibility.

Official primary-source lookup used Azure/fetch-event-source documentation
through Context7 and npm metadata. The exact package is2.0.1 and its lockfile
integrity matches the resolved registry metadata:
`sha512-W6CLUJ2eBMw3Rec70qrsEW0jOm/3twwJv21mrmj2yORiaVmVYGS4sSS5yUwvQc1ZlDLYGPnClVWmUUMagKNsfA==`.
Installation succeeded; only this dependency and its lock entry were added.

## Actual verification

Private evidence lives in `.local/phase4-acceptance/04-13/`. The approved NEW07
launcher records argv/cwd, exact HEAD/working state, UTC intervals, monotonic
elapsed time, actual exits and resource observations. Directories0700,
evidence/harness files0600, owned binary0700, child umask022. Secrets are
suppressed/redacted and never passed in command arguments. Every result below
was inspected after its producer exited.

| Check,2026-10-25 UTC | Start–end | Elapsed | Actual result |
| --- | --- | ---: | --- |
| Exact authorized preserved infra start |22:22:23–22:22:31|7.835s|0; four infra healthy, IDs/mounts/settings preserved |
| Native causal RED-v2 |22:24:45–22:24:53|7.397s|1; built/started actual APIs, stream required200/got400 |
| Actual App/client causal RED-v2 |22:24:46–22:25:05|18.560s|1; all17 new cases fail absent stream/guard behavior |
| First native GREEN |22:27:14–22:27:42|28.566s|0; root+5subtests, package27.591s |
| Full web after first fixes |22:28:22–22:28:30|7.847s|0;168/168 |
| Web typecheck |22:28:23–22:28:29|6.236s|0 |
| Web eslint |22:29:17–22:29:20|3.055s|0 |
| Affected native Delivery |22:29:18–22:29:32|13.613s|0;9subtests, package12.529s |
| Affected Auth/Alerts/Events/Triage/SummaryRules |22:29:19–22:29:57|38.128s|0; package37.140s |
| Complete OpenAPI11 |22:29:20–22:29:22|1.145s|0; all11 top-level assertions |
| Final worktree Stream |22:32:03–22:32:42|38.874s|0; root+6subtests, package35.825s |
| Final full web |22:32:04–22:32:12|7.759s|0;170/170 |
| Final phase4 lint |22:32:06–22:32:10|4.323s|1; exactly two test lll findings |
| Working-tree browser environment |22:32:58–22:33:27|28.422s|1; readiness failed before Chromium execution |
| Clean ordinary make test |22:33:00–22:33:18|18.631s|0; default Go/web170/generator121 |
| Three-mode quality wrapper |22:33:01–22:33:12|11.000s|1; default all3 PASS, phase4 vet/build PASS then lint2 fails; integration unrun |
| Separate web build / format |22:33:02–22:33:03|0.444s/0.214s|127 each; dependency replacement setup race |

Exact native Stream command, from backend via NEW context:

```sh
bash ../scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Stream$' -tags=phase4 -race -timeout=10m -p=1
```

Delivery uses the same flags and exact Delivery selector. Other affected
families use `^TestPhase4(Auth|Alerts|Events|Triage|SummaryRules)$`, phase4/race/
6m/p1. OpenAPI runs the complete `^TestOpenAPI` selector without integration
tag,60s/p1. No passing-by-empty selector, cap increase, Phase3 full aggregate,
golden regeneration or skipped failure was used.

Native Stream builds two actual production binaries against one newly created
migrated PG database, with CH deliberately unreachable. Each replica receives
the exact set of206 own IDs and excludes foreign identity/payload. A real REST
response precedes a held transaction's commit, which precedes both stream
attachments. Another205 real commits after the first frame cross page/poll
boundaries; reconnect recovers a later disconnected commit. Invalid/expired
headers, URL credentials, malformed/future cursors fail before SSE headers.
An actual heartbeat arrives15s after attachment despite ordinary2s global
HTTP write timeout. Token expiry closes around1–2s even with JWT leeway.
100-client admission,101st429 and release pass.

The actual slow-reader fixture shrinks a real TCP receive window, stops
reading, and commits100 newly owned large workflows. After8s (projection1s,
write5s plus scheduling margin), all100 admission slots are available through
real independent HTTP requests. This observes handler resource release rather
than claiming the unread socket's kernel-buffered bytes/FIN have drained.
The10minute maximum is enforced in source; no10minute wall-clock test ran.
The concurrent205-commit case does not instrument or hold an internal
Store.Read page-query boundary; no stronger deterministic internal-page barrier
claim is made.

## Preserved failures and deviations

1. Initial native RED compilation failed on a nonexistent testify helper.
   This was setup failure, not behavioral RED. The corrected RED-v2 built the
   production binary and failed on the actual absent route before source work.
   The native fail-fast gate means later auth/resource branches were exercised
   GREEN, not individually observed preimplementation failures.
2. First component GREEN failed4/97 plus an uncaught missing APIError import.
   The import was restored; connection copy no longer collides with the
   existing loading-status query; exact fixture totals exclude the separately
   observed stream; the new401 helper tolerates immediate session disposal.
   All prior assertions and final170 component cases pass.
3. Scoped backend lint initially found8 style/complexity issues, then one
   replay-complexity issue. Focused replay/write/assert helpers corrected those.
   The final batch introduced two remaining long test lines listed below.
4. The first slow-socket test required draining kernel-buffered bytes/FIN
   within3s and actually timed out. Its failed record remains. The corrected
   external HTTP admission barrier verifies the blocked handler released its
   resource after the write budget, independent of client drain speed.
5. I incorrectly ran dependency-reading web/browser commands concurrently
   with ordinary `make test`, whose npm-ci replaces node_modules. Their actual
   failures are retained: missing openapi-typescript/prettier and the browser
   Vite log's ERR_MODULE_NOT_FOUND for vite/dist/node/index.js. The browser
   never reached Chromium or generated results/screenshots, so browser behavior
   is unproved. Ordinary make test itself completed0. Continue sequentially.
6. After three consolidated source-fix batches, the executor stopped further
   source edits per the attempt cap. Root explicitly directed a truthful
   incomplete summary-only commit and bounded continuation. This is not an
   approval request to the user or a gate waiver.

## Deferred Issues / exact continuation

- `backend/testphase4/stream_test.go:190` —125-character QueryRow assertion.
- `backend/testphase4/stream_test.go:280` —124-character QueryRow assertion.

Both are `lll` maximum120 violations. No other final phase4 lint finding was
reported. Root should wrap these two lines within the existing owner, then
run remaining web gates sequentially with make test, finish three-mode quality,
and obtain actual current-source Chromium1440/375 proof using the retained
private harness. The harness creates its own DB, current API and loopback
transparent fault proxy/Vite; it uses cached full Chromium1248, not the deleted
headless shell or browser route mocks. Its intended live/drop/backfill/privacy/
filter assertions have **not yet executed**. Then stage exact source paths,
run unchanged-policy scans, make GREEN commit, and obtain committed-HEAD native
and browser evidence before independent review/root acceptance.

The implementation is not a stub: the owned new source scan found no TODO,
FIXME, placeholder or fabricated UI data. The new stream auth/network surface
is already assigned to the plan's T04-27/28/29 threat model; no extra endpoint,
migration or trust boundary was introduced. Remaining test/proof limits above
must not be converted to a complete-plan claim.

## Runtime and cleanup

The initial read-only check found NEW PG/CH/Redis/Kafka stopped and five apps
restarting. Root explicitly authorized only `compose start --wait
--wait-timeout120 kafka clickhouse postgres redis` through the exact private
NEW context. It completed0, all four became healthy, all9 owned container IDs
were preserved, and inspected infra Mounts/Config/HostConfig matched before/
after. No build/pull/recreate/shared migration/Compose app start/reset/group/
checkpoint/fence/retention/prune occurred. Detector checkpoint-history-loss
remains a separate hold; neither nine healthy nor fresh detection is claimed.

Native and browser fixtures used only newly owned databases. Shared schema8,
counter/history/reservations were unchanged. Browser finally cleanup joined
its API/proxy/Vite processes and dropped its own DB; native cleanups joined
their APIs and dropped their owned DBs. All launched test/quality/browser
producers have exited. The final process inspection found no owned04-13 or
testphase4 process (only the inspection command). Final available disk was
6.9GiB, above the2GiB floor. Existing shared app processes are not owned
acceptance producers.

## Scans and source identity

Pinned existing gitleaks8.30.1 passed staged RED, generated schema, owned
private harness and full-history scans at22:25:03–22:25:05, with unchanged
policy and exceptions. Summary-only staging, current generated schema/private
harness and full history are scanned again at close-out; GREEN staged scan
remains owed because implementation is intentionally unstaged. No scanner
exception or broad copied source-directory scan was added.

Generated schema remains ignored/untracked, never force-added. Current SHA256:
`18fa5c129537eb3678664b22f306a917fe20fef264570d65e75bdc44b215a28e`.
No authentication gate occurred; the stopped-store hold was superseded by
root's explicit preserved-start authorization.

## Self-Check: PASSED

All14 approved artifacts exist, including ignored generated schema. RED
`bc5acbe` exists and precedes the uncommitted implementation. Private actual
exits/failures and browser setup logs exist; no browser results file exists.
This self-check confirms the incomplete handoff's artifacts and claims, not
Task2 completion. Shared tracking and user dirt remain untouched.
