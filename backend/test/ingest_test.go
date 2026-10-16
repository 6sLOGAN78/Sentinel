//go:build integration

package test_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/6sLOGAN78/sentinel/internal/eventwriter"
	"github.com/6sLOGAN78/sentinel/internal/ingest"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/ratelimit"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

type ingestFixture struct {
	server   *httptest.Server
	producer *kafkax.Producer
	consumer *kgo.Client
	kafka    config.KafkaConfig
	key      string
}

func newIngestFixture(t *testing.T, scopes []string, wrap func(kafkax.Publisher) kafkax.Publisher) *ingestFixture {
	return newIngestFixtureWithKafka(t, scopes, wrap, kafkaCfg)
}

func newIngestFixtureWithKafka(
	t *testing.T,
	scopes []string,
	wrap func(kafkax.Publisher) kafkax.Publisher,
	kafkaBase config.KafkaConfig,
) *ingestFixture {
	t.Helper()
	logger := zerolog.Nop()
	db, err := database.New(t.Context(), pgCfg, logger)
	require.NoError(t, err)
	t.Cleanup(db.Close)

	store := apikeys.NewStore(db.Pool)
	key, err := store.Create(t.Context(), "demo", scopes, "ingest-test-"+uuid.NewString())
	require.NoError(t, err)
	verifier := apikeys.NewVerifier(store, 32, time.Second, nil)

	topic := kafkaBase.EventsTopic
	if topic == "" {
		topic = sentineltesting.RequireUniqueTopic(t, kafkaBase, "ingest")
	}
	kafkaSettings := kafkaBase
	kafkaSettings.EventsTopic = topic
	if kafkaSettings.ProduceTimeout == 0 {
		kafkaSettings.ProduceTimeout = 10 * time.Second
	}
	if kafkaSettings.RecordRetries == 0 {
		kafkaSettings.RecordRetries = 5
	}
	if kafkaSettings.MaxBufferedRecords == 0 {
		kafkaSettings.MaxBufferedRecords = 50_000
	}
	if kafkaSettings.MaxBufferedBytes == 0 {
		kafkaSettings.MaxBufferedBytes = 64 << 20
	}
	cfg := &config.Config{
		Kafka: kafkaSettings,
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
		RateLimit: config.RateLimitConfig{
			Capacity: 10_000, RefillRate: 2_000, Timeout: 250 * time.Millisecond, FailMode: "closed",
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
	redisClient, err := redisx.New(t.Context(), redisCfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	ingest.NewHandler(verifier, publisher, ratelimit.New(redisClient, cfg.RateLimit, logger), cfg, logger).Register(e)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(kafkaBase.Brokers),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	return &ingestFixture{server: server, producer: producer, consumer: consumer, kafka: cfg.Kafka, key: key}
}

func TestIngestRetryNoDuplicates(t *testing.T) {
	kafka, handle, err := sentineltesting.StartKafkaFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(handle.Close)
	kafka.ProduceTimeout = 2 * time.Second
	kafka.RecordRetries = 1
	kafka.MaxBufferedRecords = 100
	kafka.MaxBufferedBytes = 1 << 20

	barrier := filepath.Join(t.TempDir(), "publish-prefix")
	t.Setenv("SENTINEL_TEST_KAFKA_PREFIX_BARRIER", barrier)
	t.Setenv("SENTINEL_TEST_KAFKA_PREFIX_COUNT", "2")
	fixture := newIngestFixtureWithKafka(t, []string{apikeys.ScopeWrite}, nil, kafka)
	events := []map[string]any{
		integrationEvent("demo", nil), integrationEvent("demo", nil),
		integrationEvent("demo", nil), integrationEvent("demo", nil),
	}
	body := mustBatchJSON(t, events...)

	type result struct {
		response *http.Response
		err      error
	}
	firstDone := make(chan result, 1)
	go func() {
		response, postErr := doPostIngestRaw(t.Context(), fixture, fixture.key, body)
		firstDone <- result{response: response, err: postErr}
	}()
	requireFileEventually(t, barrier+".reached")
	require.NoError(t, handle.Pause(t.Context()))
	paused := true
	t.Cleanup(func() {
		if paused {
			_ = handle.Unpause(context.Background())
		}
	})
	require.NoError(t, os.WriteFile(barrier+".release", []byte("release"), 0o600))
	first := <-firstDone
	require.NoError(t, first.err)
	require.Equal(t, http.StatusServiceUnavailable, first.response.StatusCode)
	require.NoError(t, first.response.Body.Close())

	require.NoError(t, handle.Unpause(t.Context()))
	paused = false
	requireKafkaProduceEventually(t, kafka)
	retryFixture := newIngestFixtureWithKafka(t, []string{apikeys.ScopeWrite}, nil, fixture.kafka)
	retry := postIngestRaw(t, retryFixture, retryFixture.key, body)
	require.Equal(t, http.StatusAccepted, retry.StatusCode)
	var accepted struct {
		EventIDs []string `json:"event_ids"`
	}
	require.NoError(t, json.NewDecoder(retry.Body).Decode(&accepted))
	wantIDs := make([]string, 0, len(events))
	for _, event := range events {
		wantIDs = append(wantIDs, event["event_id"].(string))
	}
	require.Equal(t, wantIDs, accepted.EventIDs)

	records := fetchRecords(t, fixture.consumer, len(events)+2)
	seen := make(map[string]int, len(events))
	for _, record := range records {
		var event map[string]any
		require.NoError(t, json.Unmarshal(record.Value, &event))
		seen[event["event_id"].(string)]++
	}
	require.Len(t, seen, len(events))
	for index, id := range wantIDs {
		if index < 2 {
			require.Equal(t, 2, seen[id])
		} else {
			require.Equal(t, 1, seen[id])
		}
	}

	store, conn := newDedupeStore(t)
	writerKafka := fixture.kafka
	writerKafka.EventWriterGroup = "ingest-retry-" + uuid.NewString()
	settings := integrationWriterSettings()
	settings.BatchSize = len(records)
	consumer, err := kafkax.NewConsumer(t.Context(), writerKafka, settings)
	require.NoError(t, err)
	t.Cleanup(func() { closeWriterConsumer(t, consumer) })
	writerCtx, cancelWriter := context.WithCancel(context.Background())
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- eventwriter.New(
			consumer, store, unusedDLQ{}, settings, contracts.DefaultLimits(), zerolog.Nop(),
		).Run(writerCtx)
	}()
	waitCommittedOffset(t, writerKafka, writerKafka.EventWriterGroup, writerKafka.EventsTopic, int64(len(records)))
	cancelWriter()
	require.NoError(t, <-writerDone)
	var physical uint64
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count() FROM events").Scan(&physical))
	require.Equal(t, uint64(len(records)), physical)
	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(len(events)), unique)
	for _, id := range wantIDs {
		stored, getErr := store.Get(t.Context(), "demo", id)
		require.NoError(t, getErr, id)
		require.Equal(t, id, stored.EventID)
	}
}

func TestIngestKafkaTimeout503(t *testing.T) {
	kafka, handle, err := sentineltesting.StartKafkaFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(handle.Close)
	kafka.ProduceTimeout = time.Second
	kafka.RecordRetries = 1
	kafka.MaxBufferedRecords = 100
	kafka.MaxBufferedBytes = 1 << 20
	fixture := newIngestFixtureWithKafka(t, []string{apikeys.ScopeWrite}, nil, kafka)

	require.NoError(t, handle.Pause(t.Context()))
	paused := true
	t.Cleanup(func() {
		if paused {
			_ = handle.Unpause(context.Background())
		}
	})
	started := time.Now()
	response := postIngest(t, fixture, fixture.key, integrationEvent("demo", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, "1", response.Header.Get("Retry-After"))
	require.Less(t, time.Since(started), 5*time.Second)
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&envelope))
	require.Equal(t, "SERVICE_UNAVAILABLE", envelope.Error.Code)
	require.NotContains(t, envelope.Error.Message, kafka.Brokers)

	require.NoError(t, handle.Unpause(t.Context()))
	paused = false
	require.Eventually(t, func() bool {
		return fixture.producer.Ping(t.Context()) == nil
	}, 20*time.Second, 100*time.Millisecond)
}

func requireKafkaProduceEventually(t *testing.T, kafka config.KafkaConfig) {
	t.Helper()
	probeTopic := sentineltesting.RequireUniqueTopic(t, kafka, "recovery-probe")
	client, err := kgo.NewClient(
		kgo.SeedBrokers(kafka.Brokers),
		kgo.DefaultProduceTopic(probeTopic),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RecordDeliveryTimeout(2*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	require.Eventually(t, func() bool {
		return client.ProduceSync(t.Context(), &kgo.Record{Value: []byte("ready")}).FirstErr() == nil
	}, 20*time.Second, 100*time.Millisecond, "Kafka did not recover real publication")
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

func doPostIngest(
	ctx context.Context, fixture *ingestFixture, key string, events ...map[string]any,
) (*http.Response, error) {
	body, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, fixture.server.URL+"/api/v1/events:batch", bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	client := &http.Client{Timeout: 15 * time.Second}

	return client.Do(req)
}

func doPostIngestRaw(
	ctx context.Context, fixture *ingestFixture, key string, body []byte,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, fixture.server.URL+"/api/v1/events:batch", bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}

	return (&http.Client{Timeout: 15 * time.Second}).Do(req)
}

func postIngestRaw(t *testing.T, fixture *ingestFixture, key string, body []byte) *http.Response {
	t.Helper()
	response, err := doPostIngestRaw(t.Context(), fixture, key, body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })

	return response
}

func postIngest(t *testing.T, fixture *ingestFixture, key string, events ...map[string]any) *http.Response {
	t.Helper()
	response, err := doPostIngest(t.Context(), fixture, key, events...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })

	return response
}

func fetchRecords(t *testing.T, consumer *kgo.Client, count int) []*kgo.Record {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	records := make([]*kgo.Record, 0, count)
	for len(records) < count {
		fetches := consumer.PollRecords(ctx, count-len(records))
		require.NoError(t, fetches.Err())
		fetches.EachRecord(func(record *kgo.Record) { records = append(records, record) })
	}
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

func TestIngestStatusCodes(t *testing.T) {
	oversized := []byte(`{"events":["` + strings.Repeat("x", (1<<20)+1) + `"]}`)
	malformed := []byte(`{"events":`)

	t.Run("invalid key precedes oversized body", func(t *testing.T) {
		fixture := newIngestFixture(t, []string{apikeys.ScopeWrite}, nil)
		response := postIngestRaw(t, fixture, "invalid", oversized)
		require.Equal(t, http.StatusUnauthorized, response.StatusCode)
		requireNoRecord(t, fixture.consumer)
	})

	for _, tc := range []struct {
		name string
		body []byte
	}{
		{name: "oversized body", body: oversized},
		{name: "malformed body", body: malformed},
	} {
		t.Run("missing scope precedes "+tc.name, func(t *testing.T) {
			fixture := newIngestFixture(t, []string{apikeys.ScopeRead}, nil)
			response := postIngestRaw(t, fixture, fixture.key, tc.body)
			require.Equal(t, http.StatusForbidden, response.StatusCode)
			requireNoRecord(t, fixture.consumer)
		})
	}

	t.Run("valid scope reports oversized body", func(t *testing.T) {
		fixture := newIngestFixture(t, []string{apikeys.ScopeWrite}, nil)
		response := postIngestRaw(t, fixture, fixture.key, oversized)
		require.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode)
		requireNoRecord(t, fixture.consumer)
	})
}

func TestIngestInvalidBatch(t *testing.T) {
	valid := integrationEvent("demo", nil)
	invalid := integrationEvent("demo", nil)
	invalid["event_id"] = "not-a-uuid"
	mismatch := integrationEvent("other", nil)

	tooMany := make([]map[string]any, 501)
	for i := range tooMany {
		tooMany[i] = integrationEvent("demo", nil)
	}
	tooManyBody, err := json.Marshal(map[string]any{"events": tooMany})
	require.NoError(t, err)

	cases := []struct {
		name    string
		body    []byte
		status  int
		details bool
	}{
		{name: "malformed envelope", body: []byte(`{"events":`), status: http.StatusBadRequest},
		{
			name:   "duplicate envelope field",
			body:   []byte(`{"events":[],"events":[` + string(mustJSON(t, valid)) + `]}`),
			status: http.StatusBadRequest,
		},
		{name: "case wrong envelope", body: []byte(`{"Events":[]}`), status: http.StatusBadRequest},
		{name: "unknown envelope field", body: []byte(`{"events":[],"extra":true}`), status: http.StatusBadRequest},
		{name: "trailing envelope", body: []byte(`{"events":[]} true`), status: http.StatusBadRequest},
		{name: "empty events", body: []byte(`{"events":[]}`), status: http.StatusBadRequest},
		{name: "null events", body: []byte(`{"events":null}`), status: http.StatusBadRequest},
		{name: "too many events", body: tooManyBody, status: http.StatusRequestEntityTooLarge},
		{name: "tenant mismatch beats field error", body: mustBatchJSON(t, invalid, mismatch), status: http.StatusForbidden},
		{
			name: "indexed validation detail", body: mustBatchJSON(t, valid, invalid),
			status: http.StatusBadRequest, details: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newIngestFixture(t, []string{apikeys.ScopeWrite}, nil)
			response := postIngestRaw(t, fixture, fixture.key, tc.body)
			require.Equal(t, tc.status, response.StatusCode)
			if tc.details {
				var envelope struct {
					Error struct {
						Details []struct {
							Field string `json:"field"`
							Error string `json:"error"`
						} `json:"details"`
					} `json:"error"`
				}
				require.NoError(t, json.NewDecoder(response.Body).Decode(&envelope))
				require.NotEmpty(t, envelope.Error.Details)
				require.LessOrEqual(t, len(envelope.Error.Details), 50)
				require.Equal(t, "events[1].event_id", envelope.Error.Details[0].Field)
			}
			requireNoRecord(t, fixture.consumer)
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)

	return raw
}

func mustBatchJSON(t *testing.T, events ...map[string]any) []byte {
	t.Helper()

	return mustJSON(t, map[string]any{"events": events})
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

	type postResult struct {
		response *http.Response
		err      error
	}
	firstEvent := integrationEvent("demo", nil)
	firstDone := make(chan postResult, 1)
	go func() {
		response, err := doPostIngest(t.Context(), fixture, fixture.key, firstEvent)
		firstDone <- postResult{response: response, err: err}
	}()
	select {
	case <-gate.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first publish did not enter gate")
	}

	response := postIngest(t, fixture, fixture.key, integrationEvent("demo", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, "1", response.Header.Get(echo.HeaderRetryAfter))
	close(gate.release)
	first := <-firstDone
	require.NoError(t, first.err)
	t.Cleanup(func() { _ = first.response.Body.Close() })
	require.Equal(t, http.StatusAccepted, first.response.StatusCode)

	records := fetchRecords(t, fixture.consumer, 1)
	require.Len(t, records, 1)
	requireNoRecord(t, fixture.consumer)
}
