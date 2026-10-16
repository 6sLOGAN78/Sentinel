package kafkax //nolint:testpackage // bounded close and rebalance budgeting require white-box state injection

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

func TestNewConsumerRejectsMalformedBrokerList(t *testing.T) {
	_, err := NewConsumer(context.Background(), config.KafkaConfig{Brokers: "not-host-port"}, config.WriterConfig{})
	require.ErrorContains(t, err, "kafka")
}

func TestConsumerCloseHonorsContext(t *testing.T) {
	release := make(chan struct{})
	consumer := &Consumer{
		closeDone: make(chan struct{}),
		closeFn:   func() { <-release },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, consumer.Close(ctx), context.DeadlineExceeded)
	close(release)
	require.Eventually(t, func() bool {
		return consumer.Close(context.Background()) == nil
	}, time.Second, time.Millisecond)
}

func TestRebalanceTimeoutCoversWorstProcessingBudget(t *testing.T) {
	settings := config.WriterConfig{
		MaxAttempts:      3,
		OperationTimeout: 2 * time.Second,
		RetryMaxBackoff:  time.Second,
		FlushInterval:    time.Second,
		ShutdownTimeout:  3 * time.Second,
	}

	require.Equal(t, 27*time.Second, rebalanceTimeout(settings), "collection + insert + aggregate poison + commit + shutdown + margin")
}
