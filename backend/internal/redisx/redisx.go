// Package redisx constructs the Redis client used for rate limiting.
package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const pingTimeout = 5 * time.Second

// New creates a Redis client and pings it. A failed ping closes the client and
// returns an error; callers must treat it as fatal at startup.
func New(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{Addr: cfg.Address})

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()

		return nil, fmt.Errorf("pinging redis: %w", err)
	}

	return client, nil
}
