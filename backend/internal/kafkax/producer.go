// Package kafkax provides bounded Kafka clients shared by Sentinel services.
package kafkax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// Publisher durably publishes one validated event batch.
type Publisher interface {
	PublishBatch(context.Context, []*contracts.Event, string) error
}

// publishSpanName names the span wrapping one batch produce.
const publishSpanName = "publish"

// ErrBufferFull reports a batch that does not fit the remaining producer
// buffer. It is returned before any record reaches the client, so callers
// shed the request (503) instead of waiting for space.
var ErrBufferFull = errors.New("kafka producer buffer full")

// Producer owns one long-lived bounded franz-go client.
type Producer struct {
	client     *kgo.Client
	produce    func(context.Context, []*kgo.Record) error
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	topic      string
	timeout    time.Duration
	// maxBuffered is MaxBufferedRecords. reserved counts the records of every
	// batch currently inside publish, so franz-go's buffer (including callers
	// it would block in Produce) never exceeds the limit.
	maxBuffered int64
	reserved    atomic.Int64
}

// NewProducer configures an all-ISR, non-idempotent producer (ADR 0013) with finite buffers,
// retries, and delivery time.
func NewProducer(_ context.Context, cfg config.KafkaConfig) (*Producer, error) {
	brokers, err := config.ParseBrokers(cfg.Brokers)
	if err != nil {
		return nil, err
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.DefaultProduceTopic(cfg.EventsTopic),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		// ADR 0013: idempotence plus cancellation can lose acknowledged records.
		kgo.DisableIdempotentWrite(),
		kgo.RecordDeliveryTimeout(cfg.ProduceTimeout),
		kgo.RecordRetries(cfg.RecordRetries),
		kgo.MaxBufferedRecords(cfg.MaxBufferedRecords),
		kgo.MaxBufferedBytes(cfg.MaxBufferedBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka producer: %w", err)
	}

	return &Producer{
		client: client,
		produce: func(ctx context.Context, records []*kgo.Record) error {
			return client.ProduceSync(ctx, records...).FirstErr()
		},
		topic:       cfg.EventsTopic,
		timeout:     cfg.ProduceTimeout,
		maxBuffered: int64(cfg.MaxBufferedRecords),
	}, nil
}

// WithTracing makes PublishBatch start a "publish" span from tp and inject
// its context into every record with prop.
func (p *Producer) WithTracing(tp trace.TracerProvider, prop propagation.TextMapPropagator) *Producer {
	p.tracer = tp.Tracer("sentinel/kafkax")
	p.propagator = prop

	return p
}

// tracing returns the configured tracer and propagator. Without WithTracing a
// noop tracer keeps the caller's span context, which is still injected.
func (p *Producer) tracing() (trace.Tracer, propagation.TextMapPropagator) {
	tracer, prop := p.tracer, p.propagator
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("sentinel/kafkax")
	}
	if prop == nil {
		prop = propagation.TraceContext{}
	}

	return tracer, prop
}

// BufferedRecords reports the records admitted to publish and not yet
// acknowledged or failed. The reservation caps it at MaxBufferedRecords.
// franz-go's BufferedProduceRecords is not used: it also counts callers
// blocked in Produce and decrements only after a record's promise runs.
func (p *Producer) BufferedRecords() int64 { return p.reserved.Load() }

// Ping verifies that a broker is reachable and the producer topic has usable
// partition leaders. A broker-only metadata response is not sufficient for
// publication readiness after an outage.
func (p *Producer) Ping(ctx context.Context) error {
	if err := p.client.Ping(ctx); err != nil {
		return fmt.Errorf("ping Kafka: %w", err)
	}
	req := kmsg.NewPtrMetadataRequest()
	req.Topics = []kmsg.MetadataRequestTopic{{Topic: kmsg.StringPtr(p.topic)}}
	response, err := req.RequestWith(ctx, p.client)
	if err != nil {
		return fmt.Errorf("read Kafka topic metadata: %w", err)
	}
	if len(response.Topics) != 1 || response.Topics[0].Topic == nil || *response.Topics[0].Topic != p.topic {
		return fmt.Errorf("read Kafka topic metadata: topic %q missing", p.topic)
	}
	topic := response.Topics[0]
	if err = kerr.ErrorForCode(topic.ErrorCode); err != nil {
		return fmt.Errorf("read Kafka topic metadata: %w", err)
	}
	if len(topic.Partitions) == 0 {
		return fmt.Errorf("read Kafka topic metadata: topic %q has no partitions", p.topic)
	}
	for _, partition := range topic.Partitions {
		if err = kerr.ErrorForCode(partition.ErrorCode); err != nil {
			return fmt.Errorf("read Kafka topic metadata: partition %d unavailable: %w", partition.Partition, err)
		}
		if partition.Leader < 0 {
			return fmt.Errorf("read Kafka topic metadata: partition %d has no leader", partition.Partition)
		}
	}

	return nil
}

// Close flushes acknowledged work within ctx, then closes the client.
func (p *Producer) Close(ctx context.Context) error {
	err := p.client.Flush(ctx)
	p.client.Close()
	if err != nil {
		return fmt.Errorf("flush Kafka producer: %w", err)
	}

	return nil
}

// PublishBatch waits for every record result and returns the first error.
// One "publish" span covers the whole batch, and every record carries the
// same traceparent. Span attributes never include tenant, IP or principal.
func (p *Producer) PublishBatch(ctx context.Context, events []*contracts.Event, requestID string) error {
	tracer, prop := p.tracing()
	publishCtx, span := tracer.Start(ctx, publishSpanName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination.name", p.topic),
			attribute.Int("messaging.batch.message_count", len(events)),
		))
	defer span.End()

	if err := p.publish(publishCtx, prop, events, requestID); err != nil {
		span.SetStatus(codes.Error, "publish failed")

		return err
	}

	return nil
}

func (p *Producer) publish(
	ctx context.Context, prop propagation.TextMapPropagator, events []*contracts.Event, requestID string,
) error {
	records, err := BuildEventRecords(ctx, prop, p.topic, events, requestID)
	if err != nil {
		return err
	}
	if !p.reserve(int64(len(records))) {
		return fmt.Errorf("publish event batch: %w", ErrBufferFull)
	}
	defer p.reserved.Add(-int64(len(records)))

	produceCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if handled, hookErr := publishWithPrefixBarrier(produceCtx, p.client, records); handled {
		if hookErr != nil {
			return fmt.Errorf("publish event batch: %w", hookErr)
		}

		return nil
	}

	if err = p.produce(produceCtx, records); err != nil {
		return fmt.Errorf("publish event batch: %w", err)
	}

	return nil
}

// reserve admits a whole batch only if it fits the remaining buffer, so a
// batch is either fully handed to franz-go or shed before any record is sent.
// The reservation is released once ProduceSync has resolved every record.
func (p *Producer) reserve(n int64) bool {
	for {
		current := p.reserved.Load()
		if current+n > p.maxBuffered {
			return false
		}
		if p.reserved.CompareAndSwap(current, current+n) {
			return true
		}
	}
}

type wireEvent struct {
	SchemaVersion   int                  `json:"schema_version"`
	EventID         string               `json:"event_id"`
	TenantID        string               `json:"tenant_id"`
	OccurredAt      contracts.Timestamp  `json:"occurred_at"`
	ReceivedAt      *contracts.Timestamp `json:"received_at"`
	Source          string               `json:"source"`
	EventType       string               `json:"event_type"`
	SourceIP        *string              `json:"source_ip"`
	DestinationIP   *string              `json:"destination_ip"`
	DestinationPort *int                 `json:"destination_port"`
	Protocol        *string              `json:"protocol"`
	Principal       *string              `json:"principal"`
	Action          *string              `json:"action"`
	Outcome         *string              `json:"outcome"`
	Attributes      map[string]any       `json:"attributes"`
}

// BuildEventRecords encodes events as canonical topic records with the
// schema_version and request_id headers. The trace context of ctx is injected
// once and copied, so every record of the batch has the same traceparent.
func BuildEventRecords(
	ctx context.Context, prop propagation.TextMapPropagator, topic string, events []*contracts.Event, requestID string,
) ([]*kgo.Record, error) {
	records := make([]*kgo.Record, 0, len(events))
	for _, event := range events {
		value, err := json.Marshal(wireEvent{
			SchemaVersion: event.SchemaVersion, EventID: event.EventID, TenantID: event.TenantID,
			OccurredAt: event.OccurredAt, ReceivedAt: event.ReceivedAt, Source: event.Source,
			EventType: event.EventType, SourceIP: event.SourceIP, DestinationIP: event.DestinationIP,
			DestinationPort: event.DestinationPort, Protocol: event.Protocol, Principal: event.Principal,
			Action: event.Action, Outcome: event.Outcome, Attributes: event.Attributes,
		})
		if err != nil {
			return nil, fmt.Errorf("encode event: %w", err)
		}

		sourceIP := ""
		if event.SourceIP != nil {
			sourceIP = *event.SourceIP
		}
		records = append(records, &kgo.Record{
			Topic: topic,
			Key:   []byte(event.TenantID + "|" + sourceIP),
			Value: value,
			Headers: []kgo.RecordHeader{
				{Key: "schema_version", Value: []byte(strconv.Itoa(event.SchemaVersion))},
				{Key: "request_id", Value: []byte(requestID)},
			},
		})
	}

	if len(records) > 0 {
		first := HeaderCarrier{Record: records[0]}
		prop.Inject(ctx, first)
		for _, record := range records[1:] {
			for _, field := range prop.Fields() {
				if value := first.Get(field); value != "" {
					HeaderCarrier{Record: record}.Set(field, value)
				}
			}
		}
	}

	return records, nil
}
