//go:build integration

package kafkax

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	prefixBarrierEnv   = "SENTINEL_TEST_KAFKA_PREFIX_BARRIER"
	prefixCountEnv     = "SENTINEL_TEST_KAFKA_PREFIX_COUNT"
	prefixBarrierPoll  = 10 * time.Millisecond
	prefixBarrierPerms = 0o600
)

var prefixBarrierUsed atomic.Bool //nolint:gochecknoglobals // once-only integration fault per process

func publishWithPrefixBarrier(ctx context.Context, client *kgo.Client, records []*kgo.Record) (bool, error) {
	path := os.Getenv(prefixBarrierEnv)
	if path == "" || !prefixBarrierUsed.CompareAndSwap(false, true) {
		return false, nil
	}
	prefix, err := strconv.Atoi(os.Getenv(prefixCountEnv))
	if err != nil || prefix < 1 || prefix >= len(records) {
		return true, fmt.Errorf("invalid integration prefix count %q", os.Getenv(prefixCountEnv))
	}
	if err = client.ProduceSync(ctx, records[:prefix]...).FirstErr(); err != nil {
		return true, fmt.Errorf("publish acknowledged integration prefix: %w", err)
	}
	// #nosec G304 G703 -- integration-only barrier path is supplied by the trusted test runner.
	file, err := os.OpenFile(path+".reached", os.O_WRONLY|os.O_CREATE|os.O_EXCL, prefixBarrierPerms)
	if err != nil {
		return true, fmt.Errorf("create Kafka test barrier: %w", err)
	}
	if err = file.Close(); err != nil {
		return true, fmt.Errorf("close Kafka test barrier: %w", err)
	}

	ticker := time.NewTicker(prefixBarrierPoll)
	defer ticker.Stop()
	for {
		// #nosec G703 -- integration-only path selects a test-runner-owned release barrier.
		if _, statErr := os.Stat(path + ".release"); statErr == nil {
			break
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return true, fmt.Errorf("inspect Kafka test barrier: %w", statErr)
		}
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-ticker.C:
		}
	}
	if err = client.ProduceSync(ctx, records[prefix:]...).FirstErr(); err != nil {
		return true, err
	}

	return true, nil
}
