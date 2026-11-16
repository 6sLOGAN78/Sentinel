package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"time"
)

// Log output formats accepted by LoggingConfig.Format.
const (
	// LogFormatJSON emits one JSON object per line (the default everywhere).
	LogFormatJSON = "json"
	// LogFormatConsole emits human-readable lines; explicit opt-in only.
	LogFormatConsole = "console"
)

// ObservabilityConfig holds logging settings for a binary.
type ObservabilityConfig struct {
	ServiceName string        `koanf:"service_name"`
	Environment string        `koanf:"environment"`
	Logging     LoggingConfig `koanf:"logging"`
	Tracing     TracingConfig `koanf:"tracing"`
}

// TracingConfig controls OpenTelemetry span export. The zero value disables
// export; spans then still carry trace IDs for log correlation.
type TracingConfig struct {
	Enabled      bool    `koanf:"enabled"`
	SampleRatio  float64 `koanf:"sample_ratio"`
	OTLPEndpoint string  `koanf:"otlp_endpoint"`
}

// Validate checks the sample ratio bounds and, when export is enabled, that
// the OTLP/HTTP endpoint is a bare http(s) origin without credentials.
func (c TracingConfig) Validate() error {
	if math.IsNaN(c.SampleRatio) || c.SampleRatio < 0 || c.SampleRatio > 1 {
		return errors.New("tracing sample_ratio must be within [0, 1]")
	}

	if !c.Enabled {
		return nil
	}

	if c.OTLPEndpoint == "" {
		return errors.New("tracing otlp_endpoint is required when tracing is enabled")
	}

	// The parse error is not wrapped: it would echo the raw value, which may
	// hold credentials.
	endpoint, err := url.Parse(c.OTLPEndpoint)
	if err != nil {
		return errors.New("tracing otlp_endpoint is not a valid URL")
	}

	switch {
	case endpoint.Scheme != "http" && endpoint.Scheme != "https":
		return errors.New("tracing otlp_endpoint scheme must be http or https")
	case endpoint.User != nil:
		return errors.New("tracing otlp_endpoint must not contain credentials")
	case endpoint.Hostname() == "":
		return errors.New("tracing otlp_endpoint host is required")
	case endpoint.Path != "" && endpoint.Path != "/":
		return errors.New("tracing otlp_endpoint must be an origin without a path")
	case endpoint.RawQuery != "" || endpoint.Fragment != "":
		return errors.New("tracing otlp_endpoint must not have a query or fragment")
	}

	return nil
}

// LoggingConfig controls log level and format.
type LoggingConfig struct {
	Level              string        `koanf:"level"`
	Format             string        `koanf:"format"`
	SlowQueryThreshold time.Duration `koanf:"slow_query_threshold"`
}

// DefaultObservabilityConfig returns the default observability settings.
func DefaultObservabilityConfig() *ObservabilityConfig {
	return &ObservabilityConfig{
		ServiceName: "sentinel",
		Environment: "development",
		Logging: LoggingConfig{
			Level:              levelInfo,
			Format:             LogFormatJSON,
			SlowQueryThreshold: defaultSlowQuery,
		},
	}
}

// Validate checks the observability settings.
func (c *ObservabilityConfig) Validate() error {
	if c.ServiceName == "" {
		return errors.New("service_name is required")
	}

	switch c.Logging.Level {
	case "debug", levelInfo, "warn", "error":
	default:
		return fmt.Errorf("invalid logging level: %s (must be one of: debug, info, warn, error)", c.Logging.Level)
	}

	switch c.Logging.Format {
	case LogFormatJSON, LogFormatConsole:
	default:
		return fmt.Errorf("invalid logging format: %q (must be one of: json, console)", c.Logging.Format)
	}

	if c.Logging.SlowQueryThreshold < 0 {
		return errors.New("logging slow_query_threshold must be non-negative")
	}

	return c.Tracing.Validate()
}

// GetLogLevel returns the effective log level.
func (c *ObservabilityConfig) GetLogLevel() string {
	if c.Logging.Level != "" {
		return c.Logging.Level
	}

	if c.Environment == "production" {
		return levelInfo
	}

	return "debug"
}

// IsProduction reports whether the environment is production.
func (c *ObservabilityConfig) IsProduction() bool {
	return c.Environment == "production"
}
