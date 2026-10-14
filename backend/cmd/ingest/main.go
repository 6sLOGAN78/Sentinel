// Command ingest is the Sentinel event ingest service process skeleton.
package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/httpx"
	"github.com/6sLOGAN78/sentinel/internal/ingest"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/ratelimit"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
)

func main() {
	os.Exit(app.Run(config.ServiceIngest, setup))
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

	client, err := redisx.New(ctx, cfg.Redis)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}

	lc.Add("redis", app.CloseRedis(client))

	producer, err := kafkax.NewProducer(ctx, cfg.Kafka)
	if err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	lc.Add("kafka", producer.Close)

	if err = producer.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}

	verifier := apikeys.NewVerifier(
		apikeys.NewStore(db.Pool), cfg.Ingest.APIKeyCacheCapacity, cfg.Ingest.APIKeyCacheTTL, nil,
	)
	e := echo.New()
	middleware.Apply(e, &log, cfg.Server.CORSAllowedOrigins)
	ingest.NewHandler(verifier, producer, ratelimit.New(client, cfg.RateLimit, log), cfg, log).Register(e)

	server := httpx.New(cfg.Server, e)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", server.HTTPServer().Addr)
	if err != nil {
		return fmt.Errorf("http listen: %w", err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	lc.Add("http-serve", func(closeCtx context.Context) error {
		select {
		case serveError := <-serveErr:
			return serveError
		case <-closeCtx.Done():
			return closeCtx.Err()
		}
	})
	lc.Add("http", server.Shutdown)

	return nil
}
