// Command query-api is the Sentinel query service process skeleton.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
)

func main() {
	os.Exit(app.Run(config.ServiceQueryAPI, setup))
}

func setup(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *app.Lifecycle) error {
	db, err := database.New(ctx, cfg.Database, log)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}

	lc.Add("postgres", func(context.Context) error {
		db.Close()

		return nil
	})

	if err := app.DialTCP(ctx, cfg.ClickHouse.HTTPAddress, app.DialTimeout); err != nil {
		return err
	}

	lc.Add("clickhouse", func(context.Context) error { return nil })

	client, err := redisx.New(ctx, cfg.Redis)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}

	lc.Add("redis", app.CloseRedis(client))

	return nil
}
