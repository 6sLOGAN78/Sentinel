// Package logger builds zerolog loggers for Sentinel binaries.
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/rs/zerolog"
)

const (
	timeFormat      = "2006-01-02 15:04:05"
	maxFieldLen     = 200
	localEnv        = "local"
	jsonIndent      = "    "
	pgxLevelDebug   = 6
	pgxLevelInfo    = 4
	pgxLevelWarn    = 3
	pgxLevelError   = 2
	pgxLevelDefault = 0
)

// New creates a zerolog logger that writes to stdout and tags every line with
// the service name and environment.
func New(cfg *config.ObservabilityConfig, service string) zerolog.Logger {
	return NewWithWriter(cfg, service, os.Stdout)
}

// NewWithWriter is New with an explicit output. JSON is emitted in every
// environment except local, where a console writer is used.
func NewWithWriter(cfg *config.ObservabilityConfig, service string, w io.Writer) zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.GetLogLevel())
	if err != nil || level == zerolog.NoLevel {
		level = zerolog.InfoLevel
	}

	out := w
	if cfg.Environment == localEnv {
		out = zerolog.ConsoleWriter{Out: w, TimeFormat: timeFormat}
	}

	return zerolog.New(out).
		Level(level).
		With().
		Timestamp().
		Str("service", service).
		Str("environment", cfg.Environment).
		Logger()
}

// NewPgxLogger creates a pgx SQL logger.
func NewPgxLogger(level zerolog.Level) zerolog.Logger {
	writer := zerolog.ConsoleWriter{
		Out:              os.Stdout,
		TimeFormat:       timeFormat,
		FormatFieldValue: formatFieldValue,
	}

	return zerolog.New(writer).
		Level(level).
		With().
		Timestamp().
		Str("component", "database").
		Logger()
}

func formatFieldValue(i any) string {
	switch v := i.(type) {
	case string:
		if len(v) > maxFieldLen {
			return v[:maxFieldLen] + "..."
		}

		return v
	case []byte:
		var obj any
		if json.Unmarshal(v, &obj) == nil {
			if pretty, merr := json.MarshalIndent(obj, "", jsonIndent); merr == nil {
				return "\n" + string(pretty)
			}
		}

		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
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
