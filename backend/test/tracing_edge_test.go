//go:build integration

package test_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/ingest"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/ratelimit"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

// newTracedIngestFixture builds the ingest Echo stack against the suite's real
// PostgreSQL, Redis and Kafka, wired for tracing exactly like cmd/ingest:
// a traced producer and the public-endpoint otelhttp handler.
func newTracedIngestFixture(t *testing.T, tr *telemetry.Tracing) *ingestFixture {
	t.Helper()
	logger := zerolog.Nop()
	db, err := database.New(t.Context(), pgCfg, logger)
	require.NoError(t, err)
	t.Cleanup(db.Close)

	store := apikeys.NewStore(db.Pool)
	key, err := store.Create(t.Context(), "demo", []string{apikeys.ScopeWrite}, "trace-test-"+uuid.NewString())
	require.NoError(t, err)

	kafkaSettings := kafkaCfg
	kafkaSettings.EventsTopic = sentineltesting.RequireUniqueTopic(t, kafkaCfg, "trace")
	kafkaSettings.ProduceTimeout = 10 * time.Second
	kafkaSettings.RecordRetries = 5
	kafkaSettings.MaxBufferedRecords = 50_000
	kafkaSettings.MaxBufferedBytes = 64 << 20
	cfg := &config.Config{
		Kafka:  kafkaSettings,
		Ingest: config.IngestConfig{MaxBatchSize: 500, MaxBodyBytes: 1 << 20, MaxInFlight: 64},
		Contracts: config.ContractsConfig{
			MaxAttrKeys: 32, MaxAttrBytes: 4096, FutureSkew: 5 * time.Minute, PastSkew: 168 * time.Hour,
		},
		RateLimit: config.RateLimitConfig{
			Capacity: 10_000, RefillRate: 2_000, Timeout: 250 * time.Millisecond, FailMode: "closed",
		},
	}

	producer, err := kafkax.NewProducer(t.Context(), cfg.Kafka)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, producer.Close(context.Background())) })
	producer.WithTracing(tr.Provider(), tr.Propagator())

	redisClient, err := redisx.New(t.Context(), redisCfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })

	e := echo.New()
	middleware.Apply(e, &logger, nil)
	ingest.NewHandler(apikeys.NewVerifier(store, 32, time.Second, nil), producer,
		ratelimit.New(redisClient, cfg.RateLimit, logger), cfg, logger).Register(e)
	// Same options as cmd/ingest tracedHandler.
	server := httptest.NewServer(otelhttp.NewHandler(e, "ingest",
		otelhttp.WithTracerProvider(tr.Provider()),
		otelhttp.WithPropagators(tr.Propagator()),
		otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()),
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
	))
	t.Cleanup(server.Close)

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(kafkaCfg.Brokers),
		kgo.ConsumeTopics(kafkaSettings.EventsTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	return &ingestFixture{server: server, producer: producer, consumer: consumer, kafka: cfg.Kafka, key: key}
}

func TestTracingEdgePropagation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ratio float64
		flags string
	}{
		{name: "sampled", ratio: 1, flags: "01"},
		{name: "unsampled", ratio: 0, flags: "00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tr, err := telemetry.NewTracing(t.Context(),
				config.TracingConfig{Enabled: true, SampleRatio: tc.ratio, OTLPEndpoint: "http://127.0.0.1:4318"},
				config.ServiceIngest, telemetry.WithExporter(exporter))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tr.Shutdown(context.Background())) })

			fixture := newTracedIngestFixture(t, tr)
			source := "192.0.2.41"
			response := postIngest(t, fixture, fixture.key,
				integrationEvent("demo", &source), integrationEvent("demo", nil), integrationEvent("demo", &source))
			require.Equal(t, http.StatusAccepted, response.StatusCode)

			records := fetchRecords(t, fixture.consumer, 3)
			// Close waits for in-flight requests, so the server span has ended.
			fixture.server.Close()
			require.NoError(t, tr.ForceFlush(t.Context()))

			traceparent := kafkaHeader(records[0].Headers, "traceparent")
			require.Regexp(t, `^00-[0-9a-f]{32}-[0-9a-f]{16}-`+tc.flags+`$`, traceparent)
			for _, record := range records {
				require.Equal(t, traceparent, kafkaHeader(record.Headers, "traceparent"))
				require.Equal(t, "1", kafkaHeader(record.Headers, "schema_version"))
				require.NotEmpty(t, kafkaHeader(record.Headers, "request_id"))
			}

			spans := exporter.GetSpans()
			if tc.ratio == 0 {
				require.Empty(t, spans, "ratio 0 exports nothing")

				return
			}

			byName := map[string]tracetest.SpanStub{}
			for _, span := range spans {
				byName[span.Name] = span
			}
			require.Len(t, byName, 2, "exactly the server span and the publish span")
			server, ok := byName["POST /api/v1/events:batch"]
			require.True(t, ok, "server span named by route template")
			publish, ok := byName["publish"]
			require.True(t, ok)

			parts := strings.Split(traceparent, "-")
			require.Equal(t, publish.SpanContext.TraceID().String(), parts[1])
			require.Equal(t, publish.SpanContext.SpanID().String(), parts[2])
			require.Equal(t, server.SpanContext.TraceID(), publish.SpanContext.TraceID())
			require.Equal(t, server.SpanContext.SpanID(), publish.Parent.SpanID())
		})
	}
}
