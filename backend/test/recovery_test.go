//go:build integration

package test_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/internal/config"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
	"github.com/6sLOGAN78/sentinel/migrations"
)

const writerBarrierEnv = "SENTINEL_TEST_WRITER_AFTER_INSERT_BARRIER"

func TestWriterCrashRestart(t *testing.T) {
	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "writer-crash")
	group := "writer-crash-" + uuid.NewString()
	kafka := writerKafkaConfig(topic, group)
	events := canonicalWriterEvents(4, time.Now().UTC().Add(-time.Minute))

	clickhouse, conn := newRecoveryDatabase(t)
	require.NoError(t, conn.Exec(t.Context(), "SYSTEM STOP MERGES "+clickhouse.Database+".events"))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = conn.Exec(ctx, "SYSTEM START MERGES "+clickhouse.Database+".events")
	})
	preInsertKafka := kafka
	preInsertKafka.EventWriterGroup = group + "-preinsert"
	preInsert := startRecoveryProc(
		t, filepath.Join(binDir, "event-writer"), recoveryWriterEnv(t, preInsertKafka, clickhouse),
	)
	requireOutputEventually(t, preInsert)
	require.NoError(t, preInsert.cmd.Process.Signal(syscall.SIGKILL))
	require.NotZero(t, preInsert.requireExit(t, failFastLimit))
	require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(), events, uuid.NewString()))

	barrier := filepath.Join(t.TempDir(), "after-insert")
	integrationBinary := buildIntegrationBinary(t, "event-writer")
	env := recoveryWriterEnv(t, kafka, clickhouse)
	env = append(env, writerBarrierEnv+"="+barrier)
	first := startRecoveryProc(t, integrationBinary, env)
	requireFileEventually(t, barrier+".reached")

	var physical uint64
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count() FROM events").Scan(&physical))
	require.Equal(t, uint64(len(events)), physical)
	_, committed := committedNextOffset(t, kafka, group, topic)
	require.False(t, committed, "source offset advanced before the crash barrier")
	require.NoError(t, first.cmd.Process.Signal(syscall.SIGKILL))
	require.NotZero(t, first.requireExit(t, failFastLimit))

	restart := startRecoveryProc(t, filepath.Join(binDir, "event-writer"), recoveryWriterEnv(t, kafka, clickhouse))
	waitRecoveryOffset(t, kafka, group, topic, int64(len(events)), restart)
	require.Zero(t, restart.terminate(t), restart.output.String())
	repeated := startRecoveryProc(t, filepath.Join(binDir, "event-writer"), recoveryWriterEnv(t, kafka, clickhouse))
	requireOutputEventually(t, repeated)
	require.Zero(t, repeated.terminate(t), repeated.output.String())

	store := storageclickhouse.NewEventStore(conn)
	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(len(events)), unique)
	for _, event := range events {
		stored, getErr := store.Get(t.Context(), "demo", event.EventID)
		require.NoError(t, getErr, event.EventID)
		require.Equal(t, event.EventID, stored.EventID)
	}
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count() FROM events").Scan(&physical))
	require.Greater(t, physical, uint64(len(events)), "replay did not create the expected physical duplicate effects")
}

func buildIntegrationBinary(t *testing.T, service string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), service)
	cmd := exec.CommandContext(t.Context(), "go", "build", "-tags=integration", "-o", path, "./cmd/"+service)
	cmd.Dir = ".."
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return path
}

func startRecoveryProc(t *testing.T, binary string, env []string) *proc {
	t.Helper()
	out := &syncBuffer{}
	cmd := exec.CommandContext(t.Context(), binary)
	cmd.Dir = t.TempDir()
	cmd.Env = env
	cmd.Stdout = out
	cmd.Stderr = out
	require.NoError(t, cmd.Start())
	p := &proc{cmd: cmd, done: make(chan error, 1), output: out}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})

	return p
}

func recoveryWriterEnv(t *testing.T, kafka config.KafkaConfig, clickhouse config.ClickHouseConfig) []string {
	t.Helper()
	env := serviceEnv(t, "event-writer")
	replacements := map[string]string{
		"SENTINEL_KAFKA.BROKERS":             kafka.Brokers,
		"SENTINEL_KAFKA.EVENTS_TOPIC":        kafka.EventsTopic,
		"SENTINEL_KAFKA.EVENT_WRITER_GROUP":  kafka.EventWriterGroup,
		"SENTINEL_CLICKHOUSE.HTTP_ADDRESS":   clickhouse.HTTPAddress,
		"SENTINEL_CLICKHOUSE.NATIVE_ADDRESS": clickhouse.NativeAddress,
		"SENTINEL_CLICKHOUSE.USER":           clickhouse.User,
		"SENTINEL_CLICKHOUSE.PASSWORD":       clickhouse.Password,
		"SENTINEL_CLICKHOUSE.DATABASE":       clickhouse.Database,
		"SENTINEL_WRITER.BATCH_SIZE":         "4",
		"SENTINEL_WRITER.FLUSH_INTERVAL":     "100ms",
	}
	for key, value := range replacements {
		env = without(env, key+"=")
		env = append(env, key+"="+value)
	}

	return env
}

func newRecoveryDatabase(t *testing.T) (config.ClickHouseConfig, storageclickhouse.Conn) {
	t.Helper()
	cfg := chCfg
	cfg.Database = "recovery_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := chmigrate.New(cfg.HTTPAddress, cfg.User, cfg.Password, cfg.Database, nil).
		Run(t.Context(), migrations.ClickHouse())
	require.NoError(t, err)
	conn, err := storageclickhouse.Open(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	return cfg, conn
}

func requireFileEventually(t *testing.T, path string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)

		return err == nil
	}, 20*time.Second, 25*time.Millisecond, "barrier %s was not reached", path)
}

func requireOutputEventually(t *testing.T, process *proc) {
	t.Helper()
	const text = "service started"
	require.Eventually(t, func() bool {
		return strings.Contains(process.output.String(), text)
	}, failFastLimit, 25*time.Millisecond, "process output never contained %q:\n%s", text, process.output.String())
}

func waitRecoveryOffset(
	t *testing.T,
	kafka config.KafkaConfig,
	group string,
	topic string,
	want int64,
	process *proc,
) {
	t.Helper()
	require.Eventually(t, func() bool {
		got, ok := committedNextOffset(t, kafka, group, topic)

		return ok && got == want
	}, 70*time.Second, 100*time.Millisecond, "writer did not recover offset; output:\n%s", process.output.String())
}
