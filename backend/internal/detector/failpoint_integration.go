//go:build integration

package detector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
)

const (
	detectorTestEnv          = "SENTINEL_TEST_DETECTOR_"
	detectorBarrierMaximum   = 30 * time.Second
	detectorBarrierPoll      = 10 * time.Millisecond
	detectorObservationLimit = 4096
)

//nolint:gochecknoglobals // integration-only, process-scoped finite fault controls
var (
	detectorBarrierUsed atomic.Bool
	detectorObserved    atomic.Uint64
)

type boundaryObservation struct {
	Boundary   string                `json:"boundary"`
	Topic      string                `json:"topic"`
	Partition  int32                 `json:"partition"`
	Offset     int64                 `json:"offset"`
	Member     string                `json:"member"`
	Generation int32                 `json:"generation"`
	Token      uint64                `json:"token"`
	Checkpoint *detection.Checkpoint `json:"checkpoint,omitempty"`
}

func reachBoundary(ctx context.Context, boundary string, tp detection.TopicPartition, offset int64) error {
	if os.Getenv(detectorTestEnv+"BOUNDARY") != boundary || detectorBarrierUsed.Load() {
		return nil
	}
	wantPartition, err := strconv.ParseInt(os.Getenv(detectorTestEnv+"PARTITION"), 10, 32)
	if err != nil {
		return errors.New("invalid detector test partition")
	}
	wantOffset, err := strconv.ParseInt(os.Getenv(detectorTestEnv+"OFFSET"), 10, 64)
	if err != nil {
		return errors.New("invalid detector test offset")
	}
	if int64(tp.Partition) != wantPartition || offset != wantOffset || !detectorBarrierUsed.CompareAndSwap(false, true) {
		return nil
	}
	path := os.Getenv(detectorTestEnv + "BARRIER")
	if err = writeObservation(path+".reached", boundaryObservation{
		Boundary: boundary, Topic: tp.Topic, Partition: tp.Partition, Offset: offset,
	}); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, detectorBarrierMaximum)
	defer cancel()
	ticker := time.NewTicker(detectorBarrierPoll)
	defer ticker.Stop()
	for {
		// #nosec G703 -- private integration path comes only from the trusted test runner.
		if _, err = os.Lstat(path + ".release"); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("inspect detector test release")
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("detector test barrier: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func observeAssignment(ctx context.Context, boundary string,
	tp detection.TopicPartition, a kafkax.DetectorAssignment) error {
	dir := os.Getenv(detectorTestEnv + "OBSERVATIONS")
	if dir == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if detectorObserved.Add(1) > detectorObservationLimit {
		return errors.New("detector test observation limit")
	}
	next := int64(-1)
	if a.Checkpoint != nil {
		m, err := detection.CheckpointMetadata(*a.Checkpoint)
		if err != nil {
			return err
		}
		next = m.ProcessedNext
	}
	name := fmt.Sprintf("%s-%d-%d-%d-%d.json", boundary, tp.Partition, a.Generation, a.Token, next)
	return writeObservation(filepath.Join(dir, name), boundaryObservation{Boundary: boundary,
		Topic: tp.Topic, Partition: tp.Partition, Offset: next - 1, Member: a.Member,
		Generation: a.Generation, Token: a.Token, Checkpoint: a.Checkpoint})
}

func writeObservation(path string, observation boundaryObservation) error {
	// #nosec G703 -- test runner owns this integration-only 0700 directory.
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("detector test directory must be private")
	}
	// #nosec G304 G703 -- exclusive create refuses symlinks/existing control files.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return errors.New("create detector test observation")
	}
	raw, err := json.Marshal(observation)
	if err == nil {
		_, err = file.Write(raw)
	}
	return errors.Join(err, file.Close())
}
