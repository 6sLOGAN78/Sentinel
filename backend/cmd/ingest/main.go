// Command ingest is the Sentinel event ingest service process skeleton.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/httpx"
	"github.com/6sLOGAN78/sentinel/internal/ingest"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
	"github.com/6sLOGAN78/sentinel/internal/ratelimit"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func main() {
	os.Exit(app.Run(config.ServiceIngest, setup))
}

func setup(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *app.Lifecycle) error {
	reg := telemetry.NewRegistry()
	ingestMetrics := telemetry.NewIngestMetrics(reg)

	var opsServer *opshttp.Server
	lc.Add("ops-http", func(closeCtx context.Context) error {
		if opsServer == nil {
			return nil
		}

		return opsServer.Shutdown(closeCtx)
	})

	// Registered right after ops-http so LIFO closes it after the HTTP server
	// and the producer have stopped, flushing their final spans.
	tr, err := telemetry.NewTracing(ctx, cfg.Observability.Tracing, config.ServiceIngest)
	if err != nil {
		return fmt.Errorf("tracing: %w", err)
	}
	lc.Add("tracing", tr.Shutdown)

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
	producer.WithTracing(tr.Provider(), tr.Propagator())
	lc.Add("kafka", producer.Close)
	telemetry.RegisterProducerBuffer(reg, producer.BufferedRecords, cfg.Kafka.MaxBufferedRecords)

	if err = producer.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}

	draining := &opshttp.Draining{}
	opsServer, err = opshttp.Start(cfg.Ops, draining, []opshttp.Check{
		{Name: "postgres", Fn: db.Pool.Ping},
		{Name: "redis", Fn: func(checkCtx context.Context) error { return client.Ping(checkCtx).Err() }},
		{Name: "kafka", Fn: producer.Ping},
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

	verifier := apikeys.NewVerifier(
		apikeys.NewStore(db.Pool), cfg.Ingest.APIKeyCacheCapacity, cfg.Ingest.APIKeyCacheTTL, nil,
	)
	e := echo.New()
	middleware.Apply(e, &log, cfg.Server.CORSAllowedOrigins)
	ingest.NewHandler(verifier, producer, ratelimit.New(client, cfg.RateLimit, log), cfg, log).
		WithMetrics(ingestMetrics).Register(e)

	server := httpx.New(cfg.Server, tracedHandler(e, tr))
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
	lc.Add("draining", opshttp.DrainCloser(draining, cfg.Ops.DrainHold))

	return nil
}

// tracedHandler wraps the public Echo router with the ingest server span.
// The endpoint is public: a client traceparent never decides sampling; it is
// kept only as a link on a new root. Ops endpoints are on a separate mux and
// are not traced.
func tracedHandler(e *echo.Echo, tr *telemetry.Tracing) http.Handler {
	return otelhttp.NewHandler(e, "ingest",
		otelhttp.WithTracerProvider(tr.Provider()),
		otelhttp.WithPropagators(tr.Propagator()),
		otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()),
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
	)
}
