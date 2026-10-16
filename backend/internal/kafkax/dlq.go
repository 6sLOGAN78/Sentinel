package kafkax

import (
	"bytes"
	"context"
	"encoding/binary"
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
	dlqMaxHeaders     = 128
	dlqMaxTopicBytes  = 249
	dlqTrustedPrefix  = "dlq."
	// Record-v2: worst-case length/deltas, attributes and header count, plus
	// the 61-byte batch and a conservative one-topic Produce request envelope.
	// Topic length uses five bytes to also cover flexible request versions.
	dlqWireFramingBytes = 10 + 16 + 2 + 61 + 128 + 5
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
	if !validDLQTopic(topic) {
		return nil, errors.New("DLQ destination topic is invalid")
	}
	if _, ok := dlqSum(maxValueBytes, dlqMetadataBudget); !ok {
		return nil, errors.New("DLQ max value bytes overflows record limit")
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
	record, err := BuildDLQRecordForTopic(original, reason, time.Now().UTC(), p.maxValueBytes, p.topic)
	if err != nil {
		return err
	}

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
	return BuildDLQRecordForTopic(original, reason, at, maxValueBytes, DefaultDLQTopic)
}

// BuildDLQRecordForTopic bounds the encoded quarantine envelope for its actual
// destination before copying untrusted key, value, or header data.
func BuildDLQRecordForTopic(
	original *kgo.Record,
	reason DLQReason,
	at time.Time,
	maxValueBytes int,
	topic string,
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
	limit, ok := dlqSum(maxValueBytes, dlqMetadataBudget)
	if !ok {
		return nil, errors.New("build DLQ record: record limit overflow")
	}
	if !validDLQTopic(topic) || !validDLQTopic(original.Topic) {
		return nil, errors.New("build DLQ record: invalid destination or origin topic")
	}

	value := original.Value
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

	size, ok := dlqRecordSize(topic, nil, value, headers)
	if !ok || size > limit {
		return nil, errors.New("build DLQ record: mandatory metadata exceeds record limit")
	}
	key, size, headers, err := dlqBoundKey(original.Key, size, limit, headers)
	if err != nil {
		return nil, err
	}
	headers = dlqCopyHeaders(original.Headers, headers, size, limit)

	return &kgo.Record{
		Topic:   topic,
		Key:     bytes.Clone(key),
		Value:   bytes.Clone(value),
		Headers: headers,
	}, nil
}

func dlqBoundKey(
	key []byte, size, limit int, headers []kgo.RecordHeader,
) ([]byte, int, []kgo.RecordHeader, error) {
	if key == nil {
		return nil, size, headers, nil
	}
	redaction := []kgo.RecordHeader{
		{Key: "dlq.key.redacted", Value: []byte("true")},
		{
			Key:   "dlq.key.original_bytes",
			Value: []byte(strconv.Itoa(len(key))),
		},
	}
	redactionSize := 0
	for _, header := range redaction {
		headerSize, _ := dlqHeaderSize(header)
		redactionSize += headerSize // fixed keys and bounded decimal length
	}
	keySize, ok := dlqDataSize(len(key))
	// Reserve redaction metadata before admitting the key. The nil key already
	// contributes one encoded byte to size.
	candidateSize, fits := dlqSum(size-1, keySize, redactionSize)
	if ok && fits && candidateSize <= limit {
		return key, candidateSize - redactionSize, headers, nil
	}
	size, ok = dlqSum(size, redactionSize)
	if !ok || size > limit {
		return nil, 0, nil, errors.New("build DLQ record: key redaction exceeds record limit")
	}
	return nil, size, append(headers, redaction...), nil
}

func dlqCopyHeaders(source, headers []kgo.RecordHeader, size, limit int) []kgo.RecordHeader {
	for i, header := range source {
		// Bound inspection too: spoofed or oversized headers must not force a
		// full scan of arbitrary source metadata.
		if i >= dlqMaxHeaders || len(headers) >= dlqMaxHeaders {
			break
		}
		if len(header.Key) >= len(dlqTrustedPrefix) &&
			strings.EqualFold(header.Key[:len(dlqTrustedPrefix)], dlqTrustedPrefix) {
			continue
		}
		headerSize, ok := dlqHeaderSize(header)
		candidateSize, fits := dlqSum(size, headerSize)
		if !ok || !fits || candidateSize > limit {
			break
		}
		headers = append(headers, kgo.RecordHeader{
			Key: strings.Clone(header.Key), Value: bytes.Clone(header.Value),
		})
		size = candidateSize
	}
	return headers
}

func (r DLQReason) valid() bool {
	switch r {
	case DLQReasonDecode, DLQReasonValidation, DLQReasonMissingReceivedAt:
		return true
	default:
		return false
	}
}

func validDLQTopic(topic string) bool {
	if len(topic) < 1 || len(topic) > dlqMaxTopicBytes || topic == "." || topic == ".." {
		return false
	}
	for i := range len(topic) {
		c := topic[i]
		if c != '.' && c != '_' && c != '-' && (c < 'a' || c > 'z') &&
			(c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func dlqRecordSize(topic string, key, value []byte, headers []kgo.RecordHeader) (int, bool) {
	keySize, keyOK := dlqDataSize(len(key))
	valueSize, valueOK := dlqDataSize(len(value))
	total, ok := dlqSum(dlqWireFramingBytes, len(topic), keySize, valueSize)
	if !ok || !keyOK || !valueOK {
		return 0, false
	}
	for _, header := range headers {
		headerSize, headerOK := dlqHeaderSize(header)
		total, ok = dlqSum(total, headerSize)
		if !ok || !headerOK {
			return 0, false
		}
	}
	return total, true
}

func dlqHeaderSize(header kgo.RecordHeader) (int, bool) {
	keySize, keyOK := dlqDataSize(len(header.Key))
	valueSize, valueOK := dlqDataSize(len(header.Value))
	total, ok := dlqSum(keySize, valueSize)
	return total, ok && keyOK && valueOK
}

func dlqDataSize(length int) (int, bool) {
	var scratch [binary.MaxVarintLen64]byte
	return dlqSum(length, binary.PutVarint(scratch[:], int64(length)))
}

func dlqSum(parts ...int) (int, bool) {
	const maxInt = int(^uint(0) >> 1)
	total := 0
	for _, part := range parts {
		if part < 0 || part > maxInt-total {
			return 0, false
		}
		total += part
	}
	return total, true
}
