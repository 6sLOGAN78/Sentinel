package main

import (
	"context"
	"errors"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/twmb/franz-go/pkg/kgo"
)

const maxScanRecords = 100_000
const maxScanBytes = 32 << 20

type history struct{ Start, End int64 }
type observer interface {
	History(context.Context) (map[int32]history, error)
	Checkpoints(context.Context) (map[int32]*detection.Checkpoint, error)
	Scan(context.Context, int32, int64, int64, func(*kgo.Record) error) error
}

type partitionSnapshot struct {
	Partition     int32   `json:"partition"`
	SnapshotEnd   int64   `json:"snapshot_end"`
	ProcessedNext int64   `json:"processed_next"`
	Empty         bool    `json:"empty"`
	MaxEventTime  *string `json:"max_event_time"`
}

type snapshot struct {
	SchemaVersion      int                 `json:"schema_version"`
	SnapshotAt         string              `json:"snapshot_at"`
	Deadline           string              `json:"deadline"`
	ProducersQuiescent bool                `json:"producers_quiescent"`
	SourceSnapshots    []partitionSnapshot `json:"source_snapshots"`
	SourcePartitions   map[string]int      `json:"source_partitions"`
}

func caughtUp(context.Context, observer, []string, time.Time, time.Time,
	func() time.Time, func(context.Context, time.Duration) error) (snapshot, error) {
	return snapshot{}, errors.New("snapshot behavior not implemented")
}

func reconstruct(context.Context, observer, int32, int64, int64, *int64) (*int64, error) {
	return nil, errors.New("reconstruction behavior not implemented")
}
