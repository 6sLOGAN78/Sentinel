//go:build !integration

package detector

import (
	"context"

	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

// Production hooks deliberately do not inspect configuration or environment.
func reachBoundary(context.Context, string, detection.TopicPartition, int64) error { return nil }

func observeAssignment(context.Context, string, detection.TopicPartition, kafkax.DetectorAssignment) error {
	return nil
}
