//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDetectionScenarios executes the exact private operator path against the
// real preserved local pipeline. Fixed historical fixture equality is guarded
// separately in the reducer and generator, never injected into a live group.
func TestDetectionScenarios(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 16*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	output := filepath.Join(t.TempDir(), "scenarios.json")
	runPreservedCommand(ctx, t, root, "python3", "-B", "scripts/seed_scenarios.py", "--output", output)
	raw, err := os.ReadFile(output)
	require.NoError(t, err)
	var proof struct {
		Verified bool              `json:"verified"`
		Drained  bool              `json:"drained"`
		Expected []json.RawMessage `json:"expected_alerts"`
	}
	require.NoError(t, json.Unmarshal(raw, &proof))
	require.True(t, proof.Verified)
	require.True(t, proof.Drained)
	require.Len(t, proof.Expected, 8, "two malicious crossings per rule and benign zero")
}
