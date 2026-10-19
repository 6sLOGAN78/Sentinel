package detector //nolint:testpackage // verify worker publication of immutable restored-assignment snapshots.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

type readinessConsumer struct{ snapshot kafkax.DetectorReadiness }

func (c *readinessConsumer) ReadinessSnapshot() kafkax.DetectorReadiness { return c.snapshot }
func (c *readinessConsumer) GroupMetadata() (string, int32) {
	return c.snapshot.Member, c.snapshot.Generation
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
