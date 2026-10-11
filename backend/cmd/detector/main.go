// Command detector is the Sentinel detection service process skeleton.
package main

import (
	"context"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
)

func main() {
	os.Exit(app.Run(config.ServiceDetector, setup))
}

func setup(ctx context.Context, cfg *config.Config, _ zerolog.Logger, lc *app.Lifecycle) error {
	if err := app.DialBrokers(ctx, cfg.Kafka.Brokers, app.DialTimeout); err != nil {
		return err
	}

	lc.Add("kafka", func(context.Context) error { return nil })

	return nil
}
