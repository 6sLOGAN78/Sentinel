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

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

type alertPipelineFixture struct {
	ch      storageclickhouse.Conn
	db      *database.Database
	kafka   config.KafkaConfig
	client  *kgo.Client
	admin   *kadm.Client
	process *proc
	alert   *contracts.Alert
	raw     []byte
}

func newAlertPipelineFixture(t *testing.T) *alertPipelineFixture {
	t.Helper()
	chConfig, conn := newRecoveryDatabase(t)
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	kafka := kafkaCfg
	kafka.AlertsTopic = sentineltesting.RequireUniqueTopic(t, kafkaCfg, "alert-pipeline")
	kafka.AlertWriterGroup = "alert-pipeline-" + uuid.NewString()
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	env := serviceEnv(t, "alert-writer")
	for key, value := range map[string]string{
		"SENTINEL_KAFKA.ALERTS_TOPIC":       kafka.AlertsTopic,
		"SENTINEL_KAFKA.ALERT_WRITER_GROUP": kafka.AlertWriterGroup,
		"SENTINEL_CLICKHOUSE.DATABASE":      chConfig.Database,
		"SENTINEL_WRITER.BATCH_SIZE":        "4",
		"SENTINEL_WRITER.FLUSH_INTERVAL":    "100ms",
	} {
		env = append(without(env, key+"="), key+"="+value)
	}
	process := startProc(t, "alert-writer", "", env)
	requireOutputEventually(t, process, "service started")
	start := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	principal := "analyst|\u03b4|" + uuid.NewString()
	alert := &contracts.Alert{
		SchemaVersion: 1, TenantID: "demo", RuleID: "AUTH_BRUTE_FORCE", RuleVersion: 1,
		Severity: "high", Status: "open", CreatedAt: contracts.NewTimestamp(start.Add(5 * time.Minute)),
		WindowStart: contracts.NewTimestamp(start), WindowEnd: contracts.NewTimestamp(start.Add(5 * time.Minute)),
		EntityKey: "192.0.2.10|" + principal, Summary: "10 authentication failures", EventCount: 10,
		SampleEventIDs: []string{uuid.NewString(), uuid.NewString()}, DedupeKey: "AUTH_BRUTE_FORCE|192.0.2.10|" + principal,
	}
	alert.AlertID = contracts.DedupeKey(alert.TenantID, alert.RuleID, []string{"192.0.2.10", principal}, start)
	require.NoError(t, alert.Validate())
	raw, err := json.Marshal(alert)
	require.NoError(t, err)
	return &alertPipelineFixture{ch: conn, db: db, kafka: kafka, client: client,
		admin: kadm.NewClient(client), process: process, alert: alert, raw: raw}
}

func (f *alertPipelineFixture) publish(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, f.client.ProduceSync(ctx, &kgo.Record{Topic: f.kafka.AlertsTopic,
		Key: []byte(f.alert.TenantID + "|" + f.alert.AlertID), Value: f.raw}).FirstErr())
}

func (f *alertPipelineFixture) waitStored(t *testing.T, offset int64) {
	t.Helper()
	// A missing persistence schema is a behavioral failure after successful
	// compilation, container startup, binary startup and real Kafka publication.
	var rows uint64
	require.NoError(t, f.ch.QueryRow(t.Context(), "SELECT count() FROM alerts").Scan(&rows),
		"immutable alert persistence schema is absent")
	var relation *string
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), "SELECT to_regclass('alert_workflows')::text").Scan(&relation))
	require.NotNil(t, relation, "workflow persistence schema is absent")
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		offsets, err := f.admin.FetchOffsets(ctx, f.kafka.AlertWriterGroup)
		if err != nil {
			return false
		}
		got, ok := offsets.Lookup(f.kafka.AlertsTopic, 0)
		return ok && got.Err == nil && got.At == offset
	}, 25*time.Second, 50*time.Millisecond, "alert source never committed; output:\n%s", f.process.output.String())
	var stored string
	require.NoError(t, f.ch.QueryRow(t.Context(), `SELECT canonical_payload FROM alerts
		WHERE tenant_id={tenant:String} AND alert_id={id:String} LIMIT 1`,
		driverclickhouse.Named("tenant", f.alert.TenantID), driverclickhouse.Named("id", f.alert.AlertID)).Scan(&stored))
	require.JSONEq(t, string(f.raw), stored, "source commit must follow exact immutable evidence")
	var count uint64
	require.NoError(t, f.ch.QueryRow(t.Context(), `SELECT uniqExact(alert_id) FROM alerts
		WHERE tenant_id={tenant:String}`, driverclickhouse.Named("tenant", f.alert.TenantID)).Scan(&count))
	require.Equal(t, uint64(1), count)
}

// Omitting either native write or committing before their acknowledgements must fail.
func TestAlertWriterPipeline(t *testing.T) {
	f := newAlertPipelineFixture(t)
	f.publish(t)
	f.waitStored(t, 1)
	var status, rule, severity, summary, entity, fingerprint string
	var assignee *string
	var version, seq, count int64
	var created, updated time.Time
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT status,assignee,version,seq,
		created_at,updated_at,rule_id,severity,summary,entity_key,event_count,immutable_fingerprint
		FROM alert_workflows WHERE tenant_id=$1 AND alert_id=$2`, f.alert.TenantID, f.alert.AlertID).
		Scan(&status, &assignee, &version, &seq, &created, &updated, &rule, &severity, &summary, &entity, &count, &fingerprint))
	require.Equal(t, "open", status)
	require.Nil(t, assignee)
	require.Equal(t, int64(1), version)
	require.Positive(t, seq)
	require.Equal(t, f.alert.CreatedAt.Time, created)
	require.Equal(t, created, updated)
	require.Equal(t, f.alert.RuleID, rule)
	require.Equal(t, f.alert.Severity, severity)
	require.Equal(t, f.alert.Summary, summary)
	require.Equal(t, f.alert.EntityKey, entity)
	require.Equal(t, int64(10), count)
	require.Len(t, fingerprint, 64)
	require.Zero(t, f.process.terminate(t), f.process.output.String())
}

// An upsert that reopens or reallocates a workflow must fail even on identical bytes.
func TestAlertWorkflowReplay(t *testing.T) {
	f := newAlertPipelineFixture(t)
	f.publish(t)
	f.waitStored(t, 1)
	changed := time.Date(2026, 10, 16, 13, 0, 0, 123000000, time.UTC)
	_, err := f.db.Pool.Exec(t.Context(), `UPDATE alert_workflows SET status='resolved',
		assignee='analyst-7',version=9,updated_at=$1 WHERE alert_id=$2`, changed, f.alert.AlertID)
	require.NoError(t, err)
	var before string
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT row_to_json(w)::text
		FROM alert_workflows w WHERE alert_id=$1`, f.alert.AlertID).Scan(&before))
	f.publish(t)
	f.waitStored(t, 2)
	var after string
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT row_to_json(w)::text
		FROM alert_workflows w WHERE alert_id=$1`, f.alert.AlertID).Scan(&after))
	require.JSONEq(t, before, after, "replay must preserve all workflow fields including seq and timestamps")
	var count int
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM alert_workflows WHERE alert_id=$1`, f.alert.AlertID).Scan(&count))
	require.Equal(t, 1, count)
	require.Zero(t, f.process.terminate(t), f.process.output.String())
}
