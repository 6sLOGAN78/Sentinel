package detection //nolint:testpackage // verify unchanged private accounting and recoverable state on rejection.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

func boundRules() *Rules {
	return &Rules{SchemaVersion: 1, Rules: []Rule{{ID: authRuleID, Version: 1, Enabled: true,
		Threshold: MaxWindowEvents, Window: time.Hour, Cooldown: time.Second, Severity: "high"}}}
}

func boundEvent(n int, principal string) *contracts.Event {
	ip := "192.0.2.10"
	return &contracts.Event{SchemaVersion: 1, EventID: fmt.Sprintf("00000000-0000-4000-8000-%012d", n),
		TenantID: "demo", EventType: "auth.failure", SourceIP: &ip, Principal: &principal,
		OccurredAt: contracts.NewTimestamp(time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC))}
}

func applyBound(t *testing.T, s *PartitionState, offset int64, e *contracts.Event) {
	t.Helper()
	_, err := s.Apply(offset, e)
	require.NoError(t, err)
	require.NoError(t, s.Acknowledge(offset))
}

func rejectUnchanged(t *testing.T, s *PartitionState, offset int64, e *contracts.Event) {
	t.Helper()
	before, err := s.Checkpoint()
	require.NoError(t, err)
	w, l, b := s.Usage()
	_, err = s.Apply(offset, e)
	require.Error(t, err, "capacity admission must reject before mutation")
	after, err := s.Checkpoint()
	require.NoError(t, err, "rejected input left unacknowledged state")
	require.Equal(t, before, after)
	nw, nl, nb := s.Usage()
	require.Equal(t, []int{w, l, b}, []int{nw, nl, nb})
}

func TestStateBounds(t *testing.T) { //nolint:gocognit // Independent adversarial resource subcases share the named acceptance family.
	t.Run("aggregate_windows", func(t *testing.T) {
		budget := NewBounds(StateLimits{Windows: 2})
		a, b := NewPartitionStateWithBounds(boundRules(), budget), NewPartitionStateWithBounds(boundRules(), budget)
		applyBound(t, a, 0, boundEvent(1, "a"))
		applyBound(t, b, 0, boundEvent(2, "b"))
		rejectUnchanged(t, a, 1, boundEvent(3, "c"))
	})
	t.Run("production_windows_aggregate", func(t *testing.T) {
		budget := NewBounds(StateLimits{})
		a, b := NewPartitionStateWithBounds(boundRules(), budget), NewPartitionStateWithBounds(boundRules(), budget)
		for n := range MaxWindows {
			s := a
			if n%2 == 1 {
				s = b
			}
			applyBound(t, s, int64(n/2), boundEvent(n, strconv.Itoa(n)))
		}
		rejectUnchanged(t, b, MaxWindows/2, boundEvent(MaxWindows+1, "overflow"))
	})
	t.Run("unique_ids", func(t *testing.T) {
		s := NewPartitionState(boundRules())
		for n := range MaxWindowEvents {
			applyBound(t, s, int64(n), boundEvent(n, "a"))
		}
		applyBound(t, s, MaxWindowEvents, boundEvent(0, "a"))
		rejectUnchanged(t, s, MaxWindowEvents+1, boundEvent(MaxWindowEvents+1, "a"))
	})
	t.Run("ledger_fixed_time_aggregate", func(t *testing.T) {
		budget := NewBounds(StateLimits{})
		a, b := NewPartitionStateWithBounds(boundRules(), budget), NewPartitionStateWithBounds(boundRules(), budget)
		for n := range MaxLedgerEntries {
			s := a
			if n%2 == 1 {
				s = b
			}
			applyBound(t, s, int64(n/2), boundEvent(0, "a"))
		}
		rejectUnchanged(t, b, MaxLedgerEntries/2, boundEvent(0, "a"))
	})
	t.Run("bytes_and_frozen_payload", func(t *testing.T) {
		r := boundRules()
		r.Rules[0].Threshold = 2
		s := NewPartitionStateWithBounds(r, NewBounds(StateLimits{Bytes: 1600}))
		applyBound(t, s, 0, boundEvent(0, "a"))
		rejectUnchanged(t, s, 1, boundEvent(1, "a"))
	})
	t.Run("production_bytes_aggregate", func(t *testing.T) {
		budget := NewBounds(StateLimits{})
		a, b := NewPartitionStateWithBounds(boundRules(), budget), NewPartitionStateWithBounds(boundRules(), budget)
		// Exercise the reducer's byte defense even for a direct caller bypassing
		// contract validation. Runtime principals are additionally schema-capped.
		large := strings.Repeat("x", 64<<10)
		for n := 0; ; n++ {
			s := a
			if n%2 == 1 {
				s = b
			}
			e := boundEvent(n, large+strconv.Itoa(n))
			need := windowBaseBytes + len(e.TenantID) + len(authRuleID) + len(*e.SourceIP) + len(*e.Principal) +
				eventEntryBytes + len(e.EventID) + ledgerEntryBytes
			if need > MaxStateBytes-budget.bytes {
				rejectUnchanged(t, s, int64(n/2), e)
				require.LessOrEqual(t, budget.bytes, MaxStateBytes)
				break
			}
			applyBound(t, s, int64(n/2), e)
		}
	})
	t.Run("release_only_safe_prefix_and_retired_state", func(t *testing.T) {
		budget := NewBounds(StateLimits{Windows: 1, Ledger: 2})
		s := NewPartitionStateWithBounds(boundRules(), budget)
		applyBound(t, s, 0, boundEvent(0, "a"))
		cp, err := s.Checkpoint()
		require.NoError(t, err)
		s.Committed(cp)
		require.Equal(t, 1, budget.ledger, "live pin retains prefix entry")
		marker := boundEvent(1, "a")
		marker.EventType = "auth.success"
		marker.OccurredAt = contracts.NewTimestamp(marker.OccurredAt.Add(2 * time.Hour))
		_, err = s.Apply(1, marker)
		require.NoError(t, err)
		require.Equal(t, 1, budget.windows, "unfinished effect cannot retire state")
		require.NoError(t, s.Acknowledge(1))
		require.Zero(t, budget.windows)
		require.Equal(t, 2, budget.ledger)
		cp, err = s.Checkpoint()
		require.NoError(t, err)
		s.Committed(cp)
		require.Zero(t, budget.ledger)
		require.Zero(t, budget.bytes)
	})
	t.Run("ports_exact", func(t *testing.T) {
		r := boundRules()
		r.Rules[0].ID = scanRuleID
		s := NewPartitionStateWithBounds(r, NewBounds(StateLimits{Ports: 2}))
		for n := range 3 {
			e := boundEvent(n, "a")
			e.EventType = "net.connection"
			dest, port := "198.51.100.2", n%2
			e.DestinationIP, e.DestinationPort = &dest, &port
			applyBound(t, s, int64(n), e)
		}
		e := boundEvent(4, "a")
		e.EventType = "net.connection"
		dest, port := "198.51.100.2", 2
		e.DestinationIP, e.DestinationPort = &dest, &port
		rejectUnchanged(t, s, 3, e)
	})
	t.Run("offset_overflow", func(t *testing.T) {
		s := NewPartitionState(boundRules())
		rejectUnchanged(t, s, math.MaxInt64, boundEvent(0, "a"))
	})
	t.Run("aggregate_resource_arithmetic", func(t *testing.T) {
		require.NoError(t, ResourceBudget(16<<20, 2, 32<<20, MaxStateBytes))
		for _, v := range [][4]int64{{64 << 20, 3, 1, MaxStateBytes}, {math.MaxInt64, 2, 1, 1},
			{1, 1, math.MaxInt64, 1}, {-1, 1, 1, 1}, {1, 0, 1, 1}} {
			require.Error(t, ResourceBudget(v[0], v[1], v[2], v[3]))
		}
	})
}

func TestLateMetric(t *testing.T) {
	s := NewPartitionState(boundRules())
	marker := boundEvent(0, "a")
	marker.EventType = "auth.success"
	applyBound(t, s, 0, marker)
	maximum := *s.maximum
	for n, delta := range []time.Duration{-Lateness, -Lateness - time.Millisecond} {
		e := boundEvent(n+1, "a")
		e.OccurredAt = contracts.NewTimestamp(marker.OccurredAt.Add(delta))
		out, err := s.Apply(int64(n+1), e)
		require.NoError(t, err)
		require.Equal(t, n == 1, out.Late)
		require.NoError(t, s.Acknowledge(int64(n+1)))
	}
	_, err := s.Checkpoint()
	require.NoError(t, err)
	require.Equal(t, maximum, *s.maximum, "idle checkpoint must not advance event time")
}
