package kafkax

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
)

const (
	defaultLagInterval = 5 * time.Second
	lagQueryTimeout    = 2 * time.Second
	// unseeded marks an owned partition whose processed position is unknown.
	// It exports no lag until a commit seeds it, rather than inventing a value.
	unseeded int64 = -1
)

// EndOffsetLister lists broker end offsets. *kadm.Client satisfies it.
type EndOffsetLister interface {
	ListEndOffsets(ctx context.Context, topics ...string) (kadm.ListedOffsets, error)
}

// CommittedOffsetSource reads a group's committed offsets and the partition
// start offsets used when nothing is committed. *kadm.Client satisfies it.
type CommittedOffsetSource interface {
	FetchOffsets(ctx context.Context, group string) (kadm.OffsetResponses, error)
	ListStartOffsets(ctx context.Context, topics ...string) (kadm.ListedOffsets, error)
}

// LagSink publishes and removes per-partition lag. *telemetry.ConsumerMetrics
// satisfies it.
type LagSink interface {
	SetLag(group, topic string, partition int32, lag int64)
	DeleteLag(group, topic string, partition int32)
}

// LagTracker exports position-based consumer lag: broker end offset minus the
// next offset this process has durably processed, per owned partition. End
// offsets are polled independently of the fetch loop, so lag keeps rising
// while the worker is stuck in storage retries. It never uses committed-offset
// lag (kadm Lag), which is wrong for consumers whose commits trail processing.
type LagTracker struct {
	lister   EndOffsetLister
	sink     LagSink
	group    string
	topic    string
	interval time.Duration

	mu   sync.Mutex
	next map[int32]int64
}

// NewLagTracker builds a tracker for one group/topic. A non-positive interval
// selects the 5 s default. lister may be nil when the tracker is passed to
// NewConsumer through WithLagTracker, which binds the consumer's kadm client.
func NewLagTracker(lister EndOffsetLister, sink LagSink, group, topic string, interval time.Duration) *LagTracker {
	if interval <= 0 {
		interval = defaultLagInterval
	}

	return &LagTracker{
		lister: lister, sink: sink, group: group, topic: topic, interval: interval,
		next: make(map[int32]int64),
	}
}

func (t *LagTracker) bind(lister EndOffsetLister) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lister == nil {
		t.lister = lister
	}
}

func (t *LagTracker) source() EndOffsetLister {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lister
}

// Assigned records newly owned partitions with their initial processed next
// offset. A negative value means the position is unknown.
func (t *LagTracker) Assigned(partitions map[int32]int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for partition, next := range partitions {
		if next < 0 {
			next = unseeded
		}
		if prior, owned := t.next[partition]; owned && prior > next {
			next = prior
		}
		t.next[partition] = next
	}
}

// SeedAssigned resolves the initial position of newly assigned partitions
// from the group's committed offsets, falling back to the partition start
// offset (the consumer resets to the start). Each query is bounded by 2 s.
// Partitions that cannot be resolved stay unseeded until the next commit.
func (t *LagTracker) SeedAssigned(ctx context.Context, src CommittedOffsetSource, partitions []int32) {
	if len(partitions) == 0 {
		return
	}
	seeds := make(map[int32]int64, len(partitions))
	for _, partition := range partitions {
		seeds[partition] = unseeded
	}
	if src == nil {
		t.Assigned(seeds)
		return
	}

	fetchCtx, cancel := context.WithTimeout(ctx, lagQueryTimeout)
	committed, err := src.FetchOffsets(fetchCtx, t.group)
	cancel()
	if err != nil {
		t.Assigned(seeds)
		return
	}
	missing := false
	for _, partition := range partitions {
		response, ok := committed.Lookup(t.topic, partition)
		if ok && response.Err == nil && response.At >= 0 {
			seeds[partition] = response.At
		} else {
			missing = true
		}
	}
	if missing {
		startCtx, cancelStart := context.WithTimeout(ctx, lagQueryTimeout)
		starts, startErr := src.ListStartOffsets(startCtx, t.topic)
		cancelStart()
		for partition, next := range seeds {
			if next != unseeded || startErr != nil {
				continue
			}
			if start, ok := starts.Lookup(t.topic, partition); ok && start.Err == nil && start.Offset >= 0 {
				seeds[partition] = start.Offset
			}
		}
	}
	t.Assigned(seeds)
}

// Processed advances the processed next offset of an owned partition. It never
// moves backwards (replay after a rebalance) and ignores partitions this
// process does not own, so a late commit cannot recreate a revoked series.
func (t *LagTracker) Processed(partition int32, next int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prior, owned := t.next[partition]
	if owned && next > prior {
		t.next[partition] = next
	}
}

// Revoked forgets partitions and deletes their lag series.
func (t *LagTracker) Revoked(partitions []int32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, partition := range partitions {
		delete(t.next, partition)
		if t.sink != nil {
			t.sink.DeleteLag(t.group, t.topic, partition)
		}
	}
}

// Tick lists end offsets once (2 s timeout) and publishes max(0, end - next)
// for each seeded owned partition. On error previous values are left in place.
func (t *LagTracker) Tick(ctx context.Context) error {
	lister := t.source()
	if lister == nil || t.sink == nil {
		return errors.New("lag tracker has no offset source or sink")
	}
	t.mu.Lock()
	owned := len(t.next)
	t.mu.Unlock()
	if owned == 0 {
		return nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, lagQueryTimeout)
	ends, err := lister.ListEndOffsets(queryCtx, t.topic)
	cancel()
	if err != nil {
		return fmt.Errorf("list end offsets: %w", err)
	}

	// Publish under the lock so a concurrent Revoked cannot be undone.
	t.mu.Lock()
	defer t.mu.Unlock()
	for partition, next := range t.next {
		if next == unseeded {
			continue
		}
		end, ok := ends.Lookup(t.topic, partition)
		if !ok || end.Err != nil || end.Offset < 0 {
			continue
		}
		t.sink.SetLag(t.group, t.topic, partition, max(0, end.Offset-next))
	}

	return nil
}

// Run ticks immediately and then every interval until ctx is done. Transient
// query errors keep the last values and never stop the loop. It returns nil on
// cancellation and an error only when the tracker is not wired.
func (t *LagTracker) Run(ctx context.Context) error {
	if t.source() == nil || t.sink == nil {
		return errors.New("lag tracker has no offset source or sink")
	}
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		_ = t.Tick(ctx) // transient: the previous gauge values stay in place
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
