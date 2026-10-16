//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/eventwriter"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

type poisonOrigin struct {
	partition int32
	offset    int64
}

// The delay seam controls latency only. Successful effects always traverse the
// production publisher and are independently consumed from the actual DLQ.
type slowRealDLQ struct {
	inner        *kafkax.DLQPublisher
	second       chan struct{}
	mu           sync.Mutex
	started      int
	startedAt    time.Time
	acknowledged map[poisonOrigin]bool
	failFirst    bool
}

func (p *slowRealDLQ) Publish(ctx context.Context, record *kgo.Record, reason kafkax.DLQReason) error {
	p.mu.Lock()
	p.started++
	if p.started == 1 {
		p.startedAt = time.Now()
	}
	if p.started == 2 {
		close(p.second)
	}
	p.mu.Unlock()
	timer := time.NewTimer(750 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	if p.failFirst && record.Partition == 0 && record.Offset == 0 {
		return errors.New("deliberate first poison failure")
	}
	if err := p.inner.Publish(ctx, record, reason); err != nil {
		return err
	}
	p.mu.Lock()
	p.acknowledged[poisonOrigin{record.Partition, record.Offset}] = true
	p.mu.Unlock()
	return nil
}

func (p *slowRealDLQ) acknowledgedOrigins() map[poisonOrigin]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[poisonOrigin]bool, len(p.acknowledged))
	for origin := range p.acknowledged {
		out[origin] = true
	}
	return out
}

func (p *slowRealDLQ) elapsed() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Since(p.startedAt)
}

type releaseConsumer struct {
	*kafkax.Consumer
	released chan struct{}
	once     sync.Once
}

func (c *releaseConsumer) AllowRebalance() {
	c.Consumer.AllowRebalance()
	c.once.Do(func() { close(c.released) })
}

type observedDLQ struct {
	mu      sync.Mutex
	records map[poisonOrigin]*kgo.Record
	err     error
}

func startObservedDLQ(ctx context.Context, t *testing.T, kafka config.KafkaConfig, topic string) *observedDLQ {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: {0: kgo.NewOffset().AtStart()}}))
	require.NoError(t, err)
	readCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	observed := &observedDLQ{records: make(map[poisonOrigin]*kgo.Record)}
	go func() {
		defer close(done)
		for readCtx.Err() == nil {
			fetches := client.PollRecords(readCtx, 100)
			observed.mu.Lock()
			if pollErr := fetches.Err(); pollErr != nil && readCtx.Err() == nil {
				observed.err = pollErr
			}
			for _, record := range fetches.Records() {
				partition, partitionErr := strconv.ParseInt(testHeaderValue(record.Headers, "dlq.origin.partition"), 10, 32)
				offset, offsetErr := strconv.ParseInt(testHeaderValue(record.Headers, "dlq.origin.offset"), 10, 64)
				if partitionErr != nil || offsetErr != nil {
					observed.err = errors.New("invalid DLQ origin")
					continue
				}
				observed.records[poisonOrigin{int32(partition), offset}] = record
			}
			observed.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		cancel()
		client.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("DLQ observer did not stop")
		}
	})
	return observed
}

func (o *observedDLQ) snapshot() (map[poisonOrigin]*kgo.Record, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	records := make(map[poisonOrigin]*kgo.Record, len(o.records))
	for origin, record := range o.records {
		records[origin] = record
	}
	return records, o.err
}

type poisonFixture struct {
	kafka    config.KafkaConfig
	settings config.WriterConfig
	admin    *kadm.Client
	store    *storageclickhouse.EventStore
	records  []*kgo.Record
	events   []*contracts.Event
	poison   map[poisonOrigin]*kgo.Record
	dlq      *kafkax.DLQPublisher
	observed *observedDLQ
}

func newPoisonFixture(ctx context.Context, t *testing.T, failedFirst bool) *poisonFixture {
	t.Helper()
	store, _ := newDedupeStore(t)
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafkaCfg.Brokers, ",")...),
		kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	admin := kadm.NewClient(client)
	topic := "writer-rebalance-" + uuid.NewString()
	response, err := admin.CreateTopics(ctx, 2, 1, nil, topic)
	require.NoError(t, err)
	require.NoError(t, response[topic].Err)
	kafka := writerKafkaConfig(topic, "rebalance-"+uuid.NewString())
	dlqTopic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "rebalance-dlq")
	settings := integrationWriterSettings()
	settings.MaxAttempts = 1
	settings.OperationTimeout = time.Second
	settings.ShutdownTimeout = time.Second
	settings.FlushInterval = 100 * time.Millisecond
	f := &poisonFixture{kafka: kafka, settings: settings, admin: admin, store: store,
		poison: make(map[poisonOrigin]*kgo.Record)}
	f.dlq = integrationDLQPublisher(t, kafka, dlqTopic, settings)
	f.observed = startObservedDLQ(ctx, t, kafka, dlqTopic)
	f.events = canonicalWriterEvents(4, time.Now().UTC().Add(-time.Minute))
	for partition := range int32(2) {
		for offset := range int64(14) {
			record := &kgo.Record{Topic: topic, Partition: partition, Key: []byte(fmt.Sprintf("key-%d-%d", partition, offset)),
				Value: []byte(`{"broken":`), Headers: []kgo.RecordHeader{{Key: "request_id", Value: []byte("rebalance-proof")}}}
			valid := offset == 0 || offset == 13
			if failedFirst && partition == 0 {
				valid = offset == 12 || offset == 13
			}
			if valid {
				index := int(partition) * 2
				if offset == 13 {
					index++
				}
				record.Value, err = json.Marshal(f.events[index])
				require.NoError(t, err)
			}
			f.records = append(f.records, record)
		}
	}
	require.NoError(t, client.ProduceSync(ctx, f.records...).FirstErr())
	for _, record := range f.records {
		if strings.HasPrefix(string(record.Value), `{"broken":`) {
			f.poison[poisonOrigin{record.Partition, record.Offset}] = record
		}
	}
	f.settings.BatchSize = len(f.records)
	return f
}

func startPoisonWriter(ctx context.Context, t *testing.T, consumer eventwriter.Consumer, f *poisonFixture,
	dlq eventwriter.QuarantinePublisher) (<-chan struct{}, *error) {
	t.Helper()
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		runErr = eventwriter.New(consumer, f.store, dlq, f.settings, contracts.DefaultLimits(), zerolog.Nop()).Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("writer did not stop within bounded cleanup")
		}
	})
	return done, &runErr
}

func joinRebalanceMember(ctx context.Context, t *testing.T, kafka config.KafkaConfig) (<-chan struct{}, func()) {
	t.Helper()
	assigned := make(chan struct{}, 1)
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...),
		kgo.ConsumerGroup(kafka.EventWriterGroup),
		kgo.ConsumeTopics(kafka.EventsTopic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()), kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(context.Context, *kgo.Client, map[string][]int32) {
			select {
			case assigned <- struct{}{}:
			default:
			}
		}))
	require.NoError(t, err)
	pollCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for pollCtx.Err() == nil {
			client.PollRecords(pollCtx, 100)
		}
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			closed := make(chan struct{})
			go func() { defer close(closed); client.CloseAllowingRebalance() }()
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Error("second member close exceeded deadline")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("second member poll did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return assigned, stop
}

func TestWriterSlowPoisonRebalance(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	for _, tc := range []struct {
		name                      string
		failedFirst, cancelDuring bool
	}{
		{name: "success_prefix_exhaustion_mixed_partitions"},
		{name: "failed_earliest_poison", failedFirst: true},
		{name: "cancellation_during_slow_publication", cancelDuring: true},
	} {
		t.Run(tc.name, func(t *testing.T) { runSlowPoisonRebalance(ctx, t, tc.failedFirst, tc.cancelDuring) })
	}
}

func runSlowPoisonRebalance(ctx context.Context, t *testing.T, failedFirst, cancelDuring bool) {
	t.Helper()
	f := newPoisonFixture(ctx, t, failedFirst)
	consumer, err := kafkax.NewConsumer(ctx, f.kafka, f.settings)
	require.NoError(t, err)
	t.Cleanup(func() { closeWriterConsumer(t, consumer) })
	owned := &releaseConsumer{Consumer: consumer, released: make(chan struct{})}
	delayed := &slowRealDLQ{inner: f.dlq, second: make(chan struct{}),
		acknowledged: make(map[poisonOrigin]bool), failFirst: failedFirst}
	runCtx, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()
	done, runErr := startPoisonWriter(runCtx, t, owned, f, delayed)
	select {
	case <-delayed.second:
	case <-time.After(15 * time.Second):
		t.Fatal("second real poison publication did not enter")
	}
	started := time.Now()
	assigned, stopMember := joinRebalanceMember(ctx, t, f.kafka)
	require.Eventually(t, func() bool {
		groups, describeErr := f.admin.DescribeGroups(ctx, f.kafka.EventWriterGroup)
		return describeErr == nil && groups[f.kafka.EventWriterGroup].State == "PreparingRebalance"
	}, 3*time.Second, 10*time.Millisecond, "second member never initiated an actual group rebalance")
	canceledAt := time.Now()
	if cancelDuring {
		cancelWriter()
	}
	select {
	case <-owned.released:
	case <-time.After(6 * time.Second):
		t.Fatal("aggregate poison work did not release ownership within six seconds")
	}
	t.Logf("poison handoff released after %s", time.Since(started))
	require.Less(t, delayed.elapsed(), 6*time.Second, "bound starts at the first poison publication")
	if cancelDuring {
		require.Less(t, time.Since(canceledAt), f.settings.ShutdownTimeout+250*time.Millisecond)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("writer did not exit after releasing ownership")
	}
	require.ErrorIs(t, *runErr, context.DeadlineExceeded)
	select {
	case <-assigned:
	case <-time.After(5 * time.Second):
		t.Fatal("joining member never received actual assignment")
	}
	assertPoisonPrefixes(ctx, t, f, delayed)
	assertInitialPoisonCommits(ctx, t, f, failedFirst)
	stopMember()
	closeWriterConsumer(t, consumer)
	recoverPoisonFixture(ctx, t, f)
}

func assertInitialPoisonCommits(ctx context.Context, t *testing.T, f *poisonFixture, failedFirst bool) {
	t.Helper()
	offsets, err := f.admin.FetchOffsets(ctx, f.kafka.EventWriterGroup)
	require.NoError(t, err)
	first, ok := offsets.Lookup(f.kafka.EventsTopic, 0)
	if failedFirst {
		require.True(t, !ok || first.At <= 0, "failed earliest poison must leave partition zero replayable")
	} else {
		require.True(t, ok)
		require.GreaterOrEqual(t, first.At, int64(1), "durable valid prefix did not commit")
	}
	independent, ok := offsets.Lookup(f.kafka.EventsTopic, 1)
	require.True(t, ok)
	require.GreaterOrEqual(t, independent.At, int64(1), "independently durable partition did not commit")
}

func recoverPoisonFixture(ctx context.Context, t *testing.T, f *poisonFixture) {
	t.Helper()
	consumer, err := kafkax.NewConsumer(ctx, f.kafka, f.settings)
	require.NoError(t, err)
	t.Cleanup(func() { closeWriterConsumer(t, consumer) })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done, runErr := startPoisonWriter(runCtx, t, consumer, f, f.dlq)
	// Read the full durable origin set before accepting any final offset claim.
	require.Eventually(t, func() bool {
		records, readErr := f.observed.snapshot()
		if readErr != nil {
			return false
		}
		for origin := range f.poison {
			if records[origin] == nil {
				return false
			}
		}
		return true
	}, 15*time.Second, 20*time.Millisecond, "recovered writer did not publish every original poison origin")
	require.Eventually(t, func() bool {
		offsets, fetchErr := f.admin.FetchOffsets(ctx, f.kafka.EventWriterGroup)
		if fetchErr != nil {
			return false
		}
		for partition := range int32(2) {
			offset, ok := offsets.Lookup(f.kafka.EventsTopic, partition)
			if !ok || offset.Err != nil || offset.At != 14 {
				return false
			}
		}
		return true
	}, 5*time.Second, 20*time.Millisecond, "recovered group did not commit both complete durable prefixes")
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("recovered writer did not stop")
	}
	require.NoError(t, *runErr)
	assertPoisonPrefixes(ctx, t, f, &slowRealDLQ{acknowledged: make(map[poisonOrigin]bool)})
	count, countErr := f.store.CountUnique(ctx, "demo", time.Time{})
	require.NoError(t, countErr)
	require.Equal(t, uint64(len(f.events)), count, "replay must remain immediately duplicate-safe")
}

func assertPoisonPrefixes(ctx context.Context, t *testing.T, f *poisonFixture, delayed *slowRealDLQ) {
	t.Helper()
	acknowledged := delayed.acknowledgedOrigins()
	require.Eventually(t, func() bool {
		records, err := f.observed.snapshot()
		if err != nil {
			return false
		}
		for origin := range acknowledged {
			if records[origin] == nil {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond, "acknowledged DLQ effects were not consumed from Kafka")
	offsets, err := f.admin.FetchOffsets(ctx, f.kafka.EventWriterGroup)
	require.NoError(t, err)
	durable, err := f.observed.snapshot()
	require.NoError(t, err)
	for partition := range int32(2) {
		prefix := observedDurablePrefix(f, partition, durable)
		offset, ok := offsets.Lookup(f.kafka.EventsTopic, partition)
		if ok && offset.At >= 0 {
			require.NoError(t, offset.Err)
			require.LessOrEqual(t, offset.At, prefix, "partition %d committed beyond independently observed effects", partition)
		}
	}
	assertObservedPoisonPayloads(t, f, durable)
	for _, event := range f.events {
		got, getErr := f.store.Get(ctx, "demo", event.EventID)
		require.NoError(t, getErr)
		require.Equal(t, event.EventID, got.EventID)
	}
}

func observedDurablePrefix(f *poisonFixture, partition int32, durable map[poisonOrigin]*kgo.Record) int64 {
	var prefix int64
	for _, source := range f.records {
		if source.Partition != partition {
			continue
		}
		origin := poisonOrigin{partition, source.Offset}
		if _, isPoison := f.poison[origin]; isPoison && durable[origin] == nil {
			break
		}
		prefix = source.Offset + 1
	}
	return prefix
}

func assertObservedPoisonPayloads(t *testing.T, f *poisonFixture, durable map[poisonOrigin]*kgo.Record) {
	t.Helper()
	for origin, record := range durable {
		source := f.poison[origin]
		require.NotNil(t, source)
		require.Equal(t, source.Key, record.Key)
		require.Equal(t, source.Value, record.Value)
		require.Equal(t, f.kafka.EventsTopic, testHeaderValue(record.Headers, "dlq.origin.topic"))
		require.Equal(t, "decode_error", testHeaderValue(record.Headers, "dlq.reason"))
		require.Equal(t, "rebalance-proof", testHeaderValue(record.Headers, "request_id"))
	}
}
