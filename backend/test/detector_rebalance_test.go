//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
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
		for _, dir := range dirs {
			for _, o := range detectorObservations(t, dir) {
				if o.Boundary == "committed" && observationAtNine(o) && groupOwns(group, o.Member, o.Topic, o.Partition) {
					owners[o.Partition] = o.Member
				}
			}
		}
		return len(owners) == 12
	}, 20*time.Second, 25*time.Millisecond, "missing actual per-process incorporated-nine checkpoint observations")
	return owners
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
