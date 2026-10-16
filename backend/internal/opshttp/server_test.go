package opshttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
