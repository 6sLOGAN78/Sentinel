//go:build integration

package test_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

func checkpointAdmin(t *testing.T) *kadm.Client {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafkaCfg.Brokers, ",")...))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return kadm.NewClient(client)
}

func brokerCheckpoint(t *testing.T, admin *kadm.Client, kafka config.KafkaConfig) kadm.OffsetResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	offsets, err := admin.FetchOffsets(ctx, kafka.DetectorGroup)
	require.NoError(t, err)
	o, ok := offsets.Lookup(kafka.EventsTopic, 0)
	require.True(t, ok)
	require.NoError(t, o.Err)
	return o
}

func seedBrokerCheckpoint(t *testing.T, admin *kadm.Client, kafka config.KafkaConfig, cp detection.Checkpoint) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	response, err := admin.CommitOffsets(ctx, kafka.DetectorGroup,
		kadm.Offsets{kafka.EventsTopic: {0: {Topic: kafka.EventsTopic, Partition: 0,
			At: cp.Offset, LeaderEpoch: -1, Metadata: cp.Metadata}}})
	require.NoError(t, err)
	require.NoError(t, response.Error())
	stored := brokerCheckpoint(t, admin, kafka)
	require.Equal(t, cp.Offset, stored.At)
	require.Equal(t, cp.Metadata, stored.Metadata)
}

func validBrokerCheckpoint(t *testing.T) detection.Checkpoint {
	t.Helper()
	raw, err := os.ReadFile("../../config/rules.yaml")
	require.NoError(t, err)
	rules, err := detection.LoadRules(bytes.NewReader(raw))
	require.NoError(t, err)
	state := detection.NewPartitionState(rules)
	e := canonicalWriterEvents(1, time.Now().UTC().Truncate(5*time.Minute).Add(time.Second))[0]
	ip, principal := "192.0.2.7", "checkpoint-user"
	e.EventType, e.SourceIP, e.Principal = "auth.failure", &ip, &principal
	_, err = state.Apply(0, e)
	require.NoError(t, err)
	require.NoError(t, state.Acknowledge(0))
	cp, err := state.Checkpoint()
	require.NoError(t, err)
	return cp
}

func requireCheckpointFailure(t *testing.T, env []string, kafka config.KafkaConfig, admin *kadm.Client,
	cp detection.Checkpoint) {
	t.Helper()
	seedBrokerCheckpoint(t, admin, kafka, cp)
	p := startProc(t, "detector", "", env)
	require.NotZero(t, p.requireExit(t, 10*time.Second), "invalid restore cannot retry forever ready: %s", p.output.String())
	retained := brokerCheckpoint(t, admin, kafka)
	require.Equal(t, cp.Offset, retained.At, "restore failure cannot reset/seek/commit a new source position")
	require.Equal(t, cp.Metadata, retained.Metadata)
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://127.0.0.1:" + opsPort(t, env) + "/readyz")
	if err == nil {
		defer response.Body.Close()
		require.NotEqual(t, http.StatusOK, response.StatusCode)
	}
}

func TestDetectorCheckpoint(t *testing.T) {
	t.Run("real pre-pin metadata and stored-byte replay", func(t *testing.T) {
		f := newCooldownFixture(t, false)
		// The valid nonmatching marker establishes max BEFORE the active pin.
		marker := canonicalWriterEvents(1, f.start.Add(300*time.Second))
		marker[0].EventType, marker[0].SourceIP = "auth.success", &f.ip
		producer := newWriterProducer(t, f.kafka)
		require.NoError(t, producer.PublishBatch(t.Context(), marker, uuid.NewString()))
		f.offset++
		f.waitSource(t)
		f.send(t, 280)
		original := f.alerts(t)
		require.Len(t, original, 1)
		before := brokerCheckpoint(t, f.admin, f.kafka)
		require.Equal(t, int64(1), before.At)
		var m detection.Metadata
		require.NoError(t, json.Unmarshal([]byte(before.Metadata), &m))
		require.Equal(t, marker[0].OccurredAt.UnixMilli(), *m.PrefixMaxEventTime)
		require.Equal(t, marker[0].OccurredAt.Add(-detection.Lateness).UnixMilli(), *m.SealedThrough)
		require.Equal(t, int64(11), m.ProcessedNext)
		require.False(t, m.DecisionFrontier.None)
		require.LessOrEqual(t, len(before.Metadata), 1024)
		require.Zero(t, f.process.terminate(t), f.process.output.String())
		f.process = startProc(t, "detector", "", f.env)
		replayed := f.alerts(t)
		if len(replayed) == 0 {
			replayed = f.alerts(t)
		}
		require.Len(t, replayed, 1, "restart must finish real stored-byte replay: %s", f.process.output.String())
		require.Equal(t, original[0].Value, replayed[0].Value)
		f.waitSource(t)
		after := brokerCheckpoint(t, f.admin, f.kafka)
		require.Equal(t, before.At, after.At)
		require.Equal(t, before.Metadata, after.Metadata)
		require.Zero(t, f.process.terminate(t), f.process.output.String())
	})
	admin := checkpointAdmin(t)
	valid := validBrokerCheckpoint(t)
	for _, tc := range []struct{ name, metadata string }{
		{"missing", ""}, {"malformed", "{"}, {"unknown format", strings.Replace(valid.Metadata, `"format":2`, `"format":9`, 1)},
		{"oversized", strings.Repeat("x", 1025)},
		{"missing explicit prefix", strings.Replace(valid.Metadata, `"prefix_max_event_time":null,`, "", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, kafka := detectorPGEnv(t, pgCfg)
			require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(),
				canonicalWriterEvents(3, time.Now().UTC()), uuid.NewString()))
			requireCheckpointFailure(t, env, kafka, admin, detection.Checkpoint{Offset: 0, Metadata: tc.metadata})
		})
	}
}

func TestDetectorRetention(t *testing.T) {
	admin := checkpointAdmin(t)
	valid := validBrokerCheckpoint(t)
	t.Run("trimmed existing pin", func(t *testing.T) {
		env, kafka := detectorPGEnv(t, pgCfg) // unique test-owned topic; no shared topic deletion.
		require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(),
			canonicalWriterEvents(4, time.Now().UTC()), uuid.NewString()))
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		deleted, err := admin.DeleteRecords(ctx, kadm.Offsets{kafka.EventsTopic: {0: {
			Topic: kafka.EventsTopic, Partition: 0, At: 2}}})
		require.NoError(t, err)
		require.NoError(t, deleted.Error())
		starts, err := admin.ListStartOffsets(ctx, kafka.EventsTopic)
		require.NoError(t, err)
		start, ok := starts.Lookup(kafka.EventsTopic, 0)
		require.True(t, ok)
		require.NoError(t, start.Err)
		require.Equal(t, int64(2), start.Offset, "broker actually removed pinned history")
		requireCheckpointFailure(t, env, kafka, admin, valid)
		// A different NEW group explicitly starts at retained earliest; it must
		// neither fail restoration nor claim the deleted first two records.
		fresh := kafka
		fresh.DetectorGroup += "-new"
		env = append(without(env, "SENTINEL_KAFKA.DETECTOR_GROUP="), "SENTINEL_KAFKA.DETECTOR_GROUP="+fresh.DetectorGroup)
		p := startProc(t, "detector", "", env)
		require.Eventually(t, func() bool {
			offsets, fetchErr := admin.FetchOffsets(t.Context(), fresh.DetectorGroup)
			o, present := offsets.Lookup(fresh.EventsTopic, 0)
			return fetchErr == nil && present && o.Err == nil && o.At == 4
		}, 10*time.Second, 50*time.Millisecond, "new group did not use retained earliest: %s", p.output.String())
		require.Zero(t, p.terminate(t), p.output.String())
	})
	t.Run("checkpoint beyond truncated log end", func(t *testing.T) {
		env, kafka := detectorPGEnv(t, pgCfg)
		require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(),
			canonicalWriterEvents(2, time.Now().UTC()), uuid.NewString()))
		// Install a checkpoint from a longer history: the broker's current end
		// is independently verified below. No broker or shared log is reset.
		ends, err := admin.ListEndOffsets(t.Context(), kafka.EventsTopic)
		require.NoError(t, err)
		end, ok := ends.Lookup(kafka.EventsTopic, 0)
		require.True(t, ok)
		require.Equal(t, int64(2), end.Offset)
		valid.Offset = 10
		requireCheckpointFailure(t, env, kafka, admin, valid)
	})
}
