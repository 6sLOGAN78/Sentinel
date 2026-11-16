package testing_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

// TestContainerActionAllowlist pins the complete docker verb allowlist used by
// fault tests: pause, unpause, start and a bounded graceful stop. Every other
// verb (destructive or not) is refused before any process is executed.
func TestContainerActionAllowlist(t *testing.T) {
	t.Parallel()
	for action, want := range map[string][]string{
		"pause":   {"pause", "abc"},
		"unpause": {"unpause", "abc"},
		"start":   {"start", "abc"},
		"stop":    {"stop", "-t", "10", "abc"},
	} {
		args, err := sentineltesting.ContainerActionArgs(action, "abc")
		require.NoError(t, err, action)
		require.Equal(t, want, args, action)
	}
	for _, action := range []string{"rm", "kill", "restart", "exec", "system", "volume", "compose", "", "stop -f"} {
		_, err := sentineltesting.ContainerActionArgs(action, "abc")
		require.ErrorContains(t, err, "unsupported container action", action)
	}
}
