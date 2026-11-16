package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

const testEndpoint = "http://127.0.0.1:4318"

func newTestTracing(
	t *testing.T, cfg config.TracingConfig,
) (*telemetry.Tracing, *tracetest.InMemoryExporter) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tr, err := telemetry.NewTracing(t.Context(), cfg, "ingest", telemetry.WithExporter(exporter))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Shutdown(context.Background())) })

	return tr, exporter
}

func startAndEnd(t *testing.T, ctx context.Context, tr *telemetry.Tracing) trace.SpanContext {
	t.Helper()

	_, span := tr.Provider().Tracer("test").Start(ctx, "op")
	span.End()
	require.NoError(t, tr.ForceFlush(t.Context()))

	return span.SpanContext()
}

func TestTracingRatioOneExportsSpan(t *testing.T) {
	t.Parallel()

	tr, exporter := newTestTracing(t, config.TracingConfig{Enabled: true, SampleRatio: 1, OTLPEndpoint: testEndpoint})
	sc := startAndEnd(t, t.Context(), tr)

	require.True(t, sc.IsSampled())
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "op", spans[0].Name)
	require.Equal(t, sc.TraceID(), spans[0].SpanContext.TraceID())
	require.Contains(t, spans[0].Resource.Attributes(), attribute.String("service.name", "ingest"))
}

func TestTracingRatioZeroExportsNothingButHasTraceID(t *testing.T) {
	t.Parallel()

	tr, exporter := newTestTracing(t, config.TracingConfig{Enabled: true, SampleRatio: 0, OTLPEndpoint: testEndpoint})
	sc := startAndEnd(t, t.Context(), tr)

	require.True(t, sc.TraceID().IsValid(), "unsampled spans still carry a trace ID for logs")
	require.True(t, sc.IsValid())
	require.False(t, sc.IsSampled())
	require.Empty(t, exporter.GetSpans())
}

func TestTracingDisabledExportsNothingButHasTraceID(t *testing.T) {
	t.Parallel()

	// The exporter is offered but must never receive spans while disabled.
	tr, exporter := newTestTracing(t, config.TracingConfig{Enabled: false, SampleRatio: 1})
	sc := startAndEnd(t, t.Context(), tr)

	require.True(t, sc.TraceID().IsValid())
	require.False(t, sc.IsSampled())
	require.Empty(t, exporter.GetSpans())
}

func TestTracingParentBasedFollowsSampledRemoteParent(t *testing.T) {
	t.Parallel()

	tr, exporter := newTestTracing(t, config.TracingConfig{Enabled: true, SampleRatio: 0, OTLPEndpoint: testEndpoint})
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	sc := startAndEnd(t, trace.ContextWithRemoteSpanContext(t.Context(), parent), tr)

	require.True(t, sc.IsSampled())
	require.Equal(t, parent.TraceID(), sc.TraceID())
	require.Len(t, exporter.GetSpans(), 1)
}

func TestTracingShutdownIsIdempotent(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tr, err := telemetry.NewTracing(t.Context(),
		config.TracingConfig{Enabled: true, SampleRatio: 1, OTLPEndpoint: testEndpoint}, "ingest",
		telemetry.WithExporter(exporter))
	require.NoError(t, err)

	_, span := tr.Provider().Tracer("test").Start(t.Context(), "final")
	span.End()
	require.NoError(t, tr.Shutdown(t.Context()))
	require.Len(t, exporter.GetSpans(), 1, "shutdown flushes queued spans")
	require.NoError(t, tr.Shutdown(t.Context()))
}

func TestTracingBuildsOTLPExporterWithoutDialing(t *testing.T) {
	t.Parallel()

	tr, err := telemetry.NewTracing(t.Context(),
		config.TracingConfig{Enabled: true, SampleRatio: 0, OTLPEndpoint: testEndpoint + "/"}, "ingest")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	require.NoError(t, tr.Shutdown(ctx))
}

func TestTracingRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	for _, cfg := range []config.TracingConfig{
		{SampleRatio: 2},
		{Enabled: true, SampleRatio: 1},
		{Enabled: true, SampleRatio: 1, OTLPEndpoint: "http://user:pw@jaeger:4318"},
	} {
		_, err := telemetry.NewTracing(t.Context(), cfg, "ingest")
		require.Error(t, err, "%+v", cfg)
	}
}

func TestTracingPropagatorIsW3CTraceContext(t *testing.T) {
	t.Parallel()

	tr, _ := newTestTracing(t, config.TracingConfig{Enabled: true, SampleRatio: 1, OTLPEndpoint: testEndpoint})
	require.Contains(t, tr.Propagator().Fields(), "traceparent")

	ctx, span := tr.Provider().Tracer("test").Start(t.Context(), "op")
	defer span.End()

	carrier := propagation.MapCarrier{}
	tr.Propagator().Inject(ctx, carrier)
	require.Regexp(t, `^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`, carrier.Get("traceparent"))
}

// Client-identifying attributes (IP, user agent, raw path, Host) must never
// leave the process, whatever instrumentation sets them.
func TestTracingExportDropsAttributesOutsideAllowlist(t *testing.T) {
	t.Parallel()

	tr, exporter := newTestTracing(t, config.TracingConfig{Enabled: true, SampleRatio: 1, OTLPEndpoint: testEndpoint})
	_, span := tr.Provider().Tracer("test").Start(t.Context(), "op", trace.WithAttributes(
		attribute.String("client.address", "198.51.100.7"),
		attribute.String("network.peer.address", "198.51.100.7"),
		attribute.Int("network.peer.port", 51000),
		attribute.String("user_agent.original", "curl/8"),
		attribute.String("url.path", "/api/v1/alerts/123"),
		attribute.String("server.address", "attacker.example"),
		attribute.String("tenant_id", "demo"),
		attribute.String("http.request.method", "POST"),
		attribute.String("http.route", "/api/v1/events:batch"),
		attribute.String("messaging.system", "kafka"),
	))
	span.SetAttributes(attribute.Int("http.response.status_code", 202), attribute.String("api_key", "sk_test"))
	span.End()
	require.NoError(t, tr.ForceFlush(t.Context()))

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	keys := map[attribute.Key]bool{}
	for _, kv := range spans[0].Attributes {
		keys[kv.Key] = true
	}
	require.Equal(t, map[attribute.Key]bool{
		"http.request.method": true, "http.route": true, "messaging.system": true, "http.response.status_code": true,
	}, keys)
}
