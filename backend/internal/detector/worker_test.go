package detector //nolint:testpackage // Exercise owned effects and checkpoint metadata without a fake PG runtime.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
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
	w := New(c, nil, rules, settings, contracts.DefaultLimits(), nil, config.DetectorPGConfig{})
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
	t.Run("live pin and failed poison preserve clocks", testDetectorLivePoisonPin)
	t.Run("poison ordering cannot acknowledge pending effects", testPoisonAcknowledgementOrder)
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

func testPoisonAcknowledgementOrder(t *testing.T) {
	w, _ := poisonWorker(t)
	state := detection.NewPartitionStateWithBounds(w.rules, w.bounds)
	for _, offset := range []int64{-1, math.MaxInt64} {
		before, err := state.Checkpoint()
		require.NoError(t, err)
		require.Error(t, state.AcknowledgePoison(offset))
		after, err := state.Checkpoint()
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
	r := poisonEvent(t, 0, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	var e contracts.Event
	require.NoError(t, json.Unmarshal(r.Value, &e))
	_, err := state.ApplySource(detection.TopicPartition{Topic: "events"}, 0, &e)
	require.NoError(t, err)
	windows, ledger, bytes := state.Usage()
	require.Error(t, state.AcknowledgePoison(1), "pending valid effects cannot be acknowledged as poison")
	afterWindows, afterLedger, afterBytes := state.Usage()
	require.Equal(t, []int{windows, ledger, bytes}, []int{afterWindows, afterLedger, afterBytes})
	require.NoError(t, state.Acknowledge(0))
	require.Error(t, state.AcknowledgePoison(0), "duplicate acknowledgement is invalid")
}

func testDetectorLivePoisonPin(t *testing.T) {
	w, c := poisonWorker(t)
	valid := poisonEvent(t, 0, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	var event contracts.Event
	require.NoError(t, json.Unmarshal(valid.Value, &event))
	principal := "live"
	event.EventType, event.Principal = "auth.failure", &principal
	valid.Value, _ = json.Marshal(event)
	require.NoError(t, w.process(t.Context(), []*kgo.Record{valid}))
	tp := detection.TopicPartition{Topic: "events"}
	before := poisonMetadata(t, c.committed[tp])
	w.WithQuarantine(poisonPublisher{func(context.Context, *kgo.Record, kafkax.DLQReason) error { return nil }})
	require.NoError(t, w.process(t.Context(), []*kgo.Record{{Topic: "events", Offset: 1, Value: []byte("{")}}))
	cp := c.committed[tp]
	require.Zero(t, cp.Offset, "unexpired contributor still pins replay before consumed-next")
	after := poisonMetadata(t, cp)
	require.Equal(t, before.PrefixMaxEventTime, after.PrefixMaxEventTime)
	require.Equal(t, before.SealedThrough, after.SealedThrough)
	require.Equal(t, before.DecisionFrontier, after.DecisionFrontier)
	require.Equal(t, int64(2), after.ProcessedNext)
	w.WithQuarantine(poisonPublisher{func(context.Context, *kgo.Record, kafkax.DLQReason) error {
		return errors.New("outage")
	}})
	require.Error(t, w.process(t.Context(), []*kgo.Record{{Topic: "events", Offset: 2, Value: []byte("{")}}))
	require.Equal(t, cp, c.committed[tp], "unacknowledged poison cannot alter durable pin or metadata")
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
	for _, mode := range []string{"slow", "shared retries", "parent cancellation", "shutdown deadline"} {
		t.Run(mode, func(t *testing.T) { testWorkerAggregateBudget(t, mode) })
	}
}

func testWorkerAggregateBudget(t *testing.T, mode string) {
	w, c := poisonWorker(t)
	var records []*kgo.Record
	for n := range int64(400) {
		records = append(records, &kgo.Record{Topic: "events", Offset: n, Value: []byte("{")})
	}
	attempts := make(map[int64]int)
	acks := 0
	ctx, cancel := context.WithCancel(t.Context())
	if mode == "shutdown deadline" {
		cancel()
		ctx, cancel = context.WithTimeout(t.Context(), 75*time.Millisecond)
	}
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
		if mode == "slow" || mode == "shutdown deadline" {
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
}

type recordingWaiter struct {
	ceilings []time.Duration
	fail     error
}

func (r *recordingWaiter) wait(_ context.Context, ceiling time.Duration) error {
	r.ceilings = append(r.ceilings, ceiling)
	return r.fail
}

func jitterWorker(t *testing.T, waiter *recordingWaiter) (*Worker, *poisonConsumer) {
	t.Helper()
	w, c := poisonWorker(t)
	w.settings.MaxAttempts = 3
	w.settings.RetryMinBackoff = 100 * time.Millisecond
	w.settings.RetryMaxBackoff = 150 * time.Millisecond
	return w.WithRetryWaiter(waiter.wait), c
}

func TestDetectorQuarantineUsesJitteredWaiter(t *testing.T) {
	t.Run("transient failures recover within ceilings", func(t *testing.T) {
		waiter := &recordingWaiter{}
		w, c := jitterWorker(t, waiter)
		calls := 0
		w.WithQuarantine(poisonPublisher{func(context.Context, *kgo.Record, kafkax.DLQReason) error {
			calls++
			if calls <= 2 {
				return errors.New("dlq recovering")
			}
			return nil
		}})
		require.NoError(t, w.process(t.Context(), []*kgo.Record{{Topic: "events", Offset: 0, Value: []byte("{")}}))
		require.Equal(t, 3, calls)
		require.Equal(t, []time.Duration{100 * time.Millisecond, 150 * time.Millisecond}, waiter.ceilings,
			"waits receive the capped exponential ceiling, not a fixed timer")
		require.Equal(t, int64(1), c.committed[detection.TopicPartition{Topic: "events"}].Offset)
	})
	t.Run("shared budget is not reset by success", func(t *testing.T) {
		waiter := &recordingWaiter{}
		w, c := jitterWorker(t, waiter)
		attempts := make(map[int64]int)
		w.WithQuarantine(poisonPublisher{func(_ context.Context, r *kgo.Record, _ kafkax.DLQReason) error {
			attempts[r.Offset]++
			if r.Offset == 0 && attempts[0] > 2 {
				return nil
			}
			return errors.New("dlq outage")
		}})
		err := w.process(t.Context(), []*kgo.Record{
			{Topic: "events", Offset: 0, Value: []byte("{")}, {Topic: "events", Offset: 1, Value: []byte("{")},
		})
		require.Error(t, err)
		require.Equal(t, map[int64]int{0: 3, 1: 1}, attempts)
		require.Len(t, waiter.ceilings, 2, "remaining allowance is shared across the batch")
		require.Equal(t, int64(1), c.committed[detection.TopicPartition{Topic: "events"}].Offset)
	})
	t.Run("wait failure is terminal without acknowledging poison", func(t *testing.T) {
		waiter := &recordingWaiter{fail: errors.New("wait aborted")}
		w, c := jitterWorker(t, waiter)
		calls := 0
		w.WithQuarantine(poisonPublisher{func(context.Context, *kgo.Record, kafkax.DLQReason) error {
			calls++
			return errors.New("dlq outage")
		}})
		require.Error(t, w.process(t.Context(), []*kgo.Record{{Topic: "events", Offset: 0, Value: []byte("{")}}))
		require.Equal(t, 1, calls)
		require.Len(t, waiter.ceilings, 1)
		cp, ok := c.committed[detection.TopicPartition{Topic: "events"}]
		if ok {
			require.Zero(t, cp.Offset, "unacknowledged poison cannot advance the source offset")
		}
	})
}

type allowAllDecisions struct{ decisionStore }

func (allowAllDecisions) CheckpointCommitted(context.Context, postgres.DetectionOwnership, detection.Checkpoint) error {
	return nil
}

func (allowAllDecisions) Decide(_ context.Context, _ postgres.DetectionOwnership,
	candidate detection.Candidate) (postgres.DetectionDecision, error) {
	return postgres.DetectionDecision{Allowed: true, Canonical: candidate.CanonicalBytes()}, nil
}

type ackPublisher struct{ published [][]byte }

func (p *ackPublisher) PublishCanonical(_ context.Context, canonical []byte) error {
	p.published = append(p.published, canonical)
	return nil
}

type recordedPositions struct{ calls map[int32][]int64 }

func (p *recordedPositions) Processed(partition int32, next int64) {
	if p.calls == nil {
		p.calls = make(map[int32][]int64)
	}
	p.calls[partition] = append(p.calls[partition], next)
}

type failingCommitConsumer struct{ *poisonConsumer }

func (failingCommitConsumer) CommitCheckpoints(context.Context,
	map[detection.TopicPartition]detection.Checkpoint) error {
	return errors.New("kafka commit unavailable")
}

func gatherValue(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			matched := 0
			for _, pair := range metric.GetLabel() {
				if labels[pair.GetName()] == pair.GetValue() {
					matched++
				}
			}
			if matched == len(labels) {
				return metric
			}
		}
	}
	return nil
}

// alertBatch builds ten AUTH failures crossing the threshold at at, then one
// valid input more than the lateness tolerance behind the event-time maximum.
func alertBatch(t *testing.T, at time.Time) []*kgo.Record {
	t.Helper()
	principal := "metrics"
	var records []*kgo.Record
	for n := range int64(11) {
		r := poisonEvent(t, n, at)
		var e contracts.Event
		require.NoError(t, json.Unmarshal(r.Value, &e))
		e.EventID = fmt.Sprintf("00000000-0000-4000-8000-%012d", n+1)
		e.EventType, e.Principal = "auth.failure", &principal
		if n == 10 {
			e.EventType, e.Principal = "auth.success", nil
			e.OccurredAt = contracts.NewTimestamp(at.Add(-detection.Lateness - time.Millisecond))
		}
		raw, err := json.Marshal(e)
		require.NoError(t, err)
		r.Value = raw
		records = append(records, r)
	}
	return records
}

func TestWorkerMetricsAndProcessedPosition(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	w, c := poisonWorker(t)
	reg := telemetry.NewRegistry()
	positions := &recordedPositions{}
	publisher := &ackPublisher{}
	w.publisher = publisher
	w.decisions = allowAllDecisions{}
	w.now = func() time.Time { return at.Add(2500 * time.Millisecond) }
	require.Same(t, w, w.WithMetrics(telemetry.NewDetectorMetrics(reg)).WithPositions(positions))

	require.NoError(t, w.process(t.Context(), alertBatch(t, at)))
	require.Len(t, publisher.published, 1)
	require.Equal(t, int64(11), poisonMetadata(t, c.committed[detection.TopicPartition{Topic: "events"}]).ProcessedNext)

	alerts := gatherValue(t, reg, "sentinel_detection_alerts_total",
		map[string]string{"rule_id": "AUTH_BRUTE_FORCE", "severity": "high"})
	require.NotNil(t, alerts)
	require.InDelta(t, 1, alerts.GetCounter().GetValue(), 0)
	late := gatherValue(t, reg, "sentinel_detection_late_events_total", nil)
	require.NotNil(t, late)
	require.InDelta(t, 1, late.GetCounter().GetValue(), 0)
	delay := gatherValue(t, reg, "sentinel_detection_delay_seconds", nil)
	require.NotNil(t, delay)
	require.EqualValues(t, 1, delay.GetHistogram().GetSampleCount())
	require.InDelta(t, 2.5, delay.GetHistogram().GetSampleSum(), 1e-9, "fake now minus trigger occurred_at")

	windows, ledger, bytes := w.bounds.Usage()
	require.Positive(t, windows)
	for name, want := range map[string]int{"sentinel_detector_state_windows": windows,
		"sentinel_detector_state_ledger_entries": ledger, "sentinel_detector_state_bytes": bytes} {
		got := gatherValue(t, reg, name, nil)
		require.NotNil(t, got, name)
		require.InDelta(t, float64(want), got.GetGauge().GetValue(), 0, name)
	}
	require.Equal(t, map[int32][]int64{0: {11}}, positions.calls,
		"processed position is last offset+1 of each committed partition")
}

func TestWorkerReportsNoPositionWhenCommitFails(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	w, c := poisonWorker(t)
	w.consumer = failingCommitConsumer{c}
	positions := &recordedPositions{}
	w.publisher = &ackPublisher{}
	w.decisions = allowAllDecisions{}
	w.WithMetrics(telemetry.NewDetectorMetrics(telemetry.NewRegistry())).WithPositions(positions)

	require.ErrorContains(t, w.process(t.Context(), alertBatch(t, at)), "commit unavailable")
	require.Empty(t, positions.calls, "an uncommitted batch is not a processed position")
}

func TestWorkerWithoutMetricsOrPositionsIsSafe(t *testing.T) {
	w, _ := poisonWorker(t)
	w.publisher = &ackPublisher{}
	w.decisions = allowAllDecisions{}
	require.NoError(t, w.process(t.Context(), alertBatch(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))))
}
