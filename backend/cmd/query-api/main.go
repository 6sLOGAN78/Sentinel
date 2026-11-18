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
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	metricnoop "go.opentelemetry.io/otel/metric/noop"

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
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
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
	// The readiness prober dials internal ops listeners only; the per-probe
	// request deadline (1 s) lives in the prober.
	readinessDialTimeout    = time.Second
	readinessMaxHeaderBytes = 8 << 10
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

	reg := telemetry.NewRegistry()
	tr, err := openTracing(ctx, cfg, lc)
	if err != nil {
		return err
	}

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
	deliveries := alertstream.NewStore(db.Pool)
	streams, listener, err := startAlertListener(ctx, cfg, deliveries, telemetry.NewSSEMetrics(reg), lc)
	if err != nil {
		return err
	}

	draining := &opshttp.Draining{}
	opsServer, err = startOps(cfg.Ops, draining, []opshttp.Check{
		{Name: "postgres", Fn: db.Pool.Ping},
		{Name: "redis", Fn: func(checkCtx context.Context) error { return client.Ping(checkCtx).Err() }},
		{Name: "clickhouse", Fn: clickhouseConn.Ping},
		{Name: "alert-listener", Fn: listener.Ready},
	}, reg, lc)
	if err != nil {
		return err
	}

	e := echo.New()
	middleware.Apply(e, &log, cfg.Server.CORSAllowedOrigins)
	deliveries.Register(e, jwtVerifier)
	streams.Register(e, jwtVerifier)
	if err = registerQueryRoutes(e, cfg, db.Pool, clickhouseConn, jwtVerifier, cursors, rules); err != nil {
		return err
	}

	if err = startQueryHTTP(ctx, cfg.Server, tracedHandler(e, tr), lc); err != nil {
		return err
	}
	lc.Add("draining", opshttp.DrainCloser(draining, cfg.Ops.DrainHold))
	startDeliveryProjector(ctx, deliveries, draining, lc)
	lc.Add("alert-streams", streams.Close)

	return nil
}

// registerQueryRoutes mounts the tenant-scoped read and triage routes plus the
// lag-only metrics route. Prometheus is deliberately absent from readiness:
// when it is down only the lag values degrade to unavailable.
func registerQueryRoutes(e *echo.Echo, cfg *config.Config, pool *pgxpool.Pool, ch driver.Conn,
	jwtVerifier *auth.Verifier, cursors *query.CursorCodec, rules *detection.Rules,
) error {
	prom, err := query.NewPrometheusMetrics(cfg.Observability.PrometheusURL)
	if err != nil {
		return err
	}
	verifier := apikeys.NewVerifier(apikeys.NewStore(pool), 0, 0, nil)
	eventquery.NewAuthenticatedHandler(verifier, jwtVerifier, storageclickhouse.NewEventStore(ch)).Register(e)
	e.GET("/api/v1/session", getSession, jwtVerifier.Middleware)
	alerts := storagepostgres.NewAlertStore(pool, alertQueryTimeout)
	query.NewHandler(query.Dependencies{Alerts: alerts, Workflows: alerts, Triage: alerts,
		Details: storageclickhouse.NewQueryAlertStore(ch), Auth: jwtVerifier, Cursors: cursors}).Register(e)
	query.NewEventHandler(storageclickhouse.NewQueryEventStore(ch), jwtVerifier, cursors).Register(e)
	query.NewRulesHandler(rules, jwtVerifier).Register(e)
	query.NewSummaryHandler(storageclickhouse.NewQueryEventStore(ch), alerts, jwtVerifier, nil).Register(e)
	query.NewLagHandler(prom, jwtVerifier, cfg.Kafka, nil).Register(e)
	query.NewSystemHandler(prom, newReadinessProber(), jwtVerifier, *cfg.Observability, cfg.Kafka, nil).Register(e)

	return nil
}

// newReadinessProber probes the configured internal ops addresses directly:
// no proxy from the environment, no keep-alive pool across replicas and a
// bounded response header.
func newReadinessProber() *query.DNSReadinessProber {
	return query.NewDNSReadinessProber(net.DefaultResolver, &http.Client{Transport: &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: readinessDialTimeout}).DialContext,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: readinessMaxHeaderBytes,
	}})
}

// openTracing is registered right after ops-http so LIFO closes it after the
// HTTP server, the streams and the projector have stopped, flushing their
// final spans.
func openTracing(ctx context.Context, cfg *config.Config, lc *app.Lifecycle) (*telemetry.Tracing, error) {
	tr, err := telemetry.NewTracing(ctx, cfg.Observability.Tracing, config.ServiceQueryAPI)
	if err != nil {
		return nil, fmt.Errorf("tracing: %w", err)
	}
	lc.Add("tracing", tr.Shutdown)
	return tr, nil
}

// startOps serves health, readiness and the process registry on the internal
// ops listener and fails the lifecycle if that listener stops serving.
func startOps(cfg config.OpsConfig, draining *opshttp.Draining, checks []opshttp.Check,
	reg *prometheus.Registry, lc *app.Lifecycle,
) (*opshttp.Server, error) {
	opsServer, err := opshttp.Start(cfg, draining, checks, opshttp.WithMetricsHandler(telemetry.Handler(reg)))
	if err != nil {
		return nil, fmt.Errorf("ops http: %w", err)
	}
	go func() {
		<-opsServer.Done()
		if serveErr := opsServer.Err(); serveErr != nil {
			lc.Fail(fmt.Errorf("ops http: %w", serveErr))
		}
	}()
	return opsServer, nil
}

func startAlertListener(ctx context.Context, cfg *config.Config, deliveries *alertstream.Store,
	sse *telemetry.SSEMetrics, lc *app.Lifecycle,
) (*alertstream.Handler, *alertstream.Listener, error) {
	streams := alertstream.NewHandler(ctx, deliveries, cfg.Stream).WithMetrics(sse)
	listener, err := alertstream.NewListener(database.ConnString(cfg.Database), streams)
	if err != nil {
		return nil, nil, err
	}
	listenCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if listenErr := listener.Run(listenCtx); listenErr != nil && listenCtx.Err() == nil {
			lc.Fail(fmt.Errorf("alert listener: %w", listenErr))
		}
	}()
	lc.Add("alert-listener", func(closeCtx context.Context) error {
		stop()
		select {
		case <-done:
			return nil
		case <-closeCtx.Done():
			return closeCtx.Err()
		}
	})
	return streams, listener, nil
}

// tracedHandler wraps the public Echo router with the query-api server span,
// using the ingest options: explicit provider and propagator, no OTel globals,
// and a public endpoint so a client traceparent can never force sampling
// (T-05-14-04). middleware.TraceRename names spans by route template.
func tracedHandler(e *echo.Echo, tr *telemetry.Tracing) http.Handler {
	return otelhttp.NewHandler(e, config.ServiceQueryAPI,
		otelhttp.WithTracerProvider(tr.Provider()),
		otelhttp.WithPropagators(tr.Propagator()),
		otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()),
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
	)
}

func startQueryHTTP(ctx context.Context, cfg config.ServerConfig, handler http.Handler, lc *app.Lifecycle) error {
	server := httpx.New(cfg, handler)
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
