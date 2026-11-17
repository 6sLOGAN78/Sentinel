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
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func main() {
	os.Exit(app.Run(config.ServiceEventWriter, setup))
}

func setup(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *app.Lifecycle) error {
	var opsServer *opshttp.Server
	draining := &opshttp.Draining{}
	reg := telemetry.NewRegistry()
	consumerMetrics := telemetry.NewConsumerMetrics(reg)
	lc.Add("ops-http", func(closeCtx context.Context) error {
		if opsServer == nil {
			return nil
		}

		return opsServer.Shutdown(closeCtx)
	})

	// Registered right after ops-http so LIFO closes it after the worker and
	// the Kafka clients have stopped, flushing their final spans.
	tr, err := telemetry.NewTracing(ctx, cfg.Observability.Tracing, config.ServiceEventWriter)
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	lc.Add("tracing", tr.Shutdown)

	conn, err := storageclickhouse.Open(ctx, cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}
	lc.Add("clickhouse", func(context.Context) error { return conn.Close() })

	// NewConsumer binds its own kadm client as the tracker's end-offset source.
	tracker := kafkax.NewLagTracker(nil, consumerMetrics, cfg.Kafka.EventWriterGroup, cfg.Kafka.EventsTopic, 0)
	consumer, err := kafkax.NewConsumer(ctx, cfg.Kafka, cfg.Writer, kafkax.WithLagTracker(tracker))
	if err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	lc.Add("kafka-consumer", consumer.Close)
	if err = consumer.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	startLagTracker(ctx, lc, draining, tracker)
	dlq, err := kafkax.NewDLQPublisher(ctx, cfg.Kafka, cfg.Kafka.DLQTopic, cfg.Writer.DLQMaxBytes)
	if err != nil {
		return fmt.Errorf("kafka DLQ: %w", err)
	}
	dlq.WithMetrics(consumerMetrics)
	lc.Add("kafka-dlq", dlq.Close)

	worker := eventwriter.New(
		consumer,
		storageclickhouse.NewEventStore(conn),
		dlq,
		cfg.Writer,
		contracts.LimitsFromConfig(cfg.Contracts),
		log,
	).WithMetrics(consumerMetrics).WithTracing(tr.Provider(), tr.Propagator())
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

	opsServer, err = opshttp.Start(cfg.Ops, draining, []opshttp.Check{
		{Name: "kafka", Fn: consumer.Ping},
		{Name: "clickhouse", Fn: conn.Ping},
	}, opshttp.WithMetricsHandler(telemetry.Handler(reg)))
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

// startLagTracker runs the position-lag poller until shutdown. It is
// registered after the Kafka consumer, so LIFO close stops it first.
func startLagTracker(ctx context.Context, lc *app.Lifecycle, draining *opshttp.Draining, tracker *kafkax.LagTracker) {
	lagCtx, stopLag := context.WithCancel(ctx)
	lagDone := make(chan struct{})
	go func() {
		defer close(lagDone)
		if lagErr := tracker.Run(lagCtx); lagErr != nil && lagCtx.Err() == nil {
			draining.Begin()
			lc.Fail(fmt.Errorf("lag tracker: %w", lagErr))
		}
	}()
	lc.Add("lag-tracker", func(closeCtx context.Context) error {
		stopLag()
		select {
		case <-lagDone:
			return nil
		case <-closeCtx.Done():
			return closeCtx.Err()
		}
	})
}
