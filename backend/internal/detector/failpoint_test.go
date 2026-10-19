//go:build !integration

package detector //nolint:testpackage // Verify pending source work cannot reach private restore/effect paths.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

func TestDetectorFailpointBuildBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	dir := t.TempDir()
	binary := filepath.Join(dir, "detector")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/detector")
	build.Dir = "../.."
	raw, err := build.CombinedOutput()
	require.NoError(t, err, string(raw))
	barrier := filepath.Join(dir, "forbidden")
	// Invalid operator configuration terminates this standalone production probe;
	// the real mid-window family also drives this default binary through publication.
	command := exec.CommandContext(ctx, binary)
	command.Dir = dir
	command.Env = append(os.Environ(), "SENTINEL_DETECTOR.RULES_PATH="+filepath.Join(dir, "absent.yaml"),
		"SENTINEL_TEST_DETECTOR_BOUNDARY=before-publish", "SENTINEL_TEST_DETECTOR_BARRIER="+barrier,
		"SENTINEL_TEST_DETECTOR_OBSERVATIONS="+dir)
	_, err = command.CombinedOutput()
	require.Error(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "default production binary must have no fault-control file effects")
}

type pendingSourceConsumer struct{ *poisonConsumer }

func (c pendingSourceConsumer) Assignments() map[detection.TopicPartition]Assignment {
	assignments := c.poisonConsumer.Assignments()
	for tp, a := range assignments {
		a.RestorePending = true
		assignments[tp] = a
	}
	return assignments
}

type pendingRestoreStore struct {
	decisionStore
	calls int
}

func (s *pendingRestoreStore) PrepareRestore(context.Context, postgres.DetectionOwnership,
	*detection.Checkpoint, bool) (*detection.Checkpoint, error) {
	s.calls++
	return nil, errors.New("pending restore reached PG")
}

func TestDetectorPendingSource(t *testing.T) {
	t.Run("idle cannot restore", func(t *testing.T) {
		w, base := poisonWorker(t)
		w.consumer = pendingSourceConsumer{base}
		w.partitions = make(map[detection.TopicPartition]partition)
		store := &pendingRestoreStore{}
		w.decisions = store
		require.NoError(t, w.syncAssignments(t.Context(), w.consumer.Assignments()))
		require.Zero(t, store.calls)
		require.Empty(t, w.partitions)
		require.Empty(t, base.committed)
		w.publishReadiness(w.consumer.Assignments())
		require.Error(t, w.Ready(t.Context()))
	})
	t.Run("records cannot mutate or commit", func(t *testing.T) {
		w, base := poisonWorker(t)
		w.consumer = pendingSourceConsumer{base}
		tp := detection.TopicPartition{Topic: "events", Partition: 0}
		before, err := w.partitions[tp].state.Checkpoint()
		require.NoError(t, err)
		record := poisonEvent(t, 0, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
		require.Error(t, w.process(t.Context(), []*kgo.Record{record}))
		after, err := w.partitions[tp].state.Checkpoint()
		require.NoError(t, err)
		require.Equal(t, before, after)
		require.Empty(t, base.committed)
		require.Error(t, w.commitEffects(t.Context(), map[detection.TopicPartition]detection.Checkpoint{tp: before}))
		require.Empty(t, base.committed)
		w.publishReadiness(w.consumer.Assignments())
		require.Error(t, w.Ready(t.Context()))
	})
}
