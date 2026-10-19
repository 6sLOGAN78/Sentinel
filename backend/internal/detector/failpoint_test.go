//go:build !integration

package detector_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDetectorFailpointBuildBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	dir := t.TempDir()
	binary := filepath.Join(dir, "detector")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/detector")
	build.Dir = "../.."
	raw, err := build.CombinedOutput()
	require.NoError(t, err, string(raw))
	barrier := filepath.Join(dir, "forbidden")
	// Invalid operator configuration terminates this standalone production probe;
	// the real mid-window family also drives this default binary through publication.
	command := exec.CommandContext(ctx, binary)
	command.Dir = dir
	command.Env = append(os.Environ(), "SENTINEL_DETECTOR.RULES_PATH="+filepath.Join(dir, "absent.yaml"),
		"SENTINEL_TEST_DETECTOR_BOUNDARY=before-publish", "SENTINEL_TEST_DETECTOR_BARRIER="+barrier,
		"SENTINEL_TEST_DETECTOR_OBSERVATIONS="+dir)
	_, err = command.CombinedOutput()
	require.Error(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "default production binary must have no fault-control file effects")
}
