# Phase 4 — Pattern map

Root inline mapping from directly inspected source,2026-10-21. Stale pre-Phase1codebase map not authoritative. File names below are proposed new responsibilities, not existing implementations.

| Proposed area / files | Existing closest analog | Concrete contract to retain |
|---|---|---|
| `backend/internal/auth/` and `backend/cmd/dev-jwt/` | `internal/apikeys/verifier.go`, `internal/eventquery/handler.go` | Verify before extracting principal; typed auth/unavailable failures, no suppliedsecretlogs |
| `backend/internal/query/` handlers | `internal/eventquery/handler.go` | Echo handlers return existing errs values and tenant-scoped JSON; constructor injected store |
| `backend/internal/query/` page/cursor validation | `internal/contracts/jsonkeys.go`, `internal/validation/` | Strictinputvalidation,knownfields,boundedtypes; don'tduplicatecanonicalAlert/Event |
| CH event list/summary | `internal/storage/clickhouse/events.go` | Native `driverclickhouse.Named` parameters, bounded context, visibleuniqueness notphysicalrows |
| PG list/detail/triage | `internal/storage/postgres/alerts.go` | `pgxpool`, timeout5s, rollback1s; tenantpredicate and errorpropagation |
| Audit/delivery migration | `backend/migrations/postgres/003_alert_workflows.sql`,006reservations | AdditiveternSQL with create/dropseparator; preserveexistingidentity/immutability |
| `backend/internal/alertstream/` | `internal/app/app.go`, `internal/opshttp/server.go` | Cancelablelifecycle, boundedwork/drain, noleakedgoroutines; useexistingpgx |
| query-api wiring | `backend/cmd/query-api/main.go` | `app.Run(config.ServiceQueryAPI,setup)`, lifecycleclosers, realdeps, middleware.Apply |
| `web/src/auth/`,api/hooks/pages/components | `web/src/App.tsx`,main,test/setup,api/types | React19/Vite, TSstrict, generatedOpenAPItypes, existingVitest; nativeHTMLandCSS |
| Native Phase4 tests | `backend/test/*_test.go` | Realstack,uniquetestidentities,actualmigrations,boundednonemptyrunner |
| Phase4guards/CI | `scripts/check-phase3-tests.sh`, `scripts/test_phase3_guards.py` | Independentexactfamilies,empty/failurepropagate,closedshards,no capweakening |

## Inspected code excerpts

Existing query-api wiring:

```go
e := echo.New()
middleware.Apply(e, &log, cfg.Server.CORSAllowedOrigins)
eventquery.NewHandler(verifier, storageclickhouse.NewEventStore(clickhouseConn)).Register(e)
```

Preserve existing API-key single-event route compatibility while adding explicit dashboard bearer identity; never ambiguousfallback for badbearer.

Existing event lookup:

```go
event, err := h.store.Get(c.Request().Context(), principal.TenantID, eventID)
```

Tenant comes from authenticated principal, not URL/body. New list/evidence/summary/backfill must keep that property on every underlying query.

Native CH parameter binding:

```go
driverclickhouse.Named("tenant", tenantID),
driverclickhouse.Named("id", eventID),
```

Existing PG workflow insertion:

```sql
ON CONFLICT (alert_id) DO NOTHING
```

New triage updates are a separateauthorizedpath; replaymustneverresetstatus/version/audit/seq. Newdeliverytrigger observes onlynewinsert, migrationmustqueuepreexistingrowswithoutmutatingthem.

## Cross-cutting traps

Workflowseqisnotcommitorder(ADR0008); do notcopysimplemaxseqreplay. CHcanonicalAlertisimmutableandstrictschema; introduceworkflowDTO/deliveryenvelopeoutsideit. ExistingHTTPWriteTimeoutmustbeinspectedforSSE. Testswithnewfamiliesneedexplicitbuildtag/packageisolationtokeepoldaggregatecaps. NeverreuselegacyClerk/ts-restsuggestionsfromstaleCLAUDEsections.
