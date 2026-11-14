//go:build phase4

package testphase4_test

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

// TestHarnessEnvBootstrap proves the shared Phase 4 helper alone yields a valid
// query-api configuration on a clean runner. CI runs it under env -i, so no
// launcher-exported SENTINEL_* or PG* variable can make it pass. Its name must
// not start with TestPhase4: the closed eight-family guard counts those.
func TestHarnessEnvBootstrap(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	raw, err := os.ReadFile(filepath.Join(root, ".env.example"))
	require.NoError(t, err)
	envFile := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(envFile, raw, 0o600))
	want, err := godotenv.Read(envFile)
	require.NoError(t, err)
	material := make([]byte, 48)
	_, err = rand.Read(material)
	require.NoError(t, err)
	t.Setenv("SENTINEL_AUTH.JWT_SECRET", base64.RawURLEncoding.EncodeToString(material))

	// Negative control first, before dotenv values enter the process env: with
	// SENTINEL_PRIMARY.ENV unset and no helper, Load must fail on Primary.Env.
	t.Setenv("SENTINEL_PRIMARY.ENV", "")
	require.NoError(t, os.Unsetenv("SENTINEL_PRIMARY.ENV"))
	t.Setenv("SENTINEL_ENV_FILE", envFile)
	_, err = config.Load(config.ServiceQueryAPI)
	require.ErrorContains(t, err, "Primary.Env", "without the helper the clean-runner config must be invalid")

	cfg := phase4ConfigFrom(t, envFile)
	port, err := strconv.Atoi(want["SENTINEL_DATABASE.PORT"])
	require.NoError(t, err)
	require.Equal(t, want["SENTINEL_DATABASE.HOST"], cfg.Database.Host)
	require.Equal(t, port, cfg.Database.Port)
	require.Equal(t, want["SENTINEL_DATABASE.NAME"], cfg.Database.Name)
	require.Equal(t, want["SENTINEL_CLICKHOUSE.NATIVE_ADDRESS"], cfg.ClickHouse.NativeAddress)
	require.Equal(t, want["SENTINEL_KAFKA.BROKERS"], cfg.Kafka.Brokers)
	// startQueryAPI copies os.Environ into the child, so the dotenv values must
	// now be in the process env for the child to inherit the DB config.
	require.NotEmpty(t, os.Getenv("SENTINEL_DATABASE.HOST"), "child query-api would not inherit DB config")

	again, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)
	require.Equal(t, cfg.Database.Host, again.Database.Host)
}

// phase4ConfigFrom makes a Phase 4 family self-sufficient on a clean runner: it
// selects the local dotenv path for envFile and loads the query-api config, so
// godotenv puts the file values into the process env that startQueryAPI copies
// into each child. Callers set SENTINEL_AUTH.JWT_SECRET before calling it.
func phase4ConfigFrom(t *testing.T, envFile string) *config.Config {
	t.Helper()
	t.Setenv("SENTINEL_PRIMARY.ENV", "local")
	t.Setenv("SENTINEL_ENV_FILE", envFile)
	_, err := os.Stat(envFile)
	require.NoError(t, err, "run make dev-up first: %s missing", envFile)
	cfg, err := config.Load(config.ServiceQueryAPI)
	require.NoError(t, err)

	return cfg
}

// phase4Config loads the Phase 4 configuration from the repo-root .env.
func phase4Config(t *testing.T, root string) *config.Config {
	t.Helper()

	return phase4ConfigFrom(t, filepath.Join(root, ".env"))
}
