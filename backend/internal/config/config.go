// Package config loads and validates Sentinel service configuration from the
// process environment.
package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
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
	sectionIngest      = "ingest"
	sectionWriter      = "writer"
	sectionDetector    = "detector"
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
	defaultHTTPSeconds = 30
	defaultBatchSize   = 500
	defaultBodyBytes   = 1 << 20
	defaultInFlight    = 64
	defaultCacheSize   = 1024
	defaultCacheTTL    = 30 * time.Second
	defaultRedisDial   = 100 * time.Millisecond
	defaultRedisRead   = 100 * time.Millisecond
	defaultRedisWrite  = 100 * time.Millisecond
	defaultRedisTries  = 1
	defaultRateCap     = 10_000
	defaultRateRefill  = 2_000.0
	defaultRateTimeout = 250 * time.Millisecond
	defaultEventTopic  = "security.events.v1"
	defaultDLQTopic    = "security.events.dlq.v1"
	defaultWriterGroup = "sentinel-event-writer"
	defaultProduceWait = 10 * time.Second
	defaultRetries     = 5
	defaultRecordCap   = 50_000
	defaultByteCap     = 64 << 20
	defaultWriterBatch = 5000
	defaultWriterFlush = time.Second
	defaultWriterTries = 5
	defaultRetryStart  = 100 * time.Millisecond
	defaultRetryMax    = 5 * time.Second
	defaultWriterOp    = 10 * time.Second
	defaultWriterStop  = 20 * time.Second
	defaultFetchBytes  = 16 << 20
	defaultPartBytes   = 4 << 20
	defaultFetches     = 2
	defaultDLQBytes    = 64 << 10
	defaultOpsBind     = "127.0.0.1"
	defaultOpsCheck    = 2 * time.Second
	maxFetchBytes      = 64 << 20
	maxPartBytes       = 16 << 20
	maxFetches         = 8
	maxDLQBytes        = 1 << 20
	maxAggregateFetch  = 128 << 20
	detectorStateBytes = 64 << 20
	defaultPGAcquire   = 2 * time.Second
	defaultPGTx        = 5 * time.Second
	defaultPGStatement = 2 * time.Second
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
	Ops           OpsConfig            `koanf:"ops"`
	Database      DatabaseConfig       `koanf:"database"`
	Redis         RedisConfig          `koanf:"redis"`
	Kafka         KafkaConfig          `koanf:"kafka"`
	ClickHouse    ClickHouseConfig     `koanf:"clickhouse"`
	RateLimit     RateLimitConfig      `koanf:"ratelimit"`
	Contracts     ContractsConfig      `koanf:"contracts"`
	Ingest        IngestConfig         `koanf:"ingest"`
	Writer        WriterConfig         `koanf:"writer"`
	Detector      DetectorConfig       `koanf:"detector"`
}

// DetectorConfig loads immutable startup rules and bounds one owned batch.
type DetectorConfig struct {
	RulesPath             string           `koanf:"rules_path" validate:"required"`
	AllowConfigTransition bool             `koanf:"allow_config_transition"`
	Writer                WriterConfig     `koanf:"writer"`
	PG                    DetectorPGConfig `koanf:"pg"`
}

// ComputeDetectorResources validates the conservative accounted resource sum.
// It is not an RSS guarantee: driver/runtime/transient allocations are separate.
// Check against the ceiling before multiplication/addition to avoid overflow.
func ComputeDetectorResources(fetch, concurrency, producer, state int64) error {
	const ceiling = int64(256 << 20)
	if fetch <= 0 || concurrency <= 0 || producer <= 0 || state <= 0 ||
		producer > ceiling || state > ceiling-producer || fetch > (ceiling-producer-state)/concurrency {
		return errors.New("validate detector config: aggregate accounted resources exceed 256 MiB")
	}
	return nil
}

// DetectorPGConfig bounds acquisition, decision transactions and independent
// rollback cleanup. SQL lock and statement limits are transaction-local in
// the decision store; merely opening the pool does not activate cooldown.
type DetectorPGConfig struct {
	AcquireTimeout     time.Duration `koanf:"acquire_timeout" validate:"gte=1ms,lte=30s"`
	TransactionTimeout time.Duration `koanf:"transaction_timeout" validate:"gte=1ms,lte=30s"`
	LockTimeout        time.Duration `koanf:"lock_timeout" validate:"gte=1ms,lte=30s"`
	StatementTimeout   time.Duration `koanf:"statement_timeout" validate:"gte=1ms,lte=30s"`
	CleanupTimeout     time.Duration `koanf:"cleanup_timeout" validate:"gte=1ms,lte=30s"`
}

// ComputeDetectorPGBudgets caps PG work within the existing aggregate owned
// batch deadline, reserving independent cleanup time even after cancellation.
func ComputeDetectorPGBudgets(pg DetectorPGConfig, writer WriterConfig) (DetectorPGConfig, error) {
	const reserveFraction = 4
	for _, d := range []time.Duration{pg.AcquireTimeout, pg.TransactionTimeout, pg.LockTimeout,
		pg.StatementTimeout, pg.CleanupTimeout} {
		if d < time.Millisecond || d > 30*time.Second {
			return DetectorPGConfig{}, errors.New("validate detector postgres config: invalid deadline")
		}
	}
	if pg.LockTimeout > pg.StatementTimeout || pg.StatementTimeout > pg.TransactionTimeout {
		return DetectorPGConfig{}, errors.New("validate detector postgres config: invalid deadline order")
	}
	budget, err := ComputeWriterBudgets(writer)
	if err != nil {
		return DetectorPGConfig{}, err
	}
	pg.AcquireTimeout = min(pg.AcquireTimeout, budget.Processing/reserveFraction)
	pg.CleanupTimeout = min(pg.CleanupTimeout, budget.Processing/reserveFraction, writer.ShutdownTimeout)
	pg.TransactionTimeout = min(pg.TransactionTimeout, budget.Processing-pg.AcquireTimeout-pg.CleanupTimeout)
	pg.StatementTimeout = min(pg.StatementTimeout, pg.TransactionTimeout)
	pg.LockTimeout = min(pg.LockTimeout, pg.StatementTimeout)
	if pg.AcquireTimeout < time.Millisecond || pg.CleanupTimeout < time.Millisecond || pg.LockTimeout < time.Millisecond {
		return DetectorPGConfig{}, errors.New("validate detector postgres config: insufficient ownership budget")
	}
	return pg, nil
}

func validateDetectorPool(db DatabaseConfig) error {
	const maxDetectorConns = 100
	const maxLifetimeSeconds = int64(math.MaxInt64) / int64(time.Second)
	if db.Port < 1 || db.Port > 65535 || db.MaxOpenConns < 1 || db.MaxOpenConns > maxDetectorConns ||
		db.MaxIdleConns < 1 || db.MaxIdleConns > db.MaxOpenConns ||
		db.ConnMaxLifetime < 1 || int64(db.ConnMaxLifetime) > maxLifetimeSeconds ||
		db.ConnMaxIdleTime < 1 || int64(db.ConnMaxIdleTime) > maxLifetimeSeconds {
		return errors.New("validate detector postgres config: invalid pool bounds")
	}
	return nil
}

// OpsConfig controls the private liveness/readiness listener.
type OpsConfig struct {
	BindAddress  string        `koanf:"bind_address" validate:"required,ip"`
	Port         string        `koanf:"port" validate:"required"`
	CheckTimeout time.Duration `koanf:"check_timeout" validate:"gt=0,lte=10s"`
	// DrainHold is a bounded readiness propagation interval. Production uses
	// zero; lifecycle acceptance tests set a short value to observe draining.
	DrainHold time.Duration `koanf:"drain_hold" validate:"min=0,lte=5s"`
}

// Primary holds settings that every binary needs.
type Primary struct {
	Env string `koanf:"env" validate:"required"`
}

// ServerConfig holds optional HTTP server settings.
type ServerConfig struct {
	BindAddress        string   `koanf:"bind_address" validate:"omitempty,ip"`
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

// RedisConfig holds Redis connection settings. Address is host:port without
// a URL scheme. Outside local and test environments TLS and a password are
// required (see validateRedisSecurity).
type RedisConfig struct {
	Address      string        `koanf:"address" validate:"required,hostname_port"`
	Username     string        `koanf:"username"`
	Password     string        `koanf:"password"`
	TLS          bool          `koanf:"tls"`
	DialTimeout  time.Duration `koanf:"dial_timeout" validate:"gt=0,lte=1s"`
	ReadTimeout  time.Duration `koanf:"read_timeout" validate:"gt=0,lte=1s"`
	WriteTimeout time.Duration `koanf:"write_timeout" validate:"gt=0,lte=1s"`
	MaxRetries   int           `koanf:"max_retries" validate:"min=-1,max=2"`
	// CACertFile is an optional PEM bundle used instead of the system roots.
	CACertFile string `koanf:"ca_cert_file"`
}

// KafkaConfig holds comma-separated host:port broker addresses.
type KafkaConfig struct {
	Brokers            string        `koanf:"brokers" validate:"required"`
	EventsTopic        string        `koanf:"events_topic" validate:"required"`
	DLQTopic           string        `koanf:"dlq_topic" validate:"required"`
	EventWriterGroup   string        `koanf:"event_writer_group" validate:"required"`
	AlertsTopic        string        `koanf:"alerts_topic" validate:"required"`
	DetectorGroup      string        `koanf:"detector_group" validate:"required"`
	AlertWriterGroup   string        `koanf:"alert_writer_group" validate:"required"`
	ProduceTimeout     time.Duration `koanf:"produce_timeout" validate:"gt=0"`
	RecordRetries      int           `koanf:"record_retries" validate:"min=0"`
	MaxBufferedRecords int           `koanf:"max_buffered_records" validate:"min=1"`
	MaxBufferedBytes   int           `koanf:"max_buffered_bytes" validate:"min=1"`
}

// IngestConfig bounds public request work and API-key verification caching.
type IngestConfig struct {
	MaxBatchSize        int           `koanf:"max_batch_size" validate:"min=1,max=500"`
	MaxBodyBytes        int64         `koanf:"max_body_bytes" validate:"min=1,max=1048576"`
	MaxInFlight         int           `koanf:"max_in_flight" validate:"min=1"`
	APIKeyCacheCapacity int           `koanf:"api_key_cache_capacity" validate:"min=1"`
	APIKeyCacheTTL      time.Duration `koanf:"api_key_cache_ttl" validate:"gt=0,lte=30s"`
}

// WriterConfig bounds Kafka fetches, in-memory batches, ClickHouse retries,
// shutdown flushing, and the poison-record value retained by the later DLQ stage.
type WriterConfig struct {
	BatchSize              int           `koanf:"batch_size" validate:"min=1,max=5000"`
	FlushInterval          time.Duration `koanf:"flush_interval" validate:"gt=0"`
	MaxAttempts            int           `koanf:"max_attempts" validate:"min=1,max=10"`
	RetryMinBackoff        time.Duration `koanf:"retry_min_backoff" validate:"gt=0"`
	RetryMaxBackoff        time.Duration `koanf:"retry_max_backoff" validate:"gt=0,lte=5s"`
	OperationTimeout       time.Duration `koanf:"operation_timeout" validate:"gt=0"`
	ShutdownTimeout        time.Duration `koanf:"shutdown_timeout" validate:"gt=0,lte=30s"`
	FetchMaxBytes          int32         `koanf:"fetch_max_bytes" validate:"min=1"`
	FetchMaxPartitionBytes int32         `koanf:"fetch_max_partition_bytes" validate:"min=1"`
	MaxConcurrentFetches   int           `koanf:"max_concurrent_fetches" validate:"min=1"`
	DLQMaxBytes            int           `koanf:"dlq_max_bytes" validate:"min=1"`
}

// WriterBudgets bounds retry work, one owned batch, and group handoff. Poison
// work shares one Retry budget regardless of the number of source records.
type WriterBudgets struct {
	Retry      time.Duration
	Processing time.Duration
	Rebalance  time.Duration
}

// ComputeWriterBudgets validates timing and batch bounds for environment-loaded
// settings and directly constructed writers, without overflowing durations.
func ComputeWriterBudgets(writer WriterConfig) (WriterBudgets, error) {
	const schedulingMargin = 5 * time.Second
	const minimumRebalance = 15 * time.Second
	const maximumRebalance = 5 * time.Minute
	if writer.BatchSize < 1 || writer.BatchSize > defaultWriterBatch ||
		writer.MaxAttempts < 1 || writer.MaxAttempts > 10 || writer.FlushInterval <= 0 ||
		writer.OperationTimeout <= 0 || writer.ShutdownTimeout <= 0 || writer.ShutdownTimeout > 30*time.Second ||
		writer.OperationTimeout > writer.ShutdownTimeout || writer.RetryMinBackoff <= 0 ||
		writer.RetryMinBackoff > writer.RetryMaxBackoff || writer.RetryMaxBackoff > defaultRetryMax {
		return WriterBudgets{}, errors.New("validate writer config: invalid processing bounds")
	}
	// The validated attempt/operation/backoff caps make both products safe.
	retry, err := sumWriterDurations(time.Duration(writer.MaxAttempts)*writer.OperationTimeout,
		time.Duration(writer.MaxAttempts-1)*writer.RetryMaxBackoff)
	if err != nil {
		return WriterBudgets{}, err
	}
	processing, err := sumWriterDurations(retry, retry, writer.OperationTimeout)
	if err != nil {
		return WriterBudgets{}, err
	}
	rebalance, err := sumWriterDurations(writer.FlushInterval, processing, writer.ShutdownTimeout, schedulingMargin)
	if err != nil {
		return WriterBudgets{}, err
	}
	if rebalance > maximumRebalance {
		return WriterBudgets{}, errors.New("validate writer config: ownership budget exceeds five minutes")
	}
	return WriterBudgets{Retry: retry, Processing: processing, Rebalance: max(minimumRebalance, rebalance)}, nil
}

func sumWriterDurations(parts ...time.Duration) (time.Duration, error) {
	var total time.Duration
	for _, part := range parts {
		if part < 0 || part > time.Duration(math.MaxInt64)-total {
			return 0, errors.New("validate writer config: ownership budget overflows duration")
		}
		total += part
	}
	return total, nil
}

// ClickHouseConfig holds ClickHouse connection settings. Addresses are
// host:port without a URL scheme.
type ClickHouseConfig struct {
	HTTPAddress   string `koanf:"http_address" validate:"required,hostname_port"`
	NativeAddress string `koanf:"native_address" validate:"required,hostname_port"`
	User          string `koanf:"user" validate:"required"`
	Password      string `koanf:"password"`
	Database      string `koanf:"database" validate:"required"`
}

// RateLimitConfig controls rate limiter behaviour when Redis is unavailable.
type RateLimitConfig struct {
	Capacity   int           `koanf:"capacity" validate:"min=1"`
	RefillRate float64       `koanf:"refill_rate" validate:"gt=0"`
	Timeout    time.Duration `koanf:"timeout" validate:"gt=0,lte=1s"`
	FailMode   string        `koanf:"fail_mode" validate:"required,oneof=closed open"`
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
		ServiceIngest:      {sectionRedis, sectionKafka, sectionDatabase, sectionIngest},
		ServiceDetector:    {sectionKafka, sectionDatabase, sectionDetector},
		ServiceEventWriter: {sectionKafka, sectionClickHouse, sectionWriter},
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
	if cfg.Server.Port == "" {
		cfg.Server.Port = "8080"
		if service == ServiceQueryAPI {
			cfg.Server.Port = "8081"
		}
	}
	if cfg.Ops.Port == "" {
		cfg.Ops.Port = defaultOpsPort(service)
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
		"server.bind_address":                        "",
		"server.read_timeout":                        defaultHTTPSeconds,
		"server.write_timeout":                       defaultHTTPSeconds,
		"server.idle_timeout":                        defaultHTTPSeconds,
		"ops.bind_address":                           defaultOpsBind,
		"ops.check_timeout":                          defaultOpsCheck,
		"ops.drain_hold":                             time.Duration(0),
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
		"redis.dial_timeout":                         defaultRedisDial,
		"redis.read_timeout":                         defaultRedisRead,
		"redis.write_timeout":                        defaultRedisWrite,
		"redis.max_retries":                          defaultRedisTries,
		"ratelimit.capacity":                         defaultRateCap,
		"ratelimit.refill_rate":                      defaultRateRefill,
		"ratelimit.timeout":                          defaultRateTimeout,
		"ratelimit.fail_mode":                        failModeClosed,
		"contracts.max_attr_keys":                    defaultAttrKeys,
		"contracts.max_attr_bytes":                   defaultAttrBytes,
		"contracts.future_skew":                      defaultFutureSkew,
		"contracts.past_skew":                        defaultPastSkew,
		"kafka.events_topic":                         defaultEventTopic,
		"kafka.dlq_topic":                            defaultDLQTopic,
		"kafka.event_writer_group":                   defaultWriterGroup,
		"kafka.alerts_topic":                         "security.alerts.v1",
		"kafka.detector_group":                       "sentinel-detector",
		"kafka.alert_writer_group":                   "sentinel-alert-writer",
		"detector.rules_path":                        "../config/rules.yaml",
		"detector.pg.acquire_timeout":                defaultPGAcquire,
		"detector.pg.transaction_timeout":            defaultPGTx,
		"detector.pg.lock_timeout":                   time.Second,
		"detector.pg.statement_timeout":              defaultPGStatement,
		"detector.pg.cleanup_timeout":                time.Second,
		"kafka.produce_timeout":                      defaultProduceWait,
		"kafka.record_retries":                       defaultRetries,
		"kafka.max_buffered_records":                 defaultRecordCap,
		"kafka.max_buffered_bytes":                   defaultByteCap,
		"ingest.max_batch_size":                      defaultBatchSize,
		"ingest.max_body_bytes":                      defaultBodyBytes,
		"ingest.max_in_flight":                       defaultInFlight,
		"ingest.api_key_cache_capacity":              defaultCacheSize,
		"ingest.api_key_cache_ttl":                   defaultCacheTTL,
		"writer.batch_size":                          defaultWriterBatch,
		"writer.flush_interval":                      defaultWriterFlush,
		"writer.max_attempts":                        defaultWriterTries,
		"writer.retry_min_backoff":                   defaultRetryStart,
		"writer.retry_max_backoff":                   defaultRetryMax,
		"writer.operation_timeout":                   defaultWriterOp,
		"writer.shutdown_timeout":                    defaultWriterStop,
		"writer.fetch_max_bytes":                     defaultFetchBytes,
		"writer.fetch_max_partition_bytes":           defaultPartBytes,
		"writer.max_concurrent_fetches":              defaultFetches,
		"writer.dlq_max_bytes":                       defaultDLQBytes,
	}

	for key, val := range defaults {
		// Set only fails on invalid paths, which cannot happen for literals.
		_ = k.Set(key, val)
		if strings.HasPrefix(key, "writer.") {
			_ = k.Set("detector."+key, val)
		}
	}
}

func validateFor(cfg *Config, sections []string) error {
	v := validator.New()

	if err := v.Struct(cfg.Server); err != nil {
		return fmt.Errorf("validate server config: %w", err)
	}
	if err := v.Struct(cfg.Ops); err != nil {
		return fmt.Errorf("validate ops config: %w", err)
	}
	if err := validateListenerPorts(cfg.Server.Port, cfg.Ops.Port, cfg.Primary.Env == testEnvironment); err != nil {
		return err
	}

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
		case sectionIngest:
			target = cfg.Ingest
		case sectionWriter:
			target = cfg.Writer
		case sectionDetector:
			target = cfg.Detector
		default:
			return fmt.Errorf("unknown config section %q", section)
		}

		if err := v.Struct(target); err != nil {
			return fmt.Errorf("validate %s config: %w", section, err)
		}

		if err := validateSection(cfg, section); err != nil {
			return err
		}
	}

	return nil
}

func defaultOpsPort(service string) string {
	switch service {
	case ServiceIngest:
		return "9101"
	case ServiceDetector:
		return "9102"
	case ServiceEventWriter:
		return "9103"
	case ServiceAlertWriter:
		return "9104"
	case ServiceQueryAPI:
		return "9105"
	default:
		return ""
	}
}

func validateListenerPorts(publicPort, opsPort string, allowEphemeralPublic bool) error {
	public, err := parsePort(publicPort, allowEphemeralPublic)
	if err != nil {
		return fmt.Errorf("validate server config: %w", err)
	}
	ops, err := parsePort(opsPort, false)
	if err != nil {
		return fmt.Errorf("validate ops config: %w", err)
	}
	if public == ops {
		return errors.New("validate listener config: public and ops ports must differ")
	}

	return nil
}

func parsePort(raw string, allowZero bool) (uint64, error) {
	port, err := strconv.ParseUint(raw, 10, 16)
	if err != nil || (port == 0 && !allowZero) {
		return 0, fmt.Errorf("port %q must be between 1 and 65535", raw)
	}

	return port, nil
}

// validateSection applies the checks a struct tag cannot express.
func validateSection(cfg *Config, section string) error {
	switch section {
	case sectionDatabase:
		return validateSSLMode(cfg.Primary.Env, cfg.Database.SSLMode)
	case sectionKafka:
		_, err := ParseBrokers(cfg.Kafka.Brokers)

		return err
	case sectionIngest:
		writeTimeout := time.Duration(cfg.Server.WriteTimeout) * time.Second
		if cfg.Kafka.ProduceTimeout >= writeTimeout {
			return errors.New("validate ingest config: produce timeout must be below HTTP write timeout")
		}
		if cfg.Kafka.MaxBufferedRecords < cfg.Ingest.MaxBatchSize {
			return errors.New("validate ingest config: buffered records must cover the maximum batch size")
		}
		if cfg.RateLimit.Capacity < cfg.Ingest.MaxBatchSize {
			return errors.New("validate ingest config: rate-limit capacity must cover the maximum batch size")
		}

		return nil
	case sectionWriter:
		return validateWriterConfig(cfg.Writer)
	case sectionDetector:
		if strings.TrimSpace(cfg.Detector.RulesPath) == "" {
			return errors.New("validate detector config: rules path is required")
		}
		if err := validateWriterConfig(cfg.Detector.Writer); err != nil {
			return err
		}
		if err := validateDetectorPool(cfg.Database); err != nil {
			return err
		}
		if err := ComputeDetectorResources(int64(cfg.Detector.Writer.FetchMaxBytes),
			int64(cfg.Detector.Writer.MaxConcurrentFetches), int64(cfg.Kafka.MaxBufferedBytes), detectorStateBytes); err != nil {
			return err
		}
		_, err := ComputeDetectorPGBudgets(cfg.Detector.PG, cfg.Detector.Writer)
		return err
	case sectionRedis:
		return validateRedisSecurity(cfg.Primary.Env, cfg.Redis)
	default:
		return nil
	}
}

func validateWriterConfig(writer WriterConfig) error {
	checks := []struct {
		invalid bool
		message string
	}{
		{writer.FetchMaxBytes > maxFetchBytes, "fetch max bytes exceeds 64 MiB"},
		{writer.FetchMaxPartitionBytes > maxPartBytes, "partition fetch bytes exceeds 16 MiB"},
		{writer.MaxConcurrentFetches > maxFetches, "concurrent fetches exceeds 8"},
		{writer.DLQMaxBytes > maxDLQBytes, "DLQ record cap exceeds 1 MiB"},
		{writer.RetryMinBackoff > writer.RetryMaxBackoff, "retry minimum exceeds retry maximum"},
		{writer.OperationTimeout > writer.ShutdownTimeout, "operation timeout exceeds shutdown timeout"},
		{writer.FetchMaxPartitionBytes > writer.FetchMaxBytes, "partition fetch bytes exceed total fetch bytes"},
		{
			int64(writer.FetchMaxBytes)*int64(writer.MaxConcurrentFetches) > maxAggregateFetch,
			"aggregate fetch memory exceeds 128 MiB",
		},
	}
	for _, check := range checks {
		if check.invalid {
			return errors.New("validate writer config: " + check.message)
		}
	}

	_, err := ComputeWriterBudgets(writer)
	return err
}

// ParseBrokers splits a comma-separated broker list, trimming blanks, and
// requires at least one entry with every entry in host:port form.
func ParseBrokers(brokers string) ([]string, error) {
	list := splitList(brokers)
	if len(list) == 0 {
		return nil, errors.New("validate kafka config: brokers has no address")
	}

	for _, addr := range list {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || host == "" {
			return nil, fmt.Errorf("validate kafka config: broker %q is not host:port", addr)
		}

		if n, perr := strconv.ParseUint(port, 10, 16); perr != nil || n == 0 {
			return nil, fmt.Errorf("validate kafka config: broker %q has an invalid port", addr)
		}
	}

	return list, nil
}

// validateRedisSecurity requires TLS and a password for Redis outside local
// development and tests, mirroring validateSSLMode for Postgres: Redis backs
// rate limiting, a security control.
func validateRedisSecurity(environment string, cfg RedisConfig) error {
	if cfg.CACertFile != "" && !cfg.TLS {
		return errors.New("validate redis config: ca_cert_file requires tls=true")
	}

	if environment == localEnvironment || environment == testEnvironment {
		return nil
	}

	if !cfg.TLS || cfg.Password == "" {
		return fmt.Errorf("validate redis config: tls and password are required in environment %q", environment)
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
