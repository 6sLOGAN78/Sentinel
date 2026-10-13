package kafkax //nolint:testpackage // recordsFor verifies the exact wire contract.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
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

	producer := &Producer{topic: "events-test"}
	records, err := producer.recordsFor(events, "request-1")
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
