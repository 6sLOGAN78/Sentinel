package app_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
)

func detectorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SENTINEL_PRIMARY.ENV", "test")
	t.Setenv("SENTINEL_KAFKA.BROKERS", "127.0.0.1:9092")
	t.Setenv("SENTINEL_DATABASE.HOST", "127.0.0.1")
	t.Setenv("SENTINEL_DATABASE.USER", "sentinel")
	t.Setenv("SENTINEL_DATABASE.NAME", "sentinel")
}

func TestCloseLIFO(t *testing.T) {
	t.Parallel()

	lc := app.NewLifecycle(zerolog.Nop())

	var order []string

	for _, name := range []string{"a", "b", "c"} {
		lc.Add(name, func(context.Context) error {
			order = append(order, name)

			return nil
		})
	}

	if err := lc.CloseAll(context.Background()); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}

	if got := strings.Join(order, ""); got != "cba" {
		t.Fatalf("close order = %q, want cba", got)
	}
}

func TestCloseErrorsAreJoined(t *testing.T) {
	t.Parallel()

	lc := app.NewLifecycle(zerolog.Nop())
	errB := errors.New("b failed")

	var closed []string

	lc.Add("a", func(context.Context) error { closed = append(closed, "a"); return nil })
	lc.Add("b", func(context.Context) error { closed = append(closed, "b"); return errB })
	lc.Add("c", func(context.Context) error { closed = append(closed, "c"); return nil })

	err := lc.CloseAll(context.Background())
	if !errors.Is(err, errB) {
		t.Fatalf("expected joined error containing b failure, got %v", err)
	}

	if got := strings.Join(closed, ""); got != "cba" {
		t.Fatalf("later closers must still run, closed = %q", got)
	}
}

func TestCloseRedisClient(t *testing.T) {
	t.Parallel()

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	lc := app.NewLifecycle(zerolog.Nop())
	lc.Add("redis", app.CloseRedis(client))

	if err := lc.CloseAll(context.Background()); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}

	err := client.Ping(context.Background()).Err()
	if err == nil || !strings.Contains(err.Error(), "client is closed") {
		t.Fatalf("expected closed client error, got %v", err)
	}
}

func TestCloseOnSetupErrorStillClosesInLIFO(t *testing.T) {
	detectorEnv(t)

	var order []string

	setup := func(_ context.Context, _ *config.Config, _ zerolog.Logger, lc *app.Lifecycle) error {
		lc.Add("one", func(context.Context) error { order = append(order, "one"); return nil })
		lc.Add("two", func(context.Context) error { order = append(order, "two"); return nil })

		return errors.New("boom")
	}

	// The context is never cancelled: Run must return without waiting.
	done := make(chan int, 1)

	go func() { done <- app.RunContext(context.Background(), config.ServiceDetector, setup) }()

	select {
	case code := <-done:
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run blocked after setup error")
	}

	if got := strings.Join(order, ","); got != "two,one" {
		t.Fatalf("close order = %q, want two,one", got)
	}
}

func TestShutdownAfterCancel(t *testing.T) {
	detectorEnv(t)

	closed := false
	started := make(chan struct{})

	//nolint:unparam // must match the app.SetupFunc signature
	setup := func(_ context.Context, _ *config.Config, _ zerolog.Logger, lc *app.Lifecycle) error {
		lc.Add("res", func(context.Context) error { closed = true; return nil })
		close(started)

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)

	go func() { done <- app.RunContext(ctx, config.ServiceDetector, setup) }()

	select {
	case <-started:
	case code := <-done:
		t.Fatalf("Run returned %d before setup started", code)
	case <-time.After(5 * time.Second):
		t.Fatal("setup did not start")
	}

	select {
	case code := <-done:
		t.Fatalf("Run returned %d before cancel", code)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	if !closed {
		t.Fatal("closer did not run on shutdown")
	}
}

func TestShutdownCloserFailureExitsNonZero(t *testing.T) {
	detectorEnv(t)

	setup := func(_ context.Context, _ *config.Config, _ zerolog.Logger, lc *app.Lifecycle) error {
		lc.Add("res", func(context.Context) error { return errors.New("stuck") })

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if code := app.RunContext(ctx, config.ServiceDetector, setup); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestFatalBackgroundFailureExitsNonZeroAndClosesLIFO(t *testing.T) {
	detectorEnv(t)

	var order []string
	workerErr := errors.New("writer retries exhausted")
	setup := func(_ context.Context, _ *config.Config, _ zerolog.Logger, lc *app.Lifecycle) error {
		lc.Add("dependency", func(context.Context) error { order = append(order, "dependency"); return nil })
		lc.Add("worker", func(context.Context) error { order = append(order, "worker"); return nil })
		lc.Fail(workerErr)

		return nil
	}

	require.Equal(t, 1, app.RunContext(context.Background(), config.ServiceDetector, setup))
	require.Equal(t, []string{"worker", "dependency"}, order)
}

func TestLifecycleFailKeepsFirstError(t *testing.T) {
	lc := app.NewLifecycle(zerolog.Nop())
	first := errors.New("first")
	second := errors.New("second")

	lc.Fail(first)
	lc.Fail(second)

	require.ErrorIs(t, lc.Err(), first)
	require.NotErrorIs(t, lc.Err(), second)
}

func TestFailureReportedDuringCleanupExitsNonZero(t *testing.T) {
	detectorEnv(t)
	workerErr := errors.New("worker failed during shutdown")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	setup := func(_ context.Context, _ *config.Config, _ zerolog.Logger, lc *app.Lifecycle) error {
		lc.Add("worker", func(context.Context) error {
			lc.Fail(workerErr)

			return nil
		})

		return nil
	}

	require.Equal(t, 1, app.RunContext(ctx, config.ServiceDetector, setup))
}

func TestClosePanicInSetupIsRecovered(t *testing.T) {
	detectorEnv(t)

	closed := false

	setup := func(_ context.Context, _ *config.Config, _ zerolog.Logger, lc *app.Lifecycle) error {
		lc.Add("res", func(context.Context) error { closed = true; return nil })

		panic("kaboom")
	}

	if code := app.RunContext(context.Background(), config.ServiceDetector, setup); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	if !closed {
		t.Fatal("closer did not run after panic")
	}
}

func TestCloseInvalidConfigExitsNonZero(t *testing.T) {
	t.Setenv("SENTINEL_PRIMARY.ENV", "test")
	t.Setenv("SENTINEL_KAFKA.BROKERS", "")

	called := false
	setup := func(context.Context, *config.Config, zerolog.Logger, *app.Lifecycle) error {
		called = true

		return nil
	}

	if code := app.RunContext(context.Background(), config.ServiceDetector, setup); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	if called {
		t.Fatal("setup must not run with invalid config")
	}
}

func TestDialTCP(t *testing.T) {
	t.Parallel()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	if err = app.DialTCP(context.Background(), ln.Addr().String(), time.Second); err != nil {
		t.Fatalf("dial listening port: %v", err)
	}

	start := time.Now()

	if err = app.DialTCP(context.Background(), "127.0.0.1:1", time.Second); err == nil {
		t.Fatal("expected error dialing closed port")
	}

	if time.Since(start) > 3*time.Second {
		t.Fatal("DialTCP exceeded its timeout")
	}
}

func TestDialBrokersChecksEvery(t *testing.T) {
	t.Parallel()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	good := ln.Addr().String()

	if err = app.DialBrokers(context.Background(), good+", "+good, time.Second); err != nil {
		t.Fatalf("all reachable: %v", err)
	}

	if err = app.DialBrokers(context.Background(), good+",127.0.0.1:1", time.Second); err == nil {
		t.Fatal("expected error when one broker is unreachable")
	}
}

func TestDialBrokersRejectsEmptyList(t *testing.T) {
	t.Parallel()

	for _, brokers := range []string{"", ",", " , ", "kafka"} {
		if err := app.DialBrokers(context.Background(), brokers, time.Second); err == nil {
			t.Fatalf("brokers %q: expected error, got nil", brokers)
		}
	}
}
