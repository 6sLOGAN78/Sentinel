// Command detector is the Sentinel detection service process skeleton.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
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

	kafkaClient, err := kafkax.NewProducer(ctx, cfg.Kafka)
	if err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	lc.Add("kafka", kafkaClient.Close)
	if err = kafkaClient.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}

	draining := &opshttp.Draining{}
	opsServer, err = opshttp.Start(cfg.Ops, draining, []opshttp.Check{{Name: "kafka", Fn: kafkaClient.Ping}})
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
