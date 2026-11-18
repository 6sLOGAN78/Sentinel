package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
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
	// PrometheusURL is the query API base URL used by query-api for the
	// consumer lag and System endpoints. Empty means not configured.
	PrometheusURL string `koanf:"prometheus_url"`
	// GrafanaURL is the optional dashboard link shown on the System page.
	// Empty means no link is offered.
	GrafanaURL string `koanf:"grafana_url"`
	// System lists the internal ops address and expected replica count of
	// every pipeline service for the System page readiness probes.
	System SystemConfig `koanf:"system"`
}

// maxExpectedReplicas bounds the declared replica count per service.
const maxExpectedReplicas = 64

// SystemConfig holds one readiness target per pipeline service.
type SystemConfig struct {
	Ingest      ServiceTarget `koanf:"ingest"`
	EventWriter ServiceTarget `koanf:"event_writer"`
	Detector    ServiceTarget `koanf:"detector"`
	AlertWriter ServiceTarget `koanf:"alert_writer"`
	QueryAPI    ServiceTarget `koanf:"query_api"`
}

// ServiceTarget is a service's internal ops host:port (resolved to every
// replica) and its declared replica count. An empty address means not
// configured; an expected count of 0 means not declared.
type ServiceTarget struct {
	Address  string `koanf:"address"`
	Expected int    `koanf:"expected"`
}

// Validate requires each configured address to be host:port and each
// expected count to be within [0, 64].
func (c SystemConfig) Validate() error {
	for _, entry := range []struct {
		name   string
		target ServiceTarget
	}{
		{"ingest", c.Ingest}, {"event_writer", c.EventWriter}, {"detector", c.Detector},
		{"alert_writer", c.AlertWriter}, {"query_api", c.QueryAPI},
	} {
		name, target := entry.name, entry.target
		if target.Expected < 0 || target.Expected > maxExpectedReplicas {
			return fmt.Errorf("system.%s.expected must be within [0, %d]", name, maxExpectedReplicas)
		}

		if target.Address == "" {
			continue
		}

		if !validHostPort(target.Address) {
			return fmt.Errorf("system.%s.address must be host:port", name)
		}
	}

	return nil
}

// validHostPort reports whether addr is a nonempty host and a port in 1..65535.
func validHostPort(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}

	n, err := strconv.ParseUint(port, 10, 16)

	return err == nil && n != 0
}

// validateHTTPURL accepts an empty value or an http(s) URL with a host and no
// credentials. The parse error is not wrapped because it would echo the raw
// value, which may hold credentials.
func validateHTTPURL(field, raw string) error {
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)

	switch {
	case err != nil:
		return fmt.Errorf("%s is not a valid URL", field)
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("%s scheme must be http or https", field)
	case u.User != nil:
		return fmt.Errorf("%s must not contain credentials", field)
	case u.Hostname() == "":
		return fmt.Errorf("%s host is required", field)
	}

	return nil
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

	if err := validateHTTPURL("prometheus_url", c.PrometheusURL); err != nil {
		return err
	}

	if err := validateHTTPURL("grafana_url", c.GrafanaURL); err != nil {
		return err
	}

	if err := c.System.Validate(); err != nil {
		return err
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
