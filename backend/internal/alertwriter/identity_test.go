package alertwriter_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// Splitting every pipe, hashing display text, or dropping tenant/rule/window
// from the identity must fail these independently assembled component fixtures.
func TestAlertIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, rule, entity string
		parts              []string
		invalid            bool
	}{
		{"AUTH pipes Unicode", "AUTH_BRUTE_FORCE", "192.0.2.1|analyst|δ|租户", []string{"192.0.2.1", "analyst|δ|租户"}, false},
		{"AUTH IPv6", "AUTH_BRUTE_FORCE", "2001:db8::1|δ", []string{"2001:db8::1", "δ"}, false},
		{"AUTH empty", "AUTH_BRUTE_FORCE", "192.0.2.1|", []string{"192.0.2.1", ""}, true},
		{"SCAN IPv4 IPv6", "PORT_SCAN", "192.0.2.1|2001:db8::2", []string{"192.0.2.1", "2001:db8::2"}, false},
		{"SCAN third component", "PORT_SCAN", "192.0.2.1|192.0.2.2|alice", []string{"192.0.2.1", "192.0.2.2", "alice"}, true},
		{"SCAN missing destination", "PORT_SCAN", "192.0.2.1", []string{"192.0.2.1"}, true},
		{"FLOOD source", "REQUEST_FLOOD", "192.0.2.1", []string{"192.0.2.1"}, false},
		{"DENY IPv6", "DENY_SPIKE", "2001:db8::1", []string{"2001:db8::1"}, false},
		{"volume extra component", "REQUEST_FLOOD", "192.0.2.1|alice", []string{"192.0.2.1", "alice"}, true},
		{"zone", "DENY_SPIKE", "fe80::1%eth0", []string{"fe80::1%eth0"}, true},
		{"mapped", "DENY_SPIKE", "::ffff:192.0.2.1", []string{"::ffff:192.0.2.1"}, true},
		{"unsupported", "UNKNOWN_RULE", "192.0.2.1", []string{"192.0.2.1"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := identityAlert(t)
			a.RuleID, a.EntityKey = tc.rule, tc.entity
			a.AlertID = contracts.DedupeKey(a.TenantID, tc.rule, tc.parts, a.WindowStart.Time)
			err := alertwriter.ValidateIdentity(a, []byte(a.TenantID+"|"+a.AlertID))
			if tc.invalid {
				require.Error(t, err)
				require.NotContains(t, err.Error(), tc.entity)
			} else {
				require.NoError(t, err)
			}
		})
	}
	for _, mutate := range []struct {
		name string
		fn   func(*contracts.Alert)
	}{
		{"tenant", func(a *contracts.Alert) { a.TenantID = "other-tenant" }},
		{"hash", func(a *contracts.Alert) { a.AlertID = strings.Repeat("a", 64) }},
		{"window", func(a *contracts.Alert) { a.WindowStart = contracts.NewTimestamp(a.WindowStart.Add(-time.Minute)) }},
	} {
		t.Run("changed "+mutate.name, func(t *testing.T) {
			a := identityAlert(t)
			mutate.fn(a)
			require.Error(t, alertwriter.ValidateIdentity(a, []byte(a.TenantID+"|"+a.AlertID)))
		})
	}
	a := identityAlert(t)
	require.Error(t, alertwriter.ValidateIdentity(a, []byte("wrong|"+a.AlertID)))
	require.Error(t, alertwriter.ValidateIdentity(nil, nil))
}

// Omitting any immutable field or sorting evidence would make a mutation below
// collide. Mutable status/detection-clock changes must leave the digest equal.
func TestImmutableFingerprint(t *testing.T) {
	base := identityAlert(t)
	base.SampleEventIDs = append(base.SampleEventIDs, "00000000-0000-4000-8000-000000000002")
	want, err := alertwriter.ImmutableFingerprint(base)
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		fn   func(*contracts.Alert)
	}{
		{"schema", func(a *contracts.Alert) { a.SchemaVersion++ }},
		{"ID", func(a *contracts.Alert) { a.AlertID = strings.Repeat("a", 64) }},
		{"tenant", func(a *contracts.Alert) { a.TenantID = "tenant-b" }},
		{"rule", func(a *contracts.Alert) { a.RuleID = "DENY_SPIKE" }},
		{"version", func(a *contracts.Alert) { a.RuleVersion++ }},
		{"severity", func(a *contracts.Alert) { a.Severity = "critical" }},
		{"entity", func(a *contracts.Alert) { a.EntityKey += "|δ" }},
		{"summary", func(a *contracts.Alert) { a.Summary += " changed" }},
		{"count", func(a *contracts.Alert) { a.EventCount++ }},
		{"window start", func(a *contracts.Alert) { a.WindowStart = contracts.NewTimestamp(a.WindowStart.Add(time.Millisecond)) }},
		{"window end", func(a *contracts.Alert) { a.WindowEnd = contracts.NewTimestamp(a.WindowEnd.Add(time.Millisecond)) }},
		{"created", func(a *contracts.Alert) { a.CreatedAt = contracts.NewTimestamp(a.CreatedAt.Add(time.Millisecond)) }},
		{"evidence order", func(a *contracts.Alert) {
			a.SampleEventIDs[0], a.SampleEventIDs[1] = a.SampleEventIDs[1], a.SampleEventIDs[0]
		}},
		{"dedupe display", func(a *contracts.Alert) { a.DedupeKey += "|display" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := *base
			a.SampleEventIDs = append([]string(nil), base.SampleEventIDs...)
			tc.fn(&a)
			got, fpErr := alertwriter.ImmutableFingerprint(&a)
			require.NoError(t, fpErr)
			require.NotEqual(t, want, got)
		})
	}
	base.Status = "resolved"
	at := contracts.NewTimestamp(base.CreatedAt.Add(time.Hour))
	base.DetectedAt = &at
	got, err := alertwriter.ImmutableFingerprint(base)
	require.NoError(t, err)
	require.Equal(t, want, got)
	_, err = alertwriter.ImmutableFingerprint(nil)
	require.Error(t, err)
}

func identityAlert(t *testing.T) *contracts.Alert {
	t.Helper()
	a, err := contracts.DecodeAlert(strings.NewReader(string(orderingRecord(t).Value)))
	require.NoError(t, err)
	return a
}
