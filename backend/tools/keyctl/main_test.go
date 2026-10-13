package main_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyctlHelpAndInvalidCommands(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "keyctl")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", path, ".")
	require.NoError(t, build.Run())
	help := exec.CommandContext(t.Context(), path, "--help")
	out, err := help.CombinedOutput()
	require.NoError(t, err)
	require.Contains(t, string(out), "30 seconds")
	for _, args := range [][]string{
		{}, {"unknown"}, {"create", "--tenant", "invalid tenant"}, {"revoke", "--key-id", "bad"},
	} {
		cmd := exec.CommandContext(t.Context(), path, args...)
		out, err = cmd.CombinedOutput()
		require.Error(t, err)
		require.NotContains(t, string(out), "invalid tenant")
	}
}
