package detection_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/detection"
)

const authRulesYAML = `schema_version: 1
rules:
  - {id: AUTH_BRUTE_FORCE, version: 1, enabled: true, threshold: 10, window: 300s, severity: high, cooldown: 60s}
  - {id: PORT_SCAN, version: 1, enabled: false, threshold: 20, window: 60s, severity: medium, cooldown: 30s}
  - {id: REQUEST_FLOOD, version: 1, enabled: false, threshold: 300, window: 60s, severity: high, cooldown: 30s}
  - {id: DENY_SPIKE, version: 1, enabled: false, threshold: 30, window: 120s, severity: medium, cooldown: 60s}
`

func loadAuthRules(t *testing.T) *detection.Rules {
	t.Helper()
	rules, err := detection.LoadRules(strings.NewReader(authRulesYAML))
	require.NoError(t, err)
	return rules
}

// All four implemented built-ins must load; unknown IDs still fail closed.
func TestRulesLoaderScan(t *testing.T) {
	for _, tt := range []struct {
		id    string
		valid bool
	}{
		{"PORT_SCAN", true},
		{"REQUEST_FLOOD", true},
		{"DENY_SPIKE", true},
	} {
		t.Run(tt.id, func(t *testing.T) {
			input := strings.Replace(authRulesYAML, "id: "+tt.id+", version: 1, enabled: false",
				"id: "+tt.id+", version: 1, enabled: true", 1)
			rules, err := detection.LoadRules(strings.NewReader(input))
			if tt.valid {
				require.NoError(t, err, "enabled SCAN must load before detection can run")
				require.Len(t, rules.Rules, 4)
			} else {
				require.Error(t, err)
			}
		})
	}
	_, err := detection.LoadRules(strings.NewReader(strings.Replace(authRulesYAML, "PORT_SCAN", "UNKNOWN_RULE", 1)))
	require.Error(t, err)
}

func TestRulesLoaderVolume(t *testing.T) {
	input := strings.ReplaceAll(authRulesYAML, "enabled: false", "enabled: true")
	rules, err := detection.LoadRules(strings.NewReader(input))
	require.NoError(t, err, "all implemented built-ins must reach the reducer")
	require.Len(t, rules.Rules, 4)
	for _, rule := range rules.Rules {
		require.True(t, rule.Enabled)
	}
	_, err = detection.LoadRules(strings.NewReader(strings.Replace(input, "REQUEST_FLOOD", "UNKNOWN_RULE", 1)))
	require.Error(t, err, "unknown enabled predicates must remain rejected")
}

// Removing KnownFields, the second-document EOF check or the bounded read must
// make the corresponding malformed-input case fail.
func TestRulesLoaderShape(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{"valid", authRulesYAML, true},
		{"trailing comment", authRulesYAML + "# end of document\n", true},
		{"unknown root field", authRulesYAML + "unknown: true\n", false},
		{"unknown rule field", strings.Replace(authRulesYAML, "threshold: 10", "unknown: true, threshold: 10", 1), false},
		{"second document", authRulesYAML + "---\nschema_version: 1\nrules: []\n", false},
		{"empty second document", authRulesYAML + "---\n", false},
		{"null second document", authRulesYAML + "---\nnull\n", false},
		{"malformed YAML", "schema_version: 1\nrules: [\n", false},
		{"wrong rules shape", "schema_version: 1\nrules: {}\n", false},
		{"duplicate mapping key", authRulesYAML + "schema_version: 1\n", false},
		{"empty document", "", false},
		{"at byte cap", authRulesYAML + "#" + strings.Repeat("x", (64<<10)-len(authRulesYAML)-1), true},
		{"over byte cap", authRulesYAML + "#" + strings.Repeat("x", (64<<10)-len(authRulesYAML)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, err := detection.LoadRules(strings.NewReader(tt.input))
			if !tt.valid {
				require.Error(t, err)
				require.Nil(t, rules)
				return
			}
			require.NoError(t, err)
			require.Len(t, rules.Rules, 4)
			require.Equal(t, 1, rules.SchemaVersion)
			require.Equal(t, detection.Rule{
				ID: "AUTH_BRUTE_FORCE", Version: 1, Enabled: true, Threshold: 10,
				Window: 5 * time.Minute, Severity: "high", Cooldown: time.Minute,
			}, rules.Rules[0])
			require.Equal(t, map[string]int{
				"AUTH_BRUTE_FORCE": 1, "PORT_SCAN": 1, "REQUEST_FLOOD": 1, "DENY_SPIKE": 1,
			}, rules.Versions())
			require.Equal(t, loadAuthRules(t).Fingerprint(), rules.Fingerprint())
		})
	}

	reader := strings.NewReader(strings.Repeat("x", 2*(64<<10)))
	_, err := detection.LoadRules(reader)
	require.Error(t, err)
	require.Equal(t, (64<<10)-1, reader.Len(), "read at most the cap plus one overflow byte")
}
