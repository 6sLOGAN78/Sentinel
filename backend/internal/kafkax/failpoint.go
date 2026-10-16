//go:build !integration

package kafkax

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
)

func publishWithPrefixBarrier(context.Context, *kgo.Client, []*kgo.Record) (bool, error) {
	return false, nil
}
