//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

func TestDetectorThirdReplicaRebalance(t *testing.T) {
	f := newCooldownFixture(t, false)
	stopInitialDetector(t, f)
	topic := "detector-transfer-" + uuid.NewString()
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
	third, thirdDir := observedDetector(t, binary, f.env)
	moved := transferredTarget(t, f, thirdDir, oldOwners)
	require.Eventually(t, func() bool {
		return observedTransition(t, []string{firstDir, secondDir}, "revoked", moved.Partition,
			oldOwners[moved.Partition], 0) &&
			observedTransition(t, []string{thirdDir}, "committed", moved.Partition, moved.Member, moved.Token)
	}, 20*time.Second, 25*time.Millisecond, "target must be revoked and its nine contributors replayed before event ten")
	require.NotEqual(t, oldOwners[moved.Partition], moved.Member)
	f.process = third
	before := recoveryCheckpoint(t, f, moved.Partition, 9)
	require.Equal(t, before, *moved.Checkpoint)
	require.NoError(t, producer.ProduceSync(t.Context(), tenth[moved.Partition]).FirstErr())
	recoveryCheckpoint(t, f, moved.Partition, 10)
	requireRecoveredAlert(t, f, canonical[moved.Partition])
	t.Logf("target partition=%d oldOwner=%s newOwner=%s restored=%+v", moved.Partition,
		oldOwners[moved.Partition], moved.Member, *moved.Checkpoint)
	for _, process := range []*proc{third, second, first} {
		require.Zero(t, process.terminate(t), process.output.String())
	}
}

func TestDetectorCooperativeAcquisition(t *testing.T) {
	f := newCooldownFixture(t, false)
	stopInitialDetector(t, f)
	topic := "detector-acquisition-" + uuid.NewString()
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
	retained := make(map[int32]detectorObservation)
	for _, observation := range detectorObservations(t, firstDir) {
		if observation.Boundary == "committed" && observationAtNine(observation) &&
			oldOwners[observation.Partition] == observation.Member {
			retained[observation.Partition] = observation
		}
	}
	require.NotEmpty(t, retained, "survivor must retain incorporated state while acquiring additions")
	require.Less(t, len(retained), 12)
	require.Zero(t, second.terminate(t), second.output.String())
	moved := acquiredTarget(t, f, firstDir, oldOwners, retained)
	requireStatusEventually(t, "http://127.0.0.1:"+opsPort(t, first.cmd.Env)+"/readyz", http.StatusOK)
	require.NotEqual(t, oldOwners[moved.Partition], moved.Member)
	before := recoveryCheckpoint(t, f, moved.Partition, 9)
	require.Equal(t, before, *moved.Checkpoint)
	require.NoError(t, producer.ProduceSync(t.Context(), tenth[moved.Partition]).FirstErr())
	recoveryCheckpoint(t, f, moved.Partition, 10)
	requireRecoveredAlert(t, f, canonical[moved.Partition])
	for partition, previous := range retained {
		retainedBefore := recoveryCheckpoint(t, f, partition, 9)
		require.Equal(t, *previous.Checkpoint, retainedBefore)
		require.NoError(t, producer.ProduceSync(t.Context(), tenth[partition]).FirstErr())
		recoveryCheckpoint(t, f, partition, 10)
		requireRetainedAcquisitionAlert(t, f, canonical[partition])
		break // One actual retained live window plus the acquired live window.
	}
	t.Logf("cooperative 2->1 target=%d oldOwner=%s survivor=%s generation=%d retained=%d restored=%+v",
		moved.Partition, oldOwners[moved.Partition], moved.Member, moved.Generation, len(retained), *moved.Checkpoint)
	require.Zero(t, first.terminate(t), first.output.String())
}

func requireRetainedAcquisitionAlert(t *testing.T, f *cooldownFixture, canonical []byte) {
	t.Helper()
	var expected contracts.Alert
	require.NoError(t, json.Unmarshal(canonical, &expected))
	records := f.alerts(t)
	require.NotEmpty(t, records, "retained window must detect event ten, not restart its count")
	for _, record := range records {
		require.Equal(t, "demo|"+expected.AlertID, string(record.Key))
		require.Equal(t, canonical, record.Value)
	}
	require.Eventually(t, func() bool {
		var count int
		return f.db.Pool.QueryRow(t.Context(), "SELECT count(*) FROM alert_workflows WHERE alert_id=$1", expected.AlertID).
			Scan(&count) == nil && count == 1
	}, 10*time.Second, 25*time.Millisecond)
	store := storageclickhouse.NewAlertStore(f.ch)
	count, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(2), count, "one immutable acquired identity and one immutable retained identity")
	stored, err := store.Get(t.Context(), "demo", expected.AlertID)
	require.NoError(t, err)
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	require.Equal(t, canonical, raw)
	var decision []byte
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT canonical_payload FROM detection_decisions WHERE alert_id=$1", expected.AlertID).Scan(&decision))
	require.Equal(t, canonical, decision)
}

func acquiredTarget(t *testing.T, f *cooldownFixture, dir string,
	oldOwners map[int32]string, retained map[int32]detectorObservation) detectorObservation {
	t.Helper()
	var moved detectorObservation
	require.Eventually(t, func() bool {
		group, err := recoveryGroup(t, f)
		if err != nil || group.Err != nil || len(group.Members) != 1 {
			return false
		}
		observations := detectorObservations(t, dir)
		for _, observation := range observations {
			if observation.Boundary == "restored" && observationAtNine(observation) &&
				observation.Member != oldOwners[observation.Partition] && oldOwners[observation.Partition] != "" &&
				groupOwns(group, observation.Member, f.kafka.EventsTopic, observation.Partition) &&
				observedTransition(t, []string{dir}, "committed", observation.Partition, observation.Member, observation.Token) &&
				retainedNineCurrent(t, f, observations, retained, observation.Generation) {
				moved = observation
				return true
			}
		}
		return false
	}, 25*time.Second, 25*time.Millisecond,
		"survivor failed to retain completed partitions and restore acquired additions")
	return moved
}

func retainedNineCurrent(t *testing.T, f *cooldownFixture, observations []detectorObservation,
	retained map[int32]detectorObservation, generation int32) bool {
	t.Helper()
	for partition, previous := range retained {
		assigned := false
		for _, current := range observations {
			if current.Partition != partition || current.Generation != generation {
				continue
			}
			if current.Token != previous.Token || current.Member != previous.Member || current.Boundary == "restored" {
				return false
			}
			if observationAtNine(current) && *current.Checkpoint == *previous.Checkpoint {
				assigned = assigned || current.Boundary == "assigned"
			}
		}
		if !assigned || !retainedFenceCurrent(t, f, previous, generation) {
			return false
		}
	}
	return true
}

func retainedFenceCurrent(t *testing.T, f *cooldownFixture, previous detectorObservation, generation int32) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var member string
	var actualGeneration int32
	var token uint64
	var offset, ordinal int64
	err := f.db.Pool.QueryRow(ctx, `SELECT member_id,generation,assignment_token,frontier_offset,frontier_ordinal
		FROM detection_partition_fences WHERE group_id=$1 AND topic=$2 AND partition_id=$3`,
		f.kafka.DetectorGroup, f.kafka.EventsTopic, previous.Partition).
		Scan(&member, &actualGeneration, &token, &offset, &ordinal)
	return err == nil && member == previous.Member && actualGeneration == generation && token == previous.Token &&
		offset == -1 && ordinal == -1
}

func observedTransition(t *testing.T, dirs []string, boundary string,
	partition int32, member string, token uint64) bool {
	t.Helper()
	for _, dir := range dirs {
		for _, observation := range detectorObservations(t, dir) {
			if observation.Boundary == boundary && observation.Partition == partition &&
				observation.Member == member && (token == 0 || observation.Token == token) && observationAtNine(observation) {
				return true
			}
		}
	}
	return false
}

func recoveryGroup(t *testing.T, f *cooldownFixture) (kadm.DescribedGroup, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	groups, err := f.admin.DescribeGroups(ctx, f.kafka.DetectorGroup)
	return groups[f.kafka.DetectorGroup], err
}

func groupOwns(group kadm.DescribedGroup, memberID, topic string, partition int32) bool {
	for _, member := range group.Members {
		if member.MemberID != memberID {
			continue
		}
		assignment, ok := member.Assigned.AsConsumer()
		if !ok {
			continue
		}
		for _, assigned := range assignment.Topics {
			if assigned.Topic == topic {
				for _, id := range assigned.Partitions {
					if id == partition {
						return true
					}
				}
			}
		}
	}
	return false
}

func observationAtNine(observation detectorObservation) bool {
	if observation.Checkpoint == nil {
		return false
	}
	var m struct {
		ProcessedNext int64 `json:"processed_next"`
	}
	return json.Unmarshal([]byte(observation.Checkpoint.Metadata), &m) == nil && m.ProcessedNext == 9
}

func transferredTarget(t *testing.T, f *cooldownFixture, dir string,
	oldOwners map[int32]string) detectorObservation {
	t.Helper()
	var moved detectorObservation
	require.Eventually(t, func() bool {
		group, describeErr := recoveryGroup(t, f)
		if describeErr != nil || group.Err != nil || len(group.Members) != 3 {
			return false
		}
		for _, observation := range detectorObservations(t, dir) {
			if observation.Boundary == "restored" && observationAtNine(observation) &&
				observation.Member != oldOwners[observation.Partition] && oldOwners[observation.Partition] != "" &&
				groupOwns(group, observation.Member, f.kafka.EventsTopic, observation.Partition) {
				moved = observation
				return true
			}
		}
		return false
	}, 25*time.Second, 25*time.Millisecond, "third replica never restored a seeded target from a different actual owner")
	return moved
}

func incorporatedOwners(t *testing.T, f *cooldownFixture, dirs []string) map[int32]string {
	t.Helper()
	owners := make(map[int32]string)
	require.Eventually(t, func() bool {
		group, err := recoveryGroup(t, f)
		if err != nil || group.Err != nil {
			return false
		}
		observations := make([][]detectorObservation, len(dirs))
		for i, dir := range dirs {
			observations[i] = detectorObservations(t, dir)
		}
		return collectIncorporatedOwners(owners, group, f.kafka.EventsTopic, observations)
	}, 20*time.Second, 25*time.Millisecond, "missing actual per-process incorporated-nine checkpoint observations")
	return owners
}

func collectIncorporatedOwners(owners map[int32]string, group kadm.DescribedGroup, topic string,
	observations [][]detectorObservation) bool {
	// A prior poll's owner can have lost its partition while the new owner's
	// commit observation is still being written. Require one complete current
	// snapshot, with incorporated state in both processes, before causing a move.
	clear(owners)
	if group.Err != nil || group.State != "Stable" || len(group.Members) != 2 || len(observations) != 2 {
		return false
	}
	members := make(map[string]bool)
	for _, process := range observations {
		incorporated := false
		for _, o := range process {
			if o.Boundary == "committed" && o.Topic == topic && o.Partition >= 0 && o.Partition < 12 &&
				observationAtNine(o) && groupOwns(group, o.Member, topic, o.Partition) {
				owners[o.Partition] = o.Member
				members[o.Member] = true
				incorporated = true
			}
		}
		if !incorporated {
			return false
		}
	}
	return len(owners) == 12 && len(members) == 2
}

func seedTransferPartitions(t *testing.T, f *cooldownFixture,
	producer *kgo.Client) (map[int32][]byte, map[int32]*kgo.Record) {
	t.Helper()
	canonical := make(map[int32][]byte)
	tenth := make(map[int32]*kgo.Record)
	for partition := range int32(12) {
		events, payload := recoveryAuthEvents(t, f, fmt.Sprintf("transfer-%s-%d", f.principal, partition))
		canonical[partition] = payload
		for i, event := range events {
			raw, err := json.Marshal(event)
			require.NoError(t, err)
			record := &kgo.Record{Topic: f.kafka.EventsTopic, Partition: partition, Key: []byte("demo|" + f.ip), Value: raw}
			if i == 9 {
				tenth[partition] = record
			} else {
				require.NoError(t, producer.ProduceSync(t.Context(), record).FirstErr())
			}
		}
		recoveryCheckpoint(t, f, partition, 9)
	}
	return canonical, tenth
}
