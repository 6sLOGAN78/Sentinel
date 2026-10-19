//go:build integration

package test_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

// Removing the detector's real threshold publication must fail this test even
// when the binary starts and Kafka accepts all source events.
func TestDetectionPipelineAuth(t *testing.T) {
	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "detector-auth")
	alertsTopic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "detector-alerts")
	group := "detector-auth-" + uuid.NewString()
	rules, err := filepath.Abs("../../config/rules.yaml")
	require.NoError(t, err)
	content, err := os.ReadFile(rules)
	require.NoError(t, err)
	privateRules := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(privateRules, content, 0o600))
	env := serviceEnv(t, "detector")
	for key, value := range map[string]string{
		"SENTINEL_KAFKA.EVENTS_TOPIC":   topic,
		"SENTINEL_KAFKA.ALERTS_TOPIC":   alertsTopic,
		"SENTINEL_KAFKA.DETECTOR_GROUP": group,
		"SENTINEL_DETECTOR.RULES_PATH":  privateRules,
	} {
		env = append(without(env, key+"="), key+"="+value)
	}
	process := startProc(t, "detector", "", env)
	requireOutputEventually(t, process)

	observer, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafkaCfg.Brokers, ",")...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{alertsTopic: {0: kgo.NewOffset().AtStart()}}))
	require.NoError(t, err)
	t.Cleanup(observer.Close)
	kafka := kafkaCfg
	kafka.EventsTopic = topic
	producer := newWriterProducer(t, kafka)
	start := time.Now().UTC().Truncate(5 * time.Minute)
	events := canonicalWriterEvents(11, start.Add(time.Second))
	ip, principal := "192.0.2.10", "example-user"
	for _, event := range events {
		event.EventType, event.SourceIP, event.Principal = "auth.failure", &ip, &principal
	}
	require.NoError(t, producer.PublishBatch(t.Context(), events[:9], uuid.NewString()))
	quietCtx, cancelQuiet := context.WithTimeout(t.Context(), 2*time.Second)
	before := observer.PollRecords(quietCtx, 10)
	cancelQuiet()
	require.Empty(t, before.Records(), "nine events must not alert")
	require.NoError(t, producer.PublishBatch(t.Context(), events[9:10], uuid.NewString()))
	readCtx, cancelRead := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancelRead()
	var records []*kgo.Record
	for len(records) == 0 && readCtx.Err() == nil {
		fetches := observer.PollRecords(readCtx, 10)
		if readCtx.Err() == nil {
			require.NoError(t, fetches.Err(), "Kafka alert fetch setup must succeed")
		}
		records = append(records, fetches.Records()...)
	}
	require.Len(t, records, 1, "missing tenth-event AUTH alert; detector output:\n%s", process.output.String())
	alert, err := contracts.DecodeAlert(bytes.NewReader(records[0].Value))
	require.NoError(t, err)
	require.NoError(t, alert.Validate())
	id := contracts.DedupeKey("demo", "AUTH_BRUTE_FORCE", []string{ip, principal}, start)
	require.Equal(t, "demo|"+id, string(records[0].Key))
	require.Equal(t, id, alert.AlertID)
	require.Equal(t, "AUTH_BRUTE_FORCE", alert.RuleID)
	require.Equal(t, 1, alert.RuleVersion)
	require.Equal(t, "high", alert.Severity)
	require.Equal(t, "open", alert.Status)
	require.Equal(t, 10, alert.EventCount)
	require.Equal(t, ip+"|"+principal, alert.EntityKey)
	require.Equal(t, start, alert.WindowStart.Time)
	require.Equal(t, start.Add(5*time.Minute), alert.WindowEnd.Time)
	require.Equal(t, alert.WindowEnd, alert.CreatedAt)
	require.Nil(t, alert.DetectedAt)
	require.Len(t, alert.SampleEventIDs, 10)
	for i := range 10 {
		require.Equal(t, events[i].EventID, alert.SampleEventIDs[i])
	}
	// Independently inspect the real broker checkpoint: the live fired window
	// must retain its first source offset even after the output is durable.
	var metadata struct {
		Format        int                        `json:"format"`
		ProcessedNext int64                      `json:"processed_next"`
		Prefix        *int64                     `json:"prefix_max_event_time"`
		Sealed        *int64                     `json:"sealed_through"`
		Versions      map[string]int             `json:"rule_versions"`
		Effective     map[string]int64           `json:"rule_effective_from"`
		Frontier      detection.DecisionFrontier `json:"decision_frontier"`
		Drained       bool                       `json:"drained"`
	}
	admin := kadm.NewClient(observer)
	require.Eventually(t, func() bool {
		fetchCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		offsets, fetchErr := admin.FetchOffsets(fetchCtx, group)
		if fetchErr != nil {
			return false
		}
		offset, ok := offsets.Lookup(topic, 0)
		return ok && offset.Err == nil && offset.At == 0 && len(offset.Metadata) <= 1024 &&
			json.Unmarshal([]byte(offset.Metadata), &metadata) == nil && metadata.ProcessedNext == 10
	}, 10*time.Second, 50*time.Millisecond, "missing acknowledged live state pin")
	require.Equal(t, 2, metadata.Format)
	require.Nil(t, metadata.Prefix, "first source record has an empty pre-pin prefix")
	require.NotNil(t, metadata.Sealed)
	require.Len(t, metadata.Versions, 4)
	require.Len(t, metadata.Effective, 4)
	require.False(t, metadata.Frontier.None)
	require.Equal(t, int64(9), metadata.Frontier.Offset)
	require.Zero(t, metadata.Frontier.RuleOrdinal)
	require.Equal(t, id, metadata.Frontier.AlertID)
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	defer db.Close()
	var fingerprint string
	var allowed bool
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT immutable_fingerprint,allowed
		FROM detection_decisions WHERE alert_id=$1`, id).Scan(&fingerprint, &allowed))
	require.True(t, allowed)
	require.Equal(t, fingerprint, metadata.Frontier.Fingerprint)
	require.False(t, metadata.Drained)
	require.NoError(t, producer.PublishBatch(t.Context(), events[10:], uuid.NewString()))
	quietCtx, cancelQuiet = context.WithTimeout(t.Context(), 2*time.Second)
	after := observer.PollRecords(quietCtx, 10)
	cancelQuiet()
	require.Empty(t, after.Records(), "fired window must not alert again")
	require.Zero(t, process.terminate(t), process.output.String())
}
