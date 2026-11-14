// Package retry provides bounded capped-exponential backoff with full jitter.
// Replicas that fail together must not retry together, so every wait draws a
// uniform delay in [0, ceiling] instead of sleeping a fixed interval.
package retry

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"
)

const multiplier = 2

// Source returns a uniform value in the inclusive range [0, maximum].
type Source func(maximum int64) (int64, error)

// Waiter blocks for a jittered delay bounded by ceiling or until ctx ends.
type Waiter func(ctx context.Context, ceiling time.Duration) error

// Option customizes delay sampling.
type Option func(*options)

type options struct {
	source Source
}

// WithSource replaces the crypto/rand source, chiefly for deterministic tests.
func WithSource(source Source) Option {
	return func(o *options) {
		if source != nil {
			o.source = source
		}
	}
}

// Policy bounds retries by attempt count and a doubling, capped ceiling.
type Policy struct {
	Min      time.Duration
	Max      time.Duration
	Attempts int
}

// Validate rejects policies that would not back off or not terminate.
func (p Policy) Validate() error {
	if p.Min <= 0 || p.Max < p.Min || p.Attempts < 1 {
		return errors.New("validate retry policy: require 0 < min <= max and attempts >= 1")
	}
	return nil
}

// Ceiling is min(Min*2^(attempt-1), Max). Doubling stops at Max, so large
// attempt numbers cannot overflow.
func (p Policy) Ceiling(attempt int) time.Duration {
	ceiling := p.Min
	for n := 1; n < attempt && ceiling < p.Max; n++ {
		if ceiling > p.Max/multiplier {
			return p.Max
		}
		ceiling *= multiplier
	}
	return min(ceiling, p.Max)
}

// Delay draws a full-jitter delay in [0, Ceiling(attempt)].
func (p Policy) Delay(attempt int, opts ...Option) time.Duration {
	return jitter(p.Ceiling(attempt), opts)
}

// Wait sleeps a full-jitter delay in [0, ceiling] and returns ctx.Err() as soon
// as ctx ends.
func Wait(ctx context.Context, ceiling time.Duration, opts ...Option) error {
	timer := time.NewTimer(jitter(ceiling, opts))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Do runs op up to p.Attempts times with jittered waits between failures. It
// stops on success, context cancellation or the last attempt, and the returned
// error wraps the last operation error.
func Do(ctx context.Context, p Policy, op func(context.Context) error, opts ...Option) error {
	if err := p.Validate(); err != nil {
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= p.Attempts; attempt++ {
		lastErr = op(ctx)
		if lastErr == nil {
			return nil
		}
		if attempt == p.Attempts {
			break
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("retry stopped after %d attempts: %w", attempt, errors.Join(lastErr, err))
		}
		if err := Wait(ctx, p.Ceiling(attempt), opts...); err != nil {
			return fmt.Errorf("retry stopped after %d attempts: %w", attempt, errors.Join(lastErr, err))
		}
	}
	return fmt.Errorf("retry exhausted after %d attempts: %w", p.Attempts, lastErr)
}

func jitter(ceiling time.Duration, opts []Option) time.Duration {
	if ceiling <= 0 {
		return 0
	}
	o := options{source: cryptoSource}
	for _, opt := range opts {
		opt(&o)
	}
	value, err := o.source(int64(ceiling))
	if err != nil || value < 0 || value > int64(ceiling) {
		return ceiling
	}
	return time.Duration(value)
}

func cryptoSource(maximum int64) (int64, error) {
	bound := maximum
	if bound < math.MaxInt64 {
		bound++ // make maximum itself reachable
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(bound))
	if err != nil {
		return 0, fmt.Errorf("read jitter: %w", err)
	}
	return value.Int64(), nil
}
