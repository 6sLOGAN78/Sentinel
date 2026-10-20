//go:build integration

package test_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

// TestDetectionScenarios executes the exact private operator path against the
// real preserved local pipeline. Fixed historical fixture equality is guarded
// separately in the reducer and generator, never injected into a live group.
func TestDetectionScenarios(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 16*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	rawFixture, err := os.ReadFile(filepath.Join(root, "testdata/detection/scenarios.json"))
	require.NoError(t, err)
	var fixtures struct {
		Cases []scenarioCase `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(rawFixture, &fixtures))
	for _, fixture := range fixtures.Cases {
		if fixture.Name == "all" || fixture.Name == "boundary" || fixture.Name == "lateness" {
			t.Run("isolated-static-"+fixture.Name, func(t *testing.T) {
				if fixture.Name == "all" {
					defer seedStaticWorkflowCollision(t)()
				}
				runStaticScenario(ctx, t, root, fixture)
			})
		}
	}
	t.Run("preserved-live-edges", func(t *testing.T) {
		output := filepath.Join(t.TempDir(), "scenarios.json")
		runPreservedCommand(ctx, t, root, "python3", "-B", "scripts/seed_scenarios.py", "--edges", "--output", output)
		raw, readErr := os.ReadFile(output)
		require.NoError(t, readErr)
		var proof struct {
			Verified bool              `json:"verified"`
			Drained  bool              `json:"drained"`
			Expected []json.RawMessage `json:"expected_alerts"`
		}
		require.NoError(t, json.Unmarshal(raw, &proof))
		require.True(t, proof.Verified)
		require.True(t, proof.Drained)
		require.Len(t, proof.Expected, 10, "two crossings per rule, benign zero, boundary and watermark equality")
	})
}

type scenarioCase struct {
	Name     string             `json:"name"`
	Events   []*contracts.Event `json:"events"`
	Expected []struct {
		Payload contracts.Alert `json:"payload"`
	} `json:"expected_alerts"`
}

// A shared-PG workflow with a scenario source but a different identity must not
// contaminate its exact-set verifier or be deleted to make that verifier pass.
func seedStaticWorkflowCollision(t *testing.T) func() {
	t.Helper()
	f := newAlertReplayFixture(t)
	f.publish(t, f.alert)
	detections := storageclickhouse.NewAlertStore(f.ch)
	workflows := storagepostgres.NewAlertStore(f.db.Pool, f.settings.OperationTimeout)
	require.NoError(t, f.run(t, detections, workflows))
	before := f.workflow(t)
	return func() {
		require.JSONEq(t, before, f.workflow(t), "static acceptance must preserve the complete unrelated workflow")
	}
}

func scenarioEnvironment(t *testing.T, service string, settings map[string]string) []string {
	t.Helper()
	result := serviceEnv(t, service)
	for key, value := range settings {
		result = append(without(result, key+"="), key+"="+value)
	}
	return result
}

func newStaticScenarioPG(ctx context.Context, t *testing.T) config.DatabaseConfig {
	t.Helper()
	admin, err := pgx.Connect(ctx, database.ConnString(pgCfg))
	require.NoError(t, err)
	pg := pgCfg
	pg.Name = "static_scenario_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{pg.Name}.Sanitize()
	_, err = admin.Exec(ctx, "CREATE DATABASE "+identifier)
	require.NoError(t, err)
	// Registered before service cleanups: LIFO stops/joins every owned process
	// before dropping only this fixture's unique database, never shared rows.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		defer admin.Close(cleanup)
		_, dropErr := admin.Exec(cleanup, "DROP DATABASE "+identifier)
		require.NoError(t, dropErr)
	})
	require.NoError(t, database.Migrate(ctx, pg, zerolog.Nop()))
	return pg
}

func runStaticScenario(ctx context.Context, t *testing.T, root string, fixture scenarioCase) {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafkaCfg.Brokers, ",")...))
	require.NoError(t, err)
	defer client.Close()
	admin := kadm.NewClient(client)
	topic, alerts := "scenario-"+uuid.NewString(), "scenario-alerts-"+uuid.NewString()
	group := "scenario-detector-" + uuid.NewString()
	created, err := admin.CreateTopics(ctx, 6, 1, nil, topic)
	require.NoError(t, err)
	require.NoError(t, created[topic].Err)
	created, err = admin.CreateTopics(ctx, 3, 1, nil, alerts)
	require.NoError(t, err)
	require.NoError(t, created[alerts].Err)
	rules := filepath.Join(t.TempDir(), "rules.yaml")
	rulesBytes, err := os.ReadFile(filepath.Join(root, "config/rules.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(rules, rulesBytes, 0o600))
	ch, _ := newRecoveryDatabase(t)
	pg := newStaticScenarioPG(ctx, t)
	settings := map[string]string{
		"SENTINEL_KAFKA.EVENTS_TOPIC": topic, "SENTINEL_KAFKA.ALERTS_TOPIC": alerts,
		"SENTINEL_KAFKA.DETECTOR_GROUP":     group,
		"SENTINEL_KAFKA.EVENT_WRITER_GROUP": "scenario-events-" + uuid.NewString(),
		"SENTINEL_KAFKA.ALERT_WRITER_GROUP": "scenario-writer-" + uuid.NewString(),
		"SENTINEL_DETECTOR.RULES_PATH":      rules, "SENTINEL_CLICKHOUSE.DATABASE": ch.Database,
		"SENTINEL_DATABASE.NAME":         pg.Name,
		"SENTINEL_WRITER.FLUSH_INTERVAL": "100ms",
	}
	for _, service := range []string{"detector", "event-writer", "alert-writer"} {
		process := startProc(t, service, "", scenarioEnvironment(t, service, settings))
		requireOutputEventually(t, process)
	}
	ingestEnv := scenarioEnvironment(t, "ingest", settings)
	// Historical fixed fixtures exercise default binaries in an isolated group.
	// Live operator acceptance below keeps the real production skew unchanged.
	ingestEnv = append(ingestEnv, "SENTINEL_CONTRACTS.PAST_SKEW=876000h")
	ingestProcess := startProc(t, "ingest", "", ingestEnv)
	requireOutputEventually(t, ingestProcess)
	var port string
	for _, value := range ingestEnv {
		if strings.HasPrefix(value, "SENTINEL_SERVER.PORT=") {
			port = strings.TrimPrefix(value, "SENTINEL_SERVER.PORT=")
		}
	}
	db, err := database.New(ctx, pg, zerolog.Nop())
	require.NoError(t, err)
	defer db.Close()
	key, err := apikeys.NewStore(db.Pool).Create(ctx, "demo", []string{apikeys.ScopeWrite}, "scenario-"+uuid.NewString())
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "manifest.json")
	manifest := map[string]any{"schema_version": 1, "tenant": "demo", "event_ids": []string{},
		"run_start":          time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"kafka_start":        map[string]int64{},
		"preserve_event_ids": []string{}, "preserve_alert_bytes": []string{}, "prior_events": map[string]string{},
		"prior_ch": map[string]string{}, "prior_pg": map[string]string{}}
	sourceKeys := map[string]bool{}
	for _, event := range fixture.Events {
		sourceKeys[event.TenantID+"|"+*event.SourceIP] = true
	}
	keys := make([]string, 0, len(sourceKeys))
	for k := range sourceKeys {
		keys = append(keys, k)
	}
	manifest["source_keys"] = keys
	expected := make([]string, 0, len(fixture.Expected))
	for _, item := range fixture.Expected {
		raw, marshalErr := json.Marshal(item.Payload)
		require.NoError(t, marshalErr)
		expected = append(expected, string(raw))
	}
	manifest["expected_alert_bytes"] = expected
	manifest = staticVerifier(ctx, t, root, "capture", path, manifest, settings, ch, pg)
	ids := postStaticScenario(ctx, t, "http://127.0.0.1:"+port, key, fixture.Events)
	manifest["event_ids"] = ids
	proof := staticVerifier(ctx, t, root, "verify", path, manifest, settings, ch, pg)
	require.Equal(t, true, proof["verified"])
	require.Equal(t, true, proof["drained"])
}

func staticVerifier(ctx context.Context, t *testing.T, root, mode, path string, manifest map[string]any,
	settings map[string]string, ch config.ClickHouseConfig, pg config.DatabaseConfig) map[string]any {
	t.Helper()
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	cmd := exec.CommandContext(ctx, "go", "run", "./tools/check-alerts", mode, "--manifest", path)
	cmd.Dir = filepath.Join(root, "backend")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "PGHOST=" + pg.Host,
		"PGPORT=" + strconv.Itoa(pg.Port), "PGUSER=" + pg.User, "PGPASSWORD=" + pg.Password,
		"PGDATABASE=" + pg.Name, "PGSSLMODE=" + pg.SSLMode, "SENTINEL_KAFKA.BROKERS=" + kafkaCfg.Brokers,
		"SENTINEL_CLICKHOUSE.NATIVE_ADDRESS=" + ch.NativeAddress, "SENTINEL_CLICKHOUSE.USER=" + ch.User,
		"SENTINEL_CLICKHOUSE.PASSWORD=" + ch.Password, "SENTINEL_CLICKHOUSE.DATABASE=" + ch.Database}
	for _, name := range []string{"SENTINEL_KAFKA.EVENTS_TOPIC", "SENTINEL_KAFKA.ALERTS_TOPIC",
		"SENTINEL_KAFKA.DETECTOR_GROUP"} {
		cmd.Env = append(cmd.Env, name+"="+settings[name])
	}
	output, err := cmd.Output()
	stage := "unclassified_child_failure"
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		stage = fixedVerifierStage(exit.Stderr)
	}
	require.NoError(t, err, "private verifier %s failed at %s", mode, stage)
	var result map[string]any
	require.NoError(t, json.Unmarshal(output, &result))
	return result
}

// Child stderr is never printed: only these verifier-owned fixed stages can
// reach diagnostics. Go's trailing exit-status line is deliberately ignored.
func fixedVerifierStage(output []byte) string {
	for _, stage := range []string{"source_checkpoint", "kafka_range", "clickhouse_set", "workflow_payload",
		"exact_payload_set", "prior_read", "prior_content", "current_event_rows", "private_context"} {
		for line := range strings.SplitSeq(string(output), "\n") {
			if strings.TrimSpace(line) == "alert verification failed: "+stage {
				return stage
			}
		}
	}
	return "unclassified_child_failure"
}

func postStaticScenario(ctx context.Context, t *testing.T, url, key string, events []*contracts.Event) []string {
	t.Helper()
	ids := make([]string, 0, len(events))
	seen := map[string]bool{}
	client := &http.Client{Timeout: 10 * time.Second}
	for index := 0; index < len(events); index += 500 {
		batch := events[index:min(index+500, len(events))]
		body, err := json.Marshal(map[string]any{"events": batch})
		require.NoError(t, err)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/v1/events:batch", bytes.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Api-Key", key)
		response, err := client.Do(request)
		require.NoError(t, err)
		var ack struct {
			Accepted int      `json:"accepted_count"`
			IDs      []string `json:"event_ids"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&ack)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusAccepted, response.StatusCode)
		require.NoError(t, decodeErr)
		require.Equal(t, len(batch), ack.Accepted)
		for i, event := range batch {
			require.Equal(t, event.EventID, ack.IDs[i])
			if !seen[event.EventID] {
				seen[event.EventID] = true
				ids = append(ids, event.EventID)
			}
		}
	}
	return ids
}
