// Command detector runs the bounded event-time AUTH detection pipeline.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/app"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/detector"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/opshttp"
)

func main() {
	os.Exit(app.Run(config.ServiceDetector, setup))
}

func setup(ctx context.Context, cfg *config.Config, log zerolog.Logger, lc *app.Lifecycle) error {
	var opsServer *opshttp.Server
	draining := &opshttp.Draining{}
	metrics := &opshttp.DetectionMetrics{}
	fail := func(err error) { draining.Begin(); lc.Fail(err) }
	lc.Add("ops-http", func(closeCtx context.Context) error {
		if opsServer == nil {
			return nil
		}

		return opsServer.Shutdown(closeCtx)
	})
	pgBudgets, err := config.ComputeDetectorPGBudgets(cfg.Detector.PG, cfg.Detector.Writer)
	if err != nil {
		return err
	}
	db, err := openPostgres(ctx, cfg.Database, log, lc, pgBudgets)
	if err != nil {
		return err
	}

	rules, err := loadRules(cfg.Detector.RulesPath)
	if err != nil {
		return err
	}
	if err = checkConfigTransition(ctx, cfg); err != nil {
		return err
	}
	consumer, err := detector.NewKafkaConsumer(ctx, cfg.Kafka, cfg.Detector.Writer, rules, fail)
	if err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	consumer.EnableConfigTransition(cfg.Detector.AllowConfigTransition)
	lc.Add("kafka-consumer", consumer.Close)
	if err = consumer.Ping(ctx); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	publisher, err := detector.NewAlertProducer(ctx, cfg.Kafka)
	if err != nil {
		return err
	}
	lc.Add("kafka-alerts", publisher.Close)
	if err = publisher.Ping(ctx); err != nil {
		return err
	}
	worker := detector.New(consumer, publisher, rules, cfg.Detector.Writer,
		contracts.LimitsFromConfig(cfg.Contracts), db.Pool, pgBudgets, metrics)
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() {
		workerErr := worker.Run(workerCtx)
		if workerErr != nil {
			fail(workerErr)
		}
		workerDone <- workerErr
	}()
	lc.Add("detector", func(closeCtx context.Context) error {
		cancelWorker()
		select {
		case workerErr := <-workerDone:
			return workerErr
		case <-closeCtx.Done():
			return closeCtx.Err()
		}
	})

	opsServer, err = opshttp.Start(cfg.Ops, draining, []opshttp.Check{
		{Name: "kafka", Fn: consumer.Ping}, {Name: "alerts", Fn: publisher.Ping},
		{Name: "postgres", Fn: db.Pool.Ping},
	}, opshttp.WithDetectionMetrics(metrics))
	if err != nil {
		return fmt.Errorf("ops http: %w", err)
	}
	go func() {
		<-opsServer.Done()
		if serveErr := opsServer.Err(); serveErr != nil {
			lc.Fail(fmt.Errorf("ops http: %w", serveErr))
		}
	}()

	lc.Add("draining", opshttp.DrainCloser(draining, cfg.Ops.DrainHold))

	return nil
}

func checkConfigTransition(ctx context.Context, cfg *config.Config) error {
	if !cfg.Detector.AllowConfigTransition {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, cfg.Detector.Writer.OperationTimeout)
	defer cancel()
	return kafkax.VerifyStoppedDetectorGroup(checkCtx, cfg.Kafka)
}

func loadRules(path string) (*detection.Rules, error) {
	//nolint:gosec // Startup-only operator-owned path; LoadRules caps and strictly validates its contents.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open detector rules: %w", err)
	}
	rules, err := detection.LoadRules(file)
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	return rules, closeErr
}

func openPostgres(ctx context.Context, cfg config.DatabaseConfig, log zerolog.Logger,
	lc *app.Lifecycle, budgets config.DetectorPGConfig,
) (*database.Database, error) {
	connectCtx, cancelConnect := context.WithTimeout(ctx, budgets.AcquireTimeout)
	defer cancelConnect()
	// Driver connection errors may carry user/database identifiers. Disable
	// debug SQL tracing and expose only a stable dependency classification.
	db, err := database.New(connectCtx, cfg, log.Level(zerolog.InfoLevel))
	if err != nil {
		return nil, errors.New("postgres unavailable")
	}
	lc.Add("postgres", func(closeCtx context.Context) error {
		cleanupCtx, cancelCleanup := context.WithTimeout(closeCtx, budgets.CleanupTimeout)
		defer cancelCleanup()
		done := make(chan struct{})
		go func() { defer close(done); db.Close() }()
		select {
		case <-done:
			return nil
		case <-cleanupCtx.Done():
			return errors.New("postgres cleanup deadline exceeded")
		}
	})
	return db, nil
}
