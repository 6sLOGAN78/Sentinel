package config_test

import (
	"testing"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDetectorSettings(t *testing.T) {
	setCompleteEnv(t)
	t.Setenv("SENTINEL_KAFKA.EVENTS_TOPIC", "config.events")
	t.Setenv("SENTINEL_KAFKA.ALERTS_TOPIC", "config.alerts")
	t.Setenv("SENTINEL_KAFKA.DETECTOR_GROUP", "config-detector")
	t.Setenv("SENTINEL_DETECTOR.RULES_PATH", "/private/config/rules.yaml")
	cfg, err := config.Load(config.ServiceDetector)
	require.NoError(t, err)
	require.Equal(t, "config.events", cfg.Kafka.EventsTopic)
	require.Equal(t, "config.alerts", cfg.Kafka.AlertsTopic)
	require.Equal(t, "config-detector", cfg.Kafka.DetectorGroup)
	require.NotEqual(t, cfg.Kafka.DetectorGroup, cfg.Kafka.EventWriterGroup)
	require.Equal(t, "/private/config/rules.yaml", cfg.Detector.RulesPath)
	t.Setenv("SENTINEL_DETECTOR.RULES_PATH", "   ")
	_, err = config.Load(config.ServiceDetector)
	require.Error(t, err, "blank paths must fail service validation")
	_, err = config.Load(config.ServiceIngest)
	require.NoError(t, err, "unrelated service config remains independent")
}
