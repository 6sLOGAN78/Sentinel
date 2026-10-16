//go:build integration

package eventwriter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

const (
	writerBarrierEnv    = "SENTINEL_TEST_WRITER_AFTER_INSERT_BARRIER"
	barrierPollInterval = 10 * time.Millisecond
	barrierMaximumWait  = 30 * time.Second
	privateBarrierPerms = 0o600
)

var writerBarrierUsed atomic.Bool //nolint:gochecknoglobals // once-only integration fault per process

func afterInsertBeforeCommit(ctx context.Context) error {
	path := os.Getenv(writerBarrierEnv)
	if path == "" || !writerBarrierUsed.CompareAndSwap(false, true) {
		return nil
	}
	file, err := os.OpenFile(path+".reached", os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateBarrierPerms)
	if err != nil {
		return fmt.Errorf("create writer test barrier: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close writer test barrier: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, barrierMaximumWait)
	defer cancel()
	ticker := time.NewTicker(barrierPollInterval)
	defer ticker.Stop()
	for {
		if _, statErr := os.Stat(path + ".release"); statErr == nil {
			return nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect writer test barrier: %w", statErr)
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for writer test barrier release: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}
