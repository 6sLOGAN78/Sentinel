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

func scanRules(t *testing.T) *detection.Rules {
	t.Helper()
	rules := loadAuthRules(t)
	for i := range rules.Rules {
		if rules.Rules[i].ID == "PORT_SCAN" {
			rules.Rules[i].Enabled = true
		}
	}
	return rules
}

func scanEvent(id, port int) *contracts.Event {
	event := authEvent(id)
	destination := "192.0.2.20"
	event.EventType, event.DestinationIP, event.DestinationPort = "net.connection", &destination, &port
	return event
}

// Counting events instead of distinct ports, counting a duplicate ID's new
// port, or changing the first crossing's evidence must fail these assertions.
func TestScanThresholds(t *testing.T) {
	for _, distinct := range []int{19, 20, 21} {
		t.Run(fmt.Sprintf("%d distinct ports", distinct), func(t *testing.T) {
			state := detection.NewPartitionState(scanRules(t))
			var alerts []*contracts.Alert
			for i := range distinct {
				alerts = append(alerts, applyAuthEvent(t, state, int64(i), scanEvent(i+1, i)).Alerts...)
			}
			if distinct < 20 {
				require.Empty(t, alerts)
			} else {
				require.Len(t, alerts, 1, "twenty exact ports must emit one SCAN alert")
				require.Equal(t, 20, alerts[0].EventCount)
			}
		})
	}
	t.Run("many unique events on nineteen ports", func(t *testing.T) {
		state := detection.NewPartitionState(scanRules(t))
		for i := range 40 {
			require.Empty(t, applyAuthEvent(t, state, int64(i), scanEvent(i+1, i%19)).Alerts)
		}
	})
	t.Run("repeated-port state reaches the shared event bound without eviction", func(t *testing.T) {
		state := detection.NewPartitionState(scanRules(t))
		for i := range detection.MaxWindowEvents {
			require.Empty(t, applyAuthEvent(t, state, int64(i), scanEvent(i+1, 443)).Alerts)
		}
		out, err := state.Apply(detection.MaxWindowEvents, scanEvent(detection.MaxWindowEvents+1, 444))
		require.Error(t, err, "bounded evidence must refuse another unique event")
		require.Empty(t, out.Alerts)
		windows, _, _ := state.Usage()
		require.Equal(t, 1, windows, "capacity exhaustion must not evict unexpired evidence")
	})
	t.Run("duplicate ID cannot contribute another port", func(t *testing.T) {
		state := detection.NewPartitionState(scanRules(t))
		for i := range 19 {
			require.Empty(t, applyAuthEvent(t, state, int64(i), scanEvent(i+1, i)).Alerts)
		}
		require.Empty(t, applyAuthEvent(t, state, 19, scanEvent(1, 19)).Alerts)
		out := applyAuthEvent(t, state, 20, scanEvent(20, 19))
		require.Len(t, out.Alerts, 1, "only the new ID may supply the twentieth port")
		require.Equal(t, 20, out.Alerts[0].EventCount)
	})
	t.Run("repeated ports preserve unique event count and source order", testScanEvidence)
}

func testScanEvidence(t *testing.T) {
	t.Helper()
	state := detection.NewPartitionState(scanRules(t))
	var samples []string
	for i := range 23 {
		event := scanEvent(100-i, i%19)
		if i < 20 {
			samples = append(samples, event.EventID)
		}
		require.Empty(t, applyAuthEvent(t, state, int64(i), event).Alerts)
	}
	out := applyAuthEvent(t, state, 23, scanEvent(77, 19))
	require.Len(t, out.Alerts, 1)
	start := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	end := contracts.NewTimestamp(start.Add(time.Minute))
	alert := out.Alerts[0]
	require.Equal(t, &contracts.Alert{
		SchemaVersion: 1, AlertID: contracts.DedupeKey("demo", "PORT_SCAN", []string{"192.0.2.10", "192.0.2.20"}, start),
		TenantID: "demo", RuleID: "PORT_SCAN", RuleVersion: 1, Severity: "medium", Status: "open",
		CreatedAt: end, WindowStart: contracts.NewTimestamp(start), WindowEnd: end,
		EntityKey: "192.0.2.10|192.0.2.20", Summary: "Distinct destination ports probed",
		EventCount: 24, SampleEventIDs: samples, DedupeKey: "demo|PORT_SCAN|192.0.2.10|192.0.2.20|2026-10-17T12:00:00Z",
	}, alert)
	require.NoError(t, alert.Validate())
	before, err := json.Marshal(alert)
	require.NoError(t, err)
	require.Empty(t, applyAuthEvent(t, state, 24, scanEvent(76, 20)).Alerts)
	after, err := json.Marshal(alert)
	require.NoError(t, err)
	require.Equal(t, before, after, "fired evidence must stay frozen")
	cp, err := state.Checkpoint()
	require.NoError(t, err)
	require.Zero(t, cp.Offset, "fired state must retain its first input pin")
	replayed := detection.NewPartitionState(scanRules(t))
	require.NoError(t, replayed.Restore(cp))
	for i := range 24 {
		port := i % 19
		if i == 23 {
			port = 19
		}
		got := applyAuthEvent(t, replayed, int64(i), scanEvent(100-i, port))
		if i == 23 {
			require.Len(t, got.Alerts, 1)
			b, marshalErr := json.Marshal(got.Alerts[0])
			require.NoError(t, marshalErr)
			require.Equal(t, before, b)
		}
	}
	marker := scanEvent(75, 21)
	marker.EventType = "auth.success"
	marker.OccurredAt = contracts.NewTimestamp(start.Add(90 * time.Second))
	require.Empty(t, applyAuthEvent(t, state, 25, marker).Alerts)
	windows, _, _ := state.Usage()
	require.Zero(t, windows, "only event-time expiry may retire fired state")
}

// Missing fields must never form synthetic groups; removing tenant/source/
// destination isolation, IPv6 handling or the half-open boundary must fail.
func TestScanPredicates(t *testing.T) {
	t.Run("rule isolation", testScanRuleIsolation)
	t.Run("invalid fields", testScanInvalidFields)
	t.Run("group isolation", testScanGroupIsolation)
	t.Run("IPv6", testScanIPv6)
	t.Run("half-open minute boundary", testScanBoundary)
	t.Run("disabled SCAN", func(t *testing.T) {
		state := detection.NewPartitionState(loadAuthRules(t))
		for i := range 20 {
			require.Empty(t, applyAuthEvent(t, state, int64(i), scanEvent(i+1, i)).Alerts)
		}
	})
}

func testScanRuleIsolation(t *testing.T) {
	t.Helper()
	t.Run("AUTH and SCAN with identical display components remain separate", func(t *testing.T) {
		state := detection.NewPartitionState(scanRules(t))
		principal := "192.0.2.20"
		for i := range 9 {
			event := authEvent(i + 1)
			event.Principal = &principal
			require.Empty(t, applyAuthEvent(t, state, int64(i), event).Alerts)
		}
		for i := range 20 {
			out := applyAuthEvent(t, state, int64(9+i), scanEvent(100+i, i))
			if i < 19 {
				require.Empty(t, out.Alerts)
			} else {
				require.Len(t, out.Alerts, 1)
				require.Equal(t, "PORT_SCAN", out.Alerts[0].RuleID)
				require.Equal(t, 20, out.Alerts[0].EventCount)
			}
		}
		event := authEvent(10)
		event.Principal = &principal
		out := applyAuthEvent(t, state, 29, event)
		require.Len(t, out.Alerts, 1)
		require.Equal(t, "AUTH_BRUTE_FORCE", out.Alerts[0].RuleID)
		require.Equal(t, 10, out.Alerts[0].EventCount)
	})
}

func testScanInvalidFields(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"null source", "null destination", "null port", "wrong type",
		"invalid source", "invalid destination", "negative port", "large port",
	} {
		t.Run(name, func(t *testing.T) {
			state := detection.NewPartitionState(scanRules(t))
			for i := range 20 {
				event := scanEvent(i+1, i)
				invalid := "not-an-ip"
				switch name {
				case "null source":
					event.SourceIP = nil
				case "null destination":
					event.DestinationIP = nil
				case "null port":
					event.DestinationPort = nil
				case "wrong type":
					event.EventType = "net.request"
				case "invalid source":
					event.SourceIP = &invalid
				case "invalid destination":
					event.DestinationIP = &invalid
				case "negative port":
					*event.DestinationPort = -1
				case "large port":
					*event.DestinationPort = 65536
				}
				require.Empty(t, applyAuthEvent(t, state, int64(i), event).Alerts)
			}
			windows, _, _ := state.Usage()
			require.Zero(t, windows)
		})
	}
}

func testScanGroupIsolation(t *testing.T) {
	t.Helper()
	other := "192.0.2.30"
	mutate := map[string]func(*contracts.Event){
		"tenant":      func(event *contracts.Event) { event.TenantID = "other" },
		"source":      func(event *contracts.Event) { event.SourceIP = &other },
		"destination": func(event *contracts.Event) { event.DestinationIP = &other },
	}
	for _, name := range []string{"tenant", "source", "destination"} {
		t.Run(name+" isolation", func(t *testing.T) {
			state := detection.NewPartitionState(scanRules(t))
			var offset int64
			for port := range 20 {
				for group := range 2 {
					event := scanEvent(port*2+group+1, port)
					if group == 1 {
						mutate[name](event)
					}
					out := applyAuthEvent(t, state, offset, event)
					offset++
					if port < 19 {
						require.Empty(t, out.Alerts)
					} else {
						require.Len(t, out.Alerts, 1)
						require.Equal(t, 20, out.Alerts[0].EventCount)
						require.Equal(t, event.TenantID, out.Alerts[0].TenantID)
						require.Equal(t, *event.SourceIP+"|"+*event.DestinationIP, out.Alerts[0].EntityKey)
					}
				}
			}
		})
	}
}

func testScanIPv6(t *testing.T) {
	t.Helper()
	state := detection.NewPartitionState(scanRules(t))
	source, destination := "2001:db8::10", "2001:db8::20"
	for i := range 20 {
		event := scanEvent(i+1, 65516+i)
		event.SourceIP, event.DestinationIP = &source, &destination
		out := applyAuthEvent(t, state, int64(i), event)
		if i == 19 {
			require.Len(t, out.Alerts, 1)
			require.Equal(t, source+"|"+destination, out.Alerts[0].EntityKey)
			id := contracts.DedupeKey("demo", "PORT_SCAN", []string{source, destination}, out.Alerts[0].WindowStart.Time)
			require.Equal(t, id, out.Alerts[0].AlertID)
		}
	}
}

func testScanBoundary(t *testing.T) {
	t.Helper()
	state := detection.NewPartitionState(scanRules(t))
	start := scanEvent(1, 0).OccurredAt.Truncate(time.Minute)
	for i := range 39 {
		event := scanEvent(i+1, i%20)
		at := start.Add(time.Minute - time.Millisecond)
		if i >= 19 {
			at = start.Add(time.Minute)
		}
		event.OccurredAt = contracts.NewTimestamp(at)
		out := applyAuthEvent(t, state, int64(i), event)
		if i < 38 {
			require.Empty(t, out.Alerts, "end instant belongs only to next window")
		} else {
			require.Len(t, out.Alerts, 1)
			require.Equal(t, start.Add(time.Minute), out.Alerts[0].WindowStart.Time)
			require.Equal(t, start.Add(2*time.Minute), out.Alerts[0].WindowEnd.Time)
		}
	}
}
