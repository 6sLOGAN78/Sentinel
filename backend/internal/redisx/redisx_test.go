package redisx_test

import (
	"context"
	"strings"
	"testing"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/redisx"
)

func TestRedisUnreachableIsFatal(t *testing.T) {
	t.Parallel()

	c, err := redisx.New(context.Background(), config.RedisConfig{Address: "127.0.0.1:1"})
	if err == nil || c != nil {
		t.Fatalf("expected nil client and error, got %v %v", c, err)
	}

	if !strings.Contains(err.Error(), "pinging redis") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRedisCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c, err := redisx.New(ctx, config.RedisConfig{Address: "127.0.0.1:1"})
	if err == nil || c != nil {
		t.Fatalf("expected nil client and error, got %v %v", c, err)
	}
}
