package kafkax //nolint:testpackage // inject the external transport while exercising the real publisher.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

type alertDriver struct {
	*kgo.Client
	produce func(context.Context, ...*kgo.Record) kgo.ProduceResults
}

func (d alertDriver) ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults {
	return d.produce(ctx, records...)
}

func alertFixture() *contracts.Alert {
	start := contracts.NewTimestamp(time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC))
	end := contracts.NewTimestamp(start.Add(5 * time.Minute))
	return &contracts.Alert{
		SchemaVersion: 1, AlertID: strings.Repeat("a", 64), TenantID: "demo", RuleID: "AUTH_BRUTE_FORCE",
		RuleVersion: 1, Severity: "high", Status: "open", CreatedAt: end, WindowStart: start, WindowEnd: end,
		EntityKey: "192.0.2.10|user", Summary: "Failed logins", EventCount: 10,
		SampleEventIDs: []string{"0199da90-9800-7000-8000-000000000001"}, DedupeKey: "display",
	}
}

// Removing all-ISR, changing bytes/key, unbounding buffering/deadlines or ignoring
// any delivery error must fail independently of broker availability.
func TestAlertPublisherBounds(t *testing.T) {
	t.Run("canonical bytes and bounded deadline", func(t *testing.T) {
		called := false
		p := &AlertProducer{topic: "alerts-test", timeout: 100 * time.Millisecond}
		p.client = alertDriver{produce: func(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults {
			called = true
			require.Len(t, records, 1)
			require.Equal(t, "alerts-test", records[0].Topic)
			require.Equal(t, "demo|"+strings.Repeat("a", 64), string(records[0].Key))
			want := `{"schema_version":1,"alert_id":"` + strings.Repeat("a", 64) +
				`","tenant_id":"demo","rule_id":"AUTH_BRUTE_FORCE","rule_version":1,"severity":"high",` +
				`"status":"open","created_at":"2026-10-17T12:05:00.000Z",` +
				`"window_start":"2026-10-17T12:00:00.000Z","window_end":"2026-10-17T12:05:00.000Z",` +
				`"entity_key":"192.0.2.10|user","summary":"Failed logins","event_count":10,` +
				`"sample_event_ids":["0199da90-9800-7000-8000-000000000001"],"dedupe_key":"display"}`
			require.Equal(t, want, string(records[0].Value))
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.Positive(t, time.Until(deadline))
			require.LessOrEqual(t, time.Until(deadline), 100*time.Millisecond)
			return kgo.ProduceResults{{Record: records[0]}}
		}}
		require.NoError(t, p.Publish(context.Background(), []*contracts.Alert{alertFixture()}))
		require.True(t, called)
	})
	t.Run("all ISR finite client options", func(t *testing.T) {
		cfg := config.KafkaConfig{Brokers: "127.0.0.1:1", AlertsTopic: "alerts-test",
			ProduceTimeout: time.Second, RecordRetries: 2, MaxBufferedRecords: 8, MaxBufferedBytes: 65536}
		p, err := NewAlertProducer(context.Background(), cfg)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, p.Close(context.Background())) })
		client := p.client.(*kgo.Client)
		require.Equal(t, kgo.AllISRAcks(), client.OptValue(kgo.RequiredAcks))
		require.Equal(t, time.Second, client.OptValue(kgo.RecordDeliveryTimeout))
		require.EqualValues(t, 2, client.OptValue(kgo.RecordRetries))
		require.EqualValues(t, 8, client.OptValue(kgo.MaxBufferedRecords))
		require.EqualValues(t, 65536, client.OptValue(kgo.MaxBufferedBytes))
	})
	t.Run("delivery failure", func(t *testing.T) {
		p := &AlertProducer{timeout: time.Second, client: alertDriver{
			produce: func(_ context.Context, records ...*kgo.Record) kgo.ProduceResults {
				return kgo.ProduceResults{{Record: records[0]}, {Record: records[1], Err: errors.New("unavailable")}}
			},
		}}
		require.Error(t, p.Publish(context.Background(), []*contracts.Alert{alertFixture(), alertFixture()}))
	})
	t.Run("batch and validation reject before transport", func(t *testing.T) {
		p := &AlertProducer{client: alertDriver{produce: func(context.Context, ...*kgo.Record) kgo.ProduceResults {
			t.Fatal("invalid batch reached transport")
			return nil
		}}}
		require.Error(t, p.Publish(context.Background(), make([]*contracts.Alert, 5)))
		invalid := alertFixture()
		invalid.RuleVersion = 0
		require.Error(t, p.Publish(context.Background(), []*contracts.Alert{invalid}))
	})
}

func capturingAlertProducer(captured *[]*kgo.Record) *AlertProducer {
	return &AlertProducer{topic: "alerts-test", timeout: time.Second, client: alertDriver{
		produce: func(_ context.Context, records ...*kgo.Record) kgo.ProduceResults {
			*captured = append(*captured, records...)
			results := make(kgo.ProduceResults, 0, len(records))
			for _, r := range records {
				results = append(results, kgo.ProduceResult{Record: r})
			}
			return results
		},
	}}
}

// The detect span context travels on the alert record so the alert-writer's
// store_alert span joins the same trace.
func TestAlertPublisherInjectsTraceContext(t *testing.T) {
	canonical, err := json.Marshal(alertFixture())
	require.NoError(t, err)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	ctx, span := provider.Tracer("test").Start(t.Context(), "detect")
	defer span.End()
	want := "00-" + span.SpanContext().TraceID().String() + "-" + span.SpanContext().SpanID().String() + "-01"

	t.Run("canonical publish carries the span context", func(t *testing.T) {
		var captured []*kgo.Record
		p := capturingAlertProducer(&captured)
		require.Same(t, p, p.WithPropagator(propagation.TraceContext{}))
		require.NoError(t, p.PublishCanonical(ctx, canonical))
		require.Len(t, captured, 1)
		require.Equal(t, []kgo.RecordHeader{{Key: "traceparent", Value: []byte(want)}}, captured[0].Headers)
		require.Equal(t, canonical, captured[0].Value, "trace context never alters the canonical bytes")
	})
	t.Run("batch publish carries the span context", func(t *testing.T) {
		var captured []*kgo.Record
		p := capturingAlertProducer(&captured).WithPropagator(propagation.TraceContext{})
		require.NoError(t, p.Publish(ctx, []*contracts.Alert{alertFixture(), alertFixture()}))
		require.Len(t, captured, 2)
		for _, r := range captured {
			require.Equal(t, []kgo.RecordHeader{{Key: "traceparent", Value: []byte(want)}}, r.Headers)
		}
	})
	t.Run("default propagator is W3C trace context", func(t *testing.T) {
		var captured []*kgo.Record
		require.NoError(t, capturingAlertProducer(&captured).PublishCanonical(ctx, canonical))
		require.Len(t, captured, 1)
		require.Equal(t, []kgo.RecordHeader{{Key: "traceparent", Value: []byte(want)}}, captured[0].Headers)
	})
	t.Run("no span adds no header", func(t *testing.T) {
		var captured []*kgo.Record
		p := capturingAlertProducer(&captured).WithPropagator(propagation.TraceContext{})
		require.NoError(t, p.PublishCanonical(context.Background(), canonical))
		require.NoError(t, p.Publish(context.Background(), []*contracts.Alert{alertFixture()}))
		require.Len(t, captured, 2)
		for _, r := range captured {
			require.Empty(t, r.Headers)
		}
	})
}
