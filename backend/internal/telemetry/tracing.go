package telemetry

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const (
	// spanQueueSize bounds spans waiting for export; the processor drops new
	// spans when it is full rather than buffering without limit.
	spanQueueSize = 2048
	// spanExportBatchSize bounds one OTLP export request.
	spanExportBatchSize = 512
	// otlpTracesPath is the OTLP/HTTP traces signal path appended to the origin.
	otlpTracesPath = "/v1/traces"
)

// Tracing owns one process TracerProvider and the W3C propagator. Both are
// passed explicitly to instrumented components; no OTel global is set.
type Tracing struct {
	provider     *sdktrace.TracerProvider
	propagator   propagation.TextMapPropagator
	shutdownOnce sync.Once
	shutdownErr  error
}

// TracingOption customizes NewTracing.
type TracingOption func(*tracingOptions)

type tracingOptions struct {
	exporter sdktrace.SpanExporter
}

// WithExporter replaces the OTLP/HTTP exporter used when tracing is enabled
// (a test seam for an in-memory exporter). It is ignored while disabled.
func WithExporter(exporter sdktrace.SpanExporter) TracingOption {
	return func(o *tracingOptions) { o.exporter = exporter }
}

// NewTracing builds the provider for service. Enabled: a ParentBased ratio
// sampler feeding a bounded batch processor and the OTLP/HTTP exporter.
// Disabled: no exporter and no processor, but spans still get valid trace IDs
// so request logs can carry trace_id.
func NewTracing(
	ctx context.Context, cfg config.TracingConfig, service string, opts ...TracingOption,
) (*Tracing, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("tracing config: %w", err)
	}

	options := tracingOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	providerOptions := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service))),
	}

	if cfg.Enabled {
		exporter := options.exporter
		if exporter == nil {
			otlp, err := otlptracehttp.New(ctx,
				otlptracehttp.WithEndpointURL(strings.TrimSuffix(cfg.OTLPEndpoint, "/")+otlpTracesPath))
			if err != nil {
				return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
			}
			exporter = otlp
		}

		providerOptions = append(providerOptions,
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
			sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(allowlistExporter{next: exporter},
				sdktrace.WithMaxQueueSize(spanQueueSize),
				sdktrace.WithMaxExportBatchSize(spanExportBatchSize))),
		)
	} else {
		providerOptions = append(providerOptions,
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.NeverSample())))
	}

	return &Tracing{
		provider:   sdktrace.NewTracerProvider(providerOptions...),
		propagator: propagation.TraceContext{},
	}, nil
}

// Provider returns the process tracer provider.
func (t *Tracing) Provider() trace.TracerProvider { return t.provider }

// Propagator returns the W3C trace-context propagator.
func (t *Tracing) Propagator() propagation.TextMapPropagator { return t.propagator }

// ForceFlush exports every queued span within ctx.
func (t *Tracing) ForceFlush(ctx context.Context) error {
	if err := t.provider.ForceFlush(ctx); err != nil {
		return fmt.Errorf("flush spans: %w", err)
	}

	return nil
}

// Shutdown flushes queued spans and stops export. Later calls return the
// first result.
func (t *Tracing) Shutdown(ctx context.Context) error {
	t.shutdownOnce.Do(func() {
		if err := t.provider.Shutdown(ctx); err != nil {
			t.shutdownErr = fmt.Errorf("shutdown tracing: %w", err)
		}
	})

	return t.shutdownErr
}

// SampledTraceLogger returns log with trace_id when ctx carries a sampled span
// context, so worker lines join an exported trace; otherwise log unchanged.
func SampledTraceLogger(ctx context.Context, log zerolog.Logger) zerolog.Logger {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsSampled() {
		return log
	}

	return log.With().Str("trace_id", sc.TraceID().String()).Logger()
}

// spanAttributeAllowed lists the only span attribute keys that may leave the
// process. Client address, user agent, raw URL path, Host and any identity
// field are dropped whatever instrumentation recorded them. Worker spans add
// only bounded pipeline coordinates: rule_id (a fixed rule set), Kafka
// partition and offset, and batch size and partition count.
func spanAttributeAllowed(key attribute.Key) bool {
	switch key {
	case "http.request.method", "http.response.status_code", "http.route", "url.scheme",
		"network.protocol.version", "error.type",
		"messaging.system", "messaging.destination.name", "messaging.batch.message_count",
		labelRuleID, labelPartition, "offset", "batch.size", "batch.partitions":
		return true
	default:
		return false
	}
}

// allowlistExporter strips attributes outside spanAttributeAllowed before
// delegating to the real exporter.
type allowlistExporter struct {
	next sdktrace.SpanExporter
}

func (e allowlistExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	filtered := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, span := range spans {
		filtered[i] = allowlistedSpan{ReadOnlySpan: span}
	}

	if err := e.next.ExportSpans(ctx, filtered); err != nil {
		return fmt.Errorf("export spans: %w", err)
	}

	return nil
}

func (e allowlistExporter) Shutdown(ctx context.Context) error {
	if err := e.next.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown span exporter: %w", err)
	}

	return nil
}

// allowlistedSpan overrides Attributes on an SDK read-only span.
type allowlistedSpan struct {
	sdktrace.ReadOnlySpan
}

func (s allowlistedSpan) Attributes() []attribute.KeyValue {
	all := s.ReadOnlySpan.Attributes()
	kept := make([]attribute.KeyValue, 0, len(all))
	for _, kv := range all {
		if spanAttributeAllowed(kv.Key) {
			kept = append(kept, kv)
		}
	}

	return kept
}
