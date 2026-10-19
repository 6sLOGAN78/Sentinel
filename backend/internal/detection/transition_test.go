package detection //nolint:testpackage // exercise acknowledged state, private windows and handover gates.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/stretchr/testify/require"
)

func configHandover(t *testing.T) (*Rules, *Rules, Checkpoint) {
	t.Helper()
	old, err := LoadRules(strings.NewReader(configDocument(t)))
	require.NoError(t, err)
	next := &Rules{SchemaVersion: 1, Rules: slices.Clone(old.Rules)}
	for i := range next.Rules {
		if next.Rules[i].ID == authRuleID {
			next.Rules[i].Window = 600 * time.Second
			next.Rules[i].Version++
		}
	}
	s := NewPartitionState(old)
	sealed := time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC).UnixMilli()
	s.maximum, s.sealed = cloneTime(&sealed), cloneTime(&sealed)
	s.last, s.acknowledged, s.processedNext = 10, 10, 11
	cp, err := s.Checkpoint()
	require.NoError(t, err)
	return old, next, cp
}

func TestConfigDrain(t *testing.T) {
	old, next, cp := configHandover(t)
	unchanged, err := TransitionCheckpoint(old, old, cp, 0)
	require.NoError(t, err)
	require.Equal(t, cp, unchanged)
	changed, err := TransitionCheckpoint(old, next, cp, 0)
	require.NoError(t, err)
	require.NoError(t, NewPartitionState(next).Restore(changed))
	for _, name := range []string{"same version drift", "active", "mixed manifest", "version rollback"} {
		t.Run(name, func(t *testing.T) {
			bad := &Rules{SchemaVersion: 1, Rules: slices.Clone(next.Rules)}
			var m Metadata
			require.NoError(t, json.Unmarshal([]byte(cp.Metadata), &m))
			switch name {
			case "same version drift":
				bad.Rules[0].Version = old.Rules[0].Version
			case "active":
				m.Drained = false
			case "mixed manifest":
				m.ConfigFingerprint = strings.Repeat("0", 64)
			case "version rollback":
				bad.Rules[1].Version = 0
			}
			raw, marshalErr := json.Marshal(m)
			require.NoError(t, marshalErr)
			_, transitionErr := TransitionCheckpoint(old, bad, Checkpoint{Offset: cp.Offset, Metadata: string(raw)}, 0)
			require.Error(t, transitionErr)
		})
	}
}

func TestConfigWindowEnlargement(t *testing.T) {
	shared := time.Date(2026, 1, 1, 0, 5, 1, 0, time.UTC).UnixMilli()
	for _, duration := range []time.Duration{600 * time.Second, 60 * time.Second} {
		old, next, cp := configHandover(t)
		for i := range next.Rules {
			if next.Rules[i].ID == authRuleID {
				next.Rules[i].Window = duration
			}
		}
		out, err := TransitionCheckpoint(old, next, cp, shared)
		require.NoError(t, err)
		state := NewPartitionState(next)
		require.NoError(t, state.Restore(out))
		var m Metadata
		require.NoError(t, json.Unmarshal([]byte(out.Metadata), &m))
		require.Len(t, m.RuleEffectiveFrom, 4)
		gate := m.RuleEffectiveFrom[authRuleID]
		require.GreaterOrEqual(t, gate, shared)
		require.Zero(t, gate%duration.Milliseconds())
		for _, at := range []int64{gate - 1, gate} {
			e := checkpointEvent(int(at%1000), 0, 0)
			e.OccurredAt = contracts.NewTimestamp(time.UnixMilli(at))
			r, applyErr := state.Apply(cp.Offset, e)
			require.NoError(t, applyErr)
			require.Empty(t, r.Alerts)
			count, _, _ := state.Usage()
			if at < gate {
				require.Zero(t, count, "partial historical window must not reuse an old ID")
			} else {
				require.Equal(t, 1, count, "equality at effective_from is admitted")
			}
			require.NoError(t, state.Acknowledge(cp.Offset))
			cp.Offset++
		}
	}
}

func TestConfigLedgerHandover(t *testing.T) {
	old, next, cp := configHandover(t)
	// A pending output must never yield a handover checkpoint.
	s := NewPartitionState(old)
	e := checkpointEvent(1, 1000, 0)
	_, err := s.Apply(0, e)
	require.NoError(t, err)
	_, err = s.Checkpoint()
	require.Error(t, err)
	out, err := TransitionCheckpoint(old, next, cp, 0)
	require.NoError(t, err)
	var m Metadata
	require.NoError(t, json.Unmarshal([]byte(out.Metadata), &m))
	require.True(t, m.DecisionFrontier.None, "new namespace begins without pretending an old decision used new settings")
	entity := []string{"192.0.2.8", "handover"}
	at := time.UnixMilli(*m.SealedThrough).Truncate(600 * time.Second)
	require.NotEqual(t, contracts.DedupeKey("demo", authRuleID, entity, at),
		contracts.DedupeKey("demo", authRuleID, entity, time.UnixMilli(m.RuleEffectiveFrom[authRuleID])),
		"aligned gate prevents old larger-window ID reuse")
}
