// Package logger builds zerolog loggers for Sentinel binaries.
package logger

import (
	"io"
	"os"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/rs/zerolog"
)

const (
	consoleTimeFormat = "2006-01-02 15:04:05"
	pgxLevelDebug     = 6
	pgxLevelInfo      = 4
	pgxLevelWarn      = 3
	pgxLevelError     = 2
	pgxLevelDefault   = 0
)

// timestampHook adds the spec section 11.3 "timestamp" field (UTC,
// RFC3339Nano) to every event without touching zerolog globals.
type timestampHook struct{}

// Run implements zerolog.Hook.
func (timestampHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	e.Str("timestamp", time.Now().UTC().Format(time.RFC3339Nano))
}

// New creates a zerolog logger that writes to stdout and tags every line with
// the service name and environment.
func New(cfg *config.ObservabilityConfig, service string) zerolog.Logger {
	return NewWithWriter(cfg, service, os.Stdout)
}

// NewWithWriter is New with an explicit output. Every environment emits one
// JSON object per line; a human console writer is used only when
// cfg.Logging.Format is "console".
func NewWithWriter(cfg *config.ObservabilityConfig, service string, w io.Writer) zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.GetLogLevel())
	if err != nil || level == zerolog.NoLevel {
		level = zerolog.InfoLevel
	}

	out := w
	if cfg.Logging.Format == config.LogFormatConsole {
		out = zerolog.ConsoleWriter{Out: w, TimeFormat: consoleTimeFormat}
	}

	return zerolog.New(out).
		Hook(timestampHook{}).
		Level(level).
		With().
		Str("service", service).
		Str("environment", cfg.Environment).
		Logger()
}

// NewPgxLogger creates a pgx SQL logger that writes JSON lines to stdout.
func NewPgxLogger(level zerolog.Level) zerolog.Logger {
	return zerolog.New(os.Stdout).
		Hook(timestampHook{}).
		Level(level).
		With().
		Str("component", "database").
		Logger()
}

// GetPgxTraceLogLevel converts a zerolog level to a pgx log level.
func GetPgxTraceLogLevel(level zerolog.Level) int {
	switch level { //nolint:exhaustive // every other level maps to the default pgx level
	case zerolog.DebugLevel:
		return pgxLevelDebug
	case zerolog.InfoLevel:
		return pgxLevelInfo
	case zerolog.WarnLevel:
		return pgxLevelWarn
	case zerolog.ErrorLevel:
		return pgxLevelError
	default:
		return pgxLevelDefault
	}
}
