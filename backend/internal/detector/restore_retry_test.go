package detector //nolint:testpackage // drive partitionState restore retries directly.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

// refreshConsumer models the kafkax adapter: a refresh replaces the retained
// assignment's checkpoint with the broker's committed one.
type refreshConsumer struct {
	*poisonConsumer
	checkpoint *detection.Checkpoint
	broker     *detection.Checkpoint
	refreshes  []uint64
	refreshErr error
}

func (c *refreshConsumer) Assignments() map[detection.TopicPartition]Assignment {
	out := c.poisonConsumer.Assignments()
	for tp, a := range out {
		a.Checkpoint = c.checkpoint
		out[tp] = a
	}
	return out
}

func (c *refreshConsumer) RefreshCheckpoint(_ context.Context, _ detection.TopicPartition, token uint64) error {
	c.refreshes = append(c.refreshes, token)
	if c.refreshErr != nil {
		return c.refreshErr
	}
	c.checkpoint = c.broker
	return nil
}

type retryRestoreStore struct {
	decisionStore
	failures int
	received []*detection.Checkpoint
}

func (s *retryRestoreStore) PrepareRestore(_ context.Context, _ postgres.DetectionOwnership,
	cp *detection.Checkpoint, _ bool) (*detection.Checkpoint, error) {
	s.received = append(s.received, cp)
	if len(s.received) <= s.failures {
		return nil, errors.New("first Kafka checkpoint lost the startup rebalance")
	}
	return cp, nil
}

func (*retryRestoreStore) CheckpointCommitted(context.Context, postgres.DetectionOwnership,
	detection.Checkpoint) error {
	return nil
}

func retryWorker(t *testing.T) (*Worker, *refreshConsumer, *retryRestoreStore, detection.TopicPartition) {
	t.Helper()
	w, poison := poisonWorker(t)
	tp := detection.TopicPartition{Topic: "events", Partition: 0}
	broker := frontierCheckpoint(t, w.rules, tp, false)
	c := &refreshConsumer{poisonConsumer: poison, broker: &broker}
	w.consumer = c
	store := &retryRestoreStore{failures: 1}
	w.decisions = store
	delete(w.partitions, tp)
	return w, c, store, tp
}

// A failed restore of an unchanged token re-reads Kafka before the retry and
// restores from the broker checkpoint, never from the stale empty snapshot.
func TestRestoreRetryRefreshesCheckpoint(t *testing.T) {
	w, c, store, tp := retryWorker(t)
	_, err := w.partitionState(t.Context(), tp, c.Assignments())
	require.Error(t, err)
	require.Empty(t, c.refreshes, "a first restore uses the offset-fetch snapshot")
	require.Nil(t, store.received[0])
	_, err = w.partitionState(t.Context(), tp, c.Assignments())
	require.NoError(t, err)
	require.Equal(t, []uint64{1}, c.refreshes)
	require.Equal(t, c.broker, store.received[1], "retry restores the broker checkpoint")
	require.Empty(t, w.retry, "a successful restore clears the retry mark")
	require.Empty(t, c.committed, "an adopted broker checkpoint is not committed again")
}

func TestRestoreRetryRefreshFailureFailsClosed(t *testing.T) {
	w, c, store, tp := retryWorker(t)
	_, err := w.partitionState(t.Context(), tp, c.Assignments())
	require.Error(t, err)
	c.refreshErr = errors.New("detector committed checkpoint unavailable")
	_, err = w.partitionState(t.Context(), tp, c.Assignments())
	require.ErrorIs(t, err, c.refreshErr)
	require.Len(t, store.received, 1, "no restore runs without a fresh broker read")
	require.NotContains(t, w.partitions, tp)
}

func TestRestoreRetryMarkClearsOnNewToken(t *testing.T) {
	w, c, _, tp := retryWorker(t)
	_, err := w.partitionState(t.Context(), tp, c.Assignments())
	require.Error(t, err)
	c.snapshot.Tokens[tp] = 2 // reassigned: the new token has its own offset fetch
	require.NoError(t, w.syncAssignments(t.Context(), c.Assignments()))
	require.Empty(t, c.refreshes)
	require.NotContains(t, w.retry, tp)
}
