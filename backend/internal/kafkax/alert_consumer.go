package kafkax

import (
	"context"
	"errors"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

// NewAlertConsumer uses the established bounded consumed-next adapter with an
// independent alert topic/group. Event-writer configuration remains untouched.
func NewAlertConsumer(ctx context.Context, kafka config.KafkaConfig, writer config.WriterConfig) (*Consumer, error) {
	if kafka.AlertsTopic == "" || kafka.AlertWriterGroup == "" {
		return nil, errors.New("alert topic and consumer group are required")
	}
	kafka.EventsTopic = kafka.AlertsTopic
	kafka.EventWriterGroup = kafka.AlertWriterGroup
	return NewConsumer(ctx, kafka, writer)
}
