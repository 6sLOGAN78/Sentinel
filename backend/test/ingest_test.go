//go:build integration

package test_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/ingest"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

type ingestFixture struct {
	server   *httptest.Server
	producer *kafkax.Producer
	consumer *kgo.Client
	key      string
}

func newIngestFixture(t *testing.T, scopes []string, wrap func(kafkax.Publisher) kafkax.Publisher) *ingestFixture {
	t.Helper()
	logger := zerolog.Nop()
	db, err := database.New(t.Context(), pgCfg, logger)
	require.NoError(t, err)
	t.Cleanup(db.Close)

	store := apikeys.NewStore(db.Pool)
	key, err := store.Create(t.Context(), "demo", scopes, "ingest-test-"+uuid.NewString())
	require.NoError(t, err)
	verifier := apikeys.NewVerifier(store, 32, time.Second, nil)

	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "ingest")
	cfg := &config.Config{
		Kafka: config.KafkaConfig{
			Brokers:            kafkaCfg.Brokers,
			EventsTopic:        topic,
			ProduceTimeout:     10 * time.Second,
			RecordRetries:      5,
			MaxBufferedRecords: 50_000,
			MaxBufferedBytes:   64 << 20,
		},
		Ingest: config.IngestConfig{
			MaxBatchSize: 500,
			MaxBodyBytes: 1 << 20,
			MaxInFlight:  64,
		},
		Contracts: config.ContractsConfig{
			MaxAttrKeys:  32,
			MaxAttrBytes: 4096,
			FutureSkew:   5 * time.Minute,
			PastSkew:     168 * time.Hour,
		},
	}
	if wrap != nil {
		cfg.Ingest.MaxInFlight = 1
	}
	producer, err := kafkax.NewProducer(t.Context(), cfg.Kafka)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, producer.Close(context.Background())) })

	publisher := kafkax.Publisher(producer)
	if wrap != nil {
		publisher = wrap(publisher)
	}
	e := echo.New()
	middleware.Apply(e, &logger, nil)
	ingest.NewHandler(verifier, publisher, cfg, logger).Register(e)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(kafkaCfg.Brokers),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	return &ingestFixture{server: server, producer: producer, consumer: consumer, key: key}
}

func integrationEvent(tenant string, sourceIP *string) map[string]any {
	return map[string]any{
		"schema_version": 1,
		"event_id":       uuid.NewString(),
		"tenant_id":      tenant,
		"occurred_at":    time.Now().UTC().Add(-time.Second).Format("2006-01-02T15:04:05.000Z"),
		"source":         "integration-test",
		"event_type":     "net.connection",
		"source_ip":      sourceIP,
	}
}

func postIngest(t *testing.T, fixture *ingestFixture, key string, events ...map[string]any) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{"events": events})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fixture.server.URL+"/api/v1/events:batch", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })

	return response
}

func fetchRecords(t *testing.T, consumer *kgo.Client, count int) []*kgo.Record {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fetches := consumer.PollRecords(ctx, count)
	require.NoError(t, fetches.Err())
	records := make([]*kgo.Record, 0, count)
	fetches.EachRecord(func(record *kgo.Record) { records = append(records, record) })
	require.Len(t, records, count)

	return records
}

func kafkaHeader(headers []kgo.RecordHeader, key string) string {
	for _, header := range headers {
		if header.Key == key {
			return string(header.Value)
		}
	}

	return ""
}

func requireNoRecord(t *testing.T, consumer *kgo.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	fetches := consumer.PollRecords(ctx, 1)
	count := 0
	fetches.EachRecord(func(*kgo.Record) { count++ })
	require.Zero(t, count)
}

func TestIngestAcceptedAfterAck(t *testing.T) {
	fixture := newIngestFixture(t, []string{apikeys.ScopeWrite}, nil)
	source := "192.0.2.40"
	first := integrationEvent("demo", &source)
	second := integrationEvent("demo", nil)

	response := postIngest(t, fixture, fixture.key, first, second)
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	var body struct {
		AcceptedCount int      `json:"accepted_count"`
		RejectedCount int      `json:"rejected_count"`
		EventIDs      []string `json:"event_ids"`
		RequestID     string   `json:"request_id"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Equal(t, 2, body.AcceptedCount)
	require.Zero(t, body.RejectedCount)
	require.Equal(t, []string{first["event_id"].(string), second["event_id"].(string)}, body.EventIDs)
	require.NotEmpty(t, body.RequestID)

	records := fetchRecords(t, fixture.consumer, 2)
	require.Equal(t, []byte("demo|192.0.2.40"), records[0].Key)
	require.Equal(t, []byte("demo|"), records[1].Key)
	for _, record := range records {
		require.Equal(t, "1", kafkaHeader(record.Headers, "schema_version"))
		require.Equal(t, body.RequestID, kafkaHeader(record.Headers, "request_id"))
	}
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(records[1].Value, &decoded))
	value, exists := decoded["source_ip"]
	require.True(t, exists)
	require.Nil(t, value)
}

func TestIngestAuth(t *testing.T) {
	t.Run("missing write scope", func(t *testing.T) {
		fixture := newIngestFixture(t, []string{apikeys.ScopeRead}, nil)
		response := postIngest(t, fixture, fixture.key, integrationEvent("demo", nil))
		require.Equal(t, http.StatusForbidden, response.StatusCode)
		requireNoRecord(t, fixture.consumer)
	})

	t.Run("invalid credential", func(t *testing.T) {
		fixture := newIngestFixture(t, []string{apikeys.ScopeWrite}, nil)
		response := postIngest(t, fixture, "invalid", integrationEvent("demo", nil))
		require.Equal(t, http.StatusUnauthorized, response.StatusCode)
		requireNoRecord(t, fixture.consumer)
	})

	t.Run("tenant mismatch", func(t *testing.T) {
		fixture := newIngestFixture(t, []string{apikeys.ScopeWrite}, nil)
		response := postIngest(t, fixture, fixture.key, integrationEvent("other", nil))
		require.Equal(t, http.StatusForbidden, response.StatusCode)
		requireNoRecord(t, fixture.consumer)
	})
}

type blockedPublisher struct {
	inner   kafkax.Publisher
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockedPublisher) PublishBatch(ctx context.Context, events []*contracts.Event, requestID string) error {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return p.inner.PublishBatch(ctx, events, requestID)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestIngestShedsUnderLoad(t *testing.T) {
	gate := &blockedPublisher{entered: make(chan struct{}), release: make(chan struct{})}
	fixture := newIngestFixture(t, []string{apikeys.ScopeWrite}, func(inner kafkax.Publisher) kafkax.Publisher {
		gate.inner = inner

		return gate
	})

	firstDone := make(chan *http.Response, 1)
	go func() { firstDone <- postIngest(t, fixture, fixture.key, integrationEvent("demo", nil)) }()
	select {
	case <-gate.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first publish did not enter gate")
	}

	response := postIngest(t, fixture, fixture.key, integrationEvent("demo", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, "1", response.Header.Get(echo.HeaderRetryAfter))
	close(gate.release)
	require.Equal(t, http.StatusAccepted, (<-firstDone).StatusCode)

	records := fetchRecords(t, fixture.consumer, 1)
	require.Len(t, records, 1)
	requireNoRecord(t, fixture.consumer)
}
