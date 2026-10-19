package detection //nolint:testpackage // exact live-window state is a reconstruction proof obligation.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

func replayStream(seed uint64) []*contracts.Event {
	// Closed SCAN/DENY history overlaps a still-live AUTH pin; after advancing
	// the pin its suffix must not recreate a partial historical detection.
	events := []*contracts.Event{
		checkpointEvent(501, 1000, 1), checkpointEvent(502, 5000, 0),
		checkpointEvent(503, 1000, 1), checkpointEvent(504, 1000, 1),
		checkpointEvent(505, 1000, 3), checkpointEvent(506, 1000, 3), checkpointEvent(507, 1000, 3),
		checkpointEvent(508, 61000, 1), checkpointEvent(509, 150000, 4),
		checkpointEvent(1, 280000, 0), checkpointEvent(2, 300000, 4), checkpointEvent(3, 280000, 0),
		checkpointEvent(4, 300000, 4), checkpointEvent(5, 280000, 0), checkpointEvent(6, 269999, 0),
		checkpointEvent(7, 280000, 0), checkpointEvent(7, 280000, 0), checkpointEvent(8, 270000, 0),
	}
	rng := rand.New(rand.NewPCG(seed, seed+17))
	for i := range 160 {
		at := int64(300000+i*2300) - int64(rng.IntN(45000))
		e := checkpointEvent(i+20, at, rng.IntN(5))
		principal := fmt.Sprintf("entity|%d", rng.IntN(3))
		e.Principal = &principal
		if i%11 == 0 {
			e.EventID = events[len(events)-1].EventID
		}
		events = append(events, e)
	}
	return events
}

func equalReplayState(t *testing.T, expected, actual *PartitionState) {
	t.Helper()
	require.Equal(t, expected.windows, actual.windows, "counts, IDs, ports, first offsets and frozen evidence")
	require.Equal(t, expected.maximum, actual.maximum)
	require.Equal(t, expected.sealed, actual.sealed)
	require.Equal(t, expected.frontier, actual.frontier)
	a, err := expected.Checkpoint()
	require.NoError(t, err)
	b, err := actual.Checkpoint()
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func replayAt(t *testing.T, s *PartitionState, events []*contracts.Event, through int,
	outputs map[string]string) *PartitionState {
	t.Helper()
	cp, err := s.Checkpoint()
	require.NoError(t, err)
	s.Committed(cp)
	restored := NewPartitionState(s.rules)
	require.NoError(t, restored.Restore(cp))
	for offset := cp.Offset; offset < int64(through); offset++ {
		checkpointApply(t, restored, offset, events[offset], outputs)
	}
	equalReplayState(t, s, restored)
	return restored
}

func TestReplayEveryRestartCut(t *testing.T) {
	t.Run("shared frozen fixtures", replaySharedFixtures)
	for seed := uint64(1); seed <= 6; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			events := replayStream(seed)
			baseline := NewPartitionState(checkpointRules())
			expected := make(map[string]string)
			for i, e := range events {
				checkpointApply(t, baseline, int64(i), e, expected)
			}
			require.NotEmpty(t, expected, "fixture must cross actual thresholds")
			replayAllCuts(t, baseline, events, expected)
			// Repeated restarts on one run exercise successive advancing pins.
			s := NewPartitionState(checkpointRules())
			actual := make(map[string]string)
			pins := make(map[int64]bool)
			for i, e := range events {
				checkpointApply(t, s, int64(i), e, actual)
				if i%7 == 0 {
					cp, err := s.Checkpoint()
					require.NoError(t, err)
					pins[cp.Offset] = true
					s = replayAt(t, s, events, i+1, actual)
				}
			}
			require.Greater(t, len(pins), 3, "fixture must advance pin repeatedly")
			require.Equal(t, expected, actual)
			equalReplayState(t, baseline, s)
		})
	}
}

func replaySharedFixtures(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("../../../testdata/detection/scenarios.json")
	require.NoError(t, err)
	var fixture struct {
		Cases []struct {
			Name     string             `json:"name"`
			Events   []*contracts.Event `json:"events"`
			Expected []struct {
				Payload contracts.Alert `json:"payload"`
			} `json:"expected_alerts"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	config, err := os.ReadFile("../../../config/rules.yaml")
	require.NoError(t, err)
	rules, err := LoadRules(bytes.NewReader(config))
	require.NoError(t, err)
	require.NotEmpty(t, fixture.Cases)
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			baseline := NewPartitionState(rules)
			expected := make(map[string]string)
			for i, e := range tc.Events {
				checkpointApply(t, baseline, int64(i), e, expected)
			}
			require.Len(t, expected, len(tc.Expected))
			for _, a := range tc.Expected {
				require.Contains(t, expected, a.Payload.AlertID)
			}
			replayAllCuts(t, baseline, tc.Events, expected)
		})
	}
}

func replayAllCuts(t *testing.T, baseline *PartitionState, events []*contracts.Event, expected map[string]string) {
	t.Helper()
	for cut := 0; cut <= len(events); cut++ {
		s := NewPartitionState(baseline.rules)
		outputs := make(map[string]string)
		for i := range cut {
			checkpointApply(t, s, int64(i), events[i], outputs)
		}
		s = replayAt(t, s, events, cut, outputs)
		for i := cut; i < len(events); i++ {
			checkpointApply(t, s, int64(i), events[i], outputs)
		}
		require.Equal(t, expected, outputs, "canonical union at restart cut %d", cut)
		equalReplayState(t, baseline, s)
	}
}
