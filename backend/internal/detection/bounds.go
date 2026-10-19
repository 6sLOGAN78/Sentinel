package detection

import (
	"errors"
	"maps"
	"slices"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// StateLimits may only lower the production admission ceilings in fixtures.
type StateLimits struct {
	Windows, Ledger, Bytes, Events, Ports int
}

// Bounds is shared by every partition in one worker, used serially.
type Bounds struct {
	limits                 StateLimits
	windows, ledger, bytes int
}

// NewBounds constructs process-wide admission accounting.
func NewBounds(limits StateLimits) *Bounds {
	lower := func(value, ceiling int) int {
		if value <= 0 || value > ceiling {
			return ceiling
		}
		return value
	}
	limits.Windows = lower(limits.Windows, MaxWindows)
	limits.Ledger = lower(limits.Ledger, MaxLedgerEntries)
	limits.Bytes = lower(limits.Bytes, MaxStateBytes)
	limits.Events = lower(limits.Events, MaxWindowEvents)
	limits.Ports = lower(limits.Ports, MaxWindowEvents)
	return &Bounds{limits: limits}
}

// NewPartitionStateWithBounds attaches a partition to process accounting.
func NewPartitionStateWithBounds(rules *Rules, bounds *Bounds) *PartitionState {
	if bounds == nil {
		bounds = NewBounds(StateLimits{})
	}
	e := make(map[string]int64, ruleCount)
	for _, id := range ruleIDs() {
		e[id] = 0
	}
	return &PartitionState{
		rules: rules, windows: make(map[windowKey]*windowState), prefix: make(map[int64]*int64),
		last: -1, acknowledged: -1, effective: e, frontier: DecisionFrontier{None: true}, bounds: bounds,
	}
}

// ResourceBudget validates aggregate accounted fetch, producer and state bytes.
func ResourceBudget(fetch, concurrency, producer, state int64) error {
	return config.ComputeDetectorResources(fetch, concurrency, producer, state)
}

var errCapacity = errors.New("detector state capacity exceeded")

func (b *Bounds) reserve(windows, ledger, bytes int) error {
	if windows < 0 || ledger < 0 || bytes < 0 || windows > b.limits.Windows-b.windows ||
		ledger > b.limits.Ledger-b.ledger || bytes > b.limits.Bytes-b.bytes {
		return errCapacity
	}
	b.windows += windows
	b.ledger += ledger
	b.bytes += bytes
	return nil
}

// Accounting charges 128 bytes per ID (hash table growth/slack and string
// header), 32 per exact port, 64 per offset/pointer/time entry, and 1024 per
// window (struct, map headers/slack and all twenty sample string headers).
// Variable key/ID/frozen JSON strings are charged by byte length. This bounds
// accounted retained state, not Go heap/RSS, allocator or transient encoding.
func windowBytes(k windowKey, w *windowState) int {
	if w == nil {
		return 0
	}
	n := windowBaseBytes + len(k.tenant) + len(k.rule) + len(k.ip) + len(k.principal)
	n += len(w.frozen) + len(w.ports)*portEntryBytes
	for id := range w.ids {
		n += eventEntryBytes + len(id)
	}
	return n
}

// prepare computes every matching change in private copies, including the
// frozen payload, before any prefix/max/window field can be changed. At most
// four bounded windows are copied. Duplicate inputs retain the existing maps.
func (s *PartitionState) prepare(source *TopicPartition, offset int64, event *contracts.Event) (
	map[windowKey]*windowState, Outcome, int, int, error,
) {
	draft := *s
	draft.windows = make(map[windowKey]*windowState, ruleCount)
	for _, r := range s.rules.Rules {
		second, _, matched := ruleGroup(event, r.ID)
		if !r.Enabled || !matched {
			continue
		}
		start := event.OccurredAt.UnixMilli() / r.Window.Milliseconds() * r.Window.Milliseconds()
		key := windowKey{tenant: event.TenantID, rule: r.ID, ip: *event.SourceIP, principal: second, start: start}
		if old := s.windows[key]; old != nil {
			copyWindow := *old
			if _, duplicate := old.ids[event.EventID]; !duplicate {
				copyWindow.ids = maps.Clone(old.ids)
				copyWindow.ports = maps.Clone(old.ports)
				copyWindow.samples = slices.Clone(old.samples)
			}
			draft.windows[key] = &copyWindow
		}
	}
	out, err := draft.applyRules(source, offset, event)
	if err != nil {
		return nil, Outcome{}, 0, 0, err
	}
	var windows, bytes int
	for key, next := range draft.windows {
		old := s.windows[key]
		if old == nil {
			windows++
		}
		bytes += windowBytes(key, next) - windowBytes(key, old)
	}
	return draft.windows, out, windows, bytes, nil
}

// Release relinquishes all volatile state only when ownership ends. Kafka's
// durable pin remains the recovery authority for the next owner.
func (s *PartitionState) Release() {
	w, l, b := s.Usage()
	s.bounds.windows -= w
	s.bounds.ledger -= l
	s.bounds.bytes -= b
	s.windows = make(map[windowKey]*windowState)
	s.prefix = make(map[int64]*int64)
}
