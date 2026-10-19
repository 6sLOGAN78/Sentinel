package detector //nolint:testpackage // Exercise owned effects and checkpoint metadata without a fake PG runtime.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

type poisonConsumer struct {
	readinessConsumer
	committed map[detection.TopicPartition]detection.Checkpoint
	commitCtx error
}

func (c *poisonConsumer) CommitCheckpoints(ctx context.Context,
	checkpoints map[detection.TopicPartition]detection.Checkpoint) error {
	c.commitCtx = ctx.Err()
	for tp, cp := range checkpoints {
		c.committed[tp] = cp
	}
	return ctx.Err()
}

type poisonDecisions struct{ decisionStore }

func (poisonDecisions) CheckpointCommitted(context.Context, postgres.DetectionOwnership, detection.Checkpoint) error {
	return nil
}

type poisonPublisher struct {
	publish func(context.Context, *kgo.Record, kafkax.DLQReason) error
}

func (p poisonPublisher) Publish(ctx context.Context, r *kgo.Record, reason kafkax.DLQReason) error {
	return p.publish(ctx, r, reason)
}

func poisonWorker(t *testing.T) (*Worker, *poisonConsumer) {
	t.Helper()
	file, err := os.Open("../../../config/rules.yaml")
	require.NoError(t, err)
	defer file.Close()
	rules, err := detection.LoadRules(file)
	require.NoError(t, err)
	c := &poisonConsumer{committed: make(map[detection.TopicPartition]detection.Checkpoint)}
	c.snapshot = kafkax.DetectorReadiness{Observed: true, Member: "joined", Generation: 1,
		Tokens: make(map[detection.TopicPartition]uint64)}
	settings := config.WriterConfig{BatchSize: 500, FlushInterval: time.Millisecond, MaxAttempts: 2,
		RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
		OperationTimeout: 50 * time.Millisecond, ShutdownTimeout: time.Second}
	w := New(c, nil, rules, settings, contracts.DefaultLimits(), nil, config.DetectorPGConfig{}, &opshttp.DetectionMetrics{})
	w.decisions = poisonDecisions{}
	for n := range int32(2) {
		tp := detection.TopicPartition{Topic: "events", Partition: n}
		c.snapshot.Tokens[tp] = 1
		w.partitions[tp] = partition{token: 1, state: detection.NewPartitionStateWithBounds(rules, w.bounds),
			owner: postgres.DetectionOwnership{Source: tp, Token: 1, Member: "joined", Generation: 1}}
	}
	return w, c
}

func poisonEvent(t *testing.T, offset int64, at time.Time) *kgo.Record {
	t.Helper()
	ip := "192.0.2.8"
	received := contracts.NewTimestamp(at)
	e := &contracts.Event{SchemaVersion: 1, EventID: "00000000-0000-4000-8000-000000000001", TenantID: "demo",
		OccurredAt: received, ReceivedAt: &received, Source: "test", EventType: "auth.success", SourceIP: &ip}
	raw, err := json.Marshal(e)
	require.NoError(t, err)
	return &kgo.Record{Topic: "events", Key: []byte("demo|" + ip), Value: raw, Offset: offset}
}

func poisonMetadata(t *testing.T, cp detection.Checkpoint) detection.Metadata {
	t.Helper()
	var m detection.Metadata
	require.NoError(t, json.Unmarshal([]byte(cp.Metadata), &m))
	require.Equal(t, 2, m.Format)
	return m
}

func TestDetectorPoisonPrefix(t *testing.T) {
	t.Run("trusted classification and event time", testDetectorClassification)
	t.Run("failed partition retains acknowledged prefix", func(t *testing.T) {
		w, c := poisonWorker(t)
		now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		good := poisonEvent(t, 0, now)
		bad := &kgo.Record{Topic: "events", Offset: 1, Value: []byte("{")}
		safe := poisonEvent(t, 0, now)
		safe.Partition = 1
		calls := 0
		w.WithQuarantine(poisonPublisher{func(context.Context, *kgo.Record, kafkax.DLQReason) error {
			calls++
			return errors.New("untrusted driver payload")
		}})
		err := w.process(t.Context(), []*kgo.Record{good, bad, poisonEvent(t, 2, now.Add(time.Hour)), safe})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "untrusted driver")
		require.Equal(t, 2, calls)
		for p := range int32(2) {
			cp, ok := c.committed[detection.TopicPartition{Topic: "events", Partition: p}]
			require.True(t, ok, "safe durable partitions must commit independently")
			require.Equal(t, int64(1), cp.Offset)
			m := poisonMetadata(t, cp)
			require.Equal(t, now.UnixMilli(), *m.PrefixMaxEventTime)
			require.Equal(t, int64(1), m.ProcessedNext)
		}
	})
	t.Run("ledger rejection leaves state immutable", func(t *testing.T) {
		w, _ := poisonWorker(t)
		state := w.partitions[detection.TopicPartition{Topic: "events"}].state
		w.bounds = detection.NewBounds(detection.StateLimits{Ledger: 1})
		state.Release()
		state = detection.NewPartitionStateWithBounds(w.rules, w.bounds)
		p := w.partitions[detection.TopicPartition{Topic: "events"}]
		p.state = state
		w.partitions[detection.TopicPartition{Topic: "events"}] = p
		w.WithQuarantine(poisonPublisher{func(context.Context, *kgo.Record, kafkax.DLQReason) error { return nil }})
		// One record is admitted; another in the same uncommitted batch must
		// fail capacity rather than evicting its prefix ledger.
		err := w.process(t.Context(), []*kgo.Record{
			{Topic: "events", Offset: 0, Value: []byte("{")}, {Topic: "events", Offset: 1, Value: []byte("{")},
		})
		require.ErrorContains(t, err, "capacity")
		cp, checkpointErr := state.Checkpoint()
		require.NoError(t, checkpointErr)
		require.Equal(t, int64(1), cp.Offset)
		require.Equal(t, int64(1), poisonMetadata(t, cp).ProcessedNext)
	})
}

func testDetectorClassification(t *testing.T) {
	for _, kind := range []string{"json", "schema", "key", "missing timestamp", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			w, c := poisonWorker(t)
			r := poisonEvent(t, 0, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
			reason := kafkax.DLQReasonValidation
			switch kind {
			case "json":
				r.Value, reason = []byte("{"), kafkax.DLQReasonDecode
			case "schema":
				r.Value = []byte(`{"schema_version":9}`)
				reason = kafkax.DLQReasonMissingReceivedAt
			case "key":
				r.Key = []byte("other|192.0.2.8")
			case "missing timestamp":
				var e contracts.Event
				require.NoError(t, json.Unmarshal(r.Value, &e))
				e.ReceivedAt = nil
				r.Value, _ = json.Marshal(e)
				reason = kafkax.DLQReasonMissingReceivedAt
			case "oversized":
				r.Value, reason = make([]byte, (1<<20)+1), kafkax.DLQReasonDecode
			}
			r.Headers = []kgo.RecordHeader{{Key: "DLQ.origin.topic", Value: []byte("forged")}}
			calls := 0
			w.WithQuarantine(poisonPublisher{func(ctx context.Context, original *kgo.Record, got kafkax.DLQReason) error {
				calls++
				require.NoError(t, ctx.Err())
				require.Same(t, r, original)
				require.Equal(t, reason, got)
				return nil
			}})
			require.NoError(t, w.process(t.Context(), []*kgo.Record{r}))
			require.Equal(t, 1, calls)
			cp := c.committed[detection.TopicPartition{Topic: "events"}]
			require.Equal(t, int64(1), cp.Offset)
			m := poisonMetadata(t, cp)
			require.Nil(t, m.PrefixMaxEventTime)
			require.Nil(t, m.SealedThrough)
			require.True(t, m.DecisionFrontier.None)
			restore := detection.NewPartitionState(w.rules)
			require.NoError(t, restore.Restore(cp), "poison-only checkpoints must restore strictly")
		})
	}
}

func TestWorkerAggregateBudget(t *testing.T) {
	for _, mode := range []string{"slow", "shared retries", "parent cancellation"} {
		t.Run(mode, func(t *testing.T) {
			w, c := poisonWorker(t)
			var records []*kgo.Record
			for n := range int64(400) {
				records = append(records, &kgo.Record{Topic: "events", Offset: n, Value: []byte("{")})
			}
			attempts := make(map[int64]int)
			acks := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			w.WithQuarantine(poisonPublisher{func(op context.Context, r *kgo.Record, _ kafkax.DLQReason) error {
				attempts[r.Offset]++
				if mode == "shared retries" && attempts[r.Offset] == 1 {
					return errors.New("temporary failure")
				}
				if mode == "parent cancellation" {
					cancel()
					return op.Err()
				}
				if mode == "slow" {
					timer := time.NewTimer(20 * time.Millisecond)
					defer timer.Stop()
					select {
					case <-op.Done():
						return op.Err()
					case <-timer.C:
					}
				}
				acks++
				return nil
			}})
			start := time.Now()
			require.Error(t, w.process(ctx, records))
			require.Less(t, time.Since(start), 400*time.Millisecond)
			if mode == "parent cancellation" {
				require.Empty(t, c.committed)
				return
			}
			require.Positive(t, acks, "earlier acknowledged poison must remain committable")
			cp := c.committed[detection.TopicPartition{Topic: "events"}]
			require.Equal(t, int64(acks), cp.Offset)
			require.NoError(t, c.commitCtx, "effects expiry must preserve independent commit reservation")
			if mode == "shared retries" {
				require.Equal(t, 1, acks, "successful poison must not replenish failed-attempt allowance")
			}
		})
	}
}
