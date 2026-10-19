package detection

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func configDocument(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../../config/rules.yaml")
	require.NoError(t, err)
	return string(b)
}

func TestRulesConfig(t *testing.T) {
	raw := configDocument(t)
	_, err := LoadRules(strings.NewReader(raw))
	require.NoError(t, err)
	cases := map[string]string{
		"unknown":         raw + "extra: true\n",
		"case":            strings.Replace(raw, "enabled:", "Enabled:", 1),
		"duplicate key":   strings.Replace(raw, "enabled: true", "enabled: true\n    enabled: true", 1),
		"duplicate ID":    strings.Replace(raw, "PORT_SCAN", "AUTH_BRUTE_FORCE", 1),
		"missing enabled": strings.Replace(raw, "    enabled: true\n", "", 1),
		"null enabled":    strings.Replace(raw, "enabled: true", "enabled: null", 1),
		"alias": strings.Replace(strings.Replace(raw, "enabled: true", "enabled: &switch true", 1),
			"enabled: true", "enabled: *switch", 1),
		"merge":                   strings.Replace(raw, "    version: 1", "    <<: {version: 1}", 1),
		"trailing doc":            raw + "---\n",
		"trailing data":           raw + "broken [\n",
		"oversized":               raw + strings.Repeat("#", MaxRuleBytes),
		"schema":                  strings.Replace(raw, "schema_version: 1", "schema_version: 2", 1),
		"unknown rule":            strings.Replace(raw, "PORT_SCAN", "CUSTOM_SCAN", 1),
		"severity":                strings.Replace(raw, "severity: high", "severity: HIGH", 1),
		"missing field":           strings.Replace(raw, "    cooldown: 60s\n", "", 1),
		"zero threshold":          strings.Replace(raw, "threshold: 10", "threshold: 0", 1),
		"count capacity":          strings.Replace(raw, "threshold: 10", "threshold: 4097", 1),
		"integer overflow":        strings.Replace(raw, "threshold: 10", "threshold: 9999999999999999999999999", 1),
		"duration overflow":       strings.Replace(raw, "window: 300s", "window: 9999999999999999999h", 1),
		"small window":            strings.Replace(raw, "window: 300s", "window: 999ms", 1),
		"large window":            strings.Replace(raw, "window: 300s", "window: 3601s", 1),
		"submillisecond cooldown": strings.Replace(raw, "cooldown: 60s", "cooldown: 1ns", 1),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, loadErr := LoadRules(strings.NewReader(doc))
			require.Error(t, loadErr)
			require.NotContains(t, loadErr.Error(), doc)
		})
	}
}

func TestConfigFingerprint(t *testing.T) {
	rules, err := LoadRules(strings.NewReader(configDocument(t)))
	require.NoError(t, err)
	reversed := &Rules{SchemaVersion: rules.SchemaVersion, Rules: slices.Clone(rules.Rules)}
	slices.Reverse(reversed.Rules)
	require.Equal(t, rules.Fingerprint(), reversed.Fingerprint(), "semantic order is independent of YAML order")
	for name, change := range map[string]func(*Rule){
		"threshold": func(r *Rule) { r.Threshold++ }, "version": func(r *Rule) { r.Version++ },
		"window": func(r *Rule) { r.Window += time.Second }, "cooldown": func(r *Rule) { r.Cooldown += time.Second },
		"severity": func(r *Rule) { r.Severity = "critical" }, "enabled": func(r *Rule) { r.Enabled = !r.Enabled },
	} {
		t.Run(name, func(t *testing.T) {
			changed := &Rules{SchemaVersion: rules.SchemaVersion, Rules: slices.Clone(rules.Rules)}
			change(&changed.Rules[0])
			require.NotEqual(t, rules.Fingerprint(), changed.Fingerprint())
		})
	}
}
