# 04-06 ownership amendment A1 — guarded typed event search

Status: PROPOSED; independent approval required before implementation.

At accepted baseline74578e9, the executor stopped before any source/test edit:
the current APIClient returns only session, alerts and alertDetail. Session
context supplies that client without exposing credentials. The approved Events
page cannot call a guarded generated event search method that does not exist.

Add exactly one path: `web/src/api/client.ts`. Its only new responsibility is
generated EventFilters/EventPage types and `events(filters, signal)` using the
existing authenticated transport. Preserve current generation and lifetime
checks before credentials/network and after body parsing, caller plus session
cancellation, current-session401 revocation and finite response validation.
No credential enters public context, query keys, URLs, storage or logs. A
standalone fetch or an unguarded generic transport is not an alternative.

The full envelope becomes eleven declared paths (ten tracked plus the existing
ignored generated schema), two original feature tasks and <=45% implementation
context target. No extra feature, dependency, framework, migration or index.
No source edits, tests or RED/GREEN results preceded this proposal.

## Backend arrangement requiring independent assessment

The existing shared CursorCodec holds the authenticated AEAD but its public
Encode/Decode payload is AlertFilters plus a SHA256 alert-ID position. Event
filters and UUID positions must not be squeezed into those alert fields.
The new owned query/events.go may define cohesive event filter/position/lister
types, event-specific methods on the same CursorCodec and a focused bearer
event-list registrar called by owned main.go. Exact endpoint/tenant/filter/
frozen-range binding and strict bounded decoding remain required. Separate
authenticated payload interpretation must prevent alert/event token reuse;
all current alert codec behavior remains unchanged. Reuse genuine shared
primitives, not copy-pasted unrelated handler code or hidden unowned edits.

This is a proposed arrangement, not a mandate to hide shared changes in an
owned file. If clean implementation actually requires cursor.go, alerts.go or
any other path, stop before editing and declare another causal checkpoint.
No duplicate Echo route registration; legacy API-key single-event lookup and
invalid-bearer non-downgrade remain unchanged by this event-list increment.

## Unchanged acceptance gates

- Verified tenant predicates and native parameterization; dedupe tenant+event
  identity before filters/order/limit; deterministic retained representation
  matching existing Get received_at behavior, with timestamp ties tested.
- Half-open frozen range24h/default7d/max, limit50/default200/max, stable
  millisecond tuple pagination, cursor<=2KiB and five-second query cap.
- Exact nonempty phase4-only native Events family: immediate duplicate CH
  inserts without OPTIMIZE/merge waits, full unique pages, ties, half-open
  bounds, IPv4/IPv6/type/outcome and cross-tenant/invalid-query negatives.
- Labelled safe Events UI,300ms debounce, cancellation and bounded navigation;
  detail dialog remains07. Existing64web, Auth/Alerts/cursor/UTC/session/detail
  regressions, OpenAPI/drift, static quality, actual browser and local gates
  remain required. No test/selector/cap weakening or mock-only acceptance.
- Root owns shared tracking/reviews. Preserve userconfig/.serena. No source
  path beyond the eleven, remote action, hosted CI, destructive operation or
  Phase5 advancement is authorized.04-16publication still needs explicit human
  action approval. Independent approval here is planning only; causal RED,
  minimal GREEN, source review and root acceptance still follow.
