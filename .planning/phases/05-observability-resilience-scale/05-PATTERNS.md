# Phase 5: Observability, Resilience & Scale - Pattern Map

**Mapped:** 2026-10-30
**Files analyzed:** 46 new/modified files
**Analogs found:** 39 / 46. Seven have no in-repo analog: OTel tracer, Grafana JSON, Prometheus rules/tests, Jaeger overlay, open-loop sender, report.py, lag tracker. For these, use the RESEARCH.md patterns plus the nearest structural analog listed below.

> Note: CLAUDE.md's "Technology Stack/Architecture" text describes an older scaffold (`server.Server`, Clerk, asynq, New Relic). Those do not exist in the current tree. The real conventions are `app.Run(service, setup)` + `app.Lifecycle`, `opshttp`, `config.Load` (koanf), `auth.Verifier`, `errs.NewXxxError`, and external `_test` packages. Follow the code excerpts below, not the CLAUDE.md prose.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `backend/internal/telemetry/metrics.go` (NEW: Registry, IngestMetrics, ConsumerMetrics, DetectorMetrics, SSEMetrics) | utility/provider | event-driven (counters) | `backend/internal/opshttp/metrics.go` | role-match (hand-rolled → client_golang) |
| `backend/internal/telemetry/metrics_test.go` (label allowlist guard) | test | — | `backend/internal/opshttp/metrics_test.go` | exact |
| `backend/internal/telemetry/tracing.go` (TracerProvider, sampler, shutdown) | provider | batch export | `app.Lifecycle.Add` usage in `cmd/detector/main.go` | partial (no OTel code exists) |
| `backend/internal/telemetry/tracing_test.go` | test | — | `opshttp/metrics_test.go` | partial |
| `backend/internal/opshttp/metrics.go` + `server.go` (MOD: `WithMetricsHandler(http.Handler)`, mount `/metrics` for every service) | middleware/config | request-response | itself (`Option`, `WithDetectionMetrics`) | exact |
| `backend/internal/opshttp/metrics_test.go` (MOD) | test | — | itself | exact |
| `backend/internal/logger/logger.go` (MOD: honor `Logging.Format`, `timestamp` hook) | utility | transform | itself | exact |
| `backend/internal/logger/logger_test.go` (MOD) | test | — | itself | exact |
| `backend/internal/middleware/context.go`, `global.go`, `chain.go` (MOD: trace_id, de-dup request_id, otel span rename) | middleware | request-response | itself | exact |
| `backend/internal/config/observability.go` + `config.go` (MOD: `Tracing{Enabled,SampleRatio,OTLPEndpoint}`, `PrometheusURL`, `GrafanaURL`) | config | — | `ObservabilityConfig`, `setDefaults` | exact |
| `backend/internal/kafkax/headers.go` (NEW: TextMapCarrier) | utility | transform | `kafkax/producer.go` `recordsFor` headers | role-match |
| `backend/internal/kafkax/producer.go` (MOD: inject traceparent) | service | streaming | itself lines 144-174 | exact |
| `backend/internal/kafkax/lag.go` (NEW: position tracker + kadm ListEndOffsets loop) | service | polling/event-driven | `kafkax/detector_consumer.go` (kadm + revoke/lost callbacks) | role-match |
| `backend/internal/kafkax/consumer.go` (MOD: OnPartitionsRevoked/Lost to delete lag series) | service | streaming | `kafkax/detector_consumer.go:149-235` | exact |
| `backend/internal/retry/retry.go` (NEW: Policy, full jitter, ctx wait) | utility | — | `eventwriter/writer.go:349-428` | exact (extract) |
| `backend/internal/retry/retry_test.go` | test | — | `eventwriter/writer_test.go` | role-match |
| `backend/internal/eventwriter/writer.go` (MOD: use `retry`, CH write histogram, store span) | service | batch | itself | exact |
| `backend/internal/alertwriter/poison.go` (MOD: jitter via `retry`) | service | batch | `eventwriter/writer.go:349-358` | exact |
| `backend/internal/detector/poison.go` (MOD: jitter via `retry`) | service | batch | `eventwriter/writer.go:349-358` | exact |
| `backend/internal/detector/worker.go` (MOD: typed DetectorMetrics, detect span, state gauges) | service | streaming | itself lines 103-130, 459 | exact |
| `backend/internal/alertstream/handler.go` (MOD: `sentinel_sse_clients` gauge) | controller | streaming | itself lines 124-165 | exact |
| `backend/internal/ingest/handler.go` (MOD: `sentinel_ingest_events_total{result}`) | controller | request-response | itself lines 74-154 | exact |
| `backend/internal/query/system.go` (NEW: `GET /api/v1/system`) | controller | request-response | `backend/internal/query/summary.go` + `triage.go` (403) | exact |
| `backend/internal/query/system_test.go` (NEW) | test | — | `backend/internal/ingest/handler_test.go` (func-type fakes) | role-match |
| `backend/cmd/{ingest,detector,event-writer,alert-writer,query-api}/main.go` (MOD: registry + tracer wiring) | config/wiring | — | `backend/cmd/detector/main.go` | exact |
| `backend/internal/testing/containers.go` (MOD: allowlisted `stop`/`start`) | test util | — | itself lines 237-270 | exact |
| `backend/test/chaos_*_test.go` (NEW: broker restart, CH outage recovery, slow consumer lag, overload bounds) | test (integration) | — | `backend/test/writer_test.go:183-245`, `ingest_test.go:580-610` | exact |
| `backend/test/metrics_endpoints_test.go` / graceful shutdown | test (integration) | — | `backend/test/lifecycle_test.go:158-245` | exact |
| `Makefile` (MOD: `test-chaos`, `bench`, `bench-down`) | config | — | `Makefile` `test-integ`, `seed-scenarios` | exact |
| `scripts/check-pins.sh` (MOD: all compose files, port ranges) | config/CI | — | itself `check_images` | exact |
| `deployments/compose/docker-compose.observability.yml` (NEW) | config | — | `deployments/compose/docker-compose.yml` | role-match |
| `deployments/compose/docker-compose.bench.yml` (NEW overlay) | config | — | `docker-compose.yml` service blocks | role-match |
| `observability/prometheus/prometheus.yml`, `alerts.yml`, `alerts_test.yml` | config | — | none | no analog |
| `observability/grafana/provisioning/**.yml`, `dashboards/sentinel-pipeline.json` | config | — | none | no analog |
| `scripts/test_observability_config.py` (NEW) | test | — | `scripts/test_stack_config.py` | role-match |
| `generator/src/sentinel_generator/bench/*.py` (NEW: schedule, pools, open-loop sender, SSE subscriber) | service | streaming/batch | `generator/src/sentinel_generator/send.py`, `events.py` | role-match |
| `generator/tests/test_bench_*.py` | test | — | `generator/tests/test_send.py` | exact |
| `bench/run.py` (orchestration) | utility | batch | `scripts/seed_scenarios.py` | role-match |
| `bench/report.py` + `bench/tests/test_report.py` | utility | transform | `scripts/seed_scenarios.py` (`verify_late_observation`, refuse-on-mismatch) | partial |
| `scripts/seed_scenarios.py` (MOD: renamed late metric + larger body bound) | utility | — | itself lines 211-225 | exact |
| `api/openapi.yaml` (MOD: `/api/v1/system`, `getMetricsSummary` description) | config/contract | — | `/api/v1/metrics/summary` op lines 401-435 | exact |
| `web/src/api/client.ts` (MOD: `system()`) | service (client) | request-response | `client.ts` `summary()`/`rules()` lines 140-174 | exact |
| `web/src/pages/System.tsx` (NEW) | component | request-response (polling) | `web/src/pages/Rules.tsx`, `Overview.tsx` | exact |
| `web/src/pages/System.test.tsx` (NEW) | test | — | `web/src/pages/Rules.test.tsx` | exact |
| `web/src/App.tsx` (MOD: admin-only nav link + `/system` route) | component | — | itself lines 103-168 | exact |
| `web/src/pages/Overview.tsx` (MOD: lag panel replaces line 183) | component | request-response | itself | exact |
| `docs/decisions/0010-*.md`, `0011-*.md`, 0004 amendment; `docs/operations.md`, `docs/architecture.md` | docs | — | `docs/decisions/0004-metrics-label-policy.md` | exact |

---

## Pattern Assignments

### `backend/internal/telemetry/metrics.go` (provider, event-driven)

**Analog:** `backend/internal/opshttp/metrics.go` (lines 1-31). It is the only metric code in the repo: a per-process struct injected via an `Option`, with **no global registry**. Keep that shape and swap the atomic for client_golang vectors registered on an injected `*prometheus.Registry`.

```go
// opshttp/metrics.go:9-22 (shape to preserve: per-process struct, Option injection)
type DetectionMetrics struct{ late atomic.Uint64 }
func (m *DetectionMetrics) IncLate() { m.late.Add(1) }
type Option func(*handler)
func WithDetectionMetrics(metrics *DetectionMetrics) Option {
	return func(h *handler) { h.metrics = metrics }
}
```

Rules to carry over:
- The constructor takes `reg prometheus.Registerer` and returns a typed struct (`NewIngestMetrics(reg)`, `NewConsumerMetrics(reg, ...)`). No `promauto`, no package vars (`gochecknoglobals`). `backend/.golangci.yml:106,172` already allows `prometheus.*Opts` literals and bucket magic numbers.
- Labels only from the ADR 0004 allowlist (`docs/decisions/0004-metrics-label-policy.md`): `result,status,rule_id,severity,reason,group,topic,partition,service`. The `sentinel_retries_total{operation}` label needs an ADR amendment, or use `reason`.
- The rename `detection_late_events_total` → `sentinel_detection_late_events_total` has 5 consumers, all of which must be updated: `docs/operations.md`, `docs/architecture.md`, `scripts/seed_scenarios.py:216-225`, `backend/test/detector_capacity_test.go`, `backend/internal/opshttp/metrics_test.go`.
- Nil-safe methods (the worker already guards with `if late && w.metrics != nil` at `detector/worker.go:459`). Keep the nil-safe receiver style so tests can pass nil.

**Test analog:** `opshttp/metrics_test.go:14-44`. This is an external `_test` package with concurrent increments via `wg.Go` and a "per process wiring, not global" assertion. Mirror it with `prometheus/testutil.ToFloat64` and a guard that walks `reg.Gather()` and asserts every label name ∈ allowlist.

```go
// metrics_test.go:22-39 (concurrency + non-global assertion)
var wg sync.WaitGroup
for range 16 { wg.Go(func() { for range 100 { metrics.IncLate() } }) }
wg.Wait()
...
require.Contains(t, r.Body.String(), "detection_late_events_total 0\n",
	"counter must be per process wiring, not global")
```

---

### `backend/internal/opshttp/server.go` + `metrics.go` (MOD: mount `/metrics` for every service)

**Analog:** itself. The mount point is `NewHandler` (lines 76-97):

```go
for _, option := range options {
	option(h)
}
mux := http.NewServeMux()
mux.HandleFunc("GET /healthz", h.healthz)
mux.HandleFunc("GET /readyz", h.readyz)
if h.metrics != nil {
	mux.HandleFunc("GET /metrics", h.serveMetrics)
}
```

Replace the `metrics *DetectionMetrics` field with `metricsHandler http.Handler`, add `WithMetricsHandler(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))`, and keep the 404 behavior when no handler is passed (existing test line 40-43 asserts it). `Start(cfg, draining, checks, options...)` (line 194) already forwards options, so no signature change is needed.

---

### `backend/cmd/*/main.go` (wiring, all five binaries)

**Analog:** `backend/cmd/detector/main.go` `setup`. This is the canonical per-binary order: ops-http closer first, dependencies, worker goroutine with `lc.Add`, `opshttp.Start(... options)`, then the DrainCloser last.

```go
// cmd/detector/main.go (setup head)
var opsServer *opshttp.Server
draining := &opshttp.Draining{}
metrics := &opshttp.DetectionMetrics{}
fail := func(err error) { draining.Begin(); lc.Fail(err) }
lc.Add("ops-http", func(closeCtx context.Context) error { ... opsServer.Shutdown(closeCtx) })
...
worker := detector.New(consumer, publisher, rules, cfg.Detector.Writer,
	contracts.LimitsFromConfig(cfg.Contracts), db.Pool, pgBudgets, metrics).WithQuarantine(dlq)
...
opsServer, err = opshttp.Start(cfg.Ops, draining, []opshttp.Check{
	{Name: "kafka", Fn: consumer.Ping}, ...
	{Name: "detector", Fn: worker.Ready},
}, opshttp.WithDetectionMetrics(metrics))
...
lc.Add("draining", opshttp.DrainCloser(draining, cfg.Ops.DrainHold))
```

- Build `reg := prometheus.NewRegistry()` plus Go/process collectors at the top of `setup`, then pass it into the constructors.
- **Tracer shutdown ordering:** Lifecycle closes LIFO (`app/app.go:109-130`). Register `lc.Add("tracing", tp.Shutdown)` *before* the Kafka clients are added, so it closes *after* the workers stop and flushes their spans. RESEARCH says "before the Kafka clients close". Pick an order and add an `app_test.go` assertion on `lc.Names()`.
- **Option-method injection** for optional collaborators already exists: `func (w *Worker) WithQuarantine(p) *Worker` (`detector/worker.go:107-111`). Prefer `WithMetrics(...)`/`WithTracer(...)` builders over growing variadic constructor args. `detector.New(..., metrics ...*opshttp.DetectionMetrics)` uses a variadic today, so change it to a typed struct.
- ingest/query-api: wrap Echo with otelhttp at `httpx.New(cfg.Server, e)` (`cmd/ingest/main.go`, `cmd/query-api/main.go:startQueryHTTP`). The Echo instance is built by `middleware.Apply(e, &log, cfg.Server.CORSAllowedOrigins)`.
- **Phase 4 ownership:** `cmd/query-api/main.go` is a shared surface (RESEARCH Pitfall 12). The System handler registration goes next to `query.NewSummaryHandler(...).Register(e)`.

---

### `backend/internal/logger/logger.go` (MOD)

**Analog:** itself, lines 32-52. The environment coupling to remove:

```go
out := w
if cfg.Environment == localEnv {
	out = zerolog.ConsoleWriter{Out: w, TimeFormat: timeFormat}
}
return zerolog.New(out).Level(level).With().
	Timestamp().                       // emits "time" — spec needs "timestamp"
	Str("service", service).
	Str("environment", cfg.Environment).
	Logger()
```

- Switch on `cfg.Logging.Format` (`"json"` default from `config/observability.go:30`, `"console"` explicit). Add `"console"` to `Validate()` (lines 37-53, same switch style as the level check).
- Replace `.Timestamp()` with a `zerolog.Hook` that writes `timestamp` (UTC RFC3339Nano). Do not mutate the global `zerolog.TimestampFieldName` (`gochecknoglobals` intent).
- `NewPgxLogger` (line 55) is always ConsoleWriter. Bring it under the same format switch.

**Test analog:** `logger_test.go:14-28` (`NewWithWriter` into a `bytes.Buffer`, then `json.Unmarshal` and field asserts). `TestLoggerLocalUsesConsole` (lines 45-57) **must be rewritten**, because it asserts the behavior being removed. Add asserts for `timestamp`, `level`, `service`, `request_id`, `trace_id`, and absence of the API key/JWT.

---

### `backend/internal/middleware/context.go`, `global.go`, `chain.go` (MOD)

**Analog:** itself.
- `context.go` `EnhanceContext` builds the request logger with `request_id, method, path, ip`. Add `trace_id`/`span_id` from `trace.SpanContextFromContext(c.Request().Context())` here. It runs after otelhttp because otelhttp wraps the whole Echo handler.
- `global.go:71-73` adds `request_id` a **second time** (duplicate JSON key). Remove it:
  ```go
  if requestID := GetRequestID(c); requestID != "" {
      e = e.Str("request_id", requestID)   // duplicate of context logger field — delete
  }
  ```
- `global.go:47-85` `RequestLogger` is also the observation point for `sentinel_ingest_requests_total{status}` and `..._request_duration_seconds`. It already computes the mapped `statusCode` via `toHTTPError(v.Error).Status` (lines 54-56). Reuse that value so the metric matches the client-visible status.
- `chain.go:17-28` `Chain` returns `[]Named` in a tested order. Add any span-rename middleware as a new `Named{Name: "trace", ...}` entry and update the order test in `middleware_test.go`.
- Do not add a `route`/`path` metric label (Pitfall 10). `c.Path()` is OK for span names only.

---

### `backend/internal/config/observability.go` + `config.go` (MOD)

**Analog:** itself.

```go
// observability.go:10-21
type ObservabilityConfig struct {
	ServiceName string        `koanf:"service_name"`
	Environment string        `koanf:"environment"`
	Logging     LoggingConfig `koanf:"logging"`
}
```
```go
// config.go setDefaults (lines ~502-520): flat dotted keys
"observability.logging.level":  levelInfo,
"observability.logging.format": "json",
```

- Add a `Tracing TracingConfig \`koanf:"tracing"\`` sub-struct (`enabled`, `sample_ratio`, `otlp_endpoint`), plus `PrometheusURL`/`GrafanaURL` (query-api only), with defaults in `setDefaults` and checks in `Validate()`.
- Validate with the bounded style used elsewhere (`validate:"min=0,lte=..."` tags as in `OpsConfig`, `config.go:229-236`).
- Env keys arrive as `SENTINEL_OBSERVABILITY.TRACING.SAMPLE_RATIO`.
- Tests go in `config/config_test.go` (external package).

---

### `backend/internal/retry/retry.go` (NEW utility) + callers

**Analog:** `backend/internal/eventwriter/writer.go:349-358, 397-428`. Extract exactly this:

```go
func waitForRetry(ctx context.Context, maximum time.Duration) error {
	timer := time.NewTimer(jitterDelay(maximum))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func jitterDelay(maximum time.Duration) time.Duration {
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(maximum)+1))
	if err != nil {
		return maximum
	}
	return time.Duration(value.Int64())
}
```
```go
// writer.go:399-416 attempt loop shape
backoff := w.settings.RetryMinBackoff
for attempt := 1; attempt <= w.settings.MaxAttempts; attempt++ {
	... lastErr = op(...)
	if lastErr == nil { return nil }
	if attempt == w.settings.MaxAttempts || ctx.Err() != nil { break }
	if err := waitForRetry(ctx, backoff); err != nil { return fmt.Errorf("...: %w", errors.Join(lastErr, err)) }
	backoff = min(backoff*backoffMultiplier, w.settings.RetryMaxBackoff)
}
```

**Callers lacking jitter (the REL-01 gap):**
- `alertwriter/poison.go:22-45`: `retry()` uses `time.NewTimer(retries.backoff)`. Replace the timer/select with `retry.Wait(ctx, retries.backoff)`. Keep the shared-budget `effectRetries{remaining, backoff}` semantics ("Success cannot reset either bound").
- `detector/poison.go:73-99` `quarantine`: same replacement. Keep the `poisonRetries` shared sequence.
- `alertstream/delivery.go`, `listener.go`: 100 ms..5 s backoff without jitter (RESEARCH A4).

Keep the policy semantics as a value type (`Policy{Min, Max, Attempts}`). Callers own their remaining-attempt counters where the budget is shared per batch.

---

### `backend/internal/kafkax/headers.go` (NEW) + `producer.go` (MOD)

**Analog:** `kafkax/producer.go:144-174` (`recordsFor`). Headers are built inline:

```go
records = append(records, &kgo.Record{
	Topic: p.topic,
	Key:   []byte(event.TenantID + "|" + sourceIP),
	Value: value,
	Headers: []kgo.RecordHeader{
		{Key: "schema_version", Value: []byte(strconv.Itoa(event.SchemaVersion))},
		{Key: "request_id", Value: []byte(requestID)},
	},
})
```

- `recordsFor(events, requestID)` has no ctx. Add a ctx parameter (or a precomputed traceparent string) so `propagation.TraceContext{}.Inject(ctx, headerCarrier{r})` runs once per batch and every record gets the same value.
- The carrier implementation follows the RESEARCH "Kafka header carrier" snippet.
- The DLQ copies non-`dlq.*` headers bounded at 128 (`kafkax/dlq.go:25,235`), so `traceparent` propagates into the DLQ automatically. Add an assertion in `dlq_test.go`.
- Producer unit test analog: `kafkax/producer_test.go`.

---

### `backend/internal/kafkax/lag.go` (NEW) + `consumer.go` (MOD)

**Analog for kadm use:** `kafkax/detector_consumer.go:113,272,453`. It uses `kadm.NewClient(client)` on the existing `*kgo.Client`, plus `kadm.ListedOffsets`.

**Analog for rebalance callbacks:** `detector_consumer.go:149-158, 208-235`

```go
client, err := kgo.NewClient(
	..., kgo.SessionTimeout(consumerSessionTimeout),
	kgo.RebalanceTimeout(budgets.Rebalance),
	kgo.OnPartitionsAssigned(c.assigned), kgo.OnPartitionsRevoked(c.revoked),
	kgo.OnPartitionsLost(c.lost), kgo.OnOffsetsFetched(c.fetched),
)
...
func (c *DetectorConsumer) revoked(ctx context.Context, _ *kgo.Client, topics map[string][]int32) {
	c.releaseAssignments(ctx, "revoked", topics)
}
```

- The event-writer `kafkax.NewConsumer` (`consumer.go:33-66`) has **no** revoke/lost hooks. Add `kgo.OnPartitionsRevoked/Lost` that call `GaugeVec.DeleteLabelValues(group, topic, partition)` (Pitfall 8).
- Also check the alert-writer consumer (`kafkax/alert_consumer.go`).
- Tracker goroutine lifecycle follows the `startDeliveryProjector` shape in `cmd/query-api/main.go`: `context.WithCancel` + done channel + `lc.Add(name, closer waiting on done or closeCtx)`.
- Each kadm call gets a bounded `context.WithTimeout` (2 s), the same as `checkConfigTransition` in `cmd/detector/main.go`.
- `processed_next` for the detector must come from the worker's processed position, **not** the committed pin (ADR 0004/0007).

---

### `backend/internal/ingest/handler.go` (MOD: result counter)

**Analog:** itself, lines 74-154. Every exit path is already explicit. Map each one to a `result` label value:

| Line | Return | `result` |
|---|---|---|
| 79 | in-flight full → 503 | `shed` |
| 86, 105, 148 | auth/limiter/publish unavailable → 503 | `unavailable` |
| 108 | `NewTooManyRequestsError` | `rate_limited` |
| 143 | invalid batch 400 | `rejected` |
| 151 | 202 after Kafka ack | `accepted` (add `len(events)`) |

Inject metrics through `NewHandler(v, publisher, rateLimiter, cfg, log)` (lines 48-55). Add a parameter or a `WithMetrics` method. Unit-test analog: `ingest/handler_test.go` (func-type fakes `verifierFunc`, `publisherFunc`, `limiterFunc`, lines 31-48).

---

### `backend/internal/detector/worker.go` (MOD), `alertstream/handler.go` (MOD), `eventwriter/writer.go` (MOD)

- Detector: replace `metrics *opshttp.DetectionMetrics` (line 103) with `*telemetry.DetectorMetrics`. The `w.bounds *detection.Bounds` field (line 102) is the source for the `sentinel_detector_state_*` gauges. Set them after each acknowledge.
- SSE gauge: `alertstream/handler.go:124-125` admits (`h.clients <- struct{}{}; h.active.Add(1)`) and line 165 releases (`defer func() { <-h.clients; h.active.Done() }()`). Inc/Dec `sentinel_sse_clients` at exactly those two points.
- CH write histogram: wrap `w.store.InsertBatch(operationCtx, events)` at `eventwriter/writer.go:402` with `result` ∈ {ok,error}. The batch `store` span goes around the same call.

---

### `backend/internal/query/system.go` (NEW controller, request-response)

**Analog:** `backend/internal/query/summary.go` (whole file), with the 403 pattern from `query/triage.go:29-37`.

Imports and handler shape (`summary.go:1-12, 60-82`):
```go
import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/errs"
)

type SummaryHandler struct {
	events SummaryEvents
	alerts SummaryAlerts
	auth   *auth.Verifier
	now    func() time.Time
}

func NewSummaryHandler(events SummaryEvents, alerts SummaryAlerts, verifier *auth.Verifier,
	now func() time.Time,
) *SummaryHandler {
	if now == nil {
		now = time.Now
	}
	return &SummaryHandler{events: events, alerts: alerts, auth: verifier, now: now}
}

func (h *SummaryHandler) Register(e *echo.Echo) {
	e.GET("/api/v1/metrics/summary", h.GetSummary, h.auth.Middleware)
}
```

Core pattern (`summary.go:91-106`): principal from verified middleware, a single captured `now`, bounded ctx, and a dependency failure mapped to 503:
```go
p, err := auth.RequirePrincipal(c)
if err != nil {
	return err
}
now := h.now().UTC()
ctx, cancel := context.WithTimeout(c.Request().Context(), detailTimeout)
defer cancel()
events, err := h.events.SummarizeEvents(ctx, p.TenantID, now)
if err != nil {
	return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
}
```

Role gate plus audit (`triage.go:30-37`). Add `auth.RequireAdmin(p)` in `auth/jwt.go` next to `RequireTriage` (lines 125-131):
```go
if auth.RequireTriage(p) != nil {
	auth.Audit(c, "alert_transition", "denied", "role_forbidden", p)
	return errs.NewForbiddenError(http.StatusText(http.StatusForbidden), false)
}
```
```go
// auth/jwt.go:125-131 — model RequireAdmin on this
func RequireTriage(p Principal) error {
	if p.Role != "analyst" && p.Role != "admin" {
		return ErrForbidden
	}
	return nil
}
```

Differences from Summary:
- **Prometheus failure must NOT 503.** Return 200 with per-field `{"state":"unavailable","reason":"prometheus_unavailable"|"no_data"}` (UI-SPEC States). Return 503 only when query-api itself cannot answer.
- Depend on a small interface (`SystemMetrics { Query(ctx, promql string, t time.Time) (...) }`) so tests fake it. The PromQL strings are package-level **consts** (allowed; not mutable globals) from the fixed allowlist in RESEARCH Pattern 5. No request parameters: reject `c.Request().URL.RawQuery != ""` like `triage.go:39`.
- Readiness probes to ops `/readyz` use bounded concurrency and a 1 s timeout. Copy the semaphore and buffered-results approach from `opshttp/server.go:103-175` (`sem` channel, buffered `results`, `ctx.Done()` select).
- The response includes `observed_at` and an optional `grafana_url` (only `http(s)`).

**Test analog:** `backend/internal/ingest/handler_test.go:31-48` (func-type fakes, `httptest`, `echo.New()` + `middleware.Apply`, external package). Integration/role coverage analog: `backend/testphase4/summary_rules_test.go` (`tokenFor(role)` with a signed HS256 token; asserts 401 unauthenticated and per-role statuses). Cases: viewer/analyst → 403, admin → 200, Prometheus down → 200 with all `unavailable`, never `0`.

---

### `api/openapi.yaml` (MOD)

**Analog:** `/api/v1/metrics/summary` (lines 401-435). Copy the `tags`, `operationId`, `description`, `security: - BearerAuth: []` and the `responses` set with `$ref: "#/components/responses/{BadRequest,Unauthorized,Forbidden,ServiceUnavailable}"`.
- Update line 418 ("Consumer lag is unavailable until Phase 5.") to point at `GET /api/v1/system`.
- After the edit, run `npm run gen:api`. `backend/test/openapi_drift_test.go` and `openapi_runtime_auth_test.go` enforce the spec/route match. Extend them for the new route.

---

### `web/src/api/client.ts` (MOD: `system()`)

**Analog:** `client.ts:140-174` (`summary`, `rules`). These methods follow abort-check → GET → `assertCurrent()` → shape validation → `throw new APIError(503)` on a malformed body:

```ts
async rules(signal?: AbortSignal) {
  signal?.throwIfAborted();
  const result = await client.GET("/api/v1/rules", { signal });
  assertCurrent();
  signal?.throwIfAborted();
  if (
    !result.data ||
    !Array.isArray(result.data.rules) ||
    result.data.rules.length > 4
  )
    throw new APIError(503);
  return result.data;
},
```

A 403 surfaces as `APIError(403)` from the shared fetch wrapper (lines 65-68), and 401 calls `unauthorized()`. `System.tsx` branches on `error instanceof APIError && error.status === 403`. Export a type alias the same way as the `paths[...]` aliases at lines 4-31.

---

### `web/src/pages/System.tsx` (NEW component)

**Analog:** `web/src/pages/Rules.tsx` (whole file, 58 lines) for the query/loading/error/retry skeleton. `Overview.tsx` shows tables with `th scope="row"`, `<dl>` breakdowns and the secondary Refresh button.

```tsx
// Rules.tsx:4-29
const { session, client } = useSession();
const rules = useQuery({
  queryKey: ["rules", session?.generation],
  queryFn: ({ signal }) => client!.rules(signal),
  enabled: !!session && !!client,
  retry: false,
});
return (
  <section aria-labelledby="rules-title">
    <h2 id="rules-title">Rules</h2>
    ...
    {rules.isPending && <p role="status">Loading rules…</p>}
    {rules.isError && (
      <div role="alert">
        <p>Could not load rules. Try again.</p>
        <button onClick={() => void rules.refetch()}>Retry loading rules</button>
      </div>
    )}
```

```tsx
// Overview.tsx:174-180 refresh button disabled while fetching
<button className="secondary" disabled={summary.isFetching}
  onClick={() => void summary.refetch()}>Refresh overview</button>
```

Deltas required by UI-SPEC:
- `queryKey: ["system", session.generation]` with `enabled: session.role === "admin"`. No request for non-admins; render Forbidden directly.
- Add `refetchInterval: 15000, refetchIntervalInBackground: false`.
- The Refresh button is **primary** (no `secondary` class).
- Existing CSS classes to reuse (`web/src/styles.css`): `.page-description` (186), `.mobile-alerts` + `dl` grid (200-224), `.identifier` (252), `.badge` (255), `.empty-state` (272), `.error-state` (282), `@media (max-width: 599px)` (300). Tokens `--success/-bg`, `--warning/-bg`, `--critical/-bg`, `--muted-bg` exist at lines 21-27. **No new tokens.** Add only a card-grid rule (`repeat(auto-fit, minmax(240px, 1fr))`).

**Test analog:** `web/src/pages/Rules.test.tsx:1-70`. It renders the real `<App>` under `StrictMode`, stubs global `fetch` routing by pathname (with `/api/v1/session` returning `{role}`), signs in through the token field, and asserts on `requests[]`. Parametrize `role` (`start(list, role = "viewer")`, line 33) to cover admin/analyst/viewer, and assert that **no** `/api/v1/system` request is sent for non-admins. Use `vi.useFakeTimers` for the 15 s poll and the 60 s staleness cases.

---

### `web/src/App.tsx` (MOD)

**Analog:** itself, lines 103-168. Nav links come from a literal array `["overview","alerts","events","rules"]`, with `aria-current` logic duplicated in `style` and `aria-current` (lines 107-127), labels from a ternary chain (146-152), and routing from a ternary chain (158-168):
```tsx
) : path === "/rules" ? (
  <Rules key={session.generation} />
```
- Append `"system"` only when `session.role === "admin"`.
- Add `"/system"` to the `path === "/events" || ...` known-path checks (both copies).
- Route `/system` → `<System key={session.generation} />` for **every** role (the forbidden state lives inside the page).
- Existing nav assertions are in `App.test.tsx`. Update them. Phase 4 shared surface: sequence the change after Phase 4 closes.

### `web/src/pages/Overview.tsx` (MOD)

Replace line 183 (`<p>Consumer lag — available in Phase 5</p>`) with a separate `useQuery(["system", session.generation])` panel. It must be **independent** of `summary` (it renders outside the `{data && ...}` block, as line 183 does today). For admins it uses the same `["system", generation]` key, so React Query dedupes it with the System page. The "View system health" link reuses App's pushState + `sentinel:navigate` dispatch (`App.tsx:137-143`). Update `Overview.test.tsx`.

---

### `backend/internal/testing/containers.go` (MOD: broker stop/start)

**Analog:** itself, lines 237-270:
```go
func (c *FaultContainer) Pause(ctx context.Context) error {
	return dockerContainerCommand(ctx, "pause", c.GetContainerID())
}
func dockerContainerCommand(ctx context.Context, action, id string) error {
	if id == "" { return errors.New("container id is empty") }
	if action != "pause" && action != "unpause" {
		return fmt.Errorf("unsupported container action %q", action)
	}
	//nolint:gosec // exec uses fixed docker argv without a shell
	if out, err := exec.CommandContext(ctx, "docker", action, id).CombinedOutput(); err != nil { ... }
	return nil
}
```
Extend the allowlist to `stop`/`start` and add `Stop`/`Start` methods. `StartKafkaFaultContainer` (lines 326-369) already binds a **fixed host port** (`hc.PortBindings` with `FreePort()`), so the broker address survives a restart. Only use the dedicated fault broker, never the TestMain broker (comment on line 325).

### `backend/test/chaos_*_test.go` (NEW integration)

**Analog:** `backend/test/writer_test.go:183-245` `TestWriterInsertFailureNoCommit`. This is the full fault → assert no commit → recover → assert exact unique count pattern:
```go
clickhouseCfg, handle, err := sentineltesting.StartClickHouseFaultContainer(t.Context())
require.NoError(t, err)
t.Cleanup(handle.Close)
...
require.NoError(t, handle.Pause(t.Context()))
... runErr := eventwriter.New(consumer, store, unusedDLQ{}, settings, ...).Run(t.Context())
require.Error(t, runErr)
_, committed := committedNextOffset(t, kafka, group, topic)
require.False(t, committed, "failed insert advanced the source offset")
require.NoError(t, handle.Unpause(t.Context()))
require.Eventually(t, func() bool { return conn.Ping(t.Context()) == nil }, 20*time.Second, 100*time.Millisecond)
... waitCommittedOffset(t, kafka, group, topic, int64(len(events)))
unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
require.Equal(t, uint64(len(events)), unique)
```
- Overload/bounded analog: `backend/test/ingest_test.go:580-610` `TestIngestShedsUnderLoad` (gated publisher, then assert `503` + `Retry-After: 1`).
- Graceful-shutdown analog: `backend/test/lifecycle_test.go:158-245` (build binary, `exec.CommandContext`, `SIGTERM`, assert exit code and output).
- Use the build tag `//go:build integration` (`writer_test.go:1`) and helpers `RequireUniqueTopic`, `committedNextOffset`, `waitCommittedOffset` from the same package.

### `Makefile` (MOD)

**Analog:** the `test-integ` target (line 60-61) and `test-phase3` (a script with a finite ceiling):
```make
test-integ: ## Run all integration tests (needs Docker and make dev-up)
	cd backend && go test -tags=integration -timeout 32m -p 1 ./test/...
```
- `test-chaos` calls `scripts/go-test-nonempty.sh '<pkgs>' '<regex>' -tags=integration -timeout <N>m -p 1`. That script (whole file) fails on zero matched tests.
- Replace `bench` (lines 90-92, currently exits 1) with the `bench/run.py` invocation.
- Add the targets to `.PHONY` (line 19).
- **Do not** use `$(COMPOSE)` (line 4), which is bound to the root `.env` and project `sentinel`. Bench must use `-p sentinel-bench-<run>` and a private env file.

### `scripts/check-pins.sh` (MOD)

**Analog:** itself `check_images`. It is hardcoded to `compose="deployments/compose/docker-compose.yml"`, and the port regex `'"([0-9.]+:)?[0-9]+:[0-9]+"'` skips ranges `a-b:c`. Loop over `deployments/compose/docker-compose*.yml` and widen the regex to `[0-9]+(-[0-9]+)?`. The `[ "$count" -ge 4 ]` minimum applies per base file only.

### `deployments/compose/docker-compose.observability.yml` / `docker-compose.bench.yml` (NEW)

**Analog:** `deployments/compose/docker-compose.yml`. Conventions to copy:
- Header comment block (lines 1-9): pinned by tag **and** sha256, `127.0.0.1` binds only, no literal secrets (`${VAR:?set in .env}`).
- Image line style: `image: apache/kafka:4.3.1@sha256:...` (line 18).
- Healthcheck block shape (lines 39-44 / 140-145).
- App service shape (lines 162-208): `restart: unless-stopped`, `mem_limit`, `cpus`, `SENTINEL_OPS.PORT`, healthcheck via `/app/healthprobe http://127.0.0.1:910x/readyz`.
- Every app service sets `SENTINEL_PRIMARY.ENV: local` (lines 127, 182, 230, 266, 300). After the logger change, also set `SENTINEL_OBSERVABILITY.LOGGING.FORMAT: json` explicitly in the overlays.
- **Do not set top-level `name:`** in overlays (base has `name: sentinel`, line 10). The bench project name comes from `-p`.
- The bench overlay uses `ports: !reset []` plus ranges and `stop_grace_period: 40s` on workers.

### `observability/**` (Prometheus, Grafana, alerts): no analog

Use RESEARCH Patterns 6/7. The only repo constraint that applies: pinned image digests and `127.0.0.1` binds (check-pins). The dashboard uses datasource `uid` (e.g. `sentinel-prom`).

### `scripts/test_observability_config.py` (NEW)

**Analog:** `scripts/test_stack_config.py` (stdlib `unittest`, `ROOT = Path(__file__).resolve().parents[1]`, `subprocess` against real `docker compose config`). Same layout: a `unittest.TestCase` class, stdlib only, run via `python3 -m unittest`. Assert that dashboard exprs and rules reference only registered metric names and that no forbidden label names appear (ADR 0004 list).

---

### `generator/src/sentinel_generator/bench/*.py` (NEW: live-time, pools, open-loop)

**Analog:** `generator/src/sentinel_generator/send.py` (lines 1-235) and `events.py` (lines 1-88).

Reuse, don't duplicate:
- `events.build_event_id(rng, timestamp_ms)` (events.py:36-45) for UUIDv7 IDs. Use `_format_time` (54-56) for millisecond `Z` timestamps.
- `send._serialize_batch` (79-87, 1 MiB guard), `_validate_accepted` (117-138, exact 202 envelope check), `_retry_after_seconds` (90-106), `RetryPolicy` (33-48, validated frozen dataclass), `SendError` (29-30).
- Injection seams: `Sleeper`/`UTCClock`/`Jitter` callables (lines 24-26) make the code testable. Bench equivalents should inject a monotonic clock and a sleeper.

Must differ:
- `events.build_event` (59-88) draws IPs uniformly from `DOC_NETWORKS` (12-17), and `send.generate_events` (61-76) anchors `base_time = run_start - count s` (Pitfall 2). The bench needs a **new** builder with `occurred_at = intended send time` and bounded entity pools. Leave `generate_events`/`build_event` untouched (seed contracts depend on them).
- Async: `httpx.AsyncClient` (same `httpx==0.28.1`), per RESEARCH Pattern 8.
- Retry budget capped well below 30 s (Pitfall 4), unlike `DEFAULT_RETRY_BUDGET_SECONDS = 120.0`.

**Test analog:** `generator/tests/test_send.py:1-60`. It uses `httpx.MockTransport(handler)` clients (`_client`, line 50-51), an `_accepted(request)` 202 builder (33-44), a fixed `RUN_START` and a fake `API_KEY` constant, and pytest functions with `-> None`. mypy runs over `src tests` (Makefile `lint`), so type-annotate everything.

### `bench/run.py` (orchestration) and `bench/report.py`

**Analog:** `scripts/seed_scenarios.py`.
- `child_environment(kind, settings)` (lines 36-70): an allowlisted env per child, never ambient secrets. Copy it for docker/keyctl/dev-jwt children.
- `run_command` (124-157): bounded `subprocess.run(..., timeout=, check=True, capture_output=True)`, an output size cap (8 MiB), and errors mapped to a safe `SeedError` message.
- `write_private` / `read_private` (73-121): private 0600/0700 evidence with `O_NOFOLLOW`. Also `scripts/seed.py:128-160` (`secure_key_file`, `_prepare_key_directory`).
- `stack_settings` (164-208): resolves settings via `docker compose ... config --format json` and **refuses unfamiliar overrides**. Copy this refusal posture for the project-name denylist (`sentinel`, `sentinel-p4-demo-20261024`) and the port/disk preflight.
- Refuse-on-mismatch style for report.py: `verify_late_observation` (228-232):
  ```python
  if after_identity != before_identity or after < before:
      raise SeedError("detector restarted or late counter reset during this run")
  if after - before != expected:
      raise SeedError("new late drops differ from explicit scenario expectations")
  ```
- The shebang + module docstring + `from __future__ import annotations` header style is at lines 1-8.

### `scripts/seed_scenarios.py` (MOD): breaks after the metrics change

`late_metric` (lines 211-225) reads at most **8192 bytes** of `/metrics` and parses `detection_late_events_total `. With promhttp plus Go/process collectors the body will exceed 8 KiB, so the read raises `ValueError` → "detector late counter unavailable". It must be updated together with the rename: a larger bound (e.g. 256 KiB) and the new metric name. `scripts/test_seed_scenarios.py` covers it.

---

## Shared Patterns

### Dependency injection, no globals
**Source:** `backend/cmd/detector/main.go` `setup`, `detector/worker.go:107-130` (`WithQuarantine`, variadic optional deps)
**Apply to:** every new metric/tracer/retry collaborator. Construct in `setup`, pass through constructors or `WithX` builders, and register closers with `lc.Add`. Lint forbids `init()` and package vars.

### Lifecycle and background goroutines
**Source:** `cmd/query-api/main.go` `startDeliveryProjector` / `startAlertListener`
```go
projectCtx, stopProjector := context.WithCancel(ctx)
projectDone := make(chan struct{})
go func() {
	defer close(projectDone)
	if projectErr := deliveries.Run(projectCtx); projectErr != nil && projectCtx.Err() == nil {
		draining.Begin()
		lc.Fail(fmt.Errorf("alert delivery: %w", projectErr))
	}
}()
lc.Add("alert-delivery", func(closeCtx context.Context) error {
	stopProjector()
	select { case <-projectDone: return nil; case <-closeCtx.Done(): return closeCtx.Err() }
})
```
**Apply to:** the lag tracker loop, tracer provider shutdown, and the start-offset (pin margin) poller.

### HTTP errors and auth
**Source:** `errs.NewServiceUnavailableError(http.StatusText(...), 1)`, `errs.NewForbiddenError(..., false)`, `auth.RequirePrincipal(c)`, `h.auth.Middleware` on route registration, `auth.Audit(c, action, outcome, reason, p)` on denials (`query/triage.go:30-37`)
**Apply to:** `query/system.go`. Handlers return errors to the central handler (`middleware.Apply` installs `GlobalErrorHandler`).

### Bounded concurrency and timeouts
**Source:** `opshttp/server.go:103-175` (semaphore channel, buffered results, ctx timeout), `ingest/handler.go:75-80` (non-blocking `select` admission → 503)
**Apply to:** System readiness fan-out, the lag tracker kadm calls (2 s), and Prometheus queries (2 s).

### Metric hygiene
**Source:** `docs/decisions/0004-metrics-label-policy.md`. Allowed labels are `result,status,rule_id,severity,reason,group,topic,partition,service`. Lag is position-based.
**Apply to:** all telemetry, dashboards and alert rules. The guard test lives in `telemetry` plus `scripts/test_observability_config.py`.

### Go test conventions
External `_test` packages (`package opshttp_test`, `ingest_test`), testify `require`, `t.Context()`, `wg.Go`, and `t.Helper()` in helpers. Integration tests use `//go:build integration` in `backend/test/` with `sentineltesting` helpers. Phase 4 binary tests use `//go:build phase4` in `backend/testphase4/`.

### Web conventions
`useSession()` → `{session, client}`, `useQuery` with `retry: false` and key `[name, session.generation]`, copy as literal strings, `role="status"`/`role="alert"`, and tests through the real `<App>` with a stubbed `fetch`. Shape validation in `client.ts` throws `APIError(503)`.

### Python conventions
Stdlib plus `httpx` only. Safe error classes (`SendError`/`SeedError`) with display-safe messages. Injected clocks, sleepers and jitter. Private files 0600/0700. Bounded subprocess with timeouts. Tests use pytest + `httpx.MockTransport` and pass mypy/ruff.

## No Analog Found

| File | Role | Data Flow | Reason / Use instead |
|---|---|---|---|
| `backend/internal/telemetry/tracing.go` | provider | batch export | No OTel code in repo. RESEARCH Pattern 3 + "Sampler config" snippet. Lifecycle registration per the shared pattern above |
| `backend/internal/kafkax/lag.go` (computation) | service | polling | No position-lag code. RESEARCH Pattern 2. Use the kadm and callback analogs cited above for plumbing only |
| `observability/prometheus/{prometheus,alerts,alerts_test}.yml` | config | — | No Prometheus config exists. RESEARCH Pattern 6 + `promtool` in the pinned image |
| `observability/grafana/**` | config | — | RESEARCH Pattern 6 (uid-referenced datasource, file provider) |
| Jaeger service in the observability overlay | config | — | RESEARCH Standard Stack (pinned `jaegertracing/jaeger:2.22.0@sha256:...`, bounded `max_traces`) |
| `generator/.../bench/sender.py` (open-loop core) | service | streaming | The existing sender is closed-loop and synchronous. RESEARCH Pattern 8 "Open-loop scheduling core" |
| `bench/report.py` (reconciliation math) | utility | transform | RESEARCH Pattern 9 equations. Only the refuse-on-mismatch style comes from `seed_scenarios.py` |

## Metadata

**Analog search scope:** `backend/internal/{opshttp,logger,middleware,app,config,kafkax,eventwriter,alertwriter,detector,alertstream,ingest,query,auth,testing}`, `backend/cmd/*`, `backend/test`, `backend/testphase4`, `web/src/{App.tsx,pages,api}`, `deployments/compose`, `Makefile`, `scripts/*`, `generator/src`, `generator/tests`, `api/openapi.yaml`, `docs/decisions`
**Files scanned:** ~45
**Pattern extraction date:** 2026-10-30
