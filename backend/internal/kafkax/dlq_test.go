package kafkax_test

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

func TestBuildDLQRecordEncodedHeaderFlood(t *testing.T) {
	const valueCap = 64
	cases := map[string][]kgo.RecordHeader{
		"nil values":        make([]kgo.RecordHeader, 10_000),
		"empty values":      make([]kgo.RecordHeader, 10_000),
		"mixed spoofing":    make([]kgo.RecordHeader, 10_000),
		"varint boundaries": {},
	}
	for i := range cases["empty values"] {
		cases["empty values"][i].Value = []byte{}
	}
	for i := range cases["mixed spoofing"] {
		if i%2 == 0 {
			cases["mixed spoofing"][i] = kgo.RecordHeader{Key: "DlQ.reason", Value: []byte("spoofed")}
		}
	}
	for _, n := range []int{0, 1, 63, 64, 127, 128, 8191, 8192} {
		for range 150 {
			cases["varint boundaries"] = append(cases["varint boundaries"], kgo.RecordHeader{
				Key: strings.Repeat("k", n), Value: []byte(strings.Repeat("v", n)),
			})
		}
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			record, err := kafkax.BuildDLQRecord(&kgo.Record{
				Topic: "events", Key: []byte("tenant"), Value: []byte("poison"), Headers: headers,
			}, kafkax.DLQReasonDecode, time.Unix(0, 0), valueCap)
			require.NoError(t, err)
			require.LessOrEqual(t, encodedDLQBytes(record), valueCap+8192, "encoded record exceeds bound")
			require.LessOrEqual(t, len(record.Headers), 128, "trusted metadata counts toward the header cap")
			require.Equal(t, "decode_error", headerValue(record.Headers, "dlq.reason"))
		})
	}
}

// encodedDLQBytes serializes record-v2 fields independently of the production
// accounting. Worst-case deltas plus batch and Produce request framing make the
// oracle conservative even when the producer batches this record with others.
func encodedDLQBytes(record *kgo.Record) int {
	body := []byte{0} // attributes
	var scratch [binary.MaxVarintLen64]byte
	appendVarint := func(value int64) {
		n := binary.PutVarint(scratch[:], value)
		body = append(body, scratch[:n]...)
	}
	appendBytes := func(value []byte) {
		if value == nil {
			appendVarint(-1)
			return
		}
		appendVarint(int64(len(value)))
		body = append(body, value...)
	}
	appendVarint(math.MaxInt64) // timestamp delta
	appendVarint(math.MaxInt32) // offset delta
	appendBytes(record.Key)
	appendBytes(record.Value)
	appendVarint(int64(len(record.Headers)))
	for _, header := range record.Headers {
		appendVarint(int64(len(header.Key)))
		body = append(body, header.Key...)
		appendBytes(header.Value)
	}
	n := binary.PutVarint(scratch[:], int64(len(body)))
	// 61-byte record batch; 128 bytes cover request/header, transactional ID,
	// acks, timeout, arrays, partition and record-set lengths/tags. The topic
	// itself is additional, including its int16 length (or compact equivalent).
	return 61 + 128 + 2 + len(record.Topic) + n + len(body)
}

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

func TestBuildDLQRecordForTopicEncodedBoundaries(t *testing.T) {
	const valueCap = 64
	for _, topic := range []string{kafkax.DefaultDLQTopic, strings.Repeat("t", 249)} {
		for _, n := range []int{0, 1, 63, 64, 127, 128, 7400, 7600, 7800, 8000, 8191, 8192, 1 << 20} {
			t.Run(strconv.Itoa(len(topic))+"/"+strconv.Itoa(n), func(t *testing.T) {
				original := &kgo.Record{
					Topic: strings.Repeat("s", 249), Partition: math.MaxInt32, Offset: math.MaxInt64,
					Key: []byte(strings.Repeat("k", n)), Value: []byte(strings.Repeat("v", valueCap)),
					Headers: []kgo.RecordHeader{
						{Key: "dlq.origin.topic", Value: []byte("forged")},
						{Key: strings.Repeat("h", n), Value: []byte(strings.Repeat("x", n))},
					},
				}
				record, err := kafkax.BuildDLQRecordForTopic(original, kafkax.DLQReasonDecode,
					time.Unix(0, 0), valueCap, topic)
				require.NoError(t, err)
				require.Equal(t, topic, record.Topic)
				require.Equal(t, original.Topic, headerValue(record.Headers, "dlq.origin.topic"))
				require.Equal(t, original.Value, record.Value)
				require.LessOrEqual(t, encodedDLQBytes(record), valueCap+8192)
				require.LessOrEqual(t, len(record.Headers), 128)
				if record.Key == nil {
					require.Equal(t, "true", headerValue(record.Headers, "dlq.key.redacted"))
					require.Equal(t, strconv.Itoa(n), headerValue(record.Headers, "dlq.key.original_bytes"))
				} else {
					require.Equal(t, original.Key, record.Key)
				}
			})
		}
	}
}

func TestBuildDLQRecordForTopicEncodedHeaderFlood(t *testing.T) {
	const valueCap = 64
	topic := strings.Repeat("t", 249)
	record, err := kafkax.BuildDLQRecordForTopic(&kgo.Record{
		Topic: "events", Value: []byte(strings.Repeat("v", valueCap)),
		Headers: make([]kgo.RecordHeader, 10_000),
	}, kafkax.DLQReasonValidation, time.Unix(0, 0), valueCap, topic)
	require.NoError(t, err)
	require.Equal(t, topic, record.Topic)
	require.Len(t, record.Headers, 128)
	require.LessOrEqual(t, encodedDLQBytes(record), valueCap+8192)
}

func TestBuildDLQRecordForTopicRejectsInvalidMetadata(t *testing.T) {
	for _, topic := range []string{"", ".", "..", "has space", "a/b", "a\n", "\u00e9", strings.Repeat("t", 250)} {
		t.Run(strconv.Quote(topic), func(t *testing.T) {
			_, err := kafkax.BuildDLQRecordForTopic(&kgo.Record{Topic: "events"},
				kafkax.DLQReasonDecode, time.Unix(0, 0), 64, topic)
			require.ErrorContains(t, err, "topic")
			_, err = kafkax.BuildDLQRecordForTopic(&kgo.Record{Topic: topic},
				kafkax.DLQReasonDecode, time.Unix(0, 0), 64, "valid-topic_1")
			require.ErrorContains(t, err, "topic")
		})
	}
	_, err := kafkax.BuildDLQRecordForTopic(nil, kafkax.DLQReasonDecode, time.Unix(0, 0), 64, "dlq")
	require.ErrorContains(t, err, "nil")
	for _, capBytes := range []int{0, -1, math.MaxInt, math.MaxInt - 8191} {
		_, err = kafkax.BuildDLQRecordForTopic(&kgo.Record{Topic: "events"},
			kafkax.DLQReasonDecode, time.Unix(0, 0), capBytes, "dlq")
		require.Error(t, err)
	}
}

func TestBuildDLQRecordPreservesNilEmptyAndCopiesSource(t *testing.T) {
	for _, payload := range [][]byte{nil, {}, []byte("source")} {
		original := &kgo.Record{
			Topic: "events", Key: payload, Value: payload,
			Headers: []kgo.RecordHeader{{Key: "nil", Value: nil}, {Key: "empty", Value: []byte{}},
				{Key: "copy", Value: []byte("header")}},
		}
		record, err := kafkax.BuildDLQRecord(original, kafkax.DLQReasonDecode, time.Unix(0, 0), 64)
		require.NoError(t, err)
		require.Equal(t, payload, record.Key)
		require.Equal(t, payload, record.Value)
		require.Nil(t, record.Headers[5].Value)
		require.NotNil(t, record.Headers[6].Value)
		original.Headers[2].Value[0] = 'X'
		require.Equal(t, "header", headerValue(record.Headers, "copy"))
		if len(payload) > 0 {
			payload[0] = 'X'
			require.Equal(t, "source", string(record.Key))
			require.Equal(t, "source", string(record.Value))
		}
	}
}

func TestBuildDLQRecordBoundsSourceInspection(t *testing.T) {
	headers := make([]kgo.RecordHeader, 10_000)
	for i := range headers {
		headers[i] = kgo.RecordHeader{Key: "dLq.reason"}
	}
	headers[len(headers)-1] = kgo.RecordHeader{Key: "late-header", Value: []byte("must not be scanned")}
	record, err := kafkax.BuildDLQRecord(&kgo.Record{Topic: "events", Headers: headers},
		kafkax.DLQReasonDecode, time.Unix(0, 0), 1)
	require.NoError(t, err)
	require.Len(t, record.Headers, 5)
	require.LessOrEqual(t, encodedDLQBytes(record), 1+8192)
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
