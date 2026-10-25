package alertstream

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
}

func TestStreamLifetimeMODEL(t *testing.T) {
	// MODEL: advance the supplied clock; this does not claim ten minutes of
	// native wall-clock observation. The same deadline operation owns expiry.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, token := range []time.Time{now.Add(time.Minute), now.Add(time.Hour)} {
		deadline := minTime(now.Add(streamLifetime), token)
		require.LessOrEqual(t, deadline.Sub(now), 10*time.Minute)
		require.False(t, deadline.After(token))
		require.False(t, now.Add(10*time.Minute).Before(deadline))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, writeStream(ctx, http.NewResponseController(httptest.NewRecorder()), httptest.NewRecorder(), nil))
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
