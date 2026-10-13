// Command query-api is the Sentinel query service process skeleton.
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
	"github.com/6sLOGAN78/sentinel/internal/eventquery"
	"github.com/6sLOGAN78/sentinel/internal/httpx"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
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

	clickhouseConn, err := storageclickhouse.Open(ctx, cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}
	lc.Add("clickhouse", func(context.Context) error { return clickhouseConn.Close() })

	client, err := redisx.New(ctx, cfg.Redis)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}

	lc.Add("redis", app.CloseRedis(client))

	verifier := apikeys.NewVerifier(apikeys.NewStore(db.Pool), 0, 0, nil)
	e := echo.New()
	middleware.Apply(e, &log, cfg.Server.CORSAllowedOrigins)
	eventquery.NewHandler(verifier, storageclickhouse.NewEventStore(clickhouseConn)).Register(e)

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
