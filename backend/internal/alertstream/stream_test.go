package alertstream //nolint:testpackage // Queue/admission/clock-model checks require private state.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

func TestStreamConfigBoundaries(t *testing.T) {
	for key, value := range map[string]string{
		"SENTINEL_PRIMARY.ENV": "test", "SENTINEL_DATABASE.HOST": "localhost",
		"SENTINEL_DATABASE.USER": "unit", "SENTINEL_DATABASE.NAME": "unit",
		"SENTINEL_REDIS.ADDRESS": "localhost:6379", "SENTINEL_CLICKHOUSE.HTTP_ADDRESS": "localhost:8123",
		"SENTINEL_CLICKHOUSE.NATIVE_ADDRESS": "localhost:9000", "SENTINEL_AUTH.JWT_SECRET": strings.Repeat("u", 48),
	} {
		t.Setenv(key, value)
	}
	_, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	for _, tc := range []struct{ key, value string }{
		{"clients", "0"}, {"clients", "-1"}, {"clients", "101"},
		{"queue", "0"}, {"queue", "33"}, {"queue", "9223372036854775807"},
		{"write_timeout", "0s"}, {"write_timeout", "-1s"}, {"write_timeout", "6s"},
		{"lifetime", "0s"}, {"lifetime", "-1s"}, {"lifetime", "11m"},
		{"lifetime", "9223372036854775807s"},
	} {
		t.Run(tc.key+"_"+tc.value, func(t *testing.T) {
			t.Setenv("SENTINEL_STREAM."+strings.ToUpper(tc.key), tc.value)
			_, loadErr := config.Load(config.ServiceQueryAPI)
			require.Error(t, loadErr, "runtime config must reject unsafe stream bounds")
		})
	}
	for key, value := range map[string]string{"CLIENTS": "2", "QUEUE": "1", "WRITE_TIMEOUT": "100ms", "LIFETIME": "1s"} {
		t.Setenv("SENTINEL_STREAM."+key, value)
	}
	cfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	h := NewHandler(t.Context(), nil, cfg.Stream)
	require.Equal(t, 2, cap(h.clients))
	hints, detach := h.subscribe(func() {})
	defer detach()
	require.Equal(t, 1, cap(hints))
	require.Equal(t, 100*time.Millisecond, h.settings.WriteTimeout)
	now := time.Now()
	require.Equal(t, now.Add(time.Second), h.deadline(now, now.Add(time.Hour)))
	require.NoError(t, h.Close(t.Context()))
}

func TestStreamLifetimeMODEL(t *testing.T) {
	// MODEL: advance the supplied clock; this does not claim ten minutes of
	// native wall-clock observation. The same deadline operation owns expiry.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := NewHandler(t.Context(), nil)
	defer func() { require.NoError(t, h.Close(t.Context())) }()
	for _, token := range []time.Time{now.Add(time.Minute), now.Add(time.Hour)} {
		deadline := h.deadline(now, token)
		require.LessOrEqual(t, deadline.Sub(now), 10*time.Minute)
		require.False(t, deadline.After(token))
		require.False(t, now.Add(10*time.Minute).Before(deadline))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, writeStream(ctx, http.NewResponseController(httptest.NewRecorder()),
		httptest.NewRecorder(), nil, streamWriteTimeout))
}

func TestStreamDirectConfigCapsAndAdmission(t *testing.T) {
	h := NewHandler(t.Context(), nil, config.StreamConfig{Clients: 101, Queue: 33,
		WriteTimeout: time.Duration(1<<63 - 1), Lifetime: time.Duration(1<<63 - 1)})
	require.Equal(t, 100, cap(h.clients))
	require.Equal(t, 32, h.settings.Queue)
	require.Equal(t, 5*time.Second, h.settings.WriteTimeout)
	require.Equal(t, 10*time.Minute, h.settings.Lifetime)
	for range 100 {
		require.True(t, h.admit())
	}
	require.False(t, h.admit())
	for range 100 {
		<-h.clients
		h.active.Done()
	}
	require.NoError(t, h.Close(t.Context()))
	require.False(t, h.admit())
}

func TestListenerUnavailableCancellation(t *testing.T) {
	h := NewHandler(t.Context(), nil)
	listener, err := NewListener("postgres://unit@127.0.0.1:1/unit?sslmode=disable", h)
	require.NoError(t, err)
	require.Error(t, listener.Ready(t.Context()))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx) }()
	cancel()
	select {
	case err = <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("listener cancellation did not join")
	}
	require.Error(t, listener.Ready(t.Context()))
	require.NoError(t, h.Close(t.Context()))
}

func TestStreamHintQueueBoundsAndCancellation(t *testing.T) {
	h := NewHandler(context.Background(), nil)
	// Assert an actual behavioral capability without making absent symbols a
	// compilation failure during RED. Production handlers own this registry.
	registry, ok := any(h).(interface {
		subscribe(context.CancelFunc) (<-chan struct{}, func())
		notify()
	})
	require.True(t, ok, "handler must own a bounded notification subscription registry")
	slow, cancelSlow := context.WithCancel(context.Background())
	hints, unsubscribe := registry.subscribe(cancelSlow)
	defer unsubscribe()
	require.Equal(t, 32, cap(hints))
	for range 32 {
		registry.notify()
	}
	require.Len(t, hints, 32)
	require.NoError(t, slow.Err())
	other, cancelOther := context.WithCancel(context.Background())
	defer cancelOther()
	otherHints, detach := registry.subscribe(cancelOther)
	defer detach()
	started := time.Now()
	registry.notify()
	require.Less(t, time.Since(started), time.Second)
	require.ErrorIs(t, slow.Err(), context.Canceled, "33rd hint cancels and detaches only the full subscriber")
	require.NoError(t, other.Err())
	require.Len(t, otherHints, 1)
	unsubscribe()
	unsubscribe()
	require.NoError(t, h.Close(t.Context()))
	closed, cancelClosed := context.WithCancel(context.Background())
	defer cancelClosed()
	_, detachClosed := registry.subscribe(cancelClosed)
	defer detachClosed()
	require.ErrorIs(t, closed.Err(), context.Canceled)
}
