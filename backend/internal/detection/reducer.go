// Package detection implements deterministic event-time windows and replay pins.
package detection

import (
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// Admission limits bound replay state without evicting unexpired evidence.
const (
	MaxRuleBytes     = 64 << 10
	MaxWindows       = 10_000
	MaxWindowEvents  = 4096
	MaxLedgerEntries = 100_000
	MaxStateBytes    = 64 << 20
	MaxMetadataBytes = 1024
	Lateness         = 30 * time.Second
	authRuleID       = "AUTH_BRUTE_FORCE"
	ruleCount        = 4
	maxSamples       = 20
	ledgerEntryBytes = 64
	windowBaseBytes  = 1024
	eventEntryBytes  = 128
	portEntryBytes   = 32
	metadataFormat   = 2
)

// TopicPartition identifies independent Kafka replay state.
type TopicPartition struct {
	Topic     string
	Partition int32
}

// Outcome contains frozen first-threshold alerts and lateness classification.
type Outcome struct {
	Alerts     []*contracts.Alert
	Candidates []Candidate
	Late       bool
}
type windowKey struct {
	tenant, rule, ip, principal string
	start                       int64
}
type windowState struct {
	first, end int64
	ids        map[string]struct{}
	ports      map[int]struct{}
	samples    []string
	frozen     string
}

// PartitionState is used serially by one owner. Prefix entries retain only time
// maxima, never source payloads. The committed sealed floor is distinct from
// the maximum used to reproduce original lateness decisions on replay.
type PartitionState struct {
	rules               *Rules
	windows             map[windowKey]*windowState
	prefix              map[int64]*int64
	maximum, sealed     *int64
	last, processedNext int64
	acknowledged        int64
	effective           map[string]int64
	frontier            DecisionFrontier
	bounds              *Bounds
}

// NewPartitionState creates an empty partition with explicit effective gates.
func NewPartitionState(rules *Rules) *PartitionState {
	return NewPartitionStateWithBounds(rules, NewBounds(StateLimits{}))
}

func cloneTime(v *int64) *int64 {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

// Usage conservatively accounts for variable strings and map/entry overhead.
func (s *PartitionState) Usage() (int, int, int) {
	windows, ledger := len(s.windows), len(s.prefix)
	bytes := ledger * ledgerEntryBytes
	for k, w := range s.windows {
		bytes += windowBytes(k, w)
	}
	return windows, ledger, bytes
}

// Apply preserves the source-less reducer API for pure callers. It returns
// legacy alerts only; runtime callers use ApplySource to freeze source identity.
func (s *PartitionState) Apply(offset int64, event *contracts.Event) (Outcome, error) {
	return s.apply(nil, offset, event)
}

func (s *PartitionState) apply(source *TopicPartition, offset int64, event *contracts.Event) (Outcome, error) {
	if offset < 0 || offset == math.MaxInt64 || offset <= s.last || s.last > s.acknowledged {
		return Outcome{}, errors.New("invalid source effect order")
	}
	t := event.OccurredAt.UnixMilli()
	late := s.maximum != nil && t < *s.maximum-Lateness.Milliseconds()
	var changes []windowAdmission
	outcome := Outcome{Late: late}
	windows, bytes := 0, 0
	if !late {
		var err error
		changes, outcome, windows, bytes, err = s.prepare(source, offset, event)
		if err != nil {
			return Outcome{}, err
		}
	}
	if err := s.bounds.reserve(windows, 1, bytes+ledgerEntryBytes); err != nil {
		return Outcome{}, err
	}
	for _, change := range changes {
		change.state.ids[event.EventID] = struct{}{}
		if change.port != nil {
			change.state.ports[*change.port] = struct{}{}
		}
		s.windows[change.key] = change.state
	}
	s.prefix[offset] = cloneTime(s.maximum)
	s.last = offset
	s.processedNext = max(s.processedNext, offset+1)
	if s.maximum == nil || t > *s.maximum {
		s.maximum = cloneTime(&t)
	}
	return outcome, nil
}

func ruleGroup(event *contracts.Event, ruleID string) (string, *int, bool) {
	switch ruleID {
	case authRuleID:
		if event.EventType == "auth.failure" && event.SourceIP != nil && event.Principal != nil && *event.Principal != "" {
			return *event.Principal, nil, true
		}
	case scanRuleID:
		if scanMatches(event) {
			return *event.DestinationIP, event.DestinationPort, true
		}
	case floodRuleID, denyRuleID:
		return "", nil, volumeMatches(event, ruleID)
	}
	return "", nil, false
}

// Acknowledge seals only after all effects through this source offset succeed.
func (s *PartitionState) Acknowledge(throughOffset int64) error {
	if throughOffset != s.last {
		return errors.New("invalid effect acknowledgement")
	}
	s.acknowledged = throughOffset
	if s.maximum != nil {
		floor := *s.maximum - Lateness.Milliseconds()
		if s.sealed == nil || floor > *s.sealed {
			s.sealed = cloneTime(&floor)
		}
		for key, w := range s.windows {
			if w.end <= *s.sealed {
				s.bounds.windows--
				s.bounds.bytes -= windowBytes(key, w)
				delete(s.windows, key)
			}
		}
	}
	return nil
}

// AcknowledgePoison advances only source bookkeeping after durable quarantine.
// It cannot alter event clocks, windows, evidence or pending candidate effects.
func (s *PartitionState) AcknowledgePoison(offset int64) error {
	if offset < 0 || offset == math.MaxInt64 || offset <= s.last || s.last > s.acknowledged {
		return errors.New("invalid source effect order")
	}
	if err := s.bounds.reserve(0, 1, ledgerEntryBytes); err != nil {
		return err
	}
	s.prefix[offset] = cloneTime(s.maximum)
	s.last, s.acknowledged = offset, offset
	s.processedNext = max(s.processedNext, offset+1)
	return nil
}

// Checkpoint snapshots the pin and metadata after effects are acknowledged.
func (s *PartitionState) Checkpoint() (Checkpoint, error) {
	if s.last != s.acknowledged {
		return Checkpoint{}, errors.New("unacknowledged detector effects")
	}
	pin := s.last + 1
	prefix := s.maximum
	for _, w := range s.windows {
		pin = min(pin, w.first)
	}
	if pin <= s.last {
		var ok bool
		prefix, ok = s.prefix[pin]
		if !ok {
			return Checkpoint{}, errors.New("missing pre-pin maximum")
		}
	}
	m := Metadata{
		Format: metadataFormat, ConfigFingerprint: s.rules.Fingerprint(), RuleVersions: s.rules.Versions(),
		PrefixMaxEventTime: prefix, SealedThrough: s.sealed, ProcessedNext: s.processedNext,
		RuleEffectiveFrom: s.effective, DecisionFrontier: s.frontier, Drained: len(s.windows) == 0,
	}
	b, err := json.Marshal(m)
	if err != nil || len(b) > MaxMetadataBytes {
		return Checkpoint{}, errors.New("checkpoint exceeds metadata bounds")
	}
	return Checkpoint{Offset: pin, Metadata: string(b)}, nil
}

// Restore validates the complete recovery envelope before accepting replay.
func (s *PartitionState) Restore(cp Checkpoint) error {
	if cp.Offset < 0 || len(cp.Metadata) == 0 || len(cp.Metadata) > MaxMetadataBytes {
		return errors.New("invalid checkpoint bounds")
	}
	m, err := decodeCheckpointMetadata(cp.Metadata)
	if err != nil {
		return err
	}
	if m.Format != metadataFormat || m.ConfigFingerprint != s.rules.Fingerprint() || m.ProcessedNext < cp.Offset ||
		len(m.RuleVersions) != ruleCount || len(m.RuleEffectiveFrom) != ruleCount ||
		!validDecisionFrontier(m.DecisionFrontier, m.ProcessedNext) {
		return errors.New("checkpoint configuration mismatch")
	}
	for id, v := range s.rules.Versions() {
		if m.RuleVersions[id] != v {
			return errors.New("checkpoint rule version mismatch")
		}
		if gate, ok := m.RuleEffectiveFrom[id]; !ok || gate < 0 {
			return errors.New("invalid rule effective gate")
		}
	}
	if err = validateCheckpointContext(m, cp.Offset); err != nil {
		return err
	}
	s.maximum = cloneTime(m.PrefixMaxEventTime)
	s.sealed = cloneTime(m.SealedThrough)
	s.effective = m.RuleEffectiveFrom
	s.processedNext = m.ProcessedNext
	s.frontier = m.DecisionFrontier
	s.last = cp.Offset - 1
	s.acknowledged = s.last
	return nil
}

// Committed prunes only prefix entries protected by the broker's durable pin.
func (s *PartitionState) Committed(cp Checkpoint) {
	for offset := range s.prefix {
		if offset < cp.Offset {
			s.bounds.ledger--
			s.bounds.bytes -= ledgerEntryBytes
			delete(s.prefix, offset)
		}
	}
}
