package kafkax

import (
	"context"
	"errors"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

type alertClient interface {
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
	Flush(context.Context) error
	Close()
}

// AlertProducer publishes canonical alerts using a bounded client.
type AlertProducer struct {
	client    alertClient
	readiness *Producer
	topic     string
	timeout   time.Duration
}

// NewAlertProducer constructs the detector's independent publisher.
func NewAlertProducer(context.Context, config.KafkaConfig) (*AlertProducer, error) {
	return nil, errors.New("alert adapter not extracted")
}

// Publish acknowledges every canonical alert.
func (*AlertProducer) Publish(context.Context, []*contracts.Alert) error {
	return errors.New("alert adapter not extracted")
}

// Close releases the adapter's clients.
func (*AlertProducer) Close(context.Context) error { return nil }
