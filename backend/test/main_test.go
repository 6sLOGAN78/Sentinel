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
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
	"github.com/6sLOGAN78/sentinel/migrations"
)

// Shared fixtures started once per package in TestMain and read-only afterwards.
//
//nolint:gochecknoglobals // TestMain fixtures are shared by every test in the package
var (
	pgCfg                config.DatabaseConfig
	redisCfg             config.RedisConfig
	kafkaCfg             config.KafkaConfig
	chCfg                config.ClickHouseConfig
	binDir               string
	lifecycleTopic       string
	lifecycleGroup       string
	lifecycleAlertsTopic string
	lifecycleRulesPath   string
)

//nolint:gochecknoglobals // read-only service list shared by the integration tests
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

	if err = buildBinaries(ctx, dir); err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 1
	}
	// Pin one private rules snapshot for the whole fixture lifetime. Every
	// service process starts in a fresh directory and must use this absolute path.
	rules, err := os.ReadFile("../../config/rules.yaml")
	if err != nil {
		fmt.Fprintln(os.Stderr, "read lifecycle rules:", err)
		return 1
	}
	lifecycleRulesPath = filepath.Join(dir, "rules.yaml")
	if err = os.WriteFile(lifecycleRulesPath, rules, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "pin lifecycle rules:", err)
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

		cfg, stop, serr := sentineltesting.StartPostgres(ctx)
		pgCfg = cfg
		record(stop, serr)
	}()
	go func() {
		defer wg.Done()

		cfg, stop, serr := sentineltesting.StartRedis(ctx)
		redisCfg = cfg
		record(stop, serr)
	}()
	go func() {
		defer wg.Done()

		cfg, stop, serr := sentineltesting.StartKafka(ctx)
		kafkaCfg = cfg
		record(stop, serr)
	}()
	go func() {
		defer wg.Done()

		cfg, stop, serr := sentineltesting.StartClickHouse(ctx)
		chCfg = cfg
		record(stop, serr)
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

	migrateCtx, cancelMigrations := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelMigrations()
	if err = database.Migrate(migrateCtx, pgCfg, zerolog.Nop()); err != nil {
		fmt.Fprintln(os.Stderr, "migrate postgres:", err)

		return 1
	}
	if _, err = chmigrate.New(
		chCfg.HTTPAddress, chCfg.User, chCfg.Password, chCfg.Database, nil,
	).Run(migrateCtx, migrations.ClickHouse()); err != nil {
		fmt.Fprintln(os.Stderr, "migrate clickhouse:", err)

		return 1
	}
	lifecycleTopic, err = sentineltesting.CreateUniqueTopic(migrateCtx, kafkaCfg, "lifecycle")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create lifecycle topic:", err)

		return 1
	}
	lifecycleGroup = "lifecycle-" + uuid.NewString()
	lifecycleAlertsTopic, err = sentineltesting.CreateUniqueTopic(migrateCtx, kafkaCfg, "lifecycle-alerts")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create lifecycle alerts topic:", err)

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
