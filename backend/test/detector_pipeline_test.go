//go:build integration

package test_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
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
	requireOutputEventually(t, process, "service started")

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
	for i := range 10 {
		require.Equal(t, events[i].EventID, alert.SampleEventIDs[i])
	}
	require.Len(t, alert.SampleEventIDs, 10)
	require.NoError(t, producer.PublishBatch(t.Context(), events[10:], uuid.NewString()))
	quietCtx, cancelQuiet = context.WithTimeout(t.Context(), 2*time.Second)
	after := observer.PollRecords(quietCtx, 10)
	cancelQuiet()
	require.Empty(t, after.Records(), "fired window must not alert again")
	require.Zero(t, process.terminate(t), process.output.String())
}
