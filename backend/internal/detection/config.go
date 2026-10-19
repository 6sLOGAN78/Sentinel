package detection

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	yamlInteger = "!!int"
	yamlString  = "!!str"
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
	var document yaml.Node
	if err = yaml.Unmarshal(b, &document); err != nil || !completeRulesDocument(&document) {
		return nil, errors.New("rules require complete explicit settings without aliases or merges")
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
		if !slices.Contains(ruleIDs(), r.ID) || seen[r.ID] || r.Version < 1 || r.Version > math.MaxInt32 || r.Threshold < 1 ||
			r.Threshold > MaxWindowEvents || r.Window < time.Second || r.Window > time.Hour ||
			r.Window%time.Millisecond != 0 || r.Cooldown < time.Millisecond || r.Cooldown > time.Hour ||
			r.Cooldown%time.Millisecond != 0 {
			return nil, errors.New("invalid rule settings")
		}
		if !slices.Contains([]string{"low", "medium", "high", "critical"}, r.Severity) {
			return nil, errors.New("invalid rule severity")
		}
		seen[r.ID] = true
	}
	slices.SortFunc(rules.Rules, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	return &rules, nil
}

func completeRulesDocument(document *yaml.Node) bool {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return false
	}
	root := document.Content[0]
	if !exactRuleMapping(root, map[string]string{"schema_version": yamlInteger, "rules": "!!seq"}) {
		return false
	}
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value != "rules" {
			continue
		}
		list := root.Content[i+1]
		if len(list.Content) != ruleCount {
			return false
		}
		for _, rule := range list.Content {
			if !exactRuleMapping(rule, map[string]string{"id": yamlString, "version": yamlInteger, "enabled": "!!bool",
				"threshold": yamlInteger, "window": yamlString, "severity": yamlString, "cooldown": yamlString}) {
				return false
			}
		}
	}
	return true
}

func exactRuleMapping(node *yaml.Node, keys map[string]string) bool {
	if node.Kind != yaml.MappingNode || len(node.Content) != 2*len(keys) || node.Anchor != "" {
		return false
	}
	seen := make(map[string]bool, len(keys))
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		want, present := keys[key.Value]
		if !present || seen[key.Value] || key.Tag != yamlString || value.Tag != want ||
			value.Kind == yaml.AliasNode || value.Anchor != "" {
			return false
		}
		seen[key.Value] = true
	}
	return true
}

// Fingerprint binds replay to sorted definitions and fixed semantic policies.
func (r *Rules) Fingerprint() string {
	canonical := Rules{SchemaVersion: r.SchemaVersion, Rules: slices.Clone(r.Rules)}
	slices.SortFunc(canonical.Rules, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	// A policy change requires an explicit new generation, just like YAML edits.
	b, _ := json.Marshal(struct {
		Rules       Rules  `json:"rules"`
		Semantics   string `json:"semantics"`
		LatenessMS  int64  `json:"lateness_ms"`
		MaxSamples  int    `json:"max_samples"`
		WindowLimit int    `json:"window_limit"`
		EventLimit  int    `json:"event_limit"`
		LedgerLimit int    `json:"ledger_limit"`
		StateLimit  int    `json:"state_limit"`
	}{canonical, "predicates.v1/epoch-half-open-ms.v1/unique-events-ports.v1/first-threshold-evidence.v1/" +
		"monotone-event-cooldown.v1/fixed-rule-ordinals.v1/dedupe.v1", Lateness.Milliseconds(), maxSamples,
		MaxWindows, MaxWindowEvents, MaxLedgerEntries, MaxStateBytes})
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
