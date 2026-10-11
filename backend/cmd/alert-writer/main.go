// Command alert-writer is the Sentinel alert persistence process skeleton.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
)

func main() {
	os.Exit(app.Run(config.ServiceAlertWriter, setup))
}

func setup(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *app.Lifecycle) error {
	if err := app.DialBrokers(ctx, cfg.Kafka.Brokers, app.DialTimeout); err != nil {
		return err
	}

	lc.Add("kafka", func(context.Context) error { return nil })

	if err := app.DialTCP(ctx, cfg.ClickHouse.HTTPAddress, app.DialTimeout); err != nil {
		return err
	}

	lc.Add("clickhouse", func(context.Context) error { return nil })

	db, err := database.New(ctx, cfg.Database, log)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}

	lc.Add("postgres", func(context.Context) error {
		db.Close()

		return nil
	})

	return nil
}
