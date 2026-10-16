// Package kafkax provides bounded Kafka clients shared by Sentinel services.
package kafkax

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// Publisher durably publishes one validated event batch.
type Publisher interface {
	PublishBatch(context.Context, []*contracts.Event, string) error
}

// Producer owns one long-lived bounded franz-go client.
type Producer struct {
	client  *kgo.Client
	topic   string
	timeout time.Duration
}

// NewProducer configures an all-ISR, idempotent producer with finite buffers,
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
		kgo.AllowIdempotentProduceCancellation(),
		kgo.RecordDeliveryTimeout(cfg.ProduceTimeout),
		kgo.RecordRetries(cfg.RecordRetries),
		kgo.MaxBufferedRecords(cfg.MaxBufferedRecords),
		kgo.MaxBufferedBytes(cfg.MaxBufferedBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka producer: %w", err)
	}

	return &Producer{client: client, topic: cfg.EventsTopic, timeout: cfg.ProduceTimeout}, nil
}

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
func (p *Producer) PublishBatch(ctx context.Context, events []*contracts.Event, requestID string) error {
	records, err := p.recordsFor(events, requestID)
	if err != nil {
		return err
	}

	produceCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if handled, hookErr := publishWithPrefixBarrier(produceCtx, p.client, records); handled {
		if hookErr != nil {
			return fmt.Errorf("publish event batch: %w", hookErr)
		}

		return nil
	}

	if err = p.client.ProduceSync(produceCtx, records...).FirstErr(); err != nil {
		return fmt.Errorf("publish event batch: %w", err)
	}

	return nil
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

func (p *Producer) recordsFor(events []*contracts.Event, requestID string) ([]*kgo.Record, error) {
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
			Topic: p.topic,
			Key:   []byte(event.TenantID + "|" + sourceIP),
			Value: value,
			Headers: []kgo.RecordHeader{
				{Key: "schema_version", Value: []byte(strconv.Itoa(event.SchemaVersion))},
				{Key: "request_id", Value: []byte(requestID)},
			},
		})
	}

	return records, nil
}
