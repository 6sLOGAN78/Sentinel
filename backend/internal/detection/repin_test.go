package detection //nolint:testpackage // build realistic acknowledged checkpoints with the private-state fixtures.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const repinGroup = "sentinel-detector"

// repinUndrained returns pin=1, processed_next=4 with a fired, still-open window.
func repinUndrained(t *testing.T) Checkpoint {
	t.Helper()
	s := NewPartitionState(checkpointRules())
	outputs := make(map[string]string)
	checkpointApply(t, s, 0, checkpointEvent(1, 300000, 4), outputs)
	for i := range 3 {
		checkpointApply(t, s, int64(i+1), checkpointEvent(i+2, 280000, 0), outputs)
	}
	cp, err := s.Checkpoint()
	require.NoError(t, err)
	m := repinDecode(t, cp)
	require.False(t, m.Drained)
	require.Equal(t, int64(1), cp.Offset)
	require.Equal(t, int64(4), m.ProcessedNext)
	require.False(t, m.DecisionFrontier.None, "fixture must carry a real decision frontier")
	return cp
}

// repinDrained returns pin=processed_next=5 after the fired window retired.
func repinDrained(t *testing.T) Checkpoint {
	t.Helper()
	s := NewPartitionState(checkpointRules())
	outputs := make(map[string]string)
	checkpointApply(t, s, 0, checkpointEvent(1, 300000, 4), outputs)
	for i := range 3 {
		checkpointApply(t, s, int64(i+1), checkpointEvent(i+2, 280000, 0), outputs)
	}
	checkpointApply(t, s, 4, checkpointEvent(5, 630000, 4), outputs)
	cp, err := s.Checkpoint()
	require.NoError(t, err)
	m := repinDecode(t, cp)
	require.True(t, m.Drained)
	require.Equal(t, int64(5), cp.Offset)
	require.Equal(t, int64(5), m.ProcessedNext)
	require.False(t, m.DecisionFrontier.None)
	require.NotNil(t, m.SealedThrough)
	return cp
}

func repinDecode(t *testing.T, cp Checkpoint) Metadata {
	t.Helper()
	m, err := CheckpointMetadata(cp)
	require.NoError(t, err)
	return m
}

// requirePreserved compares the decoded clocks, frontier and manifest field by field.
func requirePreserved(t *testing.T, before, after Metadata) {
	t.Helper()
	require.Equal(t, before.Format, after.Format)
	require.Equal(t, before.ConfigFingerprint, after.ConfigFingerprint)
	require.Equal(t, before.RuleVersions, after.RuleVersions)
	require.Equal(t, before.SealedThrough, after.SealedThrough)
	require.Equal(t, before.PrefixMaxEventTime, after.PrefixMaxEventTime)
	require.Equal(t, before.RuleEffectiveFrom, after.RuleEffectiveFrom)
	require.Equal(t, before.DecisionFrontier, after.DecisionFrontier)
}

func TestRepinExpiredCheckpoint(t *testing.T) {
	rules := checkpointRules()
	undrained, drained := repinUndrained(t), repinDrained(t)
	cases := []struct {
		name                    string
		old                     Checkpoint
		start, end              int64
		pin, neverFrom, neverTo int64
		openWindowsDiscarded    bool
	}{
		{"drained below start", drained, 9, 20, 9, 5, 9, false},
		{"drained start at end", drained, 12, 12, 12, 5, 12, false},
		{"undrained processed_next below start", undrained, 6, 20, 6, 4, 6, true},
		{"undrained start within evaluated range", undrained, 2, 20, 4, 4, 4, true},
		{"undrained start equals processed_next", undrained, 4, 20, 4, 4, 4, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, err := RepinExpiredCheckpoint(rules, tc.old, tc.start, tc.end)
			require.NoError(t, err)
			require.Equal(t, tc.old.Offset, entry.Committed)
			require.Equal(t, tc.start, entry.Start)
			require.Equal(t, tc.end, entry.End)
			require.Equal(t, tc.neverFrom, entry.NeverEvaluatedFrom)
			require.Equal(t, tc.neverTo, entry.NeverEvaluatedTo)
			require.Equal(t, tc.openWindowsDiscarded, entry.OpenWindowsDiscarded)
			require.Equal(t, tc.pin, entry.Repinned.Offset)
			require.LessOrEqual(t, len(entry.Repinned.Metadata), MaxMetadataBytes)
			before, after := repinDecode(t, tc.old), repinDecode(t, entry.Repinned)
			requirePreserved(t, before, after)
			require.True(t, after.Drained)
			require.Equal(t, tc.pin, after.ProcessedNext)
			require.NoError(t, NewPartitionState(rules).Restore(entry.Repinned))
		})
	}
	t.Run("rejects", func(t *testing.T) {
		other := checkpointRules()
		other.Rules[0].Version++
		for name, run := range map[string]func() error{
			"history not expired": func() error { _, err := RepinExpiredCheckpoint(rules, drained, 5, 9); return err },
			"end before start":    func() error { _, err := RepinExpiredCheckpoint(rules, drained, 9, 8); return err },
			"negative start":      func() error { _, err := RepinExpiredCheckpoint(rules, drained, -1, 8); return err },
			"processed beyond end": func() error {
				_, err := RepinExpiredCheckpoint(rules, undrained, 2, 3)
				return err
			},
			"fingerprint mismatch": func() error { _, err := RepinExpiredCheckpoint(other, drained, 9, 20); return err },
			"invalid metadata": func() error {
				_, err := RepinExpiredCheckpoint(rules, Checkpoint{Offset: drained.Offset, Metadata: "{}"}, 9, 20)
				return err
			},
			"oversized metadata": func() error {
				bad := Checkpoint{Offset: drained.Offset, Metadata: strings.Repeat(" ", MaxMetadataBytes) + drained.Metadata}
				_, err := RepinExpiredCheckpoint(rules, bad, 9, 20)
				return err
			},
			"nil rules": func() error { _, err := RepinExpiredCheckpoint(nil, drained, 9, 20); return err },
		} {
			require.Error(t, run(), name)
		}
	})
}

func repinBounds(t *testing.T) []PartitionBounds {
	t.Helper()
	undrained, drained := repinUndrained(t), repinDrained(t)
	return []PartitionBounds{
		{Topic: "security.events.v1", Partition: 2, Committed: undrained.Offset, HasCommitted: true,
			Metadata: undrained.Metadata, Start: 6, End: 20},
		{Topic: "security.events.v1", Partition: 0, Committed: drained.Offset, HasCommitted: true,
			Metadata: drained.Metadata, Start: 9, End: 20},
		{Topic: "security.events.v1", Partition: 1, Committed: drained.Offset, HasCommitted: true,
			Metadata: drained.Metadata, Start: 5, End: 20},
		{Topic: "security.events.v1", Partition: 3, Start: 9, End: 20},
		{Topic: "a.events", Partition: 7, Committed: drained.Offset, HasCommitted: true,
			Metadata: drained.Metadata, Start: 8, End: 8},
	}
}

func TestPlanHistoryRecovery(t *testing.T) {
	rules := checkpointRules()
	t.Run("expired partitions only, sorted", func(t *testing.T) {
		plan, err := PlanHistoryRecovery(rules, repinGroup, repinBounds(t))
		require.NoError(t, err)
		require.Equal(t, repinGroup, plan.Group)
		require.Equal(t, rules.Fingerprint(), plan.ConfigFingerprint)
		require.Len(t, plan.Entries, 3, "in-range and uncommitted partitions produce no entry")
		got := make([]string, 0, len(plan.Entries))
		for _, e := range plan.Entries {
			got = append(got, fmt.Sprintf("%s:%d", e.Topic, e.Partition))
			require.NoError(t, NewPartitionState(rules).Restore(e.Repinned))
		}
		require.Equal(t, []string{"a.events:7", "security.events.v1:0", "security.events.v1:2"}, got)
		require.Equal(t, int64(8), plan.Entries[0].Repinned.Offset)
		require.Equal(t, int64(9), plan.Entries[1].Repinned.Offset)
		require.Equal(t, int64(6), plan.Entries[2].Repinned.Offset)
		require.True(t, plan.Entries[2].OpenWindowsDiscarded)
	})
	t.Run("nothing lost", func(t *testing.T) {
		parts := repinBounds(t)
		for i := range parts {
			parts[i].Start = min(parts[i].Start, parts[i].Committed)
		}
		plan, err := PlanHistoryRecovery(rules, repinGroup, parts)
		require.NoError(t, err)
		require.Empty(t, plan.Entries)
		empty, err := PlanHistoryRecovery(rules, repinGroup, nil)
		require.NoError(t, err)
		require.Empty(t, empty.Entries)
		require.Equal(t, empty.Digest(), plan.Digest())
	})
	other := checkpointRules()
	other.Rules[1].Version++
	for name, mutate := range map[string]func(*PartitionBounds){
		"committed beyond end":  func(p *PartitionBounds) { p.Committed, p.End = p.Start+1, p.Start },
		"end before start":      func(p *PartitionBounds) { p.End = p.Start - 1 },
		"invalid metadata":      func(p *PartitionBounds) { p.Metadata = `{"format":2}` },
		"missing metadata":      func(p *PartitionBounds) { p.Metadata = "" },
		"oversized metadata":    func(p *PartitionBounds) { p.Metadata = strings.Repeat("x", MaxMetadataBytes+1) },
		"empty topic":           func(p *PartitionBounds) { p.Topic = "" },
		"negative partition":    func(p *PartitionBounds) { p.Partition = -1 },
		"duplicate partition":   func(p *PartitionBounds) { p.Topic, p.Partition = "security.events.v1", 2 },
		"committed below zero":  func(p *PartitionBounds) { p.Committed = -1 },
		"uncommitted end<start": func(p *PartitionBounds) { p.HasCommitted, p.End = false, p.Start-1 },
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			parts := repinBounds(t)
			mutate(&parts[1])
			plan, err := PlanHistoryRecovery(rules, repinGroup, parts)
			require.Error(t, err)
			require.Empty(t, plan.Entries, "an error never yields a plan")
		})
	}
	t.Run("rejects fingerprint mismatch", func(t *testing.T) {
		_, err := PlanHistoryRecovery(other, repinGroup, repinBounds(t))
		require.Error(t, err, "a config change must use the drained-transition runbook")
	})
	t.Run("rejects empty group", func(t *testing.T) {
		_, err := PlanHistoryRecovery(rules, "", repinBounds(t))
		require.Error(t, err)
	})
}

func TestRecoveryPlanDigest(t *testing.T) {
	rules := checkpointRules()
	base, err := PlanHistoryRecovery(rules, repinGroup, repinBounds(t))
	require.NoError(t, err)
	again, err := PlanHistoryRecovery(rules, repinGroup, repinBounds(t))
	require.NoError(t, err)
	digest := base.Digest()
	require.Len(t, digest, 64)
	require.Equal(t, strings.ToLower(digest), digest)
	require.Equal(t, digest, again.Digest(), "same inputs give the same digest")

	shuffled := repinBounds(t)
	shuffled[0], shuffled[4] = shuffled[4], shuffled[0]
	reordered, err := PlanHistoryRecovery(rules, repinGroup, shuffled)
	require.NoError(t, err)
	require.Equal(t, digest, reordered.Digest(), "input order is not part of the plan")

	inputs := map[string]func(*PartitionBounds){
		"committed": func(p *PartitionBounds) {
			cp := repinDrainedAt(t, 4)
			p.Committed, p.Metadata = cp.Offset, cp.Metadata
		},
		"start":     func(p *PartitionBounds) { p.Start++ },
		"end":       func(p *PartitionBounds) { p.End++ },
		"partition": func(p *PartitionBounds) { p.Partition = 9 },
		"topic":     func(p *PartitionBounds) { p.Topic = "security.events.v2" },
	}
	for name, mutate := range inputs {
		parts := repinBounds(t)
		mutate(&parts[1])
		changed, planErr := PlanHistoryRecovery(rules, repinGroup, parts)
		require.NoError(t, planErr, name)
		require.NotEqual(t, digest, changed.Digest(), name)
	}
	group, err := PlanHistoryRecovery(rules, repinGroup+"-2", repinBounds(t))
	require.NoError(t, err)
	require.NotEqual(t, digest, group.Digest(), "group")

	fields := map[string]func(*RecoveryEntry){
		"never_from":  func(e *RecoveryEntry) { e.NeverEvaluatedFrom++ },
		"never_to":    func(e *RecoveryEntry) { e.NeverEvaluatedTo++ },
		"open":        func(e *RecoveryEntry) { e.OpenWindowsDiscarded = !e.OpenWindowsDiscarded },
		"pin":         func(e *RecoveryEntry) { e.Repinned.Offset++ },
		"metadata":    func(e *RecoveryEntry) { e.Repinned.Metadata += " " },
		"committed":   func(e *RecoveryEntry) { e.Committed++ },
		"field shift": func(e *RecoveryEntry) { e.Start, e.End = e.End, e.Start },
	}
	for name, mutate := range fields {
		plan := clonePlan(t, base)
		mutate(&plan.Entries[1]) // log_start 9 != log_end 20, so a swap is observable
		require.NotEqual(t, digest, plan.Digest(), name)
	}
	fingerprint := clonePlan(t, base)
	fingerprint.ConfigFingerprint = strings.Repeat("0", 64)
	require.NotEqual(t, digest, fingerprint.Digest(), "fingerprint")
	require.Equal(t, digest, base.Digest(), "digest does not mutate the plan")
}

// repinDrainedAt builds a drained checkpoint at pin=processed_next=offset.
func repinDrainedAt(t *testing.T, offset int64) Checkpoint {
	t.Helper()
	s := NewPartitionState(checkpointRules())
	outputs := make(map[string]string)
	for i := range offset {
		checkpointApply(t, s, i, checkpointEvent(int(i)+1, 300000, 4), outputs)
	}
	cp, err := s.Checkpoint()
	require.NoError(t, err)
	require.Equal(t, offset, cp.Offset)
	return cp
}

func clonePlan(t *testing.T, p RecoveryPlan) RecoveryPlan {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	var out RecoveryPlan
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}
