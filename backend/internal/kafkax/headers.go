package kafkax

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// SampledRecordContext extracts the record's W3C trace context with prop and
// reports whether it is a valid, sampled parent. Workers start spans only
// for sampled parents, so unsampled traffic costs no span. A nil prop uses
// propagation.TraceContext.
func SampledRecordContext(
	ctx context.Context, prop propagation.TextMapPropagator, record *kgo.Record,
) (trace.SpanContext, bool) {
	if prop == nil {
		prop = propagation.TraceContext{}
	}
	sc := trace.SpanContextFromContext(prop.Extract(ctx, HeaderCarrier{Record: record}))

	return sc, sc.IsValid() && sc.IsSampled()
}

// HeaderCarrier adapts a Kafka record's headers to
// propagation.TextMapCarrier so W3C trace context travels with the record.
type HeaderCarrier struct {
	Record *kgo.Record
}

// Get returns the first header value for key, or "".
func (c HeaderCarrier) Get(key string) string {
	for _, header := range c.Record.Headers {
		if header.Key == key {
			return string(header.Value)
		}
	}

	return ""
}

// Set replaces the value of an existing header key or appends a new header.
func (c HeaderCarrier) Set(key, value string) {
	for i := range c.Record.Headers {
		if c.Record.Headers[i].Key == key {
			c.Record.Headers[i].Value = []byte(value)

			return
		}
	}

	c.Record.Headers = append(c.Record.Headers, kgo.RecordHeader{Key: key, Value: []byte(value)})
}

// Keys returns every header key in record order.
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c.Record.Headers))
	for _, header := range c.Record.Headers {
		keys = append(keys, header.Key)
	}

	return keys
}
