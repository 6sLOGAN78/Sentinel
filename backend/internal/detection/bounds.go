package detection

import (
	"errors"

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

type windowAdmission struct {
	key   windowKey
	state *windowState
	port  *int
}

// prepare stages bounded deltas and frozen bytes before any recoverable change.
// Existing ID/port maps are never copied per input. Only the first twenty
// sample headers and the one threshold-crossing ID set need private copies.
func (s *PartitionState) prepare(source *TopicPartition, offset int64, event *contracts.Event) (
	[]windowAdmission, Outcome, int, int, error,
) {
	var changes []windowAdmission
	var out Outcome
	var windows, bytes int
	for ordinal, id := range ruleIDs() {
		for _, r := range s.rules.Rules {
			if r.ID != id || !r.Enabled {
				continue
			}
			change, result, nw, nb, err := s.prepareWindow(source, offset, ordinal, event, r)
			if err != nil {
				return nil, Outcome{}, 0, 0, err
			}
			if change.state != nil {
				changes = append(changes, change)
			}
			out.Alerts = append(out.Alerts, result.Alerts...)
			out.Candidates = append(out.Candidates, result.Candidates...)
			windows += nw
			bytes += nb
		}
	}
	return changes, out, windows, bytes, nil
}

func (s *PartitionState) prepareWindow(source *TopicPartition, offset int64, ordinal int,
	event *contracts.Event, r Rule,
) (windowAdmission, Outcome, int, int, error) {
	second, port, matched := ruleGroup(event, r.ID)
	if !matched {
		return windowAdmission{}, Outcome{}, 0, 0, nil
	}
	start := event.OccurredAt.UnixMilli() / r.Window.Milliseconds() * r.Window.Milliseconds()
	end := start + r.Window.Milliseconds()
	if start < s.effective[r.ID] || s.sealed != nil && end <= *s.sealed {
		return windowAdmission{}, Outcome{}, 0, 0, nil
	}
	key := windowKey{tenant: event.TenantID, rule: r.ID, ip: *event.SourceIP, principal: second, start: start}
	old := s.windows[key]
	var next windowState
	windows, bytes := 0, eventEntryBytes+len(event.EventID)
	if old == nil {
		next = windowState{first: offset, end: end, ids: make(map[string]struct{})}
		if port != nil {
			next.ports = make(map[int]struct{})
		}
		windows = 1
		bytes += windowBytes(key, &next)
	} else {
		next = *old
	}
	if _, duplicate := next.ids[event.EventID]; duplicate {
		return windowAdmission{}, Outcome{}, 0, 0, nil
	}
	if len(next.ids) >= s.bounds.limits.Events {
		return windowAdmission{}, Outcome{}, 0, 0, errCapacity
	}
	count := len(next.ids) + 1
	if port != nil {
		var portBytes int
		var err error
		count, portBytes, err = reservePort(next.ports, *port, s.bounds.limits.Ports)
		if err != nil {
			return windowAdmission{}, Outcome{}, 0, 0, err
		}
		bytes += portBytes
	}
	if len(next.samples) < maxSamples {
		samples := make([]string, len(next.samples)+1, maxSamples)
		copy(samples, next.samples)
		samples[len(next.samples)] = event.EventID
		next.samples = samples
	}
	var out Outcome
	if next.frozen == "" && count >= r.Threshold {
		var err error
		out, err = freezeAdmission(source, offset, ordinal, event, r, key, &next)
		if err != nil {
			return windowAdmission{}, Outcome{}, 0, 0, err
		}
		bytes += len(next.frozen)
	}
	return windowAdmission{key: key, state: &next, port: port}, out, windows, bytes, nil
}

func reservePort(ports map[int]struct{}, port, limit int) (int, int, error) {
	count := len(ports)
	if _, exists := ports[port]; exists {
		return count, 0, nil
	}
	if count >= limit {
		return 0, 0, errCapacity
	}
	return count + 1, portEntryBytes, nil
}

func freezeAdmission(source *TopicPartition, offset int64, ordinal int, event *contracts.Event,
	r Rule, key windowKey, next *windowState,
) (Outcome, error) {
	// freezeEvidence only reads ID cardinality. Supply it privately exactly
	// once at threshold crossing, without mutating the retained ID set.
	evidence := *next
	evidence.ids = make(map[string]struct{}, len(next.ids)+1)
	for id := range next.ids {
		evidence.ids[id] = struct{}{}
	}
	evidence.ids[event.EventID] = struct{}{}
	a, raw, fingerprint, err := freezeEvidence(key, &evidence, r)
	if err != nil {
		return Outcome{}, err
	}
	next.frozen = string(raw)
	out := Outcome{Alerts: []*contracts.Alert{a}}
	if source != nil {
		out.Candidates = []Candidate{{source: *source, offset: offset, ordinal: ordinal,
			trigger: contracts.NewTimestamp(event.OccurredAt.Time), canonical: next.frozen, fingerprint: fingerprint}}
	}
	return out, nil
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
