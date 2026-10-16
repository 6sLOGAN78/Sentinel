package opshttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/opshttp"
)

func TestReadyzDeadlineRetainsBoundedAdmission(t *testing.T) {
	t.Parallel()
	const capacity = 16
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	handler := opshttp.NewHandler(20*time.Millisecond, &opshttp.Draining{}, []opshttp.Check{{
		Name: "kafka",
		Fn: func(context.Context) error {
			calls.Add(1)
			<-release // Model a client call that does not promptly honor cancellation.
			return nil
		},
	}})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: time.Second}
	for range capacity {
		response, err := client.Get(server.URL + "/readyz")
		require.NoError(t, err, "readiness must return even while the dependency remains blocked")
		var body struct {
			Dependencies map[string]string `json:"dependencies"`
		}
		require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
		require.Equal(t, "unavailable", body.Dependencies["kafka"])
	}
	response, err := client.Get(server.URL + "/readyz")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, int32(capacity), calls.Load(), "timed-out workers must retain their admission slots")
	response, err = client.Get(server.URL + "/healthz")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode, "readiness exhaustion must not affect liveness")
}
