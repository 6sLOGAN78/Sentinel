package config_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

// Ignoring a decision deadline or permitting an unbounded pool must fail here.
func TestDetectorPGSettings(t *testing.T) {
	for _, key := range []string{
		"acquire_timeout", "transaction_timeout", "lock_timeout", "statement_timeout", "cleanup_timeout",
	} {
		for _, value := range []string{"0s", "-1s", "1ns", "2562048h", "31s"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				setCompleteEnv(t)
				t.Setenv("SENTINEL_DETECTOR.PG."+key, value)
				_, err := config.Load(config.ServiceDetector)
				require.Error(t, err, "detector must reject an unsafe PostgreSQL deadline")
			})
		}
	}
	for key, values := range map[string][]string{
		"max_open_conns":     {"0", "-1", "101", "4294967297"},
		"max_idle_conns":     {"0", "26"},
		"conn_max_lifetime":  {"0", "-1", strconv.FormatInt(1<<63-1, 10)},
		"conn_max_idle_time": {"0", "-1", strconv.FormatInt(1<<63-1, 10)},
	} {
		for _, value := range values {
			t.Run(key+"/"+value, func(t *testing.T) {
				setCompleteEnv(t)
				t.Setenv("SENTINEL_DATABASE."+key, value)
				_, err := config.Load(config.ServiceDetector)
				require.Error(t, err, "detector must reject unsafe pool bounds before duration conversion")
			})
		}
	}
	t.Run("valid defaults preserve writer", func(t *testing.T) {
		setCompleteEnv(t)
		cfg, err := config.Load(config.ServiceDetector)
		require.NoError(t, err)
		require.Equal(t, cfg.Writer, cfg.Detector.Writer)
		pg, err := config.ComputeDetectorPGBudgets(cfg.Detector.PG, cfg.Detector.Writer)
		require.NoError(t, err)
		require.Equal(t, 5*time.Second, pg.TransactionTimeout)
		require.Equal(t, time.Second, pg.LockTimeout)
		require.Equal(t, 2*time.Second, pg.StatementTimeout)
		require.Equal(t, time.Second, pg.CleanupTimeout)
		writer := cfg.Detector.Writer
		writer.MaxAttempts, writer.OperationTimeout, writer.ShutdownTimeout = 1, time.Second, time.Second
		bounded, err := config.ComputeDetectorPGBudgets(cfg.Detector.PG, writer)
		require.NoError(t, err)
		// This writer owns exactly three seconds of processing. Acquisition and
		// independent cleanup reserve 750ms each, leaving a 1500ms transaction.
		require.Equal(t, 750*time.Millisecond, bounded.AcquireTimeout)
		require.Equal(t, 1500*time.Millisecond, bounded.TransactionTimeout)
		require.Equal(t, 750*time.Millisecond, bounded.CleanupTimeout)
		require.Equal(t, 1500*time.Millisecond, bounded.StatementTimeout)
		require.LessOrEqual(t, bounded.LockTimeout, bounded.StatementTimeout)
	})
	for name, values := range map[string]map[string]string{
		"lock exceeds statement":        {"lock_timeout": "3s"},
		"statement exceeds transaction": {"statement_timeout": "6s"},
	} {
		t.Run(name, func(t *testing.T) {
			setCompleteEnv(t)
			for key, value := range values {
				t.Setenv("SENTINEL_DETECTOR.PG."+key, value)
			}
			_, err := config.Load(config.ServiceDetector)
			require.Error(t, err)
		})
	}
}
