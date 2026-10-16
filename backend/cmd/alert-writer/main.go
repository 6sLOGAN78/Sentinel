// Command alert-writer is the Sentinel alert persistence process skeleton.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
)

func main() {
	os.Exit(app.Run(config.ServiceAlertWriter, setup))
}

func setup(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *app.Lifecycle) error {
	var opsServer *opshttp.Server
	lc.Add("ops-http", func(closeCtx context.Context) error {
		if opsServer == nil {
			return nil
		}

		return opsServer.Shutdown(closeCtx)
	})

	kafkaClient, err := kafkax.NewProducer(ctx, cfg.Kafka)
	if err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	lc.Add("kafka", kafkaClient.Close)
	if err = kafkaClient.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}

	clickhouse := chmigrate.New(
		cfg.ClickHouse.HTTPAddress,
		cfg.ClickHouse.User,
		cfg.ClickHouse.Password,
		cfg.ClickHouse.Database,
		nil,
	)
	if err = clickhouse.Ping(ctx); err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}

	db, err := database.New(ctx, cfg.Database, log)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}

	lc.Add("postgres", func(context.Context) error {
		db.Close()

		return nil
	})

	draining := &opshttp.Draining{}
	opsServer, err = opshttp.Start(cfg.Ops, draining, []opshttp.Check{
		{Name: "kafka", Fn: kafkaClient.Ping},
		{Name: "clickhouse", Fn: clickhouse.Ping},
		{Name: "postgres", Fn: db.Pool.Ping},
	})
	if err != nil {
		return fmt.Errorf("ops http: %w", err)
	}
	go func() {
		<-opsServer.Done()
		if serveErr := opsServer.Err(); serveErr != nil {
			lc.Fail(fmt.Errorf("ops http: %w", serveErr))
		}
	}()
	lc.Add("draining", opshttp.DrainCloser(draining, cfg.Ops.DrainHold))

	return nil
}
