//go:build integration

package test_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

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
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

const (
	overloadClients        = 64
	overloadDuration       = 20 * time.Second
	overloadPauseAt        = 4 * time.Second
	overloadPauseFor       = 10 * time.Second
	overloadBatch          = 500 // the ingest MaxBatchSize, so 8 in flight can exceed the buffer
	overloadMaxInFlight    = 8
	overloadBufferLimit    = 2000
	overloadBufferBytes    = 16 << 20
	overloadProduceTimeout = 3 * time.Second
	overloadCapacity       = 2000
	overloadRefill         = 2000
	overloadHeapBound      = 256 << 20
	overloadSampleEvery    = 100 * time.Millisecond
	overloadClientTimeout  = 30 * time.Second
	overloadTestBudget     = 3 * time.Minute
	overloadBufferedMetric = "sentinel_kafka_producer_buffered_records"
	overloadRequestsMetric = "sentinel_ingest_requests_total"
	overloadEventsMetric   = "sentinel_ingest_events_total"
)

// overloadPublisher forwards to the real producer and records the exact
// number of concurrent publishes (all inside the in-flight gate) plus the IDs
// of batches whose publish failed (ambiguous: they may still land).
type overloadPublisher struct {
	inner      kafkax.Publisher
	current    atomic.Int64
	peak       atomic.Int64
	bufferFull atomic.Int64
	mu         sync.Mutex
	failed     map[string]struct{}
}

// countingVerifier counts requests that passed the in-flight gate: the
// handler authenticates exactly once, right after acquiring a slot.
type countingVerifier struct {
	inner interface {
		Authenticate(ctx context.Context, key string) (apikeys.Principal, error)
	}
	calls atomic.Int64
}

func (v *countingVerifier) Authenticate(ctx context.Context, key string) (apikeys.Principal, error) {
	v.calls.Add(1)
	return v.inner.Authenticate(ctx, key)
}

func (p *overloadPublisher) PublishBatch(ctx context.Context, events []*contracts.Event, requestID string) error {
	now := p.current.Add(1)
	defer p.current.Add(-1)
	for {
		peak := p.peak.Load()
		if now <= peak || p.peak.CompareAndSwap(peak, now) {
			break
		}
	}
	err := p.inner.PublishBatch(ctx, events, requestID)
	if errors.Is(err, kafkax.ErrBufferFull) {
		p.bufferFull.Add(1) // shed before any record reached the client
		return err
	}
	if err != nil {
		p.mu.Lock()
		for _, event := range events {
			p.failed[event.EventID] = struct{}{}
		}
		p.mu.Unlock()
	}
	return err
}

type overloadFixture struct {
	server    *httptest.Server
	producer  *kafkax.Producer
	publisher *overloadPublisher
	verifier  *countingVerifier
	reg       *prometheus.Registry
	kafka     config.KafkaConfig
	handle    *sentineltesting.FaultContainer
	key       string
}

func newOverloadFixture(t *testing.T) *overloadFixture {
	t.Helper()
	kafka, handle, err := sentineltesting.StartKafkaFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(handle.Close)
	kafka.EventsTopic = sentineltesting.RequireUniqueTopic(t, kafka, "chaos-overload")
	kafka.ProduceTimeout = overloadProduceTimeout
	kafka.RecordRetries = 5
	kafka.MaxBufferedRecords = overloadBufferLimit
	kafka.MaxBufferedBytes = overloadBufferBytes
	cfg := &config.Config{
		Kafka:  kafka,
		Ingest: config.IngestConfig{MaxBatchSize: overloadBatch, MaxBodyBytes: 1 << 20, MaxInFlight: overloadMaxInFlight},
		Contracts: config.ContractsConfig{
			MaxAttrKeys: 32, MaxAttrBytes: 4096, FutureSkew: 5 * time.Minute, PastSkew: 168 * time.Hour,
		},
		RateLimit: config.RateLimitConfig{
			Capacity: overloadCapacity, RefillRate: overloadRefill, Timeout: 250 * time.Millisecond, FailMode: "closed",
		},
	}
	logger := zerolog.Nop()
	db, err := database.New(t.Context(), pgCfg, logger)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := apikeys.NewStore(db.Pool)
	key, err := store.Create(t.Context(), "demo", []string{apikeys.ScopeWrite}, "chaos-overload-"+uuid.NewString())
	require.NoError(t, err)
	producer, err := kafkax.NewProducer(t.Context(), cfg.Kafka)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = producer.Close(ctx)
	})
	reg := prometheus.NewRegistry()
	metrics := telemetry.NewIngestMetrics(reg)
	telemetry.RegisterProducerBuffer(reg, producer.BufferedRecords, cfg.Kafka.MaxBufferedRecords)
	publisher := &overloadPublisher{inner: producer, failed: make(map[string]struct{})}
	redisClient, err := redisx.New(t.Context(), redisCfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	e := echo.New()
	middleware.Apply(e, &logger, nil)
	verifier := &countingVerifier{inner: apikeys.NewVerifier(store, 32, time.Second, nil)}
	ingest.NewHandler(verifier, publisher,
		ratelimit.New(redisClient, cfg.RateLimit, logger), cfg, logger).WithMetrics(metrics).Register(e)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)

	return &overloadFixture{server: server, producer: producer, publisher: publisher, verifier: verifier, reg: reg,
		kafka: cfg.Kafka, handle: handle, key: key}
}

// overloadBody builds one batch of fresh canonical events. IDs share a random
// base and differ in the last two bytes, so they stay unique and canonical.
func overloadBody() []byte {
	occurredAt := time.Now().UTC().Add(-time.Second).Format("2006-01-02T15:04:05.000Z")
	base := uuid.New()
	var buf bytes.Buffer
	buf.Grow(overloadBatch * 160)
	buf.WriteString(`{"events":[`)
	for i := range overloadBatch {
		id := base
		id[14], id[15] = byte(i>>8), byte(i)
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(`{"schema_version":1,"event_id":"`)
		buf.WriteString(id.String())
		buf.WriteString(`","tenant_id":"demo","occurred_at":"`)
		buf.WriteString(occurredAt)
		buf.WriteString(`","source":"chaos-overload","event_type":"net.connection"}`)
	}
	buf.WriteString(`]}`)
	return buf.Bytes()
}

// overloadTally aggregates every client-visible outcome.
type overloadTally struct {
	mu                sync.Mutex
	statuses          map[int]int
	missingRetryAfter int
	transportErrors   []string
	badAccepted       int
	accepted          map[string]struct{}
}

func (o *overloadTally) record(status int, retryAfter string, acceptedIDs []string, acceptedCount int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.statuses[status]++
	if (status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable) && retryAfter == "" {
		o.missingRetryAfter++
	}
	if status == http.StatusAccepted {
		if acceptedCount != overloadBatch || len(acceptedIDs) != overloadBatch {
			o.badAccepted++
		}
		for _, id := range acceptedIDs {
			o.accepted[id] = struct{}{}
		}
	}
}

func (o *overloadTally) fail(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.transportErrors) < 10 {
		o.transportErrors = append(o.transportErrors, err.Error())
	} else {
		o.transportErrors[len(o.transportErrors)-1] = "... and more"
	}
}

// overloadRequest sends one batch and records the outcome.
func overloadRequest(f *overloadFixture, client *http.Client, tally *overloadTally) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		f.server.URL+"/api/v1/events:batch", bytes.NewReader(overloadBody()))
	if err != nil {
		tally.fail(err)
		return
	}
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("X-Api-Key", f.key)
	response, err := client.Do(req)
	if err != nil {
		tally.fail(err)
		return
	}
	defer response.Body.Close()
	var body struct {
		AcceptedCount int      `json:"accepted_count"`
		EventIDs      []string `json:"event_ids"`
	}
	if response.StatusCode == http.StatusAccepted {
		if err = json.NewDecoder(response.Body).Decode(&body); err != nil {
			tally.fail(err)
			return
		}
	} else if _, err = io.Copy(io.Discard, response.Body); err != nil {
		tally.fail(err)
		return
	}
	tally.record(response.StatusCode, response.Header.Get(echo.HeaderRetryAfter), body.EventIDs, body.AcceptedCount)
}

// overloadSample is the running maximum of every bounded quantity.
type overloadSample struct {
	mu          sync.Mutex
	maxBuffered float64
	limit       float64
	maxHeap     uint64
	samples     int
	gatherErr   error
}

func (s *overloadSample) take(reg *prometheus.Registry) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	families, err := reg.Gather()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples++
	s.maxHeap = max(s.maxHeap, mem.HeapInuse)
	if err != nil {
		s.gatherErr = err
		return
	}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			switch family.GetName() {
			case overloadBufferedMetric:
				s.maxBuffered = max(s.maxBuffered, metric.GetGauge().GetValue())
			case overloadBufferedMetric + "_limit": // sentinel_kafka_producer_buffered_records_limit
				s.limit = metric.GetGauge().GetValue()
			}
		}
	}
}

func startOverloadSampler(reg *prometheus.Registry) (*overloadSample, func()) {
	sample := &overloadSample{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(overloadSampleEvery)
		defer ticker.Stop()
		for {
			sample.take(reg)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return sample, func() { cancel(); <-done }
}

// pauseBrokerDuringLoad pauses the dedicated fault broker for part of the run.
func pauseBrokerDuringLoad(t *testing.T, handle *sentineltesting.FaultContainer) <-chan error {
	t.Helper()
	result := make(chan error, 1)
	var paused atomic.Bool
	t.Cleanup(func() {
		if paused.Load() {
			_ = handle.Unpause(context.Background())
		}
	})
	go func() {
		time.Sleep(overloadPauseAt)
		if err := handle.Pause(context.Background()); err != nil {
			result <- err
			return
		}
		paused.Store(true)
		time.Sleep(overloadPauseFor)
		err := handle.Unpause(context.Background())
		if err == nil {
			paused.Store(false)
		}
		result <- err
	}()
	return result
}

// gatheredCounters returns one counter family keyed by its single label value.
func gatheredCounters(t *testing.T, reg *prometheus.Registry, name string) map[string]float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	values := make(map[string]float64)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := metric.GetLabel()
			require.Len(t, labels, 1)
			values[labels[0].GetValue()] = metric.GetCounter().GetValue()
		}
	}
	return values
}

func TestChaosOverloadBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), overloadTestBudget)
	defer cancel()
	f := newOverloadFixture(t)
	tally := &overloadTally{statuses: make(map[int]int), accepted: make(map[string]struct{})}
	transport := &http.Transport{MaxConnsPerHost: overloadClients, MaxIdleConnsPerHost: overloadClients}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Timeout: overloadClientTimeout, Transport: transport}

	sample, stopSampling := startOverloadSampler(f.reg)
	faults := pauseBrokerDuringLoad(t, f.handle)
	deadline := time.Now().Add(overloadDuration)
	var clients sync.WaitGroup
	for range overloadClients {
		clients.Go(func() {
			for time.Now().Before(deadline) && ctx.Err() == nil {
				overloadRequest(f, client, tally)
			}
		})
	}
	clients.Wait()
	stopSampling()
	select {
	case err := <-faults:
		require.NoError(t, err, "broker pause/unpause failed")
	case <-ctx.Done():
		t.Fatal("broker fault schedule did not finish")
	}

	tally.mu.Lock()
	statuses := maps.Clone(tally.statuses)
	tally.mu.Unlock()
	require.Empty(t, tally.transportErrors, "client transport errors")
	for status := range statuses {
		require.Contains(t, []int{http.StatusAccepted, http.StatusTooManyRequests, http.StatusServiceUnavailable},
			status, "unexpected status %d under overload", status)
	}
	for _, status := range []int{http.StatusAccepted, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		require.Positive(t, statuses[status], "status %d never observed; the run did not exercise it", status)
	}
	require.Zero(t, tally.missingRetryAfter, "429/503 without Retry-After")
	require.Zero(t, tally.badAccepted, "202 that did not accept the whole batch")

	// Bounds: producer queue (via the registry, as /metrics serves it),
	// in-flight requests (exact peak of concurrent publishes) and heap.
	require.NoError(t, sample.gatherErr)
	require.InDelta(t, float64(overloadBufferLimit), sample.limit, 0, "buffered_records_limit gauge")
	require.LessOrEqual(t, sample.maxBuffered, sample.limit, "producer buffer exceeded its limit")
	require.Greater(t, sample.maxBuffered, float64(overloadBufferLimit-overloadBatch),
		"the pause never filled the buffer to within one batch of its limit")
	require.Positive(t, f.publisher.bufferFull.Load(), "a full producer buffer never shed a batch")
	require.LessOrEqual(t, f.publisher.peak.Load(), int64(overloadMaxInFlight), "publishes exceeded max_in_flight")
	gateSheds := int64(sumCounts(statuses)) - f.verifier.calls.Load()
	require.Positive(t, gateSheds, "the max_in_flight gate never shed a request")
	require.Less(t, sample.maxHeap, uint64(overloadHeapBound), "HeapInuse exceeded the bound")

	// /metrics agrees with what clients saw.
	requests := gatheredCounters(t, f.reg, overloadRequestsMetric)
	for status, count := range statuses {
		require.InDelta(t, float64(count), requests[strconv.Itoa(status)], 0,
			"requests_total{status=%d}", status)
	}
	require.InDelta(t, float64(len(tally.accepted)),
		gatheredCounters(t, f.reg, overloadEventsMetric)[telemetry.ResultAccepted], 0)

	unique, extra := drainOverloadTopic(ctx, t, f, tally.accepted)
	t.Logf("overload: statuses=%v requests=%d gate sheds=%d buffer-full sheds=%d accepted events=%d "+
		"max buffered=%.0f/%.0f peak publishes=%d/%d max HeapInuse=%.1f MiB (bound %d MiB) samples=%d "+
		"CH unique=%d ambiguous landed=%d",
		statuses, sumCounts(statuses), gateSheds, f.publisher.bufferFull.Load(), len(tally.accepted),
		sample.maxBuffered, sample.limit, f.publisher.peak.Load(), overloadMaxInFlight,
		float64(sample.maxHeap)/(1<<20), overloadHeapBound>>20, sample.samples, unique, extra)
}

func sumCounts(statuses map[int]int) int {
	total := 0
	for _, count := range statuses {
		total += count
	}
	return total
}

// drainOverloadTopic writes the topic to ClickHouse with the real event-writer
// and proves no acknowledged event was lost. Extra stored events may only come
// from batches whose publish failed ambiguously (never acknowledged).
func drainOverloadTopic(
	ctx context.Context, t *testing.T, f *overloadFixture, accepted map[string]struct{},
) (int, int) {
	t.Helper()
	require.Eventually(t, func() bool {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return f.producer.Ping(pingCtx) == nil
	}, 30*time.Second, 250*time.Millisecond, "fault broker did not recover")
	store, conn := newDedupeStore(t)
	writerKafka := config.KafkaConfig{Brokers: f.kafka.Brokers, EventsTopic: f.kafka.EventsTopic,
		EventWriterGroup: "chaos-overload-" + uuid.NewString()}
	consumer, err := kafkax.NewConsumer(ctx, writerKafka, integrationWriterSettings())
	require.NoError(t, err)
	t.Cleanup(func() { closeWriterConsumer(t, consumer) })
	writerCtx, cancelWriter := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- eventwriter.New(consumer, store, unusedDLQ{}, integrationWriterSettings(),
			contracts.DefaultLimits(), zerolog.Nop()).Run(writerCtx)
	}()
	end := topicEndOffset(t, writerKafka, writerKafka.EventsTopic)
	require.Eventually(t, func() bool {
		got, ok := committedNextOffset(t, writerKafka, writerKafka.EventWriterGroup, writerKafka.EventsTopic)
		return ok && got == end
	}, time.Minute, 100*time.Millisecond, "writer did not commit the whole topic")
	cancelWriter()
	require.NoError(t, <-done)

	rows, err := conn.Query(ctx, "SELECT DISTINCT event_id FROM events WHERE tenant_id = 'demo'")
	require.NoError(t, err)
	stored := make(map[string]struct{})
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		stored[id] = struct{}{}
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.NotEmpty(t, stored, "nothing reached ClickHouse")
	unique, err := store.CountUnique(ctx, "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(len(stored)), unique)

	missing := 0
	for id := range accepted {
		if _, ok := stored[id]; !ok {
			missing++
		}
	}
	require.Zero(t, missing, "acknowledged events missing from ClickHouse")
	f.publisher.mu.Lock()
	defer f.publisher.mu.Unlock()
	var unexplained []string
	extra := 0
	for id := range stored {
		if _, ok := accepted[id]; ok {
			continue
		}
		extra++
		if _, ambiguous := f.publisher.failed[id]; !ambiguous {
			unexplained = append(unexplained, id)
		}
	}
	require.Empty(t, slices.Sorted(slices.Values(unexplained)), "stored events that were never published")
	if extra == 0 {
		require.Len(t, accepted, len(stored), "accepted count must equal CH unique count")
	}
	return len(stored), extra
}
