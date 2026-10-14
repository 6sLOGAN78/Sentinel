package kafkax

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const (
	// DefaultDLQTopic is the durable quarantine topic used by the event writer.
	DefaultDLQTopic = "security.events.dlq.v1"

	// dlqMetadataBudget bounds the output topic, key, mandatory metadata, and
	// copied origin headers in addition to the configured value allowance.
	dlqMetadataBudget = 8 << 10
)

// DLQReason is a stable, non-sensitive poison classification.
type DLQReason string

const (
	// DLQReasonDecode identifies records that cannot be strictly decoded.
	DLQReasonDecode DLQReason = "decode_error"
	// DLQReasonValidation identifies decoded records that violate the event contract.
	DLQReasonValidation DLQReason = "validation_error"
	// DLQReasonMissingReceivedAt identifies a missing server-owned ingestion timestamp.
	DLQReasonMissingReceivedAt DLQReason = "missing_received_at"
)

// DLQPublisher durably writes bounded quarantine records.
type DLQPublisher struct {
	client        *kgo.Client
	topic         string
	timeout       time.Duration
	maxValueBytes int
}

// NewDLQPublisher configures an independent all-ISR, idempotent producer. A
// blank topic selects DefaultDLQTopic; tests and isolated deployments may pass
// an explicit topic.
func NewDLQPublisher(
	_ context.Context,
	cfg config.KafkaConfig,
	topic string,
	maxValueBytes int,
) (*DLQPublisher, error) {
	brokers, err := config.ParseBrokers(cfg.Brokers)
	if err != nil {
		return nil, err
	}
	if maxValueBytes < 1 {
		return nil, errors.New("DLQ max value bytes must be positive")
	}
	if topic == "" {
		topic = DefaultDLQTopic
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.DefaultProduceTopic(topic),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.AllowIdempotentProduceCancellation(),
		kgo.RecordDeliveryTimeout(cfg.ProduceTimeout),
		kgo.RecordRetries(cfg.RecordRetries),
		kgo.MaxBufferedRecords(cfg.MaxBufferedRecords),
		kgo.MaxBufferedBytes(cfg.MaxBufferedBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka DLQ producer: %w", err)
	}

	return &DLQPublisher{
		client:        client,
		topic:         topic,
		timeout:       cfg.ProduceTimeout,
		maxValueBytes: maxValueBytes,
	}, nil
}

// Publish builds and synchronously acknowledges one quarantine record.
func (p *DLQPublisher) Publish(ctx context.Context, original *kgo.Record, reason DLQReason) error {
	record, err := BuildDLQRecord(original, reason, time.Now().UTC(), p.maxValueBytes)
	if err != nil {
		return err
	}
	record.Topic = p.topic

	publishCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if err = p.client.ProduceSync(publishCtx, record).FirstErr(); err != nil {
		return fmt.Errorf("publish DLQ record: %w", err)
	}

	return nil
}

// Close flushes acknowledged DLQ work within ctx, then closes the producer.
func (p *DLQPublisher) Close(ctx context.Context) error {
	err := p.client.Flush(ctx)
	p.client.Close()
	if err != nil {
		return fmt.Errorf("flush Kafka DLQ producer: %w", err)
	}

	return nil
}

// BuildDLQRecord preserves bounded source context and adds trusted origin
// metadata. Raw decoder or validation errors never enter the record.
func BuildDLQRecord(
	original *kgo.Record,
	reason DLQReason,
	at time.Time,
	maxValueBytes int,
) (*kgo.Record, error) {
	if original == nil {
		return nil, errors.New("build DLQ record: original record is nil")
	}
	if !reason.valid() {
		return nil, errors.New("build DLQ record: unsupported DLQ reason")
	}
	if maxValueBytes < 1 {
		return nil, errors.New("build DLQ record: max value bytes must be positive")
	}

	value := append([]byte(nil), original.Value...)
	headers := []kgo.RecordHeader{
		{Key: "dlq.reason", Value: []byte(reason)},
		{Key: "dlq.origin.topic", Value: []byte(original.Topic)},
		{Key: "dlq.origin.partition", Value: []byte(strconv.FormatInt(int64(original.Partition), 10))},
		{Key: "dlq.origin.offset", Value: []byte(strconv.FormatInt(original.Offset, 10))},
		{Key: "dlq.at", Value: []byte(at.UTC().Format(time.RFC3339Nano))},
	}
	if len(value) > maxValueBytes {
		value = nil
		headers = append(headers,
			kgo.RecordHeader{Key: "dlq.value.redacted", Value: []byte("true")},
			kgo.RecordHeader{
				Key:   "dlq.value.original_bytes",
				Value: []byte(strconv.Itoa(len(original.Value))),
			},
		)
	}

	key := append([]byte(nil), original.Key...)
	limit := maxValueBytes + dlqMetadataBudget
	if dlqRecordSize(DefaultDLQTopic, key, value, headers) > limit {
		key = nil
		headers = append(headers,
			kgo.RecordHeader{Key: "dlq.key.redacted", Value: []byte("true")},
			kgo.RecordHeader{
				Key:   "dlq.key.original_bytes",
				Value: []byte(strconv.Itoa(len(original.Key))),
			},
		)
	}

	for _, header := range original.Headers {
		if strings.HasPrefix(strings.ToLower(header.Key), "dlq.") {
			continue
		}
		candidate := kgo.RecordHeader{
			Key:   header.Key,
			Value: append([]byte(nil), header.Value...),
		}
		if dlqRecordSize(DefaultDLQTopic, key, value, append(headers, candidate)) > limit {
			continue
		}
		headers = append(headers, candidate)
	}

	return &kgo.Record{
		Topic:   DefaultDLQTopic,
		Key:     key,
		Value:   value,
		Headers: headers,
	}, nil
}

func (r DLQReason) valid() bool {
	switch r {
	case DLQReasonDecode, DLQReasonValidation, DLQReasonMissingReceivedAt:
		return true
	default:
		return false
	}
}

func dlqRecordSize(topic string, key, value []byte, headers []kgo.RecordHeader) int {
	total := len(topic) + len(key) + len(value)
	for _, header := range headers {
		total += len(header.Key) + len(header.Value)
	}

	return total
}
