package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

func TestDetectionStateClientBounds(t *testing.T) {
	t.Setenv("SENTINEL_DETECTOR.RULES_PATH", "../../../config/rules.yaml")
	t.Setenv("SENTINEL_KAFKA.BROKERS", "127.0.0.1:29092")
	require.LessOrEqual(t, scanFetchBytes, observerReadBytes)
	source, err := newKafkaObserver()
	require.NoError(t, err, "bounded admin client must construct before network observation")
	source.client.Close()
}

type fakeObserver struct {
	bounds  map[int32]history
	cps     map[int32]*detection.Checkpoint
	records []*kgo.Record
	moving  bool
	reads   int
}

func (*fakeObserver) Fingerprint() string { return strings.Repeat("a", 64) }

func (f *fakeObserver) History(context.Context) (map[int32]history, error) {
	f.reads++
	out := make(map[int32]history)
	for p, b := range f.bounds {
		out[p] = b
	}
	if f.moving && f.reads > 1 {
		out[0] = history{0, 2}
	}
	return out, nil
}
func (f *fakeObserver) Checkpoints(context.Context) (map[int32]*detection.Checkpoint, error) {
	return f.cps, nil
}
func (f *fakeObserver) Scan(ctx context.Context, _ int32, start, end int64, visit func(*kgo.Record) error) error {
	for _, r := range f.records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if r.Offset >= start && r.Offset < end {
			if err := visit(r); err != nil {
				return err
			}
		}
	}
	return nil
}
func fixtureCP(pin, next int64, prefix, sealed *int64, drained bool) *detection.Checkpoint {
	m := detection.Metadata{Format: 2, ConfigFingerprint: strings.Repeat("a", 64),
		RuleVersions:      map[string]int{"AUTH_BRUTE_FORCE": 1, "PORT_SCAN": 1, "REQUEST_FLOOD": 1, "DENY_SPIKE": 1},
		RuleEffectiveFrom: map[string]int64{"AUTH_BRUTE_FORCE": 0, "PORT_SCAN": 0, "REQUEST_FLOOD": 0, "DENY_SPIKE": 0},
		DecisionFrontier:  detection.DecisionFrontier{None: true}, PrefixMaxEventTime: prefix, SealedThrough: sealed,
		ProcessedNext: next, Drained: drained}
	b, _ := json.Marshal(m)
	return &detection.Checkpoint{Offset: pin, Metadata: string(b)}
}
func intPointer(v int64) *int64 { return &v }
func emptyObserver() *fakeObserver {
	f := &fakeObserver{bounds: make(map[int32]history), cps: make(map[int32]*detection.Checkpoint)}
	for p := range int32(6) {
		f.bounds[p] = history{0, 0}
		f.cps[p] = fixtureCP(0, 0, nil, nil, true)
	}
	return f
}

//nolint:gocognit // Independent named proof cases share the fake source and clock.
func TestDetectionStateSnapshot(t *testing.T) {
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		pin, next int64
		prefix    *int64
		empty     bool
	}{
		{"explicit_empty", 0, 0, nil, true}, {"initialized_idle", 3, 3, intPointer(start.UnixMilli() - 5000), false},
		{"pin_is_not_processed_next", 1, 3, intPointer(start.UnixMilli() - 5000), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := emptyObserver()
			f.bounds[0] = history{0, tc.next}
			var floor *int64
			if !tc.empty {
				floor = intPointer(start.UnixMilli() - 30000)
			}
			f.cps[0] = fixtureCP(tc.pin, tc.next, tc.prefix, floor, tc.pin == tc.next)
			got, err := caughtUp(t.Context(), f, []string{"demo|192.0.2.11"}, start, start.Add(15*time.Minute),
				func() time.Time { return start }, func(context.Context, time.Duration) error { return nil })
			require.NoError(t, err)
			require.Len(t, got.SourceSnapshots, 6)
			require.Equal(t, tc.empty, got.SourceSnapshots[0].Empty)
			if tc.empty {
				require.Nil(t, got.SourceSnapshots[0].MaxEventTime)
			} else {
				require.Equal(t, start.Format(time.RFC3339Nano), *got.SourceSnapshots[0].MaxEventTime)
			}
			p := kgo.StickyKeyPartitioner(nil).ForTopic("events").Partition(&kgo.Record{Key: []byte("demo|192.0.2.11")}, 6)
			require.Equal(t, p, got.SourcePartitions["demo|192.0.2.11"])
		})
	}
	t.Run("catchup_waits_for_processed_not_pin", func(t *testing.T) {
		f := emptyObserver()
		f.bounds[0] = history{0, 3}
		f.cps[0] = fixtureCP(0, 1, nil, intPointer(start.UnixMilli()-30000), false)
		clock := start
		got, err := caughtUp(t.Context(), f, []string{"demo|192.0.2.11"}, start, start.Add(15*time.Minute),
			func() time.Time { return clock }, func(_ context.Context, d time.Duration) error {
				require.LessOrEqual(t, d, time.Second)
				clock = clock.Add(d)
				f.cps[0] = fixtureCP(0, 3, nil, intPointer(start.UnixMilli()-30000), false)
				return nil
			})
		require.NoError(t, err)
		require.Equal(t, int64(3), got.SourceSnapshots[0].ProcessedNext)
		require.Greater(t, clock, start)
	})
	t.Run("missing_metadata_reconstructs_but_waits_for_detector", func(t *testing.T) {
		f := emptyObserver()
		f.bounds[0] = history{0, 1}
		f.cps[0] = nil
		f.records = []*kgo.Record{snapshotEvent(t, 0, start)}
		clock := start
		waits := 0
		got, err := caughtUp(t.Context(), f, []string{"demo|192.0.2.11"}, start, start.Add(runBudget),
			func() time.Time { return clock }, func(_ context.Context, d time.Duration) error {
				clock = clock.Add(d)
				waits++
				f.cps[0] = fixtureCP(1, 1, intPointer(start.UnixMilli()), intPointer(start.UnixMilli()-30000), true)
				return nil
			})
		require.NoError(t, err)
		require.Equal(t, 1, waits)
		require.Equal(t, start.Format(time.RFC3339Nano), *got.SourceSnapshots[0].MaxEventTime)
	})
	for _, defect := range []string{"corrupt", "trimmed", "missing_unsafe", "moving", "missing_metadata", "timeout"} {
		t.Run(defect, func(t *testing.T) {
			f := emptyObserver()
			f.bounds[0] = history{0, 1}
			f.cps[0] = fixtureCP(0, 1, nil, intPointer(start.UnixMilli()-30000), false)
			switch defect {
			case "corrupt":
				f.cps[0].Metadata = `{"format":2}`
			case "trimmed":
				f.bounds[0] = history{1, 1}
			case "missing_unsafe":
				f.cps[0] = nil
				f.bounds[0] = history{1, 1}
			case "missing_metadata":
				f.cps[0] = nil
			case "moving":
				f.moving = true
			case "timeout":
				f.cps[0] = fixtureCP(0, 0, nil, nil, true)
			}
			clock := start
			waits := 0
			_, err := caughtUp(t.Context(), f, []string{"demo|192.0.2.11"}, start, start.Add(15*time.Minute),
				func() time.Time { return clock }, func(_ context.Context, d time.Duration) error {
					clock = clock.Add(d)
					waits++
					return nil
				})
			require.Error(t, err)
			require.LessOrEqual(t, waits, 60)
		})
	}
	t.Run("bounded_reconstruction_includes_prefix", func(t *testing.T) {
		f := emptyObserver()
		f.records = []*kgo.Record{snapshotEvent(t, 1, start.Add(-time.Second)),
			snapshotEvent(t, 2, start.Add(-2*time.Second))}
		maximum, err := reconstruct(t.Context(), f, 0, 1, 3, intPointer(start.UnixMilli()))
		require.NoError(t, err)
		require.Equal(t, start.UnixMilli(), *maximum)
		maximum, err = reconstruct(t.Context(), f, 0, 1, 3, nil)
		require.NoError(t, err)
		require.Equal(t, start.Add(-time.Second).UnixMilli(), *maximum)
	})
	t.Run("scan_limits_and_gaps", func(t *testing.T) {
		for _, kind := range []string{"record_cap", "byte_cap", "gap", "canceled"} {
			t.Run(kind, func(t *testing.T) {
				f := emptyObserver()
				f.records = []*kgo.Record{snapshotEvent(t, 0, start)}
				end := int64(1)
				ctx := t.Context()
				if kind == "record_cap" {
					end = maxScanRecords + 1
				}
				if kind == "byte_cap" {
					f.records[0].Value = make([]byte, maxScanBytes+1)
				}
				if kind == "gap" {
					end = 2
				}
				if kind == "canceled" {
					cancelCtx, cancel := context.WithCancel(ctx)
					cancel()
					ctx = cancelCtx
				}
				_, err := reconstruct(ctx, f, 0, 0, end, nil)
				require.Error(t, err)
			})
		}
	})
	t.Run("private_protocol_refuses_unbounded_or_nonquiescent", func(t *testing.T) {
		for _, input := range []string{`{}`, `{"producers_quiescent":false}`, strings.Repeat("x", 65537)} {
			err := runProtocol(t.Context(), strings.NewReader(input), &strings.Builder{}, emptyObserver())
			require.Error(t, err)
		}
	})
	t.Run("private_protocol_roundtrip_and_ambiguity", func(t *testing.T) {
		at := time.Now().UTC().Truncate(time.Millisecond)
		body, err := json.Marshal(snapshotRequest{SchemaVersion: 1, ProducersQuiescent: true,
			SourceKeys: []string{"demo|192.0.2.11"}, StartedAt: at, Deadline: at.Add(runBudget)})
		require.NoError(t, err)
		var output strings.Builder
		require.NoError(t, runProtocol(t.Context(), strings.NewReader(string(body)), &output, emptyObserver()))
		var result snapshot
		require.NoError(t, json.Unmarshal([]byte(output.String()), &result))
		require.Len(t, result.SourceSnapshots, 6)
		duplicate := strings.Replace(string(body), `"producers_quiescent":true`,
			`"producers_quiescent":false,"producers_quiescent":true`, 1)
		require.Error(t, runProtocol(t.Context(), strings.NewReader(duplicate), &strings.Builder{}, emptyObserver()))
	})
	t.Run("cancellation_interrupts_wait", func(t *testing.T) {
		f := emptyObserver()
		f.bounds[0] = history{0, 1}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := caughtUp(ctx, f, nil, start, start.Add(15*time.Minute), func() time.Time { return start },
			func(context.Context, time.Duration) error { return errors.New("canceled") })
		require.Error(t, err)
	})
}

func snapshotEvent(t *testing.T, offset int64, at time.Time) *kgo.Record {
	t.Helper()
	source := "192.0.2.11"
	received := contracts.NewTimestamp(at)
	b, err := json.Marshal(contracts.Event{SchemaVersion: 1, EventID: "0199f000-0000-7000-8000-000000000001",
		TenantID: "demo", OccurredAt: received, ReceivedAt: &received, Source: "generator",
		EventType: "auth.success", SourceIP: &source})
	require.NoError(t, err)
	return &kgo.Record{Offset: offset, Key: []byte("demo|192.0.2.11"), Value: b}
}
