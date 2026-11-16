//go:build integration

package test_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/eventwriter"
	"github.com/6sLOGAN78/sentinel/internal/retry"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
	"github.com/6sLOGAN78/sentinel/migrations"
)

const chaosClickHouseEvents = 6

// chaosClickHouse starts a dedicated, migrated ClickHouse that may be paused.
// The handle unpauses itself on cleanup before it is terminated.
func chaosClickHouse(t *testing.T) (*sentineltesting.FaultContainer, storageclickhouse.Conn, *atomic.Bool) {
	t.Helper()
	cfg, handle, err := sentineltesting.StartClickHouseFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(handle.Close)
	paused := &atomic.Bool{}
	t.Cleanup(func() {
		if paused.Load() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = handle.Unpause(ctx)
		}
	})
	_, err = chmigrate.New(cfg.HTTPAddress, cfg.User, cfg.Password, cfg.Database, nil).
		Run(t.Context(), migrations.ClickHouse())
	require.NoError(t, err)
	conn, err := storageclickhouse.Open(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return handle, conn, paused
}

// countingWaiter observes every retry wait and delegates to the production
// full-jitter wait.
func countingWaiter(count *atomic.Int64, first chan<- struct{}) retry.Waiter {
	return func(ctx context.Context, ceiling time.Duration) error {
		if count.Add(1) == 1 && first != nil {
			close(first)
		}
		return retry.Wait(ctx, ceiling)
	}
}

func chaosWriterTopic(t *testing.T, prefix string) (config.KafkaConfig, string, string) {
	t.Helper()
	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, prefix)
	group := prefix + "-" + uuid.NewString()

	return writerKafkaConfig(topic, group), topic, group
}

func TestChaosClickHouseShortOutageAbsorbed(t *testing.T) {
	handle, conn, paused := chaosClickHouse(t)
	store := storageclickhouse.NewEventStore(conn)
	kafka, topic, group := chaosWriterTopic(t, "chaos-ch-short")
	events := canonicalWriterEvents(chaosClickHouseEvents, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(), events, uuid.NewString()))

	// The outage is shorter than the retry budget: ends as soon as one retry
	// wait is observed, leaving at least four further attempts.
	settings := integrationWriterSettings()
	settings.BatchSize = len(events)
	settings.MaxAttempts = 6
	settings.OperationTimeout = 2 * time.Second
	settings.RetryMinBackoff = 100 * time.Millisecond
	settings.RetryMaxBackoff = 500 * time.Millisecond
	var retries atomic.Int64
	firstRetry := make(chan struct{})

	require.NoError(t, handle.Pause(t.Context()))
	paused.Store(true)
	writer := startChaosWriter(t, kafka, store, settings, func(w *eventwriter.Writer) *eventwriter.Writer {
		return w.WithRetryWaiter(countingWaiter(&retries, firstRetry))
	})
	select {
	case <-firstRetry:
	case <-time.After(30 * time.Second):
		t.Fatal("writer never retried the insert during the ClickHouse outage")
	}
	_, committed := committedNextOffset(t, kafka, group, topic)
	require.False(t, committed, "offset advanced while ClickHouse was paused")
	require.NoError(t, handle.Unpause(t.Context()))
	paused.Store(false)

	waitCommittedOffset(t, kafka, group, topic, int64(len(events)))
	done, runErr := writer.exited()
	require.False(t, done, "writer exited (%v) on an outage within its retry budget", runErr)
	require.GreaterOrEqual(t, retries.Load(), int64(1))
	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(len(events)), unique)
	require.NoError(t, writer.stop(t))
	t.Logf("short outage absorbed after %d retry waits", retries.Load())
}

func TestChaosClickHouseLongOutageRestart(t *testing.T) {
	handle, conn, paused := chaosClickHouse(t)
	store := storageclickhouse.NewEventStore(conn)
	kafka, topic, group := chaosWriterTopic(t, "chaos-ch-long")
	events := canonicalWriterEvents(chaosClickHouseEvents, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(), events, uuid.NewString()))

	// The outage outlasts the whole retry budget (3 attempts of 500 ms).
	settings := integrationWriterSettings()
	settings.BatchSize = len(events)
	settings.MaxAttempts = 3
	settings.OperationTimeout = 500 * time.Millisecond
	settings.RetryMinBackoff = 10 * time.Millisecond
	settings.RetryMaxBackoff = 20 * time.Millisecond
	var retries atomic.Int64

	require.NoError(t, handle.Pause(t.Context()))
	paused.Store(true)
	started := time.Now()
	first := startChaosWriter(t, kafka, store, settings, func(w *eventwriter.Writer) *eventwriter.Writer {
		return w.WithRetryWaiter(countingWaiter(&retries, nil))
	})
	var runErr error
	require.Eventually(t, func() bool {
		var done bool
		done, runErr = first.exited()
		return done
	}, 30*time.Second, 50*time.Millisecond, "writer did not give up after its retry budget")
	require.Error(t, runErr, "an outage beyond the retry budget must be terminal")
	require.ErrorContains(t, runErr, "insert event batch")
	require.Less(t, time.Since(started), 15*time.Second)
	require.Equal(t, int64(settings.MaxAttempts-1), retries.Load(), "every attempt must have been spent")
	_, committed := committedNextOffset(t, kafka, group, topic)
	require.False(t, committed, "terminal insert failure advanced the source offset")
	closeWriterConsumer(t, first.consumer)

	require.NoError(t, handle.Unpause(t.Context()))
	paused.Store(false)
	require.Eventually(t, func() bool { return conn.Ping(t.Context()) == nil }, 30*time.Second, 100*time.Millisecond)
	restart := startChaosWriter(t, kafka, store, settings, nil)
	waitCommittedOffset(t, kafka, group, topic, int64(len(events)))
	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(len(events)), unique)
	require.NoError(t, restart.stop(t))
	for _, event := range events {
		got, getErr := store.Get(t.Context(), "demo", event.EventID)
		require.NoError(t, getErr, "event %s", event.EventID)
		require.Equal(t, event.EventID, got.EventID)
	}
}
