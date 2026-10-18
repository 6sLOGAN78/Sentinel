package detection_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

func authEvent(id int) *contracts.Event {
	ip, principal := "192.0.2.10", "example|user"
	at := contracts.NewTimestamp(time.Date(2026, time.October, 17, 12, 0, 1, 0, time.UTC))
	return &contracts.Event{
		SchemaVersion: 1, EventID: fmt.Sprintf("00000000-0000-4000-8000-%012d", id),
		TenantID: "demo", EventType: "auth.failure", Source: "auth-fixture",
		OccurredAt: at, ReceivedAt: &at, SourceIP: &ip, Principal: &principal,
	}
}

func applyAuthEvent(
	t *testing.T, state *detection.PartitionState, offset int64, event *contracts.Event,
) detection.Outcome {
	t.Helper()
	out, err := state.Apply(offset, event)
	require.NoError(t, err)
	require.False(t, out.Late)
	require.NoError(t, state.Acknowledge(offset))
	return out
}

// Counting duplicate IDs, triggering at nine or grouping missing fields into an
// empty principal/source bucket must break these observable threshold checks.
func TestAuthThreshold(t *testing.T) {
	t.Run("nine duplicate tenth and repeat", func(t *testing.T) {
		state := detection.NewPartitionState(loadAuthRules(t))
		for i := range 9 {
			require.Empty(t, applyAuthEvent(t, state, int64(i), authEvent(i+1)).Alerts)
		}
		require.Empty(t, applyAuthEvent(t, state, 9, authEvent(1)).Alerts, "a duplicate is not the tenth contributor")
		out := applyAuthEvent(t, state, 10, authEvent(10))
		require.Len(t, out.Alerts, 1, "ten unique failures must alert")
		require.Equal(t, 10, out.Alerts[0].EventCount)
		require.Empty(t, applyAuthEvent(t, state, 11, authEvent(11)).Alerts, "one hit per window")
	})
	for _, name := range []string{"null source", "null principal", "empty principal", "wrong event type"} {
		t.Run(name, func(t *testing.T) {
			state := detection.NewPartitionState(loadAuthRules(t))
			for i := range 10 {
				event := authEvent(i + 1)
				switch name {
				case "null source":
					event.SourceIP = nil
				case "null principal":
					event.Principal = nil
				case "empty principal":
					empty := ""
					event.Principal = &empty
				case "wrong event type":
					event.EventType = "auth.success"
				}
				require.Empty(t, applyAuthEvent(t, state, int64(i), event).Alerts)
			}
			windows, _, _ := state.Usage()
			require.Zero(t, windows, "nonmatching input must not create a group")
		})
	}
}

// Using wall time, sorting samples, including duplicate evidence, changing the
// first-threshold count or allowing later input to mutate emitted bytes must fail.
func TestAuthCanonicalEvidence(t *testing.T) {
	for _, threshold := range []int{10, 25} {
		t.Run(fmt.Sprintf("threshold %d", threshold), func(t *testing.T) {
			rules := loadAuthRules(t)
			rules.Rules[0].Threshold = threshold
			state := detection.NewPartitionState(rules)
			var alert *contracts.Alert
			var samples []string
			for i := range threshold {
				event := authEvent(threshold - i)
				// Deliberately reverse event times and IDs: samples follow source order.
				event.OccurredAt = contracts.NewTimestamp(event.OccurredAt.Add(time.Duration(threshold-i) * time.Millisecond))
				if i < 20 {
					samples = append(samples, event.EventID)
				}
				out := applyAuthEvent(t, state, int64(i), event)
				if i < threshold-1 {
					require.Empty(t, out.Alerts)
				} else {
					require.Len(t, out.Alerts, 1)
					alert = out.Alerts[0]
				}
			}
			start := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
			end := contracts.NewTimestamp(start.Add(5 * time.Minute))
			require.Equal(t, &contracts.Alert{
				SchemaVersion: 1,
				AlertID:       contracts.DedupeKey("demo", "AUTH_BRUTE_FORCE", []string{"192.0.2.10", "example|user"}, start),
				TenantID:      "demo", RuleID: "AUTH_BRUTE_FORCE", RuleVersion: 1, Severity: "high", Status: "open",
				CreatedAt: end, WindowStart: contracts.NewTimestamp(start), WindowEnd: end,
				EntityKey: "192.0.2.10|example|user", Summary: "Repeated failed logins against one user",
				EventCount: threshold, SampleEventIDs: samples,
				DedupeKey: "demo|AUTH_BRUTE_FORCE|192.0.2.10|example|user|2026-10-17T12:00:00Z",
			}, alert)
			require.NoError(t, alert.Validate())
			before, err := json.Marshal(alert)
			require.NoError(t, err)
			require.NotContains(t, string(before), "detected_at")
			require.Contains(t, string(before), `"created_at":"2026-10-17T12:05:00.000Z"`)
			for i := range 5 {
				require.Empty(t, applyAuthEvent(t, state, int64(threshold+i), authEvent(threshold+i+1)).Alerts)
			}
			after, err := json.Marshal(alert)
			require.NoError(t, err)
			require.Equal(t, before, after, "later matches must preserve frozen first-threshold evidence")
		})
	}
}
