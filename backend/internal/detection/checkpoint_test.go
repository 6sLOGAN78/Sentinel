package detection //nolint:testpackage // compare complete live reconstruction, including private evidence sets.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

func checkpointRules() *Rules {
	r := &Rules{SchemaVersion: 1}
	for i, id := range ruleIDs() {
		window := []time.Duration{300, 60, 60, 120}[i] * time.Second
		r.Rules = append(r.Rules, Rule{ID: id, Version: 1, Enabled: true, Threshold: 3,
			Window: window, Cooldown: time.Minute, Severity: "high"})
	}
	return r
}

func checkpointEvent(id int, millis int64, kind int) *contracts.Event {
	base := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	ip, principal, destination, action, port := "192.0.2.7", "replay|user", "198.51.100.9", "deny", id%100+1
	at := contracts.NewTimestamp(base.Add(time.Duration(millis) * time.Millisecond))
	return &contracts.Event{SchemaVersion: 1, EventID: fmt.Sprintf("00000000-0000-4000-8000-%012d", id),
		TenantID: "demo", Source: "checkpoint-fixture", OccurredAt: at, ReceivedAt: &at,
		EventType: []string{"auth.failure", "net.connection", "net.request", "policy.decision", "auth.success"}[kind],
		SourceIP:  &ip, Principal: &principal, DestinationIP: &destination, DestinationPort: &port, Action: &action}
}

func checkpointApply(t *testing.T, s *PartitionState, offset int64, e *contracts.Event,
	outputs map[string]string) Outcome {
	t.Helper()
	out, err := s.ApplySource(TopicPartition{Topic: "replay", Partition: 0}, offset, e)
	require.NoError(t, err)
	for _, c := range out.Candidates {
		var a contracts.Alert
		require.NoError(t, json.Unmarshal(c.CanonicalBytes(), &a))
		if previous, ok := outputs[a.AlertID]; ok {
			require.Equal(t, previous, string(c.CanonicalBytes()), "same ID must always have identical canonical bytes")
		}
		outputs[a.AlertID] = string(c.CanonicalBytes())
		require.NoError(t, s.EffectDecision(c))
	}
	require.NoError(t, s.Acknowledge(offset))
	return out
}

func TestCheckpointPrefixAndSealed(t *testing.T) {
	t.Run("pre-pin maximum, fired pin and durable retirement", func(t *testing.T) {
		s := NewPartitionState(checkpointRules())
		outputs := make(map[string]string)
		checkpointApply(t, s, 0, checkpointEvent(1, 300000, 4), outputs)
		for i := range 3 {
			checkpointApply(t, s, int64(i+1), checkpointEvent(i+2, 280000, 0), outputs)
		}
		cp, err := s.Checkpoint()
		require.NoError(t, err)
		require.Equal(t, int64(1), cp.Offset, "fired state keeps earliest contributing offset")
		var m Metadata
		require.NoError(t, json.Unmarshal([]byte(cp.Metadata), &m))
		require.Equal(t, checkpointEvent(1, 300000, 4).OccurredAt.UnixMilli(), *m.PrefixMaxEventTime)
		require.False(t, m.DecisionFrontier.None)
		require.Len(t, m.RuleVersions, 4)
		require.Len(t, m.RuleEffectiveFrom, 4)
		require.LessOrEqual(t, len(cp.Metadata), 1024)
		restored := NewPartitionState(checkpointRules())
		require.NoError(t, restored.Restore(cp))
		for i := range 3 {
			checkpointApply(t, restored, int64(i+1), checkpointEvent(i+2, 280000, 0), outputs)
		}
		require.True(t, checkpointApply(t, restored, 4, checkpointEvent(8, 269999, 0), outputs).Late)
		require.False(t, checkpointApply(t, restored, 5, checkpointEvent(9, 270000, 0), outputs).Late)
		before := cloneTime(restored.sealed)
		_, err = restored.Apply(6, checkpointEvent(10, 630000, 4))
		require.NoError(t, err)
		require.Equal(t, before, restored.sealed, "unacknowledged effects cannot seal or retire")
		require.NotEmpty(t, restored.windows)
		_, err = restored.Checkpoint()
		require.Error(t, err)
		require.NoError(t, restored.Acknowledge(6))
		require.Empty(t, restored.windows)
		cp, err = restored.Checkpoint()
		require.NoError(t, err)
		require.Equal(t, int64(7), cp.Offset)
		_, ledger, _ := restored.Usage()
		require.Positive(t, ledger, "snapshot does not prune ledger before broker ack")
		restored.Committed(cp)
		_, ledger, _ = restored.Usage()
		require.Zero(t, ledger)
	})
	t.Run("strict complete format two", func(t *testing.T) {
		s := NewPartitionState(checkpointRules())
		checkpointApply(t, s, 0, checkpointEvent(1, 280000, 0), make(map[string]string))
		cp, err := s.Checkpoint()
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(cp.Metadata), &fields))
		for field := range fields {
			t.Run("missing "+field, func(t *testing.T) {
				copyFields := make(map[string]json.RawMessage)
				for k, v := range fields {
					if k != field {
						copyFields[k] = v
					}
				}
				raw, marshalErr := json.Marshal(copyFields)
				require.NoError(t, marshalErr)
				require.Error(t, NewPartitionState(checkpointRules()).Restore(Checkpoint{Offset: cp.Offset, Metadata: string(raw)}))
			})
		}
		for _, bad := range []string{cp.Metadata + "{}", strings.Repeat("x", 1025),
			strings.Replace(cp.Metadata, `"format":2`, `"format":3`, 1),
			strings.Replace(cp.Metadata, `"format":2`, `"format":2,"format":2`, 1),
			strings.Replace(cp.Metadata, `"drained":false`, `"drained":null`, 1)} {
			require.Error(t, NewPartitionState(checkpointRules()).Restore(Checkpoint{Offset: cp.Offset, Metadata: bad}))
		}
	})
}
