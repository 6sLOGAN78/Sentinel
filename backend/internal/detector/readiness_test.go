package detector //nolint:testpackage // verify worker publication of immutable restored-assignment snapshots.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

type readinessConsumer struct{ snapshot kafkax.DetectorReadiness }

func (c *readinessConsumer) ReadinessSnapshot() kafkax.DetectorReadiness { return c.snapshot }
func (c *readinessConsumer) GroupMetadata() (string, int32) {
	return c.snapshot.Member, c.snapshot.Generation
}

type retainedStore struct {
	*postgres.DetectionDecisionStore
	register func(context.Context, postgres.DetectionOwnership, detection.DecisionFrontier) error
}

func (s retainedStore) Register(ctx context.Context, owner postgres.DetectionOwnership,
	frontier detection.DecisionFrontier) error {
	return s.register(ctx, owner, frontier)
}

func TestRetainedOwnerRebinding(t *testing.T) {
	for _, scenario := range []string{"success", "register_failure", "concurrent_revoke", "concurrent_generation"} {
		t.Run(scenario, func(t *testing.T) {
			tp := detection.TopicPartition{Topic: "events", Partition: 0}
			c := &readinessConsumer{snapshot: kafkax.DetectorReadiness{Member: "joined", Generation: 1,
				Observed: true, Tokens: map[detection.TopicPartition]uint64{tp: 1}}}
			rules := &detection.Rules{SchemaVersion: 1}
			w := New(c, nil, rules, config.WriterConfig{}, contracts.Limits{}, nil, config.DetectorPGConfig{})
			state := detection.NewPartitionState(rules)
			before, err := state.Checkpoint()
			require.NoError(t, err)
			oldOwner := postgres.DetectionOwnership{Member: "joined", Generation: 1, Token: 1, Source: tp}
			w.partitions[tp] = partition{token: 1, state: state, owner: oldOwner}
			w.publishReadiness(c.Assignments())
			require.NoError(t, w.Ready(t.Context()))
			c.snapshot.Generation = 2
			called := false
			w.decisions = retainedStore{register: func(_ context.Context, owner postgres.DetectionOwnership,
				frontier detection.DecisionFrontier) error {
				called = true
				require.Equal(t, int32(2), owner.Generation)
				require.Equal(t, "joined", owner.Member)
				require.Equal(t, state.DecisionFrontier(), frontier)
				switch scenario {
				case "register_failure":
					return errors.New("PG unavailable")
				case "concurrent_revoke":
					delete(c.snapshot.Tokens, tp)
					c.snapshot.Observed = false
				case "concurrent_generation":
					c.snapshot.Generation++
				}
				return nil
			}}
			_, err = w.partitionState(t.Context(), tp, c.Assignments())
			require.True(t, called, "new generation must be registered before reusing retained state")
			after, checkpointErr := state.Checkpoint()
			require.NoError(t, checkpointErr)
			require.Equal(t, before, after)
			require.Same(t, state, w.partitions[tp].state)
			if scenario == "success" {
				require.NoError(t, err)
				require.Equal(t, int32(2), w.partitions[tp].owner.Generation)
				w.publishReadiness(c.Assignments())
				require.NoError(t, w.Ready(t.Context()))
			} else {
				require.Error(t, err)
				require.Equal(t, oldOwner, w.partitions[tp].owner)
				require.Error(t, w.Ready(t.Context()))
			}
		})
	}
}
func (c *readinessConsumer) Assignments() map[detection.TopicPartition]Assignment {
	out := make(map[detection.TopicPartition]Assignment)
	for tp, token := range c.snapshot.Tokens {
		out[tp] = Assignment{Token: token, Member: c.snapshot.Member, Generation: c.snapshot.Generation}
	}
	return out
}
func (*readinessConsumer) PollRecords(context.Context, int) ([]*kgo.Record, error) { return nil, nil }
func (*readinessConsumer) CommitCheckpoints(context.Context,
	map[detection.TopicPartition]detection.Checkpoint) error {
	return nil
}
func (*readinessConsumer) AllowRebalance() {}

func TestWorkerReadiness(t *testing.T) {
	c := &readinessConsumer{snapshot: kafkax.DetectorReadiness{Generation: -1}}
	w := New(c, nil, nil, config.WriterConfig{}, contracts.Limits{}, nil, config.DetectorPGConfig{})
	require.Error(t, w.Ready(t.Context()), "dependency ping is not completed restoration")
	c.snapshot.Member, c.snapshot.Generation = "joined", 1
	w.publishReadiness(c.Assignments())
	require.Error(t, w.Ready(t.Context()), "joined before assignment callback is not ready")
	c.snapshot.Observed = true
	w.publishReadiness(c.Assignments())
	require.NoError(t, w.Ready(t.Context()), "observed joined empty assignment is ready")
	tp := detection.TopicPartition{Topic: "events", Partition: 0}
	c.snapshot.Tokens = map[detection.TopicPartition]uint64{tp: 1}
	require.Error(t, w.Ready(t.Context()), "new assignment invalidates empty snapshot")
	w.publishReadiness(c.Assignments())
	require.Error(t, w.Ready(t.Context()), "assignment must actually be restored")
	w.partitions[tp] = partition{token: 1}
	w.publishReadiness(c.Assignments())
	require.NoError(t, w.Ready(t.Context()))
	c.snapshot.Observed = false
	require.Error(t, w.Ready(t.Context()), "revoke invalidates readiness immediately")
	c.snapshot.Observed = true
	c.snapshot.Tokens = map[detection.TopicPartition]uint64{tp: 2}
	require.Error(t, w.Ready(t.Context()), "reassignment cannot reuse old restored token")
	w.partitions[tp] = partition{token: 2}
	w.publishReadiness(c.Assignments())
	require.NoError(t, w.Ready(t.Context()))
	c.snapshot.Generation++
	require.Error(t, w.Ready(t.Context()), "generation change invalidates restored snapshot")
}
