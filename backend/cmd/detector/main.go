// Command detector runs the bounded event-time AUTH detection pipeline.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/detector"
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
)

func main() {
	os.Exit(app.Run(config.ServiceDetector, setup))
}

func setup(ctx context.Context, cfg *config.Config, _ zerolog.Logger, lc *app.Lifecycle) error {
	var opsServer *opshttp.Server
	lc.Add("ops-http", func(closeCtx context.Context) error {
		if opsServer == nil {
			return nil
		}

		return opsServer.Shutdown(closeCtx)
	})

	file, err := os.Open(cfg.Detector.RulesPath)
	if err != nil {
		return fmt.Errorf("open detector rules: %w", err)
	}
	rules, err := detection.LoadRules(file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	consumer, err := detector.NewKafkaConsumer(ctx, cfg.Kafka, cfg.Detector.Writer, rules, lc.Fail)
	if err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	lc.Add("kafka-consumer", consumer.Close)
	if err = consumer.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	publisher, err := detector.NewAlertProducer(ctx, cfg.Kafka)
	if err != nil {
		return err
	}
	lc.Add("kafka-alerts", publisher.Close)
	if err = publisher.Ping(ctx); err != nil {
		return err
	}
	worker := detector.New(consumer, publisher, rules, cfg.Detector.Writer, contracts.LimitsFromConfig(cfg.Contracts))
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() {
		workerErr := worker.Run(workerCtx)
		if workerErr != nil {
			lc.Fail(workerErr)
		}
		workerDone <- workerErr
	}()
	lc.Add("detector", func(closeCtx context.Context) error {
		cancelWorker()
		select {
		case workerErr := <-workerDone:
			return workerErr
		case <-closeCtx.Done():
			return closeCtx.Err()
		}
	})

	draining := &opshttp.Draining{}
	opsServer, err = opshttp.Start(cfg.Ops, draining, []opshttp.Check{
		{Name: "kafka", Fn: consumer.Ping}, {Name: "alerts", Fn: publisher.Ping},
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
