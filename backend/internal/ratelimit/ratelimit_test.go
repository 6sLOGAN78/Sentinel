package ratelimit_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/ratelimit"
)

func unavailableRedis(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr:                  "127.0.0.1:1",
		DialTimeout:           10 * time.Millisecond,
		ReadTimeout:           10 * time.Millisecond,
		WriteTimeout:          10 * time.Millisecond,
		MaxRetries:            -1,
		ContextTimeoutEnabled: true,
	})
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	return client
}

func TestLimiterFailurePolicy(t *testing.T) {
	t.Run("closed returns an error without incrementing bypasses", func(t *testing.T) {
		limiter := ratelimit.New(unavailableRedis(t), config.RateLimitConfig{
			Capacity: 500, RefillRate: 100, Timeout: 25 * time.Millisecond, FailMode: "closed",
		}, zerolog.Nop())

		allowed, retryAfter, err := limiter.Allow(t.Context(), "aaaaaaaaaaaa", 1)
		require.Error(t, err)
		require.False(t, allowed)
		require.Zero(t, retryAfter)
		require.Zero(t, limiter.BypassCount())
	})

	t.Run("open admits, counts, and logs only sanitized context", func(t *testing.T) {
		var logs bytes.Buffer
		limiter := ratelimit.New(unavailableRedis(t), config.RateLimitConfig{
			Capacity: 500, RefillRate: 100, Timeout: 25 * time.Millisecond, FailMode: "open",
		}, zerolog.New(&logs))

		allowed, retryAfter, err := limiter.Allow(t.Context(), "aaaaaaaaaaaa", 3)
		require.NoError(t, err)
		require.True(t, allowed)
		require.Zero(t, retryAfter)
		require.Equal(t, uint64(1), limiter.BypassCount())
		require.Contains(t, logs.String(), `"event":"ratelimit_bypass"`)
		require.NotContains(t, logs.String(), "aaaaaaaaaaaa")
		require.NotContains(t, logs.String(), "snt_")
	})
}

func TestLimiterRejectsInvalidCostWithoutRedis(t *testing.T) {
	limiter := ratelimit.New(unavailableRedis(t), config.RateLimitConfig{
		Capacity: 500, RefillRate: 100, Timeout: 25 * time.Millisecond, FailMode: "open",
	}, zerolog.Nop())

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	allowed, _, err := limiter.Allow(ctx, "aaaaaaaaaaaa", 0)
	require.Error(t, err)
	require.False(t, allowed)
	require.Zero(t, limiter.BypassCount())
}
