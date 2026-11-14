package logger_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

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

func TestLoggerLocalEmitsJSON(t *testing.T) {
	cfg := config.DefaultObservabilityConfig()
	cfg.Environment = "local"
	cfg.Logging.Format = "json"

	var buf bytes.Buffer

	l := logger.NewWithWriter(cfg, "ingest", &buf)
	l.Info().Msg("structured")

	line := decodeSingleLine(t, buf.Bytes())
	require.Equal(t, "ingest", line["service"])
	require.Equal(t, "local", line["environment"])
	require.Equal(t, "info", line["level"])
	require.Equal(t, "structured", line["message"])
	require.NotContains(t, line, "time")
	requireUTCTimestamp(t, line)
}

func TestLoggerConsoleOptIn(t *testing.T) {
	cfg := config.DefaultObservabilityConfig()
	cfg.Environment = "production"
	cfg.Logging.Format = "console"

	var buf bytes.Buffer

	l := logger.NewWithWriter(cfg, "ingest", &buf)
	l.Info().Msg("pretty")

	var line map[string]any
	require.Error(t, json.Unmarshal(buf.Bytes(), &line))
	require.Contains(t, buf.String(), "pretty")
}

func TestPgxLoggerJSON(t *testing.T) {
	out := captureStdout(t, func() {
		pl := logger.NewPgxLogger(zerolog.InfoLevel)
		pl.Info().Str("sql", "SELECT 1").Msg("query")
	})

	line := decodeSingleLine(t, out)
	require.Equal(t, "database", line["component"])
	require.Equal(t, "query", line["message"])
	require.Equal(t, "SELECT 1", line["sql"])
	require.NotContains(t, line, "time")
	requireUTCTimestamp(t, line)
}

// decodeSingleLine asserts raw is exactly one JSON object line and decodes it.
func decodeSingleLine(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	trimmed := bytes.TrimRight(raw, "\n")
	require.NotContains(t, string(trimmed), "\n", "expected exactly one line")

	var line map[string]any
	require.NoError(t, json.Unmarshal(trimmed, &line), string(raw))

	return line
}

// requireUTCTimestamp asserts the timestamp field is RFC3339Nano in UTC.
func requireUTCTimestamp(t *testing.T, line map[string]any) {
	t.Helper()

	ts, ok := line["timestamp"].(string)
	require.True(t, ok, "timestamp must be a string field: %v", line)
	require.True(t, strings.HasSuffix(ts, "Z"), "timestamp must be UTC: %s", ts)

	parsed, err := time.Parse(time.RFC3339Nano, ts)
	require.NoError(t, err)
	require.Equal(t, time.UTC, parsed.Location())
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// fn wrote. NewPgxLogger binds os.Stdout when it is called, so fn must build
// the logger itself.
func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	orig := os.Stdout
	os.Stdout = w //nolint:reassign // NewPgxLogger's fixed signature binds os.Stdout; restored below

	defer func() { os.Stdout = orig }() //nolint:reassign // restores the original stdout

	fn()

	require.NoError(t, w.Close())

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	return out
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
