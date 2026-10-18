package config_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

// Ignoring a decision deadline or permitting an unbounded pool must fail here.
func TestDetectorPGSettings(t *testing.T) {
	for _, key := range []string{"acquire_timeout", "transaction_timeout", "lock_timeout", "statement_timeout", "cleanup_timeout"} {
		for _, value := range []string{"0s", "-1s", "2562048h", "31s"} {
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
