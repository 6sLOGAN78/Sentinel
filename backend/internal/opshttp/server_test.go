package opshttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
)

func TestHealthz(t *testing.T) {
	draining := &opshttp.Draining{}
	handler := opshttp.NewHandler(time.Second, draining, []opshttp.Check{{
		Name: "database",
		Fn:   func(context.Context) error { return errors.New("password=secret host=db.internal") },
	}})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"status":"live"}`, recorder.Body.String())
}

func TestReadyzDependencies(t *testing.T) {
	var unavailable atomic.Bool
	handler := opshttp.NewHandler(time.Second, &opshttp.Draining{}, []opshttp.Check{
		{Name: "database", Fn: func(context.Context) error {
			if unavailable.Load() {
				return errors.New("postgres://sentinel:secret@db.internal/sentinel")
			}
			return nil
		}},
		{Name: "redis", Fn: func(context.Context) error { return nil }},
	})

	status, body := probe(t, handler)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, map[string]string{"database": "ok", "redis": "ok"}, body.Dependencies)

	unavailable.Store(true)
	status, body = probe(t, handler)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "unavailable", body.Dependencies["database"])
	require.NotContains(t, body.Raw, "secret")
	require.NotContains(t, body.Raw, "db.internal")
}

func TestReadyzRecovery(t *testing.T) {
	var unavailable atomic.Bool
	unavailable.Store(true)
	handler := opshttp.NewHandler(time.Second, &opshttp.Draining{}, []opshttp.Check{{
		Name: "kafka",
		Fn: func(context.Context) error {
			if unavailable.Load() {
				return errors.New("broker unavailable")
			}
			return nil
		},
	}})

	status, _ := probe(t, handler)
	require.Equal(t, http.StatusServiceUnavailable, status)
	unavailable.Store(false)
	status, _ = probe(t, handler)
	require.Equal(t, http.StatusOK, status)
}

func TestReadyzDraining(t *testing.T) {
	draining := &opshttp.Draining{}
	handler := opshttp.NewHandler(time.Second, draining, nil)

	status, _ := probe(t, handler)
	require.Equal(t, http.StatusOK, status)
	draining.Begin()
	status, body := probe(t, handler)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "draining", body.Status)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestReadyzTimeoutAndConcurrencyBound(t *testing.T) {
	started := make(chan struct{}, 32)
	release := make(chan struct{})
	check := opshttp.Check{Name: "slow", Fn: func(ctx context.Context) error {
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	handler := opshttp.NewHandler(5*time.Second, &opshttp.Draining{}, []opshttp.Check{check})

	client := httptest.NewServer(handler)
	t.Cleanup(client.Close)
	var wg sync.WaitGroup
	statuses := make(chan int, 17)
	for range 17 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := (&http.Client{Timeout: time.Second}).Get(client.URL + "/readyz")
			if err != nil {
				statuses <- 0
				return
			}
			defer response.Body.Close()
			statuses <- response.StatusCode
		}()
	}

	for range 16 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("sixteen readiness checks did not start")
		}
	}
	select {
	case status := <-statuses:
		require.Equal(t, http.StatusServiceUnavailable, status)
	case <-time.After(time.Second):
		t.Fatal("saturated readiness request did not return immediately")
	}
	close(release)
	wg.Wait()
	close(statuses)

	saturated := 1
	for status := range statuses {
		if status == http.StatusServiceUnavailable {
			saturated++
		}
	}
	require.Equal(t, 1, saturated)
	select {
	case <-started:
		t.Fatal("saturated request launched a dependency check")
	default:
	}
}

func TestReadyzUsesOneBoundedRequestContext(t *testing.T) {
	const checkTimeout = 30 * time.Millisecond
	secondCalled := false
	handler := opshttp.NewHandler(checkTimeout, &opshttp.Draining{}, []opshttp.Check{
		{Name: "slow", Fn: func(ctx context.Context) error {
			<-ctx.Done()

			return ctx.Err()
		}},
		{Name: "after", Fn: func(context.Context) error {
			secondCalled = true

			return nil
		}},
	})

	started := time.Now()
	status, body := probe(t, handler)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Less(t, time.Since(started), 500*time.Millisecond)
	require.Equal(t, "unavailable", body.Dependencies["slow"])
	require.Equal(t, "unavailable", body.Dependencies["after"])
	require.False(t, secondCalled)
}

func TestServerBindConflictFailsSynchronously(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })

	_, err = opshttp.Start(config.OpsConfig{
		BindAddress:  "127.0.0.1",
		Port:         strconv.Itoa(listener.Addr().(*net.TCPAddr).Port),
		CheckTimeout: time.Second,
	}, &opshttp.Draining{}, nil)
	require.ErrorContains(t, err, "listen")
}

func TestServerShutdownIsIdempotentAndReleasesListener(t *testing.T) {
	server, err := opshttp.Start(config.OpsConfig{
		BindAddress:  "127.0.0.1",
		Port:         "0",
		CheckTimeout: time.Second,
	}, &opshttp.Draining{}, nil)
	require.NoError(t, err)
	addr := server.Addr()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, server.Shutdown(ctx))
	require.NoError(t, server.Shutdown(ctx))
	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("serve goroutine did not stop")
	}
	require.NoError(t, server.Err())

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", addr)
	require.NoError(t, err, "ops listener leaked after shutdown")
	require.NoError(t, listener.Close())
}

func TestDrainCloserIsIdempotentAndBounded(t *testing.T) {
	draining := &opshttp.Draining{}
	closer := opshttp.DrainCloser(draining, time.Millisecond)
	require.NoError(t, closer(t.Context()))
	require.True(t, draining.Active())
	require.NoError(t, closer(t.Context()))

	bounded := opshttp.DrainCloser(&opshttp.Draining{}, time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, bounded(ctx), context.Canceled)
}

type readinessBody struct {
	Status       string            `json:"status"`
	Dependencies map[string]string `json:"dependencies"`
	Raw          string            `json:"-"`
}

func probe(t *testing.T, handler http.Handler) (int, readinessBody) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var body readinessBody
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	body.Raw = recorder.Body.String()

	return recorder.Code, body
}
