// Package detection implements deterministic event-time windows and replay pins.
package detection

import (
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
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
	windowBaseBytes  = 512
	eventEntryBytes  = 128
	portEntryBytes   = 32
	metadataFormat   = 2
)

// TopicPartition identifies independent Kafka replay state.
type TopicPartition struct {
	Topic     string
	Partition int32
}

// Checkpoint couples an exact resume pin with its recovery metadata.
type Checkpoint struct {
	Offset   int64
	Metadata string
}

// DecisionFrontier is explicit none until the durable PG decision slice.
type DecisionFrontier struct {
	None        bool   `json:"none"`
	Offset      int64  `json:"offset,omitempty"`
	RuleOrdinal int    `json:"rule_ordinal,omitempty"`
	AlertID     string `json:"alert_id,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// Metadata is the bounded format-2 recovery envelope.
type Metadata struct {
	Format             int              `json:"format"`
	ConfigFingerprint  string           `json:"config_fingerprint"`
	RuleVersions       map[string]int   `json:"rule_versions"`
	PrefixMaxEventTime *int64           `json:"prefix_max_event_time"`
	SealedThrough      *int64           `json:"sealed_through"`
	ProcessedNext      int64            `json:"processed_next"`
	RuleEffectiveFrom  map[string]int64 `json:"rule_effective_from"`
	DecisionFrontier   DecisionFrontier `json:"decision_frontier"`
	Drained            bool             `json:"drained"`
}

// Outcome contains frozen first-threshold alerts and lateness classification.
type Outcome struct {
	Alerts []*contracts.Alert
	Late   bool
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
	fired      bool
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
}

// NewPartitionState creates an empty partition with explicit effective gates.
func NewPartitionState(rules *Rules) *PartitionState {
	e := make(map[string]int64, ruleCount)
	for _, id := range ruleIDs() {
		e[id] = 0
	}
	return &PartitionState{
		rules: rules, windows: make(map[windowKey]*windowState), prefix: make(map[int64]*int64),
		last: -1, acknowledged: -1, effective: e,
	}
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
		bytes += windowBaseBytes + len(k.tenant) + len(k.rule) + len(k.ip) + len(k.principal) + len(w.ports)*portEntryBytes
		for id := range w.ids {
			bytes += eventEntryBytes + len(id)
		}
	}
	return windows, ledger, bytes
}

// Apply accepts one ordered source record; effects must be acknowledged next.
func (s *PartitionState) Apply(offset int64, event *contracts.Event) (Outcome, error) {
	if offset < 0 || offset <= s.last || s.last > s.acknowledged {
		return Outcome{}, errors.New("invalid source effect order")
	}
	if len(s.prefix) >= MaxLedgerEntries {
		return Outcome{}, errors.New("detector prefix capacity exceeded")
	}
	s.prefix[offset] = cloneTime(s.maximum)
	s.last = offset
	s.processedNext = max(s.processedNext, offset+1)
	t := event.OccurredAt.UnixMilli()
	late := s.maximum != nil && t < *s.maximum-Lateness.Milliseconds()
	if s.maximum == nil || t > *s.maximum {
		s.maximum = cloneTime(&t)
	}
	if late {
		return Outcome{Late: true}, nil
	}
	for _, r := range s.rules.Rules {
		if !r.Enabled {
			continue
		}
		switch r.ID {
		case authRuleID:
			if event.EventType == "auth.failure" && event.SourceIP != nil && event.Principal != nil && *event.Principal != "" {
				return s.applyWindow(offset, event, r, *event.Principal, nil)
			}
		case scanRuleID:
			if scanMatches(event) {
				return s.applyWindow(offset, event, r, *event.DestinationIP, event.DestinationPort)
			}
		}
	}
	return Outcome{}, nil
}

func (s *PartitionState) applyWindow(
	offset int64, event *contracts.Event, r Rule, second string, port *int,
) (Outcome, error) {
	t := event.OccurredAt.UnixMilli()
	start := t / r.Window.Milliseconds() * r.Window.Milliseconds()
	end := start + r.Window.Milliseconds()
	if start < s.effective[r.ID] || s.sealed != nil && end <= *s.sealed {
		return Outcome{}, nil
	}
	key := windowKey{tenant: event.TenantID, rule: r.ID, ip: *event.SourceIP, principal: second, start: start}
	w := s.windows[key]
	if w == nil {
		if len(s.windows) >= MaxWindows {
			return Outcome{}, errors.New("detector window capacity exceeded")
		}
		w = &windowState{first: offset, end: end, ids: make(map[string]struct{})}
		if port != nil {
			w.ports = make(map[int]struct{})
		}
		s.windows[key] = w
	}
	if _, ok := w.ids[event.EventID]; ok {
		return Outcome{}, nil
	}
	if len(w.ids) >= MaxWindowEvents {
		return Outcome{}, errors.New("detector event capacity exceeded")
	}
	w.ids[event.EventID] = struct{}{}
	if port != nil {
		w.ports[*port] = struct{}{}
	}
	if len(w.samples) < maxSamples {
		w.samples = append(w.samples, event.EventID)
	}
	count := len(w.ids)
	if port != nil {
		count = len(w.ports)
	}
	if w.fired || count < r.Threshold {
		return Outcome{}, nil
	}
	a := windowAlert(key, w, r)
	if err := a.Validate(); err != nil {
		return Outcome{}, errors.New("invalid canonical alert")
	}
	w.fired = true
	return Outcome{Alerts: []*contracts.Alert{a}}, nil
}

func windowAlert(key windowKey, w *windowState, r Rule) *contracts.Alert {
	start := time.UnixMilli(key.start).UTC()
	end := contracts.NewTimestamp(time.UnixMilli(w.end))
	entity := []string{key.ip, key.principal}
	display := strings.Join(entity, "|")
	summary := "Repeated failed logins against one user"
	if r.ID == scanRuleID {
		summary = "Distinct destination ports on one host"
	}
	return &contracts.Alert{
		SchemaVersion: 1, AlertID: contracts.DedupeKey(key.tenant, r.ID, entity, start),
		TenantID: key.tenant, RuleID: r.ID, RuleVersion: r.Version, Severity: r.Severity, Status: "open",
		CreatedAt: end, WindowStart: contracts.NewTimestamp(start), WindowEnd: end, EntityKey: display,
		Summary: summary, EventCount: len(w.ids), SampleEventIDs: slices.Clone(w.samples),
		DedupeKey: key.tenant + "|" + r.ID + "|" + display + "|" + start.Format(time.RFC3339Nano),
	}
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
				delete(s.windows, key)
			}
		}
	}
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
		RuleEffectiveFrom: s.effective, DecisionFrontier: DecisionFrontier{None: true}, Drained: len(s.windows) == 0,
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
	d := json.NewDecoder(strings.NewReader(cp.Metadata))
	d.DisallowUnknownFields()
	var m Metadata
	if err := d.Decode(&m); err != nil {
		return errors.New("invalid checkpoint metadata")
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return errors.New("trailing checkpoint data")
	}
	if m.Format != metadataFormat || m.ConfigFingerprint != s.rules.Fingerprint() || m.ProcessedNext < cp.Offset ||
		len(m.RuleVersions) != ruleCount || len(m.RuleEffectiveFrom) != ruleCount ||
		!m.DecisionFrontier.None || m.DecisionFrontier.Offset != 0 || m.DecisionFrontier.RuleOrdinal != 0 ||
		m.DecisionFrontier.AlertID != "" || m.DecisionFrontier.Fingerprint != "" {
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
	if m.PrefixMaxEventTime != nil && !contracts.ValidWindow(time.UnixMilli(*m.PrefixMaxEventTime)) ||
		m.SealedThrough != nil && !contracts.ValidWindow(time.UnixMilli(*m.SealedThrough)) {
		return errors.New("invalid checkpoint time")
	}
	if m.PrefixMaxEventTime != nil && m.SealedThrough == nil {
		return errors.New("checkpoint missing sealed floor")
	}
	if m.Drained && m.ProcessedNext != cp.Offset {
		return errors.New("invalid drained checkpoint")
	}
	s.maximum = cloneTime(m.PrefixMaxEventTime)
	s.sealed = cloneTime(m.SealedThrough)
	s.effective = m.RuleEffectiveFrom
	s.processedNext = m.ProcessedNext
	s.last = cp.Offset - 1
	s.acknowledged = s.last
	return nil
}

// Committed prunes only prefix entries protected by the broker's durable pin.
func (s *PartitionState) Committed(cp Checkpoint) {
	for offset := range s.prefix {
		if offset < cp.Offset {
			delete(s.prefix, offset)
		}
	}
}
