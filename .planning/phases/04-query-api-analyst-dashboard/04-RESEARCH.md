---
phase: 04-query-api-analyst-dashboard
status: researched
date: 2026-10-21
mode: mvp
---

# Phase 4 — Implementation Research

## Scope and provenance

As an analyst, I want to investigate and triage my tenant's detections in a live dashboard, so that I can act on evidence without losing alerts during reconnects.

Sources of required behavior: `spec.md` §§7–10,14–15; `.planning/REQUIREMENTS.md`; Phase4 in `.planning/ROADMAP.md`; ADR0006 and ADR0008. All20 requirements AUTH01–04, API01–08, LIVE01–03, UI01–04/UI06 remain mandatory. No Phase4 CONTEXT exists. Engineering recommendations below are autonomous defaults, not invented user-locked decisions. User requested `4 --auto`; no additional product preferences were supplied.

Specialist research reported findings but failed to persist a report; root stopped that agent and compiled this artifact from its findings plus direct source inspection and official documentation. No implementation, installation or live acceptance occurred during research. Planning artifacts are not runtime evidence.

## Standard Stack

- Preserve `backend/` Go module, actual Go1.26.9 toolchain, Echo4.16.0, pgx5.11.0, native ClickHouse client and existing lifecycle/config/error middleware. `CLAUDE.md` and `.planning/codebase` include stale pre-Phase1 boilerplate: do not restore Clerk/asynq/NewRelic/ts-rest or migrate frameworks.
- Preserve `web/` React19.3.0, Vite8.3.4, TypeScript6.0.3, Vitest5.0.3 and openapi-typescript7.13.0. Generate request/response types from `api/openapi.yaml`; do not hand-maintain parallel types.
- Minimal additions proposed: `github.com/golang-jwt/jwt/v5` v5.3.1; `@tanstack/react-query`5.104.5; `openapi-fetch`0.17.0; `@microsoft/fetch-event-source`2.0.1; `@playwright/test`1.64.1. Specialist reported registry/official-doc checks, but execution must independently resolve exact packages and inspect lockfile changes before installing. No unrelated upgrades.
- Native HTML/CSS components and system fonts are sufficient. No shadcn initialization, registry code, hosted fonts, external auth provider or chart framework is required. Use accessible SVG/table charts from real summary values.

## Existing boundaries and concrete analogs

| Area | Existing source | Extend rather than replace |
|---|---|---|
| HTTP lifecycle | `backend/cmd/query-api/main.go`, `backend/internal/httpx/httpx.go`, `backend/internal/middleware/` | Existing Echo router, bounded HTTP settings, structured errors, drain ordering |
| Event identity reads | `backend/internal/eventquery/handler.go`, `backend/internal/storage/clickhouse/events.go` | Current GET event route authenticates read API keys; preserve producer-seed compatibility |
| Event storage | `backend/migrations/clickhouse/002_events.sql` | Visible dedupe before pagination and aggregates, never rely on merges |
| Alert authority | `backend/internal/storage/postgres/alerts.go`, `backend/migrations/postgres/003_alert_workflows.sql` | PG list/status/version/seq; CH immutable evidence; insert-only replay |
| Immutable protection | `backend/internal/alertwriter/`, PG006 reservations | Do not weaken permanent pre-CH reservation or change canonical detections |
| Contract | `api/openapi.yaml`, `backend/test/openapi*_test.go`, `web/src/api/types.ts` | Exact canonical Event/Alert schema drift tests remain authoritative |
| UI | `web/src/App.tsx`, `web/src/main.tsx`, `web/src/test/setup.ts` | Currently a placeholder, no existing design tokens/router/component library |
| Native tests | `backend/test/`, `scripts/go-test-nonempty.sh`, `scripts/check-phase3-tests.sh` | Explicit nonempty selection and immutable existing family budgets |

## Authentication and browser boundary

Recommend header JWT, not cookies: existing OpenAPI already specifies bearer auth and browser SSE must never put tokens in URLs. Use a verified JWT-only dashboard identity, plus the existing explicit API-key branch ONLY on legacy single-event lookup. Never downgrade a present invalid bearer token to an API key. Producer keys must not gain PATCH, SSE or dashboard-wide access.

Development sign-in: operator invokes a local dev-only CLI to mint a short-lived JWT, then pastes it into a masked sign-in field. No network token-mint endpoint, built-in shared credential, hard-coded demo secret or client role/tenant chooser. Store bearer only in memory; reload requires sign-in. This is explicitly development authentication, not a production identity provider. CLI must refuse production configuration; production verification may accept externally issued matching tokens but cannot enable dev minting.

Proposed verifier contract: allow only HS256 with a random >=32-byte configured secret; require issuer `sentinel-dev`, audience `sentinel-dashboard`, nonempty `sub`, valid tenant slug, known single role, `exp`, `iat`, `nbf`; lifetime <=15minutes, clock leeway30seconds, token<=8KiB. Verify signature/algorithm/claims before extracting tenant or role. Do not log headers/tokens/claims payload. CLI reads signing key from private env/file, never argv or git; stdout is an explicitly secret token output, excluded from recorded logs.

Add authenticated `GET /api/v1/session` returning only verified subject/tenant/role/expiry for browser bootstrap. Reads allow viewer/analyst/admin; triage allows analyst/admin only. Admin management endpoints remain v2, not a reason to add config CRUD. Unauthorized401, role403, valid but foreign ID404 to avoid existence disclosure. SQL always derives tenant from the verified principal; conflicting tenant query/body fields are rejected, not trusted.

Use fetch-event-source with Authorization and AbortController. Narrow exact-origin CORS, no credential cookies, no wildcard origin; deployment through same-origin proxy is preferred. Log only redacted security outcomes/request IDs. Audit AUTH04 includes existing producer authentication denial paths as well as role-sensitive denial/transition paths; never include supplied credentials or request bodies.

## Bounded investigation and triage

- Events: UTC half-open range, default last24h/max7days (bounded by retained event data), limit default50/max200. Whitelist source IP/type/outcome; parse IPv4/IPv6 canonically. Deduplicate by tenant+event_id before filtering/order/limit. Stable descending `(occurred_at,event_id)` with millisecond timestamp precision and strict tuple cursor. Do not change original canonical schema to carry workflow metadata.
- Cursor is versioned, length<=2KiB, bound to endpoint, verified tenant and normalized filters/range. Validate complete shape/types; tampering/filter changes/overflow return400. Time defaults freeze in cursor rather than moving between pages. Encode opaque tuples, not offsets. Parameterized filters and literal whitelisted SQL fragments only; limit+1 for `has_more`.
- Alerts: list PG `alert_workflows` directly, applying tenant/status/severity/rule/time/search BEFORE order and limit+1. No CH pre-page followed by PG status filtering. Keep canonical immutable Alert separate from a new workflow list DTO (`detection` or immutable fields + status/assignee/version). Existing OpenAPI AlertList currently uses canonical Alert and requires an explicit additive DTO correction, regenerated TS, fixtures and drift tests.
- Details: validate ID; tenant-scoped PG row first; fetch tenant-scoped immutable CH alert and <=20 unique sample events. Event TTL may remove evidence: report unavailable IDs/retention explicitly, not invented evidence or unsafe cross-tenant fallback. Explain the recorded rule_version, not silently current parameters. Existing rule snapshots/config registry must be inspected for historic lookup; unsupported historic versions are clearly labelled unavailable. Audit history bounded/paginated or, with forward-only lifecycle, its maximum two transitions proven.
- PATCH: strict unknown/duplicate field handling and bounded body; only `open→acknowledged→resolved`, no reopen/skip, version positive safe integer. `UPDATE ... WHERE tenant_id=$1 AND alert_id=$2 AND version=$3 AND status=$4` plus audit insert in ONE PG transaction, actor from identity, server time, version+1. Failed audit or stale version rolls back both. Conflict409 must not overwrite another analyst's work; UI refetches and asks for action again. Concurrent clients yield exactly one winner/audit row. Return workflow result independent of a subsequent optional CH fetch failure; no claimed rollback after positive commit.
- GET rules returns startup-loaded active definitions, read-only. GET summary uses deduped real CH events and authoritative PG workflows, defined last60s event rate, open severity counts, top20 rules and bounded24h hourly/5minute trend. Define alert count semantics independent of mutable status and tenant scope. Do not sum physical duplicates. Individual dependency failure returns503, not a fabricated zero.

## Gap-safe live delivery decision

ADR0008 states workflow `seq` is allocation order, NOT commit order. A transaction can allocate41, stall, then commit after42 was delivered; `workflow.seq > 42` permanently loses41. Neither LISTEN nor periodic max(seq) scans fixes that.

Two viable choices:

1. Serialize every workflow insertion before identity allocation until commit. Simpler schema, but touches all writer paths, requires old-writer drain/cutover and couples source finalization globally. Does not repair old cursors/old transaction allocations by itself.
2. Recommended: additive durable pending→delivery-log projection; workflow seq remains immutable. Three new PG tables: `alert_delivery_pending(alert_id PK,tenant_id)`, singleton transactional counter, `alert_deliveries(delivery_seq bigint PK,alert_id UNIQUE,tenant_id)` with tenant+delivery index. New workflow AFTER INSERT trigger queues identity in the SAME transaction; migration queues all preexisting rows without a max(seq) shortcut. Projector locks counter row before selecting a bounded100 pending rows; inserts deliveries with transactional counter increments, deletes only successfully projected pending identities, updates counter and NOTIFYs a constant wakeup in the same bounded transaction. Lock held through commit prevents a later delivery number becoming visible before an earlier one. No CH/network calls while counter locked; duplicates are idempotent. Rollback consumes no committed delivery cursor.

This is a proposed Phase4 design requiring a new ADR, not an existing approved decision or a change to canonical Kafka Alert. Trigger/migration must preserve Phase3 insert-only semantics, work for already-running writers, and be proven on preexisting rows plus reversed commits. All query-api replicas may project using the same DB lock; each replica independently reads/broadcasts the shared durable log. Never use a competing Kafka consumer group for browser fan-out.

Expose `seq` in a REST/SSE **delivery envelope**, explicitly defined as `alert_deliveries.delivery_seq`, not `alert_workflows.seq`. Use decimal strings for all delivery cursor IDs to avoid JS bigint precision loss. Add `GET /api/v1/alerts/backfill?after_seq=<decimal>&limit=...` and SSE Last-Event-ID using that same cursor. Tenant is verified server-side; never advance a tenant cursor from a foreign payload. Existing list's event-time pagination is not a replay cursor. OpenAPI documents this distinction, finite limit and errors.

Replay: establish/reestablish dedicated pgx LISTEN connection, then read durable log; also poll every1s irrespective of notifications. LISTEN is an optimization only, never source of truth or readiness's sole recovery mechanism. Backoff on connection failure100ms..5s with cancellation. All clients fetch ascending tenant deliveries after their committed cursor; fill bounded pages, no startup global highwater skip. Initial browser attaches from0 (or a documented snapshot cursor proven atomically consistent), dedupes by alert_id, invalidates existing page cache rather than append forever. Persist cursor only after corresponding update processed, never on heartbeats; reconnect on any gap/error. Poll recovers lost NOTIFY, projector crashes and LISTEN disconnect. Cross-replica log readers are independent.

Server defaults: <=100 concurrent streams/replica, queue32 notifications/client, heartbeat15s, finite write deadline5s, max stream10min AND no later than token expiry, replay page<=200. A slow client is disconnected; durable replay repairs it. Register/shutdown/cancel must release sockets/listener goroutines. HTTP global WriteTimeout must not silently break streams; use route-aware ResponseController write deadlines, retain ordinary handler deadlines. Log grows durably as workflows already do: no new retention guarantee/GC in this phase, state capacity limit honestly; pending backlog must not be materialized in memory. Enforce process queues and bounded queries, not an unsupported fixed disk guarantee.

## UI architecture

See `04-UI-SPEC.md`. Four authenticated pages plus development sign-in; Overview consumer-lag data and System page explicitly deferredPhase5. Browser history/deep links and back/forward must work, including alert drawer state. Query keys include verified identity/session generation and every normalized filter/cursor; abort and clear ALL caches/streams on sign-out/expiry/switch. Never use decoded-but-unverified token claims as proof. React text/pre JSON rendering only; no dangerouslySetInnerHTML. Search300ms debounce, bounded pages, displayed data never unbounded concatenation. New live alerts indicate refresh/invalidate; do not silently change current filtered/paginated ordering.

## Validation Architecture

All tests below are planned, not run or passing. Frameworks already exist except pinned Playwright. Add tests alongside each vertical slice, not a final testing-only foundation.

| Capability | Exact planned family | Level / causal assertion |
|---|---|---|
| Sign-in/roles | `TestPhase4Auth`, `web/src/auth/session.test.tsx` | Native HTTP malformed/alg/expiry/issuer/aud/role tests; memory-only browser session; viewer403 |
| Event investigation | `TestPhase4Events` | Real CH duplicate insert before merges, tuple-tie pagination, bounds and foreign-ID404 |
| Alert list/detail | `TestPhase4Alerts` | Real PG sparse status data yields full pages; CH evidence retention and historical explanation |
| Triage | `TestPhase4Triage` | Real PG concurrent stale updates and injected audit failure atomically preserve row/audit |
| Summary/rules | `TestPhase4SummaryRules` | Exact seeded deduped counts, tenant isolation, real dependency failure not zeros |
| Delivery projection | `TestPhase4Delivery` | Native controlled reversed workflow commits, transactional counter rollback, preexisting seed, projector restart |
| Streams | `TestPhase4Stream` | Two real query-api replicas, native killed LISTEN link, missed NOTIFY, snapshot handoff, slow client bounds |
| Isolation matrix | `TestPhase4TenantMatrix` | Every list/detail/filter/cursor/PATCH/evidence/stats/rules/session/backfill/SSE with two identities; no foreign payload |
| Browser acceptance | `web/e2e/phase4.spec.ts` | Real seeded stack and buffering-off proxy; no route mocks; disconnect/reconnect/expiry,keyboard,roles,JSON injection |

Quick: `cd backend && go test -race -timeout 60s ./internal/auth/... ./internal/query/... ./internal/alertstream/...`; only invoke packages once created. Browser/component feedback `cd web && npm test` bounded120s; `npm run typecheck && npm run lint && npm run build`.

Add `scripts/check-phase4-tests.sh` using existing nonempty-runner conventions and exact top-level selectors above, each independently proven present; ordinary groups cap6m, delivery/stream10m, explicit bounded browser10m. New selectors must not silently enter existing32m full suite beyond budget; measure and decide an explicit test-tag/package boundary before final CI wiring, NEVER weaken old caps/skip old families. Preserve `make test-integ`, all7Phase2 nonempty groups, exact44+2Phase3 groups and old45min job caps. Add independent jobs/shards if needed and prove complete disjoint selector union. Orchestration MODEL tests are not native acceptance.

Every slice needs tests-first behavioral RED, scoped tests-only commit where appropriate, minimal GREEN and real production wiring. Native suites must execute actual migrations (not merely compile). Migrations additive/idempotent; no reset/drop/OPTIMIZE/volume deletion. Private evidence logs record SHA, command,start/end,real exit; shell/network failures are not assertion results. No pushes/hosted runs during planning; later execution authority must be checked from this phase's plan, not assumed from Phase3 authorization.

## Recommended decomposition and review focus

Plan vertical increments: sign-in→own alert list; alert filtering/detail; event search/detail; triage+history; rules+summary; live delivery end-to-end; recovery/proxy/tenant hardening. Each task has real browser→API→store behavior or refines that slice. No standalone schema/API/UI layer cake. Phase4 is not new-project Walking Skeleton.

Review likely silent failures: JS bigint cursor truncation; stale cross-tenant cache after session change; status-filter short pages; delayed lower workflow seq; valid PATCH commit followed by response enrichment failure; duplicate conflicting JWT credentials; historic rule-version drift; evidence TTL; SQL wildcard search; stream token expiry and proxy buffering. Include these tests in owners' tasks, not only prose.

Security enforcement enabled absent explicit false: every PLAN needs threat model (attacker input/trust boundary/mitigation/observable negative test), parameterized tenant predicates, safe logs, bounded resources, JWT verification and DOM-safe JSON. ASVS identifiers must use a declared edition; do not claim certification. High threats cannot be silently accepted under --auto.

## Primary sources checked

- [PostgreSQL sequence behavior](https://www.postgresql.org/docs/current/functions-sequence.html): sequence operations do not provide commit-order replay guarantees. Gap-safe projection above is an engineering inference/design, not a PostgreSQL feature claim.
- [PostgreSQL NOTIFY](https://www.postgresql.org/docs/current/sql-notify.html): transaction delivery/coalescing and small payloads; durable table remains authoritative.
- [pgx Conn/WaitForNotification](https://pkg.go.dev/github.com/jackc/pgx/v5#Conn.WaitForNotification): dedicated connection, caller cancellation; add application reconnect/re-LISTEN/poll.
- [JWT parser options](https://golang-jwt.github.io/jwt/usage/parse/): explicit accepted methods, issuer/audience/time checks. App-specific tenant/role/lifetime bounds remain our responsibility.
- [Fetch Event Source](https://github.com/Azure/fetch-event-source): header-bearing fetch streams and application-controlled retry. Browser native EventSource cannot supply arbitrary Authorization headers.
- [TanStack query keys](https://tanstack.com/query/latest/docs/framework/react/guides/query-keys): cache keys describe data dependencies; include session/filter isolation.
- [openapi-fetch](https://openapi-ts.dev/openapi-fetch/): generated-schema typed client; runtime trust validation still required at server boundaries.

No benchmark, memory-bound proof, live proxy acceptance or full-native Phase4 pass is claimed by this report.
