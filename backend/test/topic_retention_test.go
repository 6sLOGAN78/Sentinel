//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

func TestTopicRetentionOverridesReapply(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	_, broker, err := sentineltesting.StartKafkaFaultContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(broker.Close)

	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	envFile := filepath.Join(t.TempDir(), "fixture.env")
	require.NoError(t, os.WriteFile(envFile, []byte("SENTINEL_POSTGRES_PASSWORD=fixture-only\nSENTINEL_CLICKHOUSE_PASSWORD=fixture-only\n"), 0o600))
	composeEnv := make([]string, 0, len(os.Environ()))
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case "EVENTS_PARTITIONS", "ALERTS_PARTITIONS", "EVENTS_RETENTION_MS", "ALERTS_RETENTION_MS", "DLQ_RETENTION_MS", "SENTINEL_POSTGRES_PASSWORD", "SENTINEL_CLICKHOUSE_PASSWORD":
			continue
		}
		composeEnv = append(composeEnv, value)
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", filepath.Join(root, "deployments/compose/docker-compose.yml"), "--env-file", envFile, "config", "--format", "json")
	cmd.Env = composeEnv
	resolved, err := cmd.Output()
	require.NoError(t, err, "resolve synthetic Compose configuration")
	var cfg struct {
		Services map[string]struct {
			Command     []string          `json:"command"`
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(resolved, &cfg))
	init := cfg.Services["topic-init"]
	require.Len(t, init.Command, 1)
	// Compose config serializes shell dollars as $$; decode the same escaping
	// Compose removes when creating the container, then adapt fixture networking.
	program := strings.ReplaceAll(init.Command[0], "$$", "$")
	program = strings.ReplaceAll(program, "kafka:9092", "localhost:9092")

	runInit := func() {
		t.Helper()
		args := []string{"exec"}
		for _, name := range []string{"EVENTS_PARTITIONS", "ALERTS_PARTITIONS", "EVENTS_RETENTION_MS", "ALERTS_RETENTION_MS", "DLQ_RETENTION_MS"} {
			args = append(args, "-e", name+"="+init.Environment[name])
		}
		args = append(args, broker.GetContainerID(), "/bin/bash", "-c", program)
		out, runErr := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		require.NoError(t, runErr, "topic init: %s", out)
	}
	assertTopics := func() {
		t.Helper()
		for _, topic := range []struct{ name, partitions, retention string }{
			{"security.events.v1", init.Environment["EVENTS_PARTITIONS"], init.Environment["EVENTS_RETENTION_MS"]},
			{"security.alerts.v1", init.Environment["ALERTS_PARTITIONS"], init.Environment["ALERTS_RETENTION_MS"]},
			{"security.events.dlq.v1", "1", init.Environment["DLQ_RETENTION_MS"]},
		} {
			out, describeErr := exec.CommandContext(ctx, "docker", "exec", broker.GetContainerID(), "/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", "localhost:9092", "--describe", "--topic", topic.name).CombinedOutput()
			require.NoError(t, describeErr, "describe topic: %s", out)
			require.Regexp(t, regexp.MustCompile(`PartitionCount:\s*`+topic.partitions+`\b`), string(out), topic.name)
			require.Regexp(t, regexp.MustCompile(`retention\.ms=`+topic.retention+`(?:,|\s|$)`), string(out), topic.name+" must use current retention")
		}
	}
	runInit()
	assertTopics()
	// These policies affect only empty topics in the dedicated fixture broker.
	init.Environment["EVENTS_RETENTION_MS"] = "172800000"
	init.Environment["ALERTS_RETENTION_MS"] = "691200000"
	init.Environment["DLQ_RETENTION_MS"] = "777600000"
	runInit()
	assertTopics()
	runInit()
	assertTopics()
}
