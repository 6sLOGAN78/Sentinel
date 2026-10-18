package detection_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

var candidateSource = detection.TopicPartition{Topic: "security.events.v1", Partition: 3}

func applySource(t *testing.T, state *detection.PartitionState, offset int64, event *contracts.Event) detection.Outcome {
	t.Helper()
	out, err := state.ApplySource(candidateSource, offset, event)
	require.NoError(t, err)
	require.NoError(t, state.Acknowledge(offset))
	return out
}

func TestFrozenEvidence(t *testing.T) {
	for _, kind := range []string{"AUTH", "SCAN"} {
		t.Run(kind, func(t *testing.T) {
			rules := scanRules(t)
			threshold, crossing := 25, 25
			if kind == "AUTH" {
				rules.Rules[0].Threshold = threshold
			} else {
				threshold, crossing = 20, 26
			}
			state := detection.NewPartitionState(rules)
			var samples []string
			var frozen *contracts.Alert
			var candidates []detection.Candidate
			var before []byte
			for i := range crossing {
				event := authEvent(100 - i)
				if kind == "SCAN" {
					port := i % 19
					if i == crossing-1 {
						port = 19
					}
					event = scanEvent(100-i, port)
				}
				event.OccurredAt = contracts.NewTimestamp(event.OccurredAt.Add(time.Duration(crossing-i) * time.Millisecond))
				if i < 20 {
					samples = append(samples, event.EventID)
				}
				out := applySource(t, state, int64(i), event)
				if i < crossing-1 {
					require.Empty(t, out.Alerts)
					require.Empty(t, out.Candidates)
				} else {
					require.Len(t, out.Alerts, 1)
					frozen, candidates = out.Alerts[0], out.Candidates
					var err error
					before, err = json.Marshal(frozen)
					require.NoError(t, err)
				}
			}
			require.NoError(t, frozen.Validate())
			require.Equal(t, crossing, frozen.EventCount, "SCAN counts matching unique events, not predicate ports")
			require.Equal(t, samples, frozen.SampleEventIDs, "first twenty unique IDs follow Kafka order")
			require.Equal(t, frozen.WindowEnd, frozen.CreatedAt)
			require.Equal(t, "open", frozen.Status)
			require.Equal(t, 1, frozen.RuleVersion)
			require.NotContains(t, string(before), "detected_at")
			require.Contains(t, string(before), `"created_at":"2026-10-17T12:0`)
			if kind == "AUTH" {
				require.Equal(t, "high", frozen.Severity)
				require.Equal(t, "Repeated failed logins against one user", frozen.Summary)
			} else {
				require.Equal(t, "medium", frozen.Severity)
				require.Equal(t, "Distinct destination ports on one host", frozen.Summary)
			}
			for i := range 6 {
				event := authEvent(200 + i)
				if kind == "SCAN" {
					event = scanEvent(200+i, 100+i)
				}
				if i == 0 {
					event.EventID = samples[0]
				}
				event.OccurredAt = frozen.WindowStart
				out := applySource(t, state, int64(crossing+i), event)
				require.Empty(t, out.Alerts)
				require.Empty(t, out.Candidates)
			}
			after, err := json.Marshal(frozen)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Len(t, candidates, 1, "first crossing must freeze canonical candidate bytes")
			require.Equal(t, before, candidates[0].CanonicalBytes())
		})
	}
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
			require.Equal(t, contracts.DedupeKey("demo", "AUTH_BRUTE_FORCE", []string{ip, principal}, a.WindowStart.Time), a.AlertID)
			require.NotEqual(t, contracts.DedupeKey("demo", "AUTH_BRUTE_FORCE", []string{a.EntityKey}, a.WindowStart.Time), a.AlertID)
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
		require.Equal(t, contracts.DedupeKey("demo", "PORT_SCAN", []string{source, destination}, a.WindowStart.Time), a.AlertID)
	})
}
