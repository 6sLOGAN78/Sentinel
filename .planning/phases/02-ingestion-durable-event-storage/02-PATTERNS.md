# Phase 2: Ingestion & Durable Event Storage - Pattern Map

**Mapped:** 2026-10-13
**Files classified:** 80 proposed paths (including generated files and inferred implementation splits)
**Analogs found:** 71 / 80; 47 exact role/data-flow matches, 24 role matches, 9 without a close implementation analog.

## Scope and Authority

Read current Phase 2 CONTEXT/RESEARCH, PROJECT.md, CLAUDE.md and live backend/generator/deployment sources. No applicable AGENTS.md or project skills directories were found. CLAUDE.md's generated pre-Phase-1 stack sections are stale: there is no Server dependency container, handler/service/repository scaffold, Clerk, Asynq or New Relic in the current application. Use explicit dependencies, the five cmd entrypoints, app.Lifecycle, httpx, contracts and middleware.Apply.

Paths below expand the research's package-level proposals into a concrete suggested split. They are planning candidates, not a requirement to create every file separately; co-locate closely related small types when clearer. eventwriter and query are inferred packages to keep cmd files wiring-only. scripts/seed.sh is an inferred home for the multi-step seed workflow. Existing contracts/event.go, migrations/embed.go, middleware/chain.go and generator/events.py are reusable assets; they need no change merely to reuse them. Deferred metrics, JWT, dashboard/list routes and detection are excluded.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog (path:lines) | Match Quality | Assignment |
|---|---|---|---|---|---|
| `backend/cmd/ingest/main.go` | service | request-response | `backend/cmd/query-api/main.go:21-44` | exact | P1 |
| `backend/cmd/event-writer/main.go` | service | streaming | `backend/internal/app/app.go:94-144` | role-match | P1/P6 |
| `backend/cmd/query-api/main.go` | service | request-response | `backend/cmd/query-api/main.go:21-44` | exact | P1/P2 |
| `backend/cmd/detector/main.go` | service | request-response | `backend/cmd/detector/main.go:14-25` | exact | P1 |
| `backend/cmd/alert-writer/main.go` | service | request-response | `backend/cmd/alert-writer/main.go:20-44` | exact | P1 |
| `backend/internal/app/app.go` | utility | event-driven | `backend/internal/app/app.go:108-160` | exact | P1 |
| `backend/internal/app/app_test.go` | test | event-driven | `backend/internal/app/app_test.go:87-159` | exact | P1 |
| `backend/internal/config/config.go` | config | transform | `backend/internal/config/config.go:128-145,227-313` | exact | P3 |
| `backend/internal/config/config_test.go` | test | transform | `backend/internal/config/config_test.go:51-84,139-155` | exact | P3 |
| `backend/internal/httpx/httpx.go` | utility | request-response | `backend/internal/httpx/httpx.go:29-43,58-96` | exact | P1 |
| `backend/internal/httpx/httpx_test.go` | test | request-response | `backend/internal/httpx/httpx_test.go:36-76` | exact | P1 |
| `backend/internal/middleware/global.go` | middleware | request-response | `backend/internal/middleware/global.go:26-87,136-154` | exact | P2 |
| `backend/internal/middleware/middleware_test.go` | test | request-response | `backend/internal/middleware/middleware_test.go:27-55,123-199` | exact | P2 |
| `backend/internal/errs/http.go` | model | transform | `backend/internal/errs/http.go:9-13,30-63` | exact | P2 |
| `backend/internal/errs/errs_test.go` | test | transform | `backend/internal/errs/errs_test.go:15-31` | exact | P2 |
| `backend/internal/apikeys/keys.go` | utility | transform | `none` | none | P4 |
| `backend/internal/apikeys/store.go` | service | CRUD | `backend/internal/database/database.go:54-57; backend/test/migrate_test.go:87-90` | role-match | P4 |
| `backend/internal/apikeys/cache.go` | store | CRUD | `none` | none | P4 |
| `backend/internal/apikeys/verifier.go` | service | request-response | `none` | none | P4 |
| `backend/internal/apikeys/keys_test.go` | test | transform | `backend/internal/errs/errs_test.go:34-59` | role-match | P7 |
| `backend/internal/apikeys/verifier_test.go` | test | request-response | `backend/internal/middleware/middleware_test.go:27-55` | exact | P4/P7 |
| `backend/internal/ratelimit/ratelimit.go` | service | request-response | `backend/internal/redisx/redisx.go:60-79` | role-match | P5 |
| `backend/internal/ratelimit/ratelimit_test.go` | test | request-response | `backend/test/migrate_test.go:188-199` | exact | P5/P7 |
| `backend/internal/redisx/redisx.go` | utility | request-response | `backend/internal/redisx/redisx.go:24-57` | exact | P5 |
| `backend/internal/kafkax/client.go` | utility | streaming | `backend/internal/redisx/redisx.go:60-79` | role-match | P6 |
| `backend/internal/kafkax/producer.go` | service | streaming | `none` | none | P6 |
| `backend/internal/kafkax/consumer.go` | service | streaming | `none` | none | P6 |
| `backend/internal/kafkax/dlq.go` | utility | transform | `backend/internal/contracts/event.go:86-103` | role-match | P6 |
| `backend/internal/kafkax/kafkax_test.go` | test | streaming | `backend/test/lifecycle_test.go:88-121` | role-match | P6/P7 |
| `backend/internal/storage/clickhouse/client.go` | utility | request-response | `backend/internal/redisx/redisx.go:60-79` | role-match | P6 |
| `backend/internal/storage/clickhouse/events.go` | service | batch | `none` | none | P6 |
| `backend/internal/storage/clickhouse/events_test.go` | test | batch | `backend/test/migrate_test.go:134-161` | role-match | P6/P7 |
| `backend/internal/eventwriter/writer.go` | service | streaming | `none` | none | P6 |
| `backend/internal/eventwriter/writer_test.go` | test | streaming | `backend/internal/app/app_test.go:118-159` | role-match | P6/P7 |
| `backend/internal/opshttp/opshttp.go` | service | request-response | `backend/internal/httpx/httpx.go:29-43,58-96` | exact | P1 |
| `backend/internal/opshttp/opshttp_test.go` | test | request-response | `backend/internal/httpx/httpx_test.go:36-76` | exact | P1/P7 |
| `backend/internal/ingest/handler.go` | controller | request-response | `backend/internal/middleware/middleware_test.go:27-38; backend/internal/contracts/event.go:167-232` | role-match | P2 |
| `backend/internal/ingest/inflight.go` | middleware | request-response | `backend/internal/middleware/chain.go:10-28` | exact | P2 |
| `backend/internal/ingest/handler_test.go` | test | request-response | `backend/internal/middleware/middleware_test.go:27-68` | exact | P2/P7 |
| `backend/internal/query/handler.go` | controller | request-response | `backend/internal/middleware/middleware_test.go:27-38` | role-match | P2/P6 |
| `backend/internal/query/handler_test.go` | test | request-response | `backend/internal/middleware/middleware_test.go:41-68` | exact | P2/P7 |
| `backend/tools/keyctl/main.go` | utility | CRUD | `backend/tools/chmigrate/main.go:18-50` | role-match | P4 |
| `backend/tools/healthprobe/main.go` | utility | request-response | `backend/tools/chmigrate/main.go:18-23` | role-match | P1/P8 |
| `backend/tools/chmigrate/main.go` | utility | file-I/O | `backend/tools/chmigrate/main.go:25-50` | exact | P8 |
| `backend/internal/chmigrate/chmigrate.go` | service | file-I/O | `backend/internal/chmigrate/chmigrate.go:69-143,243-272` | exact | P8 |
| `backend/internal/chmigrate/chmigrate_test.go` | test | file-I/O | `backend/migrations/migrations_test.go:37-82` | role-match | P8 |
| `backend/migrations/postgres/002_producer_keys.sql` | migration | CRUD | `backend/migrations/postgres/001_baseline.sql:1-7` | exact | P8 |
| `backend/migrations/clickhouse/002_events.sql` | migration | batch | `backend/migrations/clickhouse/001_create_database.sql:1-5` | role-match | P8 |
| `backend/migrations/migrations_test.go` | test | file-I/O | `backend/migrations/migrations_test.go:13-82` | exact | P8 |
| `backend/internal/testing/containers.go` | utility | event-driven | `backend/internal/testing/containers.go:43-85,190-249` | exact | P7 |
| `backend/test/main_test.go` | test | event-driven | `backend/test/main_test.go:36-120` | exact | P7 |
| `backend/test/lifecycle_test.go` | test | event-driven | `backend/test/lifecycle_test.go:27-66,88-172` | exact | P7 |
| `backend/test/migrate_test.go` | test | CRUD | `backend/test/migrate_test.go:50-69` | exact | P7/P8 |
| `backend/test/ingest_test.go` | test | request-response | `backend/test/lifecycle_test.go:88-121; backend/internal/middleware/middleware_test.go:41-55` | exact | P7 |
| `backend/test/writer_test.go` | test | streaming | `backend/test/lifecycle_test.go:88-172` | role-match | P6/P7 |
| `backend/test/dedupe_test.go` | test | batch | `backend/test/migrate_test.go:134-161` | role-match | P6/P7 |
| `backend/test/ratelimit_test.go` | test | request-response | `backend/test/migrate_test.go:188-199` | exact | P5/P7 |
| `backend/test/readyz_test.go` | test | request-response | `backend/internal/httpx/httpx_test.go:36-63` | exact | P1/P7 |
| `backend/test/openapi_test.go` | test | transform | `backend/test/openapi_test.go:117-181` | exact | P2 |
| `api/openapi.yaml` | model | request-response | `api/openapi.yaml:100-129,512-528` | exact | P2 |
| `web/src/api/schema.d.ts (generated, ignored)` | model | transform | `web/package.json:12-15` | exact | P2 |
| `generator/src/sentinel_generator/cli.py` | utility | request-response | `none` | none | P9 |
| `generator/src/sentinel_generator/send.py` | service | batch | `generator/src/sentinel_generator/events.py:59-87` | role-match | P9 |
| `generator/tests/test_send.py` | test | batch | `generator/tests/test_events.py:27-31,82-89` | role-match | P9 |
| `generator/pyproject.toml` | config | transform | `generator/pyproject.toml:1-18,24-40` | exact | P9 |
| `generator/uv.lock` | config | transform | `generator/uv.lock (regenerate)` | exact | P9 |
| `generator/requirements.txt` | config | transform | `generator/requirements.txt:1-2` | exact | P9 |
| `generator/README.md` | config | file-I/O | `docs/decisions/0003-atomic-batch-semantics.md:11-23` | role-match | P9 |
| `Makefile` | config | batch | `Makefile:12-13,24-41,62-76` | exact | P10 |
| `scripts/seed.sh` | utility | batch | `scripts/go-test-nonempty.sh:1-29` | role-match | P10 |
| `deployments/compose/docker-compose.yml` | config | event-driven | `deployments/compose/docker-compose.yml:13-40,43-63,65-103` | exact | P10 |
| `deployments/docker/Dockerfile` | config | batch | `none` | none | P10 |
| `.dockerignore` | config | file-I/O | `.gitignore:56-67` | role-match | P10 |
| `.gitignore` | config | file-I/O | `.gitignore:56-67` | exact | P10 |
| `.env.example` | config | transform | `.env.example:13-58` | exact | P3/P10 |
| `.github/workflows/ci.yml` | config | batch | `.github/workflows/ci.yml:30-45,138-167` | exact | P10 |
| `docs/decisions/0003-atomic-batch-semantics.md` | config | file-I/O | `docs/decisions/0003-atomic-batch-semantics.md:11-23` | exact | P10 |
| `docs/architecture.md` | config | file-I/O | `docs/decisions/0002-event-id-policy.md:11-21` | role-match | P10 |
| `backend/go.mod` | config | transform | `backend/go.mod:1-25` | exact | P3 |
| `backend/go.sum` | config | transform | `backend/go.sum (regenerate)` | exact | P3 |

## Pattern Assignments

Assignments P1–P10 apply to every corresponding table row. Five primary pattern families suffice: lifecycle/server, Echo envelopes/tests, connection constructors/config, migration/container harness and seeded generator. Supporting files below establish exact integration contracts; no additional analog search is needed.

### P1 — Binaries, app failure propagation, httpx and opshttp

**Copy imports/wiring from:** backend/cmd/query-api/main.go:4-15,21-31. stdlib imports, third-party imports, then module-local imports separated by blank lines:

```go
import (
    "context"
    "fmt"
    "os"

    "github.com/rs/zerolog"

    "github.com/6sLOGAN78/sentinel/internal/app"
    "github.com/6sLOGAN78/sentinel/internal/config"
    "github.com/6sLOGAN78/sentinel/internal/database"
    "github.com/6sLOGAN78/sentinel/internal/redisx"
)
```

```go
db, err := database.New(ctx, cfg.Database, log)
if err != nil {
    return fmt.Errorf("postgres: %w", err)
}
lc.Add("postgres", func(context.Context) error {
    db.Close()
    return nil
})
```

Retain main's `os.Exit(app.Run(config.ServiceQueryAPI, setup))` (query-api:17-19); every setup must match the existing signature in app/app.go:94-95:

```go
type SetupFunc func(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *Lifecycle) error
```

**Lifecycle adaptation:** app/app.go:66-86 closes LIFO and joins errors; 156-160 creates one fresh 30-second shutdown context. Current RunContext waits only at `<-ctx.Done()` (131), then returns zero if closers succeed (143). A writer goroutine returning an error would otherwise leave the process running. Add a tested failure signal, such as Lifecycle.Fail backed by context.WithCancelCause; distinguish parent cancellation from worker failure and preserve nonzero exit even if cleanup succeeds. A goroutine must never call os.Exit.

Registration order should be ops closer, dependency closers, worker/public-server closer, then readiness-draining closer. Reverse shutdown marks unready first, stops intake/flushes, closes dependencies and closes ops last. Shutdown flush uses a fresh bounded context; canceled request/worker contexts cannot flush successfully. Register resources immediately after creation so partial startup cleans up.

**Server shape:** httpx/httpx.go:35-42 already centralizes timeouts:

```go
return &Server{srv: &http.Server{
    Addr:              ":" + port,
    Handler:           handler,
    ReadHeaderTimeout: readHeaderTimeout,
    ReadTimeout:       secondsOrDefault(cfg.ReadTimeout),
    WriteTimeout:      secondsOrDefault(cfg.WriteTimeout),
    IdleTimeout:       secondsOrDefault(cfg.IdleTimeout),
}}
```

Add BindAddress using net.JoinHostPort; preserve unset-address behavior and nil-safe Shutdown (87-96). Bind synchronously in setup, then call Serve in a supervised goroutine. httpx.New/Serve/Shutdown are existing signatures; opshttp's constructor is new, not an existing API. Keep httpx free of storage, Kafka and Echo dependencies (httpx_test.go:79-87).

opshttp should use a plain net/http mux, fixed-name context-aware checks, per-check timeouts and a bounded readiness cache. Kafka checks use protocol Ping, not app.DialTCP. detector/alert-writer only gain real dependency checks plus ops; their business workers remain Phase 3.

**Tests to copy:** app/app_test.go:87-159 tests RunContext with injected setup and started/done channels; 24-67 proves close order and joined errors. Do not parallelize tests using t.Setenv. httpx/httpx_test.go:39-55 demonstrates actual listening and shutdown:

```go
l, err := net.Listen("tcp", "127.0.0.1:0")
require.NoError(t, err)
s := httpx.New(config.ServerConfig{}, okHandler())
done := make(chan error, 1)
go func() { done <- s.Serve(l) }()
```

Add tests for bind conflicts, draining, fatal consumer failure, listener failure, real dependency outages and normal SIGTERM.

### P2 — Ingest/query handlers, middleware and error contracts

**Existing HTTP construction:** middleware/middleware_test.go:27-38 is the current concrete Echo route pattern:

```go
e := echo.New()
logger := zerolog.Nop()
middleware.Apply(e, &logger, []string{"https://example.test"})
e.GET("/slow", func(echo.Context) error {
    return errs.NewTooManyRequestsError("slow", 7)
})
```

Use middleware.Apply for both public services. Guard auth explicitly with the new verifier; there is **no existing authentication middleware to copy**. query uses key tenant plus events:read; ingest uses events:write. Public errors return errs types; storage causes are logged separately and sanitized.

**Existing response handling:** middleware/global.go:149-154:

```go
if httpErr.RetryAfter > 0 &&
    (httpErr.Status == http.StatusTooManyRequests || httpErr.Status == http.StatusServiceUnavailable) {
    c.Response().Header().Set(echo.HeaderRetryAfter, strconv.Itoa(httpErr.RetryAfter))
}
_ = c.JSON(httpErr.Status, httpErr.Response(GetRequestID(c)))
```

Existing constructors in errs/types.go:
- NewUnauthorizedError(message string, override bool)
- NewForbiddenError(message string, override bool)
- NewBadRequestError(message string, override bool, code *string, errors []FieldError, action *Action)
- NewNotFoundError(message string, override bool, code *string)
- NewPayloadTooLargeError(message string, override bool)
- NewTooManyRequestsError(message string, retryAfterSeconds int)
- NewServiceUnavailableError(message string, retryAfterSeconds int)

**Required contract repair:** errs/http.go:30-35 and 58-63 currently omit all field errors:

```go
type ErrorBody struct {
    Code      string `json:"code"`
    Message   string `json:"message"`
    RequestID string `json:"request_id"`
}
func (e *HTTPError) Response(requestID string) ErrorResponse {
    return ErrorResponse{Error: ErrorBody{
        Code: e.Code, Message: e.Message, RequestID: requestID,
    }}
}
```

D-07/D-27 require optional error.details. Add `Details []FieldError json:"details,omitempty"` to the wire model and map HTTPError.Errors; keep internal fields hidden. Existing FieldError uses **Field/Error**, not Field/Message (http.go:9-13). Adapt errs_test.go:15-31: it currently passes field errors and explicitly asserts they disappear. Ordinary errors must retain their three-field wire shape.

OpenAPI Error (api/openapi.yaml:512-528) has additionalProperties:false, so explicitly add optional details with maxItems:50 and field/error items. getEvent security (104-107) must contain separate ApiKeyAuth and BearerAuth alternatives (OR). TestOpenAPISecurity (openapi_test.go:138-150) currently asserts every non-batch public route uses BearerAuth only: add the getEvent case. Regenerate web types via `cd web && npm run gen:api`; schema.d.ts is ignored by .gitignore:72-73, so do not force-add it. Existing types.ts aliases the generated Error schema.

**Use validation implementation directly:** contracts/event.go:123-130,167-169,215-232:

```go
func DecodeEvent(r io.Reader) (*Event, error) {
    return strictDecode[Event](r, "event", eventFields())
}
func (e *Event) Validate(now time.Time, l Limits) error {
    checks := []func() error{
        e.checkIdentity, e.checkEnums, e.checkIPsAndPort, e.checkStrings,
        func() error { return e.checkTimes(now, l) },
        func() error { return e.checkAttributes(l) },
    }
    for _, c := range checks {
        if err := c(); err != nil {
            return err
        }
    }
    return nil
}
```

Validate returns the **first** error per event. Aggregate across events, use errors.As to obtain contracts.FieldError (Field/Message), translate to errs.FieldError, prefix `events[i].`, cap at 50. Strict DecodeEvent already rejects duplicate/unknown event fields and trailing data; do not replace with Echo Bind or runtime JSON Schema compilation. Shallow batch envelope decoding still needs strict unknown/trailing/duplicate-key handling of its own.

Overwrite received_at using contracts.NewTimestamp(time.Now()) once per batch; it truncates UTC to milliseconds (event.go:46-48). Writer validation must use persisted received_at.Time rather than current wall clock; otherwise replay after the skew window becomes poison.

**Locked-order conflict:** D-27's written precedence puts scope 403 after rate-limit 429, but its next sentence and research Pattern 2 place the scope guard beside authentication, before body/rate limiting. Research Pattern 2 also puts field validation before tenant mismatch, while resolved D-27 requires tenant 403 to beat field 400. Do not copy its numbered order blindly. Planner must write executable mixed-error cases: uniform bad-key 401; bounded body/count 413; malformed-envelope 400; quota 429; tenant mismatch 403 ahead of field-validation 400. Record how early missing-scope 403 is reconciled with the precedence wording. No unresolved user checkpoint is implied.

Use the research's escaped route constant `/api/v1/events\:batch` (Go raw string) and test literal colon, wrong suffix, encoded %3A and method mismatch through e.ServeHTTP. Load shedding is a route semaphore before expensive auth; it intentionally can return 503 before auth under saturation.

**Hardening adaptation:** global.go:27-30 passes empty origins to Echo (allow-all default); return a no-op CORS middleware when empty. global.go:36,79 logs full v.URI; use path-only v.URIPath instead. chain.go:36-45 already preserves a configured IP extractor and otherwise uses ExtractIPDirect; retain that behavior. Reuse middleware tests' synchronized log sink (123-140) to assert key, query and request bodies never leak.

### P3 — Config and pinned dependency files

**Copy:** config/config.go:128-145,159-192,227-313.

```go
type ContractsConfig struct {
    MaxAttrKeys  int           `koanf:"max_attr_keys" validate:"min=1"`
    MaxAttrBytes int           `koanf:"max_attr_bytes" validate:"min=1"`
    FutureSkew   time.Duration `koanf:"future_skew" validate:"min=0"`
    PastSkew     time.Duration `koanf:"past_skew" validate:"min=0"`
}
```

Use nested koanf tags, named numeric/duration constants and setDefaults. Service defaults must be selected from Load(service), because setDefaults currently receives **only** the koanf pointer (227), not the service. Add database to requiredSections[ingest]. Add new Ops/Ingest/Writer and rate-limit/Kafka fields from research Pattern 12, with service-specific validation and cross-field checks. NativeAddress is currently optional; make it required for native-client consumers without accidentally requiring irrelevant dependencies for detector.

Check batch-size/hard-cap relationships, positive flush/retry/check durations, producer caps, rate-limit capacity >= max batch and produce timeout below write timeout. Test invalid values via real config.Load. Existing setCompleteEnv (config_test.go:15-25) lacks NATIVE_ADDRESS and will need updating.

backend/go.mod:1-25 establishes Go 1.26.0 with toolchain go1.26.9. Add research-pinned franz-go v1.22.1, kadm v1.19.0 (test-only usage) and clickhouse-go v2.49.0, regenerate go.sum with go mod tidy. No boilerplate SDKs or new schema-validation runtime are needed.

### P4 — API keys, cache, verifier and keyctl

No current cryptographic key/verifier/cache analog exists. Use research Pattern 4 for behavior; use existing database constructors and tool skeleton only for project conventions.

database/database.go:54-57 exposes `Database.Pool *pgxpool.Pool`; pass that pool explicitly to the key store. Parameterized pgx example from test/migrate_test.go:87-90:

```go
require.NoError(t, admin.Pool.QueryRow(ctx,
    "SELECT format('CREATE ROLE special LOGIN PASSWORD %L', $1::text)", specialPassword).Scan(&stmt))
```

Copy the `$1` binding style, **not** that test's dynamic role-DDL workflow. All key CRUD SQL uses bound values. database's RedactingLogger strips query args and credentials (29-51); reuse database.New to preserve it.

Use SplitN(raw,"_",3), because the secret alphabet contains underscores; validate alphabet/length before lookup. Hash and compare every request, including cached IDs; unknown IDs use a dummy fixed-size hash comparison. Cache only positive lookup data, bounded by size and 30-second TTL; do not cache negative lookups or authentication decisions. Protect maps and copied scope/hash slices against races.

keyctl should copy tools/chmigrate/main.go:18-39:

```go
func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, "chmigrate:", err)
        os.Exit(1)
    }
}
```

Adapt the tool name and subcommands; do not add a service to SERVICES. create reveals the full key once; list excludes hashes/secrets; revoke persists revocation. Research's ensure command is a useful implementation of idempotent seed provisioning; preserve .local/ key files at mode 0600 and never echo their contents. CLI uses plain secret environment variables/PGPASSWORD, while service config uses dotted names. Tests must cover underscore-containing secrets, cached wrong secrets, expiry/revocation, capacity, zero negative caching, scopes and cross-tenant reads/writes.

### P5 — Redis token bucket and client timeout bounds

**Constructor analog:** redisx/redisx.go:62-79:

```go
opts, err := Options(cfg)
if err != nil {
    return nil, err
}
client := redis.NewClient(opts)
pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
defer cancel()
if err = client.Ping(pingCtx).Err(); err != nil {
    _ = client.Close()
    return nil, fmt.Errorf("pinging redis: %w", err)
}
return client, nil
```

Preserve Options' TLS/auth/CA setup (24-57). No Redis script implementation exists: use research Pattern 5, Redis TIME and one bucket key per public key_id. Hold redis.NewScript in a Limiter field rather than a package global. Bound client dial/retries plus per-call time; validate actual elapsed latency with Redis paused. Do not assume the existing five-second startup ping bounds request-time Redis operations. Extend redisx options/config if needed rather than constructing an insecure client in ratelimit.

Use integer Retry-After >=1; fail-closed yields sanitized 503; fail-open warns without secrets. Metrics are deferred. Reuse real Redis integration fixture in test/migrate_test.go:188-199 and cleanup patterns in P7; test concurrent atomic accounting and independent key IDs.

### P6 — Kafka, native ClickHouse and the event-writer

There is **no existing streaming pipeline or native ClickHouse persistence**. The TCP-only skeletons are wiring patterns, not durable adapters. Use research Patterns 3,6,8 for library signatures; connection constructors copy P5's create/ping/close-on-failure/error-wrap shape.

Canonical model contract from contracts/event.go:87-103 includes:
```go
ReceivedAt      *Timestamp     `json:"received_at,omitempty"`
SourceIP        *string        `json:"source_ip,omitempty"`
DestinationPort *int           `json:"destination_port,omitempty"`
Attributes      map[string]any `json:"attributes,omitempty"`
```

ClickHouse Nullable(UInt16) mapping needs explicit validated *int -> *uint16 conversion; decoding nullable values into zero-valued scalars loses nulls. Reconstruct schema_version when reading since the minimum table omits it, and decode attributes_json into Attributes.

Producer: one long-lived kgo client, AllISRAcks, default idempotency, AllowIdempotentProduceCancellation, bounded delivery/retries/buffers. Request ProduceSync must finish all results before 202. Headers schema_version/request_id and tenant|source_ip key derive from authenticated/validated event data. Any partial publish returns 503; retry retains event IDs.

Consumer: DisableAutoCommit + BlockRebalanceOnPoll, capped PollRecords/fetch memory; all successfully polled records remain owned until effects complete. Decode/validate -> DLQ ack for poison + successful insert for good records -> CommitRecords(last record per partition) -> AllowRebalance. Do not commit a later offset across an earlier failed effect. Stop polling while retrying. Fresh native batch per insert attempt; abort failed batches. Exhausted infrastructure retries exit nonzero; they never become poison.

Shutdown: stop polling, flush/commit with fresh bounded context, then CloseAllowingRebalance; plain kgo.Close can block with a held rebalance. LIFO ordering must keep producer/ClickHouse clients alive until the worker finishes.

Read-time correctness: lookup binds both tenant and event_id and selects one row; counts use uniqExact(event_id). FINAL dedupes sort keys, not arbitrary event_id across changed occurred_at/months. Do not enable non_replicated_deduplication_window or insert-token dedupe in Phase 2. Raw double-insert count should be 2N before merges; use a test arrangement that prevents an asynchronous merge from making that raw-count assertion flaky, while user-visible assertions never rely on background merges.

Research's ORDER BY received_at ASC LIMIT 1 chooses the earliest **currently retained** row. ReplacingMergeTree without a version may later retain a different row for equal sort keys; it also does not give deterministic ordering for equal millisecond received_at. This is enough for one visible ID, but is not proof of ADR 0002's immutable first-content-wins policy. Planner should explicitly distinguish duplicate visibility from conflicting-content winner semantics and choose/document/test the latter if claiming it.

### P7 — Unit and real-container integration tests

**Imports/helper alias:** backend/test/migrate_test.go:18-28 uses testify assert/require, zerolog and `sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"`. Unit packages are external *_test. Real integration files start with:
```go
//go:build integration

package test_test
```

**Existing container lifecycle:** internal/testing/containers.go:260-268:

```go
func RequirePostgres(t *testing.T) config.DatabaseConfig {
    t.Helper()
    cfg, stop, err := StartPostgres(context.Background())
    mustStart(t, err)
    t.Cleanup(stop)
    return cfg
}
```

Add handle-returning helpers for dedicated outage containers while keeping existing callers working. Shared TestMain fixtures (main_test.go:18-26,75-104) must not be paused by parallel tests. Export FreePort from containers.go:190-202. NativeAddress already exists in StartClickHouse's returned config (181-187); the missing piece is forwarding it to binary environments.

Update lifecycle_test.go serviceEnv (27-66): postgres for ingest, native CH addresses, unique ops/public ports, topics and writer group. Kafka auto-create is false (containers.go:228), so TestMain must create required unique topics and migrate schemas before service tests. Concurrent lifecycle writer processes must not accidentally share test traffic or rebalance the writer under test.

startProc (lifecycle_test.go:90-121) gives real subprocesses with isolated environment and t.Context cleanup. Reuse it for SIGKILL/restart and deadline assertions. For tests reading process logs before exit, replace the existing *bytes.Buffer with a synchronized sink; middleware_test.go:123-140 is the existing safe sink analog. Assert readiness rather than fixed sleeps for new integration workflows.

**Migration compatibility trap:** TestMigratePostgresIdempotent (migrate_test.go:62,69) hardcodes version 1. Adding 002 requires updating the assertion, preferably computing expected version from embedded files. Include this existing test in the migration task.

Ingest tests must consume real Kafka to prove zero publication on rejected batches. Writer tests verify committed group offsets as well as ClickHouse rows; a successful row count alone cannot prove commit-after-effects. Fault injection wraps a real publisher/insert adapter or pauses a dedicated real dependency. Required tests include duplicate inserts with differing occurred_at, DLQ publish failure without commit, insert retry exhaustion, kill/restart, Kafka timeout and readiness recovery.

### P8 — Migrations and chmigrate TTL

**Postgres analog:** migrations/postgres/001_baseline.sql:3-7:
```sql
SELECT 1;

---- create above / drop below ----

SELECT 1;
```

Keep the tern separator and provide a down section; use research's producer_keys DDL and constraints.

**ClickHouse convention:** migrations/clickhouse/001_create_database.sql:1-5 says later DDL must be unqualified. Existing chmigrate applies SQL with its configured database (chmigrate.go:130-135):
```go
for _, stmt := range SplitStatements(string(raw)) {
    if _, err = r.exec(ctx, stmt, r.database); err != nil {
        return fmt.Errorf("applying %s: %w", name, err)
    }
}
```

Use CREATE TABLE IF NOT EXISTS events, ReplacingMergeTree and fixed seven-day migration TTL; migrations/embed.go:9 already embeds both SQL globs. Native persistence belongs in storage/clickhouse, not chmigrate. Keep HTTP migration auth in request headers, as existing exec does.

TTL adjustment cannot call Runner.exec from tools/chmigrate because exec is unexported (243). Add a narrow exported validated Runner operation in internal/chmigrate, backed by existing exec, and test it with the package's HTTP fake-server pattern. Run it **even when no migrations were applied**: current tools/chmigrate/main.go:42-45 returns early. Validate integer days 1..3650 and database identifier before constructing the fixed ALTER; do not alter immutable migration history. Automatic MATERIALIZE TTL is out of scope.

healthprobe is a new minimal tool: copy main/run exit handling, add context/HTTP timeout and exit zero only for HTTP 200; distroless provides no curl.

### P9 — Generator sender, CLI, tests and lockfiles

**Builder analog:** generator/events.py:59-70:
```python
def build_event(
    rng: random.Random, tenant_id: str, index: int, base_time: datetime
) -> dict[str, object]:
    if base_time.tzinfo is None or base_time.utcoffset() is None:
        raise ValueError("base_time must be timezone-aware")
    occurred = (base_time + timedelta(seconds=index)).astimezone(UTC)
```

Reuse it; there is no current CLI/network sender. Anchor base_time = run_start - timedelta(seconds=count) so all events are in the past; preserve seeded RNG and reserved IP behavior. Reject/cap counts outside skew-safe range or choose a bounded-offset strategy for large runs. Build encoded batch bytes once and resend unchanged on retry. Distinguish logical sent/accepted totals from retry attempts. API keys come from env/file, never CLI flags/logs.

Add argparse entrypoint via [project.scripts], pinned httpx==0.28.1 and an injectable httpx.Client/MockTransport plus clock/sleep seams. Strict mypy and ruff rules are in pyproject.toml:24-40. Existing S311 exemption covers **only events.py**; adding seeded random.Random/backoff randomness in send.py requires an explicit documented handling rather than accidentally breaking lint.

**Test analog:** tests/test_events.py:27-31:
```python
def test_same_seed_is_byte_identical() -> None:
    first = build_event(random.Random(42), tenant_id="demo", index=0, base_time=BASE)
    second = build_event(random.Random(42), tenant_id="demo", index=0, base_time=BASE)
    assert first == second
    assert json.dumps(first, sort_keys=True) == json.dumps(second, sort_keys=True)
```

Extend with MockTransport tests for identical retry body, Retry-After, bounded attempts, authentication failures, transport errors, skew and totals. Generate uv.lock and use the exact export command from requirements.txt:1-2:
```sh
uv export --project generator --format requirements-txt --no-dev --no-hashes --no-emit-project -o generator/requirements.txt
```

### P10 — Compose, Make seed, packaging, CI and docs

**Copy deployment conventions:** compose/docker-compose.yml:65-72:
```yaml
clickhouse:
  image: clickhouse/clickhouse-server:26.3.47.6@sha256:55a55347b1b959ae51d40373901e81aa671d7fc9e6d05d0603d1e0ad3ac22c6d
  ports: ["127.0.0.1:18123:8123", "127.0.0.1:19000:9000"]
  environment:
    CLICKHOUSE_DB: sentinel
    CLICKHOUSE_USER: sentinel
    CLICKHOUSE_PASSWORD: ${SENTINEL_CLICKHOUSE_PASSWORD:?set in .env}
```

Use research Pattern 11 for the new Dockerfile (no existing Dockerfile analog). Existing infrastructure images remain pinned; app services use build only, quoted loopback publishes, service DNS internally, real ops readiness probe and restart policy. Put root .dockerignore before building; exclude .env anywhere and .local plus .git, caches and planning. All five service binaries and healthprobe are built; only ingest/event-writer/query-api containers run in Phase 2.

Makefile:12-13 provides literal dotenv_or_env for passwords. Preserve it; do not include/source the dotenv file or pass secrets in argv. Add migrate then app startup to dev-up after infra/topic-init. make run sets public and ops loopback binds. Keep bench's explicit failure placeholder.

seed must invoke keyctl ensure for demo/demo2, send exactly 1000 demo events over HTTP, then poll tenant-scoped uniqExact with the run's received_at boundary and spot-check IDs using the read API. Repeated runs must not mistake previous data for current success; use generator run metadata. Secret key headers should be passed using protected header files/env, not echoed command arguments. Fail on every shortfall; keep retry deadlines bounded. Shell script can copy scripts/go-test-nonempty.sh's set -euo pipefail and root resolution, while seed orchestration is new.

CI go job uses working-directory backend (ci.yml:21-23). Add banned-module inspection with robust pipeline failure handling (avoid inversion masking go list failure). Extend nonempty-test invocations with Phase 2 prefixes and integration tag; preserve existing defect coverage. Note make lint does **not** run integration-tag lint; CI does (ci.yml:34-39). Local verification must include that check explicitly when touching integration code.

Document cache revocation delay, literal-colon path, at-least-once/partial-publish retries, read-time dedupe and winner semantics, DLQ policy, TTL ALTER/MATERIALIZE distinction, local non-HA stack and seed/key CLI usage in existing docs/architecture.md and generator/README.md. ADR 0003's speculative route note (17) should become the verified escaped-colon behavior.

## Shared Patterns

- **Explicit dependencies:** pass pool/client/verifier/publisher interfaces into small constructors. No resurrected server.Server container; wiring belongs in cmd setup.
- **Errors and logging:** fmt.Errorf("operation: %w", err) internally; errs constructors at HTTP boundaries; fixed sanitized readiness/DLQ reasons. Use middleware.GetLogger/GetRequestID in request work and zerolog passed to services. Never log bodies, keys, query strings or bound SQL arguments.
- **Validation authority:** contracts.DecodeEvent, Event.Validate, LimitsFromConfig and NewTimestamp are the existing production APIs. Preserve nulls and canonical IDs. Schema compilers remain in tests.
- **Auth:** new shared apikeys verifier for both public services, trusted tenant from key, explicit scope guards, same generic 401 for missing/malformed/unknown/revoked. SQL includes authenticated tenant.
- **Bounded lifecycle:** register closers on acquisition, use one shared 30-second shutdown budget, drain before dependency closes, propagate worker failures into process exit. New listeners bind synchronously.
- **Testing:** external test packages, t.Helper in helpers, t.Cleanup for resources, t.Parallel only without environment/shared mutable state, real dependencies for durable acceptance/replay semantics.

## No Analog Found

| File | Role | Data Flow | Reason / Research source |
|---|---|---|---|
| `backend/internal/apikeys/keys.go` | utility | transform | No current key/cache/auth implementation; use Pattern 4. |
| `backend/internal/apikeys/cache.go` | store | CRUD | No current key/cache/auth implementation; use Pattern 4. |
| `backend/internal/apikeys/verifier.go` | service | request-response | No current key/cache/auth implementation; use Pattern 4. |
| `backend/internal/kafkax/producer.go` | service | streaming | No Kafka/native persistence/worker implementation; use Patterns 3, 6, 8. |
| `backend/internal/kafkax/consumer.go` | service | streaming | No Kafka/native persistence/worker implementation; use Patterns 3, 6, 8. |
| `backend/internal/storage/clickhouse/events.go` | service | batch | No Kafka/native persistence/worker implementation; use Patterns 3, 6, 8. |
| `backend/internal/eventwriter/writer.go` | service | streaming | No Kafka/native persistence/worker implementation; use Patterns 3, 6, 8. |
| `generator/src/sentinel_generator/cli.py` | utility | request-response | No current CLI; use Pattern 10 and argparse. |
| `deployments/docker/Dockerfile` | config | batch | No existing Dockerfile; use Pattern 11. |

Role matches for storage/Redis/config provide project structure only; they do not prove new library semantics. Research code must still be adapted to lint and acceptance requirements.

## Metadata

**Analog search scope:** backend/cmd, backend/internal, backend/tools, backend/migrations, backend/test, generator/src and tests, deployments, Makefile, scripts, .github, api, web and docs/decisions.
**Files scanned:** inventory of live implementation paths plus focused source reads; 80 proposed paths classified (some do not yet exist).
**Pattern extraction date:** 2026-10-13.
**Verification:** source mapping only; no source edits, service launches or tests executed. Concrete excerpts refer to the pre-Phase-2 live files and line numbers.

