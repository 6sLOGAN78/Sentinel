package detection_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

func TestWindowBoundary(t *testing.T) {
	rules := loadAuthRules(t)
	rules.Rules[0].Threshold = 1
	state := detection.NewPartitionState(rules)
	start := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	var ids []string
	for i, delta := range []time.Duration{0, 5*time.Minute - time.Millisecond, 5 * time.Minute} {
		event := authEvent(i + 1)
		event.OccurredAt = contracts.NewTimestamp(start.Add(delta).In(time.FixedZone("fixture", 19800)))
		out := applyAuthEvent(t, state, int64(i), event)
		if i == 1 {
			require.Empty(t, out.Alerts, "end-1ms still belongs to first fired window")
			continue
		}
		require.Len(t, out.Alerts, 1)
		a := out.Alerts[0]
		wantStart := start
		if i == 2 {
			wantStart = start.Add(5 * time.Minute)
		}
		require.Equal(t, contracts.NewTimestamp(wantStart), a.WindowStart)
		require.Equal(t, contracts.NewTimestamp(wantStart.Add(5*time.Minute)), a.WindowEnd)
		require.Equal(t, a.WindowEnd, a.CreatedAt)
		raw, err := json.Marshal(a)
		require.NoError(t, err)
		require.Contains(t, string(raw), ".000Z")
		ids = append(ids, a.AlertID)
	}
	require.NotEqual(t, ids[0], ids[1])
}

func TestWatermarkLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		behind time.Duration
		late   bool
	}{
		{"equal accepted", 30 * time.Second, false},
		{"one millisecond before dropped", 30*time.Second + time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := loadAuthRules(t)
			rules.Rules[0].Threshold = 1
			state := detection.NewPartitionState(rules)
			marker := authEvent(1)
			marker.EventType = "auth.success"
			marker.OccurredAt = contracts.NewTimestamp(marker.OccurredAt.Add(time.Minute))
			require.Empty(t, applyAuthEvent(t, state, 0, marker).Alerts)
			event := authEvent(2)
			event.OccurredAt = contracts.NewTimestamp(marker.OccurredAt.Add(-tc.behind))
			out, err := state.Apply(1, event)
			require.NoError(t, err)
			require.Equal(t, tc.late, out.Late)
			if tc.late {
				require.Empty(t, out.Alerts)
			} else {
				require.Len(t, out.Alerts, 1)
			}
			require.NoError(t, state.Acknowledge(1))
		})
	}
	t.Run("poison and idle do not advance event time", func(t *testing.T) {
		state := detection.NewPartitionState(loadAuthRules(t))
		require.Empty(t, applyAuthEvent(t, state, 0, authEvent(1)).Alerts)
		before, err := state.Checkpoint()
		require.NoError(t, err)
		_, err = contracts.DecodeEvent(strings.NewReader(`{"occurred_at":"2099-01-01T00:00:00.000Z","poison":true}`))
		require.Error(t, err, "poison never enters the reducer")
		for range 3 {
			cp, checkpointErr := state.Checkpoint()
			require.NoError(t, checkpointErr)
			require.Equal(t, before, cp, "idle snapshots cannot consult wall clock or retire pins")
		}
		out := applyAuthEvent(t, state, 1, authEvent(2))
		require.False(t, out.Late, "historical fixture remains accepted regardless of current wall clock")
		windows, _, _ := state.Usage()
		require.Equal(t, 1, windows)
	})
}
