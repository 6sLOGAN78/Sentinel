package logger_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/logger"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestLoggerJSONServiceField(t *testing.T) {
	cfg := config.DefaultObservabilityConfig()
	cfg.Environment = "production"

	var buf bytes.Buffer

	l := logger.NewWithWriter(cfg, "ingest", &buf)
	l.Info().Msg("hello")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	require.Equal(t, "ingest", line["service"])
	require.Equal(t, "production", line["environment"])
	require.Equal(t, "hello", line["message"])
}

func TestLoggerLevelFiltering(t *testing.T) {
	cfg := config.DefaultObservabilityConfig()
	cfg.Environment = "production"
	cfg.Logging.Level = "warn"

	var buf bytes.Buffer

	l := logger.NewWithWriter(cfg, "detector", &buf)
	l.Info().Msg("dropped")
	require.Empty(t, buf.String())

	l.Warn().Msg("kept")
	require.Contains(t, buf.String(), "kept")
}

func TestLoggerLocalUsesConsole(t *testing.T) {
	cfg := config.DefaultObservabilityConfig()
	cfg.Environment = "local"

	var buf bytes.Buffer

	l := logger.NewWithWriter(cfg, "ingest", &buf)
	l.Info().Msg("pretty")

	var line map[string]any
	require.Error(t, json.Unmarshal(buf.Bytes(), &line))
	require.Contains(t, buf.String(), "pretty")
}

func TestLoggerPgxLevels(t *testing.T) {
	require.Equal(t, 6, logger.GetPgxTraceLogLevel(zerolog.DebugLevel))
	require.Equal(t, 4, logger.GetPgxTraceLogLevel(zerolog.InfoLevel))
	require.Equal(t, 3, logger.GetPgxTraceLogLevel(zerolog.WarnLevel))
	require.Equal(t, 2, logger.GetPgxTraceLogLevel(zerolog.ErrorLevel))
	require.Equal(t, 0, logger.GetPgxTraceLogLevel(zerolog.TraceLevel))

	pl := logger.NewPgxLogger(zerolog.InfoLevel)
	require.Equal(t, zerolog.InfoLevel, pl.GetLevel())
}
