package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/twmb/franz-go/pkg/kgo"
)

const maxScanRecords = 100_000
const maxScanBytes = 32 << 20
const sourcePartitions = 6
const runBudget = 15 * time.Minute

type history struct{ Start, End int64 }
type observer interface {
	Fingerprint() string
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

func caughtUp(ctx context.Context, source observer, keys []string, started, deadline time.Time,
	now func() time.Time, wait func(context.Context, time.Duration) error) (snapshot, error) {
	if now().Before(started) || !now().Before(deadline) || deadline.After(started.Add(runBudget)) {
		return snapshot{}, errors.New("invalid operator deadline")
	}
	// One catch-up deadline and one aggregate scan budget, including all partitions.
	ctx, cancel := context.WithTimeout(ctx, min(time.Minute, deadline.Sub(now())))
	defer cancel()
	ends, err := source.History(ctx)
	if err != nil || !validHistory(ends) {
		return snapshot{}, errors.New("source history unavailable")
	}
	limit := minTime(now().Add(time.Minute), deadline)
	scan := scanBudget{}
	scanned := make(map[int32]bool)
	for {
		if ctx.Err() != nil || !now().Before(limit) {
			return snapshot{}, errors.New("snapshot catch-up deadline exceeded")
		}
		if readErr := stableHistory(ctx, source, ends); readErr != nil {
			return snapshot{}, readErr
		}
		cps, fetchErr := source.Checkpoints(ctx)
		if fetchErr != nil {
			return snapshot{}, errors.New("checkpoint read failed")
		}
		parts, ready, partErr := collectPartitions(ctx, source, ends, cps, &scan, scanned)
		if partErr != nil {
			return snapshot{}, partErr
		}
		if ready {
			return finishSnapshot(ctx, source, ends, keys, parts, started, deadline)
		}
		if err = wait(ctx, min(time.Second, limit.Sub(now()))); err != nil {
			return snapshot{}, errors.New("snapshot wait interrupted")
		}
	}
}

func finishSnapshot(ctx context.Context, source observer, ends map[int32]history, keys []string,
	parts []partitionSnapshot, started, deadline time.Time) (snapshot, error) {
	if err := stableHistory(ctx, source, ends); err != nil {
		return snapshot{}, err
	}
	return snapshot{SchemaVersion: 1, SnapshotAt: started.UTC().Format(time.RFC3339Nano),
		Deadline: deadline.UTC().Format(time.RFC3339Nano), ProducersQuiescent: true,
		SourceSnapshots: parts, SourcePartitions: mapSourceKeys(keys)}, nil
}

func stableHistory(ctx context.Context, source observer, ends map[int32]history) error {
	current, err := source.History(ctx)
	if err != nil || !reflect.DeepEqual(ends, current) {
		return errors.New("source moved; keep demo producers quiescent")
	}
	return nil
}

func collectPartitions(ctx context.Context, source observer, ends map[int32]history,
	cps map[int32]*detection.Checkpoint, budget *scanBudget, scanned map[int32]bool) ([]partitionSnapshot, bool, error) {
	parts := make([]partitionSnapshot, 0, sourcePartitions)
	ready := true
	for p := range int32(sourcePartitions) {
		part, pending, err := snapshotPartition(ctx, source, p, ends[p], cps[p], budget, scanned)
		if err != nil {
			return nil, false, err
		}
		ready = ready && !pending
		parts = append(parts, part)
	}
	return parts, ready, nil
}

func mapSourceKeys(keys []string) map[string]int {
	// Same keyed murmur2 as franz-go's default UniformBytesPartitioner.
	partitioner := kgo.StickyKeyPartitioner(nil).ForTopic("source")
	mapping := make(map[string]int, len(keys))
	for _, key := range keys {
		mapping[key] = partitioner.Partition(&kgo.Record{Key: []byte(key)}, sourcePartitions)
	}
	return mapping
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func validHistory(bounds map[int32]history) bool {
	if len(bounds) != sourcePartitions {
		return false
	}
	for p := range int32(sourcePartitions) {
		b, present := bounds[p]
		if !present || b.Start < 0 || b.End < b.Start {
			return false
		}
	}
	return true
}

func snapshotPartition(ctx context.Context, source observer, p int32, b history, cp *detection.Checkpoint,
	budget *scanBudget, scanned map[int32]bool) (partitionSnapshot, bool, error) {
	part := partitionSnapshot{Partition: p, SnapshotEnd: b.End, Empty: b.End == 0}
	if cp == nil {
		pending, err := missingCheckpoint(ctx, source, p, b, budget, scanned)
		return part, pending, err
	}
	if cp.Offset < b.Start || cp.Offset > b.End {
		return part, false, errors.New("checkpoint pin outside retained history")
	}
	m, err := detection.CheckpointMetadata(*cp)
	if err != nil || m.ConfigFingerprint != source.Fingerprint() || m.ProcessedNext > b.End {
		return part, false, errors.New("invalid or incompatible detector checkpoint")
	}
	if m.ProcessedNext < b.End {
		return part, true, nil
	}
	part.ProcessedNext = m.ProcessedNext
	if b.End == 0 {
		if m.SealedThrough != nil || m.PrefixMaxEventTime != nil {
			return part, false, errors.New("nonempty metadata on empty source")
		}
		return part, false, nil
	}
	if m.SealedThrough == nil {
		return part, false, errors.New("nonempty source has no safe event-time maximum")
	}
	maximum := time.UnixMilli(*m.SealedThrough + detection.Lateness.Milliseconds()).UTC()
	if !contracts.ValidWindow(maximum) {
		return part, false, errors.New("checkpoint maximum outside supported time")
	}
	formatted := maximum.Format(time.RFC3339Nano)
	part.MaxEventTime = &formatted
	return part, false, nil
}

func missingCheckpoint(ctx context.Context, source observer, p int32, b history,
	budget *scanBudget, scanned map[int32]bool) (bool, error) {
	if b.Start != 0 {
		return false, errors.New("missing checkpoint with trimmed history")
	}
	if b.End == 0 {
		return false, nil
	}
	// A scan can reconstruct a conservative maximum but cannot prove detector
	// effects. Still wait for a valid checkpoint before live admission.
	if scanned[p] {
		return true, nil
	}
	if _, err := budget.reconstruct(ctx, source, p, b.Start, b.End, nil); err != nil {
		return false, err
	}
	scanned[p] = true
	return true, nil
}

type scanBudget struct{ records, bytes int }

func reconstruct(ctx context.Context, source observer, p int32, start, end int64, prefix *int64) (*int64, error) {
	budget := scanBudget{}
	return budget.reconstruct(ctx, source, p, start, end, prefix)
}

func (b *scanBudget) reconstruct(ctx context.Context, source observer, p int32,
	start, end int64, prefix *int64) (*int64, error) {
	if start < 0 || end < start || end-start > int64(maxScanRecords-b.records) {
		return nil, errors.New("source scan exceeds record budget")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if ctx.Err() != nil {
		return nil, errors.New("source scan interrupted")
	}
	var maximum *int64
	if prefix != nil {
		value := *prefix
		maximum = &value
	}
	next := start
	err := source.Scan(ctx, p, start, end, func(record *kgo.Record) error {
		if ctx.Err() != nil || record.Offset != next || record.Offset >= end {
			return errors.New("source scan history gap")
		}
		next++
		if err := b.charge(record); err != nil {
			return err
		}
		if value := validRecordTime(record); value != nil && (maximum == nil || *value > *maximum) {
			maximum = value
		}
		return nil
	})
	if err != nil || ctx.Err() != nil || next != end {
		return nil, errors.New("source scan incomplete or unsafe")
	}
	return maximum, nil
}

func (b *scanBudget) charge(record *kgo.Record) error {
	b.records++
	b.bytes += len(record.Value) + len(record.Key)
	for _, h := range record.Headers {
		b.bytes += len(h.Key) + len(h.Value)
	}
	if b.records > maxScanRecords || b.bytes > maxScanBytes {
		return errors.New("source scan budget exceeded")
	}
	return nil
}

func validRecordTime(record *kgo.Record) *int64 {
	event, err := contracts.DecodeEvent(bytes.NewReader(record.Value))
	// Poison does not advance the detector maximum; error details stay private.
	if err == nil && event.Validate(event.OccurredAt.Time, contracts.DefaultLimits()) == nil &&
		event.ReceivedAt != nil && !event.ReceivedAt.IsZero() {
		ip := ""
		if event.SourceIP != nil {
			ip = *event.SourceIP
		}
		if string(record.Key) == event.TenantID+"|"+ip {
			value := event.OccurredAt.UnixMilli()
			return &value
		}
	}
	return nil
}
