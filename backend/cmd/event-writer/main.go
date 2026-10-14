// Command event-writer is the Sentinel ClickHouse event writer process skeleton.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/eventwriter"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

func main() {
	os.Exit(app.Run(config.ServiceEventWriter, setup))
}

func setup(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *app.Lifecycle) error {
	conn, err := storageclickhouse.Open(ctx, cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}
	lc.Add("clickhouse", func(context.Context) error { return conn.Close() })

	consumer, err := kafkax.NewConsumer(ctx, cfg.Kafka, cfg.Writer)
	if err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	lc.Add("kafka-consumer", consumer.Close)
	if err = consumer.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	dlq, err := kafkax.NewDLQPublisher(ctx, cfg.Kafka, "", cfg.Writer.DLQMaxBytes)
	if err != nil {
		return fmt.Errorf("kafka DLQ: %w", err)
	}
	lc.Add("kafka-dlq", dlq.Close)

	worker := eventwriter.New(
		consumer,
		storageclickhouse.NewEventStore(conn),
		dlq,
		cfg.Writer,
		contracts.LimitsFromConfig(cfg.Contracts),
		log,
	)
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() {
		workerErr := worker.Run(workerCtx)
		if workerErr != nil {
			log.Error().Err(workerErr).Msg("event writer stopped")
			lc.Fail(workerErr)
		}
		workerDone <- workerErr
	}()
	lc.Add("event-writer", func(closeCtx context.Context) error {
		cancelWorker()
		select {
		case workerErr := <-workerDone:
			return workerErr
		case <-closeCtx.Done():
			return closeCtx.Err()
		}
	})

	return nil
}
