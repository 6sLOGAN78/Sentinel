//go:build !integration

package eventwriter

import "context"

func afterInsertBeforeCommit(context.Context) error { return nil }
