//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDetectionScenariosPreservedRepeat is deliberately execution-owned by
// 03-13/14, after seed_scenarios.py implements the private output contract below.
// It must never be used as an assertion RED against an absent operator command.
// The operator verifier reconciles Kafka/CH/PG, late metrics, drained checkpoints
// and the prior manifest; no new group, reset, topic or volume deletion is allowed.
func TestDetectionScenariosPreservedRepeat(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 32*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "scripts", "seed_scenarios.py"), "03-13 must supply orchestration before this acceptance is executed")
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	runPreservedCommand(t, ctx, root, nil, "python3", "scripts/seed.py")
	legacyBytes, readLegacyErr := os.ReadFile(filepath.Join(root, ".local", "last-send.json"))
	require.NoError(t, readLegacyErr)
	require.NoError(t, os.WriteFile(legacy, legacyBytes, 0o600))
	previous := legacy
	var previousIDs map[string]bool
	for _, name := range []string{"first", "second"} {
		output := filepath.Join(dir, name+".json")
		// 03-13 provides --output and --preserve-manifest: the latter must read
		// prior acknowledged IDs and recheck their rows after this run, without
		// allowing historical rows to satisfy the current-run exact-set verifier.
		runPreservedCommand(t, ctx, root, nil, "python3", "scripts/seed_scenarios.py", "--output", output, "--preserve-manifest", previous)
		info, statErr := os.Stat(output)
		require.NoError(t, statErr)
		require.Zero(t, info.Mode().Perm()&0o077, "operator manifests must remain private")
		body, readErr := os.ReadFile(output)
		require.NoError(t, readErr)
		var manifest struct {
			EventIDs           []string `json:"event_ids"`
			Verified           bool     `json:"verified"`
			PriorRowsPreserved bool     `json:"prior_rows_preserved"`
			NewLateDrops       int      `json:"new_late_drops"`
			Drained            bool     `json:"drained"`
		}
		require.NoError(t, json.Unmarshal(body, &manifest))
		require.True(t, manifest.Verified, "actual Kafka/CH/PG reconciliation is required")
		require.True(t, manifest.PriorRowsPreserved)
		require.Zero(t, manifest.NewLateDrops)
		require.True(t, manifest.Drained)
		require.NotEmpty(t, manifest.EventIDs)
		ids := make(map[string]bool, len(manifest.EventIDs))
		for _, id := range manifest.EventIDs {
			require.False(t, previousIDs[id], "repeat reused a prior event identity")
			ids[id] = true
		}
		previousIDs = ids
		previous = output
	}
}

func runPreservedCommand(t *testing.T, ctx context.Context, root string, extra []string, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), extra...)
	// Child tools own sanitized diagnostics; credentials only enter via env.
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "preserved operator command failed (%d diagnostic bytes)", len(out))
}
