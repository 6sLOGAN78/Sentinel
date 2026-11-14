# Phase 5 Plan 01: Ingest metrics slice Summary (in progress)

## Package legitimacy approval

Recorded before any go.mod change. Question put to the user: "Plan 05-01 asks you to approve the new Phase 5 third-party packages before anything is installed. Approve them?"

User reply (verbatim): "Approve all (Recommended)"

Provenance (read-only, 2026-11-14):

| Module@version | Origin |
|---|---|
| github.com/prometheus/client_golang@v1.25.0 | https://github.com/prometheus/client_golang (0d88875bc7) |
| go.opentelemetry.io/otel@v1.47.0 | https://github.com/open-telemetry/opentelemetry-go (66cfc9520e) |
| go.opentelemetry.io/otel/sdk@v1.47.0 | same repo, 66cfc9520e |
| go.opentelemetry.io/otel/trace@v1.47.0 | same repo, 66cfc9520e |
| go.opentelemetry.io/otel/metric@v1.47.0 | same repo, 66cfc9520e |
| go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.47.0 | same repo, 66cfc9520e |
| go.opentelemetry.io/otel/exporters/otlp/otlptrace@v1.47.0 | lookup timed out three times on proxy.golang.org; approved on the basis of same repository and v1.47.0 tag as its siblings. Origin to be re-verified when `go get` resolves it (see below) |
| go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.72.0 | https://github.com/open-telemetry/opentelemetry-go-contrib (0c76f6137a) |

Images (index digests; not pulled in this plan):

- prom/prometheus:v3.15.1@sha256:fa3302279710be2f21a8301ec17a8b4f2301fd829bd113839b3b6b02b6f191a2
- grafana/grafana:13.2.5@sha256:9f7aa1e1e3246aade5792d35a738049f7f32f89cd51d1956cf426a4e86268a84
- jaegertracing/jaeger:2.22.0@sha256:836b967b5abcf1f7758066aa6b348619267a2f1459f72d472840570f9bd88a17

These match the RESEARCH table.
