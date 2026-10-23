# 04-06 ownership amendment A1 — independent check

Decision: **APPROVE / PASS**. Zero blockers; zero warnings.

This approves the bounded planning amendment at proposal `65e7d93` against
accepted baseline `74578e9`. It is not runtime acceptance, a source review of an
implementation, permission to skip RED, or whole-phase verification.

## Scope checked

Read the complete original 04-06 PLAN, proposed OWNERSHIP-A1, shared CONTRACT,
approved UI-SPEC, prior 04-05 SUMMARY and phase pattern map. Inspected current
client/session/App, query alerts/cursor and cursor tests, query-api wiring,
legacy event handler and native event/detail adapters to assess whether the
declared arrangement can work. No root AGENTS.md or project .codex/skills or
.agents/skills directory was present. The proposal diff contains only the
ownership amendment; no implementation evidence is inferred from it.

The additional path is causally necessary: `web/src/api/client.ts` currently
returns only session, alerts and alertDetail. Session context exports that
guarded client, and its APIClient type is derived from the factory return type.
Adding generated EventFilters/EventPage and events(filters, signal) there
allows Events to use the established authority boundary without changing
session context or exposing a credential. Existing consumers need no new
dependency or alternate fetch path.

The resulting declared envelope is exactly eleven paths:

- backend/internal/query/events.go
- backend/internal/storage/clickhouse/query_events.go
- backend/internal/query/cursor_test.go
- backend/testphase4/events_test.go
- backend/cmd/query-api/main.go
- api/openapi.yaml
- web/src/api/schema.d.ts (existing ignored generated artifact)
- web/src/api/client.ts
- web/src/pages/Events.tsx
- web/src/pages/Events.test.tsx
- web/src/App.tsx

This is an explicit bounded exception to the usual file-count target: ten
tracked paths plus one derivative generated declaration, the original two
feature tasks, and the unchanged <=45% implementation-context target. The
amendment supplies the missing client wiring rather than adding another
feature. No additional source path is demonstrably unavoidable at planning
time. If implementation proves otherwise, its stop-and-redeclare rule applies.

## Architectural assessment

The backend arrangement is cohesive and feasible without hiding an unavoidable
shared edit. Go permits new methods on the package-local CursorCodec in the
new query/events.go; that file can access its existing AEAD while defining
event-specific filters, UUID positions and authenticated payload interpretation.
The existing alert payload and Position are unsuitable for events, and the
amendment explicitly forbids overloading them. Distinct authenticated event
interpretation, including an event domain/AAD or equivalently disjoint
authenticated payload discrimination, can reject cross-type tokens without
changing cursor.go or current alert behavior. Endpoint, verified tenant,
normalized filters, frozen range, position validity and size bounds remain
required; sharing an AEAD does not relax those checks.

A focused event-list handler/registrar in owned query/events.go can receive its
real lister, verifier and codec directly from owned main.go. It can reuse the
existing principal/error/time/page primitives without changing the alert
Dependencies or Register method. Current legacy registration owns only
GET /api/v1/events/:event_id; a single bearer-protected GET /api/v1/events is
distinct. The current anti-downgrade middleware covers the slash-prefixed
single-event path, so preserving it and the legacy registrar preserves API-key
lookup and invalid-bearer non-downgrade. No edit to alerts.go, cursor.go or the
legacy handler is required by this increment.

The new native adapter can implement event listing using the existing Conn and
canonical Event representation within query_events.go. The amendment preserves
native Named parameters and tenant predicates, identity deduplication before
filters/order/limit, earliest received_at compatibility with current Get, and
deterministic tie handling. It does not require modifying canonical Event,
existing Get or an existing migration. Actual SQL correctness, immediate
duplicate visibility and full stable pages must still be proved by the native
Events family.

## Gates retained

The amendment keeps all original bounds, half-open frozen ranges, cursor
authentication and strict query rejection. The guarded client must retain
generation/lifetime checks before credentials/network and after parsing,
combined cancellation, current-session401 revocation and finite response
validation. Owned Events tests can exercise these behaviors through the real
client; no session-test ownership expansion is inherently necessary.

The labelled Events UI, approved states, safe rendering,300ms debounce,
cancellation, bounded previous-page history and App navigation remain in the
original feature. Event detail remains the explicitly assigned07 refinement.
The existing64web tests and all affected Auth/Alerts/cursor/UTC/session/detail,
OpenAPI/drift, static, browser and local acceptance gates remain required.
There is no permission to weaken selectors, caps, assertions or native proof.

No tests, builds, application execution, migrations, services, commits or remote
operations were performed for this check. Only this review file was written;
user configuration, .serena and root tracking were untouched. Implementation
still requires causal RED, minimal GREEN, independent source review and root
acceptance. Publication remains subject to04-16's explicit human action gate.

```yaml
status: passed
decision: approve
blockers: 0
warnings: 0
issues: []
```
