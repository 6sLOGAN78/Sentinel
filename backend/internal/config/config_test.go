package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLoadServerBindAddress(t *testing.T) {
	for _, address := range []string{"", "127.0.0.1", "::1", "0.0.0.0", "::"} {
		t.Run(address, func(t *testing.T) {
			setCompleteEnv(t)
			t.Setenv("SENTINEL_SERVER.BIND_ADDRESS", address)
			cfg, err := config.Load(config.ServiceDetector)
			require.NoError(t, err)
			require.Equal(t, address, cfg.Server.BindAddress)
		})
	}
}

func TestLoadRejectsMalformedBindAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "[::1]", "http://127.0.0.1", "bad address", "999.1.1.1"} {
		t.Run(address, func(t *testing.T) {
			setCompleteEnv(t)
			t.Setenv("SENTINEL_SERVER.BIND_ADDRESS", address)
			_, err := config.Load(config.ServiceDetector)
			require.ErrorContains(t, err, "server")
		})
	}
}

// setCompleteEnv sets every variable any binary needs.
func setCompleteEnv(t *testing.T) {
	t.Helper()

	t.Setenv("SENTINEL_PRIMARY.ENV", "test")
	t.Setenv("SENTINEL_REDIS.ADDRESS", "127.0.0.1:6379")
	t.Setenv("SENTINEL_KAFKA.BROKERS", "127.0.0.1:9092")
	t.Setenv("SENTINEL_CLICKHOUSE.HTTP_ADDRESS", "127.0.0.1:18123")
	t.Setenv("SENTINEL_CLICKHOUSE.NATIVE_ADDRESS", "127.0.0.1:19000")
	t.Setenv("SENTINEL_DATABASE.HOST", "127.0.0.1")
	t.Setenv("SENTINEL_DATABASE.USER", "sentinel")
	t.Setenv("SENTINEL_DATABASE.NAME", "sentinel")
}

func TestLoadComplete(t *testing.T) {
	setCompleteEnv(t)

	cfg, err := config.Load("ingest")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:6379", cfg.Redis.Address)
	require.Equal(t, "127.0.0.1:9092", cfg.Kafka.Brokers)
	require.Equal(t, "ingest", cfg.Observability.ServiceName)
	require.Equal(t, "test", cfg.Observability.Environment)
	require.Equal(t, 5432, cfg.Database.Port)
}

func TestLoadNativeClickHouseRequiredOnlyForConsumers(t *testing.T) {
	setCompleteEnv(t)
	t.Setenv("SENTINEL_CLICKHOUSE.NATIVE_ADDRESS", "")

	for _, service := range []string{config.ServiceEventWriter, config.ServiceAlertWriter, config.ServiceQueryAPI} {
		_, err := config.Load(service)
		require.ErrorContains(t, err, "clickhouse", service)
	}

	for _, service := range []string{config.ServiceIngest, config.ServiceDetector} {
		_, err := config.Load(service)
		require.NoError(t, err, service)
	}
}

func TestLoadIngestDefaultsAndServicePorts(t *testing.T) {
	setCompleteEnv(t)

	ingestCfg, err := config.Load(config.ServiceIngest)
	require.NoError(t, err)
	require.Equal(t, "8080", ingestCfg.Server.Port)
	require.Equal(t, 30, ingestCfg.Server.ReadTimeout)
	require.Equal(t, 30, ingestCfg.Server.WriteTimeout)
	require.Equal(t, 30, ingestCfg.Server.IdleTimeout)
	require.Equal(t, 500, ingestCfg.Ingest.MaxBatchSize)
	require.Equal(t, int64(1<<20), ingestCfg.Ingest.MaxBodyBytes)
	require.Equal(t, 64, ingestCfg.Ingest.MaxInFlight)
	require.Equal(t, 1024, ingestCfg.Ingest.APIKeyCacheCapacity)
	require.Equal(t, 30*time.Second, ingestCfg.Ingest.APIKeyCacheTTL)
	require.Equal(t, "security.events.v1", ingestCfg.Kafka.EventsTopic)
	require.Equal(t, "sentinel-event-writer", ingestCfg.Kafka.EventWriterGroup)
	require.Equal(t, 10*time.Second, ingestCfg.Kafka.ProduceTimeout)
	require.Equal(t, 5, ingestCfg.Kafka.RecordRetries)
	require.Equal(t, 50_000, ingestCfg.Kafka.MaxBufferedRecords)
	require.Equal(t, 64<<20, ingestCfg.Kafka.MaxBufferedBytes)

	queryCfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	require.Equal(t, "8081", queryCfg.Server.Port)
}

func TestLoadEventWriterDefaults(t *testing.T) {
	setCompleteEnv(t)

	cfg, err := config.Load(config.ServiceEventWriter)
	require.NoError(t, err)
	require.Equal(t, "sentinel-event-writer", cfg.Kafka.EventWriterGroup)
	require.Equal(t, 5000, cfg.Writer.BatchSize)
	require.Equal(t, time.Second, cfg.Writer.FlushInterval)
	require.Equal(t, 5, cfg.Writer.MaxAttempts)
	require.Equal(t, 100*time.Millisecond, cfg.Writer.RetryMinBackoff)
	require.Equal(t, 5*time.Second, cfg.Writer.RetryMaxBackoff)
	require.Equal(t, 10*time.Second, cfg.Writer.OperationTimeout)
	require.Equal(t, 20*time.Second, cfg.Writer.ShutdownTimeout)
	require.Equal(t, int32(16<<20), cfg.Writer.FetchMaxBytes)
	require.Equal(t, int32(4<<20), cfg.Writer.FetchMaxPartitionBytes)
	require.Equal(t, 2, cfg.Writer.MaxConcurrentFetches)
	require.Equal(t, 64<<10, cfg.Writer.DLQMaxBytes)
}

func TestLoadEventWriterRejectsUnsafeBounds(t *testing.T) {
	cases := []struct {
		key   string
		value string
	}{
		{"SENTINEL_WRITER.BATCH_SIZE", "5001"},
		{"SENTINEL_WRITER.FLUSH_INTERVAL", "0s"},
		{"SENTINEL_WRITER.MAX_ATTEMPTS", "0"},
		{"SENTINEL_WRITER.MAX_ATTEMPTS", "11"},
		{"SENTINEL_WRITER.RETRY_MIN_BACKOFF", "0s"},
		{"SENTINEL_WRITER.RETRY_MAX_BACKOFF", "6s"},
		{"SENTINEL_WRITER.OPERATION_TIMEOUT", "0s"},
		{"SENTINEL_WRITER.SHUTDOWN_TIMEOUT", "31s"},
		{"SENTINEL_WRITER.FETCH_MAX_BYTES", "0"},
		{"SENTINEL_WRITER.FETCH_MAX_BYTES", "67108865"},
		{"SENTINEL_WRITER.FETCH_MAX_PARTITION_BYTES", "0"},
		{"SENTINEL_WRITER.FETCH_MAX_PARTITION_BYTES", "16777217"},
		{"SENTINEL_WRITER.MAX_CONCURRENT_FETCHES", "0"},
		{"SENTINEL_WRITER.MAX_CONCURRENT_FETCHES", "9"},
		{"SENTINEL_WRITER.DLQ_MAX_BYTES", "0"},
		{"SENTINEL_WRITER.DLQ_MAX_BYTES", "1048577"},
	}

	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setCompleteEnv(t)
			t.Setenv(tc.key, tc.value)

			_, err := config.Load(config.ServiceEventWriter)
			require.ErrorContains(t, err, "writer")
		})
	}
}

func TestLoadEventWriterRejectsCrossFieldBounds(t *testing.T) {
	t.Run("retry range", func(t *testing.T) {
		setCompleteEnv(t)
		t.Setenv("SENTINEL_WRITER.RETRY_MIN_BACKOFF", "2s")
		t.Setenv("SENTINEL_WRITER.RETRY_MAX_BACKOFF", "1s")
		_, err := config.Load(config.ServiceEventWriter)
		require.ErrorContains(t, err, "retry")
	})

	t.Run("operation fits shutdown", func(t *testing.T) {
		setCompleteEnv(t)
		t.Setenv("SENTINEL_WRITER.OPERATION_TIMEOUT", "21s")
		t.Setenv("SENTINEL_WRITER.SHUTDOWN_TIMEOUT", "20s")
		_, err := config.Load(config.ServiceEventWriter)
		require.ErrorContains(t, err, "operation timeout")
	})

	t.Run("partition fetch fits total", func(t *testing.T) {
		setCompleteEnv(t)
		t.Setenv("SENTINEL_WRITER.FETCH_MAX_PARTITION_BYTES", "16777217")
		t.Setenv("SENTINEL_WRITER.FETCH_MAX_BYTES", "16777216")
		_, err := config.Load(config.ServiceEventWriter)
		require.ErrorContains(t, err, "fetch")
	})

	t.Run("aggregate fetch memory is bounded", func(t *testing.T) {
		setCompleteEnv(t)
		t.Setenv("SENTINEL_WRITER.FETCH_MAX_BYTES", "67108864")
		t.Setenv("SENTINEL_WRITER.MAX_CONCURRENT_FETCHES", "3")
		_, err := config.Load(config.ServiceEventWriter)
		require.ErrorContains(t, err, "aggregate fetch")
	})
}

func TestLoadIngestRequiresDatabase(t *testing.T) {
	setCompleteEnv(t)
	t.Setenv("SENTINEL_DATABASE.HOST", "")

	_, err := config.Load(config.ServiceIngest)
	require.ErrorContains(t, err, "database")
}

func TestLoadIngestRejectsUnsafeBounds(t *testing.T) {
	setCompleteEnv(t)

	t.Setenv("SENTINEL_KAFKA.PRODUCE_TIMEOUT", "30s")
	_, err := config.Load(config.ServiceIngest)
	require.ErrorContains(t, err, "produce timeout")

	t.Setenv("SENTINEL_KAFKA.PRODUCE_TIMEOUT", "10s")
	t.Setenv("SENTINEL_KAFKA.MAX_BUFFERED_RECORDS", "499")
	_, err = config.Load(config.ServiceIngest)
	require.ErrorContains(t, err, "buffered records")
}

func TestLoadMissingRedisPerService(t *testing.T) {
	setCompleteEnv(t)
	t.Setenv("SENTINEL_REDIS.ADDRESS", "")

	_, err := config.Load("ingest")
	require.Error(t, err)
	require.Contains(t, err.Error(), "redis")

	_, err = config.Load("detector")
	require.NoError(t, err)
}

func TestLoadRequiredSectionsPerService(t *testing.T) {
	cases := []struct {
		service string
		unset   []string
		wantErr bool
	}{
		{"ingest", []string{"SENTINEL_KAFKA.BROKERS"}, true},
		{"detector", []string{"SENTINEL_REDIS.ADDRESS", "SENTINEL_CLICKHOUSE.HTTP_ADDRESS", "SENTINEL_DATABASE.HOST"}, false},
		{"detector", []string{"SENTINEL_KAFKA.BROKERS"}, true},
		{"event-writer", []string{"SENTINEL_REDIS.ADDRESS", "SENTINEL_DATABASE.HOST"}, false},
		{"event-writer", []string{"SENTINEL_CLICKHOUSE.HTTP_ADDRESS"}, true},
		{"alert-writer", []string{"SENTINEL_REDIS.ADDRESS"}, false},
		{"alert-writer", []string{"SENTINEL_DATABASE.HOST"}, true},
		{"query-api", []string{"SENTINEL_KAFKA.BROKERS"}, false},
		{"query-api", []string{"SENTINEL_REDIS.ADDRESS"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.service, func(t *testing.T) {
			setCompleteEnv(t)

			for _, k := range tc.unset {
				t.Setenv(k, "")
			}

			_, err := config.Load(tc.service)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadUnknownService(t *testing.T) {
	setCompleteEnv(t)

	_, err := config.Load("bogus")
	require.Error(t, err)
}

func TestLoadMissingPrimaryEnv(t *testing.T) {
	setCompleteEnv(t)
	t.Setenv("SENTINEL_PRIMARY.ENV", "")

	_, err := config.Load("detector")
	require.Error(t, err)
}

func TestLoadRejectsSchemeAddress(t *testing.T) {
	setCompleteEnv(t)
	t.Setenv("SENTINEL_CLICKHOUSE.HTTP_ADDRESS", "http://127.0.0.1:18123")

	_, err := config.Load("event-writer")
	require.Error(t, err)

	t.Setenv("SENTINEL_CLICKHOUSE.HTTP_ADDRESS", "127.0.0.1:18123")

	cfg, err := config.Load("event-writer")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:18123", cfg.ClickHouse.HTTPAddress)

	t.Setenv("SENTINEL_REDIS.ADDRESS", "redis://127.0.0.1:6379")

	_, err = config.Load("ingest")
	require.Error(t, err)
}

func TestLoadRateLimitFailMode(t *testing.T) {
	setCompleteEnv(t)

	cfg, err := config.Load("detector")
	require.NoError(t, err)
	require.Equal(t, "closed", cfg.RateLimit.FailMode)

	t.Setenv("SENTINEL_RATELIMIT.FAIL_MODE", "open")

	cfg, err = config.Load("detector")
	require.NoError(t, err)
	require.Equal(t, "open", cfg.RateLimit.FailMode)

	t.Setenv("SENTINEL_RATELIMIT.FAIL_MODE", "maybe")

	_, err = config.Load("detector")
	require.Error(t, err)
}

func TestLoadContractsDefaultsAndOverrides(t *testing.T) {
	setCompleteEnv(t)

	cfg, err := config.Load("detector")
	require.NoError(t, err)
	require.Equal(t, 32, cfg.Contracts.MaxAttrKeys)
	require.Equal(t, 4096, cfg.Contracts.MaxAttrBytes)
	require.Equal(t, 5*time.Minute, cfg.Contracts.FutureSkew)
	require.Equal(t, 168*time.Hour, cfg.Contracts.PastSkew)

	t.Setenv("SENTINEL_CONTRACTS.MAX_ATTR_KEYS", "8")
	t.Setenv("SENTINEL_CONTRACTS.FUTURE_SKEW", "1m")

	cfg, err = config.Load("detector")
	require.NoError(t, err)
	require.Equal(t, 8, cfg.Contracts.MaxAttrKeys)
	require.Equal(t, time.Minute, cfg.Contracts.FutureSkew)
}

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), "envfile")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))

	return p
}

func TestDotenvLoadedWhenLocal(t *testing.T) {
	t.Setenv("SENTINEL_PRIMARY.ENV", "local")
	t.Setenv("SENTINEL_ENV_FILE", writeEnvFile(t, "SENTINEL_KAFKA.BROKERS=from-dotenv:9092\n"))
	t.Setenv("SENTINEL_KAFKA.BROKERS", "")
	require.NoError(t, os.Unsetenv("SENTINEL_KAFKA.BROKERS"))

	cfg, err := config.Load("detector")
	require.NoError(t, err)
	require.Equal(t, "from-dotenv:9092", cfg.Kafka.Brokers)

	t.Cleanup(func() { _ = os.Unsetenv("SENTINEL_KAFKA.BROKERS") })
}

func TestDotenvNotLoadedOutsideLocal(t *testing.T) {
	t.Setenv("SENTINEL_PRIMARY.ENV", "production")
	t.Setenv("SENTINEL_ENV_FILE", writeEnvFile(t, "SENTINEL_KAFKA.BROKERS=from-dotenv:9092\n"))
	require.NoError(t, os.Unsetenv("SENTINEL_KAFKA.BROKERS"))

	_, err := config.Load("detector")
	require.Error(t, err)
}

func TestDotenvDefaultPathNotBackendEnv(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)

	backend := filepath.Join(filepath.Dir(thisFile), "..", "..")
	t.Chdir(backend)
	t.Setenv("SENTINEL_ENV_FILE", "")
	require.NoError(t, os.Unsetenv("SENTINEL_ENV_FILE"))

	got, err := filepath.Abs(config.EnvFilePath())
	require.NoError(t, err)

	wantBackend, err := filepath.Abs(filepath.Join(backend, ".env"))
	require.NoError(t, err)
	require.NotEqual(t, wantBackend, got)

	t.Setenv("SENTINEL_ENV_FILE", "rel/path.env")
	require.Equal(t, "rel/path.env", config.EnvFilePath())
}

func TestDotenvIgnoresCwdEnv(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("SENTINEL_KAFKA.BROKERS=from-cwd:9092\n"), 0o600))
	t.Chdir(dir)

	require.NoError(t, os.Unsetenv("SENTINEL_ENV_FILE"))
	require.NoError(t, os.Unsetenv("SENTINEL_KAFKA.BROKERS"))
	t.Setenv("SENTINEL_PRIMARY.ENV", "local")

	// default ../.env relative to a temp dir is absent: not an error, and
	// the cwd .env is never read.
	_, err := config.Load("detector")
	require.Error(t, err)
	require.Contains(t, err.Error(), "kafka")
}

func TestLoadDatabaseSSLMode(t *testing.T) {
	setCompleteEnv(t)

	cfg, err := config.Load("alert-writer")
	require.NoError(t, err)
	require.Equal(t, "verify-full", cfg.Database.SSLMode)

	t.Setenv("SENTINEL_DATABASE.SSL_MODE", "bogus")

	_, err = config.Load("alert-writer")
	require.Error(t, err)

	t.Setenv("SENTINEL_PRIMARY.ENV", "production")

	for _, mode := range []string{"disable", "allow", "prefer", "require"} {
		t.Setenv("SENTINEL_DATABASE.SSL_MODE", mode)

		_, err = config.Load("alert-writer")
		require.Error(t, err, mode)
		require.Contains(t, err.Error(), "ssl_mode")
	}

	t.Setenv("SENTINEL_DATABASE.SSL_MODE", "verify-ca")

	_, err = config.Load("alert-writer")
	require.NoError(t, err)

	t.Setenv("SENTINEL_PRIMARY.ENV", "local")
	t.Setenv("SENTINEL_ENV_FILE", filepath.Join(t.TempDir(), "absent.env"))
	t.Setenv("SENTINEL_DATABASE.SSL_MODE", "disable")

	_, err = config.Load("alert-writer")
	require.NoError(t, err)
}

func TestLoadCORSAllowedOriginsList(t *testing.T) {
	setCompleteEnv(t)
	t.Setenv("SENTINEL_SERVER.CORS_ALLOWED_ORIGINS", "https://a.example.test, https://b.example.test,,")

	cfg, err := config.Load("query-api")
	require.NoError(t, err)
	require.Equal(t, []string{"https://a.example.test", "https://b.example.test"}, cfg.Server.CORSAllowedOrigins)

	t.Setenv("SENTINEL_SERVER.CORS_ALLOWED_ORIGINS", "https://only.example.test")

	cfg, err = config.Load("query-api")
	require.NoError(t, err)
	require.Equal(t, []string{"https://only.example.test"}, cfg.Server.CORSAllowedOrigins)
}

func TestLoadKafkaBrokersMustBeHostPort(t *testing.T) {
	setCompleteEnv(t)

	for _, bad := range []string{",", " , ", "kafka", ":9092", "kafka:", "kafka:abc", "kafka:0", "kafka:70000", "a:1,,b"} {
		t.Setenv("SENTINEL_KAFKA.BROKERS", bad)

		_, err := config.Load("detector")
		require.Error(t, err, bad)
		require.Contains(t, err.Error(), "kafka", bad)
	}

	t.Setenv("SENTINEL_KAFKA.BROKERS", " kafka-1:9092, [2001:db8::1]:9092 ,")

	cfg, err := config.Load("detector")
	require.NoError(t, err)

	brokers, err := config.ParseBrokers(cfg.Kafka.Brokers)
	require.NoError(t, err)
	require.Equal(t, []string{"kafka-1:9092", "[2001:db8::1]:9092"}, brokers)
}

func TestLoadRedisSecurity(t *testing.T) {
	setCompleteEnv(t)

	// Plaintext without AUTH is accepted only for local development and tests.
	cfg, err := config.Load("ingest")
	require.NoError(t, err)
	require.False(t, cfg.Redis.TLS)

	t.Setenv("SENTINEL_REDIS.CA_CERT_FILE", "/etc/ssl/redis-ca.pem")

	_, err = config.Load("ingest")
	require.ErrorContains(t, err, "ca_cert_file requires tls")

	t.Setenv("SENTINEL_REDIS.CA_CERT_FILE", "")
	t.Setenv("SENTINEL_PRIMARY.ENV", "production")

	_, err = config.Load("ingest")
	require.ErrorContains(t, err, "validate redis config")

	t.Setenv("SENTINEL_REDIS.TLS", "true")

	_, err = config.Load("ingest")
	require.ErrorContains(t, err, "password")

	t.Setenv("SENTINEL_REDIS.PASSWORD", "change-me")
	t.Setenv("SENTINEL_REDIS.USERNAME", "ingest")

	cfg, err = config.Load("ingest")
	require.NoError(t, err)
	require.True(t, cfg.Redis.TLS)
	require.Equal(t, "ingest", cfg.Redis.Username)

	// Services without Redis are not affected.
	t.Setenv("SENTINEL_REDIS.TLS", "false")

	_, err = config.Load("detector")
	require.NoError(t, err)
}
