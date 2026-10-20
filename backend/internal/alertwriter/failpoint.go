//go:build !integration

package alertwriter

import "context"

// Production binaries never read fault controls or touch marker files.
func afterReservationBeforeInsert(context.Context, string) error { return nil }
