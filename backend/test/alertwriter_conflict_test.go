//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	storagepostgres "github.com/6sLOGAN78/sentinel/internal/storage/postgres"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

type alertReplayFixture struct {
	chConfig config.ClickHouseConfig
	ch       storageclickhouse.Conn
	db       *database.Database
	kafka    config.KafkaConfig
	client   *kgo.Client
	admin    *kadm.Client
	alert    *contracts.Alert
	settings config.WriterConfig
	dlq      *kafkax.DLQPublisher
}

func newAlertReplayFixture(t *testing.T) *alertReplayFixture {
	t.Helper()
	chConfig, ch := newRecoveryDatabase(t)
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	kafka := kafkaCfg
	kafka.AlertsTopic = sentineltesting.RequireUniqueTopic(t, kafka, "alert-replay")
	kafka.AlertWriterGroup = "alert-replay-" + uuid.NewString()
	kafka.DLQTopic = sentineltesting.RequireUniqueTopic(t, kafka, "alert-replay-dlq")
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	start := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	principal := "analyst|δ|" + uuid.NewString()
	end := contracts.NewTimestamp(start.Add(5 * time.Minute))
	a := &contracts.Alert{SchemaVersion: 1, TenantID: "demo", RuleID: "AUTH_BRUTE_FORCE", RuleVersion: 1,
		Severity: "high", Status: "open", CreatedAt: end, WindowStart: contracts.NewTimestamp(start), WindowEnd: end,
		EntityKey: "192.0.2.10|" + principal, Summary: "10 authentication failures", EventCount: 10,
		SampleEventIDs: []string{uuid.NewString(), uuid.NewString()}, DedupeKey: "AUTH_BRUTE_FORCE|192.0.2.10|" + principal}
	a.AlertID = contracts.DedupeKey(a.TenantID, a.RuleID, []string{"192.0.2.10", principal}, start)
	require.NoError(t, a.Validate())
	settings := integrationWriterSettings()
	settings.BatchSize, settings.MaxAttempts = 1, 1
	settings.OperationTimeout, settings.ShutdownTimeout = time.Second, time.Second
	dlq := integrationDLQPublisher(t, kafka, kafka.DLQTopic, settings)
	return &alertReplayFixture{chConfig: chConfig, ch: ch, db: db, kafka: kafka, client: client,
		admin: kadm.NewClient(client), alert: a, settings: settings, dlq: dlq}
}

func (f *alertReplayFixture) publish(t *testing.T, a *contracts.Alert) *kgo.Record {
	t.Helper()
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	record := &kgo.Record{Topic: f.kafka.AlertsTopic, Key: []byte(a.TenantID + "|" + a.AlertID), Value: raw,
		Headers: []kgo.RecordHeader{{Key: "dlq.origin.topic", Value: []byte("forged")}}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, f.client.ProduceSync(ctx, record).FirstErr())
	return record
}

func (f *alertReplayFixture) committed(t *testing.T) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	offsets, err := f.admin.FetchOffsets(ctx, f.kafka.AlertWriterGroup)
	require.NoError(t, err)
	offset, ok := offsets.Lookup(f.kafka.AlertsTopic, 0)
	if !ok || offset.At < 0 {
		return 0
	}
	require.NoError(t, offset.Err)
	return offset.At
}

func (f *alertReplayFixture) waitOffset(t *testing.T, next int64) {
	t.Helper()
	require.Eventually(t, func() bool { return f.committed(t) == next }, 12*time.Second, 25*time.Millisecond,
		"source did not advance through its durable effect")
}

func (f *alertReplayFixture) binary(t *testing.T) *proc {
	t.Helper()
	env := serviceEnv(t, "alert-writer")
	for key, value := range map[string]string{
		"SENTINEL_KAFKA.ALERTS_TOPIC":       f.kafka.AlertsTopic,
		"SENTINEL_KAFKA.ALERT_WRITER_GROUP": f.kafka.AlertWriterGroup,
		"SENTINEL_KAFKA.DLQ_TOPIC":          f.kafka.DLQTopic,
		"SENTINEL_CLICKHOUSE.DATABASE":      f.chConfig.Database,
		"SENTINEL_WRITER.BATCH_SIZE":        "1",
		"SENTINEL_WRITER.FLUSH_INTERVAL":    "100ms",
	} {
		env = append(without(env, key+"="), key+"="+value)
	}
	p := startProc(t, "alert-writer", "", env)
	requireOutputEventually(t, p, "service started")
	return p
}

func (f *alertReplayFixture) workflow(t *testing.T) string {
	t.Helper()
	var row string
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		`SELECT row_to_json(w)::text FROM alert_workflows w WHERE alert_id=$1`, f.alert.AlertID).Scan(&row))
	return row
}

func (f *alertReplayFixture) rows(t *testing.T, workflows uint64) {
	t.Helper()
	var chRows, pgRows uint64
	require.NoError(t, f.ch.QueryRow(t.Context(), `SELECT count() FROM alerts WHERE alert_id={id:String}`,
		driverclickhouse.Named("id", f.alert.AlertID)).Scan(&chRows))
	require.Equal(t, uint64(1), chRows, "a conflict/replay must never insert another immutable representation")
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM alert_workflows WHERE alert_id=$1`,
		f.alert.AlertID).Scan(&pgRows))
	require.Equal(t, workflows, pgRows)
	if chRows > 0 {
		store := storageclickhouse.NewAlertStore(f.ch)
		unique, err := store.CountUnique(t.Context(), f.alert.TenantID, time.Time{})
		require.NoError(t, err)
		require.Equal(t, uint64(1), unique)
		stored, err := store.Get(t.Context(), f.alert.TenantID, f.alert.AlertID)
		require.NoError(t, err)
		want, err := alertwriter.ImmutableFingerprint(f.alert)
		require.NoError(t, err)
		got, err := alertwriter.ImmutableFingerprint(stored)
		require.NoError(t, err)
		require.Equal(t, want, got, "immediate detail must retain the first immutable winner")
	}
}

// Removing either preflight or quarantine-before-commit must fail against real
// stores and broker offsets. An invalid changed tenant may fail at hash validation.
func TestAlertWriterConflict(t *testing.T) {
	for _, tc := range []struct {
		name, mutation string
		orphan         bool
	}{
		{"PG winner changed payload", "payload", false},
		{"PG winner changed tenant", "tenant", false},
		{"PG authoritative tenant differs", "winner tenant", false},
		{"CH orphan changed payload", "payload", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireReplayConflict(t, tc.mutation, tc.orphan)
		})
	}
	t.Run("competing first writers", testCompetingAlertWriters)
}

func requireReplayConflict(t *testing.T, mutation string, orphan bool) {
	t.Helper()
	f := newAlertReplayFixture(t)
	if orphan {
		require.NoError(t, storageclickhouse.NewAlertStore(f.ch).InsertBatch(t.Context(), []*contracts.Alert{f.alert}))
	} else {
		f.publish(t, f.alert)
		require.NoError(t, f.run(t, storageclickhouse.NewAlertStore(f.ch),
			storagepostgres.NewAlertStore(f.db.Pool, f.settings.OperationTimeout)))
	}
	if mutation == "winner tenant" {
		_, err := f.db.Pool.Exec(t.Context(), `UPDATE alert_workflows SET tenant_id='other-tenant' WHERE alert_id=$1`,
			f.alert.AlertID)
		require.NoError(t, err)
	}
	before := ""
	if !orphan {
		before = f.workflow(t)
	}
	observed := startObservedDLQ(t.Context(), t, f.kafka, f.kafka.DLQTopic)
	p := f.binary(t)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("final writer output:\n%s", p.output.String())
		}
	})
	candidate := *f.alert
	switch mutation {
	case "tenant":
		candidate.TenantID = "other-tenant"
	case "payload":
		candidate.Summary = "conflicting immutable evidence"
	}
	source := f.publish(t, &candidate)
	require.Eventually(t, func() bool {
		records, err := observed.snapshot()
		return err == nil && records[poisonOrigin{source.Partition, source.Offset}] != nil
	}, 6*time.Second, 25*time.Millisecond,
		"conflicting content was not durably quarantined; output:\n%s", p.output.String())
	records, err := observed.snapshot()
	require.NoError(t, err)
	quarantined := records[poisonOrigin{source.Partition, source.Offset}]
	require.Equal(t, source.Value, quarantined.Value)
	require.Equal(t, source.Key, quarantined.Key)
	require.Equal(t, f.kafka.AlertsTopic, testHeaderValue(quarantined.Headers, "dlq.origin.topic"))
	require.Equal(t, "validation_error", testHeaderValue(quarantined.Headers, "dlq.reason"))
	f.waitOffset(t, source.Offset+1)
	if orphan {
		f.rows(t, 0)
	} else {
		f.rows(t, 1)
		require.JSONEq(t, before, f.workflow(t))
	}
	require.Zero(t, p.terminate(t), p.output.String())
}

func testCompetingAlertWriters(t *testing.T) {
	t.Helper()
	f := newAlertReplayFixture(t)
	other := *f
	other.kafka.AlertsTopic = sentineltesting.RequireUniqueTopic(t, f.kafka, "alert-competing")
	other.kafka.AlertWriterGroup = "alert-competing-" + uuid.NewString()
	candidate := *f.alert
	candidate.Summary = "competing immutable summary"
	other.alert = &candidate
	observed := startObservedDLQ(t.Context(), t, f.kafka, f.kafka.DLQTopic)
	first, second := f.binary(t), other.binary(t)
	rawA, err := json.Marshal(f.alert)
	require.NoError(t, err)
	rawB, err := json.Marshal(other.alert)
	require.NoError(t, err)
	key := []byte(f.alert.TenantID + "|" + f.alert.AlertID)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	require.NoError(t, f.client.ProduceSync(ctx,
		&kgo.Record{Topic: f.kafka.AlertsTopic, Key: key, Value: rawA},
		&kgo.Record{Topic: other.kafka.AlertsTopic, Key: key, Value: rawB}).FirstErr())
	f.waitOffset(t, 1)
	other.waitOffset(t, 1)
	var summary string
	require.NoError(t, f.db.Pool.QueryRow(ctx, `SELECT summary FROM alert_workflows WHERE alert_id=$1`,
		f.alert.AlertID).Scan(&summary))
	loserRaw, loserTopic := rawB, other.kafka.AlertsTopic
	if summary == candidate.Summary {
		f.alert = &candidate
		loserRaw, loserTopic = rawA, f.kafka.AlertsTopic
	} else {
		require.Equal(t, f.alert.Summary, summary)
	}
	f.rows(t, 1)
	require.Eventually(t, func() bool {
		records, snapshotErr := observed.snapshot()
		return snapshotErr == nil && len(records) == 1
	}, 3*time.Second, 25*time.Millisecond)
	records, err := observed.snapshot()
	require.NoError(t, err)
	for _, record := range records {
		require.Equal(t, loserRaw, record.Value)
		require.Equal(t, loserTopic, testHeaderValue(record.Headers, "dlq.origin.topic"))
	}
	require.Zero(t, first.terminate(t), first.output.String())
	require.Zero(t, second.terminate(t), second.output.String())
}

// Concurrent independently owned consumer groups must share the global PG lock,
// preserve the entire resolved row, and skip rather than reinsert retained CH.
func TestAlertWriterConcurrentReplay(t *testing.T) {
	f := newAlertReplayFixture(t)
	f.publish(t, f.alert)
	detections := storageclickhouse.NewAlertStore(f.ch)
	workflows := storagepostgres.NewAlertStore(f.db.Pool, f.settings.OperationTimeout)
	require.NoError(t, f.run(t, detections, workflows))
	_, err := f.db.Pool.Exec(t.Context(), `UPDATE alert_workflows SET status='resolved',assignee='analyst-7',
		version=17,updated_at=$1 WHERE alert_id=$2`, time.Date(2026, 10, 16, 14, 0, 0, 123000000, time.UTC), f.alert.AlertID)
	require.NoError(t, err)
	before := f.workflow(t)
	const replicas = 4
	results := make(chan error, replicas)
	fixtures := make([]alertReplayFixture, replicas)
	for i := range replicas {
		fixtures[i] = *f
		fixtures[i].kafka.AlertWriterGroup = "concurrent-replay-" + uuid.NewString()
		fixture := &fixtures[i]
		consumer, consumerErr := fixture.consumer(t)
		require.NoError(t, consumerErr)
		go func() { results <- fixture.runConsumer(t.Context(), consumer, detections, workflows) }()
	}
	for range replicas {
		require.NoError(t, <-results)
	}
	for i := range replicas {
		require.Equal(t, int64(1), fixtures[i].committed(t))
	}
	f.rows(t, 1)
	require.JSONEq(t, before, f.workflow(t), "every mutable/list field must survive simultaneous replay")
}

type commitOnceAlertConsumer struct {
	*kafkax.Consumer
	cancel context.CancelFunc
}

func (c *commitOnceAlertConsumer) CommitRecords(ctx context.Context, records ...*kgo.Record) error {
	err := c.Consumer.CommitRecords(ctx, records...)
	if err == nil {
		c.cancel()
	}
	return err
}

func (f *alertReplayFixture) consumer(t *testing.T) (*kafkax.Consumer, error) {
	t.Helper()
	consumer, err := kafkax.NewAlertConsumer(t.Context(), f.kafka, f.settings)
	if err == nil {
		t.Cleanup(func() { closeWriterConsumer(t, consumer) })
	}
	return consumer, err
}

func (f *alertReplayFixture) run(t *testing.T,
	detections alertwriter.DetectionStore, workflows alertwriter.WorkflowStore) error {
	t.Helper()
	consumer, err := f.consumer(t)
	if err != nil {
		return err
	}
	defer closeWriterConsumer(t, consumer)
	return f.runConsumer(t.Context(), consumer, detections, workflows)
}

func (f *alertReplayFixture) runConsumer(ctx context.Context, consumer *kafkax.Consumer,
	detections alertwriter.DetectionStore, workflows alertwriter.WorkflowStore) error {
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	owned := &commitOnceAlertConsumer{Consumer: consumer, cancel: cancel}
	return alertwriter.New(owned, detections, workflows, f.settings, zerolog.Nop()).WithQuarantine(f.dlq).Run(runCtx)
}
