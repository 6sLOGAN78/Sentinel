//go:build integration

package test_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
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
	env           []string
}

func newCooldownFixture(t *testing.T, short bool) *cooldownFixture {
	t.Helper()
	return newCooldownPGFixture(t, short, pgCfg)
}

func newCooldownPGFixture(t *testing.T, short bool, pg config.DatabaseConfig) *cooldownFixture {
	t.Helper()
	env, kafka := detectorPGEnv(t, pg)
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
	require.NoError(t, database.Migrate(t.Context(), pg, zerolog.Nop()))
	db, err := database.New(t.Context(), pg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	chCfg, ch := newRecoveryDatabase(t)
	writerEnv := serviceEnv(t, "alert-writer")
	for key, value := range map[string]string{"SENTINEL_KAFKA.ALERTS_TOPIC": kafka.AlertsTopic,
		"SENTINEL_KAFKA.ALERT_WRITER_GROUP": "cooldown-writer-" + uuid.NewString(),
		"SENTINEL_CLICKHOUSE.DATABASE":      chCfg.Database,
		"SENTINEL_DATABASE.HOST":            pg.Host, "SENTINEL_DATABASE.PORT": strconv.Itoa(pg.Port),
		"SENTINEL_DATABASE.USER": pg.User, "SENTINEL_DATABASE.PASSWORD": pg.Password,
		"SENTINEL_DATABASE.NAME": pg.Name, "SENTINEL_DATABASE.SSL_MODE": "disable"} {
		writerEnv = append(without(writerEnv, key+"="), key+"="+value)
	}
	writer := startProc(t, "alert-writer", "", writerEnv)
	requireOutputEventually(t, writer)
	process := startProc(t, "detector", "", env)
	requireOutputEventually(t, process)
	requireStatusEventually(t, "http://127.0.0.1:"+opsPort(t, env)+"/readyz", http.StatusOK)
	observer, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{kafka.AlertsTopic: {0: kgo.NewOffset().AtStart()}}))
	require.NoError(t, err)
	t.Cleanup(observer.Close)
	// Principal is not part of FLOOD/DENY identity. Isolate every rule by source
	// within the documentation prefix, including repeated families in one PG.
	entropy := uuid.New()
	address := [16]byte{0x20, 0x01, 0x0d, 0xb8}
	copy(address[4:], entropy[:12])
	return &cooldownFixture{kafka: kafka, db: db, ch: ch, process: process, observer: observer,
		admin: kadm.NewClient(observer), rules: rules, state: detection.NewPartitionState(rules),
		start: time.Now().UTC().Truncate(5 * time.Minute), ip: netip.AddrFrom16(address).String(),
		principal: "cooldown|" + uuid.NewString(), env: env}
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
	f.stored(t, 1, 0)
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
	var generation int32
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT generation FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2 AND partition_id=0`, f.kafka.DetectorGroup, f.kafka.EventsTopic).Scan(&generation))
	f.process = startProc(t, "detector", "", f.env)
	requireOutputEventually(t, f.process)
	require.Eventually(t, func() bool {
		var current int32
		return f.db.Pool.QueryRow(t.Context(), `SELECT generation FROM detection_partition_fences
			WHERE group_id=$1 AND topic=$2 AND partition_id=0`, f.kafka.DetectorGroup, f.kafka.EventsTopic).
			Scan(&current) == nil && current > generation
	}, 15*time.Second, 25*time.Millisecond,
		"restarted detector never verified durable frontier: %s", f.process.output.String())
	replayed := f.alerts(t)
	require.Len(t, replayed, 1, "observe full active-window replay before terminating restarted detector")
	require.Equal(t, f.candidates[2].CanonicalBytes(), replayed[0].Value)
	f.waitSource(t)
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
		TransactionTimeout: 5 * time.Second, LockTimeout: time.Second,
		StatementTimeout: 2 * time.Second, CleanupTimeout: time.Second}, f.rules)
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
	var first contracts.Alert
	require.NoError(t, json.Unmarshal(f.candidates[0].CanonicalBytes(), &first))
	_, mutationErr := f.db.Pool.Exec(t.Context(),
		"UPDATE detection_decisions SET allowed=NOT allowed WHERE alert_id=$1", first.AlertID)
	require.ErrorContains(t, mutationErr, "immutable", "first decision cannot be rewritten through SQL")
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
		require.Equal(t, f.candidates[2].TriggerOccurredAt().Time, maximum.UTC())
		require.Equal(t, f.candidates[2].TriggerOccurredAt().Add(time.Minute), next.UTC())
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
		advanced := owner
		advanced.Token++
		require.NoError(t, store.Register(t.Context(), advanced, frontier))
		require.Error(t, store.Register(t.Context(), owner, frontier), "delayed lower token must not steal current fence")
		_, err := store.Decide(t.Context(), owner, f.candidates[0])
		require.Error(t, err, "superseded assignment must not make even replay decisions")
		owner = advanced
	})
	t.Run("restore requires matching decision and fence; PG ahead allowed", func(t *testing.T) {
		last := f.candidates[2]
		var alert contracts.Alert
		require.NoError(t, json.Unmarshal(last.CanonicalBytes(), &alert))
		frontier := detection.DecisionFrontier{Offset: last.Offset(), RuleOrdinal: last.RuleOrdinal(),
			AlertID: alert.AlertID, Fingerprint: last.Fingerprint()}
		require.NoError(t, store.Register(t.Context(), owner, frontier), "legitimate newer PG frontier must restore")
		wrong := frontier
		wrong.AlertID = strings.Repeat("f", 64)
		require.Error(t, store.Register(t.Context(), owner, wrong), "missing checkpoint ledger row must fail closed")
		wrong = frontier
		wrong.Fingerprint = strings.Repeat("f", 64)
		require.Error(t, store.Register(t.Context(), owner, wrong), "different checkpoint fingerprint must fail closed")
		var oldOffset int64
		var oldOrdinal int
		require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT frontier_offset,frontier_ordinal
			FROM detection_partition_fences WHERE group_id=$1 AND topic=$2 AND partition_id=0`, owner.Group, owner.Source.Topic).
			Scan(&oldOffset, &oldOrdinal))
		_, err := f.db.Pool.Exec(t.Context(), `UPDATE detection_partition_fences SET frontier_offset=-1,frontier_ordinal=-1
			WHERE group_id=$1 AND topic=$2 AND partition_id=0`, owner.Group, owner.Source.Topic)
		require.NoError(t, err)
		require.Error(t, store.Register(t.Context(), owner, frontier), "restore-behind fence must fail closed")
		_, err = f.db.Pool.Exec(t.Context(), `UPDATE detection_partition_fences SET frontier_offset=$3,frontier_ordinal=$4
			WHERE group_id=$1 AND topic=$2 AND partition_id=0`, owner.Group, owner.Source.Topic, oldOffset, oldOrdinal)
		require.NoError(t, err)
	})
	t.Run("PG lock failure retains last successful checkpoint", func(t *testing.T) {
		blocked := newCooldownFixture(t, true)
		blocked.send(t, 0)
		require.Len(t, blocked.alerts(t), 1)
		events := canonicalWriterEvents(10, blocked.start.Add(2*time.Second))
		for _, event := range events {
			event.EventType, event.SourceIP, event.Principal = "auth.failure", &blocked.ip, &blocked.principal
		}
		producer := newWriterProducer(t, blocked.kafka)
		require.NoError(t, producer.PublishBatch(t.Context(), events[:9], uuid.NewString()))
		blocked.offset += 9
		blocked.waitSource(t)
		before, err := blocked.admin.FetchOffsets(t.Context(), blocked.kafka.DetectorGroup)
		require.NoError(t, err)
		checkpoint, ok := before.Lookup(blocked.kafka.EventsTopic, 0)
		require.True(t, ok)
		lock, err := blocked.db.Pool.Begin(t.Context())
		require.NoError(t, err)
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, lock.Rollback(ctx))
		}()
		_, err = lock.Exec(t.Context(), `SELECT 1 FROM detection_partition_fences
			WHERE group_id=$1 AND topic=$2 AND partition_id=0 FOR UPDATE`,
			blocked.kafka.DetectorGroup, blocked.kafka.EventsTopic)
		require.NoError(t, err)
		require.NoError(t, producer.PublishBatch(t.Context(), events[9:], uuid.NewString()))
		require.NotZero(t, blocked.process.requireExit(t, 5*time.Second), "lock timeout must terminate boundedly")
		after, err := blocked.admin.FetchOffsets(t.Context(), blocked.kafka.DetectorGroup)
		require.NoError(t, err)
		retained, ok := after.Lookup(blocked.kafka.EventsTopic, 0)
		require.True(t, ok)
		require.Equal(t, checkpoint.At, retained.At)
		require.Equal(t, checkpoint.Metadata, retained.Metadata, "PG failure must preserve exact last successful metadata")
		require.Empty(t, blocked.alerts(t), "failed decision must not publish")
		var decisions int
		require.NoError(t, blocked.db.Pool.QueryRow(t.Context(), "SELECT count(*) FROM detection_decisions WHERE topic=$1",
			blocked.kafka.EventsTopic).Scan(&decisions))
		require.Equal(t, 1, decisions, "failed decision must not mutate durable ledger")
	})
}

// Separate tests share PG in a full package run. REQUEST_FLOOD ignores the
// principal, so a random principal alone cannot isolate its global alert ID.
func TestDetectorCooldownFixtureIsolation(t *testing.T) {
	first := newCooldownFixture(t, true)
	second := newCooldownFixture(t, true)
	second.start = first.start // Same event-time window is intentional.
	var identities []string
	for _, f := range []*cooldownFixture{first, second} {
		require.Zero(t, f.process.terminate(t), f.process.output.String())
		owner := cooldownOwner(t, f, 0)
		candidate := cooldownCandidate(t, f.rules, owner.Source, 1000,
			f.start.Add(59*time.Second), "demo", f.ip, f.principal, "REQUEST_FLOOD")
		var alert contracts.Alert
		require.NoError(t, json.Unmarshal(candidate.CanonicalBytes(), &alert))
		identities = append(identities, alert.AlertID)
		decision, err := cooldownStore(f).Decide(t.Context(), owner, candidate)
		require.NoError(t, err, "separate test fixtures must not collide on a global alert identity")
		require.True(t, decision.Allowed)
		require.Equal(t, candidate.CanonicalBytes(), decision.Canonical)
	}
	require.NotEqual(t, identities[0], identities[1])
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

func cooldownStore(f *cooldownFixture,
	hooks ...func(context.Context, string, detection.TopicPartition, int64) error) *postgres.DetectionDecisionStore {
	return postgres.NewDetectionDecisionStore(f.db.Pool, config.DetectorPGConfig{AcquireTimeout: time.Second,
		TransactionTimeout: 5 * time.Second, LockTimeout: 250 * time.Millisecond,
		StatementTimeout: time.Second, CleanupTimeout: time.Second}, f.rules, hooks...)
}

func cooldownOwner(t *testing.T, f *cooldownFixture, partition int32) postgres.DetectionOwnership {
	t.Helper()
	o := postgres.DetectionOwnership{Group: f.kafka.DetectorGroup, Config: f.rules.Fingerprint(),
		Source: detection.TopicPartition{Topic: f.kafka.EventsTopic, Partition: partition}}
	var token int64
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT incarnation,member_id,generation,assignment_token
		FROM detection_partition_fences WHERE group_id=$1 AND topic=$2 AND partition_id=$3`,
		o.Group, o.Source.Topic, partition).Scan(&o.Incarnation, &o.Member, &o.Generation, &token))
	o.Token = uint64(token)
	return o
}

// Inspect all operational rows, so a rejected owner cannot silently alter its
// fence or cursor even when the immutable decision count stays unchanged.
func cooldownRows(t *testing.T, f *cooldownFixture) string {
	t.Helper()
	var rows string
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT json_build_array(
		(SELECT json_agg(c ORDER BY alert_id) FROM detection_decisions c WHERE group_id=$1),
		(SELECT json_agg(c ORDER BY partition_id) FROM detection_partition_fences c WHERE group_id=$1),
		(SELECT json_agg(c ORDER BY entity_components) FROM detection_cooldown_cursors c WHERE group_id=$1))::text`,
		f.kafka.DetectorGroup).Scan(&rows))
	return rows
}

func TestDetectorCooldownCrash(t *testing.T) {
	binary := buildIntegrationBinary(t, "detector")
	for _, boundary := range []string{"before-decision-commit", "after-decision-commit-before-publish",
		"after-ack-before-checkpoint", "after-successful-checkpoint"} {
		t.Run(boundary, func(t *testing.T) {
			f := newCooldownFixture(t, false)
			stopInitialDetector(t, f)
			// Exercise a nonzero source partition through the shared protocol.
			// This also guards the barrier helper against a hardcoded zero.
			topic := "cooldown-cut-" + uuid.NewString()
			response, err := f.admin.CreateTopics(t.Context(), 2, 1, nil, topic)
			require.NoError(t, err)
			require.NoError(t, response[topic].Err)
			f.kafka.EventsTopic = topic
			f.env = append(without(f.env, "SENTINEL_KAFKA.EVENTS_TOPIC="), "SENTINEL_KAFKA.EVENTS_TOPIC="+topic)
			events, canonical := recoveryAuthEvents(t, f, f.principal)
			env, barrier := detectorBarrierEnv(t, f.env, boundary, 1, 9)
			env = append(without(env, "SENTINEL_DETECTOR.WRITER.BATCH_SIZE="), "SENTINEL_DETECTOR.WRITER.BATCH_SIZE=1")
			f.process = startRecoveryProc(t, binary, env)
			publishCooldownPartition(t, f, 1, events)
			reachedDetectorBarrier(t, barrier, boundary, f.kafka.EventsTopic, 1, 9)
			var count int
			require.NoError(t, f.db.Pool.QueryRow(t.Context(),
				"SELECT count(*) FROM detection_decisions WHERE topic=$1", f.kafka.EventsTopic).Scan(&count))
			if boundary == "before-decision-commit" {
				require.Zero(t, count, "uncommitted decision must be invisible to another connection")
				// NOWAIT proves the reached observation is causally inside the
				// live transaction, rather than before Begin or after Commit.
				tx, beginErr := f.db.Pool.Begin(t.Context())
				require.NoError(t, beginErr)
				_, lockErr := tx.Exec(t.Context(), `SELECT 1 FROM detection_partition_fences
					WHERE group_id=$1 FOR UPDATE NOWAIT`, f.kafka.DetectorGroup)
				require.ErrorContains(t, lockErr, "55P03")
				require.NoError(t, tx.Rollback(t.Context()))
			} else {
				require.Equal(t, 1, count)
			}
			next := int64(9)
			if boundary == "after-successful-checkpoint" {
				next = 10
			}
			before := recoveryCheckpoint(t, f, 1, next)
			workflow := ""
			if boundary == "before-decision-commit" || boundary == "after-decision-commit-before-publish" {
				require.Empty(t, f.alerts(t))
			} else {
				requireRecoveredAlert(t, f, canonical)
				workflow = resolveCooldownWorkflow(t, f, canonical)
			}
			killDetector(t, f.process)
			f.process = startProc(t, "detector", "", f.env)
			requireRecoveredAlert(t, f, canonical)
			after := recoveryCheckpoint(t, f, 1, 10)
			if next == 10 {
				require.Equal(t, before, after)
			}
			if workflow != "" {
				require.Equal(t, workflow, cooldownWorkflow(t, f, canonical))
			}
			require.Zero(t, f.process.terminate(t), f.process.output.String())
		})
	}
	// This retained family proves old allow AND suppress after a newer cursor,
	// equality at expiry and complete immutable PG bytes independently of cuts.
	t.Run("old allow and suppress after newer cursor", TestDetectorCooldownReplay)
}

func cooldownWorkflow(t *testing.T, f *cooldownFixture, canonical []byte) string {
	t.Helper()
	var alert contracts.Alert
	require.NoError(t, json.Unmarshal(canonical, &alert))
	var row string
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT row_to_json(w)::text FROM alert_workflows w WHERE alert_id=$1", alert.AlertID).Scan(&row))
	return row
}

func resolveCooldownWorkflow(t *testing.T, f *cooldownFixture, canonical []byte) string {
	t.Helper()
	var alert contracts.Alert
	require.NoError(t, json.Unmarshal(canonical, &alert))
	_, err := f.db.Pool.Exec(t.Context(), `UPDATE alert_workflows SET status='resolved',assignee='cooldown-analyst',
		version=7,updated_at=created_at+interval '1 second' WHERE alert_id=$1`, alert.AlertID)
	require.NoError(t, err)
	return cooldownWorkflow(t, f, canonical)
}

func publishCooldownPartition(t *testing.T, f *cooldownFixture, partition int32, events []*contracts.Event) {
	t.Helper()
	producer, err := kgo.NewClient(kgo.SeedBrokers(f.kafka.Brokers), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	defer producer.Close()
	for _, event := range events {
		raw, marshalErr := json.Marshal(event)
		require.NoError(t, marshalErr)
		record := &kgo.Record{Topic: f.kafka.EventsTopic, Partition: partition, Key: []byte("demo|" + f.ip), Value: raw}
		require.NoError(t, producer.ProduceSync(t.Context(), record).FirstErr())
	}
}

func TestDetectorCooldownOutage(t *testing.T) {
	t.Run("connection outage preserves checkpoint", func(t *testing.T) {
		pg, handle, err := sentineltesting.StartPostgresFaultContainer(t.Context())
		require.NoError(t, err)
		t.Cleanup(handle.Close)
		f := newCooldownPGFixture(t, false, pg)
		f.send(t, 299)
		f.stored(t, 1, 0)
		before, err := f.admin.FetchOffsets(t.Context(), f.kafka.DetectorGroup)
		require.NoError(t, err)
		checkpoint, ok := before.Lookup(f.kafka.EventsTopic, 0)
		require.True(t, ok)
		f.alerts(t)
		require.NoError(t, handle.Pause(t.Context()))
		paused := true
		t.Cleanup(func() {
			if !paused {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			require.NoError(t, handle.Unpause(ctx))
		})
		requireDependencyEventually(t, "http://127.0.0.1:"+opsPort(t, f.env)+"/readyz", "postgres", "unavailable")
		events := canonicalWriterEvents(10, f.start.Add(301*time.Second))
		for _, event := range events {
			event.EventType, event.SourceIP, event.Principal = "auth.failure", &f.ip, &f.principal
		}
		require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), events, uuid.NewString()))
		require.NotZero(t, f.process.requireExit(t, 5*time.Second))
		after, err := f.admin.FetchOffsets(t.Context(), f.kafka.DetectorGroup)
		require.NoError(t, err)
		retained, ok := after.Lookup(f.kafka.EventsTopic, 0)
		require.True(t, ok)
		require.Equal(t, checkpoint, retained)
		require.Empty(t, f.alerts(t))
		require.NoError(t, handle.Unpause(t.Context()))
		paused = false
		f.process = startProc(t, "detector", "", f.env)
		f.offset = 20
		f.waitSource(t)
		f.stored(t, 1, 1)
		require.Zero(t, f.process.terminate(t), f.process.output.String())
	})
	t.Run("commit acknowledged ambiguously then immutable lookup", func(t *testing.T) {
		f := newCooldownFixture(t, true)
		f.send(t, 0)
		stopInitialDetector(t, f)
		owner := cooldownOwner(t, f, 0)
		candidate := cooldownCandidate(t, f.rules, owner.Source, 100, f.start.Add(60*time.Second),
			"demo", f.ip, f.principal, "AUTH_BRUTE_FORCE")
		observed := false
		store := cooldownStore(f, func(_ context.Context, boundary string, _ detection.TopicPartition, _ int64) error {
			if boundary == "after-decision-commit-before-publish" {
				observed = true
				return context.DeadlineExceeded
			}
			return nil
		})
		decision, err := store.Decide(t.Context(), owner, candidate)
		require.Error(t, err, "uncertain acknowledgement cannot authorize a publication")
		require.True(t, observed, "post-commit callback must observe a committed real transaction")
		require.Empty(t, decision.Canonical)
		before := cooldownRows(t, f)
		decision, err = cooldownStore(f).Decide(t.Context(), owner, candidate)
		require.NoError(t, err)
		require.True(t, decision.Allowed)
		require.Equal(t, candidate.CanonicalBytes(), decision.Canonical)
		require.Equal(t, before, cooldownRows(t, f))
	})
	t.Run("lock deadline and explicit rollback", TestDetectorCooldownReplay)
}

func TestDetectorCooldownOwners(t *testing.T) {
	f := newCooldownFixture(t, false)
	stopInitialDetector(t, f)
	topic := "cooldown-transfer-" + uuid.NewString()
	response, err := f.admin.CreateTopics(t.Context(), 12, 1, nil, topic)
	require.NoError(t, err)
	require.NoError(t, response[topic].Err)
	f.kafka.EventsTopic = topic
	f.env = append(without(f.env, "SENTINEL_KAFKA.EVENTS_TOPIC="), "SENTINEL_KAFKA.EVENTS_TOPIC="+topic)
	binary := buildIntegrationBinary(t, "detector")
	first, firstDir := observedDetector(t, binary, f.env)
	second, secondDir := observedDetector(t, binary, f.env)
	f.process = first
	require.Eventually(t, func() bool {
		group, describeErr := recoveryGroup(t, f)
		return describeErr == nil && group.Err == nil && len(group.Members) == 2
	}, 15*time.Second, 25*time.Millisecond)
	producer, err := kgo.NewClient(kgo.SeedBrokers(kafkaCfg.Brokers), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	canonical, tenth := seedTransferPartitions(t, f, producer)
	oldOwners := incorporatedOwners(t, f, []string{firstDir, secondDir})
	oldFences := make(map[int32]postgres.DetectionOwnership)
	for id := range int32(12) {
		oldFences[id] = cooldownOwner(t, f, id)
	}
	third, thirdDir := observedDetector(t, binary, f.env)
	moved := transferredTarget(t, f, thirdDir, oldOwners)
	require.Eventually(t, func() bool {
		return observedTransition(t, []string{firstDir, secondDir}, "revoked", moved.Partition,
			oldOwners[moved.Partition], 0) &&
			observedTransition(t, []string{thirdDir}, "committed", moved.Partition, moved.Member, moved.Token)
	}, 20*time.Second, 25*time.Millisecond)
	f.process = third
	require.NoError(t, producer.ProduceSync(t.Context(), tenth[moved.Partition]).FirstErr())
	recoveryCheckpoint(t, f, moved.Partition, 10)
	requireRecoveredAlert(t, f, canonical[moved.Partition])
	current := cooldownOwner(t, f, moved.Partition)
	require.Equal(t, moved.Member, current.Member)
	require.NotEqual(t, oldFences[moved.Partition].Member, current.Member)
	assertCooldownStaleOwners(t, f, current, oldFences[moved.Partition])
	for _, process := range []*proc{third, second, first} {
		require.Zero(t, process.terminate(t), process.output.String())
	}
}

func assertCooldownStaleOwners(t *testing.T, f *cooldownFixture, current, old postgres.DetectionOwnership) {
	t.Helper()
	t.Run("concurrent stale owners on moved target", func(t *testing.T) {
		store := cooldownStore(f)
		candidate := cooldownCandidate(t, f.rules, current.Source, 0, f.start.Add(2*time.Second),
			"demo", f.ip, f.principal+"-missing", "AUTH_BRUTE_FORCE")
		before := cooldownRows(t, f)
		var wg sync.WaitGroup
		errorsFound := make(chan error, 10)
		for _, change := range []func(*postgres.DetectionOwnership){
			func(v *postgres.DetectionOwnership) { *v = old },
			func(v *postgres.DetectionOwnership) { v.Generation-- },
			func(v *postgres.DetectionOwnership) { v.Member = "wrong-equal-generation" },
			func(v *postgres.DetectionOwnership) { v.Token++ },
			func(v *postgres.DetectionOwnership) { v.Incarnation = uuid.NewString(); v.Generation++ },
		} {
			stale := current
			change(&stale)
			wg.Go(func() {
				// Register only delayed/invalid assignments; a token change with
				// the same member is checked by Decide, not self-registered.
				if stale.Token == current.Token || stale.Member != current.Member || stale.Generation != current.Generation {
					errorsFound <- store.Register(t.Context(), stale, detection.DecisionFrontier{None: true})
				}
				_, err := store.Decide(t.Context(), stale, candidate)
				errorsFound <- err
			})
		}
		wg.Wait()
		close(errorsFound)
		for err := range errorsFound {
			require.Error(t, err)
		}
		_, err := store.Decide(t.Context(), current, candidate)
		require.Error(t, err, "absent earlier decision cannot be recreated")
		require.Equal(t, before, cooldownRows(t, f), "rejections must preserve all operational rows")
	})
}

func TestDetectorCooldownRestoreBehindCheckpoint(t *testing.T) {
	f := newCooldownFixture(t, false)
	dlqTopic := sentineltesting.RequireUniqueTopic(t, f.kafka, "cooldown-restore-dlq")
	f.env = append(without(f.env, "SENTINEL_KAFKA.DLQ_TOPIC="), "SENTINEL_KAFKA.DLQ_TOPIC="+dlqTopic)
	f.send(t, 299)
	f.stored(t, 1, 0)
	require.Len(t, f.alerts(t), 1)
	marker := canonicalWriterEvents(1, f.start.Add(330*time.Second))
	marker[0].EventType, marker[0].SourceIP = "auth.success", &f.ip
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), marker, uuid.NewString()))
	f.offset++
	f.waitSource(t)
	stopInitialDetector(t, f)
	before, err := f.admin.FetchOffsets(t.Context(), f.kafka.DetectorGroup)
	require.NoError(t, err)
	checkpoint, ok := before.Lookup(f.kafka.EventsTopic, 0)
	require.True(t, ok)
	require.Equal(t, int64(11), checkpoint.At, "marker seals first window and advances pin beyond lost seed")
	cp := detection.Checkpoint{Offset: checkpoint.At, Metadata: checkpoint.Metadata}
	m, err := detection.CheckpointMetadata(cp)
	require.NoError(t, err)
	require.False(t, m.DecisionFrontier.None)
	require.Equal(t, int64(9), m.DecisionFrontier.Offset, "processed_next alone is not proof of a decision")
	// Simulate restoring ONLY this fixture's operational PG rows. The Kafka
	// checkpoint, immutable detection and workflow intentionally remain newer.
	tx, err := f.db.Pool.Begin(t.Context())
	require.NoError(t, err)
	for _, query := range []string{
		"DELETE FROM detection_decisions WHERE group_id=$1",
		"DELETE FROM detection_cooldown_cursors WHERE group_id=$1",
		`UPDATE detection_partition_fences SET frontier_offset=-1,frontier_ordinal=-1,
		 drained=false,drain_checkpoint=NULL WHERE group_id=$1`,
	} {
		_, err = tx.Exec(t.Context(), query, f.kafka.DetectorGroup)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(t.Context()))
	operational := cooldownRows(t, f)
	events := canonicalWriterEvents(10, f.start.Add(340*time.Second))
	for _, event := range events {
		event.EventType, event.SourceIP, event.Principal = "auth.failure", &f.ip, &f.principal
	}
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), events, uuid.NewString()))
	f.process = startProc(t, "detector", "", f.env)
	require.NotZero(t, f.process.requireExit(t, 8*time.Second), "inconsistent restore must fail terminally")
	client := &http.Client{Timeout: time.Second}
	response, statusErr := client.Get("http://127.0.0.1:" + opsPort(t, f.env) + "/readyz")
	if statusErr == nil {
		defer response.Body.Close()
		require.NotEqual(t, http.StatusOK, response.StatusCode)
	}
	after, err := f.admin.FetchOffsets(t.Context(), f.kafka.DetectorGroup)
	require.NoError(t, err)
	retained, ok := after.Lookup(f.kafka.EventsTopic, 0)
	require.True(t, ok)
	require.Equal(t, checkpoint, retained)
	require.Equal(t, operational, cooldownRows(t, f))
	require.Empty(t, f.alerts(t), "lost eligibility seed must never create a new allow/publication")
	ends, err := f.admin.ListEndOffsets(t.Context(), dlqTopic)
	require.NoError(t, err)
	dlq, ok := ends.Lookup(dlqTopic, 0)
	require.True(t, ok)
	require.NoError(t, dlq.Err)
	require.Zero(t, dlq.Offset, "operational recovery failure cannot quarantine valid source records")
}
