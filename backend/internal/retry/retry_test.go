package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/retry"
)

func testPolicy() retry.Policy {
	return retry.Policy{Min: 100 * time.Millisecond, Max: 5 * time.Second, Attempts: 5}
}

func TestPolicyCeilingDoublesToCapWithoutOverflow(t *testing.T) {
	t.Parallel()
	p := testPolicy()
	want := []time.Duration{
		100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond,
		1600 * time.Millisecond, 3200 * time.Millisecond, 5 * time.Second, 5 * time.Second,
	}
	for n, expected := range want {
		require.Equal(t, expected, p.Ceiling(n+1), "attempt %d", n+1)
	}
	for _, n := range []int{62, 63, 64, 1000} {
		require.Equal(t, p.Max, p.Ceiling(n), "attempt %d must cap, not overflow", n)
	}
	require.Equal(t, p.Min, p.Ceiling(0), "non-positive attempts use the minimum")
}

func TestPolicyDelayIsFullJitterWithinCeiling(t *testing.T) {
	t.Parallel()
	p := testPolicy()
	for _, attempt := range []int{1, 3, 8} {
		ceiling := p.Ceiling(attempt)
		seen := make(map[time.Duration]struct{})
		for range 2000 {
			d := p.Delay(attempt)
			require.GreaterOrEqual(t, d, time.Duration(0))
			require.LessOrEqual(t, d, ceiling)
			seen[d] = struct{}{}
		}
		require.GreaterOrEqual(t, len(seen), 2, "delay must not be fixed at attempt %d", attempt)
	}
}

func TestPolicyDelayInjectableSourceAndFallback(t *testing.T) {
	t.Parallel()
	p := testPolicy()
	var bounds []int64
	atCeiling := retry.WithSource(func(maximum int64) (int64, error) {
		bounds = append(bounds, maximum)
		return maximum, nil
	})
	require.Equal(t, p.Ceiling(2), p.Delay(2, atCeiling))
	require.Equal(t, []int64{int64(p.Ceiling(2))}, bounds, "source receives the inclusive ceiling")

	failing := retry.WithSource(func(int64) (int64, error) { return 0, errors.New("rng unavailable") })
	require.Equal(t, p.Ceiling(3), p.Delay(3, failing), "RNG error falls back to the ceiling")

	outOfRange := retry.WithSource(func(maximum int64) (int64, error) { return maximum + 1, nil })
	require.Equal(t, p.Ceiling(1), p.Delay(1, outOfRange), "out-of-range source values fall back to the ceiling")

	zero := retry.WithSource(func(int64) (int64, error) { return 0, nil })
	require.Zero(t, p.Delay(4, zero))
}

func TestPolicyValidate(t *testing.T) {
	t.Parallel()
	require.NoError(t, testPolicy().Validate())
	require.NoError(t, retry.Policy{Min: time.Second, Max: time.Second, Attempts: 1}.Validate())
	for name, p := range map[string]retry.Policy{
		"zero min":     {Min: 0, Max: time.Second, Attempts: 3},
		"negative min": {Min: -time.Millisecond, Max: time.Second, Attempts: 3},
		"max below":    {Min: time.Second, Max: time.Millisecond, Attempts: 3},
		"no attempts":  {Min: time.Millisecond, Max: time.Second, Attempts: 0},
	} {
		require.Error(t, p.Validate(), name)
	}
}

func TestWaitStopsOnCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	atCeiling := retry.WithSource(func(maximum int64) (int64, error) { return maximum, nil })
	done := make(chan error, 1)
	go func() { done <- retry.Wait(ctx, 5*time.Second, atCeiling) }()
	time.Sleep(10 * time.Millisecond)
	cancelled := time.Now()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		require.Less(t, time.Since(cancelled), 50*time.Millisecond)
	case <-time.After(time.Second):
		t.Fatal("wait ignored cancellation")
	}
	require.NoError(t, retry.Wait(t.Context(), time.Millisecond))
}

func TestDoBoundsAttemptsAndWrapsLastError(t *testing.T) {
	t.Parallel()
	p := retry.Policy{Min: time.Millisecond, Max: 2 * time.Millisecond, Attempts: 4}
	calls := 0
	var last error
	err := retry.Do(t.Context(), p, func(context.Context) error {
		calls++
		last = errors.New("transient")
		return last
	})
	require.Equal(t, p.Attempts, calls)
	require.ErrorIs(t, err, last)
	require.Contains(t, err.Error(), "4 attempts")
}

func TestDoStopsAfterFirstSuccess(t *testing.T) {
	t.Parallel()
	p := retry.Policy{Min: time.Millisecond, Max: 2 * time.Millisecond, Attempts: 5}
	calls := 0
	err := retry.Do(t.Context(), p, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls)
}

func TestDoStopsOnCancellationAndRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	p := retry.Policy{Min: time.Hour, Max: time.Hour, Attempts: 3}
	calls := 0
	opErr := errors.New("transient")
	err := retry.Do(ctx, p, func(context.Context) error {
		calls++
		cancel()
		return opErr
	})
	require.Equal(t, 1, calls)
	require.ErrorIs(t, err, opErr)
	require.ErrorIs(t, err, context.Canceled)

	calls = 0
	err = retry.Do(t.Context(), retry.Policy{}, func(context.Context) error {
		calls++
		return nil
	})
	require.Error(t, err)
	require.Zero(t, calls, "invalid policy must not run the operation")
}
