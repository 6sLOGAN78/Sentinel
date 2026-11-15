package testing_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

// TestFreePortsDistinct guards the harness contract that a service's server and
// ops ports never collide (separate FreePort calls can return the same port).
func TestFreePortsDistinct(t *testing.T) {
	t.Parallel()
	for range 50 {
		ports, err := sentineltesting.FreePorts(8)
		require.NoError(t, err)
		require.Len(t, ports, 8)
		seen := map[string]bool{}
		for _, port := range ports {
			require.False(t, seen[port], "duplicate port %s", port)
			seen[port] = true
		}
	}
}
