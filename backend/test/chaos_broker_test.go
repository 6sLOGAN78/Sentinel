//go:build integration

package test_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
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

const (
	chaosBrokerBatch     = 20
	chaosBrokerRecovery  = 60 * time.Second
	chaosProduceTimeout  = 10 * time.Second // newWriterProducer's ProduceTimeout
	chaosProduceSlack    = 5 * time.Second
	chaosMaxBuffered     = 5000 // newWriterProducer's MaxBufferedRecords
	chaosWriterStopLimit = 30 * time.Second
)

// chaosWriter is one in-process event-writer instance with its own consumer.
type chaosWriter struct {
	cancel   context.CancelFunc
	done     chan error
	consumer *kafkax.Consumer
	once     sync.Once
	result   error
	finished bool
}

func startChaosWriter(
	t *testing.T, kafka config.KafkaConfig, store eventwriter.EventStore, settings config.WriterConfig,
	configure func(*eventwriter.Writer) *eventwriter.Writer,
) *chaosWriter {
	t.Helper()
	consumer, err := kafkax.NewConsumer(t.Context(), kafka, settings)
	require.NoError(t, err)
	writer := eventwriter.New(consumer, store, unusedDLQ{}, settings, contracts.DefaultLimits(), zerolog.Nop())
	if configure != nil {
		writer = configure(writer)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &chaosWriter{cancel: cancel, done: make(chan error, 1), consumer: consumer}
	go func() { w.done <- writer.Run(ctx) }()
	t.Cleanup(func() {
		w.cancel()
		select {
		case <-w.done:
		case <-time.After(chaosWriterStopLimit):
			t.Errorf("chaos writer did not stop within %s", chaosWriterStopLimit)
		}
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelClose()
		_ = consumer.Close(closeCtx)
	})

	return w
}

// exited reports whether Run has returned, without blocking.
func (w *chaosWriter) exited() (bool, error) {
	if w.finished {
		return true, w.result
	}
	select {
	case err := <-w.done:
		w.finished, w.result = true, err
		w.done <- err // keep the value for the cleanup drain
		return true, err
	default:
		return false, nil
	}
}

// stop cancels Run and returns its result.
func (w *chaosWriter) stop(t *testing.T) error {
	t.Helper()
	w.once.Do(w.cancel)
	if done, err := w.exited(); done {
		return err
	}
	select {
	case err := <-w.done:
		w.finished, w.result = true, err
		w.done <- err
		return err
	case <-time.After(chaosWriterStopLimit):
		t.Fatalf("chaos writer did not stop within %s", chaosWriterStopLimit)
		return nil
	}
}

func topicEndOffset(t *testing.T, kafka config.KafkaConfig, topic string) int64 {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...))
	require.NoError(t, err)
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ends, err := kadm.NewClient(client).ListEndOffsets(ctx, topic)
	require.NoError(t, err)
	end, ok := ends.Lookup(topic, 0)
	require.True(t, ok, "topic %s partition 0 missing", topic)
	require.NoError(t, end.Err)

	return end.Offset
}

func TestChaosKafkaBrokerRestart(t *testing.T) {
	brokerCfg, handle, err := sentineltesting.StartKafkaFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(handle.Close)
	topic := sentineltesting.RequireUniqueTopic(t, brokerCfg, "chaos-broker")
	group := "chaos-broker-" + uuid.NewString()
	kafka := config.KafkaConfig{Brokers: brokerCfg.Brokers, EventsTopic: topic, EventWriterGroup: group}
	_, conn := newRecoveryDatabase(t)
	store := storageclickhouse.NewEventStore(conn)
	producer := newWriterProducer(t, kafka)

	// Sample the producer buffer throughout; it must never exceed its cap.
	var maxBuffered atomic.Int64
	sampleCtx, stopSampling := context.WithCancel(context.Background())
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if n := producer.BufferedRecords(); n > maxBuffered.Load() {
				maxBuffered.Store(n)
			}
			select {
			case <-sampleCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	t.Cleanup(func() { stopSampling(); <-sampled })

	batchA := canonicalWriterEvents(chaosBrokerBatch, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, producer.PublishBatch(t.Context(), batchA, uuid.NewString()))
	settings := integrationWriterSettings()
	settings.BatchSize = chaosBrokerBatch
	writer := startChaosWriter(t, kafka, store, settings, nil)
	waitCommittedOffset(t, kafka, group, topic, chaosBrokerBatch)

	// Real broker outage: docker stop, not pause.
	require.NoError(t, handle.Stop(t.Context()))
	batchB := canonicalWriterEvents(chaosBrokerBatch, time.Now().UTC().Add(-time.Minute))
	started := time.Now()
	publishErr := producer.PublishBatch(t.Context(), batchB, uuid.NewString())
	require.Error(t, publishErr, "publish acknowledged while the broker was stopped")
	require.Less(t, time.Since(started), chaosProduceTimeout+chaosProduceSlack,
		"failed publish was not bounded by the producer delivery timeout")

	require.NoError(t, handle.Start(t.Context()))
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		return producer.Ping(ctx) == nil
	}, chaosBrokerRecovery, 250*time.Millisecond, "broker did not recover after restart")

	// The failed publish wrote nothing, and nothing beyond batch A was committed.
	require.Equal(t, int64(chaosBrokerBatch), topicEndOffset(t, kafka, topic), "failed publish left records")
	committed, ok := committedNextOffset(t, kafka, group, topic)
	require.True(t, ok)
	require.Equal(t, int64(chaosBrokerBatch), committed)

	// Republish batch B until it is acknowledged (transient post-restart
	// leadership errors are retried; duplicates are harmless by identity).
	require.Eventually(t, func() bool {
		return producer.PublishBatch(t.Context(), batchB, uuid.NewString()) == nil
	}, chaosBrokerRecovery, 500*time.Millisecond, "batch B was never acknowledged after restart")

	if done, runErr := writer.exited(); done {
		t.Logf("event-writer exited during the outage (%v); restarting it", runErr)
		writer = startChaosWriter(t, kafka, store, settings, nil)
	}
	total := topicEndOffset(t, kafka, topic)
	require.GreaterOrEqual(t, total, int64(2*chaosBrokerBatch))
	require.Eventually(t, func() bool {
		got, found := committedNextOffset(t, kafka, group, topic)
		return found && got == total
	}, chaosBrokerRecovery, 100*time.Millisecond, "writer did not commit every record after the restart")
	require.NoError(t, writer.stop(t))

	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(2*chaosBrokerBatch), unique)
	for _, event := range append(batchA, batchB...) {
		got, getErr := store.Get(t.Context(), "demo", event.EventID)
		require.NoError(t, getErr, "event %s", event.EventID)
		require.Equal(t, event.EventID, got.EventID)
	}
	stopSampling()
	<-sampled
	require.LessOrEqual(t, maxBuffered.Load(), int64(chaosMaxBuffered))
	t.Logf("broker restart: total records=%d unique=%d max buffered=%d", total, unique, maxBuffered.Load())
}
