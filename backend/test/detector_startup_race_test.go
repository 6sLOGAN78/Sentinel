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

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

const (
	startupRaceReplicas   = 3
	startupRacePartitions = 6
	startupRaceIterations = 20
	startupRaceSettle     = 45 * time.Second
	startupRaceQuiet      = 3 * time.Second
	startupRacePoll       = 50 * time.Millisecond
)

// startupRaceEnv lets a diagnosis run widen the sample without editing code.
const startupRaceIterationsEnv = "SENTINEL_TEST_STARTUP_RACE_ITERATIONS"

type startupRaceMember struct {
	process *proc
	dir     string
}

type startupRaceDeps struct {
	binary string
	admin  *kadm.Client
	db     *database.Database
}

// TestDetectorConcurrentStartup reproduces GAP-3 (bench run 20261119a): three
// detector replicas start together on a fresh group and topic. No replica may
// exit, and every partition must end up restored by exactly one current owner
// whose PostgreSQL fence names that owner.
func TestDetectorConcurrentStartup(t *testing.T) {
	client, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafkaCfg.Brokers, ",")...))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	deps := startupRaceDeps{binary: buildIntegrationBinary(t, "detector"), admin: kadm.NewClient(client), db: db}
	iterations := startupRaceIterations
	if raw := os.Getenv(startupRaceIterationsEnv); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		require.NoError(t, parseErr)
		require.Positive(t, parsed)
		iterations = parsed
	}
	// Run every iteration so a diagnosis sees each fatal class and its frequency.
	failures := make(map[string]int)
	first := 0
	for i := range iterations {
		for _, class := range runStartupRaceIteration(t, deps, i+1) {
			failures[class]++
			if first == 0 {
				first = i + 1
			}
		}
	}
	if len(failures) > 0 {
		t.Fatalf("concurrent detector startup fatal reproduced (first at iteration %d of %d); fatal classes: %v",
			first, iterations, failures)
	}
	t.Logf("no startup fatal in %d iterations of %d concurrent replicas", iterations, startupRaceReplicas)
}

// runStartupRaceIteration returns the worker-failure text of every replica
// that exited; empty means the iteration settled without a fatal.
func runStartupRaceIteration(t *testing.T, deps startupRaceDeps, iteration int) []string {
	t.Helper()
	env, kafka := detectorPGEnv(t, pgCfg)
	topic := "detector-startup-" + uuid.NewString()
	response, err := deps.admin.CreateTopics(t.Context(), startupRacePartitions, 1, nil, topic)
	require.NoError(t, err)
	require.NoError(t, response[topic].Err)
	kafka.EventsTopic = topic
	env = append(without(env, "SENTINEL_KAFKA.EVENTS_TOPIC="), "SENTINEL_KAFKA.EVENTS_TOPIC="+topic)
	f := &cooldownFixture{kafka: kafka, admin: deps.admin, db: deps.db, env: env}

	members := make([]startupRaceMember, 0, startupRaceReplicas)
	for range startupRaceReplicas {
		process, dir := observedDetector(t, deps.binary, env)
		members = append(members, startupRaceMember{process: process, dir: dir})
	}
	settled := waitStartupRaceSettled(t, f, members)
	if failures := startupRaceFailures(t, deps, f, iteration, members); len(failures) > 0 {
		return failures
	}
	require.True(t, settled, "iteration %d: partitions never settled to one restored owner each", iteration)
	time.Sleep(startupRaceQuiet)
	if failures := startupRaceFailures(t, deps, f, iteration, members); len(failures) > 0 {
		return failures
	}
	requireStartupFences(t, f)
	t.Logf("iteration %d: %d partitions owned exactly once by %d replicas", iteration,
		startupRacePartitions, startupRaceReplicas)
	terminateStartupMembers(t, members)
	return nil
}

// startupRaceFailures logs and returns the failure of every exited replica.
// It then records the durable state the failure left behind and restarts the
// exited replicas, as Docker's restart policy did in the bench, to show whether
// that state heals. The survivors are stopped so the next iteration is fresh.
func startupRaceFailures(t *testing.T, deps startupRaceDeps, f *cooldownFixture, iteration int,
	members []startupRaceMember) []string {
	t.Helper()
	exited := exitedStartupMembers(members)
	if len(exited) == 0 {
		return nil
	}
	failures := make([]string, 0, len(exited))
	live := make([]startupRaceMember, 0, len(members))
	for i, member := range members {
		if !containsIndex(exited, i) {
			live = append(live, member)
			continue
		}
		code := member.process.requireExit(t, time.Second)
		lines := startupFailureLines(member.process.output.String())
		t.Logf("iteration %d replica %d exited %d; classified failure:\n%s", iteration, i, code, lines)
		failures = append(failures, workerFailure(lines))
	}
	logStartupRaceState(t, f, iteration)
	for range exited {
		process, dir := observedDetector(t, deps.binary, f.env)
		live = append(live, startupRaceMember{process: process, dir: dir})
	}
	healed := waitStartupRaceSettled(t, f, live) && len(exitedStartupMembers(live)) == 0
	t.Logf("iteration %d: after restarting %d replica(s) the group healed=%t", iteration, len(exited), healed)
	if !healed {
		for _, index := range exitedStartupMembers(live) {
			t.Logf("iteration %d restart failure: %s", iteration,
				workerFailure(startupFailureLines(live[index].process.output.String())))
		}
		logStartupRaceState(t, f, iteration)
	}
	terminateStartupMembers(t, live)
	return failures
}

func containsIndex(indexes []int, want int) bool {
	for _, index := range indexes {
		if index == want {
			return true
		}
	}
	return false
}

// logStartupRaceState prints, per partition, whether Kafka holds a committed
// checkpoint and what the PostgreSQL fence records. A bound manifest with no
// Kafka checkpoint is the state "existing detector group lost its Kafka
// checkpoint" rejects.
func logStartupRaceState(t *testing.T, f *cooldownFixture, iteration int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	offsets, offsetErr := f.admin.FetchOffsets(ctx, f.kafka.DetectorGroup)
	rows, err := f.db.Pool.Query(ctx, `SELECT partition_id,member_id,generation,assignment_token,
		rules_manifest IS NOT NULL,frontier_offset FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2 ORDER BY partition_id`, f.kafka.DetectorGroup, f.kafka.EventsTopic)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var partition, generation int32
		var member string
		var token, frontier int64
		var manifest bool
		require.NoError(t, rows.Scan(&partition, &member, &generation, &token, &manifest, &frontier))
		committed := "none"
		if o, ok := offsets.Lookup(f.kafka.EventsTopic, partition); offsetErr == nil && ok && o.Err == nil && o.At >= 0 {
			committed = strconv.FormatInt(o.At, 10)
		}
		t.Logf("iteration %d state partition=%d kafka_checkpoint=%s fence_member=%s generation=%d token=%d "+
			"manifest=%t frontier=%d", iteration, partition, committed, member, generation, token, manifest, frontier)
	}
	require.NoError(t, rows.Err())
}

// workerFailure extracts the error text of the "background worker failed" line.
func workerFailure(lines string) string {
	for _, line := range strings.Split(lines, "\n") {
		var entry struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Message == "background worker failed" {
			return entry.Error
		}
	}
	return "exit without worker failure line"
}

// waitStartupRaceSettled returns true once the stable 3-member group assigns
// each partition exactly once and that owner observed a completed restore at
// the current generation. It returns false early when any replica exits.
func waitStartupRaceSettled(t *testing.T, f *cooldownFixture, members []startupRaceMember) bool {
	t.Helper()
	deadline := time.Now().Add(startupRaceSettle)
	for time.Now().Before(deadline) {
		if len(exitedStartupMembers(members)) > 0 {
			return false
		}
		if startupRaceOwnedOnce(t, f, members) {
			return true
		}
		time.Sleep(startupRacePoll)
	}
	return false
}

func startupRaceOwnedOnce(t *testing.T, f *cooldownFixture, members []startupRaceMember) bool {
	t.Helper()
	group, err := recoveryGroup(t, f)
	if err != nil || group.Err != nil || group.State != "Stable" || len(group.Members) != startupRaceReplicas {
		return false
	}
	restored := startupRaceRestored(t, f.kafka.EventsTopic, members)
	for partition := range int32(startupRacePartitions) {
		owners := 0
		for _, member := range group.Members {
			if groupOwns(group, member.MemberID, f.kafka.EventsTopic, partition) {
				owners++
				if !restored[partition][member.MemberID] {
					return false
				}
			}
		}
		if owners != 1 {
			return false
		}
	}
	return true
}

// startupRaceRestored maps partition -> members that completed a restore.
func startupRaceRestored(t *testing.T, topic string, members []startupRaceMember) map[int32]map[string]bool {
	t.Helper()
	restored := make(map[int32]map[string]bool)
	for _, member := range members {
		for _, o := range detectorObservations(t, member.dir) {
			if o.Boundary != "restored" || o.Topic != topic {
				continue
			}
			if restored[o.Partition] == nil {
				restored[o.Partition] = make(map[string]bool)
			}
			restored[o.Partition][o.Member] = true
		}
	}
	return restored
}

// requireStartupFences proves the durable side agrees with Kafka: one fence
// per partition, each naming the member Kafka currently assigns it to.
func requireStartupFences(t *testing.T, f *cooldownFixture) {
	t.Helper()
	group, err := recoveryGroup(t, f)
	require.NoError(t, err)
	rows, err := f.db.Pool.Query(t.Context(), `SELECT partition_id,member_id FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2 ORDER BY partition_id`, f.kafka.DetectorGroup, f.kafka.EventsTopic)
	require.NoError(t, err)
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var partition int32
		var member string
		require.NoError(t, rows.Scan(&partition, &member))
		require.True(t, groupOwns(group, member, f.kafka.EventsTopic, partition),
			"fence for partition %d names %s, not its current Kafka owner", partition, member)
		seen++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, startupRacePartitions, seen)
}

func exitedStartupMembers(members []startupRaceMember) []int {
	var exited []int
	for i, member := range members {
		select {
		case err := <-member.process.done:
			member.process.done <- err
			exited = append(exited, i)
		default:
		}
	}
	return exited
}

func terminateStartupMembers(t *testing.T, members []startupRaceMember) {
	t.Helper()
	for i := len(members) - 1; i >= 0; i-- {
		select {
		case err := <-members[i].process.done:
			members[i].process.done <- err
		default:
			require.Zero(t, members[i].process.terminate(t), members[i].process.output.String())
		}
	}
}

// startupFailureLines keeps only error/warn log lines, which name the class.
func startupFailureLines(output string) string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, `"level":"error"`) || strings.Contains(line, `"level":"warn"`) ||
			strings.Contains(line, `"level":"fatal"`) {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func restoreRetryStore(t *testing.T) (*database.Database, *postgres.DetectionDecisionStore,
	postgres.DetectionOwnership, *detection.Rules) {
	t.Helper()
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	_, kafka := detectorPGEnv(t, pgCfg)
	raw, err := os.ReadFile("../../config/rules.yaml")
	require.NoError(t, err)
	rules, err := detection.LoadRules(strings.NewReader(string(raw)))
	require.NoError(t, err)
	store := postgres.NewDetectionDecisionStore(db.Pool, config.DetectorPGConfig{AcquireTimeout: time.Second,
		TransactionTimeout: 3 * time.Second, LockTimeout: time.Second, StatementTimeout: 2 * time.Second,
		CleanupTimeout: time.Second}, rules)
	owner := postgres.DetectionOwnership{Group: kafka.DetectorGroup,
		Incarnation: postgres.DetectionGroupIncarnation(kafka.DetectorGroup), Member: "kgo-member-a", Generation: 1,
		Token: 1, Source: detection.TopicPartition{Topic: kafka.EventsTopic, Partition: 1}, Config: rules.Fingerprint()}
	return db, store, owner, rules
}

func fenceManifestBound(t *testing.T, db *database.Database, o postgres.DetectionOwnership) bool {
	t.Helper()
	var bound bool
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT rules_manifest IS NOT NULL FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition).Scan(&bound))
	return bound
}

func emptyCheckpoint(t *testing.T, rules *detection.Rules) detection.Checkpoint {
	t.Helper()
	state := detection.NewPartitionState(rules)
	require.NoError(t, state.BeginAt(0))
	cp, err := state.Checkpoint()
	require.NoError(t, err)
	return cp
}

// TestDetectorRestoreRetryWithoutAckedCheckpoint is the deterministic PG half
// of the GAP-3 interleaving. A first restore registers its fence, but its first
// Kafka checkpoint commit is never acknowledged (the startup rebalance rejected
// it). Retrying without a checkpoint must succeed, for the same member at a
// later generation and for a new owner, because the manifest is bound only by
// CheckpointCommitted. Once a checkpoint was acknowledged, losing it still
// fails closed.
func TestDetectorRestoreRetryWithoutAckedCheckpoint(t *testing.T) {
	db, store, first, rules := restoreRetryStore(t)
	_, err := store.PrepareRestore(t.Context(), first, nil, false)
	require.NoError(t, err, "fresh group first restore")
	require.False(t, fenceManifestBound(t, db, first), "restore alone must not bind the manifest")
	// No Kafka commit and no CheckpointCommitted: the restore commit failed.
	retry := first
	retry.Generation = 3 // cooperative rebalance retained the partition with the same token
	_, err = store.PrepareRestore(t.Context(), retry, nil, false)
	require.NoError(t, err, "same member retries after an unacknowledged first checkpoint")
	moved := first
	moved.Member, moved.Generation, moved.Token = "kgo-member-b", 4, 1
	_, err = store.PrepareRestore(t.Context(), moved, nil, false)
	require.NoError(t, err, "a new owner restores the never-acknowledged partition")
	cp := emptyCheckpoint(t, rules)
	require.NoError(t, store.CheckpointCommitted(t.Context(), moved, cp))
	require.True(t, fenceManifestBound(t, db, moved), "acknowledged checkpoint binds the manifest")
	_, err = store.PrepareRestore(t.Context(), moved, &cp, false)
	require.NoError(t, err, "restore from the acknowledged checkpoint")
	lost := moved
	lost.Generation = 5
	_, err = store.PrepareRestore(t.Context(), lost, nil, false)
	require.ErrorIs(t, err, postgres.ErrDecisionInvariant, "real checkpoint loss stays fail-closed")
	require.ErrorContains(t, err, "existing detector group lost its Kafka checkpoint")
}

// TestDetectorManifestUpgradePath covers fences written by the previous code,
// which bound the manifest during restore. A group whose checkpoint Kafka had
// acknowledged restores unchanged and keeps its manifest. A legacy partition
// left with a bound manifest and no Kafka checkpoint cannot be told apart from
// real checkpoint loss, so it still fails closed (operator escalation, ADR 0012).
func TestDetectorManifestUpgradePath(t *testing.T) {
	db, store, owner, rules := restoreRetryStore(t)
	legacyBind := func(o postgres.DetectionOwnership) {
		t.Helper()
		raw, err := json.Marshal(rules)
		require.NoError(t, err)
		_, err = db.Pool.Exec(t.Context(), `UPDATE detection_partition_fences SET rules_manifest=$4
			WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, o.Group, o.Source.Topic, o.Source.Partition, raw)
		require.NoError(t, err)
	}
	_, err := store.PrepareRestore(t.Context(), owner, nil, false)
	require.NoError(t, err)
	legacyBind(owner)
	cp := emptyCheckpoint(t, rules)
	require.NoError(t, store.CheckpointCommitted(t.Context(), owner, cp))
	var before []byte
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT rules_manifest FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, owner.Group, owner.Source.Topic, owner.Source.Partition).
		Scan(&before))
	upgraded := owner
	upgraded.Member, upgraded.Generation = "kgo-upgraded", 2
	_, err = store.PrepareRestore(t.Context(), upgraded, &cp, false)
	require.NoError(t, err, "a legacy-bound group with an acknowledged checkpoint restores unchanged")
	require.NoError(t, store.CheckpointCommitted(t.Context(), upgraded, cp))
	var after []byte
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT rules_manifest FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2 AND partition_id=$3`, owner.Group, owner.Source.Topic, owner.Source.Partition).
		Scan(&after))
	require.JSONEq(t, string(before), string(after), "an existing manifest is validated, never replaced")

	wedged := owner
	wedged.Source.Partition = 2
	_, err = store.PrepareRestore(t.Context(), wedged, nil, false)
	require.NoError(t, err)
	legacyBind(wedged) // old code bound it, then the first Kafka commit failed
	wedged.Generation = 3
	_, err = store.PrepareRestore(t.Context(), wedged, nil, false)
	require.ErrorIs(t, err, postgres.ErrDecisionInvariant, "legacy wedged state stays fail-closed")
	require.ErrorContains(t, err, "existing detector group lost its Kafka checkpoint")
}
