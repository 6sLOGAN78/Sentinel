//go:build integration

package test_test

import (
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

// Omitting PG from startup or processing allows a broken dependency to advance
// source checkpoints and must fail these real default-binary assertions.
func TestDetectorPGDependency(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		env, _ := detectorPGEnv(t, pgCfg)
		p := startProc(t, "detector", "", env)
		url := "http://127.0.0.1:" + opsPort(t, env)
		requireStatusEventually(t, url+"/readyz", http.StatusOK)
		requireDependencyEventually(t, url+"/readyz", "postgres", "ok")
		require.Zero(t, p.terminate(t), p.output.String())
	})
	t.Run("unavailable startup", func(t *testing.T) {
		pg := pgCfg
		pg.Port = 1
		pg.Password = "private-pg-sentinel-marker"
		env, kafka := detectorPGEnv(t, pg)
		events := canonicalWriterEvents(1, time.Now().UTC().Add(-time.Minute))
		require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(), events, uuid.NewString()))
		p := startProc(t, "detector", "", env)
		require.NotZero(t, p.requireExit(t, 5*time.Second), "PG outage must fail startup")
		_, committed := committedNextOffset(t, kafka, kafka.DetectorGroup, kafka.EventsTopic)
		require.False(t, committed, "PG startup failure must not advance source metadata")
		require.Contains(t, p.output.String(), "postgres")
		require.NotContains(t, p.output.String(), pg.Password)
		require.NotContains(t, p.output.String(), "postgres://")
	})
	t.Run("outage before source processing", func(t *testing.T) {
		pg, handle, err := sentineltesting.StartPostgresFaultContainer(t.Context())
		require.NoError(t, err)
		t.Cleanup(handle.Close)
		migrateCtx, cancelMigrations := context.WithTimeout(t.Context(), readinessWait)
		defer cancelMigrations()
		require.NoError(t, database.Migrate(migrateCtx, pg, zerolog.Nop()))
		env, kafka := detectorPGEnv(t, pg)
		p := startProc(t, "detector", "", env)
		url := "http://127.0.0.1:" + opsPort(t, env)
		requireStatusEventually(t, url+"/readyz", http.StatusOK)
		admin := checkpointAdmin(t)
		before := emptyPGDependencyCheckpoint(t, admin, kafka)
		require.NoError(t, handle.Pause(t.Context()))
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			require.NoError(t, handle.Unpause(ctx))
		})
		requireDependencyEventually(t, url+"/readyz", "postgres", "unavailable")
		events := canonicalWriterEvents(1, time.Now().UTC().Add(-time.Minute))
		require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(), events, uuid.NewString()))
		require.NotZero(t, p.requireExit(t, 5*time.Second), p.output.String())
		after := brokerCheckpoint(t, admin, kafka)
		require.Equal(t, before.Offset, after.At, "PG outage must not advance source offset")
		require.Equal(t, before.Metadata, after.Metadata, "PG outage must preserve complete checkpoint metadata")
		ends, endErr := admin.ListEndOffsets(t.Context(), kafka.AlertsTopic)
		require.NoError(t, endErr)
		end, ok := ends.Lookup(kafka.AlertsTopic, 0)
		require.True(t, ok)
		require.NoError(t, end.Err)
		require.Zero(t, end.Offset, "PG outage must not publish an alert")
		require.Contains(t, p.output.String(), "postgres unavailable")
		require.NotContains(t, p.output.String(), pg.Password)
	})
}

func emptyPGDependencyCheckpoint(t *testing.T, admin *kadm.Client, kafka config.KafkaConfig) detection.Checkpoint {
	t.Helper()
	var before detection.Checkpoint
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		offsets, err := admin.FetchOffsets(ctx, kafka.DetectorGroup)
		if err != nil {
			return false
		}
		offset, ok := offsets.Lookup(kafka.EventsTopic, 0)
		if !ok || offset.Err != nil || offset.At != 0 {
			return false
		}
		before = detection.Checkpoint{Offset: offset.At, Metadata: offset.Metadata}
		metadata, decodeErr := detection.CheckpointMetadata(before)
		return decodeErr == nil && metadata.Format == 2 && metadata.ProcessedNext == 0 && metadata.Drained &&
			metadata.PrefixMaxEventTime == nil && metadata.SealedThrough == nil && metadata.DecisionFrontier.None
	}, readinessWait, 25*time.Millisecond, "actual empty format-2 checkpoint must precede PG fault")
	return before
}

func detectorPGEnv(t *testing.T, pg config.DatabaseConfig) ([]string, config.KafkaConfig) {
	t.Helper()
	kafka := kafkaCfg
	kafka.EventsTopic = sentineltesting.RequireUniqueTopic(t, kafka, "pg-events")
	kafka.AlertsTopic = sentineltesting.RequireUniqueTopic(t, kafka, "pg-alerts")
	kafka.DetectorGroup = "pg-detector-" + uuid.NewString()
	rules, err := filepath.Abs("../../config/rules.yaml")
	require.NoError(t, err)
	env := serviceEnv(t, "detector")
	for key, value := range map[string]string{
		"SENTINEL_DATABASE.HOST":               pg.Host,
		"SENTINEL_DATABASE.PORT":               strconv.Itoa(pg.Port),
		"SENTINEL_DATABASE.USER":               pg.User,
		"SENTINEL_DATABASE.PASSWORD":           pg.Password,
		"SENTINEL_DATABASE.NAME":               pg.Name,
		"SENTINEL_DATABASE.SSL_MODE":           "disable",
		"SENTINEL_DETECTOR.PG.ACQUIRE_TIMEOUT": "500ms",
		"SENTINEL_DETECTOR.RULES_PATH":         rules,
		"SENTINEL_KAFKA.EVENTS_TOPIC":          kafka.EventsTopic,
		"SENTINEL_KAFKA.ALERTS_TOPIC":          kafka.AlertsTopic,
		"SENTINEL_KAFKA.DETECTOR_GROUP":        kafka.DetectorGroup,
	} {
		env = append(without(env, key+"="), key+"="+value)
	}
	return env, kafka
}
