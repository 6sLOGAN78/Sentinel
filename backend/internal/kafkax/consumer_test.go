package kafkax_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

func TestNewConsumerRejectsMalformedBrokerList(t *testing.T) {
	_, err := kafkax.NewConsumer(context.Background(), config.KafkaConfig{Brokers: "not-host-port"}, config.WriterConfig{})
	require.ErrorContains(t, err, "kafka")
}
