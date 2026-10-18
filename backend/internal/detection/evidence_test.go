package detection_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

func candidateSource() detection.TopicPartition {
	return detection.TopicPartition{Topic: "security.events.v1", Partition: 3}
}

func applySource(
	t *testing.T, state *detection.PartitionState, offset int64, event *contracts.Event,
) detection.Outcome {
	t.Helper()
	out, err := state.ApplySource(candidateSource(), offset, event)
	require.NoError(t, err)
	require.NoError(t, state.Acknowledge(offset))
	return out
}

func TestFrozenEvidence(t *testing.T) {
	for _, kind := range []string{"AUTH", "SCAN"} {
		t.Run(kind, func(t *testing.T) {
			state, out, samples, crossing := evidenceCrossing(t, kind)
			frozen, candidates := out.Alerts[0], out.Candidates
			before, err := json.Marshal(frozen)
			require.NoError(t, err)
			require.NoError(t, frozen.Validate())
			require.Equal(t, crossing, frozen.EventCount, "SCAN counts matching unique events, not predicate ports")
			require.Equal(t, samples, frozen.SampleEventIDs, "first twenty unique IDs follow Kafka order")
			require.Equal(t, frozen.WindowEnd, frozen.CreatedAt)
			require.Equal(t, "open", frozen.Status)
			require.Equal(t, 1, frozen.RuleVersion)
			require.NotContains(t, string(before), "detected_at")
			end := "2026-10-17T12:05:00.000Z"
			if kind == "AUTH" {
				require.Equal(t, "high", frozen.Severity)
				require.Equal(t, "Repeated failed logins against one user", frozen.Summary)
			} else {
				end = "2026-10-17T12:01:00.000Z"
				require.Equal(t, "medium", frozen.Severity)
				require.Equal(t, "Distinct destination ports on one host", frozen.Summary)
			}
			require.Contains(t, string(before), `"created_at":"`+end+`"`)
			for i := range 6 {
				event := authEvent(200 + i)
				if kind == "SCAN" {
					event = scanEvent(200+i, 100+i)
				}
				if i == 0 {
					event.EventID = samples[0]
				}
				event.OccurredAt = frozen.WindowStart
				later := applySource(t, state, int64(crossing+i), event)
				require.Empty(t, later.Alerts)
				require.Empty(t, later.Candidates)
			}
			after, err := json.Marshal(frozen)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Len(t, candidates, 1, "first crossing must freeze canonical candidate bytes")
			require.Equal(t, before, candidates[0].CanonicalBytes())
		})
	}
}

func evidenceCrossing(t *testing.T, kind string) (*detection.PartitionState, detection.Outcome, []string, int) {
	t.Helper()
	rules := scanRules(t)
	crossing := 26
	if kind == "AUTH" {
		rules.Rules[0].Threshold, crossing = 25, 25
	}
	state := detection.NewPartitionState(rules)
	var samples []string
	var out detection.Outcome
	for i := range crossing {
		event := evidenceEvent(kind, i, crossing)
		if i < 20 {
			samples = append(samples, event.EventID)
		}
		out = applySource(t, state, int64(i), event)
		if i < crossing-1 {
			require.Empty(t, out.Alerts)
			require.Empty(t, out.Candidates)
		}
	}
	require.Len(t, out.Alerts, 1)
	return state, out, samples, crossing
}

func evidenceEvent(kind string, index, crossing int) *contracts.Event {
	event := authEvent(100 - index)
	if kind == "SCAN" {
		port := index % 19
		if index == crossing-1 {
			port = 19
		}
		event = scanEvent(100-index, port)
	}
	event.OccurredAt = contracts.NewTimestamp(event.OccurredAt.Add(time.Duration(crossing-index) * time.Millisecond))
	return event
}

func TestEntityIdentity(t *testing.T) {
	for _, principal := range []string{"user|team|東京", "user|team", "user"} {
		t.Run(principal, func(t *testing.T) {
			rules := loadAuthRules(t)
			rules.Rules[0].Threshold = 1
			state := detection.NewPartitionState(rules)
			event := authEvent(1)
			ip := "2001:db8::10"
			event.SourceIP, event.Principal = &ip, &principal
			out := applyAuthEvent(t, state, 0, event)
			require.Len(t, out.Alerts, 1)
			a := out.Alerts[0]
			require.Equal(t, ip+"|"+principal, a.EntityKey)
			want := contracts.DedupeKey("demo", "AUTH_BRUTE_FORCE", []string{ip, principal}, a.WindowStart.Time)
			require.Equal(t, want, a.AlertID)
			displayHash := contracts.DedupeKey("demo", "AUTH_BRUTE_FORCE", []string{a.EntityKey}, a.WindowStart.Time)
			require.NotEqual(t, displayHash, a.AlertID)
		})
	}
	t.Run("IPv6 SCAN components", func(t *testing.T) {
		rules := scanRules(t)
		for i := range rules.Rules {
			if rules.Rules[i].ID == "PORT_SCAN" {
				rules.Rules[i].Threshold = 1
			}
		}
		event := scanEvent(1, 443)
		source, destination := "2001:db8::10", "2001:db8::20"
		event.SourceIP, event.DestinationIP = &source, &destination
		out := applyAuthEvent(t, detection.NewPartitionState(rules), 0, event)
		require.Len(t, out.Alerts, 1)
		a := out.Alerts[0]
		want := contracts.DedupeKey("demo", "PORT_SCAN", []string{source, destination}, a.WindowStart.Time)
		require.Equal(t, want, a.AlertID)
	})
}
