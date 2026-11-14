package kafkax

import (
	"context"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// NewDLQPublisherWithProducer exposes the produce seam to external tests so a
// publish acknowledgement or failure can be scripted without a broker.
func NewDLQPublisherWithProducer(
	produce func(context.Context, *kgo.Record) error, topic string, timeout time.Duration, maxValueBytes int,
) *DLQPublisher {
	return &DLQPublisher{produce: produce, topic: topic, timeout: timeout, maxValueBytes: maxValueBytes}
}
