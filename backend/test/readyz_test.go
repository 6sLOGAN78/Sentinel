//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

const readinessWait = 15 * time.Second

func TestHealthz(t *testing.T) {
	for _, service := range services {
		t.Run(service, func(t *testing.T) {
			env := serviceEnv(t, service)
			p := startProc(t, service, "", env)
			opsURL := "http://127.0.0.1:" + opsPort(t, env)
			requireStatusEventually(t, opsURL+"/healthz", http.StatusOK)
			requireStatusEventually(t, opsURL+"/readyz", http.StatusOK)
			require.Equal(t, 0, p.terminate(t), p.output.String())
		})
	}
}

func TestReadyzDependencies(t *testing.T) {
	pg, pgHandle, err := sentineltesting.StartPostgresFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(pgHandle.Close)
	migrateCtx, cancelMigrations := context.WithTimeout(t.Context(), readinessWait)
	defer cancelMigrations()
	require.NoError(t, database.Migrate(migrateCtx, pg, zerolog.Nop()))
	redis, redisHandle, err := sentineltesting.StartRedisFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(redisHandle.Close)
	kafka, kafkaHandle, err := sentineltesting.StartKafkaFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(kafkaHandle.Close)
	clickhouse, clickhouseHandle, err := sentineltesting.StartClickHouseFaultContainer(t.Context())
	require.NoError(t, err)
	t.Cleanup(clickhouseHandle.Close)

	kafka.EventsTopic = sentineltesting.RequireUniqueTopic(t, kafka, "ready-events")
	kafka.DLQTopic = sentineltesting.RequireUniqueTopic(t, kafka, "ready-dlq")
	kafka.AlertsTopic = sentineltesting.RequireUniqueTopic(t, kafka, "ready-alerts")
	kafka.EventWriterGroup = "readyz-writer"

	fixtures := startReadinessProcesses(t, pg, redis, kafka, clickhouse)
	assertOutageRecovery(t, pgHandle, "postgres", fixtures, "ingest", "detector", "alert-writer", "query-api")
	assertOutageRecovery(t, redisHandle, "redis", fixtures, "ingest", "query-api")
	assertOutageRecovery(t, kafkaHandle, "kafka", fixtures, "ingest", "detector", "event-writer", "alert-writer")
	assertOutageRecovery(t, clickhouseHandle, "clickhouse", fixtures, "event-writer", "alert-writer", "query-api")
}

func TestReadyzDraining(t *testing.T) {
	for _, service := range services {
		t.Run(service, func(t *testing.T) {
			env := append(serviceEnv(t, service), "SENTINEL_OPS.DRAIN_HOLD=250ms")
			p := startProc(t, service, "", env)
			opsURL := "http://127.0.0.1:" + opsPort(t, env)
			requireStatusEventually(t, opsURL+"/readyz", http.StatusOK)
			require.NoError(t, p.cmd.Process.Signal(terminateSignal))
			requireStatusEventually(t, opsURL+"/readyz", http.StatusServiceUnavailable)
			requireStatusEventually(t, opsURL+"/healthz", http.StatusOK)
			require.Equal(t, 0, p.requireExit(t, shutdownLimit), p.output.String())
		})
	}
}

type readinessProcess struct {
	proc   *proc
	opsURL string
}

func startReadinessProcesses(
	t *testing.T,
	pg config.DatabaseConfig,
	redis config.RedisConfig,
	kafka config.KafkaConfig,
	clickhouse config.ClickHouseConfig,
) map[string]readinessProcess {
	t.Helper()
	fixtures := make(map[string]readinessProcess, len(services))
	for _, service := range services {
		env := serviceEnvWithDependencies(t, service, pg, redis, kafka, clickhouse)
		p := startProc(t, service, "", env)
		opsURL := "http://127.0.0.1:" + opsPort(t, env)
		requireStatusEventually(t, opsURL+"/readyz", http.StatusOK)
		fixtures[service] = readinessProcess{proc: p, opsURL: opsURL}
	}
	t.Cleanup(func() {
		for _, fixture := range fixtures {
			if fixture.proc.cmd.ProcessState == nil {
				_ = fixture.proc.cmd.Process.Signal(terminateSignal)
			}
		}
	})

	return fixtures
}

func assertOutageRecovery(
	t *testing.T,
	handle *sentineltesting.FaultContainer,
	name string,
	fixtures map[string]readinessProcess,
	services ...string,
) {
	t.Helper()
	defer func() {
		if t.Failed() {
			for _, service := range services {
				t.Logf("%s dependency %s recovery process output:\n%s",
					service, name, fixtures[service].proc.output.String())
			}
		}
	}()
	require.NoError(t, handle.Pause(t.Context()), name)
	paused := true
	t.Cleanup(func() {
		if paused {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = handle.Unpause(ctx)
		}
	})
	for _, service := range services {
		t.Logf("checking %s during %s outage", service, name)
		requireStatusEventually(t, fixtures[service].opsURL+"/healthz", http.StatusOK)
		requireDependencyEventually(t, fixtures[service].opsURL+"/readyz", name, "unavailable")
	}
	require.NoError(t, handle.Unpause(t.Context()), name)
	paused = false
	for _, service := range services {
		requireDependencyEventually(t, fixtures[service].opsURL+"/readyz", name, "ok")
	}
}

func requireStatusEventually(t *testing.T, url string, want int) {
	t.Helper()
	require.Eventually(t, func() bool {
		response, err := (&http.Client{Timeout: 3 * time.Second}).Get(url)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == want
	}, readinessWait, 50*time.Millisecond, "%s did not return %d", url, want)
}

func requireDependencyEventually(t *testing.T, url, dependency, want string) {
	t.Helper()
	var lastMu sync.Mutex
	var lastStates map[string]string
	var lastError error
	defer func() {
		if t.Failed() {
			lastMu.Lock()
			defer lastMu.Unlock()
			t.Logf("last dependency states: %v; request error: %v", lastStates, lastError)
		}
	}()
	require.Eventually(t, func() bool {
		response, err := (&http.Client{Timeout: 3 * time.Second}).Get(url)
		lastMu.Lock()
		lastError = err
		lastMu.Unlock()
		if err != nil {
			return false
		}
		defer response.Body.Close()
		var body struct {
			Dependencies map[string]string `json:"dependencies"`
		}
		if json.NewDecoder(response.Body).Decode(&body) != nil {
			return false
		}
		lastMu.Lock()
		lastStates = body.Dependencies
		lastMu.Unlock()
		return body.Dependencies[dependency] == want
	}, readinessWait, 50*time.Millisecond, "%s did not report %s=%s", url, dependency, want)
}

func serviceEnvWithDependencies(
	t *testing.T,
	service string,
	pg config.DatabaseConfig,
	redis config.RedisConfig,
	kafka config.KafkaConfig,
	clickhouse config.ClickHouseConfig,
) []string {
	t.Helper()
	env := serviceEnv(t, service)
	replacements := map[string]string{
		"SENTINEL_DATABASE.HOST":             pg.Host,
		"SENTINEL_DATABASE.PORT":             strconv.Itoa(pg.Port),
		"SENTINEL_DATABASE.USER":             pg.User,
		"SENTINEL_DATABASE.PASSWORD":         pg.Password,
		"SENTINEL_DATABASE.NAME":             pg.Name,
		"SENTINEL_DATABASE.SSL_MODE":         pg.SSLMode,
		"SENTINEL_REDIS.ADDRESS":             redis.Address,
		"SENTINEL_KAFKA.BROKERS":             kafka.Brokers,
		"SENTINEL_KAFKA.EVENTS_TOPIC":        kafka.EventsTopic,
		"SENTINEL_KAFKA.DLQ_TOPIC":           kafka.DLQTopic,
		"SENTINEL_KAFKA.ALERTS_TOPIC":        kafka.AlertsTopic,
		"SENTINEL_KAFKA.EVENT_WRITER_GROUP":  kafka.EventWriterGroup,
		"SENTINEL_CLICKHOUSE.HTTP_ADDRESS":   clickhouse.HTTPAddress,
		"SENTINEL_CLICKHOUSE.NATIVE_ADDRESS": clickhouse.NativeAddress,
		"SENTINEL_CLICKHOUSE.USER":           clickhouse.User,
		"SENTINEL_CLICKHOUSE.PASSWORD":       clickhouse.Password,
		"SENTINEL_CLICKHOUSE.DATABASE":       clickhouse.Database,
	}
	for key, value := range replacements {
		env = without(env, key+"=")
		env = append(env, key+"="+value)
	}

	return env
}

func opsPort(t *testing.T, env []string) string {
	t.Helper()
	const key = "SENTINEL_OPS.PORT"
	const prefix = key + "="
	for _, value := range env {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	t.Fatalf("missing %s", key)

	return ""
}
