//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/eventwriter"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

const (
	chaosLagMetric      = "sentinel_kafka_consumer_lag"
	chaosLagTick        = time.Second // LagTracker interval argument; production keeps its 5 s default
	chaosLagSample      = 200 * time.Millisecond
	chaosSlowEvents     = 5000
	chaosSlowPublishes  = 10
	chaosSlowBatch      = 250
	chaosSlowDelay      = time.Second
	chaosSlowPeakFloor  = 1000
	chaosSlowRiseWithin = 30 * time.Second
	chaosLagRecovery    = 60 * time.Second
	chaosRebalanceParts = 4
	chaosRebalancePer   = 500
	chaosRebalanceBatch = 100
	chaosRebalanceDelay = 2 * time.Second
	chaosRebalanceWait  = 45 * time.Second
	chaosLagWriterStop  = 30 * time.Second
)

// slowEventStore delays every insert by delay while slow is set, then performs
// the real ClickHouse insert. The delay honors ctx like a stalled server would.
type slowEventStore struct {
	inner *storageclickhouse.EventStore
	delay time.Duration
	slow  atomic.Bool
}

func newSlowEventStore(inner *storageclickhouse.EventStore, delay time.Duration) *slowEventStore {
	s := &slowEventStore{inner: inner, delay: delay}
	s.slow.Store(true)
	return s
}

func (s *slowEventStore) InsertBatch(ctx context.Context, events []*contracts.Event) error {
	if s.slow.Load() {
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return s.inner.InsertBatch(ctx, events)
}

// lagWriter is one in-process event-writer replica with the production lag
// wiring: its own registry, ConsumerMetrics, LagTracker and consumer option.
type lagWriter struct {
	reg      *prometheus.Registry
	consumer *kafkax.Consumer
	cancel   context.CancelFunc
	writer   chan error
	tracker  chan error
}

func startLagWriter(
	t *testing.T, kafka config.KafkaConfig, store eventwriter.EventStore, settings config.WriterConfig,
) *lagWriter {
	t.Helper()
	reg := prometheus.NewRegistry()
	metrics := telemetry.NewConsumerMetrics(reg)
	tracker := kafkax.NewLagTracker(nil, metrics, kafka.EventWriterGroup, kafka.EventsTopic, chaosLagTick)
	consumer, err := kafkax.NewConsumer(t.Context(), kafka, settings, kafkax.WithLagTracker(tracker))
	require.NoError(t, err)
	writer := eventwriter.New(consumer, store, unusedDLQ{}, settings, contracts.DefaultLimits(), zerolog.Nop()).
		WithMetrics(metrics)
	ctx, cancel := context.WithCancel(context.Background())
	w := &lagWriter{
		reg: reg, consumer: consumer, cancel: cancel, writer: make(chan error, 1), tracker: make(chan error, 1),
	}
	go func() { w.writer <- writer.Run(ctx) }()
	go func() { w.tracker <- tracker.Run(ctx) }()
	t.Cleanup(func() { w.stop(t) })

	return w
}

// stop cancels the writer and tracker, then closes the consumer, in the
// production LIFO order (lag-tracker closes before kafka-consumer).
func (w *lagWriter) stop(t *testing.T) {
	t.Helper()
	w.cancel()
	for name, done := range map[string]chan error{"writer": w.writer, "lag tracker": w.tracker} {
		select {
		case err, ok := <-done:
			if ok {
				require.NoError(t, err, "%s returned an error", name)
				close(done)
			}
		case <-time.After(chaosLagWriterStop):
			t.Errorf("%s did not stop within %s", name, chaosLagWriterStop)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), chaosLagWriterStop)
	defer cancel()
	require.NoError(t, w.consumer.Close(ctx))
}

// exited reports a writer that returned early (a failed run), without blocking.
func (w *lagWriter) exited() error {
	select {
	case err := <-w.writer:
		w.writer <- err
		if err == nil {
			return errors.New("writer returned before cancellation")
		}
		return err
	default:
		return nil
	}
}

// lagSeries reads every sentinel_kafka_consumer_lag series from reg, keyed by
// partition, as a scrape would see it.
func lagSeries(t *testing.T, reg *prometheus.Registry) map[int32]float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	series := make(map[int32]float64)
	for _, family := range families {
		if family.GetName() != chaosLagMetric {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() != "partition" {
					continue
				}
				partition, parseErr := strconv.ParseInt(label.GetValue(), 10, 32)
				require.NoError(t, parseErr)
				series[int32(partition)] = metric.GetGauge().GetValue()
			}
		}
	}

	return series
}

func lagSum(series map[int32]float64) float64 {
	var sum float64
	for _, value := range series {
		sum += value
	}
	return sum
}

type lagSample struct {
	at  time.Duration
	sum float64
	n   int
}

// lagSampler records the summed lag of one registry every chaosLagSample.
type lagSampler struct {
	mu      sync.Mutex
	samples []lagSample
	stop    context.CancelFunc
	done    chan struct{}
}

func startLagSampler(t *testing.T, reg *prometheus.Registry) *lagSampler {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &lagSampler{stop: cancel, done: make(chan struct{})}
	started := time.Now()
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(chaosLagSample)
		defer ticker.Stop()
		for {
			if sum, n, err := gatheredLag(reg); err == nil {
				s.mu.Lock()
				s.samples = append(s.samples, lagSample{at: time.Since(started), sum: sum, n: n})
				s.mu.Unlock()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	t.Cleanup(s.close)

	return s
}

// gatheredLag sums every lag series in reg and counts them.
func gatheredLag(reg *prometheus.Registry) (float64, int, error) {
	families, err := reg.Gather()
	if err != nil {
		return 0, 0, err
	}
	sum, n := 0.0, 0
	for _, family := range families {
		if family.GetName() != chaosLagMetric {
			continue
		}
		for _, metric := range family.GetMetric() {
			sum += metric.GetGauge().GetValue()
			n++
		}
	}
	return sum, n, nil
}

func (s *lagSampler) close() {
	s.stop()
	<-s.done
}

func (s *lagSampler) snapshot() []lagSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.samples)
}

// risingTicks returns the longest run of consecutive increases between the
// distinct published values (each distinct value is one tracker tick) and the
// peak sum observed so far.
func risingTicks(samples []lagSample) (int, float64) {
	longest, run, peak := 0, 0, 0.0
	last, seen := 0.0, false
	for _, sample := range samples {
		if sample.n == 0 {
			continue
		}
		peak = max(peak, sample.sum)
		switch {
		case !seen:
			seen = true
		case sample.sum > last:
			run++
			longest = max(longest, run)
		case sample.sum < last:
			run = 0
		}
		last = sample.sum
	}
	return longest, peak
}

// committedNextOffsets returns the group's committed next offset per partition.
func committedNextOffsets(t *testing.T, admin *kadm.Client, group, topic string) map[int32]int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	offsets, err := admin.FetchOffsets(ctx, group)
	require.NoError(t, err)
	committed := make(map[int32]int64)
	offsets.Each(func(o kadm.OffsetResponse) {
		if o.Topic == topic && o.Err == nil && o.At >= 0 {
			committed[o.Partition] = o.At
		}
	})
	return committed
}

// endOffsets returns the broker end offset per partition of topic.
func endOffsets(t *testing.T, admin *kadm.Client, topic string) map[int32]int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	listed, err := admin.ListEndOffsets(ctx, topic)
	require.NoError(t, err)
	ends := make(map[int32]int64)
	listed.Each(func(o kadm.ListedOffset) {
		if o.Err == nil {
			ends[o.Partition] = o.Offset
		}
	})
	return ends
}

// independentBacklog computes end - committed per partition with kadm, the way
// an operator would, without using the tracker. Uncommitted partitions start at 0.
func independentBacklog(t *testing.T, admin *kadm.Client, group, topic string, partitions int32) map[int32]int64 {
	t.Helper()
	ends := endOffsets(t, admin, topic)
	committed := committedNextOffsets(t, admin, group, topic)
	backlog := make(map[int32]int64, partitions)
	for partition := range partitions {
		backlog[partition] = ends[partition] - committed[partition]
	}
	return backlog
}

func newChaosAdmin(t *testing.T, kafka config.KafkaConfig) *kadm.Client {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...),
		kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return kadm.NewClient(client)
}

func TestChaosSlowConsumerLag(t *testing.T) {
	store, _ := newDedupeStore(t)
	topic := requirePartitionedTopic(t, newChaosAdmin(t, kafkaCfg), "chaos-slow", 1)
	group := "chaos-slow-" + uuid.NewString()
	kafka := writerKafkaConfig(topic, group)
	slow := newSlowEventStore(store, chaosSlowDelay)
	settings := integrationWriterSettings()
	settings.BatchSize = chaosSlowBatch
	settings.FlushInterval = 200 * time.Millisecond
	writer := startLagWriter(t, kafka, slow, settings)
	sampler := startLagSampler(t, writer.reg)

	// The partition series must exist (seeded from the start offset) before load.
	require.Eventually(t, func() bool {
		series := lagSeries(t, writer.reg)
		value, ok := series[0]
		return ok && value == 0
	}, 20*time.Second, 50*time.Millisecond, "lag series for partition 0 never appeared at 0")

	// Producers outpace the slowed writer (250 records/s) over ~5 s.
	producer := newWriterProducer(t, kafka)
	perPublish := chaosSlowEvents / chaosSlowPublishes
	for range chaosSlowPublishes {
		batch := canonicalWriterEvents(perPublish, time.Now().UTC().Add(-time.Minute))
		require.NoError(t, producer.PublishBatch(t.Context(), batch, uuid.NewString()))
		time.Sleep(500 * time.Millisecond)
	}

	require.Eventually(t, func() bool {
		rises, peak := risingTicks(sampler.snapshot())
		return rises >= 2 && peak >= chaosSlowPeakFloor
	}, chaosSlowRiseWithin, chaosLagSample, "lag did not rise across two ticks to >= %d", chaosSlowPeakFloor)
	require.NoError(t, writer.exited())
	rises, peak := risingTicks(sampler.snapshot())
	atRelease := lagSum(lagSeries(t, writer.reg))
	require.Positive(t, atRelease, "backlog drained before the slowdown ended")

	slow.slow.Store(false)
	released := time.Now()
	require.Eventually(t, func() bool {
		series := lagSeries(t, writer.reg)
		_, owned := series[0]
		committed, ok := committedNextOffset(t, kafka, group, topic)
		return owned && lagSum(series) == 0 && ok && committed == chaosSlowEvents
	}, chaosLagRecovery, 100*time.Millisecond, "lag did not return to 0 with every record committed")
	recovery := time.Since(released)
	require.NoError(t, writer.exited())

	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(chaosSlowEvents), unique, "slow consumer lost or invented events")
	t.Logf("slow consumer: peak lag=%.0f rising ticks=%d lag at release=%.0f recovery to 0 in %s unique=%d committed=%d",
		peak, rises, atRelease, recovery.Round(10*time.Millisecond), unique, chaosSlowEvents)
}

func requirePartitionedTopic(t *testing.T, admin *kadm.Client, prefix string, partitions int32) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	topic := prefix + "-" + uuid.NewString()
	response, err := admin.CreateTopics(ctx, partitions, 1, nil, topic)
	require.NoError(t, err)
	require.NoError(t, response[topic].Err)
	return topic
}

// produceToPartitions writes perPartition canonical events to every partition.
func produceToPartitions(
	t *testing.T, kafka config.KafkaConfig, topic string, partitions int32, perPartition int,
) {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...),
		kgo.RecordPartitioner(kgo.ManualPartitioner()), kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.RecordDeliveryTimeout(10*time.Second))
	require.NoError(t, err)
	defer client.Close()
	records := make([]*kgo.Record, 0, int(partitions)*perPartition)
	for partition := range partitions {
		for _, event := range canonicalWriterEvents(perPartition, time.Now().UTC().Add(-time.Minute)) {
			value, marshalErr := json.Marshal(event)
			require.NoError(t, marshalErr)
			records = append(records, &kgo.Record{Topic: topic, Partition: partition, Value: value,
				Key: []byte(event.TenantID + "|" + strconv.Itoa(int(partition)))})
		}
	}
	require.NoError(t, client.ProduceSync(t.Context(), records...).FirstErr())
}

func partitionKeys(series map[int32]float64) []int32 {
	return slices.Sorted(maps.Keys(series))
}

func TestChaosLagAfterRebalance(t *testing.T) {
	store, _ := newDedupeStore(t)
	admin := newChaosAdmin(t, kafkaCfg)
	topic := requirePartitionedTopic(t, admin, "chaos-lag-rebalance", chaosRebalanceParts)
	group := "chaos-lag-rebalance-" + uuid.NewString()
	kafka := writerKafkaConfig(topic, group)
	produceToPartitions(t, kafka, topic, chaosRebalanceParts, chaosRebalancePer)
	total := chaosRebalanceParts * chaosRebalancePer
	slow := newSlowEventStore(store, chaosRebalanceDelay)
	settings := integrationWriterSettings()
	settings.BatchSize = chaosRebalanceBatch
	settings.FlushInterval = 200 * time.Millisecond
	all := []int32{0, 1, 2, 3}

	writerA := startLagWriter(t, kafka, slow, settings)
	require.Eventually(t, func() bool {
		series := lagSeries(t, writerA.reg)
		return slices.Equal(partitionKeys(series), all) && lagSum(series) > 0
	}, chaosRebalanceWait, 50*time.Millisecond, "writer A never exported lag for every partition")

	writerB := startLagWriter(t, kafka, slow, settings)
	var ownedA, ownedB []int32
	require.Eventually(t, func() bool {
		ownedA, ownedB = partitionKeys(lagSeries(t, writerA.reg)), partitionKeys(lagSeries(t, writerB.reg))
		union := slices.Sorted(slices.Values(append(slices.Clone(ownedA), ownedB...)))
		return len(ownedA) > 0 && len(ownedB) > 0 && slices.Equal(union, all) &&
			groupMemberCount(t, kafka, group) == 2
	}, chaosRebalanceWait, 50*time.Millisecond, "rebalance did not split the partitions without overlap")
	require.NoError(t, writerA.exited())
	require.NoError(t, writerB.exited())
	revoked := slices.DeleteFunc(slices.Clone(all), func(p int32) bool { return slices.Contains(ownedA, p) })
	require.NotEmpty(t, revoked)

	// Every partition is exported by exactly one replica, with exactly the
	// independently computed backlog once the latest commit has been ticked.
	var matched map[int32]int64
	require.Eventually(t, func() bool {
		before := independentBacklog(t, admin, group, topic, chaosRebalanceParts)
		seriesA, seriesB := lagSeries(t, writerA.reg), lagSeries(t, writerB.reg)
		after := independentBacklog(t, admin, group, topic, chaosRebalanceParts)
		if !maps.Equal(before, after) {
			return false
		}
		var backlog int64
		for _, partition := range all {
			valueA, inA := seriesA[partition]
			valueB, inB := seriesB[partition]
			if inA == inB {
				return false // stale (double) series or a missing one
			}
			value := valueA + valueB
			if int64(value) != after[partition] {
				return false
			}
			backlog += after[partition]
		}
		matched = after
		return int64(lagSum(seriesA)+lagSum(seriesB)) == backlog && backlog > 0
	}, chaosRebalanceWait, 50*time.Millisecond, "sum(lag) across replicas never equaled the kadm backlog")
	for _, partition := range revoked {
		_, stale := lagSeries(t, writerA.reg)[partition]
		require.False(t, stale, "revoked partition %d still exported by writer A", partition)
	}
	var backlog int64
	for _, value := range matched {
		backlog += value
	}

	slow.slow.Store(false)
	released := time.Now()
	require.Eventually(t, func() bool {
		seriesA, seriesB := lagSeries(t, writerA.reg), lagSeries(t, writerB.reg)
		ends := endOffsets(t, admin, topic)
		committed := committedNextOffsets(t, admin, group, topic)
		union := slices.Sorted(slices.Values(append(partitionKeys(seriesA), partitionKeys(seriesB)...)))
		return slices.Equal(union, all) && lagSum(seriesA) == 0 && lagSum(seriesB) == 0 &&
			maps.Equal(ends, committed)
	}, chaosLagRecovery, 100*time.Millisecond, "lag did not reach 0 on both replicas with every record committed")
	recovery := time.Since(released)
	require.NoError(t, writerA.exited())
	require.NoError(t, writerB.exited())

	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(total), unique)
	t.Logf("lag after rebalance: A owns %v, B owns %v, revoked from A %v; sum(lag)=backlog=%d %v; "+
		"both 0 after %s; unique=%d", ownedA, ownedB, revoked, backlog, matched,
		recovery.Round(10*time.Millisecond), unique)
}
