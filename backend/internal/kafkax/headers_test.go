package kafkax_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

func TestHeaderCarrierRoundTrip(t *testing.T) {
	t.Parallel()

	record := &kgo.Record{Headers: []kgo.RecordHeader{{Key: "request_id", Value: []byte("r-1")}}}
	carrier := kafkax.HeaderCarrier{Record: record}

	require.Equal(t, "r-1", carrier.Get("request_id"))
	require.Empty(t, carrier.Get("traceparent"))

	carrier.Set("traceparent", "a")
	carrier.Set("traceparent", "b")
	require.Equal(t, "b", carrier.Get("traceparent"))
	require.Equal(t, []string{"request_id", "traceparent"}, carrier.Keys())
	require.Len(t, record.Headers, 2, "Set replaces an existing key instead of duplicating it")
}

func TestHeaderCarrierInjectExtractPreservesTrace(t *testing.T) {
	t.Parallel()

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{
			0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36,
		},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	})
	prop := propagation.TraceContext{}
	record := &kgo.Record{}
	prop.Inject(trace.ContextWithSpanContext(context.Background(), sc), kafkax.HeaderCarrier{Record: record})

	require.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		kafkax.HeaderCarrier{Record: record}.Get("traceparent"))

	extracted := trace.SpanContextFromContext(prop.Extract(context.Background(), kafkax.HeaderCarrier{Record: record}))
	require.Equal(t, sc.TraceID(), extracted.TraceID())
	require.Equal(t, sc.SpanID(), extracted.SpanID())
	require.True(t, extracted.IsSampled())
	require.True(t, extracted.IsRemote())
}

func TestHeaderCarrierIgnoresMalformedTraceparent(t *testing.T) {
	t.Parallel()

	record := &kgo.Record{Headers: []kgo.RecordHeader{{Key: "traceparent", Value: []byte("not-a-trace")}}}
	extracted := trace.SpanContextFromContext(
		propagation.TraceContext{}.Extract(context.Background(), kafkax.HeaderCarrier{Record: record}))
	require.False(t, extracted.IsValid())
}
