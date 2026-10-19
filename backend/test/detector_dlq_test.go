//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/detector"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

func TestDetectorPoison(t *testing.T) {
	t.Run("default binary trusted bounded quarantine", testDetectorRealPoison)
	t.Run("real DLQ outage preserves source", testDetectorDLQOutage)
}

func testDetectorRealPoison(t *testing.T) {
	env, kafka := detectorPGEnv(t, pgCfg)
	kafka.DLQTopic = sentineltesting.RequireUniqueTopic(t, kafka, "detector-dlq")
	env = append(without(env, "SENTINEL_KAFKA.DLQ_TOPIC="), "SENTINEL_KAFKA.DLQ_TOPIC="+kafka.DLQTopic,
		"SENTINEL_DETECTOR.WRITER.DLQ_MAX_BYTES=64")
	observed := startObservedDLQ(t.Context(), t, kafka, kafka.DLQTopic)
	p := startProc(t, "detector", "", env)
	requireOutputEventually(t, p, "service started")
	now := time.Now().UTC().Truncate(time.Second)
	events := canonicalWriterEvents(3, now)
	ip := "192.0.2.81"
	for _, event := range events {
		event.EventType, event.SourceIP = "auth.success", &ip
		event.OccurredAt = contracts.NewTimestamp(now)
	}
	good := dlqEventRecord(t, kafka.EventsTopic, events[0])
	missing := *events[1]
	missing.ReceivedAt = nil
	badSchema := *events[1]
	badSchema.SchemaVersion = 9
	badKey := dlqEventRecord(t, kafka.EventsTopic, events[1])
	badKey.Key = []byte("other|192.0.2.81")
	late := *events[2]
	late.OccurredAt = contracts.NewTimestamp(now.Add(-31 * time.Second))
	records := []*kgo.Record{good,
		{Topic: kafka.EventsTopic, Value: []byte("{")},
		dlqEventRecord(t, kafka.EventsTopic, &missing), dlqEventRecord(t, kafka.EventsTopic, &badSchema), badKey,
		{Topic: kafka.EventsTopic, Value: []byte(strings.Repeat("private-context", 10000))},
		dlqEventRecord(t, kafka.EventsTopic, &late)}
	for _, record := range records {
		record.Headers = []kgo.RecordHeader{{Key: "DLQ.origin.topic", Value: []byte("forged")},
			{Key: "dlq.origin.offset", Value: []byte("999999")}}
	}
	produceDLQSources(t, kafka, records)
	admin := checkpointAdmin(t)
	require.Eventually(t, func() bool {
		cp := brokerCheckpoint(t, admin, kafka)
		var m detection.Metadata
		return cp.At == int64(len(records)) && json.Unmarshal([]byte(cp.Metadata), &m) == nil &&
			m.ProcessedNext == int64(len(records))
	}, 8*time.Second, 25*time.Millisecond, "poison was not durably checkpointed:\n%s", p.output.String())
	got, err := observed.snapshot()
	require.NoError(t, err)
	require.Len(t, got, 5, "late valid events are normal drops, never quarantine")
	for i, reason := range []string{"decode_error", "missing_received_at", "validation_error", "validation_error", "decode_error"} {
		r := got[poisonOrigin{0, int64(i + 1)}]
		require.NotNil(t, r)
		requireDLQOrigin(t, r, records[i+1], reason)
		if len(records[i+1].Value) > 64 {
			require.Empty(t, r.Value)
			require.Equal(t, "true", testHeaderValue(r.Headers, "dlq.value.redacted"))
		}
	}
	cp := brokerCheckpoint(t, admin, kafka)
	var m detection.Metadata
	require.NoError(t, json.Unmarshal([]byte(cp.Metadata), &m))
	require.Equal(t, now.UnixMilli(), *m.PrefixMaxEventTime)
	require.Equal(t, now.Add(-detection.Lateness).UnixMilli(), *m.SealedThrough)
	require.True(t, m.DecisionFrontier.None)
	require.Zero(t, p.terminate(t), p.output.String())
}

func dlqEventRecord(t *testing.T, topic string, event *contracts.Event) *kgo.Record {
	t.Helper()
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	key := event.TenantID + "|"
	if event.SourceIP != nil {
		key += *event.SourceIP
	}
	return &kgo.Record{Topic: topic, Key: []byte(key), Value: raw}
}

func produceDLQSources(t *testing.T, kafka config.KafkaConfig, records []*kgo.Record) {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...),
		kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, client.ProduceSync(ctx, records...).FirstErr())
}

func requireDLQOrigin(t *testing.T, dlq, source *kgo.Record, reason string) {
	t.Helper()
	require.Equal(t, source.Topic, testHeaderValue(dlq.Headers, "dlq.origin.topic"))
	require.Equal(t, strconv.Itoa(int(source.Partition)), testHeaderValue(dlq.Headers, "dlq.origin.partition"))
	require.Equal(t, strconv.FormatInt(source.Offset, 10), testHeaderValue(dlq.Headers, "dlq.origin.offset"))
	require.Equal(t, reason, testHeaderValue(dlq.Headers, "dlq.reason"))
	size := len(dlq.Value) + len(dlq.Key)
	for _, header := range dlq.Headers {
		size += len(header.Key) + len(header.Value)
	}
	require.LessOrEqual(t, size, 64+(8<<10))
}

func unavailableDLQ(t *testing.T, kafka config.KafkaConfig, settings config.WriterConfig) *kafkax.DLQPublisher {
	t.Helper()
	kafka.Brokers, kafka.ProduceTimeout, kafka.RecordRetries = "127.0.0.1:1", settings.OperationTimeout, 0
	publisher, err := kafkax.NewDLQPublisher(t.Context(), kafka, kafka.DLQTopic, 64)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = publisher.Close(ctx)
	})
	return publisher
}

func testDetectorDLQOutage(t *testing.T) {
	_, kafka := detectorPGEnv(t, pgCfg)
	kafka.DLQTopic = sentineltesting.RequireUniqueTopic(t, kafka, "detector-outage-dlq")
	settings := integrationWriterSettings()
	settings.MaxAttempts, settings.BatchSize = 1, 500
	settings.OperationTimeout, settings.ShutdownTimeout = 300*time.Millisecond, time.Second
	file, err := os.Open("../../config/rules.yaml")
	require.NoError(t, err)
	defer file.Close()
	rules, err := detection.LoadRules(file)
	require.NoError(t, err)
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	defer db.Close()
	consumer, err := detector.NewKafkaConsumer(t.Context(), kafka, settings, rules, func(error) {})
	require.NoError(t, err)
	defer closeDetectorConsumer(t, consumer)
	alerts, err := detector.NewAlertProducer(t.Context(), kafka)
	require.NoError(t, err)
	defer func() { require.NoError(t, alerts.Close(t.Context())) }()
	poison := &kgo.Record{Topic: kafka.EventsTopic, Value: []byte("{")}
	produceDLQSources(t, kafka, []*kgo.Record{poison})
	pg := config.DetectorPGConfig{AcquireTimeout: 100 * time.Millisecond, TransactionTimeout: 200 * time.Millisecond,
		LockTimeout: 100 * time.Millisecond, StatementTimeout: 100 * time.Millisecond, CleanupTimeout: 100 * time.Millisecond}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	w := detector.New(consumer, alerts, rules, settings, contracts.DefaultLimits(), db.Pool, pg).
		WithQuarantine(unavailableDLQ(t, kafka, settings))
	err = w.Run(ctx)
	require.ErrorContains(t, err, "quarantine", "real DLQ outage must be terminal, sanitized and replayable")
	cp := brokerCheckpoint(t, checkpointAdmin(t), kafka)
	var m detection.Metadata
	require.NoError(t, json.Unmarshal([]byte(cp.Metadata), &m))
	require.Zero(t, cp.At)
	require.Zero(t, m.ProcessedNext)
	require.Nil(t, m.PrefixMaxEventTime)
}

func closeDetectorConsumer(t *testing.T, consumer *detector.KafkaConsumer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, consumer.Close(ctx))
}
