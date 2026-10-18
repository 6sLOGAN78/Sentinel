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

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

type cooldownFixture struct {
	kafka         config.KafkaConfig
	db            *database.Database
	ch            storageclickhouse.Conn
	process       *proc
	observer      *kgo.Client
	admin         *kadm.Client
	rules         *detection.Rules
	state         *detection.PartitionState
	start         time.Time
	ip, principal string
	offset        int64
	candidates    []detection.Candidate
}

func newCooldownFixture(t *testing.T, short bool) *cooldownFixture {
	t.Helper()
	env, kafka := detectorPGEnv(t, pgCfg)
	raw, err := os.ReadFile("../../config/rules.yaml")
	require.NoError(t, err)
	if short {
		raw = bytes.Replace(raw, []byte("window: 300s"), []byte("window: 1s"), 1)
	}
	rules, err := detection.LoadRules(bytes.NewReader(raw))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	env = append(without(env, "SENTINEL_DETECTOR.RULES_PATH="), "SENTINEL_DETECTOR.RULES_PATH="+path)
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	chCfg, ch := newRecoveryDatabase(t)
	writerEnv := serviceEnv(t, "alert-writer")
	for key, value := range map[string]string{"SENTINEL_KAFKA.ALERTS_TOPIC": kafka.AlertsTopic,
		"SENTINEL_KAFKA.ALERT_WRITER_GROUP": "cooldown-writer-" + uuid.NewString(),
		"SENTINEL_CLICKHOUSE.DATABASE":      chCfg.Database} {
		writerEnv = append(without(writerEnv, key+"="), key+"="+value)
	}
	writer := startProc(t, "alert-writer", "", writerEnv)
	requireOutputEventually(t, writer, "service started")
	process := startProc(t, "detector", "", env)
	requireOutputEventually(t, process, "service started")
	observer, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{kafka.AlertsTopic: {0: kgo.NewOffset().AtStart()}}))
	require.NoError(t, err)
	t.Cleanup(observer.Close)
	return &cooldownFixture{kafka: kafka, db: db, ch: ch, process: process, observer: observer,
		admin: kadm.NewClient(observer), rules: rules, state: detection.NewPartitionState(rules),
		start: time.Now().UTC().Truncate(5 * time.Minute), ip: "192.0.2.19", principal: "cooldown|" + uuid.NewString()}
}

func (f *cooldownFixture) send(t *testing.T, seconds int) {
	t.Helper()
	events := canonicalWriterEvents(10, f.start.Add(time.Duration(seconds)*time.Second))
	for _, event := range events {
		event.EventType, event.SourceIP, event.Principal = "auth.failure", &f.ip, &f.principal
		out, err := f.state.ApplySource(detection.TopicPartition{Topic: f.kafka.EventsTopic, Partition: 0}, f.offset, event)
		require.NoError(t, err)
		f.candidates = append(f.candidates, out.Candidates...)
		require.NoError(t, f.state.Acknowledge(f.offset))
		f.offset++
	}
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), events, uuid.NewString()))
	f.waitSource(t)
}

func (f *cooldownFixture) waitSource(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		offsets, err := f.admin.FetchOffsets(ctx, f.kafka.DetectorGroup)
		if err != nil {
			return false
		}
		o, ok := offsets.Lookup(f.kafka.EventsTopic, 0)
		var m detection.Metadata
		return ok && o.Err == nil && json.Unmarshal([]byte(o.Metadata), &m) == nil && m.ProcessedNext == f.offset
	}, 15*time.Second, 50*time.Millisecond, "source did not finish: %s", f.process.output.String())
}

func (f *cooldownFixture) alerts(t *testing.T) []*kgo.Record {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	var records []*kgo.Record
	for ctx.Err() == nil {
		records = append(records, f.observer.PollRecords(ctx, 100).Records()...)
	}
	return records
}

func (f *cooldownFixture) stored(t *testing.T, allowed, suppressed int) {
	t.Helper()
	var relation *string
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), "SELECT to_regclass('detection_decisions')::text").Scan(&relation))
	require.NotNil(t, relation, "durable allow/suppress ledger is absent")
	var yes, no int
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT count(*) FILTER(WHERE allowed),
		count(*) FILTER(WHERE NOT allowed) FROM detection_decisions WHERE topic=$1`, f.kafka.EventsTopic).Scan(&yes, &no))
	require.Equal(t, allowed, yes)
	require.Equal(t, suppressed, no)
	for _, candidate := range f.candidates {
		var a contracts.Alert
		require.NoError(t, json.Unmarshal(candidate.CanonicalBytes(), &a))
		var allow bool
		var raw []byte
		require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT allowed,canonical_payload
			FROM detection_decisions WHERE alert_id=$1`, a.AlertID).Scan(&allow, &raw))
		require.Equal(t, candidate.CanonicalBytes(), raw)
		expected := 0
		if allow {
			expected = 1
		}
		require.Eventually(t, func() bool {
			var count int
			return f.db.Pool.QueryRow(t.Context(), "SELECT count(*) FROM alert_workflows WHERE alert_id=$1", a.AlertID).
				Scan(&count) == nil && count == expected
		}, 10*time.Second, 25*time.Millisecond)
		var count uint64
		require.NoError(t, f.ch.QueryRow(t.Context(), `SELECT uniqExact(alert_id) FROM alerts WHERE alert_id={id:String}`,
			driverclickhouse.Named("id", a.AlertID)).Scan(&count))
		require.Equal(t, uint64(expected), count)
	}
}

func TestDetectorCooldownVisible(t *testing.T) {
	f := newCooldownFixture(t, false)
	f.send(t, 299)
	f.send(t, 301)
	records := f.alerts(t)
	require.Len(t, records, 1, "adjacent AUTH windows two seconds apart require real visible suppression")
	f.stored(t, 1, 1)
	f.send(t, 601)
	records = append(records, f.alerts(t)...)
	require.Len(t, records, 2)
	require.NotEqual(t, string(records[0].Key), string(records[1].Key))
	f.stored(t, 2, 1)
	require.Zero(t, f.process.terminate(t), f.process.output.String())
}

func TestDetectorCooldownExpiry(t *testing.T) {
	f := newCooldownFixture(t, true)
	f.send(t, 0)
	f.send(t, 2)
	f.send(t, 60)
	require.Len(t, f.alerts(t), 2, "fresh window exactly at expiry must allow; earlier suppressed window stays suppressed")
	f.stored(t, 2, 1)
	// More evidence in the already suppressed window must never retry at expiry.
	f.send(t, 2)
	require.Empty(t, f.alerts(t))
	f.stored(t, 2, 1)
	require.Zero(t, f.process.terminate(t), f.process.output.String())
}

func TestDetectorCooldownReplay(t *testing.T) {
	f := newCooldownFixture(t, true)
	f.send(t, 0)
	f.send(t, 2)
	f.send(t, 60)
	f.alerts(t)
	f.stored(t, 2, 1)
	require.Zero(t, f.process.terminate(t), f.process.output.String())
	owner := postgres.DetectionOwnership{Group: f.kafka.DetectorGroup,
		Source: detection.TopicPartition{Topic: f.kafka.EventsTopic, Partition: 0}, Config: f.rules.Fingerprint()}
	var token int64
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT incarnation,member_id,generation,assignment_token
		FROM detection_partition_fences WHERE group_id=$1 AND topic=$2 AND partition_id=0`, owner.Group, owner.Source.Topic).
		Scan(&owner.Incarnation, &owner.Member, &owner.Generation, &token))
	owner.Token = uint64(token)
	store := postgres.NewDetectionDecisionStore(f.db.Pool, config.DetectorPGConfig{AcquireTimeout: time.Second,
		TransactionTimeout: 5 * time.Second, LockTimeout: time.Second, StatementTimeout: 2 * time.Second, CleanupTimeout: time.Second}, f.rules)
	var before, after string
	query := `SELECT row_to_json(c)::text FROM detection_cooldown_cursors c WHERE topic=$1`
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), query, owner.Source.Topic).Scan(&before))
	for i, candidate := range f.candidates {
		decision, err := store.Decide(t.Context(), owner, candidate)
		require.NoError(t, err)
		require.Equal(t, i != 1, decision.Allowed)
		require.Equal(t, candidate.CanonicalBytes(), decision.Canonical)
	}
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), query, owner.Source.Topic).Scan(&after))
	require.Equal(t, before, after, "old allow/suppress replay must not consult or mutate newer cursor")
	t.Run("monotone clock and identity isolation", func(t *testing.T) {
		for _, tc := range []struct {
			tenant, ip, principal, rule string
			seconds                     int
			allowed                     bool
		}{
			{"demo", f.ip, f.principal, "AUTH_BRUTE_FORCE", 59, false},
			{"other", f.ip, f.principal, "AUTH_BRUTE_FORCE", 59, true},
			{"demo", "192.0.2.20", f.principal, "AUTH_BRUTE_FORCE", 59, true},
			{"demo", f.ip, f.principal + "|other", "AUTH_BRUTE_FORCE", 59, true},
			{"demo", f.ip, "", "REQUEST_FLOOD", 59, true},
		} {
			f.offset += 1000
			candidate := cooldownCandidate(t, f.rules, owner.Source, f.offset,
				f.start.Add(time.Duration(tc.seconds)*time.Second), tc.tenant, tc.ip, tc.principal, tc.rule)
			decision, err := store.Decide(t.Context(), owner, candidate)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, decision.Allowed)
		}
		var maximum, next time.Time
		require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT max_candidate_time,next_eligible_at
			FROM detection_cooldown_cursors WHERE topic=$1 AND tenant_id='demo'
			AND rule_id='AUTH_BRUTE_FORCE' AND entity_components=$2`, owner.Source.Topic,
			`["`+f.ip+`","`+f.principal+`"]`).Scan(&maximum, &next))
		require.Equal(t, f.start.Add(time.Minute), maximum.UTC())
		require.Equal(t, f.start.Add(2*time.Minute), next.UTC())
	})
	t.Run("divergent reconstruction and missing older decision", func(t *testing.T) {
		candidate := cooldownCandidate(t, f.rules, owner.Source, 0, f.start, "demo", f.ip, f.principal, "AUTH_BRUTE_FORCE")
		_, err := store.Decide(t.Context(), owner, candidate)
		require.Error(t, err, "different frozen evidence for old ID must fail closed")
		missing := cooldownCandidate(t, f.rules, owner.Source, 1, f.start.Add(3*time.Second),
			"demo", f.ip, f.principal, "AUTH_BRUTE_FORCE")
		_, err = store.Decide(t.Context(), owner, missing)
		require.Error(t, err, "absent older source decision must fail closed")
	})
	t.Run("ownership registration rejects stale and random incarnation", func(t *testing.T) {
		frontier := detection.DecisionFrontier{None: true}
		for _, change := range []func(*postgres.DetectionOwnership){
			func(o *postgres.DetectionOwnership) { o.Generation-- },
			func(o *postgres.DetectionOwnership) { o.Member = "wrong-member" },
			func(o *postgres.DetectionOwnership) { o.Incarnation = uuid.NewString(); o.Generation++ },
			func(o *postgres.DetectionOwnership) { o.Token = 0 },
		} {
			stale := owner
			change(&stale)
			require.Error(t, store.Register(t.Context(), stale, frontier))
			_, err := store.Decide(t.Context(), stale, f.candidates[0])
			require.Error(t, err)
		}
	})
}

func cooldownCandidate(t *testing.T, rules *detection.Rules, source detection.TopicPartition,
	offset int64, at time.Time, tenant, ip, principal, rule string) detection.Candidate {
	t.Helper()
	state := detection.NewPartitionState(rules)
	count := 10
	if rule == "REQUEST_FLOOD" {
		count = 300
	}
	events := canonicalWriterEvents(count, at)
	var candidate detection.Candidate
	for i, event := range events {
		event.TenantID, event.SourceIP = tenant, &ip
		event.EventType, event.Principal = "auth.failure", &principal
		if rule == "REQUEST_FLOOD" {
			event.EventType = "net.request"
			event.Principal = nil
		}
		out, err := state.ApplySource(source, offset+int64(i), event)
		require.NoError(t, err)
		if len(out.Candidates) > 0 {
			candidate = out.Candidates[0]
		}
		require.NoError(t, state.Acknowledge(offset+int64(i)))
	}
	require.NotEmpty(t, candidate.CanonicalBytes())
	return candidate
}
