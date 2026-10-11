// Package config loads and validates Sentinel service configuration from the
// process environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
)

const (
	sectionRedis       = "redis"
	sectionKafka       = "kafka"
	sectionClickHouse  = "clickhouse"
	sectionDatabase    = "database"
	levelInfo          = "info"
	envPrefix          = "SENTINEL_"
	envKeyPrimaryEnv   = "SENTINEL_PRIMARY.ENV"
	envKeyEnvFile      = "SENTINEL_ENV_FILE"
	defaultEnvFile     = "../.env"
	localEnvironment   = "local"
	testEnvironment    = "test"
	failModeClosed     = "closed"
	defaultSSLMode     = "verify-full"
	defaultDBPort      = 5432
	defaultMaxConns    = 25
	defaultConnSeconds = 300
	defaultAttrKeys    = 32
	defaultAttrBytes   = 4096
	defaultFutureSkew  = 5 * time.Minute
	defaultPastSkew    = 168 * time.Hour
	defaultSlowQuery   = 100 * time.Millisecond
	defaultCHDatabase  = "sentinel"
	defaultCHUser      = "default"
)

// Service names accepted by Load.
const (
	ServiceIngest      = "ingest"
	ServiceDetector    = "detector"
	ServiceEventWriter = "event-writer"
	ServiceAlertWriter = "alert-writer"
	ServiceQueryAPI    = "query-api"
)

// Config is the complete runtime configuration for a Sentinel binary.
type Config struct {
	Primary       Primary              `koanf:"primary"`
	Observability *ObservabilityConfig `koanf:"observability"`
	Server        ServerConfig         `koanf:"server"`
	Database      DatabaseConfig       `koanf:"database"`
	Redis         RedisConfig          `koanf:"redis"`
	Kafka         KafkaConfig          `koanf:"kafka"`
	ClickHouse    ClickHouseConfig     `koanf:"clickhouse"`
	RateLimit     RateLimitConfig      `koanf:"ratelimit"`
	Contracts     ContractsConfig      `koanf:"contracts"`
}

// Primary holds settings that every binary needs.
type Primary struct {
	Env string `koanf:"env" validate:"required"`
}

// ServerConfig holds optional HTTP server settings.
type ServerConfig struct {
	Port               string   `koanf:"port"`
	ReadTimeout        int      `koanf:"read_timeout"`
	WriteTimeout       int      `koanf:"write_timeout"`
	IdleTimeout        int      `koanf:"idle_timeout"`
	CORSAllowedOrigins []string `koanf:"cors_allowed_origins"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	Host            string `koanf:"host" validate:"required"`
	Port            int    `koanf:"port" validate:"required"`
	User            string `koanf:"user" validate:"required"`
	Password        string `koanf:"password"`
	Name            string `koanf:"name" validate:"required"`
	SSLMode         string `koanf:"ssl_mode" validate:"required,oneof=disable allow prefer require verify-ca verify-full"`
	MaxOpenConns    int    `koanf:"max_open_conns" validate:"required"`
	MaxIdleConns    int    `koanf:"max_idle_conns" validate:"required"`
	ConnMaxLifetime int    `koanf:"conn_max_lifetime" validate:"required"`
	ConnMaxIdleTime int    `koanf:"conn_max_idle_time" validate:"required"`
}

// RedisConfig holds the Redis address as host:port without a URL scheme.
type RedisConfig struct {
	Address string `koanf:"address" validate:"required,hostname_port"`
}

// KafkaConfig holds comma-separated host:port broker addresses.
type KafkaConfig struct {
	Brokers string `koanf:"brokers" validate:"required"`
}

// ClickHouseConfig holds ClickHouse connection settings. Addresses are
// host:port without a URL scheme.
type ClickHouseConfig struct {
	HTTPAddress   string `koanf:"http_address" validate:"required,hostname_port"`
	NativeAddress string `koanf:"native_address" validate:"omitempty,hostname_port"`
	User          string `koanf:"user" validate:"required"`
	Password      string `koanf:"password"`
	Database      string `koanf:"database" validate:"required"`
}

// RateLimitConfig controls rate limiter behaviour when Redis is unavailable.
type RateLimitConfig struct {
	FailMode string `koanf:"fail_mode" validate:"required,oneof=closed open"`
}

// ContractsConfig bounds event contract validation.
type ContractsConfig struct {
	MaxAttrKeys  int           `koanf:"max_attr_keys" validate:"min=1"`
	MaxAttrBytes int           `koanf:"max_attr_bytes" validate:"min=1"`
	FutureSkew   time.Duration `koanf:"future_skew" validate:"min=0"`
	PastSkew     time.Duration `koanf:"past_skew" validate:"min=0"`
}

// requiredSections lists, per binary, the optional sections that must validate.
func requiredSections() map[string][]string {
	return map[string][]string{
		ServiceIngest:      {sectionRedis, sectionKafka},
		ServiceDetector:    {sectionKafka},
		ServiceEventWriter: {sectionKafka, sectionClickHouse},
		ServiceAlertWriter: {sectionKafka, sectionDatabase, sectionClickHouse},
		ServiceQueryAPI:    {sectionDatabase, sectionClickHouse, sectionRedis},
	}
}

// EnvFilePath returns the dotenv file path: SENTINEL_ENV_FILE taken literally
// when set, otherwise ../.env (the repo-root file when run from backend/).
func EnvFilePath() string {
	if p := os.Getenv(envKeyEnvFile); p != "" {
		return p
	}

	return defaultEnvFile
}

// Load builds and validates the configuration for the named binary. It never
// exits the process; callers decide how to handle the returned error.
func Load(service string) (*Config, error) {
	sections, ok := requiredSections()[service]
	if !ok {
		return nil, fmt.Errorf("unknown service %q", service)
	}

	// Dotenv is a local-development convenience only.
	if os.Getenv(envKeyPrimaryEnv) == localEnvironment {
		if err := godotenv.Load(EnvFilePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("load env file: %w", err)
		}
	}

	k := koanf.New(".")
	setDefaults(k)

	err := k.Load(env.ProviderWithValue(envPrefix, ".", envKeyValue), nil)
	if err != nil {
		return nil, fmt.Errorf("load environment variables: %w", err)
	}

	cfg := &Config{}
	if err = k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.Observability.ServiceName = service
	cfg.Observability.Environment = cfg.Primary.Env

	if err = validateFor(cfg, sections); err != nil {
		return nil, err
	}

	return cfg, nil
}

// listKeys are config keys whose env value is a comma-separated list.
func listKeys() map[string]struct{} {
	return map[string]struct{}{
		"server.cors_allowed_origins": {},
	}
}

// envKeyValue maps SENTINEL_SECTION.KEY to section.key and splits list values
// on commas, trimming blanks, so one env var can carry several entries.
func envKeyValue(rawKey, value string) (string, any) {
	key := strings.ToLower(strings.TrimPrefix(rawKey, envPrefix))

	if _, ok := listKeys()[key]; ok {
		return key, splitList(value)
	}

	return key, value
}

// splitList splits a comma-separated value and drops blank entries.
func splitList(value string) []string {
	items := []string{}

	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}

	return items
}

func setDefaults(k *koanf.Koanf) {
	defaults := map[string]any{
		"observability.logging.level":                levelInfo,
		"observability.logging.format":               "json",
		"observability.logging.slow_query_threshold": defaultSlowQuery,
		"database.port":                              defaultDBPort,
		"database.ssl_mode":                          defaultSSLMode,
		"database.max_open_conns":                    defaultMaxConns,
		"database.max_idle_conns":                    defaultMaxConns,
		"database.conn_max_lifetime":                 defaultConnSeconds,
		"database.conn_max_idle_time":                defaultConnSeconds,
		"clickhouse.user":                            defaultCHUser,
		"clickhouse.database":                        defaultCHDatabase,
		"ratelimit.fail_mode":                        failModeClosed,
		"contracts.max_attr_keys":                    defaultAttrKeys,
		"contracts.max_attr_bytes":                   defaultAttrBytes,
		"contracts.future_skew":                      defaultFutureSkew,
		"contracts.past_skew":                        defaultPastSkew,
	}

	for key, val := range defaults {
		// Set only fails on invalid paths, which cannot happen for literals.
		_ = k.Set(key, val)
	}
}

func validateFor(cfg *Config, sections []string) error {
	v := validator.New()

	if err := v.Struct(cfg.Primary); err != nil {
		return fmt.Errorf("validate primary config: %w", err)
	}

	if err := v.Struct(cfg.RateLimit); err != nil {
		return fmt.Errorf("validate ratelimit config: %w", err)
	}

	if err := v.Struct(cfg.Contracts); err != nil {
		return fmt.Errorf("validate contracts config: %w", err)
	}

	if err := cfg.Observability.Validate(); err != nil {
		return fmt.Errorf("validate observability config: %w", err)
	}

	for _, section := range sections {
		var target any

		switch section {
		case sectionDatabase:
			target = cfg.Database
		case sectionRedis:
			target = cfg.Redis
		case sectionKafka:
			target = cfg.Kafka
		case sectionClickHouse:
			target = cfg.ClickHouse
		default:
			return fmt.Errorf("unknown config section %q", section)
		}

		if err := v.Struct(target); err != nil {
			return fmt.Errorf("validate %s config: %w", section, err)
		}

		if section == sectionDatabase {
			if err := validateSSLMode(cfg.Primary.Env, cfg.Database.SSLMode); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateSSLMode rejects Postgres sslmodes that allow plaintext or skip
// certificate verification outside local development and tests.
func validateSSLMode(environment, mode string) error {
	if environment == localEnvironment || environment == testEnvironment {
		return nil
	}

	switch mode {
	case "verify-ca", "verify-full":
		return nil
	default:
		return fmt.Errorf("database ssl_mode %q is not allowed in environment %q", mode, environment)
	}
}
