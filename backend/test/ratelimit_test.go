//go:build integration

package test_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/ingest"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/ratelimit"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

const rateLimitTestKey = "snt_aaaaaaaaaaaa_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func rateLimitConfig(capacity int, refill float64, failMode string) config.RateLimitConfig {
	return config.RateLimitConfig{
		Capacity: capacity, RefillRate: refill, Timeout: 250 * time.Millisecond, FailMode: failMode,
	}
}

func newRateLimiter(t *testing.T, redisConfig config.RedisConfig, cfg config.RateLimitConfig, log zerolog.Logger) (*ratelimit.Limiter, *redis.Client) {
	t.Helper()
	client, err := redisx.New(t.Context(), redisConfig)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	return ratelimit.New(client, cfg, log), client
}

func TestRateLimitTokenBucket(t *testing.T) {
	limiter, client := newRateLimiter(t, redisCfg, rateLimitConfig(500, 500, "closed"), zerolog.Nop())
	require.NoError(t, client.FlushDB(t.Context()).Err())
	require.NoError(t, client.ScriptFlush(t.Context()).Err())

	allowed, retryAfter, err := limiter.Allow(t.Context(), "aaaaaaaaaaaa", 400)
	require.NoError(t, err)
	require.True(t, allowed, "cold script must fall back from NOSCRIPT and charge cost N")
	require.Zero(t, retryAfter)

	allowed, retryAfter, err = limiter.Allow(t.Context(), "aaaaaaaaaaaa", 101)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, 1, retryAfter)

	require.Eventually(t, func() bool {
		ok, _, allowErr := limiter.Allow(t.Context(), "aaaaaaaaaaaa", 101)
		return allowErr == nil && ok
	}, time.Second, 25*time.Millisecond, "Redis TIME refill should make the request admissible")

	allowed, _, err = limiter.Allow(t.Context(), "bbbbbbbbbbbb", 500)
	require.NoError(t, err)
	require.True(t, allowed, "public key IDs need isolated buckets")

	ttl, err := client.PTTL(t.Context(), "rl:aaaaaaaaaaaa").Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, 2*time.Second)
}

func TestRateLimitConcurrent(t *testing.T) {
	limiter, client := newRateLimiter(t, redisCfg, rateLimitConfig(500, 0.001, "closed"), zerolog.Nop())
	require.NoError(t, client.Del(t.Context(), "rl:concurrent01").Err())

	var admitted atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 700)
	for range 700 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, _, err := limiter.Allow(t.Context(), "concurrent01", 1)
			if err != nil {
				errs <- err
				return
			}
			if allowed {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int64(500), admitted.Load(), "aggregate concurrent cost must never exceed available tokens")
}

type rateLimitVerifier struct{}

func (rateLimitVerifier) Authenticate(context.Context, string) (apikeys.Principal, error) {
	return apikeys.Principal{KeyID: "aaaaaaaaaaaa", TenantID: "demo", Scopes: []string{apikeys.ScopeWrite}}, nil
}

type countingPublisher struct {
	inner kafkax.Publisher
	calls atomic.Int64
}

func (p *countingPublisher) PublishBatch(ctx context.Context, events []*contracts.Event, requestID string) error {
	p.calls.Add(1)

	return p.inner.PublishBatch(ctx, events, requestID)
}

type rateLimitHTTPFixture struct {
	server    *httptest.Server
	consumer  *kgo.Client
	publisher *countingPublisher
	limiter   *ratelimit.Limiter
}

func newRateLimitHTTPFixture(
	t *testing.T, redisConfig config.RedisConfig, limiterConfig config.RateLimitConfig, logger zerolog.Logger,
) *rateLimitHTTPFixture {
	t.Helper()
	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "ratelimit")
	cfg := &config.Config{
		Kafka: config.KafkaConfig{
			Brokers: kafkaCfg.Brokers, EventsTopic: topic, ProduceTimeout: 10 * time.Second,
			RecordRetries: 5, MaxBufferedRecords: 1000, MaxBufferedBytes: 1 << 20,
		},
		Ingest: config.IngestConfig{MaxBatchSize: 500, MaxBodyBytes: 1 << 20, MaxInFlight: 4},
		Contracts: config.ContractsConfig{
			MaxAttrKeys: 32, MaxAttrBytes: 4096, FutureSkew: 5 * time.Minute, PastSkew: 168 * time.Hour,
		},
		RateLimit: limiterConfig,
	}
	producer, err := kafkax.NewProducer(t.Context(), cfg.Kafka)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, producer.Close(context.Background())) })
	publisher := &countingPublisher{inner: producer}
	limiter, redisClient := newRateLimiter(t, redisConfig, limiterConfig, logger)
	require.NoError(t, redisClient.Del(t.Context(), "rl:aaaaaaaaaaaa").Err())
	e := echo.New()
	middleware.Apply(e, &logger, nil)
	ingest.NewHandler(rateLimitVerifier{}, publisher, limiter, cfg, logger).Register(e)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)
	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(kafkaCfg.Brokers), kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	return &rateLimitHTTPFixture{server: server, consumer: consumer, publisher: publisher, limiter: limiter}
}

func startDedicatedRedis(t *testing.T) (config.RedisConfig, *sentineltesting.FaultContainer) {
	t.Helper()
	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "redis:8.10.2", ExposedPorts: []string{"6379/tcp"},
			WaitingFor: wait.ForLog("Ready to accept connections").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	handle := &sentineltesting.FaultContainer{Container: container}
	t.Cleanup(handle.Close)
	host, err := container.Host(t.Context())
	require.NoError(t, err)
	port, err := container.MappedPort(t.Context(), "6379/tcp")
	require.NoError(t, err)

	return config.RedisConfig{
		Address: net.JoinHostPort(host, port.Port()), DialTimeout: 100 * time.Millisecond,
		ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond, MaxRetries: 1,
	}, handle
}

func pauseRedis(t *testing.T, container *sentineltesting.FaultContainer) {
	t.Helper()
	require.NoError(t, container.Pause(t.Context()))
	paused := true
	t.Cleanup(func() {
		if paused {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			require.NoError(t, container.Unpause(ctx))
		}
	})
}

func postRateLimitedRaw(t *testing.T, fixture *rateLimitHTTPFixture, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, fixture.server.URL+"/api/v1/events:batch", bytes.NewReader(body),
	)
	require.NoError(t, err)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("X-Api-Key", rateLimitTestKey)
	response, err := (&http.Client{Timeout: time.Second}).Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })

	return response
}

func postRateLimited(t *testing.T, fixture *rateLimitHTTPFixture) *http.Response {
	t.Helper()

	return postRateLimitedRaw(t, fixture, mustBatchJSON(t, integrationEvent("demo", nil)))
}

func TestRateLimitValidationConsumesQuota(t *testing.T) {
	fixture := newRateLimitHTTPFixture(t, redisCfg, rateLimitConfig(2, 0.001, "closed"), zerolog.Nop())

	malformed := postRateLimitedRaw(t, fixture, []byte(`{"events":`))
	require.Equal(t, http.StatusBadRequest, malformed.StatusCode)

	invalid := integrationEvent("demo", nil)
	invalid["event_id"] = "not-a-uuid"
	for range 2 {
		response := postRateLimitedRaw(t, fixture, mustBatchJSON(t, invalid))
		require.Equal(t, http.StatusBadRequest, response.StatusCode)
	}

	denied := postRateLimited(t, fixture)
	require.Equal(t, http.StatusTooManyRequests, denied.StatusCode)
	require.NotEmpty(t, denied.Header.Get(echo.HeaderRetryAfter))
	require.Zero(t, fixture.publisher.calls.Load())
	requireNoRecord(t, fixture.consumer)
}

func TestRateLimitQuotaPrecedesTenantAndFields(t *testing.T) {
	fixture := newRateLimitHTTPFixture(t, redisCfg, rateLimitConfig(2, 0.001, "closed"), zerolog.Nop())
	allowed, _, err := fixture.limiter.Allow(t.Context(), "aaaaaaaaaaaa", 2)
	require.NoError(t, err)
	require.True(t, allowed)

	invalid := integrationEvent("demo", nil)
	invalid["event_id"] = "not-a-uuid"
	mismatch := integrationEvent("other", nil)
	response := postRateLimitedRaw(t, fixture, mustBatchJSON(t, invalid, mismatch))
	require.Equal(t, http.StatusTooManyRequests, response.StatusCode)
	require.Zero(t, fixture.publisher.calls.Load())
	requireNoRecord(t, fixture.consumer)
}

func TestRateLimitRedisDownClosed(t *testing.T) {
	redisConfig, container := startDedicatedRedis(t)
	fixture := newRateLimitHTTPFixture(t, redisConfig, rateLimitConfig(500, 100, "closed"), zerolog.Nop())
	pauseRedis(t, container)

	started := time.Now()
	response := postRateLimited(t, fixture)
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, "1", response.Header.Get(echo.HeaderRetryAfter))
	require.Less(t, time.Since(started), time.Second)
	require.Zero(t, fixture.publisher.calls.Load())
	require.Zero(t, fixture.limiter.BypassCount())
	requireNoRecord(t, fixture.consumer)
}

func TestRateLimitRedisDownOpen(t *testing.T) {
	redisConfig, container := startDedicatedRedis(t)
	var logs bytes.Buffer
	fixture := newRateLimitHTTPFixture(t, redisConfig, rateLimitConfig(500, 100, "open"), zerolog.New(&logs))
	pauseRedis(t, container)

	started := time.Now()
	response := postRateLimited(t, fixture)
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	require.Less(t, time.Since(started), time.Second)
	require.Equal(t, int64(1), fixture.publisher.calls.Load())
	require.Equal(t, uint64(1), fixture.limiter.BypassCount())
	require.Len(t, fetchRecords(t, fixture.consumer, 1), 1)
	require.Contains(t, logs.String(), `"event":"ratelimit_bypass"`)
	require.NotContains(t, logs.String(), rateLimitTestKey)
	require.NotContains(t, logs.String(), "aaaaaaaaaaaa")
}
