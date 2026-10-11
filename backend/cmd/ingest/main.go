// Command ingest is the Sentinel event ingest service process skeleton.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
)

func main() {
	os.Exit(app.Run(config.ServiceIngest, setup))
}

func setup(ctx context.Context, cfg *config.Config, _ zerolog.Logger, lc *app.Lifecycle) error {
	client, err := redisx.New(ctx, cfg.Redis)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}

	lc.Add("redis", app.CloseRedis(client))

	if err := app.DialBrokers(ctx, cfg.Kafka.Brokers, app.DialTimeout); err != nil {
		return err
	}

	lc.Add("kafka", func(context.Context) error { return nil })

	return nil
}
