//go:build !integration

package alertwriter //nolint:testpackage // Verify the private production no-op directly.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAlertWriterFailpointBuildBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	dir := t.TempDir()
	t.Setenv("SENTINEL_TEST_ALERT_WRITER_AFTER_RESERVATION_BARRIER", filepath.Join(dir, "forbidden"))
	t.Setenv("SENTINEL_TEST_ALERT_WRITER_ALERT_ID", strings.Repeat("a", 64))
	require.NoError(t, afterReservationBeforeInsert(ctx, strings.Repeat("a", 64)))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
	list := exec.CommandContext(ctx, "go", "list", "-json", ".")
	raw, err := list.Output()
	require.NoError(t, err)
	var source struct {
		GoFiles []string `json:"GoFiles"`
	}
	require.NoError(t, json.Unmarshal(raw, &source))
	require.Contains(t, source.GoFiles, "failpoint.go")
	require.NotContains(t, source.GoFiles, "failpoint_integration.go")
	build := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(dir, "alert-writer"), "./cmd/alert-writer")
	build.Dir = "../.."
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
}
