//go:build integration

package test_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

func TestDetectorConfigRestart(t *testing.T) {
	f := newCooldownFixture(t, false)
	f.start = f.start.Truncate(10 * time.Minute)
	f.send(t, 299)
	original := f.alerts(t)
	require.Len(t, original, 1)
	require.Zero(t, f.process.terminate(t))
	f.process = startProc(t, "detector", "", f.env)
	replayed := f.alerts(t)
	if len(replayed) == 0 {
		replayed = f.alerts(t)
	}
	require.Len(t, replayed, 1)
	require.Equal(t, original[0].Value, replayed[0].Value)
	f.waitSource(t)
	require.Zero(t, f.process.terminate(t))
	raw, err := os.ReadFile("../../config/rules.yaml")
	require.NoError(t, err)
	changed := strings.Replace(string(raw), "    version: 1", "    version: 2", 1)
	changed = strings.Replace(changed, "window: 300s", "window: 600s", 1)
	path := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
	env := append(without(f.env, "SENTINEL_DETECTOR.RULES_PATH="), "SENTINEL_DETECTOR.RULES_PATH="+path,
		"SENTINEL_DETECTOR.ALLOW_CONFIG_TRANSITION=true")
	active := startProc(t, "detector", "", env)
	require.NotZero(t, active.requireExit(t, 10*time.Second), "increased version cannot adopt active old windows")
	f.process = startProc(t, "detector", "", f.env)
	marker := canonicalWriterEvents(1, f.start.Add(330*time.Second))
	marker[0].EventType, marker[0].SourceIP = "auth.success", &f.ip
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), marker, uuid.NewString()))
	f.offset++
	f.waitSource(t)
	before := brokerCheckpoint(t, f.admin, f.kafka)
	var old detection.Metadata
	require.NoError(t, json.Unmarshal([]byte(before.Metadata), &old))
	require.True(t, old.Drained)
	require.Equal(t, old.ProcessedNext, before.At)
	require.Zero(t, f.process.terminate(t))
	drainReplay := f.alerts(t)
	require.Len(t, drainReplay, 1, "old-config drain restart replays its retained active window")
	require.Equal(t, original[0].Value, drainReplay[0].Value)
	// Equal-version drift fails even when the source is drained.
	drift := strings.Replace(changed, "    version: 2", "    version: 1", 1)
	require.NoError(t, os.WriteFile(path, []byte(drift), 0o600))
	bad := startProc(t, "detector", "", env)
	require.NotZero(t, bad.requireExit(t, 10*time.Second))
	require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
	// A live old-config member, even drained, forbids a rolling handover.
	f.process = startProc(t, "detector", "", f.env)
	requireOutputEventually(t, f.process, "service started")
	oldProcess := f.process
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("old member output: %s", oldProcess.output.String())
		}
	})
	var oldMember string
	require.Eventually(t, func() bool {
		groups, describeErr := f.admin.DescribeGroups(t.Context(), f.kafka.DetectorGroup)
		group, present := groups[f.kafka.DetectorGroup]
		if describeErr != nil || !present || group.Err != nil || group.State != "Stable" || len(group.Members) != 1 {
			return false
		}
		oldMember = group.Members[0].MemberID
		return true
	}, 30*time.Second, 100*time.Millisecond, "old replica must join before the mixed-member probe")
	peerEnv := append(without(f.env, "SENTINEL_OPS.PORT="),
		"SENTINEL_OPS.PORT="+opsPort(t, serviceEnv(t, "detector")))
	peer := startProc(t, "detector", "", peerEnv)
	require.Eventually(t, func() bool {
		groups, describeErr := f.admin.DescribeGroups(t.Context(), f.kafka.DetectorGroup)
		group, present := groups[f.kafka.DetectorGroup]
		return describeErr == nil && present && group.Err == nil && group.State == "Stable" && len(group.Members) == 2
	}, 30*time.Second, 100*time.Millisecond, "unchanged idle replicas must coexist")
	f.process.requireRunningFor(t, time.Second)
	peer.requireRunningFor(t, time.Second)
	require.Zero(t, peer.terminate(t), peer.output.String())
	require.Eventually(t, func() bool {
		groups, describeErr := f.admin.DescribeGroups(t.Context(), f.kafka.DetectorGroup)
		group, present := groups[f.kafka.DetectorGroup]
		if describeErr != nil || !present || group.Err != nil || group.State != "Stable" || len(group.Members) != 1 {
			return false
		}
		oldMember = group.Members[0].MemberID
		return true
	}, 30*time.Second, 100*time.Millisecond)
	mixedEnv := append(without(env, "SENTINEL_OPS.PORT="),
		"SENTINEL_OPS.PORT="+opsPort(t, serviceEnv(t, "detector")))
	mixed := startProc(t, "detector", "", mixedEnv)
	require.NotZero(t, mixed.requireExit(t, 30*time.Second))
	require.Contains(t, mixed.output.String(), "exactly one detector member")
	groups, err := f.admin.DescribeGroups(t.Context(), f.kafka.DetectorGroup)
	require.NoError(t, err)
	require.Len(t, groups[f.kafka.DetectorGroup].Members, 1, "rejected transition must not join/evict the old member")
	require.Equal(t, oldMember, groups[f.kafka.DetectorGroup].Members[0].MemberID)
	require.Zero(t, f.process.terminate(t))
	f.process = startProc(t, "detector", "", env)
	require.Eventually(t, func() bool {
		cp := brokerCheckpoint(t, f.admin, f.kafka)
		var m detection.Metadata
		return json.Unmarshal([]byte(cp.Metadata), &m) == nil && m.RuleVersions["AUTH_BRUTE_FORCE"] == 2 &&
			m.RuleEffectiveFrom["AUTH_BRUTE_FORCE"] == f.start.Add(600*time.Second).UnixMilli()
	}, 12*time.Second, 100*time.Millisecond,
		"real drained restart must publish authorized gates: %s", f.process.output.String())
	var fingerprint string
	var drained bool
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT config_fingerprint,drained FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2 AND partition_id=0`, f.kafka.DetectorGroup, f.kafka.EventsTopic).
		Scan(&fingerprint, &drained))
	var current detection.Metadata
	require.NoError(t, json.Unmarshal([]byte(brokerCheckpoint(t, f.admin, f.kafka).Metadata), &current))
	require.Equal(t, current.ConfigFingerprint, fingerprint)
	require.True(t, drained)
	require.Len(t, current.RuleEffectiveFrom, 4)
	f.stored(t, 1, 0)
	require.Empty(t, f.alerts(t), "handover cannot replay historical alerts")
	// At 400s the new 600s window starts before its gate, despite being newer
	// than the old watermark. The deliberate transition gap creates no alert.
	producer := newWriterProducer(t, f.kafka)
	var versionTwo []byte
	for _, seconds := range []int{400, 600} {
		events := canonicalWriterEvents(10, f.start.Add(time.Duration(seconds)*time.Second))
		for _, event := range events {
			event.EventType, event.SourceIP, event.Principal = "auth.failure", &f.ip, &f.principal
		}
		require.NoError(t, producer.PublishBatch(t.Context(), events, uuid.NewString()))
		f.offset += int64(len(events))
		f.waitSource(t)
		alerts := f.alerts(t)
		if seconds == 400 {
			require.Empty(t, alerts)
			continue
		}
		require.Len(t, alerts, 1)
		var alert struct {
			RuleVersion int    `json:"rule_version"`
			WindowStart string `json:"window_start"`
		}
		require.NoError(t, json.Unmarshal(alerts[0].Value, &alert))
		require.Equal(t, 2, alert.RuleVersion)
		require.Equal(t, f.start.Add(600*time.Second).Format("2006-01-02T15:04:05.000Z"), alert.WindowStart)
		versionTwo = alerts[0].Value
	}
	require.Zero(t, f.process.terminate(t))
	f.process = startProc(t, "detector", "", env)
	// Observe actual restoration and stored-byte replay before terminating.
	newReplay := f.alerts(t)
	if len(newReplay) == 0 {
		newReplay = f.alerts(t)
	}
	require.Len(t, newReplay, 1)
	require.Equal(t, versionTwo, newReplay[0].Value)
	f.waitSource(t)
	require.Zero(t, f.process.terminate(t), f.process.output.String())
}

func TestConfigLedgerHandover(t *testing.T) {
	// Migrations are additive and applying the runner twice preserves the ledger.
	require.NoError(t, database.Migrate(t.Context(), pgCfg, zerolog.Nop()))
	require.NoError(t, database.Migrate(t.Context(), pgCfg, zerolog.Nop()))
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	_, kafka := detectorPGEnv(t, pgCfg)
	raw, err := os.ReadFile("../../config/rules.yaml")
	require.NoError(t, err)
	rules, err := detection.LoadRules(strings.NewReader(string(raw)))
	require.NoError(t, err)
	budgets := config.DetectorPGConfig{AcquireTimeout: time.Second, TransactionTimeout: 3 * time.Second,
		LockTimeout: time.Second, StatementTimeout: 2 * time.Second, CleanupTimeout: time.Second}
	store := postgres.NewDetectionDecisionStore(db.Pool, budgets, rules)
	o := postgres.DetectionOwnership{Group: kafka.DetectorGroup,
		Incarnation: postgres.DetectionGroupIncarnation(kafka.DetectorGroup), Member: "old", Generation: 1,
		Token: 1, Source: detection.TopicPartition{Topic: kafka.EventsTopic, Partition: 0}, Config: rules.Fingerprint()}
	_, err = store.PrepareRestore(t.Context(), o, nil, false)
	require.NoError(t, err)
	state := detection.NewPartitionState(rules)
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	var candidates []detection.Candidate
	offset := int64(0)
	for _, seconds := range []int{299, 301} {
		for _, event := range canonicalWriterEvents(10, start.Add(time.Duration(seconds)*time.Second)) {
			ip, principal := "192.0.2.8", "ledger-handover"
			event.EventType, event.SourceIP, event.Principal = "auth.failure", &ip, &principal
			out, applyErr := state.ApplySource(o.Source, offset, event)
			require.NoError(t, applyErr)
			for _, candidate := range out.Candidates {
				_, decideErr := store.Decide(t.Context(), o, candidate)
				require.NoError(t, decideErr)
				candidates = append(candidates, candidate)
				require.NoError(t, state.EffectDecision(candidate))
			}
			require.NoError(t, state.Acknowledge(offset))
			offset++
		}
	}
	require.Len(t, candidates, 2)
	// Acknowledge neither publication nor checkpoint for a pending candidate.
	pending, pendingErr := state.Checkpoint()
	require.NoError(t, pendingErr)
	next := &detection.Rules{SchemaVersion: 1, Rules: slices.Clone(rules.Rules)}
	next.Rules[0].Version++
	next.Rules[0].Window = 600 * time.Second
	newStore := postgres.NewDetectionDecisionStore(db.Pool, budgets, next)
	newOwner := o
	newOwner.Member, newOwner.Generation, newOwner.Config = "new", 2, next.Fingerprint()
	_, err = newStore.PrepareRestore(t.Context(), newOwner, &pending, true)
	require.Error(t, err, "active checkpoint cannot authorize config changes")
	marker := canonicalWriterEvents(1, start.Add(630*time.Second))[0]
	marker.EventType = "auth.success"
	_, err = state.ApplySource(o.Source, offset, marker)
	require.NoError(t, err)
	require.NoError(t, state.Acknowledge(offset))
	cp, err := state.Checkpoint()
	require.NoError(t, err)
	_, err = newStore.PrepareRestore(t.Context(), newOwner, &cp, true)
	require.Error(t, err, "a caller-supplied drained flag cannot replace acknowledged PG proof")
	admin := checkpointAdmin(t)
	// Commit the actual source prefix before recording the PG drain certificate.
	// The source exists independently from this store-level transaction fixture.
	producer := newWriterProducer(t, kafka)
	require.NoError(t, producer.PublishBatch(t.Context(), canonicalWriterEvents(int(cp.Offset), start), uuid.NewString()))
	seedBrokerCheckpoint(t, admin, kafka, cp)
	require.NoError(t, store.CheckpointCommitted(t.Context(), o, cp))
	_, err = newStore.PrepareRestore(t.Context(), newOwner, &cp, false)
	require.Error(t, err, "drain still requires stopped-old-owner authorization")
	// Equal-version drift is checked against actual PG-saved definitions.
	drift := &detection.Rules{SchemaVersion: 1, Rules: slices.Clone(next.Rules)}
	drift.Rules[1].Threshold++
	driftStore := postgres.NewDetectionDecisionStore(db.Pool, budgets, drift)
	driftOwner := newOwner
	driftOwner.Config = drift.Fingerprint()
	_, err = driftStore.PrepareRestore(t.Context(), driftOwner, &cp, true)
	require.Error(t, err, "unchanged rule version cannot conceal changed settings")
	adopted, err := newStore.PrepareRestore(t.Context(), newOwner, &cp, true)
	require.NoError(t, err)
	require.NotNil(t, adopted)
	require.NoError(t, detection.NewPartitionState(next).Restore(*adopted))
	// Simulate PG commit followed by failure before the new Kafka checkpoint.
	newOwner.Generation++
	recovered, err := newStore.PrepareRestore(t.Context(), newOwner, &cp, true)
	require.NoError(t, err)
	require.Equal(t, adopted, recovered, "crash recovery must reuse precisely the authorized gates")
	seedBrokerCheckpoint(t, admin, kafka, *adopted)
	require.NoError(t, newStore.CheckpointCommitted(t.Context(), newOwner, *adopted))
	_, err = newStore.PrepareRestore(t.Context(), newOwner, adopted, false)
	require.NoError(t, err)
	var m detection.Metadata
	require.NoError(t, json.Unmarshal([]byte(adopted.Metadata), &m))
	m.RuleEffectiveFrom["AUTH_BRUTE_FORCE"] = 0
	bad, err := json.Marshal(m)
	require.NoError(t, err)
	tampered := &detection.Checkpoint{Offset: adopted.Offset, Metadata: string(bad)}
	_, err = newStore.PrepareRestore(t.Context(), newOwner, tampered, false)
	require.Error(t, err, "matching fingerprint does not authorize dropping persisted gates")
	for i, candidate := range candidates {
		_, err = store.Decide(t.Context(), o, candidate)
		require.Error(t, err, "old ownership cannot create/replay decisions after handover")
		var allowed bool
		var canonical []byte
		require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT allowed,canonical_payload FROM detection_decisions
			WHERE topic=$1 AND source_offset=$2`, o.Source.Topic, candidate.Offset()).Scan(&allowed, &canonical))
		require.Equal(t, i == 0, allowed)
		require.Equal(t, candidate.CanonicalBytes(), canonical)
	}
	var cursors int
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM detection_cooldown_cursors
		WHERE group_id=$1 AND config_fingerprint=$2`, o.Group, o.Config).Scan(&cursors))
	require.Equal(t, 1, cursors, "old cursor is retained")
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM detection_cooldown_cursors
		WHERE group_id=$1 AND config_fingerprint=$2`, o.Group, newOwner.Config).Scan(&cursors))
	require.Zero(t, cursors, "new cursor namespace starts empty only after verified handover")
}
