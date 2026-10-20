//go:build integration

package alertwriter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

const (
	reservationBarrierMaximum = 30 * time.Second
	reservationBarrierPoll    = 10 * time.Millisecond
)

//nolint:gochecknoglobals // Integration-only, process-scoped once-only boundary.
var reservationBarrierUsed atomic.Bool

func afterReservationBeforeInsert(ctx context.Context, id string) error {
	path := os.Getenv("SENTINEL_TEST_ALERT_WRITER_AFTER_RESERVATION_BARRIER")
	if path == "" || os.Getenv("SENTINEL_TEST_ALERT_WRITER_ALERT_ID") != id ||
		!reservationBarrierUsed.CompareAndSwap(false, true) {
		return nil
	}
	// #nosec G703 -- trusted integration runner owns this private directory.
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("alert reservation test directory must be private")
	}
	// #nosec G304 G703 -- exclusive creation refuses existing files and symlinks.
	file, err := os.OpenFile(path+".reached", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("create alert reservation test marker")
	}
	if err = file.Close(); err != nil {
		return errors.New("close alert reservation test marker")
	}
	waitCtx, cancel := context.WithTimeout(ctx, reservationBarrierMaximum)
	defer cancel()
	ticker := time.NewTicker(reservationBarrierPoll)
	defer ticker.Stop()
	for {
		// #nosec G703 -- Lstat rejects symlinks; only a regular private release is accepted.
		info, err = os.Lstat(path + ".release")
		if err == nil {
			if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				return errors.New("invalid alert reservation test release")
			}
			return waitCtx.Err()
		}
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("inspect alert reservation test release")
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-ticker.C:
		}
	}
}
