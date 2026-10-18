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

func volumeRules(t *testing.T) *detection.Rules {
	t.Helper()
	rules := loadAuthRules(t)
	for i := range rules.Rules {
		rules.Rules[i].Enabled = true
	}
	return rules
}

func volumeEvent(id int, rule string) *contracts.Event {
	event := authEvent(id)
	event.Principal = nil
	event.EventType = "net.request"
	if rule == "DENY_SPIKE" {
		action := "deny"
		event.EventType, event.Action = "policy.decision", &action
	}
	return event
}

// Missing dispatch, wrong predicates, non-unique counts or a two-component
// source identity must break these tests through the public reducer API.
func TestFloodThresholds(t *testing.T) {
	testVolumeThresholds(t, "REQUEST_FLOOD", 300, time.Minute, "high")
}

func TestDenyThresholds(t *testing.T) {
	testVolumeThresholds(t, "DENY_SPIKE", 30, 2*time.Minute, "medium")
	for _, outcome := range []string{"success", "failure", "deny", "allow", ""} {
		t.Run("deny independent of outcome "+outcome, func(t *testing.T) {
			state := detection.NewPartitionState(volumeRules(t))
			var alerts []*contracts.Alert
			for i := range 30 {
				event := volumeEvent(i+1, "DENY_SPIKE")
				event.Outcome = &outcome
				alerts = append(alerts, applyAuthEvent(t, state, int64(i), event).Alerts...)
			}
			require.Len(t, alerts, 1)
			require.Equal(t, "DENY_SPIKE", alerts[0].RuleID)
		})
	}
}

func testVolumeThresholds(t *testing.T, rule string, threshold int, window time.Duration, severity string) {
	t.Helper()
	for _, count := range []int{threshold - 1, threshold, threshold + 1} {
		t.Run(fmt.Sprintf("%d unique", count), func(t *testing.T) {
			state := detection.NewPartitionState(volumeRules(t))
			var alerts []*contracts.Alert
			for i := range count {
				alerts = append(alerts, applyAuthEvent(t, state, int64(i), volumeEvent(i+1, rule)).Alerts...)
			}
			if count < threshold {
				require.Empty(t, alerts)
				return
			}
			require.Len(t, alerts, 1, "first exact threshold must emit one alert")
			a := alerts[0]
			start := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
			require.Equal(t, contracts.DedupeKey("demo", rule, []string{"192.0.2.10"}, start), a.AlertID)
			require.Equal(t, "demo|"+rule+"|192.0.2.10|2026-10-17T12:00:00Z", a.DedupeKey)
			require.Equal(t, "192.0.2.10", a.EntityKey)
			require.Equal(t, rule, a.RuleID)
			require.Equal(t, 1, a.RuleVersion)
			require.Equal(t, severity, a.Severity)
			require.Equal(t, "open", a.Status)
			require.Equal(t, threshold, a.EventCount)
			require.Equal(t, contracts.NewTimestamp(start), a.WindowStart)
			require.Equal(t, contracts.NewTimestamp(start.Add(window)), a.WindowEnd)
			require.Equal(t, a.WindowEnd, a.CreatedAt)
			require.Nil(t, a.DetectedAt)
			require.Len(t, a.SampleEventIDs, 20)
			for i := range 20 {
				require.Equal(t, volumeEvent(i+1, rule).EventID, a.SampleEventIDs[i])
			}
			require.NoError(t, a.Validate())
			before, err := json.Marshal(a)
			require.NoError(t, err)
			require.Empty(t, applyAuthEvent(t, state, int64(count), volumeEvent(count+1, rule)).Alerts)
			after, err := json.Marshal(a)
			require.NoError(t, err)
			require.Equal(t, before, after, "frozen evidence cannot change after crossing")
			cp, err := state.Checkpoint()
			require.NoError(t, err)
			require.Zero(t, cp.Offset, "retain fired window replay pin")
			replayed := detection.NewPartitionState(volumeRules(t))
			require.NoError(t, replayed.Restore(cp))
			var recovered []*contracts.Alert
			for i := range count {
				recovered = append(recovered, applyAuthEvent(t, replayed, int64(i), volumeEvent(i+1, rule)).Alerts...)
			}
			require.Equal(t, alerts, recovered)
		})
	}
	t.Run("duplicate IDs", func(t *testing.T) {
		state := detection.NewPartitionState(volumeRules(t))
		for i := range threshold - 1 {
			require.Empty(t, applyAuthEvent(t, state, int64(i), volumeEvent(i+1, rule)).Alerts)
		}
		require.Empty(t, applyAuthEvent(t, state, int64(threshold-1), volumeEvent(1, rule)).Alerts)
		out := applyAuthEvent(t, state, int64(threshold), volumeEvent(threshold, rule))
		require.Len(t, out.Alerts, 1)
		require.Equal(t, threshold, out.Alerts[0].EventCount)
	})
	t.Run("null source and wrong predicates", func(t *testing.T) {
		for _, name := range []string{"null source", "invalid source", "wrong type", "allow", "other action", "null action"} {
			t.Run(name, func(t *testing.T) {
				state := detection.NewPartitionState(volumeRules(t))
				for i := range threshold {
					event := volumeEvent(i+1, rule)
					switch name {
					case "null source":
						event.SourceIP = nil
					case "invalid source":
						invalid := "not-an-ip"
						event.SourceIP = &invalid
					case "wrong type":
						event.EventType = "auth.success"
					default:
						if rule != "DENY_SPIKE" {
							return
						}
						action, outcome := "allow", "deny"
						if name == "other action" {
							action = "DENY"
						}
						event.Action, event.Outcome = &action, &outcome
						if name == "null action" {
							event.Action = nil
						}
					}
					require.Empty(t, applyAuthEvent(t, state, int64(i), event).Alerts)
				}
				windows, _, _ := state.Usage()
				require.Zero(t, windows, "unmatched input must not allocate counters")
			})
		}
	})
	t.Run("tenant and source isolation with irrelevant grouping fields", func(t *testing.T) {
		state := detection.NewPartitionState(volumeRules(t))
		for i := range threshold {
			for group := range 3 {
				event := volumeEvent(i*3+group+1, rule)
				if group == 1 {
					event.TenantID = "other"
				}
				if group == 2 {
					ip := "2001:db8::10"
					event.SourceIP = &ip
				}
				// Neither principal nor destination belongs in volume identity.
				value := fmt.Sprintf("user-%d", i)
				event.Principal = &value
				out := applyAuthEvent(t, state, int64(i*3+group), event)
				if i < threshold-1 {
					require.Empty(t, out.Alerts)
				} else {
					require.Len(t, out.Alerts, 1)
					require.Equal(t, event.TenantID, out.Alerts[0].TenantID)
					require.Equal(t, *event.SourceIP, out.Alerts[0].EntityKey)
				}
			}
		}
	})
	t.Run("half-open boundary", func(t *testing.T) {
		state := detection.NewPartitionState(volumeRules(t))
		start := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
		for i := range threshold - 1 {
			event := volumeEvent(i+1, rule)
			event.OccurredAt = contracts.NewTimestamp(start.Add(window - time.Millisecond))
			require.Empty(t, applyAuthEvent(t, state, int64(i), event).Alerts)
		}
		for i := range threshold {
			event := volumeEvent(threshold+i, rule)
			event.OccurredAt = contracts.NewTimestamp(start.Add(window))
			out := applyAuthEvent(t, state, int64(threshold-1+i), event)
			if i < threshold-1 {
				require.Empty(t, out.Alerts)
			} else {
				require.Len(t, out.Alerts, 1)
				require.Equal(t, contracts.NewTimestamp(start.Add(window)), out.Alerts[0].WindowStart)
			}
		}
	})
}
