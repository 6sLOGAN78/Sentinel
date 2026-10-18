package detection_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

func TestCandidateFreeze(t *testing.T) {
	t.Run("actual source and trigger clock", func(t *testing.T) {
		rules := loadAuthRules(t)
		rules.Rules[0].Threshold = 2
		state := detection.NewPartitionState(rules)
		first, trigger := authEvent(1), authEvent(2)
		first.OccurredAt = contracts.NewTimestamp(first.OccurredAt.Add(9 * time.Second))
		require.Empty(t, applySource(t, state, 40, first).Candidates)
		out := applySource(t, state, 41, trigger)
		require.Len(t, out.Candidates, 1, "freeze a candidate at first crossing")
		c := out.Candidates[0]
		require.Equal(t, candidateSource(), c.Source())
		require.Equal(t, int64(41), c.Offset())
		require.Equal(t, 0, c.RuleOrdinal())
		require.Equal(t, trigger.OccurredAt, c.TriggerOccurredAt(), "cooldown uses crossing event time even out of order")
		require.NotEqual(t, out.Alerts[0].CreatedAt, c.TriggerOccurredAt())
		before, err := json.Marshal(out.Alerts[0])
		require.NoError(t, err)
		require.Equal(t, before, c.CanonicalBytes())
		want, err := storageclickhouse.ImmutableFingerprint(out.Alerts[0])
		require.NoError(t, err)
		require.Equal(t, want, c.Fingerprint(), "same immutable-content policy as persistence")
		copyBytes := c.CanonicalBytes()
		copyBytes[0] = '!'
		out.Alerts[0].SampleEventIDs[0] = authEvent(999).EventID
		out.Alerts[0].Summary = "consumer mutation"
		require.Equal(t, before, c.CanonicalBytes(), "neither returned bytes nor legacy alert can mutate the candidate")
		require.Equal(t, want, c.Fingerprint())
	})
	t.Run("fixed rule ordinals independent of config order", func(t *testing.T) {
		rules := volumeRules(t)
		for i := range rules.Rules {
			rules.Rules[i].Threshold = 1
		}
		slices.Reverse(rules.Rules)
		state := detection.NewPartitionState(rules)
		for i := range rules.Rules {
			rules.Rules[i].Enabled = true
		}
		events := []*contracts.Event{
			authEvent(1), scanEvent(2, 443), volumeEvent(3, "REQUEST_FLOOD"), volumeEvent(4, "DENY_SPIKE"),
		}
		for ordinal, event := range events {
			out := applySource(t, state, int64(ordinal), event)
			require.Len(t, out.Candidates, 1)
			require.Equal(t, ordinal, out.Candidates[0].RuleOrdinal(), "AUTH/SCAN/FLOOD/DENY ordinals are 0/1/2/3")
		}
	})
	t.Run("one crossing survives repeats expiry and separate windows", func(t *testing.T) {
		rules := loadAuthRules(t)
		rules.Rules[0].Threshold, rules.Rules[0].Window = 1, time.Second
		state := detection.NewPartitionState(rules)
		first := applySource(t, state, 0, authEvent(1))
		require.Len(t, first.Candidates, 1)
		cp, err := state.Checkpoint()
		require.NoError(t, err)
		require.Zero(t, cp.Offset, "fired state retains its original pin")
		for i := range 3 {
			event := authEvent(i + 2)
			event.OccurredAt = contracts.NewTimestamp(event.OccurredAt.Add(time.Duration(i) * 100 * time.Millisecond))
			require.Empty(t, applySource(t, state, int64(i+1), event).Candidates)
		}
		next := authEvent(10)
		next.OccurredAt = contracts.NewTimestamp(next.OccurredAt.Add(time.Second))
		second := applySource(t, state, 4, next)
		require.Len(t, second.Candidates, 1, "no volatile cross-window cooldown decision")
		require.NotEqual(t, first.Alerts[0].AlertID, second.Alerts[0].AlertID)
		marker := authEvent(11)
		marker.EventType = "auth.success"
		marker.OccurredAt = contracts.NewTimestamp(first.Alerts[0].WindowEnd.Add(detection.Lateness))
		require.Empty(t, applySource(t, state, 5, marker).Candidates)
		old := authEvent(12)
		old.OccurredAt = first.Alerts[0].WindowEnd
		old.OccurredAt = contracts.NewTimestamp(old.OccurredAt.Add(-time.Millisecond))
		out := applySource(t, state, 6, old)
		require.True(t, out.Late)
		require.Empty(t, out.Candidates, "expired window cannot retry a decision")
		require.Empty(t, out.Alerts)
	})
	t.Run("invalid source rejected before state changes", func(t *testing.T) {
		for _, source := range []detection.TopicPartition{{Partition: 1}, {Topic: candidateSource().Topic, Partition: -1}} {
			state := detection.NewPartitionState(loadAuthRules(t))
			_, err := state.ApplySource(source, 0, authEvent(1))
			require.Error(t, err)
			windows, ledger, _ := state.Usage()
			require.Zero(t, windows)
			require.Zero(t, ledger)
		}
	})
}
