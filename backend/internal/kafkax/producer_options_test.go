package kafkax //nolint:testpackage // reads the configured franz-go client options of each producer.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

func optionTestKafka() config.KafkaConfig {
	return config.KafkaConfig{
		Brokers: "127.0.0.1:1", EventsTopic: "events-test", AlertsTopic: "alerts-test",
		ProduceTimeout: time.Second, RecordRetries: 5, MaxBufferedRecords: 100, MaxBufferedBytes: 1 << 20,
	}
}

// requireNoSequenceRewind pins ADR 0013: cancellable delivery with idempotence
// lets franz-go rewind sequence numbers, so the broker can deduplicate a new,
// acknowledged batch away. Producers disable idempotence instead, keep all-ISR
// acks and one in-flight request per broker (ordering).
func requireNoSequenceRewind(t *testing.T, client *kgo.Client) {
	t.Helper()
	require.Equal(t, true, client.OptValue(kgo.DisableIdempotentWrite), "idempotent write must be disabled")
	require.Equal(t, false, client.OptValue(kgo.AllowIdempotentProduceCancellation),
		"idempotent cancellation rewinds sequences and loses acknowledged records")
	require.Equal(t, 1, client.OptValue(kgo.MaxProduceRequestsInflightPerBroker))
	require.Equal(t, kgo.AllISRAcks(), client.OptValue(kgo.RequiredAcks))
}

func TestEventProducerCannotRewindSequences(t *testing.T) {
	t.Parallel()

	producer, err := NewProducer(t.Context(), optionTestKafka())
	require.NoError(t, err)
	t.Cleanup(producer.client.Close)
	requireNoSequenceRewind(t, producer.client)
}

func TestDLQPublisherCannotRewindSequences(t *testing.T) {
	t.Parallel()

	publisher, err := NewDLQPublisher(t.Context(), optionTestKafka(), "", 64<<10)
	require.NoError(t, err)
	t.Cleanup(publisher.client.Close)
	requireNoSequenceRewind(t, publisher.client)
}

func TestAlertProducerCannotRewindSequences(t *testing.T) {
	t.Parallel()

	producer, err := NewAlertProducer(t.Context(), optionTestKafka())
	require.NoError(t, err)
	client, ok := producer.client.(*kgo.Client)
	require.True(t, ok)
	t.Cleanup(func() { client.Close(); producer.readiness.client.Close() })
	requireNoSequenceRewind(t, client)
	requireNoSequenceRewind(t, producer.readiness.client)
}
