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
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

// Missing SCAN dispatch must fail after the default detector binary starts,
// processes nineteen distinct ports and accepts the twentieth unique port.
func TestDetectionPipelineScan(t *testing.T) {
	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "detector-scan")
	alertsTopic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "scan-alerts")
	group := "detector-scan-" + uuid.NewString()
	rulesPath, err := filepath.Abs("../../config/rules.yaml")
	require.NoError(t, err)
	content, err := os.ReadFile(rulesPath)
	require.NoError(t, err)
	// A private copy explicitly enables SCAN even during the RED test task.
	content = bytes.Replace(content, []byte("id: PORT_SCAN\n    version: 1\n    enabled: false"),
		[]byte("id: PORT_SCAN\n    version: 1\n    enabled: true"), 1)
	privateRules := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(privateRules, content, 0o600))
	env := serviceEnv(t, "detector")
	for key, value := range map[string]string{
		"SENTINEL_KAFKA.EVENTS_TOPIC": topic, "SENTINEL_KAFKA.ALERTS_TOPIC": alertsTopic,
		"SENTINEL_KAFKA.DETECTOR_GROUP": group, "SENTINEL_DETECTOR.RULES_PATH": privateRules,
	} {
		env = append(without(env, key+"="), key+"="+value)
	}
	process := startProc(t, "detector", "", env)
	requireOutputEventually(t, process, "service started")
	observer, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafkaCfg.Brokers, ",")...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{alertsTopic: {0: kgo.NewOffset().AtStart()}}))
	require.NoError(t, err)
	t.Cleanup(observer.Close)
	admin := kadm.NewClient(observer)
	kafka := kafkaCfg
	kafka.EventsTopic = topic
	producer := newWriterProducer(t, kafka)
	start := time.Now().UTC().Truncate(time.Minute)
	events := canonicalWriterEvents(25, start.Add(time.Second))
	source, destination := "2001:db8::10", "2001:db8::20"
	for i, event := range events {
		port := i % 19
		if i >= 23 {
			port = i - 4
		}
		event.EventType, event.SourceIP, event.DestinationIP = "net.connection", &source, &destination
		event.DestinationPort = &port
	}
	require.NoError(t, producer.PublishBatch(t.Context(), events[:23], uuid.NewString()))
	// Prove source progress before asserting no hit for 23 IDs on 19 ports.
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		offsets, fetchErr := admin.FetchOffsets(ctx, group)
		if fetchErr != nil {
			return false
		}
		offset, ok := offsets.Lookup(topic, 0)
		var metadata struct {
			ProcessedNext int64 `json:"processed_next"`
		}
		return ok && offset.Err == nil && json.Unmarshal([]byte(offset.Metadata), &metadata) == nil &&
			metadata.ProcessedNext == 23
	}, 15*time.Second, 50*time.Millisecond, "SCAN fixture input was not processed: %s", process.output.String())
	quietCtx, cancelQuiet := context.WithTimeout(t.Context(), 2*time.Second)
	before := observer.PollRecords(quietCtx, 10)
	cancelQuiet()
	require.Empty(t, before.Records(), "23 unique IDs sharing only 19 ports must not alert")
	require.NoError(t, producer.PublishBatch(t.Context(), events[23:24], uuid.NewString()))
	readCtx, cancelRead := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancelRead()
	var records []*kgo.Record
	for len(records) == 0 && readCtx.Err() == nil {
		fetches := observer.PollRecords(readCtx, 10)
		if readCtx.Err() == nil {
			require.NoError(t, fetches.Err(), "Kafka SCAN fetch setup must succeed")
		}
		records = append(records, fetches.Records()...)
	}
	require.Len(t, records, 1, "missing twentieth-port SCAN alert; detector output:\n%s", process.output.String())
	alert, err := contracts.DecodeAlert(bytes.NewReader(records[0].Value))
	require.NoError(t, err)
	require.NoError(t, alert.Validate())
	id := contracts.DedupeKey("demo", "PORT_SCAN", []string{source, destination}, start)
	var samples []string
	for _, event := range events[:20] {
		samples = append(samples, event.EventID)
	}
	end := contracts.NewTimestamp(start.Add(time.Minute))
	require.Equal(t, "demo|"+id, string(records[0].Key))
	require.Equal(t, &contracts.Alert{
		SchemaVersion: 1, AlertID: id, TenantID: "demo", RuleID: "PORT_SCAN", RuleVersion: 1,
		Severity: "medium", Status: "open", CreatedAt: end, WindowStart: contracts.NewTimestamp(start), WindowEnd: end,
		EntityKey: source + "|" + destination, Summary: "Distinct destination ports on one host",
		EventCount: 24, SampleEventIDs: samples,
		DedupeKey: "demo|PORT_SCAN|" + source + "|" + destination + "|" + start.Format(time.RFC3339Nano),
	}, alert)
	// The fired scan window retains its source pin after publication is durable.
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		offsets, fetchErr := admin.FetchOffsets(ctx, group)
		if fetchErr != nil {
			return false
		}
		offset, ok := offsets.Lookup(topic, 0)
		var metadata struct {
			ProcessedNext int64 `json:"processed_next"`
		}
		return ok && offset.Err == nil && offset.At == 0 && json.Unmarshal([]byte(offset.Metadata), &metadata) == nil &&
			metadata.ProcessedNext == 24
	}, 10*time.Second, 50*time.Millisecond, "missing acknowledged fired SCAN pin")
	require.NoError(t, producer.PublishBatch(t.Context(), events[24:], uuid.NewString()))
	quietCtx, cancelQuiet = context.WithTimeout(t.Context(), 2*time.Second)
	after := observer.PollRecords(quietCtx, 10)
	cancelQuiet()
	require.Empty(t, after.Records(), "twenty-first port must not change the frozen first hit")
	require.Zero(t, process.terminate(t), process.output.String())
}
