// Command query-api is the Sentinel query service process skeleton.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/alertstream"
	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/eventquery"
	"github.com/6sLOGAN78/sentinel/internal/httpx"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
	"github.com/6sLOGAN78/sentinel/internal/query"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

func main() {
	os.Exit(app.Run(config.ServiceQueryAPI, setup))
}

const (
	alertQueryTimeout      = 5 * time.Second
	clickhouseDialTimeout  = 5 * time.Second
	clickhouseReadTimeout  = 30 * time.Second
	clickhouseMaxOpenConns = 4
	clickhouseMaxIdleConns = 2
)

func setup(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *app.Lifecycle) error {
	rules, err := loadStartupRules(cfg.Detector.RulesPath)
	if err != nil {
		return err
	}
	return setupWithRules(ctx, cfg, log, lc, rules)
}

func setupWithRules(ctx context.Context, cfg *config.Config, log zerolog.Logger,
	lc *app.Lifecycle, rules *detection.Rules,
) error {
	jwtVerifier, err := auth.NewVerifier([]byte(cfg.Auth.JWTSecret), cfg.Auth.Issuer, cfg.Auth.Audience)
	if err != nil {
		return err
	}
	cursors, err := query.NewCursorCodec([]byte(cfg.Auth.JWTSecret))
	if err != nil {
		return err
	}
	var opsServer *opshttp.Server
	lc.Add("ops-http", func(closeCtx context.Context) error {
		if opsServer == nil {
			return nil
		}

		return opsServer.Shutdown(closeCtx)
	})

	db, err := database.New(ctx, cfg.Database, log)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}

	lc.Add("postgres", func(context.Context) error {
		db.Close()

		return nil
	})

	// Open lazily so PG workflow reads remain available during a CH outage.
	// Readiness still pings the actual connection; event reads fail independently.
	clickhouseConn, err := driverclickhouse.Open(&driverclickhouse.Options{
		Addr: []string{cfg.ClickHouse.NativeAddress},
		Auth: driverclickhouse.Auth{
			Database: cfg.ClickHouse.Database, Username: cfg.ClickHouse.User, Password: cfg.ClickHouse.Password,
		},
		DialTimeout: clickhouseDialTimeout, ReadTimeout: clickhouseReadTimeout,
		MaxOpenConns: clickhouseMaxOpenConns, MaxIdleConns: clickhouseMaxIdleConns, ConnMaxLifetime: time.Hour,
		Compression: &driverclickhouse.Compression{Method: driverclickhouse.CompressionLZ4},
	})
	if err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}
	lc.Add("clickhouse", func(context.Context) error { return clickhouseConn.Close() })

	client, err := redisx.New(ctx, cfg.Redis)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}

	lc.Add("redis", app.CloseRedis(client))

	draining := &opshttp.Draining{}
	opsServer, err = opshttp.Start(cfg.Ops, draining, []opshttp.Check{
		{Name: "postgres", Fn: db.Pool.Ping},
		{Name: "redis", Fn: func(checkCtx context.Context) error { return client.Ping(checkCtx).Err() }},
		{Name: "clickhouse", Fn: clickhouseConn.Ping},
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

	verifier := apikeys.NewVerifier(apikeys.NewStore(db.Pool), 0, 0, nil)
	e := echo.New()
	middleware.Apply(e, &log, cfg.Server.CORSAllowedOrigins)
	deliveries := alertstream.NewStore(db.Pool)
	deliveries.Register(e, jwtVerifier)
	eventquery.NewAuthenticatedHandler(verifier, jwtVerifier, storageclickhouse.NewEventStore(clickhouseConn)).Register(e)
	e.GET("/api/v1/session", getSession, jwtVerifier.Middleware)
	alerts := storagepostgres.NewAlertStore(db.Pool, alertQueryTimeout)
	query.NewHandler(query.Dependencies{Alerts: alerts, Workflows: alerts, Triage: alerts,
		Details: storageclickhouse.NewQueryAlertStore(clickhouseConn), Auth: jwtVerifier, Cursors: cursors}).Register(e)
	query.NewEventHandler(storageclickhouse.NewQueryEventStore(clickhouseConn), jwtVerifier, cursors).Register(e)
	query.NewRulesHandler(rules, jwtVerifier).Register(e)
	query.NewSummaryHandler(storageclickhouse.NewQueryEventStore(clickhouseConn), alerts, jwtVerifier, nil).Register(e)

	if err = startQueryHTTP(ctx, cfg.Server, e, lc); err != nil {
		return err
	}
	lc.Add("draining", opshttp.DrainCloser(draining, cfg.Ops.DrainHold))
	startDeliveryProjector(ctx, deliveries, draining, lc)

	return nil
}

func startQueryHTTP(ctx context.Context, cfg config.ServerConfig, e *echo.Echo, lc *app.Lifecycle) error {
	server := httpx.New(cfg, e)
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

func startDeliveryProjector(ctx context.Context, deliveries *alertstream.Store,
	draining *opshttp.Draining, lc *app.Lifecycle,
) {
	projectCtx, stopProjector := context.WithCancel(ctx)
	projectDone := make(chan struct{})
	go func() {
		defer close(projectDone)
		if projectErr := deliveries.Run(projectCtx); projectErr != nil && projectCtx.Err() == nil {
			draining.Begin()
			lc.Fail(fmt.Errorf("alert delivery: %w", projectErr))
		}
	}()
	lc.Add("alert-delivery", func(closeCtx context.Context) error {
		stopProjector()
		select {
		case <-projectDone:
			return nil
		case <-closeCtx.Done():
			return closeCtx.Err()
		}
	})
}

func loadStartupRules(path string) (*detection.Rules, error) {
	file, err := os.Open(path) //nolint:gosec // G304: trusted startup config path, never request input; bounded loader.
	if err != nil {
		return nil, fmt.Errorf("open startup rules: %w", err)
	}
	rules, loadErr := detection.LoadRules(file)
	closeErr := file.Close()
	if loadErr != nil {
		return nil, fmt.Errorf("load startup rules: %w", loadErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close startup rules: %w", closeErr)
	}
	return rules, nil
}

func getSession(c echo.Context) error {
	p, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, p)
}
