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

// Wrong dispatch, cross-rule counting, premature crossing, mutable evidence or
// a source identity with a synthetic empty suffix must fail on real Kafka.
func TestDetectionPipelineVolume(t *testing.T) {
	topic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "detector-volume")
	alertsTopic := sentineltesting.RequireUniqueTopic(t, kafkaCfg, "volume-alerts")
	group := "detector-volume-" + uuid.NewString()
	rulesPath, err := filepath.Abs("../../config/rules.yaml")
	require.NoError(t, err)
	content, err := os.ReadFile(rulesPath)
	require.NoError(t, err)
	// Explicit private enabling also keeps this acceptance runnable at RED.
	content = bytes.ReplaceAll(content, []byte("enabled: false"), []byte("enabled: true"))
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
	start := time.Now().UTC().Truncate(2 * time.Minute)
	requests := canonicalWriterEvents(301, start.Add(time.Second))
	denies := canonicalWriterEvents(31, start.Add(time.Second))
	auth := canonicalWriterEvents(9, start.Add(time.Second))
	scan := canonicalWriterEvents(19, start.Add(time.Second))
	floodSource, denySource := "192.0.2.10", "2001:db8::30"
	authSource, scanSource, destination := "192.0.2.40", "192.0.2.50", "192.0.2.60"
	action, outcome, principal := "deny", "success", "volume-auth-control"
	for _, event := range requests {
		event.EventType, event.SourceIP, event.Action = "net.request", &floodSource, &action
	}
	for _, event := range denies {
		event.EventType, event.SourceIP, event.Action, event.Outcome = "policy.decision", &denySource, &action, &outcome
	}
	for _, event := range auth {
		event.EventType, event.SourceIP, event.Principal = "auth.failure", &authSource, &principal
	}
	for i, event := range scan {
		port := 1000 + i
		event.EventType, event.SourceIP = "net.connection", &scanSource
		event.DestinationIP, event.DestinationPort = &destination, &port
	}
	below := append([]*contracts.Event{}, requests[:299]...)
	below = append(below, denies[:29]...)
	below = append(below, auth...)
	below = append(below, scan...)
	// Replayed IDs cannot supply either missing threshold contributor.
	below = append(below, requests[0], denies[0])
	require.NoError(t, producer.PublishBatch(t.Context(), below, uuid.NewString()))
	waitVolumeProgress(t, admin, group, topic, int64(len(below)), process)
	quietCtx, cancelQuiet := context.WithTimeout(t.Context(), time.Second)
	before := observer.PollRecords(quietCtx, 10)
	cancelQuiet()
	require.Empty(t, before.Records(), "299/29 and isolated AUTH/SCAN controls must not hit")
	require.NoError(t, producer.PublishBatch(t.Context(), []*contracts.Event{requests[299], denies[29]}, uuid.NewString()))
	readCtx, cancelRead := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancelRead()
	var records []*kgo.Record
	for len(records) < 2 && readCtx.Err() == nil {
		fetches := observer.PollRecords(readCtx, 10)
		if readCtx.Err() == nil {
			require.NoError(t, fetches.Err())
		}
		records = append(records, fetches.Records()...)
	}
	require.Len(t, records, 2, "exact flood/deny alerts required; output:\n%s", process.output.String())
	seen := make(map[string]bool)
	for _, record := range records {
		alert, decodeErr := contracts.DecodeAlert(bytes.NewReader(record.Value))
		require.NoError(t, decodeErr)
		require.NoError(t, alert.Validate())
		require.False(t, seen[alert.RuleID], "no repeated rule hits")
		seen[alert.RuleID] = true
		source, count, window, severity, events := floodSource, 300, time.Minute, "high", requests
		summary := "Excessive network request volume"
		if alert.RuleID == "DENY_SPIKE" {
			source, count, window, severity, events = denySource, 30, 2*time.Minute, "medium", denies
			summary = "Burst of denied policy decisions"
		} else {
			require.Equal(t, "REQUEST_FLOOD", alert.RuleID, "AUTH/SCAN and unrelated predicates must not cross-hit")
		}
		id := contracts.DedupeKey("demo", alert.RuleID, []string{source}, start)
		require.Equal(t, id, alert.AlertID)
		require.Equal(t, "demo|"+id, string(record.Key))
		require.Equal(t, "demo", alert.TenantID)
		require.Equal(t, 1, alert.RuleVersion)
		require.Equal(t, severity, alert.Severity)
		require.Equal(t, summary, alert.Summary)
		require.Equal(t, "open", alert.Status)
		require.Equal(t, source, alert.EntityKey)
		require.Equal(t, count, alert.EventCount)
		require.Equal(t, contracts.NewTimestamp(start), alert.WindowStart)
		require.Equal(t, contracts.NewTimestamp(start.Add(window)), alert.WindowEnd)
		require.Equal(t, alert.WindowEnd, alert.CreatedAt)
		require.Nil(t, alert.DetectedAt)
		require.Equal(t, "demo|"+alert.RuleID+"|"+source+"|"+start.Format(time.RFC3339Nano), alert.DedupeKey)
		require.Len(t, alert.SampleEventIDs, 20)
		for i := range 20 {
			require.Equal(t, events[i].EventID, alert.SampleEventIDs[i])
		}
	}
	require.Equal(t, map[string]bool{"REQUEST_FLOOD": true, "DENY_SPIKE": true}, seen)
	require.NoError(t, producer.PublishBatch(t.Context(), []*contracts.Event{requests[300], denies[30]}, uuid.NewString()))
	waitVolumeProgress(t, admin, group, topic, int64(len(below)+4), process)
	quietCtx, cancelQuiet = context.WithTimeout(t.Context(), time.Second)
	after := observer.PollRecords(quietCtx, 10)
	cancelQuiet()
	require.Empty(t, after.Records(), "301/31 cannot create another alert or mutate frozen evidence")
	require.Zero(t, process.terminate(t), process.output.String())
}

func waitVolumeProgress(t *testing.T, admin *kadm.Client, group, topic string, want int64, process *proc) {
	t.Helper()
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		offsets, err := admin.FetchOffsets(ctx, group)
		if err != nil {
			return false
		}
		offset, ok := offsets.Lookup(topic, 0)
		var metadata struct {
			ProcessedNext int64 `json:"processed_next"`
		}
		return ok && offset.Err == nil && offset.At == 0 &&
			json.Unmarshal([]byte(offset.Metadata), &metadata) == nil && metadata.ProcessedNext == want
	}, 15*time.Second, 50*time.Millisecond, "missing volume state pin/progress: %s", process.output.String())
}
