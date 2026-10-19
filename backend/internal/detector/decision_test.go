package detector //nolint:testpackage // inspect restore ordering before source effects.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

type frontierRestoreStore struct {
	decisionStore
	restore   func(*detection.Checkpoint) error
	committed int
}

func (s *frontierRestoreStore) PrepareRestore(_ context.Context, _ postgres.DetectionOwnership,
	cp *detection.Checkpoint, _ bool) (*detection.Checkpoint, error) {
	return cp, s.restore(cp)
}

func (s *frontierRestoreStore) CheckpointCommitted(context.Context, postgres.DetectionOwnership,
	detection.Checkpoint) error {
	s.committed++
	return nil
}

// A source checkpoint is handed unchanged to the authoritative PG validator
// before reducer restore/readiness. ProcessedNext never substitutes for that
// immutable decision proof. The real PG cases live in the integration family.
func TestDecisionFrontierRestore(t *testing.T) {
	for _, scenario := range []string{"none", "valid ledger ahead", "missing", "mismatch", "fence behind"} {
		t.Run(scenario, func(t *testing.T) {
			w, c := poisonWorker(t)
			tp := detection.TopicPartition{Topic: "events"}
			cp := frontierCheckpoint(t, w.rules, tp, scenario != "none")
			m, err := detection.CheckpointMetadata(cp)
			require.NoError(t, err)
			if scenario == "none" {
				require.True(t, m.DecisionFrontier.None)
			} else {
				require.False(t, m.DecisionFrontier.None)
				require.Equal(t, int64(9), m.DecisionFrontier.Offset)
				require.Equal(t, int64(11), m.ProcessedNext)
				require.Equal(t, int64(11), cp.Offset)
			}
			store := &frontierRestoreStore{restore: func(got *detection.Checkpoint) error {
				// Worker must not strip the metadata before the PG restore gate.
				require.Equal(t, &cp, got)
				var forwarded detection.Metadata
				require.NoError(t, json.Unmarshal([]byte(got.Metadata), &forwarded))
				require.Equal(t, m.DecisionFrontier, forwarded.DecisionFrontier)
				if scenario == "none" || scenario == "valid ledger ahead" {
					return nil
				}
				return errors.New("immutable decision proof " + scenario)
			}}
			w.decisions = store
			a := c.Assignments()[tp]
			a.Checkpoint = &cp
			_, err = w.restorePartition(t.Context(), tp, a)
			if scenario == "none" || scenario == "valid ledger ahead" {
				require.NoError(t, err)
				require.Equal(t, 1, store.committed)
			} else {
				require.ErrorContains(t, err, scenario)
				require.Zero(t, store.committed)
				require.Empty(t, c.committed)
				require.Error(t, w.Ready(t.Context()))
			}
		})
	}
}

func frontierCheckpoint(t *testing.T, rules *detection.Rules, tp detection.TopicPartition,
	populated bool) detection.Checkpoint {
	t.Helper()
	state := detection.NewPartitionState(rules)
	if populated {
		at := time.Date(2026, 10, 19, 0, 4, 59, 0, time.UTC)
		ip, principal := "192.0.2.1", "frontier"
		for i := range int64(11) {
			e := &contracts.Event{TenantID: "demo", EventID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i),
				EventType: "auth.failure", SourceIP: &ip, Principal: &principal, OccurredAt: contracts.NewTimestamp(at)}
			if i == 10 {
				e.EventType, e.OccurredAt = "auth.success", contracts.NewTimestamp(at.Add(31*time.Second))
			}
			out, err := state.ApplySource(tp, i, e)
			require.NoError(t, err)
			for _, candidate := range out.Candidates {
				require.NoError(t, state.EffectDecision(candidate))
			}
			require.NoError(t, state.Acknowledge(i))
		}
	}
	cp, err := state.Checkpoint()
	require.NoError(t, err)
	return cp
}
