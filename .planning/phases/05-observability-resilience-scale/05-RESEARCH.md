# Phase 5: Observability, Resilience & Scale - Research

**Researched:** 2026-10-30
**Domain:** Prometheus/OpenTelemetry instrumentation of a Go/franz-go Kafka pipeline, Grafana-as-code, failure-injection testing, open-loop benchmarking with exact reconciliation
**Confidence:** HIGH for the codebase audit and capacity math (read from source). MEDIUM for library APIs (Go proxy and pkg.go.dev verified, not Context7). LOW only where marked.

<user_constraints>
## User Constraints (from CONTEXT.md)

No CONTEXT.md exists. The user chose to continue without discuss-phase. Because there are no locked user decisions, the binding constraints are the spec, CLAUDE.md and the STATE.md holds listed below. Items in "Open Questions" need a user or planner decision.

### Locked Decisions
None from discuss-phase. Treat the STATE.md binding decisions (below, under Project Constraints) as locked.

### Claude's Discretion
Everything not fixed by spec/CLAUDE.md/STATE.md: library choice inside the mandated stack, dashboard layout, bench harness design, test organization.

### Deferred Ideas (OUT OF SCOPE)
Kubernetes manifests, hardened CI and the full docs refresh belong to Phase 6 (DEP-01..05). Aegis and ML are v2. Do not raise detector admission constants, which are Phase 3 contracts (see Pitfall 1).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| OBS-02 | Spec metrics on internal `/metrics`, no high-cardinality labels, position-based lag | Audit §A1 (only one hand-rolled counter exists). Pattern 1 (registry injection), Pattern 2 (lag tracker), label-allowlist guard test |
| OBS-03 | JSON logs with service/timestamp/level/request_id/trace_id, scrubbed, no secrets or bodies | Audit §A2 (`local` env forces ConsoleWriter, field is `time` not `timestamp`, no trace_id). Pattern 4 |
| OBS-04 | OTel spans for ingest/publish/detect/store, Kafka-header propagation, configurable sampling | Audit §A3 (zero OTel code). Pattern 3 (batch span + header carrier + ParentBased ratio sampler) |
| OBS-05 | Grafana dashboards as code plus Prometheus alert examples (lag, ingest errors, write errors, restart loops) | Stack: Prometheus v3.15.1, Grafana 13.2.5 provisioning. Pattern 6. `promtool check/test rules` |
| REL-01 | Backoff + jitter + bounded retries + terminal handling | Audit §A4: event-writer has full jitter. Alert-writer and detector poison retries have NO jitter. Shared `internal/retry` |
| REL-02 | Graceful shutdown commits only completed offsets | Audit §A5: Lifecycle/DrainCloser exist. Compose lacks `stop_grace_period` (Docker default 10s < 30s shutdown budget) |
| REL-03 | Failure suites: writer restart mid-batch, CH unreachable, Kafka broker restart, slow consumer, DLQ, SSE reconnect, rate-limit overload | Audit §A6: most exist. Missing: true Kafka broker restart (only pause/unpause allowlisted), a named suite selector, and a slow-consumer lag assertion |
| REL-04 | Bounded memory/queues under overload, expected 429/503 | Existing in-flight cap and token bucket. Add buffer/RSS gauges plus a bench overload scenario with assertions |
| BENCH-01 | `make bench`, bounded, configurable, multi-replica, open-loop, benign+malicious mix | Pattern 7 (isolated bench Compose project). Pattern 8 (asyncio open-loop sender) |
| BENCH-02 | ≥1M events. Reconcile sent/accepted/rejected/unique-persisted and expected vs observed hits | Pattern 9 (per-run tenant, exact equations). Pitfall 1/2 (detector capacity, entity pools) |
| BENCH-03 | `bench/report.py` writes `benchmarks/results/<date>-<scenario>.md` and refuses on mismatch | Pattern 9/10, stdlib-only report |
| BENCH-04 | Detector restart under load recovers, with measured recovery time | Pattern 11 (catch-up formula, measurement definition, session timeout) |
| UI-05 | System page: pipeline/consumer health, throughput, lag, errors, optional Grafana link | Pattern 5 (query-api `GET /api/v1/system` with fixed PromQL, admin-only recommendation). Also wires the Overview lag placeholder |
</phase_requirements>

## Project Constraints (from CLAUDE.md, spec and STATE.md)

**CLAUDE.md / spec:**
- Delivery is at-least-once. Commit offsets only after effects succeed, and every consumer is idempotent. Make no exactly-once claims.
- No unbounded buffering. Shed with 429/503 and cap producer queues, CH batches and per-key state.
- **Metrics hygiene:** no IP, user, event ID, request ID or tenant labels (ADR 0004 allowlist: `result,status,rule_id,severity,reason,group,topic,partition,service`). A new label needs an ADR 0004 amendment.
- DoD: workflow works end-to-end, tests cover success/invalid/failure cases, CI is green, and there are no stubs, fake analytics or invented numbers. Every perf claim must link to a result file.
- Pinned versions: tags plus sha256 digests in Compose (`scripts/check-pins.sh` enforces this, but only for `docker-compose.yml`). go.sum and lockfiles.
- golangci-lint: `gochecknoglobals`, `gochecknoinits`, `mnd`, `funlen` 100/50, `cyclop` 30, `gocognit` 20, `testpackage`, `errorlint`, `nolintlint`. The config already pre-allows `prometheus.*Opts` struct literals and the `ExponentialBuckets/LinearBuckets` magic numbers (`backend/.golangci.yml:106,172`).
- `server` must not import router/handler. Inject dependencies through constructors, with no globals or `init()`.
- Never read `backend/.env` or the root `.env`.

**STATE.md binding holds that constrain native and benchmark runs** [VERIFIED: .planning/STATE.md, 04-07-STACK-HOLD.md]:
- No volume reset, topic deletion, `OPTIMIZE`, offset/group/fence reset, history rewrite, broad secret-scan bypass, speculative deadline increase or invented proof. "No further prune/reset/guard change."
- Two existing local stacks must not be touched: the **old** project `sentinel` (ports 29092/18123/19000/15432/16379/8080/8081/9101-9105) and the **NEW** isolated project `sentinel-p4-demo-20261024` (PG 25432, CH 28123/29000, Redis 26379, Kafka 39092, API 18081, Vite 15173). The NEW detector holds "checkpoint history lost" (pin below retained start). Do not use default Make/runtime/seed targets against either stack, and never run a stale migrator image.
- Preserve the user's dirty `.planning/config.json` and `.serena/` unstaged. Only root handles remote pushes.
- Phase 4 is still open (04-14 acceptance, 04-15, 04-16). Phase 5 plans must not edit Phase 4-owned files in ways that invalidate those pending acceptances. `Overview.tsx`, `App.tsx`, `api/openapi.yaml` and `query-api/main.go` are shared surfaces, so sequence the edits after Phase 4 closes or declare explicit ownership.

## Summary

Phase 5 is mostly **new instrumentation and a new benchmark harness on top of a correct but nearly un-instrumented pipeline**. The codebase has no Prometheus client, no OpenTelemetry code, no Prometheus/Grafana/trace backend in Compose, and a `make bench` that exits 1. The only metric is a hand-rolled `detection_late_events_total` text line. Logs are JSON only outside `local` (and Compose sets `local`). The timestamp field is `time`, not `timestamp`, and there is no `trace_id`. Resilience is much further along. Event-writer retries use crypto full jitter. Graceful drain/lifecycle exists. Testcontainers fault helpers (pause/unpause for Kafka, CH, PG and Redis) and many failure tests already exist: writer crash mid-batch, insert failure with no commit, DLQ, rate limit, ingest shedding, SSE reconnect. The gaps are jitter in alert-writer and detector retries, a real Kafka broker *restart*, a slow-consumer lag assertion, and a single named failure-suite selector.

The dominant planning risk is **detector admission capacity, not ingest speed**. Detector bounds are process-wide constants: 10,000 windows, 100,000 prefix-ledger entries, 64 MiB of accounted state, and 4,096 IDs per window. The ledger holds one entry per source offset from the oldest open window's first offset forward. With benign `auth.failure` traffic, the AUTH 300 s window plus 30 s lateness makes that horizon up to 330 s. So **one detector process can sustain at most about 100,000 / 330 ≈ 300 events/s of event-time-dense input** before it hits capacity, turns not-ready and terminates. After a replica is killed, the survivors absorb its partitions. The current generator draws IPs uniformly from three /24s plus an IPv6 /32 and spaces events 1 s apart *backwards* from run start. That means: (a) almost every event opens a new window, which caps a process near 100 ev/s, and (b) a 1M-event run starts about 11.6 days in the past, which ingest rejects (past skew 168 h). The benchmark therefore needs a **new live-time, bounded-entity-pool, open-loop generator mode** and a **capacity preflight**. The resulting ceiling (about 300 ev/s/process) is itself the honest "scaling bottleneck" spec §17 step 9 asks the report to explain.

The second risk is the **environment**. The root filesystem is 99% full (2.6 GB free of 183 GB), and the host has 4 GB of RAM available out of 15 GB. Another unrelated stack (`devrag-stack`) is running, and both Sentinel stacks are in restart loops because their infra containers are missing. A 1M-event, multi-replica run plus image builds will not fit safely. The user must free disk space; the agent must not prune.

**Primary recommendation:** Build one shared `internal/telemetry` package (Prometheus registry + OTel tracer provider + log hook) injected through constructors. Add position-based lag and detector capacity gauges. Ship Prometheus/Grafana/Jaeger as a Compose overlay. Run benchmarks only in a separate disposable Compose project `sentinel-bench-<run>` with its own ports and private env. Use a Python asyncio open-loop sender with live event time and bounded entity pools. Use a stdlib-only `bench/report.py` that recomputes every reconciliation equation from raw JSON and refuses to emit on mismatch.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Metric emission (ingest, lag, delay, CH write, DLQ, SSE) | Go services (each binary's ops listener `/metrics`) | — | Only the process knows its positions and durations. Ops port is internal-only |
| Position-based consumer lag | Go consumers (kafkax) | Prometheus (aggregation) | Processed position lives in the worker. End offsets come from a periodic kadm query |
| Trace creation and propagation | Go services + Kafka record headers | Jaeger (storage/UI) | W3C traceparent travels in headers. The backend is replaceable |
| Dashboards and alert rules | Prometheus/Grafana config-as-code (`observability/`) | — | Spec FR-15. Files are provisioned, not clicked |
| System page data | query-api (fixed server-side PromQL + readiness) | Browser (render only) | Browser never talks to Prometheus and never sends PromQL. Auth/role enforced server-side |
| Load generation and latency capture | Python bench harness (host) | — | Spec mandates Python for generator/reports. The client side owns intended-time stamps |
| Reconciliation and report | `bench/report.py` (host, offline over raw JSON) | CH/PG/Kafka read-only queries | Report must be reproducible from saved raw measurements |
| Failure injection | Go integration tests (testcontainers) and bench harness (docker kill) | — | Deterministic, isolated containers, never shared stacks |

## Codebase Audit — what ALREADY exists (build on it, don't duplicate)

### A1. Metrics [VERIFIED: source]
- `backend/internal/opshttp/metrics.go` is a hand-rolled `DetectionMetrics{late atomic.Uint64}` writing `detection_late_events_total` as text. `/metrics` is mounted **only when** `WithDetectionMetrics` is passed (detector only). Consumers of that name: `docs/operations.md`, `docs/architecture.md`, `scripts/seed_scenarios.py`, `backend/test/detector_capacity_test.go`, `opshttp/metrics_test.go`. A rename must update all five.
- **No** `github.com/prometheus/client_golang` in `backend/go.mod` (only an indirect copy inside `tools.mod` via golangci). The OpenAPI `/metrics` op is documented as "internal network only".
- Ops listeners per service: ingest 9101, detector 9102, event-writer 9103, alert-writer 9104, query-api 9105, published on 127.0.0.1 only.

### A2. Logging [VERIFIED: backend/internal/logger/logger.go, middleware/*]
- zerolog v1.35.1. `NewWithWriter` uses **ConsoleWriter when `Environment == "local"`**, and Compose sets `SENTINEL_PRIMARY.ENV: local`, so Compose logs are not JSON. `Logging.Format` exists but is ignored.
- `.Timestamp()` emits field `time`, but spec §11.3 requires `timestamp`. There is no `trace_id`.
- The request logger logs `request_id` twice (context logger plus explicit `.Str`), which produces duplicate JSON keys. It also logs `ip` (`c.RealIP()`) and `user_id`. It does not log bodies or headers (good).
- `NewPgxLogger` is always a ConsoleWriter.

### A3. Tracing [VERIFIED: grep]
- Zero OTel code. `go.opentelemetry.io/otel` v1.46.0, `otelhttp` v0.69.0 and the trace/metric modules are present only as **indirect** dependencies (from testcontainers).
- Event records already carry headers `schema_version` and `request_id` (`kafkax/producer.go:166`). Alerts carry no headers. The event-writer DLQ copies non-`dlq.*` source headers (bounded at 128), so a `traceparent` header would be copied into DLQ records. That is acceptable and useful.

### A4. Retry / backoff [VERIFIED: source]
- `eventwriter/writer.go`: exponential ×2, min 100 ms, max 5 s, 5 attempts, full jitter via `crypto/rand` (`jitterDelay`). Terminal error → process failure → Compose `restart: unless-stopped`.
- `alertwriter/poison.go` `retry()` and `detector/poison.go` use exponential backoff **without jitter** (`time.NewTimer(retries.backoff)`). This is the REL-01 gap.
- `alertstream/delivery.go` and `listener.go`: 100 ms..5 s backoff, no jitter.
- The Python sender (`generator/send.py`) already has bounded attempts (5), a total retry budget (120 s), `Retry-After` honoring and 0.5–1.5× jitter.

### A5. Shutdown [VERIFIED: app/app.go, cmd/*/main.go]
- `app.Lifecycle` runs LIFO closers with a hardcoded `shutdownTimeout = 30s`, and `opshttp.DrainCloser` flips readiness to draining. Worker stop defaults to `defaultWriterStop = 20s`.
- **Compose has no `stop_grace_period`**, so Docker sends SIGKILL after 10 s by default. Graceful-shutdown proof under Compose needs `stop_grace_period: 40s` on workers.
- Kafka consumer `SessionTimeout` is a constant 45 s (`kafkax/consumer.go:16`, detector consumer too). This dominates SIGKILL failover time (Pattern 11).

### A6. Failure tests already present (backend/test, `-tags=integration`, testcontainers) [VERIFIED: grep]
| Spec §15 failure | Existing test(s) | Gap |
|---|---|---|
| Event-writer restart mid-batch | `TestWriterCrashRestart` (SIGKILL at after-insert barrier, merges stopped), `TestWriterCommitAfterInsert` | Mostly covered. Phase 5 can reference these |
| CH temporarily unreachable | `TestWriterInsertFailureNoCommit` (CH fault container pause) | Add a "recovers after unpause and completes without loss" variant if absent |
| Kafka broker restart | Only `Pause/Unpause` (`internal/testing/containers.go` allowlists `pause`/`unpause`) | **Missing**. Need real stop/start of a dedicated broker (fixed host port is already bound, so the address survives) |
| Slow consumer | `TestWriterSlowPoisonRebalance` | Add lag-gauge growth/recovery assertion once metrics exist |
| Malformed → DLQ | `TestWriterPoisonToDLQ`, `TestDetectorPoison`, `TestAlertWriterPoison`, `TestWriterDLQFailureNoCommit` | Covered |
| SSE disconnect/reconnect | `testphase4` `TestPhase4Stream`/`TestPhase4Delivery` (build tag `phase4`), web tests | Covered by Phase 4. The Phase 5 suite should invoke, not duplicate |
| Rate-limit overload | `TestRateLimit*`, `TestIngestShedsUnderLoad` (503 + `Retry-After: 1`) | Add memory/queue-bounded assertions (REL-04) |
| Detector crash boundaries | `TestDetectorCrashBoundaries`, `TestDetectorMidWindowRecovery`, `TestDetectorThirdReplicaRebalance`, `TestDetectorCapacity` | Covered (Phase 3) |

`make test-integ` already has a 32 m ceiling, so do not inflate it. Add a separate `make test-chaos` selector with its own finite ceiling and a nonempty-match guard (`scripts/go-test-nonempty.sh`).

### A7. Load shedding and limits [VERIFIED: config.go]
- Ingest `max_in_flight` 64 → 503 + `Retry-After: 1`.
- Redis token bucket: cap 10,000 tokens, **refill 2,000 events/s per key**, fail-closed by default.
- Producer caps: 50,000 records / 64 MiB. Writer batch 5,000, flush 1 s, fetch caps 16 MiB / 4 MiB per partition.
- Detector resources are validated so the aggregate is ≤ 256 MiB. Detector and alert-writer containers have `mem_limit: 256m, cpus: 1.0`.

### A8. Generator [VERIFIED: generator/src]
- Python 3.13 (uv-managed, `httpx==0.28.1` is the only runtime dependency) with a synchronous closed-loop `send_events` and `MAX_EVENT_COUNT = 1_000_000`.
- `generate_events` sets `base_time = run_start − count seconds`, which puts a 1M-event run 11.6 days in the past and is **rejected by ingest past-skew 168 h**.
- `_random_ip` is uniform over 192.0.2/24, 198.51.100/24, 203.0.113/24 and **2001:db8::/32**, so source IPs are effectively unique. Principals are `user-0000..9999`. The event mix is uniform over 5 types. Benign `auth.failure` events carry `action=deny,outcome=failure`.
- `scenarios.py`/`live_schedule.py` contain Phase 3 live scenario builders (threat bursts, `auth.success` window-advance markers, a 15-minute orchestration deadline). Reuse them for injected attacks.

### A9. Storage dedupe [VERIFIED: migrations, storage/clickhouse]
- `events`: ReplacingMergeTree, `PARTITION BY toYYYYMM(occurred_at)`, `ORDER BY (tenant_id, occurred_at, event_id)`, TTL 7 days, bloom filter on `event_id`.
- Reads collapse with `GROUP BY tenant_id,event_id`. Counts use `uniqExact(event_id)`. **`FINAL` is never used** (`alerts.go:85` comment says so explicitly).
- PG `alert_workflows.created_at` is **event-time derived (window_end)**. There is no wall-clock persistence column, so detection→persistence latency cannot be read from storage (Pattern 10).

### A10. Web [VERIFIED: web/src]
- React 19.3, React Query 5.104.5, openapi-fetch 0.17, Vite 8.3.4, Vitest 5.0.3. Pages: Overview, Alerts, Events, Rules. **No System page.**
- `Overview.tsx:183` renders "Consumer lag — available in Phase 5", and OpenAPI `getMetricsSummary` says "Consumer lag is unavailable until Phase 5".
- UI-SPEC: "No System link until Phase 5". Sidebar 224 px; tokens defined in `04-UI-SPEC.md`. Reuse them.

### A11. Compose and Make [VERIFIED]
- Images: `apache/kafka:4.3.1`, `clickhouse/clickhouse-server:26.3.47.6`, `postgres:18.6`, `redis:8.10.2`, distroless static runtime, `golang:1.26.9` build (all digest-pinned).
- Fixed host ports on every service block `--scale`. Topics are created by `topic-init` with `EVENTS_PARTITIONS` (default 6), events retention 24 h, alerts/DLQ 7 d, RF 1.
- No Prometheus, Grafana, trace backend or web container.
- `make bench` prints "bench is available from Phase 5" and exits 1. `benchmarks/README.md` is a placeholder, and `benchmarks/results/` does not exist.

## Standard Stack

### Core (Go, add to `backend/go.mod`)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/prometheus/client_golang` | v1.25.0 (2026-10-08, requires go 1.26.0, which matches the module) | Registry, Counter/Gauge/HistogramVec, `promhttp.HandlerFor`, Go/process collectors, `api/prometheus/v1` query client | Official Prometheus Go client [VERIFIED: proxy.golang.org origin github.com/prometheus/client_golang] |
| `go.opentelemetry.io/otel` + `/sdk` + `/trace` | v1.47.0 (2026-10-02) | TracerProvider, `ParentBased(TraceIDRatioBased)`, BatchSpanProcessor, `propagation.TraceContext` | Official OTel Go [VERIFIED: proxy.golang.org] |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | v1.47.0 | OTLP/HTTP export to Jaeger (4318) | Avoids pulling gRPC. Jaeger v2 accepts OTLP natively [VERIFIED: proxy.golang.org] |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v0.72.0 | Server spans around the Echo `http.Handler` (ingest, query-api) | Already an indirect dependency. **`otelecho` is deprecated** [VERIFIED: its go.mod says "Deprecated: Use github.com/labstack/echo-opentelemetry"], and `echo-opentelemetry` is itself deprecated in favor of `labstack/echo-otel/v4` (v0.0.x, too new). Use otelhttp plus a 5-line middleware that renames the span to `c.Path()` |
| `github.com/twmb/franz-go/pkg/kadm` | v1.19.0 (already a dependency) | `ListEndOffsets` for position-based lag, `ListStartOffsets` for pin-retention margin | Already used. No new dependency |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/twmb/franz-go/plugin/kotel` | v1.7.1 | Per-record produce/fetch spans plus `NewRecordCarrier` | **Do not adopt by default.** Its hooks create a span per record (a 500-event batch makes 500 spans) and it pins otel/sdk v1.34 transitively. A ~20-line `[]kgo.RecordHeader` TextMapCarrier in kafkax is enough [VERIFIED: pkg.go.dev kotel] |
| `github.com/twmb/franz-go/plugin/kprom` | v1.5.0 | franz-go client metrics | Optional. Not required by the spec. Its labels (node_id/topic) are bounded |
| `github.com/exaring/otelpgx` | v0.13.0 | pgx spans | Optional. The spec asks for ingest/publish/detect/store spans only. Skip for MVP |

### Infrastructure images (bench/observability Compose overlay)
| Image | Tag@digest | Purpose |
|-------|-----------|---------|
| `prom/prometheus` | `v3.15.1@sha256:fa3302279710be2f21a8301ec17a8b4f2301fd829bd113839b3b6b02b6f191a2` (2026-10-21) | Scrape, rules, `promtool` for CI rule tests [VERIFIED: Docker Hub API] |
| `grafana/grafana` | `13.2.5@sha256:9f7aa1e1e3246aade5792d35a738049f7f32f89cd51d1956cf426a4e86268a84` (2026-10-27) | File-provisioned datasources and dashboards [VERIFIED: Docker Hub API] |
| `jaegertracing/jaeger` | `2.22.0@sha256:836b967b5abcf1f7758066aa6b348619267a2f1459f72d472840570f9bd88a17` (2026-10-06) | OTLP receiver plus in-memory trace store (bounded `max_traces`) [VERIFIED: Docker Hub API] |

Digests are index digests read from the Docker Hub API on 2026-10-30. Re-read them with `docker buildx imagetools inspect` at execution time and record the values.

### Python (no new packages)
- Bench harness and `bench/report.py` use **stdlib plus the existing `httpx==0.28.1`** (`httpx.AsyncClient` for open-loop sends and SSE streaming). Compute exact percentiles from raw samples (≤ about 10k requests). No HdrHistogram or numpy is needed.

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Jaeger all-in-one | Tempo + Grafana, or OTel Collector | More services and memory on a RAM-starved host. Jaeger is one container |
| Python asyncio sender | vegeta (installed at `~/.local/bin/vegeta`, version unknown, unpinned), k6 | Spec mandates a Python generator. The request rate is low (≤ 10 req/s at 500-event batches), so Python is not the bottleneck. vegeta cannot generate per-event deterministic bodies or record per-batch intended time with event IDs |
| query-api → Prometheus for System page | query-api computing lag directly via kadm | kadm gives **committed** lag, and the detector commits a pin up to 330 s behind by design, so the System page would show permanent false lag. Position lives in workers, so Prometheus is the source |
| docker stats sampling for resources | cAdvisor | An extra privileged container. `docker stats --no-stream --format json` plus `process_resident_memory_bytes` is enough |

**Installation:**
```bash
cd backend && go get github.com/prometheus/client_golang@v1.25.0 \
  go.opentelemetry.io/otel@v1.47.0 go.opentelemetry.io/otel/sdk@v1.47.0 \
  go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.47.0 \
  go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.72.0 && go mod tidy
```
Then run `scripts/check-go-dependencies.sh` (prohibited list: newrelic, pkg/errors, clerk, asynq, resend; none of these are pulled) and `go mod tidy -diff` (CI).

## Package Legitimacy Audit

slopcheck could not be installed (the sandbox classifier denied the pip install). Per protocol, **every package below is `[ASSUMED]`** and the planner must gate first install behind `checkpoint:human-verify`. Provenance evidence: each Go module resolved on proxy.golang.org with `Origin.URL` pointing at the official org repo.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| github.com/prometheus/client_golang v1.25.0 | Go proxy | ~11 yrs | n/a (Go) | github.com/prometheus/client_golang | unavailable | Approved pending checkpoint [ASSUMED] |
| go.opentelemetry.io/otel{,/sdk,/trace} v1.47.0 | Go proxy | ~6 yrs | n/a | github.com/open-telemetry/opentelemetry-go | unavailable | Approved pending checkpoint [ASSUMED] |
| go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.47.0 | Go proxy | ~5 yrs | n/a | same repo, Subdir exporters/... | unavailable | Approved pending checkpoint [ASSUMED] |
| go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.72.0 | Go proxy | ~5 yrs (already indirect at v0.69.0) | n/a | github.com/open-telemetry/opentelemetry-go-contrib | unavailable | Approved pending checkpoint [ASSUMED] |
| prom/prometheus, grafana/grafana, jaegertracing/jaeger images | Docker Hub | official | n/a | official orgs | n/a | Pin by digest |

**Packages removed due to [SLOP]:** none. **Flagged [SUS]:** none. `labstack/echo-opentelemetry` (v0.0.4, deprecated) and `otelecho` (deprecated) are **not recommended**.

## Architecture Patterns

### System Architecture Diagram

```
 bench harness (host, Python asyncio, open-loop)            operator browser
   │ schedule t_i ─► POST /api/v1/events:batch (traceparent)     │ JWT
   │ records intended/sent/acked times, statuses, event_ids      ▼
   ▼                                                    web (Vite) ─► query-api ──► GET /api/v1/system
 ingest ×N (otelhttp span "ingest" ─► span "publish")              │  fixed PromQL   │ readiness probes
   │  injects traceparent into every record header                 ▼                 ▼
   ▼                                                        Prometheus ◄── scrape /metrics (all replicas, dns_sd)
 Kafka security.events.v1 (P partitions)                         │ rules: lag, error rate, write errors, restarts, capacity
   ├──► event-writer ×M: batch → CH insert (span "store", links ≤32 sampled parents)
   │        poison → security.events.dlq.v1
   │        lag tracker: processed_next vs ListEndOffsets
   └──► detector ×K: per-record span "detect" (only if parent sampled)
            capacity gauges (windows/ledger/bytes), delay histogram
            allow ─► PG decision ─► Kafka security.alerts.v1 (traceparent of trigger record)
                                         └──► alert-writer: CH alert + PG workflow (span "store_alert")
                                                    └──► PG NOTIFY ─► query-api SSE ─► bench SSE subscriber (t_notify)
 OTLP/HTTP spans from all services ─► Jaeger (bounded in-memory)
 bench report.py ◄── raw JSON (client log, Prom snapshots, CH/PG/Kafka read-only counts, docker stats)
```

### Recommended Project Structure (new/changed only)
```
backend/internal/telemetry/      # NEW: Registry (prometheus.Registry + collectors), Tracer setup/shutdown, log hook
backend/internal/telemetry/metrics.go   # typed metric structs per service (IngestMetrics, ConsumerMetrics, DetectorMetrics, SSEMetrics)
backend/internal/kafkax/headers.go      # NEW: TextMapCarrier over []kgo.RecordHeader
backend/internal/kafkax/lag.go          # NEW: position tracker + periodic kadm.ListEndOffsets
backend/internal/retry/                 # NEW: shared Policy{Min,Max,Attempts}, full jitter, ctx-aware wait
backend/internal/query/system.go + handler   # NEW: GET /api/v1/system (fixed PromQL, readiness)
backend/test/chaos_*_test.go            # NEW (build tag `chaos` or `integration` + selector): broker restart, CH outage recovery, slow consumer lag, overload bounds
observability/prometheus/prometheus.yml # scrape configs (dns_sd per service)
observability/prometheus/alerts.yml     # alert examples; alerts_test.yml for `promtool test rules`
observability/grafana/provisioning/{datasources,dashboards}/*.yml
observability/grafana/dashboards/sentinel-pipeline.json
deployments/compose/docker-compose.observability.yml   # prometheus, grafana, jaeger (127.0.0.1 ports)
deployments/compose/docker-compose.bench.yml           # replicas, !reset ports, stop_grace_period, bench limits
generator/src/sentinel_generator/bench/   # open-loop sender, live-time event model, entity pools, attack plan, SSE subscriber
bench/report.py                           # stdlib-only reconciliation + Markdown/JSON
bench/run.py (or scripts/bench.py)        # orchestration: isolated project up, keys, run, kill detector, collect, down
benchmarks/results/<date>-<scenario>.md + .json
web/src/pages/System.tsx (+ test)
docs/decisions/0010-observability-tracing.md, 0011-benchmark-method.md (+ ADR 0004 amendment if new labels)
```

### Pattern 1: Injected per-process Prometheus registry (no globals)
**What:** Each binary builds a `*prometheus.Registry` in `main`, registers `collectors.NewGoCollector()` and `collectors.NewProcessCollector(...)`, constructs typed metric structs, passes them to constructors, and mounts `promhttp.HandlerFor(reg, promhttp.HandlerOpts{})` on the ops mux for **every** service, not just the detector.
**Why:** `gochecknoglobals` forbids `promauto` package-level vars, and per-test registries make metrics unit-testable.
**Spec metric map:**
| Metric | Owner | Labels (ADR 0004) | Observation point |
|---|---|---|---|
| `sentinel_ingest_events_total` | ingest | `result` ∈ {accepted, rejected, rate_limited, shed, unavailable} | after Kafka ack / rejection |
| `sentinel_ingest_requests_total` | ingest | `status` (HTTP code class or exact code; bounded set) | request logger end |
| `sentinel_ingest_request_duration_seconds` | ingest | none (or `status`) | histogram |
| `sentinel_kafka_consumer_lag` | event-writer, detector, alert-writer | `group,topic,partition` | gauge = end_offset − processed_next |
| `sentinel_detection_alerts_total` | detector | `rule_id,severity` | after allow publish ack (counts publications) |
| `sentinel_detection_delay_seconds` | detector | none/`rule_id` | wall now − trigger occurred_at at publish ack |
| `sentinel_clickhouse_write_duration_seconds` | event-writer, alert-writer | `result` ∈ {ok,error} (`service` comes from scrape job) | around InsertBatch |
| `sentinel_dlq_events_total` | event-writer, detector, alert-writer | `reason` (existing enum) | after DLQ publish ack |
| `sentinel_sse_clients` | query-api | none | gauge inc/dec in stream handler |
| (extra, recommended) `sentinel_detection_late_events_total` | detector | none | replaces `detection_late_events_total` (update 5 consumers) |
| (extra) `sentinel_detector_state_{windows,ledger_entries,bytes}` + `_limit` | detector | none | from `Bounds` after each acknowledge |
| (extra) `sentinel_detector_pin_retention_margin_offsets` | detector | `topic,partition` | pin offset − log start offset (kadm.ListStartOffsets, periodic). Directly detects the "checkpoint history lost" hazard before it happens |
| (extra) `sentinel_kafka_producer_buffered_records` | ingest, detector | none | `client.BufferedProduceRecords()` (REL-04 proof) |
| (extra) `sentinel_retries_total` | workers | `operation` (new label → ADR 0004 amendment) or `reason` | each retry |

### Pattern 2: Position-based lag that stays truthful when the worker is stuck
**What:** The worker publishes `processed_next` per owned partition (an atomic set after effects are acknowledged; for the detector use `processedNext`, **not the pin**). A background goroutine in the consumer process calls `kadm.ListEndOffsets(ctx, topic)` every 5–10 s with a 2 s timeout and sets the gauge `end − processed_next`.
**Why not fetch HWM only:** `FetchTopicPartition.HighWatermark` updates only while polling. During a CH outage the writer is in `insertWithRetry` and not polling, so lag would freeze exactly when it should rise. **Why not kadm.Lag:** that is committed-offset lag. Detector commits trail by up to 330 s by design (ADR 0004 explicitly rejects it).
**Revocation:** on revoke/lost callbacks call `GaugeVec.DeleteLabelValues(group, topic, partition)`, or stale series survive on the old owner and double-count in `sum()`.

### Pattern 3: Tracing — one batch span plus header propagation, sampled at the edge
- **Sampler:** `sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))`. `SENTINEL_OBSERVABILITY.TRACING.{ENABLED,SAMPLE_RATIO,OTLP_ENDPOINT}` goes through koanf (default disabled or 0.01 locally; 0 disables export). Workers use the same ParentBased sampler, so they follow the edge decision. Unsampled spans still have valid trace IDs, so `trace_id` can be logged for every request [CITED: OTel Go SDK sampler semantics, ASSUMED detail for non-recording span IDs → verify in a unit test].
- **ingest:** otelhttp server span (renamed to the route) → child span `publish` around `PublishBatch` → `propagation.TraceContext{}.Inject(ctx, headerCarrier(&record.Headers))` for each record (same traceparent for the batch).
- **detector:** per record, `Extract` → if `SpanContextFromContext(ctx).IsSampled()` start a `detect` span. On allow, inject that context into the alert record header.
- **event-writer:** one `store` span per CH batch with `trace.WithLinks(...)` to at most 32 sampled record contexts (span links, not parent, because a batch has many parents).
- **alert-writer:** extract the alert record header → `store_alert` span child.
- **BatchSpanProcessor** is bounded (default queue 2048, drops when full), which is compliant with bounded-resources rules. Call `tp.Shutdown(ctx)` as a Lifecycle closer **before** the Kafka clients close.
- Do not put tenant/IP/principal in span attributes unless scrubbed. Spans are not metrics, but the threat model still applies. Allowed: rule_id, partition, offset, batch size.

### Pattern 4: Logging to spec
- A config switch honors `Logging.Format` (`json` default in every env). ConsoleWriter is used only when format=`console`, set explicitly for interactive `make run`.
- Use a zerolog `Hook` that adds `timestamp` (UTC RFC3339Nano) instead of `.Timestamp()`, which avoids mutating the global `zerolog.TimestampFieldName`.
- `trace_id` (and `span_id`) is added to the request-scoped logger in a middleware placed **after** the otelhttp handler. Worker batch loggers add `trace_id` when processing a record context.
- Remove the duplicate `request_id`. Keep never logging bodies, Authorization or X-API-Key. Add a test that captures logs from a request with credentials and asserts the secret is absent and the line is valid JSON with all five required fields.
- Decide whether `ip` stays in request logs (ADR or threat-model note). It is allowed in logs but is a personal-data field.

### Pattern 5: System page via query-api (UI-05) and the Overview lag panel
- `GET /api/v1/system` (OpenAPI first, then `npm run gen:api`). The server runs a **fixed allowlist of PromQL** through `client_golang/api/prometheus/v1` with a 2 s timeout:
  - `sum by (job)(up)` vs expected replicas
  - `sum(rate(sentinel_ingest_events_total[1m])) by (result)`
  - 5xx/429/503 ratio
  - `max by (group)(sentinel_kafka_consumer_lag)` summed
  - `histogram_quantile(0.95, sum by (le)(rate(sentinel_detection_delay_seconds_bucket[5m])))`
  - CH write p95
  - `sum(rate(sentinel_dlq_events_total[5m]))`
  - restarts via `changes(process_start_time_seconds[15m])`
- Plus per-service readiness by HTTP GET to internal ops `/readyz` (bounded concurrency, 1 s timeout). The response says **"unavailable"** per field when Prometheus is down. Never use zeros (DoD forbids fake values).
- query-api readiness must **not** depend on Prometheus. Only the System section degrades.
- **Authorization:** the data is global, not tenant-scoped, and ingest volume leaks other tenants' activity. Recommend **admin-only** (`403` for viewer/analyst). See Open Question 2.
- An optional Grafana URL comes from config (`SENTINEL_OBSERVABILITY.GRAFANA_URL`) and renders as a plain link only when configured.
- Overview "Consumer lag" consumes the same endpoint (admin), or shows "requires operator role". It must not be a hardcoded value.

### Pattern 6: Dashboards and alerts as code
- Grafana provisioning: `datasources/prometheus.yml` (uid fixed, e.g. `sentinel-prom`) and `dashboards/sentinel.yml` (file provider path `/var/lib/grafana/dashboards`). The dashboard JSON references `datasource.uid`, not the name. Set `GF_AUTH_ANONYMOUS_ENABLED=false`, and give the admin password through the private env, never committed.
- Panels: ingest events/s by result, request error ratio, lag by group/partition, detection delay p50/p95/p99, CH write p95, DLQ/s by reason, SSE clients, detector capacity %, pin retention margin, restarts.
- Alerts (`alerts.yml`):
  - `SentinelConsumerLagSustained`: `max by (group)(sentinel_kafka_consumer_lag) > X for 5m`
  - `SentinelIngestErrorRate`: 5xx ratio > 5% for 5m
  - `SentinelClickHouseWriteErrors`: `rate(..._count{result="error"}[5m]) > 0 for 5m`
  - `SentinelRestartLoop`: `changes(process_start_time_seconds[15m]) > 2`
  - Add `SentinelDetectorCapacityHigh` (> 80%) and `SentinelPinRetentionLow`
- **Tests:** `promtool check config`, `promtool check rules` and `promtool test rules alerts_test.yml` run inside the pinned prom image (`docker run --rm --entrypoint promtool ...`). A Python unittest parses the dashboard JSON and asserts every expression references only registered metric names and no forbidden label names (guards both hygiene and drift).

### Pattern 7: Isolated, disposable bench stack (respects every STATE hold)
- `make bench` runs `docker compose -p sentinel-bench-<runid> -f docker-compose.yml -f docker-compose.observability.yml -f docker-compose.bench.yml --env-file .local/bench/<runid>/env`.
  - The env file is generated privately (0600, dir 0700) with random PG/CH/JWT secrets. It never reads or copies the root `.env`.
  - **Refuse** to start if the project name is `sentinel` or `sentinel-p4-demo-20261024`, or if any target host port is in use.
- The bench overlay uses `ports: !reset []` on scaled services (Compose merge supports `!reset`/`!override` [CITED: docs.docker.com/reference/compose-file/merge]) and publishes only:
  - ingest replicas on a range (e.g. `"127.0.0.1:38080-38082:8080"`), with the sender round-robining URLs (no load balancer)
  - query-api on a range
  - Prometheus/Grafana/Jaeger on distinct 3xxxx ports
  - `scripts/check-pins.sh` must be extended to scan **all** compose files, and its port regex must accept ranges (today it would silently skip `a-b:c`)
- Replicas: `deploy.replicas` or `--scale`: detector 3, event-writer 2, ingest 2, query-api 2, alert-writer 1. Use a fresh topic layout via `EVENTS_PARTITIONS` (e.g. 6 or 12; record it). Workers get `stop_grace_period: 40s`. Bench rate-limit config is explicit and recorded (Pattern 9).
- Prometheus scrapes replicas with `dns_sd_configs: [{names: [detector], type: A, port: 9102}]` (Compose DNS returns all replica IPs).
- Teardown: `make bench-down RUN=<id>` stops the project. Volume removal is a separate `CONFIRM=1` target **scoped to `sentinel-bench-*` only**. A bench stack left idle beyond 24 h hits the exact "checkpoint history lost" condition (Pitfall 6), so teardown is part of the run, not an afterthought.
- Image build: build once per source commit (`sentinel-bench:<gitsha>`), recorded in the report. Disk is the constraint (Environment section).

### Pattern 8: Open-loop sender (coordinated-omission safe)
- Precompute a schedule: batch *k* has intended send time `T0 + k·(B/R)`. Events inside a batch get `occurred_at = intended time` (live event time, so ingest skew is satisfied, and event-time-to-alert latency includes any client-side queueing).
- An asyncio task per batch **starts at its intended time regardless of earlier responses**. A semaphore caps in-flight requests (e.g. 64) to stay bounded. If the cap is hit, the wait is counted in latency (measured from intended time), and the harness records `schedule_slip_ms`. It never silently drops or reschedules.
- Per request, record `{batch_seq, intended_ns, sent_ns, done_ns, status, retry_after, attempt, event_ids_hash, accepted_count}`. Report **both** `done − intended` (CO-corrected response time) and `done − sent` (service time), plus the max slip. If slip exceeds a threshold (e.g. 1 s p99), the report flags "generator saturated" in Limitations [CITED: grafana.com/docs/k6 open-vs-closed: closed models suffer coordinated omission; arrival-rate executors drop and report iterations rather than hide them].
- **Retries:** 429/503/transport errors retry the *same bytes* (idempotent event_id) with `Retry-After`/jitter within a bounded per-batch budget that is **kept below ~20 s**. A retried batch delivered later than 30 s after its occurred_at is *late* for the detector (watermark = max − 30 s) and silently loses rule hits (Pitfall 4). Final non-acceptance is counted as `unaccepted`, never dropped from accounting.
- Use one process with multiple `httpx.AsyncClient`s (one per ingest URL). At ≤ 10 req/s Python is not the bottleneck. Record harness CPU% to prove it.

### Pattern 9: Reconciliation equations (`report.py` refuses to emit on any failure)
Use a unique tenant per run (`bench-<runid>`) and fresh producer keys (keyctl), so every count is exactly scoped.
1. `generated = Σ events in schedule` (≥ 1,000,000)
2. `generated = accepted_unique + rejected_4xx + unaccepted_final`. `accepted_unique` = distinct event_ids returned in 202 `event_ids`. Expected `rejected_4xx` is 0 (bench sends only valid events).
3. `kafka_records` (Δ end offsets on the fresh topic) ≥ `accepted_unique`. The excess is producer-side duplicates from 5xx retries and is reported.
4. `ch_unique = uniqExact(event_id) WHERE tenant_id = run` must **equal** `accepted_unique` → `loss = 0`. `ch_physical − ch_unique` = physical duplicates (reported, allowed under at-least-once).
5. Every accepted ID is present: compare a CH-side hash/aggregate (e.g. `groupBitXor(cityHash64(event_id))`) with the same function computed client-side, or do a set check in chunks. Do not rely only on counts.
6. Per rule: `expected_hits[rule] = injected attacks[rule]` (distinct entities, one window each, below 4096 events). `observed = count PG alert_workflows for tenant/rule` (via query-api list or read-only psql) and `CH uniq alert_id`. They must be equal. `benign_hits` must be 0, guaranteed by the pool preflight (Pattern 12).
7. `dlq_events{run window} = 0` and `sentinel_detection_late_events_total Δ = 0`, or exactly equal to deliberately injected late/poison counts.
8. Lag returns to baseline after the run, and recovery is measured (Pattern 11).

If any equation fails, `report.py` exits nonzero, writes **no** `.md`, and writes `<date>-<scenario>.FAILED.json` with the diff. Every number in the `.md` is computed from the saved raw JSON, so the report is reproducible offline.

### Pattern 10: Latency definitions (be explicit in the report)
- **Ingest:** CO-corrected `done − intended` and service time, p50/p95/p99, per batch, with batch size stated.
- **Detection:** `sentinel_detection_delay_seconds` (detector wall clock at publish ack minus trigger occurred_at) from the Prometheus histogram, quantiles over the run window. Bucket resolution must be stated.
- **Event-time-to-UI notification:** the harness SSE subscriber records arrival time per alert_id. The trigger event's intended/occurred time is known from the attack plan. This is exact per alert. PG persistence precedes SSE delivery (Phase 4 gap-safe delivery), so this is an upper bound on persistence latency. There is no wall-clock persistence column (A9). Do **not** add one in Phase 5 unless the planner accepts a migration-009 dependency (Open Question 3).
- **API p95:** a separate fixed-rate open-loop query phase against `GET /api/v1/events` and `/api/v1/alerts` with defined filters, dataset (the run tenant) and concurrency.
- **Sample size:** with N alerts, report p99 only if N ≥ 100, otherwise "n/a (n=N)". Design ≥ 100 injected attacks if p99 detection latency is wanted.
- **SSE practicalities:** stream lifetime defaults to 10 min (`defaultStreamLifetime`) and dev JWT is ≤ 15 min, so the subscriber must reconnect with `Last-Event-ID` and re-mint tokens via `dev-jwt` (secret only through child env or a private file). Per-client queue is 32 (`defaultStreamQueue`), so the subscriber must read continuously.

### Pattern 11: Detector restart under load (BENCH-04)
- **Procedure:** at steady state (lag stable for ≥ 60 s), `docker kill -s KILL <one detector replica>` (crash, not graceful). Docker `restart: unless-stopped` brings it back.
- **Recovery time definition** (state it in the ADR/report): `t_recovered − t_kill`, where `t_recovered` is the first time `max(sentinel_kafka_consumer_lag{group=detector})` stays ≤ the pre-kill p95 baseline for 30 consecutive seconds. Use an explicitly *bounded* timeout (e.g. 10 min). Exceeding it fails the run.
- **Expected components (formula, do not pre-claim numbers):**
  - Group failover waits up to `session.timeout` = **45 s** for the dead member unless the container rejoins sooner. Static membership is not configured.
  - Replay per moved partition = events from the committed pin to the end = `rate_p × (H_pin + D)`, where `H_pin` ≤ 330 s (AUTH window + lateness) and D = downtime.
  - Catch-up time ≈ `Σ_owned rate_p × (H_pin + D) / (C_proc − Σ_owned rate_p)`. `C_proc` (per-process max processing rate) is unknown and must be **measured**. Derive it from the lag decrease slope during catch-up and report it.
- **No-loss proof:** equations 4–6 after the kill. Every expected alert is observed exactly once. Also `Δ alerts_total` (publications) may exceed unique alerts, so report the duplicate publications.

### Pattern 12: Bounded entity pools plus capacity preflight (the critical bench design input)
The harness must compute, before sending, predicted per-process detector state after N−1 failover and refuse configurations above about 70–80% of any cap:
- `ledger ≈ (R_total / (N−1)) × 330 s` (with benign auth.failure present) must be ≤ 100,000 × 0.8 → **R_total ≤ about 240 × (N−1) ev/s** (e.g. N=3 → about 480 ev/s → 1M events in about 35 min; N=4 with ≥ 8 partitions → about 720 ev/s).
- `windows ≈ Σ_rules distinct keys active within (window + 30 s) per process` ≤ 8,000. Use bounded pools, e.g. S source IPs, D destinations per source, U principals. Keys = AUTH S×U', SCAN S×D', FLOOD S, DENY S, ×2 for window overlap.
- `benign threshold safety`: for each rule, the max per-key count per window in the *generated* stream (computed exactly by the generator, cheap) must stay below the threshold (AUTH < 10/300 s, SCAN < 20 ports/60 s, FLOOD < 300/60 s, DENY < 30/120 s). Otherwise reseed or adjust pools. This makes `expected benign hits = 0` a checked fact, not a hope.
- Partition skew: keys are `tenant|source_ip` with the franz-go default murmur2 sticky-key partitioner. Measure per-partition offset deltas in the warm-up and use the *max* partition share in the preflight.
- Attack entities come from address ranges disjoint from the benign pools (e.g. a dedicated IPv6 /64 under 2001:db8::/32), and each attack stays < 4,096 events per window.
- Export `sentinel_detector_state_*` gauges (Pattern 1), so the report shows real peak headroom instead of the estimate.

### Anti-Patterns to Avoid
- **promauto / default global registry:** violates `gochecknoglobals` and leaks between tests.
- **Committed-offset lag for the detector:** shows permanent false lag of up to 330 s of events.
- **Closed-loop "send next after response" benchmarking:** hides latency under load (coordinated omission).
- **Raising detector constants or adding eviction to "pass" the bench:** Phase 3 contract (ADR 0007: "No eviction of unexpired state"). Report the bottleneck instead.
- **Running bench or chaos against the `sentinel` or `sentinel-p4-demo-20261024` projects,** or resetting groups/offsets to make readiness green.
- **Using `FINAL` or `OPTIMIZE` in correctness paths:** the project already dedupes at read with `uniqExact`/`GROUP BY`, and `OPTIMIZE` is forbidden by STATE.
- **Per-record spans via kotel hooks at bench rates:** 500 spans per request and heavy export.
- **Hardcoded or "0" fallback values on the System page when Prometheus is down.**

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Prometheus text exposition, histograms | the current `fmt.Fprintf` counter | client_golang Registry + promhttp | escaping, histogram buckets, content negotiation, Go/process collectors |
| traceparent parsing/formatting | string splitting | `propagation.TraceContext{}` + header carrier | W3C edge cases, tracestate |
| Span batching/export | custom queue | `sdktrace.NewBatchSpanProcessor` + otlptracehttp | bounded queue, retries, shutdown flush |
| Querying Prometheus from Go | raw JSON decoding | `client_golang/api/prometheus/v1` | typed results, warnings |
| Rule validation | eyeballing YAML | `promtool check rules` / `promtool test rules` | catches PromQL errors in CI |
| Dashboard deployment | manual UI clicks | Grafana file provisioning | spec FR-15 "as code" |
| Percentiles | streaming approximation | exact sort over raw samples (≤ ~10k) | exact, auditable, no deps |
| Container resource sampling | /proc parsing | `docker stats --no-stream --format json` + `process_resident_memory_bytes` | per-container and per-process |
| End offsets / log start offsets | raw kmsg requests | `kadm.ListEndOffsets` / `ListStartOffsets` | already a dependency |

**Key insight:** this phase's credibility comes from measurements being *reproducible and auditable*. Every custom shortcut (approximate percentiles, committed lag, closed-loop sends, eyeballed counts) is exactly what a reviewer will challenge.

## Runtime State Inventory

Phase 5 is not a rename, but its runs create and depend on runtime state. Inventory:

| Category | Items Found | Action Required |
|----------|-------------|-----------------|
| Stored data | Old `sentinel` volumes, NEW `sentinel-p4-demo-20261024_{kafka,clickhouse,...}-data` volumes (NEW detector pin below retained start: "checkpoint history lost") | **Do not touch.** Bench uses its own `sentinel-bench-<run>_*` volumes |
| Live service config | Kafka consumer groups `sentinel-detector`, `sentinel-event-writer`, `sentinel-alert-writer` with committed metadata on existing stacks | Never reset. Bench groups live in a separate broker, so names may match safely |
| OS-registered state | Docker containers of both Sentinel projects currently `Restarting (1)` (infra containers absent). `devrag-stack` (unrelated) running | Do not start/stop them. Record the `devrag-stack` load as bench noise or ask the user to stop it (Open Question 5) |
| Secrets/env vars | Root `.env` (never read), `.local/auth/jwt.key`, `.local/keys/*.key` | Bench generates its own private env/keys under `.local/bench/<run>/` (0700/0600) |
| Build artifacts | Shared images built per project. STATE warns about stale 006/07 images | Bench builds a fresh image tagged by git SHA. Never reuse other projects' images or migrators |

## Common Pitfalls

### Pitfall 1: Detector capacity exhaustion masquerading as a "bench failure"
**What goes wrong:** At more than about 300 ev/s per detector process (or with unbounded IPs, about 100 ev/s), `Bounds.reserve` returns `errCapacity`, the detector marks not-ready and terminates. Compose restarts it, it replays and dies again: a crash loop, all lag, no alerts.
**Why:** process-wide constants `MaxLedgerEntries = 100_000`, `MaxWindows = 10_000` (`detection/reducer.go:16-19`). The ledger spans pin→head, which is up to 330 s of events. `StateLimits` can only *lower* these.
**How to avoid:** Pattern 12 preflight, bounded pools, size for N−1 survivors, and capacity gauges with an alert.
**Warning signs:** `TestDetectorCapacity`-style log "detector state capacity exceeded", restart counter rising, readyz not_ready.

### Pitfall 2: Generator time model incompatible with 1M events
**What goes wrong:** `generate_events(count=1_000_000)` places events up to 11.6 days in the past, ingest returns 400 (past skew 168 h), and 1 s spacing makes event time diverge from wall time.
**How to avoid:** a new live mode where `occurred_at` = intended send time. Keep the existing `send` mode untouched (its tests and seed contracts depend on it).

### Pitfall 3: Rate limiter caps accepted throughput at 2,000 ev/s per key
**What goes wrong:** the bench's overload or high-rate phases get 429 at the token-bucket refill, not at true capacity.
**How to avoid:** set the bench rate-limit config explicitly and record it in the report. For the overload scenario (REL-04), deliberately exceed it to prove 429 + `Retry-After`. Use the limiter to keep accepted density under detector capacity, which is the system's actual backpressure design.

### Pitfall 4: Late events silently remove expected alerts
**What goes wrong:** a batch retried after a > 30 s delay arrives below the partition watermark. The detector drops it as late (counter only), and the attack's threshold is never crossed, so expected ≠ observed.
**How to avoid:** cap the per-batch retry budget well below 30 s, send attack events without retry delay where possible, and include the `late_events` delta in reconciliation.

### Pitfall 5: Docker stop kills graceful shutdown
**What goes wrong:** the default `stop_grace_period` of 10 s sends SIGKILL before the 30 s lifecycle and 20 s writer stop finish, so a "graceful shutdown" test is actually a crash test.
**How to avoid:** set `stop_grace_period: 40s` for workers in compose overlays and assert exit code 0 plus "shutdown complete" in logs for the graceful variant.

### Pitfall 6: Idle bench stack loses checkpoint history
**What goes wrong:** windows stay open while no new event time arrives ("idle partitions retain state indefinitely"). After events retention of 24 h, the pin falls below log start and the detector fails closed. This is exactly the NEW stack hold.
**How to avoid:** tear the bench stack down at the end of the run, export the pin-retention-margin gauge and alert, and document the hazard in operations.md.

### Pitfall 7: Fixed host ports prevent scaling; pin checker blind to overlays
**How to avoid:** use `ports: !reset []` plus ranges, and extend `check-pins.sh` to all compose files and to range syntax.

### Pitfall 8: Stale lag series after rebalance
**How to avoid:** delete label values on revoke/lost. In tests, assert that `sum(lag)` across replicas equals the true backlog after a rebalance.

### Pitfall 9: Metrics counted on replay
**What goes wrong:** `alerts_total` and the delay histogram increment again when a crashed detector republishes.
**How to avoid:** document the metric as "publications". Unique alert counts come from PG/CH. Optionally observe delay only for first-time allow decisions (not replayed decisions).

### Pitfall 10: Echo route label cardinality
**What goes wrong:** labelling HTTP metrics with the raw URL path (`/api/v1/events/<id>`) explodes cardinality.
**How to avoid:** use `c.Path()` (the route template) only, or no path label at all. ADR 0004's allowlist does not include `route`, so amend it if you add one.

### Pitfall 11: Disk exhaustion mid-run
**What goes wrong:** 99% full root disk. Kafka (snappy-compressed events, about 600 B JSON each), CH parts, Prometheus TSDB, image layers and logs can fill the disk and corrupt the run or the other stacks.
**How to avoid:** a preflight that requires free space (e.g. ≥ 15 GB) and aborts otherwise. Bound Prometheus retention (`--storage.tsdb.retention.size`) and Jaeger `max_traces`. Bound container logs (`logging: driver json-file, max-size`).

### Pitfall 12: Shared files with unfinished Phase 4
**What goes wrong:** editing `Overview.tsx`, `App.tsx`, `openapi.yaml` or `cmd/query-api/main.go` while 04-14..04-16 acceptance is pending can invalidate those reviews.
**How to avoid:** schedule UI-05 and the System API after Phase 4 closes, or declare explicit ownership in the plan.

## Code Examples

### Registry injection plus ops mount
```go
// Source: client_golang docs (prometheus.NewRegistry, collectors, promhttp.HandlerFor)
reg := prometheus.NewRegistry()
reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
m := telemetry.NewConsumerMetrics(reg) // constructs GaugeVec{group,topic,partition}, etc.
ops, err := opshttp.Start(cfg.Ops, draining, checks, opshttp.WithMetricsHandler(promhttp.HandlerFor(reg, promhttp.HandlerOpts{})))
```

### Kafka header carrier (≈20 lines, replaces kotel dependency)
```go
// Source: go.opentelemetry.io/otel/propagation TextMapCarrier interface
type headerCarrier struct{ r *kgo.Record }
func (c headerCarrier) Get(k string) string { for _, h := range c.r.Headers { if h.Key == k { return string(h.Value) } }; return "" }
func (c headerCarrier) Set(k, v string) { for i := range c.r.Headers { if c.r.Headers[i].Key == k { c.r.Headers[i].Value = []byte(v); return } }; c.r.Headers = append(c.r.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)}) }
func (c headerCarrier) Keys() []string { ks := make([]string, 0, len(c.r.Headers)); for _, h := range c.r.Headers { ks = append(ks, h.Key) }; return ks }
// inject: propagation.TraceContext{}.Inject(ctx, headerCarrier{r: record})
```

### Sampler config
```go
// Source: go.opentelemetry.io/otel/sdk/trace
tp := sdktrace.NewTracerProvider(
  sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
  sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
lc.Add("tracing", tp.Shutdown) // register so it closes after workers (LIFO)
```

### Full-jitter shared retry
```go
// Mirrors eventwriter.jitterDelay; one package for writer, alert-writer, detector poison, stream listener
func (p Policy) Delay(attempt int) time.Duration { // attempt>=1
  ceil := min(p.Min<<(attempt-1), p.Max) // guard overflow: cap shift
  return cryptoUniform(0, ceil)
}
```

### Open-loop scheduling core (Python)
```python
# Source: open-model principle (k6 arrival-rate docs); stdlib asyncio + httpx.AsyncClient
async def run(schedule, clients, sem, log):
    t0 = time.monotonic_ns()
    tasks = []
    for k, batch in enumerate(schedule):
        due = t0 + batch.offset_ns
        await asyncio.sleep(max(0, due - time.monotonic_ns()) / 1e9)
        tasks.append(asyncio.create_task(send(batch, due, clients[k % len(clients)], sem, log)))
    await asyncio.gather(*tasks)
# send(): intended=due; acquire sem (wait counts toward latency); post; record done-intended and done-sent
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `otelecho` contrib middleware | deprecated → `labstack/echo-otel/v4` (pre-1.0) | 2026 [VERIFIED: module go.mod deprecation notices] | Use `otelhttp` around Echo instead |
| Jaeger v1 all-in-one (`jaegertracing/all-in-one`) | Jaeger v2 (`jaegertracing/jaeger`, OTel-collector based, OTLP native) | v2.x, current 2.22.0 | Single image, OTLP 4317/4318, UI 16686 |
| Prometheus 2.x | Prometheus 3.x (v3.15.1) | 3.0 (2024) | UTF-8 metric names allowed, but keep classic `snake_case` names |
| ClickHouse FINAL "always slow" | parallel FINAL. Newer versions add automatic partition-local FINAL decisions [LOW: secondary sources] | 22.x–26.x | Irrelevant to correctness here (`uniqExact` is used). If the report includes a dedupe-cost comparison (FINAL vs `LIMIT 1 BY` vs `uniqExact`), **measure** on the run dataset via `system.query_log` (`query_duration_ms`, `read_rows`, `memory_usage`) with read-only queries. Never claim |

**Deprecated/outdated:** `detection_late_events_total` unprefixed name (rename with all 5 consumers). The `local` → ConsoleWriter coupling.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | All Go modules and images are legitimate (slopcheck unavailable) | Package Audit | Supply-chain risk. Mitigated by the human-verify checkpoint |
| A2 | Unsampled (non-recording) spans still carry a valid TraceID usable for `trace_id` log fields | Pattern 3/4 | Logs for unsampled requests lack trace_id. Verify with a unit test, or generate the ID in middleware |
| A3 | Compose DNS for a scaled service returns all replica IPs to Prometheus `dns_sd_configs` type A | Pattern 7 | Some replicas unscraped. Fallback: static targets per replica container name |
| A4 | `client_golang/api/prometheus/v1` lives in the client_golang v1.25.0 module (no separate module) | Pattern 5 | Extra dependency |
| A5 | Per-process detector ceiling ≈ 100,000/330 ≈ 300 ev/s (derived from source, not measured) | Pattern 12, Pitfall 1 | Actual may be lower (CPU) or slightly different (sawtooth). The bench measures it |
| A6 | Jaeger 2.22 in-memory store supports a bounded `max_traces` setting | Stack | Unbounded memory. Verify in its config docs at execution time |
| A7 | Docker Hub digests read via API are the multi-arch index digests | Stack | Pin mismatch. Re-verify with `docker buildx imagetools inspect` |
| A8 | The franz-go default keyed partitioner is murmur2-compatible sticky-key | Pattern 12 | Skew estimate. Measured in warm-up regardless |

## Open Questions

1. **Disk space (blocking for bench and image builds).**
   - Known: `/` has 2.6 GB free (99% used). STATE forbids agent-initiated prune.
   - Recommendation: a plan checkpoint requiring the user to free ≥ 15 GB before any bench/chaos task. The bench preflight enforces it.
2. **Who may see the System page?** Global pipeline data leaks cross-tenant volume. Recommendation: admin-only, with an Overview lag panel shown to admins only. Needs user confirmation because it changes UI-01 behaviour for analysts.
3. **Persistence-latency measurement.** Without a wall-clock column, persistence latency is bounded by SSE notification. Recommendation: accept the SSE upper bound plus the detector delay histogram. Adding `persisted_at` (migration 009) is possible but touches Phase 4-owned delivery triggers.
4. **Benign event mix.** Including `auth.failure` sets the 330 s horizon, which roughly triples the ceiling penalty. Recommendation: keep the realistic mix, report the ceiling honestly, and optionally add a second scenario without AUTH traffic to show the horizon effect (clearly labelled).
5. **Unrelated host load (`devrag-stack`, about 2 GB RAM).** Ask the user to stop it for the run, or record it as a limitation with measured host load.
6. **Phase 4 sequencing.** UI-05 and `GET /api/v1/system` touch Phase 4 surfaces. Recommendation: order these plans last, after Phase 4 closes.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker Engine | all stacks, testcontainers | ✓ | 29.9.2 | — |
| Docker Compose | bench/obs overlays (`!reset`) | ✓ | v5.6.1 | — |
| Go toolchain | backend | ✓ | host 1.25.5, auto-switches to cached go1.26.9 (`GOTOOLCHAIN=auto`) | — |
| uv / Python 3.13 | generator, bench, report | ✓ uv 0.9.18 (host python3 is 3.10; uv provides 3.13 per `.python-version`) | — | — |
| Node | web | ✓ | 22.23.4 | — |
| vegeta | not required | ✓ (unpinned, unknown version) | — | not used |
| Free disk | bench data, images | ✗ **2.6 GB free (99%)** | — | **none: user must free space** |
| Free RAM | multi-replica stack | ~4 GB available of 15 GB (devrag-stack running) | — | reduce replicas (detector 3 → min), record limitation |
| CPU | bench | 16 cores | — | — |
| Old/NEW Sentinel stacks | NOT used by Phase 5 | infra missing, apps restart-looping | — | bench uses isolated project |

**Missing with no fallback:** free disk space. **Missing with fallback:** RAM headroom (smaller replica counts, documented).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` + testify v1.12.1 + testcontainers-go v0.44.0. Vitest 5.0.3 (web). pytest 9.1.1 (generator/bench). unittest (scripts). promtool (pinned image) |
| Config file | `backend/.golangci.yml`, `web/vite.config.*`, `generator/pyproject.toml` |
| Quick run command | `cd backend && go test -race ./internal/telemetry/... ./internal/retry/... ./internal/kafkax/... ./internal/opshttp/...` |
| Full suite command | `make test && make lint`, then `make test-integ` (32 m), then new `make test-chaos` (own finite ceiling) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| OBS-02 | all spec metrics registered; label names ⊆ ADR 0004 allowlist; lag = end − processed_next; series deleted on revoke | unit | `go test ./internal/telemetry/... ./internal/kafkax/ -run 'TestMetric|TestLag'` | ❌ Wave 0 |
| OBS-02 | `/metrics` served on every service ops port | integration | `go test -tags=integration ./test/ -run TestMetricsEndpoints` | ❌ |
| OBS-03 | JSON line has service/timestamp/level/request_id/trace_id; no API key/JWT/body | unit | `go test ./internal/logger/ ./internal/middleware/ -run TestLog` | partial (logger_test exists) |
| OBS-04 | traceparent injected by ingest, extracted by detector and writers; sampling 0 → no export; ratio honored | unit (in-memory exporter `tracetest.NewInMemoryExporter`) + integration | `go test ./internal/telemetry/... -run TestTrace` | ❌ |
| OBS-05 | rules valid, alert unit tests fire; dashboard exprs only known metrics | CI static | `docker run --rm --entrypoint promtool prom/prometheus@sha256:… test rules /w/alerts_test.yml`; `python3 -m unittest scripts/test_observability_config.py` | ❌ |
| REL-01 | jitter within [0, ceil]; bounded attempts; terminal error surfaces to lifecycle | unit | `go test ./internal/retry/ ./internal/alertwriter/ ./internal/detector/ -run TestRetry` | ❌ |
| REL-02 | SIGTERM mid-batch → exit 0, commits ≤ completed offsets | integration | `go test -tags=integration ./test/ -run TestGracefulShutdown` | partial (lifecycle tests) |
| REL-03 | broker stop/start, CH outage recovery, slow consumer lag rises/falls, DLQ, SSE reconnect, rate-limit overload | integration/chaos | `make test-chaos` | partial (A6 table) |
| REL-04 | overload: only 202/429/503, buffered records ≤ cap, RSS ≤ bound | integration + bench scenario | `go test -tags=integration ./test/ -run TestOverloadBounded`; `make bench SCENARIO=overload` | ❌ |
| BENCH-01..04 | schedule open-loop; reconciliation refuses on mismatch; report fields present | unit (pytest with synthetic raw JSON incl. mismatch fixtures) + manual-heavy run | `cd generator && uv run --locked pytest tests/test_bench_*.py`; `uv run python ../bench/report.py --selftest` | ❌ |
| BENCH-02/04 | actual ≥ 1M run with kill | manual/long (≈ 40 min) | `make bench SCENARIO=sustained-restart` | ❌ (requires disk) |
| UI-05 | System page states: loading/unavailable/forbidden/data; no fake zeros; admin gating | component (Vitest) + Go handler tests | `cd web && npm test -- System`; `go test ./internal/query/ -run TestSystem` | ❌ |

### Sampling Rate
- **Per task commit:** the quick run command plus the affected package tests.
- **Per wave merge:** `make test && make lint`.
- **Phase gate:** full suite, `make test-chaos` and one real `make bench` producing a reconciled report (not simulated) before `/gsd:verify-work`.

### Wave 0 Gaps
- [ ] `backend/internal/telemetry/*_test.go`: registry/label allowlist guard
- [ ] `backend/internal/retry/retry_test.go`
- [ ] `backend/test/chaos_*_test.go` + `make test-chaos` + nonempty guard
- [ ] Extend `internal/testing/containers.go` with stop/start (allowlisted) for a dedicated broker
- [ ] `generator/tests/test_bench_schedule.py`, `test_bench_pools.py`, `bench/tests/test_report.py` (mismatch → refuse)
- [ ] `scripts/test_observability_config.py` + promtool rule tests
- [ ] `web/src/pages/System.test.tsx`

## Security Domain

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes (System API, bench JWT minting) | existing HS256 dev JWT. Bench tokens via `dev-jwt` with secrets only in child env or a private file |
| V3 Session Management | limited | existing 15-min token, SSE without URL tokens |
| V4 Access Control | yes | `/api/v1/system` role check server-side (admin recommended). `/metrics`, Prometheus, Grafana and Jaeger bound to 127.0.0.1 / internal network only |
| V5 Input Validation | yes | System endpoint takes no user PromQL (fixed allowlist). Bench config validated (bounded ints like the generator's `_bounded_int`) |
| V7 Logging | yes | no secrets/bodies. Scrub. JSON. Test-enforced |
| V6 Cryptography | minimal | `crypto/rand` jitter is fine. No new crypto |

### Known Threat Patterns
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| PromQL injection via System API | Tampering/Info disclosure | fixed server-side queries, no parameters |
| Cross-tenant info via global metrics | Info disclosure | no tenant labels. Admin-only System page |
| Metrics/trace endpoints exposed | Info disclosure | loopback binds, internal Compose network, Grafana auth with private password |
| Secrets in traces/logs/report | Info disclosure | no credential attributes. Report writes no keys/tokens. gitleaks in CI |
| Cardinality DoS on Prometheus | DoS | label allowlist test, route templates only |
| Bench tooling touching protected stacks | Tampering | project-name denylist plus port preflight in `make bench` |

## Sources

### Primary (HIGH confidence)
- Repository source read in this session: `backend/internal/{opshttp,logger,middleware,app,kafkax,eventwriter,alertwriter,detector,detection,ratelimit,config}`, `backend/test/*`, `deployments/compose/docker-compose.yml`, `Makefile`, `generator/src/*`, `docs/decisions/0004,0007`, `docs/operations.md`, `docs/event-storage.md`, `.planning/STATE.md`, `04-07-STACK-HOLD.md`, `spec.md` §11–§18
- proxy.golang.org `@latest` and `.mod` for client_golang v1.25.0, otel v1.47.0, otlptracehttp v1.47.0, otelhttp v0.72.0, kotel v1.7.1, kprom v1.5.0, otelecho v0.71.0 (deprecated), echo-opentelemetry v0.0.4 (deprecated), franz-go v1.22.2
- Local module cache franz-go v1.22.1 (`BufferedProduceRecords`, `Fetches.EachPartition`, `FetchPartition.HighWatermark`, default compression snappy) and kadm v1.19.0 (`Lag`, `CalculateGroupLag`)
- Docker Hub API tag and digest lookups for prom/prometheus v3.15.1, grafana/grafana 13.2.5, jaegertracing/jaeger 2.22.0
- [Docker Compose merge (`!reset`, `!override`)](https://docs.docker.com/reference/compose-file/merge/)
- [kotel package docs](https://pkg.go.dev/github.com/twmb/franz-go/plugin/kotel)

### Secondary (MEDIUM confidence)
- [k6 open vs closed models (coordinated omission)](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/open-vs-closed/)
- [k6 community: coordinated omission](https://community.grafana.com/t/is-k6-safe-from-the-coordinated-omission-problem/98853)

### Tertiary (LOW confidence)
- [ClickHouse FINAL behavior notes](https://oneuptime.com/blog/post/2026-03-31-clickhouse-how-to-use-final-keyword-efficiently-in-clickhouse/view), [pulse.support FINAL trade-offs](https://pulse.support/kb/clickhouse-final-clause-performance), [ClickHouse issue 49685](https://github.com/ClickHouse/ClickHouse/issues/49685), [ClickHouse ReplacingMergeTree guide](https://clickhouse.com/docs/zh/guides/replacing-merge-tree). Not needed for correctness; measure if reported

## Metadata

**Confidence breakdown:**
- Codebase audit and capacity math: HIGH. Read directly from source; the derived ceiling is flagged as an estimate (A5).
- Standard stack: MEDIUM-HIGH. Versions verified on official registries; Context7 was not used; slopcheck unavailable.
- Architecture patterns: MEDIUM-HIGH. Standard practice, consistent with ADRs 0004/0007.
- Pitfalls: HIGH for codebase-derived ones (1–9, 11, 12), MEDIUM for 10.
- Benchmark method: MEDIUM-HIGH. The open-model principle is cited; the reconciliation design is original to this project.

**Research date:** 2026-10-30
**Valid until:** 2026-11-29 (image digests and OTel minor versions move monthly; re-verify pins at execution)
