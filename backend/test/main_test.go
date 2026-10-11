//go:build integration

package test_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/6sLOGAN78/sentinel/internal/config"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

// Shared fixtures started once per package in TestMain and read-only afterwards.
//
//nolint:gochecknoglobals // TestMain fixtures are shared by every test in the package
var (
	pgCfg    config.DatabaseConfig
	redisCfg config.RedisConfig
	kafkaCfg config.KafkaConfig
	chCfg    config.ClickHouseConfig
	binDir   string
)

var services = []string{"ingest", "detector", "event-writer", "alert-writer", "query-api"}

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "sentinel-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdir temp:", err)

		return 1
	}
	defer os.RemoveAll(dir)

	binDir = dir

	if err := buildBinaries(ctx, dir); err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 1
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		stops []func()
		errs  []error
	)

	record := func(stop func(), err error) {
		mu.Lock()
		defer mu.Unlock()

		if err != nil {
			errs = append(errs, err)

			return
		}

		stops = append(stops, stop)
	}

	wg.Add(4)

	go func() {
		defer wg.Done()

		cfg, stop, err := sentineltesting.StartPostgres(ctx)
		pgCfg = cfg
		record(stop, err)
	}()
	go func() {
		defer wg.Done()

		cfg, stop, err := sentineltesting.StartRedis(ctx)
		redisCfg = cfg
		record(stop, err)
	}()
	go func() {
		defer wg.Done()

		cfg, stop, err := sentineltesting.StartKafka(ctx)
		kafkaCfg = cfg
		record(stop, err)
	}()
	go func() {
		defer wg.Done()

		cfg, stop, err := sentineltesting.StartClickHouse(ctx)
		chCfg = cfg
		record(stop, err)
	}()

	wg.Wait()

	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()

	if len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "start containers:", errs)

		return 1
	}

	return m.Run()
}

func buildBinaries(ctx context.Context, dir string) error {
	for _, name := range services {
		cmd := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(dir, name), "./cmd/"+name)
		cmd.Dir = ".."

		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build %s: %w\n%s", name, err, out)
		}
	}

	return nil
}
