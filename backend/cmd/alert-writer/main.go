// Command alert-writer persists immutable evidence and insert-only workflows.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
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

	consumer, err := kafkax.NewAlertConsumer(ctx, cfg.Kafka, cfg.Writer)
	if err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	lc.Add("kafka-consumer", consumer.Close)
	if err = consumer.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}

	clickhouse, err := storageclickhouse.Open(ctx, cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}
	lc.Add("clickhouse", func(context.Context) error { return clickhouse.Close() })

	db, err := database.New(ctx, cfg.Database, log)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}

	lc.Add("postgres", func(context.Context) error {
		db.Close()

		return nil
	})
	startWriter(ctx, cfg, log, lc, consumer, clickhouse, db)

	draining := &opshttp.Draining{}
	opsServer, err = opshttp.Start(cfg.Ops, draining, []opshttp.Check{
		{Name: "kafka", Fn: consumer.Ping},
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

func startWriter(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *app.Lifecycle,
	consumer *kafkax.Consumer, conn storageclickhouse.Conn, db *database.Database) {
	worker := alertwriter.New(consumer, storageclickhouse.NewAlertStore(conn),
		storagepostgres.NewAlertStore(db.Pool, cfg.Writer.OperationTimeout), cfg.Writer, log)
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		err := worker.Run(workerCtx)
		if err != nil {
			log.Error().Err(err).Msg("alert writer stopped")
			lc.Fail(err)
		}
		done <- err
	}()
	lc.Add("alert-writer", func(closeCtx context.Context) error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-closeCtx.Done():
			return closeCtx.Err()
		}
	})
}
