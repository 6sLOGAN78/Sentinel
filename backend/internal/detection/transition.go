package detection

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// CheckpointMetadata checks the complete bounded envelope without authorizing a
// configuration change. Only PG's saved manifest and acknowledged drain can do that.
func CheckpointMetadata(cp Checkpoint) (Metadata, error) {
	if cp.Offset < 0 || len(cp.Metadata) == 0 || len(cp.Metadata) > MaxMetadataBytes {
		return Metadata{}, errors.New("invalid checkpoint bounds")
	}
	m, err := decodeCheckpointMetadata(cp.Metadata)
	if err != nil {
		return Metadata{}, err
	}
	if m.Format != metadataFormat || len(m.ConfigFingerprint) != 64 || m.ProcessedNext < cp.Offset ||
		len(m.RuleVersions) != ruleCount || len(m.RuleEffectiveFrom) != ruleCount ||
		!validDecisionFrontier(m.DecisionFrontier, m.ProcessedNext) {
		return Metadata{}, errors.New("invalid checkpoint configuration")
	}
	for _, id := range ruleIDs() {
		gate, present := m.RuleEffectiveFrom[id]
		if m.RuleVersions[id] < 1 || !present || gate < 0 || gate > 0 && !contracts.ValidWindow(time.UnixMilli(gate)) {
			return Metadata{}, errors.New("invalid checkpoint rule manifest")
		}
	}
	if err = validateCheckpointContext(m, cp.Offset); err != nil {
		return Metadata{}, err
	}
	return m, nil
}

// TransitionCheckpoint validates a drained handover against the saved rules.
// The caller must prove that Kafka acknowledged this checkpoint and that all
// old owners stopped; this pure function cannot authorize a PG generation.
func TransitionCheckpoint(previous, next *Rules, cp Checkpoint, sealed int64) (Checkpoint, error) {
	if err := NewPartitionState(previous).Restore(cp); err != nil {
		return Checkpoint{}, err
	}
	if previous.Fingerprint() == next.Fingerprint() {
		return cp, nil
	}
	m, err := CheckpointMetadata(cp)
	if err != nil || !m.Drained || m.ProcessedNext != cp.Offset || sealed < 0 || len(next.Rules) != ruleCount {
		return Checkpoint{}, errors.New("configuration change requires verified drain")
	}
	if err = increasingRuleVersions(previous, next); err != nil {
		return Checkpoint{}, err
	}
	if m.SealedThrough != nil {
		sealed = max(sealed, *m.SealedThrough)
	}
	for _, gate := range m.RuleEffectiveFrom {
		sealed = max(sealed, gate)
	}
	for _, rule := range next.Rules {
		width := rule.Window.Milliseconds()
		if width < 1000 || width > time.Hour.Milliseconds() {
			return Checkpoint{}, errors.New("invalid transition window")
		}
		gate := sealed / width * width
		if gate < sealed {
			gate += width
		}
		if gate < 0 || gate > 0 && !contracts.ValidWindow(time.UnixMilli(gate)) {
			return Checkpoint{}, errors.New("configuration handover time out of bounds")
		}
		m.RuleEffectiveFrom[rule.ID] = gate
	}
	m.ConfigFingerprint, m.RuleVersions = next.Fingerprint(), next.Versions()
	m.DecisionFrontier = DecisionFrontier{None: true}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > MaxMetadataBytes {
		return Checkpoint{}, errors.New("configuration handover metadata exceeds bounds")
	}
	return Checkpoint{Offset: cp.Offset, Metadata: string(raw)}, nil
}

func increasingRuleVersions(previous, next *Rules) error {
	seen := make(map[string]bool, ruleCount)
	for _, newRule := range next.Rules {
		if seen[newRule.ID] {
			return errors.New("duplicate transition rule")
		}
		seen[newRule.ID] = true
		found := false
		for _, oldRule := range previous.Rules {
			if oldRule.ID != newRule.ID {
				continue
			}
			found = true
			oldVersion, newVersion := oldRule.Version, newRule.Version
			oldRule.Version, newRule.Version = 0, 0
			if newVersion < oldVersion || oldRule != newRule && newVersion <= oldVersion {
				return errors.New("changed rule requires strictly increased version")
			}
		}
		if !found {
			return errors.New("unknown transition rule")
		}
	}
	return nil
}
