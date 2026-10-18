package detection

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Rule is one finite, versioned startup definition.
type Rule struct {
	ID        string        `yaml:"id" json:"id"`
	Version   int           `yaml:"version" json:"version"`
	Enabled   bool          `yaml:"enabled" json:"enabled"`
	Threshold int           `yaml:"threshold" json:"threshold"`
	Window    time.Duration `yaml:"window" json:"window"`
	Severity  string        `yaml:"severity" json:"severity"`
	Cooldown  time.Duration `yaml:"cooldown" json:"cooldown"`
}

// Rules contains the complete immutable startup configuration.
type Rules struct {
	SchemaVersion int    `yaml:"schema_version" json:"schema_version"`
	Rules         []Rule `yaml:"rules" json:"rules"`
}

func ruleIDs() []string {
	return []string{authRuleID, "PORT_SCAN", "REQUEST_FLOOD", "DENY_SPIKE"}
}

// LoadRules rejects unknown fields, extra documents and unbounded input.
func LoadRules(reader io.Reader) (*Rules, error) {
	b, err := io.ReadAll(io.LimitReader(reader, MaxRuleBytes+1))
	if err != nil || len(b) > MaxRuleBytes {
		return nil, errors.New("rules input exceeds bounds")
	}
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	var rules Rules
	if err = d.Decode(&rules); err != nil {
		return nil, errors.New("invalid rules document")
	}
	var extra any
	if err = d.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("rules require exactly one document")
	}
	if rules.SchemaVersion != 1 || len(rules.Rules) != ruleCount {
		return nil, errors.New("rules require schema 1 and four definitions")
	}
	seen := make(map[string]bool)
	for _, r := range rules.Rules {
		if !slices.Contains(ruleIDs(), r.ID) || seen[r.ID] || r.Version < 1 || r.Threshold < 1 ||
			r.Threshold > MaxWindowEvents || r.Window < time.Second || r.Window > time.Hour ||
			r.Window%time.Millisecond != 0 || r.Cooldown <= 0 || r.Cooldown > time.Hour {
			return nil, errors.New("invalid rule settings")
		}
		if !slices.Contains([]string{"low", "medium", "high", "critical"}, r.Severity) {
			return nil, errors.New("invalid rule severity")
		}
		// Other predicates are enabled only by their subsequent feature plans.
		if r.Enabled && r.ID != authRuleID && r.ID != scanRuleID {
			return nil, errors.New("rule implementation not enabled")
		}
		seen[r.ID] = true
	}
	slices.SortFunc(rules.Rules, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	return &rules, nil
}

// Fingerprint binds replay to sorted effective definitions.
func (r *Rules) Fingerprint() string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Versions includes every rule, including disabled rules.
func (r *Rules) Versions() map[string]int {
	v := make(map[string]int, ruleCount)
	for _, rule := range r.Rules {
		v[rule.ID] = rule.Version
	}
	return v
}
