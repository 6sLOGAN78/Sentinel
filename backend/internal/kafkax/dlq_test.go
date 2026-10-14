package kafkax_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

func TestBuildDLQRecordPreservesSmallOrigin(t *testing.T) {
	original := &kgo.Record{
		Topic:     "security.events.v1",
		Partition: 3,
		Offset:    42,
		Key:       []byte("demo|192.0.2.10"),
		Value:     []byte(`{"not":"an event"}`),
		Headers:   []kgo.RecordHeader{{Key: "request_id", Value: []byte("request-1")}},
	}
	at := time.Date(2026, 10, 14, 18, 4, 5, 123_000_000, time.FixedZone("IST", 5*60*60+30*60))

	record, err := kafkax.BuildDLQRecord(original, kafkax.DLQReasonDecode, at, 64<<10)
	require.NoError(t, err)
	require.Equal(t, kafkax.DefaultDLQTopic, record.Topic)
	require.Equal(t, original.Key, record.Key)
	require.Equal(t, original.Value, record.Value)
	require.Equal(t, "decode_error", headerValue(record.Headers, "dlq.reason"))
	require.Equal(t, original.Topic, headerValue(record.Headers, "dlq.origin.topic"))
	require.Equal(t, "3", headerValue(record.Headers, "dlq.origin.partition"))
	require.Equal(t, "42", headerValue(record.Headers, "dlq.origin.offset"))
	require.Equal(t, "2026-10-14T12:34:05.123Z", headerValue(record.Headers, "dlq.at"))
	require.Equal(t, "request-1", headerValue(record.Headers, "request_id"))
	require.Empty(t, headerValue(record.Headers, "dlq.value.redacted"))
}

func TestBuildDLQRecordRedactsOversizeValueAndBoundsHeaderFlood(t *testing.T) {
	const valueCap = 64
	original := &kgo.Record{
		Topic:     "events",
		Partition: 0,
		Offset:    9,
		Key:       []byte("tenant-key"),
		Value:     []byte(strings.Repeat("s", valueCap+1)),
	}
	for i := range 500 {
		original.Headers = append(original.Headers, kgo.RecordHeader{
			Key:   "origin-" + strconv.Itoa(i),
			Value: []byte(strings.Repeat("x", 128)),
		})
	}

	record, err := kafkax.BuildDLQRecord(
		original,
		kafkax.DLQReasonValidation,
		time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
		valueCap,
	)
	require.NoError(t, err)
	require.Empty(t, record.Value)
	require.Equal(t, "true", headerValue(record.Headers, "dlq.value.redacted"))
	require.Equal(t, strconv.Itoa(valueCap+1), headerValue(record.Headers, "dlq.value.original_bytes"))
	require.LessOrEqual(t, dlqRecordBytes(record), valueCap+(8<<10))
	require.Less(t, len(record.Headers), len(original.Headers), "origin header flood must be truncated")
	for _, header := range record.Headers {
		require.NotContains(t, string(header.Value), strings.Repeat("s", valueCap+1))
	}
}

func TestBuildDLQRecordRejectsUnstableReason(t *testing.T) {
	_, err := kafkax.BuildDLQRecord(
		&kgo.Record{Topic: "events"},
		kafkax.DLQReason("decoder leaked: secret"),
		time.Now(),
		64<<10,
	)
	require.ErrorContains(t, err, "DLQ reason")
}

func headerValue(headers []kgo.RecordHeader, key string) string {
	for _, header := range headers {
		if header.Key == key {
			return string(header.Value)
		}
	}

	return ""
}

func dlqRecordBytes(record *kgo.Record) int {
	total := len(record.Topic) + len(record.Key) + len(record.Value)
	for _, header := range record.Headers {
		total += len(header.Key) + len(header.Value)
	}

	return total
}
