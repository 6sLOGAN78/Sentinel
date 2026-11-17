//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/detector"
	"github.com/6sLOGAN78/sentinel/internal/eventwriter"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

// pipelineTracing gives every service its own provider (and service.name)
// over one shared in-memory exporter, at ratio 1.
func pipelineTracing(t *testing.T, exporter *tracetest.InMemoryExporter, service string) *telemetry.Tracing {
	t.Helper()
	tr, err := telemetry.NewTracing(t.Context(),
		config.TracingConfig{Enabled: true, SampleRatio: 1, OTLPEndpoint: "http://127.0.0.1:4318"},
		service, telemetry.WithExporter(exporter))
	require.NoError(t, err)
	// InMemoryExporter.Shutdown resets its store, so spans are read first.
	t.Cleanup(func() { require.NoError(t, tr.Shutdown(context.Background())) })

	return tr
}

// runPipelineWorker runs fn until the returned stop is called and reports its
// error; stop waits so every span the worker started has ended.
func runPipelineWorker(t *testing.T, name string, fn func(context.Context) error) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err, "%s stopped with an error", name)
		case <-time.After(30 * time.Second):
			t.Fatalf("%s did not stop within 30s", name)
		}
	}
	t.Cleanup(stop)

	return stop
}

func authEvent(at time.Time, eventType, ip string, principal *string) map[string]any {
	return map[string]any{
		"schema_version": 1,
		"event_id":       uuid.NewString(),
		"tenant_id":      "demo",
		"occurred_at":    at.UTC().Format("2006-01-02T15:04:05.000Z"),
		"source":         "tracing-pipeline",
		"event_type":     eventType,
		"source_ip":      ip,
		"principal":      principal,
	}
}

// TestTracingPipelinePropagation proves one sampled ingest request is traced
// ingest -> publish -> detect -> store_alert through Kafka headers on real
// Kafka, ClickHouse and PostgreSQL, and that the event-writer store span links
// the same trace.
func TestTracingPipelinePropagation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	exporter := tracetest.NewInMemoryExporter()
	ingestTracing := pipelineTracing(t, exporter, config.ServiceIngest)
	detectorTracing := pipelineTracing(t, exporter, config.ServiceDetector)
	eventTracing := pipelineTracing(t, exporter, config.ServiceEventWriter)
	alertTracing := pipelineTracing(t, exporter, config.ServiceAlertWriter)

	fixture := newTracedIngestFixture(t, ingestTracing)
	kafka := fixture.kafka
	kafka.AlertsTopic = sentineltesting.RequireUniqueTopic(t, kafkaCfg, "trace-alerts")
	kafka.DetectorGroup = "trace-detector-" + uuid.NewString()
	kafka.EventWriterGroup = "trace-events-" + uuid.NewString()
	kafka.AlertWriterGroup = "trace-alert-writer-" + uuid.NewString()
	_, conn := newRecoveryDatabase(t)
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	settings := integrationWriterSettings()
	settings.BatchSize = 500

	stopDetector := startTracedDetector(t, kafka, settings, db, detectorTracing)
	stopEvents := startTracedEventWriter(t, kafka, settings, conn, eventTracing)
	stopAlerts := startTracedAlertWriter(t, kafka, settings, conn, db, alertTracing)

	// Ten AUTH failures in one 5 min window: nine in one request, the tenth
	// (threshold-crossing) in a second, then a window-advance marker.
	start := time.Now().UTC().Truncate(5 * time.Minute).Add(-5 * time.Minute)
	ip, principal := "192.0.2.77", "trace-"+uuid.NewString()
	events := make([]map[string]any, 0, 10)
	for i := range 10 {
		events = append(events, authEvent(start.Add(time.Second+time.Duration(i)*time.Millisecond),
			"auth.failure", ip, &principal))
	}
	marker := authEvent(time.Now().UTC().Add(-time.Second), "auth.success", "192.0.2.78", nil)
	for _, batch := range [][]map[string]any{events[:9], events[9:], {marker}} {
		response := postIngest(t, fixture, fixture.key, batch...)
		require.Equal(t, http.StatusAccepted, response.StatusCode)
	}

	thresholdEvent, ok := events[9]["event_id"].(string)
	require.True(t, ok)
	thresholdTrace := thresholdTraceID(t, fixture.consumer, thresholdEvent)
	alertID := contracts.DedupeKey("demo", "AUTH_BRUTE_FORCE", []string{ip, principal}, start)
	require.Eventually(t, func() bool {
		var workflows int
		return db.Pool.QueryRow(ctx, `SELECT count(*) FROM alert_workflows WHERE alert_id=$1`,
			alertID).Scan(&workflows) == nil && workflows == 1
	}, time.Minute, 100*time.Millisecond, "alert-writer never persisted the threshold alert")
	require.Eventually(t, func() bool {
		var stored uint64
		return conn.QueryRow(ctx, `SELECT uniqExact(event_id) FROM events WHERE tenant_id={tenant:String}`,
			driverclickhouse.Named("tenant", "demo")).Scan(&stored) == nil && stored == 11
	}, time.Minute, 100*time.Millisecond, "event-writer never stored the 11 events")

	// Stopping each worker ends its spans; the server closed its request spans.
	stopAlerts()
	stopEvents()
	stopDetector()
	fixture.server.Close()
	for _, tr := range []*telemetry.Tracing{ingestTracing, detectorTracing, eventTracing, alertTracing} {
		require.NoError(t, tr.ForceFlush(ctx))
	}
	requireTraceChain(t, exporter.GetSpans(), thresholdTrace)
}

func thresholdTraceID(t *testing.T, consumer *kgo.Client, eventID string) string {
	t.Helper()
	traces := map[string]bool{}
	threshold := ""
	for _, record := range fetchRecords(t, consumer, 11) {
		parts := strings.Split(kafkaHeader(record.Headers, "traceparent"), "-")
		require.Len(t, parts, 4)
		require.Equal(t, "01", parts[3], "ratio 1 samples every request")
		traces[parts[1]] = true
		var event struct {
			EventID string `json:"event_id"`
		}
		require.NoError(t, json.Unmarshal(record.Value, &event))
		if event.EventID == eventID {
			threshold = parts[1]
		}
	}
	require.Len(t, traces, 3, "each request has its own trace")
	require.NotEmpty(t, threshold)

	return threshold
}

func requireTraceChain(t *testing.T, spans tracetest.SpanStubs, traceID string) {
	t.Helper()
	byName := map[string]tracetest.SpanStub{}
	linked := false
	for _, span := range spans {
		if span.Name == "store" {
			for _, link := range span.Links {
				linked = linked || link.SpanContext.TraceID().String() == traceID
			}
		}
		if span.SpanContext.TraceID().String() != traceID {
			continue
		}
		_, seen := byName[span.Name]
		require.False(t, seen && span.Name != "store", "duplicate %q span in the threshold trace", span.Name)
		byName[span.Name] = span
	}
	server, ok := byName["POST /api/v1/events:batch"]
	require.True(t, ok, "missing ingest server span; got %v", spanNames(spans))
	publish, ok := byName["publish"]
	require.True(t, ok, "missing publish span")
	detect, ok := byName["detect"]
	require.True(t, ok, "missing detect span")
	storeAlert, ok := byName["store_alert"]
	require.True(t, ok, "missing store_alert span")
	require.Equal(t, server.SpanContext.SpanID(), publish.Parent.SpanID())
	require.Equal(t, publish.SpanContext.SpanID(), detect.Parent.SpanID(), "detect is a child via the event header")
	require.Equal(t, detect.SpanContext.SpanID(), storeAlert.Parent.SpanID(),
		"store_alert is a child via the alert header")
	rule := ""
	for _, kv := range detect.Attributes {
		if kv.Key == "rule_id" {
			rule = kv.Value.AsString()
		}
	}
	require.Equal(t, "AUTH_BRUTE_FORCE", rule)
	require.True(t, linked, "no event-writer store span links the threshold trace")
}

func spanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name)
	}

	return names
}

func startTracedDetector(t *testing.T, kafka config.KafkaConfig, settings config.WriterConfig,
	db *database.Database, tr *telemetry.Tracing) func() {
	t.Helper()
	file, err := os.Open("../../config/rules.yaml")
	require.NoError(t, err)
	defer file.Close()
	rules, err := detection.LoadRules(file)
	require.NoError(t, err)
	consumer, err := detector.NewKafkaConsumer(t.Context(), kafka, settings, rules, func(error) {})
	require.NoError(t, err)
	t.Cleanup(func() { closeDetectorConsumer(t, consumer) })
	alerts, err := detector.NewAlertProducer(t.Context(), kafka)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, alerts.Close(context.Background())) })
	alerts.WithPropagator(tr.Propagator())
	pg := config.DetectorPGConfig{AcquireTimeout: time.Second, TransactionTimeout: 3 * time.Second,
		LockTimeout: time.Second, StatementTimeout: 2 * time.Second, CleanupTimeout: time.Second}
	worker := detector.New(consumer, alerts, rules, settings, contracts.DefaultLimits(), db.Pool, pg).
		WithTracing(tr.Provider(), tr.Propagator())

	return runPipelineWorker(t, "detector", worker.Run)
}

func startTracedEventWriter(t *testing.T, kafka config.KafkaConfig, settings config.WriterConfig,
	conn storageclickhouse.Conn, tr *telemetry.Tracing) func() {
	t.Helper()
	consumer, err := kafkax.NewConsumer(t.Context(), kafka, settings)
	require.NoError(t, err)
	t.Cleanup(func() { closeWriterConsumer(t, consumer) })
	writer := eventwriter.New(consumer, storageclickhouse.NewEventStore(conn), unusedDLQ{}, settings,
		contracts.DefaultLimits(), zerolog.Nop()).WithTracing(tr.Provider(), tr.Propagator())

	return runPipelineWorker(t, "event-writer", writer.Run)
}

func startTracedAlertWriter(t *testing.T, kafka config.KafkaConfig, settings config.WriterConfig,
	conn storageclickhouse.Conn, db *database.Database, tr *telemetry.Tracing) func() {
	t.Helper()
	consumer, err := kafkax.NewAlertConsumer(t.Context(), kafka, settings)
	require.NoError(t, err)
	t.Cleanup(func() { closeWriterConsumer(t, consumer) })
	writer := alertwriter.New(consumer, storageclickhouse.NewAlertStore(conn),
		storagepostgres.NewAlertStore(db.Pool, settings.OperationTimeout), settings, zerolog.Nop()).
		WithTracing(tr.Provider(), tr.Propagator())

	return runPipelineWorker(t, "alert-writer", writer.Run)
}
