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

// setCompleteEnv sets every variable any binary needs.
func setCompleteEnv(t *testing.T) {
	t.Helper()

	t.Setenv("SENTINEL_PRIMARY.ENV", "test")
	t.Setenv("SENTINEL_REDIS.ADDRESS", "127.0.0.1:6379")
	t.Setenv("SENTINEL_KAFKA.BROKERS", "127.0.0.1:9092")
	t.Setenv("SENTINEL_CLICKHOUSE.HTTP_ADDRESS", "127.0.0.1:18123")
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
