package opshttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
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
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
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
	unblock()
	require.Eventually(t, func() bool {
		recoveryResponse, recoveryErr := client.Get(server.URL + "/readyz")
		if recoveryErr != nil {
			return false
		}
		defer recoveryResponse.Body.Close()
		return recoveryResponse.StatusCode == http.StatusOK
	}, time.Second, 10*time.Millisecond, "admission must recover when the stuck checks return")
}

func TestReadyzDependencyPanicReleasesAdmission(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	handler := opshttp.NewHandler(time.Second, &opshttp.Draining{}, []opshttp.Check{{
		Name: "kafka",
		Fn: func(context.Context) error {
			calls.Add(1)
			panic("internal dependency details")
		},
	}})
	for range 20 {
		status, body := probe(t, handler)
		require.Equal(t, http.StatusServiceUnavailable, status)
		require.Equal(t, "unavailable", body.Dependencies["kafka"])
		require.NotContains(t, body.Raw, "internal dependency details")
	}
	require.Equal(t, int32(20), calls.Load(), "dependency panics must not leak admission slots")
}
