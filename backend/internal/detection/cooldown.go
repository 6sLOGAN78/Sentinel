package detection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// DecisionFrontier returns the checkpoint's last fully effected PG decision.
func (s *PartitionState) DecisionFrontier() DecisionFrontier { return s.frontier }

// EffectDecision advances the marker only after PG and allowed publication finish.
func (s *PartitionState) EffectDecision(c Candidate) error {
	if c.Offset() != s.last || s.last <= s.acknowledged {
		return errors.New("decision outside pending source effect")
	}
	var a contracts.Alert
	if err := json.Unmarshal(c.CanonicalBytes(), &a); err != nil {
		return err
	}
	f := DecisionFrontier{Offset: c.Offset(), RuleOrdinal: c.RuleOrdinal(),
		AlertID: a.AlertID, Fingerprint: c.Fingerprint()}
	if !validDecisionFrontier(f, s.processedNext) {
		return errors.New("invalid effected decision frontier")
	}
	old := s.frontier
	if !old.None {
		if f.Offset < old.Offset || f.Offset == old.Offset && f.RuleOrdinal < old.RuleOrdinal {
			return nil
		}
		if f.Offset == old.Offset && f.RuleOrdinal == old.RuleOrdinal && f != old {
			return errors.New("decision frontier reconstruction mismatch")
		}
	}
	s.frontier = f
	return nil
}

func validDecisionFrontier(f DecisionFrontier, next int64) bool {
	if f.None {
		return f.Offset == 0 && f.RuleOrdinal == 0 && f.AlertID == "" && f.Fingerprint == ""
	}
	if f.Offset < 0 || f.Offset >= next || f.RuleOrdinal < 0 || f.RuleOrdinal >= ruleCount {
		return false
	}
	for _, value := range []string{f.AlertID, f.Fingerprint} {
		if len(value) != sha256.Size*2 {
			return false
		}
		if _, err := hex.DecodeString(value); err != nil {
			return false
		}
	}
	return true
}

// Cooldown advances the monotone candidate clock; only an allow moves eligibility.
// Zero previous/eligible values describe a fresh cursor. Equality is eligible.
func Cooldown(previous, eligible, trigger time.Time, duration time.Duration) (bool, time.Time, time.Time) {
	clock := trigger
	if previous.After(clock) {
		clock = previous
	}
	allowed := !clock.Before(eligible)
	if allowed {
		eligible = clock.Add(duration)
	}
	return allowed, clock, eligible
}
