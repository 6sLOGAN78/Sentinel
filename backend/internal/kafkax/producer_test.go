package kafkax //nolint:testpackage // record building and the produce seam verify the exact wire contract.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func TestRecordsForEventsUsesStableKeyHeadersAndCanonicalNulls(t *testing.T) {
	source := "192.0.2.10"
	received := contracts.NewTimestamp(time.Date(2026, 10, 13, 12, 0, 0, 123_000_000, time.UTC))
	events := []*contracts.Event{
		{
			SchemaVersion: contracts.EventSchemaVersion,
			EventID:       "0199da90-9800-7000-8000-000000000001",
			TenantID:      "demo",
			OccurredAt:    received,
			ReceivedAt:    &received,
			Source:        "generator",
			EventType:     "net.connection",
			SourceIP:      &source,
		},
		{
			SchemaVersion: contracts.EventSchemaVersion,
			EventID:       "0199da90-9800-7000-8000-000000000002",
			TenantID:      "demo",
			OccurredAt:    received,
			ReceivedAt:    &received,
			Source:        "generator",
			EventType:     "net.connection",
			SourceIP:      nil,
		},
	}

	records, err := BuildEventRecords(context.Background(), propagation.TraceContext{}, "events-test", events, "request-1")
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, []byte("demo|192.0.2.10"), records[0].Key)
	require.Equal(t, []byte("demo|"), records[1].Key)
	for _, record := range records {
		require.Equal(t, "events-test", record.Topic)
		require.Equal(t, "1", headerValue(record.Headers, "schema_version"))
		require.Equal(t, "request-1", headerValue(record.Headers, "request_id"))
	}

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(records[1].Value, &decoded))
	value, exists := decoded["source_ip"]
	require.True(t, exists, "canonical record retains an explicit null source_ip")
	require.Nil(t, value)
}

func headerValue(headers []kgo.RecordHeader, key string) string {
	for _, header := range headers {
		if header.Key == key {
			return string(header.Value)
		}
	}

	return ""
}

// tracedProducer returns a producer whose produce seam captures the records it
// would send, traced by a provider at the given ratio with an in-memory exporter.
func tracedProducer(
	t *testing.T, cfg config.TracingConfig, produceErr error,
) (*Producer, *tracetest.InMemoryExporter, *telemetry.Tracing, *[]*kgo.Record) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tr, err := telemetry.NewTracing(t.Context(), cfg, "ingest", telemetry.WithExporter(exporter))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Shutdown(context.Background())) })

	captured := &[]*kgo.Record{}
	producer := &Producer{topic: "events-test", timeout: time.Second, maxBuffered: 100}
	producer.produce = func(_ context.Context, records []*kgo.Record) error {
		*captured = records

		return produceErr
	}

	return producer.WithTracing(tr.Provider(), tr.Propagator()), exporter, tr, captured
}

func tracedEvents(count int) []*contracts.Event {
	at := contracts.NewTimestamp(time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC))
	events := make([]*contracts.Event, 0, count)
	for range count {
		source := "192.0.2.10"
		events = append(events, &contracts.Event{
			SchemaVersion: contracts.EventSchemaVersion, EventID: "0199da90-9800-7000-8000-000000000001",
			TenantID: "demo", OccurredAt: at, ReceivedAt: &at, Source: "generator",
			EventType: "net.connection", SourceIP: &source,
		})
	}

	return events
}

func spanNamed(t *testing.T, exporter *tracetest.InMemoryExporter, name string) tracetest.SpanStub {
	t.Helper()

	for _, span := range exporter.GetSpans() {
		if span.Name == name {
			return span
		}
	}
	require.FailNow(t, "span not exported", name)

	return tracetest.SpanStub{}
}

func enabledFull() config.TracingConfig {
	return config.TracingConfig{Enabled: true, SampleRatio: 1, OTLPEndpoint: "http://127.0.0.1:4318"}
}

func TestPublishBatchSpanCarriesSameTraceparentOnEveryRecord(t *testing.T) {
	t.Parallel()

	producer, exporter, tr, captured := tracedProducer(t, enabledFull(), nil)
	parentCtx, parent := tr.Provider().Tracer("test").Start(t.Context(), "ingest")
	require.NoError(t, producer.PublishBatch(parentCtx, tracedEvents(3), "request-7"))
	parent.End()
	require.NoError(t, tr.ForceFlush(t.Context()))

	publish := spanNamed(t, exporter, "publish")
	require.Equal(t, trace.SpanKindProducer, publish.SpanKind)
	require.Equal(t, parent.SpanContext().SpanID(), publish.Parent.SpanID(), "publish is a child of the server span")
	require.ElementsMatch(t, []attribute.KeyValue{
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.destination.name", "events-test"),
		attribute.Int("messaging.batch.message_count", 3),
	}, publish.Attributes, "only messaging attributes, never tenant/IP/principal")

	want := "00-" + publish.SpanContext.TraceID().String() + "-" + publish.SpanContext.SpanID().String() + "-01"
	require.Len(t, *captured, 3)
	for _, record := range *captured {
		require.Equal(t, want, headerValue(record.Headers, "traceparent"))
		require.Equal(t, "1", headerValue(record.Headers, "schema_version"))
		require.Equal(t, "request-7", headerValue(record.Headers, "request_id"))
		require.Len(t, record.Headers, 3)
	}
}

func TestPublishBatchFailureMarksPublishSpanError(t *testing.T) {
	t.Parallel()

	producer, exporter, tr, _ := tracedProducer(t, enabledFull(), errors.New("broker unavailable"))
	err := producer.PublishBatch(t.Context(), tracedEvents(1), "request-1")
	require.ErrorContains(t, err, "publish event batch")
	require.NoError(t, tr.ForceFlush(t.Context()))

	publish := spanNamed(t, exporter, "publish")
	require.Equal(t, codes.Error, publish.Status.Code)
}

func TestPublishBatchDisabledTracingStillAddsUnsampledTraceparent(t *testing.T) {
	t.Parallel()

	producer, exporter, tr, captured := tracedProducer(t, config.TracingConfig{}, nil)
	require.NoError(t, producer.PublishBatch(t.Context(), tracedEvents(2), "request-1"))
	require.NoError(t, tr.ForceFlush(t.Context()))

	require.Empty(t, exporter.GetSpans())
	first := headerValue((*captured)[0].Headers, "traceparent")
	require.Regexp(t, `^00-[0-9a-f]{32}-[0-9a-f]{16}-00$`, first)
	require.NotContains(t, first, strings.Repeat("0", 32))
	require.Equal(t, first, headerValue((*captured)[1].Headers, "traceparent"))
}

func TestPublishBatchWithoutTracingInjectsIncomingContext(t *testing.T) {
	t.Parallel()

	var captured []*kgo.Record
	producer := &Producer{topic: "events-test", timeout: time.Second, maxBuffered: 100}
	producer.produce = func(_ context.Context, records []*kgo.Record) error {
		captured = records

		return nil
	}

	require.NoError(t, producer.PublishBatch(t.Context(), tracedEvents(1), "request-1"))
	require.Empty(t, headerValue(captured[0].Headers, "traceparent"), "no span context, no header")

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{
			0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36,
		},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	})
	require.NoError(t, producer.PublishBatch(trace.ContextWithSpanContext(t.Context(), sc), tracedEvents(1), "request-1"))
	require.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		headerValue(captured[0].Headers, "traceparent"))
}

func TestPublishBatchShedsWhenBatchDoesNotFitTheBuffer(t *testing.T) {
	t.Parallel()

	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	producer := &Producer{topic: "events-test", timeout: time.Second, maxBuffered: 10}
	producer.produce = func(context.Context, []*kgo.Record) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return nil
	}
	first := make(chan error, 1)
	go func() { first <- producer.PublishBatch(context.Background(), tracedEvents(6), "first") }()
	<-entered

	started := time.Now()
	err := producer.PublishBatch(t.Context(), tracedEvents(6), "over")
	require.ErrorIs(t, err, ErrBufferFull)
	require.Less(t, time.Since(started), producer.timeout/2, "a full buffer sheds at once, not after the timeout")
	require.Equal(t, int32(1), calls.Load(), "a rejected batch must not reach the client")
	require.NoError(t, producer.PublishBatch(t.Context(), tracedEvents(4), "fits"), "6 + 4 fits a limit of 10")

	close(release)
	require.NoError(t, <-first)
	require.NoError(t, producer.PublishBatch(t.Context(), tracedEvents(10), "after"), "finished batches release space")
	require.Equal(t, int32(3), calls.Load())
}

func TestPublishBatchReleasesReservationOnFailure(t *testing.T) {
	t.Parallel()

	producer := &Producer{topic: "events-test", timeout: time.Second, maxBuffered: 5}
	producer.produce = func(context.Context, []*kgo.Record) error { return errors.New("broker down") }
	require.Error(t, producer.PublishBatch(t.Context(), tracedEvents(5), "fails"))
	producer.produce = func(context.Context, []*kgo.Record) error { return nil }
	require.NoError(t, producer.PublishBatch(t.Context(), tracedEvents(5), "after"), "a failed batch released its space")
}

func TestNewProducerReservesUpToMaxBufferedRecords(t *testing.T) {
	t.Parallel()

	producer, err := NewProducer(t.Context(), config.KafkaConfig{
		Brokers: "127.0.0.1:1", EventsTopic: "events-test", ProduceTimeout: time.Second,
		MaxBufferedRecords: 7, MaxBufferedBytes: 1 << 20,
	})
	require.NoError(t, err)
	t.Cleanup(producer.client.Close)
	require.Equal(t, int64(7), producer.maxBuffered)
}
