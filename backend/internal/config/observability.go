package config

import (
	"errors"
	"fmt"
	"time"
)

// ObservabilityConfig holds logging settings for a binary.
type ObservabilityConfig struct {
	ServiceName string        `koanf:"service_name"`
	Environment string        `koanf:"environment"`
	Logging     LoggingConfig `koanf:"logging"`
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
			Level:              "info",
			Format:             "json",
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
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid logging level: %s (must be one of: debug, info, warn, error)", c.Logging.Level)
	}

	if c.Logging.SlowQueryThreshold < 0 {
		return errors.New("logging slow_query_threshold must be non-negative")
	}

	return nil
}

// GetLogLevel returns the effective log level.
func (c *ObservabilityConfig) GetLogLevel() string {
	if c.Logging.Level != "" {
		return c.Logging.Level
	}

	if c.Environment == "production" {
		return "info"
	}

	return "debug"
}

// IsProduction reports whether the environment is production.
func (c *ObservabilityConfig) IsProduction() bool {
	return c.Environment == "production"
}
